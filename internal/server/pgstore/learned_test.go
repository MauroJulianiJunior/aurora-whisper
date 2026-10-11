package pgstore

import (
	"fmt"
	"io/fs"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Os testes deste arquivo NÃO precisam de Postgres e rodam sempre: a junção do
// pull (mergePage) e a leitura do SQL do aprendido e da operação contra as
// migrations. O que só o banco prova está em learned_pg_test.go.

// pageOf é o pull como o Store em memória o faz: as listas inteiras, juntas
// por seq, cortadas por CutPage. É a referência de mergePage.
func pageOf(limit int, all []cloudstore.Change) ([]cloudstore.Change, bool) {
	sorted := append([]cloudstore.Change(nil), all...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	n, more := cloudstore.CutPage(sorted, limit, cloudstore.Change.TextBytes)
	return sorted[:n], more
}

// readAsPull simula as três leituras do Pull sobre as listas inteiras depois
// do cursor: até limit+1 linhas de cada uma, e os ditados param uma linha
// depois da que chega ao orçamento de texto (o laço de Store.Pull).
func readAsPull(limit int, after int64, items, tombs, learned []cloudstore.Change) (a, b, c []cloudstore.Change) {
	take := func(l []cloudstore.Change) []cloudstore.Change {
		var out []cloudstore.Change
		for _, x := range l {
			if x.Seq > after && len(out) < limit+1 {
				out = append(out, x)
			}
		}
		return out
	}
	used, over := 0, false
	for _, x := range take(items) {
		a = append(a, x)
		if over {
			break
		}
		used += x.TextBytes()
		over = used >= cloudstore.PageTextBudget
	}
	return a, take(tombs), take(learned)
}

func seqsOf(cs []cloudstore.Change) []int64 {
	out := make([]int64, len(cs))
	for i, c := range cs {
		out[i] = c.Seq
	}
	return out
}

func TestMergePage_FundeAsTresListasPorSeq(t *testing.T) {
	item := func(seq int64) cloudstore.Change {
		return cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityTranscription, Operation: cloudstore.OpUpsert,
			Item: &cloudstore.Transcription{FinalText: "a"}}
	}
	tomb := func(seq int64) cloudstore.Change {
		return cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityTranscription, Operation: cloudstore.OpTombstone}
	}
	pair := func(seq int64) cloudstore.Change {
		return cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityLearned, Operation: cloudstore.OpUpsert,
			Learned: &cloudstore.Learned{Variant: "sf", Term: "ECF"}}
	}
	items := []cloudstore.Change{item(1), item(4), item(7)}
	tombs := []cloudstore.Change{tomb(2), tomb(8)}
	learned := []cloudstore.Change{pair(3), pair(5), pair(6)}
	for _, c := range []struct {
		limit int
		seqs  []int64
		more  bool
	}{
		{500, []int64{1, 2, 3, 4, 5, 6, 7, 8}, false},
		{8, []int64{1, 2, 3, 4, 5, 6, 7, 8}, false},
		{7, []int64{1, 2, 3, 4, 5, 6, 7}, true},
		{3, []int64{1, 2, 3}, true},
		{1, []int64{1}, true},
	} {
		got, more := mergePage(c.limit, items, tombs, learned)
		if fmt.Sprint(seqsOf(got)) != fmt.Sprint(c.seqs) || more != c.more {
			t.Errorf("limit %d: %v more %v; quero %v more %v", c.limit, seqsOf(got), more, c.seqs, c.more)
		}
		for _, ch := range got {
			if (ch.EntityType == cloudstore.EntityLearned) != (ch.Learned != nil) {
				t.Errorf("limit %d: a mudança %d perdeu o payload do tipo dela: %+v", c.limit, ch.Seq, ch)
			}
		}
	}
	if got, more := mergePage(500); got == nil || len(got) != 0 || more {
		t.Errorf("sem nada: %v more %v; quero a lista vazia, não nula", got, more)
	}
}

// A junção sobre o que o Pull lê (os ditados cortados no orçamento, cada
// lista em limit+1) dá a mesma página que a junção das listas inteiras (o
// Store em memória), página a página, até o fim: nada se perde nem se repete,
// com ditados no teto, lápides e aprendidos misturados. Determinístico (a
// semente é fixa).
func TestMergePage_ComOQueOPullLeEhIgualAoStoreEmMemoria(t *testing.T) {
	big := strings.Repeat("x", cloudstore.MaxTextBytes)
	rng := rand.New(rand.NewSource(8))
	cutByBudget := 0
	for round := 0; round < 40; round++ {
		var items, tombs, learned, all []cloudstore.Change
		n := 1 + rng.Intn(60)
		for seq := int64(1); seq <= int64(n); seq++ {
			var c cloudstore.Change
			switch k := rng.Intn(10); {
			case k < 5:
				size := rng.Intn(cloudstore.MaxTextBytes + 1)
				if rng.Intn(3) == 0 {
					size = cloudstore.MaxTextBytes
				}
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityTranscription, Operation: cloudstore.OpUpsert,
					Item: &cloudstore.Transcription{RawText: big[:size], FinalText: big[:size]}}
				items = append(items, c)
			case k < 7:
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityTranscription, Operation: cloudstore.OpTombstone}
				tombs = append(tombs, c)
			default:
				v := strings.Repeat("é", 1+rng.Intn(80))
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityLearned, Operation: cloudstore.OpUpsert,
					Learned: &cloudstore.Learned{Variant: v, Term: v, Domain: &v}}
				learned = append(learned, c)
			}
			all = append(all, c)
		}
		for _, limit := range []int{1, 2, 5, 17, 500} {
			var after int64
			var seen []int64
			for pages := 0; ; pages++ {
				if pages > n+1 {
					t.Fatalf("rodada %d, limit %d: o cursor não anda", round, limit)
				}
				var rest []cloudstore.Change
				for _, c := range all {
					if c.Seq > after {
						rest = append(rest, c)
					}
				}
				want, wantMore := pageOf(limit, rest)
				a, b, c := readAsPull(limit, after, items, tombs, learned)
				got, more := mergePage(limit, a, b, c)
				if fmt.Sprint(seqsOf(got)) != fmt.Sprint(seqsOf(want)) || more != wantMore {
					t.Fatalf("rodada %d, limit %d, depois de %d: %v more %v; o Store em memória dá %v more %v",
						round, limit, after, seqsOf(got), more, seqsOf(want), wantMore)
				}
				seen = append(seen, seqsOf(got)...)
				if more && len(got) < limit {
					cutByBudget++
				}
				if len(got) > 0 {
					after = got[len(got)-1].Seq
				}
				if !more {
					break
				}
			}
			if len(seen) != n {
				t.Fatalf("rodada %d, limit %d: o pull leu %d mudanças, quero as %d", round, limit, len(seen), n)
			}
			for i, s := range seen {
				if s != int64(i+1) {
					t.Fatalf("rodada %d, limit %d: o pull leu %v, quero 1..%d sem buraco nem repetição", round, limit, seen, n)
				}
			}
		}
	}
	// Sem páginas cortadas pelo orçamento, o caso dos ditados lidos pela
	// metade não teria sido exercitado.
	if cutByBudget == 0 {
		t.Fatal("nenhuma página foi cortada pelo orçamento de texto: o teste não exercita a leitura parada nele")
	}
}

// createTableColumns lê as colunas do CREATE TABLE de uma migration embutida:
// o primeiro identificador de cada linha, menos as de constraint.
func createTableColumns(t *testing.T, file string) map[string]bool {
	t.Helper()
	raw, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatalf("ler %s: %v", file, err)
	}
	sql := string(raw)
	start := strings.Index(sql, "CREATE TABLE ")
	end := strings.Index(sql[start:], "\n);")
	if start < 0 || end < 0 {
		t.Fatalf("%s: CREATE TABLE não encontrado", file)
	}
	cols := map[string]bool{}
	first := regexp.MustCompile(`^\s*([a-z_][a-z0-9_]*)\s`)
	for _, line := range strings.Split(sql[start:start+end], "\n")[1:] {
		m := first.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch m[1] {
		case "primary", "unique", "foreign", "check", "constraint":
			continue
		}
		cols[m[1]] = true
	}
	return cols
}

// listOf separa uma lista de colunas ("a, b, c") ou de parâmetros.
func listOf(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// As colunas que o ramo do aprendido lê e grava são as da 0016, todas, e o
// INSERT tem um parâmetro por coluna. O UPDATE não toca a chave
// (normalized_variant) nem created_at, e grava só colunas da 0016. Sem
// Postgres, é o que impede um nome de coluna errado de só aparecer quando o
// Mauro subir o contêiner.
func TestSQL_OAprendidoUsaAsColunasDa0016(t *testing.T) {
	table := createTableColumns(t, "0016_learned_pairs_table.sql")
	if len(learnedCols) != len(table) {
		t.Errorf("learnedCols tem %d colunas, a 0016 tem %d", len(learnedCols), len(table))
	}
	seen := map[string]bool{}
	for _, c := range learnedCols {
		if !table[c] {
			t.Errorf("learnedCols: %q não é coluna da 0016", c)
		}
		if seen[c] {
			t.Errorf("learnedCols: %q repetida", c)
		}
		seen[c] = true
	}
	m := regexp.MustCompile(`(?s)^INSERT INTO learned_pairs \((.*)\) VALUES \((.*)\)$`).FindStringSubmatch(sqlInsertLearned)
	if m == nil || len(listOf(m[1])) != len(learnedCols) || len(listOf(m[2])) != len(learnedCols) {
		t.Errorf("o INSERT do aprendido não tem uma coluna e um parâmetro por coluna: %s", sqlInsertLearned)
	}

	set := regexp.MustCompile(`(?s)SET(.*)WHERE`).FindStringSubmatch(sqlUpdateLearned)
	if set == nil {
		t.Fatalf("o UPDATE do aprendido sem SET ... WHERE: %s", sqlUpdateLearned)
	}
	for _, a := range listOf(set[1]) {
		col := strings.TrimSpace(strings.SplitN(a, "=", 2)[0])
		if !table[col] {
			t.Errorf("o UPDATE do aprendido grava %q, que não é coluna da 0016", col)
		}
		if col == "normalized_variant" || col == "created_at" || col == "id" || col == "user_id" {
			t.Errorf("o UPDATE do aprendido grava %q: a chave, o dono e a criação não mudam", col)
		}
	}
	for _, frag := range []string{"WHERE user_id = $1 AND id = $2 AND version = $11"} {
		if !strings.Contains(sqlUpdateLearned, frag) {
			t.Errorf("o UPDATE do aprendido sem %q: %s", frag, sqlUpdateLearned)
		}
	}
	for _, frag := range []string{"FROM learned_pairs l", "e.seq = l.last_seq", "l.last_seq > $2", "ORDER BY l.last_seq", "LIMIT $3"} {
		if !strings.Contains(sqlPullLearned, frag) {
			t.Errorf("a consulta do pull do aprendido sem %q: %s", frag, sqlPullLearned)
		}
	}
	if !strings.Contains(sqlLearnedKeyTaken, "normalized_variant = $2") {
		t.Errorf("a conferência da chave não olha normalized_variant: %s", sqlLearnedKeyTaken)
	}
}

// A applied_mutation grava e lê a operação (a coluna da 0009, F8c), com um
// parâmetro por coluna, e só colunas da 0009.
func TestSQL_AMutacaoAplicadaGravaELeAOperacao(t *testing.T) {
	table := createTableColumns(t, "0009_applied_mutations_table.sql")
	if !table["operation"] {
		t.Fatal("a 0009 não tem a coluna operation")
	}
	m := regexp.MustCompile(`(?s)INSERT INTO applied_mutations\s*\((.*)\)\s*VALUES \((.*)\)`).FindStringSubmatch(sqlRecordApplied)
	if m == nil {
		t.Fatalf("o INSERT da mutação aplicada não se lê: %s", sqlRecordApplied)
	}
	cols, params := listOf(m[1]), listOf(m[2])
	if len(cols) != len(params) || len(cols) != 11 {
		t.Errorf("o INSERT da mutação aplicada tem %d colunas e %d parâmetros, quero 11 e 11 (os argumentos de recordApplied)", len(cols), len(params))
	}
	for i, c := range cols {
		if !table[c] {
			t.Errorf("o INSERT da mutação aplicada grava %q, que não é coluna da 0009", c)
		}
		if params[i] != fmt.Sprintf("$%d", i+1) {
			t.Errorf("o parâmetro %d do INSERT é %q", i+1, params[i])
		}
	}
	if cols[5] != "operation" {
		t.Errorf("a sexta coluna do INSERT é %q, quero operation (a ordem dos argumentos de recordApplied)", cols[5])
	}
	sel := regexp.MustCompile(`(?s)SELECT (.*?)\s+FROM applied_mutations`).FindStringSubmatch(sqlApplied)
	if sel == nil {
		t.Fatalf("a leitura da mutação aplicada não se lê: %s", sqlApplied)
	}
	read := listOf(sel[1])
	want := []string{"entity_type", "entity_id", "device_id", "operation", "status", "result_version", "merged", "result_code"}
	if fmt.Sprint(read) != fmt.Sprint(want) {
		t.Errorf("a leitura da mutação aplicada = %v, quero %v (a ordem do Scan de readApplied)", read, want)
	}
}

// A listagem do celular (F8d) lê as colunas da 0016 na ordem do scanLearned,
// só os pares vivos do usuário (undone_at IS NULL), do mais recente (last_seq)
// para o mais antigo, com o limite como parâmetro. Sem Postgres, é o que
// impede um nome de coluna ou uma ordem errada de só aparecer quando o Mauro
// subir o contêiner (o teste com o banco está em learned_celular_pg_test.go).
func TestSQL_AListagemDoCelular(t *testing.T) {
	table := createTableColumns(t, "0016_learned_pairs_table.sql")
	m := regexp.MustCompile(`(?s)^SELECT (.*?)\s+FROM learned_pairs\s+WHERE (.*?)\s+ORDER BY (.*?)\s+LIMIT (.*)$`).FindStringSubmatch(sqlListLearned)
	if m == nil {
		t.Fatalf("a listagem do celular não se lê: %s", sqlListLearned)
	}
	if got := listOf(m[1]); fmt.Sprint(got) != fmt.Sprint(learnedCols) {
		t.Errorf("a listagem lê %v, quero as colunas do scanLearned, nesta ordem: %v", got, learnedCols)
	}
	for _, c := range listOf(m[1]) {
		if !table[c] {
			t.Errorf("a listagem lê %q, que não é coluna da 0016", c)
		}
	}
	if m[2] != "user_id = $1 AND undone_at IS NULL" || m[3] != "last_seq DESC" || strings.TrimSpace(m[4]) != "$2" {
		t.Errorf("a listagem: WHERE %q, ORDER BY %q, LIMIT %q; quero só os vivos do usuário, do last_seq maior para o menor, com o limite em $2",
			m[2], m[3], m[4])
	}
	if !table["undone_at"] || !table["last_seq"] {
		t.Error("a 0016 perdeu undone_at ou last_seq, de que a listagem depende")
	}
}
