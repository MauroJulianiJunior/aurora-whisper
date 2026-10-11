package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// bancoDoLigar é um historico.db migrado, com o dono fixo, na pasta
// temporária: o banco, o caminho e o dono.
func bancoDoLigar(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	ctx := context.Background()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	r, err := GarantirDonoFixo(ctx, db, "")
	if err != nil {
		t.Fatal(err)
	}
	return db, caminho, r.Dono
}

// O OK é gravado na MESMA transação do primeiro lote (ADR-019, "Decisões da
// F10", desvio 3): um gancho que derruba o primeiro lote leva o OK junto no
// rollback. A chave fica ausente, a rodada também, e nada é apagado.
func TestLigar_OPrimeiroLoteQueCaiLevaOOKJunto(t *testing.T) {
	ctx := context.Background()
	db, caminho, dono := bancoDoLigar(t)
	agora := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		criado := emTexto(agora.AddDate(0, -7, 0).Add(time.Duration(i) * time.Minute))
		if _, err := db.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at) VALUES (?, ?, 'windows', 'b', 'f', 'smart', 'local', 'done', ?, ?)`,
			fmt.Sprintf("0199e000-0000-7000-8000-%012d", i), dono, criado, criado); err != nil {
			t.Fatal(err)
		}
	}
	caiu := errors.New("o primeiro lote caiu")
	ganchoDoPrimeiroLote = func() error { return caiu }
	t.Cleanup(func() { ganchoDoPrimeiroLote = nil })

	if _, err := NovosSeisMeses(db, caminho).Ligar(ctx, agora, 3); !errors.Is(err, caiu) {
		t.Fatalf("o Ligar com o primeiro lote caído = %v", err)
	}
	for _, c := range []string{ChaveRetencaoAceitaEm, ChaveRetencaoRodadaEm} {
		if _, ok, err := lerChave(ctx, db, c); err != nil || ok {
			t.Errorf("%s ficou gravada (%v)", c, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("sobraram %d, %v; quero os 3", n, err)
	}
	// Sem o gancho, o clique de novo liga, sem outra cópia (a primeira ficou
	// registrada logo depois de conferida).
	ganchoDoPrimeiroLote = nil
	res, err := NovosSeisMeses(db, caminho).Ligar(ctx, agora.Add(time.Minute), 3)
	if err != nil || res.Apagados != 3 || !res.CopiaJaExistia {
		t.Fatalf("o Ligar de novo = %+v, %v; quero 3 apagados, com a cópia de antes", res, err)
	}
}

// O lote seguinte que falha depois do OK (o contexto do pedido cancelado, o
// banco ocupado): o OK e o primeiro lote já estão confirmados, então a regra
// está LIGADA e o Ligar não é uma recusa. Devolve ErrLigadoIncompleto com o
// resultado marcado (Incompleto, e os Apagados até ali), e o resto sai na
// rodada seguinte do ciclo.
func TestLigar_OLoteSeguinteQueFalhaDeixaLigadoEIncompleto(t *testing.T) {
	db, caminho, dono := bancoDoLigar(t)
	agora := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < LoteDosSeisMeses+100; i++ {
		criado := emTexto(agora.AddDate(0, -7, 0).Add(time.Duration(i) * time.Minute))
		if _, err := db.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at) VALUES (?, ?, 'windows', 'b', 'f', 'smart', 'local', 'done', ?, ?)`,
			fmt.Sprintf("0199e000-0000-7000-8000-%012d", i), dono, criado, criado); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	// O pedido cai logo depois de o OK ser confirmado: o lote seguinte já
	// nasce com o contexto cancelado.
	ganchoDepoisDoOK = cancelar
	t.Cleanup(func() { ganchoDepoisDoOK = nil })

	r, err := NovosSeisMeses(db, caminho).Ligar(ctx, agora, LoteDosSeisMeses+100)
	if !errors.Is(err, ErrLigadoIncompleto) || !errors.Is(err, context.Canceled) {
		t.Fatalf("o Ligar com o lote seguinte caído = %v; quero ErrLigadoIncompleto com a causa", err)
	}
	if !r.Incompleto || r.Apagados != LoteDosSeisMeses || r.Previa != LoteDosSeisMeses+100 || r.Copia == "" {
		t.Fatalf("o resultado = %+v; quero incompleto, com o primeiro lote apagado e a cópia feita", r)
	}
	if _, ok, err := lerChave(context.Background(), db, ChaveRetencaoAceitaEm); err != nil || !ok {
		t.Fatalf("o OK não ficou gravado (%v): a regra tem de estar ligada", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&n); err != nil || n != 100 {
		t.Fatalf("sobraram %d, %v; quero os 100 do lote seguinte", n, err)
	}
	// A rodada seguinte do ciclo leva o resto.
	ganchoDepoisDoOK = nil
	if apagados, err := NovosSeisMeses(db, caminho).ApagarForaDaJanela(context.Background(), agora.Add(time.Hour)); err != nil || apagados != 100 {
		t.Fatalf("a rodada seguinte apagou %d, %v; quero os 100", apagados, err)
	}
}
