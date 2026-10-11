package pgstore

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/chavedeapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// As chaves da IA (ADR-021, C3a), sem Postgres: o SQL do pacote lê e grava só
// as colunas da 0017, e os tamanhos e a lista de provedores do CHECK são os do
// Go (o cofre, a chavedeapi e o cloudstore). Uma mudança de um lado sem o
// outro quebra aqui, e não no primeiro PUT em produção.
func TestSQL_AsChavesDaIAConferemComA0017(t *testing.T) {
	const arquivo = "0017_ai_provider_keys_table.sql"
	cols := createTableColumns(t, arquivo)
	lidas := listOf(colunasDaChaveDeIA)
	if len(lidas) != len(cols)-1 || cols["user_id"] != true {
		t.Errorf("a leitura tem %d colunas, a 0017 tem %d (com o user_id)", len(lidas), len(cols))
	}
	for _, c := range lidas {
		if !cols[c] {
			t.Errorf("colunasDaChaveDeIA lê %q, que não é coluna da 0017", c)
		}
	}
	m := regexp.MustCompile(`INSERT INTO ai_provider_keys \(([^)]*)\)`).FindStringSubmatch(sqlGravarChaveDeIA)
	if m == nil {
		t.Fatal("o INSERT da chave não foi achado")
	}
	if gravadas := listOf(m[1]); len(gravadas) != len(cols) {
		t.Errorf("o INSERT grava %v, a 0017 tem %d colunas", gravadas, len(cols))
	}

	raw, err := fs.ReadFile(migrations.FS, arquivo)
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	lista := regexp.MustCompile(`provider IN \(([^)]*)\)`).FindStringSubmatch(sql)
	if lista == nil {
		t.Fatal("a lista dos provedores não foi achada na 0017")
	}
	var naTabela []string
	for _, p := range strings.Split(lista[1], ",") {
		naTabela = append(naTabela, strings.Trim(strings.TrimSpace(p), "'"))
	}
	if !slices.Equal(naTabela, cloudstore.ProvedoresDeIA) {
		t.Errorf("a 0017 aceita %v, o cloudstore %v", naTabela, cloudstore.ProvedoresDeIA)
	}
	for _, c := range []struct {
		trecho string
		ok     bool
	}{
		{"octet_length(cipher_key_id) = 8", cofre.TamanhoDoID == 8 && cloudstore.TamanhoDoIDDaCifra == 8},
		{"octet_length(nonce) = 12", cofre.TamanhoDoNonce == 12 && cloudstore.TamanhoDoNonceDaChave == 12},
		{"octet_length(ciphertext) BETWEEN 17 AND 2576", cloudstore.MinCifradoDaChave == 1+cofre.TamanhoDaEtiqueta &&
			cloudstore.MaxCifradoDaChave == chavedeapi.TamanhoMaximo+cofre.TamanhoDaEtiqueta},
		{"key_suffix ~ '^[!-~]{0,4}$'", cloudstore.MaxFinalDaChave == chavedeapi.TamanhoMaximoDoFinal},
	} {
		if !strings.Contains(sql, c.trecho) || !c.ok {
			t.Errorf("a 0017 (%q) e o Go não conferem", c.trecho)
		}
	}
}
