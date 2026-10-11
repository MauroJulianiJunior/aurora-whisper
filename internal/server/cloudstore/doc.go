// Package cloudstore é o contrato de gravação e leitura da nuvem na etapa 5
// (ADR-019, fatia F2): os tipos do ditado na nuvem, as regras puras do push
// (validar, mesclar, acusar conflito, recusar, responder gone) e das rotas de
// CRUD, a interface Store que o servidor HTTP usa e uma implementação em
// memória dela (MemoryStore).
//
// Desde a F8b, também o aprendido (learned.go; ADR-019, "Decisões da F8"): o
// par que o botão Aprender de um PC gravou, com as pré-checagens de
// internal/dictionary (as mesmas que o PC faz na projeção) e a decisão pura
// DecideLearned (vale o primeiro na nuvem, e o desfazer vence). O pull o
// funde por seq aos ditados e às lápides, e o "apagar meus dados" o leva
// junto.
//
// Desde a C1 da ADR-021, também o ditado que nasce na nuvem (criar.go): o do
// celular, que o servidor transcreve e grava (Store.Create), sem aparelho e
// com o epoch esperado conferido sob a trava do contador, e o Store.Situacao,
// que diz se um id é um ditado, uma lápide ou nada. A regra é a de criar do
// push, e a validação, a do payload dele.
//
// A implementação Postgres está em internal/server/pgstore. As duas chamam as
// MESMAS regras deste pacote, e a mesma suíte de contrato
// (internal/server/contracttest) roda contra as duas: o servidor em memória e
// o de Postgres respondem igual, e só o Postgres prova a concorrência de
// verdade.
//
// # Ponto único de gravação (ADR-009; ADR-019, "Gravação única")
//
// Toda mudança de um ditado, venha do CRUD (o celular), do push (o PC) ou do
// "excluir definitivamente", e toda mudança do aprendido, passa por um
// aplicador só, numa transação curta:
//
//  1. o contador do usuário sobe (o seq da mudança) e trava até o fim;
//  2. a entidade é gravada com a versão nova e o last_seq dela;
//  3. o sync_event é gravado com o mesmo seq e o aparelho de origem;
//  4. no push, a applied_mutation é gravada, para um reenvio receber
//     duplicate com o mesmo resultado.
//
// Este pacote decide O QUE gravar (puro, sem banco); o Store decide COMO
// (a transação). Uma decisão que não grava entidade não gasta seq: o número
// volta, e a sequência de eventos de cada usuário não tem buraco.
//
// # O que nunca entra aqui
//
// Áudio, contexto de janela (o nome do app em foco, título, texto de perto) e
// target_app: a lista de campos do payload é fechada, e campo fora dela é
// recusado. Este pacote não loga nada, e os erros que devolve nunca carregam
// texto ditado.
package cloudstore
