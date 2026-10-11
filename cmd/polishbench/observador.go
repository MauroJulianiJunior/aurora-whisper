package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// observador é um http.RoundTripper que conta o STATUS de cada resposta do
// provedor e, nas que não são 200, os identificadores curtos do erro (tipo,
// código e parâmetro) e, só nos erros de PEDIDO (400, 404 e 422), um trecho
// higienizado da mensagem. É como o relatório responde às dúvidas de protocolo
// que só a chamada real resolve (a OpenAI recusa o esforço "none" neste
// modelo? com que param? a Anthropic aceita output_config.format?).
//
// Nas respostas 200 ele guarda só a FORMA do envelope (impressaoDoEnvelope: as
// chaves de topo, status ou stop_reason, os tipos dos itens e das partes), para
// o relatório responder às dúvidas de formato mesmo quando a resposta dá
// resposta_invalida.
//
// O QUE ELE NUNCA GUARDA: o texto da resposta boa (o ditado organizado),
// qualquer cabeçalho (o de autenticação é a chave) e a mensagem dos erros de
// autenticação e de permissão (401, 403): é onde a OpenAI devolve um pedaço da
// chave recusada. Nos outros erros de status, também não há mensagem: só nos
// de pedido, onde o provedor não tem motivo para citar credencial.
//
// Os identificadores só entram se forem só letras minúsculas, "_" e "." (até
// 48 caracteres); o que não for assim vira "(outro)". E a mensagem passa por
// mensagemSegura: qualquer sequência de 20 caracteres ou mais de letra, dígito,
// "-", "_" ou "*" (uma chave, um pedaço de chave, um id de organização) some.
type observador struct {
	base http.RoundTripper

	mu        sync.Mutex
	contagem  map[string]int
	mensagens map[string]int
	envelopes map[string]int
}

func novoObservador(base http.RoundTripper) *observador {
	return &observador{base: base, contagem: map[string]int{}, mensagens: map[string]int{}, envelopes: map[string]int{}}
}

// limiteCorpoObservado é o que se lê do corpo de um erro para achar os
// identificadores. O cliente do Polish lê no máximo isto mesmo (1 MiB).
const limiteCorpoObservado = 1<<20 + 1

// maxMensagensDistintas limita quantas mensagens diferentes se guardam.
const maxMensagensDistintas = 20

func (o *observador) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := o.base.RoundTrip(req)
	if err != nil {
		o.marca("erro de rede ou prazo", "")
		return nil, err
	}
	rotulo := fmt.Sprintf("%d", resp.StatusCode)
	mensagem := ""
	if resp.StatusCode == http.StatusOK {
		o.marcaEnvelope(impressaoDoEnvelope(lerEDevolver(resp), !strings.HasSuffix(req.URL.Path, "/v1/messages")))
	}
	if resp.StatusCode != http.StatusOK {
		corpo, _ := io.ReadAll(io.LimitReader(resp.Body, limiteCorpoObservado))
		resp.Body.Close()
		// O cliente lê o mesmo corpo de volta: nada muda para ele.
		resp.Body = io.NopCloser(bytes.NewReader(corpo))
		rotulo += identificadoresDoErro(corpo)
		if erroDePedido(resp.StatusCode) {
			if m := mensagemDoErro(corpo); m != "" {
				mensagem = fmt.Sprintf("%d: %s", resp.StatusCode, m)
			}
		}
	}
	o.marca(rotulo, mensagem)
	return resp, nil
}

// corpoRestaurado devolve ao cliente o que o observador já leu, e depois o resto
// do corpo (ou o erro que interrompeu a leitura).
type corpoRestaurado struct {
	io.Reader
	fechar io.Closer
}

func (c corpoRestaurado) Close() error { return c.fechar.Close() }

// erroDeLeitura é o Reader que só devolve o erro que interrompeu a leitura do
// corpo, para o cliente vê-lo depois do que já foi lido.
type erroDeLeitura struct{ err error }

func (e erroDeLeitura) Read([]byte) (int, error) { return 0, e.err }

// lerEDevolver lê o corpo de uma resposta 200 (até limiteCorpoObservado) e o
// deixa na resposta como estava: o cliente lê os mesmos bytes, e o mesmo erro
// se a leitura foi interrompida (prazo, cancelamento).
func lerEDevolver(resp *http.Response) []byte {
	lido, err := io.ReadAll(io.LimitReader(resp.Body, limiteCorpoObservado))
	var resto io.Reader = resp.Body
	if err != nil {
		resto = erroDeLeitura{err}
	}
	resp.Body = corpoRestaurado{Reader: io.MultiReader(bytes.NewReader(lido), resto), fechar: resp.Body}
	return lido
}

// marcaEnvelope conta a impressão digital de uma resposta 200.
func (o *observador) marcaEnvelope(impressao string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ja := o.envelopes[impressao]; ja || len(o.envelopes) < maxMensagensDistintas {
		o.envelopes[impressao]++
	}
}

// Envelopes devolve uma cópia das impressões digitais das respostas 200, com
// quantas vezes apareceram.
func (o *observador) Envelopes() map[string]int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return copiar(o.envelopes)
}

// maxItensDaImpressao limita quantas chaves, tipos de item e tipos de parte
// entram numa impressão digital.
const maxItensDaImpressao = 30

// identificadorDe lê um valor JSON que deve ser um identificador curto: o
// próprio texto se for (letras minúsculas, "_" e "."), "(outro)" se for texto
// que não é, "(nulo)" e "(nao_texto)" nos outros casos. Nunca devolve texto
// livre.
func identificadorDe(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "(nao_json)"
	}
	switch s := v.(type) {
	case nil:
		return "(nulo)"
	case string:
		if reIdentificador.MatchString(s) {
			return s
		}
		return "(outro)"
	}
	return "(nao_texto)"
}

// tiposDe devolve o campo "type" de cada elemento do array JSON, em ordem,
// como identificadores (no máximo maxItensDaImpressao). ok=false se não for um
// array de objetos.
func tiposDe(raw json.RawMessage) (tipos []string, ok bool) {
	var itens []map[string]json.RawMessage
	if json.Unmarshal(raw, &itens) != nil {
		return nil, false
	}
	for _, it := range itens {
		if len(tipos) == maxItensDaImpressao {
			break
		}
		if t, tem := it["type"]; tem {
			tipos = append(tipos, identificadorDe(t))
		} else {
			tipos = append(tipos, "(sem_type)")
		}
	}
	return tipos, true
}

// impressaoDoEnvelope descreve a FORMA de uma resposta 200, sem nenhum texto do
// modelo: as chaves de topo, o valor de status (OpenAI) ou stop_reason
// (Anthropic), o motivo de "incomplete_details", os tipos dos itens de output e
// das partes de content. É como o relatório responde às dúvidas de formato da
// S2 mesmo quando a resposta dá errado (resposta_invalida): sem isto a rodada
// paga teria de ser repetida com diagnóstico. Só identificadores curtos entram
// (identificadorDe); o texto do modelo nunca.
func impressaoDoEnvelope(corpo []byte, openai bool) string {
	var topo map[string]json.RawMessage
	if json.Unmarshal(corpo, &topo) != nil {
		return "(nao_json)"
	}
	chaves := make([]string, 0, len(topo))
	for k := range topo {
		if reIdentificador.MatchString(k) {
			chaves = append(chaves, k)
		} else {
			chaves = append(chaves, "(outro)")
		}
	}
	sort.Strings(chaves)
	if len(chaves) > maxItensDaImpressao {
		chaves = chaves[:maxItensDaImpressao]
	}
	partes := []string{"chaves=" + strings.Join(chaves, ",")}
	campo := func(nome string) string {
		if raw, tem := topo[nome]; tem {
			return identificadorDe(raw)
		}
		return "(ausente)"
	}
	if openai {
		partes = append(partes, "status="+campo("status"))
		if raw, tem := topo["incomplete_details"]; tem {
			var det map[string]json.RawMessage
			if json.Unmarshal(raw, &det) == nil && det != nil {
				motivo := "(ausente)"
				if r, ok := det["reason"]; ok {
					motivo = identificadorDe(r)
				}
				partes = append(partes, "incompleto="+motivo)
			}
		}
		if e := identificadoresDoErro(corpo); e != "" {
			partes = append(partes, "erro"+e)
		}
		var itens []map[string]json.RawMessage
		if raw, tem := topo["output"]; !tem {
			partes = append(partes, "output=(ausente)")
		} else if tipos, ok := tiposDe(raw); !ok {
			partes = append(partes, "output=(nao_lista)")
		} else {
			partes = append(partes, "output="+strings.Join(tipos, ","))
			_ = json.Unmarshal(raw, &itens)
		}
		var conteudo []string
		for _, it := range itens {
			if tipos, ok := tiposDe(it["content"]); ok {
				conteudo = append(conteudo, tipos...)
			}
		}
		partes = append(partes, "partes="+strings.Join(conteudo, ","))
		return strings.Join(partes, " ")
	}
	partes = append(partes, "tipo="+campo("type"), "stop_reason="+campo("stop_reason"))
	if raw, tem := topo["content"]; !tem {
		partes = append(partes, "partes=(ausente)")
	} else if tipos, ok := tiposDe(raw); !ok {
		partes = append(partes, "partes=(nao_lista)")
	} else {
		partes = append(partes, "partes="+strings.Join(tipos, ","))
	}
	return strings.Join(partes, " ")
}

// erroDePedido diz se o status é de um erro do PEDIDO (o corpo descreve o que
// há de errado nele), e não da credencial nem da conta.
func erroDePedido(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity
}

func (o *observador) marca(rotulo, mensagem string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.contagem[rotulo]++
	if mensagem == "" {
		return
	}
	if _, ja := o.mensagens[mensagem]; ja || len(o.mensagens) < maxMensagensDistintas {
		o.mensagens[mensagem]++
	}
}

// Contagens devolve uma cópia do que foi visto: "200", "400 tipo=... codigo=...
// param=...".
func (o *observador) Contagens() map[string]int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return copiar(o.contagem)
}

// Mensagens devolve uma cópia das mensagens higienizadas dos erros de pedido,
// "400: texto", com quantas vezes apareceram.
func (o *observador) Mensagens() map[string]int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return copiar(o.mensagens)
}

func copiar(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

var reIdentificador = regexp.MustCompile(`^[a-z_.]{1,48}$`)

// identificadoresDoErro tira, do corpo de um erro, o tipo, o código e o
// parâmetro (e o error_code da Anthropic), só quando são identificadores
// curtos. Devolve " tipo=x codigo=y param=z" (só os que existem) ou "".
func identificadoresDoErro(corpo []byte) string {
	var e struct {
		Error struct {
			Type    any `json:"type"`
			Code    any `json:"code"`
			Param   any `json:"param"`
			Details struct {
				ErrorCode any `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(corpo, &e) != nil {
		return ""
	}
	var partes []string
	for _, campo := range []struct {
		nome string
		v    any
	}{
		{"tipo", e.Error.Type}, {"codigo", e.Error.Code}, {"param", e.Error.Param}, {"detalhe", e.Error.Details.ErrorCode},
	} {
		s, ok := campo.v.(string)
		switch {
		case !ok || s == "":
			continue
		case reIdentificador.MatchString(s):
			partes = append(partes, campo.nome+"="+s)
		default:
			partes = append(partes, campo.nome+"=(outro)")
		}
	}
	if len(partes) == 0 {
		return ""
	}
	return " " + strings.Join(partes, " ")
}

// reSequenciaLonga casa o que pode ser (um pedaço de) uma chave ou de um
// identificador de conta: 20 caracteres ou mais de letra, dígito, "-", "_" ou
// "*" sem espaço.
var reSequenciaLonga = regexp.MustCompile(`[A-Za-z0-9_*-]{20,}`)

// maxRunasDaMensagem é o tamanho máximo da mensagem guardada.
const maxRunasDaMensagem = 200

// mensagemDoErro tira o error.message do corpo de um erro e o higieniza.
func mensagemDoErro(corpo []byte) string {
	var e struct {
		Error struct {
			Message any `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(corpo, &e) != nil {
		return ""
	}
	s, _ := e.Error.Message.(string)
	return mensagemSegura(s)
}

// mensagemSegura higieniza o texto de uma mensagem de erro do provedor: tira
// os caracteres de controle, troca por "…" toda sequência que pareça um pedaço
// de chave ou de identificador (reSequenciaLonga), junta os espaços e corta em
// maxRunasDaMensagem.
func mensagemSegura(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
	s = reSequenciaLonga.ReplaceAllString(s, "…")
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxRunasDaMensagem {
		s = string([]rune(s)[:maxRunasDaMensagem]) + "…"
	}
	return s
}

// contagensOrdenadas devolve as chaves de um mapa de contagens em ordem
// estável (para o relatório).
func contagensOrdenadas(m map[string]int) []string {
	chaves := make([]string, 0, len(m))
	for k := range m {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	return chaves
}
