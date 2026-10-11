package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// As decisões do aprendido no PC (ADR-019, "Decisões da F8", F8f2) contra a
// nuvem de verdade e o cliente de verdade: a mutação que o PC monta passa nas
// pré-checagens da nuvem, e a resposta dela (o applied, o conflict com o
// state, o duplicate do reenvio) é decidida pelo PC como a ADR manda. O
// espelho é o de verdade (o historico.db de cada PC); o dicionário é uma
// Semente em memória (nenhum dicionario.json). O motor ainda não manda nem
// aplica o aprendido (F8f3 e F8f4): o envio, o pull e a varredura daqui são
// montados à mão, com as decisões da F8f2.

// aprendizE2E é um PC com o espelho, o dicionário em memória e o cliente.
type aprendizE2E struct {
	t       *testing.T
	pc      *pcE2E
	fila    *storage.Fila
	cliente *syncengine.Cliente
	semente dictionary.Semente
	cursor  int64
	epoch   string
}

func novoAprendizE2E(t *testing.T, n *nuvemE2E) *aprendizE2E {
	t.Helper()
	pc := n.novoPC()
	a := &aprendizE2E{t: t, pc: pc, fila: pc.repo.Fila(pc.dono), cliente: syncengine.NovoCliente(n.base, pc.token, nil),
		semente: dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: "ECD", Prioridade: 90}}}}
	if _, _, err := a.fila.Preparar(context.Background(), pc.aparelho, "p-e2e", time.Now()); err != nil {
		t.Fatal(err)
	}
	// O "Enviar o histórico" já clicado (D3).
	if _, err := pc.db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_envio_aceito_em', 'x')`); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *aprendizE2E) estado(id string) storage.EstadoDoAprendido {
	a.t.Helper()
	es, err := a.fila.Espelho(context.Background())
	if err != nil {
		a.t.Fatal(err)
	}
	for _, e := range es {
		if e.ID == id {
			return e
		}
	}
	return storage.EstadoDoAprendido{ID: id}
}

func (a *aprendizE2E) noArquivo(id string) *dictionary.ParDoPainel {
	for _, p := range a.semente.ParesDoPainel() {
		if p.ID == id {
			return &p
		}
	}
	return nil
}

// painel é o Aprender ou o Desfazer do painel: o arquivo muda, e a versão
// local sobe no ato.
func (a *aprendizE2E) painel(aprender bool, termo, variante string) {
	a.t.Helper()
	var err error
	if aprender {
		a.semente, _, err = a.semente.AprenderVariante(termo, variante)
	} else {
		a.semente, _, err = a.semente.DesfazerVariante(variante)
	}
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.fila.SubirVersaoLocal(context.Background(), []string{dictionary.IDDoPar(variante)}); err != nil {
		a.t.Fatal(err)
	}
}

// varredura vê a mudança daqui: a projeção e o marcador (em voo, só a versão).
func (a *aprendizE2E) varredura(id string) {
	a.t.Helper()
	proj := a.noArquivo(id)
	if err := a.fila.MudarAprendido(context.Background(), time.Now(), []string{id},
		func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			if e.Marcador != nil && e.Marcador.EmVoo() {
				return storage.AcaoDoAprendido{MudouAqui: true}
			}
			return storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: proj}, MudouAqui: true,
				Marcador: storage.MarcadorCriar}
		}); err != nil {
		a.t.Fatal(err)
	}
}

// enviar manda a fila do aprendido num lote; perder descarta a resposta (o
// push que a nuvem aplicou e o PC não ouviu).
func (a *aprendizE2E) enviar(perder bool) ([]syncengine.Mutation, []syncengine.DecisaoDoAprendido) {
	a.t.Helper()
	ctx := context.Background()
	itens, err := a.fila.PendentesDoAprendido(ctx, 10)
	if err != nil {
		a.t.Fatal(err)
	}
	var muts []syncengine.Mutation
	var envios []storage.Envio
	for _, it := range itens {
		id := it.Marcador.MutationID
		if id == "" {
			id = storage.NewID()
			envios = append(envios, storage.Envio{EntityType: storage.EntidadeAprendido, EntityID: it.ID, MutationID: id,
				Versao: it.VersaoLocal})
		}
		m, err := syncengine.MutacaoDoAprendido(it, id)
		if err != nil {
			a.t.Fatal(err)
		}
		muts = append(muts, m)
	}
	if err := a.fila.MarcarEnvio(ctx, envios); err != nil {
		a.t.Fatal(err)
	}
	res, err := a.cliente.Push(ctx, muts)
	if err != nil {
		a.t.Fatalf("o push: %v", err)
	}
	a.epoch = res.Epoch
	if perder {
		return muts, nil
	}
	porID := map[string]syncengine.MutationResult{}
	var ids []string
	for i, r := range res.Results {
		porID[itens[i].ID] = r
		ids = append(ids, itens[i].ID)
	}
	var ds []syncengine.DecisaoDoAprendido
	if err := a.fila.AplicarComAprendido(ctx, a.pc.aparelho, res.Epoch, nil, nil, nil, storage.RespostaDoAprendido{
		IDs: ids, Agora: time.Now(),
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			d := syncengine.DecidirResultadoDoAprendido(e, porID[e.ID])
			ds = append(ds, d)
			return d.Acao
		},
	}); err != nil {
		a.t.Fatalf("a resposta: %v", err)
	}
	return muts, ds
}

// terminar são os passos 2 e 3 do pull de um par: o lote, a intenção antes
// da gravação, e o espelho na transação da página.
func (a *aprendizE2E) terminar(plano syncengine.PlanoDoPar, epoch string, cursor *int64) syncengine.DecisaoDoAprendido {
	a.t.Helper()
	ctx := context.Background()
	var r *dictionary.ResultadoDoPar
	if plano.Lote {
		novo, lote, err := a.semente.AplicarPares([]dictionary.ParDaNuvem{plano.Par}, dictionary.ConferenciasDoLote{})
		if err != nil {
			a.t.Fatal(err)
		}
		res := lote.Resultados[0]
		if acao, ok := syncengine.IntencaoDoPar(plano, res); ok {
			if err := a.fila.MudarAprendido(ctx, time.Now(), []string{plano.ID},
				func(storage.EstadoDoAprendido) storage.AcaoDoAprendido { return acao }); err != nil {
				a.t.Fatal(err)
			}
		}
		a.semente, r = novo, &res
	}
	var d syncengine.DecisaoDoAprendido
	if err := a.fila.AplicarComAprendido(ctx, a.pc.aparelho, epoch, nil, cursor, nil, storage.RespostaDoAprendido{
		IDs: []string{plano.ID}, Agora: time.Now(),
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			d = syncengine.DecidirPar(e, plano, r)
			return d.Acao
		},
	}); err != nil {
		a.t.Fatalf("a página: %v", err)
	}
	return d
}

// receber puxa do cursor e decide cada par, um por vez.
func (a *aprendizE2E) receber() []syncengine.DecisaoDoAprendido {
	a.t.Helper()
	var ds []syncengine.DecisaoDoAprendido
	for {
		p, err := a.cliente.Pull(context.Background(), a.cursor, syncengine.LimiteDoPull)
		if err != nil {
			a.t.Fatalf("o pull: %v", err)
		}
		for _, c := range p.Changes {
			if c.EntityType != storage.EntidadeAprendido {
				continue
			}
			id := strings.ToLower(c.EntityID)
			plano := syncengine.PlanejarPar(a.estado(id), c, syncengine.ArquivoDoPar{Projecao: a.noArquivo(id)})
			seq := c.Seq
			ds = append(ds, a.terminar(plano, p.Epoch, &seq))
		}
		a.cursor, a.epoch = p.LastSeq, p.Epoch
		if !p.HasMore {
			return ds
		}
	}
}

// tentarDeNovo é a varredura aplicando o par da nuvem guardado.
func (a *aprendizE2E) tentarDeNovo(id string) syncengine.DecisaoDoAprendido {
	a.t.Helper()
	plano, ok := syncengine.PlanejarPendente(a.estado(id), syncengine.ArquivoDoPar{Projecao: a.noArquivo(id)})
	if !ok {
		a.t.Fatalf("nada pendente: %+v", a.estado(id))
	}
	return a.terminar(plano, a.epoch, nil)
}

func tipos(ds []syncengine.DecisaoDoAprendido) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Tipo)
	}
	return strings.Join(out, ",")
}

// O PC 1 aprende, o PC 2 recebe (D2: a palavra é criada); o PC 2 desfaz
// enquanto o PC 1, desconectado, desfaz e aprende de novo o mesmo par: o PC
// 1 perde na nuvem (o desfazer vence, D4), ganha a etiqueta e segue a nuvem.
// Depois, o PC 2 desfaz sem ouvir a resposta e reaprende: o reenvio repete o
// delete com o mesmo id, a nuvem responde duplicate, e o reaprender sobe com
// a base que a resposta trouxe e termina reaprendido, sem etiqueta. O PC 1
// recebe o reaprender, e a etiqueta dele dá lugar ao par aplicado.
func TestSyncE2E_AsDecisoesDoAprendido(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := novoAprendizE2E(t, n), novoAprendizE2E(t, n)
	sf := dictionary.IDDoPar("sf")

	// O PC 1 aprende "sf" -> "ECF", e sobe.
	pc1.painel(true, "ECF", "sf")
	pc1.varredura(sf)
	muts, ds := pc1.enviar(false)
	if tipos(ds) != syncengine.TipoAplicado || muts[0].Operation != syncengine.OpUpsert || muts[0].BaseVersion != 0 {
		t.Fatalf("o aprender do PC 1: %s %+v", tipos(ds), muts)
	}
	if e := pc1.estado(sf); e.ServerVersion == nil || *e.ServerVersion != 1 || e.MudouAqui() || e.Marcador != nil {
		t.Fatalf("o PC 1 depois do aprender: %+v", e)
	}

	// O PC 2 recebe: a palavra é criada, e o id entra no "veio da nuvem".
	if ds := pc2.receber(); tipos(ds) != syncengine.TipoAplicado {
		t.Fatalf("o PC 2 recebe: %s", tipos(ds))
	}
	if p := pc2.noArquivo(sf); p == nil || p.Termo != "ECF" {
		t.Fatalf("o arquivo do PC 2: %+v", p)
	}
	if e := pc2.estado(sf); !e.VeioDaNuvem || *e.ServerVersion != 1 || e.Situacao != storage.EspelhoAplicado {
		t.Errorf("o espelho do PC 2: %+v", e)
	}

	// O PC 2 desfaz (versão 2, desfeito).
	pc2.painel(false, "", "sf")
	pc2.varredura(sf)
	if muts, ds := pc2.enviar(false); tipos(ds) != syncengine.TipoAplicado || muts[0].Operation != syncengine.OpDelete ||
		muts[0].Payload != nil {
		t.Fatalf("o desfazer do PC 2: %s %+v", tipos(ds), muts)
	}

	// O PC 1, sem saber, desfaz e aprende de novo (o arquivo fica igual).
	pc1.painel(false, "", "sf")
	pc1.painel(true, "ECF", "sf")
	pc1.varredura(sf)
	muts, ds = pc1.enviar(false)
	if tipos(ds) != syncengine.TipoConflito || muts[0].BaseVersion != 1 {
		t.Fatalf("o PC 1 perde: %s %+v", tipos(ds), muts)
	}
	if e := pc1.estado(sf); e.Situacao != storage.EspelhoDesfeitoEmOutro || e.Marcador != nil || e.MudouAqui() {
		t.Fatalf("a etiqueta do PC 1: %+v", e)
	}
	// O state passa a valer aqui (a varredura o tenta), e o pull da mesma
	// versão é eco.
	if d := pc1.tentarDeNovo(sf); d.Tipo != syncengine.TipoDesfeito {
		t.Fatalf("o PC 1 segue a nuvem: %+v", d)
	}
	if pc1.noArquivo(sf) != nil {
		t.Error("o par ficou no PC 1")
	}
	// O cursor do PC 1 está em 0: o pull traz o par uma vez, já na versão do
	// state que o PC 1 aplicou.
	if ds := pc1.receber(); tipos(ds) != syncengine.TipoEco {
		t.Errorf("o pull do PC 1: %s", tipos(ds))
	}
	if e := pc1.estado(sf); e.Situacao != storage.EspelhoDesfeitoEmOutro || *e.ServerVersion != 2 || !e.BaseDesfeita {
		t.Errorf("a etiqueta depois do pull: %+v", e)
	}

	// O PC 2 reaprende, desfaz sem ouvir a resposta, e reaprende de novo.
	pc2.painel(true, "ECF", "sf")
	pc2.varredura(sf)
	if _, ds := pc2.enviar(false); tipos(ds) != syncengine.TipoAplicado {
		t.Fatalf("o reaprender do PC 2: %s", tipos(ds))
	}
	pc2.painel(false, "", "sf")
	pc2.varredura(sf)
	perdido, _ := pc2.enviar(true)
	pc2.painel(true, "ECF", "sf")
	pc2.varredura(sf)
	reenvio, ds := pc2.enviar(false)
	if len(reenvio) != 1 || reenvio[0].MutationID != perdido[0].MutationID || reenvio[0].Operation != syncengine.OpDelete ||
		tipos(ds) != syncengine.TipoAplicado {
		t.Fatalf("o reenvio: %+v / %+v, %s", reenvio, perdido, tipos(ds))
	}
	e := pc2.estado(sf)
	if !e.BaseDesfeita || !e.MudouAqui() || e.Marcador != nil {
		t.Fatalf("o PC 2 depois do reenvio: %+v", e)
	}
	pc2.varredura(sf)
	muts, ds = pc2.enviar(false)
	if tipos(ds) != syncengine.TipoAplicado || muts[0].Operation != syncengine.OpUpsert || muts[0].BaseVersion != *e.ServerVersion {
		t.Fatalf("o reaprender depois do reenvio: %s %+v", tipos(ds), muts)
	}
	e = pc2.estado(sf)
	if e.BaseDesfeita || e.MudouAqui() || e.Situacao != storage.EspelhoNenhuma || e.Marcador != nil {
		t.Errorf("o PC 2 no fim: %+v", e)
	}

	// Na nuvem: reaprendido, sem etiqueta para ninguém.
	ch := n.tudoNaConta(pc1.pc.token)[sf]
	if ch.PayloadAprendido == nil || ch.PayloadAprendido.UndoneAt != nil || ch.PayloadAprendido.Term != "ECF" ||
		ch.EntityVersion != *e.ServerVersion {
		t.Fatalf("na nuvem: %+v %+v", ch, ch.PayloadAprendido)
	}

	// O PC 1 recebe o reaprender: o par volta, e a etiqueta dá lugar a ele.
	if ds := pc1.receber(); tipos(ds) != syncengine.TipoAplicado {
		t.Fatalf("o PC 1 recebe o reaprender: %s", tipos(ds))
	}
	if p := pc1.noArquivo(sf); p == nil || p.Termo != "ECF" {
		t.Errorf("o arquivo do PC 1: %+v", p)
	}
	if e := pc1.estado(sf); e.Situacao != storage.EspelhoAplicado || *e.ServerVersion != ch.EntityVersion || e.Nuvem != nil {
		t.Errorf("o PC 1 no fim: %+v", e)
	}
}
