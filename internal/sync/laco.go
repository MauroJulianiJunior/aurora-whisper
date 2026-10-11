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
//   - de tempos em tempos (Intervalo), para trazer o que mudou no celular ou
//     em outro PC;
//   - ao conectar (o nuvem.Servico chama Avisar).
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
	NovoID    func() string
	Agora     func() time.Time
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
	return &Laco{cfg: cfg, log: log, gatilho: make(chan struct{}, 1)}
}

// Avisar acorda o laço: uma gravação local pôs um marcador, ou o PC acabou de
// conectar. Nunca bloqueia (é chamado na goroutine do ditado).
func (l *Laco) Avisar() {
	select {
	case l.gatilho <- struct{}{}:
	default:
	}
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
	proxima := l.cfg.Agora() // a primeira rodada é na subida
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		timer.Reset(proxima.Sub(l.cfg.Agora()))
		select {
		case <-ctx.Done():
			return
		case <-l.gatilho:
			l.contarPendentes(ctx)
			// Numa espera depois de falha, a gravação não fura a espera: a
			// rede caída não vira um pedido por ditado.
			if !emEspera {
				if p := l.cfg.Agora().Add(l.cfg.Debounce); p.Before(proxima) {
					proxima = p
				}
			}
			continue
		case <-timer.C:
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
		proxima = l.cfg.Agora().Add(EsperaDepois(err, falhas, l.cfg.Intervalo, l.cfg.Espera))
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
