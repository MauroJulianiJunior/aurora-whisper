package clipboard

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// Snapshot é o conteúdo do clipboard capturado por Preserve, mais o
// número de sequência observado logo depois da nossa própria escrita —
// usado por Restore para decidir se ainda é seguro restaurar (ver doc.go,
// "O problema central").
//
// Limitação documentada: só o texto Unicode (CF_UNICODETEXT) é
// preservado. Se o clipboard tinha outra coisa (imagem, arquivos, RTF,
// HTML), Restore não a traz de volta — ver doc.go, "O que é preservado".
type Snapshot struct {
	hadText bool
	text    string

	// ourText é o texto que NÓS escrevemos no clipboard. Restore compara
	// o conteúdo atual com ele para decidir se é seguro devolver o
	// original — ver shouldRestore, em policy.go, para o porquê de não
	// usar o número de sequência.
	ourText string

	// valid é false quando Preserve não chegou a concluir (ex.: nem
	// conseguiu abrir o clipboard) — não há nada de real para Restore
	// fazer com um snapshot assim.
	valid bool
}

// Config ajusta a política de retry de abertura e o atraso antes de
// restaurar.
type Config struct {
	// Retry governa as tentativas de OpenClipboard, tanto em Preserve
	// quanto em Restore.
	Retry RetryPolicy

	// RestoreDelay é quanto Restore espera — depois de reconfirmar que
	// nada mais alterou o clipboard (shouldRestore) — antes de escrever
	// o conteúdo anterior de volta.
	//
	// [ASSUMPTION] Não existe, no Win32, um sinal síncrono de "o app
	// alvo já colou" (ver doc.go do pacote — pesquisado deliberadamente,
	// não presumido). Sem esse sinal, um atraso fixo é a estratégia
	// honesta disponível. O valor abaixo (120ms) é um palpite de
	// engenharia — tempo de sobra para uma UI comum processar um
	// Ctrl+V — e NÃO foi medido em aplicativo real. Precisa ser medido
	// na matriz mínima da spec (Bloco de Notas, textarea do Chrome,
	// WhatsApp Web, VS Code, inputs web comuns) pelo roteiro de teste
	// manual, e este valor ajustado aqui (e a ADR-011, se a medição
	// mudar o desenho) conforme o resultado.
	RestoreDelay time.Duration
}

// DefaultConfig devolve a configuração usada em produção quando nenhuma
// é passada explicitamente.
func DefaultConfig() Config {
	return Config{
		Retry:        DefaultRetryPolicy(),
		RestoreDelay: 120 * time.Millisecond, // [ASSUMPTION] — ver Config.RestoreDelay
	}
}

// Preserver preserva e restaura o clipboard do sistema ao redor de uma
// escrita temporária (ADR-011 decisão 4, AC-009).
type Preserver struct {
	backend backend
	cfg     Config
	sleep   func(time.Duration)
	log     *slog.Logger
}

// Option configura um Preserver criado por New.
type Option func(*Preserver)

// WithConfig substitui a configuração default (retry e RestoreDelay).
func WithConfig(cfg Config) Option {
	return func(p *Preserver) { p.cfg = cfg }
}

// WithLogger substitui o logger default (slog.Default()). Um logger nil
// é ignorado.
func WithLogger(l *slog.Logger) Option {
	return func(p *Preserver) {
		if l != nil {
			p.log = l
		}
	}
}

// withSleep substitui a função de espera (retry e RestoreDelay). Não
// exportada de propósito: só o teste interno deste pacote precisa trocar
// isto por um dublê que não espera tempo real; nenhum chamador de
// produção deveria mexer aqui.
func withSleep(sleep func(time.Duration)) Option {
	return func(p *Preserver) { p.sleep = sleep }
}

// withBackend substitui o backend real por um dublê. Não exportada de
// propósito, pelo mesmo motivo de withSleep.
func withBackend(b backend) Option {
	return func(p *Preserver) { p.backend = b }
}

// New cria um Preserver pronto para uso em produção, contra o clipboard
// real do Windows (ou o stub que devolve erro, fora do Windows).
func New(opts ...Option) *Preserver {
	p := &Preserver{
		backend: newBackend(),
		cfg:     DefaultConfig(),
		sleep:   time.Sleep,
		log:     slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ErrNothingToRestore é devolvido por Restore quando snap é nil ou
// inválido (Preserve não chegou a concluir) — não há o que restaurar.
var ErrNothingToRestore = errors.New("clipboard: nada para restaurar (snapshot inválido ou ausente)")

// ErrRestoreSkippedExternalChange é devolvido por Restore quando o
// número de sequência mostra que algo mais alterou o clipboard depois da
// nossa escrita. Não é uma falha operacional — é a corrida reconhecida
// por ADR-011 decisão 4 do outro lado: restaurar por cima destruiria essa
// mudança mais nova, então Restore desiste de propósito. Ver
// shouldRestore.
var ErrRestoreSkippedExternalChange = errors.New("clipboard: restore pulado — algo mais alterou o clipboard nesse meio tempo")

// Preserve tira um snapshot do clipboard atual (só CF_UNICODETEXT — ver
// doc.go) e escreve text no lugar.
//
// Passa por sysinput.Guard() antes de tocar em qualquer coisa: sob
// "go test", ou antes de o binário do Agent chamar sysinput.Enable(),
// devolve o erro do Guard sem abrir o clipboard.
func (p *Preserver) Preserve(text string) (*Snapshot, error) {
	if err := sysinput.Guard(); err != nil {
		return nil, err
	}
	return p.preserve(text)
}

// preserve é a lógica de verdade, sem o portão. Existe separada de
// Preserve para que um teste consiga exercitá-la contra um backend dublê
// — que por definição nunca toca o clipboard real — sem precisar
// contornar sysinput.Guard. Em produção, o único jeito de alcançar esta
// função é passando por Preserve, que já checou Guard.
func (p *Preserver) preserve(text string) (*Snapshot, error) {
	if err := openWithRetry(p.backend.open, p.cfg.Retry, p.sleep); err != nil {
		p.log.Warn("clipboard: preserve não conseguiu abrir o clipboard", "err", err)
		return nil, err
	}
	defer p.backend.close()

	prevText, hadText, err := p.backend.unicodeText()
	if err != nil {
		// Não temos certeza do que capturamos: não seguimos para
		// escrever por cima (ver doc.go, "Armadilhas tratadas" — nunca
		// destruir conteúdo sem ter certeza do snapshot).
		p.log.Warn("clipboard: preserve falhou ao ler o conteúdo anterior", "err", err)
		return nil, fmt.Errorf("clipboard: ler conteúdo anterior: %w", err)
	}

	if err := p.backend.setUnicodeText(text); err != nil {
		p.log.Warn("clipboard: preserve falhou ao escrever o novo conteúdo", "err", err)
		return nil, fmt.Errorf("clipboard: escrever novo conteúdo: %w", err)
	}

	p.log.Info("clipboard: preserve concluído",
		"had_previous_text", hadText,
		"new_text_runes", len([]rune(text)),
	)

	return &Snapshot{
		hadText: hadText,
		text:    prevText,
		ourText: text,
		valid:   true,
	}, nil
}

// Restore devolve o clipboard ao estado capturado por snap.
//
// Passa por sysinput.Guard() antes de tocar em qualquer coisa, pelo
// mesmo motivo de Preserve.
func (p *Preserver) Restore(snap *Snapshot) error {
	if err := sysinput.Guard(); err != nil {
		return err
	}
	return p.restore(snap)
}

// restore é a lógica de verdade, sem o portão — ver o comentário de
// preserve para o motivo de existir separada.
func (p *Preserver) restore(snap *Snapshot) error {
	if snap == nil || !snap.valid {
		return ErrNothingToRestore
	}

	// Ver Config.RestoreDelay: não há sinal síncrono de "já foi lido",
	// então esperamos um intervalo configurável antes de sequer tentar.
	if p.sleep != nil && p.cfg.RestoreDelay > 0 {
		p.sleep(p.cfg.RestoreDelay)
	}

	if err := openWithRetry(p.backend.open, p.cfg.Retry, p.sleep); err != nil {
		p.log.Warn("clipboard: restore não conseguiu abrir o clipboard", "err", err)
		return err
	}
	defer p.backend.close()

	// Comparamos o CONTEÚDO, não o número de sequência: o Histórico da
	// Área de Transferência do Windows reescreve o clipboard depois de
	// cada escrita e faria a sequência mudar sempre, pulando todo restore.
	// Ver shouldRestore, em policy.go.
	//
	// Quando nem a leitura funciona, seguimos restaurando: perder o que a
	// pessoa tinha é certeza; sobrescrever mudança externa é só risco.
	currentText, _, readErr := p.backend.unicodeText()
	if !shouldRestore(currentText, snap.ourText, readErr == nil) {
		// Sem registrar o conteúdo, nem o nosso nem o dela (NFR-010).
		p.log.Info("clipboard: restore pulado — o conteúdo mudou desde a escrita")
		return ErrRestoreSkippedExternalChange
	}

	if !snap.hadText {
		// O clipboard original não tinha CF_UNICODETEXT — ou estava
		// vazio, ou tinha só um formato que este pacote não preserva
		// (ver doc.go). O mais fiel que conseguimos fazer é esvaziar,
		// não fingir que reconstituímos o que estava lá.
		if err := p.backend.empty(); err != nil {
			p.log.Warn("clipboard: restore falhou ao esvaziar (original não tinha texto)", "err", err)
			return fmt.Errorf("clipboard: esvaziar: %w", err)
		}
		p.log.Info("clipboard: restore concluído (original sem texto — esvaziado)")
		return nil
	}

	if err := p.backend.setUnicodeText(snap.text); err != nil {
		p.log.Warn("clipboard: restore falhou ao reescrever o conteúdo anterior", "err", err)
		return fmt.Errorf("clipboard: reescrever conteúdo anterior: %w", err)
	}
	p.log.Info("clipboard: restore concluído", "restored_text_runes", len([]rune(snap.text)))
	return nil
}
