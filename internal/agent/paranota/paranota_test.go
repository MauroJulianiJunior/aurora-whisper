package paranota

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"

	_ "modernc.org/sqlite" // driver puro Go, só para teste (ADR-008).
)

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func nota(id, titulo string, editada time.Duration) storage.Nota {
	return storage.Nota{ID: id, Titulo: titulo, CreatedAt: t0, UpdatedAt: t0.Add(editada)}
}

// TestEscolher é a tabela do destino pelo título.
// A regra (Escolher, Acrescentar, o texto da pílula) tem os casos dela em
// internal/notas; aqui ficam a ponte com a nota do PC e Gravar.

// TestEscolher_PelaNotaDoPC: o Escolher daqui olha o título e as datas da
// storage.Nota (o empate vai para a editada por último).
func TestEscolher_PelaNotaDoPC(t *testing.T) {
	ns := []storage.Nota{
		nota("antiga", "Mercado", 0),
		nota("nova", "mercado", time.Hour),
		nota("outra", "Lista", 0),
	}
	i, resto := Escolher(ns, "mercado: tomate")
	if i < 0 || ns[i].ID != "nova" || resto != "tomate" {
		t.Fatalf("Escolher = %d, %q; quer a editada por último e o resto", i, resto)
	}
	if i, _ := Escolher(ns, "Comprar pão"); i != -1 {
		t.Fatalf("sem título no começo: %d, quer -1", i)
	}
}

func TestResultado_Pilula(t *testing.T) {
	if (Resultado{Nova: true}).Pilula() != TextoNotaNova {
		t.Fatal("nota nova: texto errado")
	}
	if (Resultado{Titulo: "Mercado", Linhas: 2}).Pilula() != "Mercado +2" {
		t.Fatal("resultado: texto errado")
	}
	if got := TextoDaPilula("  Lista   de\tcompras ", 1); got != "Lista de compras +1" {
		t.Fatalf("espaços: got %q", got)
	}
	if !strings.HasSuffix(TextoDaPilula(strings.Repeat("palavra ", 12), 12), "… +12") {
		t.Fatal("título longo: sem o corte")
	}
}

// ---------------------------------------------------------------------
// Gravar, no banco de verdade (SQLite em memória)
// ---------------------------------------------------------------------

const dono = "dono-1"

func repoDeVerdade(t *testing.T) *storage.SQLiteNotaRepository {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	r := storage.NewSQLiteNotaRepository(db)
	agora := t0
	r.UsarRelogio(func() time.Time { agora = agora.Add(time.Second); return agora })
	return r
}

func criar(t *testing.T, r *storage.SQLiteNotaRepository, titulo, corpo string) storage.Nota {
	t.Helper()
	n, err := r.Criar(context.Background(), storage.NovaNota{UserID: dono, Titulo: titulo, Corpo: corpo})
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}
	return n
}

func TestGravar_AcrescentaNaNotaComOTitulo(t *testing.T) {
	ctx := context.Background()
	r := repoDeVerdade(t)
	alvo := criar(t, r, "Lista de compras", "- [ ] Arroz\n- [x] Feijão")
	criar(t, r, "Outra", "x")

	res, err := Gravar(ctx, r, dono, "ditado-1", "Lista de compras: leite, pão e café.")
	if err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	if res.NotaID != alvo.ID || res.Nova || res.Linhas != 3 || res.Pilula() != "Lista de compras +3" {
		t.Fatalf("resultado %+v", res)
	}
	n, _ := r.Get(ctx, dono, alvo.ID)
	if n.Corpo != "- [ ] Arroz\n- [ ] Leite\n- [ ] Pão\n- [ ] Café\n- [x] Feijão" {
		t.Fatalf("corpo %q", n.Corpo)
	}
	if n.Version != alvo.Version+1 {
		t.Fatalf("versão %d, quer %d", n.Version, alvo.Version+1)
	}
}

// TestGravar_AcrescentarNaoMoveANotaENotaNovaVaiAoTopo: a ordem manual (ADR-022,
// N2b). Acrescentar a uma nota existente não a tira de onde a pessoa a pôs; o
// ditado que vira nota nova entra no topo.
func TestGravar_AcrescentarNaoMoveANotaENotaNovaVaiAoTopo(t *testing.T) {
	ctx := context.Background()
	r := repoDeVerdade(t)
	alvo := criar(t, r, "Lista de compras", "- [ ] Arroz")
	criar(t, r, "Meio", "x")
	criar(t, r, "Topo", "y") // lista: Topo, Meio, Lista de compras
	antes, _ := r.Get(ctx, dono, alvo.ID)

	if _, err := Gravar(ctx, r, dono, "d1", "Lista de compras: leite."); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	depois, _ := r.Get(ctx, dono, alvo.ID)
	if depois.Posicao != antes.Posicao || depois.Version != antes.Version+1 {
		t.Fatalf("o acréscimo mexeu na posição: %q → %q", antes.Posicao, depois.Posicao)
	}
	res, err := Gravar(ctx, r, dono, "d2", "Ligar para o contador amanhã.")
	if err != nil || !res.Nova {
		t.Fatalf("Gravar nova: %v %+v", err, res)
	}
	lista, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
	if len(lista) != 4 || lista[0].ID != res.NotaID || lista[3].ID != alvo.ID {
		t.Fatalf("a nota nova devia abrir a lista e a acrescentada ficar onde estava: %v", lista)
	}
}

func TestGravar_SemTituloNasceNotaNova(t *testing.T) {
	ctx := context.Background()
	r := repoDeVerdade(t)
	criar(t, r, "Lista de compras", "- [ ] Arroz")
	texto := "Ligar para o contador amanhã cedo."

	res, err := Gravar(ctx, r, dono, "ditado-7", texto)
	if err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	if !res.Nova || res.Pilula() != TextoNotaNova {
		t.Fatalf("resultado %+v", res)
	}
	n, _ := r.Get(ctx, dono, res.NotaID)
	if n.Corpo != texto || n.Titulo != "" {
		t.Fatalf("nota nova: título %q, corpo %q; quer o ditado inteiro, sem título", n.Titulo, n.Corpo)
	}
	if n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != "ditado-7" {
		t.Fatalf("DitadoDeOrigem = %v", n.DitadoDeOrigem)
	}
}

// TestGravar_ArquivadaELixeiraNaoSaoDestino: decisão da N9 — só as notas
// ativas. A arquivada e a da lixeira com o mesmo título não recebem nada; o
// ditado vira nota nova, à vista.
func TestGravar_ArquivadaELixeiraNaoSaoDestino(t *testing.T) {
	ctx := context.Background()
	r := repoDeVerdade(t)
	arq := criar(t, r, "Mercado", "- [ ] Arroz")
	arquivar := true
	if _, err := r.Atualizar(ctx, dono, arq.ID, arq.Version, storage.MudancaDaNota{Arquivada: &arquivar}); err != nil {
		t.Fatal(err)
	}
	lix := criar(t, r, "Mercado", "- [ ] Sal")
	if err := r.SoftDelete(ctx, dono, lix.ID, t0); err != nil {
		t.Fatal(err)
	}

	res, err := Gravar(ctx, r, dono, "d", "mercado: tomate")
	if err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	if !res.Nova {
		t.Fatalf("a arquivada ou a da lixeira recebeu o ditado: %+v", res)
	}
	for _, id := range []string{arq.ID, lix.ID} {
		n, _ := r.Get(ctx, dono, id)
		if strings.Contains(n.Corpo, "omate") {
			t.Fatalf("nota %s mudou: %q", id, n.Corpo)
		}
	}
}

// repoQueMuda é o banco de verdade com o painel salvando a nota de destino uma
// vez, entre a leitura e a gravação do Gravar.
type repoQueMuda struct {
	*storage.SQLiteNotaRepository
	mudou bool
}

func (r *repoQueMuda) Atualizar(ctx context.Context, userID, id string, base int64, m storage.MudancaDaNota) (storage.Nota, error) {
	if !r.mudou {
		r.mudou = true
		corpo := "- [ ] Arroz\n- [ ] Editado no painel"
		if _, err := r.SQLiteNotaRepository.Atualizar(ctx, userID, id, base, storage.MudancaDaNota{Corpo: &corpo}); err != nil {
			return storage.Nota{}, err
		}
	}
	return r.SQLiteNotaRepository.Atualizar(ctx, userID, id, base, m)
}

func TestGravar_NotaMudouNoMeioLeDeNovo(t *testing.T) {
	ctx := context.Background()
	base := repoDeVerdade(t)
	alvo := criar(t, base, "Mercado", "- [ ] Arroz")
	r := &repoQueMuda{SQLiteNotaRepository: base}

	if _, err := Gravar(ctx, r, dono, "d", "mercado: sal"); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	n, _ := base.Get(ctx, dono, alvo.ID)
	if n.Corpo != "- [ ] Arroz\n- [ ] Editado no painel\n- [ ] Sal" {
		t.Fatalf("a edição do painel se perdeu ou o item não entrou: %q", n.Corpo)
	}
}

// repoQuebrado falha em tudo.
type repoQuebrado struct{}

var errDisco = errors.New("disco cheio")

func (repoQuebrado) Listar(context.Context, storage.FiltroDeNotas) ([]storage.Nota, error) {
	return nil, errDisco
}
func (repoQuebrado) Atualizar(context.Context, string, string, int64, storage.MudancaDaNota) (storage.Nota, error) {
	return storage.Nota{}, errDisco
}
func (repoQuebrado) Criar(context.Context, storage.NovaNota) (storage.Nota, error) {
	return storage.Nota{}, errDisco
}

func TestGravar_FalhaVoltaComoErro(t *testing.T) {
	if _, err := Gravar(context.Background(), repoQuebrado{}, dono, "d", "mercado: sal"); !errors.Is(err, errDisco) {
		t.Fatalf("err = %v, quer o erro do banco", err)
	}
	if _, err := Gravar(context.Background(), repoQuebrado{}, dono, "d", "   "); !errors.Is(err, ErrNadaParaGravar) {
		t.Fatalf("ditado vazio: err = %v", err)
	}
}

// TestGravar_CorpoGrandeDemaisFalha: passar do teto do corpo é erro, e quem
// chama deixa o ditado no histórico.
func TestGravar_CorpoGrandeDemaisFalha(t *testing.T) {
	ctx := context.Background()
	r := repoDeVerdade(t)
	criar(t, r, "Mercado", strings.Repeat("a", storage.MaxCorpoDaNotaBytes-10))
	if _, err := Gravar(ctx, r, dono, "d", "mercado: "+strings.Repeat("b", 100)); !errors.Is(err, storage.ErrCorpoGrande) {
		t.Fatalf("err = %v, quer ErrCorpoGrande", err)
	}
}
