package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// Este arquivo é o lado da API local do aprendizado pelo painel (etapa 3,
// fatia F3; ADR-017). Duas coisas separadas, de propósito:
//
//   - SUGERIR acontece ao salvar uma edição (PATCH /api/history/{id}). Só
//     devolve o que o motor (learning.Sugerir) achou: NUNCA grava no
//     dicionário (AC-022). Uma sugestão ignorada não deixa rastro, a não ser
//     na memória deste processo (abaixo).
//   - APRENDER, LISTAR e DESFAZER são /api/aprendizado. Aprender é o clique
//     de quem usa, e só ele grava.
//
// # Só o clique grava, e o servidor desconfia do clique
//
// O servidor não aceita qualquer par "de"/"para". Antes de chamar o
// Aprendizado ele refaz as checagens, sem confiar no que a página diz:
//
//  1. o ditado existe e é do dono da instalação;
//  2. o "de" está no texto BRUTO daquele ditado (decisão 6 da ADR-017: o
//     alias é a forma que o ASR escreveu);
//  3. o par foi uma sugestão que ESTE processo devolveu, para esta VERSÃO
//     do ditado. O texto de antes da edição não é guardado (a ADR-017
//     descartou a coluna nova), então o motor não pode rodar de novo no
//     clique; o que o servidor guarda, em memória, são os pares curtos que
//     o motor produziu. Quem salva de novo invalida a sugestão anterior (a
//     versão muda), e quem inventa um par não passa;
//  4. o que o dicionário recusa (variante de outro termo, termo inativo,
//     variante que não agiria...) o dicionário diz, e vira a frase da tela.
//
// # Nada de texto em log
//
// O que se registra são contagens e códigos de um conjunto fechado (os
// Motivo do motor, a classe do erro). O par, o ditado e o dono da variante
// nunca vão a log (CLAUDE.md, "Safety rules"): a mensagem de erro que vai à
// TELA cita o termo e o dono, porque é isso que a tela precisa mostrar, mas
// só a resposta HTTP os carrega.

// Aprendizado é o que a API do painel precisa do dicionário do Agent:
// gravar, desfazer e listar (dictionary.Aprendizado) e ler o dicionário de
// agora, para o motor de sugestão saber o que já existe.
//
// Seguro de qualquer goroutine. Em produção é o aprendizadoDoAgent, de
// cmd/agent.
type Aprendizado interface {
	dictionary.Aprendizado
	// Dicionario devolve o dicionário como está em disco agora. Erro
	// (dictionary.ErrSemArquivo e os outros do arquivo) = não há o que
	// ensinar, e a edição sai sem sugestão.
	Dicionario(ctx context.Context) (dictionary.Semente, error)
}

const (
	// ocorrenciasNosUltimos é quantos ditados ativos, dos mais novos, o
	// "aparece em N ditados" olha (sem contar o que acabou de ser editado).
	// Lixeira não entra: é o que a pessoa descartou, e o que ela vê no
	// histórico é o alcance que ela mede.
	ocorrenciasNosUltimos = 500
	// maxCorpoAprendizado é o teto do corpo de POST /api/aprendizado*: um
	// id, uma variante e um termo curtos.
	maxCorpoAprendizado = 16 << 10
	// maxDitadosLembrados é de quantos ditados o servidor guarda as
	// sugestões, em memória. Só o par curto, nunca o texto do ditado.
	maxDitadosLembrados = 32
)

// sugestaoDTO é uma troca que a pessoa pode mandar aprender.
type sugestaoDTO struct {
	// De é a forma que o ASR escreveu (lida do raw_text): é o alias a gravar.
	De string `json:"de"`
	// Para é a forma final: a canônica do dicionário se o termo já existe,
	// e senão como a pessoa escreveu, na caixa de meio de frase.
	Para string `json:"para"`
	// TermoExistente é o termo do dicionário que já é Para, ou "" quando
	// aprender cria o termo.
	TermoExistente string `json:"termo_existente"`
	// Ocorrencias é em quantos dos últimos ditados ativos (sem este) o De
	// aparece no texto bruto. 0 = em nenhum, ou não deu para contar.
	Ocorrencias int `json:"ocorrencias"`
}

// editadoDTO é a resposta de PATCH /api/history/{id}: o item atualizado e
// as sugestões de aprendizado, de 0 a 2. Nunca nil: a lista vem vazia
// quando não há o que sugerir.
type editadoDTO struct {
	transcriptionDTO
	Sugestoes []sugestaoDTO `json:"sugestoes"`
}

// par é o "de" e o "para" de uma sugestão.
type par struct{ de, para string }

// lembradas é a memória das sugestões devolvidas, por ditado e versão.
// Só o que o motor produziu; some ao reiniciar o Agent, e com ele a página
// que tinha o botão.
type lembradas struct {
	mu    sync.Mutex
	itens []lembrada // do mais antigo ao mais novo
}

type lembrada struct {
	id     string
	versao int64
	pares  []par
}

// lembrar guarda as sugestões do ditado id na versão dada, no lugar das
// anteriores. Sem pares, só esquece.
func (l *lembradas) lembrar(id string, versao int64, pares []par) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.esquecerSemTrava(id)
	if len(pares) == 0 {
		return
	}
	if len(l.itens) >= maxDitadosLembrados {
		l.itens = l.itens[1:]
	}
	l.itens = append(l.itens, lembrada{id: id, versao: versao, pares: pares})
}

func (l *lembradas) esquecerSemTrava(id string) {
	for i, it := range l.itens {
		if it.id == id {
			l.itens = append(l.itens[:i:i], l.itens[i+1:]...)
			return
		}
	}
}

// foiSugerida diz se p foi devolvida, para este ditado e esta versão dele.
func (l *lembradas) foiSugerida(id string, versao int64, p par) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range l.itens {
		if it.id != id || it.versao != versao {
			continue
		}
		for _, q := range it.pares {
			if q == p {
				return true
			}
		}
	}
	return false
}

// sugerirDaEdicao roda o motor sobre a edição que acabou de ser gravada e
// lembra o que ele devolveu. antes é o ditado como estava ANTES do PATCH
// (lido antes de gravar); depois, o que o repositório devolveu ao gravar.
//
// Nunca falha a edição: sem aprendizado, sem dicionário, ou com qualquer
// erro, a lista sai vazia e a edição já gravada segue valendo. Não grava
// nada no dicionário.
func (s *Server) sugerirDaEdicao(ctx context.Context, antes, depois storage.Transcription) []sugestaoDTO {
	saida := []sugestaoDTO{}
	ap := s.pref.Aprendizado
	if ap == nil {
		return saida
	}
	// O texto de antes só é o que esta edição trocou se mais ninguém gravou
	// entre a leitura e a gravação. A versão sobe de um em um.
	if depois.Version != antes.Version+1 {
		s.log.Info("aprendizado: edição sem sugestão", "motivo", "versao_mudou_no_meio")
		return saida
	}

	dic, err := ap.Dicionario(ctx)
	if err != nil {
		s.log.Info("aprendizado: edição sem sugestão", "motivo", classeDoErro(err))
		return saida
	}
	res := learning.Sugerir(depois.RawText, antes.FinalText, depois.FinalText, dic,
		s.contadorDeOcorrencias(ctx, depois.ID))

	pares := make([]par, 0, len(res.Sugestoes))
	for _, sug := range res.Sugestoes {
		saida = append(saida, sugestaoDTO{
			De: sug.De, Para: sug.Para,
			TermoExistente: sug.TermoExistente,
			Ocorrencias:    sug.OcorrenciasNoHistorico,
		})
		pares = append(pares, par{de: sug.De, para: sug.Para})
	}
	// Substitui as do salvamento anterior, que são de outra versão. Se a
	// edição sai sem sugestão por um dos retornos de cima, as antigas ficam
	// até a fila as soltar, e não valem: foiSugerida confere a versão.
	s.sugeridas.lembrar(depois.ID, depois.Version, pares)

	descartes := make([]string, len(res.Descartes))
	for i, m := range res.Descartes {
		descartes[i] = string(m)
	}
	s.log.Info("aprendizado: edição avaliada",
		"sugestoes", len(res.Sugestoes),
		"descartes", len(res.Descartes),
		"motivos_do_descarte", strings.Join(descartes, ","))
	return saida
}

// contadorDeOcorrencias devolve o ContadorOcorrencias do motor: em quantos
// dos últimos ocorrenciasNosUltimos ditados ativos, fora o editado, o "de"
// aparece no texto bruto. O histórico só é lido se o motor chegar a
// perguntar (uma edição sem sugestão não custa a leitura), e uma vez só.
func (s *Server) contadorDeOcorrencias(ctx context.Context, editado string) learning.ContadorOcorrencias {
	var (
		brutos []string
		lido   bool
	)
	return func(de string) int {
		if !lido {
			lido = true
			brutos = s.brutosDosOutros(ctx, editado)
		}
		return learning.OcorrenciasEm(de, brutos)
	}
}

// brutosDosOutros lê o texto bruto dos últimos ditados ativos do dono, sem
// o editado. Um erro de leitura devolve o que já veio: a contagem é um
// aviso, e nunca faz a edição falhar.
func (s *Server) brutosDosOutros(ctx context.Context, editado string) []string {
	var brutos []string
	filtro := storage.TranscriptionListFilter{UserID: s.userID, Limit: storage.MaxTranscriptionListLimit}
	for len(brutos) < ocorrenciasNosUltimos {
		pagina, err := s.transcriptions.List(ctx, filtro)
		if err != nil {
			s.log.Warn("aprendizado: não consegui ler o histórico para contar", "motivo", "erro_de_leitura")
			break
		}
		for _, t := range pagina.Items {
			if t.ID != editado && len(brutos) < ocorrenciasNosUltimos {
				brutos = append(brutos, t.RawText)
			}
		}
		if pagina.NextCursor == "" {
			break
		}
		filtro.Cursor = pagina.NextCursor
	}
	return brutos
}

// aprendidaDTO é uma variante que o painel ensinou: Variante vira Termo.
type aprendidaDTO struct {
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
	// Situacao e Motivo são as linhas de estado do espelho do sync (ADR-019,
	// "Decisões da F8"), só em GET /api/aprendizado, com os nomes do fio que a
	// web lê (06; situacaoNoFio): ausentes no par que vale aqui (a linha do
	// arquivo, com o Desfazer), sempre. Situacao é "nao_aplicado" (o par que
	// chegou da nuvem e não vale aqui; o Motivo é um dictionary.MotivoDoPar),
	// "desfeito_em_outro_aparelho" (a troca deste PC que perdeu na nuvem
	// para um desfazer, e saiu do arquivo) ou "trocado_em_outro_aparelho" (a
	// troca deste PC que perdeu para outro termo; a que vale aqui é a da
	// nuvem, na linha do arquivo). Nenhuma linha de estado está no
	// dicionário daqui, e nenhuma tem Desfazer. As etiquetas somem quando a
	// pessoa aprende de novo, ou em 30 dias.
	Situacao string `json:"situacao,omitempty"`
	Motivo   string `json:"motivo,omitempty"`
	// ValeAoReiniciar é verdadeiro só na resposta de aprender/desfazer quando
	// o arquivo foi gravado mas o dicionário em memória não pôde ser trocado
	// (dictionary.ErrGravadoSemTroca): a mudança é real, e só vale depois de
	// reiniciar o Agent. Ausente no caso comum.
	ValeAoReiniciar bool `json:"vale_ao_reiniciar,omitempty"`
}

type aprendidasDTO struct {
	Items []aprendidaDTO `json:"items"`
}

type aprenderRequest struct {
	// ID é o ditado de onde a sugestão saiu.
	ID string `json:"id"`
	// De e Para são os da sugestão, como a resposta do PATCH os devolveu.
	De   string `json:"de"`
	Para string `json:"para"`
}

type desfazerRequest struct {
	Variante string `json:"variante"`
}

// EspelhoDoAprendido é o que GET /api/aprendizado lê do espelho do sync
// (storage.Fila.Espelho): o estado de cada id do aprendido que o PC conhece.
type EspelhoDoAprendido interface {
	Espelho(ctx context.Context) ([]storage.EstadoDoAprendido, error)
}

// handleGetAprendizado atende GET /api/aprendizado: o que o painel
// aprendeu, na ordem do arquivo, e depois as linhas de estado do espelho do
// sync (comEstados). Sem dicionário, ou sem Aprendizado, a lista do arquivo é
// vazia: a tela só mostra a seção quando há algo; o arquivo ausente
// (dictionary.ErrSemArquivo) também, e as linhas de estado saem (o par da
// nuvem "não aplicado" pelo arquivo_indisponivel, inclusive). O arquivo que
// existe e não se lê (inválido, de outro usuário) é um 503, e as linhas de
// estado não saem: a lista vazia diria que não há nada aprendido, e a tela
// esconderia a seção com o que a pessoa precisa desfazer (a revisão do
// ADR-017; TestListaComErroNaoViraListaVazia). O espelho que não se lê não
// derruba a lista: ela sai só com o arquivo, e o log diz por quê, sem texto.
func (s *Server) handleGetAprendizado(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	out := aprendidasDTO{Items: []aprendidaDTO{}}
	if s.pref.Aprendizado == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	lista, err := s.pref.Aprendizado.Aprendidas(r.Context())
	if err != nil {
		s.recusarAprendizado(w, err, "", "")
		return
	}
	var es []storage.EstadoDoAprendido
	if s.pref.Espelho != nil {
		if es, err = s.pref.Espelho.Espelho(r.Context()); err != nil {
			s.log.Warn("aprendizado: não consegui ler o espelho do sync; a lista sai só com o arquivo", "motivo", "erro_de_leitura")
			es = nil
		}
	}
	out.Items = comEstados(lista, es, time.Now())
	writeJSON(w, http.StatusOK, out)
}

// comEstados é a lista "Aprendido no painel" (ADR-019, "Decisões da F8",
// "O que o celular e o PC mostram"; 06, GET /api/aprendizado): uma linha por
// variante do arquivo, na ordem dele, como antes da F8 e sempre sem
// situacao (é o par que vale aqui, e tem o Desfazer); e, depois, as linhas
// de estado do espelho (linhaDeEstado), da mais recente para a mais antiga.
// A mesma variante pode ter as duas: no "trocado em outro aparelho", a do
// arquivo é a troca da nuvem, e a de estado, a que este PC tinha aprendido.
func comEstados(lista []dictionary.Aprendida, es []storage.EstadoDoAprendido, agora time.Time) []aprendidaDTO {
	out := make([]aprendidaDTO, 0, len(lista))
	noArquivo := map[string]bool{}
	for _, a := range lista {
		noArquivo[dictionary.IDDoPar(a.Variante)] = true
		out = append(out, aprendidaDTO{Termo: a.Termo, Variante: a.Variante})
	}

	type comLinha struct {
		e    storage.EstadoDoAprendido
		item aprendidaDTO
	}
	var estados []comLinha
	for _, e := range es {
		if item, ok := linhaDeEstado(e, noArquivo[e.ID], agora); ok {
			estados = append(estados, comLinha{e, item})
		}
	}
	sort.SliceStable(estados, func(i, j int) bool {
		a, b := estados[i].e.SituacaoEm, estados[j].e.SituacaoEm
		if (a == nil) != (b == nil) {
			return a != nil
		}
		if a != nil && !a.Equal(*b) {
			return a.After(*b)
		}
		return estados[i].e.ID < estados[j].e.ID
	})
	for _, c := range estados {
		out = append(out, c.item)
	}
	return out
}

// linhaDeEstado é a linha de estado de um id do espelho, quando ele tem uma
// (ok falso: nenhuma), com a situacao no nome do fio (situacaoNoFio). Nenhuma
// está no dicionário daqui, e nenhuma tem Desfazer:
//
//   - "não aplicado neste PC": o par da nuvem guardado, que não vale aqui,
//     com o motivo. O id que está no arquivo não ganha a linha (o par que
//     vale aqui é o do arquivo);
//   - "desfeito em outro aparelho": a troca deste PC que perdeu para um
//     desfazer (o par perdido; numa linha sem ele, a da nuvem), fora do
//     arquivo. No arquivo, a varredura seguinte a resolve (o desfazer da
//     nuvem passa a valer aqui), e a linha espera;
//   - "trocado em outro aparelho": a troca deste PC que perdeu para outro
//     termo (o par perdido), esteja o id no arquivo ou não: a troca que vale
//     aqui é a da nuvem, na linha do arquivo. Sem o par perdido (uma linha de
//     antes da 0008), não há o que mostrar.
//
// O par sem texto (o esquecer o apaga, e fica o id) não aparece, nem a
// etiqueta que passou do prazo (a varredura a tira quando roda; com o PC
// desconectado, ela não roda). As outras situações do espelho (aplicado,
// aplicando, desfeito aqui, já vale aqui, recusado) nunca viram linha.
func linhaDeEstado(e storage.EstadoDoAprendido, noArquivo bool, agora time.Time) (aprendidaDTO, bool) {
	nome, ok := situacaoNoFio(e.Situacao)
	if !ok {
		return aprendidaDTO{}, false
	}
	daNuvem := func() (aprendidaDTO, bool) {
		if e.Nuvem == nil || e.Nuvem.Par.Variante == "" {
			return aprendidaDTO{}, false
		}
		return aprendidaDTO{Termo: e.Nuvem.Par.Termo, Variante: e.Nuvem.Par.Variante, Situacao: nome}, true
	}
	if e.Situacao == storage.EspelhoNaoAplicado {
		if noArquivo {
			return aprendidaDTO{}, false
		}
		item, ok := daNuvem()
		item.Motivo = e.Motivo
		return item, ok
	}
	if etiquetaVencida(e, agora) || e.Situacao == storage.EspelhoDesfeitoEmOutro && noArquivo {
		return aprendidaDTO{}, false
	}
	if p := e.Perdido; p != nil && p.Variante != "" {
		return aprendidaDTO{Termo: p.Termo, Variante: p.Variante, Situacao: nome}, true
	}
	if e.Situacao == storage.EspelhoDesfeitoEmOutro {
		return daNuvem()
	}
	return aprendidaDTO{}, false
}

// situacaoNoFio é o nome da situação do espelho no fio (06, "A tradução do
// espelho para o fio"): os nomes do espelho (storage.SituacaoDoEspelho) não
// são os que a web lê, e uma situacao que ela não conhece faz a linha sumir
// sem erro. Só as três de estado saem; as outras cinco (aplicado, aplicando,
// desfeito aqui, já vale aqui, recusado), nunca: ok falso.
func situacaoNoFio(s storage.SituacaoDoEspelho) (string, bool) {
	switch s {
	case storage.EspelhoNaoAplicado:
		return "nao_aplicado", true
	case storage.EspelhoDesfeitoEmOutro:
		return "desfeito_em_outro_aparelho", true
	case storage.EspelhoTrocadoEmOutro:
		return "trocado_em_outro_aparelho", true
	}
	return "", false
}

// etiquetaVencida diz se a etiqueta de e passou do prazo
// (syncengine.PrazoDaEtiqueta, contado de quando a situação começou).
func etiquetaVencida(e storage.EstadoDoAprendido, agora time.Time) bool {
	return e.SituacaoEm != nil && !agora.Before(e.SituacaoEm.Add(syncengine.PrazoDaEtiqueta))
}

// handleAprender atende POST /api/aprendizado: o clique em Aprender.
func (s *Server) handleAprender(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.aprendizadoLigado(w) {
		return
	}
	var body aprenderRequest
	if !lerCorpo(w, r, &body) {
		return
	}
	body.ID, body.De, body.Para = strings.TrimSpace(body.ID), strings.TrimSpace(body.De), strings.TrimSpace(body.Para)
	if body.ID == "" || body.De == "" || body.Para == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, `Faltou o ditado, o "de" ou o "para".`)
		return
	}

	t, err := s.transcriptions.Get(r.Context(), s.userID, body.ID)
	if err != nil {
		s.recusarLeituraDoDitado(w, err)
		return
	}
	// 2. O "de" é a forma que o ASR escreveu NAQUELE ditado.
	if learning.OcorrenciasEm(body.De, []string{t.RawText}) == 0 {
		s.log.Info("aprendizado: recusado", "motivo", "de_fora_do_bruto")
		writeError(w, http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf(`"%s" não está no que o reconhecimento de voz escreveu nesse ditado.`, body.De))
		return
	}
	// 3. O par saiu do motor, para esta versão do ditado.
	if !s.sugeridas.foiSugerida(t.ID, t.Version, par{de: body.De, para: body.Para}) {
		s.log.Info("aprendizado: recusado", "motivo", "nao_foi_sugerida")
		writeError(w, http.StatusConflict, codeConflict, fraseDaSugestaoVencida)
		return
	}

	// 4. O que o dicionário decide.
	feito, err := s.pref.Aprendizado.Aprender(r.Context(), body.Para, body.De)
	reinicio := errors.Is(err, dictionary.ErrGravadoSemTroca)
	if err != nil && !reinicio {
		s.recusarAprendizado(w, err, body.De, body.Para)
		return
	}
	s.log.Info("aprendizado: aprendido", "vale_ao_reiniciar", reinicio)
	writeJSON(w, http.StatusOK, aprendidaDTO{Termo: feito.Termo, Variante: feito.Variante, ValeAoReiniciar: reinicio})
}

// fraseDaSugestaoVencida é a do 409 de "o par não foi sugerido para esta
// versão do ditado". A sugestão vence por mais de um motivo: o ditado mudou por
// outro caminho (outra aba, a lixeira e a volta), o Agent reiniciou (as
// sugestões vivem só em memória) ou o 33º ditado soltou o mais antigo. A frase
// não afirma a causa, que o Agent não sabe; diz o que funciona. Salvar de novo
// o mesmo texto não devolve a sugestão (antes e depois ficam iguais, e o motor
// não vê troca): o que devolve é corrigir o ditado de novo.
const fraseDaSugestaoVencida = "Essa sugestão não vale mais; corrija o ditado de novo para aprender."

// fraseDeDefeito é a de todo erro que não é do pedido nem do dicionário: sem
// detalhe (o texto do erro pode citar termo ou caminho) e sem prometer o que
// não se sabe (não diz "nada foi gravado").
const fraseDeDefeito = "Deu um erro dentro do Agent. Tente de novo."

// recusarLeituraDoDitado responde quando o ditado do clique não pôde ser lido.
// A frase vai para a pílula depois de "Não deu para aprender.", e por isso é
// uma frase inteira, e não o "registro não encontrado" das outras rotas.
func (s *Server) recusarLeituraDoDitado(w http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		s.log.Info("aprendizado: recusado", "motivo", "ditado_nao_achado", "status", http.StatusNotFound)
		writeError(w, http.StatusNotFound, codeNotFound, "Esse ditado não existe mais.")
		return
	}
	s.log.Info("aprendizado: recusado", "motivo", "erro_de_leitura", "status", http.StatusInternalServerError)
	writeError(w, http.StatusInternalServerError, codeInternal, fraseDeDefeito)
}

// handleDesfazer atende POST /api/aprendizado/desfazer. É POST, e não
// DELETE com o texto no caminho: a variante é texto ditado, e não vai para
// a URL.
func (s *Server) handleDesfazer(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.aprendizadoLigado(w) {
		return
	}
	var body desfazerRequest
	if !lerCorpo(w, r, &body) {
		return
	}
	body.Variante = strings.TrimSpace(body.Variante)
	if body.Variante == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, `Faltou a variante a desfazer.`)
		return
	}
	feito, err := s.pref.Aprendizado.Desfazer(r.Context(), body.Variante)
	reinicio := errors.Is(err, dictionary.ErrGravadoSemTroca)
	if err != nil && !reinicio {
		s.recusarAprendizado(w, err, body.Variante, "")
		return
	}
	s.log.Info("aprendizado: desfeito", "vale_ao_reiniciar", reinicio)
	writeJSON(w, http.StatusOK, aprendidaDTO{Termo: feito.Termo, Variante: feito.Variante, ValeAoReiniciar: reinicio})
}

// aprendizadoLigado responde 503 quando o Agent não montou o aprendizado.
func (s *Server) aprendizadoLigado(w http.ResponseWriter) bool {
	if s.pref.Aprendizado != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, "O aprendizado não está disponível nesta versão.")
	return false
}

// lerCorpo decodifica um corpo JSON pequeno e já responde 400 quando não
// serve.
func lerCorpo(w http.ResponseWriter, r *http.Request, destino any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxCorpoAprendizado)
	if err := json.NewDecoder(r.Body).Decode(destino); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return false
	}
	return true
}

// recusarAprendizado traduz o erro do Aprendizado para a resposta da tela e
// registra só a CLASSE do erro. variante e termo são os do pedido (vazios
// quando não há): a frase os cita, e só a resposta os carrega.
//
// Os erros de regra cabem na tela como estão ("SF já vira ECD"). O que é
// defeito ou ambiente vira uma frase sem detalhe: o texto do erro pode vir
// de fora do pacote e nunca é repassado.
func (s *Server) recusarAprendizado(w http.ResponseWriter, err error, variante, termo string) {
	status, code, msg := traduzirErroDeAprendizado(err, variante, termo)
	s.log.Info("aprendizado: recusado", "motivo", classeDoErro(err), "status", status)
	writeError(w, status, code, msg)
}

func traduzirErroDeAprendizado(err error, variante, termo string) (int, string, string) {
	var outro *dictionary.ErroVarianteDeOutroTermo
	var termoDeOutro *dictionary.ErroTermoEVarianteDeOutro
	switch {
	case errors.As(err, &outro):
		if outro.ETermo {
			return http.StatusConflict, codeConflict,
				fmt.Sprintf(`"%s" já é um termo do dicionário.`, variante)
		}
		return http.StatusConflict, codeConflict,
			fmt.Sprintf(`"%s" já vira "%s" no dicionário.`, variante, outro.Dono)
	case errors.As(err, &termoDeOutro):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf(`"%s" já é variante de "%s" e não pode virar termo.`, termo, termoDeOutro.Dono)
	case errors.Is(err, dictionary.ErrJaExiste):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf(`"%s" já vira "%s" no dicionário.`, variante, termo)
	case errors.Is(err, dictionary.ErrTermoInativo):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf(`"%s" está desligado no dicionário.`, termo)
	case errors.Is(err, dictionary.ErrNaoAgiria):
		return http.StatusConflict, codeConflict,
			fmt.Sprintf(`O dicionário não aplicaria "%s" num ditado. Nada foi gravado.`, variante)
	case errors.Is(err, dictionary.ErrSemNovidade):
		return http.StatusBadRequest, codeBadRequest,
			"Só muda maiúscula, acento ou espaço, e isso não se aprende."
	case errors.Is(err, dictionary.ErrVazio):
		return http.StatusBadRequest, codeBadRequest, "Faltou o termo ou a variante."
	case errors.Is(err, dictionary.ErrInvalido):
		return http.StatusBadRequest, codeBadRequest, "Essa troca é grande demais para aprender."
	case errors.Is(err, dictionary.ErrNaoAprendida):
		return http.StatusNotFound, codeNotFound,
			fmt.Sprintf(`"%s" não foi aprendido pelo painel.`, variante)
	case errors.Is(err, dictionary.ErrSemArquivo):
		return http.StatusServiceUnavailable, codeUnavailable,
			"Não achei o dicionário do Agent, então não há onde aprender."
	case errors.Is(err, dictionary.ErrArquivoInvalido):
		return http.StatusServiceUnavailable, codeUnavailable,
			"O dicionário do Agent não abre. Nada foi gravado."
	case errors.Is(err, dictionary.ErrUsuarioDiferente):
		return http.StatusServiceUnavailable, codeUnavailable,
			"O dicionário mudou de dono. Reinicie o Agent."
	case errors.Is(err, dictionary.ErrNaoCarrega):
		return http.StatusInternalServerError, codeInternal,
			"O dicionário com essa troca não carregaria. Nada foi gravado."
	default:
		return http.StatusInternalServerError, codeInternal, fraseDeDefeito
	}
}

// classeDoErro é o nome de um erro do aprendizado, de um conjunto fechado:
// é o que vai ao log, no lugar do texto do erro.
func classeDoErro(err error) string {
	var outro *dictionary.ErroVarianteDeOutroTermo
	var termoDeOutro *dictionary.ErroTermoEVarianteDeOutro
	switch {
	case errors.As(err, &outro):
		return "variante_de_outro_termo"
	case errors.As(err, &termoDeOutro):
		return "termo_e_variante_de_outro"
	case errors.Is(err, dictionary.ErrJaExiste):
		return "ja_existe"
	case errors.Is(err, dictionary.ErrTermoInativo):
		return "termo_inativo"
	case errors.Is(err, dictionary.ErrNaoAgiria):
		return "nao_agiria"
	case errors.Is(err, dictionary.ErrSemNovidade):
		return "sem_novidade"
	case errors.Is(err, dictionary.ErrVazio):
		return "vazio"
	case errors.Is(err, dictionary.ErrInvalido):
		return "invalido"
	case errors.Is(err, dictionary.ErrNaoAprendida):
		return "nao_aprendida"
	case errors.Is(err, dictionary.ErrSemArquivo):
		return "sem_arquivo"
	case errors.Is(err, dictionary.ErrArquivoInvalido):
		return "arquivo_invalido"
	case errors.Is(err, dictionary.ErrUsuarioDiferente):
		return "usuario_diferente"
	case errors.Is(err, dictionary.ErrNaoCarrega):
		return "nao_carrega"
	default:
		return "erro"
	}
}
