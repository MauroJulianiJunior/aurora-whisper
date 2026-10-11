package dataset

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDuration_OneSecondAt16kHz(t *testing.T) {
	dir := t.TempDir()
	path := writeTestWAV(t, dir, "one-second.wav", 16000, 16000)

	got, err := Duration(path)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if got != time.Second {
		t.Fatalf("Duration = %v, want 1s", got)
	}
}

func TestDuration_HalfSecondAt8kHz(t *testing.T) {
	dir := t.TempDir()
	path := writeTestWAV(t, dir, "half-second.wav", 8000, 4000)

	got, err := Duration(path)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if got != 500*time.Millisecond {
		t.Fatalf("Duration = %v, want 500ms", got)
	}
}

func TestDuration_NotAWAVFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/not-a-wav.wav"
	if err := os.WriteFile(path, []byte("not a wav file at all, just garbage bytes"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := Duration(path); err == nil {
		t.Fatal("Duration: want erro para arquivo que não é WAV, got nil")
	}
}

func TestDuration_MissingFile(t *testing.T) {
	if _, err := Duration("does-not-exist.wav"); err == nil {
		t.Fatal("Duration: want erro para arquivo ausente, got nil")
	}
}

// TestWriteWAV_RoundTripDuration_Integer prova a ida e volta com o caso
// fácil (duração exata) — WriteWAV seguido de Duration devolve o mesmo
// valor que writeTestWAV+Duration já provam para o escritor de teste,
// agora para o escritor de produção.
func TestWriteWAV_RoundTripDuration_Integer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one-second.wav")
	samples := make([]int16, 16000) // 1s a 16kHz

	if err := WriteWAV(path, 16000, samples); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	got, err := Duration(path)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if got != time.Second {
		t.Fatalf("Duration = %v, want 1s", got)
	}
}

// TestWriteWAV_RoundTripDuration_NonInteger cobre o caso que
// TestWriteWAV_RoundTripDuration_Integer não cobre: uma contagem de
// amostras que não é múltipla da taxa de amostragem, então a duração
// resultante não é um número inteiro de segundos — exatamente a forma
// real das frases do dataset (ex.: 2.3s de fala).
func TestWriteWAV_RoundTripDuration_NonInteger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-round-second.wav")
	const sampleRate = 16000
	numSamples := 36800 // 2.3s exatos a 16kHz
	samples := make([]int16, numSamples)

	if err := WriteWAV(path, sampleRate, samples); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	got, err := Duration(path)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	want := 2300 * time.Millisecond
	if got != want {
		t.Fatalf("Duration = %v, want %v", got, want)
	}
}

// TestWriteWAV_RoundTripSamples confere que o PCM em si — não só a
// duração — sobrevive intacto, incluindo os extremos de int16
// (math.MinInt16/MaxInt16, onde um escritor com sinal errado ou largura
// errada por byte costuma aparecer primeiro).
func TestWriteWAV_RoundTripSamples(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "samples.wav")
	want := []int16{0, 1, -1, math.MaxInt16, math.MinInt16, 12345, -12345}

	if err := WriteWAV(path, 16000, want); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lendo WAV escrito: %v", err)
	}
	const headerSize = 44
	body := raw[headerSize:]
	if len(body) != len(want)*2 {
		t.Fatalf("corpo do WAV tem %d bytes, want %d", len(body), len(want)*2)
	}

	got := make([]int16, len(want))
	for i := range got {
		got[i] = int16(binary.LittleEndian.Uint16(body[i*2 : i*2+2]))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("amostra[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

// TestWriteWAV_HeaderFields verifica os bytes do cabeçalho um a um, em
// vez de só confiar que Duration consegue relê-los depois — um
// cabeçalho poderia acertar byteRate (o que Duration usa) e errar
// blockAlign ou bitsPerSample (o que Duration ignora) sem que o teste de
// ida e volta acima percebesse.
func TestWriteWAV_HeaderFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "header.wav")
	samples := []int16{100, -100, 200, -200, 300}
	const sampleRate = 16000

	if err := WriteWAV(path, sampleRate, samples); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lendo WAV escrito: %v", err)
	}

	const (
		wantByteRate  = sampleRate * 2
		wantBlkAlign  = 2
		wantBitsDepth = 16
	)
	wantDataSize := len(samples) * 2

	if len(raw) != 44+wantDataSize {
		t.Fatalf("tamanho do arquivo = %d bytes, want %d", len(raw), 44+wantDataSize)
	}
	if s := string(raw[0:4]); s != "RIFF" {
		t.Fatalf("chunk RIFF = %q, want \"RIFF\"", s)
	}
	if v := binary.LittleEndian.Uint32(raw[4:8]); v != uint32(36+wantDataSize) {
		t.Fatalf("tamanho RIFF = %d, want %d", v, 36+wantDataSize)
	}
	if s := string(raw[8:12]); s != "WAVE" {
		t.Fatalf("formato = %q, want \"WAVE\"", s)
	}
	if s := string(raw[12:16]); s != "fmt " {
		t.Fatalf("chunk fmt = %q, want \"fmt \"", s)
	}
	if v := binary.LittleEndian.Uint32(raw[16:20]); v != 16 {
		t.Fatalf("tamanho do chunk fmt = %d, want 16", v)
	}
	if v := binary.LittleEndian.Uint16(raw[20:22]); v != 1 {
		t.Fatalf("audio format = %d, want 1 (PCM)", v)
	}
	if v := binary.LittleEndian.Uint16(raw[22:24]); v != 1 {
		t.Fatalf("numChannels = %d, want 1 (mono)", v)
	}
	if v := binary.LittleEndian.Uint32(raw[24:28]); v != sampleRate {
		t.Fatalf("sampleRate = %d, want %d", v, sampleRate)
	}
	if v := binary.LittleEndian.Uint32(raw[28:32]); v != wantByteRate {
		t.Fatalf("byteRate = %d, want %d", v, wantByteRate)
	}
	if v := binary.LittleEndian.Uint16(raw[32:34]); v != wantBlkAlign {
		t.Fatalf("blockAlign = %d, want %d", v, wantBlkAlign)
	}
	if v := binary.LittleEndian.Uint16(raw[34:36]); v != wantBitsDepth {
		t.Fatalf("bitsPerSample = %d, want %d", v, wantBitsDepth)
	}
	if s := string(raw[36:40]); s != "data" {
		t.Fatalf("chunk data = %q, want \"data\"", s)
	}
	if v := binary.LittleEndian.Uint32(raw[40:44]); v != uint32(wantDataSize) {
		t.Fatalf("tamanho do chunk data = %d, want %d", v, wantDataSize)
	}
}

func TestWriteWAV_InvalidSampleRate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.wav")

	if err := WriteWAV(path, 0, []int16{1, 2, 3}); err == nil {
		t.Fatal("WriteWAV: want erro para sample rate 0, got nil")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("WriteWAV: não deveria ter criado arquivo quando o sample rate é inválido")
	}
}

// TestReadWAV_RoundTrip prova que ReadWAV desfaz exatamente o que
// WriteWAV faz: mesma taxa de amostragem, mesmas amostras, incluindo os
// extremos de int16 onde um leitor com sinal ou largura errada por byte
// costuma aparecer primeiro.
func TestReadWAV_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.wav")
	const sampleRate = 16000
	want := []int16{0, 1, -1, math.MaxInt16, math.MinInt16, 12345, -12345}

	if err := WriteWAV(path, sampleRate, want); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	gotRate, gotSamples, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if gotRate != sampleRate {
		t.Fatalf("sampleRate = %d, want %d", gotRate, sampleRate)
	}
	if len(gotSamples) != len(want) {
		t.Fatalf("len(samples) = %d, want %d", len(gotSamples), len(want))
	}
	for i := range want {
		if gotSamples[i] != want[i] {
			t.Fatalf("amostra[%d] = %d, want %d", i, gotSamples[i], want[i])
		}
	}
}

// TestReadWAV_NotAWAVFile confere que ReadWAV nunca tenta decodificar
// lixo como se fossem amostras — mesmo comportamento de erro claro que
// Duration já tem para arquivo que não é WAV.
func TestReadWAV_NotAWAVFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-wav.wav")
	if err := os.WriteFile(path, []byte("nem cabecalho RIFF nem nada parecido"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, _, err := ReadWAV(path); err == nil {
		t.Fatal("ReadWAV: want erro para arquivo que não é WAV, got nil")
	}
}

// TestReadWAV_MissingFile confere o erro claro para caminho inexistente,
// em vez de um panic.
func TestReadWAV_MissingFile(t *testing.T) {
	if _, _, err := ReadWAV("does-not-exist.wav"); err == nil {
		t.Fatal("ReadWAV: want erro para arquivo ausente, got nil")
	}
}

// TestWriteWAV_OverwritesExistingFile prova o comportamento que a flag
// -redo de cmd/asrrec depende: regravar uma amostra chama WriteWAV de
// novo no MESMO path, e o conteúdo antigo não pode sobreviver misturado
// com o novo (um escritor que abrisse em modo append, em vez de
// truncar, produziria um WAV corrompido aqui).
func TestWriteWAV_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "redo.wav")

	if err := WriteWAV(path, 16000, make([]int16, 16000)); err != nil {
		t.Fatalf("primeira WriteWAV: %v", err)
	}
	if err := WriteWAV(path, 16000, make([]int16, 8000)); err != nil {
		t.Fatalf("segunda WriteWAV (regravação): %v", err)
	}

	got, err := Duration(path)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if want := 500 * time.Millisecond; got != want {
		t.Fatalf("Duration após regravar = %v, want %v (não pode sobrar conteúdo da gravação anterior)", got, want)
	}
}
