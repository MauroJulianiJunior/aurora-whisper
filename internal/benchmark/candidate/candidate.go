// Package candidate implementa o adaptador genérico de processo externo
// exigido por docs/adr/ADR-012-asr-local-como-processo-externo.md: mede
// qualquer runtime de ASR local que "recebe wav, devolve texto", sem
// código novo por candidato — só configuração.
//
// # Dois modos, nunca somados (ADR-012)
//
//   - Cold (cold.go): um processo novo é iniciado a cada transcrição, o
//     caminho do áudio é passado por argv (com o marcador AudioPlaceholder
//     substituído), o processo escreve a transcrição em stdout e sai. É o
//     caminho mais simples de integrar — qualquer executável de linha de
//     comando que aceite um caminho de arquivo serve — e mede "quanto
//     custa se nada ficar vivo".
//   - Warm (warm.go): um processo é iniciado UMA VEZ e fica residente. O
//     protocolo é deliberadamente o mais simples que ainda é genérico: uma
//     linha de texto por stdin (o caminho do áudio) produz uma linha de
//     texto por stdout (a transcrição). Um candidato só participa da
//     medição warm se declarar WarmArgs — candidatos que só sabem rodar
//     como CLI de um tiro continuam medidos em cold, e o relatório mostra
//     "warm: não medido" para eles, em vez de inventar um número.
//
// Cada chamada tem um Timeout próprio: o processo (cold) ou a resposta
// (warm) que não voltar a tempo é tratado como falha, nunca como
// travamento do harness — ver ErrTimeout.
package candidate

import (
	"errors"
	"time"
)

// AudioPlaceholder é o marcador substituído pelo caminho absoluto do
// arquivo de áudio dentro de Config.ColdArgs. Precisa aparecer
// exatamente uma vez.
const AudioPlaceholder = "{{audio}}"

// Config descreve como rodar um candidato, conforme ADR-012: nome,
// executável, argumentos (com o marcador do caminho do áudio) e como ler
// o resultado de volta (stdout, para os dois modos).
type Config struct {
	// Name identifica o candidato no relatório (ex.: "whisper-cpp-base",
	// "parakeet-onnx-q8"). Nunca inclui segredo.
	Name string
	// Executable é o caminho (ou nome resolvido via PATH) do binário do
	// candidato. O harness NUNCA baixa nem instala isso — é responsabilidade
	// de uma pessoa ter o runtime instalado antes de rodar o benchmark de
	// verdade (ADR-012, "NAO BAIXE MODELOS").
	Executable string
	// ColdArgs é o argv usado no modo cold — um processo novo por amostra.
	// Deve conter AudioPlaceholder exatamente uma vez.
	ColdArgs []string
	// WarmArgs é o argv usado para iniciar o processo residente do modo
	// warm (protocolo de linha por stdin/stdout — ver doc.go). Vazio
	// significa "este candidato não participa da medição warm".
	WarmArgs []string
	// Timeout é o tempo máximo tolerado por transcrição (cold: do início
	// do processo até ele sair; warm: do envio da linha até a resposta).
	// Zero usa DefaultTimeout.
	Timeout time.Duration
	// StartupTimeout é o tempo máximo tolerado para o processo warm subir
	// e ficar pronto para receber a primeira linha. Zero usa
	// DefaultStartupTimeout.
	StartupTimeout time.Duration
}

// DefaultTimeout e DefaultStartupTimeout são usados quando Config não
// declara um valor.
const (
	DefaultTimeout        = 30 * time.Second
	DefaultStartupTimeout = 30 * time.Second
)

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

func (c Config) startupTimeout() time.Duration {
	if c.StartupTimeout > 0 {
		return c.StartupTimeout
	}
	return DefaultStartupTimeout
}

// Result é o resultado de uma transcrição de candidato, cold ou warm.
type Result struct {
	// Text é a transcrição (stdout trimado). Vazio se Err != nil.
	Text string
	// WallTime é o tempo de ponta a ponta medido pelo harness: para cold,
	// do Start() do processo até ele sair; para warm, do envio da
	// requisição até a linha de resposta chegar. É deliberadamente o
	// tempo que produção pagaria (ADR-012), não só inferência.
	WallTime time.Duration
	// PeakRSSBytes é o maior RSS amostrado durante a chamada.
	// RSSMeasured indica se a amostragem foi possível neste ambiente
	// (internal/benchmark/rss) — quando false, PeakRSSBytes é 0 e NÃO deve
	// ser tratado como medição real.
	PeakRSSBytes int64
	RSSMeasured  bool
}

// ErrTimeout é devolvido quando uma chamada (cold ou warm) não termina
// dentro de Config.Timeout.
var ErrTimeout = errors.New("candidate: tempo esgotado esperando o candidato")

// ErrEmptyExecutable é devolvido quando Config.Executable está vazio.
var ErrEmptyExecutable = errors.New("candidate: Executable vazio")

// ErrMissingPlaceholder é devolvido quando ColdArgs não contém
// AudioPlaceholder.
var ErrMissingPlaceholder = errors.New("candidate: ColdArgs não contém " + AudioPlaceholder)

// substitutePlaceholder devolve uma cópia de args com AudioPlaceholder
// trocado por audioPath. Devolve ErrMissingPlaceholder se o marcador não
// aparecer em nenhum argumento.
func substitutePlaceholder(args []string, audioPath string) ([]string, error) {
	out := make([]string, len(args))
	found := false
	for i, a := range args {
		if a == AudioPlaceholder {
			found = true
			out[i] = audioPath
			continue
		}
		out[i] = a
	}
	if !found {
		return nil, ErrMissingPlaceholder
	}
	return out, nil
}
