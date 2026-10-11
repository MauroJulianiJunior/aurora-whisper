package syncengine

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"

	_ "modernc.org/sqlite"
)

// O motor contra um SQLite de verdade e uma nuvem de mentira com roteiro: as
// regras de ordem (nada de pull depois de um push sem resposta; o reenvio
// com os mesmos ids antes de tudo), o recomeço, o epoch e o cursor. Os
// mesmos casos contra o servidor da nuvem de verdade estão na suíte de ponta
// a ponta (internal/server/integration).

const donoDoMotor = "0199a000-0000-7000-8000-00000000d0d0"

type pcDeTeste struct {
	db   *sql.DB
	repo *storage.SQLiteTranscriptionRepository
	fila *storage.Fila
}

func novoPC(t *testing.T) pcDeTeste {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a1'), ('nuvem_conectado_em', 'p1')`); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSQLiteTranscriptionRepository(db)
	return pcDeTeste{db: db, repo: repo, fila: repo.Fila(donoDoMotor)}
}

func (p pcDeTeste) ditar(t *testing.T, id, texto string) {
	t.Helper()
	agora := time.Now().UTC()
	if err := p.repo.Put(storage.Transcription{
		ID: id, UserID: donoDoMotor, SourcePlatform: "windows", RawText: "bruto " + texto, FinalText: texto,
		Mode: "smart", ASRProvider: "whisper-local", Status: "done", CreatedAt: agora, UpdatedAt: agora,
		Version: 1, SyncState: storage.SituacaoNormal,
	}); err != nil {
		t.Fatal(err)
	}
}

func (p pcDeTeste) cursor(t *testing.T) (int64, string) {
	t.Helper()
	var c int64
	var e sql.NullString
	if err := p.db.QueryRow(`SELECT last_server_seq, sync_epoch FROM sync_state`).Scan(&c, &e); err != nil {
		t.Fatal(err)
	}
	return c, e.String
}

// nuvemDeRoteiro é um SyncClient que responde o que o teste manda e anota a
// ordem dos pedidos.
type nuvemDeRoteiro struct {
	mu      sync.Mutex
	pedidos []string
	pushes  [][]Mutation
	push    func(muts []Mutation) (PushResult, error)
	pull    func(after int64) (ChangePage, error)
}

func (n *nuvemDeRoteiro) Push(_ context.Context, muts []Mutation) (PushResult, error) {
	n.mu.Lock()
	n.pedidos = append(n.pedidos, "push")
	n.pushes = append(n.pushes, muts)
	f := n.push
	n.mu.Unlock()
	return f(muts)
}

func (n *nuvemDeRoteiro) Pull(_ context.Context, after int64, _ int) (ChangePage, error) {
	n.mu.Lock()
	n.pedidos = append(n.pedidos, "pull")
	f := n.pull
	n.mu.Unlock()
	return f(after)
}

func aplicaTudo(epoch string) func([]Mutation) (PushResult, error) {
	return func(muts []Mutation) (PushResult, error) {
		r := PushResult{Epoch: epoch}
		for _, m := range muts {
			r.Results = append(r.Results, MutationResult{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 1})
		}
		return r, nil
	}
}

func paginaVazia(epoch string) func(int64) (ChangePage, error) {
	return func(after int64) (ChangePage, error) { return ChangePage{LastSeq: after, Epoch: epoch}, nil }
}

func novoMotor(p pcDeTeste, n SyncClient) *Motor {
	var seq int
	var mu sync.Mutex
	return &Motor{Fila: p.fila, Cliente: n, Aparelho: "a1", PareadoEm: "p1", NovoID: func() string {
		mu.Lock()
		defer mu.Unlock()
		seq++
		return "0199a000-0000-7000-8000-" + strings.Repeat("0", 11) + string(rune('0'+seq%10))
	}}
}

const (
	idM1 = "0199a000-0000-7000-8000-000000000101"
	idM2 = "0199a000-0000-7000-8000-000000000102"
)

// Um push sem resposta encerra a rodada SEM pull; a rodada seguinte começa
// pelo reenvio, com os MESMOS mutation_id, e só depois puxa.
func TestMotor_SemPullDepoisDeUmPushSemResposta(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	p.ditar(t, idM2, "dois")
	n := &nuvemDeRoteiro{
		push: func([]Mutation) (PushResult, error) { return PushResult{}, transitorio(CodigoRede) },
		pull: paginaVazia("e1"),
	}
	m := novoMotor(p, n)
	if _, err := m.Rodada(ctx); !errors.Is(err, ErrTransitorio) {
		t.Fatalf("Rodada = %v", err)
	}
	if strings.Join(n.pedidos, ",") != "push" {
		t.Fatalf("pedidos = %v; quero só o push, sem pull", n.pedidos)
	}
	primeiros := map[string]string{}
	for _, mut := range n.pushes[0] {
		primeiros[mut.EntityID] = mut.MutationID
	}
	ms, _ := p.fila.Marcadores(ctx)
	for _, mk := range ms {
		if mk.MutationID != primeiros[mk.EntityID] || mk.VersaoEnviada != 1 {
			t.Errorf("o marcador em voo: %+v", mk)
		}
	}

	// A rede volta: o reenvio sai com os mesmos ids, antes do pull.
	n.push = aplicaTudo("e1")
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(n.pedidos, ",") != "push,push,pull" {
		t.Fatalf("pedidos = %v", n.pedidos)
	}
	for _, mut := range n.pushes[1] {
		if mut.MutationID != primeiros[mut.EntityID] {
			t.Errorf("o reenvio de %s trocou o id", mut.EntityID)
		}
	}
	if c, _ := p.fila.Contar(ctx); c != 0 {
		t.Errorf("%d na fila depois do applied", c)
	}
}

// Um ditado gravado DURANTE o voo de outro entra no próximo lote da mesma
// rodada; e o que mudou durante o voo vai de novo.
func TestMotor_OQueMudaNoVooVaiDeNovo(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
	vezes := 0
	n.push = func(muts []Mutation) (PushResult, error) {
		vezes++
		if vezes == 1 {
			// Durante o voo: a pessoa edita o ditado.
			if _, err := p.repo.UpdateFinalText(ctx, donoDoMotor, idM1, 1, "um, editado"); err != nil {
				t.Error(err)
			}
		}
		return aplicaTudo("e1")(muts)
	}
	if _, err := novoMotor(p, n).Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if len(n.pushes) != 2 {
		t.Fatalf("%d pushes; quero 2 (o criar e a edição)", len(n.pushes))
	}
	segundo := n.pushes[1][0]
	if segundo.BaseVersion != 1 || !strings.Contains(string(segundo.Payload), "um, editado") ||
		segundo.MutationID == n.pushes[0][0].MutationID {
		t.Errorf("o segundo envio: %+v %s", segundo, segundo.Payload)
	}
}

// Um texto acima do teto da nuvem (uma linha de antes do teto no painel) nem
// sai: o ditado fica "recusado" aqui, sai da fila, e o resto sobe. Sem isso,
// um texto enorme passaria do corpo do push e travaria a fila inteira.
func TestMotor_TextoGrandeNaoSaiNemTrava(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	p.ditar(t, idM1, "grande")
	p.ditar(t, idM2, "normal")
	if _, err := p.db.Exec(`UPDATE local_transcriptions SET final_text = ? WHERE id = ?`,
		strings.Repeat("a", storage.MaxTextoBytes+1), idM1); err != nil {
		t.Fatal(err)
	}
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
	rel, err := novoMotor(p, n).Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.pushes) != 1 || len(n.pushes[0]) != 1 || n.pushes[0][0].EntityID != idM2 {
		t.Fatalf("o que subiu: %+v", n.pushes)
	}
	l, _ := p.repo.Get(ctx, donoDoMotor, idM1)
	if l.SyncState != storage.SituacaoRecusado || l.LastSyncError == nil || *l.LastSyncError != CodigoTextoGrande {
		t.Errorf("o grande: %s %v", l.SyncState, l.LastSyncError)
	}
	if c, _ := p.fila.Contar(ctx); c != 0 || rel.Recusados[CodigoTextoGrande] != 1 {
		t.Errorf("fila %d, relatório %+v", c, rel.Recusados)
	}
}

// O 410 do mesmo epoch: só o cursor volta a 0, e a rodada roda de novo, uma
// vez. Nada é reenviado (as versões da nuvem que o PC guardou continuam
// valendo), e a fila não muda. O segundo 410 seguido volta como erro.
func TestMotor_410ReleUmaVezSemReenviar(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(after int64) (ChangePage, error) {
		return ChangePage{LastSeq: 7, Epoch: "e1"}, nil
	}}
	m := novoMotor(p, n)
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	var afters []int64
	n.pull = func(after int64) (ChangePage, error) {
		afters = append(afters, after)
		if len(afters) == 1 {
			return ChangePage{}, ErrRecomeco
		}
		return ChangePage{LastSeq: 8, Epoch: "e1"}, nil
	}
	pushesAntes := len(n.pushes)
	rel, err := m.Rodada(ctx)
	if err != nil || rel.Releituras != 1 || rel.Recomecos != 0 {
		t.Fatalf("Rodada = %+v, %v", rel, err)
	}
	if len(afters) != 2 || afters[0] != 7 || afters[1] != 0 {
		t.Errorf("afters = %v; quero 7 (o 410) e depois 0", afters)
	}
	if len(n.pushes) != pushesAntes {
		t.Errorf("o 410 reenviou: %d pushes", len(n.pushes)-pushesAntes)
	}
	if l, _ := p.repo.Get(ctx, donoDoMotor, idM1); l.ServerVersion == nil || *l.ServerVersion != 1 {
		t.Errorf("a versão da nuvem depois do 410: %v", l.ServerVersion)
	}
	if c, e := p.cursor(t); c != 8 || e != "e1" {
		t.Errorf("cursor = %d, %q; quero 8 no mesmo epoch", c, e)
	}

	afters = nil
	n.pull = func(after int64) (ChangePage, error) {
		afters = append(afters, after)
		if len(afters) > 5 {
			// Um laço sem o limite pararia aqui, em vez de girar para sempre.
			return ChangePage{}, contrato(CodigoCursor)
		}
		return ChangePage{}, ErrRecomeco
	}
	if _, err := m.Rodada(ctx); !errors.Is(err, ErrRecomeco) {
		t.Fatalf("o 410 seguido = %v; quero o erro de volta", err)
	}
	if len(afters) != 2 {
		t.Errorf("%d pulls com o 410 seguido; quero 2 (a releitura uma vez só)", len(afters))
	}
}

// O 410 e o epoch novo na mesma rodada (o PC fora durante a restauração de um
// backup): a releitura do 0 traz o epoch novo, e o PC recomeça ali mesmo.
func TestMotor_410EEpochNovoNaMesmaRodada(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(int64) (ChangePage, error) { return ChangePage{LastSeq: 7, Epoch: "e1"}, nil }}
	m := novoMotor(p, n)
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	n.push = aplicaTudo("e2")
	var afters []int64
	n.pull = func(after int64) (ChangePage, error) {
		afters = append(afters, after)
		if len(afters) == 1 {
			return ChangePage{}, ErrRecomeco
		}
		return ChangePage{LastSeq: 3, Epoch: "e2"}, nil
	}
	rel, err := m.Rodada(ctx)
	if err != nil || rel.Releituras != 1 || rel.Recomecos != 1 {
		t.Fatalf("Rodada = %+v, %v", rel, err)
	}
	if c, e := p.cursor(t); c != 3 || e != "e2" {
		t.Errorf("cursor = %d, %q; quero 3 do epoch novo", c, e)
	}
	// O recomeço do epoch novo reenviou o ditado inteiro (a base vazia).
	ultimo := n.pushes[len(n.pushes)-1]
	if len(ultimo) != 1 || ultimo[0].EntityID != idM1 || ultimo[0].BaseVersion != 0 {
		t.Errorf("o reenvio do epoch novo: %+v", ultimo)
	}
}

// Reconectar (outro nuvem_conectado_em, ou outro aparelho) continua do
// cursor: nada é reenviado, e o pull segue de onde estava.
func TestMotor_ReconexaoContinuaDoCursor(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	var afters []int64
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(after int64) (ChangePage, error) {
		afters = append(afters, after)
		return ChangePage{LastSeq: 7, Epoch: "e1"}, nil
	}}
	if _, err := novoMotor(p, n).Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	m := novoMotor(p, n)
	m.Aparelho, m.PareadoEm = "a2", "p2"
	afters = nil
	pushesAntes := len(n.pushes)
	rel, err := m.Rodada(ctx)
	if err != nil || rel.Reconexoes != 1 || rel.Recomecos != 0 {
		t.Fatalf("Rodada = %+v, %v", rel, err)
	}
	if len(n.pushes) != pushesAntes || len(afters) != 1 || afters[0] != 7 {
		t.Errorf("a reconexão: %d pushes, afters %v; quero nenhum push e o pull do 7", len(n.pushes)-pushesAntes, afters)
	}
}

// Um epoch diferente do guardado descarta a resposta inteira e recomeça.
func TestMotor_EpochNovoRecomeca(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(after int64) (ChangePage, error) {
		return ChangePage{LastSeq: 3, Epoch: "e1", Changes: []Change{{Seq: 3, EntityType: storage.EntidadeDitado, EntityID: idM1, Operation: OpTombstone}}}, nil
	}}
	m := novoMotor(p, n)
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if c, e := p.cursor(t); c != 3 || e != "e1" {
		t.Fatalf("cursor = %d, %q", c, e)
	}
	// A nuvem restaurou um backup: outro epoch. A página dele é descartada
	// (o cursor não vai a 9), e o recomeço lê tudo de novo do 0.
	var afters []int64
	n.pull = func(after int64) (ChangePage, error) {
		afters = append(afters, after)
		return ChangePage{LastSeq: 9, Epoch: "e2"}, nil
	}
	rel, err := m.Rodada(ctx)
	if err != nil || rel.Recomecos != 1 {
		t.Fatalf("Rodada = %+v, %v", rel, err)
	}
	if len(afters) != 2 || afters[0] != 3 || afters[1] != 0 {
		t.Errorf("afters = %v; quero 3 (descartada) e depois 0", afters)
	}
	if c, e := p.cursor(t); c != 9 || e != "e2" {
		t.Errorf("cursor = %d, %q; quero 9 do epoch novo", c, e)
	}
}

// O cursor: a página aplicada o anda, a recusa por regra também, e uma
// página que diz "há mais" sem andar é falha (senão o laço giraria preso).
func TestMotor_Cursor(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	paginas := []ChangePage{
		{LastSeq: 2, HasMore: true, Epoch: "e1", Changes: []Change{
			{Seq: 1, EntityType: "learned", EntityID: idM1, Operation: OpUpsert, EntityVersion: 1}, // não vale aqui
			{Seq: 2, EntityType: storage.EntidadeDitado, EntityID: "lixo", Operation: OpTombstone}, // não vale aqui
		}},
		{LastSeq: 4, Epoch: "e1", Changes: []Change{
			{Seq: 4, EntityType: storage.EntidadeDitado, EntityID: idM2, Operation: OpUpsert, EntityVersion: 2,
				Payload: &Item{ID: idM2, FinalText: "de outro PC", RawText: "b", Mode: "smart", ASRProvider: "x", Status: "done",
					SourcePlatform: "windows", CreatedAt: time.Now().UTC(), Version: 2}},
		}},
	}
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(after int64) (ChangePage, error) {
		pg := paginas[0]
		paginas = paginas[1:]
		return pg, nil
	}}
	rel, err := novoMotor(p, n).Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := p.cursor(t); c != 4 {
		t.Errorf("cursor = %d; quero 4", c)
	}
	if rel.Pull[TipoNaoAplicado] != 2 || rel.Pull[TipoRecebido] != 1 || rel.Paginas != 2 {
		t.Errorf("relatório: %+v", rel)
	}
	l, err := p.repo.Get(ctx, donoDoMotor, idM2)
	if err != nil || l.FinalText != "de outro PC" || *l.ServerVersion != 2 {
		t.Errorf("o que veio: %+v, %v", l, err)
	}
	if c, _ := p.fila.Contar(ctx); c != 0 {
		t.Errorf("aplicar ecoou: %d na fila", c)
	}

	n.pull = func(after int64) (ChangePage, error) {
		return ChangePage{LastSeq: after, HasMore: true, Epoch: "e1"}, nil
	}
	if _, err := novoMotor(p, n).Rodada(ctx); !errors.Is(err, ErrContrato) {
		t.Errorf("o cursor que não anda = %v; quero ErrContrato", err)
	}
}
