package nuvem

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Os limiares do filtro de silêncio: um segmento sai quando o modelo diz que
// provavelmente não é fala (no_speech_prob acima de LimiteSemFala) E que o
// que ele escreveu é pouco confiável (avg_logprob abaixo de LimiteLogprob).
// São os limiares padrão do próprio Whisper (ADR-021, "Detecção de fala" e
// escolha menor 7), a calibrar com uso real. As duas condições juntas: um
// segmento de fala baixa e hesitante tem no_speech_prob alto OU logprob baixo,
// raramente os dois.
const (
	LimiteSemFala  = 0.6
	LimiteLogprob  = -1.0
	limiteResposta = 2 << 20 // o teto do corpo lido do provedor
)

// respostaTranscricao é o que as duas APIs devolvem. Os campos de segmento são
// ponteiros: o segmento que não os traz (os modelos gpt-*-transcribe nem
// devolvem segmentos) nunca é descartado por um zero que ninguém mediu.
type respostaTranscricao struct {
	Text     *string    `json:"text"`
	Segments []segmento `json:"segments"`
}

type segmento struct {
	Start        float64  `json:"start"`
	End          float64  `json:"end"`
	Text         string   `json:"text"`
	AvgLogprob   *float64 `json:"avg_logprob"`
	NoSpeechProb *float64 `json:"no_speech_prob"`
}

// ehSilencio diz se o segmento é um trecho que o próprio modelo marca como
// sem fala. Os limiares são estritos: exatamente 0,6 e exatamente -1,0
// ficam.
func (s segmento) ehSilencio() bool {
	return s.NoSpeechProb != nil && s.AvgLogprob != nil &&
		*s.NoSpeechProb > LimiteSemFala && *s.AvgLogprob < LimiteLogprob
}

// textoTranscrito é o resultado de ler o corpo de uma resposta.
type textoTranscrito struct {
	texto string
	spans []asr.Span
	// descartados é quantos segmentos o filtro de silêncio tirou.
	descartados int
}

// lerTranscricao lê o corpo de um 200. Um corpo que não é JSON, ou que não
// traz texto nem segmentos, é uma resposta inesperada. O texto vazio NÃO é
// erro aqui: quem chama o transforma em ErrSemFala.
//
// Se nenhum segmento saiu, o texto é o do provedor, byte a byte (só sem os
// espaços das bordas). Se algum saiu, o texto é refeito dos segmentos que
// ficaram: o texto do topo ainda traz o que se descartou.
func lerTranscricao(corpo []byte) (textoTranscrito, bool) {
	var r respostaTranscricao
	if err := json.Unmarshal(corpo, &r); err != nil {
		return textoTranscrito{}, false
	}
	if r.Text == nil && len(r.Segments) == 0 {
		return textoTranscrito{}, false
	}

	var (
		mantidos    []segmento
		descartados int
	)
	for _, s := range r.Segments {
		if s.ehSilencio() {
			descartados++
			continue
		}
		mantidos = append(mantidos, s)
	}

	var spans []asr.Span
	for _, s := range mantidos {
		if t := strings.TrimSpace(s.Text); t != "" {
			spans = append(spans, asr.Span{Text: t, Start: segundos(s.Start), End: segundos(s.End)})
		}
	}

	texto := ""
	if r.Text != nil {
		texto = strings.TrimSpace(*r.Text)
	}
	if descartados > 0 || r.Text == nil {
		partes := make([]string, 0, len(spans))
		for _, sp := range spans {
			partes = append(partes, sp.Text)
		}
		texto = strings.Join(partes, " ")
	}
	return textoTranscrito{texto: texto, spans: spans, descartados: descartados}, true
}

func segundos(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
