package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pagina"
)

// A política de privacidade DA INSTÂNCIA (docs/politica-de-privacidade.md é só
// o modelo, com campos [A PREENCHER]; quem hospeda preenche uma cópia que fica
// FORA do repositório, e o deploy a leva à VPS: docs/deploy-privacidade.md).
// Quando a nuvem tem essa cópia (POLITICA_FILE, lida na subida pelo
// cmd/server), ela a serve em GET /privacidade, pública como o app: quem vai
// usar a nuvem lê a política ANTES de entrar. Sem a cópia, a rota não existe
// (o 404 das rotas não montadas) e nada mais muda.
//
// O servidor transforma o Markdown em HTML uma vez, na subida
// (pagina.Markdown, que escapa tudo), e guarda os bytes da página na memória:
// não há negociação de conteúdo nem script no navegador para desenhá-la. A CSP
// de sempre vale (sem script nem estilo embutido), e a página tem o visual das
// outras páginas fixas da nuvem (pagina.css).

// CaminhoDaPolitica é o endereço da política da instância.
const CaminhoDaPolitica = "/privacidade"

// MaxPolitica é o teto do arquivo da política: a do modelo tem uns 40 KiB, e
// 96 KiB deixa folga sem deixar uma pasta errada (um dump, um log) virar a
// "política" da nuvem.
const MaxPolitica = 96 << 10

// marcaDeCampo é o começo de um campo do modelo ainda por preencher, nas duas
// formas que o modelo usa: "[A PREENCHER: o responsável]" e "[A PREENCHER, se
// for o caso: ...]" (e a nota do topo, "[A PREENCHER]"). Uma cópia que ainda os
// tem não é a política de ninguém: servi-la diria ao público que o responsável
// é "[A PREENCHER...]". A nota do topo do modelo também conta de propósito: a
// cópia a tira (ela é para quem preenche, não para quem lê).
const marcaDeCampo = "[A PREENCHER"

// reTextosDoApp é o título do que o modelo traz depois da política: os textos
// do app sobre privacidade (a folha do aceite, as Configurações do celular).
// São texto interno, nunca publicado: a página termina na linha anterior, mesmo
// que a cópia da instância os tenha herdado.
var reTextosDoApp = regexp.MustCompile(`(?m)^# Os textos do app sobre privacidade[ \t]*$`)

// Os erros de NovaPoliticaDePrivacidade. Nenhum traz o conteúdo do arquivo: o
// texto vai para o terminal e para o log do serviço.
var (
	ErrPoliticaVazia   = errors.New("o arquivo está vazio")
	ErrPoliticaGrande  = errors.New("o arquivo passa de " + strconv.Itoa(MaxPolitica>>10) + " KiB: não parece uma política de privacidade")
	ErrPoliticaNaoUTF8 = errors.New("o arquivo não é um texto UTF-8 válido")
	ErrPoliticaBinaria = errors.New("o arquivo tem caracteres de controle: não parece um texto em Markdown")
	ErrPoliticaModelo  = errors.New("o arquivo ainda tem campos [A PREENCHER...] do modelo: preencha-os (e tire a nota do topo) antes de publicar")
)

// PoliticaDePrivacidade é a página da instância, já conferida e na memória.
type PoliticaDePrivacidade struct {
	pagina arquivoDoPainel
}

// NovaPoliticaDePrivacidade confere o conteúdo do arquivo e monta a página.
// Tira o BOM UTF-8 do começo (o Bloco de Notas o põe) e troca o fim de linha do
// Windows (CRLF, e o CR solto) por LF. Recusa o que passa de MaxPolitica
// (medido no arquivo, antes de trocar nada), o que não é UTF-8 válido, o que
// tem caractere de controle (menos o tab e a quebra de linha: um binário), o
// vazio ou só de espaços e o que ainda tem campo [A PREENCHER...] do modelo.
// Os textos internos do modelo (a partir de "# Os textos do app sobre
// privacidade") ficam de fora da página e da conferência dos campos.
func NovaPoliticaDePrivacidade(raw []byte) (*PoliticaDePrivacidade, error) {
	if len(raw) > MaxPolitica {
		return nil, ErrPoliticaGrande
	}
	if !utf8.Valid(raw) {
		return nil, ErrPoliticaNaoUTF8
	}
	texto := bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
	texto = bytes.ReplaceAll(texto, []byte("\r\n"), []byte("\n"))
	texto = bytes.ReplaceAll(texto, []byte("\r"), []byte("\n"))
	for _, r := range string(texto) {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return nil, ErrPoliticaBinaria
		}
	}
	if loc := reTextosDoApp.FindIndex(texto); loc != nil {
		texto = texto[:loc[0]]
	}
	if len(bytes.TrimSpace(texto)) == 0 {
		return nil, ErrPoliticaVazia
	}
	if bytes.Contains(texto, []byte(marcaDeCampo)) {
		return nil, ErrPoliticaModelo
	}
	corpo := pagina.Privacidade(pagina.Markdown(string(texto)))
	h := sha256.Sum256(corpo)
	return &PoliticaDePrivacidade{pagina: arquivoDoPainel{
		corpo: corpo,
		tipo:  tiposDaPagina[".html"],
		etag:  `"` + hex.EncodeToString(h[:16]) + `"`,
	}}, nil
}

// handlePolitica responde ao GET /privacidade (e ao HEAD). Só existe com a
// política na CloudConfig; sem ela a rota não está na tabela. Como o index.html
// do app: o navegador confere a cada vez (no-cache, ETag, 304), e a cópia nova,
// depois de reiniciar a nuvem, vale na próxima leitura.
func (c *Cloud) handlePolitica(w http.ResponseWriter, r *http.Request) {
	servirArquivo(c.privacidade.pagina, cacheDaRaiz)(w, r)
}
