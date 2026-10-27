package pgstore

import (
	"fmt"
	"io/fs"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Os testes deste arquivo NÃO precisam de Postgres e rodam sempre: o SQL das
// notas e dos marcadores (notas.go) lido contra as migrations 0020 a 0025, e a
// junção do pull com as notas. O comportamento no banco é a suíte de contrato
// (contracttest/notas.go), que roda contra o Postgres em TestContrato_Postgres.

func lerArquivo(t *testing.T, file string) string {
	t.Helper()
	raw, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatalf("ler %s: %v", file, err)
	}
	return string(raw)
}

// As colunas que a nota e o marcador leem e gravam são as das 0021 e 0020,
// todas, e o INSERT tem um parâmetro por coluna. O UPDATE não toca o id, o
// dono, a criação nem o ditado de origem.
func TestSQL_ANotaEOMarcadorUsamAsColunasDas0020E0021(t *testing.T) {
	for _, c := range []struct {
		file   string
		cols   []string
		insert string
		update string
		frozen []string
		where  string
	}{
		{"0021_notes_table.sql", noteCols, sqlInsertNote, sqlUpdateNote,
			[]string{"id", "user_id", "created_at", "ditado_de_origem"}, "WHERE user_id = $1 AND id = $2 AND version = $22"},
		{"0020_labels_table.sql", labelCols, sqlInsertLabel, sqlUpdateLabel,
			[]string{"id", "user_id", "created_at"}, "WHERE user_id = $1 AND id = $2 AND version = $7"},
	} {
		table := createTableColumns(t, c.file)
		if len(c.cols) != len(table) {
			t.Errorf("%s: as colunas do Go são %d, a migration tem %d", c.file, len(c.cols), len(table))
		}
		seen := map[string]bool{}
		for _, col := range c.cols {
			if !table[col] || seen[col] {
				t.Errorf("%s: a coluna %q não é da migration, ou está repetida", c.file, col)
			}
			seen[col] = true
		}
		m := regexp.MustCompile(`(?s)^INSERT INTO \w+ \((.*)\) VALUES \((.*)\)$`).FindStringSubmatch(c.insert)
		if m == nil || len(listOf(m[1])) != len(c.cols) || len(listOf(m[2])) != len(c.cols) {
			t.Errorf("%s: o INSERT não tem uma coluna e um parâmetro por coluna: %s", c.file, c.insert)
		}
		set := regexp.MustCompile(`(?s)SET(.*)WHERE`).FindStringSubmatch(c.update)
		if set == nil {
			t.Fatalf("%s: o UPDATE sem SET ... WHERE: %s", c.file, c.update)
		}
		for _, a := range listOf(set[1]) {
			col := strings.TrimSpace(strings.SplitN(a, "=", 2)[0])
			if !table[col] {
				t.Errorf("%s: o UPDATE grava %q, que não é coluna", c.file, col)
			}
			for _, f := range c.frozen {
				if col == f {
					t.Errorf("%s: o UPDATE grava %q, que não muda", c.file, col)
				}
			}
		}
		if !strings.Contains(c.update, c.where) {
			t.Errorf("%s: o UPDATE sem %q", c.file, c.where)
		}
	}
	// noteArgs tem um valor por coluna, na ordem do INSERT.
	if got := len(noteArgs(cloudstore.Note{})); got != len(noteCols) {
		t.Errorf("noteArgs tem %d valores, noteCols tem %d", got, len(noteCols))
	}
	// O UPDATE da nota usa os parâmetros de $1 a $22, cada um uma vez (são os
	// 22 argumentos de putNote).
	usos := map[string]int{}
	for _, p := range regexp.MustCompile(`\$\d+`).FindAllString(sqlUpdateNote, -1) {
		usos[p]++
	}
	for i := 1; i <= 22; i++ {
		if n := usos[fmt.Sprintf("$%d", i)]; n != 1 {
			t.Errorf("o UPDATE da nota usa $%d %d vezes", i, n)
		}
	}
	if len(usos) != 22 {
		t.Errorf("o UPDATE da nota usa %d parâmetros, quero 22", len(usos))
	}
}

// As listas fechadas da 0021 são as do Go: as oito cores, e a posição do índice
// fracionário (a expressão do CHECK aplicada a uma tabela de chaves dá o mesmo
// que cloudstore.PosicaoValida).
func TestSQL_AsListasDa0021SaoAsDoGo(t *testing.T) {
	sql := lerArquivo(t, "0021_notes_table.sql")
	m := regexp.MustCompile(`cor IN \(([^)]*)\)`).FindStringSubmatch(sql)
	if m == nil {
		t.Fatal("0021: não achei o CHECK da cor")
	}
	var cores []string
	for _, c := range listOf(m[1]) {
		cores = append(cores, strings.Trim(c, "'"))
	}
	if strings.Join(cores, ",") != strings.Join(cloudstore.CoresDaNota, ",") {
		t.Errorf("as cores da 0021 = %v, as do Go = %v", cores, cloudstore.CoresDaNota)
	}
	p := regexp.MustCompile(`posicao ~ '([^']+)' AND right\(posicao, 1\) <> '0'`).FindStringSubmatch(sql)
	if p == nil {
		t.Fatal("0021: não achei o CHECK da posição")
	}
	re := regexp.MustCompile(p[1])
	for _, k := range []string{"V", "0V", "a0b", "Az", strings.Repeat("z", 128), "", "0", "V0", "a-b", "é", " V", strings.Repeat("z", 129)} {
		sqlOk := re.MatchString(k) && !strings.HasSuffix(k, "0")
		if sqlOk != cloudstore.PosicaoValida(k) {
			t.Errorf("a posição %q: o CHECK diz %v, o Go diz %v", k, sqlOk, cloudstore.PosicaoValida(k))
		}
	}
	if !strings.Contains(sql, `posicao            TEXT COLLATE "C"`) {
		t.Error(`0021: a posição não é COLLATE "C" (a ordem dos bytes)`)
	}
	if !strings.Contains(sql, "octet_length(titulo) <= 1024") || cloudstore.MaxNoteTitleBytes != 1024 ||
		!strings.Contains(sql, "octet_length(corpo) <= 65536") || cloudstore.MaxTextBytes != 65536 {
		t.Error("os tetos do título e do corpo não são os do Go")
	}
	if !strings.Contains(lerArquivo(t, "0020_labels_table.sql"), "char_length(nome) BETWEEN 1 AND 50") || cloudstore.MaxLabelName != 50 {
		t.Error("o teto do nome do marcador não é o do Go")
	}
}

// As consultas das notas: o pull pelo last_seq com o evento da
// note_sync_events, as listagens na ordem do Go (cloudstore.NoteBefore e
// LabelBefore), os marcadores da nota por subconsulta, a lápide e a arrumação.
func TestSQL_AsConsultasDasNotas(t *testing.T) {
	for name, c := range map[string]struct {
		sql   string
		frags []string
	}{
		"o pull das notas": {sqlPullNotes, []string{"FROM notes n", "LEFT JOIN note_sync_events e ON e.user_id = n.user_id AND e.seq = n.last_seq",
			"n.last_seq > $2", "ORDER BY n.last_seq", "LIMIT $3", "FROM note_labels nl WHERE nl.user_id = n.user_id AND nl.note_id = n.id ORDER BY nl.label_id"}},
		"o pull dos marcadores": {sqlPullLabels, []string{"FROM labels b", "LEFT JOIN note_sync_events e ON e.user_id = b.user_id AND e.seq = b.last_seq",
			"b.last_seq > $2", "ORDER BY b.last_seq", "LIMIT $3"}},
		"o pull das lápides":   {sqlPullNoteTombstones, []string{"FROM note_tombstones", "seq > $2", "ORDER BY seq", "LIMIT $3"}},
		"as ativas":            {sqlListNotesAtivas, []string{"n.deleted_at IS NULL AND NOT n.arquivada", "ORDER BY n.fixada DESC, n.posicao, n.id", "LIMIT $2"}},
		"o arquivo":            {sqlListNotesArquivo, []string{"n.deleted_at IS NULL AND n.arquivada", "ORDER BY n.fixada DESC, n.posicao, n.id", "LIMIT $2"}},
		"a lixeira":            {sqlListNotesLixeira, []string{"n.deleted_at IS NOT NULL", "ORDER BY n.deleted_at DESC, n.id", "LIMIT $2"}},
		"os marcadores":        {sqlListLabels, []string{"WHERE user_id = $1", `ORDER BY nome COLLATE "C", id`, "LIMIT $2"}},
		"a nota travada":       {sqlSelectNote, []string{"FROM notes n WHERE n.user_id = $1 AND n.id = $2"}},
		"as ligações":          {sqlInsertNoteLabels, []string{"INSERT INTO note_labels (user_id, note_id, label_id)", "unnest($3::text[]::uuid[])"}},
		"o apagar do marcador": {sqlDeleteLabelLinks, []string{"DELETE FROM note_labels WHERE user_id = $1 AND label_id = $2"}},
		"a lápide":             {sqlNoteTombstone, []string{"INSERT INTO note_tombstones (user_id, entity_type, entity_id, seq)"}},
		"o evento":             {sqlNoteEvent, []string{"INSERT INTO note_sync_events", "(user_id, seq, entity_type, entity_id, operation, entity_version, device_id, changed_at)"}},
		"a arrumação dos eventos das notas": {sqlArrumarEventosDasNotas, []string{"DELETE FROM note_sync_events", "e.changed_at <= $2",
			"NOT EXISTS (SELECT 1 FROM notes n WHERE n.user_id = e.user_id AND n.last_seq = e.seq)",
			"NOT EXISTS (SELECT 1 FROM labels b WHERE b.user_id = e.user_id AND b.last_seq = e.seq)", "LIMIT $3"}},
		"o virou_nota do ditado": {sqlSelectOne, []string{"(SELECT vn.note_id FROM transcription_note_links vn WHERE vn.user_id = transcriptions.user_id AND vn.transcription_id = transcriptions.id)"}},
		"o virou_nota no pull":   {sqlPullEntities, []string{"vn.user_id = t.user_id AND vn.transcription_id = t.id"}},
		"a ligação":              {sqlGravarVirouNota, []string{"INSERT INTO transcription_note_links (user_id, transcription_id, note_id)"}},
	} {
		for _, f := range c.frags {
			if !strings.Contains(c.sql, f) {
				t.Errorf("%s: sem %q:\n%s", name, f, c.sql)
			}
		}
	}
	// As colunas da ligação e da lápide existem nas migrations.
	links := createTableColumns(t, "0025_transcription_note_links_table.sql")
	tombs := createTableColumns(t, "0023_note_tombstones_table.sql")
	events := createTableColumns(t, "0024_note_sync_events_table.sql")
	nl := createTableColumns(t, "0022_note_labels_table.sql")
	for _, c := range []struct {
		table map[string]bool
		cols  []string
	}{
		{links, []string{"user_id", "transcription_id", "note_id"}},
		{tombs, []string{"user_id", "entity_type", "entity_id", "seq"}},
		{events, []string{"user_id", "seq", "entity_type", "entity_id", "operation", "entity_version", "device_id", "changed_at"}},
		{nl, []string{"user_id", "note_id", "label_id"}},
	} {
		if len(c.table) != len(c.cols) {
			t.Errorf("a tabela tem %v, o SQL usa %v", c.table, c.cols)
		}
		for _, col := range c.cols {
			if !c.table[col] {
				t.Errorf("a coluna %s não existe (%v)", col, c.table)
			}
		}
	}
}

// A junção do pull com as notas: duas listas que param no orçamento de texto
// (os ditados e as notas), mais os marcadores e as lápides, dão a mesma página
// que a junção das listas inteiras (o Store em memória), página a página, até
// o fim. Determinístico (a semente é fixa).
func TestMergePage_ComAsNotasEhIgualAoStoreEmMemoria(t *testing.T) {
	big := strings.Repeat("x", cloudstore.MaxTextBytes)
	rng := rand.New(rand.NewSource(7))
	cutByBudget := 0
	// readBudget lê uma lista como o Pull: até limit+1, parando uma linha
	// depois da que chega ao orçamento.
	readBudget := func(l []cloudstore.Change, after int64, limit int) []cloudstore.Change {
		var out []cloudstore.Change
		used, over := 0, false
		for _, x := range l {
			if x.Seq <= after || len(out) == limit+1 {
				continue
			}
			out = append(out, x)
			if over {
				break
			}
			used += x.TextBytes()
			over = used >= cloudstore.PageTextBudget
		}
		return out
	}
	take := func(l []cloudstore.Change, after int64, limit int) []cloudstore.Change {
		var out []cloudstore.Change
		for _, x := range l {
			if x.Seq > after && len(out) < limit+1 {
				out = append(out, x)
			}
		}
		return out
	}
	for round := 0; round < 40; round++ {
		var items, notes, labels, tombs, all []cloudstore.Change
		n := 1 + rng.Intn(60)
		for seq := int64(1); seq <= int64(n); seq++ {
			var c cloudstore.Change
			size := rng.Intn(cloudstore.MaxTextBytes + 1)
			if rng.Intn(3) == 0 {
				size = cloudstore.MaxTextBytes
			}
			switch k := rng.Intn(10); {
			case k < 3:
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityTranscription, Operation: cloudstore.OpUpsert,
					Item: &cloudstore.Transcription{RawText: big[:size], FinalText: big[:size]}}
				items = append(items, c)
			case k < 6:
				nota := &cloudstore.Note{Titulo: "t", Corpo: big[:size]}
				if rng.Intn(4) == 0 {
					nota.Conflict = &cloudstore.NoteConflict{Corpo: big[:size]}
				}
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityNote, Operation: cloudstore.OpUpsert, Note: nota}
				notes = append(notes, c)
			case k < 8:
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityLabel, Operation: cloudstore.OpUpsert,
					Label: &cloudstore.Label{Nome: strings.Repeat("é", 1+rng.Intn(50))}}
				labels = append(labels, c)
			default:
				c = cloudstore.Change{Seq: seq, EntityType: cloudstore.EntityNote, Operation: cloudstore.OpTombstone}
				tombs = append(tombs, c)
			}
			all = append(all, c)
		}
		for _, limit := range []int{1, 3, 17, 500} {
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
				got, more := mergePage(limit, readBudget(items, after, limit), nil, nil,
					readBudget(notes, after, limit), take(labels, after, limit), take(tombs, after, limit))
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
		}
	}
	if cutByBudget == 0 {
		t.Fatal("nenhuma página foi cortada pelo orçamento de texto")
	}
}
