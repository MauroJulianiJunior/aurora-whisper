package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// relogioDeNotas é um relógio que o teste anda à mão: cada gravação do
// repositório lê o instante dele.
type relogioDeNotas struct{ t time.Time }

func (r *relogioDeNotas) Now() time.Time { return r.t }
func (r *relogioDeNotas) Andar(d time.Duration) {
	r.t = r.t.Add(d)
}

var inicioDasNotas = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func novoRepoDeNotas(t *testing.T) (*storage.SQLiteNotaRepository, *relogioDeNotas) {
	t.Helper()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := storage.NewSQLiteNotaRepository(db)
	rel := &relogioDeNotas{t: inicioDasNotas}
	repo.UsarRelogio(rel.Now)
	return repo, rel
}

const dono = "dono-1"

func criarNota(t *testing.T, r *storage.SQLiteNotaRepository, titulo, corpo string) storage.Nota {
	t.Helper()
	n, err := r.Criar(context.Background(), storage.NovaNota{UserID: dono, Titulo: titulo, Corpo: corpo})
	if err != nil {
		t.Fatalf("Criar(%q): %v", titulo, err)
	}
	return n
}

func criarMarcador(t *testing.T, r *storage.SQLiteNotaRepository, nome string) storage.MarcadorDeNota {
	t.Helper()
	m, err := r.CriarMarcador(context.Background(), dono, nome)
	if err != nil {
		t.Fatalf("CriarMarcador(%q): %v", nome, err)
	}
	return m
}

func titulos(ns []storage.Nota) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Titulo
	}
	return out
}

func mesmaLista(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNotas_CriarELer(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	ctx := context.Background()
	m := criarMarcador(t, r, "Compras")

	n, err := r.Criar(ctx, storage.NovaNota{
		UserID: dono, Titulo: "Mercado", Corpo: "- [ ] leite\n- [x] pão", Cor: "verde",
		Fixada: true, DitadoDeOrigem: "ditado-9", Marcadores: []string{m.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.ID) != 36 || n.ID[14] != '7' {
		t.Errorf("o id deveria ser um UUID v7: %q", n.ID)
	}
	if n.Version != 1 || n.SyncState != "pending" || n.ServerVersion != nil {
		t.Errorf("version=%d sync_state=%q server_version=%v", n.Version, n.SyncState, n.ServerVersion)
	}
	if n.Titulo != "Mercado" || n.Corpo != "- [ ] leite\n- [x] pão" || n.Cor == nil || *n.Cor != "verde" ||
		!n.Fixada || n.Arquivada || n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != "ditado-9" {
		t.Errorf("campos da nota: %+v", n)
	}
	if !mesmaLista(n.Marcadores, []string{m.ID}) {
		t.Errorf("marcadores = %v", n.Marcadores)
	}
	if !n.CreatedAt.Equal(inicioDasNotas) || !n.UpdatedAt.Equal(inicioDasNotas) || n.DeletedAt != nil || n.PurgeAt != nil {
		t.Errorf("datas: %+v", n)
	}

	lida, err := r.Get(ctx, dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lida.Corpo != n.Corpo || lida.Version != 1 {
		t.Errorf("Get diferente do Criar: %+v", lida)
	}

	// Sem título (a ADR deixa vazio), sem cor, sem marcador: Marcadores nunca é nil.
	sem := criarNota(t, r, "", "uma ideia")
	if sem.Cor != nil || sem.Marcadores == nil || len(sem.Marcadores) != 0 || sem.DitadoDeOrigem != nil {
		t.Errorf("nota simples: %+v", sem)
	}
}

func TestNotas_CriarRecusaOQueNaoVale(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	ctx := context.Background()
	casos := []struct {
		nome string
		nova storage.NovaNota
		want error
	}{
		{"vazia", storage.NovaNota{UserID: dono, Titulo: "  ", Corpo: "\n "}, storage.ErrNotaVazia},
		{"cor fora da lista", storage.NovaNota{UserID: dono, Corpo: "x", Cor: "turquesa"}, storage.ErrCorInvalida},
		{"cor com caixa errada", storage.NovaNota{UserID: dono, Corpo: "x", Cor: "Verde"}, storage.ErrCorInvalida},
		{"corpo grande", storage.NovaNota{UserID: dono, Corpo: strings.Repeat("a", storage.MaxCorpoDaNotaBytes+1)}, storage.ErrCorpoGrande},
		{"título grande", storage.NovaNota{UserID: dono, Titulo: strings.Repeat("a", storage.MaxTituloDaNotaBytes+1), Corpo: "x"}, storage.ErrTituloGrande},
		{"nulo no corpo", storage.NovaNota{UserID: dono, Corpo: "a\x00b"}, storage.ErrTextoInvalido},
		{"marcador que não existe", storage.NovaNota{UserID: dono, Corpo: "x", Marcadores: []string{"nao-existe"}}, storage.ErrNotFound},
	}
	for _, c := range casos {
		if _, err := r.Criar(ctx, c.nova); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, quer %v", c.nome, err, c.want)
		}
	}
	// O teto vale no limite exato.
	if _, err := r.Criar(ctx, storage.NovaNota{UserID: dono, Corpo: strings.Repeat("a", storage.MaxCorpoDaNotaBytes)}); err != nil {
		t.Errorf("corpo no teto: %v", err)
	}
	// Nada ficou das recusas.
	todas, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
	if len(todas) != 1 {
		t.Errorf("só a nota do teto deveria existir; há %d", len(todas))
	}
}

func TestNotas_TodasAsCoresPassam(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	if len(storage.CoresDaNota) != 8 {
		t.Fatalf("são 8 cores: %v", storage.CoresDaNota)
	}
	for _, cor := range storage.CoresDaNota {
		n, err := r.Criar(context.Background(), storage.NovaNota{UserID: dono, Corpo: "x", Cor: cor})
		if err != nil || n.Cor == nil || *n.Cor != cor {
			t.Errorf("cor %q: %v %+v", cor, err, n.Cor)
		}
	}
}

func TestNotas_OutroUsuarioNaoVe(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	ctx := context.Background()
	n := criarNota(t, r, "minha", "x")
	m := criarMarcador(t, r, "meu")

	if _, err := r.Get(ctx, "outro", n.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get de outro: %v", err)
	}
	if _, err := r.Atualizar(ctx, "outro", n.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Atualizar de outro: %v", err)
	}
	if err := r.SoftDelete(ctx, "outro", n.ID, inicioDasNotas); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("SoftDelete de outro: %v", err)
	}
	if _, err := r.Marcar(ctx, "outro", n.ID, m.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Marcar de outro: %v", err)
	}
	if err := r.ApagarMarcador(ctx, "outro", m.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("ApagarMarcador de outro: %v", err)
	}
	if ls, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: "outro"}); len(ls) != 0 {
		t.Errorf("a lista de outro não é vazia: %v", ls)
	}
	if ms, _ := r.ListarMarcadores(ctx, "outro"); len(ms) != 0 {
		t.Errorf("os marcadores de outro não são vazios: %v", ms)
	}
	// O mesmo nome de marcador em outro usuário é outro marcador.
	if _, err := r.CriarMarcador(ctx, "outro", "meu"); err != nil {
		t.Errorf("o nome é único por usuário, e não global: %v", err)
	}
	// E o marcador de um usuário não vai numa nota de outro.
	if _, err := r.Criar(ctx, storage.NovaNota{UserID: "outro", Corpo: "x", Marcadores: []string{m.ID}}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("marcador alheio: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestNotas_Atualizar(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	n := criarNota(t, r, "a", "b")

	rel.Andar(time.Minute)
	n2, err := r.Atualizar(ctx, dono, n.ID, 1, storage.MudancaDaNota{Titulo: ptr("novo"), Corpo: ptr("- um\n- dois")})
	if err != nil {
		t.Fatal(err)
	}
	if n2.Version != 2 || n2.Titulo != "novo" || n2.Corpo != "- um\n- dois" || !n2.UpdatedAt.Equal(inicioDasNotas.Add(time.Minute)) {
		t.Errorf("depois da edição: %+v", n2)
	}
	if !n2.CreatedAt.Equal(inicioDasNotas) {
		t.Errorf("created_at não pode mudar: %v", n2.CreatedAt)
	}

	// A versão velha é 409.
	if _, err := r.Atualizar(ctx, dono, n.ID, 1, storage.MudancaDaNota{Titulo: ptr("tarde")}); !errors.Is(err, storage.ErrVersionConflict) {
		t.Errorf("versão velha: %v", err)
	}
	if lida, _ := r.Get(ctx, dono, n.ID); lida.Titulo != "novo" || lida.Version != 2 {
		t.Errorf("a edição recusada mexeu na nota: %+v", lida)
	}

	// Fixar, arquivar e cor; só o que veio muda.
	n3, err := r.Atualizar(ctx, dono, n.ID, 2, storage.MudancaDaNota{Fixada: ptr(true), MudarCor: true, Cor: "azul"})
	if err != nil {
		t.Fatal(err)
	}
	if !n3.Fixada || n3.Arquivada || n3.Cor == nil || *n3.Cor != "azul" || n3.Titulo != "novo" || n3.Version != 3 {
		t.Errorf("fixar+cor: %+v", n3)
	}
	n4, err := r.Atualizar(ctx, dono, n.ID, 3, storage.MudancaDaNota{Fixada: ptr(false), Arquivada: ptr(true), MudarCor: true, Cor: ""})
	if err != nil {
		t.Fatal(err)
	}
	if n4.Fixada || !n4.Arquivada || n4.Cor != nil || n4.Version != 4 {
		t.Errorf("desafixar+arquivar+sem cor: %+v", n4)
	}
	n5, err := r.Atualizar(ctx, dono, n.ID, 4, storage.MudancaDaNota{Arquivada: ptr(false)})
	if err != nil || n5.Arquivada {
		t.Errorf("desarquivar: %v %+v", err, n5)
	}

	// Recusas: nada muda e a versão não sobe.
	for nome, m := range map[string]storage.MudancaDaNota{
		"cor inválida":  {MudarCor: true, Cor: "marrom"},
		"corpo grande":  {Corpo: ptr(strings.Repeat("a", storage.MaxCorpoDaNotaBytes+1))},
		"título grande": {Titulo: ptr(strings.Repeat("a", storage.MaxTituloDaNotaBytes+1))},
		"nulo":          {Titulo: ptr("a\x00")},
	} {
		if _, err := r.Atualizar(ctx, dono, n.ID, 5, m); err == nil || errors.Is(err, storage.ErrVersionConflict) {
			t.Errorf("%s: err = %v", nome, err)
		}
	}
	if lida, _ := r.Get(ctx, dono, n.ID); lida.Version != 5 {
		t.Errorf("as recusas subiram a versão: %d", lida.Version)
	}

	// Editar tudo para vazio vale (a tela decide o que fazer com a nota vazia).
	if v, err := r.Atualizar(ctx, dono, n.ID, 5, storage.MudancaDaNota{Titulo: ptr(""), Corpo: ptr("")}); err != nil || v.Corpo != "" {
		t.Errorf("esvaziar: %v", err)
	}

	if _, err := r.Atualizar(ctx, dono, "nao-existe", 1, storage.MudancaDaNota{Titulo: ptr("x")}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("nota inexistente: %v", err)
	}
}

func TestNotas_ListaOrdemEVistas(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	mk := func(titulo string) storage.Nota {
		rel.Andar(time.Minute)
		return criarNota(t, r, titulo, "corpo "+titulo)
	}
	a, b, c, d := mk("a"), mk("b"), mk("c"), mk("d")

	lista := func(v storage.VistaDasNotas) []string {
		ns, err := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, Vista: v})
		if err != nil {
			t.Fatal(err)
		}
		return titulos(ns)
	}

	// A nota nova entra no topo; a vista vazia é a das ativas.
	if got := lista(""); !mesmaLista(got, []string{"d", "c", "b", "a"}) {
		t.Errorf("ordem inicial: %v", got)
	}
	// Fixar a mais velha a põe no topo, mesmo sem ser a mais recente.
	rel.Andar(time.Minute)
	if _, err := r.Atualizar(ctx, dono, a.ID, 1, storage.MudancaDaNota{Fixada: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if got := lista(storage.VistaAtivas); !mesmaLista(got, []string{"a", "d", "c", "b"}) {
		t.Errorf("fixada primeiro: %v", got)
	}
	// Editar b NÃO a move (ADR-022, N2b): ela fica onde a pessoa a pôs.
	rel.Andar(time.Minute)
	if _, err := r.Atualizar(ctx, dono, b.ID, 1, storage.MudancaDaNota{Corpo: ptr("mudou")}); err != nil {
		t.Fatal(err)
	}
	if got := lista(storage.VistaAtivas); !mesmaLista(got, []string{"a", "d", "c", "b"}) {
		t.Errorf("editar moveu a nota: %v", got)
	}

	// Arquivo: sai das ativas, entra no Arquivo.
	if _, err := r.Atualizar(ctx, dono, c.ID, 1, storage.MudancaDaNota{Arquivada: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if got := lista(storage.VistaAtivas); !mesmaLista(got, []string{"a", "d", "b"}) {
		t.Errorf("ativas sem a arquivada: %v", got)
	}
	if got := lista(storage.VistaArquivo); !mesmaLista(got, []string{"c"}) {
		t.Errorf("arquivo: %v", got)
	}

	// Lixeira: o apagado por último vem primeiro, arquivada ou não.
	if err := r.SoftDelete(ctx, dono, d.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	rel.Andar(time.Hour)
	if err := r.SoftDelete(ctx, dono, c.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	if got := lista(storage.VistaLixeira); !mesmaLista(got, []string{"c", "d"}) {
		t.Errorf("lixeira: %v", got)
	}
	if got := lista(storage.VistaAtivas); !mesmaLista(got, []string{"a", "b"}) {
		t.Errorf("ativas sem as da lixeira: %v", got)
	}
	if got := lista(storage.VistaArquivo); len(got) != 0 {
		t.Errorf("a arquivada que foi para a lixeira ainda está no Arquivo: %v", got)
	}

	if _, err := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, Vista: "outra"}); err == nil {
		t.Error("vista inválida deveria falhar")
	}
	if _, err := r.Listar(ctx, storage.FiltroDeNotas{}); err == nil {
		t.Error("sem UserID deveria falhar")
	}
}

func TestNotas_BuscaSemCaixaENemAcento(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	ctx := context.Background()
	criarNota(t, r, "Reunião", "pauta do ANO")
	criarNota(t, r, "", "- [ ] comprar maçã\n- [ ] pão")
	criarNota(t, r, "Outra", "nada")

	busca := func(q string) []string {
		ns, err := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, Busca: q})
		if err != nil {
			t.Fatal(err)
		}
		return titulos(ns)
	}
	if got := busca("reuniao"); !mesmaLista(got, []string{"Reunião"}) {
		t.Errorf("sem acento no título: %v", got)
	}
	if got := busca("REUNIÃO"); !mesmaLista(got, []string{"Reunião"}) {
		t.Errorf("com caixa e acento: %v", got)
	}
	if got := busca("ano"); !mesmaLista(got, []string{"Reunião"}) {
		t.Errorf("no corpo, sem caixa: %v", got)
	}
	if got := busca("MACA"); len(got) != 1 || got[0] != "" {
		t.Errorf("maçã por maca: %v", got)
	}
	if got := busca("  pão "); len(got) != 1 {
		t.Errorf("a busca ignora espaço nas bordas: %v", got)
	}
	if got := busca("inexistente"); len(got) != 0 {
		t.Errorf("nada deveria casar: %v", got)
	}
	if got := busca(""); len(got) != 3 {
		t.Errorf("busca vazia não filtra: %v", got)
	}
	// A busca respeita a vista: na lixeira, só acha o que está lá.
	ns, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, Busca: "reuniao", Vista: storage.VistaLixeira})
	if len(ns) != 0 {
		t.Errorf("a busca da lixeira achou nota ativa: %v", ns)
	}
}

func TestNotas_LixeiraRestaurarEApagarDeVez(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	n := criarNota(t, r, "x", "y")

	rel.Andar(time.Hour)
	agora := rel.Now()
	if err := r.SoftDelete(ctx, dono, n.ID, agora); err != nil {
		t.Fatal(err)
	}
	na, _ := r.Get(ctx, dono, n.ID)
	if na.DeletedAt == nil || !na.DeletedAt.Equal(agora) {
		t.Fatalf("deleted_at = %v", na.DeletedAt)
	}
	if na.PurgeAt == nil || !na.PurgeAt.Equal(agora.AddDate(0, 0, 30)) {
		t.Errorf("purge_at deveria ser +30 dias: %v", na.PurgeAt)
	}
	if na.Version != 2 {
		t.Errorf("version = %d", na.Version)
	}

	// Reapagar não reinicia o prazo nem sobe a versão.
	if err := r.SoftDelete(ctx, dono, n.ID, agora.Add(10*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if again, _ := r.Get(ctx, dono, n.ID); !again.PurgeAt.Equal(*na.PurgeAt) || again.Version != 2 {
		t.Errorf("reapagar mexeu na nota: %+v", again)
	}

	// Na lixeira não se edita, marca nem se apaga de vez fora dela.
	if _, err := r.Atualizar(ctx, dono, n.ID, 2, storage.MudancaDaNota{Titulo: ptr("z")}); !errors.Is(err, storage.ErrNotaNaLixeira) {
		t.Errorf("editar na lixeira: %v", err)
	}
	m := criarMarcador(t, r, "m")
	if _, err := r.Marcar(ctx, dono, n.ID, m.ID); !errors.Is(err, storage.ErrNotaNaLixeira) {
		t.Errorf("marcar na lixeira: %v", err)
	}

	back, err := r.Restaurar(ctx, dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.DeletedAt != nil || back.PurgeAt != nil || back.Version != 3 {
		t.Errorf("restaurada: %+v", back)
	}
	// Restaurar de novo não muda nada.
	if again, err := r.Restaurar(ctx, dono, n.ID); err != nil || again.Version != 3 {
		t.Errorf("restaurar a que não está na lixeira: %v %+v", err, again)
	}

	// Apagar de vez só da lixeira.
	if err := r.ApagarDeVez(ctx, dono, n.ID); !errors.Is(err, storage.ErrNotaForaDaLixeira) {
		t.Errorf("apagar de vez fora da lixeira: %v", err)
	}
	if _, err := r.Marcar(ctx, dono, n.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.SoftDelete(ctx, dono, n.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.ApagarDeVez(ctx, dono, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, dono, n.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("depois de apagar de vez: %v", err)
	}
	if err := r.ApagarDeVez(ctx, dono, n.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("apagar de vez a que não existe: %v", err)
	}
	if err := r.SoftDelete(ctx, dono, "nao-existe", rel.Now()); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("SoftDelete de inexistente: %v", err)
	}
	if _, err := r.Restaurar(ctx, dono, "nao-existe"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Restaurar de inexistente: %v", err)
	}
}

func TestNotas_ExpurgoApagaSoOQueVenceuNaLixeira(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	m := criarMarcador(t, r, "m")

	velha := criarNota(t, r, "velha", "x")
	if _, err := r.Marcar(ctx, dono, velha.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	recente := criarNota(t, r, "recente", "x")
	arquivada := criarNota(t, r, "arquivada", "x")
	if _, err := r.Atualizar(ctx, dono, arquivada.ID, 1, storage.MudancaDaNota{Arquivada: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	ativa := criarNota(t, r, "ativa", "x")

	if err := r.SoftDelete(ctx, dono, velha.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	rel.Andar(20 * 24 * time.Hour)
	if err := r.SoftDelete(ctx, dono, recente.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}

	// Um dia antes do prazo da velha: nada sai.
	venceVelha := inicioDasNotas.AddDate(0, 0, 30)
	if n, err := r.PurgeExpired(ctx, venceVelha.Add(-time.Nanosecond)); err != nil || n != 0 {
		t.Fatalf("antes do prazo: n=%d err=%v", n, err)
	}
	// No prazo: só a velha.
	if n, err := r.PurgeExpired(ctx, venceVelha); err != nil || n != 1 {
		t.Fatalf("no prazo: n=%d err=%v", n, err)
	}
	if _, err := r.Get(ctx, dono, velha.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a velha deveria ter saído: %v", err)
	}
	for _, id := range []string{recente.ID, arquivada.ID, ativa.ID} {
		if _, err := r.Get(ctx, dono, id); err != nil {
			t.Errorf("nota %s não podia sair: %v", id, err)
		}
	}
	// O marcador continua; só a ligação com a nota expurgada saiu.
	if ms, _ := r.ListarMarcadores(ctx, dono); len(ms) != 1 {
		t.Errorf("o expurgo da nota levou o marcador: %v", ms)
	}

	// A nota NÃO tem a retenção de 6 meses: um ano depois, ativa e arquivada
	// continuam, e só a da lixeira vai embora no prazo dela.
	umAno := inicioDasNotas.AddDate(1, 0, 0)
	if n, err := r.PurgeExpired(ctx, umAno); err != nil || n != 1 {
		t.Fatalf("um ano depois: n=%d err=%v", n, err)
	}
	if ls, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono}); len(ls) != 1 || ls[0].Titulo != "ativa" {
		t.Errorf("a ativa deveria seguir: %v", titulos(ls))
	}
	if ls, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, Vista: storage.VistaArquivo}); len(ls) != 1 {
		t.Errorf("a arquivada deveria seguir: %v", titulos(ls))
	}
}

func TestMarcadores_NomeUnicoSemCaixaNemAcento(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	ctx := context.Background()
	m := criarMarcador(t, r, "  Reunião   de  equipe ")
	if m.Nome != "Reunião de equipe" {
		t.Errorf("o nome deveria vir limpo: %q", m.Nome)
	}
	if m.Version != 1 || m.SyncState != "pending" || len(m.ID) != 36 {
		t.Errorf("marcador novo: %+v", m)
	}
	for _, repetido := range []string{"reuniao de equipe", "REUNIÃO DE EQUIPE", "Reuniao  de equipe", "reunião de equipe"} {
		if _, err := r.CriarMarcador(ctx, dono, repetido); !errors.Is(err, storage.ErrMarcadorDuplicado) {
			t.Errorf("%q deveria ser duplicado: %v", repetido, err)
		}
	}
	// Pontuação diferencia: "C#" e "C++" são marcadores distintos.
	criarMarcador(t, r, "C#")
	criarMarcador(t, r, "C++")

	for _, ruim := range []string{"", "   ", "a\nb", "a\tb", "a\x00b", strings.Repeat("x", storage.MaxNomeDoMarcador+1)} {
		if _, err := r.CriarMarcador(ctx, dono, ruim); err == nil || errors.Is(err, storage.ErrMarcadorDuplicado) {
			t.Errorf("%q deveria ser recusado: %v", ruim, err)
		}
	}
	// 50 caracteres (e não bytes): 50 letras acentuadas cabem.
	if _, err := r.CriarMarcador(ctx, dono, strings.Repeat("é", storage.MaxNomeDoMarcador)); err != nil {
		t.Errorf("50 caracteres deveriam caber: %v", err)
	}
}

func TestMarcadores_ListaPorNomeSemCaixaNemAcento(t *testing.T) {
	r, _ := novoRepoDeNotas(t)
	for _, n := range []string{"zebra", "Árvore", "banana", "Abacate"} {
		criarMarcador(t, r, n)
	}
	ms, err := r.ListarMarcadores(context.Background(), dono)
	if err != nil {
		t.Fatal(err)
	}
	var nomes []string
	for _, m := range ms {
		nomes = append(nomes, m.Nome)
	}
	if !mesmaLista(nomes, []string{"Abacate", "Árvore", "banana", "zebra"}) {
		t.Errorf("ordem: %v", nomes)
	}
}

func TestMarcadores_Renomear(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	a := criarMarcador(t, r, "compras")
	b := criarMarcador(t, r, "trabalho")

	rel.Andar(time.Minute)
	// Só mudar a caixa é permitido (o nome é dele mesmo).
	a2, err := r.RenomearMarcador(ctx, dono, a.ID, 1, "Compras")
	if err != nil {
		t.Fatal(err)
	}
	if a2.Nome != "Compras" || a2.Version != 2 || !a2.UpdatedAt.Equal(inicioDasNotas.Add(time.Minute)) {
		t.Errorf("renomeado: %+v", a2)
	}
	// Versão velha: 409.
	if _, err := r.RenomearMarcador(ctx, dono, a.ID, 1, "Outro"); !errors.Is(err, storage.ErrVersionConflict) {
		t.Errorf("versão velha: %v", err)
	}
	// Nome de outro marcador, de qualquer jeito de escrever.
	if _, err := r.RenomearMarcador(ctx, dono, a.ID, 2, "TRABALHO"); !errors.Is(err, storage.ErrMarcadorDuplicado) {
		t.Errorf("nome do vizinho: %v", err)
	}
	if _, err := r.RenomearMarcador(ctx, dono, a.ID, 2, " "); !errors.Is(err, storage.ErrNomeDeMarcadorInvalido) {
		t.Errorf("nome vazio: %v", err)
	}
	if _, err := r.RenomearMarcador(ctx, dono, "nao-existe", 1, "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("marcador inexistente: %v", err)
	}
	if lido, _ := r.ListarMarcadores(ctx, dono); len(lido) != 2 || lido[0].Nome != "Compras" || lido[1].ID != b.ID {
		t.Errorf("as recusas mexeram nos marcadores: %v", lido)
	}
}

func TestNotas_MarcarEDesmarcar(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	n := criarNota(t, r, "n", "x")
	outra := criarNota(t, r, "outra", "x")
	zeta := criarMarcador(t, r, "zeta")
	alfa := criarMarcador(t, r, "alfa")

	rel.Andar(time.Minute)
	n1, err := r.Marcar(ctx, dono, n.ID, zeta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n1.Version != 2 || !n1.UpdatedAt.Equal(inicioDasNotas.Add(time.Minute)) {
		t.Errorf("marcar deveria subir a versão e a hora: %+v", n1)
	}
	// Repetir não muda nada.
	if again, err := r.Marcar(ctx, dono, n.ID, zeta.ID); err != nil || again.Version != 2 {
		t.Errorf("marcar de novo: %v %+v", err, again)
	}
	n2, err := r.Marcar(ctx, dono, n.ID, alfa.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Os marcadores vêm na ordem do nome, e não na de entrada.
	if !mesmaLista(n2.Marcadores, []string{alfa.ID, zeta.ID}) {
		t.Errorf("ordem dos marcadores: %v", n2.Marcadores)
	}

	// O filtro por marcador.
	ls, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, MarcadorID: alfa.ID})
	if !mesmaLista(titulos(ls), []string{"n"}) {
		t.Errorf("filtro por marcador: %v", titulos(ls))
	}
	if len(ls) == 1 && !mesmaLista(ls[0].Marcadores, []string{alfa.ID, zeta.ID}) {
		t.Errorf("a lista traz os marcadores da nota: %v", ls[0].Marcadores)
	}
	if ls, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, MarcadorID: "nao-existe"}); len(ls) != 0 {
		t.Errorf("marcador que não existe não acha nota: %v", titulos(ls))
	}
	// Na lista sem filtro, a outra nota vem com marcadores vazios (não nil).
	todas, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
	for _, x := range todas {
		if x.ID == outra.ID && (x.Marcadores == nil || len(x.Marcadores) != 0) {
			t.Errorf("marcadores da outra nota: %#v", x.Marcadores)
		}
	}

	n3, err := r.Desmarcar(ctx, dono, n.ID, zeta.ID)
	if err != nil || !mesmaLista(n3.Marcadores, []string{alfa.ID}) || n3.Version != 4 {
		t.Errorf("desmarcar: %v %+v", err, n3)
	}
	if again, err := r.Desmarcar(ctx, dono, n.ID, zeta.ID); err != nil || again.Version != 4 {
		t.Errorf("desmarcar de novo: %v %+v", err, again)
	}

	if _, err := r.Marcar(ctx, dono, n.ID, "nao-existe"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("marcador inexistente: %v", err)
	}
	if _, err := r.Marcar(ctx, dono, "nao-existe", alfa.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("nota inexistente: %v", err)
	}
}

func TestMarcadores_ApagarTiraDasNotasENaoApagaNota(t *testing.T) {
	r, rel := novoRepoDeNotas(t)
	ctx := context.Background()
	m := criarMarcador(t, r, "Compras")
	fica := criarMarcador(t, r, "Fica")
	n := criarNota(t, r, "n", "x")
	lixo := criarNota(t, r, "lixo", "x")
	for _, id := range []string{n.ID, lixo.ID} {
		if _, err := r.Marcar(ctx, dono, id, m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Marcar(ctx, dono, n.ID, fica.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.SoftDelete(ctx, dono, lixo.ID, rel.Now()); err != nil {
		t.Fatal(err)
	}
	antes, _ := r.Get(ctx, dono, n.ID)

	rel.Andar(time.Hour)
	if err := r.ApagarMarcador(ctx, dono, m.ID); err != nil {
		t.Fatal(err)
	}
	depois, err := r.Get(ctx, dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !mesmaLista(depois.Marcadores, []string{fica.ID}) {
		t.Errorf("a nota deveria ficar só com o outro marcador: %v", depois.Marcadores)
	}
	if depois.Version != antes.Version+1 {
		t.Errorf("a versão da nota deveria subir: %d -> %d", antes.Version, depois.Version)
	}
	if !depois.UpdatedAt.Equal(antes.UpdatedAt) {
		t.Errorf("sumir um marcador não pode mexer na hora de edição da nota")
	}
	// A nota da lixeira também perdeu o marcador, e continua lá.
	if l, err := r.Get(ctx, dono, lixo.ID); err != nil || len(l.Marcadores) != 0 || l.DeletedAt == nil {
		t.Errorf("a nota da lixeira: %v %+v", err, l)
	}
	if ms, _ := r.ListarMarcadores(ctx, dono); len(ms) != 1 || ms[0].ID != fica.ID {
		t.Errorf("lista de marcadores: %v", ms)
	}
	// Apagado não marca nem renomeia nem filtra.
	if _, err := r.Marcar(ctx, dono, n.ID, m.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("marcar com o apagado: %v", err)
	}
	if _, err := r.RenomearMarcador(ctx, dono, m.ID, 1, "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("renomear o apagado: %v", err)
	}
	// Apagar de novo não faz nada; inexistente é 404.
	if err := r.ApagarMarcador(ctx, dono, m.ID); err != nil {
		t.Errorf("apagar de novo: %v", err)
	}
	if err := r.ApagarMarcador(ctx, dono, "nao-existe"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("apagar inexistente: %v", err)
	}
	// O nome fica livre.
	novo, err := r.CriarMarcador(ctx, dono, "compras")
	if err != nil || novo.ID == m.ID {
		t.Errorf("recriar o nome: %v %+v", err, novo)
	}
	if ls, _ := r.Listar(ctx, storage.FiltroDeNotas{UserID: dono, MarcadorID: m.ID}); len(ls) != 0 {
		t.Errorf("o filtro pelo marcador apagado achou nota: %v", titulos(ls))
	}
}

// TestNotas_NaoMexemNaFilaDoSync prova a regra da N1: o motor de sync ainda
// não conhece nota nem marcador, e nenhuma operação deste repositório põe
// marcador em sync_outbox.
func TestNotas_NaoMexemNaFilaDoSync(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	r := storage.NewSQLiteNotaRepository(db)

	m, _ := r.CriarMarcador(ctx, dono, "m")
	m, _ = r.RenomearMarcador(ctx, dono, m.ID, 1, "m2")
	n, err := r.Criar(ctx, storage.NovaNota{UserID: dono, Corpo: "x", Marcadores: []string{m.ID}})
	if err != nil {
		t.Fatal(err)
	}
	n, _ = r.Atualizar(ctx, dono, n.ID, 1, storage.MudancaDaNota{Corpo: ptr("y"), Fixada: ptr(true)})
	_, _ = r.Desmarcar(ctx, dono, n.ID, m.ID)
	_, _ = r.Marcar(ctx, dono, n.ID, m.ID)
	_ = r.SoftDelete(ctx, dono, n.ID, time.Now())
	_, _ = r.Restaurar(ctx, dono, n.ID)
	_ = r.ApagarMarcador(ctx, dono, m.ID)
	_ = r.SoftDelete(ctx, dono, n.ID, time.Now())
	_, _ = r.PurgeExpired(ctx, time.Now().AddDate(0, 0, 31))

	var fila int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sync_outbox`).Scan(&fila); err != nil {
		t.Fatal(err)
	}
	if fila != 0 {
		t.Errorf("sync_outbox tem %d marcador(es); as notas não podem escrever nela na N1", fila)
	}
	var fora int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM local_labels WHERE sync_state <> 'pending'`).Scan(&fora); err != nil || fora != 0 {
		t.Errorf("sync_state dos marcadores: %d (%v)", fora, err)
	}
}

func TestSchema_AsTabelasDasNotas(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, tabela := range []string{"local_notes", "local_labels", "local_note_labels"} {
		assertSQLiteMasterHas(t, db, "table", tabela)
	}
	for _, indice := range []string{"idx_local_notes_lista", "idx_local_notes_lixeira", "idx_local_notes_purge", "idx_local_notes_ordem",
		"idx_local_labels_nome", "idx_local_note_labels_label"} {
		assertSQLiteMasterHas(t, db, "index", indice)
	}

	// O banco recusa o que o Go já recusa: nome repetido (com a chave igual)
	// entre os não apagados, e fixada fora de 0/1.
	if _, err := db.ExecContext(ctx, `INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at)
		VALUES ('a','u','X','x','t','t')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at)
		VALUES ('b','u','x','x','t','t')`); err == nil {
		t.Error("o índice único do nome deveria recusar")
	}
	if _, err := db.ExecContext(ctx, `UPDATE local_labels SET deleted_at = 't' WHERE id = 'a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at)
		VALUES ('b','u','x','x','t','t')`); err != nil {
		t.Errorf("o nome do apagado deveria ficar livre: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_notes (id, user_id, fixada, created_at, updated_at)
		VALUES ('n','u',2,'t','t')`); err == nil {
		t.Error("fixada = 2 deveria ser recusada")
	}
}

// TestSchema_VarreduraVeAsColunasDa0009 prova que a varredura de nomes
// proibidos (TestSchema_NoAudioOrContextColumns) lê de fato as colunas das
// três tabelas das notas: cada coluna do SQLite migrado tem de aparecer como
// linha de coluna em 0009_notas.sql.
func TestSchema_VarreduraVeAsColunasDa0009(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// As colunas das tabelas das notas vêm da 0009 e da 0012 (a posicao).
	vistas := map[string]bool{}
	for _, arquivo := range []string{"0009_notas.sql", "0012_notas_posicao.sql"} {
		conteudo, err := os.ReadFile(filepath.Join("migrations", arquivo))
		if err != nil {
			t.Fatal(err)
		}
		for _, linha := range strings.Split(string(conteudo), "\n") {
			if m := linhaDeColuna.FindStringSubmatch(linha); m != nil {
				vistas[strings.ToLower(m[1])] = true
			}
		}
	}
	total := 0
	for _, tabela := range []string{"local_notes", "local_labels", "local_note_labels"} {
		cols, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, tabela)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var c string
			if err := cols.Scan(&c); err != nil {
				t.Fatal(err)
			}
			total++
			if !vistas[c] {
				t.Errorf("%s.%s não é vista pela varredura de nomes proibidos", tabela, c)
			}
		}
		cols.Close()
	}
	if total != 17+10+2 {
		t.Errorf("as tabelas das notas têm %d colunas, esperava 29", total)
	}
}
