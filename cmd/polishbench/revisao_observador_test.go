package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// ---------------------------------------------------------------------
// A impressão digital das respostas 200 (revisão da S3): o observador só olhava
// o que não era 200, então as dúvidas de formato da S2 (status, refusal,
// incomplete_details, stop_reason) só ficavam respondidas quando tudo dava
// certo. Agora a FORMA do envelope entra no relatório, e nunca o texto.
// ---------------------------------------------------------------------

func TestImpressaoDoEnvelope_OpenAI(t *testing.T) {
	padrao := corpoOpenAI("texto do modelo", ia.Tokens{Entrada: 1, Saida: 1})
	casos := []struct{ nome, corpo, quero string }{
		{"resposta comum", padrao, "chaves=output,status,usage status=completed output=reasoning,message partes=output_text"},
		{"incompleta com o motivo", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning"}]}`,
			"chaves=incomplete_details,output,status status=incomplete incompleto=max_output_tokens output=reasoning partes="},
		{"recusa do modelo", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"não posso"}]}]}`,
			"chaves=output,status status=completed output=message partes=refusal"},
		{"várias partes, em ordem", `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"a"},{"type":"refusal"}]},{"type":"message","content":[{"type":"output_text","text":"b"}]}]}`,
			"chaves=output,status status=completed output=message,message partes=output_text,refusal,output_text"},
		{"sem status", `{"output":[]}`, "chaves=output status=(ausente) output= partes="},
		{"status nulo", `{"status":null,"output":[]}`, "chaves=output,status status=(nulo) output= partes="},
		{"status que não é texto", `{"status":7,"output":[]}`, "chaves=output,status status=(nao_texto) output= partes="},
		{"status com texto livre vira (outro)", `{"status":"Completed With Key sk-123","output":[]}`, "chaves=output,status status=(outro) output= partes="},
		{"sem output", `{"status":"completed"}`, "chaves=status status=completed output=(ausente) partes="},
		{"output que não é lista", `{"status":"completed","output":"x"}`, "chaves=output,status status=completed output=(nao_lista) partes="},
		{"item sem type e item com type estranho", `{"status":"completed","output":[{"x":1},{"type":"Tipo Estranho!"}]}`,
			"chaves=output,status status=completed output=(sem_type),(outro) partes="},
		{"incomplete_details nulo não entra", `{"status":"completed","incomplete_details":null,"output":[]}`, "chaves=incomplete_details,output,status status=completed output= partes="},
		{"incomplete_details sem motivo", `{"status":"incomplete","incomplete_details":{},"output":[]}`, "chaves=incomplete_details,output,status status=incomplete incompleto=(ausente) output= partes="},
		{"incomplete_details com motivo estranho", `{"status":"incomplete","incomplete_details":{"reason":"Ficou Sem Espaço"},"output":[]}`,
			"chaves=incomplete_details,output,status status=incomplete incompleto=(outro) output= partes="},
		{"erro dentro de um 200", `{"status":"failed","error":{"code":"server_error","message":"algo com sk-1234567890abcdefghij"},"output":[]}`,
			"chaves=error,output,status status=failed erro codigo=server_error output= partes="},
		{"chave estranha de topo vira (outro)", `{"Chave-Estranha":1,"id":"x","output":[]}`, "chaves=(outro),id,output status=(ausente) output= partes="},
		{"chaves em ordem alfabética", `{"z":1,"b":1,"a":1,"m":1,"c":1,"y":1,"d":1,"output":[]}`, "chaves=a,b,c,d,m,output,y,z status=(ausente) output= partes="},
		{"não é JSON", "isto não é json", "(nao_json)"},
		{"array em vez de objeto", `[1,2]`, "(nao_json)"},
		{"vazio", ``, "(nao_json)"},
	}
	for _, k := range casos {
		if got := impressaoDoEnvelope([]byte(k.corpo), true); got != k.quero {
			t.Errorf("%s:\n  got  %q\n  quero %q", k.nome, got, k.quero)
		}
	}
}

func TestImpressaoDoEnvelope_Anthropic(t *testing.T) {
	casos := []struct{ nome, corpo, quero string }{
		{"resposta comum", corpoAnthropic("texto do modelo", ia.Tokens{Entrada: 1, Saida: 1}), "chaves=content,stop_reason,usage tipo=(ausente) stop_reason=end_turn partes=text"},
		{"com o type de topo", `{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"a"}]}`, "chaves=content,stop_reason,type tipo=message stop_reason=end_turn partes=text"},
		{"cortada por tamanho", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"a"}]}`, "chaves=content,stop_reason tipo=(ausente) stop_reason=max_tokens partes=text"},
		{"recusa", `{"stop_reason":"refusal","content":[]}`, "chaves=content,stop_reason tipo=(ausente) stop_reason=refusal partes="},
		{"sem stop_reason", `{"content":[{"type":"text"}]}`, "chaves=content tipo=(ausente) stop_reason=(ausente) partes=text"},
		{"stop_reason nulo", `{"stop_reason":null,"content":[]}`, "chaves=content,stop_reason tipo=(ausente) stop_reason=(nulo) partes="},
		{"stop_reason livre", `{"stop_reason":"Fim Da Fila","content":[]}`, "chaves=content,stop_reason tipo=(ausente) stop_reason=(outro) partes="},
		{"sem content", `{"stop_reason":"end_turn"}`, "chaves=stop_reason tipo=(ausente) stop_reason=end_turn partes=(ausente)"},
		{"content que não é lista", `{"stop_reason":"end_turn","content":"x"}`, "chaves=content,stop_reason tipo=(ausente) stop_reason=end_turn partes=(nao_lista)"},
		{"não é JSON", "<html>", "(nao_json)"},
	}
	for _, k := range casos {
		if got := impressaoDoEnvelope([]byte(k.corpo), false); got != k.quero {
			t.Errorf("%s:\n  got  %q\n  quero %q", k.nome, got, k.quero)
		}
	}
}

// Nada do texto do modelo entra na impressão: nem na resposta, nem na recusa,
// nem numa chave que o modelo tenha ecoado.
func TestImpressaoDoEnvelope_NuncaLevaTexto(t *testing.T) {
	segredo := chaveOpenAI
	corpos := []string{
		corpoOpenAI(segredo, ia.Tokens{}),
		`{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"` + segredo + `"}]}]}`,
		`{"status":"` + segredo + `","incomplete_details":{"reason":"` + segredo + `"},"output":[{"type":"` + segredo + `"}]}`,
		`{"` + segredo + `":1,"output":[]}`,
		`{"error":{"code":"` + segredo + `","type":"` + segredo + `","message":"` + segredo + `"}}`,
	}
	for i, c := range corpos {
		for _, openai := range []bool{true, false} {
			got := impressaoDoEnvelope([]byte(c), openai)
			if strings.Contains(got, "CANARIO") || strings.Contains(got, "sk-") {
				t.Errorf("corpo %d (openai=%v): a impressão leva texto do corpo: %q", i, openai, got)
			}
		}
	}
	for _, c := range []string{corpoAnthropic(segredo, ia.Tokens{}), `{"stop_reason":"` + segredo + `","content":[{"type":"` + segredo + `"}]}`} {
		if got := impressaoDoEnvelope([]byte(c), false); strings.Contains(got, "CANARIO") {
			t.Errorf("a impressão da Anthropic leva texto: %q", got)
		}
	}
}

func TestImpressaoDoEnvelope_TetosDeItens(t *testing.T) {
	var itens []string
	for i := 0; i < 40; i++ {
		itens = append(itens, `{"type":"t"}`)
	}
	var topo []string
	for i := 0; i < 31; i++ {
		topo = append(topo, fmt.Sprintf(`"%s":1`, strings.Repeat("a", i+1)))
	}
	corpo := "{" + strings.Join(topo, ",") + `,"output":[` + strings.Join(itens, ",") + `]}`
	got := impressaoDoEnvelope([]byte(corpo), true)
	chavesNaImpressao := strings.Split(strings.TrimPrefix(strings.SplitN(got, " ", 2)[0], "chaves="), ",")
	if len(chavesNaImpressao) != maxItensDaImpressao {
		t.Errorf("chaves na impressão = %d, quero o teto de %d", len(chavesNaImpressao), maxItensDaImpressao)
	}
	if n := strings.Count(got, "t,") + 1; n != maxItensDaImpressao {
		t.Errorf("itens de output na impressão = %d, quero o teto de %d", n, maxItensDaImpressao)
	}
}

func TestObservador_200GuardaSoAFormaEDevolveOCorpoIntacto(t *testing.T) {
	corpo := corpoOpenAI("ditado organizado que não pode ser guardado", ia.Tokens{Entrada: 5, Saida: 5})
	o := novoObservador(respostaCom(200, corpo))
	if got := chamar(t, o); got != corpo {
		t.Errorf("o corpo do 200 mudou: %q", got)
	}
	chamar(t, o)
	env := o.Envelopes()
	quero := "chaves=output,status,usage status=completed output=reasoning,message partes=output_text"
	if len(env) != 1 || env[quero] != 2 {
		t.Errorf("envelopes = %v, quero só %q ×2", env, quero)
	}
	for k := range env {
		if strings.Contains(k, "ditado") {
			t.Errorf("a impressão levou o texto: %q", k)
		}
	}
	if c := o.Contagens(); c["200"] != 2 || len(c) != 1 {
		t.Errorf("contagens = %v", c)
	}
	if len(o.Mensagens()) != 0 {
		t.Error("um 200 não tem mensagem")
	}
	// Uma cópia: mexer no resultado não mexe no observador.
	env["x"] = 9
	if len(o.Envelopes()) != 1 {
		t.Error("Envelopes devolveu o mapa interno")
	}
}

func TestObservador_SoOsErrosNaoTemEnvelope(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500} {
		o := novoObservador(respostaCom(status, `{"error":{"code":"x"}}`))
		chamar(t, o)
		if len(o.Envelopes()) != 0 {
			t.Errorf("status %d gerou envelope: %v", status, o.Envelopes())
		}
	}
}

// O provedor sai do caminho da requisição: /v1/messages é a Anthropic.
func TestObservador_ProvedorPeloCaminho(t *testing.T) {
	o := novoObservador(respostaCom(200, `{"stop_reason":"end_turn","status":"completed","output":[],"content":[{"type":"text"}]}`))
	for _, caminho := range []string{"/v1/responses", "/v1/messages"} {
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1"+caminho, nil)
		resp, err := o.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	var openai, anthropic int
	for k := range o.Envelopes() {
		switch {
		case strings.Contains(k, "status=completed") && strings.Contains(k, "output="):
			openai++
		case strings.Contains(k, "stop_reason=end_turn") && strings.Contains(k, "tipo="):
			anthropic++
		}
	}
	if openai != 1 || anthropic != 1 {
		t.Errorf("envelopes = %v, quero um de cada estilo", o.Envelopes())
	}
}

func TestObservador_TetoDeEnvelopesDistintos(t *testing.T) {
	o := novoObservador(respostaCom(200, `{}`))
	for i := 0; i < maxMensagensDistintas+5; i++ {
		o.marcaEnvelope(fmt.Sprintf("forma-%d", i))
	}
	if n := len(o.Envelopes()); n != maxMensagensDistintas {
		t.Errorf("envelopes distintos = %d, quero o teto de %d", n, maxMensagensDistintas)
	}
	o.marcaEnvelope("forma-0")
	o.marcaEnvelope("forma-0")
	if got := o.Envelopes()["forma-0"]; got != 3 {
		t.Errorf("uma forma já guardada continua contando depois do teto: %d, quero 3", got)
	}
	if _, tem := o.Envelopes()[fmt.Sprintf("forma-%d", maxMensagensDistintas)]; tem {
		t.Error("passou do teto")
	}
}

// Um corpo de 200 maior que o limite lido continua inteiro para o cliente.
func TestObservador_CorpoGrandeDe200ChegaInteiroAoCliente(t *testing.T) {
	grande := `{"status":"completed","output":[],"x":"` + strings.Repeat("a", limiteCorpoObservado+500) + `"}`
	o := novoObservador(respostaCom(200, grande))
	if got := chamar(t, o); got != grande {
		t.Errorf("o corpo grande foi cortado: %d bytes de %d", len(got), len(grande))
	}
	env := o.Envelopes()
	if len(env) != 1 || env["(nao_json)"] != 1 {
		t.Errorf("um corpo maior que o limite não dá para ler como JSON: %v", env)
	}
}

// corpoQueFalha devolve o texto, depois o erro (prazo, cancelamento) UMA vez e,
// daí em diante, EOF: um corpo que não repete o erro. O observador é quem tem de
// levá-lo ao cliente.
type corpoQueFalha struct {
	texto  string
	lido   bool
	falhou bool
	err    error
}

func (c *corpoQueFalha) Read(p []byte) (int, error) {
	switch {
	case !c.lido:
		c.lido = true
		return copy(p, c.texto), nil
	case !c.falhou:
		c.falhou = true
		return 0, c.err
	}
	return 0, io.EOF
}
func (c *corpoQueFalha) Close() error { return nil }

func TestObservador_LeituraInterrompidaChegaAoClienteComOMesmoErro(t *testing.T) {
	falha := errors.New("context deadline exceeded (teste)")
	o := novoObservador(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &corpoQueFalha{texto: `{"status":"comp`, err: falha}}, nil
	}))
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/v1/responses", nil)
	resp, err := o.RoundTrip(req)
	if err != nil {
		t.Fatalf("o RoundTrip não devia falhar: %v", err)
	}
	b, err := io.ReadAll(resp.Body)
	if string(b) != `{"status":"comp` || !errors.Is(err, falha) {
		t.Errorf("o cliente leu %q com erro %v; quero o texto e o erro do prazo", b, err)
	}
	if got := o.Envelopes(); got["(nao_json)"] != 1 {
		t.Errorf("envelopes = %v", got)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Fechar a resposta fecha o corpo de baixo.
func TestObservador_CloseDoCorpoRestauradoFechaOOriginal(t *testing.T) {
	fechou := false
	corpo := struct {
		io.Reader
		io.Closer
	}{strings.NewReader(`{}`), closerFunc(func() error { fechou = true; return nil })}
	o := novoObservador(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: corpo}, nil
	}))
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/v1/responses", nil)
	resp, _ := o.RoundTrip(req)
	resp.Body.Close()
	if !fechou {
		t.Error("o corpo original não foi fechado")
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// No comando inteiro: a resposta 200 que a guarda do cliente recusa (envelope
// inesperado) deixa a forma no relatório, e o texto da recusa nunca aparece.
func TestRun_RelatorioMostraAFormaDasRespostas200(t *testing.T) {
	frases, refs := datasetPequeno()
	oa := func(p pedidoFalso) respostaFalsa {
		if p.Ditado == "qual o prazo da DCTFWeb" {
			return respostaFalsa{CorpoLiteral: `{"resultado":"texto secreto do modelo ` + chaveOpenAI + `"}`}
		}
		return respostaFalsa{Final: p.Ditado}
	}
	a := novoAmbiente(t, oa, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1,anthropic:m2", "-preco", "openai:m1=1/1,anthropic:m2=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "**Forma das respostas 200**")
	contem(t, rel, "`chaves=output,status,usage status=completed output=reasoning,message partes=output_text` ×6")
	contem(t, rel, "`chaves=resultado status=(ausente) output=(ausente) partes=` ×1")
	contem(t, rel, "`chaves=content,stop_reason,usage tipo=(ausente) stop_reason=end_turn partes=text` ×7")
	naoContem(t, a.tudoQueSaiu(), "texto secreto do modelo")
	naoContem(t, a.tudoQueSaiu(), "CANARIO")
	contem(t, rel, "`resposta_invalida`")
}

// ---------------------------------------------------------------------
// Textos do relatório (revisão da S3, itens de consistência)
// ---------------------------------------------------------------------

func TestRelatorio_TextosConsistentes(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, nil, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	rel := a.lerRelatorio()

	// O texto sobre a mensagem dos erros diz a exceção dos erros de pedido.
	contem(t, rel, "SÓ nos erros de pedido (400, 404, 422), a mensagem higienizada")
	contem(t, rel, "Nunca a mensagem dos erros de chave, permissão, limite ou servidor (401, 403, 429, 5xx)")
	naoContem(t, rel, "nunca o texto da mensagem, que pode trazer")
	// Os exemplos da categoria ja_organizada não são "todos sintéticos".
	naoContem(t, rel, "são todas sintéticas")
	contem(t, rel, "As frases mostradas vêm do dataset sintético (`frases.json`) e, na categoria `ja_organizada`, das `reference` do manifest do dataset de ASR")
	contem(t, rel, "confira os exemplos antes de compartilhar este arquivo")
	// Os caminhos dos arquivos (o de quem rodou, a pasta temporária) ficam de fora.
	dir := filepath.Dir(a.relatorio)
	naoContem(t, rel, dir)
	contem(t, rel, "Frases: `frases.json (arquivo em caminho diferente do repositório; o dataset pode ser outro)`")
	contem(t, rel, "\"já organizada\": `manifest.json (arquivo em caminho diferente do repositório; o dataset pode ser outro)`")
}

func TestNomeDoArquivo(t *testing.T) {
	if got := nomeDoArquivo(padraoFrases, padraoFrases); got != "frases.json" {
		t.Errorf("no caminho padrão: %q", got)
	}
	if got := nomeDoArquivo(padraoManifesto, padraoManifesto); got != "manifest-normalizado.json" {
		t.Errorf("no caminho padrão: %q", got)
	}
	got := nomeDoArquivo(filepath.Join("C:\\", "Users", "quem-rodou", "meus", "frases.json"), padraoFrases)
	if strings.Contains(got, "quem-rodou") || !strings.HasPrefix(got, "frases.json (arquivo em caminho diferente") {
		t.Errorf("fora do padrão: %q", got)
	}
	if got := nomeDoArquivo("frases.json", padraoFrases); !strings.Contains(got, "caminho diferente") {
		t.Errorf("só o nome, sem a pasta, também é diferente do padrão: %q", got)
	}
}
