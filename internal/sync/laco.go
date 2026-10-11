package syncengine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O laço de sync em segundo plano (ADR-019, F7b). Uma goroutine montada pelo
// cmd/agent, só com o histórico aberto. Ele NUNCA atrasa nem quebra o ditado:
// o ditado grava a linha (e o marcador) no SQLite e segue; o laço só é
// acordado, sem bloquear (Avisar), e faz a rede depois, fora de qualquer
// transação.
//
// Quando roda:
//   - na subida;
//   - depois de cada gravação local que pôs um marcador (Avisar), com um
//     intervalo curto (Debounce) para juntar as que vêm em seguida;
//   - de tempos em tempos, para trazer o que mudou no celular ou em outro PC:
//     a cada Intervalo (60 s) e, enquanto o painel do PC está aberto e lendo o
//     contador de mudanças, a cada IntervaloComPainel (10 s) (ADR-021, S);
//   - ao conectar (o nuvem.Servico chama Avisar).
//
// O painel aberto é quem lê o contador (LerMudancas, a GET /api/nuvem/mudancas
// da API local): a leitura é o aviso. Passada a JanelaDoPainel (15 s) sem
// leitura, o laço volta aos 60 s. O contador sobe quando uma rodada grava no
// histórico daqui algo que veio da nuvem (Relatorio.AplicouDaNuvem), e é com
// ele que o painel sabe que deve reler a lista.
//
// Desconectado, nada roda e nenhum pedido sai (e na fila só entra a mudança
// num ditado que a nuvem já tem, para subir na volta: quem decide isso é a
// gravação, storage.emitir). Uma rodada
// que falha espera (BackoffPolicy, com jitter) e não deixa a próxima
// gravação furar a espera; um 429 espera o que a nuvem pediu; um 401
// desconecta (Conexao.TokenRecusado) e o laço volta a só esperar.

// Credencial é o que o laço precisa da conexão do PC com a nuvem, lida a cada
// rodada (o token vem do Gerenciador de Credenciais, atrás do portão).
type Credencial struct {
	// Configurada: a instalação já foi ligada a uma nuvem (a URL existe).
	Configurada bool
	// Conectado: o aparelho está na instalação e o token no Cofre.
	Conectado bool
	URL       string
	Token     string
	Aparelho  string
	PareadoEm string
}

// Conexao é o lado do pareamento (no Agent, o nuvem.Servico).
type Conexao interface {
	Credencial(ctx context.Context) (Credencial, error)
	// TokenRecusado é o 401: se o token ainda for o guardado, ele sai do
	// Cofre, e o PC fica desconectado (o mesmo caminho do Conferir).
	TokenRecusado(ctx context.Context, token string) error
}

// Os tempos do laço.
const (
	// IntervaloPadrao: a busca periódica (decisão 1 da F7b, a recomendada).
	IntervaloPadrao = 60 * time.Second
	// IntervaloComPainelPadrao: a busca periódica com o painel do PC aberto e
	// lendo o contador (ADR-021, decisão P e escolha menor 17, aprovada pelo
	// Mauro em 10-out-2026). A alternativa que ele não escolheu é 30 s sempre:
	// os dois valores iguais a 30 s.
	IntervaloComPainelPadrao = 10 * time.Second
	// JanelaDoPainelPadrao: por quanto tempo uma leitura do contador mantém o
	// painel como aberto. O painel lê a cada 3 s, então 15 s perdoam a leitura
	// que atrasou e fecham logo depois que ele sai de vista.
	JanelaDoPainelPadrao = 15 * time.Second
	// DebouncePadrao: a espera depois de uma gravação local.
	DebouncePadrao = 2 * time.Second
)

// ConfigDoLaco é o que o Laco usa. Fila, Conexao e NovoID são obrigatórios.
type ConfigDoLaco struct {
	Fila    *storage.Fila
	Conexao Conexao
	// Cliente monta o SyncClient de uma rodada; nil é o Cliente HTTPS sobre
	// HTTP (nil: NovoHTTP()).
	Cliente func(base, token string) SyncClient
	HTTP    *http.Client
	// Intervalo, Debounce e Espera: zero é o padrão.
	Intervalo time.Duration
	Debounce  time.Duration
	Espera    BackoffPolicy
	// IntervaloComPainel é o Intervalo enquanto o painel do PC lê o contador
	// (LerMudancas); zero é o padrão, e nunca passa de Intervalo.
	IntervaloComPainel time.Duration
	// JanelaDoPainel é quanto tempo uma leitura do contador vale como "o
	// painel está aberto"; zero é o padrão.
	JanelaDoPainel time.Duration
	NovoID         func() string
	Agora          func() time.Time
	// Dicionario é o dicionario.json do aprendido (Motor.Dicionario); nil
	// deixa o aprendido de fora, como na F7. Quem o liga é o Agent (F8g).
	Dicionario DicionarioDoAprendido
	// Log recebe só contagens e códigos; nil descarta.
	Log *slog.Logger
}

// A situação que a bandeja mostra.
const (
	situacaoNenhuma int32 = iota
	situacaoDesconectada
	situacaoSemConexao
	situacaoEmDia
	// situacaoAguardando: a nuvem respondeu 429 (pediu para esperar).
	situacaoAguardando
	// situacaoRespostaInesperada: a nuvem respondeu, mas fora do contrato (um
	// 400, 403 ou 404, um JSON que não se lê, um recomeço que não resolveu).
	situacaoRespostaInesperada
)

// falhando diz se a situação é a de uma rodada que não terminou.
func falhando(s int32) bool {
	return s == situacaoSemConexao || s == situacaoAguardando || s == situacaoRespostaInesperada
}

// situacaoDaFalha é a linha da bandeja para a rodada que terminou com err (que
// não é o 401): o 429 é "aguardando", a resposta fora do contrato é "resposta
// inesperada", e o resto (a rede, o prazo, um 5xx, um erro local) é "sem
// conexão". Só o texto muda; a espera é a de EsperaDepois.
func situacaoDaFalha(err error) int32 {
	var e *ErrEspere
	switch {
	case errors.As(err, &e):
		return situacaoAguardando
	case errors.Is(err, ErrContrato), errors.Is(err, ErrRecomeco), errors.Is(err, storage.ErrEpochDiferente):
		return situacaoRespostaInesperada
	}
	return situacaoSemConexao
}

// Laco é o laço de sync. Seguro para uso concorrente.
type Laco struct {
	cfg     ConfigDoLaco
	log     *slog.Logger
	gatilho chan struct{}
	// painel acorda o laço quando o painel acaba de abrir (a primeira leitura
	// do contador depois da JanelaDoPainel sem nenhuma): a próxima busca não
	// espera os 60 s que já corriam.
	painel chan struct{}
	// despertador arma uma espera; os testes trocam o relógio inteiro por um
	// falso (o Agora da configuração e este).
	despertador func(d time.Duration) (espera <-chan time.Time, desarmar func())

	// mudancas é o contador da API local (LerMudancas): sobe, uma vez por
	// rodada, quando ela grava no histórico algo que veio da nuvem. Só em
	// memória: recomeça do zero a cada execução do Agent, e o painel, que
	// compara com a última leitura, só precisa de "mudou".
	mudancas atomic.Uint64
	// lidoEm é o instante (UnixNano) da última leitura do contador pelo
	// painel; 0, nunca.
	lidoEm atomic.Int64

	situacao  atomic.Int32
	pendentes atomic.Int64
	// feitos e total: o envio inicial em curso (Fila.Progresso); total zero
	// quando não há.
	feitos atomic.Int64
	total  atomic.Int64
	// freio: os pares que o freio da última varredura que comparou o arquivo
	// segurou (Relatorio.Freio); 0 sem o freio. Só em memória.
	freio atomic.Int64
}

// NovoLaco monta o laço. Faltar um obrigatório é erro de quem monta (panic).
func NovoLaco(cfg ConfigDoLaco) *Laco {
	switch {
	case cfg.Fila == nil:
		panic("sync: Fila nil")
	case cfg.Conexao == nil:
		panic("sync: Conexao nil")
	case cfg.NovoID == nil:
		panic("sync: NovoID nil")
	}
	if cfg.Intervalo <= 0 {
		cfg.Intervalo = IntervaloPadrao
	}
	if cfg.IntervaloComPainel <= 0 {
		cfg.IntervaloComPainel = IntervaloComPainelPadrao
	}
	// O painel aberto nunca deixa a busca mais lenta do que ela já é.
	if cfg.IntervaloComPainel > cfg.Intervalo {
		cfg.IntervaloComPainel = cfg.Intervalo
	}
	if cfg.JanelaDoPainel <= 0 {
		cfg.JanelaDoPainel = JanelaDoPainelPadrao
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = DebouncePadrao
	}
	if cfg.Espera.Base <= 0 && cfg.Espera.Max <= 0 {
		r := cfg.Espera.Rand
		cfg.Espera = DefaultBackoffPolicy
		cfg.Espera.Rand = r
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	if cfg.Cliente == nil {
		h := cfg.HTTP
		if h == nil {
			h = NovoHTTP()
		}
		cfg.Cliente = func(base, token string) SyncClient { return NovoCliente(base, token, h) }
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Laco{
		cfg: cfg, log: log, gatilho: make(chan struct{}, 1), painel: make(chan struct{}, 1),
		despertador: despertadorReal,
	}
}

// despertadorReal arma um time.Timer; uma espera zero ou negativa dispara já.
func despertadorReal(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// Avisar acorda o laço: uma gravação local pôs um marcador, ou o PC acabou de
// conectar. Nunca bloqueia (é chamado na goroutine do ditado).
func (l *Laco) Avisar() {
	select {
	case l.gatilho <- struct{}{}:
	default:
	}
}

// LerMudancas é a leitura que o painel do PC faz do contador de mudanças (a
// GET /api/nuvem/mudancas da API local; ADR-021, subfatia S): devolve quantas
// rodadas gravaram no histórico daqui algo que veio da nuvem, nesta execução
// do Agent. O painel relê a lista quando o número muda. Não leva texto, id,
// token nem URL: é um número.
//
// A própria leitura é o aviso de que o painel está aberto: nos próximos
// JanelaDoPainel o laço busca a nuvem a cada IntervaloComPainel. A primeira
// leitura depois de uma janela sem nenhuma também acorda o laço, para a busca
// seguinte não esperar o que sobrava dos 60 s. Nunca bloqueia (é chamada pela
// goroutine de HTTP).
func (l *Laco) LerMudancas() uint64 {
	agora := l.cfg.Agora()
	if !l.aberto(l.lidoEm.Swap(agora.UnixNano()), agora) {
		select {
		case l.painel <- struct{}{}:
		default:
		}
	}
	return l.mudancas.Load()
}

// aberto diz se a leitura de lidoEm (UnixNano; 0, nunca) ainda vale, em agora,
// como "o painel está aberto": menos de JanelaDoPainel atrás. Uma leitura no
// futuro (o relógio do PC voltou) não vale: a próxima leitura do painel, em
// poucos segundos, a refaz.
func (l *Laco) aberto(lidoEm int64, agora time.Time) bool {
	if lidoEm == 0 {
		return false
	}
	d := agora.Sub(time.Unix(0, lidoEm))
	return d >= 0 && d < l.cfg.JanelaDoPainel
}

// intervaloEm é a espera depois de uma rodada que deu certo e terminou em
// agora: a curta com o painel aberto e lendo, a de sempre sem ele.
func (l *Laco) intervaloEm(agora time.Time) time.Duration {
	if l.aberto(l.lidoEm.Load(), agora) {
		return l.cfg.IntervaloComPainel
	}
	return l.cfg.Intervalo
}

// Resumo é a linha da bandeja: "Nuvem: em dia", "Nuvem: 3 para enviar",
// "Nuvem: enviando 120 de 800" (o envio inicial, F7c; também no 429, que é a
// nuvem pedindo para esperar no meio dele), "Nuvem: sem conexão", "Nuvem:
// aguardando" (o 429), "Nuvem: resposta inesperada" (a nuvem no ar, mas fora
// do contrato), "Nuvem: desconectada", ou "" quando a instalação nunca foi
// ligada a uma nuvem (ou antes da primeira rodada). Sem conexão, desconectada
// e a resposta inesperada vencem o progresso. Só lê memória.
func (l *Laco) Resumo() string {
	s := l.situacao.Load()
	switch s {
	case situacaoDesconectada:
		return "Nuvem: desconectada"
	case situacaoSemConexao:
		return "Nuvem: sem conexão"
	case situacaoRespostaInesperada:
		return "Nuvem: resposta inesperada"
	}
	if total := l.total.Load(); total > 0 {
		return "Nuvem: enviando " + milhar(l.feitos.Load()) + " de " + milhar(total)
	}
	switch s {
	case situacaoAguardando:
		return "Nuvem: aguardando"
	case situacaoEmDia:
		if n := l.pendentes.Load(); n > 0 {
			return "Nuvem: " + strconv.FormatInt(n, 10) + " para enviar"
		}
		return "Nuvem: em dia"
	}
	return ""
}

// Freio é o aviso do freio do aprendido para o painel (o historico.freio_trocas
// do GET /api/nuvem, 06): quantos pares saíram do dicionario.json de uma vez,
// sem o Desfazer do painel, na última varredura que comparou o arquivo, e que
// por isso nada do aprendido grava nem sobe (ResultadoDaVarredura.Freio). 0
// sem o freio, e também antes da primeira varredura desta execução: não é
// guardado além da memória, e some na varredura que o solta. Só lê memória.
func (l *Laco) Freio() int { return int(l.freio.Load()) }

// Rodar é o laço; volta quando ctx acaba.
func (l *Laco) Rodar(ctx context.Context) {
	falhas := 0
	emEspera := false
	// livre: a última rodada deu certo (e não foi o "desconectado"). Só então a
	// abertura do painel adianta a próxima: depois de uma falha a espera é do
	// backoff, e desconectado não há o que buscar.
	livre := false
	var fim time.Time        // quando a última rodada terminou
	proxima := l.cfg.Agora() // a primeira rodada é na subida
	for {
		espera, desarmar := l.despertador(proxima.Sub(l.cfg.Agora()))
		select {
		case <-ctx.Done():
			desarmar()
			return
		case <-l.gatilho:
			desarmar()
			l.contarPendentes(ctx)
			// Numa espera depois de falha, a gravação não fura a espera: a
			// rede caída não vira um pedido por ditado.
			if !emEspera {
				if p := l.cfg.Agora().Add(l.cfg.Debounce); p.Before(proxima) {
					proxima = p
				}
			}
			continue
		case <-l.painel:
			desarmar()
			// O painel acabou de abrir: a busca seguinte é a de quem o tem
			// aberto, contada do fim da última rodada, e não os 60 s que já
			// corriam. Se isso já passou, a busca sai agora.
			if livre {
				if p := fim.Add(l.cfg.IntervaloComPainel); p.Before(proxima) {
					proxima = p
				}
			}
			continue
		case <-espera:
		}

		err := l.rodada(ctx)
		if ctx.Err() != nil {
			return
		}
		switch {
		case err == nil, errors.Is(err, errDesconectado), errors.Is(err, ErrNaoAutorizado):
			falhas, emEspera = 0, false
		default:
			falhas++
			emEspera = true
		}
		// Só a rodada que deu certo usa a espera curta do painel aberto; as
		// outras (a falha, o desconectado, o 401) têm a espera delas.
		fim = l.cfg.Agora()
		intervalo := l.cfg.Intervalo
		if err == nil {
			intervalo = l.intervaloEm(fim)
		}
		livre = err == nil
		proxima = fim.Add(EsperaDepois(err, falhas, intervalo, l.cfg.Espera))
	}
}

// RodadaAgora roda UMA rodada já, na goroutine de quem chama, com tudo o que
// a rodada do laço faz (a credencial, o motor, o 401, o log e a bandeja).
// Para os testes de ponta a ponta e o diagnóstico; o Agent usa Rodar. Não
// pode ser chamada junto com Rodar.
func (l *Laco) RodadaAgora(ctx context.Context) error {
	err := l.rodada(ctx)
	if errors.Is(err, errDesconectado) {
		return nil
	}
	return err
}

// errDesconectado: a rodada não rodou, porque o PC não está conectado.
var errDesconectado = errors.New("sync: o PC não está conectado à nuvem")

// rodada é uma volta: a credencial, o push e o pull, o log e a situação.
func (l *Laco) rodada(ctx context.Context) error {
	cred, err := l.cfg.Conexao.Credencial(ctx)
	if err != nil {
		if !falhando(l.situacao.Swap(situacaoSemConexao)) {
			l.log.Warn("sync: não consegui ler a conexão com a nuvem; tento de novo depois", "err", err)
		}
		return err
	}
	if !cred.Conectado {
		if cred.Configurada {
			l.situacao.Store(situacaoDesconectada)
		} else {
			l.situacao.Store(situacaoNenhuma)
		}
		return errDesconectado
	}
	antes := l.situacao.Load()
	m := &Motor{
		Fila: l.cfg.Fila, Cliente: l.cfg.Cliente(cred.URL, cred.Token),
		Aparelho: cred.Aparelho, PareadoEm: cred.PareadoEm, NovoID: l.cfg.NovoID, Agora: l.cfg.Agora,
		AoLote: func() { l.contarPendentes(ctx) }, Dicionario: l.cfg.Dicionario,
	}
	rel, err := m.Rodada(ctx)
	if rel.FreioConferido {
		l.freio.Store(int64(rel.Freio))
	}
	// O contador do painel sobe depois da gravação (o Rodada só conta o que já
	// foi para o SQLite), mesmo numa rodada que falhou mais adiante: as páginas
	// que ela aplicou antes já estão no histórico.
	if rel.AplicouDaNuvem() > 0 {
		l.mudancas.Add(1)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	l.contarPendentes(ctx)
	codigo := CodigoDoErro(err)
	if rerr := l.cfg.Fila.RegistrarRodada(ctx, cred.Aparelho, l.cfg.Agora(), codigo, rel.Teve()); rerr != nil {
		l.log.Warn("sync: não consegui anotar a rodada", "err", rerr)
	}
	campos := camposDoLog(rel, l.pendentes.Load())
	switch {
	case err == nil:
		l.situacao.Store(situacaoEmDia)
		switch {
		case falhando(antes):
			l.log.Info("sync: a nuvem voltou a responder", campos...)
		case rel.Teve():
			l.log.Info("sync: rodada", campos...)
		}
		return nil
	case errors.Is(err, ErrNaoAutorizado):
		// 401: o PC foi revogado (ou o token venceu). O caminho é o do
		// Conferir: o token sai do Cofre, o aparelho da instalação, e o
		// ditado segue local. Daí em diante só a mudança num ditado que a
		// nuvem já tem entra na fila (storage.emitir), para subir na volta.
		l.log.Info("sync: a nuvem recusou o token; desconectado neste PC", campos...)
		if terr := l.cfg.Conexao.TokenRecusado(ctx, cred.Token); terr != nil {
			l.log.Warn("sync: não consegui desconectar depois do 401", "err", terr)
		}
		l.situacao.Store(situacaoDesconectada)
		return err
	default:
		l.situacao.Store(situacaoDaFalha(err))
		if !falhando(antes) {
			l.log.Warn("sync: a rodada não terminou; tento de novo depois", append(campos, "codigo", codigo)...)
		} else {
			l.log.Debug("sync: a rodada não terminou de novo", append(campos, "codigo", codigo)...)
		}
		return err
	}
}

// contarPendentes atualiza a contagem e o progresso do envio inicial da
// bandeja (na goroutine do laço).
func (l *Laco) contarPendentes(ctx context.Context) {
	if n, err := l.cfg.Fila.Contar(ctx); err == nil {
		l.pendentes.Store(int64(n))
	}
	if feitos, total, err := l.cfg.Fila.Progresso(ctx); err == nil {
		l.feitos.Store(int64(feitos))
		l.total.Store(int64(total))
	}
}

// milhar escreve n com o ponto do milhar, como o painel ("7.000").
func milhar(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 || len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// camposDoLog são as contagens de uma rodada, para o log. Nunca texto, id,
// token, URL ou e-mail.
func camposDoLog(r Relatorio, pendentes int64) []any {
	return []any{
		"enviados", r.Enviados,
		"aplicados", r.Push[TipoAplicado],
		"mesclados", r.Push[TipoMesclado],
		"conflitos", r.Push[TipoConflito],
		"recusados", r.RecusadosEmTexto(),
		"reenviar", r.Push[TipoReenviar],
		"gone_apagados", r.Push[TipoApagado],
		"gone_fora_da_janela", r.Push[TipoForaDaJanela],
		"recebidos", r.Recebidos,
		"aplicados_do_pull", r.Pull[TipoRecebido] + r.Pull[TipoApagado],
		"pulados", r.Pull[TipoPulado],
		"ecos", r.Pull[TipoEco],
		"reconciliados", r.Pull[TipoReconciliado],
		"nao_aplicados", r.Pull[TipoNaoAplicado],
		"paginas", r.Paginas,
		"recomecos", r.Recomecos,
		"releituras", r.Releituras,
		"reconexoes", r.Reconexoes,
		"completados", r.Completados,
		"pendentes", pendentes,
		"aprendido_marcados", r.Varredura[TipoVarreduraUpsert] + r.Varredura[TipoVarreduraDelete],
		"aprendido_a_subir", r.Trocas,
		"aprendido_reaplicados", soma(r.Reaplicados),
		"aprendido_freio", r.Freio,
		"aprendido_sem_dicionario", r.SemDicionario,
		"aprendido_aplicados", r.PullDoAprendido[TipoAplicado] + r.PullDoAprendido[TipoTrocado],
		"aprendido_desfeitos", r.PullDoAprendido[TipoDesfeito],
		"aprendido_nao_aplicados", r.PullDoAprendido[TipoNaoAplicado] + r.PullDoAprendido[TipoPendente],
		"aprendido_nao_aplicados_por_motivo", r.NaoAplicadosEmTexto(),
		"aprendido_pulados", r.PullDoAprendido[TipoPulado],
	}
}

// EsperaDepois decide quanto o laço espera depois de uma rodada que terminou
// com err (nil: deu certo), sendo falhas o número de falhas seguidas, já
// contando esta:
//   - deu certo, ou o PC não está conectado (ou acabou de ser desconectado
//     pelo 401): o intervalo;
//   - 429: o que a nuvem pediu, com até 25% a mais de jitter (no máximo 30
//     s), nunca menos;
//   - a resposta fora do contrato, ou o recomeço que não resolveu: a espera
//     longa (o teto da política, com jitter);
//   - o resto (a rede, o prazo, um 5xx, um erro local): a espera
//     exponencial da política.
func EsperaDepois(err error, falhas int, intervalo time.Duration, p BackoffPolicy) time.Duration {
	var e *ErrEspere
	switch {
	case err == nil, errors.Is(err, errDesconectado), errors.Is(err, ErrNaoAutorizado):
		return intervalo
	case errors.As(err, &e):
		extra := e.Espera / 4
		if extra > 30*time.Second {
			extra = 30 * time.Second
		}
		return e.Espera + time.Duration(p.rand()*float64(extra))
	case errors.Is(err, ErrContrato), errors.Is(err, ErrRecomeco), errors.Is(err, storage.ErrEpochDiferente):
		return p.Next(1 << 10)
	default:
		return p.Next(falhas)
	}
}
