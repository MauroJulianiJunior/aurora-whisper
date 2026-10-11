package clipboard

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// fakeBackend é o dublê de backend usado por todo este arquivo. Nunca
// toca o clipboard real — é o que permite testar Preserver.preserve/
// restore fora do portão sysinput.Guard (ver o comentário de preserve em
// preserver.go).
type fakeBackend struct {
	openErr    error
	openCalls  int
	closeCalls int

	emptyErr   error
	emptyCalls int

	text    string
	hadText bool
	readErr error

	setErr      error
	setCalls    int
	lastSetText string

	seq    uint32
	seqErr error
}

func (f *fakeBackend) open() error {
	f.openCalls++
	return f.openErr
}

func (f *fakeBackend) close() {
	f.closeCalls++
}

func (f *fakeBackend) empty() error {
	f.emptyCalls++
	return f.emptyErr
}

func (f *fakeBackend) unicodeText() (string, bool, error) {
	return f.text, f.hadText, f.readErr
}

func (f *fakeBackend) setUnicodeText(text string) error {
	f.setCalls++
	f.lastSetText = text
	if f.setErr != nil {
		return f.setErr
	}
	// O dublê precisa ser fiel neste ponto: num clipboard de verdade, ler
	// depois de escrever devolve o que foi escrito. Enquanto a decisão de
	// restaurar olhava só o número de sequência isso não fazia diferença;
	// com a comparação por conteúdo, um fake que "esquece" o que escreveu
	// faria o round trip parecer uma mudança externa.
	f.text = text
	f.hadText = true
	return nil
}

func (f *fakeBackend) sequenceNumber() (uint32, error) {
	return f.seq, f.seqErr
}

func newTestPreserver(t *testing.T, fb *fakeBackend) *Preserver {
	t.Helper()
	sleeper := &recordingSleeper{}
	return New(
		withBackend(fb),
		withSleep(sleeper.sleep),
		WithConfig(Config{Retry: DefaultRetryPolicy(), RestoreDelay: 0}),
		WithLogger(slog.New(slog.NewTextHandler(nopWriter{}, nil))),
	)
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// --- Guard: os pontos de entrada exportados recusam sob go test ---

func TestPreserve_RecusaSobGoTest(t *testing.T) {
	p := New()
	snap, err := p.Preserve("qualquer coisa")
	if !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("Preserve() erro = %v, want errors.Is ErrDisabledInTest", err)
	}
	if snap != nil {
		t.Fatalf("Preserve() snapshot = %v, want nil quando Guard recusa", snap)
	}
}

func TestRestore_RecusaSobGoTest(t *testing.T) {
	p := New()
	err := p.Restore(&Snapshot{valid: true})
	if !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("Restore() erro = %v, want errors.Is ErrDisabledInTest", err)
	}
}

// --- preserve(): lógica interna contra o dublê ---

func TestPreserver_preserve_CapturaAnteriorEEscreveNovo(t *testing.T) {
	fb := &fakeBackend{hadText: true, text: "conteúdo antigo", seq: 7}
	p := newTestPreserver(t, fb)

	snap, err := p.preserve("conteúdo novo")
	if err != nil {
		t.Fatalf("preserve() erro = %v, want nil", err)
	}
	if fb.openCalls != 1 || fb.closeCalls != 1 {
		t.Fatalf("open/close = %d/%d, want 1/1 (par completo)", fb.openCalls, fb.closeCalls)
	}
	if fb.setCalls != 1 || fb.lastSetText != "conteúdo novo" {
		t.Fatalf("setUnicodeText chamado %d vez(es) com %q, want 1 vez com %q", fb.setCalls, fb.lastSetText, "conteúdo novo")
	}
	if !snap.hadText || snap.text != "conteúdo antigo" {
		t.Fatalf("snapshot = {hadText:%v text:%q}, want {true, %q}", snap.hadText, snap.text, "conteúdo antigo")
	}
	if snap.ourText != "conteúdo novo" || !snap.valid {
		t.Fatalf("snapshot = {ourText:%q valid:%v}, want {%q, true}", snap.ourText, snap.valid, "conteúdo novo")
	}
}

func TestPreserver_preserve_ClipboardSemTextoPrevio(t *testing.T) {
	fb := &fakeBackend{hadText: false}
	p := newTestPreserver(t, fb)

	snap, err := p.preserve("novo")
	if err != nil {
		t.Fatalf("preserve() erro = %v, want nil", err)
	}
	if snap.hadText {
		t.Fatal("snap.hadText = true, want false (clipboard original não tinha CF_UNICODETEXT)")
	}
	if !snap.valid {
		t.Fatal("snap.valid = false, want true — preserve concluiu mesmo sem texto prévio")
	}
}

func TestPreserver_preserve_OpenFalhaSempre_NaoAbreNemEscreve(t *testing.T) {
	fb := &fakeBackend{openErr: errors.New("ocupado por outro processo")}
	p := newTestPreserver(t, fb)

	snap, err := p.preserve("novo")
	if err == nil {
		t.Fatal("preserve() erro = nil, want erro (open sempre falhou)")
	}
	if snap != nil {
		t.Fatalf("preserve() snapshot = %v, want nil", snap)
	}
	if fb.closeCalls != 0 {
		t.Fatalf("close chamado %d vez(es), want 0 — open nunca teve sucesso", fb.closeCalls)
	}
	if fb.setCalls != 0 {
		t.Fatalf("setUnicodeText chamado %d vez(es), want 0", fb.setCalls)
	}
}

func TestPreserver_preserve_LeituraFalha_NuncaEscrevePorCima(t *testing.T) {
	fb := &fakeBackend{readErr: errors.New("GlobalLock falhou")}
	p := newTestPreserver(t, fb)

	_, err := p.preserve("novo")
	if err == nil {
		t.Fatal("preserve() erro = nil, want erro")
	}
	if fb.setCalls != 0 {
		t.Fatalf("setUnicodeText chamado %d vez(es), want 0 — sem snapshot confiável, nunca escreve por cima", fb.setCalls)
	}
	if fb.closeCalls != 1 {
		t.Fatalf("close chamado %d vez(es), want 1 — open teve sucesso, então fecha mesmo no erro", fb.closeCalls)
	}
}

func TestPreserver_preserve_EscritaFalha_FechaMesmoAssim(t *testing.T) {
	fb := &fakeBackend{hadText: true, text: "x", setErr: errors.New("SetClipboardData falhou")}
	p := newTestPreserver(t, fb)

	_, err := p.preserve("novo")
	if err == nil {
		t.Fatal("preserve() erro = nil, want erro")
	}
	if fb.closeCalls != 1 {
		t.Fatalf("close chamado %d vez(es), want 1", fb.closeCalls)
	}
}

// --- restore(): lógica interna contra o dublê ---

func TestPreserver_restore_SnapshotNilOuInvalido(t *testing.T) {
	fb := &fakeBackend{}
	p := newTestPreserver(t, fb)

	if err := p.restore(nil); !errors.Is(err, ErrNothingToRestore) {
		t.Fatalf("restore(nil) = %v, want ErrNothingToRestore", err)
	}
	if err := p.restore(&Snapshot{valid: false}); !errors.Is(err, ErrNothingToRestore) {
		t.Fatalf("restore(inválido) = %v, want ErrNothingToRestore", err)
	}
	if fb.openCalls != 0 {
		t.Fatalf("open chamado %d vez(es), want 0 — nada para restaurar, nem tenta abrir", fb.openCalls)
	}
}

func TestPreserver_restore_EsperaODelayConfiguradoAntesDeAbrir(t *testing.T) {
	fb := &fakeBackend{text: "texto ditado", hadText: true}
	sleeper := &recordingSleeper{}
	p := New(
		withBackend(fb),
		withSleep(sleeper.sleep),
		WithConfig(Config{Retry: DefaultRetryPolicy(), RestoreDelay: 50 * time.Millisecond}),
		WithLogger(slog.New(slog.NewTextHandler(nopWriter{}, nil))),
	)

	snap := &Snapshot{hadText: true, text: "old", ourText: "texto ditado", valid: true}
	if err := p.restore(snap); err != nil {
		t.Fatalf("restore() erro = %v, want nil", err)
	}
	if len(sleeper.calls) != 1 || sleeper.calls[0] != 50*time.Millisecond {
		t.Fatalf("sleep chamado com %v, want uma chamada de 50ms (RestoreDelay)", sleeper.calls)
	}
}

// TestPreserver_restore_PuloQuandoConteudoMudou: a pessoa copiou algo novo
// entre a nossa escrita e o restore. Devolver o original por cima
// destruiria a cópia mais recente dela.
func TestPreserver_restore_PuloQuandoConteudoMudou(t *testing.T) {
	fb := &fakeBackend{text: "algo que a pessoa copiou depois", hadText: true}
	p := newTestPreserver(t, fb)

	snap := &Snapshot{hadText: true, text: "old", ourText: "texto ditado", valid: true}
	err := p.restore(snap)
	if !errors.Is(err, ErrRestoreSkippedExternalChange) {
		t.Fatalf("restore() = %v, want ErrRestoreSkippedExternalChange", err)
	}
	if fb.setCalls != 0 || fb.emptyCalls != 0 {
		t.Fatalf("setCalls=%d emptyCalls=%d, want 0/0 — não pode sobrescrever mudança externa mais nova", fb.setCalls, fb.emptyCalls)
	}
}

func TestPreserver_restore_ReescreveTextoAnterior(t *testing.T) {
	fb := &fakeBackend{text: "texto ditado", hadText: true}
	p := newTestPreserver(t, fb)

	snap := &Snapshot{hadText: true, text: "conteúdo antigo", ourText: "texto ditado", valid: true}
	if err := p.restore(snap); err != nil {
		t.Fatalf("restore() erro = %v, want nil", err)
	}
	if fb.setCalls != 1 || fb.lastSetText != "conteúdo antigo" {
		t.Fatalf("setUnicodeText chamado %d vez(es) com %q, want 1 vez com %q", fb.setCalls, fb.lastSetText, "conteúdo antigo")
	}
	if fb.emptyCalls != 0 {
		t.Fatalf("empty chamado %d vez(es), want 0", fb.emptyCalls)
	}
}

func TestPreserver_restore_OriginalSemTexto_Esvazia(t *testing.T) {
	fb := &fakeBackend{text: "texto ditado", hadText: true}
	p := newTestPreserver(t, fb)

	snap := &Snapshot{hadText: false, ourText: "texto ditado", valid: true}
	if err := p.restore(snap); err != nil {
		t.Fatalf("restore() erro = %v, want nil", err)
	}
	if fb.emptyCalls != 1 {
		t.Fatalf("empty chamado %d vez(es), want 1", fb.emptyCalls)
	}
	if fb.setCalls != 0 {
		t.Fatalf("setUnicodeText chamado %d vez(es), want 0 — não havia texto anterior para reescrever", fb.setCalls)
	}
}

// TestPreserver_restore_ErroDeLeitura_AindaAssimTentaRestaurar: sem
// conseguir comparar, preferimos devolver o original. Perder o que a
// pessoa tinha é certeza; sobrescrever mudança externa é só risco.
func TestPreserver_restore_ErroDeLeitura_AindaAssimTentaRestaurar(t *testing.T) {
	fb := &fakeBackend{readErr: errors.New("clipboard ilegível")}
	p := newTestPreserver(t, fb)

	snap := &Snapshot{hadText: true, text: "old", ourText: "texto ditado", valid: true}
	if err := p.restore(snap); err != nil {
		t.Fatalf("restore() erro = %v, want nil — sem leitura confiável, ainda tenta restaurar", err)
	}
	if fb.setCalls != 1 {
		t.Fatalf("setUnicodeText chamado %d vez(es), want 1", fb.setCalls)
	}
}

func TestPreserver_restore_OpenFalhaSempre(t *testing.T) {
	fb := &fakeBackend{openErr: errors.New("ocupado")}
	p := newTestPreserver(t, fb)

	snap := &Snapshot{hadText: true, text: "old", valid: true}
	if err := p.restore(snap); err == nil {
		t.Fatal("restore() erro = nil, want erro (open sempre falhou)")
	}
	if fb.setCalls != 0 || fb.closeCalls != 0 {
		t.Fatalf("setCalls=%d closeCalls=%d, want 0/0", fb.setCalls, fb.closeCalls)
	}
}

// --- Privacidade: conteúdo do clipboard nunca aparece em log ---

func TestPreserveRestore_NuncaLogaConteudoDoClipboard(t *testing.T) {
	const segredoNovo = "SEGREDO-DITADO-XPTO-777"
	const segredoAntigo = "SEGREDO-COPIADO-ANTES-999"

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	fb := &fakeBackend{hadText: true, text: segredoAntigo, seq: 3}
	sleeper := &recordingSleeper{}
	p := New(
		withBackend(fb),
		withSleep(sleeper.sleep),
		WithConfig(Config{Retry: DefaultRetryPolicy(), RestoreDelay: 0}),
		WithLogger(logger),
	)

	snap, err := p.preserve(segredoNovo)
	if err != nil {
		t.Fatalf("preserve() erro = %v, want nil", err)
	}
	if err := p.restore(snap); err != nil {
		t.Fatalf("restore() erro = %v, want nil", err)
	}

	out := buf.String()
	if strings.Contains(out, segredoNovo) {
		t.Fatalf("log contém o texto novo do clipboard — violação de privacidade. log:\n%s", out)
	}
	if strings.Contains(out, segredoAntigo) {
		t.Fatalf("log contém o texto anterior do clipboard — violação de privacidade. log:\n%s", out)
	}
	if out == "" {
		t.Fatal("log vazio — o teste não estaria provando nada; preserve/restore deveriam ter logado algo (metadados)")
	}
}

// TestPreserveRestore_NuncaLogaConteudoMesmoEmErro cobre os caminhos de
// erro (leitura e escrita falhando), que também manipulam texto real.
func TestPreserveRestore_NuncaLogaConteudoMesmoEmErro(t *testing.T) {
	const segredo = "SEGREDO-EM-CAMINHO-DE-ERRO-123"

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	fb := &fakeBackend{hadText: true, text: segredo, setErr: errors.New("falhou")}
	p := New(
		withBackend(fb),
		withSleep((&recordingSleeper{}).sleep),
		WithConfig(Config{Retry: DefaultRetryPolicy(), RestoreDelay: 0}),
		WithLogger(logger),
	)

	if _, err := p.preserve(segredo); err == nil {
		t.Fatal("preserve() erro = nil, want erro (setErr configurado)")
	}

	if strings.Contains(buf.String(), segredo) {
		t.Fatalf("log contém conteúdo do clipboard mesmo em caminho de erro. log:\n%s", buf.String())
	}
}
