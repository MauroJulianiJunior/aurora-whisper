// Package server reúne a orquestração do backend cloud: autenticação,
// sincronização, histórico global e gateway de ASR, conforme
// docs/ai-spec/04-system-design.md.
//
// # F1-D — validação de contratos com mocks
//
// Esta fase implementa a API HTTP completa (/api/v1) e a lógica de
// sync server-side com repositórios EM MEMÓRIA. O Postgres real e a
// integração de OAuth/device-auth chegam no Sprint 6/TASK-013
// (docs/ai-spec/13-implementation-plan.md) — este pacote já expõe as
// interfaces (SessionStore, *Repository) que essas implementações reais
// vão satisfazer, sem mudança de rota ou de handler.
//
// # Etapa 5 (ADR-019, F2 e F3)
//
// O servidor da nuvem de verdade é o Cloud (nuvem.go), montado por NewCloud
// sobre um cloudstore.Store (o histórico e o sync da F2, store_routes.go) e um
// cloudstore.Accounts (o login Google, as sessões e o token do aparelho da
// F3): o de memória, ou o de Postgres em internal/server/pgstore. A tabela de
// rotas (rotas.go) diz quem pode chamar cada rota (sessão, aparelho,
// qualquer ou público), e o que não está nela responde 404. Por fora ficam os
// cabeçalhos de segurança, a conferência do Host, o CSRF
// (http.CrossOriginProtection) e o log de acesso só com o padrão da rota
// (seguranca.go, acesso_log.go). O login está em auth_routes.go; as sessões,
// o /me e o "apagar meus dados", em sessoes.go; o app (o build da nuvem do
// painel, F4), em painel.go; as duas páginas fixas do login que não deu, em
// pagina/. O cmd/server sobe tudo (o subcomando serve, e o teste, só com a
// build tag logindeteste).
//
// O Server (server.go) é o servidor em memória da fase F1-D, descrito abaixo,
// que fica só para os testes do cliente de sync antigo até a F7a.
//
// # Responsabilidade
//
// Expor a API HTTPS versionada (/api/v1) consumida pelo Windows Agent,
// por um futuro cliente Android e pelo dashboard React em modo cloud,
// e coordenar:
//   - autenticação de sessão (auth.go: SessionStore/Principal — a
//     emissão real da sessão via Google OAuth e device auth fica para
//     TASK-013; aqui validamos o contrato do lado que consome a sessão
//     já resolvida);
//   - CRUD de devices/transcriptions/dictionary/corrections/settings,
//     sempre escopado ao usuário da sessão (AC-035, NFR-012, THR-004);
//   - sincronização (push/pull) server-side (sync.go), independente dos
//     repositórios tipados acima — motor de mutation_id/base_version/
//     SyncEvent versionando entidades genéricas, do jeito que o
//     Postgres real vai fazer;
//   - encaminhamento de áudio efêmero ao provider de ASR configurado
//     via internal/asr (asr_gateway.go), sem gravar o áudio em disco
//     nem logá-lo (AC-041).
//
// # user_id estrutural
//
// Nenhum tipo de corpo de requisição (request DTO) deste pacote declara
// um campo UserID: o único caminho para obter o usuário da requisição é
// principalFromContext, alimentado pelo middleware requireSession (no
// Server) ou pelo portão de cada rota da nuvem (Cloud.proteger) a partir da
// credencial autenticada — nunca do payload do cliente (AC-035, NFR-012,
// THR-004: IDOR). Ver auth.go e rotas.go.
//
// # Escopo desta fase (não confundir com internal/sync)
//
// internal/sync (pacote irmão, fora do escopo deste pacote) é o motor
// de sync do lado CLIENTE (Windows Agent): outbox, backoff, aplicação
// local das mudanças que chegam do servidor. O SyncEngine deste pacote
// (sync.go) é o lado SERVIDOR do mesmo protocolo — os dois pacotes
// conversam pelo contrato HTTP de docs/ai-spec/06-api-contract.md, não
// por um tipo Go compartilhado.
package server
