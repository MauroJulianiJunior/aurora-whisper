package winruntime

// O Polish no runtime (ADR-016, S4): o Smart no laço, só a rede numa
// goroutine com prazo, o resultado de volta por rt.polidos, o modo por
// tentativa no histórico, a linha de log sem texto e o último motivo para
// a tela. Tudo com dublês e um relógio curto (o prazo do teste é de
// dezenas de milissegundos). O que usa o cliente HTTP de verdade
// (internal/ia) fala só com um httptest em loopback, por um transporte que
// recusa qualquer outro destino: nada aqui chama a OpenAI ou a Anthropic.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/inserter"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

const (
	// "março": os tamanhos do log são em caracteres, não em bytes.
	ditadoLongo  = "o cliente mandou o balancete de março"
	ditadoPolido = "O cliente mandou o balancete de março."
)

// passoPolish é o que o polidorFalso faz numa chamada: segurar (se não
// nil) prende a chamada IGNORANDO o ctx — o pior caso, o de um cliente que
// não respeita o prazo —, e texto é a reescrita que ele devolve aceita.
type passoPolish struct {
	segurar chan struct{}
	texto   string
}

// polidorFalso faz o papel do resolver.PolishResolver. Cada chamada avisa
// em entrou (com o ctx que recebeu) e segue o próximo passo do roteiro.
type polidorFalso struct {
	roteiro chan passoPolish
	entrou  chan context.Context
	contar  func(entrada, saida int)
}

func (f *polidorFalso) Reescrever(ctx context.Context, _ resolver.ResolveRequest, smart resolver.ResolveResult) resolver.ResolveResult {
	f.entrou <- ctx
	p := <-f.roteiro
	if p.segurar != nil {
		<-p.segurar
	}
	if f.contar != nil {
		f.contar(900, 40)
	}
	r := smart
	r.Mode, r.FinalText, r.Reescrito, r.MotivoFallback = resolver.ModePolish, p.texto, true, resolver.MotivoNenhum
	return r
}

// polishFalso monta o AbrirPolidor dos testes: conta as aberturas e
// entrega um polidorFalso que segue o roteiro dado.
type polishFalso struct {
	abertos atomic.Int32
	roteiro chan passoPolish
	entrou  chan context.Context
}

func novoPolishFalso(passos ...passoPolish) *polishFalso {
	f := &polishFalso{roteiro: make(chan passoPolish, len(passos)+1), entrou: make(chan context.Context, 8)}
	for _, p := range passos {
		f.roteiro <- p
	}
	return f
}

func (f *polishFalso) abrir(provedor, modelo string, contar func(int, int)) (Polidor, string) {
	f.abertos.Add(1)
	return &polidorFalso{roteiro: f.roteiro, entrou: f.entrou, contar: contar}, "none"
}

// abrirCom monta o AbrirPolidor com o PolishResolver DE VERDADE (guardas e
// tudo) sobre o cliente dado. O Smart é o sem dicionário.
func abrirCom(cli resolver.PolishClient) AbrirPolidor {
	return func(string, string, func(int, int)) (Polidor, string) {
		return &resolver.PolishResolver{Smart: resolver.NewSmartResolver(nil), Client: cli}, "none"
	}
}

// clienteFixo é um resolver.PolishClient que devolve sempre a mesma
// resposta bruta, e conta as chamadas.
type clienteFixo struct {
	resposta string
	err      error
	chamadas atomic.Int32
}

func (c *clienteFixo) Polish(context.Context, resolver.PolishRequest) ([]byte, error) {
	c.chamadas.Add(1)
	return []byte(c.resposta), c.err
}

// runtimeDoPolish monta o runtime das provas unitárias: o teste faz o papel
// do laço (chama resolver, recebe de rt.polidos, conclui). O Smart é o de
// verdade, sem dicionário, e o Polish está ligado para "openai"/"modelo-x".
func runtimeDoPolish(t *testing.T, abrir AbrirPolidor) (*Runtime, *escritaSegura, *historicoFalso) {
	t.Helper()
	rt := testRuntime(t, fakeFocus{})
	saida := &escritaSegura{}
	rt.log = slog.New(slog.NewTextHandler(saida, nil))
	rt.polidos = make(chan polidoPronto, 1)
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	rt.UsarResolver(resolver.NewSmartResolver(nil), "mauro")
	rt.UsarPolish(abrir)
	rt.Ajustar(Preferencias{HoldThreshold: 700 * time.Millisecond, Polish: true, Provedor: "openai", Modelo: "modelo-x"})
	rt.aplicarAjuste()
	if rt.modo() != "polish" {
		t.Fatalf("pré-condição: modo = %q, quero polish", rt.modo())
	}
	return rt, saida, hist
}

// pedirPolish é o ResolveEffect de um ditado em Polish: resolver tem de
// voltar SEM resposta (a rede não roda no laço), e o resultado chega por
// rt.polidos.
func pedirPolish(t *testing.T, rt *Runtime, attempt dictation.AttemptID, texto string) {
	t.Helper()
	if ev := rt.resolver(dictation.ResolveEffect{Attempt: attempt, ASRResult: dictation.ASRResultDTO{Text: texto}}); ev != nil {
		t.Fatalf("no Polish, resolver respondeu na hora (%T): a chamada teria rodado no laço", ev)
	}
}

func receberPolido(t *testing.T, rt *Runtime, prazo time.Duration) polidoPronto {
	t.Helper()
	select {
	case p := <-rt.polidos:
		return p
	case <-time.After(prazo):
		t.Fatalf("o resultado do Polish não voltou em %s", prazo)
		return polidoPronto{}
	}
}

// ditarEmPolish percorre um ditado inteiro no Polish, do ResolveEffect ao
// histórico, e devolve o texto que iria para a inserção.
func ditarEmPolish(t *testing.T, rt *Runtime, attempt dictation.AttemptID, texto string) string {
	t.Helper()
	pedirPolish(t, rt, attempt, texto)
	final := textoDe(t, rt.concluirPolish(receberPolido(t, rt, 2*time.Second)))
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: attempt, Outcome: dictation.OutcomeSuccess, Text: final})
	return final
}

// camposDaLinha lê os pares chave=valor de uma linha do slog em texto.
func camposDaLinha(linha string) map[string]string {
	c := map[string]string{}
	for _, m := range regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|\S*)`).FindAllStringSubmatch(linha, -1) {
		c[m[1]] = strings.Trim(m[2], `"`)
	}
	return c
}

// camposDoPolish são TODOS os campos que a linha do Polish pode ter
// (ADR-016, decisão 12; 11-observability.md). Um campo novo tem de passar
// por aqui — e pela pergunta "isto pode carregar texto?".
var camposDoPolish = []string{
	"time", "level", "msg", "attempt", "prompt", "prompt_hash", "provedor", "modelo", "esforco",
	"desfecho", "latencia_ms", "prazo_ms", "entrada_chars", "saida_chars", "tokens_entrada", "tokens_saida",
	"frases", "frases_revertidas",
}

// linhasDoPolish devolve as linhas "winruntime: polish" do log.
func linhasDoPolish(log string) []string {
	var l []string
	for _, linha := range strings.Split(log, "\n") {
		if strings.Contains(linha, `msg="winruntime: polish"`) {
			l = append(l, linha)
		}
	}
	return l
}

// textoDeTamanho devolve um texto de n caracteres (runas), com acento.
func textoDeTamanho(n int) string { return strings.Repeat("ã", n) }

// TestPrazoPolish_CabeNoWatchdogDoNucleo: o prazo do Polish tem de deixar
// a folga de 1,5 s até o TimerResolveTimeout do núcleo, que encerra a
// tentativa SEM texto, em QUALQUER tamanho de texto. Se o núcleo encurtar
// o timer, ou alguém subir o prazo, este teste obriga a decisão a voltar
// (ADR-016, decisão 4 e emenda de 01-out-2026).
func TestPrazoPolish_CabeNoWatchdogDoNucleo(t *testing.T) {
	if pisoPrazoPolish != 3500*time.Millisecond {
		t.Fatalf("pisoPrazoPolish = %s, quero os 3,5 s da ADR-016", pisoPrazoPolish)
	}
	var rt Runtime
	for _, n := range []int{0, 1, 38, 250, 251, 359, 802, 907, 999, 1000, 1001, 1373, 3000, 50_000} {
		texto := textoDeTamanho(n)
		polish, nucleo := prazoPolish(texto), dictation.PrazoDeResolucao(texto)
		if folga := nucleo - polish; folga != 1500*time.Millisecond {
			t.Errorf("%d caracteres: Polish %s, watchdog %s; folga %s, quero 1,5 s", n, polish, nucleo, folga)
		}
		if polish < pisoPrazoPolish || polish > 8*time.Second {
			t.Errorf("%d caracteres: Polish %s, fora de [3,5 s, 8 s]", n, polish)
		}
		if got := rt.prazoDoTeto(texto); got != polish {
			t.Errorf("%d caracteres: sem ajuste de teste, o teto é %s, quero %s", n, got, polish)
		}
	}
}

// TestPrazoPolish_Tabela: 3,5 s até 250 caracteres, 6 ms por caractere
// acima, até 8 s aos 1000 (decisão 5 de docs/defeito-ditado-longo.md).
func TestPrazoPolish_Tabela(t *testing.T) {
	for _, c := range []struct {
		caracteres int
		want       time.Duration
	}{
		// A fala curta continua com os 3,5 s, ao milissegundo: os ditados
		// curtos do log de 01-out-2026 (34, 38, 41, 77 e 99 caracteres) e
		// o curto que nem vai ao provedor (9).
		{0, 3500 * time.Millisecond},
		{9, 3500 * time.Millisecond},
		{34, 3500 * time.Millisecond},
		{99, 3500 * time.Millisecond},
		{250, 3500 * time.Millisecond},
		{251, 3506 * time.Millisecond},
		{254, 3524 * time.Millisecond},
		// Os longos do incidente.
		{359, 4154 * time.Millisecond},
		{802, 6812 * time.Millisecond},
		{907, 7442 * time.Millisecond},
		// O teto do Mauro: 8 s.
		{1000, 8 * time.Second},
		{1373, 8 * time.Second},
	} {
		if got := prazoPolish(textoDeTamanho(c.caracteres)); got != c.want {
			t.Errorf("prazoPolish(%d caracteres) = %s, quero %s", c.caracteres, got, c.want)
		}
	}
}

// TestPrazoPolish_CasosDoIncidente: as chamadas do agent.log de 01-out-2026
// (modelo gpt-6-luna, esforço none), com a latência que tiveram. Com o
// prazo fixo, a de 802 caracteres caiu no Smart por 1 ms, e a de 359 passou
// a 357 ms do teto. Com o prazo novo, todas cabem com pelo menos 1 s de
// sobra — e as curtas, com o mesmo prazo de antes.
func TestPrazoPolish_CasosDoIncidente(t *testing.T) {
	for _, c := range []struct {
		caracteres int
		latencia   time.Duration // a 802 foi cortada: é o mínimo que ela levaria
		cortada    bool
	}{
		{99, 2624 * time.Millisecond, false},
		{77, 2177 * time.Millisecond, false},
		{34, 2378 * time.Millisecond, false},
		{254, 2068 * time.Millisecond, false},
		{41, 3116 * time.Millisecond, false},
		{38, 1965 * time.Millisecond, false},
		{359, 3143 * time.Millisecond, false},
		{907, 3032 * time.Millisecond, false},
		{802, 3469 * time.Millisecond, true},
	} {
		prazo := prazoPolish(textoDeTamanho(c.caracteres))
		sobra := prazo - c.latencia
		switch {
		case c.caracteres <= 250 && prazo != 3500*time.Millisecond:
			t.Errorf("%d caracteres: prazo %s; a fala curta tem de continuar com 3,5 s", c.caracteres, prazo)
		case c.caracteres > 250 && sobra < time.Second:
			t.Errorf("%d caracteres: prazo %s, latência %s (cortada: %v); sobra %s, quero ao menos 1 s", c.caracteres, prazo, c.latencia, c.cortada, sobra)
		}
	}
}

// TestPolish_PrazoPeloTextoDoResolveEffect: o prazo do Polish é medido no
// texto que entrou na resolução — o mesmo em que o núcleo mediu o watchdog —,
// e não no que o Smart devolveu. Aqui o Smart encolhe o texto (junta os
// espaços repetidos): 720 caracteres viram 479. Medir o do Smart daria
// 4,874 s ao Polish; o certo são os 6,32 s dos 720, 1,5 s abaixo do
// watchdog dos mesmos 720. Se o Smart ALONGASSE o texto (um termo do
// dicionário mais comprido), medir o dele comeria a folga.
func TestPolish_PrazoPeloTextoDoResolveEffect(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, _, _ := runtimeDoPolish(t, f.abrir)
	texto := strings.Repeat("palavra     ", 60)
	if n := len([]rune(texto)); n != 720 {
		t.Fatalf("pré-condição: %d caracteres", n)
	}

	antes := time.Now()
	pedirPolish(t, rt, 1, texto)
	ctx := <-f.entrou
	p := receberPolido(t, rt, 2*time.Second)
	if n := p.entrada; n >= 720 {
		t.Fatalf("pré-condição: o Smart devolveu %d caracteres; o teste precisa que ele encolha o texto", n)
	}
	want := prazoPolish(texto)
	if want != 6320*time.Millisecond || dictation.PrazoDeResolucao(texto)-want != 1500*time.Millisecond {
		t.Fatalf("pré-condição: prazo %s para 720 caracteres", want)
	}
	if limite, ok := ctx.Deadline(); !ok || limite.Before(antes.Add(want)) || limite.After(time.Now().Add(want)) {
		t.Fatalf("o ctx do Polish vence em %v (%v) depois do começo; quero %s, o do texto que entrou na resolução", limite.Sub(antes), ok, want)
	}
}

// TestPolish_PrazoNoLogAcompanhaOTexto: o prazo_ms da linha "winruntime:
// polish" é o teto do tamanho do texto (menos o que o Smart e a partida da
// goroutine gastaram), e o ctx da chamada vence nele. Sem ajuste de teste:
// é o prazo de produção.
func TestPolish_PrazoNoLogAcompanhaOTexto(t *testing.T) {
	for _, c := range []struct {
		caracteres int
		want       time.Duration
	}{
		{len([]rune(ditadoLongo)), 3500 * time.Millisecond},
		{359, 4154 * time.Millisecond},
		{802, 6812 * time.Millisecond},
		{1373, 8 * time.Second},
	} {
		f := novoPolishFalso(passoPolish{texto: ditadoPolido})
		rt, saida, _ := runtimeDoPolish(t, f.abrir)
		texto := ditadoLongo
		if c.caracteres > len([]rune(ditadoLongo)) {
			// Palavras de verdade: o Polish só vai ao provedor com 4 ou mais.
			texto = strings.Repeat("palavra ", c.caracteres/8) + strings.Repeat("é", c.caracteres%8)
		}
		if n := len([]rune(texto)); n != c.caracteres {
			t.Fatalf("pré-condição: o texto tem %d caracteres, quero %d", n, c.caracteres)
		}

		antes := time.Now()
		pedirPolish(t, rt, 1, texto)
		ctx := <-f.entrou
		gasto := time.Since(antes)
		if limite, ok := ctx.Deadline(); !ok || limite.Before(antes.Add(c.want)) || limite.After(time.Now().Add(c.want)) {
			t.Errorf("%d caracteres: o ctx vence em %v (%v); quero o começo da resolução + %s", c.caracteres, limite.Sub(antes), ok, c.want)
		}
		rt.concluirPolish(receberPolido(t, rt, 2*time.Second))

		linhas := linhasDoPolish(saida.String())
		if len(linhas) != 1 {
			t.Fatalf("%d linha(s) de Polish:\n%s", len(linhas), saida.String())
		}
		campos := camposDaLinha(linhas[0])
		// O prazo_ms é o teto menos o que passou entre o começo da resolução e
		// a chamada. Esse tempo está entre 0 e o gasto medido acima (a chamada
		// já tinha começado quando o ctx chegou aqui), então os limites valem
		// em qualquer máquina, lenta ou não: sem tolerância fixa no relógio.
		// O 1 é o truncamento de Milliseconds().
		ms, err := strconv.Atoi(campos["prazo_ms"])
		piso := (c.want - gasto).Milliseconds() - 1
		if err != nil || int64(ms) > c.want.Milliseconds() || int64(ms) < piso {
			t.Errorf("%d caracteres: prazo_ms=%q (%v), quero entre %d e %d", c.caracteres, campos["prazo_ms"], err, piso, c.want.Milliseconds())
		}
		if campos["desfecho"] != "aceito" {
			t.Errorf("%d caracteres: desfecho %q, quero aceito", c.caracteres, campos["desfecho"])
		}
	}
}

// TestPolish_AceitoGravaPolish: a reescrita aceita é o texto que segue, o
// histórico diz "polish", a linha de log tem os números e o último motivo
// fica vazio (o último Polish agiu).
func TestPolish_AceitoGravaPolish(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, saida, hist := runtimeDoPolish(t, f.abrir)

	if got := ditarEmPolish(t, rt, 1, ditadoLongo); got != ditadoPolido {
		t.Fatalf("texto = %q, quero o do Polish", got)
	}
	l := hist.unica(t)
	if l.Mode != "polish" || l.FinalText != ditadoPolido || l.RawText != ditadoLongo {
		t.Fatalf("histórico: modo %q, final %q, bruto %q", l.Mode, l.FinalText, l.RawText)
	}
	linhas := linhasDoPolish(saida.String())
	if len(linhas) != 1 {
		t.Fatalf("%d linha(s) de Polish, quero 1:\n%s", len(linhas), saida.String())
	}
	for _, campo := range []string{
		"level=INFO", "attempt=1", "prompt=" + resolver.PolishPromptRef, "prompt_hash=" + resolver.PolishPromptHash(),
		"provedor=openai", "modelo=modelo-x", "esforco=none", "desfecho=aceito", "latencia_ms=", "prazo_ms=",
		fmt.Sprintf("entrada_chars=%d", len([]rune(ditadoLongo))), fmt.Sprintf("saida_chars=%d", len([]rune(ditadoPolido))),
		"tokens_entrada=900", "tokens_saida=40",
	} {
		if !strings.Contains(linhas[0], campo) {
			t.Errorf("a linha do Polish não tem %q:\n%s", campo, linhas[0])
		}
	}
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoNenhum || e.Quando.IsZero() {
		t.Fatalf("último Polish = %+v, quero sem motivo e com a hora", e)
	}
}

// TestPolish_GuardaReprovaGravaSmartEGuardaOMotivo: com as guardas de
// verdade, uma resposta que inverte o sentido cai no Smart; o histórico
// diz "smart", e o motivo fica para a tela.
func TestPolish_GuardaReprovaGravaSmartEGuardaOMotivo(t *testing.T) {
	cli := &clienteFixo{resposta: `{"final_text":"O cliente pagou a guia do DAS."}`}
	rt, saida, hist := runtimeDoPolish(t, abrirCom(cli))
	const ditado = "o cliente não pagou a guia do DAS"

	if got := ditarEmPolish(t, rt, 1, ditado); got != ditado {
		t.Fatalf("texto = %q, quero o do Smart", got)
	}
	if l := hist.unica(t); l.Mode != "smart" || l.FinalText != ditado {
		t.Fatalf("histórico: modo %q, final %q; quero smart", l.Mode, l.FinalText)
	}
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoPalavras || e.Quando.IsZero() {
		t.Fatalf("último Polish = %+v, quero o motivo palavras", e)
	}
	if cli.chamadas.Load() != 1 {
		t.Fatalf("chamadas = %d, quero 1", cli.chamadas.Load())
	}
	if l := linhasDoPolish(saida.String()); len(l) != 1 || !strings.Contains(l[0], "desfecho=palavras") {
		t.Fatalf("linha do Polish: %v", l)
	}
}

// TestPolish_CurtoNaoChama: menos de 4 palavras não vai ao provedor, sai
// como Smart na hora, deixa a linha com o motivo e não mexe no último
// motivo da tela (não foi falha).
func TestPolish_CurtoNaoChama(t *testing.T) {
	cli := &clienteFixo{resposta: `{"final_text":"Ok, pode mandar."}`}
	rt, saida, hist := runtimeDoPolish(t, abrirCom(cli))
	anterior := EstadoPolish{Motivo: resolver.MotivoPrazo, Quando: time.Unix(1, 0)}
	rt.ultimoPolish.Store(&anterior)

	if got := ditarEmPolish(t, rt, 1, "ok pode mandar"); got != "ok pode mandar" {
		t.Fatalf("texto = %q, quero o do Smart", got)
	}
	if cli.chamadas.Load() != 0 {
		t.Fatalf("o ditado curto foi ao provedor (%d chamada[s])", cli.chamadas.Load())
	}
	if l := hist.unica(t); l.Mode != "smart" {
		t.Fatalf("histórico: modo %q, quero smart", l.Mode)
	}
	if e := rt.UltimoPolish(); e != anterior {
		t.Fatalf("último Polish = %+v; o curto não pode mexer nele (era %+v)", e, anterior)
	}
	if l := linhasDoPolish(saida.String()); len(l) != 1 || !strings.Contains(l[0], "desfecho=curto") || !strings.Contains(l[0], "tokens_entrada=0") {
		t.Fatalf("linha do Polish: %v", l)
	}
}

// TestPolish_ClienteLentoSmartNoPrazoTardioDescartado: um cliente que
// ignora o ctx e responde DEPOIS do prazo, com uma reescrita que as guardas
// aceitariam. O texto do Smart sai no prazo, com o motivo prazo, e a
// resposta tardia não chega ao laço — nem depois.
func TestPolish_ClienteLentoSmartNoPrazoTardioDescartado(t *testing.T) {
	segurar := make(chan struct{})
	f := novoPolishFalso(passoPolish{segurar: segurar, texto: "TARDIO"})
	rt, saida, hist := runtimeDoPolish(t, f.abrir)
	const prazo = 120 * time.Millisecond
	rt.prazoDoPolish = prazo

	inicio := time.Now()
	pedirPolish(t, rt, 1, ditadoLongo)
	<-f.entrou
	p := receberPolido(t, rt, 2*time.Second)
	decorrido := time.Since(inicio)

	if p.res.Reescrito || p.res.FinalText != ditadoLongo || p.res.MotivoFallback != resolver.MotivoPrazo {
		t.Fatalf("resultado no prazo = %+v; quero o Smart com o motivo prazo", p.res)
	}
	if decorrido < prazo || decorrido > prazo+time.Second {
		t.Fatalf("o Smart chegou em %s; quero logo depois do prazo de %s", decorrido, prazo)
	}
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: textoDe(t, rt.concluirPolish(p))})
	if l := hist.unica(t); l.Mode != "smart" {
		t.Fatalf("histórico: %q, quero smart", l.Mode)
	}
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoPrazo {
		t.Fatalf("último motivo = %q, quero prazo", e.Motivo)
	}
	// A linha diz quanto se esperou: o prazo inteiro, e nenhum token (a
	// chamada foi cortada).
	linhas := linhasDoPolish(saida.String())
	if len(linhas) != 1 {
		t.Fatalf("%d linha(s) de Polish:\n%s", len(linhas), saida.String())
	}
	campos := camposDaLinha(linhas[0])
	if ms, err := strconv.Atoi(campos["latencia_ms"]); err != nil || ms < 100 || campos["desfecho"] != "prazo" || campos["tokens_entrada"] != "0" {
		t.Fatalf("linha do prazo: latência %q (%v), desfecho %q, tokens %q", campos["latencia_ms"], err, campos["desfecho"], campos["tokens_entrada"])
	}

	// A resposta tardia chega agora — e morre na goroutine dela: a
	// goroutine da chamada entrega num canal com folga e sai, em vez de
	// ficar presa para sempre (uma por ditado que estourou o prazo).
	antes := runtime.NumGoroutine()
	close(segurar)
	select {
	case tardio := <-rt.polidos:
		t.Fatalf("o Polish tardio chegou ao laço: %+v", tardio.res)
	case <-time.After(150 * time.Millisecond):
	}
	for prazo := time.Now().Add(2 * time.Second); runtime.NumGoroutine() >= antes; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(prazo) {
			t.Fatalf("a goroutine da chamada tardia não saiu (%d goroutines, eram %d)", runtime.NumGoroutine(), antes)
		}
	}
}

// TestPolish_CtxTemOPrazoDoComecoDaResolucao: o ctx da chamada vence no
// começo da resolução mais o teto — o Smart já sai dele. E um Smart que
// gastou o teto inteiro nem abre o Polidor (nem lê a chave).
func TestPolish_CtxTemOPrazoDoComecoDaResolucao(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, _, _ := runtimeDoPolish(t, f.abrir)
	rt.prazoDoPolish = 2 * time.Second

	antes := time.Now()
	pedirPolish(t, rt, 1, ditadoLongo)
	ctx := <-f.entrou
	limite, ok := ctx.Deadline()
	if !ok || limite.Before(antes.Add(2*time.Second)) || limite.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("prazo do ctx = %v (%v); quero o começo da resolução + 2 s", limite, ok)
	}
	receberPolido(t, rt, 2*time.Second)

	// Um Smart lento: o começo da resolução já passou do teto.
	rt.prazoDoPolish = time.Millisecond
	rt.polir(2, resolver.ResolveRequest{}, resolver.ResolveResult{FinalText: ditadoLongo}, time.Now().Add(-time.Second))
	if p := receberPolido(t, rt, 2*time.Second); p.res.MotivoFallback != resolver.MotivoPrazo || p.res.FinalText != ditadoLongo {
		t.Fatalf("com o teto gasto pelo Smart: %+v; quero o Smart com prazo", p.res)
	}
	if f.abertos.Load() != 1 {
		t.Fatalf("o Polidor foi aberto %d vez(es); com o teto gasto, não abre", f.abertos.Load())
	}
}

// TestPolish_ResolverNaoBloqueiaOLaco: com a chamada presa, resolver volta
// na hora — é o que mantém o laço consumindo teclas (a prova com o laço
// inteiro é TestRun_PolishNaoPrendeOLaco).
func TestPolish_ResolverNaoBloqueiaOLaco(t *testing.T) {
	segurar := make(chan struct{})
	defer close(segurar)
	f := novoPolishFalso(passoPolish{segurar: segurar, texto: ditadoPolido})
	rt, _, _ := runtimeDoPolish(t, f.abrir)
	rt.prazoDoPolish = 5 * time.Second

	voltou := make(chan dictation.Event, 1)
	go func() {
		voltou <- rt.resolver(dictation.ResolveEffect{Attempt: 1, ASRResult: dictation.ASRResultDTO{Text: ditadoLongo}})
	}()
	select {
	case ev := <-voltou:
		if ev != nil {
			t.Fatalf("no Polish, resolver respondeu na hora (%T)", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resolver não voltou com a chamada do Polish presa")
	}
	<-f.entrou
}

// TestPolish_LiteralESmartNaoChamamNinguem: nos outros modos o Polidor nem
// é aberto, nenhuma conexão sai (o provedor em loopback conta) e o log não
// ganha linha de Polish.
func TestPolish_LiteralESmartNaoChamamNinguem(t *testing.T) {
	prov, conexoes := provedorQueConta(t)
	var abertos atomic.Int32
	abrir := func(provedor, modelo string, contar func(int, int)) (Polidor, string) {
		abertos.Add(1)
		return abrirComIA(t, prov, "sk-teste-0000")(provedor, modelo, contar)
	}
	for _, modo := range []Preferencias{
		{HoldThreshold: 700 * time.Millisecond, Provedor: "openai", Modelo: "m"},
		{HoldThreshold: 700 * time.Millisecond, Literal: true, Provedor: "openai", Modelo: "m"},
		// Literal ganha de Polish.
		{HoldThreshold: 700 * time.Millisecond, Literal: true, Polish: true, Provedor: "openai", Modelo: "m"},
	} {
		rt, saida, hist := runtimeDoPolish(t, abrir)
		rt.Ajustar(modo)
		rt.aplicarAjuste()
		ev := rt.resolver(dictation.ResolveEffect{Attempt: 1, ASRResult: dictation.ASRResultDTO{Text: ditadoLongo}})
		if ev == nil {
			t.Fatalf("%+v: resolver não respondeu na hora", modo)
		}
		rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: textoDe(t, ev)})
		if l := linhasDoPolish(saida.String()); len(l) != 0 {
			t.Fatalf("%+v: linha de Polish no log: %v", modo, l)
		}
		quero := "smart"
		if modo.Literal {
			quero = "literal"
		}
		if l := hist.unica(t); l.Mode != quero {
			t.Fatalf("%+v: histórico %q, quero %q", modo, l.Mode, quero)
		}
		select {
		case p := <-rt.polidos:
			t.Fatalf("%+v: um Polish voltou ao laço: %+v", modo, p.res)
		default:
		}
	}
	if abertos.Load() != 0 || conexoes.Load() != 0 {
		t.Fatalf("Polidor aberto %d vez(es), %d conexão(ões) ao provedor; quero zero", abertos.Load(), conexoes.Load())
	}
}

// TestPolish_SemChaveNenhumPedido: com o cliente HTTP de verdade e a chave
// ausente, o motivo é sem_chave, nenhuma conexão sai, e a tela o recebe.
func TestPolish_SemChaveNenhumPedido(t *testing.T) {
	prov, conexoes := provedorQueConta(t)
	cliente := clienteSoLoopback(t, prov)
	abrir := func(provedor, modelo string, contar func(int, int)) (Polidor, string) {
		return &resolver.PolishResolver{Smart: resolver.NewSmartResolver(nil), Client: &ia.Polidor{
			Cliente: cliente,
			Chamada: ia.Chamada{Provedor: provedor, Modelo: modelo},
			Chave:   func(context.Context) (string, error) { return "", errors.New("chaves: nenhuma chave guardada") },
		}}, "none"
	}
	rt, saida, hist := runtimeDoPolish(t, abrir)

	if got := ditarEmPolish(t, rt, 1, ditadoLongo); got != ditadoLongo {
		t.Fatalf("texto = %q, quero o do Smart", got)
	}
	if conexoes.Load() != 0 {
		t.Fatalf("%d conexão(ões) sem chave", conexoes.Load())
	}
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoSemChave {
		t.Fatalf("último motivo = %q, quero sem_chave", e.Motivo)
	}
	if l := hist.unica(t); l.Mode != "smart" {
		t.Fatalf("histórico %q, quero smart", l.Mode)
	}
	if l := linhasDoPolish(saida.String()); len(l) != 1 || !strings.Contains(l[0], "desfecho=sem_chave") {
		t.Fatalf("linha do Polish: %v", l)
	}
}

// TestPolish_SemPolidorCaiNoSmart: Polish escolhido sem UsarPolish (ou com
// o Polidor nulo): sem_cliente, e o ditado sai como Smart.
func TestPolish_SemPolidorCaiNoSmart(t *testing.T) {
	for nome, abrir := range map[string]AbrirPolidor{
		"sem UsarPolish": nil,
		"Polidor nulo":   func(string, string, func(int, int)) (Polidor, string) { return nil, "none" },
	} {
		rt, _, hist := runtimeDoPolish(t, abrir)
		if got := ditarEmPolish(t, rt, 1, ditadoLongo); got != ditadoLongo {
			t.Fatalf("%s: texto %q", nome, got)
		}
		if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoSemCliente || hist.unica(t).Mode != "smart" {
			t.Fatalf("%s: motivo %q, modo %q", nome, e.Motivo, hist.unica(t).Mode)
		}
	}
}

// TestPolish_ModoDeOutraTentativaNaoVale: o "aceito" de uma tentativa não
// marca a seguinte, e é consumido ao gravar.
func TestPolish_ModoDeOutraTentativaNaoVale(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, _, hist := runtimeDoPolish(t, f.abrir)
	pedirPolish(t, rt, 1, ditadoLongo)
	rt.concluirPolish(receberPolido(t, rt, 2*time.Second))

	// A tentativa 1 acabou sem histórico (o watchdog), e a 2 chega com
	// texto próprio sem ter passado pelo Polish aceito.
	rt.brutoDaTentativa.attempt, rt.brutoDaTentativa.texto = 2, "outro ditado"
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 2, Outcome: dictation.OutcomeSuccess, Text: "outro ditado"})
	if l := hist.unica(t); l.Mode != "smart" {
		t.Fatalf("modo da tentativa 2 = %q: herdou o aceito da 1", l.Mode)
	}
	if rt.polishDaTentativa.attempt != 0 || rt.polishDaTentativa.reescrito {
		t.Fatalf("o desfecho não foi consumido: %+v", rt.polishDaTentativa)
	}
}

// TestPolish_UltimoMotivo: a sequência que a tela vai ver — nenhum Polish
// ainda, uma falha, um acerto que a limpa.
// smartLento é um Smart que gasta tempo: o que um dicionário grande faria.
type smartLento struct{ gasto time.Duration }

func (s smartLento) Resolve(_ context.Context, req resolver.ResolveRequest) (resolver.ResolveResult, error) {
	time.Sleep(s.gasto)
	return resolver.ResolveResult{Mode: resolver.ModeSmart, RawText: req.ASRResult.Text, FinalText: req.ASRResult.Text}, nil
}

// TestPolish_SmartLentoSaiDoTeto: o tempo do Smart sai do teto da rede —
// o prazo é ancorado no começo da resolução, não no começo da chamada. Se
// fosse o contrário, um Smart lento empurraria a resposta para perto do
// watchdog do núcleo.
func TestPolish_SmartLentoSaiDoTeto(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, _, _ := runtimeDoPolish(t, f.abrir)
	rt.UsarResolver(smartLento{gasto: 80 * time.Millisecond}, "mauro")
	rt.prazoDoPolish = 200 * time.Millisecond

	pedirPolish(t, rt, 1, ditadoLongo)
	p := receberPolido(t, rt, 2*time.Second)
	if !p.res.Reescrito {
		t.Fatalf("resultado = %+v, quero o Polish aceito", p.res)
	}
	if p.prazo > 170*time.Millisecond {
		t.Fatalf("sobraram %s para a rede depois de um Smart de 80 ms num teto de 200 ms: o Smart não saiu do teto", p.prazo)
	}
}

// TestPolish_EncerramentoNaoEPrazo: com o Agent encerrando (o ctx de Run
// cancelado), o motivo é transporte — prazo é o provedor demorando, e a
// tela diria a coisa errada. E nem abre o Polidor.
func TestPolish_EncerramentoNaoEPrazo(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, _, _ := runtimeDoPolish(t, f.abrir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt.ctxRun = ctx

	pedirPolish(t, rt, 1, ditadoLongo)
	p := receberPolido(t, rt, 2*time.Second)
	if p.res.MotivoFallback != resolver.MotivoTransporte || p.res.FinalText != ditadoLongo {
		t.Fatalf("resultado com o Agent encerrando = %+v, quero o Smart com o motivo transporte", p.res)
	}
	if f.abertos.Load() != 0 {
		t.Fatalf("o Polidor foi aberto %d vez(es) com o Agent encerrando", f.abertos.Load())
	}
}

func TestPolish_UltimoMotivo(t *testing.T) {
	var rt Runtime
	if e := rt.UltimoPolish(); e != (EstadoPolish{}) {
		t.Fatalf("antes de qualquer Polish: %+v, quero vazio", e)
	}
	rt.log = slog.New(slog.DiscardHandler)
	rt.concluirPolish(polidoPronto{attempt: 1, res: resolver.ResolveResult{MotivoFallback: resolver.MotivoSemCredito}})
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoSemCredito || e.Quando.IsZero() {
		t.Fatalf("depois da falha: %+v", e)
	}
	rt.concluirPolish(polidoPronto{attempt: 2, res: resolver.ResolveResult{Reescrito: true}})
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoNenhum || e.Quando.IsZero() {
		t.Fatalf("depois do acerto: %+v, quero o motivo limpo", e)
	}
}

// TestPolish_EntregaNaoPrendeQuandoRunSai: com Run fora do ar (nada lendo
// rt.polidos), a goroutine do Polish não fica presa para sempre tentando
// entregar: pararCapturas, que Run fecha ao sair, a solta.
func TestPolish_EntregaNaoPrendeQuandoRunSai(t *testing.T) {
	f := novoPolishFalso(passoPolish{texto: ditadoPolido})
	rt, _, _ := runtimeDoPolish(t, f.abrir)
	rt.polidos = make(chan polidoPronto) // sem buffer: a entrega bloqueia
	rt.pararCapturas = make(chan struct{})

	pedirPolish(t, rt, 1, ditadoLongo)
	<-f.entrou
	time.Sleep(50 * time.Millisecond) // a goroutine chega à entrega
	close(rt.pararCapturas)
	select {
	case p := <-rt.polidos:
		t.Fatalf("entregou %+v depois de Run ter saído", p.res)
	case <-time.After(100 * time.Millisecond):
	}
}

// --- o provedor falso em loopback, para os testes com internal/ia ---

// travaDeRede deixa passar só pedido para a própria máquina.
type travaDeRede struct {
	base http.RoundTripper
	fora *atomic.Int32
}

func (g travaDeRede) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		if req.Body != nil {
			req.Body.Close()
		}
		g.fora.Add(1)
		return nil, errors.New("rede: o teste tentou sair da máquina")
	}
	return g.base.RoundTrip(req)
}

// clienteSoLoopback é o ia.Cliente apontado para prov, com a trava.
func clienteSoLoopback(t *testing.T, prov *httptest.Server) *ia.Cliente {
	t.Helper()
	var fora atomic.Int32
	t.Cleanup(func() {
		if n := fora.Load(); n > 0 {
			t.Errorf("%d pedido(s) tentaram sair da máquina", n)
		}
	})
	return &ia.Cliente{
		BaseOpenAI: prov.URL, BaseAnthropic: prov.URL,
		HTTP: &http.Client{Transport: travaDeRede{base: ia.NovoTransporteHTTP1(), fora: &fora}},
	}
}

// provedorQueConta é um provedor que só conta conexões e responde 500.
func provedorQueConta(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	prov := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	prov.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			n.Add(1)
		}
	}
	prov.Start()
	t.Cleanup(prov.Close)
	return prov, &n
}

// abrirComIA monta o Polish como cmd/agent o monta (PolishResolver sobre
// ia.Polidor), com a chave dada, apontado para prov.
func abrirComIA(t *testing.T, prov *httptest.Server, chave string) AbrirPolidor {
	t.Helper()
	cliente := clienteSoLoopback(t, prov)
	return func(provedor, modelo string, contar func(int, int)) (Polidor, string) {
		return &resolver.PolishResolver{Smart: resolver.NewSmartResolver(nil), Client: &ia.Polidor{
			Cliente: cliente,
			Chamada: ia.Chamada{Provedor: provedor, Modelo: modelo},
			Chave:   func(context.Context) (string, error) { return chave, nil },
			Uso:     func(tk ia.Tokens) { contar(tk.Entrada, tk.Saida) },
		}}, "none"
	}
}

// TestPolish_CanarioDePrivacidadeNoLog: o ditado, os warnings do modelo, o
// corpo de erro do provedor e a chave passam por TODOS os desfechos do
// Polish — aceito, chave recusada com eco, guarda que reprova, pedido
// recusado com a mensagem ecoando o ditado — e o log não pode ter nenhum
// deles. Só números e identificadores.
func TestPolish_CanarioDePrivacidadeNoLog(t *testing.T) {
	const (
		palavraCanario = "zabumbaqx"
		avisoCanario   = "aviso-kwxqvj"
		corpoCanario   = "corpo-de-erro-pqzmw"
		chave          = "sk-proj-QwErTyVaZaMeNtOzZ-XyXyKpKpWvWv-kLmNoPqR-fImJ"
	)
	ditado := "o cliente " + palavraCanario + " mandou o balancete de agosto"
	var n atomic.Int32
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(corpo), palavraCanario) {
			t.Errorf("o provedor não recebeu o ditado: o teste não exercitou o caminho")
		}
		w.Header().Set("X-Eco", r.Header.Get("Authorization"))
		resposta := func(finalText string) {
			saida, _ := json.Marshal(map[string]any{"final_text": finalText, "warnings": []string{avisoCanario + " " + palavraCanario}})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "completed",
				"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(saida)}}}},
				"usage":  map[string]any{"input_tokens": 850, "output_tokens": 33},
			})
		}
		switch n.Add(1) {
		case 1: // aceito, com warnings que o modelo não devia mandar
			resposta("O cliente " + palavraCanario + " mandou o balancete de agosto.")
		case 2: // a chave recusada, com eco da chave e do ditado
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":"%s: chave %s ditado %s"}}`, corpoCanario, r.Header.Get("Authorization"), palavraCanario)
		case 3: // a guarda reprova
			resposta("O cliente " + palavraCanario + " não mandou o balancete de agosto.")
		default: // pedido recusado, com a mensagem ecoando o ditado
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"type":"invalid_request_error","message":"%s %s"}}`, corpoCanario, palavraCanario)
		}
	}))
	t.Cleanup(prov.Close)

	rt, saida, hist := runtimeDoPolish(t, abrirComIA(t, prov, chave))
	for i := dictation.AttemptID(1); i <= 4; i++ {
		ditarEmPolish(t, rt, i, ditado)
	}

	if n.Load() != 4 || len(hist.linhas) != 4 || hist.linhas[0].Mode != "polish" || hist.linhas[2].Mode != "smart" {
		t.Fatalf("pedidos %d, linhas %d: o teste não percorreu os quatro desfechos", n.Load(), len(hist.linhas))
	}
	log := saida.String()
	linhas := linhasDoPolish(log)
	if len(linhas) != 4 {
		t.Fatalf("%d linha(s) de Polish, quero uma por ditado:\n%s", len(linhas), log)
	}
	for i, desfecho := range []string{"aceito", "chave_recusada", "palavras", "pedido_recusado"} {
		if !strings.Contains(linhas[i], "desfecho="+desfecho) {
			t.Errorf("linha %d sem desfecho=%s: %s", i+1, desfecho, linhas[i])
		}
		// Só os campos permitidos, todos presentes: um campo a mais é
		// um canal a mais para texto.
		var chaves []string
		for k := range camposDaLinha(linhas[i]) {
			chaves = append(chaves, k)
		}
		slices.Sort(chaves)
		quero := slices.Sorted(slices.Values(camposDoPolish))
		if !slices.Equal(chaves, quero) {
			t.Errorf("linha %d com os campos %v; quero exatamente %v", i+1, chaves, quero)
		}
	}
	for _, proibido := range []string{palavraCanario, avisoCanario, corpoCanario, "balancete", "final_text", "warnings"} {
		if strings.Contains(log, proibido) {
			t.Errorf("%q no log:\n%s", proibido, log)
		}
	}
	for i := 0; i+8 <= len(chave); i++ {
		if strings.Contains(log, chave[i:i+8]) {
			t.Fatalf("pedaço da chave (%q) no log:\n%s", chave[i:i+8], log)
		}
	}
	// O último motivo também é só o enum.
	if e := rt.UltimoPolish(); e.Motivo != resolver.MotivoPedidoRecusado {
		t.Fatalf("último motivo = %q", e.Motivo)
	}
}

// --- o laço de verdade (Run) ---

// tecladoFalso faz o papel do hook: eventos SEM buffer, para que um envio
// só termine quando Run o recebe — é a prova de que o laço está
// consumindo teclas.
type tecladoFalso struct {
	eventos chan dictation.RawKeyEvent
	taps    atomic.Int32
}

func (k *tecladoFalso) Events() <-chan dictation.RawKeyEvent { return k.eventos }
func (k *tecladoFalso) SynthesizeTap() error                 { k.taps.Add(1); return nil }

func (k *tecladoFalso) tecla(t *testing.T, down bool) {
	t.Helper()
	select {
	case k.eventos <- dictation.RawKeyEvent{Down: down}:
	case <-time.After(time.Second):
		t.Fatalf("o laço não consumiu a tecla (down=%v) em 1 s: está preso", down)
	}
}

// insercaoRegistrada é o inserter dos testes do laço: não toca o sistema,
// só conta o que entraria no campo.
type insercaoRegistrada struct{ textos chan string }

func (i *insercaoRegistrada) Insert(text string) inserter.Result {
	i.textos <- text
	return inserter.Result{Success: true, Method: inserter.MethodClipboard}
}

// historicoCanal entrega cada linha gravada ao teste, que roda em outra
// goroutine que Run.
type historicoCanal struct{ linhas chan storage.Transcription }

func (h *historicoCanal) Put(t storage.Transcription) error { h.linhas <- t; return nil }

// laco é o runtime inteiro rodando Run, com o relógio do sistema (o limiar
// do hold é o mínimo, 300 ms), o hook, o microfone, o ASR, a pílula e o
// inserter falsos, e o Polish ligado sobre um polishFalso.
type laco struct {
	rt     *Runtime
	tec    *tecladoFalso
	ins    *insercaoRegistrada
	hist   *historicoCanal
	saida  *escritaSegura
	cancel context.CancelFunc
	fim    chan error
}

func novoLaco(t *testing.T, abrir AbrirPolidor, prazo time.Duration) *laco {
	t.Helper()
	machine, err := dictation.NewMachine(dictation.Config{
		Key:           dictation.KeyF,
		HoldThreshold: dictation.MinHoldThreshold,
		MaxRecording:  dictation.DefaultMaxRecording,
	})
	if err != nil {
		t.Fatal(err)
	}
	l := &laco{
		tec:   &tecladoFalso{eventos: make(chan dictation.RawKeyEvent)},
		ins:   &insercaoRegistrada{textos: make(chan string, 8)},
		hist:  &historicoCanal{linhas: make(chan storage.Transcription, 8)},
		saida: &escritaSegura{},
		fim:   make(chan error, 1),
	}
	l.rt = &Runtime{
		machine:       machine,
		clk:           clock.NewSystem(),
		hook:          l.tec,
		audio:         &microfoneFalso{pronto: true},
		overlay:       &pilulaFalsa{},
		focus:         fakeFocus{},
		inserter:      l.ins,
		asr:           &asrFalso{texto: ditadoLongo},
		log:           slog.New(slog.NewTextHandler(l.saida, nil)),
		timers:        make(map[timerKey]clock.Timer),
		pending:       make(chan dictation.Event, pendingBuffer),
		insercoes:     make(chan dictation.InsertEffect, 1),
		vaga:          vagaLivre(),
		capturas:      make(chan capturaPronta, 1),
		pararCapturas: make(chan struct{}),
		polidos:       make(chan polidoPronto, 1),
		bipar:         func() error { return nil },
		prazoDoPolish: prazo,
	}
	l.rt.UsarHistorico(l.hist, provedorDeTeste, modeloDeTeste, "mauro")
	l.rt.UsarResolver(resolver.NewSmartResolver(nil), "mauro")
	l.rt.UsarPolish(abrir)
	l.rt.Ajustar(Preferencias{HoldThreshold: dictation.MinHoldThreshold, Polish: true, Provedor: "openai", Modelo: "modelo-x"})

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	go func() { l.fim <- l.rt.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-l.fim:
		case <-time.After(2 * time.Second):
			t.Error("Run não saiu depois do cancelamento")
		}
	})
	return l
}

// esperarNoLog espera o log ter n ocorrências de trecho.
func (l *laco) esperarNoLog(t *testing.T, trecho string, n int) {
	t.Helper()
	for prazo := time.Now().Add(3 * time.Second); time.Now().Before(prazo); time.Sleep(2 * time.Millisecond) {
		if strings.Count(l.saida.String(), trecho) >= n {
			return
		}
	}
	t.Fatalf("o log não chegou a %d× %q:\n%s", n, trecho, l.saida.String())
}

// segurarESoltar é um hold de verdade: a tecla desce, o limiar vence
// (a pílula entra em "Gravando") e a tecla sobe.
func (l *laco) segurarESoltar(t *testing.T, ditado int) {
	t.Helper()
	l.tec.tecla(t, true)
	l.esperarNoLog(t, "fase=recording", ditado)
	l.tec.tecla(t, false)
}

func (l *laco) inserido(t *testing.T) string {
	t.Helper()
	select {
	case s := <-l.ins.textos:
		return s
	case <-time.After(3 * time.Second):
		t.Fatalf("nada foi inserido em 3 s:\n%s", l.saida.String())
		return ""
	}
}

func (l *laco) gravado(t *testing.T) storage.Transcription {
	t.Helper()
	select {
	case h := <-l.hist.linhas:
		return h
	case <-time.After(3 * time.Second):
		t.Fatalf("nada foi gravado no histórico em 3 s:\n%s", l.saida.String())
		return storage.Transcription{}
	}
}

// TestRun_PolishNaoPrendeOLaco é a prova com o laço inteiro (ADR-016,
// decisão 5): enquanto a chamada do Polish espera — num cliente que nem
// respeita o ctx —, Run continua consumindo keydown e keyup; o texto do
// Smart entra no prazo e o Polish tardio é descartado; e o ditado
// seguinte é um hold de verdade, que desta vez sai polido.
func TestRun_PolishNaoPrendeOLaco(t *testing.T) {
	segurar := make(chan struct{})
	f := novoPolishFalso(passoPolish{segurar: segurar, texto: "TARDIO"}, passoPolish{texto: ditadoPolido})
	const prazo = 400 * time.Millisecond
	l := novoLaco(t, f.abrir, prazo)

	l.segurarESoltar(t, 1)
	select {
	case <-f.entrou:
	case <-time.After(3 * time.Second):
		t.Fatalf("a chamada do Polish não começou:\n%s", l.saida.String())
	}
	comeco := time.Now()
	// Durante a espera: a tecla desce e sobe, e o laço as consome (o
	// núcleo as ignora, porque há uma tentativa em curso).
	l.tec.tecla(t, true)
	l.tec.tecla(t, false)
	if d := time.Since(comeco); d > prazo {
		t.Fatalf("as teclas levaram %s para ser consumidas: o laço esperou a rede", d)
	}

	if got := l.inserido(t); got != ditadoLongo {
		t.Fatalf("inserido %q, quero o texto do Smart", got)
	}
	if d := time.Since(comeco); d > prazo+time.Second {
		t.Fatalf("o Smart entrou %s depois de a chamada começar; o prazo é %s", d, prazo)
	}
	if h := l.gravado(t); h.Mode != "smart" || h.FinalText != ditadoLongo {
		t.Fatalf("histórico: modo %q, final %q", h.Mode, h.FinalText)
	}
	if e := l.rt.UltimoPolish(); e.Motivo != resolver.MotivoPrazo {
		t.Fatalf("último motivo = %q, quero prazo", e.Motivo)
	}
	close(segurar) // o Polish tardio responde agora, e não pode entrar

	l.segurarESoltar(t, 2)
	if got := l.inserido(t); got != ditadoPolido {
		t.Fatalf("segundo ditado: inserido %q, quero o polido (o tardio do primeiro não pode entrar)", got)
	}
	if h := l.gravado(t); h.Mode != "polish" || h.FinalText != ditadoPolido {
		t.Fatalf("segundo ditado no histórico: modo %q, final %q", h.Mode, h.FinalText)
	}
	select {
	case s := <-l.ins.textos:
		t.Fatalf("uma terceira inserção (%q): o Polish tardio entrou", s)
	case <-time.After(100 * time.Millisecond):
	}
	if n := l.tec.taps.Load(); n != 0 {
		t.Fatalf("%d tap(s) sintetizado(s): um hold virou tap", n)
	}
	if n := len(linhasDoPolish(l.saida.String())); n != 2 {
		t.Fatalf("%d linha(s) de Polish, quero 2:\n%s", n, l.saida.String())
	}
}

// TestRun_EncerrarNoMeioDaChamadaNaoTrava: o Agent fecha com a chamada do
// Polish em voo, num cliente que ignora o ctx. Run sai na hora, o ctx da
// chamada (derivado do de Run) é cancelado, e nada é inserido depois.
func TestRun_EncerrarNoMeioDaChamadaNaoTrava(t *testing.T) {
	segurar := make(chan struct{})
	defer close(segurar)
	f := novoPolishFalso(passoPolish{segurar: segurar, texto: ditadoPolido})
	l := novoLaco(t, f.abrir, 10*time.Second)

	l.segurarESoltar(t, 1)
	var ctx context.Context
	select {
	case ctx = <-f.entrou:
	case <-time.After(3 * time.Second):
		t.Fatal("a chamada do Polish não começou")
	}

	l.cancel()
	select {
	case err := <-l.fim:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run saiu com %v", err)
		}
		l.fim <- err // devolve para o Cleanup
	case <-time.After(time.Second):
		t.Fatal("Run não saiu com a chamada do Polish em voo")
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("ctx da chamada: %v, quero cancelado pelo encerramento", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("o ctx da chamada não foi cancelado com o de Run")
	}
	select {
	case s := <-l.ins.textos:
		t.Fatalf("inseriu %q depois do encerramento", s)
	case <-time.After(100 * time.Millisecond):
	}
}

// Garante que o dublê do laço segue o contrato do hook de produção.
var _ teclado = (*tecladoFalso)(nil)
