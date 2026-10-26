package localapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// POST /api/notas/separar-itens (ADR-022, "Formatar: converter trecho"; fase
// N5; docs/ai-spec/06-api-contract.md): converte UMA linha de texto corrido de
// uma nota em itens de lista. Com o Polish configurado (modo "polish",
// provedor e modelo escolhidos), vai à IA da pessoa, com a chave dela, pelo
// prompt notas-lista@1 e as guardas de resolver.SepararItens; sem isso, ou
// quando a IA não vale, pela regra. Não grava nada: quem troca a linha pelos
// itens é o painel, que guarda a linha de antes para o Desfazer.
//
//	{"trecho": "leite pão café e duas dúzias de ovos"}
//	-> 200 {"itens": ["Leite", "Pão", "Café", "Duas dúzias de ovos"], "como": "ia"}
//	   como: "ia" | "regra" | "nenhum" (nenhum: itens vazio; o texto fica)
//
// O trecho não vai a log nenhum: o log leva como, o motivo (o enum) e a
// versão do prompt.

// ClienteDaLista devolve o cliente do provedor de IA para o provedor e o
// modelo dados (o mesmo do Polish, com a chave lida a cada chamada), ou nil
// quando não há. Em produção, o polish.Montagem.ClienteDaLista do Agent.
type ClienteDaLista func(provedor, modelo string) resolver.PolishClient

type separarItensDTO struct {
	Trecho string `json:"trecho"`
}

type itensSeparadosDTO struct {
	Itens []string `json:"itens"`
	Como  string   `json:"como"`
}

// handleSepararItens atende POST /api/notas/separar-itens.
func (s *Server) handleSepararItens(w http.ResponseWriter, r *http.Request) {
	var body separarItensDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limiteDoCorpo)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	trecho := strings.TrimSpace(body.Trecho)
	switch {
	case trecho == "":
		writeError(w, http.StatusBadRequest, codeBadRequest, "Escolha uma linha com texto para separar em itens.")
		return
	case strings.ContainsAny(trecho, "\r\n"):
		writeError(w, http.StatusBadRequest, codeBadRequest, "Separo em itens uma linha por vez.")
		return
	case utf8.RuneCountInString(trecho) > resolver.NotasListaMaxCaracteres:
		writeError(w, http.StatusBadRequest, codeBadRequest, "Esta linha é longa demais para separar em itens.")
		return
	}

	cliente, err := s.clienteDaLista(r.Context())
	if err != nil {
		// Sem as preferências, não se sabe se a pessoa ligou a IA: fica na
		// regra, que não manda nada para fora.
		s.log.Warn("localapi: separar em itens sem ler as preferências; fica a regra", "err", err)
		cliente = nil
	}
	// O mesmo prazo do Polish para o mesmo tamanho de texto (3,5 s a 8 s).
	ctx, cancel := context.WithTimeout(r.Context(), polish.Prazo(trecho))
	defer cancel()
	res := resolver.SepararItens(ctx, cliente, trecho)

	if res.Motivo != "" && res.Motivo != resolver.MotivoDaLista(resolver.MotivoSemCliente) {
		s.log.Info("notas: separar em itens sem a IA", "como", string(res.Como), "motivo", string(res.Motivo),
			"prompt", resolver.NotasListaPromptRef, "hash", resolver.NotasListaPromptHash())
	}
	itens := res.Itens
	if itens == nil {
		itens = []string{} // "[]" no JSON, nunca "null"
	}
	writeJSON(w, http.StatusOK, itensSeparadosDTO{Itens: itens, Como: string(res.Como)})
}

// clienteDaLista devolve o cliente da IA quando a pessoa a ligou: modo de
// texto "polish", provedor válido e modelo escolhido (as mesmas condições em
// que o ditado vai ao Polish). Fora disso, nil, e a separação é pela regra.
func (s *Server) clienteDaLista(ctx context.Context) (resolver.PolishClient, error) {
	if s.pref.ClienteDaLista == nil {
		return nil, nil
	}
	cur, err := s.atuais(ctx)
	if err != nil {
		return nil, err
	}
	if cur.TextMode != "polish" || !ia.Valido(cur.AIProvider) || strings.TrimSpace(cur.AIModel) == "" {
		return nil, nil
	}
	return s.pref.ClienteDaLista(cur.AIProvider, cur.AIModel), nil
}
