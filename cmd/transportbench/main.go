// Command transportbench mede o custo de transporte, sozinho, sem nenhum
// modelo de ASR envolvido — a medição que docs/adr/ADR-012-asr-local-como-processo-externo.md
// exige rodar ANTES do benchmark de candidatos (cmd/asrbench), porque é a
// única forma de separar "quanto custa atravessar a fronteira de
// processo/rede" de "quanto custa o modelo": se não for medida isolada,
// todo número warm do benchmark real sai com o transporte embutido e sem
// como decompor.
//
// # O que mede
//
// Um processo de eco (echo-pipe/echo-tcp/echo-http, subcomandos ocultos
// deste mesmo binário — ver echo_pipe.go, echo_tcp.go, echo_http.go) que
// não faz absolutamente nada além de devolver os bytes que recebeu, nos
// três transportes de docs/benchmarks/candidatos-2026-09-17.md que dá
// para medir sem dependência nova (ver transport_pipe.go, transport_tcp.go,
// transport_http.go):
//
//   - pipe stdin/stdout — o que whisper-cli usa (`-f -`)
//   - TCP cru em loopback — o piso de qualquer protocolo sobre socket
//   - HTTP POST multipart em loopback — o que whisper-server e
//     nemo-speech serve usam
//
// Named pipe do Windows NÃO entra: exigiria código Windows-only sem
// cobertura de teste no Linux (scopeNote em cli.go repete este aviso na
// saída do comando — uma omissão declarada, não silenciosa).
//
// Para cada transporte × tamanho, mede cold (iniciar o processo + uma
// ida-e-volta + encerrar, como um só número) e warm (uma ida-e-volta num
// processo já vivo) separadamente — nunca somados (ADR-012) — mais um
// controle in-process (chamada de função no mesmo processo, sem
// transporte nenhum: o piso que diz quanto do tempo é overhead do
// harness, não do transporte).
//
// # O que este binário NUNCA faz
//
//   - não baixa nem instala nenhum modelo de ASR — esta medição é,
//     deliberadamente, sem modelo nenhum;
//   - não adiciona dependência — só stdlib;
//   - não injeta entrada nem alterada recurso global do sistema (só
//     spawna subprocessos de si mesmo e fala com eles por pipe/socket de
//     loopback, exatamente como cmd/asrbench já faz com candidatos de
//     verdade).
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
