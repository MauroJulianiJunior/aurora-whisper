// Package localapi implementa a API HTTP local que serve o dashboard do
// Windows Agent: histórico de ditados, preferências (settings) e a chave
// de API do provedor de IA.
//
// Este pacote NÃO abre listener nem aplica a cadeia de segurança —
// essas responsabilidades são de internal/agent/guard (ver seu doc.go).
// Server.Handler devolve um http.Handler puro, que o chamador (start do
// Agent, e os testes de ponta a ponta deste pacote) compõe com
// guard.Protect antes de servir:
//
//	lis, _ := guard.Listen(0)
//	token, _ := guard.NewToken()
//	port := lis.Addr().(*net.TCPAddr).Port
//	srv := localapi.New(userID, transcriptionRepo, settingsRepo, preferencias)
//	handler := guard.Protect(port, token)(srv.Handler())
//	http.Serve(lis, handler)
//
// # user_id local
//
// Diferente do backend cloud, a Local API não tem sessão autenticada
// própria — quem barra acesso indevido é a cadeia do guard (Host,
// Origin, token), não um login. Nesta fase (F1-B), o Agent atende um
// único usuário local por instalação, então Server recebe o user_id
// como parâmetro fixo no construtor, resolvido pelo chamador. Hoje é o
// dono fixo da instalação, gravado no banco local
// (storage.GarantirDonoFixo; docs/ai-spec/05-data-model.md, "Dono
// local").
//
// # Rotas
//
//   - GET    /api/history            lista com busca/filtros/paginação por cursor
//   - GET    /api/history/{id}       detalhe (raw_text + final_text)
//   - PATCH  /api/history/{id}       edita final_text (concorrência otimista via base_version) e
//     devolve "sugestoes": as trocas de termo que a edição sugere aprender (ADR-017; nunca grava no
//     dicionário — ver aprendizado.go)
//   - DELETE /api/history/{id}       move para a lixeira (soft delete)
//   - POST   /api/history/{id}/restore  tira da lixeira
//   - GET    /api/settings           preferências (padrão com version 0 antes do primeiro PUT); traz
//     polish_disponivel e polish_ultimo_motivo (a frase de por que o último Polish não agiu, só o
//     motivo — ver polish.go e Preferencias.UltimoPolish)
//   - PUT    /api/settings           grava (concorrência otimista via base_version) e aplica no Agent
//   - GET    /api/ia/chave?provedor= se há chave guardada, e os últimos caracteres dela
//   - PUT    /api/ia/chave           guarda a chave no Gerenciador de Credenciais (204)
//   - DELETE /api/ia/chave?provedor= apaga a chave (204)
//   - POST   /api/ia/testar          lista os modelos do provedor com a chave guardada, e o
//     modelo_sugerido para o Polish (sugestão do projeto, nunca exigida)
//   - GET    /api/aprendizado        o que o painel aprendeu (a lista "Aprendido no painel")
//   - POST   /api/aprendizado        o clique em Aprender: {id, de, para}; refaz as checagens e grava
//   - POST   /api/aprendizado/desfazer  {variante}: tira uma variante que o painel aprendeu
//
// As rotas de aprendizado só existem de verdade com Preferencias.Aprendizado; sem ele, salvar não
// sugere nada, a lista é vazia e aprender/desfazer respondem 503. O log delas é só de contagens.
//
// Nenhuma rota expõe áudio ou contexto de janela: esses campos nem
// existem em storage.Transcription (ver AC-027, AC-028). Nenhuma rota
// devolve a chave de API: ela entra pelo PUT e só sai do cofre para ir ao
// provedor (ver ia.go e vazamento_test.go).
//
// Erros seguem o formato de docs/ai-spec/06-api-contract.md:
//
//	{"error":{"code":"CONFLICT","message":"...","request_id":"..."}}
package localapi
