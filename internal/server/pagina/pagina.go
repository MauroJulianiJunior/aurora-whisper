// Package pagina são as duas páginas fixas do login que não deu (ADR-019,
// F3): recusado (a conta não tem acesso) e falhou (venceu, foi cancelado, o
// Google falhou). Não ecoam nada do pedido. Na F4, a página provisória da F3
// (entrar, sessões, sair) deu lugar ao app (o build da nuvem do painel), e as
// duas ganharam o visual da tela Entrar dele.
//
// Sem script nem estilo embutido no HTML: a CSP do servidor é
// default-src 'self'. O estilo é o pagina.css, servido em /pagina.css.
package pagina

import "embed"

// FS são os arquivos das páginas.
//
//go:embed pagina.css recusado.html falhou.html
var FS embed.FS
