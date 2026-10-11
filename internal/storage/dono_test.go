package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os testes do dono fixo (decisão do Mauro, 29-set-2026) rodam SEMPRE em
// bancos temporários, em arquivo — a cópia é um VACUUM INTO para disco, e
// um banco em memória não exercitaria o que importa. Nunca o bin\historico.db.

// quandoDaCopia é a hora fixa que dá nome às cópias destes testes.
var quandoDaCopia = time.Date(2026, 9, 29, 10, 15, 0, 0, time.Local)

// bancoEmArquivo cria um historico.db migrado numa pasta temporária.
func bancoEmArquivo(t *testing.T) (string, *sql.DB) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatalf("abrir: %v", err)
	}
	// Uma conexão, como no Agent (cmd/agent, abrirHistorico).
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return caminho, db
}

// executar roda um SQL de semeadura e falha o teste se ele falhar.
func executar(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// povoar grava o que o banco real do Mauro pode ter: ditados de "mauro",
// de "local" (Agent sem dicionário), de "" (versões antigas) e de um dono
// qualquer ("ana": um "usuario" que o dicionário teve um dia — a migração
// não pode depender de uma lista de donos conhecidos), mais uma linha em
// cada outra tabela com user_id. Devolve quantas linhas cada tabela com
// user_id ficou tendo.
func povoar(t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()
	repo := storage.NewSQLiteTranscriptionRepository(db)
	base := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for i, dono := range []string{"mauro", "mauro", "local", "", "ana"} {
		tr := newTranscription(storage.NewID(), dono, base.Add(time.Duration(i)*time.Minute))
		if err := repo.Put(tr); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	executar(t, db, `INSERT INTO local_dictionary_entries
		(id, user_id, canonical_form, normalized_form, created_at, updated_at)
		VALUES ('e1', 'mauro', 'Supabase', 'supabase', 'x', 'x')`)
	executar(t, db, `INSERT INTO local_corrections
		(id, user_id, from_text, to_text, normalized_from, normalized_to, created_at, updated_at)
		VALUES ('c1', 'local', 'de', 'para', 'de', 'para', 'x', 'x')`)
	executar(t, db, `INSERT INTO sync_state (user_id, device_id) VALUES ('mauro', 'dev-1')`)
	// sync_outbox não tem user_id: tem de passar pela migração intocada.
	executar(t, db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
		VALUES ('transcription', 't1', 1, 'x')`)
	// O espelho do aprendido e o "veio da nuvem" (0007) também são do dono.
	executar(t, db, `INSERT INTO sync_aprendido (user_id, entity_id) VALUES ('mauro', 'l1')`)
	executar(t, db, `INSERT INTO sync_veio_da_nuvem (user_id, entity_id) VALUES ('ana', 'l1')`)
	return map[string]int64{
		"local_transcriptions":     5,
		"local_dictionary_entries": 1,
		"local_corrections":        1,
		"sync_state":               1,
		"sync_aprendido":           1,
		"sync_veio_da_nuvem":       1,
	}
}

// tabelasComUserID são as tabelas do schema atual que têm user_id.
var tabelasComUserID = []string{
	"local_corrections", "local_dictionary_entries", "local_transcriptions", "sync_aprendido", "sync_state",
	"sync_veio_da_nuvem",
}

// donos devolve, para a tabela, quantas linhas cada user_id tem.
func donos(t *testing.T, db *sql.DB, tabela string) map[string]int64 {
	t.Helper()
	linhas, err := db.Query(`SELECT user_id, COUNT(1) FROM "` + tabela + `" GROUP BY user_id`)
	if err != nil {
		t.Fatalf("donos de %s: %v", tabela, err)
	}
	defer linhas.Close()
	m := map[string]int64{}
	for linhas.Next() {
		var u string
		var n int64
		if err := linhas.Scan(&u, &n); err != nil {
			t.Fatal(err)
		}
		m[u] = n
	}
	if err := linhas.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

// retrato é o user_id de todas as tabelas com dono, para comparar antes e
// depois.
func retrato(t *testing.T, db *sql.DB) map[string]map[string]int64 {
	t.Helper()
	r := map[string]map[string]int64{}
	for _, tb := range tabelasComUserID {
		r[tb] = donos(t, db, tb)
	}
	return r
}

func mesmoRetrato(a, b map[string]map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for tb, da := range a {
		dbb := b[tb]
		if len(da) != len(dbb) {
			return false
		}
		for u, n := range da {
			if dbb[u] != n {
				return false
			}
		}
	}
	return true
}

// valorDaInstalacao lê uma chave da tabela instalacao; "" e false sem ela.
func valorDaInstalacao(t *testing.T, db *sql.DB, chave string) (string, bool) {
	t.Helper()
	var v string
	err := db.QueryRow(`SELECT valor FROM instalacao WHERE nome = ?`, chave).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("lendo %s: %v", chave, err)
	}
	return v, true
}

func existe(caminho string) bool {
	_, err := os.Stat(caminho)
	return err == nil
}

// TestGarantirDonoFixo_CopiaAntesEMigraTudo é o caminho da primeira subida
// com o banco do Mauro: cópia completa, e todas as linhas para o dono novo.
func TestGarantirDonoFixo_CopiaAntesEMigraTudo(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	esperado := povoar(t, db)
	antes := retrato(t, db)
	copia := storage.CaminhoDaCopia(caminho, quandoDaCopia)

	r, err := storage.GarantirDonoFixo(ctx, db, copia)
	if err != nil {
		t.Fatalf("GarantirDonoFixo: %v", err)
	}
	if !r.Migrou || r.Copia != copia {
		t.Fatalf("Migrou=%v Copia=%q, quer true e %q", r.Migrou, r.Copia, copia)
	}
	if !uuidPattern.MatchString(r.Dono) {
		t.Errorf("dono %q não é um UUID v7 (storage.NewID)", r.Dono)
	}

	// Todas as linhas, de todas as tabelas com user_id, são do dono novo —
	// "mauro", "local", "" e "ana" inclusive.
	for _, tb := range tabelasComUserID {
		got := donos(t, db, tb)
		if len(got) != 1 || got[r.Dono] != esperado[tb] {
			t.Errorf("%s depois da migração: %v, quer só %d do dono novo", tb, got, esperado[tb])
		}
		if r.Linhas[tb] != esperado[tb] {
			t.Errorf("Linhas[%s] = %d, quer %d", tb, r.Linhas[tb], esperado[tb])
		}
	}
	if len(r.Linhas) != len(tabelasComUserID) {
		t.Errorf("Linhas tem %d tabela(s): %v", len(r.Linhas), r.Linhas)
	}
	if fora, err := storage.LinhasDeOutroDono(ctx, db, r.Dono); err != nil || len(fora) != 0 {
		t.Errorf("depois da migração, LinhasDeOutroDono = %v, %v; quer nenhuma", fora, err)
	}
	var semDono int
	if err := db.QueryRow(`SELECT COUNT(1) FROM sync_outbox`).Scan(&semDono); err != nil || semDono != 1 {
		t.Errorf("sync_outbox (sem user_id) mudou: %d linha(s), err %v", semDono, err)
	}

	// A cópia foi feita ANTES do UPDATE: tem todas as linhas, com os donos
	// antigos, e nenhum dono fixo.
	c, err := storage.AbrirSomenteLeitura(copia)
	if err != nil {
		t.Fatalf("abrindo a cópia: %v", err)
	}
	defer c.Close()
	if got := retrato(t, c); !mesmoRetrato(got, antes) {
		t.Errorf("a cópia não é o banco de antes da migração:\ncópia: %v\nantes: %v", got, antes)
	}
	if _, err := storage.LerDonoFixo(ctx, c); !errors.Is(err, storage.ErrSemDonoFixo) {
		t.Errorf("a cópia já tem dono fixo (err %v): foi feita depois de migrar", err)
	}

	// O banco registra o dono, quando migrou, e onde ficou a cópia.
	if got, err := storage.LerDonoFixo(ctx, db); err != nil || got != r.Dono {
		t.Errorf("LerDonoFixo = %q, %v; quer %q", got, err, r.Dono)
	}
	if v, ok := valorDaInstalacao(t, db, "dono_local_migrado_em"); !ok || v == "" {
		t.Error("o banco não registra quando migrou")
	}
	if v, _ := valorDaInstalacao(t, db, "dono_local_copia"); v != copia {
		t.Errorf("o banco registra a cópia em %q, quer %q", v, copia)
	}
}

// TestGarantirDonoFixo_Idempotente: com o dono gravado, nenhuma subida
// copia nem migra de novo — e a linha de outro dono gravada depois fica
// onde está, mas LinhasDeOutroDono a acha, para o aviso.
func TestGarantirDonoFixo_Idempotente(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	povoar(t, db)

	r1, err := storage.GarantirDonoFixo(ctx, db, storage.CaminhoDaCopia(caminho, quandoDaCopia))
	if err != nil {
		t.Fatalf("primeira: %v", err)
	}
	migradoEm, _ := valorDaInstalacao(t, db, "dono_local_migrado_em")
	// Uma linha de outro dono gravada DEPOIS da migração. Se a segunda
	// chamada migrasse de novo, ela mudaria de dono.
	outra := newTranscription(storage.NewID(), "gravada-depois", time.Now())
	if err := storage.NewSQLiteTranscriptionRepository(db).Put(outra); err != nil {
		t.Fatal(err)
	}

	segunda := storage.CaminhoDaCopia(caminho, quandoDaCopia.Add(time.Hour))
	r2, err := storage.GarantirDonoFixo(ctx, db, segunda)
	if err != nil {
		t.Fatalf("segunda: %v", err)
	}
	if r2.Dono != r1.Dono {
		t.Errorf("o dono mudou: %q, depois %q", r1.Dono, r2.Dono)
	}
	if r2.Migrou || r2.Copia != "" || len(r2.Linhas) != 0 {
		t.Errorf("a segunda chamada migrou: %+v", r2)
	}
	if existe(segunda) {
		t.Error("a segunda chamada copiou o banco de novo")
	}
	if got := donos(t, db, "local_transcriptions")["gravada-depois"]; got != 1 {
		t.Error("a segunda chamada mexeu numa linha gravada depois da migração")
	}
	if v, _ := valorDaInstalacao(t, db, "dono_local_migrado_em"); v != migradoEm {
		t.Errorf("migrado_em mudou: %q, depois %q", migradoEm, v)
	}
	fora, err := storage.LinhasDeOutroDono(ctx, db, r2.Dono)
	if err != nil {
		t.Fatal(err)
	}
	if len(fora) != 1 || fora["local_transcriptions"] != 1 {
		t.Errorf("LinhasDeOutroDono = %v, quer só local_transcriptions: 1", fora)
	}
}

// TestLinhasDeOutroDono conta toda linha que não é do dono, em toda
// tabela com user_id — inclusive user_id nulo, numa tabela que o permita —
// e deixa de fora as tabelas sem nenhuma.
func TestLinhasDeOutroDono(t *testing.T) {
	ctx := context.Background()
	_, db := bancoEmArquivo(t)
	executar(t, db, `CREATE TABLE "tabela futura" (id TEXT PRIMARY KEY, user_id TEXT)`)
	executar(t, db, `INSERT INTO "tabela futura" VALUES ('1', 'dono'), ('2', NULL), ('3', 'mauro')`)
	executar(t, db, `INSERT INTO sync_state (user_id, device_id) VALUES ('dono', 'dev-1')`)
	executar(t, db, `INSERT INTO local_corrections
		(id, user_id, from_text, to_text, normalized_from, normalized_to, created_at, updated_at)
		VALUES ('c1', '', 'de', 'para', 'de', 'para', 'x', 'x')`)

	fora, err := storage.LinhasDeOutroDono(ctx, db, "dono")
	if err != nil {
		t.Fatal(err)
	}
	quer := map[string]int64{"tabela futura": 2, "local_corrections": 1}
	if len(fora) != len(quer) {
		t.Fatalf("LinhasDeOutroDono = %v, quer %v", fora, quer)
	}
	for tb, n := range quer {
		if fora[tb] != n {
			t.Errorf("LinhasDeOutroDono[%s] = %d, quer %d (tudo: %v)", tb, fora[tb], n, fora)
		}
	}
}

// TestGarantirDonoFixo_DonoIlegivelNaoEhBancoSemDono: um dono gravado que
// não se lê (vazio, aqui) não é "banco sem dono". O erro não pode vir
// como ErrSemDonoFixo — quem chama usaria a regra antiga e gravaria
// ditados que o painel nunca mais mostraria —, e nada é copiado nem
// migrado por cima dele.
func TestGarantirDonoFixo_DonoIlegivelNaoEhBancoSemDono(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	povoar(t, db)
	executar(t, db, `INSERT INTO instalacao (nome, valor) VALUES ('dono_local', '')`)
	antes := retrato(t, db)
	copia := storage.CaminhoDaCopia(caminho, quandoDaCopia)

	_, err := storage.GarantirDonoFixo(ctx, db, copia)
	if err == nil {
		t.Fatal("aceitou um dono gravado vazio")
	}
	if errors.Is(err, storage.ErrSemDonoFixo) {
		t.Errorf("dono gravado e ilegível saiu como ErrSemDonoFixo: %v", err)
	}
	if existe(copia) {
		t.Error("copiou um banco que já tem dono")
	}
	if got := retrato(t, db); !mesmoRetrato(got, antes) {
		t.Errorf("migrou por cima de um dono gravado:\ndepois: %v\nantes:  %v", got, antes)
	}
}

// TestGarantirDonoFixo_LimiteDeCopias: cada tentativa que falha depois do
// VACUUM INTO deixa uma cópia com todo o texto ditado. Com MaxCopias já
// ao lado do banco, nenhuma nova é feita e nada migra; com uma a menos,
// migra. Arquivo de outro banco, ou que não termina em .db, não conta.
func TestGarantirDonoFixo_LimiteDeCopias(t *testing.T) {
	for _, c := range []struct {
		nome     string
		copias   int
		querErro bool
	}{
		{"no limite", storage.MaxCopias, true},
		{"uma abaixo do limite", storage.MaxCopias - 1, false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			ctx := context.Background()
			caminho, db := bancoEmArquivo(t)
			povoar(t, db)
			antes := retrato(t, db)
			for i := range c.copias {
				velha := storage.CaminhoDaCopia(caminho, quandoDaCopia.Add(-time.Duration(i+1)*time.Hour))
				if err := os.WriteFile(velha, []byte("cópia de uma tentativa anterior"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// Não contam: outro banco na mesma pasta, e um arquivo que não
			// termina em .db.
			for _, nome := range []string{
				storage.CaminhoDaCopia(filepath.Join(filepath.Dir(caminho), "outro.db"), quandoDaCopia),
				storage.CaminhoDaCopia(caminho, quandoDaCopia) + ".txt",
			} {
				if err := os.WriteFile(nome, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			copia := storage.CaminhoDaCopia(caminho, quandoDaCopia)

			r, err := storage.GarantirDonoFixo(ctx, db, copia)

			if !c.querErro {
				if err != nil || !r.Migrou || !existe(copia) {
					t.Fatalf("abaixo do limite não migrou: %+v, %v", r, err)
				}
				return
			}
			if !errors.Is(err, storage.ErrCopiaFalhou) || !errors.Is(err, storage.ErrSemDonoFixo) {
				t.Fatalf("err = %v, quer ErrCopiaFalhou e ErrSemDonoFixo", err)
			}
			if existe(copia) {
				t.Error("fez mais uma cópia com o limite atingido")
			}
			if got := retrato(t, db); !mesmoRetrato(got, antes) {
				t.Errorf("migrou sem cópia:\ndepois: %v\nantes:  %v", got, antes)
			}
		})
	}
}

// TestGarantirDonoFixo_CopiaQueFalhaNaoMigra: sem cópia, nenhuma linha
// muda — e a próxima subida tenta de novo.
func TestGarantirDonoFixo_CopiaQueFalhaNaoMigra(t *testing.T) {
	casos := []struct {
		nome    string
		destino func(t *testing.T, caminhoBanco string) string
	}{
		{"destino inescrevível", func(t *testing.T, _ string) string {
			// Uma pasta que não existe: nem o VACUUM INTO cria.
			return filepath.Join(t.TempDir(), "nao-existe", "copia.db")
		}},
		{"destino já existe", func(t *testing.T, caminhoBanco string) string {
			// Uma cópia anterior com o mesmo nome nunca é sobrescrita.
			d := storage.CaminhoDaCopia(caminhoBanco, quandoDaCopia)
			if err := os.WriteFile(d, []byte("cópia anterior"), 0o644); err != nil {
				t.Fatal(err)
			}
			return d
		}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx := context.Background()
			caminho, db := bancoEmArquivo(t)
			povoar(t, db)
			antes := retrato(t, db)
			destino := c.destino(t, caminho)
			conteudoAntes, _ := os.ReadFile(destino)

			_, err := storage.GarantirDonoFixo(ctx, db, destino)
			if !errors.Is(err, storage.ErrCopiaFalhou) {
				t.Fatalf("err = %v, quer ErrCopiaFalhou", err)
			}
			// E também ErrSemDonoFixo: é o que diz a quem chama que o banco
			// continua sem dono, e que a regra antiga ainda mostra tudo.
			if !errors.Is(err, storage.ErrSemDonoFixo) {
				t.Errorf("err = %v, quer também ErrSemDonoFixo", err)
			}
			if _, err := storage.LerDonoFixo(ctx, db); !errors.Is(err, storage.ErrSemDonoFixo) {
				t.Errorf("gravou o dono sem cópia (LerDonoFixo: %v)", err)
			}
			if got := retrato(t, db); !mesmoRetrato(got, antes) {
				t.Errorf("migrou sem cópia:\ndepois: %v\nantes:  %v", got, antes)
			}
			if conteudoDepois, _ := os.ReadFile(destino); string(conteudoDepois) != string(conteudoAntes) {
				t.Error("o arquivo que já estava no destino foi mexido")
			}

			// A próxima subida, com um destino bom, migra.
			r, err := storage.GarantirDonoFixo(ctx, db, storage.CaminhoDaCopia(caminho, quandoDaCopia.Add(time.Minute)))
			if err != nil || !r.Migrou {
				t.Fatalf("a nova tentativa não migrou: %+v, %v", r, err)
			}
		})
	}
}

// TestGarantirDonoFixo_FalhaNoMeioNaoDeixaParcial: a transação quebra na
// ÚLTIMA tabela, depois de as outras já terem sido atualizadas dentro
// dela — e nada fica: nem dono, nem linha migrada.
//
// A quebra é real, não simulada: sync_state tem chave (user_id,
// device_id), e dois donos antigos no mesmo dispositivo viram a mesma
// chave quando os dois passam a ser o dono novo.
func TestGarantirDonoFixo_FalhaNoMeioNaoDeixaParcial(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	povoar(t, db)
	executar(t, db, `INSERT INTO sync_state (user_id, device_id) VALUES ('local', 'dev-1')`)
	antes := retrato(t, db)
	copia := storage.CaminhoDaCopia(caminho, quandoDaCopia)

	_, err := storage.GarantirDonoFixo(ctx, db, copia)
	if err == nil {
		t.Fatal("migrou apesar do conflito em sync_state")
	}
	if errors.Is(err, storage.ErrCopiaFalhou) {
		t.Fatalf("a falha devia ser da transação, não da cópia: %v", err)
	}
	if !errors.Is(err, storage.ErrSemDonoFixo) {
		t.Errorf("transação desfeita não saiu como ErrSemDonoFixo (o banco continua sem dono): %v", err)
	}
	if _, err := storage.LerDonoFixo(ctx, db); !errors.Is(err, storage.ErrSemDonoFixo) {
		t.Errorf("o dono ficou gravado depois da falha (LerDonoFixo: %v)", err)
	}
	if _, ok := valorDaInstalacao(t, db, "dono_local_migrado_em"); ok {
		t.Error("migrado_em ficou gravado depois da falha")
	}
	if got := retrato(t, db); !mesmoRetrato(got, antes) {
		t.Errorf("migração parcial:\ndepois: %v\nantes:  %v", got, antes)
	}
	// A cópia fica, e o erro diz onde.
	if !existe(copia) || !strings.Contains(err.Error(), copia) {
		t.Errorf("a cópia (%s) sumiu ou o erro não diz onde ela está: %v", copia, err)
	}
}

// TestGarantirDonoFixo_BancoWAL: a cópia pega o que ainda está no -wal. Um
// cópia do arquivo .db pelo sistema de arquivos perderia essas linhas.
func TestGarantirDonoFixo_BancoWAL(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	executar(t, db, `PRAGMA journal_mode = WAL`)
	executar(t, db, `PRAGMA wal_autocheckpoint = 0`)
	povoar(t, db)
	antes := retrato(t, db)

	// A premissa do teste: as linhas ainda não estão no arquivo .db.
	cru := filepath.Join(t.TempDir(), "copia-crua.db")
	bytesDoDB, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cru, bytesDoDB, 0o644); err != nil {
		t.Fatal(err)
	}
	dbCru, err := sql.Open("sqlite", cru)
	if err != nil {
		t.Fatal(err)
	}
	var noArquivo int
	if err := dbCru.QueryRow(`SELECT COUNT(1) FROM local_transcriptions`).Scan(&noArquivo); err != nil {
		t.Fatalf("lendo a cópia crua: %v", err)
	}
	dbCru.Close()
	if noArquivo != 0 {
		t.Fatalf("premissa: %d ditado(s) já no .db; o teste não exercitaria o -wal", noArquivo)
	}

	copia := storage.CaminhoDaCopia(caminho, quandoDaCopia)
	if _, err := storage.GarantirDonoFixo(ctx, db, copia); err != nil {
		t.Fatalf("GarantirDonoFixo: %v", err)
	}
	c, err := storage.AbrirSomenteLeitura(copia)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := retrato(t, c); !mesmoRetrato(got, antes) {
		t.Errorf("a cópia perdeu o que estava no -wal:\ncópia: %v\nantes: %v", got, antes)
	}
}

// TestGarantirDonoFixo_BancoSemLinhas é a instalação nova: o dono é
// gerado, e nenhuma cópia de banco vazio fica largada ao lado dele.
func TestGarantirDonoFixo_BancoSemLinhas(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	copia := storage.CaminhoDaCopia(caminho, quandoDaCopia)

	r, err := storage.GarantirDonoFixo(ctx, db, copia)
	if err != nil {
		t.Fatalf("GarantirDonoFixo: %v", err)
	}
	if !r.Migrou || r.Copia != "" || existe(copia) {
		t.Errorf("banco vazio: Migrou=%v Copia=%q copia existe=%v; quer true, vazio, não", r.Migrou, r.Copia, existe(copia))
	}
	for _, tb := range tabelasComUserID {
		if n, ok := r.Linhas[tb]; !ok || n != 0 {
			t.Errorf("Linhas[%s] = %d (presente %v), quer 0", tb, n, ok)
		}
	}
	if got, err := storage.LerDonoFixo(ctx, db); err != nil || got != r.Dono {
		t.Errorf("LerDonoFixo = %q, %v; quer %q", got, err, r.Dono)
	}
}

// TestGarantirDonoFixo_TabelaNovaComUserID: as tabelas saem do schema, não
// de uma lista. Uma migration futura com user_id é migrada sem ninguém
// lembrar de acrescentá-la aqui.
func TestGarantirDonoFixo_TabelaNovaComUserID(t *testing.T) {
	ctx := context.Background()
	caminho, db := bancoEmArquivo(t)
	executar(t, db, `CREATE TABLE "tabela futura" (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)`)
	executar(t, db, `INSERT INTO "tabela futura" VALUES ('1', 'mauro'), ('2', 'local')`)

	r, err := storage.GarantirDonoFixo(ctx, db, storage.CaminhoDaCopia(caminho, quandoDaCopia))
	if err != nil {
		t.Fatalf("GarantirDonoFixo: %v", err)
	}
	if got := donos(t, db, "tabela futura"); len(got) != 1 || got[r.Dono] != 2 {
		t.Errorf("tabela futura depois da migração: %v", got)
	}
	if r.Linhas["tabela futura"] != 2 {
		t.Errorf("Linhas = %v", r.Linhas)
	}
}

// TestLerDonoFixo: o banco anterior a esta versão, sem a tabela, e o que
// tem a tabela mas ainda não o dono, são o mesmo caso para quem lê.
func TestLerDonoFixo(t *testing.T) {
	ctx := context.Background()

	t.Run("banco sem a tabela instalacao", func(t *testing.T) {
		_, db := bancoEmArquivo(t)
		executar(t, db, `DROP TABLE instalacao`)
		if _, err := storage.LerDonoFixo(ctx, db); !errors.Is(err, storage.ErrSemDonoFixo) {
			t.Errorf("err = %v, quer ErrSemDonoFixo", err)
		}
	})
	t.Run("tabela sem dono", func(t *testing.T) {
		_, db := bancoEmArquivo(t)
		if _, err := storage.LerDonoFixo(ctx, db); !errors.Is(err, storage.ErrSemDonoFixo) {
			t.Errorf("err = %v, quer ErrSemDonoFixo", err)
		}
	})
	t.Run("com dono, somente leitura", func(t *testing.T) {
		caminho, db := bancoEmArquivo(t)
		r, err := storage.GarantirDonoFixo(ctx, db, storage.CaminhoDaCopia(caminho, quandoDaCopia))
		if err != nil {
			t.Fatal(err)
		}
		ro, err := storage.AbrirSomenteLeitura(caminho)
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		if got, err := storage.LerDonoFixo(ctx, ro); err != nil || got != r.Dono {
			t.Errorf("LerDonoFixo = %q, %v; quer %q", got, err, r.Dono)
		}
	})
}

// TestAbrirSomenteLeitura: abre mesmo com '#' e '%' no caminho — no URI do
// SQLite eles têm significado — e não escreve.
func TestAbrirSomenteLeitura(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pasta #1 com 100%")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	caminho := filepath.Join(dir, "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	db.Close()

	ro, err := storage.AbrirSomenteLeitura(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	var n int
	if err := ro.QueryRow(`SELECT COUNT(1) FROM instalacao`).Scan(&n); err != nil {
		t.Fatalf("não leu o banco em %s: %v", caminho, err)
	}
	if _, err := ro.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('x', 'y')`); err == nil {
		t.Error("escreveu num banco aberto somente para leitura")
	}
}

func TestCaminhoDaCopia(t *testing.T) {
	got := storage.CaminhoDaCopia(filepath.Join("bin", "historico.db"), quandoDaCopia)
	want := filepath.Join("bin", "historico.db.antes-do-dono-20260929-101500.db")
	if got != want {
		t.Errorf("CaminhoDaCopia = %q, quer %q", got, want)
	}
}
