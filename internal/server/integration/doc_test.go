// Package integration_test prova, de ponta a ponta e por HTTP de verdade
// (httptest.Server, não chamada direta de função), que o lado do PC e a
// nuvem (internal/server) falam o mesmo contrato:
//
//   - o pareamento da F6 (pareamento_e2e_test.go): o nuvem.Servico do PC, com
//     o servidor do painel em loopback, contra a nuvem de verdade;
//   - o sync da F7a e da F7b (sync_e2e_test.go, com o palco em
//     sync_harness_test.go): dois PCs (o SQLite, a fila, o nuvem.Servico e o
//     laço de sync com o cliente HTTPS de verdade) e o celular (a sessão do
//     navegador, pelo CRUD) contra a nuvem de verdade sobre o Store em
//     memória, com um porteiro que derruba a rede, perde respostas, manda o
//     410 e redireciona.
//
// Este pacote não tem nenhum arquivo fora de _test.go de propósito: nada
// aqui é consumido por outro pacote. Nenhum teste toca o sistema: o token é
// de mentira, criado no Store de teste, e mora num chaves.Memoria; nada abre
// janela; os bancos ficam na pasta temporária do teste.
//
// A meta dura (docs/ai-spec/02-prd.md, "Métricas iniciais"): "sync
// confirmado perdido: 0".
//
// Os testes do cliente de sync antigo (a F1-D: o motor de internal/sync que
// resolvia conflito pelo pull da mesma rodada, contra o servidor antigo sem
// Store) saíram com ele na F7a. O AC-021 (o dicionário que sincroniza) que
// eles cobriam pelo envelope genérico fica para a F8 (o aprendido), pelo
// contrato da ADR-019.
package integration_test
