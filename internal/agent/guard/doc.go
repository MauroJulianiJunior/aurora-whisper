// Package guard implementa a camada de segurança da Local API do
// Windows Agent: o listener loopback e a cadeia de middlewares HTTP
// que protegem AC-012 ("Dashboard local não aceita conexão externa") e
// NFR-007 ("Local API bind somente loopback").
//
// A Local API não é HTTP comum. Ela é superfície de ataque exposta a
// qualquer página que o usuário abrir no navegador enquanto o Agent
// roda, então cada defesa aqui neutraliza uma ameaça específica de
// docs/ai-spec/09-security-privacy.md (seção "Threat model"):
//
//   - THR-002 (DNS rebinding): um site malicioso registra um domínio
//     próprio apontando para 127.0.0.1 e faz o navegador da vítima
//     chamar a API local. O pacote chega DE loopback — checar o IP de
//     origem não protege, porque nesse ataque o IP de origem É
//     loopback. Quem denuncia o ataque é o header Host, que carrega o
//     domínio do atacante em vez de "127.0.0.1"/"localhost". Ver
//     RequireHost.
//
//   - THR-001 (abuso da Local API): mesmo vindo de loopback, uma
//     página cross-origin pode tentar chamar a API via fetch/XHR. O
//     Origin denuncia essa chamada (RequireOrigin), e o token por
//     sessão (NewToken/RequireToken) garante que só quem tem posse do
//     token — o próprio dashboard local, que o recebe em memória —
//     consegue usar a API.
//
// Uso típico no start do Agent:
//
//	lis, err := guard.Listen(0) // porta 0: SO escolhe uma livre
//	token, err := guard.NewToken()
//	port := lis.Addr().(*net.TCPAddr).Port
//	handler := guard.Protect(port, token)(mux)
//	http.Serve(lis, handler)
package guard
