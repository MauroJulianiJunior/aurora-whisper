// Package cloudauth reúne as peças puras do login e das credenciais da nuvem
// (ADR-019, fatia F3), só com a biblioteca padrão e sem banco:
//
//   - o login Google (google.go): a URL de autorização, com state, nonce e
//     PKCE (pkce.go), e a troca do código no endpoint de token;
//   - a validação do ID token que esse endpoint devolve (idtoken.go), sem
//     JWKS: ele chega direto do Google, por TLS, numa chamada autenticada com
//     o client secret (OpenID Connect Core 1.0, 3.1.3.7, item 6);
//   - os segredos (segredo.go): 256 bits aleatórios, o SHA-256 e o HMAC que o
//     banco guarda no lugar deles;
//   - a lista CONTAS_PERMITIDAS (contas.go);
//   - o caminho de volta depois do login, sem open redirect (volta.go);
//   - o rótulo curto do aparelho para a lista de sessões (rotulo.go);
//   - o limite de tentativas, um balde de fichas com relógio injetável
//     (limite.go);
//   - o IP de quem pede, com o X-Forwarded-For valendo só vindo do proxy
//     confiável (ip.go). O IP só serve de chave do limite: nunca vai a log.
//
// Nada aqui loga. Os erros têm texto fixo: nunca carregam o código, o
// state, o token, o e-mail nem o corpo de uma resposta do Google.
package cloudauth
