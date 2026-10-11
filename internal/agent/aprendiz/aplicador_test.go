package aprendiz

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// O Aplicador sobre um dicionario.json de verdade, num t.TempDir(), com um
// runtime falso (Config.Trocar): nada aqui toca o bin\, abre áudio, digita ou
// usa a área de transferência.

// O aplicador é o que o sync espera, na varredura e no Receber.
var _ syncengine.DicionarioDoAprendido = (*Aplicador)(nil)

const dicionarioDoTeste = `{
  "usuario": "mauro",
  "dominios": {
    "contabil": [
      "SistemaContabil.exe"
    ]
  },
  "entradas": [
    {
      "canonica": "ECD",
      "prioridade": 90
    },
    {
      "canonica": "DIRF",
      "prioridade": 90,
      "dominio": "contabil"
    }
  ]
}
`

// trocas é o runtime falso: guarda cada Troca, na ordem.
type trocas struct {
	mu    sync.Mutex
	itens []Troca
}

func (t *trocas) trocar(x Troca) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.itens = append(t.itens, x)
}

func (t *trocas) todas() []Troca {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Troca(nil), t.itens...)
}

type montado struct {
	a       *Aplicador
	rt      *trocas
	caminho string
	log     *bytes.Buffer
	logMu   *sync.Mutex
}

type escritorTravado struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (e escritorTravado) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.b.Write(p)
}

func montar(t *testing.T, conteudo string, cfg Config) montado {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
	m := montado{rt: &trocas{}, caminho: caminho, log: &bytes.Buffer{}, logMu: &sync.Mutex{}}
	cfg.Caminho = caminho
	if cfg.Usuario == "" {
		cfg.Usuario = "mauro"
	}
	cfg.Trocar = m.rt.trocar
	cfg.Log = slog.New(slog.NewTextHandler(escritorTravado{m.logMu, m.log}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m.a = Novo(cfg)
	return m
}

func (m montado) bytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(m.caminho)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (m montado) logs() string {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	return m.log.String()
}

// pares devolve um escolher que manda sempre os mesmos pares.
func pares(ps ...dictionary.ParDaNuvem) func(dictionary.Semente) []dictionary.ParDaNuvem {
	return func(dictionary.Semente) []dictionary.ParDaNuvem { return ps }
}

func resolve(t *testing.T, s *resolver.SmartResolver, texto string, dominio *string) string {
	t.Helper()
	out, err := s.Resolve(context.Background(), resolver.ResolveRequest{UserID: "mauro", ASRResult: asr.ASRResult{Text: texto},
		CloudPolicy: resolver.CloudDisabled, Domain: dominio})
	if err != nil {
		t.Fatal(err)
	}
	return out.FinalText
}

// O lote do sync grava uma vez e troca o dicionário e o prompt uma vez; o lote
// que não muda nada (a varredura ociosa, o convergente) não grava nem troca.
func TestAplicador_AplicarParesGravaETrocaUmaVez(t *testing.T) {
	m := montar(t, dicionarioDoTeste, Config{})
	contabil := "contabil"

	var intencao []dictionary.ResultadoDoPar
	s, lote, err := m.a.AplicarPares(pares(
		dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"},
		dictionary.ParDaNuvem{Variante: "dirfi", Termo: "DIRF", Restrito: true, Dominio: "contabil"},
	), func(l dictionary.Lote) error {
		if len(m.rt.todas()) != 0 {
			t.Error("a troca veio antes da gravação")
		}
		intencao = l.Resultados
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !lote.Mudou || len(intencao) != 2 {
		t.Fatalf("o lote: %+v; a intenção viu %d", lote, len(intencao))
	}
	for _, r := range lote.Resultados {
		if r.Situacao != dictionary.SituacaoAplicado {
			t.Errorf("%+v: quero aplicado", r)
		}
	}
	ts := m.rt.todas()
	if len(ts) != 1 {
		t.Fatalf("o runtime recebeu %d trocas, quero 1", len(ts))
	}
	if got := resolve(t, ts[0].Smart, "esse efe", nil); got != "ECF" {
		t.Errorf("o Smart novo: %q", got)
	}
	if got := resolve(t, ts[0].Smart, "dirfi", &contabil); got != "DIRF" {
		t.Errorf("o Smart novo, no domínio: %q", got)
	}
	if got := resolve(t, ts[0].Smart, "dirfi", nil); got == "DIRF" {
		t.Error("o par restrito agiu fora do domínio: a troca alargou")
	}
	// O termo criado pela D2 é AliasOnly: fora do prompt.
	if strings.Contains(ts[0].Vocabulario, "ECF") || !strings.Contains(ts[0].Vocabulario, "ECD") {
		t.Errorf("o prompt: %q", ts[0].Vocabulario)
	}
	if !reflectSemente(t, s, m.bytes(t)) {
		t.Error("a semente devolvida não é a que está no disco")
	}

	// De novo, a mesma coisa: convergente, sem gravar nem trocar.
	antes := m.bytes(t)
	_, lote, err = m.a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"}), func(dictionary.Lote) error {
		t.Error("a intenção sem nada a gravar")
		return nil
	})
	if err != nil || lote.Mudou || lote.Resultados[0].Situacao != dictionary.SituacaoConvergente {
		t.Fatalf("o convergente: %+v, %v", lote, err)
	}
	// E a varredura, com zero pares.
	if _, lote, err = m.a.AplicarPares(nil, nil); err != nil || lote.Mudou || len(lote.Resultados) != 0 {
		t.Fatalf("a varredura ociosa: %+v, %v", lote, err)
	}
	if !bytes.Equal(antes, m.bytes(t)) || len(m.rt.todas()) != 1 {
		t.Errorf("gravou ou trocou sem mudança: %d trocas", len(m.rt.todas()))
	}
}

// reflectSemente diz se s é a semente que está no disco (gravada de novo num
// arquivo ao lado, os bytes são os mesmos).
func reflectSemente(t *testing.T, s dictionary.Semente, noDisco []byte) bool {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "devolvida.json")
	if err := s.Gravar(caminho); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(b, noDisco)
}

// O lote do sync e o clique do painel dividem UMA trava: o Aprender que chega
// no meio do lote espera, relê o arquivo que o lote gravou, e a última troca
// é a dele, com os dois pares.
func TestAplicador_MesmaTravaDoPainel(t *testing.T) {
	m := montar(t, dicionarioDoTeste, Config{})
	entrou, soltar := make(chan struct{}), make(chan struct{})
	feito := make(chan error, 1)
	go func() {
		_, _, err := m.a.AplicarPares(func(dictionary.Semente) []dictionary.ParDaNuvem {
			close(entrou)
			<-soltar
			return []dictionary.ParDaNuvem{{Variante: "esse efe", Termo: "ECF"}}
		}, nil)
		feito <- err
	}()
	<-entrou

	aprendeu := make(chan error, 1)
	go func() {
		_, err := m.a.Aprender(context.Background(), "ECD", "ecedê")
		aprendeu <- err
	}()
	select {
	case err := <-aprendeu:
		t.Fatalf("o Aprender passou pela trava do lote: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(soltar)
	if err := <-feito; err != nil {
		t.Fatal(err)
	}
	if err := <-aprendeu; err != nil {
		t.Fatal(err)
	}
	ts := m.rt.todas()
	if len(ts) != 2 {
		t.Fatalf("%d trocas, quero 2", len(ts))
	}
	ultima := ts[1].Smart
	if resolve(t, ultima, "esse efe", nil) != "ECF" || resolve(t, ultima, "ecedê", nil) != "ECD" {
		t.Error("a última troca não tem os dois pares: o Aprender não releu o que o lote gravou")
	}
}

// A troca do lote também fica sob a trava, e não só a gravação: o Aprender que
// chega enquanto o runtime ainda recebe a troca do lote (o arquivo já gravado,
// o mu do dictionary.Arquivo já solto) espera, e as trocas chegam na ordem das
// gravações, a última com os dois pares. Sem a trava do Aplicador no
// AplicarPares, o Aprender gravaria e trocaria primeiro, e a troca mais velha
// (sem o par dele) chegaria ao runtime por último. O
// TestAplicador_MesmaTravaDoPainel não vê isso: o escolher que ele prende roda
// sob o mu do próprio Arquivo.
//
// Sem gcc nesta máquina não há go test -race: a prova é pela ordem.
func TestAplicador_TrocaDoLoteEsperaATravaEAMaisNovaFicaPorUltimo(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(caminho, []byte(dicionarioDoTeste), 0o644); err != nil {
		t.Fatal(err)
	}
	// O runtime falso prende a PRIMEIRA troca (a do lote, depois de gravar)
	// até o teste soltar; as outras passam direto. A lista é a ordem em que
	// as trocas chegaram ao runtime.
	var (
		mu       sync.Mutex
		chamadas int
		chegadas []Troca
	)
	entrou, soltar := make(chan struct{}), make(chan struct{})
	var umaVez sync.Once
	liberar := func() { umaVez.Do(func() { close(soltar) }) }
	t.Cleanup(liberar)
	a := Novo(Config{Caminho: caminho, Usuario: "mauro", Trocar: func(x Troca) {
		mu.Lock()
		chamadas++
		primeira := chamadas == 1
		mu.Unlock()
		if primeira {
			close(entrou)
			<-soltar
		}
		mu.Lock()
		chegadas = append(chegadas, x)
		mu.Unlock()
	}})

	feito := make(chan error, 1)
	go func() {
		_, _, err := a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"}), nil)
		feito <- err
	}()
	select {
	case <-entrou:
	case <-time.After(5 * time.Second):
		t.Fatal("o lote não pediu a troca")
	}

	aprendeu := make(chan error, 1)
	go func() {
		_, err := a.Aprender(context.Background(), "ECD", "ecedê")
		aprendeu <- err
	}()
	select {
	case err := <-aprendeu:
		t.Errorf("o Aprender gravou e trocou enquanto o runtime recebia a troca do lote (err %v)", err)
		aprendeu <- err
	case <-time.After(150 * time.Millisecond):
	}
	liberar()
	if err := <-feito; err != nil {
		t.Fatal(err)
	}
	if err := <-aprendeu; err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	ts := append([]Troca(nil), chegadas...)
	mu.Unlock()
	if len(ts) != 2 {
		t.Fatalf("%d trocas, quero 2", len(ts))
	}
	if resolve(t, ts[0].Smart, "ecedê", nil) == "ECD" {
		t.Error("a primeira troca a chegar já tem o par do Aprender: a do lote chegou depois")
	}
	ultima := ts[1]
	if resolve(t, ultima.Smart, "esse efe", nil) != "ECF" || resolve(t, ultima.Smart, "ecedê", nil) != "ECD" {
		t.Error("a última troca entregue ao runtime não tem os dois pares")
	}
	noDisco, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if !reflectSemente(t, ultima.Semente, noDisco) {
		t.Error("a última troca entregue ao runtime não é a do último arquivo gravado")
	}
}

// A troca que falha depois de o lote gravar: o arquivo ficou, o runtime não
// recebe nada, e o erro é ErrGravadoSemTroca (o sync o trata como o lote que
// não terminou). O log só tem o aviso e a ação.
func TestAplicador_GravadoSemTrocaNoSync(t *testing.T) {
	m := montar(t, dicionarioDoTeste, Config{Montar: func(dictionary.Semente) (*resolver.SmartResolver, error) {
		return nil, errors.New("segredo: ECF")
	}})
	_, lote, err := m.a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"}), nil)
	if !errors.Is(err, dictionary.ErrGravadoSemTroca) || !lote.Mudou {
		t.Fatalf("erro = %v, lote %+v; quero ErrGravadoSemTroca com o lote gravado", err, lote)
	}
	if !bytes.Contains(m.bytes(t), []byte("esse efe")) {
		t.Error("o arquivo não foi gravado")
	}
	if len(m.rt.todas()) != 0 {
		t.Error("o runtime recebeu uma troca de uma montagem que falhou")
	}
	log := m.logs()
	if !strings.Contains(log, `msg="aprendizado: gravado, mas o dicionário em memória não foi trocado" acao=sync`) {
		t.Errorf("o log não avisa:\n%s", log)
	}
	for _, proibido := range []string{"esse efe", "ECF", "segredo"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
}

// espelhoFalso é o espelho do sync do gancho: guarda os ids, se a trava do
// Aplicador estava presa quando foi chamado, e se o contexto que ele recebeu
// sobrevive ao corte do pedido.
type espelhoFalso struct {
	a      *Aplicador
	mu     sync.Mutex
	ids    []string
	soltos int
	// cortar é o cancelamento do contexto do pedido: o espelho o chama antes
	// de olhar o contexto que recebeu, como o navegador que fecha a aba
	// depois de o arquivo ser gravado. nil não corta.
	cortar func()
	// cortados conta as chamadas cujo contexto morreu com o corte.
	cortados int
	erro     error
}

func (e *espelhoFalso) SubirVersaoLocal(ctx context.Context, ids []string) error {
	livre := e.a.mu.TryLock()
	if livre {
		e.a.mu.Unlock()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if livre {
		e.soltos++
	}
	if e.cortar != nil {
		e.cortar()
	}
	if ctx.Err() != nil {
		e.cortados++
	}
	e.ids = append(e.ids, ids...)
	return e.erro
}

func (e *espelhoFalso) vistos() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.ids...)
}

// O gancho do painel: o Aprender e o Desfazer que gravaram sobem a versão local
// do id no espelho, no ato e sob a trava, e acordam o laço; o que não gravou,
// o "Adicionar termo" e o lote do sync não passam por ele. O contexto do pedido
// não o corta: o pedido cortado depois de o arquivo ser gravado (o espelho
// corta o contexto do pedido antes de olhar o que recebeu) não chega ao
// gancho como cortado.
func TestAplicador_GanchoDoPainel(t *testing.T) {
	m := montar(t, dicionarioDoTeste, Config{})
	esp := &espelhoFalso{a: m.a}
	acordou := 0
	m.a.LigarAoSync(esp, func() { acordou++ })
	pedido := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		esp.mu.Lock()
		esp.cortar = cancel
		esp.mu.Unlock()
		return ctx
	}

	if _, err := m.a.Aprender(pedido(), "ECD", "ecedê"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.a.Desfazer(pedido(), "ecedê"); err != nil {
		t.Fatal(err)
	}
	id := dictionary.IDDoPar("ecedê")
	if got := esp.vistos(); len(got) != 2 || got[0] != id || got[1] != id || acordou != 2 {
		t.Fatalf("o gancho viu %v e acordou %d vez(es); quero o id duas vezes", got, acordou)
	}
	if esp.soltos != 0 {
		t.Error("o gancho rodou fora da trava")
	}
	if esp.cortados != 0 {
		t.Errorf("o gancho recebeu o contexto do pedido, cortado depois de o arquivo mudar (%d vez(es))", esp.cortados)
	}
	esp.mu.Lock()
	esp.cortar = nil
	esp.mu.Unlock()
	ctx := context.Background()

	// O que não gravou não passa.
	if _, err := m.a.Desfazer(ctx, "nunca aprendida"); !errors.Is(err, dictionary.ErrNaoAprendida) {
		t.Fatalf("Desfazer = %v", err)
	}
	if _, err := m.a.Aprender(ctx, "ECD", "ECD"); err == nil {
		t.Fatal("o Aprender da variante igual ao termo passou")
	}
	// Nem o "Adicionar termo", nem o lote do sync.
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "LALUR"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"}), nil); err != nil {
		t.Fatal(err)
	}
	if got := esp.vistos(); len(got) != 2 || acordou != 2 {
		t.Errorf("o gancho viu %v e acordou %d vez(es) depois do resto; quero só os dois do painel", got, acordou)
	}

	// O espelho que falha: o painel segue, e o log não tem texto.
	esp.erro = errors.New("storage: o banco está ocupado")
	if _, err := m.a.Aprender(ctx, "ECD", "ecedê"); err != nil {
		t.Fatalf("o espelho que falha derrubou o Aprender: %v", err)
	}
	log := m.logs()
	if !strings.Contains(log, "não consegui anotar a mudança no espelho do sync") {
		t.Errorf("o log não avisa:\n%s", log)
	}
	for _, proibido := range []string{"ecedê", "esse efe", "LALUR"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
}

// O log do lote: só contagens, por situação e, no não aplicado, por motivo.
// Em Info quando gravou; o que só confere (a tentativa de novo de um par que
// continua não valendo aqui) fica em Debug.
func TestAplicador_LogDoLoteSoComContagens(t *testing.T) {
	m := montar(t, dicionarioDoTeste, Config{})
	if _, _, err := m.a.AplicarPares(pares(
		dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"},
		dictionary.ParDaNuvem{Variante: "efedê", Termo: "EFD", Dominio: "fiscal"},
		dictionary.ParDaNuvem{Variante: "lalurr", Termo: "LALUR", Dominio: "contabil"},
	), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "efedê", Termo: "EFD", Dominio: "fiscal"}), nil); err != nil {
		t.Fatal(err)
	}
	log := m.logs()
	if !strings.Contains(log, `level=INFO msg="aprendizado: pares da nuvem conferidos" pares=3 aplicados=1 desfeitos=0 convergentes=0 ja_vale_aqui=0 ausentes=0 nao_aplicados=2 nao_aplicados_por_motivo=dominio_ausente:1,palavra_ausente_com_dominio:1 gravou=true`) {
		t.Errorf("o log do lote:\n%s", log)
	}
	if !strings.Contains(log, `level=DEBUG msg="aprendizado: pares da nuvem conferidos" pares=1`) {
		t.Errorf("a conferência que não grava não está em Debug:\n%s", log)
	}
	for _, proibido := range []string{"esse efe", "ECF", "efedê", "EFD", "fiscal", "lalurr", "LALUR", "contabil"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
}

// dicionarioComAliasAMao tem duas variantes de ECD postas à mão (sem a
// origem "painel"): o par da nuvem que chega com uma delas "já vale aqui", e
// o desfazer que chega não a tira.
const dicionarioComAliasAMao = `{
  "usuario": "mauro",
  "entradas": [
    {
      "canonica": "ECD",
      "prioridade": 90,
      "alias": [
        "escrituração contábil",
        "escrituração digital"
      ]
    }
  ]
}
`

// O log do lote conta cada situação no seu campo. O
// TestAplicador_LogDoLoteSoComContagens só tem aplicados e não aplicados: aqui
// vão o desfeito sozinho (Info, porque grava) e um lote com um convergente,
// dois "já vale aqui" e três ausentes (Debug, porque não grava), cada um com
// uma contagem diferente. Um campo que lê a situação de outro muda a linha.
func TestAplicador_LogDoLoteContaCadaSituacao(t *testing.T) {
	m := montar(t, dicionarioComAliasAMao, Config{})
	if _, lote, err := m.a.AplicarPares(pares(
		dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"},
		dictionary.ParDaNuvem{Variante: "ecedê", Termo: "ECD"},
	), nil); err != nil || !lote.Mudou {
		t.Fatalf("o lote que aplica: %+v, %v", lote, err)
	}

	// O par aplicado pelo lote volta desfeito da nuvem.
	_, lote, err := m.a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF", Desfeito: true}), nil)
	if err != nil || !lote.Mudou || lote.Resultados[0].Situacao != dictionary.SituacaoDesfeito {
		t.Fatalf("o desfeito: %+v, %v", lote, err)
	}
	if bytes.Contains(m.bytes(t), []byte("esse efe")) {
		t.Error("o desfeito ficou no arquivo")
	}
	log := m.logs()
	if !strings.Contains(log, `level=INFO msg="aprendizado: pares da nuvem conferidos" pares=1 aplicados=0 desfeitos=1 convergentes=0 ja_vale_aqui=0 ausentes=0 nao_aplicados=0 nao_aplicados_por_motivo="" gravou=true`) {
		t.Errorf("o log do lote que desfaz:\n%s", log)
	}

	// Um convergente (o par que o lote aplicou, de novo), dois "já vale aqui"
	// (um ativo e um desfeito, cada um sobre uma variante posta à mão) e três
	// ausentes (desfeitos sem o que tirar).
	antes := m.bytes(t)
	_, lote, err = m.a.AplicarPares(pares(
		dictionary.ParDaNuvem{Variante: "ecedê", Termo: "ECD"},
		dictionary.ParDaNuvem{Variante: "escrituração contábil", Termo: "ECD"},
		dictionary.ParDaNuvem{Variante: "escrituração digital", Termo: "ECD", Desfeito: true},
		dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF", Desfeito: true},
		dictionary.ParDaNuvem{Variante: "dirfi", Termo: "DIRF", Desfeito: true},
		dictionary.ParDaNuvem{Variante: "lalurr", Termo: "LALUR", Desfeito: true},
	), nil)
	if err != nil || lote.Mudou {
		t.Fatalf("o lote que só confere: %+v, %v", lote, err)
	}
	quero := []dictionary.SituacaoDoPar{
		dictionary.SituacaoConvergente,
		dictionary.SituacaoJaValeAqui, dictionary.SituacaoJaValeAqui,
		dictionary.SituacaoAusente, dictionary.SituacaoAusente, dictionary.SituacaoAusente,
	}
	if len(lote.Resultados) != len(quero) {
		t.Fatalf("%d resultados, quero %d", len(lote.Resultados), len(quero))
	}
	for i, r := range lote.Resultados {
		if r.Situacao != quero[i] {
			t.Errorf("o par %d: %q, quero %q", i, r.Situacao, quero[i])
		}
	}
	if !bytes.Equal(antes, m.bytes(t)) {
		t.Error("o lote que só confere gravou")
	}
	log = m.logs()
	if !strings.Contains(log, `level=DEBUG msg="aprendizado: pares da nuvem conferidos" pares=6 aplicados=0 desfeitos=0 convergentes=1 ja_vale_aqui=2 ausentes=3 nao_aplicados=0 nao_aplicados_por_motivo="" gravou=false`) {
		t.Errorf("o log do lote que só confere:\n%s", log)
	}
	for _, proibido := range []string{"esse efe", "ECF", "ecedê", "escrituração", "dirfi", "DIRF", "lalurr", "LALUR"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
}

// Sem dicionário na subida, o aplicador é o arquivo indisponível para o sync,
// e não grava nada.
func TestAplicador_SemArquivo(t *testing.T) {
	a := Novo(Config{Caminho: filepath.Join(t.TempDir(), "dicionario.json")})
	if a.TemArquivo() {
		t.Fatal("sem usuário, há arquivo")
	}
	_, _, err := a.AplicarPares(pares(dictionary.ParDaNuvem{Variante: "esse efe", Termo: "ECF"}), nil)
	if !dictionary.ArquivoIndisponivel(err) {
		t.Errorf("erro = %v, quero o arquivo indisponível", err)
	}
	if _, err := a.Aprender(context.Background(), "ECF", "esse efe"); !errors.Is(err, dictionary.ErrSemArquivo) {
		t.Errorf("Aprender = %v", err)
	}
	if l, err := a.Aprendidas(context.Background()); err != nil || len(l) != 0 {
		t.Errorf("Aprendidas = %v, %v", l, err)
	}
}
