package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/authtest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"

	_ "modernc.org/sqlite"
)

// O palco da ponta a ponta do sync (ADR-019, F7a e F7b): a nuvem de verdade
// (internal/server sobre o Store em memória) num httptest, atrás de um
// porteiro que o teste controla (a rede fora, a resposta que se perde, o 410,
// o redirecionamento, o push travado); dois PCs de mentira com o Agent de
// verdade do lado do sync (o SQLite numa pasta temporária, a fila, o
// nuvem.Servico da F6 com o Cofre na memória e o laço de sync com o cliente
// HTTPS); e o "celular", que fala com a nuvem pela sessão do navegador.
//
// Nada aqui toca o sistema: o token é de mentira, criado no Store de teste, e
// vai para um chaves.Memoria; nada abre janela; os bancos ficam na pasta
// temporária do teste.

const emailDoE2E = "voce@teste.local"

// emailDaOutraConta é a segunda conta da lista (a "outra conta" do esquecer,
// F7c).
const emailDaOutraConta = "outra@teste.local"

// porteiro fica na frente da nuvem: faz o que o teste mandar e anota o que
// passou.
type porteiro struct {
	mu sync.Mutex
	// fora: a nuvem fora do ar (503 em tudo).
	fora bool
	// perderRespostas: quantos pushes são aplicados pela nuvem e têm a
	// resposta perdida no caminho (o PC recebe 502).
	perderRespostas int
	// gone410: quantos pulls com after_seq > 0 recebem 410.
	gone410 int
	// redirecionar: para onde mandar tudo (307).
	redirecionar string
	// travarPush: o push espera este canal fechar.
	travarPush chan struct{}
	// pedidos: "push", "pull:<after_seq>", etc., na ordem em que chegaram.
	pedidos []string
	// pushes: os mutation_id de cada push que chegou; entidades, os
	// entity_id, na mesma ordem.
	pushes    [][]string
	entidades [][]string
}

func (p *porteiro) envolver(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var corpo []byte
		if r.Body != nil {
			corpo, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(corpo))
		}
		ehSync := strings.HasPrefix(r.URL.Path, "/api/v1/sync/")
		p.mu.Lock()
		if ehSync {
			switch r.URL.Path {
			case "/api/v1/sync/push":
				p.pedidos = append(p.pedidos, "push")
				var b struct {
					Mutations []struct {
						MutationID string `json:"mutation_id"`
						EntityID   string `json:"entity_id"`
					} `json:"mutations"`
				}
				_ = json.Unmarshal(corpo, &b)
				var ids, ents []string
				for _, m := range b.Mutations {
					ids = append(ids, m.MutationID)
					ents = append(ents, m.EntityID)
				}
				p.pushes = append(p.pushes, ids)
				p.entidades = append(p.entidades, ents)
			default:
				p.pedidos = append(p.pedidos, "pull:"+r.URL.Query().Get("after_seq"))
			}
		}
		fora, destino, travar := p.fora, p.redirecionar, p.travarPush
		perder := ehSync && r.URL.Path == "/api/v1/sync/push" && p.perderRespostas > 0
		if perder {
			p.perderRespostas--
		}
		dar410 := ehSync && r.URL.Path == "/api/v1/sync/pull" && r.URL.Query().Get("after_seq") != "0" && p.gone410 > 0
		if dar410 {
			p.gone410--
		}
		p.mu.Unlock()

		switch {
		case fora:
			http.Error(w, "fora do ar", http.StatusServiceUnavailable)
		case destino != "":
			http.Redirect(w, r, destino+r.URL.Path, http.StatusTemporaryRedirect)
		case dar410:
			// O envelope do 410 é o do contrato (06). O servidor não o manda
			// no mesmo epoch (a D2 da F9: sem o 410 da marca d'água); o PC
			// continua a tratá-lo como "releia do 0", por robustez, e é isso
			// que o porteiro prova.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, `{"error":{"code":"RESYNC_REQUIRED","message":"recomece"}}`)
		case perder:
			// A nuvem aplica; a resposta se perde no caminho.
			next.ServeHTTP(httptest.NewRecorder(), r)
			http.Error(w, "a resposta se perdeu", http.StatusBadGateway)
		default:
			if travar != nil && r.URL.Path == "/api/v1/sync/push" {
				<-travar
			}
			next.ServeHTTP(w, r)
		}
	})
}

func (p *porteiro) mexer(f func(p *porteiro)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p)
}

func (p *porteiro) visto() ([]string, [][]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pedidos...), append([][]string(nil), p.pushes...)
}

func (p *porteiro) zerar() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pedidos, p.pushes, p.entidades = nil, nil, nil
}

// entidadesVistas são os entity_id de cada push, na ordem.
func (p *porteiro) entidadesVistas() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]string(nil), p.entidades...)
}

// nuvemE2E é a nuvem de verdade, a conta, a sessão do celular e o porteiro.
type nuvemE2E struct {
	t        *testing.T
	mem      *cloudstore.MemoryStore
	srv      *httptest.Server
	cloud    *server.Cloud
	base     string
	conta    string
	sessao   string
	segredo  []byte
	pepper   []byte
	porteiro *porteiro
	log      *contracttest.LockedBuffer
	// relogio, quando não é nil, é o relógio da nuvem e do Motor dos PCs (os
	// casos da retenção, F9d): o tempo de verdade mais o quanto o teste andou.
	// Nil: os dois usam time.Now, como antes.
	relogio *relogioE2E
}

// relogioE2E é o tempo de verdade deslocado pelo quanto o teste andou.
type relogioE2E struct {
	mu     sync.Mutex
	desloc time.Duration
}

func (r *relogioE2E) agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Now().Add(r.desloc)
}

func (r *relogioE2E) andar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.desloc += d
}

func novaNuvemE2E(t *testing.T, limites server.Limites) *nuvemE2E {
	return montarNuvemE2E(t, limites, nil)
}

// novaNuvemE2EComRelogio é a nuvem com o relógio que o teste anda, o mesmo da
// nuvem e do Motor dos PCs (F9d): só teste, o PC não muda.
func novaNuvemE2EComRelogio(t *testing.T) *nuvemE2E {
	return montarNuvemE2E(t, server.Limites{}, &relogioE2E{})
}

func montarNuvemE2E(t *testing.T, limites server.Limites, relogio *relogioE2E) *nuvemE2E {
	t.Helper()
	ctx := context.Background()
	n := &nuvemE2E{t: t, mem: cloudstore.NewMemoryStore(), porteiro: &porteiro{}, log: &contracttest.LockedBuffer{},
		pepper: []byte(cloudauth.NovoSegredo()), relogio: relogio}
	u, err := n.mem.EntrarComGoogle(ctx, cloudstore.Identidade{Sub: "sub-e2e-sync", Email: emailDoE2E, Nome: "Você"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	n.conta = u.ID
	segredo := []byte(cloudauth.NovoSegredo())
	n.segredo = segredo
	n.sessao = cloudauth.NovoSegredo()
	if _, err := n.mem.CriarSessao(ctx, cloudstore.NovaSessao{
		UserID: u.ID, TokenHMAC: cloudauth.HMAC(segredo, n.sessao), Rotulo: "Android · Chrome",
		Agora: time.Now(), ExpiraEm: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	contas, err := cloudauth.ParseContas(emailDoE2E + "," + emailDaOutraConta)
	if err != nil {
		t.Fatal(err)
	}
	n.srv = httptest.NewUnstartedServer(nil)
	n.base = "http://localhost:" + strconv.Itoa(portaDe(n.srv))
	if limites == (server.Limites{}) {
		limites = server.Limites{PushPedidos: 1 << 20, PushMutacoes: 1 << 30}
	}
	var agora func() time.Time
	if relogio != nil {
		agora = relogio.agora
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: n.mem, Accounts: n.mem, BaseURL: n.base, SessionSecret: segredo, DeviceTokenPepper: n.pepper,
		Contas: contas, Google: authtest.NovoGoogleFalso(t, n.base+"/auth/google/callback", time.Now).Client(),
		Painel: contracttest.PainelDeTeste(), Logger: slog.New(slog.NewTextHandler(n.log, nil)), Limites: limites,
		Now: agora,
	})
	if err != nil {
		t.Fatal(err)
	}
	n.cloud = cloud
	n.srv.Config.Handler = n.porteiro.envolver(cloud.Handler())
	n.srv.Start()
	t.Cleanup(n.srv.Close)
	return n
}

// ─── O celular: a sessão do navegador, pelo CRUD ───

type itemDoCelular struct {
	ID               string     `json:"id"`
	DeviceID         *string    `json:"device_id"`
	RawText          string     `json:"raw_text"`
	FinalText        string     `json:"final_text"`
	DeletedAt        *time.Time `json:"deleted_at"`
	Version          int64      `json:"version"`
	VersaoEmConflito *struct {
		FinalText string  `json:"final_text"`
		DeviceID  *string `json:"device_id"`
	} `json:"versao_em_conflito"`
}

func (n *nuvemE2E) celular(metodo, caminho string, corpo any) (int, []byte) {
	n.t.Helper()
	var r io.Reader
	if corpo != nil {
		raw, _ := json.Marshal(corpo)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(metodo, n.base+caminho, r)
	req.AddCookie(&http.Cookie{Name: "__Host-sessao", Value: n.sessao})
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.t.Fatalf("celular %s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (n *nuvemE2E) celItem(id string) itemDoCelular {
	n.t.Helper()
	st, b := n.celular(http.MethodGet, "/api/v1/transcriptions/"+id, nil)
	var it itemDoCelular
	if st != http.StatusOK || json.Unmarshal(b, &it) != nil {
		n.t.Fatalf("celular GET %s = %d %s", id, st, b)
	}
	return it
}

// celLista é o histórico e a lixeira do celular.
func (n *nuvemE2E) celLista() []itemDoCelular {
	n.t.Helper()
	var todos []itemDoCelular
	for _, q := range []string{"", "?trash=true"} {
		st, b := n.celular(http.MethodGet, "/api/v1/transcriptions"+q, nil)
		var p struct{ Items []itemDoCelular }
		if st != http.StatusOK || json.Unmarshal(b, &p) != nil {
			n.t.Fatalf("celular lista = %d %s", st, b)
		}
		todos = append(todos, p.Items...)
	}
	return todos
}

func (n *nuvemE2E) celEditar(id, texto string) {
	n.t.Helper()
	atual := n.celItem(id)
	if st, b := n.celular(http.MethodPatch, "/api/v1/transcriptions/"+id, map[string]any{"base_version": atual.Version, "final_text": texto}); st != http.StatusOK {
		n.t.Fatalf("celular editar = %d %s", st, b)
	}
}

func (n *nuvemE2E) celLixeira(id string) {
	n.t.Helper()
	if st, b := n.celular(http.MethodDelete, "/api/v1/transcriptions/"+id, nil); st != http.StatusNoContent {
		n.t.Fatalf("celular lixeira = %d %s", st, b)
	}
}

func (n *nuvemE2E) celRestaurar(id string) {
	n.t.Helper()
	if st, b := n.celular(http.MethodPost, "/api/v1/transcriptions/"+id+"/restore", nil); st != http.StatusOK {
		n.t.Fatalf("celular restaurar = %d %s", st, b)
	}
}

func (n *nuvemE2E) celExcluirDeVez(id string) {
	n.t.Helper()
	if st, b := n.celular(http.MethodPost, "/api/v1/transcriptions/"+id+"/purge", nil); st != http.StatusNoContent {
		n.t.Fatalf("celular excluir de vez = %d %s", st, b)
	}
}

func (n *nuvemE2E) celRevogar(aparelho string) {
	n.t.Helper()
	if st, b := n.celular(http.MethodDelete, "/api/v1/devices/"+aparelho, nil); st != http.StatusNoContent {
		n.t.Fatalf("celular revogar = %d %s", st, b)
	}
}

// ─── Um PC ───

type pcE2E struct {
	t        *testing.T
	nuvem    *nuvemE2E
	caminho  string
	db       *sql.DB
	repo     *storage.SQLiteTranscriptionRepository
	dono     string
	aparelho string
	token    string
	cofre    *chaves.Memoria
	servico  *nuvem.Servico
	laco     *syncengine.Laco
	log      *contracttest.LockedBuffer
	logger   *slog.Logger
	// aplicador é o dicionario.json do aprendido (F8g); nil deixa o
	// aprendido de fora do laço, como na F7.
	aplicador syncengine.DicionarioDoAprendido
}

// conexaoDoTeste adapta o nuvem.Servico ao laço, como o cmd/agent faz
// (conexaoDaNuvem, que é do package main).
type conexaoDoTeste struct{ s *nuvem.Servico }

func (c conexaoDoTeste) Credencial(ctx context.Context) (syncengine.Credencial, error) {
	cr, err := c.s.Credencial(ctx)
	return syncengine.Credencial{Configurada: cr.Configurada, Conectado: cr.Conectado, URL: cr.URL,
		Token: cr.Token, Aparelho: cr.Aparelho, PareadoEm: cr.PareadoEm}, err
}

func (c conexaoDoTeste) TokenRecusado(ctx context.Context, token string) error {
	return c.s.TokenRecusado(ctx, token)
}

// novoPC liga um PC à conta: o aparelho e o token criados no Store de teste
// (o pareamento de verdade está em pareamento_e2e_test.go), a instalação e o
// Cofre como a F6 os deixa.
func (n *nuvemE2E) novoPC() *pcE2E {
	n.t.Helper()
	pc := &pcE2E{t: n.t, nuvem: n, caminho: filepath.Join(n.t.TempDir(), "historico.db"), dono: storage.NewID(),
		cofre: chaves.NovaMemoria(), log: &contracttest.LockedBuffer{}}
	pc.aparelho = cloudstore.NewUUIDv7()
	pc.token = cloudauth.NovoSegredo()
	n.mem.AddDevice(n.conta, pc.aparelho)
	n.mem.AddDeviceToken(pc.aparelho, cloudauth.HMAC(n.pepper, pc.token))
	if err := pc.cofre.Gravar(nuvem.NomeNoCofrePortatil, pc.token); err != nil {
		n.t.Fatal(err)
	}
	pc.abrir()
	if err := storage.NovaInstalacao(pc.db).Gravar(context.Background(), map[string]string{
		"nuvem_url": n.base, "nuvem_aparelho": pc.aparelho, "nuvem_conta": n.conta, "nuvem_email": emailDoE2E,
		"nuvem_nome": "PC de teste", "nuvem_conectado_em": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		n.t.Fatal(err)
	}
	return pc
}

// abrir abre o banco e monta o Agent (o repositório, o Servico e o laço), como
// numa subida.
func (pc *pcE2E) abrir() {
	pc.t.Helper()
	db, err := sql.Open("sqlite", pc.caminho)
	if err != nil {
		pc.t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		pc.t.Fatal(err)
	}
	pc.db = db
	pc.t.Cleanup(func() { _ = db.Close() })
	pc.repo = storage.NewSQLiteTranscriptionRepository(db)
	logger := slog.New(slog.NewTextHandler(pc.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pc.servico = nuvem.Novo(nuvem.Config{
		Cofre: pc.cofre, NomeNoCofre: nuvem.NomeNoCofrePortatil, Instalacao: storage.NovaInstalacao(db),
		Abrir:    func(string) error { return errors.New("sem navegador no teste") },
		Politica: nuvem.Politica{Dev: true, Portatil: true}, Versao: "0.1.0-sprint2", NomeDoPC: "PC de teste",
		NovoID: storage.NewID, Log: logger,
		// O envio inicial e o esquecer (F7c), com a cópia ao lado do banco
		// da pasta temporária, como o Agent monta (historicoDaNuvem).
		Historico: storage.HistoricoDaNuvem{Fila: pc.repo.Fila(pc.dono), CaminhoDoBanco: pc.caminho},
	})
	pc.logger = logger
	pc.laco = pc.novoLaco()
}

// novoLaco monta o laço do PC, com o aplicador do aprendido quando há, e
// liga o aviso do freio dele ao nuvem.Servico do PC, como o montarSync.
func (pc *pcE2E) novoLaco() *syncengine.Laco {
	var agora func() time.Time
	if pc.nuvem != nil && pc.nuvem.relogio != nil {
		agora = pc.nuvem.relogio.agora
	}
	l := syncengine.NovoLaco(syncengine.ConfigDoLaco{
		Fila: pc.repo.Fila(pc.dono), Conexao: conexaoDoTeste{pc.servico}, NovoID: storage.NewID, Log: pc.logger,
		Intervalo: time.Hour, Debounce: 10 * time.Millisecond, Agora: agora,
		Espera:     syncengine.BackoffPolicy{Base: 20 * time.Millisecond, Max: 50 * time.Millisecond},
		Dicionario: pc.aplicador,
	})
	pc.servico.UsarFreio(l.Freio)
	return l
}

// reiniciar fecha o banco e sobe o Agent de novo, no mesmo arquivo (o que um
// reinício do Windows faz no meio de um envio).
func (pc *pcE2E) reiniciar() {
	pc.t.Helper()
	if err := pc.db.Close(); err != nil {
		pc.t.Fatal(err)
	}
	pc.abrir()
}

// rodada é uma volta do laço, já.
func (pc *pcE2E) rodada() error {
	pc.t.Helper()
	return pc.laco.RodadaAgora(context.Background())
}

func (pc *pcE2E) rodadaOK() {
	pc.t.Helper()
	if err := pc.rodada(); err != nil {
		pc.t.Fatalf("rodada: %v\n%s", err, pc.log.String())
	}
}

func (pc *pcE2E) ditar(texto string) string {
	pc.t.Helper()
	agora := time.Now().UTC()
	id := storage.NewID()
	if err := pc.repo.Put(storage.Transcription{
		ID: id, UserID: pc.dono, SourcePlatform: "windows", RawText: "bruto: " + texto, FinalText: texto,
		Mode: "smart", ASRProvider: "whisper-local", Status: "done", CreatedAt: agora, UpdatedAt: agora,
		Version: 1, SyncState: storage.SituacaoNormal,
	}); err != nil {
		pc.t.Fatal(err)
	}
	return id
}

func (pc *pcE2E) linha(id string) storage.Transcription {
	pc.t.Helper()
	l, err := pc.repo.Get(context.Background(), pc.dono, id)
	if err != nil {
		pc.t.Fatalf("o PC não tem %s: %v", id, err)
	}
	return l
}

func (pc *pcE2E) temLinha(id string) bool {
	pc.t.Helper()
	_, err := pc.repo.Get(context.Background(), pc.dono, id)
	return err == nil
}

func (pc *pcE2E) editar(id, texto string) {
	pc.t.Helper()
	if _, err := pc.repo.UpdateFinalText(context.Background(), pc.dono, id, pc.linha(id).Version, texto); err != nil {
		pc.t.Fatal(err)
	}
}

func (pc *pcE2E) lixeira(id string) {
	pc.t.Helper()
	if err := pc.repo.SoftDelete(context.Background(), pc.dono, id, time.Now()); err != nil {
		pc.t.Fatal(err)
	}
}

func (pc *pcE2E) restaurar(id string) {
	pc.t.Helper()
	if err := pc.repo.Restore(context.Background(), pc.dono, id); err != nil {
		pc.t.Fatal(err)
	}
}

func (pc *pcE2E) naFila() int {
	pc.t.Helper()
	n, err := pc.repo.Fila(pc.dono).Contar(context.Background())
	if err != nil {
		pc.t.Fatal(err)
	}
	return n
}

// todos são os ditados do PC, no histórico e na lixeira.
func (pc *pcE2E) todos() []storage.Transcription {
	pc.t.Helper()
	var out []storage.Transcription
	for _, lixeira := range []bool{false, true} {
		p, err := pc.repo.List(context.Background(), storage.TranscriptionListFilter{UserID: pc.dono, Trash: lixeira, Limit: 100})
		if err != nil {
			pc.t.Fatal(err)
		}
		out = append(out, p.Items...)
	}
	return out
}

// semRepetidos falha se algum id aparece duas vezes.
func semRepetidos[T any](t *testing.T, onde string, itens []T, id func(T) string) {
	t.Helper()
	visto := map[string]bool{}
	for _, it := range itens {
		if visto[id(it)] {
			t.Errorf("%s: %s aparece duas vezes", onde, id(it))
		}
		visto[id(it)] = true
	}
}
