package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// fakeRecorder é o dublê de recorder usado pelos testes de run(): nunca
// toca num microfone de verdade. BeginSample devolve um canal que nunca
// recebe nada — os testes abaixo não exercitam o desenho do medidor de
// nível em si (levelBar já é testado isoladamente em session_test.go),
// só o fluxo ao redor dele.
type fakeRecorder struct {
	openErr    error
	opened     bool
	closed     bool
	sampleRate int
	takes      [][]int16
	vads       []dictation.VADSummary
	takeIdx    int
	beginCount int
}

func (f *fakeRecorder) Open(ctx context.Context, maxRecording time.Duration) error {
	f.opened = true
	return f.openErr
}

func (f *fakeRecorder) BeginSample() <-chan float64 {
	f.beginCount++
	return make(chan float64)
}

func (f *fakeRecorder) EndSample() ([]int16, int, dictation.VADSummary, error) {
	if f.takeIdx >= len(f.takes) {
		return nil, 0, dictation.VADSummary{}, errors.New("fakeRecorder: EndSample chamado mais vezes que takes configuradas")
	}
	samples := f.takes[f.takeIdx]
	vad := dictation.VADSummary{HasSpeech: true}
	if f.takeIdx < len(f.vads) {
		vad = f.vads[f.takeIdx]
	}
	f.takeIdx++
	return samples, f.sampleRate, vad, nil
}

// DeviceReport do dublê devolve uma linha reconhecível, para os testes
// distinguirem "não imprimiu o diagnóstico" de "imprimiu vazio".
func (f *fakeRecorder) DeviceReport() string { return "dispositivo: dublê de teste" }

func (f *fakeRecorder) Close() error {
	f.closed = true
	return nil
}

// normalTake devolve n amostras com amplitude ~50% do fundo de escala:
// nem silêncio, nem baixo, nem clipping — nenhum aviso de
// recordingWarnings dispara, então os testes que não estão testando
// avisos ficam limpos.
func normalTake(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		if i%2 == 0 {
			s[i] = 16000
		} else {
			s[i] = -16000
		}
	}
	return s
}

func writeTestManifest(t *testing.T, dir string, ids ...string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`{"samples":[`)
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"` + id + `","audio":"audio/` + id + `.wav","reference":"frase ` + id + `","category":"ingles","entities":[]}`)
	}
	sb.WriteString(`]}`)
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("escrevendo manifesto: %v", err)
	}
	return path
}

func TestRun_MissingManifestFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &fakeRecorder{}

	code := run(nil, strings.NewReader(""), &stdout, &stderr, rec)

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "obrigatório") {
		t.Fatalf("stderr = %q, want menção a -manifest obrigatório", stderr.String())
	}
	if rec.opened {
		t.Fatal("rec.Open não deveria ter sido chamado sem manifesto válido")
	}
}

func TestRun_ManifestLoadError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &fakeRecorder{}

	code := run([]string{"-manifest", "nao-existe.json"}, strings.NewReader(""), &stdout, &stderr, rec)

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if rec.opened {
		t.Fatal("rec.Open não deveria ter sido chamado com manifesto inválido")
	}
}

func TestRun_RedoUnknownID(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	var stdout, stderr bytes.Buffer
	rec := &fakeRecorder{}

	code := run([]string{"-manifest", manifestPath, "-redo", "nao-existe"}, strings.NewReader(""), &stdout, &stderr, rec)

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if rec.opened {
		t.Fatal("rec.Open não deveria ter sido chamado com -redo de ID inexistente")
	}
}

func TestRun_NoPendingSamples(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := dataset.WriteWAV(filepath.Join(dir, "audio", "s1.wav"), 16000, normalTake(1600)); err != nil {
		t.Fatalf("setup: pré-gravando s1: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rec := &fakeRecorder{}

	code := run([]string{"-manifest", manifestPath}, strings.NewReader(""), &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "completo") {
		t.Fatalf("stdout = %q, want mensagem de dataset completo", stdout.String())
	}
	if rec.opened {
		t.Fatal("rec.Open não deveria ter sido chamado quando não há nada pendente")
	}
}

func TestRun_OpenError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	var stdout, stderr bytes.Buffer
	// Um erro genérico de propósito (em vez de errWindowsOnly, que só
	// existe em record_other.go, atrás de "!windows"): este teste roda
	// nos dois SOs e não deveria depender de qual arquivo de captura foi
	// compilado.
	rec := &fakeRecorder{openErr: errors.New("dispositivo de captura indisponível")}

	code := run([]string{"-manifest", manifestPath}, strings.NewReader(""), &stdout, &stderr, rec)

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "dispositivo de captura indisponível") {
		t.Fatalf("stderr = %q, want o erro de abrir o microfone", stderr.String())
	}
}

// TestRun_AcceptFlow_WritesWAVAndCreatesAudioDir é o teste central: uma
// amostra, uma tomada, aceita. Verifica que o diretório audio/ (que NÃO
// existe de antemão — o mesmo estado do testdata/asr-dataset/manifest.json
// versionado) é criado, e que o WAV escrito bate com a tomada devolvida
// pelo dublê.
func TestRun_AcceptFlow_WritesWAVAndCreatesAudioDir(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	audioPath := filepath.Join(dir, "audio", "s1.wav")

	take := normalTake(8000) // 0.5s a 16kHz
	rec := &fakeRecorder{sampleRate: 16000, takes: [][]int16{take}}

	// prompts, na ordem: ENTER (grava) / ENTER (para) / ENTER (aceita)
	stdin := strings.NewReader("\n\n\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !rec.opened || !rec.closed {
		t.Fatalf("rec.opened=%v rec.closed=%v, want ambos true", rec.opened, rec.closed)
	}
	if !strings.Contains(stdout.String(), "[1/1] categoria: ingles") {
		t.Fatalf("stdout não contém a linha de progresso esperada:\n%s", stdout.String())
	}

	gotDuration, err := dataset.Duration(audioPath)
	if err != nil {
		t.Fatalf("Duration do WAV escrito: %v", err)
	}
	if want := 500 * time.Millisecond; gotDuration != want {
		t.Fatalf("duração do WAV = %v, want %v", gotDuration, want)
	}
}

// TestRun_SkipFlow_DoesNotWriteFile confere que "s" no primeiro prompt
// pula a amostra sem gravar nada nem tocar o disco.
func TestRun_SkipFlow_DoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	audioPath := filepath.Join(dir, "audio", "s1.wav")

	rec := &fakeRecorder{}
	stdin := strings.NewReader("s\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(audioPath); err == nil {
		t.Fatal("skip não deveria ter criado o arquivo de áudio")
	}
	if rec.beginCount != 0 {
		t.Fatalf("BeginSample foi chamado %d vezes, want 0 (skip antes de gravar não grava nada)", rec.beginCount)
	}
}

// TestRun_RedoTakeThenAccept confere o "r": a primeira tomada é
// descartada, uma segunda é gravada, e É ELA que acaba no WAV.
func TestRun_RedoTakeThenAccept(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	audioPath := filepath.Join(dir, "audio", "s1.wav")

	take1 := normalTake(8000)  // 0.5s — vai ser descartada
	take2 := normalTake(16000) // 1.0s — vai ser a aceita
	rec := &fakeRecorder{sampleRate: 16000, takes: [][]int16{take1, take2}}

	// ENTER grava / ENTER para (tomada 1) / r regrava / ENTER grava /
	// ENTER para (tomada 2) / ENTER aceita
	stdin := strings.NewReader("\n\nr\n\n\n\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if rec.beginCount != 2 {
		t.Fatalf("BeginSample foi chamado %d vezes, want 2 (uma por tomada)", rec.beginCount)
	}

	gotDuration, err := dataset.Duration(audioPath)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if want := time.Second; gotDuration != want {
		t.Fatalf("duração do WAV = %v, want %v (a SEGUNDA tomada, não a primeira)", gotDuration, want)
	}
}

// TestRun_QuitMidSession_LeavesRestPending confere que "q" no primeiro
// prompt da segunda amostra encerra a sessão sem tocar nela, deixando a
// primeira (já aceita) e a segunda (pendente) num estado que uma próxima
// chamada a run() retomaria corretamente — a mesma garantia que
// pendingSamples já teria provado sozinha, aqui verificada ponta pra ponta
// através de run().
func TestRun_QuitMidSession_LeavesRestPending(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1", "s2")

	take := normalTake(8000)
	rec := &fakeRecorder{sampleRate: 16000, takes: [][]int16{take}}

	// s1: ENTER grava / ENTER para / ENTER aceita. s2: q sai.
	stdin := strings.NewReader("\n\n\nq\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "progresso salvo") {
		t.Fatalf("stdout = %q, want mensagem de progresso salvo", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "audio", "s1.wav")); err != nil {
		t.Fatalf("s1 deveria ter sido gravada antes do q: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "audio", "s2.wav")); err == nil {
		t.Fatal("s2 não deveria ter sido gravada — a sessão saiu antes dela")
	}

	// Uma segunda chamada a run(), com um recorder fresco, deveria
	// oferecer só s2 — é o comportamento retomável que o comando promete.
	rec2 := &fakeRecorder{sampleRate: 16000, takes: [][]int16{normalTake(8000)}}
	stdin2 := strings.NewReader("\n\n\n")
	var stdout2, stderr2 bytes.Buffer

	code2 := run([]string{"-manifest", manifestPath}, stdin2, &stdout2, &stderr2, rec2)

	if code2 != 0 {
		t.Fatalf("segunda chamada: code = %d, want 0; stderr=%s", code2, stderr2.String())
	}
	if !strings.Contains(stdout2.String(), "[2/2] categoria: ingles") {
		t.Fatalf("segunda chamada deveria retomar direto em s2 (posição 2/2):\n%s", stdout2.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "audio", "s2.wav")); err != nil {
		t.Fatalf("s2 deveria ter sido gravada na segunda chamada: %v", err)
	}
}

// TestRun_StdinEOF_TreatedAsQuit confere que fechar stdin no meio do
// primeiro prompt não trava nem entra em pânico — é tratado como "q".
func TestRun_StdinEOF_TreatedAsQuit(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")

	rec := &fakeRecorder{}
	stdin := strings.NewReader("") // EOF imediato
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0 (EOF tratado como saída limpa); stderr=%s", code, stderr.String())
	}
	if rec.beginCount != 0 {
		t.Fatal("EOF no primeiro prompt não deveria ter começado nenhuma gravação")
	}
}

// TestRun_RedoFlag_ReRecordsAcceptedSample confere -redo de ponta a
// ponta: uma amostra já aceita (arquivo no disco) é oferecida de novo
// quando -redo aponta pra ela, e o novo WAV substitui o antigo.
func TestRun_RedoFlag_ReRecordsAcceptedSample(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")
	audioPath := filepath.Join(dir, "audio", "s1.wav")

	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := dataset.WriteWAV(audioPath, 16000, normalTake(16000)); err != nil { // 1.0s antiga
		t.Fatalf("setup: gravação antiga: %v", err)
	}

	rec := &fakeRecorder{sampleRate: 16000, takes: [][]int16{normalTake(4000)}} // 0.25s nova
	stdin := strings.NewReader("\n\n\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath, "-redo", "s1"}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}

	got, err := dataset.Duration(audioPath)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if want := 250 * time.Millisecond; got != want {
		t.Fatalf("duração após -redo = %v, want %v (a gravação NOVA, não a antiga)", got, want)
	}
}

// TestRun_UnrecognizedInput_Reprompts confere que uma resposta fora do
// conjunto esperado não é interpretada como nenhuma das opções — o
// prompt é repetido até uma resposta reconhecida chegar.
func TestRun_UnrecognizedInput_Reprompts(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeTestManifest(t, dir, "s1")

	take := normalTake(8000)
	rec := &fakeRecorder{sampleRate: 16000, takes: [][]int16{take}}

	// "talvez" não é uma resposta válida no primeiro prompt; só depois
	// vem o ENTER que de fato inicia a gravação.
	stdin := strings.NewReader("talvez\n\n\n\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"-manifest", manifestPath}, stdin, &stdout, &stderr, rec)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "não reconhecida") {
		t.Fatalf("stderr = %q, want aviso de resposta não reconhecida", stderr.String())
	}
	if rec.beginCount != 1 {
		t.Fatalf("BeginSample foi chamado %d vezes, want 1 (só depois da resposta reconhecida)", rec.beginCount)
	}
}
