package painel

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/guard"
)

// Config é o que Handler precisa para montar as rotas.
type Config struct {
	// Porta é a porta efetiva do listener (guard.Listen). É com ela que o
	// guard monta as listas de Host e Origin aceitos.
	Porta int
	// Token é o da Local API, gerado por guard.NewToken no start.
	Token    string
	Bilhetes *Bilhetes
	// API é o handler da Local API (localapi.Server.Handler).
	API http.Handler
	// DirPainel é onde está o build do React (index.html e assets). Se
	// não houver index.html ali, a raiz serve uma página explicando como
	// construir o painel, em vez de um 404 mudo.
	DirPainel string
	// Retorno é o GET /nuvem/retorno, a volta da página de aprovação da
	// nuvem ao PC (ADR-019, F6; nuvem.Servico.Retorno). Nil: a rota cai no
	// fallback dos arquivos, como antes.
	Retorno http.Handler
}

// Handler monta as três camadas de rota do painel.
//
//	/api/sessao       Host + Origin + bilhete   troca o bilhete pelo token
//	/api/...          Host + Origin + token     a Local API da Fase 1
//	/nuvem/retorno    Host                      a volta da aprovação da nuvem (F6)
//	/...              Host                      os arquivos do painel
//
// Os arquivos do painel não pedem token porque o navegador os carrega
// ANTES de ter um — é a página que busca o token. Continuam atrás do
// Host, que é o que barra DNS rebinding (THR-002): sem ele, um site
// qualquer poderia apontar um domínio para 127.0.0.1 e ler o painel.
//
// O /nuvem/retorno também não pede token nem Origin: quem chega é o
// navegador, levado pela página da nuvem (location.replace), sem o token da
// Local API e com a origem da nuvem. O que o protege é o state do pedido em
// curso, de uso único e com prazo, e o PKCE na troca (internal/agent/nuvem);
// o Host continua barrando o DNS rebinding.
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()

	semToken := guard.Chain(guard.RequireHost(cfg.Porta), guard.RequireOrigin(cfg.Porta))
	mux.Handle("POST /api/sessao", semToken(trocarBilhete(cfg.Bilhetes, cfg.Token)))
	mux.Handle("/api/", guard.Protect(cfg.Porta, cfg.Token)(cfg.API))
	if cfg.Retorno != nil {
		mux.Handle("/nuvem/retorno", guard.RequireHost(cfg.Porta)(cfg.Retorno))
	}
	mux.Handle("/", guard.RequireHost(cfg.Porta)(arquivos(cfg.DirPainel)))

	return cabecalhosDeSeguranca(mux)
}

// trocarBilhete recebe {"bilhete": "..."} e devolve {"token": "..."}.
//
// Nenhum dos dois vai para log: o corpo não é registrado em lugar nenhum
// deste pacote, e guard.LogRequests, se estiver na cadeia, registra só
// método, caminho e status.
func trocarBilhete(b *Bilhetes, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var corpo struct {
			Bilhete string `json:"bilhete"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&corpo); err != nil {
			http.Error(w, "corpo inválido", http.StatusBadRequest)
			return
		}
		if !b.Trocar(corpo.Bilhete) {
			// Mesma resposta para bilhete inexistente, expirado ou já
			// usado: distinguir diria a quem tenta adivinhar quando ele
			// chegou perto.
			http.Error(w, "bilhete inválido ou expirado — abra o painel pelo menu da bandeja", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	}
}

// arquivos serve o build do React, com a regra de aplicação de página
// única: caminho que não é arquivo devolve o index.html, para o roteador
// do React decidir.
func arquivos(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indice := filepath.Join(dir, "index.html")
		if _, err := os.Stat(indice); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			// Esta página se formata com atributos style, e a CSP geral
			// (cabecalhosDeSeguranca) não libera estilo inline: ela saía
			// sem formatação e com violações no console (revisão de
			// 29-set-2026). A política aqui vale só para esta resposta,
			// que não carrega nada nem tem script — por isso default-src
			// 'none' — e a CSP do painel de verdade continua fechada.
			w.Header().Set("Content-Security-Policy", cspSemPainel)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(paginaSemPainel))
			return
		}

		// O caminho relativo é validado ANTES de qualquer os.Stat.
		//
		// path.Clean só trata "/" como separador. Com %5C na URL, o Go
		// decodifica "\" em r.URL.Path, os ".." sobrevivem ao Clean, e o
		// filepath.Join do Windows os resolvia: o Stat rodava FORA da
		// pasta do painel, com as permissões de quem roda o Agent, e o
		// status (404 se existia, 400 se não) dizia a qualquer conta
		// local, sem token, se um arquivo existia em qualquer lugar do
		// disco (revisão de 29-set-2026). O conteúdo só não vazava porque
		// o http.Dir, por acaso, também recusa a "\".
		//
		// filepath.Localize recusa, no Windows, "\", ":" e nome reservado,
		// e o que ele devolve é sempre local (filepath.IsLocal). Caminho
		// que não passa é tratado como arquivo inexistente: a resposta
		// passa a depender só da URL, nunca do que existe fora da pasta.
		limpo := path.Clean("/" + r.URL.Path)

		// O painel do PC nunca tem service worker (ADR-019, decisão 12): o
		// /sw.js é 404 antes do fallback de página única (que o responderia
		// com o index.html e 200) e mesmo que exista um sw.js na pasta (um
		// build da nuvem copiado para bin\painel por engano). Um navegador que
		// tenha um service worker registrado nesta origem, ao conferir o
		// /sw.js e receber 404, o remove. EqualFold: o disco do Windows não
		// distingue maiúscula.
		if strings.EqualFold(limpo, "/sw.js") {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}

		rel, errLocal := filepath.Localize(strings.TrimPrefix(limpo, "/"))
		var info os.FileInfo
		err := os.ErrNotExist
		if errLocal == nil {
			info, err = os.Stat(filepath.Join(dir, rel))
		}
		if limpo == "/" || errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir()) {
			// O index.html nunca é guardado em cache: ele aponta para os
			// assets com hash no nome, e um index velho carregaria um
			// painel velho depois de um rebuild.
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, indice)
			return
		}
		fs.ServeHTTP(w, r)
	})
}

// cabecalhosDeSeguranca vale para toda resposta.
//
// Referrer-Policy no-referrer é o que importa mais aqui: a página abre com
// o bilhete na URL (?t=...), e se ela carregasse qualquer coisa de fora,
// o cabeçalho Referer levaria o bilhete junto. A página apaga o bilhete
// da URL assim que o lê, mas a regra não pode depender disso.
func cabecalhosDeSeguranca(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cab := w.Header()
		cab.Set("Referrer-Policy", "no-referrer")
		cab.Set("X-Content-Type-Options", "nosniff")
		cab.Set("X-Frame-Options", "DENY")
		cab.Set("Content-Security-Policy",
			"default-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.ServeHTTP(w, r)
	})
}

// cspSemPainel é a CSP da página 503 abaixo, e só dela: libera estilo
// inline, que é tudo o que a página usa, e fecha o resto.
const cspSemPainel = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

const paginaSemPainel = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><title>Aurora Whisper</title></head>
<body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem;color:#222">
<h1 style="font-size:1.25rem">O painel não foi construído</h1>
<p>O Agent está rodando, mas não achou os arquivos do painel na pasta <code>painel</code> ao lado do executável.</p>
<p>Se você instalou o programa, reinstale-o: a pasta <code>painel</code> faz parte da instalação.</p>
<p>Se você está montando o programa a partir do código, rode na pasta do repositório:</p>
<pre style="background:#f3f3f3;padding:.75rem;border-radius:6px">pnpm -C web install
pnpm -C web build</pre>
<p>e abra o painel de novo pelo menu da bandeja.</p>
</body></html>`
