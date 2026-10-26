package integration_test

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// A prova de que o histórico de verdade não perde nem muda nada (ADR-019,
// resposta 3 e "Decisões da F7c"): o historico.db do bin\ como ele está hoje
// (as migrations de verdade 0001 a 0005, e os dados do fixture
// testdata/historico-bin-0005/dados.sql: 221 ditados de todo tipo, três de
// outro dono, as preferências, a instalação revogada da F6, o dicionário
// antigo e uma correção), COPIADO para a pasta do PC, como o Agent novo o
// abriria. Depois, contra a nuvem de verdade: a 0006, a conexão, o envio
// inicial e o "Esquecer esta nuvem". Cada tabela é lida linha a linha e
// coluna a coluna, antes e depois de cada passo.

const (
	fixtureDoBin  = "../../../testdata/historico-bin-0005/dados.sql"
	migrationsDir = "../../storage/migrations"
	donoDoFixture = "0199a000-0000-7000-8000-00000000d0d0"
)

// montarFixture cria, em caminho, o historico.db do bin\ na 0005: as
// migrations embutidas até a 0005, aplicadas como o Migrator aplica (uma
// transação por arquivo, com o registro), e os dados do fixture.
func montarFixture(t *testing.T, caminho string) {
	t.Helper()
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%.80s: %v", q, err)
		}
	}
	exec("PRAGMA foreign_keys = ON;")
	exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL);`)
	arquivos, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(arquivos)
	for _, a := range arquivos {
		versao := strings.TrimSuffix(filepath.Base(a), ".sql")
		if versao > "0005_sync_fila" {
			break
		}
		conteudo, err := os.ReadFile(a)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(string(conteudo)); err != nil {
			t.Fatalf("%s: %v", versao, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, '2026-10-06T00:00:00Z')`, versao); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	dados, err := os.ReadFile(fixtureDoBin)
	if err != nil {
		t.Fatal(err)
	}
	exec(string(dados))
}

// lerTabelas lê todas as tabelas de dados do banco em caminho, somente
// leitura, cada linha como coluna -> valor, em ordem de rowid.
func lerTabelas(t *testing.T, caminho string) map[string][]map[string]any {
	t.Helper()
	db, err := storage.AbrirSomenteLeitura(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	return lerTabelasDe(t, db)
}

func lerTabelasDe(t *testing.T, db *sql.DB) map[string][]map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var nomes []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		nomes = append(nomes, n)
	}
	rows.Close()
	out := map[string][]map[string]any{}
	for _, tb := range nomes {
		r, err := db.Query(`SELECT * FROM "` + tb + `" ORDER BY rowid`)
		if err != nil {
			// Tabela sem rowid (nenhuma hoje): pela chave.
			r, err = db.Query(`SELECT * FROM "` + tb + `"`)
			if err != nil {
				t.Fatal(err)
			}
		}
		cols, _ := r.Columns()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			m := map[string]any{}
			for i, c := range cols {
				if b, ok := vals[i].([]byte); ok {
					vals[i] = string(b)
				}
				m[c] = vals[i]
			}
			// A coluna virou_nota (0010, "Virar nota") nasce nula em todo ditado:
			// nula, não é mudança; preenchida, ela fica e a comparação a pega.
			if v, ok := m["virou_nota"]; ok && v == nil && tb == "local_transcriptions" {
				delete(m, "virou_nota")
			}
			out[tb] = append(out[tb], m)
		}
		r.Close()
		if _, ok := out[tb]; !ok {
			out[tb] = nil
		}
	}
	return out
}

// porID indexa as linhas de uma tabela pela coluna id.
func porID(linhas []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, l := range linhas {
		out[l["id"].(string)] = l
	}
	return out
}

func copiarArquivo(t *testing.T, de, para string) {
	t.Helper()
	a, err := os.Open(de)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := os.Create(para)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(b, a); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// pcSobre monta o Agent de teste sobre um historico.db que já existe (a
// cópia do fixture), com o dono dele.
func (n *nuvemE2E) pcSobre(caminho, dono string) *pcE2E {
	n.t.Helper()
	pc := &pcE2E{t: n.t, nuvem: n, caminho: caminho, dono: dono, cofre: chaves.NovaMemoria(), log: &contracttest.LockedBuffer{}}
	pc.abrir()
	return pc
}

// semMudar compara as tabelas de dados, fora as que o passo pode mudar.
func semMudar(t *testing.T, onde string, antes, depois map[string][]map[string]any, fora ...string) {
	t.Helper()
	pular := map[string]bool{}
	for _, f := range fora {
		pular[f] = true
	}
	for tb, linhas := range antes {
		if pular[tb] {
			continue
		}
		if !reflect.DeepEqual(linhas, depois[tb]) {
			t.Errorf("%s: a tabela %s mudou (%d linhas antes, %d depois)", onde, tb, len(linhas), len(depois[tb]))
		}
	}
}

func TestSyncE2E_HistoricoDeVerdadeNaoPerdeNemMudaNada(t *testing.T) {
	// O "bin\": o fixture montado numa pasta, e copiado byte a byte para a
	// pasta do PC (o Agent novo abre a cópia; o original fica de prova).
	fixture := filepath.Join(t.TempDir(), "historico.db")
	montarFixture(t, fixture)
	original := lerTabelas(t, fixture)
	ditados := porID(original["local_transcriptions"])
	if len(ditados) != 221 || len(original["instalacao"]) != 7 || len(original["local_settings"]) != 1 {
		t.Fatalf("o fixture: %d ditados, %d chaves, %d preferências", len(ditados), len(original["instalacao"]), len(original["local_settings"]))
	}
	pasta := t.TempDir()
	caminho := filepath.Join(pasta, "historico.db")
	copiarArquivo(t, fixture, caminho)

	n := novaNuvemE2E(t, server.Limites{})

	// 1. A subida do Agent novo: a 0006, a 0007 e a 0008. Nada muda, fora as colunas
	// novas da fila (vazia), as tabelas novas (vazias) e o registro das
	// migrations.
	pc := n.pcSobre(caminho, donoDoFixture)
	depois06 := lerTabelasDe(t, pc.db)
	semMudar(t, "a 0006, a 0007 e a 0008", original, depois06, "schema_migrations")
	// (e a 0009, das notas, e a 0010, do ditado que virou nota: o Migrate roda até a última)
	if m := depois06["schema_migrations"]; len(m) != 10 || m[6]["version"] != "0007_sync_aprendido" || m[7]["version"] != "0008_sync_aprendido_perdido" || m[8]["version"] != "0009_notas" || m[9]["version"] != "0010_ditado_virou_nota" {
		t.Errorf("as migrations: %v", m)
	}
	for _, tb := range []string{"sync_mudou_aqui", "sync_aprendido", "sync_veio_da_nuvem", "local_notes", "local_labels", "local_note_labels"} {
		if linhas, ok := depois06[tb]; !ok || len(linhas) != 0 {
			t.Errorf("a tabela nova %s: presente %v, %d linhas", tb, ok, len(linhas))
		}
	}

	// O que o envio inicial leva, pela regra (resposta 3 e D2): do dono, nos
	// 6 meses, fora da lixeira, sem a versão da nuvem, pendente e nascido
	// aqui.
	agora := time.Now()
	var elegiveis []string
	for id, l := range ditados {
		criado, _ := time.Parse("2006-01-02T15:04:05.000000000Z", l["created_at"].(string))
		if l["user_id"] == donoDoFixture && l["deleted_at"] == nil && l["device_id"] == nil &&
			agora.Before(criado.AddDate(0, 6, 0)) {
			elegiveis = append(elegiveis, id)
		}
	}
	sort.Strings(elegiveis)
	const textoGrande, noFuturo = "0199a000-0000-7000-8000-000000002002", "0199a000-0000-7000-8000-000000002003"
	if len(elegiveis) != 183 {
		t.Fatalf("o fixture tem %d elegíveis; quero 183", len(elegiveis))
	}

	// 2. A conexão (a mesma conta da nuvem de teste, como a F6 a deixa) e a
	// primeira rodada: nada do histórico sobe sem o clique.
	pc.conectarNa(n.conta, emailDoE2E, false)
	pc.rodadaOK()
	if got := n.tudoNaConta(pc.token); len(got) != 0 {
		t.Fatalf("sem o clique, subiram %d", len(got))
	}
	if h := pc.estado().Historico; h == nil || h.Previa != 183 || h.Aceito {
		t.Fatalf("a prévia: %+v", h)
	}
	antesDoEnvio := lerTabelasDe(t, pc.db)

	// 3. O envio: a cópia, os marcadores e as rodadas.
	pc.enviarHistorico()
	for i := 0; i < 3 && pc.naFila() > 0; i++ {
		pc.rodadaOK()
	}
	if pc.naFila() != 0 {
		t.Fatalf("a fila não esvaziou: %d", pc.naFila())
	}

	// A cópia: o banco inteiro de antes do envio, tabela por tabela.
	copias, _ := filepath.Glob(caminho + ".antes-da-nuvem-*.db")
	if len(copias) != 1 {
		t.Fatalf("as cópias: %v", copias)
	}
	semMudar(t, "a cópia", antesDoEnvio, lerTabelas(t, copias[0]))

	// A nuvem: exatamente os elegíveis, menos os dois que ela não aceita (o
	// texto acima do teto, que o PC nem manda, e a data no futuro), cada um
	// com o texto daqui, na versão 1.
	naNuvem := n.tudoNaConta(pc.token)
	var quero []string
	for _, id := range elegiveis {
		if id != textoGrande && id != noFuturo {
			quero = append(quero, id)
		}
	}
	if got := chavesDe(naNuvem); !reflect.DeepEqual(got, quero) {
		t.Fatalf("a nuvem tem %d; quero %d", len(got), len(quero))
	}
	for id, ch := range naNuvem {
		if ch.EntityVersion != 1 || ch.Payload.FinalText != ditados[id]["final_text"] || ch.Payload.RawText != ditados[id]["raw_text"] {
			t.Errorf("%s na nuvem: v%d %q", id, ch.EntityVersion, ch.Payload.FinalText)
		}
	}

	// O PC: cada ditado igual ao do fixture, coluna a coluna, fora as três do
	// sync (a versão da nuvem nos que subiram; a recusa nos dois). As outras
	// tabelas de dados, iguais; a instalação, só com as chaves da nuvem.
	depoisDoEnvio := lerTabelasDe(t, pc.db)
	semMudar(t, "o envio", original, depoisDoEnvio,
		"local_transcriptions", "instalacao", "schema_migrations", "sync_state", "sync_outbox")
	for id, l := range porID(depoisDoEnvio["local_transcriptions"]) {
		w := map[string]any{}
		for k, v := range ditados[id] {
			w[k] = v
		}
		switch {
		case naNuvem[id].EntityVersion == 1:
			w["server_version"] = int64(1)
		case id == textoGrande:
			w["sync_state"], w["last_sync_error"] = storage.SituacaoRecusado, syncengine.CodigoTextoGrande
		case id == noFuturo:
			w["sync_state"], w["last_sync_error"] = storage.SituacaoRecusado, "data_no_futuro"
		}
		if !reflect.DeepEqual(l, w) {
			t.Errorf("o ditado %s mudou além do sync:\nquero  %v\ntenho  %v", id, w, l)
		}
	}
	if h := pc.estado().Historico; h == nil || h.NaoSubiram != 2 || h.Envio != nil || h.Pendentes != 0 {
		t.Errorf("o resumo depois do envio: %+v", h)
	}
	for _, linha := range depoisDoEnvio["instalacao"] {
		nome := linha["nome"].(string)
		if !strings.HasPrefix(nome, "nuvem_") && !reflect.DeepEqual(linha, achar(original["instalacao"], nome)) {
			t.Errorf("a chave %s da instalação mudou: %v", nome, linha)
		}
	}

	// 4. O esquecer: o histórico volta a ser, linha a linha e coluna a
	// coluna, o do fixture (as colunas do sync também: pendente, sem versão,
	// sem erro). Ficam o dono, a cópia do dono, o install_id e as
	// preferências; a cópia do envio fica no disco.
	pc.esquecer()
	depoisDoEsquecer := lerTabelasDe(t, pc.db)
	semMudar(t, "o esquecer", original, depoisDoEsquecer, "instalacao", "schema_migrations")
	var chaves []string
	for _, linha := range depoisDoEsquecer["instalacao"] {
		chaves = append(chaves, linha["nome"].(string))
		if v := achar(original["instalacao"], linha["nome"].(string)); v != nil && !reflect.DeepEqual(v, linha) {
			t.Errorf("a chave %s mudou no esquecer", linha["nome"])
		}
	}
	sort.Strings(chaves)
	if got := strings.Join(chaves, ","); got != "dono_local,dono_local_copia,dono_local_migrado_em,install_id,nuvem_ditados_em_outra,nuvem_esquecida_em" {
		t.Errorf("a instalação depois do esquecer: %s", got)
	}
	if _, err := os.Stat(copias[0]); err != nil {
		t.Errorf("a cópia do envio sumiu: %v", err)
	}
	// E nenhum texto, id ou caminho no log do PC.
	for _, proibido := range []string{"ação", "Final 1", elegiveis[0], pasta, n.base} {
		if strings.Contains(pc.log.String(), proibido) {
			t.Errorf("o log do PC tem %q", proibido)
		}
	}
}

func achar(linhas []map[string]any, nome string) map[string]any {
	for _, l := range linhas {
		if l["nome"] == nome {
			return l
		}
	}
	return nil
}
