package storage

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

// A 0012 (a ordem manual das notas, N2b) só ganha a coluna posicao e um índice,
// e preenche a posição de cada nota na ordem que a lista tinha antes dela:
// fixadas primeiro, depois a última edição, e a criação mais recente
// desempata.

func ordemDeAntes(t *testing.T, db *sql.DB, user string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM local_notes WHERE user_id = ?
		ORDER BY fixada DESC, updated_at DESC, rowid DESC`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func ordemDeDepois(t *testing.T, db *sql.DB, user string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM local_notes WHERE user_id = ? ORDER BY posicao`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestMigracao0012_PreencheAPosicaoNaOrdemDeHoje(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	for _, v := range []string{"0008_sync_aprendido_perdido", "0009_notas", "0010_ditado_virou_nota", "0011_local_settings_gatilho_da_nota"} {
		aplicarSo(t, db, v)
	}
	// Notas embaralhadas: fixadas e comuns, horas fora da ordem de criação, duas
	// com a mesma hora (o rowid desempata), uma arquivada, uma na lixeira, e uma
	// segunda pessoa. 150 notas passam das 61 do primeiro dígito, para o
	// preenchimento atravessar os dígitos.
	insere := func(id, user string, fixada int, atualizada string, arquivada int, apagada any) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO local_notes (id, user_id, titulo, fixada, arquivada, created_at, updated_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, '2026-10-01T00:00:00.000000000Z', ?, ?)`,
			id, user, "t-"+id, fixada, arquivada, atualizada, apagada); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 150; i++ {
		// A hora anda para trás e para a frente; a cada 7ª nota repete a hora da anterior.
		h := (i * 37) % 59
		if i%7 == 0 && i > 0 {
			h = ((i - 1) * 37) % 59
		}
		hora := fmt.Sprintf("2026-10-10T10:%02d:00.000000000Z", h)
		var apagada any
		if i%23 == 0 {
			apagada = "2026-10-12T00:00:00.000000000Z"
		}
		insere(fmt.Sprintf("a%03d", i), "u1", boolInt(i%5 == 0), hora, i%11/10, apagada)
	}
	insere("b1", "u2", 0, "2026-10-10T10:00:00.000000000Z", 0, nil)
	insere("b2", "u2", 1, "2026-10-09T10:00:00.000000000Z", 0, nil)

	antes := ordemDeAntes(t, db, "u1")
	outraAntes := ordemDeAntes(t, db, "u2")
	linhasAntes := linhasDe(t, db, "local_notes")

	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if depois := ordemDeDepois(t, db, "u1"); !reflect.DeepEqual(antes, depois) {
		t.Errorf("a ordem mudou na migration:\nantes  %v\ndepois %v", antes, depois)
	}
	if depois := ordemDeDepois(t, db, "u2"); !reflect.DeepEqual(outraAntes, depois) {
		t.Errorf("a ordem da segunda pessoa mudou: %v → %v", outraAntes, depois)
	}

	// Toda chave é válida, e não há duas iguais por pessoa.
	rows, err := db.Query(`SELECT user_id, posicao FROM local_notes`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	vistas := map[string]bool{}
	for rows.Next() {
		var u, p string
		if err := rows.Scan(&u, &p); err != nil {
			t.Fatal(err)
		}
		if !PosicaoValida(p) {
			t.Errorf("posição inválida %q", p)
		}
		if vistas[u+"/"+p] {
			t.Errorf("posição repetida %q", p)
		}
		vistas[u+"/"+p] = true
	}

	// O resto de cada nota ficou como estava.
	linhasDepois := linhasDe(t, db, "local_notes")
	for i := range linhasDepois {
		delete(linhasDepois[i], "posicao")
		// As colunas da 0013 (o Migrate roda até a última), nulas.
		for _, c := range []string{"conflito_titulo", "conflito_corpo", "conflito_device_id", "conflito_em", "corpo_nuvem"} {
			if v := linhasDepois[i][c]; v != nil {
				t.Errorf("a coluna %s da 0013 = %v; quero nula", c, v)
			}
			delete(linhasDepois[i], c)
		}
	}
	if !reflect.DeepEqual(linhasAntes, linhasDepois) {
		t.Error("a migration mexeu em outra coluna das notas")
	}

	// O índice nasceu, e a nota nova cai acima da primeira do grupo.
	var indice int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_local_notes_ordem'`).Scan(&indice); err != nil || indice != 1 {
		t.Errorf("o índice: %d, %v", indice, err)
	}
	repo := NewSQLiteNotaRepository(db)
	nova, err := repo.Criar(ctx, NovaNota{UserID: "u2", Titulo: "nova"})
	if err != nil {
		t.Fatal(err)
	}
	lista, err := repo.Listar(ctx, FiltroDeNotas{UserID: "u2"})
	if err != nil {
		t.Fatal(err)
	}
	// u2: b2 (fixada) e b1; a nova é do grupo das comuns e fica acima de b1.
	if len(lista) != 3 || lista[0].ID != "b2" || lista[1].ID != nova.ID || lista[2].ID != "b1" {
		t.Errorf("depois da migration, a nota nova deveria abrir o grupo das comuns: %v", titulosDe(lista))
	}
}

func titulosDe(ns []Nota) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Titulo + "/" + n.ID
	}
	return out
}
