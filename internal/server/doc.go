// Package server reúne a orquestração do backend cloud: autenticação,
// sincronização, histórico global e o ditado pelo celular, conforme
// docs/ai-spec/04-system-design.md.
//
// # O servidor da nuvem (ADR-019 e ADR-021)
//
// O servidor da nuvem é o Cloud (nuvem.go), montado por NewCloud sobre um
// cloudstore.Store (o histórico e o sync, store_routes.go) e um
// cloudstore.Accounts (o login Google, as sessões e o token do aparelho): o de
// memória, ou o de Postgres em internal/server/pgstore. A tabela de rotas
// (rotas.go) diz quem pode chamar cada rota (sessão, aparelho, qualquer ou
// público), e o que não está nela responde 404. Por fora ficam os cabeçalhos
// de segurança, a conferência do Host, o CSRF (http.CrossOriginProtection) e o
// log de acesso só com o padrão da rota (seguranca.go, acesso_log.go).
//
//   - o login está em auth_routes.go; as sessões, o /me e o "apagar meus
//     dados", em sessoes.go; o pareamento do PC, em pareamento.go;
//   - o app (o build da nuvem do painel), em painel.go; as duas páginas fixas
//     do login que não deu, em pagina/;
//   - as chaves de IA que a pessoa cola no app, o aceite e as preferências do
//     ditado (chaves_ia.go e ditado_celular.go) e a rota de ditar (ditar.go e
//     os arquivos ditar_*), que só existem com a chave de cifra (cofre/);
//   - o cmd/server sobe tudo (o subcomando serve, e o teste, só com a build
//     tag logindeteste).
//
// O servidor em memória da fase F1-D (o Server, com os repositórios em
// memória, o SyncEngine genérico e o gateway multipart de ASR) saiu na C5c da
// ADR-021: ninguém o usava além dos próprios testes. A prova do isolamento por
// usuário, da sessão e das rotas que não existem é das suítes authtest e
// contracttest, rodadas contra a memória e contra o Postgres.
//
// # Responsabilidade
//
// Expor a API HTTPS versionada (/api/v1) consumida pelo Windows Agent, pelo
// app do celular e pelo painel em modo cloud, e coordenar:
//   - a autenticação (a sessão do navegador e o token do aparelho, rotas.go);
//   - o histórico e o sync (push e pull) escopados ao usuário da credencial
//     (AC-035, NFR-012, THR-004), com o servidor como fonte consolidada;
//   - o ditado pelo celular: o áudio chega cru, é encaminhado ao provedor de
//     transcrição com a chave da pessoa e descartado ao fim, sem tocar disco
//     nem log (AC-028, AC-041).
//
// # user_id estrutural
//
// Nenhum tipo de corpo de requisição (request DTO) deste pacote declara um
// campo UserID: o único caminho para obter o usuário da requisição é
// principalFromContext, alimentado pelo portão de cada rota da nuvem
// (Cloud.proteger) a partir da credencial autenticada — nunca do payload do
// cliente (AC-035, NFR-012, THR-004: IDOR). Ver auth.go e rotas.go.
//
// # Escopo (não confundir com internal/sync)
//
// internal/sync (pacote irmão, fora do escopo deste pacote) é o motor de sync
// do lado CLIENTE (Windows Agent): outbox, backoff, aplicação local das
// mudanças que chegam do servidor. O lado SERVIDOR do mesmo protocolo é o
// cloudstore; os dois conversam pelo contrato HTTP de
// docs/ai-spec/06-api-contract.md, não por um tipo Go compartilhado.
package server
