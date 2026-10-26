package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os testes da ordem manual das notas (ADR-022, "Ordem manual (N2b)"): a nota
// nova no topo, editar não move, fixar/arquivar/restaurar levam ao topo do
// grupo de destino, e Mover.

func listaDe(t *testing.T, r *storage.SQLiteNotaRepository, v storage.VistaDasNotas) []string {
	t.Helper()
	ns, err := r.Listar(context.Background(), storage.FiltroDeNotas{UserID: dono, Vista: v})
	if err != nil {
		t.Fatal(err)
	}
	return titulos(ns)
}

// quatro cria a, b, c, d (d é a mais nova, logo a primeira) e as devolve.
func quatro(t *testing.T, r *storage.SQLiteNotaRepository, rel *relogioDeNotas) (a, b, c, d storage.Nota) {
	t.Helper()
	mk := func(titulo string) storage.Nota {
		rel.Andar(time.Minute)
		return criarNota(t, r, titulo, "corpo "+titulo)
	}
	return mk("a"), mk("b"), mk("c"), mk("d")
}

func TestOrdem_NotaNovaEntraNoTopoDoGrupo(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a, b, _, _ := quatro(t, r, rel)
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"d", "c", "b", "a"}) {
		t.Fatalf("depois de criar: %v", got)
	}
	// Fixadas: a nota nova fixada entra no topo das fixadas, não acima delas por
	// engano, e as comuns ficam como estavam.
	for _, n := range []storage.Nota{a, b} {
		rel.Andar(time.Minute)
		if _, err := r.Atualizar(ctx, dono, n.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)}); err != nil {
			t.Fatal(err)
		}
	}
	// b foi fixada por último: topo das fixadas.
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"b", "a", "d", "c"}) {
		t.Fatalf("fixar leva ao topo do grupo das fixadas: %v", got)
	}
	rel.Andar(time.Minute)
	if _, err := r.Criar(ctx, storage.NovaNota{UserID: dono, Titulo: "e", Corpo: "x", Fixada: true}); err != nil {
		t.Fatal(err)
	}
	rel.Andar(time.Minute)
	if _, err := r.Criar(ctx, storage.NovaNota{UserID: dono, Titulo: "f", Corpo: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"e", "b", "a", "f", "d", "c"}) {
		t.Errorf("nota nova no topo do grupo dela: %v", got)
	}
}

func TestOrdem_EditarNaoMove(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	_, _, c, _ := quatro(t, r, rel)
	antes, _ := r.Get(ctx, dono, c.ID)

	rel.Andar(time.Hour)
	for i, m := range []storage.MudancaDaNota{
		{Titulo: ptr("novo título")},
		{Corpo: ptr("outro corpo")},
		{MudarCor: true, Cor: "azul"},
	} {
		n, err := r.Atualizar(ctx, dono, c.ID, antes.Version+int64(i), m)
		if err != nil {
			t.Fatal(err)
		}
		if n.Posicao != antes.Posicao {
			t.Errorf("a edição %d mudou a posição: %q → %q", i, antes.Posicao, n.Posicao)
		}
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"d", "novo título", "b", "a"}) {
		t.Errorf("a nota editada saiu do lugar: %v", got)
	}

	// Marcar e desmarcar um marcador também não movem.
	m := criarMarcador(t, r, "casa")
	if _, err := r.Marcar(ctx, dono, c.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"d", "novo título", "b", "a"}) {
		t.Errorf("marcar moveu: %v", got)
	}
}

func TestOrdem_FixarDesafixarArquivarERestaurarVaoParaOTopo(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a, b, c, d := quatro(t, r, rel) // lista: d c b a

	// Desafixar vai para o topo das comuns, não para onde estava.
	rel.Andar(time.Minute)
	af, err := r.Atualizar(ctx, dono, a.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Atualizar(ctx, dono, a.ID, af.Version, storage.MudancaDaNota{Fixada: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"a", "d", "c", "b"}) {
		t.Errorf("desafixar → topo das comuns: %v", got)
	}

	// Arquivar e desarquivar: no Arquivo ela fica no topo; volta ao topo das ativas.
	ca, err := r.Atualizar(ctx, dono, c.ID, 1, storage.MudancaDaNota{Arquivada: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Atualizar(ctx, dono, b.ID, 1, storage.MudancaDaNota{Arquivada: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, storage.VistaArquivo); !mesmaLista(got, []string{"b", "c"}) {
		t.Errorf("o último arquivado abre o Arquivo: %v", got)
	}
	if _, err := r.Atualizar(ctx, dono, c.ID, ca.Version, storage.MudancaDaNota{Arquivada: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"c", "a", "d"}) {
		t.Errorf("desarquivar → topo das ativas: %v", got)
	}

	// Restaurar da lixeira: topo do grupo (aqui, o das comuns ativas).
	if err := r.SoftDelete(ctx, dono, d.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Criar(ctx, storage.NovaNota{UserID: dono, Titulo: "e", Corpo: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"e", "c", "a"}) {
		t.Fatalf("antes de restaurar: %v", got)
	}
	if _, err := r.Restaurar(ctx, dono, d.ID); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"d", "e", "c", "a"}) {
		t.Errorf("restaurar → topo: %v", got)
	}

	// Mudar a cor junto com fixar também leva ao topo; só a cor não.
	na, _ := r.Get(ctx, dono, a.ID)
	if _, err := r.Atualizar(ctx, dono, a.ID, na.Version, storage.MudancaDaNota{Fixada: ptr(true), MudarCor: true, Cor: "rosa"}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"a", "d", "e", "c"}) {
		t.Errorf("fixar → topo das fixadas: %v", got)
	}
}

func TestOrdem_FixarJaFixadaNaoMove(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a, b, _, _ := quatro(t, r, rel)
	fa, err := r.Atualizar(ctx, dono, a.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	fb, err := r.Atualizar(ctx, dono, b.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	// "fixada: true" numa já fixada (um PATCH repetido) fica onde está.
	if _, err := r.Atualizar(ctx, dono, a.ID, fa.Version, storage.MudancaDaNota{Fixada: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got[:2], []string{"b", "a"}) {
		t.Errorf("PATCH sem mudança de grupo moveu: %v (b=%d)", got, fb.Version)
	}
}

func TestOrdem_MoverEntreVizinhas(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a, b, c, d := quatro(t, r, rel) // d c b a

	// a para o topo (antes vazio, depois = d).
	n, err := r.Mover(ctx, dono, a.ID, "", d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"a", "d", "c", "b"}) {
		t.Errorf("para o topo: %v", got)
	}
	if n.Version != 2 {
		t.Errorf("a versão devia subir 1 → 2, é %d", n.Version)
	}
	if !n.UpdatedAt.Equal(a.UpdatedAt) {
		t.Errorf("arrastar não é editar: updated_at foi de %v para %v", a.UpdatedAt, n.UpdatedAt)
	}

	// d para o fim (antes = b, depois vazio).
	if _, err := r.Mover(ctx, dono, d.ID, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"a", "c", "b", "d"}) {
		t.Errorf("para o fim: %v", got)
	}

	// b para o meio (entre a e c).
	if _, err := r.Mover(ctx, dono, b.ID, a.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := listaDe(t, r, ""); !mesmaLista(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("para o meio: %v", got)
	}

	// Soltar onde já estava (entre a e c, b está) não grava nada.
	antes, _ := r.Get(ctx, dono, b.ID)
	depoisN, err := r.Mover(ctx, dono, b.ID, a.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if depoisN.Version != antes.Version || depoisN.Posicao != antes.Posicao {
		t.Errorf("mover para onde já está gravou: v%d→v%d, %q→%q", antes.Version, depoisN.Version, antes.Posicao, depoisN.Posicao)
	}
}

func TestOrdem_MoverMuitasVezesNoMesmoVao(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a, b, c, d := quatro(t, r, rel) // d c b a
	// c e b trocam de lugar 300 vezes entre as mesmas vizinhas d e a: as chaves
	// ficam distintas e a lista, íntegra.
	for i := 0; i < 300; i++ {
		if _, err := r.Mover(ctx, dono, c.ID, b.ID, a.ID); err != nil { // d b c a
			t.Fatal(err)
		}
		if _, err := r.Mover(ctx, dono, c.ID, d.ID, b.ID); err != nil { // d c b a
			t.Fatal(err)
		}
	}
	ns, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
	if got := titulos(ns); !mesmaLista(got, []string{"d", "c", "b", "a"}) {
		t.Errorf("depois de 600 movimentos: %v", got)
	}
	vistas := map[string]bool{}
	for _, n := range ns {
		if vistas[n.Posicao] {
			t.Errorf("posição repetida %q", n.Posicao)
		}
		vistas[n.Posicao] = true
	}
}

func TestOrdem_MoverRecusa(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a, b, c, d := quatro(t, r, rel) // d c b a

	fixa, err := r.Atualizar(ctx, dono, a.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	arq, err := r.Atualizar(ctx, dono, b.ID, 1, storage.MudancaDaNota{Arquivada: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SoftDelete(ctx, dono, c.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	e := criarNota(t, r, "e", "x")
	alheia, err := r.Criar(ctx, storage.NovaNota{UserID: "outra-pessoa", Titulo: "z", Corpo: "x"})
	if err != nil {
		t.Fatal(err)
	}

	casos := []struct {
		nome       string
		id, ab, dp string
		quer       error
	}{
		{"vizinha fixada, a nota é comum", d.ID, fixa.ID, "", storage.ErrVizinhoInvalido},
		{"vizinha comum, a nota é fixada", fixa.ID, "", d.ID, storage.ErrVizinhoInvalido},
		{"vizinha no Arquivo, a nota é ativa", d.ID, arq.ID, "", storage.ErrVizinhoInvalido},
		{"vizinha na lixeira", d.ID, c.ID, "", storage.ErrVizinhoInvalido},
		{"vizinha que não existe", d.ID, "nao-existe", "", storage.ErrVizinhoInvalido},
		{"vizinha de outra pessoa", d.ID, alheia.ID, "", storage.ErrVizinhoInvalido},
		{"a nota como vizinha de si mesma", d.ID, d.ID, "", storage.ErrVizinhoInvalido},
		{"depois = a própria nota", d.ID, "", d.ID, storage.ErrVizinhoInvalido},
		{"vizinhas fora de ordem", e.ID, d.ID, e.ID, storage.ErrVizinhoInvalido}, // e é a própria
		{"sem nenhuma vizinha", d.ID, "", "", storage.ErrMoverSemVizinho},
		{"a nota não existe", "nao-existe", "", d.ID, storage.ErrNotFound},
		{"a nota é de outra pessoa", alheia.ID, "", d.ID, storage.ErrNotFound},
		{"a nota está na lixeira", c.ID, "", d.ID, storage.ErrNotaNaLixeira},
	}
	for _, k := range casos {
		antes, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
		if _, err := r.Mover(ctx, dono, k.id, k.ab, k.dp); !errors.Is(err, k.quer) {
			t.Errorf("%s: err = %v, queria %v", k.nome, err, k.quer)
		}
		depois, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
		if !mesmaLista(titulos(antes), titulos(depois)) {
			t.Errorf("%s: a recusa mexeu na lista: %v → %v", k.nome, titulos(antes), titulos(depois))
		}
	}

	// Vizinhas fora de ordem (a lista da tela estava velha): a de cima vem depois
	// da de baixo.
	f := criarNota(t, r, "f", "x") // lista ativa: f e d
	if _, err := r.Mover(ctx, dono, e.ID, d.ID, f.ID); !errors.Is(err, storage.ErrVizinhoInvalido) {
		t.Errorf("vizinhas fora de ordem: %v", err)
	}
}

func TestOrdem_AMoveNaoMexeNaFilaDoSync(t *testing.T) {
	// Mover não escreve na sync_outbox (a N7 leva a posicao com a nota).
	r, rel := novoRepoDeNotas(t)
	a, _, _, d := quatro(t, r, rel)
	if _, err := r.Mover(context.Background(), dono, a.ID, "", d.ID); err != nil {
		t.Fatal(err)
	}
	// A prova direta está em TestNotas_NaoMexemNaFilaDoSync; aqui basta a nota ter
	// ficado "pending", sem versão da nuvem.
	n, _ := r.Get(context.Background(), dono, a.ID)
	if n.SyncState != "pending" || n.ServerVersion != nil {
		t.Errorf("estado do sync: %q %v", n.SyncState, n.ServerVersion)
	}
}
