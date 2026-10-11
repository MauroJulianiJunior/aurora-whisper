package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"time"
)

// O app da nuvem (ADR-019, F4): o build da nuvem do painel (pnpm build:nuvem,
// web/dist-nuvem), que a própria nuvem serve, na mesma origem da API. O
// servidor lê a pasta uma vez, na subida (PAINEL_DIR), para a memória, e
// nenhum caminho de pedido chega ao disco: cada arquivo vira uma rota da
// tabela (ou uma entrada do mapa de /assets), e o que não está nela é o 404
// das rotas não montadas.
//
// Sem fallback de SPA: o app não tem rotas na F4, e um caminho qualquer
// continua 404. A CSP de sempre (default-src 'self') vale para tudo; o build
// não tem script nem estilo embutido (web/bundle.test.ts confere).

// tiposDoPainel são os tipos que o app usa, numa tabela fixa: no Windows, o
// mime.TypeByExtension lê o registro, que pode dizer outra coisa do .js (e o
// nosniff faria o navegador recusar o script). Extensão fora da tabela é
// erro na subida, e não um arquivo servido com tipo inventado. O .png, o
// .ico e o .webmanifest já estão aqui para os ícones e o manifest da F5.
var tiposDoPainel = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".svg":         "image/svg+xml",
	".woff2":       "font/woff2",
	".txt":         "text/plain; charset=utf-8",
	".png":         "image/png",
	".ico":         "image/vnd.microsoft.icon",
	".webmanifest": "application/manifest+json",
}

// Cache: o index.html e os arquivos da raiz (o favicon, as licenças) mudam de
// conteúdo sem mudar de nome, e o navegador confere a cada vez (ETag, 304);
// os de /assets têm o hash no nome e nunca mudam.
const (
	cacheDaRaiz   = "no-cache"
	cacheDeAssets = "public, max-age=31536000, immutable"
)

// Limites da leitura: nenhum arquivo do build chega perto (o maior, o JS, tem
// uns 300 KB; as fontes, uns 36 KB), e uma pasta errada (o disco inteiro, por
// engano) não enche a memória.
const (
	maxArquivoDoPainel  = 8 << 20
	maxArquivosDoPainel = 512
)

// nomeDoPainel: o nome de um arquivo do build. É o que o Vite gera
// (index-a1B2c3.js) e o que vira padrão de rota, então nada além de letras,
// números, ponto, hífen e sublinhado.
var nomeDoPainel = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

// reservadosDaRaiz são nomes que a nuvem já usa fora do app.
var reservadosDaRaiz = map[string]bool{"index.html": true, "pagina.css": true}

type arquivoDoPainel struct {
	corpo []byte
	tipo  string
	etag  string
}

// painel é o build na memória.
type painel struct {
	index  arquivoDoPainel
	raiz   map[string]arquivoDoPainel
	assets map[string]arquivoDoPainel
}

// ErrPainel: o build do painel falta ou não é o que o servidor serve.
var ErrPainel = errors.New("server: o build do painel")

func erroDoPainel(format string, a ...any) error {
	return fmt.Errorf("%w (PAINEL_DIR): %s", ErrPainel, fmt.Sprintf(format, a...))
}

// carregarPainel lê o build: o index.html (obrigatório), os arquivos da raiz e
// os de assets/. Os erros dizem o nome do arquivo, nunca o caminho da pasta.
func carregarPainel(fsys fs.FS) (*painel, error) {
	if fsys == nil {
		return nil, erroDoPainel("não foi dado (rode pnpm build:nuvem e aponte PAINEL_DIR para web/dist-nuvem)")
	}
	p := &painel{raiz: map[string]arquivoDoPainel{}, assets: map[string]arquivoDoPainel{}}
	contados := 0
	ler := func(nome string) (arquivoDoPainel, error) {
		contados++
		if contados > maxArquivosDoPainel {
			return arquivoDoPainel{}, erroDoPainel("mais de %d arquivos: não parece o build do painel", maxArquivosDoPainel)
		}
		base := path.Base(nome)
		if !nomeDoPainel.MatchString(base) {
			return arquivoDoPainel{}, erroDoPainel("o nome %q não é de um arquivo do build", base)
		}
		tipo, ok := tiposDoPainel[path.Ext(base)]
		if !ok {
			return arquivoDoPainel{}, erroDoPainel("%q tem uma extensão que o servidor não serve", base)
		}
		info, err := fs.Stat(fsys, nome)
		if err != nil || !info.Mode().IsRegular() {
			return arquivoDoPainel{}, erroDoPainel("não consegui ler %q", base)
		}
		if info.Size() > maxArquivoDoPainel {
			return arquivoDoPainel{}, erroDoPainel("%q é grande demais para ser do build", base)
		}
		corpo, err := fs.ReadFile(fsys, nome)
		if err != nil {
			return arquivoDoPainel{}, erroDoPainel("não consegui ler %q", base)
		}
		h := sha256.Sum256(corpo)
		return arquivoDoPainel{corpo: corpo, tipo: tipo, etag: `"` + hex.EncodeToString(h[:16]) + `"`}, nil
	}

	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return nil, erroDoPainel("não tem o index.html (rode pnpm build:nuvem e aponte PAINEL_DIR para web/dist-nuvem)")
	}
	var err error
	if p.index, err = ler("index.html"); err != nil {
		return nil, err
	}
	raiz, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, erroDoPainel("não consegui listar a pasta")
	}
	for _, e := range raiz {
		nome := e.Name()
		switch {
		case e.IsDir() && nome == "assets":
			if err := p.lerAssets(fsys, ler); err != nil {
				return nil, err
			}
		case e.IsDir():
			return nil, erroDoPainel("a pasta %q não é do build (só assets/)", nome)
		case nome == "index.html":
		case reservadosDaRaiz[nome]:
			return nil, erroDoPainel("%q é um nome que a nuvem já usa", nome)
		default:
			a, err := ler(nome)
			if err != nil {
				return nil, err
			}
			p.raiz[nome] = a
		}
	}
	return p, nil
}

func (p *painel) lerAssets(fsys fs.FS, ler func(string) (arquivoDoPainel, error)) error {
	es, err := fs.ReadDir(fsys, "assets")
	if err != nil {
		return erroDoPainel("não consegui listar assets/")
	}
	for _, e := range es {
		if e.IsDir() {
			return erroDoPainel("assets/ não tem subpastas no build (achei %q)", e.Name())
		}
		a, err := ler("assets/" + e.Name())
		if err != nil {
			return err
		}
		p.assets[e.Name()] = a
	}
	return nil
}

// CaminhoDeAprovar é a página de aprovação de um PC (ADR-019, F6).
const CaminhoDeAprovar = "/device/authorize"

// rotas são as linhas do app na tabela de rotas: o /, a página de aprovação
// de um PC, os arquivos da raiz e o /assets/{arquivo}. Todas públicas: o app é a casca, e quem guarda os
// dados é a API.
func (p *painel) rotas() []rota {
	t := []rota{
		{"GET /{$}", AcessoPublico, servirArquivo(p.index, cacheDaRaiz)},
		// A página de aprovação de um PC (F6): o mesmo index.html, e o app
		// decide pela barra de endereço (AppNuvem). Sem sessão, ela mostra
		// Entrar, com a volta para o mesmo endereço.
		{"GET " + CaminhoDeAprovar, AcessoPublico, servirArquivo(p.index, cacheDaRaiz)},
		{"GET /assets/{arquivo}", AcessoPublico, p.servirAsset},
	}
	nomes := make([]string, 0, len(p.raiz))
	for nome := range p.raiz {
		nomes = append(nomes, nome)
	}
	slices.Sort(nomes)
	for _, nome := range nomes {
		t = append(t, rota{"GET /" + nome, AcessoPublico, servirArquivo(p.raiz[nome], cacheDaRaiz)})
	}
	return t
}

func (p *painel) servirAsset(w http.ResponseWriter, r *http.Request) {
	a, ok := p.assets[r.PathValue("arquivo")]
	if !ok {
		handleNaoMontada(w, r)
		return
	}
	servirArquivo(a, cacheDeAssets)(w, r)
}

// servirArquivo responde com o arquivo da memória: o tipo da tabela, o cache
// e o ETag, e o 304 quando o navegador já tem esta versão (ServeContent).
func servirArquivo(a arquivoDoPainel, cache string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", a.tipo)
		h.Set("Cache-Control", cache)
		h.Set("ETag", a.etag)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.corpo))
	}
}
