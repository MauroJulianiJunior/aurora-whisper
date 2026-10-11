package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
	_ "time/tzdata" // America/Sao_Paulo mesmo numa máquina sem a base de fusos

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os 6 meses no storage, atrás do portão (ADR-019, "Decisões da F10", F10b),
// num historico.db de verdade na pasta temporária, com o dono fixo gravado e
// o agora dado a cada chamada. Nada aqui liga a regra no Agent (F10c): o OK é
// gravado direto na tabela instalacao, como o Ligar o deixaria.

type seisMesesDeTeste struct {
	db      *sql.DB
	repo    *storage.SQLiteTranscriptionRepository
	sm      *storage.SeisMeses
	dono    string
	caminho string
}

func novosSeisMesesDeTeste(t *testing.T) seisMesesDeTeste {
	t.Helper()
	caminho, db := bancoEmArquivo(t)
	// Banco vazio: o dono fixo nasce sem cópia.
	r, err := storage.GarantirDonoFixo(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSQLiteTranscriptionRepository(db)
	return seisMesesDeTeste{db: db, repo: repo, sm: repo.SeisMeses(caminho), dono: r.Dono, caminho: caminho}
}

func idSM(n int) string { return fmt.Sprintf("0199d000-0000-7000-8000-%012d", n) }

// ditado grava um ditado do dono, criado em criado.
func (s seisMesesDeTeste) ditado(t *testing.T, id string, criado time.Time) {
	t.Helper()
	if err := s.repo.Put(newTranscription(id, s.dono, criado)); err != nil {
		t.Fatal(err)
	}
}

// muitos grava, numa transação só, um ditado do dono para cada instante.
func (s seisMesesDeTeste) muitos(t *testing.T, primeiro int, criados []time.Time) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for i, c := range criados {
		em := emTextoDeTeste(c)
		if _, err := tx.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at) VALUES (?, ?, 'windows', 'bruto', 'final', 'smart', 'local', 'done', ?, ?)`,
			idSM(primeiro+i), s.dono, em, em); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// ligar grava o OK, como o Ligar o deixa.
func (s seisMesesDeTeste) ligar(t *testing.T) {
	t.Helper()
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveRetencaoAceitaEm, "x")
}

func (s seisMesesDeTeste) tem(t *testing.T, id string) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func (s seisMesesDeTeste) contar(t *testing.T, sqlDeContar string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(sqlDeContar, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (s seisMesesDeTeste) previa(t *testing.T, agora time.Time) storage.ContagemDosSeisMeses {
	t.Helper()
	c, err := s.sm.PreviaDosSeisMeses(context.Background(), agora)
	if err != nil {
		t.Fatalf("PreviaDosSeisMeses: %v", err)
	}
	return c
}

func (s seisMesesDeTeste) apagar(t *testing.T, agora time.Time) int {
	t.Helper()
	n, err := s.sm.ApagarForaDaJanela(context.Background(), agora)
	if err != nil {
		t.Fatalf("ApagarForaDaJanela: %v", err)
	}
	return n
}

var agoraSM = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// O portão recusa por padrão: sem o OK gravado, nada sai, nem com o relógio 2
// anos à frente; a prévia conta do mesmo jeito. É o teste que mata a mutação
// "o método ignora a chave".
func TestSeisMeses_SemOOKNadaSai(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	for i, meses := range []int{7, 12, 24} {
		s.ditado(t, idSM(i), agoraSM.AddDate(0, -meses, 0))
	}
	longe := agoraSM.AddDate(2, 0, 0)
	if c := s.previa(t, longe); c.Total != 3 {
		t.Fatalf("a prévia com o relógio 2 anos à frente = %+v; quero 3", c)
	}
	if n := s.apagar(t, longe); n != 0 {
		t.Fatalf("sem o OK, apagou %d", n)
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); n != 3 {
		t.Fatalf("sem o OK, sobraram %d de 3", n)
	}
}

// Com o OK, apaga exatamente o que a prévia contou, com o mesmo agora; e a
// segunda rodada apaga zero.
func TestSeisMeses_ComOOKApagaOQueAPreviaContou(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	velhos := []string{idSM(1), idSM(2), idSM(3)}
	for i, id := range velhos {
		s.ditado(t, id, agoraSM.AddDate(0, -6-i, -1))
	}
	novos := []string{idSM(10), idSM(11)}
	s.ditado(t, novos[0], agoraSM.AddDate(0, -5, 0))
	s.ditado(t, novos[1], agoraSM.AddDate(0, -6, 1))
	s.ligar(t)
	c := s.previa(t, agoraSM)
	if c.Total != len(velhos) {
		t.Fatalf("a prévia = %+v; quero %d", c, len(velhos))
	}
	if n := s.apagar(t, agoraSM); n != c.Total {
		t.Fatalf("apagou %d; a prévia contou %d", n, c.Total)
	}
	for _, id := range velhos {
		if s.tem(t, id) {
			t.Errorf("%s, fora da janela, ficou", id)
		}
	}
	for _, id := range novos {
		if !s.tem(t, id) {
			t.Errorf("%s, dentro da janela, saiu", id)
		}
	}
	if n := s.apagar(t, agoraSM); n != 0 {
		t.Fatalf("a segunda rodada apagou %d", n)
	}
	if c := s.previa(t, agoraSM); c.Total != 0 {
		t.Fatalf("a prévia depois = %+v", c)
	}
}

// As bordas, linha a linha e em UTC: 31/ago vence em 3/mar (o AddDate), o
// 29/fev do bissexto, e a hora exata (sai) contra 1 ns antes (fica).
func TestSeisMeses_AsBordas(t *testing.T) {
	utc := func(a int, m time.Month, d, h int) time.Time { return time.Date(a, m, d, h, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		nome          string
		criado, vence time.Time
	}{
		{"31/ago vence em 3/mar", utc(2026, time.August, 31, 12), utc(2027, time.March, 3, 12)},
		{"29/ago vence em 29/fev do bissexto", utc(2027, time.August, 29, 12), utc(2028, time.February, 29, 12)},
		{"31/ago vence em 2/mar do bissexto", utc(2027, time.August, 31, 12), utc(2028, time.March, 2, 12)},
		{"29/fev vence em 29/ago", utc(2028, time.February, 29, 12), utc(2028, time.August, 29, 12)},
		{"1/set vence em 1/mar", utc(2026, time.September, 1, 0), utc(2027, time.March, 1, 0)},
	} {
		t.Run(c.nome, func(t *testing.T) {
			if v := retention.ActiveExpiryAt(c.criado); !v.Equal(c.vence) {
				t.Fatalf("ActiveExpiryAt(%s) = %s; o caso diz %s", c.criado, v, c.vence)
			}
			s := novosSeisMesesDeTeste(t)
			s.ditado(t, idA, c.criado)
			s.ligar(t)
			antes := c.vence.Add(-time.Nanosecond)
			if p := s.previa(t, antes); p.Total != 0 {
				t.Fatalf("1 ns antes, a prévia = %+v", p)
			}
			if n := s.apagar(t, antes); n != 0 || !s.tem(t, idA) {
				t.Fatalf("1 ns antes, apagou %d", n)
			}
			if p := s.previa(t, c.vence); p.Total != 1 {
				t.Fatalf("na hora exata, a prévia = %+v", p)
			}
			if n := s.apagar(t, c.vence); n != 1 || s.tem(t, idA) {
				t.Fatalf("na hora exata, apagou %d", n)
			}
		})
	}
}

// O relógio do Agent é o do sistema, em hora local: com o time.Local em
// America/Sao_Paulo (agora = 2027-03-01T01:00Z, 28/fev 22:00 local) e num fuso
// positivo (agora = 2027-03-31T12:00Z, 1/abr 02:00 local), o ditado vencido
// perto da virada entra na prévia e sai, e o vizinho que não venceu fica. Com
// o corte contado no fuso local, o vencido ficaria de fora por dias.
func TestSeisMeses_ComOFusoLocal(t *testing.T) {
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		fuso             *time.Location
		agora            time.Time
		vencido, vizinho time.Time
	}{
		{sp, time.Date(2027, 3, 1, 1, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 1, 0, 30, 0, 0, time.UTC), time.Date(2026, 9, 1, 1, 30, 0, 0, time.UTC)},
		{time.FixedZone("UTC+14", 14*3600), time.Date(2027, 3, 31, 12, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)},
	} {
		t.Run(c.fuso.String(), func(t *testing.T) {
			antes := time.Local
			time.Local = c.fuso
			t.Cleanup(func() { time.Local = antes })
			agora := c.agora.In(time.Local)
			if errado := agora.AddDate(0, -retention.ActiveRetentionMonths, 0); !c.vencido.After(errado) {
				t.Fatalf("o cenário não exercita o fuso: o corte local %s já traria o vencido", errado.UTC())
			}
			s := novosSeisMesesDeTeste(t)
			s.ditado(t, idA, c.vencido)
			s.ditado(t, idB, c.vizinho)
			s.ligar(t)
			if p := s.previa(t, agora); p.Total != 1 {
				t.Fatalf("a prévia = %+v; quero o vencido", p)
			}
			if n := s.apagar(t, agora); n != 1 || s.tem(t, idA) || !s.tem(t, idB) {
				t.Fatalf("apagou %d; o vencido ficou %v, o vizinho ficou %v", n, s.tem(t, idA), s.tem(t, idB))
			}
		})
	}
}

// O cursor anda pelo último LIDO, e o laço só termina com uma leitura curta.
// Em 1/mar, o pré-filtro traz de propósito os ditados de 29 a 31/ago, que não
// venceram: com 501 deles e 1 vencido (28/ago), só o vencido sai, a rodada
// termina e o apagado é igual à prévia. Um cursor parado (ou pelo último
// apagado) releria os 500 para sempre: o contexto com prazo transforma isso
// em erro. A segunda parte põe um vencido na última linha de um lote cheio
// (o 1/set antes do meio-dia vence em 1/mar, antes do 29/ago): um cursor com
// >= o contaria de novo, e a prévia passaria do apagado.
func TestSeisMeses_OCursorAndaPeloUltimoLido(t *testing.T) {
	agora := time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)
	naoVenceram := func(n int) []time.Time {
		var cs []time.Time
		for i := 0; i < n; i++ {
			c := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC).Add(time.Duration(i) * 5 * time.Minute)
			if !agora.Before(retention.ActiveExpiryAt(c)) {
				t.Fatalf("o %dº não-vencido venceu: %s", i, c)
			}
			cs = append(cs, c)
		}
		return cs
	}
	for _, c := range []struct {
		nome     string
		criados  []time.Time
		vencidos []int // os índices dos vencidos em criados
	}{
		{"501 que não venceram e 1 vencido",
			append([]time.Time{time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}, naoVenceram(501)...), []int{0}},
		{"o vencido na última linha de um lote cheio",
			append(append([]time.Time{time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}, naoVenceram(498)...),
				time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)), []int{0, 499}},
	} {
		t.Run(c.nome, func(t *testing.T) {
			s := novosSeisMesesDeTeste(t)
			s.muitos(t, 0, c.criados)
			s.ligar(t)
			ctx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancelar()
			p, err := s.sm.PreviaDosSeisMeses(ctx, agora)
			if err != nil {
				t.Fatalf("a prévia não terminou: %v", err)
			}
			if p.Total != len(c.vencidos) {
				t.Fatalf("a prévia = %+v; quero %d", p, len(c.vencidos))
			}
			n, err := s.sm.ApagarForaDaJanela(ctx, agora)
			if err != nil {
				t.Fatalf("a rodada não terminou: %v", err)
			}
			if n != p.Total {
				t.Fatalf("apagou %d; a prévia contou %d", n, p.Total)
			}
			for _, i := range c.vencidos {
				if s.tem(t, idSM(i)) {
					t.Errorf("o vencido %d ficou", i)
				}
			}
			if sobra := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); sobra != len(c.criados)-len(c.vencidos) {
				t.Fatalf("sobraram %d; quero %d", sobra, len(c.criados)-len(c.vencidos))
			}
		})
	}
}

// O cursor desempata pelo id quando o created_at empata: com mais de dois
// lotes de vencidos no MESMO instante (o import, ou o relógio que repete o
// milissegundo), a fronteira de cada lote cai dentro do empate. Um cursor só
// pelo created_at (created_at > ?) pularia o resto do empate depois do
// primeiro lote: a prévia contaria 500 e o apagar deixaria vencidos para
// trás, em silêncio. Um cursor com id >= ? releria a última linha de cada
// lote, e a prévia passaria do total. O vencido depois do empate e o
// não-vencido conferem que o cursor continua dali, e só dali.
func TestSeisMeses_OCursorDesempataPeloIdNoEmpateDeCreatedAt(t *testing.T) {
	agora := time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)
	empate := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	n := 2*storage.LoteDosSeisMeses + 37
	criados := make([]time.Time, 0, n+2)
	for i := 0; i < n; i++ {
		criados = append(criados, empate)
	}
	depoisDoEmpate := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) // venceu
	naoVenceu := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)       // fica
	criados = append(criados, depoisDoEmpate, naoVenceu)
	for _, c := range []time.Time{empate, depoisDoEmpate} {
		if agora.Before(retention.ActiveExpiryAt(c)) {
			t.Fatalf("o cenário errou: %s não venceu em %s", c, agora)
		}
	}
	if !agora.Before(retention.ActiveExpiryAt(naoVenceu)) {
		t.Fatalf("o cenário errou: %s venceu em %s", naoVenceu, agora)
	}
	s := novosSeisMesesDeTeste(t)
	s.muitos(t, 0, criados)
	s.ligar(t)
	ctx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelar()
	p, err := s.sm.PreviaDosSeisMeses(ctx, agora)
	if err != nil {
		t.Fatalf("a prévia não terminou: %v", err)
	}
	if p.Total != n+1 {
		t.Fatalf("a prévia = %d; quero %d (%d no empate e 1 depois)", p.Total, n+1, n)
	}
	apagados, err := s.sm.ApagarForaDaJanela(ctx, agora)
	if err != nil {
		t.Fatalf("a rodada não terminou: %v", err)
	}
	if apagados != p.Total {
		t.Fatalf("apagou %d; a prévia contou %d", apagados, p.Total)
	}
	if sobra := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE created_at = ?`, emTextoDeTeste(empate)); sobra != 0 {
		t.Fatalf("sobraram %d do empate", sobra)
	}
	if s.tem(t, idSM(n)) {
		t.Error("o vencido depois do empate ficou")
	}
	if !s.tem(t, idSM(n+1)) {
		t.Error("o que não venceu saiu")
	}
	if c := s.previa(t, agora); c.Total != 0 {
		t.Fatalf("a prévia depois = %+v", c)
	}
}

// O que sai com o ditado e o que fica: a lixeira fora da janela sai; o ativo
// dentro dela fica; a linha de outro dono fica, e a prévia a conta; o
// pendente (com o marcador) e o que tem o selo saem, com o marcador e a
// anotação; o local_corrections vira nulo; o aprendido (a sync_aprendido, a
// sync_veio_da_nuvem e o marcador learned) fica. A segunda rodada apaga zero.
func TestSeisMeses_OQueSaiEOQueFica(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	velho := agoraSM.AddDate(0, -7, 0)
	s.ditado(t, idA, velho) // vai para a lixeira
	if err := s.repo.SoftDelete(context.Background(), s.dono, idA, agoraSM.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	s.ditado(t, idB, agoraSM.AddDate(0, -5, 0)) // ativo, dentro da janela
	s.ditado(t, idC, velho)                     // pendente: o marcador na fila e a anotação
	executar(t, s.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('transcription', ?, 1, ?)`,
		idC, emTextoDeTeste(agoraSM))
	executar(t, s.db, `INSERT INTO sync_mudou_aqui (entity_type, entity_id) VALUES ('transcription', ?)`, idC)
	s.ditado(t, idD, velho) // com o selo
	executar(t, s.db, `UPDATE local_transcriptions SET conflito_texto = 'a outra', conflito_em = ? WHERE id = ?`,
		emTextoDeTeste(agoraSM), idD)
	outro := newTranscription(idE, "mauro", velho) // de outro dono: fica
	if err := s.repo.Put(outro); err != nil {
		t.Fatal(err)
	}
	// A correção aprendida do ditado A, e o aprendido do sync, com o mesmo
	// id do C de propósito.
	executar(t, s.db, `INSERT INTO local_corrections (id, user_id, transcription_id, from_text, to_text, normalized_from,
		normalized_to, created_at, updated_at) VALUES ('c1', ?, ?, 'a', 'b', 'a', 'b', 'x', 'x')`, s.dono, idA)
	executar(t, s.db, `INSERT INTO sync_aprendido (user_id, entity_id) VALUES (?, ?)`, s.dono, idC)
	executar(t, s.db, `INSERT INTO sync_veio_da_nuvem (user_id, entity_id) VALUES (?, ?)`, s.dono, idC)
	executar(t, s.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, 0, 'x')`,
		storage.EntidadeAprendido, idC)
	s.ligar(t)

	c := s.previa(t, agoraSM)
	if c != (storage.ContagemDosSeisMeses{Total: 3, NaLixeira: 1, ComSelo: 1, DeOutroDono: 1}) {
		t.Fatalf("a prévia = %+v; quero 3 (1 na lixeira, 1 com o selo) e 1 de outro dono", c)
	}
	if n := s.apagar(t, agoraSM); n != 3 {
		t.Fatalf("apagou %d; quero 3", n)
	}
	for id, fica := range map[string]bool{idA: false, idB: true, idC: false, idD: false, idE: true} {
		if s.tem(t, id) != fica {
			t.Errorf("%s: ficou %v; quero %v", id, s.tem(t, id), fica)
		}
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'transcription'`); n != 0 {
		t.Errorf("o marcador do pendente ficou: %d", n)
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM sync_mudou_aqui`); n != 0 {
		t.Errorf("a anotação do pendente ficou: %d", n)
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM local_corrections WHERE id = 'c1' AND transcription_id IS NULL`); n != 1 {
		t.Errorf("a correção do ditado apagado não virou nula (ou sumiu)")
	}
	for tabela, quer := range map[string]int{
		`SELECT COUNT(*) FROM sync_aprendido`:                               1,
		`SELECT COUNT(*) FROM sync_veio_da_nuvem`:                           1,
		`SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'learned'`:    1,
		`SELECT COUNT(*) FROM local_transcriptions WHERE user_id = 'mauro'`: 1,
	} {
		if n := s.contar(t, tabela); n != quer {
			t.Errorf("%s = %d; quero %d", tabela, n, quer)
		}
	}
	if n := s.apagar(t, agoraSM); n != 0 {
		t.Fatalf("a segunda rodada apagou %d", n)
	}
	if c := s.previa(t, agoraSM); c != (storage.ContagemDosSeisMeses{DeOutroDono: 1}) {
		t.Fatalf("a prévia depois = %+v; quero só a linha de outro dono", c)
	}
}

// Sem o dono fixo (o banco de antes dele, ou a migração que não rodou), nada
// roda: a prévia e o apagar recusam, e nada sai.
func TestSeisMeses_SemODonoFixoNadaRoda(t *testing.T) {
	_, db := bancoEmArquivo(t)
	repo := storage.NewSQLiteTranscriptionRepository(db)
	if err := repo.Put(newTranscription(idA, "mauro", agoraSM.AddDate(-1, 0, 0))); err != nil {
		t.Fatal(err)
	}
	executar(t, db, `INSERT INTO instalacao (nome, valor) VALUES (?, 'x')`, storage.ChaveRetencaoAceitaEm)
	sm := repo.SeisMeses("")
	if _, err := sm.PreviaDosSeisMeses(context.Background(), agoraSM); err == nil {
		t.Error("a prévia sem o dono fixo não recusou")
	}
	if n, err := sm.ApagarForaDaJanela(context.Background(), agoraSM); err == nil || n != 0 {
		t.Errorf("o apagar sem o dono fixo = %d, %v; quero zero e a recusa", n, err)
	}
	var sobra int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&sobra); err != nil || sobra != 1 {
		t.Fatalf("sobraram %d, %v", sobra, err)
	}
}
