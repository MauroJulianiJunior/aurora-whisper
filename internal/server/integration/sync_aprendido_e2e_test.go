package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/aprendiz"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A F8g de ponta a ponta (ADR-019, "Decisões da F8", TEST-008 e AC-021): o
// aprendido do painel entre dois PCs e o celular, sobre a nuvem de verdade (o
// Store em memória, num httptest) e o aplicador de verdade
// (internal/agent/aprendiz) de cada PC, com o dicionario.json e o
// historico.db numa pasta temporária, o laço de sync e o painel do PC (a API
// local de verdade, sem a cadeia do guard). O runtime do Windows é um dublê
// (aprendiz.Config.Trocar): nada aqui abre janela, digita, usa a área de
// transferência ou toca o Gerenciador de Credenciais (o token vai para um
// chaves.Memoria).

// ─── Um PC com o dicionário ───

// runtimeFalso guarda cada troca de dicionário que o aplicador pediu.
type runtimeFalso struct {
	mu     sync.Mutex
	trocas []aprendiz.Troca
}

func (r *runtimeFalso) trocar(t aprendiz.Troca) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trocas = append(r.trocas, t)
}

func (r *runtimeFalso) ultima(t *testing.T) aprendiz.Troca {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.trocas) == 0 {
		t.Fatal("o runtime não recebeu dicionário nenhum")
	}
	return r.trocas[len(r.trocas)-1]
}

// espelhoDoPC é o espelho do sync do gancho do painel: a fila de agora do PC.
type espelhoDoPC struct{ pc *pcE2E }

func (e espelhoDoPC) SubirVersaoLocal(ctx context.Context, ids []string) error {
	return e.pc.repo.Fila(e.pc.dono).SubirVersaoLocal(ctx, ids)
}

type inicioParado struct{}

func (inicioParado) Estado() (bool, string) { return false, "" }
func (inicioParado) Definir(bool) error     { return nil }

type modelosParados struct{}

func (modelosParados) ListarModelos(context.Context, string, string) ([]string, error) {
	return nil, nil
}

type pcComDicionario struct {
	*pcE2E
	ap      *aprendiz.Aplicador
	arquivo string
	rt      *runtimeFalso
	h       http.Handler
	hRepo   *storage.SQLiteTranscriptionRepository
}

// novoPCComDicionario é um PC da conta com o dicionario.json s (gravado no
// formato do Gravar) e o aplicador ligado ao laço, como o Agent o monta
// (montarSync).
func (n *nuvemE2E) novoPCComDicionario(s dictionary.Semente) *pcComDicionario {
	n.t.Helper()
	pc := n.novoPC()
	caminho := filepath.Join(n.t.TempDir(), "dicionario.json")
	if err := s.Gravar(caminho); err != nil {
		n.t.Fatal(err)
	}
	rt := &runtimeFalso{}
	ap := aprendiz.Novo(aprendiz.Config{Caminho: caminho, Usuario: s.Usuario, Trocar: rt.trocar, Log: pc.logger})
	pc.aplicador = ap
	pc.laco = pc.novoLaco()
	ap.LigarAoSync(espelhoDoPC{pc}, func() { pc.laco.Avisar() })
	return &pcComDicionario{pcE2E: pc, ap: ap, arquivo: caminho, rt: rt}
}

// painel é a API local do PC, de verdade, sobre o mesmo aplicador, o mesmo
// histórico e o mesmo nuvem.Servico.
func (p *pcComDicionario) painel() http.Handler {
	if p.h == nil || p.hRepo != p.repo {
		srv := localapi.New(p.dono, p.repo, storage.NewInMemorySettingsRepository(), localapi.Preferencias{
			Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{},
			Aprendizado: p.ap, Termos: p.ap, Espelho: p.repo.Fila(p.dono), Nuvem: p.servico, Log: p.logger,
		})
		p.h, p.hRepo = srv.Handler(), p.repo
	}
	return p.h
}

func (p *pcComDicionario) chamar(metodo, caminho string, corpo any) (int, []byte) {
	p.t.Helper()
	var r *bytes.Reader
	if corpo != nil {
		b, _ := json.Marshal(corpo)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	p.painel().ServeHTTP(w, httptest.NewRequest(metodo, caminho, r))
	return w.Code, w.Body.Bytes()
}

type itemDoPainel struct {
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
	Situacao string `json:"situacao"`
	Motivo   string `json:"motivo"`
}

// lista é o GET /api/aprendizado do painel.
func (p *pcComDicionario) lista() []itemDoPainel {
	p.t.Helper()
	st, b := p.chamar(http.MethodGet, "/api/aprendizado", nil)
	var v struct{ Items []itemDoPainel }
	if st != http.StatusOK || json.Unmarshal(b, &v) != nil {
		p.t.Fatalf("GET /api/aprendizado = %d %s", st, b)
	}
	return v.Items
}

// historicoDoPainel é o historico do GET /api/nuvem com os nomes que a web lê
// (06, F8h), escritos aqui por extenso: previa_trocas (o M) e freio_trocas
// (ausente sem o freio).
type historicoDoPainel struct {
	Previa int  `json:"previa"`
	Trocas int  `json:"previa_trocas"`
	Freio  *int `json:"freio_trocas"`
	Aceito bool `json:"aceito"`
}

// historico é o historico do GET /api/nuvem.
func (p *pcComDicionario) historico() historicoDoPainel {
	p.t.Helper()
	st, b := p.chamar(http.MethodGet, "/api/nuvem", nil)
	var v struct{ Historico *historicoDoPainel }
	if st != http.StatusOK || json.Unmarshal(b, &v) != nil || v.Historico == nil {
		p.t.Fatalf("GET /api/nuvem = %d %s", st, b)
	}
	return *v.Historico
}

// enviarPeloPainel é o clique em "Enviar o histórico" (POST /api/nuvem/enviar).
func (p *pcComDicionario) enviarPeloPainel() historicoDoPainel {
	p.t.Helper()
	st, b := p.chamar(http.MethodPost, "/api/nuvem/enviar", nil)
	var v struct{ Historico *historicoDoPainel }
	if st != http.StatusOK || json.Unmarshal(b, &v) != nil || v.Historico == nil {
		p.t.Fatalf("POST /api/nuvem/enviar = %d %s", st, b)
	}
	return *v.Historico
}

// desfazerPeloPainel é o Desfazer da lista "Aprendido no painel".
func (p *pcComDicionario) desfazerPeloPainel(variante string) {
	p.t.Helper()
	if st, b := p.chamar(http.MethodPost, "/api/aprendizado/desfazer", map[string]string{"variante": variante}); st != http.StatusOK {
		p.t.Fatalf("desfazer = %d %s", st, b)
	}
}

// aprender é o Aprender do painel, pelo aplicador (o que POST
// /api/aprendizado chama depois de conferir a sugestão).
func (p *pcComDicionario) aprender(termo, variante string) {
	p.t.Helper()
	if _, err := p.ap.Aprender(context.Background(), termo, variante); err != nil {
		p.t.Fatalf("aprender %q: %v", variante, err)
	}
}

func (p *pcComDicionario) semente() dictionary.Semente {
	p.t.Helper()
	b, err := os.ReadFile(p.arquivo)
	if err != nil {
		p.t.Fatal(err)
	}
	s, err := dictionary.CarregarSemente(bytes.NewReader(b))
	if err != nil {
		p.t.Fatalf("o dicionario.json não carrega: %v", err)
	}
	return s
}

// onde diz em que termo e em que lista está a variante ("" fora do arquivo), e
// se com a origem "painel".
func (p *pcComDicionario) onde(variante string) (termo, lista string, painel bool) {
	s := p.semente()
	for _, e := range s.Entradas {
		for _, l := range []struct {
			nome string
			vs   []string
		}{{"alias", e.Alias}, {"alias_restrito", e.AliasRestrito}} {
			for _, v := range l.vs {
				if v == variante {
					return e.Canonica, l.nome, s.AliasOrigem[v] == dictionary.OrigemPainel
				}
			}
		}
	}
	return "", "", false
}

// soOsCamposDeAntes falha se o dicionario.json ganhou um campo que um Agent
// de antes da F8 não conhece (CarregarSemente recusa campo desconhecido): o
// sync guarda tudo no SQLite.
func (p *pcComDicionario) soOsCamposDeAntes() {
	p.t.Helper()
	b, err := os.ReadFile(p.arquivo)
	if err != nil {
		p.t.Fatal(err)
	}
	var cru struct {
		Topo     map[string]json.RawMessage
		Entradas []map[string]json.RawMessage `json:"entradas"`
	}
	if err := json.Unmarshal(b, &cru.Topo); err != nil {
		p.t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cru); err != nil {
		p.t.Fatal(err)
	}
	permitidos := func(m map[string]json.RawMessage, quer ...string) {
		for k := range m {
			ok := false
			for _, q := range quer {
				ok = ok || k == q
			}
			if !ok {
				p.t.Errorf("o dicionario.json ganhou o campo %q", k)
			}
		}
	}
	permitidos(cru.Topo, "usuario", "dominios", "alias_origem", "entradas")
	for _, e := range cru.Entradas {
		permitidos(e, "canonica", "prioridade", "dominio", "idioma", "alias", "alias_restrito", "restrito", "inativa", "origem")
	}
	var origens map[string]string
	var topo struct {
		AliasOrigem map[string]string `json:"alias_origem"`
	}
	_ = json.Unmarshal(b, &topo)
	origens = topo.AliasOrigem
	for v, o := range origens {
		if o != dictionary.OrigemPainel && o != dictionary.OrigemAdicionado && !strings.HasPrefix(o, "cont") {
			p.t.Errorf("a origem de %q é %q", v, o)
		}
	}
	if _, err := dictionary.CarregarSemente(bytes.NewReader(b)); err != nil {
		p.t.Errorf("o dicionario.json não carrega como antes: %v", err)
	}
}

// resolve passa texto pelo Smart da última troca do runtime, no domínio dado
// ("" é o desconhecido).
func (p *pcComDicionario) resolve(texto, dominio string) string {
	p.t.Helper()
	var d *string
	if dominio != "" {
		d = &dominio
	}
	out, err := p.rt.ultima(p.t).Smart.Resolve(context.Background(), resolver.ResolveRequest{UserID: p.semente().Usuario,
		ASRResult: asr.ASRResult{Text: texto}, CloudPolicy: resolver.CloudDisabled, Domain: d})
	if err != nil {
		p.t.Fatal(err)
	}
	return out.FinalText
}

// ─── O celular ───

type aprendidoDoCelular struct {
	ID       string `json:"id"`
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
	Restrito bool   `json:"restrito"`
	Dominio  string `json:"dominio"`
}

// celAprendidos é a lista "Aprendido" do celular (GET /api/v1/learned), por
// variante.
func (n *nuvemE2E) celAprendidos() map[string]aprendidoDoCelular {
	n.t.Helper()
	st, b := n.celular(http.MethodGet, "/api/v1/learned", nil)
	var v struct{ Items []aprendidoDoCelular }
	if st != http.StatusOK || json.Unmarshal(b, &v) != nil {
		n.t.Fatalf("celular GET /learned = %d %s", st, b)
	}
	out := map[string]aprendidoDoCelular{}
	for _, it := range v.Items {
		out[it.Variante] = it
	}
	return out
}

func (n *nuvemE2E) celDesfazerAprendido(id string) {
	n.t.Helper()
	if st, b := n.celular(http.MethodPost, "/api/v1/learned/undo", map[string]string{"id": id}); st != http.StatusOK {
		n.t.Fatalf("celular desfazer = %d %s", st, b)
	}
}

// aprendidosNaConta são os pares (não desfeitos) da conta do token, pelo pull
// do cursor 0, por variante.
func (n *nuvemE2E) aprendidosNaConta(token string) []string {
	n.t.Helper()
	var out []string
	for _, ch := range n.tudoNaConta(token) {
		if a := ch.PayloadAprendido; a != nil && a.UndoneAt == nil {
			out = append(out, a.Variant+"→"+a.Term)
		}
	}
	sort.Strings(out)
	return out
}

// pushesDoAprendido são os entity_id do aprendido que subiram desde o último
// zerar do porteiro.
func (n *nuvemE2E) subiram(ids ...string) []string {
	quer := map[string]bool{}
	for _, id := range ids {
		quer[id] = true
	}
	var out []string
	for _, lote := range n.porteiro.entidadesVistas() {
		for _, e := range lote {
			if quer[e] {
				out = append(out, e)
			}
		}
	}
	return out
}

// ─── Os dicionários ───

// sementeDoPC1: ECD sem domínio; DIRF restrito a contabil; EFD em fiscal,
// sem restrição.
func sementeDoPC1() dictionary.Semente {
	return dictionary.Semente{
		Usuario:  "mauro",
		Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}, "fiscal": {"Fiscal.exe"}},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECD", Prioridade: 90},
			{Canonica: "DIRF", Prioridade: 90, Dominio: "contabil", Restrito: true},
			{Canonica: "EFD", Prioridade: 90, Dominio: "fiscal"},
		},
	}
}

// sementeDoPC2: ECD sem domínio; DIRF em contabil, SEM restrição; e nada de
// fiscal (nem o domínio, nem EFD).
func sementeDoPC2() dictionary.Semente {
	return dictionary.Semente{
		Usuario:  "mauro",
		Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECD", Prioridade: 90},
			{Canonica: "DIRF", Prioridade: 90, Dominio: "contabil"},
		},
	}
}

// textosDoAprendido são as variantes, os termos e os domínios dos testes:
// nenhum deles pode ir a log.
var textosDoAprendido = []string{"ecêefe", "ECF", "dirfi", "DIRF", "efedê", "EFD", "ecêdê", "ECD", "contabil", "fiscal"}

func semTextoNoLog(t *testing.T, logs map[string]string) {
	t.Helper()
	for nome, log := range logs {
		if log == "" {
			t.Fatalf("o log do %s está vazio: o teste não mede nada", nome)
		}
		for _, proibido := range textosDoAprendido {
			if strings.Contains(log, proibido) {
				t.Errorf("o log do %s tem %q", nome, proibido)
			}
		}
	}
}

// ─── Os percursos ───

// O par do PC 1 chega ao PC 2 com a restrição e o domínio, e ao celular; a D3
// no PC 1 (o histórico vazio e três pares de origem "painel": nada sobe até o
// POST /api/nuvem/enviar, e a pergunta aparece com N = 0); a D3 no PC 2, que
// não clicou (nem o ditado nem o par dele sobem, e o que vem da nuvem entra);
// o "não aplicado neste PC"; o PC 2 que ainda não clicou e desfaz um par que
// veio da nuvem leva o desfazer ao celular e ao PC 1; o desfazer no celular
// some dos dois; o dicionario.json só com os campos de antes; o log sem
// texto.
func TestAprendidoE2E_DoPC1AoPC2EAoCelular(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPCComDicionario(sementeDoPC1()), n.novoPCComDicionario(sementeDoPC2())
	originalDoPC2, err := os.ReadFile(pc2.arquivo)
	if err != nil {
		t.Fatal(err)
	}
	ecf, dirfi, efede, ecede := dictionary.IDDoPar("ecêefe"), dictionary.IDDoPar("dirfi"), dictionary.IDDoPar("efedê"), dictionary.IDDoPar("ecêdê")

	// ── A D3 no PC 1: o histórico vazio e três pares do painel.
	pc1.aprender("ECF", "ecêefe") // cria o termo, sem domínio
	pc1.aprender("DIRF", "dirfi") // restrito a contabil (a entrada)
	pc1.aprender("EFD", "efedê")  // em fiscal, sem restrição
	pc1.rodadaOK()
	if got := n.subiram(ecf, dirfi, efede); len(got) != 0 {
		t.Fatalf("antes do clique subiram %v", got)
	}
	if h := pc1.historico(); h.Previa != 0 || h.Trocas != 3 || h.Aceito || h.Freio != nil {
		t.Fatalf("a pergunta com N = 0: %+v; quero previa 0, previa_trocas 3, sem o aceite e sem o freio", h)
	}
	if len(n.celAprendidos()) != 0 {
		t.Fatal("o celular vê um par antes do clique")
	}
	pc1.rodadaOK() // de novo: nada muda sem o clique
	if got := n.subiram(ecf, dirfi, efede); len(got) != 0 {
		t.Fatalf("a segunda rodada sem o clique subiu %v", got)
	}
	if h := pc1.enviarPeloPainel(); !h.Aceito {
		t.Fatalf("o clique com N = 0: %+v", h)
	}
	if c := pc1.copias(); len(c) != 0 {
		t.Errorf("o clique com N = 0 fez a cópia do histórico: %v", c)
	}
	pc1.rodadaOK()
	if got := n.subiram(ecf, dirfi, efede); len(got) != 3 {
		t.Fatalf("depois do clique subiram %v; quero os três", got)
	}
	cel := n.celAprendidos()
	quer := map[string]aprendidoDoCelular{
		"ecêefe": {ID: ecf, Termo: "ECF", Variante: "ecêefe"},
		"dirfi":  {ID: dirfi, Termo: "DIRF", Variante: "dirfi", Restrito: true, Dominio: "contabil"},
		"efedê":  {ID: efede, Termo: "EFD", Variante: "efedê", Dominio: "fiscal"},
	}
	if !reflect.DeepEqual(cel, quer) {
		t.Fatalf("o celular vê %+v\nquero %+v", cel, quer)
	}
	if h := pc1.historico(); h.Trocas != 0 || !h.Aceito {
		t.Errorf("depois do envio: %+v", h)
	}

	// ── O PC 2 não clicou: um ditado no histórico, e um par dele.
	ditado := pc2.semearHistorico(historicoComum(1, "do PC 2"))[0]
	pc2.aprender("ECD", "ecêdê")
	n.porteiro.zerar()
	pc2.rodadaOK()
	if h := pc2.historico(); h.Previa != 1 || h.Trocas != 1 || h.Aceito {
		t.Fatalf("o PC 2 sem o clique: %+v", h)
	}
	if got := n.subiram(ecede, ditado); len(got) != 0 {
		t.Errorf("o PC 2 sem o clique subiu %v", got)
	}
	// O que vem da nuvem entra antes do clique, com a adaptação deste PC.
	if termo, lista, painel := pc2.onde("ecêefe"); termo != "ECF" || lista != "alias" || !painel {
		t.Errorf("ecêefe no PC 2: %q %q %v", termo, lista, painel)
	}
	if termo, lista, painel := pc2.onde("dirfi"); termo != "DIRF" || lista != "alias_restrito" || !painel {
		t.Errorf("dirfi no PC 2: %q %q %v; quero DIRF em alias_restrito (a restrição não se perde)", termo, lista, painel)
	}
	if termo, _, _ := pc2.onde("efedê"); termo != "" {
		t.Errorf("efedê entrou no PC 2, que não conhece fiscal: %q", termo)
	}
	for _, e := range pc2.semente().Entradas {
		if e.Canonica == "ECF" && (e.Dominio != "" || e.Prioridade != 50 || e.Origem != dictionary.OrigemPainel) {
			t.Errorf("o termo criado pela D2: %+v", e)
		}
	}
	// O runtime do PC 2 recebeu o dicionário novo: o par age onde agia na
	// origem, e nunca em mais lugares.
	if got := pc2.resolve("ecêefe", ""); got != "ECF" {
		t.Errorf("ecêefe no PC 2 vira %q", got)
	}
	if got := pc2.resolve("dirfi", "contabil"); got != "DIRF" {
		t.Errorf("dirfi em contabil vira %q", got)
	}
	if got := pc2.resolve("dirfi", ""); got == "DIRF" {
		t.Error("dirfi agiu fora de contabil no PC 2: a troca alargou")
	}
	if !reflect.DeepEqual(pc2.lista(), []itemDoPainel{
		{Termo: "ECD", Variante: "ecêdê"},
		{Termo: "DIRF", Variante: "dirfi"},
		{Termo: "ECF", Variante: "ecêefe"},
		{Termo: "EFD", Variante: "efedê", Situacao: "nao_aplicado", Motivo: "dominio_ausente"},
	}) {
		t.Errorf("a lista do PC 2: %+v", pc2.lista())
	}

	// ── O PC 2, que ainda não clicou, desfaz um par que veio da nuvem: o
	// desfazer sobe antes do clique, e chega ao celular e ao PC 1.
	pc2.desfazerPeloPainel("dirfi")
	pc2.rodadaOK()
	if got := n.subiram(dirfi); len(got) != 1 {
		t.Fatalf("o desfazer antes do clique: subiram %v", got)
	}
	if got := n.subiram(ecede, ditado); len(got) != 0 {
		t.Errorf("com o desfazer, subiram também %v", got)
	}
	if _, ok := n.celAprendidos()["dirfi"]; ok {
		t.Error("o celular ainda vê o par que o PC 2 desfez")
	}
	pc1.rodadaOK()
	if termo, _, _ := pc1.onde("dirfi"); termo != "" {
		t.Errorf("o desfazer do PC 2 não chegou ao PC 1: dirfi em %q", termo)
	}
	if h := pc2.historico(); h.Aceito || h.Previa != 1 {
		t.Errorf("o desfazer gravou o aceite do PC 2: %+v", h)
	}

	// ── O desfazer no celular some dos dois, sem etiqueta.
	n.celDesfazerAprendido(ecf)
	pc1.rodadaOK()
	pc2.rodadaOK()
	for nome, pc := range map[string]*pcComDicionario{"PC 1": pc1, "PC 2": pc2} {
		if termo, _, _ := pc.onde("ecêefe"); termo != "" {
			t.Errorf("o desfazer do celular não chegou ao %s", nome)
		}
		for _, it := range pc.lista() {
			if it.Variante == "ecêefe" || it.Variante == "dirfi" {
				t.Errorf("o %s mostra %+v: o desfazer que chega pelo pull não tem etiqueta", nome, it)
			}
		}
	}
	if got := pc1.resolve("ecêefe", ""); got == "ECF" {
		t.Error("o runtime do PC 1 ainda troca ecêefe")
	}
	if cel := n.celAprendidos(); len(cel) != 1 || cel["efedê"].ID != efede {
		t.Errorf("o celular depois dos desfazeres: %+v", cel)
	}

	// O PC 2 volta ao arquivo de antes, menos o par dele (ecêdê): sem ele, os
	// mesmos bytes (o termo que a D2 criou saiu com o par).
	pc2.desfazerPeloPainel("ecêdê")
	if got, _ := os.ReadFile(pc2.arquivo); !bytes.Equal(got, originalDoPC2) {
		t.Errorf("o dicionario.json do PC 2 não voltou ao que era:\n%s\nquero\n%s", got, originalDoPC2)
	}
	pc1.soOsCamposDeAntes()
	pc2.soOsCamposDeAntes()
	semTextoNoLog(t, map[string]string{"PC1": pc1.log.String(), "PC2": pc2.log.String(), "nuvem": n.log.String()})
}

// A D4: vale o primeiro na nuvem, o desfazer vence, e as etiquetas. O PC 3
// aprende para outro termo a variante que o PC 1 já subiu: perde, segue a
// nuvem, e mostra "trocado em outro aparelho". O PC 1, desconectado, desfaz e
// aprende de novo o mesmo par (o arquivo fica como era) enquanto o PC 3 o
// desfaz: na volta, o desfazer vence, e o PC 1 mostra "desfeito em outro
// aparelho"; aprender de novo tira a etiqueta e sobe. O Aprender do PC 3 vai
// pelo painel inteiro (o PATCH sugere, o POST aprende).
func TestAprendidoE2E_D4ValeOPrimeiroEODesfazerVence(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	s3 := sementeDoPC2()
	s3.Entradas = append(s3.Entradas, dictionary.EntradaSemente{Canonica: "ECF", Prioridade: 90})
	pc1, pc3 := n.novoPCComDicionario(sementeDoPC1()), n.novoPCComDicionario(s3)
	pc1.rodadaOK()
	pc3.rodadaOK()
	if !pc1.historico().Aceito || !pc3.historico().Aceito {
		t.Fatal("pré-condição: o histórico vazio e nada aprendido gravam o aceite sozinhos")
	}
	ecede := dictionary.IDDoPar("ecêdê")

	// ── Vale o primeiro.
	pc1.aprender("ECD", "ecêdê")
	pc1.rodadaOK()
	// O PC 3, sem ter puxado, aprende a mesma variante para ECF, pelo painel.
	id := pc3.ditar("o ecêdê saiu hoje")
	st, b := pc3.chamar(http.MethodPatch, "/api/history/"+id, map[string]any{"base_version": 1, "final_text": "o ECF saiu hoje"})
	var editado struct {
		Sugestoes []struct{ De, Para string }
	}
	if st != http.StatusOK || json.Unmarshal(b, &editado) != nil || len(editado.Sugestoes) == 0 {
		t.Fatalf("o PATCH não sugeriu: %d %s", st, b)
	}
	sug := editado.Sugestoes[0]
	if !strings.EqualFold(sug.De, "ecêdê") || sug.Para != "ECF" {
		t.Fatalf("a sugestão: %+v", sug)
	}
	if st, b := pc3.chamar(http.MethodPost, "/api/aprendizado", map[string]string{"id": id, "de": sug.De, "para": sug.Para}); st != http.StatusOK {
		t.Fatalf("POST /api/aprendizado = %d %s", st, b)
	}
	if termo, _, _ := pc3.onde(sug.De); termo != "ECF" {
		t.Fatalf("pré-condição: o PC 3 aprendeu %q em %q", sug.De, termo)
	}
	pc3.rodadaOK()
	if termo, lista, painel := pc3.onde(sug.De); termo != "ECD" || lista != "alias" || !painel {
		t.Errorf("o PC 3 não seguiu a nuvem: %q %q %v", termo, lista, painel)
	}
	// A linha do arquivo é a troca da nuvem (com o Desfazer, sem situacao), e
	// a de estado, a troca que o PC 3 tinha aprendido e perdeu.
	if got := pc3.lista(); !reflect.DeepEqual(got, []itemDoPainel{{Termo: "ECD", Variante: sug.De},
		{Termo: "ECF", Variante: sug.De, Situacao: "trocado_em_outro_aparelho"}}) {
		t.Errorf("a lista do PC 3: %+v", got)
	}
	if cel := n.celAprendidos(); len(cel) != 1 || cel["ecêdê"].Termo != "ECD" {
		t.Errorf("o celular: %+v; vale o primeiro (ECD)", cel)
	}
	pc1.rodadaOK()
	if termo, _, _ := pc1.onde("ecêdê"); termo != "ECD" || len(pc1.lista()) != 1 || pc1.lista()[0].Situacao != "" {
		t.Errorf("o PC 1, que chegou primeiro: %q, %+v", termo, pc1.lista())
	}

	// ── O desfazer vence: o PC 1 desconectado desfaz e aprende de novo
	// (o arquivo fica como era), e o PC 3 desfaz.
	antes, err := os.ReadFile(pc1.arquivo)
	if err != nil {
		t.Fatal(err)
	}
	pc1.desconectar()
	pc1.desfazerPeloPainel("ecêdê")
	pc1.aprender("ECD", "ecêdê")
	if depois, _ := os.ReadFile(pc1.arquivo); !bytes.Equal(antes, depois) {
		t.Fatal("pré-condição: o Desfazer e o Aprender deixam o arquivo como era")
	}
	pc1.rodadaOK() // desconectado: nada roda
	pc3.desfazerPeloPainel(sug.De)
	pc3.rodadaOK()
	if _, ok := n.celAprendidos()["ecêdê"]; ok {
		t.Fatal("pré-condição: o desfazer do PC 3 não chegou à nuvem")
	}
	pc1.reconectar()
	n.porteiro.zerar()
	pc1.rodadaOK()
	if got := n.subiram(ecede); len(got) != 1 {
		t.Errorf("o PC 1 não mandou a mudança dele: %v", got)
	}
	if termo, _, _ := pc1.onde("ecêdê"); termo != "" {
		t.Errorf("o desfazer não venceu: ecêdê em %q no PC 1", termo)
	}
	if got := pc1.lista(); !reflect.DeepEqual(got, []itemDoPainel{{Termo: "ECD", Variante: "ecêdê", Situacao: "desfeito_em_outro_aparelho"}}) {
		t.Errorf("a lista do PC 1: %+v", got)
	}
	if _, ok := n.celAprendidos()["ecêdê"]; ok {
		t.Error("o celular vê o par: o reaprender desconectado venceu o desfazer")
	}
	if got := pc1.resolve("ecêdê", ""); got == "ECD" {
		t.Error("o runtime do PC 1 ainda troca ecêdê")
	}

	// ── Aprender de novo tira a etiqueta e sobe.
	pc1.aprender("ECD", "ecêdê")
	pc1.rodadaOK()
	if got := pc1.lista(); !reflect.DeepEqual(got, []itemDoPainel{{Termo: "ECD", Variante: "ecêdê"}}) {
		t.Errorf("a lista do PC 1 depois de aprender de novo: %+v", got)
	}
	if cel := n.celAprendidos(); cel["ecêdê"].Termo != "ECD" {
		t.Errorf("o reaprender não chegou ao celular: %+v", cel)
	}
	pc3.rodadaOK()
	if termo, _, _ := pc3.onde("ecêdê"); termo != "ECD" {
		t.Errorf("o reaprender não chegou ao PC 3: %q", termo)
	}
	if got := pc3.lista(); len(got) != 1 || got[0].Situacao != "" {
		t.Errorf("a lista do PC 3: %+v", got)
	}
	pc1.soOsCamposDeAntes()
	pc3.soOsCamposDeAntes()
	semTextoNoLog(t, map[string]string{"PC1": pc1.log.String(), "PC3": pc3.log.String(), "nuvem": n.log.String()})
}

// O esquecer, a volta e a conta nova. O PC 2 recebeu dois pares do PC 1 e
// aprendeu um. Desconectado (o laço parado, a varredura não vê), desfaz um
// par que veio da nuvem e esquece a nuvem; de volta à mesma conta, o desfazer
// sobe (antes do clique) e chega ao PC 1. Depois, esquece de novo e conecta a
// uma conta nova: a pergunta conta só o par aprendido neste PC, e, com o
// clique, só ele sobe (o que veio da conta antiga, não).
func TestAprendidoE2E_EsquecerAVoltaEAContaNova(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPCComDicionario(sementeDoPC1()), n.novoPCComDicionario(sementeDoPC2())
	pc1.rodadaOK()
	pc2.rodadaOK()
	pc1.aprender("ECF", "ecêefe")
	pc1.aprender("ECD", "ecêdê")
	pc1.rodadaOK()
	pc2.rodadaOK()
	pc2.aprender("DIRF", "dirfi")
	pc2.rodadaOK()
	if got := n.aprendidosNaConta(pc2.token); !reflect.DeepEqual(got, []string{"dirfi→DIRF", "ecêdê→ECD", "ecêefe→ECF"}) {
		t.Fatalf("a conta: %v", got)
	}

	// ── A volta à mesma conta.
	pc2.desconectar()
	pc2.desfazerPeloPainel("ecêefe")
	pc2.rodadaOK() // desconectado: nada roda
	pc2.esquecer()
	pc2.conectarNa(n.conta, emailDoE2E, true)
	pc2.rodadaOK()
	pc2.rodadaOK()
	if got := n.aprendidosNaConta(pc1.token); !reflect.DeepEqual(got, []string{"dirfi→DIRF", "ecêdê→ECD"}) {
		t.Errorf("de volta à mesma conta, o desfazer não subiu: %v", got)
	}
	if termo, _, _ := pc2.onde("ecêefe"); termo != "" {
		t.Errorf("o pull do cursor 0 pôs de volta o par que o PC 2 desfez: %q", termo)
	}
	pc1.rodadaOK()
	if termo, _, _ := pc1.onde("ecêefe"); termo != "" {
		t.Errorf("o desfazer do PC 2 não chegou ao PC 1: %q", termo)
	}
	for _, v := range []string{"ecêdê", "dirfi"} {
		if termo, _, _ := pc2.onde(v); termo == "" {
			t.Errorf("%s saiu do PC 2 na volta", v)
		}
	}

	// ── A conta nova.
	pc2.esquecer()
	outra := n.novaConta("sub-outra-aprendido", emailDaOutraConta)
	pc2.conectarNa(outra, emailDaOutraConta, false)
	pc2.rodadaOK()
	if h := pc2.historico(); h.Previa != 0 || h.Trocas != 1 || h.Aceito {
		t.Fatalf("a pergunta na conta nova: %+v; quero só o par aprendido neste PC", h)
	}
	if got := n.aprendidosNaConta(pc2.token); len(got) != 0 {
		t.Fatalf("antes do clique, a conta nova tem %v", got)
	}
	pc2.enviarPeloPainel()
	pc2.rodadaOK()
	if got := n.aprendidosNaConta(pc2.token); !reflect.DeepEqual(got, []string{"dirfi→DIRF"}) {
		t.Errorf("a conta nova tem %v; quero só o que o PC 2 aprendeu", got)
	}
	if got := n.aprendidosNaConta(pc1.token); !reflect.DeepEqual(got, []string{"dirfi→DIRF", "ecêdê→ECD"}) {
		t.Errorf("a conta antiga mudou: %v", got)
	}
	if termo, _, _ := pc2.onde("ecêdê"); termo != "ECD" {
		t.Errorf("o par da conta antiga saiu do PC 2: %q", termo)
	}
	pc2.soOsCamposDeAntes()
	semTextoNoLog(t, map[string]string{"PC1": pc1.log.String(), "PC2": pc2.log.String(), "nuvem": n.log.String()})
}
