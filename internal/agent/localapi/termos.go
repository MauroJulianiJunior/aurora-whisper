package localapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Este arquivo é o lado da API local do "Adicionar termo" das Configurações
// (ADR-020): a pessoa digita a palavra certa, opcionalmente as grafias
// erradas que o reconhecimento de voz escreve, e diz se a palavra vira dica
// para o reconhecimento de voz. Depois liga ou desliga a dica, ou remove.
//
// O mesmo desenho de /api/aprendizado: atrás da mesma cadeia (guard.Protect),
// tudo por POST com o texto no corpo (nunca na URL), corpo de no máximo 16
// KiB, a frase de cada erro pronta para a tela, e o log só com contagens e a
// classe do erro: nunca o termo nem a grafia. Diferente do Aprender, não há
// ditado a conferir: o pedido é a própria pessoa digitando, e quem confere é
// o dicionário (o que não agiria, o que quebraria outro alias, o que não cabe
// nas dicas).

type termoDTO struct {
	Termo     string   `json:"termo"`
	Variantes []string `json:"variantes"`
	// Dica: o termo está nas dicas do reconhecimento de voz. Falso no termo
	// do dicionário de antes (do_dicionario), cuja dica não muda pelo painel.
	Dica bool `json:"dica"`
	// DoDicionario: o termo já era do dicionário; a pessoa só pôs grafias.
	DoDicionario bool `json:"do_dicionario"`
}

type dicasDTO struct {
	// Usadas são os caracteres que as dicas da pessoa ocupam no prompt.
	Usadas int `json:"usadas"`
	// Orcamento é o teto do prompt (140).
	Orcamento int `json:"orcamento"`
}

type termosDTO struct {
	Items []termoDTO `json:"items"`
	Dicas dicasDTO   `json:"dicas"`
}

// mudancaDeTermosDTO é a resposta de toda mutação: a lista inteira (a tela só
// troca o estado, sem pedir de novo) e o que a mudança fez.
type mudancaDeTermosDTO struct {
	termosDTO
	// Termo é a forma canônica, como ficou no arquivo.
	Termo string `json:"termo"`
	// JaExistia: no Adicionar, o termo já estava no dicionário.
	JaExistia bool `json:"ja_existia"`
	// DicaIgnorada: no Adicionar, o pedido trazia a dica, e o termo já era do
	// dicionário: as grafias entraram e a dica dele não mudou. Ausente no
	// caso comum.
	DicaIgnorada bool `json:"dica_ignorada,omitempty"`
	// SairamDasDicas são os termos que saíram do prompt com a mudança. Nunca
	// null.
	SairamDasDicas []string `json:"sairam_das_dicas"`
	// ValeAoReiniciar: gravado, mas o dicionário em memória não foi trocado
	// (dictionary.ErrGravadoSemTroca). Ausente no caso comum.
	ValeAoReiniciar bool `json:"vale_ao_reiniciar,omitempty"`
}

type adicionarTermoRequest struct {
	Termo     string   `json:"termo"`
	Variantes []string `json:"variantes"`
	Dica      bool     `json:"dica"`
}

type dicaDoTermoRequest struct {
	Termo string `json:"termo"`
	Dica  bool   `json:"dica"`
}

type removerTermoRequest struct {
	Termo string `json:"termo"`
}

func listaDTO(l dictionary.ListaDeTermos) termosDTO {
	out := termosDTO{Items: []termoDTO{}, Dicas: dicasDTO{Usadas: l.Dicas.Usadas, Orcamento: l.Dicas.Orcamento}}
	for _, it := range l.Itens {
		vs := it.Variantes
		if vs == nil {
			vs = []string{}
		}
		out.Items = append(out.Items, termoDTO{Termo: it.Termo, Variantes: vs, Dica: it.Dica, DoDicionario: it.DoDicionario})
	}
	return out
}

// handleGetTermos atende GET /api/termos.
func (s *Server) handleGetTermos(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.termosLigados(w) {
		return
	}
	l, err := s.pref.Termos.ListarTermos(r.Context())
	if err != nil {
		s.recusarTermo(w, err, acaoListar, "")
		return
	}
	writeJSON(w, http.StatusOK, listaDTO(l))
}

// handleAdicionarTermo atende POST /api/termos.
func (s *Server) handleAdicionarTermo(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.termosLigados(w) {
		return
	}
	var body adicionarTermoRequest
	if !lerCorpo(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Termo) == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "Faltou a palavra certa.")
		return
	}
	m, err := s.pref.Termos.AdicionarTermo(r.Context(), dictionary.PedidoTermo{
		Termo: body.Termo, Variantes: body.Variantes, Dica: body.Dica,
	})
	s.responderMudanca(w, m, err, acaoAdicionar, body.Termo)
}

// handleDicaDoTermo atende POST /api/termos/dica.
func (s *Server) handleDicaDoTermo(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.termosLigados(w) {
		return
	}
	var body dicaDoTermoRequest
	if !lerCorpo(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Termo) == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "Faltou o termo.")
		return
	}
	m, err := s.pref.Termos.DicaDoTermo(r.Context(), body.Termo, body.Dica)
	s.responderMudanca(w, m, err, acaoDica, body.Termo)
}

// handleRemoverTermo atende POST /api/termos/remover. É POST, e não DELETE
// com o termo no caminho: o termo é texto da pessoa, e não vai para a URL.
func (s *Server) handleRemoverTermo(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.termosLigados(w) {
		return
	}
	var body removerTermoRequest
	if !lerCorpo(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Termo) == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "Faltou o termo.")
		return
	}
	m, err := s.pref.Termos.RemoverTermo(r.Context(), body.Termo)
	s.responderMudanca(w, m, err, acaoRemover, body.Termo)
}

func (s *Server) responderMudanca(w http.ResponseWriter, m dictionary.MudancaDeTermos, err error, acao acaoDeTermo, termo string) {
	reinicio := errors.Is(err, dictionary.ErrGravadoSemTroca)
	if err != nil && !reinicio {
		s.recusarTermo(w, err, acao, termo)
		return
	}
	sairam := m.SairamDasDicas
	if sairam == nil {
		sairam = []string{}
	}
	s.log.Info("termos: "+string(acao),
		"novas", len(m.Novas),
		"ja_existia", m.JaExistia,
		"dica_ignorada", m.DicaIgnorada,
		"sairam", len(sairam),
		"termos", len(m.Lista.Itens),
		"vale_ao_reiniciar", reinicio)
	writeJSON(w, http.StatusOK, mudancaDeTermosDTO{
		termosDTO:       listaDTO(m.Lista),
		Termo:           m.Termo,
		JaExistia:       m.JaExistia,
		DicaIgnorada:    m.DicaIgnorada,
		SairamDasDicas:  sairam,
		ValeAoReiniciar: reinicio,
	})
}

// termosLigados responde 503 quando o Agent não montou o "Adicionar termo".
func (s *Server) termosLigados(w http.ResponseWriter) bool {
	if s.pref.Termos != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, "Adicionar termo não está disponível nesta versão.")
	return false
}

// acaoDeTermo é o que a pessoa pediu: muda a frase do erro de "quebraria" e o
// nome da linha do log.
type acaoDeTermo string

const (
	acaoListar    acaoDeTermo = "listado"
	acaoAdicionar acaoDeTermo = "adicionado"
	acaoDica      acaoDeTermo = "dica"
	acaoRemover   acaoDeTermo = "removido"
)

// recusarTermo traduz o erro do dicionário para a resposta da tela e registra
// só a CLASSE do erro. termo é o do pedido: a frase o cita, e só a resposta o
// carrega.
func (s *Server) recusarTermo(w http.ResponseWriter, err error, acao acaoDeTermo, termo string) {
	status, code, msg := traduzirErroDeTermo(err, acao, strings.Join(strings.Fields(termo), " "))
	s.log.Info("termos: recusado", "acao", string(acao), "motivo", classeDoErroDeTermo(err), "status", status)
	writeError(w, status, code, msg)
}

func traduzirErroDeTermo(err error, acao acaoDeTermo, termo string) (int, string, string) {
	var (
		daVariante   *dictionary.ErroDaVariante
		comum        *dictionary.ErroPalavraComum
		semEspaco    *dictionary.ErroSemEspaco
		quebraria    *dictionary.ErroQuebraria
		naoAgiria    *dictionary.ErroNaoAgiria
		limite       *dictionary.ErroLimite
		outro        *dictionary.ErroVarianteDeOutroTermo
		termoDeOutro *dictionary.ErroTermoEVarianteDeOutro
		inativo      *dictionary.ErroTermoInativoDe
	)
	variante := ""
	if errors.As(err, &daVariante) {
		variante = daVariante.Variante
	}
	switch {
	case errors.As(err, &comum):
		switch comum.Motivo {
		case "numero":
			return http.StatusBadRequest, codeBadRequest,
				fmt.Sprintf("“%s” é só número: trocaria texto certo.", comum.Texto)
		case "muda_sentido":
			return http.StatusBadRequest, codeBadRequest,
				fmt.Sprintf("“%s” tem palavra que muda o sentido da frase, e ela sumiria do texto.", comum.Texto)
		}
		return http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf("“%s” é palavra comum demais: trocaria texto certo.", comum.Texto)
	case errors.As(err, &semEspaco):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf("Não cabe nas dicas: %s.", sobram(semEspaco.Sobram))
	case errors.As(err, &quebraria):
		return http.StatusConflict, codeConflict, fraseDoQuebraria(quebraria, acao, termo)
	case errors.As(err, &naoAgiria):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf("O dicionário não aplicaria “%s” num ditado. Nada foi gravado.", naoAgiria.Variante)
	case errors.As(err, &limite):
		switch limite.Do {
		case dictionary.LimiteTermos:
			return http.StatusBadRequest, codeBadRequest,
				fmt.Sprintf("Já há %d termos adicionados. Remova um antes.", limite.Maximo)
		case dictionary.LimiteVariantesDoTermo:
			return http.StatusBadRequest, codeBadRequest,
				fmt.Sprintf("Cada termo leva até %d grafias erradas.", limite.Maximo)
		default:
			return http.StatusBadRequest, codeBadRequest,
				fmt.Sprintf("Dá para mandar até %d grafias erradas por vez.", limite.Maximo)
		}
	case errors.As(err, &outro):
		if outro.ETermo {
			return http.StatusConflict, codeConflict, fmt.Sprintf("“%s” já é um termo do dicionário.", variante)
		}
		return http.StatusConflict, codeConflict, fmt.Sprintf("“%s” já vira “%s” no dicionário.", variante, outro.Dono)
	case errors.As(err, &termoDeOutro):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf("“%s” já vira “%s” no dicionário e não pode virar termo.", termo, termoDeOutro.Dono)
	case errors.As(err, &inativo):
		return http.StatusConflict, codeConflict, fmt.Sprintf("“%s” está desligado no dicionário.", inativo.Termo)
	case errors.Is(err, dictionary.ErrTermoInativo):
		return http.StatusConflict, codeConflict, fmt.Sprintf("“%s” está desligado no dicionário.", termo)
	case variante != "" && errors.Is(err, dictionary.ErrJaExiste):
		dono := daVariante.Termo
		if dono == "" {
			dono = termo
		}
		return http.StatusConflict, codeConflict, fmt.Sprintf("“%s” já vira “%s” no dicionário.", variante, dono)
	case variante != "" && errors.Is(err, dictionary.ErrSemNovidade):
		return http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf("“%s” só muda maiúscula, acento ou espaço, e o dicionário já entende isso.", variante)
	case variante != "" && (errors.Is(err, dictionary.ErrInvalido) || errors.Is(err, dictionary.ErrVazio)):
		return http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf("“%s” não serve: cada grafia tem de 2 a %d caracteres, até %d palavras, numa linha só.",
				variante, dictionary.MaxRunasTermo, dictionary.MaxPalavrasVariante)
	case errors.Is(err, dictionary.ErrVazio):
		return http.StatusBadRequest, codeBadRequest, "Faltou a palavra certa."
	case errors.Is(err, dictionary.ErrInvalido):
		return http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf("A palavra certa vai até %d caracteres e %d palavras, numa linha só.", dictionary.MaxRunasTermo, dictionary.MaxPalavrasVariante)
	case errors.Is(err, dictionary.ErrDicaDoDicionario):
		return http.StatusConflict, codeConflict, fmt.Sprintf("“%s” já é do dicionário: a dica dele não muda por aqui.", termo)
	case errors.Is(err, dictionary.ErrNadaAMudar):
		return http.StatusBadRequest, codeBadRequest, fmt.Sprintf("“%s” já está no dicionário assim.", termo)
	case errors.Is(err, dictionary.ErrNaoAdicionado):
		return http.StatusNotFound, codeNotFound, fmt.Sprintf("“%s” não foi adicionado por você.", termo)
	case errors.Is(err, dictionary.ErrSemArquivo):
		return http.StatusServiceUnavailable, codeUnavailable, "Não achei o dicionário do Agent, então não há onde adicionar."
	case errors.Is(err, dictionary.ErrArquivoInvalido):
		return http.StatusServiceUnavailable, codeUnavailable, "O dicionário do Agent não abre. Nada foi gravado."
	case errors.Is(err, dictionary.ErrUsuarioDiferente):
		return http.StatusServiceUnavailable, codeUnavailable, "O dicionário mudou de dono. Reinicie o Agent."
	case errors.Is(err, dictionary.ErrNaoCarrega):
		return http.StatusInternalServerError, codeInternal, "O dicionário com essa mudança não carregaria. Nada foi gravado."
	default:
		return http.StatusInternalServerError, codeInternal, fraseDeDefeito
	}
}

// fraseDoQuebraria diz o que deixaria de agir:
//
//   - a forma canônica de outro termo, escrita certa (Variante igual a
//     Termo), que passaria a virar o termo mudado;
//   - a grafia do PRÓPRIO termo do pedido, que só age com a dica ligada (a
//     dica é a prioridade, e sem ela outro termo leva a busca): a saída, hoje,
//     é remover o termo e adicioná-lo de novo sem essa grafia;
//   - a variante de outro termo, que deixaria de virar ele.
func fraseDoQuebraria(q *dictionary.ErroQuebraria, acao acaoDeTermo, termo string) string {
	switch {
	case igualNaBusca(q.Variante, q.Termo):
		if q.Vira == "" {
			return fmt.Sprintf("%s, “%s”, escrito certo, passaria a ser trocado. Nada foi gravado.", comOTermo(acao, termo), q.Termo)
		}
		return fmt.Sprintf("%s, “%s”, escrito certo, passaria a virar “%s”. Nada foi gravado.", comOTermo(acao, termo), q.Termo, q.Vira)
	case acao == acaoDica && igualNaBusca(q.Termo, termo):
		return fmt.Sprintf("“%s” só vira “%s” com a dica ligada. Para desligá-la, remova “%s” e adicione de novo sem essa grafia. Nada foi gravado.",
			q.Variante, q.Termo, q.Termo)
	case acao == acaoAdicionar && igualNaBusca(q.Termo, termo):
		return fmt.Sprintf("“%s” só vira “%s” com a dica ligada: deixe a dica marcada, ou remova “%s” e adicione de novo sem essa grafia. Nada foi gravado.",
			q.Variante, q.Termo, q.Termo)
	}
	return fmt.Sprintf("%s, “%s” deixaria de virar “%s”. Nada foi gravado.", comOTermo(acao, termo), q.Variante, q.Termo)
}

// igualNaBusca compara como a busca do dicionário compara: sem caixa, acento
// nem pontuação, e sem espaços.
func igualNaBusca(a, b string) bool {
	return dictionary.CollapseSpaces(dictionary.Normalize(a)) == dictionary.CollapseSpaces(dictionary.Normalize(b))
}

// comOTermo abre a frase do "quebraria" conforme o que a pessoa pediu.
func comOTermo(acao acaoDeTermo, termo string) string {
	switch acao {
	case acaoDica:
		return fmt.Sprintf("Mudando a dica de “%s”", termo)
	case acaoRemover:
		return fmt.Sprintf("Sem “%s”", termo)
	default:
		return fmt.Sprintf("Com “%s”", termo)
	}
}

func sobram(n int) string {
	switch n {
	case 0:
		return "não sobra nenhum caractere"
	case 1:
		return "sobra 1 caractere"
	default:
		return fmt.Sprintf("sobram %d caracteres", n)
	}
}

// classeDoErroDeTermo é o nome de um erro do "Adicionar termo", de um
// conjunto fechado: é o que vai ao log, no lugar do texto do erro.
func classeDoErroDeTermo(err error) string {
	switch {
	case errors.Is(err, dictionary.ErrPalavraComum):
		return "palavra_comum"
	case errors.Is(err, dictionary.ErrSemEspaco):
		return "sem_espaco"
	case errors.Is(err, dictionary.ErrQuebraria):
		return "quebraria"
	case errors.Is(err, dictionary.ErrLimite):
		return "limite"
	case errors.Is(err, dictionary.ErrNadaAMudar):
		return "nada_a_mudar"
	case errors.Is(err, dictionary.ErrDicaDoDicionario):
		return "dica_do_dicionario"
	case errors.Is(err, dictionary.ErrNaoAdicionado):
		return "nao_adicionado"
	default:
		return classeDoErro(err)
	}
}
