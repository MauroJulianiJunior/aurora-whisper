package server

import (
	"net/http"
	"path"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pagina"
)

// As duas páginas fixas do login que não deu (internal/server/pagina) e o
// estilo delas. O app (a tela Entrar e o resto) é o build do painel
// (painel.go).

var tiposDaPagina = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
}

// arquivoDaPagina lê um arquivo embutido; um nome que não existe é bug de
// montagem e para o servidor na subida (tabela() o chama).
func arquivoDaPagina(name string) []byte {
	b, err := pagina.FS.ReadFile(name)
	if err != nil {
		panic("server: a página embutida não tem " + name)
	}
	return b
}

// servirPagina serve um arquivo das páginas fixas, com no-cache.
func servirPagina(name string) http.HandlerFunc {
	body, ctype := arquivoDaPagina(name), tiposDaPagina[path.Ext(name)]
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}

// paginaFixa responde com uma página fixa (recusado ou falhou), sem nada do
// pedido.
func paginaFixa(w http.ResponseWriter, status int, name string) {
	w.Header().Set("Content-Type", tiposDaPagina[".html"])
	w.WriteHeader(status)
	_, _ = w.Write(arquivoDaPagina(name))
}
