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
func TestEscolher(t *testing.T) {
	notas := []storage.Nota{
		nota("compras", "Lista de compras", 0),
		nota("lista", "Lista", 0),
		nota("acao", "Ações da semana", 0),
		nota("semtitulo", "", 0),
		nota("pontuada", "Ideias:", 0),
		nota("cpp", "C++", 0),
	}
	casos := []struct {
		nome, ditado, quer, resto string
	}{
		{"dois-pontos", "Lista de compras: leite, pão e café", "compras", "leite, pão e café"},
		{"vírgula", "lista de compras, leite", "compras", "leite"},
		{"só espaço", "lista de compras leite", "compras", "leite"},
		{"ponto do ASR", "Lista de compras. Leite e ovos.", "compras", "Leite e ovos."},
		{"caixa", "LISTA DE COMPRAS: arroz", "compras", "arroz"},
		{"acento no ditado e não no título", "acoes da semana: revisar", "acao", "revisar"},
		{"acento no título e não no ditado", "Ações da semana revisar", "acao", "revisar"},
		{"o mais longo vence o prefixo curto", "lista de compras: sal", "compras", "sal"},
		{"o curto casa quando o longo não", "lista de tarefas: lavar", "lista", "de tarefas: lavar"},
		{"fim de palavra: Lista não casa com Listar", "Listar os pendentes", "", ""},
		{"só o título: não casa, vira nota nova", "Lista de compras.", "", ""},
		{"pontuação do título não precisa ser ditada", "ideias comprar um barco", "pontuada", "comprar um barco"},
		{"pontuação no meio do título é exata", "C++ ponteiros", "cpp", "ponteiros"},
		{"nenhum título no começo", "Comprar leite na lista de compras", "", ""},
		{"lista do Polish em linhas", "Lista de compras:\n- leite\n- pão", "compras", "- leite\n- pão"},
		{"vazio", "", "", ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			i, resto := Escolher(notas, c.ditado)
			got := ""
			if i >= 0 {
				got = notas[i].ID
			}
			if got != c.quer || resto != c.resto {
				t.Fatalf("Escolher(%q) = %q, %q; quer %q, %q", c.ditado, got, resto, c.quer, c.resto)
			}
		})
	}
}

// TestEscolher_EmpateVenceAEditadaPorUltimo: dois títulos iguais (sem caixa e
// sem acento): vence a nota editada por último, não a primeira da lista.
func TestEscolher_EmpateVenceAEditadaPorUltimo(t *testing.T) {
	notas := []storage.Nota{
		nota("antiga", "Mercado", 0),
		nota("nova", "mercado", time.Hour),
		nota("meio", "MERCADO", time.Minute),
	}
	i, _ := Escolher(notas, "mercado: tomate")
	if i < 0 || notas[i].ID != "nova" {
		t.Fatalf("empate: escolheu %d, quer a editada por último", i)
	}
	// Ordem inversa na lista: o mesmo resultado.
	notas[0], notas[1] = notas[1], notas[0]
	i, _ = Escolher(notas, "mercado: tomate")
	if i < 0 || notas[i].ID != "nova" {
		t.Fatalf("empate (ordem invertida): escolheu %d", i)
	}
}

func TestFormaDoCorpo(t *testing.T) {
	casos := []struct {
		corpo string
		quer  Forma
	}{
		{"", FormaParagrafo},
		{"um parágrafo\noutro", FormaParagrafo},
		{"- a\n- b", FormaMarcadores},
		{"1. a\n2. b", FormaNumerada},
		{"texto\n- [x] feito", FormaTarefas},
		{"- a\n- [ ] b", FormaTarefas},
		{"# Seção\n- a", FormaMarcadores},
	}
	for _, c := range casos {
		if got := FormaDoCorpo(linhasDe(c.corpo)); got != c.quer {
			t.Errorf("FormaDoCorpo(%q) = %v, quer %v", c.corpo, got, c.quer)
		}
	}
}

// TestAcrescentar é a tabela de como o texto entra.
func TestAcrescentar(t *testing.T) {
	casos := []struct {
		nome, corpo, resto, quer string
		n                        int
	}{
		{"lista de marcar com enumeração",
			"- [ ] Arroz\n- [x] Feijão", "leite, pão e café",
			"- [ ] Arroz\n- [ ] Leite\n- [ ] Pão\n- [ ] Café\n- [x] Feijão", 3},
		{"lista de marcar sem enumeração: um item só",
			"- [ ] Arroz", "duas dúzias de ovos.",
			"- [ ] Arroz\n- [ ] Duas dúzias de ovos", 1},
		{"todos marcados: os novos entram antes do bloco marcado",
			"Mercado do sábado\n- [x] Arroz\n- [x] Feijão", "sal",
			"Mercado do sábado\n- [ ] Sal\n- [x] Arroz\n- [x] Feijão", 1},
		{"itens abertos, um parágrafo e os marcados: depois do último aberto",
			"- [ ] A\n- [ ] B\nObservação\n- [x] C", "d",
			"- [ ] A\n- [ ] B\n- [ ] D\nObservação\n- [x] C", 1},
		{"lista do Polish em linhas numa lista de marcar",
			"- [ ] Arroz", "- leite\n- pão",
			"- [ ] Arroz\n- [ ] Leite\n- [ ] Pão", 2},
		{"número com vírgula não parte",
			"- [ ] Arroz", "1,5 kg de carne",
			"- [ ] Arroz\n- [ ] 1,5 kg de carne", 1},
		{"sigla mantém a caixa",
			"- [ ] X", "eSocial, DCTFWeb",
			"- [ ] X\n- [ ] eSocial\n- [ ] DCTFWeb", 2},
		{"marcadores",
			"Ideias\n- Barco\nfim", "carro, moto e avião",
			"Ideias\n- Barco\n- Carro\n- Moto\n- Avião\nfim", 3},
		{"sem vírgula não é enumeração para a regra: um item só",
			"- Barco", "carro e moto",
			"- Barco\n- Carro e moto", 1},
		{"numerada continua a contagem",
			"1. Um\n2. Dois", "três",
			"1. Um\n2. Dois\n3. Três", 1},
		{"parágrafo vai para o fim, como foi ditado",
			"Primeira ideia.", "comprar um barco, um carro e uma moto.",
			"Primeira ideia.\nComprar um barco, um carro e uma moto.", 1},
		{"parágrafo em nota vazia",
			"", "texto",
			"Texto", 1},
		{"parágrafo com a lista do Polish fica como veio",
			"Notas soltas", "Hoje:\n- a\n- b",
			"Notas soltas\nHoje:\n- a\n- b", 3},
		{"corpo com \\r\\n",
			"- [ ] A\r\n- [x] B", "c",
			"- [ ] A\n- [ ] C\n- [x] B", 1},
		{"resto vazio não muda nada",
			"- [ ] A", "  ",
			"- [ ] A", 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, n := Acrescentar(c.corpo, c.resto)
			if got != c.quer || n != c.n {
				t.Fatalf("Acrescentar(%q, %q) =\n%q, %d\nquer\n%q, %d", c.corpo, c.resto, got, n, c.quer, c.n)
			}
		})
	}
}

func TestTextoDaPilula(t *testing.T) {
	if got := TextoDaPilula("Lista de compras", 3); got != "Lista de compras +3" {
		t.Fatalf("got %q", got)
	}
	if got := TextoDaPilula("  Lista   de\tcompras ", 1); got != "Lista de compras +1" {
		t.Fatalf("espaços: got %q", got)
	}
	longo := strings.Repeat("palavra ", 12)
	got := TextoDaPilula(longo, 12)
	if !strings.HasSuffix(got, "… +12") || len([]rune(got)) > maxTituloNaPilula+4 {
		t.Fatalf("título longo: %q", got)
	}
	if (Resultado{Nova: true}).Pilula() != TextoNotaNova {
		t.Fatal("nota nova: texto errado")
	}
	if (Resultado{Titulo: "Mercado", Linhas: 2}).Pilula() != "Mercado +2" {
		t.Fatal("resultado: texto errado")
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
