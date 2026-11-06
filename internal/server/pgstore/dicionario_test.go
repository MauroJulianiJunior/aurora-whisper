package pgstore

import (
	"strings"
	"testing"
)

// As consultas da cópia do dicionário (ADR-023), lidas contra a 0027 sem
// Postgres: só o PC que não foi revogado é a fonte que se lê; os bytes só saem
// pela leitura que os pede; a trava da cópia nunca toca a linha do aparelho; e as
// colunas existem na migration. O comportamento é o da suíte de contrato (os dois
// Stores) e dos testes com Postgres (dicionario_pg_test.go).
func TestSQL_AsConsultasDaCopiaDoDicionario(t *testing.T) {
	for nome, c := range map[string]struct {
		sql   string
		frags []string
		nunca []string
	}{
		"o estado": {sqlCopiaInfo,
			[]string{"FROM dictionary_copies c", "d.revoked_at IS NULL", "WHERE c.user_id = $1"}, []string{"content"}},
		"o estado com os bytes": {sqlCopiaComBytes,
			[]string{"c.content", "d.revoked_at IS NULL", "WHERE c.user_id = $1"}, nil},
		"o aparelho que grava": {sqlCopiaAparelho,
			[]string{"revoked_at IS NULL", "FOR SHARE"}, []string{"FOR UPDATE"}},
		"a cópia travada": {sqlCopiaTravada,
			[]string{"FOR UPDATE OF c", "WHERE c.user_id = $1"}, []string{"content"}},
		"gravar": {sqlCopiaGravar,
			[]string{"INSERT INTO dictionary_copies (user_id, device_id, content, sha256, terms, updated_at)", "ON CONFLICT (user_id) DO UPDATE"}, nil},
		"apagar": {sqlCopiaApagar,
			[]string{"DELETE FROM dictionary_copies WHERE user_id = $1 AND device_id = $2"}, nil},
		"apagar do aparelho revogado": {sqlCopiaApagarDoAparelho,
			[]string{"DELETE FROM dictionary_copies WHERE user_id = $1 AND device_id = $2"}, nil},
	} {
		for _, f := range c.frags {
			if !strings.Contains(c.sql, f) {
				t.Errorf("%s: sem %q:\n%s", nome, f, c.sql)
			}
		}
		for _, f := range c.nunca {
			if strings.Contains(c.sql, f) {
				t.Errorf("%s: com %q:\n%s", nome, f, c.sql)
			}
		}
	}
	cols := createTableColumns(t, "0027_dictionary_copies_table.sql")
	for _, col := range []string{"user_id", "device_id", "content", "sha256", "terms", "updated_at"} {
		if !cols[col] {
			t.Errorf("a coluna %s não existe na 0027 (%v)", col, cols)
		}
	}
	if len(cols) != 6 {
		t.Errorf("a 0027 tem %d colunas, o SQL usa 6", len(cols))
	}
}

// O "apagar meus dados" apaga a cópia antes dos aparelhos (a chave estrangeira
// não tem cascata), e trava os aparelhos antes de apagar qualquer coisa (a ordem
// das travas é a do PUT e a da revogação).
func TestSQL_ApagarMeusDadosApagaACopiaAntesDosAparelhos(t *testing.T) {
	copia, aparelhos := -1, -1
	for i, passo := range apagarDaConta {
		switch {
		case strings.HasPrefix(passo.sql, "DELETE FROM dictionary_copies WHERE user_id = $1"):
			copia = i
		case strings.HasPrefix(passo.sql, "DELETE FROM devices WHERE user_id = $1"):
			aparelhos = i
		}
	}
	if copia < 0 || aparelhos < 0 || copia > aparelhos {
		t.Errorf("a cópia está no passo %d e os aparelhos no %d; quero a cópia antes", copia, aparelhos)
	}
	if !strings.Contains(sqlTravarAparelhosDaConta, "FROM devices WHERE user_id = $1 FOR UPDATE") {
		t.Errorf("a trava dos aparelhos do apagar: %s", sqlTravarAparelhosDaConta)
	}
}
