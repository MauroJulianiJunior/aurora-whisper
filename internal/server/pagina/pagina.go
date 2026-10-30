// Package pagina são as páginas fixas da nuvem que não são o app: as duas do
// login que não deu (ADR-019, F3), recusado (a conta não tem acesso) e falhou
// (venceu, foi cancelado, o Google falhou), e a moldura da política de
// privacidade da instância (privacidade.html). As do login não ecoam nada do
// pedido. Na F4, a página provisória da F3 (entrar, sessões, sair) deu lugar
// ao app (o build da nuvem do painel), e as duas ganharam o visual da tela
// Entrar dele.
//
// Sem script nem estilo embutido no HTML: a CSP do servidor é
// default-src 'self'. O estilo é o pagina.css, servido em /pagina.css.
package pagina

import (
	"bytes"
	"embed"
)

// FS são os arquivos das páginas.
//
//go:embed pagina.css recusado.html falhou.html privacidade.html
var FS embed.FS

// marcaDoCorpo é onde, no privacidade.html, entra o texto da política.
const marcaDoCorpo = "<!--CORPO-->"

// Privacidade monta a página da política de privacidade: a moldura do
// privacidade.html com o corpo, que já é HTML (o Markdown(...) do texto da
// instância, todo escapado), no lugar da marca.
func Privacidade(corpo string) []byte {
	moldura, err := FS.ReadFile("privacidade.html")
	if err != nil || !bytes.Contains(moldura, []byte(marcaDoCorpo)) {
		panic("pagina: o privacidade.html embutido não tem a marca do corpo")
	}
	return bytes.Replace(moldura, []byte(marcaDoCorpo), []byte(corpo), 1)
}
