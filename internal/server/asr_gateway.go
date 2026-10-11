package server

import (
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Este arquivo implementa o gateway de ASR cloud: POST /asr/transcribe
// (docs/ai-spec/06-api-contract.md, "ASR cloud"; AC-041; REQ-043; THR-006).
//
// Garantias estruturais (não só disciplina):
//   - O áudio nunca toca disco: chega via multipart em memória
//     (http.Request.FormFile devolve um multipart.File que, abaixo do
//     limite de MaxMemoryBytes do parser, é sempre um *bytes.Reader em
//     RAM — nunca um arquivo temporário), é lido inteiro para um
//     []byte com io.ReadAll e convertido para asr.AudioBuffer.PCM, e
//     nenhuma linha deste arquivo chama os.Create/os.WriteFile/os.Temp*.
//   - Nada do conteúdo do áudio (nem de nenhum outro campo do corpo) é
//     passado a nenhuma função de log: este pacote não loga por padrão,
//     e nenhum handler daqui recebe um *log.Logger.
//   - maxAudioBytes limita o tamanho aceito (AC-041: "limite de
//     tamanho/duração"); requisições maiores são rejeitadas antes de
//     serem lidas inteiras, via http.MaxBytesReader.
const (
	// maxAudioBytes — [ASSUMPTION]: nenhum documento fixa um número
	// exato para o limite de tamanho do gateway ASR. 20 MiB cobre
	// folgadamente um ditado de voz de alguns minutos em PCM16 mono
	// 16kHz (~1.9 MB/min) sem abrir a porta para upload de arquivos
	// grandes arbitrários.
	maxAudioBytes = 20 << 20

	// maxAudioFormMemory é o teto que multipart.Reader mantém em RAM
	// antes de fazer spill para disco. Como maxAudioBytes já é o teto
	// duro da requisição inteira, usamos o mesmo valor aqui para
	// garantir que o parser NUNCA crie um arquivo temporário para uma
	// requisição válida — spill só aconteceria acima do próprio limite
	// de tamanho, que já é rejeitado antes.
	maxAudioFormMemory = maxAudioBytes
)

// asrTranscribeResponse é o corpo de sucesso de POST /asr/transcribe,
// conforme docs/ai-spec/06-api-contract.md.
type asrTranscribeResponse struct {
	Text         string          `json:"text"`
	Language     string          `json:"language,omitempty"`
	Confidence   *float64        `json:"confidence,omitempty"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model,omitempty"`
	ProcessingMs int64           `json:"processing_ms"`
	Segments     []asrSegmentDTO `json:"segments"`
}

type asrSegmentDTO struct {
	Text    string `json:"text"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
}

// handleASRTranscribe implementa o gateway. provider é o ASRProvider
// server-side configurado (AC-041: "provider server-side" — o cliente
// nunca escolhe nem vê a credencial do provider real, THR-006).
func handleASRTranscribe(provider asr.ASRProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAudioBytes)

		if err := r.ParseMultipartForm(maxAudioFormMemory); err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "corpo multipart inválido ou áudio grande demais")
			return
		}
		// ParseMultipartForm pode ter criado arquivos temporários de
		// spill SE o corpo excedesse maxAudioFormMemory — o que não
		// acontece aqui porque MaxBytesReader já limita o corpo inteiro
		// a maxAudioBytes == maxAudioFormMemory antes do parser rodar.
		// RemoveAll é defensivo (idempotente e barato se não houver
		// nada), nunca a única linha de defesa.
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}

		file, _, err := r.FormFile("audio")
		if err != nil {
			writeError(w, http.StatusBadRequest, codeValidation, `campo multipart "audio" é obrigatório`)
			return
		}
		defer file.Close()

		raw, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "falha ao ler áudio")
			return
		}
		if len(raw)%2 != 0 {
			writeError(w, http.StatusBadRequest, codeValidation, "áudio deve ser PCM16 mono (número par de bytes)")
			return
		}

		sampleRateHz := 16000
		if v := r.FormValue("sample_rate_hz"); v != "" {
			n, convErr := parsePositiveInt(v)
			if convErr != nil {
				writeError(w, http.StatusBadRequest, codeValidation, "sample_rate_hz inválido")
				return
			}
			sampleRateHz = n
		}

		pcm := make([]int16, len(raw)/2)
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(raw[i*2 : i*2+2]))
		}

		buf := asr.AudioBuffer{PCM: pcm, SampleRateHz: sampleRateHz}
		result, err := provider.Transcribe(r.Context(), buf, asr.ASROptions{Language: r.FormValue("language")})
		// raw e pcm saem de escopo aqui e são coletados pelo GC — nunca
		// gravados, nunca logados (ver comentário do arquivo).
		if err != nil {
			writeError(w, http.StatusBadGateway, codeInternal, "falha ao transcrever")
			return
		}

		resp := asrTranscribeResponse{
			Text: result.Text, Language: result.Language, Provider: provider.ID(),
			Model: result.Model, ProcessingMs: result.ProcessingTime.Milliseconds(),
			Segments: make([]asrSegmentDTO, 0, len(result.Spans)),
		}
		if result.Confidence.Present {
			v := result.Confidence.Value
			resp.Confidence = &v
		}
		for _, span := range result.Spans {
			resp.Segments = append(resp.Segments, asrSegmentDTO{
				Text: span.Text, StartMs: span.Start.Milliseconds(), EndMs: span.End.Milliseconds(),
			})
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

var errInvalidPositiveInt = errors.New("server: valor precisa ser um inteiro positivo")

func parsePositiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errInvalidPositiveInt
	}
	return n, nil
}
