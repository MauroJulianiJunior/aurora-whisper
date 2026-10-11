package syncengine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A chegada mais rápida no PC (ADR-021, subfatia S): o laço busca a nuvem a
// cada 10 s enquanto o painel do PC lê o contador de mudanças, e a cada 60 s
// quando ele deixa de ler (depois de 15 s sem leitura); o contador sobe quando
// uma rodada grava no histórico algo que veio da nuvem.
//
// O laço roda aqui com um relógio falso: o Agora e o despertador são do teste, e
// o tempo só anda quando ele manda. Nenhuma espera de verdade.

// relogioFalso é o tempo do teste: a hora de agora e as esperas armadas pelo
// laço, que disparam quando o relógio chega nelas.
type relogioFalso struct {
	mu      sync.Mutex
	agora   time.Time
	esperas map[int]*esperaFalsa
	proximo int
	armadas int
}

type esperaFalsa struct {
	em time.Time
	c  chan time.Time
}

func novoRelogioFalso() *relogioFalso {
	return &relogioFalso{agora: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC), esperas: map[int]*esperaFalsa{}}
}

func (r *relogioFalso) Agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agora
}

// Armadas é quantas esperas o laço já armou, desde o começo.
func (r *relogioFalso) Armadas() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.armadas
}

// Despertador é o despertador do laço: uma espera zero ou negativa dispara já,
// como a do time.Timer.
func (r *relogioFalso) Despertador(d time.Duration) (<-chan time.Time, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.armadas++
	c := make(chan time.Time, 1)
	if d <= 0 {
		c <- r.agora
		return c, func() {}
	}
	id := r.proximo
	r.proximo++
	r.esperas[id] = &esperaFalsa{em: r.agora.Add(d), c: c}
	return c, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.esperas, id)
	}
}

// Avancar anda o relógio d e dispara as esperas que ele alcançou; devolve
// quantas.
func (r *relogioFalso) Avancar(d time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agora = r.agora.Add(d)
	n := 0
	for id, e := range r.esperas {
		if !e.em.After(r.agora) {
			e.c <- r.agora
			delete(r.esperas, id)
			n++
		}
	}
	return n
}

// bancada é o laço rodando sobre o relógio falso, com a nuvem de roteiro.
//
// O laço arma uma espera por volta (depois de cada rodada, de cada aviso e de
// cada abertura do painel), e é por isso que o teste sabe que ele terminou o
// que fazia: contar as esperas armadas. esperadas é quantas o teste já sabe que
// o laço tem de ter armado.
type bancada struct {
	t         *testing.T
	rel       *relogioFalso
	l         *Laco
	n         *nuvemDeRoteiro
	esperadas int
}

func novaBancada(t *testing.T, p pcDeTeste, n *nuvemDeRoteiro, ajustar func(*ConfigDoLaco)) *bancada {
	t.Helper()
	b := &bancada{t: t, rel: novoRelogioFalso(), n: n}
	cfg := ConfigDoLaco{
		Fila: p.fila, Conexao: conectada(), NovoID: novoMotor(p, n).NovoID,
		Cliente: func(string, string) SyncClient { return n },
		Agora:   b.rel.Agora,
	}
	if ajustar != nil {
		ajustar(&cfg)
	}
	b.l = NovoLaco(cfg)
	b.l.despertador = b.rel.Despertador
	ctx, cancel := context.WithCancel(context.Background())
	feito := make(chan struct{})
	go func() { b.l.Rodar(ctx); close(feito) }()
	t.Cleanup(func() { cancel(); <-feito })
	// A primeira rodada é na subida: a espera de 0 e, depois da rodada, a da
	// busca seguinte.
	b.esperadas = 2
	b.assentar()
	return b
}

// assentar espera o laço armar o que o teste sabe que ele tem de armar.
func (b *bancada) assentar() {
	b.t.Helper()
	esperarAte(b.t, "o laço armar a espera", func() bool { return b.rel.Armadas() >= b.esperadas })
}

// avancar anda o relógio d. Se uma espera disparou, o laço fez a rodada e armou
// a seguinte, e o teste espera por isso.
func (b *bancada) avancar(d time.Duration) {
	b.t.Helper()
	if b.rel.Avancar(d) > 0 {
		b.esperadas++
		b.assentar()
	}
}

// ler é o painel lendo o contador. armadas é quantas esperas novas o laço arma
// por causa disso: nenhuma quando o painel já estava aberto; uma quando ele
// acabou de abrir (o laço refaz a conta da próxima busca); duas quando, além
// disso, a próxima busca já devia ter saído (a rodada vem e arma a seguinte).
func (b *bancada) ler(armadas int) uint64 {
	b.t.Helper()
	v := b.l.LerMudancas()
	b.esperadas += armadas
	b.assentar()
	return v
}

func (b *bancada) pulls() int { return b.n.contar("pull") }

func (b *bancada) quero(oQue string, pulls int) {
	b.t.Helper()
	if got := b.pulls(); got != pulls {
		b.t.Fatalf("%s: %d buscas à nuvem; quero %d (relógio em +%s)", oQue, got, pulls, b.rel.Agora().Sub(novoRelogioFalso().Agora()))
	}
}

func nuvemQuieta() *nuvemDeRoteiro {
	return &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
}

// O laço busca a cada 10 s enquanto o painel lê, e a cada 60 s depois de 15 s
// sem leitura. A prova da S, com o relógio falso e os padrões de verdade (sem
// ajustar nenhum intervalo).
func TestLaco_Painel_10sComOPainelLendo_60sDepoisDe15sSemLer(t *testing.T) {
	b := novaBancada(t, novoPC(t), nuvemQuieta(), nil)
	b.quero("na subida", 1)

	// Sem o painel, a busca seguinte é a de 60 s. O painel abre: a próxima busca
	// passa a ser a de 10 s, contada do fim da última rodada.
	b.ler(1)

	// O painel lê a cada 3 s, e o relógio anda de 1 em 1 s: a busca sai nos 10
	// s, nos 20 s e nos 30 s.
	for s := 1; s <= 30; s++ {
		b.avancar(time.Second)
		b.quero("painel lendo, aos "+time.Duration(s*int(time.Second)).String(), 1+s/10)
		if s%3 == 0 {
			b.ler(0)
		}
	}
	// O painel deixa de ler no 30 s (a última leitura). A rodada dos 40 s ainda
	// o vê aberto (10 s desde a leitura) e espera 10 s; a dos 50 s não (20 s) e
	// espera 60.
	b.avancar(9 * time.Second)
	b.quero("aos 39 s", 4)
	b.avancar(time.Second)
	b.quero("aos 40 s", 5)
	b.avancar(10 * time.Second)
	b.quero("aos 50 s", 6)

	// Daí em diante, 60 s: nada nos 59 s seguintes, e a busca nos 60.
	b.avancar(59 * time.Second)
	b.quero("aos 109 s, sem ler há 79 s", 6)
	b.avancar(time.Second)
	b.quero("aos 110 s", 7)
	b.avancar(59 * time.Second)
	b.quero("aos 169 s", 7)
	b.avancar(time.Second)
	b.quero("aos 170 s", 8)
}

// Sem nunca ninguém ler, o laço é o de sempre: 60 s.
func TestLaco_Painel_SemLeituraSao60s(t *testing.T) {
	b := novaBancada(t, novoPC(t), nuvemQuieta(), nil)
	b.avancar(59 * time.Second)
	b.quero("aos 59 s", 1)
	b.avancar(time.Second)
	b.quero("aos 60 s", 2)
	b.avancar(59 * time.Second)
	b.quero("aos 119 s", 2)
	b.avancar(time.Second)
	b.quero("aos 120 s", 3)
}

// O painel que abre no meio dos 60 s não espera o que sobrava deles: a busca
// seguinte é a de 10 s contada do fim da última rodada.
func TestLaco_Painel_AbrirAdiantaABuscaSeguinte(t *testing.T) {
	t.Run("logo depois de uma rodada", func(t *testing.T) {
		b := novaBancada(t, novoPC(t), nuvemQuieta(), nil)
		b.avancar(3 * time.Second)
		b.ler(1)
		b.quero("depois da leitura", 1)
		b.avancar(6 * time.Second)
		b.quero("aos 9 s", 1)
		b.avancar(time.Second)
		b.quero("aos 10 s", 2)
	})

	t.Run("passados os 10 s: a busca sai agora", func(t *testing.T) {
		b := novaBancada(t, novoPC(t), nuvemQuieta(), nil)
		b.avancar(30 * time.Second)
		b.quero("aos 30 s", 1)
		// A espera adiantada já passou: o despertador dispara na hora, a rodada
		// vem e arma a seguinte (duas esperas).
		b.ler(2)
		b.quero("na abertura", 2)
		b.avancar(9 * time.Second)
		b.quero("aos 39 s", 2)
		b.avancar(time.Second)
		b.quero("aos 40 s", 3)
	})
}

// A leitura que não abre o painel (ele já estava aberto) não acorda o laço nem
// muda a espera: o painel lê a cada 3 s para sempre sem custo.
func TestLaco_Painel_LerComOPainelAbertoNaoMexeNoLaco(t *testing.T) {
	b := novaBancada(t, novoPC(t), nuvemQuieta(), nil)
	b.ler(1)
	armadas := b.rel.Armadas()
	for i := 0; i < 20; i++ {
		b.l.LerMudancas()
	}
	time.Sleep(20 * time.Millisecond)
	if got := b.rel.Armadas(); got != armadas {
		t.Errorf("%d leituras com o painel aberto armaram %d esperas", 20, got-armadas)
	}
}

// Depois de uma falha, a espera é a do backoff: abrir o painel não a fura (a
// rede caída não vira uma busca por leitura).
func TestLaco_Painel_NaoFuraAEsperaDepoisDeFalhar(t *testing.T) {
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
	falhas := 1
	n.push = func(muts []Mutation) (PushResult, error) {
		if falhas > 0 {
			falhas--
			return PushResult{}, transitorio(CodigoRede)
		}
		return aplicaTudo("e1")(muts)
	}
	b := novaBancada(t, p, n, func(c *ConfigDoLaco) {
		// A primeira falha espera 20 s exatos: metade fixa de 40 s, sem jitter.
		c.Espera = BackoffPolicy{Base: 40 * time.Second, Max: 40 * time.Second, Rand: func() float64 { return 0 }}
	})
	if got := n.contar("push"); got != 1 {
		t.Fatalf("%d pushes na subida; quero 1", got)
	}
	b.avancar(2 * time.Second)
	// O painel abre: o laço acorda, refaz a conta e continua esperando.
	b.ler(1)
	b.avancar(17 * time.Second)
	if got := n.contar("push"); got != 1 {
		t.Fatalf("aos 19 s: %d pushes; quero 1 (o painel não fura a espera)", got)
	}
	b.avancar(time.Second)
	if got := n.contar("push"); got != 2 || b.pulls() != 1 {
		t.Fatalf("aos 20 s: %d pushes e %d buscas; quero 2 e 1", got, b.pulls())
	}
}

// Desconectado, o painel aberto não faz o laço girar a cada 10 s: não há o que
// buscar, e quem conecta acorda o laço (Avisar).
func TestLaco_Painel_DesconectadoNaoGira(t *testing.T) {
	p := novoPC(t)
	n := nuvemQuieta()
	con := &conexaoDeTeste{cred: Credencial{Configurada: true, URL: "https://nuvem.exemplo.test"}}
	b := novaBancada(t, p, n, func(c *ConfigDoLaco) { c.Conexao = con })
	armadas := func(quero int, quando string) {
		t.Helper()
		if got := b.rel.Armadas(); got != quero {
			t.Fatalf("%s: o laço armou %d esperas; quero %d", quando, got, quero)
		}
	}
	// A subida armou duas (a da rodada e a de 60 s); o painel que abre, uma.
	b.ler(1)
	b.avancar(30 * time.Second)
	armadas(3, "aos 30 s, com o painel aberto")

	// Aos 60 s sai a rodada de sempre, e o painel está aberto (lê desde os 50
	// s). Depois dela, o laço desconectado espera 60 s, e não os 10 do painel.
	b.avancar(20 * time.Second)
	b.ler(1) // aos 50 s: a janela de 15 s já tinha fechado; o painel abre de novo
	b.avancar(8 * time.Second)
	b.ler(0)
	b.avancar(2 * time.Second)
	armadas(5, "aos 60 s, na rodada desconectada")
	for s := 70; s <= 110; s += 10 {
		b.avancar(10 * time.Second)
		b.ler(0)
	}
	armadas(5, "aos 110 s, com o painel lendo")
	b.avancar(10 * time.Second)
	armadas(6, "aos 120 s")
	if got := len(n.pedidos); got != 0 {
		t.Errorf("desconectado: %d pedidos à nuvem", got)
	}
}

// "30 s sempre" (a alternativa da ADR-021, escolha menor 17) é só configurar os
// dois intervalos iguais; e o do painel nunca passa do de sempre.
func TestLaco_Painel_OsIntervalos(t *testing.T) {
	l := NovoLaco(ConfigDoLaco{Fila: novoPC(t).fila, Conexao: conectada(), NovoID: func() string { return "" }})
	if l.cfg.Intervalo != 60*time.Second || l.cfg.IntervaloComPainel != 10*time.Second || l.cfg.JanelaDoPainel != 15*time.Second {
		t.Errorf("padrões: %s, %s, %s", l.cfg.Intervalo, l.cfg.IntervaloComPainel, l.cfg.JanelaDoPainel)
	}
	for _, c := range []struct {
		nome                         string
		intervalo, comPainel, quando time.Duration
	}{
		{"30 s sempre", 30 * time.Second, 30 * time.Second, 30 * time.Second},
		{"o do painel nunca passa do de sempre", 5 * time.Second, time.Minute, 5 * time.Second},
	} {
		l := NovoLaco(ConfigDoLaco{Fila: novoPC(t).fila, Conexao: conectada(), NovoID: func() string { return "" },
			Intervalo: c.intervalo, IntervaloComPainel: c.comPainel})
		if l.cfg.IntervaloComPainel != c.quando {
			t.Errorf("%s: %s; quero %s", c.nome, l.cfg.IntervaloComPainel, c.quando)
		}
	}
}

// Uma leitura vale por 15 s: aos 14,999 s o painel ainda está aberto; aos 15 s
// já não; uma leitura de nunca, ou de um relógio que voltou, não vale.
func TestLaco_Painel_AJanelaDeLeitura(t *testing.T) {
	l := NovoLaco(ConfigDoLaco{Fila: novoPC(t).fila, Conexao: conectada(), NovoID: func() string { return "" }})
	lido := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		nome   string
		lidoEm int64
		agora  time.Time
		quero  bool
	}{
		{"nunca lido", 0, lido, false},
		{"agora mesmo", lido.UnixNano(), lido, true},
		{"há 14,999 s", lido.UnixNano(), lido.Add(15*time.Second - time.Millisecond), true},
		{"há 15 s", lido.UnixNano(), lido.Add(15 * time.Second), false},
		{"há 2 min", lido.UnixNano(), lido.Add(2 * time.Minute), false},
		{"o relógio voltou", lido.UnixNano(), lido.Add(-time.Second), false},
	} {
		if got := l.aberto(c.lidoEm, c.agora); got != c.quero {
			t.Errorf("%s: aberto = %v; quero %v", c.nome, got, c.quero)
		}
	}
}

// LerMudancas nunca bloqueia, com o laço parado ou não: é a goroutine de HTTP.
func TestLaco_Painel_LerMudancasNuncaBloqueia(t *testing.T) {
	l := NovoLaco(ConfigDoLaco{Fila: novoPC(t).fila, Conexao: conectada(), NovoID: func() string { return "" }})
	feito := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			l.LerMudancas()
		}
		close(feito)
	}()
	select {
	case <-feito:
	case <-time.After(2 * time.Second):
		t.Fatal("LerMudancas bloqueou com o laço parado")
	}
}

// ---- O contador ----

const idCtr = "0199a000-0000-7000-8000-000000000201"

func ditadoDaNuvem(id, texto string, versao int64) *Item {
	criado := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	return &Item{ID: id, SourcePlatform: "web", RawText: texto, FinalText: texto, Mode: "smart", ASRProvider: "cloud",
		Status: "done", CreatedAt: criado, UpdatedAt: criado, Version: versao}
}

// lacoDeRodadas é um laço sem goroutine: o teste roda uma rodada por vez
// (RodadaAgora) contra uma nuvem que devolve as páginas do roteiro, uma por
// busca, e depois páginas vazias.
func lacoDeRodadas(t *testing.T, p pcDeTeste, n *nuvemDeRoteiro) *Laco {
	t.Helper()
	return NovoLaco(ConfigDoLaco{Fila: p.fila, Conexao: conectada(), NovoID: novoMotor(p, n).NovoID,
		Cliente: func(string, string) SyncClient { return n }})
}

func roteiroDeBuscas(paginas ...ChangePage) func(int64) (ChangePage, error) {
	var mu sync.Mutex
	chamada := 0
	return func(after int64) (ChangePage, error) {
		mu.Lock()
		defer mu.Unlock()
		chamada++
		if chamada <= len(paginas) {
			return paginas[chamada-1], nil
		}
		return ChangePage{LastSeq: after, Epoch: "e1"}, nil
	}
}

// O contador sobe uma vez por rodada que grava no histórico algo que veio da
// nuvem (o ditado novo ou mudado, a lápide que apaga a linha) e não sobe nas
// que não gravam nada.
func TestLaco_Mudancas_SobeQuandoOSyncAplicaAlgoDaNuvem(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	n := &nuvemDeRoteiro{push: aplicaTudo("e1")}
	n.pull = roteiroDeBuscas(
		ChangePage{LastSeq: 0, Epoch: "e1"}, // a primeira: nada
		ChangePage{LastSeq: 5, Epoch: "e1", Changes: []Change{{Seq: 5, EntityType: storage.EntidadeDitado, EntityID: idCtr,
			Operation: OpUpsert, EntityVersion: 3, Payload: ditadoDaNuvem(idCtr, "ditado no celular", 3)}}},
		ChangePage{LastSeq: 5, Epoch: "e1"}, // nada
		ChangePage{LastSeq: 6, Epoch: "e1", Changes: []Change{{Seq: 6, EntityType: storage.EntidadeDitado, EntityID: idCtr,
			Operation: OpUpsert, EntityVersion: 4, Payload: ditadoDaNuvem(idCtr, "editado no celular", 4)}}},
		ChangePage{LastSeq: 7, Epoch: "e1", Changes: []Change{{Seq: 7, EntityType: storage.EntidadeDitado, EntityID: idCtr,
			Operation: OpTombstone}}},
		// A lápide de um ditado que já não está aqui é eco: não grava nada.
		ChangePage{LastSeq: 8, Epoch: "e1", Changes: []Change{{Seq: 8, EntityType: storage.EntidadeDitado, EntityID: idCtr,
			Operation: OpTombstone}}},
	)
	l := lacoDeRodadas(t, p, n)
	passos := []struct {
		oQue  string
		conta uint64
	}{
		{"uma busca sem nada", 0},
		{"um ditado novo do celular", 1},
		{"uma busca sem nada, de novo", 1},
		{"a edição feita no celular", 2},
		{"a lixeira do celular apagou a linha", 3},
		{"a lápide de um ditado que já não está aqui", 3},
	}
	if got := l.LerMudancas(); got != 0 {
		t.Fatalf("antes de qualquer rodada: %d", got)
	}
	for _, passo := range passos {
		if err := l.RodadaAgora(ctx); err != nil {
			t.Fatalf("%s: %v", passo.oQue, err)
		}
		if got := l.LerMudancas(); got != passo.conta {
			t.Fatalf("%s: o contador é %d; quero %d", passo.oQue, got, passo.conta)
		}
	}
}

// O que o push traz da nuvem e muda a lista também sobe o contador: a lápide
// que ele devolve (apagar vence) e o estado que venceu um conflito.
func TestLaco_Mudancas_SobeComAQueOPushTrouxe(t *testing.T) {
	ctx := context.Background()

	t.Run("a nuvem diz que o ditado foi apagado", func(t *testing.T) {
		p := novoPC(t)
		p.ditar(t, idM1, "um")
		n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
		n.push = func(muts []Mutation) (PushResult, error) {
			r := PushResult{Epoch: "e1"}
			for _, m := range muts {
				r.Results = append(r.Results, MutationResult{MutationID: m.MutationID, Status: StatusGone, Motivo: MotivoApagado})
			}
			return r, nil
		}
		l := lacoDeRodadas(t, p, n)
		if err := l.RodadaAgora(ctx); err != nil {
			t.Fatal(err)
		}
		if got := l.LerMudancas(); got != 1 {
			t.Errorf("o contador é %d; quero 1", got)
		}
	})

	t.Run("o conflito que traz o estado da nuvem", func(t *testing.T) {
		p := novoPC(t)
		p.ditar(t, idM1, "um")
		n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
		n.push = func(muts []Mutation) (PushResult, error) {
			r := PushResult{Epoch: "e1"}
			for _, m := range muts {
				r.Results = append(r.Results, MutationResult{MutationID: m.MutationID, Status: StatusConflict, EntityVersion: 4,
					State: ditadoDaNuvem(idM1, "o texto que venceu", 4)})
			}
			return r, nil
		}
		l := lacoDeRodadas(t, p, n)
		if err := l.RodadaAgora(ctx); err != nil {
			t.Fatal(err)
		}
		if got := l.LerMudancas(); got != 1 {
			t.Errorf("o contador é %d; quero 1", got)
		}
		if lido, err := p.repo.Get(ctx, donoDoMotor, idM1); err != nil || lido.FinalText != "o texto que venceu" {
			t.Errorf("o texto que venceu não foi gravado: %q, %v", lido.FinalText, err)
		}
	})

	t.Run("o push aplicado, só do que este PC fez, não sobe", func(t *testing.T) {
		p := novoPC(t)
		p.ditar(t, idM1, "um")
		n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
		l := lacoDeRodadas(t, p, n)
		if err := l.RodadaAgora(ctx); err != nil {
			t.Fatal(err)
		}
		if got := l.LerMudancas(); got != 0 {
			t.Errorf("o ditado deste PC que subiu mexeu no contador: %d", got)
		}
	})
}

// A rodada que aplicou páginas e falhou na seguinte já mexeu no histórico: o
// contador sobe, e o painel relê.
func TestLaco_Mudancas_SobeNaRodadaQueFalhouDepoisDeAplicar(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	chamadas := 0
	n := &nuvemDeRoteiro{push: aplicaTudo("e1")}
	n.pull = func(after int64) (ChangePage, error) {
		chamadas++
		if chamadas == 1 {
			return ChangePage{LastSeq: 5, Epoch: "e1", HasMore: true, Changes: []Change{{Seq: 5, EntityType: storage.EntidadeDitado,
				EntityID: idCtr, Operation: OpUpsert, EntityVersion: 3, Payload: ditadoDaNuvem(idCtr, "veio na primeira página", 3)}}}, nil
		}
		return ChangePage{}, transitorio(CodigoRede)
	}
	l := lacoDeRodadas(t, p, n)
	if err := l.RodadaAgora(ctx); err == nil {
		t.Fatal("a rodada deu certo")
	}
	if got := l.LerMudancas(); got != 1 {
		t.Errorf("o contador é %d; quero 1", got)
	}
}

// A conta do que a rodada aplicou vem só das decisões que gravam no histórico.
func TestRelatorio_AplicouDaNuvem(t *testing.T) {
	for _, c := range []struct {
		nome  string
		rel   func(*Relatorio)
		quero int
	}{
		{"vazia", func(*Relatorio) {}, 0},
		{"recebido", func(r *Relatorio) { r.Pull[TipoRecebido] = 2 }, 2},
		{"apagado no pull", func(r *Relatorio) { r.Pull[TipoApagado] = 1 }, 1},
		{"apagado no push", func(r *Relatorio) { r.Push[TipoApagado] = 1 }, 1},
		{"conflito", func(r *Relatorio) { r.Push[TipoConflito] = 1 }, 1},
		{"o resto não conta", func(r *Relatorio) {
			r.Pull[TipoPulado], r.Pull[TipoEco], r.Pull[TipoNaoAplicado], r.Pull[TipoReconciliado] = 1, 1, 1, 1
			r.Push[TipoAplicado], r.Push[TipoMesclado], r.Push[TipoRecusado], r.Push[TipoReenviar], r.Push[TipoForaDaJanela] = 1, 1, 1, 1, 1
			r.PullDoAprendido[TipoAplicado] = 3
			r.Enviados, r.Recebidos = 5, 5
		}, 0},
	} {
		r := novoRelatorio()
		c.rel(&r)
		if got := r.AplicouDaNuvem(); got != c.quero {
			t.Errorf("%s: %d; quero %d", c.nome, got, c.quero)
		}
	}
}
