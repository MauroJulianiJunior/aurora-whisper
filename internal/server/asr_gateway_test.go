package server_test

import (
	"bytes"
	"encoding/binary"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/mock"
)

// pcm16 codifica amostras int16 como PCM16 little-endian, o formato que
// handleASRTranscribe espera no campo multipart "audio".
func pcm16(samples ...int16) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	return buf
}

// TestAC041_ASRGatewayDoesNotWriteAudioToDisk prova a garantia
// estrutural de asr_gateway.go: nenhum arquivo aparece em lugar nenhum
// do sistema de arquivos (nem no diretório de trabalho, nem no diretório
// temporário do SO) como resultado de transcrever áudio.
func TestAC041_ASRGatewayDoesNotWriteAudioToDisk(t *testing.T) {
	h := newHarness(t)

	// O diretório temporário do SO é compartilhado: com `go test ./...`,
	// os pacotes rodam em paralelo, e o t.TempDir de qualquer outro pacote
	// aparecia aqui como "arquivo novo" (visto em 29-set-2026). O teste
	// aponta o diretório temporário DESTE processo para uma pasta só dele
	// — os.TempDir lê TMP/TEMP no Windows e TMPDIR no resto, e é ali que
	// os.CreateTemp("", ...) escreveria. Sem t.Parallel neste pacote,
	// nenhum outro teste daqui roda enquanto isso.
	tmp := t.TempDir()
	for _, v := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(v, tmp)
	}
	if got := os.TempDir(); filepath.Clean(got) != filepath.Clean(tmp) {
		t.Fatalf("premissa: os.TempDir() = %q, quer %q", got, tmp)
	}

	dirsToWatch := []string{".", os.TempDir()}
	before := make(map[string]map[string]struct{}, len(dirsToWatch))
	for _, dir := range dirsToWatch {
		before[dir] = snapshotDir(t, dir)
	}

	audio := pcm16(100, -200, 300, -400, 500)
	rec := h.doMultipartAudio(h.tokenA, audio, map[string]string{"language": "pt-BR", "sample_rate_hz": "16000"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /asr/transcribe: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	for _, dir := range dirsToWatch {
		after := snapshotDir(t, dir)
		for name := range after {
			if _, existed := before[dir][name]; !existed {
				t.Fatalf("arquivo novo apareceu em %s depois de transcrever áudio: %s (AC-041: áudio nunca toca disco)", dir, name)
			}
		}
	}
}

// TestAC041_ASRGatewayNeverLogsAudioBody prova que o corpo do áudio
// nunca chega a nenhum destino de log: redireciona o logger padrão da
// stdlib para um buffer, chama a rota, e confirma que os bytes crus do
// áudio (um padrão improvável de aparecer por acaso em texto de log) não
// estão no buffer.
func TestAC041_ASRGatewayNeverLogsAudioBody(t *testing.T) {
	h := newHarness(t)

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prevOutput)

	// Um padrão de amostras suficientemente distintivo para não
	// aparecer por acidente em nenhuma mensagem de log estrutural.
	audio := pcm16(12345, -6789, 4321, -8765, 999, -111, 222, -333)
	rec := h.doMultipartAudio(h.tokenA, audio, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /asr/transcribe: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	if logBuf.Len() != 0 {
		t.Fatalf("gateway de ASR escreveu no logger padrão (deveria não logar nada): %q", logBuf.String())
	}
}

// TestASRGateway_ReturnsProviderResult confirma o mapeamento básico do
// contrato (docs/ai-spec/06-api-contract.md, "ASR cloud"): o resultado
// do provider chega ao cliente, sem áudio no caminho de volta.
func TestASRGateway_ReturnsProviderResult(t *testing.T) {
	h := newHarnessWithASR(t, mock.NewConfident("modulo fisco", 0.87))
	rec := h.doMultipartAudio(h.tokenA, pcm16(1, 2, 3, 4), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	body := decodeBody[struct {
		Text       string  `json:"text"`
		Confidence float64 `json:"confidence"`
		Provider   string  `json:"provider"`
	}](t, rec)
	if body.Text != "modulo fisco" || body.Provider != "mock-confident" {
		t.Fatalf("corpo = %+v", body)
	}
}

// snapshotDir lista os nomes de arquivo (não recursivo) de dir. Falhas
// ao ler o diretório fazem o teste falhar — não silenciam um diretório
// inacessível como "sem mudança".
func snapshotDir(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	out := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		out[filepath.Join(dir, e.Name())] = struct{}{}
	}
	return out
}
