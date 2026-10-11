package local

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// encodeWAV monta um WAV PCM 16 mono EM MEMÓRIA a partir das amostras.
//
// Em memória, e não num arquivo temporário, porque o AC-028 proíbe áudio
// em disco. O whisper-server aceita o WAV pelo corpo da requisição, o que
// foi verificado antes de este caminho virar dependência — a alternativa
// (gravar um .wav e passar o caminho) violaria o invariante e só seria
// descoberta tarde.
//
// Cabeçalho RIFF/WAVE canônico de 44 bytes, o mesmo formato que
// internal/benchmark/dataset.WriteWAV escreve e que dataset.Duration lê.
func encodeWAV(pcm []int16, sampleRateHz int) ([]byte, error) {
	if sampleRateHz <= 0 {
		return nil, fmt.Errorf("asr/local: sample rate inválido: %d", sampleRateHz)
	}
	const (
		canais = 1
		bits   = 16
	)
	blockAlign := canais * bits / 8
	dataSize := len(pcm) * blockAlign

	b := make([]byte, 44+dataSize)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+dataSize))
	copy(b[8:], "WAVE")
	copy(b[12:], "fmt ")
	binary.LittleEndian.PutUint32(b[16:], 16) // tamanho do chunk fmt
	binary.LittleEndian.PutUint16(b[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(b[22:], canais)
	binary.LittleEndian.PutUint32(b[24:], uint32(sampleRateHz))
	binary.LittleEndian.PutUint32(b[28:], uint32(sampleRateHz*blockAlign)) // byte rate
	binary.LittleEndian.PutUint16(b[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(b[34:], bits)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(dataSize))
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(s))
	}
	return b, nil
}

// respostaServidor é o corpo que o whisper-server devolve em /inference.
type respostaServidor struct {
	Text string `json:"text"`
}

// parseResposta extrai a transcrição do corpo devolvido pelo servidor.
//
// Recusa corpo vazio e recusa transcrição vazia. Os dois parecem inócuos e
// não são: uma transcrição vazia aceita aqui seguiria como se a pessoa não
// tivesse dito nada, o histórico registraria o ditado como malsucedido, e a
// causa real — servidor com problema, áudio truncado no caminho — ficaria
// invisível. O harness do benchmark aprendeu essa mesma lição e tem a
// guarda equivalente (internal/benchmark/harness).
func parseResposta(corpo []byte) (string, error) {
	if len(corpo) == 0 {
		return "", fmt.Errorf("asr/local: servidor devolveu corpo vazio")
	}
	var r respostaServidor
	if err := json.Unmarshal(corpo, &r); err != nil {
		return "", fmt.Errorf("asr/local: resposta do servidor não é JSON válido: %w", err)
	}
	texto := strings.TrimSpace(r.Text)
	if texto == "" {
		return "", fmt.Errorf("asr/local: servidor devolveu transcrição vazia")
	}
	return texto, nil
}

// Política de reinício do servidor.
//
// O servidor residente pode morrer: o SO mata por pressão de memória, o
// processo trava, alguém encerra pelo Gerenciador de Tarefas. O provider
// tenta recolocá-lo de pé, mas com limite — insistir para sempre num
// servidor que não sobe transforma cada ditado numa espera longa que
// termina em erro, e é pior para quem usa do que falhar rápido e dizer o
// motivo.
const (
	// maxReinicios é quantas vezes seguidas o provider tenta subir o
	// servidor antes de desistir e devolver ErrServerDead. Zera a cada
	// subida bem-sucedida.
	//
	// ESCOLHA POR JULGAMENTO, não medição.
	maxReinicios = 3

	// backoffBase é o intervalo da primeira espera entre tentativas;
	// dobra a cada falha.
	//
	// ESCOLHA POR JULGAMENTO, não medição.
	backoffBase = 500 * time.Millisecond

	// backoffMax limita o crescimento.
	backoffMax = 5 * time.Second
)

// backoffPara devolve quanto esperar antes da tentativa de índice n
// (começando em zero). Cresce dobrando e satura em backoffMax.
func backoffPara(n int) time.Duration {
	if n <= 0 {
		return backoffBase
	}
	d := backoffBase
	for i := 0; i < n; i++ {
		d *= 2
		if d >= backoffMax {
			return backoffMax
		}
	}
	return d
}
