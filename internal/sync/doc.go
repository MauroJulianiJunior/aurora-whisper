// Package syncengine é o sync do histórico entre o PC e a nuvem, do lado do
// PC (ADR-019, fatias F7a e F7b; 07-flows.md, "Sync"; 06-api-contract.md,
// "Sync").
//
// # Como as peças se encaixam
//
//   - A FILA mora no SQLite (internal/storage, sync_outbox): um marcador por
//     ditado, com os campos que mudaram e nenhum texto. Quem grava o ditado
//     (o ditado novo, a edição no painel, a lixeira, o restaurar) põe o
//     marcador na MESMA transação: com a nuvem conectada, sempre;
//     desconectado, só a mudança num ditado que a nuvem já tem (ela sobe na
//     volta, com a base certa).
//   - A VOLTA de um PC que ficou fora (a reconexão, o 410) no mesmo epoch não
//     reenvia nada: as versões da nuvem que o PC guardou continuam valendo, o
//     cursor continua (ou volta a 0 no 410), e sobe só a fila. Só o epoch
//     novo (um backup restaurado) recomeça com a base vazia.
//   - As DECISÕES são puras (decisao.go): o que cada resultado do push e cada
//     mudança do pull fazem com um ditado, dado o estado dele. Elas devolvem
//     uma storage.AcaoLocal, que internal/storage executa na transação da
//     resposta.
//   - O PAYLOAD é montado da linha na hora de enviar (payload.go): o ditado
//     inteiro para criar, só os campos do marcador para mudar.
//   - O APRENDIDO (aprendido.go, ADR-019, "Decisões da F8") tem as decisões
//     puras dele: a mutação (sai da projeção guardada no espelho), a resposta
//     do push e o par do pull, este em três passos, porque passa pelo
//     dicionario.json, que não tem transação comum com o SQLite. Elas devolvem
//     uma storage.AcaoDoAprendido. A VARREDURA (varredura.go, F8f3) compara o
//     dicionario.json com o espelho no começo de cada rodada: as mudanças
//     daqui viram marcadores (o upsert só depois do "Enviar o histórico"; antes
//     dele, é o M da pergunta), e o par da nuvem pendente é tentado de novo.
//     O RECEBER (receber.go, F8f4) passa o par do pull pelo arquivo: a
//     leitura curta do epoch e do pareamento antes de tocá-lo, o arquivo
//     relido sob a trava do dicionário (o par mudado aqui é pulado), a
//     intenção antes da gravação, e o espelho na transação da página, com os
//     ditados e o cursor. Com o dicionário (Motor.Dicionario), o motor varre,
//     sobe e recebe o aprendido; sem ele, tudo fica como na F7.
//   - O MOTOR é uma rodada (motor.go): o push em lotes até a fila esvaziar e
//     o pull até não ter mais, com o cursor e o epoch.
//   - O CLIENTE é o HTTPS com o token do aparelho (cliente.go), e o LAÇO
//     (laco.go) roda em segundo plano, acordado pelas gravações e pelo
//     relógio, com espera e jitter quando falha.
//
// # As regras que este pacote garante
//
//   - O aplicador nunca sobrescreve um ditado com marcador pendente: pula, e o
//     estado chega pela resposta do push ou pelo pull seguinte.
//   - Aplicar não ecoa: o estado da nuvem é gravado sem marcador, e a version
//     local sobe (uma aba aberta no painel recebe 409).
//   - O eco é reconhecido pela versão, nunca pelo device_id; e a versão de um
//     applied mesclado não é guardada.
//   - A recusa por regra não trava o cursor; só um erro de E/S o segura.
//   - Nenhum pull entre um push sem resposta e o reenvio dele.
//   - Nenhuma rede dentro de uma transação, e nenhuma espera no caminho do
//     ditado.
//   - O log só tem contagens e códigos de listas fechadas.
//
// # Por que o pacote se chama syncengine e não sync
//
// O diretório é internal/sync/ porque o layout exigido pelo CLAUDE.md aponta
// esse caminho, mas o pacote Go não pode se chamar "sync": colidiria com o
// pacote sync da biblioteca padrão. Mesmo critério de internal/context
// (package uiacontext).
package syncengine
