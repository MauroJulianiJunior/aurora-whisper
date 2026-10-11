package aprendiz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Troca é o dicionário novo que o Aplicador pede ao runtime depois de cada
// gravação: a semente que está no disco, o Smart montado dela e o prompt do
// ASR (o vocabulário) remontado do mesmo arquivo.
type Troca struct {
	Semente     dictionary.Semente
	Smart       *resolver.SmartResolver
	Vocabulario string
}

// Config é o que Novo usa.
type Config struct {
	// Caminho é o dicionario.json.
	Caminho string
	// Usuario é o dono que o Agent carregou na subida. Vazio é "não havia
	// dicionário na subida": nada é gravado nem trocado
	// (dictionary.ErrSemArquivo), porque o resolver busca por esse usuário e
	// um dicionário que apareceu depois não seria encontrado. Desde a ADR-018
	// o Agent cria um dicionário vazio na subida quando nada existe, e o caso
	// só sobra quando a criação falhou: por isso o Aplicador não cria o
	// arquivo sozinho.
	Usuario string
	// Trocar entrega a Troca ao runtime (no Agent, o winruntime.Dicionario com
	// o Polish do Smart novo). É chamado sob a trava, depois de gravar: duas
	// operações ao mesmo tempo gravam na ordem da trava e pedem a troca na
	// mesma ordem. Não pode bloquear nem chamar o Aplicador. Nil: só grava.
	Trocar func(Troca)
	// Montar monta o Smart do dicionário gravado; nil é SmartDaSemente. Um
	// campo, para o teste poder falhar a montagem de DEPOIS de gravar, que na
	// prática não acontece (o Validar roda a mesma montagem antes de gravar).
	Montar func(dictionary.Semente) (*resolver.SmartResolver, error)
	// ConferirMudanca é a conferência do "Adicionar termo" (ADR-020;
	// dictionary.Arquivo.ConferirMudanca). Nil não confere.
	ConferirMudanca func(antes, depois dictionary.Semente, novas []dictionary.Aprendida) error
	// Log recebe só contagens e códigos; nil descarta.
	Log *slog.Logger
}

// EspelhoDoSync é o que o gancho do painel usa do espelho do sync
// (storage.Fila.SubirVersaoLocal).
type EspelhoDoSync interface {
	SubirVersaoLocal(ctx context.Context, ids []string) error
}

// Aplicador é o dicionario.json do Agent: o Aprender e o Desfazer do painel,
// o "Adicionar termo" e o lote do aprendido que chega da nuvem
// (AplicarPares, a interface que o sync espera: internal/sync,
// DicionarioDoAprendido, na varredura e no Receber).
//
// # Uma trava, uma gravação, uma troca
//
// Toda operação que grava roda sob a MESMA trava (a do painel): grava o
// arquivo (relido, dictionary.Arquivo) e, gravado, pede a troca do dicionário
// e do prompt em memória, antes de soltar a trava. O último dicionário pedido
// é sempre o do último arquivo gravado: sem a trava, o pedido de uma gravação
// mais antiga podia chegar depois e desfazer a mais nova. O dicionário novo
// sai do arquivo RELIDO, e não da cópia da subida: o que o dictlearn gravou
// no meio vale também. Um lote grande do sync faz o clique do painel esperar
// (o risco que a ADR-019 aceita).
//
// # O gancho do painel
//
// Ligado ao sync (LigarAoSync), o Aprender e o Desfazer do painel que gravaram
// sobem a versão local do id no espelho NO ATO, sob a trava, e acordam o laço:
// um Desfazer seguido de um Aprender do mesmo par, que deixa o arquivo igual ao
// que era, continua sendo mudança daqui, e o Receber o pula.
//
// Seguro de qualquer goroutine.
type Aplicador struct {
	arquivo *dictionary.Arquivo
	trocar  func(Troca)
	montar  func(dictionary.Semente) (*resolver.SmartResolver, error)
	log     *slog.Logger

	mu      sync.Mutex
	espelho EspelhoDoSync
	acordar func()
}

var (
	_ dictionary.Aprendizado = (*Aplicador)(nil)
	_ dictionary.Termos      = (*Aplicador)(nil)
)

// Novo monta o Aplicador sobre o dicionário de cfg.
func Novo(cfg Config) *Aplicador {
	a := &Aplicador{trocar: cfg.Trocar, montar: cfg.Montar, log: cfg.Log}
	if a.montar == nil {
		a.montar = SmartDaSemente
	}
	if a.log == nil {
		a.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Usuario != "" {
		a.arquivo = &dictionary.Arquivo{
			Caminho: cfg.Caminho,
			Usuario: cfg.Usuario,
			// O que não carrega no repositório em memória não chega ao disco.
			Validar: Validar,
			// O que o painel responde "aprendido" tem de AGIR.
			ValidarAprendida: Agiria,
			// O "Adicionar termo": cada grafia nova age, e nada que agia
			// deixa de agir.
			ConferirMudanca: cfg.ConferirMudanca,
			// E nada que só tenha palavra de ligação, polaridade ou número.
			PalavraComum: PalavraComum,
			// O par que chega da nuvem tem de agir aqui, com a restrição
			// efetiva dele.
			ValidarPar: ValidarPar,
		}
	}
	return a
}

// TemArquivo diz se há dicionário: sem ele (o usuário vazio da subida), tudo
// é dictionary.ErrSemArquivo. O Agent o passa ao sync assim mesmo: para a
// varredura e o Receber, é o arquivo indisponível (o M desconhecido, e o par
// da nuvem guardado como pendente).
func (a *Aplicador) TemArquivo() bool { return a.arquivo != nil }

// LigarAoSync liga o gancho do painel ao espelho do sync: cada Aprender e
// Desfazer que gravou sobe, no ato e sob a trava, a versão local do id em esp
// (o id sem linha no espelho não ganha linha: a varredura o vê como par novo)
// e chama acordar (o laço, que nunca bloqueia). Uma falha do espelho é
// registrada, sem texto, e o painel segue: a varredura vê a mudança pelo
// arquivo, menos a que deixa o arquivo como era.
func (a *Aplicador) LigarAoSync(esp EspelhoDoSync, acordar func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.espelho, a.acordar = esp, acordar
}

// Aprender implementa dictionary.Aprendizado.
func (a *Aplicador) Aprender(ctx context.Context, termo, variante string) (dictionary.Aprendida, error) {
	var feito dictionary.Aprendida
	err := a.gravarETrocar(ctx, "aprender", func(arq *dictionary.Arquivo) (dictionary.Semente, []string, error) {
		s, f, err := arq.Aprender(termo, variante)
		feito = f
		return s, []string{dictionary.IDDoPar(f.Variante)}, err
	})
	return comOFeito(feito, err)
}

// Desfazer implementa dictionary.Aprendizado.
func (a *Aplicador) Desfazer(ctx context.Context, variante string) (dictionary.Aprendida, error) {
	var feito dictionary.Aprendida
	err := a.gravarETrocar(ctx, "desfazer", func(arq *dictionary.Arquivo) (dictionary.Semente, []string, error) {
		s, f, err := arq.Desfazer(variante)
		feito = f
		return s, []string{dictionary.IDDoPar(f.Variante)}, err
	})
	return comOFeito(feito, err)
}

// comOFeito devolve o que ficou gravado só quando foi gravado: sem erro, ou
// com ErrGravadoSemTroca. Com qualquer outro erro, o zero.
func comOFeito[T any](feito T, err error) (T, error) {
	if err != nil && !errors.Is(err, dictionary.ErrGravadoSemTroca) {
		var zero T
		return zero, err
	}
	return feito, err
}

// Aprendidas implementa dictionary.Aprendizado. Sem dicionário, a lista é
// vazia: não há o que desfazer, e a tela não precisa de um erro para isso.
func (a *Aplicador) Aprendidas(ctx context.Context) ([]dictionary.Aprendida, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.arquivo == nil {
		return []dictionary.Aprendida{}, nil
	}
	lista, err := a.arquivo.Aprendidas()
	if errors.Is(err, dictionary.ErrSemArquivo) {
		return []dictionary.Aprendida{}, nil
	}
	return lista, err
}

// Dicionario é o dicionário como está em disco agora (o mesmo arquivo que
// Aprender relê), sem tocá-lo: o que a API do painel passa ao motor de
// sugestão. Sem dicionário na subida, é dictionary.ErrSemArquivo, e a edição
// sai sem sugestão.
func (a *Aplicador) Dicionario(ctx context.Context) (dictionary.Semente, error) {
	if err := ctx.Err(); err != nil {
		return dictionary.Semente{}, err
	}
	if a.arquivo == nil {
		return dictionary.Semente{}, dictionary.ErrSemArquivo
	}
	return a.arquivo.Atual()
}

// ListarTermos implementa dictionary.Termos. Sem dicionário na subida, é
// dictionary.ErrSemArquivo: a tela tem de dizer que não há onde adicionar, e
// não mostrar uma lista vazia como se desse.
func (a *Aplicador) ListarTermos(ctx context.Context) (dictionary.ListaDeTermos, error) {
	if err := ctx.Err(); err != nil {
		return dictionary.ListaDeTermos{}, err
	}
	if a.arquivo == nil {
		return dictionary.ListaDeTermos{}, dictionary.ErrSemArquivo
	}
	return a.arquivo.ListarTermos()
}

// AdicionarTermo implementa dictionary.Termos.
func (a *Aplicador) AdicionarTermo(ctx context.Context, p dictionary.PedidoTermo) (dictionary.MudancaDeTermos, error) {
	return a.mudarTermos(ctx, "adicionar_termo", func(arq *dictionary.Arquivo) (dictionary.Semente, dictionary.MudancaDeTermos, error) {
		return arq.AdicionarTermo(p)
	})
}

// DicaDoTermo implementa dictionary.Termos.
func (a *Aplicador) DicaDoTermo(ctx context.Context, termo string, ligar bool) (dictionary.MudancaDeTermos, error) {
	return a.mudarTermos(ctx, "dica_do_termo", func(arq *dictionary.Arquivo) (dictionary.Semente, dictionary.MudancaDeTermos, error) {
		return arq.DicaDoTermo(termo, ligar)
	})
}

// RemoverTermo implementa dictionary.Termos.
func (a *Aplicador) RemoverTermo(ctx context.Context, termo string) (dictionary.MudancaDeTermos, error) {
	return a.mudarTermos(ctx, "remover_termo", func(arq *dictionary.Arquivo) (dictionary.Semente, dictionary.MudancaDeTermos, error) {
		return arq.RemoverTermo(termo)
	})
}

func (a *Aplicador) mudarTermos(ctx context.Context, acao string,
	op func(*dictionary.Arquivo) (dictionary.Semente, dictionary.MudancaDeTermos, error)) (dictionary.MudancaDeTermos, error) {
	var m dictionary.MudancaDeTermos
	err := a.gravarETrocar(ctx, acao, func(arq *dictionary.Arquivo) (dictionary.Semente, []string, error) {
		s, feito, err := op(arq)
		m = feito
		return s, nil, err
	})
	return comOFeito(m, err)
}

// gravarETrocar roda op (que grava o arquivo) e, gravado, sobe a versão local
// dos ids que op mudou no espelho do sync (o gancho do painel) e troca o
// dicionário e o prompt do ASR em memória, tudo sob a trava. Devolve o erro
// de op, ou dictionary.ErrGravadoSemTroca quando o arquivo foi gravado e a
// troca não.
//
// O prompt é remontado do arquivo relido em TODA gravação, e não só nas do
// "Adicionar termo": é o mesmo caminho da subida (abrirDicionarioEm). O
// aprender pelo histórico não o muda (a variante não entra no prompt, e o
// termo do painel é AliasOnly). Uma edição feita à mão no arquivo desde a
// subida passa a valer no prompt já na próxima gravação, e não só ao
// reiniciar (ADR-020).
func (a *Aplicador) gravarETrocar(ctx context.Context, acao string,
	op func(*dictionary.Arquivo) (dictionary.Semente, []string, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.arquivo == nil {
		return dictionary.ErrSemArquivo
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	semente, ids, err := op(a.arquivo)
	if err != nil {
		return err
	}
	a.mudouAqui(ctx, ids)
	return a.trocarPor(semente, acao)
}

// mudouAqui é o gancho do painel, sob a trava: a versão local de cada id sobe
// no espelho, e o laço acorda. O contexto do pedido não o corta: o arquivo já
// foi gravado.
func (a *Aplicador) mudouAqui(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	if a.espelho != nil {
		if err := a.espelho.SubirVersaoLocal(context.WithoutCancel(ctx), ids); err != nil {
			a.log.Warn("aprendizado: não consegui anotar a mudança no espelho do sync; a varredura a vê pelo arquivo", "err", err)
		}
	}
	if a.acordar != nil {
		a.acordar()
	}
}

// trocarPor monta o Smart e o prompt da semente gravada e os entrega ao
// runtime. Sob a trava.
func (a *Aplicador) trocarPor(semente dictionary.Semente, acao string) error {
	smart, err := a.montar(semente)
	if err != nil {
		// Não deveria acontecer: Validar acabou de provar que carrega. Mas
		// o arquivo já foi gravado, e quem chama precisa saber que a pessoa
		// JÁ aprendeu (a lista do painel mostra o par): devolve o que ficou
		// gravado junto com o erro, e a tela diz que só vale depois de
		// reiniciar o Agent, em vez de "não deu para aprender". No sync, é o
		// lote que não terminou (a varredura seguinte reconcilia).
		a.log.Warn("aprendizado: gravado, mas o dicionário em memória não foi trocado", "acao", acao)
		// A causa não vai no erro: pode citar termo do dicionário, e quem
		// trata o erro pode registrá-lo.
		return dictionary.ErrGravadoSemTroca
	}
	entradas := semente.ParaEntradas()
	noPrompt := dictionary.VocabularioTermos(entradas, dictionary.OrcamentoRunasPadrao)
	vocab := dictionary.Vocabulario(entradas, dictionary.OrcamentoRunasPadrao)
	if a.trocar != nil {
		a.trocar(Troca{Semente: semente, Smart: smart, Vocabulario: vocab})
	}
	a.log.Info("aprendizado: dicionário atualizado; vale a partir do próximo ditado",
		"acao", acao,
		"termos", len(semente.Entradas),
		"variantes_do_painel", len(semente.VariantesDoPainel()),
		"termos_no_prompt", len(noPrompt),
		"vocabulario_runas", utf8.RuneCountInString(vocab))
	return nil
}

// AplicarPares é o lote do aprendido que chega da nuvem (a interface que o
// sync espera: internal/sync, DicionarioDoAprendido), sob a MESMA trava do
// painel: o arquivo relido (dictionary.Arquivo.AplicarPares, com as
// conferências do Agent: o Validar, o ValidarPar e a palavra comum), uma
// gravação só quando algo muda, e então uma troca do dicionário e do prompt.
// escolher e antesDeGravar são os do sync, chamados sob a trava.
//
// O erro pode voltar com o arquivo já gravado: a troca em memória que falha
// depois da gravação é dictionary.ErrGravadoSemTroca, e o sync o trata como o
// lote que não terminou (a linha "aplicando" fica, e a varredura seguinte a
// confirma pelo arquivo). Sem dicionário, é dictionary.ErrSemArquivo, que o
// sync lê como o arquivo indisponível.
//
// O lote não passa pelo gancho do painel: o que o aplicador escreve não é
// mudança daqui (o sync guarda a projeção esperada antes de gravar).
func (a *Aplicador) AplicarPares(escolher func(atual dictionary.Semente) []dictionary.ParDaNuvem,
	antesDeGravar func(dictionary.Lote) error) (dictionary.Semente, dictionary.Lote, error) {
	if a.arquivo == nil {
		return dictionary.Semente{}, dictionary.Lote{}, dictionary.ErrSemArquivo
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	semente, lote, err := a.arquivo.AplicarPares(escolher, antesDeGravar)
	if err != nil {
		return semente, lote, err
	}
	a.registrarLote(lote)
	if !lote.Mudou {
		return semente, lote, nil
	}
	return semente, lote, a.trocarPor(semente, "sync")
}

// registrarLote registra o que o lote fez, só em contagens: por situação e,
// no "não aplicado", por motivo (a lista fechada do dictionary). Nunca a
// variante, o termo ou o domínio. Em Info só quando o lote gravou: a
// varredura tenta de novo, a cada rodada, o par que continua não valendo
// aqui, e isso não muda nada.
func (a *Aplicador) registrarLote(l dictionary.Lote) {
	if len(l.Resultados) == 0 {
		return
	}
	por := map[dictionary.SituacaoDoPar]int{}
	motivos := map[string]int{}
	for _, r := range l.Resultados {
		por[r.Situacao]++
		if r.Situacao == dictionary.SituacaoNaoAplicado {
			motivos[string(r.Motivo)]++
		}
	}
	nivel := slog.LevelDebug
	if l.Mudou {
		nivel = slog.LevelInfo
	}
	a.log.Log(context.Background(), nivel, "aprendizado: pares da nuvem conferidos",
		"pares", len(l.Resultados),
		"aplicados", por[dictionary.SituacaoAplicado],
		"desfeitos", por[dictionary.SituacaoDesfeito],
		"convergentes", por[dictionary.SituacaoConvergente],
		"ja_vale_aqui", por[dictionary.SituacaoJaValeAqui],
		"ausentes", por[dictionary.SituacaoAusente],
		"nao_aplicados", por[dictionary.SituacaoNaoAplicado],
		"nao_aplicados_por_motivo", contagens(motivos),
		"gravou", l.Mudou)
}

// contagens é "codigo:n,codigo:n", em ordem.
func contagens(m map[string]int) string {
	cs := make([]string, 0, len(m))
	for c := range m {
		cs = append(cs, c)
	}
	sort.Strings(cs)
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%d", c, m[c])
	}
	return b.String()
}
