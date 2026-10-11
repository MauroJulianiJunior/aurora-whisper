package contracttest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O ditado que nasce na nuvem (ADR-021, C1): o Store.Create e o Store.Situacao,
// contra o Store em memória e o Postgres. A rota que vai chamá-los é da C5b;
// aqui eles são chamados direto, e o resto (o celular, o PC) fala pela API,
// como em produção: o ditado criado tem de aparecer no GET do celular e no
// pull dos PCs, sem aparelho, e o PC tem de poder mudá-lo como a qualquer
// outro.

// ditadoDoApp é um ditado novo do app, como a rota o montaria depois do
// provedor e do Smart.
func ditadoDoApp(texto string) cloudstore.NewDictation {
	pt, modelo, dur, proc, conf := "pt", "gpt-4o-mini-transcribe", int64(2500), int64(1800), float32(0.9)
	return cloudstore.NewDictation{
		SourcePlatform: cloudstore.SourcePlatformWeb, RawText: "bruto: " + texto, FinalText: texto,
		Mode: "smart", Language: &pt, ASRProvider: "openai", ASRModel: &modelo,
		DurationMs: &dur, ProcessingMs: &proc, Confidence: &conf,
	}
}

// aSessao é o Actor da sessão do navegador: sem aparelho.
func aSessao(userID string) cloudstore.Actor { return cloudstore.Actor{UserID: userID} }

// situacao lê o que a conta sabe do id, e confere que não falhou.
func (w *world) situacao(userID, id string) cloudstore.SituacaoDoId {
	w.t.Helper()
	sit, err := w.b.Store.Situacao(context.Background(), userID, id)
	if err != nil {
		w.t.Fatalf("Situacao(%s): %v", id, err)
	}
	if sit.Epoch == "" {
		w.t.Fatalf("Situacao(%s) sem epoch", id)
	}
	return sit
}

// criarDoApp cria o ditado como a rota faria: lê o epoch pela Situacao, e grava
// com ele.
func (w *world) criarDoApp(userID, id, texto string) (cloudstore.Transcription, bool, error) {
	w.t.Helper()
	sit := w.situacao(userID, id)
	return w.b.Store.Create(context.Background(), aSessao(userID), sit.Epoch, id, ditadoDoApp(texto), w.clk.Now())
}

func (w *world) mustCriarDoApp(userID, id, texto string) cloudstore.Transcription {
	w.t.Helper()
	t, created, err := w.criarDoApp(userID, id, texto)
	if err != nil || !created {
		w.t.Fatalf("Create(%s): created=%v err=%v", id, created, err)
	}
	return t
}

// Criar grava o ditado pelo ponto único de gravação: a linha, o evento de sync
// sem aparelho, o seq, o GET do celular e o pull dos PCs.
func testCriarGravaPeloPontoUnico(t *testing.T, w *world) {
	id := newID()
	antes := w.situacao(w.userA, id)
	if antes.Estado != cloudstore.IdNada || antes.Ditado != nil {
		t.Fatalf("antes de criar a situação é %+v, quero nada", antes)
	}

	got := w.mustCriarDoApp(w.userA, id, "o ditado que nasceu no celular")
	agora := w.clk.Now().UTC()
	if got.ID != id || got.UserID != w.userA || got.DeviceID != nil || got.TextDeviceID != nil || got.TrashDeviceID != nil {
		t.Errorf("identidade: %+v", got)
	}
	if got.SourcePlatform != "web" || got.Status != "done" || got.Mode != "smart" || got.ASRProvider != "openai" ||
		got.FinalText != "o ditado que nasceu no celular" || got.RawText != "bruto: o ditado que nasceu no celular" {
		t.Errorf("campos: %+v", got)
	}
	if got.Version != 1 || got.TextVersion != 1 || got.TrashVersion != 1 || got.LastSeq != 1 {
		t.Errorf("versões/seq = %d %d %d / %d, quero 1 1 1 / 1", got.Version, got.TextVersion, got.TrashVersion, got.LastSeq)
	}
	if !got.CreatedAt.Equal(agora) || !got.UpdatedAt.Equal(agora) || got.DeletedAt != nil || got.PurgeAt != nil || got.Conflict != nil {
		t.Errorf("datas/lixeira/selo: %+v", got)
	}

	// O evento de sync sai sem aparelho, com o seq 1.
	evs := w.b.Events(t, w.userA)
	if len(evs) != 1 {
		t.Fatalf("%d eventos, quero 1: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Seq != 1 || ev.EntityType != cloudstore.EntityTranscription || ev.EntityID != id || ev.Operation != cloudstore.OpUpsert ||
		ev.EntityVersion != 1 || ev.DeviceID != nil || !ev.ChangedAt.Equal(agora) {
		t.Errorf("evento = %+v", ev)
	}

	// Depois de criar, a situação é o ditado, com o mesmo epoch.
	depois := w.situacao(w.userA, id)
	if depois.Estado != cloudstore.IdDitado || depois.Ditado == nil || depois.Ditado.FinalText != got.FinalText || depois.Epoch != antes.Epoch {
		t.Errorf("depois de criar a situação é %+v", depois)
	}

	// O celular o lê pela API, sem aparelho.
	it := w.get(celA, id)
	if it.DeviceID != nil || it.SourcePlatform != "web" || it.FinalText != got.FinalText || it.Version != 1 || it.Mode != "smart" {
		t.Errorf("GET do celular: %+v", it)
	}

	// O pull de cada PC da conta o entrega, como mudança sem aparelho.
	for _, pc := range []string{pcA1, pcA2} {
		p := w.pull(pc, 0, 0)
		c, ok := changeOf(p, id)
		if !ok {
			t.Fatalf("o pull de %s não entregou o ditado: %+v", pc, p)
		}
		if c.Seq != 1 || c.Operation != "upsert" || c.EntityType != "transcription" || c.EntityVersion != 1 || c.DeviceID != nil {
			t.Errorf("mudança de %s: %+v", pc, c)
		}
		if c.Payload == nil || c.Payload.SourcePlatform != "web" || c.Payload.DeviceID != nil || c.Payload.FinalText != got.FinalText {
			t.Errorf("payload de %s: %+v", pc, c.Payload)
		}
		if p.LastSeq != 1 || p.SyncEpoch != antes.Epoch {
			t.Errorf("pull de %s: last_seq=%d epoch=%s, quero 1 e o epoch lido", pc, p.LastSeq, p.SyncEpoch)
		}
	}

	// A outra conta não vê nada.
	if p := w.pull(pcB, 0, 0); len(p.Changes) != 0 {
		t.Errorf("a outra conta recebeu %+v", p.Changes)
	}
	if rec := w.do(http.MethodGet, "/api/v1/transcriptions/"+id, celB, nil); rec.Code != http.StatusNotFound {
		t.Errorf("a outra conta lê o ditado: %d", rec.Code)
	}

	// O seq segue sem buraco: o PC cria o seguinte.
	w.mustCreate(pcA1, newID(), "do PC")
	requireContiguous(t, w.seqsOf(w.userA), 2)
}

// O mesmo id não cria dois: quem confere é o id, o usuário e o epoch, nunca o
// texto. O segundo recebe o que o primeiro gravou, sem seq nem evento novo,
// também com o ditado na lixeira.
func testCriarOMesmoIdNaoCriaDois(t *testing.T, w *world) {
	id := newID()
	primeiro := w.mustCriarDoApp(w.userA, id, "o primeiro texto")

	segundo, created, err := w.criarDoApp(w.userA, id, "outro texto, que não vale")
	if err != nil || created {
		t.Fatalf("o segundo: created=%v err=%v, quero created=false", created, err)
	}
	if segundo.FinalText != "o primeiro texto" || segundo.Version != 1 || segundo.LastSeq != 1 || !segundo.CreatedAt.Equal(primeiro.CreatedAt) {
		t.Errorf("o segundo recebeu %+v, quero o que o primeiro gravou", segundo)
	}
	requireContiguous(t, w.seqsOf(w.userA), 1)
	if it := w.get(celA, id); it.FinalText != "o primeiro texto" || it.Version != 1 {
		t.Errorf("o ditado mudou: %+v", it)
	}

	// Na lixeira, continua sendo o mesmo ditado.
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("lixeira: %d %s", rec.Code, rec.Body.String())
	}
	if sit := w.situacao(w.userA, id); sit.Estado != cloudstore.IdDitado || sit.Ditado == nil || sit.Ditado.DeletedAt == nil {
		t.Fatalf("na lixeira a situação é %+v, quero o ditado com deleted_at", sit)
	}
	terceiro, created, err := w.criarDoApp(w.userA, id, "um terceiro texto")
	if err != nil || created || terceiro.DeletedAt == nil || terceiro.FinalText != "o primeiro texto" || terceiro.Version != 2 {
		t.Fatalf("o terceiro, com o ditado na lixeira: created=%v err=%v %+v", created, err, terceiro)
	}
	requireContiguous(t, w.seqsOf(w.userA), 2) // a criação e a lixeira
}

// O mesmo id em duas contas são dois ditados: uma conta nunca vê, espera nem
// recebe o da outra.
func testCriarOMesmoIdEmDuasContas(t *testing.T, w *world) {
	id := newID()
	a := w.mustCriarDoApp(w.userA, id, "texto da conta A")
	b := w.mustCriarDoApp(w.userB, id, "texto da conta B")
	if a.UserID != w.userA || b.UserID != w.userB || a.FinalText == b.FinalText {
		t.Fatalf("A=%+v B=%+v", a, b)
	}
	if got := w.get(celA, id); got.FinalText != "texto da conta A" {
		t.Errorf("A lê %q", got.FinalText)
	}
	if got := w.get(celB, id); got.FinalText != "texto da conta B" {
		t.Errorf("B lê %q", got.FinalText)
	}
	requireContiguous(t, w.seqsOf(w.userA), 1)
	requireContiguous(t, w.seqsOf(w.userB), 1)
	if c, ok := changeOf(w.pull(pcB, 0, 0), id); !ok || c.Payload == nil || c.Payload.FinalText != "texto da conta B" {
		t.Errorf("o pull de B: %+v", c)
	}
}

// O Create com o epoch velho recusa e não grava: a conta apagou os dados
// depois de o epoch ser lido. Um epoch vazio nunca confere.
func testCriarComEpochVelhoRecusaENaoGrava(t *testing.T, w *world) {
	ctx := context.Background()
	w.mustCreate(pcA1, newID(), "antes de apagar") // o contador anda: seq 1
	id := newID()
	lido := w.situacao(w.userA, id)

	novo, err := w.b.Accounts.ApagarDadosDaConta(ctx, w.userA, newID(), w.clk.Now())
	if err != nil {
		t.Fatalf("ApagarDadosDaConta: %v", err)
	}
	if novo == lido.Epoch {
		t.Fatal("o epoch não mudou")
	}

	_, created, err := w.b.Store.Create(ctx, aSessao(w.userA), lido.Epoch, id, ditadoDoApp("não pode gravar"), w.clk.Now())
	if !errors.Is(err, cloudstore.ErrEpochChanged) || created {
		t.Fatalf("Create com o epoch velho: created=%v err=%v, quero ErrEpochChanged", created, err)
	}
	if _, created, err := w.b.Store.Create(ctx, aSessao(w.userA), "", id, ditadoDoApp("sem epoch"), w.clk.Now()); !errors.Is(err, cloudstore.ErrEpochChanged) || created {
		t.Fatalf("Create sem epoch: created=%v err=%v, quero ErrEpochChanged", created, err)
	}
	if _, err := w.b.Store.Get(ctx, w.userA, id); !errors.Is(err, cloudstore.ErrNotFound) {
		t.Fatalf("o ditado foi gravado com o epoch velho: %v", err)
	}
	if evs := w.b.Events(t, w.userA); len(evs) != 0 {
		t.Fatalf("há eventos depois do apagar e da recusa: %+v", evs)
	}
	p, err := w.b.Store.Pull(ctx, w.userA, 0, 10)
	if err != nil || len(p.Changes) != 0 || p.Epoch != novo {
		t.Fatalf("pull depois da recusa: %+v, %v (epoch novo %s)", p, err, novo)
	}

	// Com o epoch de agora, grava. O contador não voltou a zero: o seq do
	// primeiro ditado depois do apagar é o 2 (a recusa não gastou nenhum).
	got, created, err := w.b.Store.Create(ctx, aSessao(w.userA), novo, id, ditadoDoApp("agora pode"), w.clk.Now())
	if err != nil || !created || got.LastSeq != 2 {
		t.Fatalf("Create com o epoch novo: created=%v err=%v %+v, quero created com seq 2", created, err, got)
	}
}

// O Create recusa o Actor com aparelho: o ditado da nuvem não nasce de um PC, e
// o evento não pode ser atribuído a um.
func testCriarRecusaOActorComAparelho(t *testing.T, w *world) {
	id := newID()
	sit := w.situacao(w.userA, id)
	_, created, err := w.b.Store.Create(context.Background(), cloudstore.Actor{UserID: w.userA, DeviceID: w.devA1}, sit.Epoch, id, ditadoDoApp("de um PC"), w.clk.Now())
	if !errors.Is(err, cloudstore.ErrDeviceNotAllowed) || created {
		t.Fatalf("created=%v err=%v, quero ErrDeviceNotAllowed", created, err)
	}
	if evs := w.b.Events(t, w.userA); len(evs) != 0 {
		t.Fatalf("eventos: %+v", evs)
	}
}

// Um ditado inválido nem trava o contador nem gasta seq: o seq do próximo é o 1.
func testCriarInvalidoNaoGastaSeq(t *testing.T, w *world) {
	ctx := context.Background()
	id := newID()
	sit := w.situacao(w.userA, id)
	doPC, modoRuim := ditadoDoApp("x"), ditadoDoApp("x")
	doPC.SourcePlatform, modoRuim.Mode = "windows", "turbo"
	invalidos := map[string]cloudstore.NewDictation{
		"origem do PC":      doPC,
		"modo":              modoRuim,
		"NUL":               ditadoDoApp("com\x00nulo"),
		"acima do teto":     ditadoDoApp(strings.Repeat("a", cloudstore.MaxTextBytes+1)),
		"UTF-8 inválido":    ditadoDoApp("\xff\xfe"),
		"com o texto vazio": {},
	}
	for nome, n := range invalidos {
		if _, created, err := w.b.Store.Create(ctx, aSessao(w.userA), sit.Epoch, id, n, w.clk.Now()); !errors.Is(err, cloudstore.ErrInvalidDictation) || created {
			t.Errorf("%s: created=%v err=%v, quero ErrInvalidDictation", nome, created, err)
		}
	}
	if _, created, err := w.b.Store.Create(ctx, aSessao(w.userA), sit.Epoch, "nao-e-uuid", ditadoDoApp("x"), w.clk.Now()); !errors.Is(err, cloudstore.ErrInvalidID) || created {
		t.Errorf("id inválido: created=%v err=%v, quero ErrInvalidID", created, err)
	}
	if _, created, err := w.b.Store.Create(ctx, aSessao(newID()), sit.Epoch, id, ditadoDoApp("x"), w.clk.Now()); !errors.Is(err, cloudstore.ErrUnknownUser) || created {
		t.Errorf("usuário sem contador: created=%v err=%v, quero ErrUnknownUser", created, err)
	}
	if evs := w.b.Events(t, w.userA); len(evs) != 0 {
		t.Fatalf("eventos depois das recusas: %+v", evs)
	}
	w.mustCriarDoApp(w.userA, id, "o primeiro de verdade")
	requireContiguous(t, w.seqsOf(w.userA), 1)
}

// O id com lápide não é recriado: o ditado foi apagado de vez.
func testCriarComLapide(t *testing.T, w *world) {
	ctx := context.Background()
	id := newID()
	w.mustCreate(pcA1, id, "vai ser apagado de vez")
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("lixeira: %d %s", rec.Code, rec.Body.String())
	}
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/purge", celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("excluir de vez: %d %s", rec.Code, rec.Body.String())
	}
	antes := len(w.b.Events(t, w.userA))

	sit := w.situacao(w.userA, id)
	if sit.Estado != cloudstore.IdLapide || sit.Ditado != nil {
		t.Fatalf("a situação é %+v, quero a lápide", sit)
	}
	if _, err := w.b.Store.Get(ctx, w.userA, id); !errors.Is(err, cloudstore.ErrNotFound) {
		t.Fatalf("o Get mostra o que a lápide esconde: %v", err)
	}
	if _, created, err := w.b.Store.Create(ctx, aSessao(w.userA), sit.Epoch, id, ditadoDoApp("de volta"), w.clk.Now()); !errors.Is(err, cloudstore.ErrTombstoned) || created {
		t.Fatalf("Create com lápide: created=%v err=%v, quero ErrTombstoned", created, err)
	}
	if got := len(w.b.Events(t, w.userA)); got != antes {
		t.Errorf("o Create recusado deixou eventos: %d, eram %d", got, antes)
	}
	// A lápide é da conta: o mesmo id na outra conta é livre.
	if other := w.situacao(w.userB, id); other.Estado != cloudstore.IdNada {
		t.Errorf("na outra conta a situação é %+v, quero nada", other)
	}
}

// A Situacao diz ditado, lápide ou nada para (user_id, id), e o epoch da conta
// junto; nunca mostra o que é de outra conta.
func testSituacao(t *testing.T, w *world) {
	ctx := context.Background()
	st := w.b.Store

	livre := newID()
	if sit := w.situacao(w.userA, livre); sit.Estado != cloudstore.IdNada || sit.Ditado != nil {
		t.Errorf("id livre: %+v", sit)
	}

	// Um ditado do PC (não só o do celular) é "ditado", com o texto.
	doPC := newID()
	w.mustCreate(pcA1, doPC, "ditado no PC")
	sit := w.situacao(w.userA, doPC)
	if sit.Estado != cloudstore.IdDitado || sit.Ditado == nil || sit.Ditado.FinalText != "ditado no PC" || sit.Ditado.DeviceID == nil || *sit.Ditado.DeviceID != w.devA1 {
		t.Errorf("ditado do PC: %+v", sit)
	}
	// O epoch é o do pull.
	if p := w.pull(pcA1, 0, 1); p.SyncEpoch != sit.Epoch {
		t.Errorf("epoch da Situacao %q, do pull %q", sit.Epoch, p.SyncEpoch)
	}

	// Na lixeira ainda é ditado; excluído de vez, é lápide.
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+doPC, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("lixeira: %d", rec.Code)
	}
	if sit := w.situacao(w.userA, doPC); sit.Estado != cloudstore.IdDitado || sit.Ditado == nil || sit.Ditado.DeletedAt == nil {
		t.Errorf("na lixeira: %+v", sit)
	}
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+doPC+"/purge", celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("excluir de vez: %d", rec.Code)
	}
	if sit := w.situacao(w.userA, doPC); sit.Estado != cloudstore.IdLapide || sit.Ditado != nil {
		t.Errorf("excluído de vez: %+v", sit)
	}

	// O id é por conta: o ditado de A é "nada" para B, e a lápide também.
	if sit := w.situacao(w.userB, doPC); sit.Estado != cloudstore.IdNada {
		t.Errorf("o id da lápide de A, na conta B: %+v", sit)
	}
	doApp := newID()
	w.mustCriarDoApp(w.userA, doApp, "do app")
	if sit := w.situacao(w.userB, doApp); sit.Estado != cloudstore.IdNada || sit.Ditado != nil {
		t.Errorf("o ditado de A, na conta B: %+v", sit)
	}

	// A leitura não gasta seq.
	antes := len(w.b.Events(t, w.userA))
	for i := 0; i < 3; i++ {
		w.situacao(w.userA, doApp)
	}
	if got := len(w.b.Events(t, w.userA)); got != antes {
		t.Errorf("a Situacao gravou eventos: %d, eram %d", got, antes)
	}

	// O que não é UUID, e o usuário que não existe.
	for _, ruim := range []string{"", "nao-e-uuid", doApp + "0"} {
		if _, err := st.Situacao(ctx, w.userA, ruim); !errors.Is(err, cloudstore.ErrInvalidID) {
			t.Errorf("Situacao(%q): %v, quero ErrInvalidID", ruim, err)
		}
	}
	if _, err := st.Situacao(ctx, newID(), livre); !errors.Is(err, cloudstore.ErrUnknownUser) {
		t.Errorf("usuário sem contador: %v, quero ErrUnknownUser", err)
	}

	// Depois do "apagar meus dados", tudo é nada, com o epoch novo.
	novo, err := w.b.Accounts.ApagarDadosDaConta(ctx, w.userA, newID(), w.clk.Now())
	if err != nil {
		t.Fatalf("ApagarDadosDaConta: %v", err)
	}
	for _, id := range []string{doPC, doApp} {
		if sit := w.situacao(w.userA, id); sit.Estado != cloudstore.IdNada || sit.Epoch != novo {
			t.Errorf("depois de apagar a situação de %s é %+v, quero nada com o epoch %s", id, sit, novo)
		}
	}
}

// O ditado criado na nuvem é um ditado como os outros: o PC o edita pelo push
// sobre a versão 1 (applied, sem mescla nem selo), o celular o edita pelo CRUD
// depois, e o PC recebe as duas mudanças.
func testCriarOPCMudaOQueONascimentoDoApp(t *testing.T, w *world) {
	id := newID()
	w.mustCriarDoApp(w.userA, id, "nasceu no celular")
	if c, ok := changeOf(w.pull(pcA1, 0, 0), id); !ok || c.EntityVersion != 1 {
		t.Fatalf("o PC não recebeu a versão 1: %+v", c)
	}

	r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "editado no PC"}))
	if r.Status != "applied" || r.EntityVersion != 2 || r.Mesclado {
		t.Fatalf("a edição do PC sobre a versão 1: %+v, quero applied v2 sem mescla", r)
	}
	if rec := w.edit(celA, id, 2, "editado no celular"); rec.Code != http.StatusOK {
		t.Fatalf("edição do celular sobre a versão 2: %d %s", rec.Code, rec.Body.String())
	}
	it := w.get(celA, id)
	if it.FinalText != "editado no celular" || it.Version != 3 || it.VersaoEmConflito != nil || it.SourcePlatform != "web" || it.DeviceID != nil {
		t.Errorf("o ditado depois das duas edições: %+v", it)
	}
	if c, ok := changeOf(w.pull(pcA2, 0, 0), id); !ok || c.EntityVersion != 3 || c.Payload == nil || c.Payload.FinalText != "editado no celular" {
		t.Errorf("o outro PC recebeu %+v", c)
	}
	requireContiguous(t, w.seqsOf(w.userA), 3)
}

// Os que criam o mesmo id ao mesmo tempo gravam um ditado só, e todos recebem o
// mesmo; os de ids diferentes gravam cada um o seu, e o seq segue sem buraco.
func testCriarConcorrente(t *testing.T, w *world) {
	ctx := context.Background()
	const mesmo, distintos = 8, 6
	id := newID()
	epoch := w.situacao(w.userA, id).Epoch

	type saida struct {
		t       cloudstore.Transcription
		created bool
		err     error
	}
	mesmos := make([]saida, mesmo)
	var wg sync.WaitGroup
	for i := range mesmos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := ditadoDoApp("texto da disputa " + string(rune('A'+i)))
			mesmos[i].t, mesmos[i].created, mesmos[i].err = w.b.Store.Create(ctx, aSessao(w.userA), epoch, id, n, w.clk.Now())
		}()
	}
	for i := 0; i < distintos; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outro := newID()
			if _, created, err := w.b.Store.Create(ctx, aSessao(w.userA), epoch, outro, ditadoDoApp("texto do "+outro[:8]), w.clk.Now()); err != nil || !created {
				t.Errorf("Create de %s: created=%v err=%v", outro, created, err)
			}
		}()
	}
	wg.Wait()

	criados := 0
	var vencedor string
	for i, s := range mesmos {
		if s.err != nil {
			t.Fatalf("a disputa %d: %v", i, s.err)
		}
		if s.created {
			criados++
			vencedor = s.t.FinalText
		}
	}
	if criados != 1 {
		t.Fatalf("%d criaram o mesmo id, quero 1", criados)
	}
	for i, s := range mesmos {
		if s.t.ID != id || s.t.FinalText != vencedor || s.t.Version != 1 {
			t.Errorf("a disputa %d recebeu %+v, quero o texto do vencedor (%q)", i, s.t, vencedor)
		}
	}
	if got := w.get(celA, id); got.FinalText != vencedor {
		t.Errorf("o ditado gravado é %q, quero %q", got.FinalText, vencedor)
	}
	requireContiguous(t, w.seqsOf(w.userA), 1+distintos)
}
