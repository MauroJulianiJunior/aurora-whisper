package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O Ligar, o Desligar, a cópia e o freio dos 6 meses no storage (ADR-019,
// "Decisões da F10", F10c), num historico.db de verdade na pasta temporária,
// com o agora dado. O ciclo do Agent (o mutex, a rodada de hora em hora) está
// em internal/agent/expurgo e em cmd/agent (retencao_test.go).

// velhos grava n ditados do dono vencidos em agora (7 meses antes), a partir
// do id primeiro.
func (s seisMesesDeTeste) velhos(t *testing.T, primeiro, n int, agora time.Time) {
	t.Helper()
	var cs []time.Time
	for i := 0; i < n; i++ {
		cs = append(cs, agora.AddDate(0, -7, 0).Add(time.Duration(i)*time.Minute))
	}
	s.muitos(t, primeiro, cs)
}

func (s seisMesesDeTeste) chave(t *testing.T, nome string) (string, bool) {
	t.Helper()
	var v string
	err := s.db.QueryRow(`SELECT valor FROM instalacao WHERE nome = ?`, nome).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

// copias são os nomes das cópias dos 6 meses ao lado do banco.
func (s seisMesesDeTeste) copias(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Dir(s.caminho))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if strings.Contains(e.Name(), ".antes-dos-6-meses-") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (s seisMesesDeTeste) ligarComOK(t *testing.T, agora time.Time, previa int) storage.ResultadoDoLigar {
	t.Helper()
	r, err := s.sm.Ligar(context.Background(), agora, previa)
	if err != nil {
		t.Fatalf("Ligar: %v", err)
	}
	return r
}

// nadaMudou confere que o Ligar recusado não gravou nenhuma chave dos 6 meses
// e não apagou nada.
func (s seisMesesDeTeste) nadaMudou(t *testing.T, linhas int) {
	t.Helper()
	for _, c := range []string{storage.ChaveRetencaoAceitaEm, storage.ChaveRetencaoRodadaEm, storage.ChaveRetencaoPausadaEm} {
		if _, ok := s.chave(t, c); ok {
			t.Errorf("a chave %s foi gravada", c)
		}
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); n != linhas {
		t.Errorf("sobraram %d ditados; quero %d", n, linhas)
	}
}

// O Ligar: reconta, copia o banco (conferida tabela por tabela, com a marca
// no nome), grava o OK e a rodada e apaga exatamente o que recontou, com os
// lotes seguintes (600 vencidos: dois lotes) e o mesmo agora.
func TestLigar_ApagaORecontadoComACopiaAntes(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 600, agoraSM)
	s.ditado(t, idA, agoraSM.AddDate(0, -1, 0))
	r := s.ligarComOK(t, agoraSM, 600)
	if r.Previa != 600 || r.Apagados != 600 || r.JaLigada || r.CopiaJaExistia {
		t.Fatalf("o Ligar = %+v; quero 600 recontados e apagados, com cópia nova", r)
	}
	if !strings.Contains(r.Copia, ".antes-dos-6-meses-") || strings.ContainsAny(r.Copia, `/\`) {
		t.Fatalf("o nome da cópia = %q; quero só o nome, com a marca", r.Copia)
	}
	if got := s.copias(t); len(got) != 1 || got[0] != r.Copia {
		t.Fatalf("as cópias ao lado do banco: %v", got)
	}
	if v, _ := s.chave(t, storage.ChaveRetencaoCopia); v != r.Copia {
		t.Errorf("a cópia registrada = %q; quero %q", v, r.Copia)
	}
	for _, c := range []string{storage.ChaveRetencaoAceitaEm, storage.ChaveRetencaoRodadaEm} {
		if v, ok := s.chave(t, c); !ok || v != emTextoDeTeste(agoraSM) {
			t.Errorf("%s = %q %v; quero o agora do Ligar", c, v, ok)
		}
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); n != 1 || !s.tem(t, idA) {
		t.Fatalf("sobraram %d; quero só o de dentro da janela", n)
	}
	// A cópia tem o banco de antes: os 601, e as outras tabelas.
	c, err := storage.AbrirSomenteLeitura(filepath.Join(filepath.Dir(s.caminho), r.Copia))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var n int
	if err := c.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&n); err != nil || n != 601 {
		t.Fatalf("a cópia tem %d ditados, %v; quero os 601 de antes", n, err)
	}
}

// A cópia que falha (a pasta do banco não existe) deixa tudo como estava:
// nenhuma chave, nada apagado.
func TestLigar_ACopiaQueFalhaNaoApagaNemGrava(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 3, agoraSM)
	sm := storage.NovosSeisMeses(s.db, filepath.Join(t.TempDir(), "nao-existe", "historico.db"))
	_, err := sm.Ligar(context.Background(), agoraSM, 3)
	var ec *storage.ErroDaCopia
	if !errors.As(err, &ec) || !errors.Is(err, storage.ErrCopiaDosSeisMeses) || errors.Is(err, storage.ErrCopiaDoEnvio) {
		t.Fatalf("o Ligar com a cópia que falha = %v; quero a cópia dos 6 meses", err)
	}
	s.nadaMudou(t, 3)
	if _, ok := s.chave(t, storage.ChaveRetencaoCopia); ok {
		t.Error("a cópia que falhou foi registrada")
	}
}

// Com 3 cópias da marca ao lado do banco, nenhuma nova é feita: "já há 3
// cópias", nada apagado nem gravado. As de outra marca não contam.
func TestLigar_TresCopiasDaMarcaRecusam(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 3, agoraSM)
	for i := 1; i <= storage.MaxCopias; i++ {
		falsa := s.caminho + ".antes-dos-6-meses-2026100" + string(rune('0'+i)) + "-120000.db"
		if err := os.WriteFile(falsa, []byte("cópia"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.sm.Ligar(context.Background(), agoraSM, 3)
	if !errors.Is(err, storage.ErrCopiasDemais) || !errors.Is(err, storage.ErrCopiaDosSeisMeses) {
		t.Fatalf("o Ligar com 3 cópias = %v; quero cópias demais", err)
	}
	if !strings.Contains(err.Error(), "já há 3 cópia") {
		t.Errorf("o detalhe não diz quantas: %v", err)
	}
	s.nadaMudou(t, 3)
	if got := s.copias(t); len(got) != storage.MaxCopias {
		t.Fatalf("as cópias: %v", got)
	}
}

// O limite de 3 conta as cópias pela MESMA regra da tela e do botão
// (CopiasAoLado: arquivo regular com o nome exato). Três entradas com a marca
// que não são cópias (um nome fora do padrão, outro com sobra, uma pasta com o
// nome exato) não aparecem na tela nem saem pelo botão: não podem barrar o
// Ligar com "já há 3 cópias, apague as que não quiser". O Ligar copia, e elas
// ficam onde estão.
func TestLigar_OLimiteContaComARegraDaTela(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 3, agoraSM)
	pasta := filepath.Dir(s.caminho)
	base := filepath.Base(s.caminho)
	for _, nome := range []string{base + ".antes-dos-6-meses-backup.db", base + ".antes-dos-6-meses-20261001-120000-x.db"} {
		if err := os.WriteFile(filepath.Join(pasta, nome), []byte("não é cópia"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(pasta, base+".antes-dos-6-meses-20261002-120000.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	e, err := s.sm.Estado(context.Background(), agoraSM)
	if err != nil || e.Copias != 0 {
		t.Fatalf("a tela conta %d cópias (%v); quero 0: nenhuma das três é cópia", e.Copias, err)
	}

	r, err := s.sm.Ligar(context.Background(), agoraSM, 3)
	if err != nil || r.Apagados != 3 || r.Copia == "" {
		t.Fatalf("o Ligar = %+v, %v; quero ligado, com a cópia feita (as três não contam no limite)", r, err)
	}
	if e, _ = s.sm.Estado(context.Background(), agoraSM); e.Copias != 1 {
		t.Fatalf("a tela conta %d cópias; quero só a do Ligar", e.Copias)
	}
	for _, nome := range []string{base + ".antes-dos-6-meses-backup.db", base + ".antes-dos-6-meses-20261001-120000-x.db", base + ".antes-dos-6-meses-20261002-120000.db"} {
		if _, err := os.Stat(filepath.Join(pasta, nome)); err != nil {
			t.Errorf("%s sumiu: %v", nome, err)
		}
	}
}

// A contagem que a tela mostrou não é a de agora: *ErrPreviaMudou com o
// número novo, sem cópia, sem OK e sem apagar. Sem nada a apagar,
// ErrNadaAApagar, também sem cópia e sem OK.
func TestLigar_APreviaDiferenteEONadaAApagar(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	if _, err := s.sm.Ligar(context.Background(), agoraSM, 0); !errors.Is(err, storage.ErrNadaAApagar) {
		t.Fatalf("o Ligar sem nada a apagar = %v", err)
	}
	s.nadaMudou(t, 0)
	s.velhos(t, 0, 3, agoraSM)
	_, err := s.sm.Ligar(context.Background(), agoraSM, 2)
	var mudou *storage.ErrPreviaMudou
	if !errors.As(err, &mudou) || mudou.Previa != 3 {
		t.Fatalf("o Ligar com a prévia velha = %v; quero o número novo, 3", err)
	}
	s.nadaMudou(t, 3)
	if got := s.copias(t); len(got) != 0 {
		t.Fatalf("o Ligar recusado copiou: %v", got)
	}
}

// Sem o dono fixo, o Ligar recusa antes de tudo; já ligada, não faz nada.
func TestLigar_SemDonoFixoEJaLigada(t *testing.T) {
	caminho, db := bancoEmArquivo(t)
	sm := storage.NovosSeisMeses(db, caminho)
	if _, err := sm.Ligar(context.Background(), agoraSM, 1); !errors.Is(err, storage.ErrSemDonoFixo) {
		t.Fatalf("o Ligar sem o dono fixo = %v", err)
	}
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 2, agoraSM)
	s.ligarComOK(t, agoraSM, 2)
	s.velhos(t, 10, 2, agoraSM)
	if r := s.ligarComOK(t, agoraSM, 2); !r.JaLigada || r.Apagados != 0 {
		t.Fatalf("o Ligar já ligado = %+v; quero nada feito", r)
	}
	if got := s.copias(t); len(got) != 1 {
		t.Fatalf("as cópias: %v", got)
	}
}

// Três pausas do freio e três OKs de novo: a cópia é uma só (os OKs seguintes
// não copiam enquanto ela existe), e o OK fica gravado. Uma cópia por OK
// acumularia o texto inteiro a cada pausa, e o quarto OK falharia com "já há
// 3 cópias".
func TestLigar_TresPausasETresOKsUmaCopiaSo(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	agora := agoraSM
	s.velhos(t, 0, 2, agora)
	primeira := s.ligarComOK(t, agora, 2)
	for i := 1; i <= 3; i++ {
		agora = agora.AddDate(0, 0, 8)
		p, err := s.sm.Passo(context.Background(), agora)
		if err != nil || !p.Pausada {
			t.Fatalf("pausa %d: %+v, %v", i, p, err)
		}
		s.velhos(t, i*100, 2, agora)
		r := s.ligarComOK(t, agora, 2)
		if !r.CopiaJaExistia || r.Copia != "" || r.Apagados != 2 {
			t.Fatalf("OK %d depois da pausa = %+v; quero sem cópia nova", i, r)
		}
	}
	if got := s.copias(t); len(got) != 1 || got[0] != primeira.Copia {
		t.Fatalf("as cópias: %v; quero só a primeira", got)
	}
	if _, ok := s.chave(t, storage.ChaveRetencaoAceitaEm); !ok {
		t.Fatal("o OK não ficou gravado")
	}
	if _, ok := s.chave(t, storage.ChaveRetencaoPausadaEm); ok {
		t.Fatal("o OK de novo não tirou a pausa")
	}
}

// A cópia apagada à mão: o próximo OK copia de novo, e registra a nova.
func TestLigar_ACopiaApagadaAMaoCopiaDeNovo(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 2, agoraSM)
	a := s.ligarComOK(t, agoraSM, 2)
	if err := s.sm.Desligar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(s.caminho), a.Copia)); err != nil {
		t.Fatal(err)
	}
	depois := agoraSM.Add(time.Hour)
	s.velhos(t, 10, 2, depois)
	b := s.ligarComOK(t, depois, 2)
	if b.CopiaJaExistia || b.Copia == "" || b.Copia == a.Copia {
		t.Fatalf("o OK depois de a cópia sumir = %+v; quero uma cópia nova", b)
	}
	if v, _ := s.chave(t, storage.ChaveRetencaoCopia); v != b.Copia {
		t.Fatalf("a cópia registrada = %q; quero a nova, %q", v, b.Copia)
	}
}

// O Desligar tira o OK, a rodada e a pausa, e deixa a cópia; depois dele, o
// passo não apaga nada, nem com o relógio um ano à frente.
func TestDesligar_TiraOOKEFicaACopia(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 2, agoraSM)
	r := s.ligarComOK(t, agoraSM, 2)
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, 'x')`, storage.ChaveRetencaoPausadaEm)
	if err := s.sm.Desligar(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{storage.ChaveRetencaoAceitaEm, storage.ChaveRetencaoRodadaEm, storage.ChaveRetencaoPausadaEm} {
		if _, ok := s.chave(t, c); ok {
			t.Errorf("o Desligar deixou %s", c)
		}
	}
	if v, ok := s.chave(t, storage.ChaveRetencaoCopia); !ok || v != r.Copia {
		t.Errorf("o Desligar tirou a cópia registrada: %q %v", v, ok)
	}
	s.velhos(t, 10, 3, agoraSM)
	p, err := s.sm.Passo(context.Background(), agoraSM.AddDate(1, 0, 0))
	if err != nil || p.Ligada || p.Apagados != 0 || s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`) != 3 {
		t.Fatalf("o passo depois do Desligar = %+v, %v; quero nada", p, err)
	}
}

// O passo dos 6 meses: sem o OK, nada; com o OK e a rodada recente, apaga; com
// a rodada há mais de 7 dias, pausa (nada apagado, o OK e a rodada tirados, a
// pausa gravada, a contagem do que teria saído); sem rodada gravada, não
// pausa.
func TestPasso_OOKOFreioEARodada(t *testing.T) {
	ctx := context.Background()
	t.Run("sem o OK", func(t *testing.T) {
		s := novosSeisMesesDeTeste(t)
		s.velhos(t, 0, 3, agoraSM)
		if p, err := s.sm.Passo(ctx, agoraSM.AddDate(1, 0, 0)); err != nil || p != (storage.PassoDosSeisMeses{}) {
			t.Fatalf("o passo sem o OK = %+v, %v", p, err)
		}
		if n := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); n != 3 {
			t.Fatalf("sobraram %d", n)
		}
	})
	t.Run("com o OK e a rodada recente", func(t *testing.T) {
		s := novosSeisMesesDeTeste(t)
		s.velhos(t, 0, 1, agoraSM)
		s.ligarComOK(t, agoraSM, 1)
		amanha := agoraSM.AddDate(0, 0, 1)
		s.velhos(t, 10, 2, amanha)
		if err := s.sm.GravarRodada(ctx, amanha.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		p, err := s.sm.Passo(ctx, amanha)
		if err != nil || !p.Ligada || p.Pausada || p.Apagados != 2 {
			t.Fatalf("o passo = %+v, %v; quero 2 apagados", p, err)
		}
	})
	t.Run("com o OK e o freio", func(t *testing.T) {
		s := novosSeisMesesDeTeste(t)
		s.velhos(t, 0, 1, agoraSM)
		s.ligarComOK(t, agoraSM, 1)
		depois := agoraSM.AddDate(0, 0, 8)
		s.velhos(t, 10, 2, depois)
		p, err := s.sm.Passo(ctx, depois)
		if err != nil || !p.Pausada || p.Apagados != 0 || p.DiasParado != 8 || p.Previa != 2 {
			t.Fatalf("o passo com o freio = %+v, %v; quero a pausa, 8 dias parado, prévia 2", p, err)
		}
		if n := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); n != 2 {
			t.Fatalf("a pausa apagou: sobraram %d", n)
		}
		for c, quer := range map[string]bool{storage.ChaveRetencaoAceitaEm: false, storage.ChaveRetencaoRodadaEm: false, storage.ChaveRetencaoPausadaEm: true} {
			if _, ok := s.chave(t, c); ok != quer {
				t.Errorf("%s presente = %v; quero %v", c, ok, quer)
			}
		}
		// Pausada, a rodada seguinte não apaga (sem o OK).
		if p, err := s.sm.Passo(ctx, depois.Add(time.Hour)); err != nil || p.Ligada || p.Apagados != 0 {
			t.Fatalf("o passo depois da pausa = %+v, %v", p, err)
		}
	})
	t.Run("sem rodada gravada não pausa", func(t *testing.T) {
		s := novosSeisMesesDeTeste(t)
		s.velhos(t, 0, 1, agoraSM)
		s.ligarComOK(t, agoraSM, 1)
		executar(t, s.db, `DELETE FROM instalacao WHERE nome = ?`, storage.ChaveRetencaoRodadaEm)
		depois := agoraSM.AddDate(0, 0, 30)
		s.velhos(t, 10, 2, depois)
		p, err := s.sm.Passo(ctx, depois)
		if err != nil || p.Pausada || p.Apagados != 2 {
			t.Fatalf("o passo sem rodada = %+v, %v; quero 2 apagados, sem pausa", p, err)
		}
	})
	t.Run("sem o dono fixo", func(t *testing.T) {
		_, db := bancoEmArquivo(t)
		repo := storage.NewSQLiteTranscriptionRepository(db)
		for _, id := range []string{idA, idB} {
			if err := repo.Put(newTranscription(id, "mauro", agoraSM.AddDate(-1, 0, 0))); err != nil {
				t.Fatal(err)
			}
		}
		executar(t, db, `INSERT INTO instalacao (nome, valor) VALUES (?, 'x')`, storage.ChaveRetencaoAceitaEm)
		p, err := repo.SeisMeses("").Passo(ctx, agoraSM)
		if err != nil || !p.SemDonoFixo || p.Pulados != 2 || p.Apagados != 0 {
			t.Fatalf("o passo sem o dono fixo = %+v, %v; quero pular os 2", p, err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("sobraram %d, %v", n, err)
		}
	})
}
