package inserter

// Este arquivo não tem build tag: é a lógica de decisão — qual caminho
// para qual tamanho, o que fazer quando um falha, quando desistir — pura,
// sem nenhuma chamada de sistema, testada contra dublês em
// strategy_test.go (ver doc.go).

import (
	"log/slog"
	"strings"
	"unicode/utf16"
)

// DefaultUnicodeThreshold é o limiar default entre os dois caminhos,
// contado em UTF-16 code units. [ASSUMPTION] — ver doc.go, "limiar entre
// os dois caminhos": não foi medido em aplicativo real; é um valor
// defensável (texto curto o bastante para que uma sequência de SendInput
// caractere-a-caractere não seja perceptivelmente lenta nem frágil),
// deixado configurável via Config.UnicodeThreshold para quando a medição
// da matriz mínima estiver disponível (registrado em open_questions).
const DefaultUnicodeThreshold = 25

// Method identifica qual camada efetivamente inseriu o texto.
type Method int

const (
	// MethodNone: nada foi inserido — texto vazio (sucesso trivial) ou
	// falha em ambos os caminhos.
	MethodNone Method = iota
	// MethodUnicode: caminho 1, SendInput com KEYEVENTF_UNICODE.
	MethodUnicode
	// MethodClipboard: caminho 2, clipboard + Ctrl+V.
	MethodClipboard
)

// String nomeia o Method — só para diagnóstico/log (NFR-010: nunca inclui
// o texto em si, só a forma como foi inserido).
func (m Method) String() string {
	switch m {
	case MethodNone:
		return "none"
	case MethodUnicode:
		return "unicode"
	case MethodClipboard:
		return "clipboard"
	default:
		return "unknown"
	}
}

// Result é o desfecho de Insert.
//
// Partial marca o caso em que o caminho unicode injetou PARTE do texto
// antes de falhar: o fallback é deliberadamente pulado, porque inserir
// tudo de novo duplicaria. Ver Insert.
type Result struct {
	// Success indica se o texto chegou ao controle com foco por algum dos
	// dois caminhos (ou se não havia nada a inserir).
	Success bool
	// Method identifica qual caminho teve sucesso. MethodNone quando
	// Success é trivial (texto vazio) ou quando ambos falharam.
	Method Method
	// ClipboardRetained é true quando a inserção falhou DEPOIS que o
	// texto já havia sido escrito no clipboard (o Ctrl+V que deveria
	// colá-lo falhou) — e, de propósito, o clipboard NÃO foi restaurado
	// ao conteúdo anterior (ADR-011, decisão 3: "não perder o texto";
	// 04-system-design.md, "Inserção", item 3: "oferecer copiar"). O
	// texto ditado fica disponível para o usuário colar manualmente em
	// outro lugar. Só é significativo quando Success é false.
	ClipboardRetained bool
	// Partial é true quando o caminho unicode injetou PARTE do texto
	// antes de falhar, e por isso o fallback de clipboard foi pulado de
	// propósito. Só é significativo quando Success é false.
	//
	// A distinção importa para a mensagem na tela: com Partial, o texto
	// NÃO está na área de transferência, e mandar a pessoa colar seria
	// mandá-la colar o que ela tinha copiado antes.
	Partial bool
}

// Config ajusta a estratégia de escolha de caminho.
type Config struct {
	// UnicodeThreshold é o número de UTF-16 code units (não runes — ver
	// doc.go) a partir do qual (exclusive) o caminho 2 é usado
	// diretamente, sem sequer tentar o caminho 1. Zero ou negativo usa
	// DefaultUnicodeThreshold.
	UnicodeThreshold int
}

// DefaultConfig devolve a configuração usada em produção quando nenhuma é
// passada explicitamente.
func DefaultConfig() Config {
	return Config{UnicodeThreshold: DefaultUnicodeThreshold}
}

func (c Config) withDefaults() Config {
	if c.UnicodeThreshold <= 0 {
		c.UnicodeThreshold = DefaultUnicodeThreshold
	}
	return c
}

// unicodeInjector e clipboardInserter são as duas portas que a estratégia
// consome — ver backend.go para as interfaces completas.

// Inserter aplica a estratégia em camadas do ADR-011, decisão 3, contra
// um unicodeInjector e um clipboardInserter injetados. Em produção, New
// monta os dois contra o Win32 real (backend_windows.go); em teste,
// strategy_test.go injeta dublês diretamente nos campos (mesmo pacote,
// teste white-box — convenção já usada por internal/agent/clipboard).
type Inserter struct {
	cfg     Config
	unicode unicodeInjector
	clip    clipboardInserter
	log     *slog.Logger
}

// New cria um Inserter pronto para produção: caminho 1 via Win32
// SendInput, caminho 2 via internal/agent/clipboard — ambos atrás de
// sysinput.Guard() (ver backend_windows.go). Fora do Windows, os dois
// caminhos sempre falham com um erro claro (backend_other.go), mas o
// pacote continua compilável e com a estratégia testável (ver doc.go).
func New(logger *slog.Logger) *Inserter {
	return NewWithConfig(DefaultConfig(), logger)
}

// NewWithConfig é New, mas com Config explícita — para quando o limiar
// medido (ver doc.go, "[ASSUMPTION]") estiver disponível.
func NewWithConfig(cfg Config, logger *slog.Logger) *Inserter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Inserter{
		cfg:     cfg.withDefaults(),
		unicode: newUnicodeBackend(),
		clip:    newClipboardBackend(logger),
		log:     logger,
	}
}

// temQuebraDeLinha diz se text tem alguma quebra de linha: LF ("\n") ou
// CR ("\r").
//
// É o que decide o caminho (ADR-016, decisão 11; ADR-011, decisão 3,
// emenda): texto com quebra vai SEMPRE pelo clipboard. Pelo SendInput, um
// "\n" vira o Enter que o aplicativo entende como "enviar" — num chat
// (WhatsApp Web), a mensagem sairia pela metade, antes de o texto acabar.
// Com o Ctrl+V, o aplicativo recebe o texto de uma vez e trata a quebra
// como quebra.
func temQuebraDeLinha(text string) bool {
	return strings.ContainsAny(text, "\r\n")
}

// normalizarQuebras devolve text com toda quebra de linha escrita como
// CRLF ("\r\n"), a do Windows: LF solto e CR solto viram CRLF, e o CRLF
// que já estava fica como está (nunca vira "\r\r\n" nem duas quebras). É
// só a quebra: nenhum outro caractere muda, e o texto que vai ao histórico
// continua com o "\n" do Polish — só o que vai ao clipboard é normalizado.
//
// Idempotente: normalizar de novo não muda nada.
func normalizarQuebras(text string) string {
	if !temQuebraDeLinha(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + strings.Count(text, "\n") + 1)
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			// CRLF é UMA quebra: este CR escreve as duas metades, e o LF
			// que vem junto é consumido aqui.
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			b.WriteString("\r\n")
		case '\n':
			b.WriteString("\r\n")
		default:
			b.WriteByte(text[i])
		}
	}
	return b.String()
}

// Insert tenta inserir text no controle com foco, na ordem do ADR-011,
// decisão 3.
//
//   - text vazio: sucesso trivial, nenhum caminho é tocado (nada a
//     inserir, nada a arriscar).
//   - text com quebra de linha, de qualquer tamanho: vai DIRETO ao
//     caminho 2, com as quebras normalizadas para CRLF (ADR-016, decisão
//     11). O caminho 1 nunca é tentado — nem como primeira escolha, nem
//     como queda: o "\n" injetado pelo SendInput pode virar Enter e
//     enviar a mensagem num chat antes da hora, e esse é justamente o
//     risco. Se o clipboard falhar, vale a regra de falha dele (último
//     item), e o texto NÃO é digitado caractere a caractere.
//   - text sem quebra, com até cfg.UnicodeThreshold UTF-16 code units:
//     tenta o caminho 1 primeiro; se falhar, cai para o caminho 2.
//   - text sem quebra, mais longo que isso: pula direto para o caminho 2
//     (ADR-011, decisão 3: "clipboard... para texto longo" — não é
//     fallback aqui, é a escolha primária).
//   - se o caminho 2 também falhar: devolve Success=false sem nunca
//     silenciosamente descartar o texto — ver Result.ClipboardRetained.
func (ins *Inserter) Insert(text string) Result {
	multilinha := temQuebraDeLinha(text)
	if multilinha {
		text = normalizarQuebras(text)
	}
	units := utf16.Encode([]rune(text))
	if len(units) == 0 {
		return Result{Success: true, Method: MethodNone}
	}

	if !multilinha && len(units) <= ins.cfg.UnicodeThreshold {
		injetados, err := ins.unicode.InjectUnicode(units)
		if err == nil {
			// Registrar o SUCESSO, e não só a falha, é o que permite
			// responder "a inserção rodou quantas vezes?".
			//
			// Em 28-set-2026 esse dado faltou: com o campo de texto não
			// esvaziado entre dois testes, o resultado pareceu
			// duplicação e a investigação foi por três caminhos errados
			// antes da aritmética mostrar que o Agent tinha inserido UMA
			// vez. Uma linha aqui teria encerrado no primeiro passo.
			//
			// Só a contagem, nunca o texto (AC-027).
			ins.log.Info("inserter: texto inserido",
				"metodo", MethodUnicode.String(), "utf16_units", len(units))
			return Result{Success: true, Method: MethodUnicode}
		}
		if injetados > 0 {
			// PARTE do texto já está na tela. O clipboard inseriria o
			// texto INTEIRO por cima, e o resultado seria duplicação —
			// pior que a inserção parcial, porque o texto certo fica
			// enterrado no meio do errado.
			//
			// Mauro viu isso em 27-set-2026: "O vencimento é dia 12. O
			// vencimento é dia 12." Basta faltar o último key-up (que
			// não produz caractere) para o unicode reportar falha com
			// todo o texto já digitado.
			ins.log.Warn("inserter: insercao unicode PARCIAL — nao vou tentar o clipboard para nao duplicar",
				"err", err, "eventos_injetados", injetados, "eventos_totais", len(units)*2)
			return Result{Success: false, Method: MethodUnicode, Partial: true}
		}
		ins.log.Warn("inserter: caminho unicode falhou sem injetar nada, tentando clipboard",
			"err", err, "utf16_units", len(units))
	}

	retained, err := ins.clip.InsertViaClipboard(text)
	if err == nil {
		// multilinha é só um sim/não: nunca o texto (AC-027).
		ins.log.Info("inserter: texto inserido",
			"metodo", MethodClipboard.String(), "utf16_units", len(units), "multilinha", multilinha)
		return Result{Success: true, Method: MethodClipboard}
	}
	// Com quebra de linha, este é o fim da linha: não há queda para o
	// unicode (ver Insert). O texto fica no clipboard quando retained.
	ins.log.Warn("inserter: caminho clipboard falhou",
		"err", err, "utf16_units", len(units), "multilinha", multilinha, "clipboard_retained", retained)
	return Result{Success: false, Method: MethodNone, ClipboardRetained: retained}
}
