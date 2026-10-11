// Command whisperwarm é o adaptador exigido pelo ADR-012
// (docs/adr/ADR-012-asr-local-como-processo-externo.md) para medir o
// whisper-server.exe em modo warm com o harness de benchmark.
//
// # O problema que este adaptador resolve
//
// O harness (internal/benchmark/candidate/warm.go) fala um protocolo de
// linha por stdin/stdout, documentado no comentário de pacote de
// internal/benchmark/candidate/candidate.go: uma linha em stdin (caminho
// do áudio) produz uma linha em stdout (a transcrição). O whisper-server
// fala HTTP (POST multipart em /inference). Os dois não encaixam
// diretamente, e o ADR-012 proíbe inventar um protocolo próprio para o
// whisper-server ou embutir o runtime no binário do Agent via CGO.
//
// whisperwarm faz a ponte: fala o protocolo de linha do harness de um
// lado, HTTP com um whisper-server filho do outro.
//
// # Ciclo de vida
//
//  1. Ao subir, inicia thirdparty/whisper/Release/whisper-server.exe (ou
//     o caminho passado em -server) como processo filho, em loopback
//     (127.0.0.1, porta efêmera escolhida pelo SO — nunca 0.0.0.0, regra
//     do projeto, ver CLAUDE.md "API local deve bindar somente em
//     loopback").
//  2. Espera o servidor responder no endpoint raiz, com timeout
//     -ready-timeout (padrão 60s).
//  3. Lê linhas de stdin. Cada linha é um caminho de áudio; para cada
//     uma, lê o arquivo, envia POST multipart para /inference, e escreve
//     a transcrição de volta como UMA linha em stdout — nunca mais,
//     nunca menos, mesmo quando a transcrição falha (ver runProtocol em
//     protocol.go para a justificativa de por que um erro também vira
//     uma linha, em vez de deixar a chamada do harness estourar por
//     timeout).
//  4. Ao receber EOF em stdin, encerra o processo whisper-server filho e
//     sai.
//
// # O salto de processo extra, e por que ele não distorce a medição
//
// Este adaptador introduz UM salto de processo que produção não teria —
// lá, o Agent falaria HTTP direto com o servidor, sem intermediário. Isso
// importa porque o benchmark existe para medir o candidato, não o
// harness.
//
// O custo desse salto já foi medido isoladamente, sem envolver nenhum
// modelo real, em docs/benchmarks/transporte-2026-09-17.md: pipe
// stdin/stdout em modo warm (exatamente o transporte que separa este
// adaptador do harness) ficou em p95 = 607 µs, machine com relógio de
// ~504 µs de granularidade — ou seja, abaixo de 0,7 ms, quase três ordens
// de grandeza abaixo da meta de produto (release→insert p50 ≤ 500 ms,
// 02-prd.md §7). Qualquer número de warm medido através deste adaptador
// que fique perto ou acima da meta é custo do candidato (carregar/rodar o
// modelo), não deste salto de processo. Não confunda os dois.
package main
