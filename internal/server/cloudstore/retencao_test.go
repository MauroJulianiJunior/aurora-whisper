package cloudstore

import (
	"testing"
	"time"
)

// As regras puras da retenção na nuvem (ADR-019, "Decisões da F9", F9a): a
// fronteira exata e 1 µs antes, o fim do mês, o 29/fev, a lixeira, a
// sobreposição, o relógio que volta, o pré-filtro que nunca deixa um vencido
// de fora, o fuso e os prazos da arrumação.

const us = time.Microsecond

func utcAt(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

func tp(t time.Time) *time.Time { return &t }

func TestDecideRetencao_Tabela(t *testing.T) {
	ago31 := utcAt(2026, time.August, 31, 12)
	// 31/ago + 6 meses: o 31/fev de 2027 (não bissexto) vira 3/mar.
	venceAgo31 := utcAt(2027, time.March, 3, 12)
	cases := []struct {
		name    string
		created time.Time
		deleted *time.Time
		now     time.Time
		want    Retencao
	}{
		// O ativo: sai aos 6 meses exatos, sem lápide.
		{"ativo: 1 µs antes dos 6 meses fica", now0r, nil, now0r.AddDate(0, 6, 0).Add(-us), RetencaoFica},
		{"ativo: nos 6 meses sai sem lápide", now0r, nil, now0r.AddDate(0, 6, 0), RetencaoSemLapide},
		{"ativo: depois dos 6 meses e da folga, sem lápide também", now0r, nil, now0r.AddDate(0, 7, 0), RetencaoSemLapide},
		{"ativo: um ano depois", now0r, nil, now0r.AddDate(1, 0, 0), RetencaoSemLapide},
		// O fim do mês: o 31/ago vence em 3/mar, e não em 28/fev nem em 1/mar
		// (o "now menos 6 meses" do Job antigo apagava dois dias antes).
		{"31/ago: em 28/fev fica", ago31, nil, utcAt(2027, time.February, 28, 12), RetencaoFica},
		{"31/ago: em 1/mar fica", ago31, nil, utcAt(2027, time.March, 1, 12), RetencaoFica},
		{"31/ago: em 2/mar fica", ago31, nil, utcAt(2027, time.March, 2, 12), RetencaoFica},
		{"31/ago: 1 µs antes de 3/mar fica", ago31, nil, venceAgo31.Add(-us), RetencaoFica},
		{"31/ago: em 3/mar sai", ago31, nil, venceAgo31, RetencaoSemLapide},
		// O 29/fev: o ano bissexto de 2028.
		{"29/ago/2027: vence em 29/fev/2028, 1 µs antes fica", utcAt(2027, time.August, 29, 12), nil, utcAt(2028, time.February, 29, 12).Add(-us), RetencaoFica},
		{"29/ago/2027: vence em 29/fev/2028", utcAt(2027, time.August, 29, 12), nil, utcAt(2028, time.February, 29, 12), RetencaoSemLapide},
		{"30/ago/2027: o 30/fev vira 1/mar", utcAt(2027, time.August, 30, 12), nil, utcAt(2028, time.February, 29, 23), RetencaoFica},
		{"30/ago/2027: sai em 1/mar", utcAt(2027, time.August, 30, 12), nil, utcAt(2028, time.March, 1, 12), RetencaoSemLapide},
		{"29/fev/2028: vence em 29/ago, 1 µs antes fica", utcAt(2028, time.February, 29, 12), nil, utcAt(2028, time.August, 29, 12).Add(-us), RetencaoFica},
		{"29/fev/2028: vence em 29/ago", utcAt(2028, time.February, 29, 12), nil, utcAt(2028, time.August, 29, 12), RetencaoSemLapide},

		// A lixeira: 30 dias depois de ir para lá (D+29 fica, D+30 sai), com
		// lápide, porque o ditado ainda está dentro da janela.
		{"lixeira: em D+29 fica", now0r, tp(now0r.Add(time.Hour)), now0r.Add(time.Hour).AddDate(0, 0, 29), RetencaoFica},
		{"lixeira: 1 µs antes de D+30 fica", now0r, tp(now0r.Add(time.Hour)), now0r.Add(time.Hour).AddDate(0, 0, 30).Add(-us), RetencaoFica},
		{"lixeira: em D+30 sai com lápide", now0r, tp(now0r.Add(time.Hour)), now0r.Add(time.Hour).AddDate(0, 0, 30), RetencaoComLapide},
		{"lixeira: muito depois, ainda na janela, com lápide", now0r, tp(now0r), now0r.AddDate(0, 5, 0), RetencaoComLapide},
		// A sobreposição: posto na lixeira perto dos 6 meses, vence com eles
		// (não ganha os 30 dias), com lápide até a janela mais a folga, e
		// sem ela depois.
		{"sobreposição: 1 µs antes dos 6 meses fica", now0r, tp(now0r.AddDate(0, 5, 25)), now0r.AddDate(0, 6, 0).Add(-us), RetencaoFica},
		{"sobreposição: nos 6 meses sai com lápide", now0r, tp(now0r.AddDate(0, 5, 25)), now0r.AddDate(0, 6, 0), RetencaoComLapide},
		{"sobreposição: 1 µs antes da folga, com lápide", now0r, tp(now0r.AddDate(0, 5, 25)), now0r.AddDate(0, 6, 0).Add(FolgaDaLapide - us), RetencaoComLapide},
		{"sobreposição: vencida há 24 h, sem lápide", now0r, tp(now0r.AddDate(0, 5, 25)), now0r.AddDate(0, 6, 0).Add(FolgaDaLapide), RetencaoSemLapide},
		{"lixeira de um ditado fora da janela há dias, sem lápide", now0r, tp(now0r), now0r.AddDate(0, 8, 0), RetencaoSemLapide},
		// O 31/ago na lixeira: a lápide nasce até 4/mar 12:00 (exclusive).
		{"31/ago na lixeira: em 3/mar, com lápide", ago31, tp(utcAt(2027, time.March, 1, 0)), venceAgo31, RetencaoComLapide},
		{"31/ago na lixeira: 1 µs antes de 4/mar, com lápide", ago31, tp(utcAt(2027, time.March, 1, 0)), venceAgo31.Add(FolgaDaLapide - us), RetencaoComLapide},
		{"31/ago na lixeira: em 4/mar, sem lápide", ago31, tp(utcAt(2027, time.March, 1, 0)), venceAgo31.Add(FolgaDaLapide), RetencaoSemLapide},
		// O 29/fev na lixeira.
		{"29/fev na lixeira: em D+30, com lápide", utcAt(2028, time.February, 29, 12), tp(utcAt(2028, time.March, 1, 12)), utcAt(2028, time.March, 31, 12), RetencaoComLapide},
		{"29/fev na lixeira: 1 µs antes de D+30 fica", utcAt(2028, time.February, 29, 12), tp(utcAt(2028, time.March, 1, 12)), utcAt(2028, time.March, 31, 12).Add(-us), RetencaoFica},
	}
	for _, c := range cases {
		// A lixeira chegou à nuvem na hora em que foi feita (o PC com
		// internet): o purge_at gravado é o de sempre, e o piso da chegada
		// não pesa.
		var purge *time.Time
		if c.deleted != nil {
			purge = purgeAt(c.created, c.deleted, *c.deleted)
		}
		if got := DecideRetencao(c.created, c.deleted, purge, c.now); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
		// Sem o purge_at (não acontece pelo código), conta FimDaLixeira: a
		// mesma decisão.
		if got := DecideRetencao(c.created, c.deleted, nil, c.now); got != c.want {
			t.Errorf("%s, sem o purge_at: %v, quero %v", c.name, got, c.want)
		}
	}
}

// now0r é o relógio dos testes da retenção (o mesmo dia de testNow).
var now0r = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// A lixeira não começa antes do próprio ditado (a revisão da F9): o PC com o
// relógio 40 dias atrasado põe na lixeira um ditado criado ontem em outro PC,
// e o deleted_at chega antes do created_at. Sem o piso, o vencimento nasceria
// 10 dias no passado, e o job o apagaria de vez, com a lápide que faz todos os
// PCs apagarem a cópia, na rodada seguinte: sem os 30 dias para restaurar
// (AC-018). Com o piso, conta do created_at. O purge_at que o push grava (o
// pré-filtro do SQL o lê) é o mesmo vencimento que DecideRetencao usa.
func TestFimDaLixeira_NuncaAntesDoDitado(t *testing.T) {
	created := now0r
	atrasado := created.AddDate(0, 0, -40)
	due := created.AddDate(0, 0, 30)
	for _, c := range []struct {
		name string
		now  time.Time
		want Retencao
	}{
		{"uma hora depois fica", created.Add(time.Hour), RetencaoFica},
		{"1 µs antes dos 30 dias do ditado fica", due.Add(-us), RetencaoFica},
		{"nos 30 dias do ditado sai com lápide", due, RetencaoComLapide},
	} {
		if got := DecideRetencao(created, &atrasado, purgeAt(created, &atrasado, created), c.now); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
	}
	if got := FimDaLixeira(created, atrasado); !got.Equal(due) {
		t.Errorf("FimDaLixeira = %s, quero %s (o created_at mais 30 dias)", got, due)
	}
	if p := purgeAt(created, &atrasado, created.Add(time.Hour)); p == nil || !p.Equal(due) {
		t.Errorf("o purge_at gravado = %v, quero %s", p, due)
	}

	// Pelo push: o PC atrasado (outro aparelho) põe na lixeira o ditado do PC
	// que criou.
	criador, atrasadoPC := "00000000-0000-7000-8000-0000000000a1", "00000000-0000-7000-8000-0000000000a2"
	cur := Transcription{ID: "00000000-0000-7000-8000-0000000000f1", CreatedAt: created, FinalText: "de ontem",
		Version: 1, TextVersion: 1, TrashVersion: 1, TextDeviceID: &criador, TrashDeviceID: &criador}
	m := Mutation{MutationID: "00000000-0000-7000-8000-0000000000e1", EntityType: EntityTranscription, EntityID: cur.ID,
		Operation: OpUpsert, BaseVersion: 1, Payload: []byte(`{"deleted_at":"` + atrasado.Format(time.RFC3339Nano) + `"}`)}
	o := DecidePush(m, &cur, false, Actor{UserID: "u", DeviceID: atrasadoPC}, created.Add(time.Hour))
	if o.Result.Status != StatusApplied || o.Next.DeletedAt == nil || o.Next.PurgeAt == nil || !o.Next.PurgeAt.Equal(due) {
		t.Fatalf("o push da lixeira atrasada = %+v, purge_at %v; quero applied e o purge_at em %s", o.Result, o.Next.PurgeAt, due)
	}
	if !o.Next.DeletedAt.Equal(atrasado) {
		t.Errorf("o deleted_at gravado = %s, quero o que o PC mandou (%s): o piso é só do vencimento", o.Next.DeletedAt, atrasado)
	}

	// A lixeira de sempre, depois do ditado, não muda: 30 dias do deleted_at.
	normal := created.Add(time.Hour)
	if got := FimDaLixeira(created, normal); !got.Equal(normal.AddDate(0, 0, 30)) {
		t.Errorf("a lixeira depois do ditado vence em %s, quero %s", got, normal.AddDate(0, 0, 30))
	}
	// E a sobreposição continua: os 6 meses do ditado vencem antes.
	perto := created.AddDate(0, 5, 25)
	if got := FimDaLixeira(created, perto); !got.Equal(created.AddDate(0, 6, 0)) {
		t.Errorf("a sobreposição vence em %s, quero os 6 meses", got)
	}
}

// O piso da chegada (a decisão do Mauro de 08-out-2026, "Fica 24 h na
// lixeira"): a lixeira que chega à nuvem com o deleted_at antigo (o PC que
// ficou sem internet; o relógio do PC atrasado, depois do created_at) nasceria
// vencida, e sairia em até 1 h, com lápide, sem passar pela lixeira
// restaurável. O purge_at nunca vem antes da chegada mais 24 h, e a janela de 6
// meses ganha do piso.
func TestVencimentoDaLixeira_OPisoDaChegada(t *testing.T) {
	chegada := now0r
	piso := chegada.Add(FolgaDaLapide)
	created := chegada.AddDate(0, 0, -60)
	cases := []struct {
		name    string
		created time.Time
		deleted time.Time
		want    time.Time
	}{
		{"o deleted_at de 40 dias atrás: 24 h depois da chegada", created, chegada.AddDate(0, 0, -40), piso},
		{"o deleted_at antes do ditado e os 30 dias dele já passados: o piso também", created, created.AddDate(0, 0, -5), piso},
		{"a lixeira feita agora: os 30 dias", created, chegada, chegada.AddDate(0, 0, 30)},
		{"o PC 2 dias sem internet: os 30 dias do deleted_at, não da chegada", created, chegada.AddDate(0, 0, -2), chegada.AddDate(0, 0, 28)},
		{"faltam 25 h para os 30 dias: eles valem", created, chegada.AddDate(0, 0, -30).Add(25 * time.Hour), chegada.Add(25 * time.Hour)},
		{"faltam 24 h para os 30 dias: o mesmo instante", created, chegada.AddDate(0, 0, -29), piso},
		{"faltam 23 h para os 30 dias: o piso", created, chegada.AddDate(0, 0, -30).Add(23 * time.Hour), piso},
		{"faltam 1 µs para os 30 dias: o piso", created, chegada.AddDate(0, 0, -30).Add(us), piso},
		// A janela ganha do piso: o ditado que fecha os 6 meses 10 h depois da
		// chegada vence com eles, e não 24 h depois.
		{"a janela 10 h depois da chegada ganha do piso", chegada.AddDate(0, -6, 0).Add(10 * time.Hour), chegada.AddDate(0, 0, -40),
			chegada.Add(10 * time.Hour)},
		{"a janela 1 µs antes do piso ganha", chegada.AddDate(0, -6, 0).Add(FolgaDaLapide - us), chegada.AddDate(0, 0, -40),
			piso.Add(-us)},
		{"a janela depois do piso: o piso", chegada.AddDate(0, -6, 0).Add(FolgaDaLapide + us), chegada.AddDate(0, 0, -40), piso},
		// O fim do mês: o 31/ago fecha a janela em 3/mar 12:00Z; a lixeira
		// antiga que chega em 2/mar 20:00Z vence na janela, e não em 3/mar
		// 20:00Z.
		{"31/ago chegado em 2/mar 20:00Z: a janela", utcAt(2026, time.August, 31, 12), utcAt(2026, time.September, 1, 12),
			utcAt(2027, time.March, 3, 12)},
	}
	for _, c := range cases {
		at := chegada
		if c.created.Equal(utcAt(2026, time.August, 31, 12)) {
			at = utcAt(2027, time.March, 2, 20)
		}
		got := VencimentoDaLixeira(c.created, c.deleted, at)
		if !got.Equal(c.want) {
			t.Errorf("%s: %s, quero %s", c.name, got, c.want)
		}
		if p := purgeAt(c.created, &c.deleted, at); p == nil || !p.Equal(c.want) {
			t.Errorf("%s: o purge_at gravado = %v, quero %s", c.name, p, c.want)
		}
		// Nunca antes de FimDaLixeira: o piso só estende.
		if got.Before(FimDaLixeira(c.created, c.deleted)) {
			t.Errorf("%s: %s antes de FimDaLixeira (%s)", c.name, got, FimDaLixeira(c.created, c.deleted))
		}
	}
}

// DecideRetencao lê o purge_at gravado: a lixeira antiga que chegou agora
// fica as 24 h do piso e então sai, com lápide (ainda na janela); a que chega
// perto da janela sai com ela, com lápide até a folga e sem ela depois.
func TestDecideRetencao_LeOPisoGravado(t *testing.T) {
	chegada := now0r
	created := chegada.AddDate(0, 0, -60)
	deleted := chegada.AddDate(0, 0, -40)
	purge := purgeAt(created, &deleted, chegada)
	for _, c := range []struct {
		name string
		now  time.Time
		want Retencao
	}{
		{"na chegada fica", chegada, RetencaoFica},
		{"1 µs antes das 24 h fica", chegada.Add(FolgaDaLapide - us), RetencaoFica},
		{"nas 24 h sai com lápide", chegada.Add(FolgaDaLapide), RetencaoComLapide},
	} {
		if got := DecideRetencao(created, &deleted, purge, c.now); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
	}
	// A janela ganha do piso: o ditado fecha os 6 meses 10 h depois da
	// chegada.
	perto := chegada.AddDate(0, -6, 0).Add(10 * time.Hour)
	janela := perto.AddDate(0, 6, 0)
	pp := purgeAt(perto, &deleted, chegada)
	for _, c := range []struct {
		name string
		now  time.Time
		want Retencao
	}{
		{"perto da janela: 1 µs antes dela fica", janela.Add(-us), RetencaoFica},
		{"perto da janela: nela sai com lápide, antes do piso", janela, RetencaoComLapide},
		{"perto da janela: na folga, sem lápide", janela.Add(FolgaDaLapide), RetencaoSemLapide},
	} {
		if got := DecideRetencao(perto, &deleted, pp, c.now); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
	}

	// Pelo push: o PC que ficou sem internet manda a lixeira de 40 dias atrás
	// sobre a versão que conhece, e a criação já na lixeira (o recomeço manda
	// tudo de novo). O deleted_at gravado continua o que o PC mandou.
	pcID := "00000000-0000-7000-8000-0000000000a1"
	cur := Transcription{ID: "00000000-0000-7000-8000-0000000000f1", CreatedAt: created, FinalText: "de dois meses",
		Version: 1, TextVersion: 1, TrashVersion: 1, TextDeviceID: &pcID, TrashDeviceID: &pcID}
	m := Mutation{MutationID: "00000000-0000-7000-8000-0000000000e1", EntityType: EntityTranscription, EntityID: cur.ID,
		Operation: OpUpsert, BaseVersion: 1, Payload: []byte(`{"deleted_at":"` + deleted.Format(time.RFC3339Nano) + `"}`)}
	o := DecidePush(m, &cur, false, Actor{UserID: "u", DeviceID: pcID}, chegada)
	if o.Result.Status != StatusApplied || o.Next.PurgeAt == nil || !o.Next.PurgeAt.Equal(chegada.Add(FolgaDaLapide)) {
		t.Fatalf("a lixeira antiga pelo push = %+v, purge_at %v; quero applied e o purge_at 24 h depois da chegada", o.Result, o.Next.PurgeAt)
	}
	if !o.Next.DeletedAt.Equal(deleted) {
		t.Errorf("o deleted_at gravado = %s, quero o que o PC mandou (%s)", o.Next.DeletedAt, deleted)
	}
	m.MutationID, m.BaseVersion = "00000000-0000-7000-8000-0000000000e2", 0
	m.Payload = []byte(`{"source_platform":"windows","raw_text":"bruto","final_text":"de dois meses","mode":"smart",` +
		`"asr_provider":"whisper-local","status":"done","created_at":"` + created.Format(time.RFC3339Nano) +
		`","deleted_at":"` + deleted.Format(time.RFC3339Nano) + `"}`)
	o = DecidePush(m, nil, false, Actor{UserID: "u", DeviceID: pcID}, chegada)
	if o.Write != WriteInsert || o.Next.PurgeAt == nil || !o.Next.PurgeAt.Equal(chegada.Add(FolgaDaLapide)) {
		t.Fatalf("a criação na lixeira antiga = %+v, purge_at %v; quero o insert com o purge_at 24 h depois da chegada", o.Result, o.Next.PurgeAt)
	}

	// A lixeira que já estava lá não ganha piso novo: o PC manda a lixeira de
	// novo, com outra data, horas depois, e nada se grava.
	trashed := o.Next
	m.MutationID, m.BaseVersion = "00000000-0000-7000-8000-0000000000e3", trashed.Version
	m.Payload = []byte(`{"deleted_at":"` + chegada.Format(time.RFC3339Nano) + `"}`)
	if o := DecidePush(m, &trashed, false, Actor{UserID: "u", DeviceID: pcID}, chegada.Add(12*time.Hour)); o.Write != WriteNone {
		t.Fatalf("a lixeira de novo = %+v (write %v), quero nada a gravar", o.Result, o.Write)
	}

	// O CRUD (o celular): a lixeira é agora, e o piso não pesa.
	if next, _ := Trash(cur, nil, chegada); next.PurgeAt == nil || !next.PurgeAt.Equal(chegada.AddDate(0, 0, 30)) {
		t.Errorf("a lixeira do celular: purge_at %v, quero os 30 dias", next.PurgeAt)
	}
}

// O relógio que anda para trás: a decisão é só da data, e o item que venceria
// agora volta a ficar se o relógio recuar (nada é decidido de vez sem a
// linha travada; ver o Reter).
func TestDecideRetencao_RelogioQueVoltaAtras(t *testing.T) {
	created := now0r
	later := now0r.AddDate(0, 6, 0)
	if got := DecideRetencao(created, nil, nil, later); got != RetencaoSemLapide {
		t.Fatalf("no vencimento: %v", got)
	}
	if got := DecideRetencao(created, nil, nil, later.Add(-time.Hour)); got != RetencaoFica {
		t.Fatalf("com o relógio uma hora atrás: %v, quero fica", got)
	}
	deleted := now0r.Add(time.Hour)
	trashDue := deleted.AddDate(0, 0, 30)
	purge := purgeAt(created, &deleted, deleted)
	if got := DecideRetencao(created, &deleted, purge, trashDue); got != RetencaoComLapide {
		t.Fatalf("lixeira no vencimento: %v", got)
	}
	if got := DecideRetencao(created, &deleted, purge, trashDue.Add(-time.Minute)); got != RetencaoFica {
		t.Fatalf("lixeira com o relógio um minuto atrás: %v, quero fica", got)
	}
}

func TestLapideVencida_JanelaMaisUmDia(t *testing.T) {
	cases := []struct {
		name     string
		dictated time.Time
		now      time.Time
		want     bool
	}{
		// O ditado de 31/ago/2026 12:00Z sai da janela em 3/mar 12:00Z, e a
		// lápide vive até 4/mar 12:00Z.
		{"31/ago: em 3/mar (o fim da janela) vive", utcAt(2026, time.August, 31, 12), utcAt(2027, time.March, 3, 12), false},
		{"31/ago: 1 µs antes de 4/mar vive", utcAt(2026, time.August, 31, 12), utcAt(2027, time.March, 4, 12).Add(-us), false},
		{"31/ago: em 4/mar vence", utcAt(2026, time.August, 31, 12), utcAt(2027, time.March, 4, 12), true},
		{"31/ago: em 2/mar (o corte calculado em Go a apagaria) vive", utcAt(2026, time.August, 31, 12), utcAt(2027, time.March, 2, 12), false},
		{"31/ago: em 1/mar (o interval do Postgres a apagaria) vive", utcAt(2026, time.August, 31, 12), utcAt(2027, time.March, 1, 12), false},
		// O 29/fev.
		{"29/ago/2027: em 29/fev/2028 vive", utcAt(2027, time.August, 29, 12), utcAt(2028, time.February, 29, 12), false},
		{"29/ago/2027: 1 µs antes de 1/mar vive", utcAt(2027, time.August, 29, 12), utcAt(2028, time.March, 1, 12).Add(-us), false},
		{"29/ago/2027: em 1/mar vence", utcAt(2027, time.August, 29, 12), utcAt(2028, time.March, 1, 12), true},
		{"29/fev/2028: 1 µs antes de 30/ago vive", utcAt(2028, time.February, 29, 12), utcAt(2028, time.August, 30, 12).Add(-us), false},
		{"29/fev/2028: em 30/ago vence", utcAt(2028, time.February, 29, 12), utcAt(2028, time.August, 30, 12), true},
		// Um dia comum.
		{"5/out: 1 µs antes de 6/abr vive", now0r, now0r.AddDate(0, 6, 1).Add(-us), false},
		{"5/out: em 6/abr vence", now0r, now0r.AddDate(0, 6, 1), true},
	}
	for _, c := range cases {
		if got := LapideVencida(c.dictated, c.now); got != c.want {
			t.Errorf("%s: %v, quero %v", c.name, got, c.want)
		}
		if got := DeixaLapide(c.dictated, c.now); got == c.want {
			t.Errorf("%s: DeixaLapide = %v, quero o contrário de LapideVencida", c.name, got)
		}
	}
}

// A propriedade do pré-filtro, hora a hora, por quatro anos (um bissexto no
// meio): nada acima do corte venceu. Um item vencido (o ativo pelos 6 meses,
// a lápide pela janela mais a folga) está sempre dentro do corte, então o
// SQL traz candidatos a mais, nunca de menos. E LapideVencida implica
// OutOfWindow: a lápide nunca sai antes da recusa por janela cobrir.
func TestCortes_ONaoVencidoFicaDeForaEOVencidoDentro(t *testing.T) {
	if testing.Short() {
		t.Skip("varre quatro anos hora a hora")
	}
	start := utcAt(2026, time.January, 1, 0)
	end := utcAt(2030, time.January, 1, 0)
	// O estouro do fim do mês é de no máximo 3 dias (31 contra 28): 80 horas
	// acima do corte cobrem toda a zona em que um defeito apareceria.
	const above = 80
	for n := start; n.Before(end); n = n.Add(time.Hour) {
		for _, now := range []time.Time{n, n.Add(30 * time.Minute), n.Add(-us)} {
			cut, cutL := CorteDoAtivo(now), CorteDaLapide(now)
			for _, c := range []time.Time{cut.Add(us), cutL.Add(us)} {
				if OutOfWindow(c, now) && c.After(cut) {
					t.Fatalf("now %s: o ditado de %s venceu e está acima do corte %s", now, c, cut)
				}
				if LapideVencida(c, now) && c.After(cutL) {
					t.Fatalf("now %s: a lápide de %s venceu e está acima do corte %s", now, c, cutL)
				}
			}
			if !now.Equal(n) {
				continue
			}
			for k := 1; k <= above; k++ {
				c := cut.Add(time.Duration(k) * time.Hour)
				if OutOfWindow(c, now) {
					t.Fatalf("now %s: o ditado de %s venceu e está acima do corte %s", now, c, cut)
				}
				cl := cutL.Add(time.Duration(k) * time.Hour)
				if LapideVencida(cl, now) {
					t.Fatalf("now %s: a lápide de %s venceu e está acima do corte %s", now, cl, cutL)
				}
			}
		}
	}
	// Do lado do item: cada ditado da grade, no vencimento e nas horas
	// seguintes, está dentro do corte; e a lápide vencida implica a janela.
	for c := start; c.Before(end); c = c.Add(time.Hour) {
		due := c.AddDate(0, 6, 0)
		for _, now := range []time.Time{due, due.Add(us), due.Add(time.Hour), due.Add(FolgaDaLapide), due.Add(FolgaDaLapide + time.Hour), due.Add(72 * time.Hour)} {
			if !OutOfWindow(c, now) {
				t.Fatalf("o ditado de %s não venceu em %s (vence em %s)", c, now, due)
			}
			if c.After(CorteDoAtivo(now)) {
				t.Fatalf("o ditado de %s venceu em %s e ficou acima do corte %s", c, now, CorteDoAtivo(now))
			}
			if LapideVencida(c, now) {
				if !OutOfWindow(c, now) {
					t.Fatalf("a lápide de %s venceu em %s sem o ditado sair da janela", c, now)
				}
				if c.After(CorteDaLapide(now)) {
					t.Fatalf("a lápide de %s venceu em %s e ficou acima do corte %s", c, now, CorteDaLapide(now))
				}
			}
		}
		if LapideVencida(c, due.Add(FolgaDaLapide-us)) {
			t.Fatalf("a lápide de %s venceu antes da janela mais a folga", c)
		}
		if !LapideVencida(c, due.Add(FolgaDaLapide)) {
			t.Fatalf("a lápide de %s não venceu na janela mais a folga", c)
		}
	}
}

// As datas em UTC: o pgx devolve timestamptz no fuso local, e o AddDate soma
// meses no relógio de parede do fuso. Com time.Local num fuso positivo, o
// 31/ago 12:00Z é 1/set 02:00 local, e 6 meses no fuso local dariam 1/mar
// 02:00 local (28/fev 12:00Z): três dias antes. Toda regra conta em UTC.
func TestRetencao_EmUTCComOFusoLocalTrocado(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("UTC+14", 14*60*60)
	t.Cleanup(func() { time.Local = old })

	created := utcAt(2026, time.August, 31, 12)
	local := func(x time.Time) time.Time { return x.In(time.Local) }
	if local(created).Day() != 1 || local(created).AddDate(0, 6, 0).UTC().After(utcAt(2027, time.March, 1, 0)) {
		t.Fatalf("o cenário não exercita o fuso: %s, %s", local(created), local(created).AddDate(0, 6, 0).UTC())
	}
	for _, now := range []time.Time{
		utcAt(2027, time.February, 28, 12), utcAt(2027, time.March, 1, 12), utcAt(2027, time.March, 3, 12).Add(-us),
		utcAt(2027, time.March, 3, 12), utcAt(2027, time.March, 4, 12).Add(-us), utcAt(2027, time.March, 4, 12),
	} {
		if a, b := OutOfWindow(local(created), local(now)), OutOfWindow(created, now); a != b {
			t.Errorf("OutOfWindow em %s: local %v, UTC %v", now, a, b)
		}
		if a, b := LapideVencida(local(created), local(now)), LapideVencida(created, now); a != b {
			t.Errorf("LapideVencida em %s: local %v, UTC %v", now, a, b)
		}
		if a, b := DecideRetencao(local(created), nil, nil, local(now)), DecideRetencao(created, nil, nil, now); a != b {
			t.Errorf("DecideRetencao (ativo) em %s: local %v, UTC %v", now, a, b)
		}
		deleted := utcAt(2027, time.February, 20, 12)
		ld := local(deleted)
		lp, up := purgeAt(local(created), &ld, ld), purgeAt(created, &deleted, deleted)
		if a, b := DecideRetencao(local(created), &ld, lp, local(now)), DecideRetencao(created, &deleted, up, now); a != b {
			t.Errorf("DecideRetencao (lixeira) em %s: local %v, UTC %v", now, a, b)
		}
		lpl := local(*lp)
		if a, b := DecideRetencao(local(created), &ld, &lpl, local(now)), DecideRetencao(created, &deleted, up, now); a != b {
			t.Errorf("DecideRetencao (lixeira, o purge_at lido no fuso local) em %s: local %v, UTC %v", now, a, b)
		}
		if a, b := CorteDoAtivo(local(now)), CorteDoAtivo(now); !a.Equal(b) {
			t.Errorf("CorteDoAtivo em %s: local %s, UTC %s", now, a, b)
		}
		if a, b := CorteDaLapide(local(now)), CorteDaLapide(now); !a.Equal(b) {
			t.Errorf("CorteDaLapide em %s: local %s, UTC %s", now, a, b)
		}
	}
	if DecideRetencao(local(created), nil, nil, local(utcAt(2027, time.March, 1, 12))) != RetencaoFica {
		t.Error("o 31/ago contado no fuso local venceu em 1/mar")
	}
	// O purge_at que o push grava também é em UTC.
	fev20 := local(utcAt(2027, time.February, 20, 12))
	if p := purgeAt(local(created), &fev20, fev20); !p.Equal(utcAt(2027, time.March, 3, 12)) {
		t.Errorf("purgeAt no fuso local = %s, quero 3/mar 12:00Z", p)
	}
	// E o piso da chegada, limitado pela janela: a lixeira antiga que chega
	// em 2/mar 20:00Z teria o piso em 3/mar 20:00Z, e a janela (3/mar 12:00Z)
	// ganha. Contada no fuso local, a janela cairia em 28/fev 12:00Z.
	set1 := local(utcAt(2026, time.September, 1, 12))
	if p := purgeAt(local(created), &set1, local(utcAt(2027, time.March, 2, 20))); !p.Equal(utcAt(2027, time.March, 3, 12)) {
		t.Errorf("o piso no fuso local = %s, quero a janela, 3/mar 12:00Z", p)
	}
}

// Os prazos da arrumação, nas fronteiras.
func TestPrazos_DaArrumacao(t *testing.T) {
	// A resposta guardada nunca vence antes da vida do token mais 10 dias:
	// se alguém baixar a constante, o reenvio de um PC que volta dentro da
	// vida do token deixa de ser duplicate.
	if PrazoDasMutacoes < TokenSemUso+10*24*time.Hour {
		t.Fatalf("PrazoDasMutacoes = %s, abaixo de TokenSemUso + 10 dias (%s)", PrazoDasMutacoes, TokenSemUso+10*24*time.Hour)
	}
	if PrazoDasMutacoes != 190*24*time.Hour || PrazoDosEventos != 90*24*time.Hour {
		t.Fatalf("prazos = %s e %s, quero 190 e 90 dias", PrazoDasMutacoes, PrazoDosEventos)
	}
	if FolgaDaLapide != FutureTolerance {
		t.Fatalf("FolgaDaLapide = %s, FutureTolerance = %s: a folga é a mesma", FolgaDaLapide, FutureTolerance)
	}
	day := 24 * time.Hour
	now := now0r
	for _, c := range []struct {
		name string
		age  time.Duration
		want bool
	}{
		{"evento de 89 dias fica", 89 * day, false},
		{"evento 1 µs antes dos 90 dias fica", 90*day - us, false},
		{"evento de 90 dias sai", 90 * day, true},
		{"evento de 91 dias sai", 91 * day, true},
		{"evento de agora fica", 0, false},
	} {
		if got := EventoVencido(now.Add(-c.age), now); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	for _, c := range []struct {
		name string
		age  time.Duration
		want bool
	}{
		{"resposta de 179 dias fica", 179 * day, false},
		{"resposta de 189 dias fica", 189 * day, false},
		{"resposta 1 µs antes dos 190 dias fica", 190*day - us, false},
		{"resposta de 190 dias sai", 190 * day, true},
		{"resposta de 191 dias sai", 191 * day, true},
	} {
		if got := MutacaoVencida(now.Add(-c.age), now); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	// A resposta de um envio feito agora ainda vale no último instante em que
	// o token dele vale (180 dias sem uso, contados desse envio).
	sent := now
	lastValid := sent.Add(TokenSemUso - us)
	if TokenVencido(sent, lastValid) || MutacaoVencida(sent, lastValid) {
		t.Fatal("o token ou a resposta venceram antes dos 180 dias")
	}
	if MutacaoVencida(sent, sent.Add(TokenSemUso)) {
		t.Fatal("a resposta venceu junto com o token")
	}
}
