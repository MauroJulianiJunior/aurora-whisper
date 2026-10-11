package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A guarda da D3 na lixeira do PC (ADR-019, "Decisões da F10", "A guarda, em
// cada situação"): uma linha de teste por linha da tabela de situações, num
// historico.db de verdade, na pasta temporária, com o relógio dado a cada
// chamada. A nuvem não existe aqui: o que ela faria (confirmar o push,
// reconciliar) é o que a fila grava com a resposta (Aplicar), ou o estado que
// o push deixa (a versão da nuvem e a fila sem o marcador). O ponta a ponta,
// com a nuvem de verdade, está em internal/server/integration
// (sync_lixeira_d3_e2e_test.go).

// agoraD3 é o "agora" destes testes.
var agoraD3 = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

const umDia = 24 * time.Hour

// expurgar roda o PurgeExpired em quando e devolve o resultado.
func (f filaDeTeste) expurgar(t *testing.T, quando time.Time) storage.ResultadoDoExpurgo {
	t.Helper()
	r, err := f.repo.PurgeExpired(context.Background(), quando)
	if err != nil {
		t.Fatalf("PurgeExpired em %s: %v", quando, err)
	}
	return r
}

// tem diz se o ditado ainda está no banco (no histórico ou na lixeira).
func (f filaDeTeste) tem(t *testing.T, id string) bool {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// subiu é o estado que o push confirmado deixa: a versão da nuvem na linha e
// a fila sem o marcador.
func (f filaDeTeste) subiu(t *testing.T, id string, v int64) {
	t.Helper()
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = ? WHERE id = ?`, v, id)
	executar(t, f.db, `DELETE FROM sync_outbox WHERE entity_type = 'transcription' AND entity_id = ?`, id)
}

func (f filaDeTeste) lixeira(t *testing.T, id string, quando time.Time) {
	t.Helper()
	if err := f.repo.SoftDelete(context.Background(), donoDaFila, id, quando); err != nil {
		t.Fatalf("SoftDelete %s: %v", id, err)
	}
}

// aplicar é a resposta da nuvem para um ditado, pela fila, com o aparelho e
// o epoch do pareamento dado.
func (f filaDeTeste) aplicar(t *testing.T, aparelho, id string, a storage.AcaoLocal) {
	t.Helper()
	if err := f.fila.Aplicar(context.Background(), aparelho, "e1", []string{id}, nil,
		func(storage.EstadoDoItem) storage.AcaoLocal { return a }); err != nil {
		t.Fatalf("Aplicar %s: %v", id, err)
	}
}

func querExpurgo(t *testing.T, r storage.ResultadoDoExpurgo, apagados, esperando int, quando string) {
	t.Helper()
	if r.Apagados != apagados || r.EsperandoANuvem != esperando {
		t.Fatalf("%s: o expurgo = %+v; quero %d apagado(s) e %d esperando a nuvem", quando, r, apagados, esperando)
	}
}

// Nunca conectado: nenhum marcador, nenhuma anotação, nenhum Esquecer. Os 30
// dias de hoje: fica aos 29, sai aos 31.
func TestLixeiraD3_NuncaConectadoSaiAos30Dias(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	f.lixeira(t, idA, agoraD3)
	querExpurgo(t, f.expurgar(t, agoraD3.Add(29*umDia)), 0, 0, "aos 29 dias")
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 1, 0, "aos 31 dias")
	if f.tem(t, idA) {
		t.Fatal("o ditado ficou")
	}
}

// Conectado, o ditado nascido aqui que a nuvem nunca teve: o SoftDelete tira
// o marcador de criar parado, e ele sai aos 30 dias.
func TestLixeiraD3_NascidoAquiSemSubirSaiAos30Dias(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	if len(f.marcadores(t)) != 1 {
		t.Fatal("o ditado novo conectado não entrou na fila")
	}
	f.lixeira(t, idA, agoraD3)
	if len(f.marcadores(t)) != 0 {
		t.Fatal("a lixeira não tirou o marcador de criar parado")
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 1, 0, "aos 31 dias")
}

// O marcador em voo (a resposta ainda não chegou) fica: a nuvem pode já ter o
// ditado. Ele fica até a resposta, e sai na rodada seguinte.
func TestLixeiraD3_EmVooFicaAteAResposta(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	f.conectar(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeDitado, EntityID: idA, MutationID: "m1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	f.lixeira(t, idA, agoraD3)
	if m, ok := f.marcadores(t)[idA]; !ok || !m.EmVoo() {
		t.Fatalf("o marcador em voo saiu com a lixeira: %+v %v", m, ok)
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 0, 1, "aos 31 dias, em voo")
	// A resposta chega: applied, com a versão da nuvem, e o marcador sai.
	v := int64(2)
	f.aplicar(t, "a1", idA, storage.AcaoLocal{ServerVersion: &v, Marcador: storage.MarcadorSai})
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia+time.Hour)), 1, 0, "a rodada depois da resposta")
}

// Conectado, o ditado que a nuvem tem: o SoftDelete põe o marcador da
// lixeira; fica até o push confirmar, e sai na rodada seguinte.
func TestLixeiraD3_ConectadoComANuvemFicaAteOPush(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	f.subiu(t, idA, 1)
	f.lixeira(t, idA, agoraD3)
	if m := f.marcadores(t)[idA]; m.Campos != storage.CampoLixeira {
		t.Fatalf("o marcador da lixeira: %+v", m)
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 0, 1, "aos 31 dias, sem o push")
	f.subiu(t, idA, 2)
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia+time.Hour)), 1, 0, "depois do push")
}

// Desconectado (o caso da D4 da F9): o Desconectar mantém o server_version, e
// o marcador da lixeira nasce do mesmo jeito. Fica aos 31 e aos 90 dias, e
// sai depois que o marcador sai (a volta, o push confirmado).
func TestLixeiraD3_DesconectadoFicaAteANuvemSaber(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	f.subiu(t, idA, 1)
	f.desconectar(t)
	f.lixeira(t, idA, agoraD3)
	if m, ok := f.marcadores(t)[idA]; !ok || m.Campos != storage.CampoLixeira {
		t.Fatalf("desconectado, a lixeira de um ditado que a nuvem tem não entrou na fila: %+v %v", m, ok)
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 0, 1, "aos 31 dias")
	querExpurgo(t, f.expurgar(t, agoraD3.Add(90*umDia)), 0, 1, "aos 90 dias")
	if !f.tem(t, idA) {
		t.Fatal("a lixeira desconectada saiu antes de a nuvem saber")
	}
	f.conectar(t)
	f.subiu(t, idA, 2)
	querExpurgo(t, f.expurgar(t, agoraD3.Add(90*umDia+time.Hour)), 1, 0, "depois de o marcador sair")
}

// Desconectado para sempre: sai quando o ditado completa 6 meses (o teto da
// D3), e não antes: 1 ns antes fica.
func TestLixeiraD3_DesconectadoParaSempreSaiNoTeto(t *testing.T) {
	f := novaFilaDeTeste(t)
	criado := agoraD3.Add(-time.Hour)
	f.conectar(t)
	f.ditar(t, idA, criado)
	f.subiu(t, idA, 1)
	f.desconectar(t)
	f.lixeira(t, idA, agoraD3)
	teto := criado.AddDate(0, 6, 0)
	querExpurgo(t, f.expurgar(t, teto.Add(-time.Nanosecond)), 0, 1, "1 ns antes do teto")
	querExpurgo(t, f.expurgar(t, teto), 1, 0, "no teto")
	if f.tem(t, idA) {
		t.Fatal("a lixeira passou dos 6 meses do ditado")
	}
}

// Depois do Esquecer, a mudança anotada (P2) fica: o A foi para a lixeira com
// a nuvem esquecida sem saber (o marcador dele estava na fila na hora do
// Esquecer). O B, editado sem subir antes do Esquecer, também está anotado: na
// volta à mesma conta, reconcilia (a nuvem o tem: o marcador de criar, e o
// push dá a versão), vai para a lixeira, o push confirma, e ele sai aos 30
// dias, mesmo continuando anotado (a anotação só segura sem server_version).
func TestLixeiraD3_EsquecerComMudancaAnotada(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	f.conectar(t)
	criado := agoraD3.Add(-time.Hour)
	for _, id := range []string{idA, idB} {
		f.ditar(t, id, criado)
		f.subiu(t, id, 1)
	}
	f.lixeira(t, idA, agoraD3) // o marcador da lixeira fica na fila
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idB, 1, "editado sem subir"); err != nil {
		t.Fatal(err)
	}
	f.desconectar(t)
	esquecida := agoraD3.Add(time.Minute)
	if err := f.fila.EsquecerNuvem(ctx, esquecida); err != nil {
		t.Fatal(err)
	}
	if got := anotados(t, e); len(got) != 2 {
		t.Fatalf("o Esquecer anotou %v; quero o A e o B", got)
	}
	// O A: na lixeira antes do Esquecer (P3 não vale), sem marcador (a fila
	// saiu), sem server_version e anotado: só o P2 o segura.
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 0, 1, "o A aos 31 dias")

	// A volta à mesma conta: outro pareamento, a reconciliação do pull (o
	// marcador de criar) e o push confirmado do B. Depois, a lixeira do B
	// sobe e é confirmada.
	f.conectar(t)
	if _, _, err := f.fila.Preparar(ctx, "a1", "p2", agoraD3.Add(32*umDia)); err != nil {
		t.Fatal(err)
	}
	f.aplicar(t, "a1", idB, storage.AcaoLocal{Marcador: storage.MarcadorCriar})
	v := int64(3)
	f.aplicar(t, "a1", idB, storage.AcaoLocal{ServerVersion: &v, Marcador: storage.MarcadorSai})
	naLixeira := agoraD3.Add(33 * umDia)
	f.lixeira(t, idB, naLixeira)
	v = 4
	f.aplicar(t, "a1", idB, storage.AcaoLocal{ServerVersion: &v, Marcador: storage.MarcadorSai})
	if got := anotados(t, e); len(got) != 2 {
		t.Fatalf("a anotação do B saiu antes do próximo Esquecer: %v", got)
	}
	querExpurgo(t, f.expurgar(t, naLixeira.Add(29*umDia)), 0, 1, "o B aos 29 dias (só o A espera)")
	querExpurgo(t, f.expurgar(t, naLixeira.Add(30*umDia)), 1, 1, "o B aos 30 dias")
	if f.tem(t, idB) || !f.tem(t, idA) {
		t.Fatalf("depois dos 30 dias do B: o B ficou %v, o A ficou %v; quero só o A", f.tem(t, idB), f.tem(t, idA))
	}
}

// A lixeira feita depois do Esquecer num ditado de antes (P3) fica, mesmo o
// que nunca subiu (o Esquecer zerou o server_version, que dizia se a nuvem o
// tinha), e também com o PC ligado a outra conta; os dois até a janela. A
// lixeira de um ditado criado depois do Esquecer sai aos 30 dias.
func TestLixeiraD3_LixeiraDepoisDoEsquecer(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	criado := agoraD3.Add(-time.Hour)
	f.conectar(t)
	f.ditar(t, idA, criado) // subiu
	f.subiu(t, idA, 1)
	f.desconectar(t)
	f.ditar(t, idB, criado) // nunca subiu: feito desconectado, sem marcador
	esquecida := agoraD3
	if err := f.fila.EsquecerNuvem(ctx, esquecida); err != nil {
		t.Fatal(err)
	}
	// O PC liga a outra conta (outro aparelho, outra nuvem).
	executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'b1'), ('nuvem_conectado_em', 'q1')`)
	depois := esquecida.Add(time.Hour)
	f.ditar(t, idC, depois) // criado depois do Esquecer, na outra conta
	for _, id := range []string{idA, idB, idC} {
		f.lixeira(t, id, depois.Add(time.Minute))
	}
	if ms := f.marcadores(t); len(ms) != 0 {
		t.Fatalf("a lixeira de um ditado que esta nuvem não tem entrou na fila: %v", ms)
	}
	r := f.expurgar(t, depois.Add(31*umDia))
	querExpurgo(t, r, 1, 2, "aos 31 dias")
	if !f.tem(t, idA) || !f.tem(t, idB) || f.tem(t, idC) {
		t.Fatalf("aos 31 dias: A %v, B %v, C %v; quero o A e o B, e o C fora", f.tem(t, idA), f.tem(t, idB), f.tem(t, idC))
	}
	teto := criado.AddDate(0, 6, 0)
	querExpurgo(t, f.expurgar(t, teto.Add(-time.Nanosecond)), 0, 2, "1 ns antes da janela")
	querExpurgo(t, f.expurgar(t, teto), 2, 0, "na janela")
}

// O recusado sai aos 30 dias (decisão do Mauro): a nuvem disse que não o quer,
// e segurar não ajuda, mesmo com o marcador da lixeira na fila.
func TestLixeiraD3_RecusadoSaiAos30Dias(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	f.subiu(t, idA, 1)
	executar(t, f.db, `UPDATE local_transcriptions SET sync_state = ?, last_sync_error = 'texto_grande' WHERE id = ?`,
		storage.SituacaoRecusado, idA)
	f.lixeira(t, idA, agoraD3)
	if _, ok := f.marcadores(t)[idA]; !ok {
		t.Fatal("a lixeira do recusado com server_version não entrou na fila; o caso não exercita a guarda")
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 1, 0, "aos 31 dias")
}

// O fora_da_janela sai aos 30 dias, mesmo com um marcador: a nuvem já disse
// que ele não vale mais lá.
func TestLixeiraD3_ForaDaJanelaComMarcadorSai(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	f.subiu(t, idA, 1)
	f.lixeira(t, idA, agoraD3)
	executar(t, f.db, `UPDATE local_transcriptions SET sync_state = ? WHERE id = ?`, storage.SituacaoForaDaJanela, idA)
	if _, ok := f.marcadores(t)[idA]; !ok {
		t.Fatal("sem o marcador, o caso não exercita a guarda")
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 1, 0, "aos 31 dias")
}

// O epoch novo (um backup restaurado): o recomeço tira o instante do Esquecer
// e as anotações, e a lixeira fica de fora da fila. O que P2 e P3 seguravam
// sai aos 30 dias.
func TestLixeiraD3_EpochNovoSai(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	criado := agoraD3.Add(-time.Hour)
	f.conectar(t)
	f.ditar(t, idA, criado)
	f.subiu(t, idA, 1)
	f.lixeira(t, idA, agoraD3) // anotado pelo Esquecer (P2)
	f.desconectar(t)
	f.ditar(t, idB, criado)
	if err := f.fila.EsquecerNuvem(ctx, agoraD3.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.lixeira(t, idB, agoraD3.Add(time.Hour)) // depois do Esquecer (P3)
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 0, 2, "antes do recomeço")

	f.conectar(t)
	if err := f.fila.Recomecar(ctx, "a1", "p2", agoraD3.Add(32*umDia)); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 0 {
		t.Fatalf("o recomeço pôs a lixeira na fila: %v", ms)
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(32*umDia)), 2, 0, "depois do recomeço")
}

// O marcador do aprendido nunca é tocado pelo expurgo, nem o que tem o mesmo
// entity_id de um ditado expurgado; a anotação do ditado expurgado sai com
// ele. E a segunda rodada não apaga nada.
func TestLixeiraD3_OAprendidoFicaEASegundaRodadaApagaZero(t *testing.T) {
	f := novaFilaDeTeste(t)
	f.ditar(t, idA, agoraD3.Add(-time.Hour))
	f.lixeira(t, idA, agoraD3)
	executar(t, f.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, 0, ?), (?, 'outro', 0, ?)`,
		storage.EntidadeAprendido, idA, emTextoDeTeste(agoraD3), storage.EntidadeAprendido, emTextoDeTeste(agoraD3))
	// Uma anotação do A, já reconciliado (com a versão da nuvem, ela não o
	// segura): ela sai junto com ele.
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id = ?`, idA)
	executar(t, f.db, `INSERT INTO sync_mudou_aqui (entity_type, entity_id) VALUES (?, ?)`, storage.EntidadeDitado, idA)

	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia)), 1, 0, "a primeira rodada")
	var aprendidos, anotacoes int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM sync_outbox WHERE entity_type = ?`, storage.EntidadeAprendido).Scan(&aprendidos); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM sync_mudou_aqui`).Scan(&anotacoes); err != nil {
		t.Fatal(err)
	}
	if aprendidos != 2 {
		t.Errorf("o expurgo tocou o marcador do aprendido: %d de 2", aprendidos)
	}
	if anotacoes != 0 {
		t.Errorf("a anotação do ditado expurgado ficou: %d", anotacoes)
	}
	querExpurgo(t, f.expurgar(t, agoraD3.Add(31*umDia+time.Hour)), 0, 0, "a segunda rodada")
}

// O freio vale também para o teto: com a última rodada do ciclo há 8 dias (o
// Agent parado, ou o relógio que pulou), a lixeira com marcador e a janela
// vencida fica na rodada em que o freio dispara; com a rodada gravada de novo
// (o ciclo a grava ao fim de toda rodada, F10c), sai na seguinte. Sem rodada
// gravada, nada freia.
func TestLixeiraD3_OFreioAdiaOTetoPorUmaRodada(t *testing.T) {
	f := novaFilaDeTeste(t)
	criado := agoraD3.AddDate(0, -7, 0)
	f.ditar(t, idA, criado)
	f.subiu(t, idA, 1)
	f.desconectar(t)
	f.lixeira(t, idA, criado.Add(time.Hour)) // o marcador da lixeira fica
	agora := agoraD3

	executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`,
		storage.ChaveRetencaoRodadaEm, emTextoDeTeste(agora.AddDate(0, 0, -8)))
	querExpurgo(t, f.expurgar(t, agora), 0, 1, "a rodada em que o freio dispara")

	// Exatamente 7 dias não freia.
	executar(t, f.db, `UPDATE instalacao SET valor = ? WHERE nome = ?`,
		emTextoDeTeste(agora.Add(-7*umDia)), storage.ChaveRetencaoRodadaEm)
	querExpurgo(t, f.expurgar(t, agora), 1, 0, "a rodada seguinte, com a rodada gravada")
}

// Sem rodada gravada, o teto vale já na primeira rodada; e um valor ilegível
// conta como freio (segurar uma rodada a mais nunca apaga cedo).
func TestLixeiraD3_OFreioSemRodadaEIlegivel(t *testing.T) {
	for _, c := range []struct {
		nome   string
		rodada string
		fica   bool
	}{
		{"sem rodada", "", false},
		{"ilegível", "ontem", true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novaFilaDeTeste(t)
			criado := agoraD3.AddDate(0, -7, 0)
			f.ditar(t, idA, criado)
			f.subiu(t, idA, 1)
			f.lixeira(t, idA, criado.Add(time.Hour))
			if c.rodada != "" {
				executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveRetencaoRodadaEm, c.rodada)
			}
			r := f.expurgar(t, agoraD3)
			if f.tem(t, idA) != c.fica || (r.EsperandoANuvem == 1) != c.fica {
				t.Fatalf("o expurgo = %+v, o ditado ficou %v; quero ficar %v", r, f.tem(t, idA), c.fica)
			}
		})
	}
}
