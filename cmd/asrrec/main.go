// Command asrrec é o gravador de console que produz o dataset de voz do
// benchmark de ASR (cmd/asrbench, internal/benchmark/dataset,
// docs/ai-spec/19-eval-plan.md): lê um manifesto JSON
// (testdata/asr-dataset/manifest.json é o versionado no repo) e, para
// cada amostra cujo arquivo de áudio ainda não existe, guia o dono do
// produto por ENTER no console até todas estarem gravadas.
//
// # Segurança
//
// A interação é exclusivamente por stdin/stdout de console (bufio.Scanner
// em os.Stdin). Este binário nunca instala hook de teclado, nunca chama
// SendInput/keybd_event/mouse_event, nunca escreve no clipboard, nunca
// cria janela nem move foco — CLAUDE.md, "Testes que agem no sistema
// real". A única coisa que ele toca no sistema real é o microfone padrão,
// via internal/agent/audio (a mesma captura WASAPI do Windows Agent de
// produção), e o disco (lendo o manifesto, escrevendo cada WAV aceito).
//
// # Build tag
//
// A captura de áudio só existe no Windows — record_windows.go
// (implementação real, sobre internal/agent/audio) e record_other.go
// (stub que devolve um erro claro em qualquer outro SO) isolam essa
// diferença atrás da interface recorder (recorder.go). Todo o resto —
// parsing do manifesto e da resposta do usuário, seleção da próxima
// amostra pendente, formatação do progresso, avisos de nível/clipping, e
// o escritor de WAV (internal/benchmark/dataset/wav.go) — é Go puro, sem
// build tag, e roda nos testes em qualquer SO (CLAUDE.md, "compilação
// multiplataforma").
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, newRecorder()))
}
