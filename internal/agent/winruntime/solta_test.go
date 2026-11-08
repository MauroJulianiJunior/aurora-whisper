package winruntime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A gravação solta no runtime (ADR-025, D2). Tudo com dublês: o teclado e o
// mouse são o tecladoFalso, o foco é um focoMutavel, o microfone, o ASR, o
// histórico e a pílula são falsos, e a inserção para na fila (rt.insercoes),
// sem chegar à thread que escreveria no campo. Nada aqui instala hook, injeta
// tecla ou clique, ou escreve no clipboard.

const ditadoSolto = "Reunião remarcada para quinta às dez."

// focoMutavel é o foco que muda no meio da gravação, como muda quando a pessoa
// clica noutro campo.
// leituras conta quantas vezes o runtime perguntou.
type focoMutavel struct {
	alvo     dictation.FocusTarget
	leituras int
}

func (f *focoMutavel) Current() dictation.FocusTarget { f.leituras++; return f.alvo }

type bancadaSolta struct {
	*bancada
	tec      *tecladoFalso
	foco     *focoMutavel
	hist     *historicoFalso
	dominios []string
	t0       time.Time
}

func novaBancadaSolta(t *testing.T) *bancadaSolta {
	t.Helper()
	machine, err := dictation.NewMachine(dictation.Config{
		Key:           dictation.KeyUnknown,
		Modifiers:     dictation.ModControl | dictation.ModMeta,
		HoldThreshold: 700 * time.Millisecond,
		MaxRecording:  dictation.DefaultMaxRecording,
		BeepEnabled:   true,
	})
	if err != nil {
		t.Fatalf("dictation.NewMachine: %v", err)
	}
	b := &bancadaSolta{
		bancada: novaBancada(t, true),
		tec:     &tecladoFalso{eventos: make(chan dictation.RawKeyEvent), cliques: make(chan struct{}, 1)},
		foco:    &focoMutavel{alvo: dictation.FocusTarget{ProcessName: "editor.exe", ControlType: "Edit"}},
		hist:    &historicoFalso{},
		t0:      time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC),
	}
	b.rt.machine = machine
	b.rt.hook = b.tec
	b.rt.focus = b.foco
	b.rt.asr = &asrFalso{texto: ditadoSolto}
	b.rt.UsarHistorico(b.hist, provedorDeTeste, modeloDeTeste, "mauro")
	b.rt.UsarResolver(resolver.NewSmartResolver(nil), "mauro")
	b.rt.UsarDominioDoApp(func(p string) *string { b.dominios = append(b.dominios, p); return nil })
	b.rt.vaga, b.rt.insercoes = vagaLivre(), make(chan dictation.InsertEffect, 1)
	return b
}

// tecla manda ao runtime um evento do gatilho como o hook mandaria: com o
// instante (ms depois de t0) e as marcas da decisão.
func (b *bancadaSolta) tecla(down bool, ms int, limpo, paraNota bool) {
	b.rt.handleRaw(dictation.RawKeyEvent{
		Down: down, At: b.t0.Add(time.Duration(ms) * time.Millisecond), Limpo: limpo, ParaNota: paraNota,
	})
}

// ligar dá os dois toques limpos e solta o segundo: a gravação fica solta.
func (b *bancadaSolta) ligar(t *testing.T) dictation.AttemptID {
	t.Helper()
	b.tecla(true, 0, false, false)
	b.tecla(false, 80, true, false)
	b.tecla(true, 300, false, false)
	b.tecla(false, 380, true, false)
	if !b.rt.machine.GravandoSemSegurar() {
		t.Fatalf("dois toques limpos: a gravação não ficou solta (estado %v)", b.rt.machine.State())
	}
	if len(b.tec.soltas) != 1 || !b.tec.soltas[0] {
		t.Fatalf("pedidos ao hook %v, quero um só, de ligar", b.tec.soltas)
	}
	return b.rt.machine.CurrentAttempt()
}

// clicar faz o que Run faz com o clique do hook do mouse.
func (b *bancadaSolta) clicar() {
	b.rt.dispatch(dictation.CliqueEvent{Attempt: b.rt.machine.CurrentAttempt()})
}

// transcrever leva a tentativa parada da captura à inserção: a captura volta,
// o ASR responde, e a resolução (Smart, sem dicionário) chega ao InsertEffect.
func (b *bancadaSolta) transcrever(t *testing.T) {
	t.Helper()
	select {
	case c := <-b.rt.capturas:
		b.rt.dispatch(b.rt.concluirCaptura(c))
	case <-time.After(2 * time.Second):
		t.Fatal("a captura não voltou")
	}
	select {
	case ev := <-b.rt.pending:
		b.rt.aoReceberPendente(ev)
	case <-time.After(2 * time.Second):
		t.Fatal("o ASR não voltou")
	}
}

// TestSolta_CliqueParaEEscreveNoCampoDeAgora: o clique para a gravação solta,
// o hook desliga o Esc e o mouse, e o texto vai para a fila de inserção. O
// dicionário é o do programa onde o cursor está na hora de escrever, não o do
// começo (ADR-025, C.1).
func TestSolta_CliqueParaEEscreveNoCampoDeAgora(t *testing.T) {
	b := novaBancadaSolta(t)
	b.ligar(t)

	b.foco.alvo = dictation.FocusTarget{ProcessName: "planilha.exe", ControlType: "Edit"}
	b.clicar()
	if got := b.tec.soltas; len(got) != 2 || got[1] {
		t.Fatalf("pedidos ao hook %v, quero ligar e desligar", got)
	}
	b.transcrever(t)

	ins := pedidosDeInsercao(b.rt)
	if len(ins) != 1 || ins[0].Text != ditadoSolto {
		t.Fatalf("pedidos de inserção %+v, quero o ditado", ins)
	}
	if len(b.dominios) != 1 || b.dominios[0] != "planilha.exe" {
		t.Fatalf("o dicionário foi escolhido por %v, quero o programa de agora (planilha.exe)", b.dominios)
	}
	if !strings.Contains(b.log.String(), "fim=clique") {
		t.Errorf("o log não diz que o clique parou:\n%s", b.log.String())
	}
}

// TestSolta_CadeadoAntesDaFase: o cadeado liga antes do "Gravando" e desliga
// antes do "Processando" (ADR-025, D3), e o ditado de sempre nunca o liga.
func TestSolta_CadeadoAntesDaFase(t *testing.T) {
	b := novaBancadaSolta(t)
	b.ligar(t)
	b.clicar()

	p := b.pilula
	if len(p.soltas) != 2 || !p.soltas[0] || p.soltas[1] {
		t.Fatalf("pedidos do cadeado %v, quero ligar e desligar", p.soltas)
	}
	gravando, processando := -1, -1
	for i, f := range p.fases {
		if f == dictation.OverlayRecording && gravando < 0 {
			gravando = i
		}
		if f == dictation.OverlayProcessing && processando < 0 {
			processando = i
		}
	}
	if gravando < 0 || processando < 0 {
		t.Fatalf("fases %v, quero gravando e processando", p.fases)
	}
	if p.antesDa[0] > gravando {
		t.Errorf("o cadeado ligou depois de mostrar gravando (fases %v, cadeado na %d)", p.fases, p.antesDa[0])
	}
	if p.antesDa[1] > processando {
		t.Errorf("o cadeado desligou depois de mostrar processando (fases %v, cadeado na %d)", p.fases, p.antesDa[1])
	}

	h := novaBancadaSolta(t)
	h.tecla(true, 0, false, false)
	h.rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	h.rt.dispatch(<-h.rt.pending)
	h.tecla(false, 3000, true, false)
	if len(h.pilula.soltas) != 0 {
		t.Fatalf("o ditado de sempre pediu o cadeado: %v", h.pilula.soltas)
	}
}

// TestSolta_OHoldUsaOCampoDoComeco é o contraponto: no ditado de sempre, o
// dicionário continua o do programa onde a tecla foi apertada, e o campo é
// lido uma vez só, no aperto. A ADR-025 só muda a gravação solta.
func TestSolta_OHoldUsaOCampoDoComeco(t *testing.T) {
	b := novaBancadaSolta(t)
	b.tecla(true, 0, false, false)
	b.rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	b.rt.dispatch(<-b.rt.pending)
	b.foco.alvo = dictation.FocusTarget{ProcessName: "planilha.exe", ControlType: "Edit"}
	b.tecla(false, 3000, true, false)
	b.transcrever(t)

	if len(b.dominios) != 1 || b.dominios[0] != "editor.exe" {
		t.Fatalf("o dicionário foi escolhido por %v, quero o do começo (editor.exe)", b.dominios)
	}
	if b.foco.leituras != 1 {
		t.Fatalf("o campo foi lido %d vezes, quero uma, no aperto", b.foco.leituras)
	}
	if len(b.tec.soltas) != 0 {
		t.Fatalf("o ditado de sempre pediu %v ao hook, quero nada", b.tec.soltas)
	}
	if len(pedidosDeInsercao(b.rt)) != 1 {
		t.Fatal("o ditado de sempre não chegou à inserção")
	}
}

// TestSolta_CampoProtegidoNoFimFicaNoHistorico: a gravação começou num campo
// comum e terminou com o cursor num de senha. O texto não é escrito, fica no
// histórico, e a pílula diz isso (ADR-025, C.2). O histórico não diz por quê.
func TestSolta_CampoProtegidoNoFimFicaNoHistorico(t *testing.T) {
	b := novaBancadaSolta(t)
	b.ligar(t)
	b.clicar()
	b.foco.alvo = dictation.FocusTarget{ProcessName: "banco.exe", ControlType: "Edit", Protected: true}
	b.transcrever(t)

	if ins := pedidosDeInsercao(b.rt); len(ins) != 0 {
		t.Fatalf("o texto foi para a inserção num campo protegido: %+v", ins)
	}
	l := b.hist.unica(t)
	if l.FinalText != ditadoSolto || l.Status != "error" {
		t.Fatalf("histórico: final %q status %q, quero o ditado, como inserção que falhou", l.FinalText, l.Status)
	}
	if f := b.pilula.ultima(t); f != overlay.FaseCampoProtegidoNoHistorico {
		t.Fatalf("pílula %v, quero FaseCampoProtegidoNoHistorico", f)
	}
	if got := b.rt.machine.State(); got != dictation.StateError {
		t.Fatalf("estado %v, quero StateError (a inserção não aconteceu)", got)
	}
	if strings.Contains(b.log.String(), "banco.exe") {
		t.Errorf("o nome do programa foi para o log:\n%s", b.log.String())
	}
}

// TestSolta_EscDescartaSemASREsemHistorico: o Esc chega do hook marcado para
// descartar e vai ao núcleo como EscEvent. Nada vai ao ASR, nada é escrito,
// nada entra no histórico, e o hook desliga o Esc e o mouse.
func TestSolta_EscDescartaSemASREsemHistorico(t *testing.T) {
	b := novaBancadaSolta(t)
	b.ligar(t)
	b.rt.handleRaw(dictation.RawKeyEvent{Key: dictation.KeyEscape, Down: true, At: b.t0.Add(2 * time.Second), Descartar: true})

	if got := b.rt.machine.State(); got != dictation.StateIdle || b.rt.machine.CurrentAttempt() != dictation.NoAttempt {
		t.Fatalf("estado %v tentativa %v, quero parado", got, b.rt.machine.CurrentAttempt())
	}
	if got := b.tec.soltas; len(got) != 2 || got[1] {
		t.Fatalf("pedidos ao hook %v, quero ligar e desligar", got)
	}
	select {
	case <-b.rt.capturas:
		t.Fatal("a captura foi parada para transcrever: o descarte foi ao ASR")
	case <-time.After(50 * time.Millisecond):
	}
	if len(b.hist.linhas) != 0 {
		t.Fatalf("o descarte entrou no histórico: %+v", b.hist.linhas)
	}
	if f := b.pilula.ultima(t); f != dictation.OverlayHidden {
		t.Fatalf("pílula %v, quero escondida", f)
	}
	if !strings.Contains(b.log.String(), "fim=descartada") {
		t.Errorf("o log não diz que a gravação foi descartada:\n%s", b.log.String())
	}
}

// TestSolta_EscSemTentativaNaoFazNada: um Esc marcado que chega depois de a
// gravação acabar não acha tentativa, e nada acontece.
func TestSolta_EscSemTentativaNaoFazNada(t *testing.T) {
	b := novaBancadaSolta(t)
	b.rt.handleRaw(dictation.RawKeyEvent{Key: dictation.KeyEscape, Down: true, Descartar: true})
	if got := b.rt.machine.State(); got != dictation.StateIdle || len(b.pilula.fases) != 0 || len(b.tec.soltas) != 0 {
		t.Fatalf("Esc sem tentativa: estado %v, pílula %v, hook %v; quero nada", got, b.pilula.fases, b.tec.soltas)
	}
}

// TestSolta_CtrlWinAltNoMeioNaoVaiParaANota: no meio da gravação solta, um
// Ctrl+Win+Alt não para (não é toque limpo) e não pode mandar o texto para uma
// nota. O clique para, e o texto vai para o campo.
func TestSolta_CtrlWinAltNoMeioNaoVaiParaANota(t *testing.T) {
	b := novaBancadaSolta(t)
	notas := &notasFalsas{}
	b.rt.notas = notas.gravar
	b.ligar(t)

	b.tecla(true, 1000, false, true)
	b.tecla(false, 1100, false, true)
	if !b.rt.machine.GravandoSemSegurar() {
		t.Fatal("o Ctrl+Win+Alt parou a gravação solta")
	}
	b.clicar()
	b.transcrever(t)

	if len(notas.textos) != 0 {
		t.Fatalf("o texto foi para uma nota: %q", notas.textos)
	}
	if ins := pedidosDeInsercao(b.rt); len(ins) != 1 {
		t.Fatalf("pedidos de inserção %d, quero o texto no campo", len(ins))
	}
}

// TestSolta_SegundoApertoSeguradoComAltVaiParaANota: o segundo aperto,
// segurado além do tempo de segurar, é o ditado de sempre, e o Alt apertado no
// meio dele conta como em qualquer ditado: o texto vai para a nota.
func TestSolta_SegundoApertoSeguradoComAltVaiParaANota(t *testing.T) {
	b := novaBancadaSolta(t)
	notas := &notasFalsas{}
	b.rt.notas = notas.gravar
	b.tecla(true, 0, false, false)
	b.tecla(false, 80, true, false)
	b.tecla(true, 300, false, false)
	b.tecla(false, 1100, false, true) // segurou 800 ms, e o Alt entrou no meio

	if !strings.Contains(b.log.String(), "fim=segurou") {
		t.Fatalf("o log não diz que o segundo aperto foi segurado:\n%s", b.log.String())
	}
	b.transcrever(t)
	if len(notas.textos) != 1 || notas.textos[0] != ditadoSolto {
		t.Fatalf("a nota recebeu %q, quero o ditado", notas.textos)
	}
	if ins := pedidosDeInsercao(b.rt); len(ins) != 0 {
		t.Fatalf("o texto da nota foi para o campo: %+v", ins)
	}
}

// TestSolta_FalhaDoMouseVaiAoLog: se o hook do mouse não instalou, a gravação
// funciona sem o clique, e o log diz isso.
func TestSolta_FalhaDoMouseVaiAoLog(t *testing.T) {
	b := novaBancadaSolta(t)
	b.tec.erroDoMouse = errors.New("recusado")
	b.ligar(t)
	b.tecla(true, 5000, false, false)
	b.tecla(false, 5080, true, false)
	log := b.log.String()
	if !strings.Contains(log, "fim=toque") || !strings.Contains(log, "o hook do mouse falhou") {
		t.Fatalf("o log não diz o fim pelo toque e a falha do mouse:\n%s", log)
	}
}

// TestSolta_LogSoDizComoTerminou: as linhas da gravação solta levam a
// tentativa, se ligou e como terminou, e nada mais (ADR-025, F).
func TestSolta_LogSoDizComoTerminou(t *testing.T) {
	b := novaBancadaSolta(t)
	b.ligar(t)
	b.foco.alvo = dictation.FocusTarget{ProcessName: "planilha.exe", ControlType: "Edit"}
	b.clicar()
	b.transcrever(t)

	linhas := 0
	for _, l := range strings.Split(b.log.String(), "\n") {
		if !strings.Contains(l, `msg="winruntime: gravação solta"`) {
			continue
		}
		linhas++
		resto := l[strings.Index(l, `msg="winruntime: gravação solta"`)+len(`msg="winruntime: gravação solta"`):]
		var chaves []string
		for _, campo := range strings.Fields(resto) {
			chaves = append(chaves, campo[:strings.Index(campo, "=")])
		}
		if j := strings.Join(chaves, ","); j != "attempt,ligada" && j != "attempt,ligada,fim" {
			t.Errorf("linha com os campos %s:\n%s", j, l)
		}
	}
	if linhas != 2 {
		t.Fatalf("%d linha(s) da gravação solta, quero 2:\n%s", linhas, b.log.String())
	}
	for _, proibido := range []string{"planilha.exe", "editor.exe", ditadoSolto} {
		if strings.Contains(b.log.String(), proibido) {
			t.Errorf("%q no log:\n%s", proibido, b.log.String())
		}
	}
}

// TestRun_GravacaoSolta é a prova com o laço inteiro: Run consome o clique do
// hook do mouse e o leva ao núcleo, e o texto é inserido; depois, numa segunda
// gravação solta, o Esc vindo do hook descarta, sem inserir nem gravar nada.
func TestRun_GravacaoSolta(t *testing.T) {
	machine, err := dictation.NewMachine(dictation.Config{
		Key:           dictation.KeyUnknown,
		Modifiers:     dictation.ModControl | dictation.ModMeta,
		HoldThreshold: dictation.MinHoldThreshold,
		MaxRecording:  dictation.DefaultMaxRecording,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sem buffer: um envio só termina quando Run o recebe.
	tec := &tecladoFalso{eventos: make(chan dictation.RawKeyEvent), cliques: make(chan struct{})}
	ins := &insercaoRegistrada{textos: make(chan string, 8)}
	hist := &historicoCanal{linhas: make(chan storage.Transcription, 8)}
	saida := &escritaSegura{}
	rt := &Runtime{
		machine:       machine,
		clk:           clock.NewSystem(),
		hook:          tec,
		audio:         &microfoneFalso{pronto: true},
		overlay:       &pilulaFalsa{},
		focus:         fakeFocus{},
		inserter:      ins,
		asr:           &asrFalso{texto: ditadoSolto},
		log:           slog.New(slog.NewTextHandler(saida, nil)),
		timers:        make(map[timerKey]clock.Timer),
		pending:       make(chan dictation.Event, pendingBuffer),
		insercoes:     make(chan dictation.InsertEffect, 1),
		vaga:          vagaLivre(),
		capturas:      make(chan capturaPronta, 1),
		pararCapturas: make(chan struct{}),
		polidos:       make(chan polidoPronto, 1),
		bipar:         func() error { return nil },
	}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	ctx, cancel := context.WithCancel(context.Background())
	fim := make(chan error, 1)
	go func() { fim <- rt.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-fim:
		case <-time.After(2 * time.Second):
			t.Error("Run não saiu depois do cancelamento")
		}
	})

	enviar := func(ev dictation.RawKeyEvent) {
		t.Helper()
		select {
		case tec.eventos <- ev:
		case <-time.After(time.Second):
			t.Fatalf("o laço não consumiu %+v em 1 s", ev)
		}
	}
	esperarNoLog := func(trecho string, n int) {
		t.Helper()
		for prazo := time.Now().Add(3 * time.Second); time.Now().Before(prazo); time.Sleep(2 * time.Millisecond) {
			if strings.Count(saida.String(), trecho) >= n {
				return
			}
		}
		t.Fatalf("o log não chegou a %d× %q:\n%s", n, trecho, saida.String())
	}
	ligar := func(base time.Time, vez int) {
		t.Helper()
		ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
		enviar(dictation.RawKeyEvent{Down: true, At: ms(0)})
		enviar(dictation.RawKeyEvent{Down: false, At: ms(80), Limpo: true})
		enviar(dictation.RawKeyEvent{Down: true, At: ms(300)})
		enviar(dictation.RawKeyEvent{Down: false, At: ms(380), Limpo: true})
		esperarNoLog("ligada=true", vez)
	}

	ligar(time.Now(), 1)
	select {
	case tec.cliques <- struct{}{}:
	case <-time.After(time.Second):
		t.Fatal("o laço não consumiu o clique em 1 s")
	}
	select {
	case s := <-ins.textos:
		if s != ditadoSolto {
			t.Fatalf("inserido %q, quero o ditado", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("nada foi inserido depois do clique:\n%s", saida.String())
	}
	select {
	case h := <-hist.linhas:
		if h.Status != "done" || h.FinalText != ditadoSolto {
			t.Fatalf("histórico: status %q final %q", h.Status, h.FinalText)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nada foi gravado no histórico")
	}
	esperarNoLog("fim=clique", 1)

	ligar(time.Now().Add(time.Minute), 2)
	enviar(dictation.RawKeyEvent{Key: dictation.KeyEscape, Down: true, Descartar: true})
	esperarNoLog("fim=descartada", 1)
	select {
	case s := <-ins.textos:
		t.Fatalf("o descarte inseriu %q", s)
	case h := <-hist.linhas:
		t.Fatalf("o descarte entrou no histórico: %+v", h)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestFocoEhMenu: o modo de menu do Win32 basta; sem ele, decide o tipo do
// controle em foco.
func TestFocoEhMenu(t *testing.T) {
	for _, c := range []struct {
		tipo       string
		menuAberto bool
		quer       bool
	}{
		{"Menu", false, true},
		{"MenuBar", false, true},
		{"MenuItem", false, true},
		{"Edit", false, false},
		{"Document", false, false},
		{"", false, false},
		{"Edit", true, true}, // o menu aberto pelo clique, sem item destacado
	} {
		if got := focoEhMenu(dictation.FocusTarget{ControlType: c.tipo}, c.menuAberto); got != c.quer {
			t.Errorf("focoEhMenu(%q, menu aberto %v) = %v, quero %v", c.tipo, c.menuAberto, got, c.quer)
		}
	}
}

// terminarComMenu liga a gravação solta, para pelo clique e transcreve,
// com o lugar do cursor e o modo de menu que o teste der.
func terminarComMenu(t *testing.T, alvo dictation.FocusTarget, menuAberto bool) *bancadaSolta {
	t.Helper()
	b := novaBancadaSolta(t)
	consultas := 0
	b.rt.menuAberto = func() bool { consultas++; return menuAberto }
	b.ligar(t)
	b.clicar()
	b.foco.alvo = alvo
	b.transcrever(t)
	if consultas > 1 {
		t.Fatalf("o modo de menu foi consultado %d vezes, quero no máximo uma, no fim", consultas)
	}
	return b
}

// TestSolta_MenuAbertoNoFimFicaNoHistorico: o clique que parou abriu um menu
// (o da bandeja, o de contexto). O texto não é escrito no menu: fica no
// histórico, e a pílula diz isso (ADR-025, risco 5).
func TestSolta_MenuAbertoNoFimFicaNoHistorico(t *testing.T) {
	b := terminarComMenu(t, dictation.FocusTarget{ProcessName: "editor.exe", ControlType: "Edit"}, true)

	if ins := pedidosDeInsercao(b.rt); len(ins) != 0 {
		t.Fatalf("o texto foi escrito com um menu aberto: %+v", ins)
	}
	l := b.hist.unica(t)
	if l.FinalText != ditadoSolto || l.Status != "error" {
		t.Fatalf("histórico: final %q status %q, quero o ditado, como inserção que falhou", l.FinalText, l.Status)
	}
	if f := b.pilula.ultima(t); f != overlay.FaseMenuNoHistorico {
		t.Fatalf("pílula %v, quero FaseMenuNoHistorico", f)
	}
	if !strings.Contains(b.log.String(), "motivo=menu_no_fim") {
		t.Errorf("a linha do overlay não diz o motivo:\n%s", b.log.String())
	}
}

// TestSolta_MenuPeloTipoDoControle: sem o modo de menu do Win32, o item de
// menu em foco (um menu que não é do Win32) também não recebe o texto.
func TestSolta_MenuPeloTipoDoControle(t *testing.T) {
	b := terminarComMenu(t, dictation.FocusTarget{ProcessName: "app.exe", ControlType: "MenuItem"}, false)
	if ins := pedidosDeInsercao(b.rt); len(ins) != 0 {
		t.Fatalf("o texto foi escrito num item de menu: %+v", ins)
	}
	if f := b.pilula.ultima(t); f != overlay.FaseMenuNoHistorico {
		t.Fatalf("pílula %v, quero FaseMenuNoHistorico", f)
	}
}

// TestSolta_CampoProtegidoVemAntesDoMenu: um menu aberto sobre um campo de
// senha é, antes de tudo, um campo de senha.
func TestSolta_CampoProtegidoVemAntesDoMenu(t *testing.T) {
	b := terminarComMenu(t, dictation.FocusTarget{ProcessName: "banco.exe", ControlType: "Edit", Protected: true}, true)
	if f := b.pilula.ultima(t); f != overlay.FaseCampoProtegidoNoHistorico {
		t.Fatalf("pílula %v, quero FaseCampoProtegidoNoHistorico", f)
	}
}

// TestSolta_SemMenuEscreve: sem menu, o texto vai para o campo como sempre.
func TestSolta_SemMenuEscreve(t *testing.T) {
	b := terminarComMenu(t, dictation.FocusTarget{ProcessName: "editor.exe", ControlType: "Edit"}, false)
	if ins := pedidosDeInsercao(b.rt); len(ins) != 1 {
		t.Fatalf("pedidos de inserção %d, quero o texto no campo", len(ins))
	}
}

// TestSolta_OHoldNaoOlhaOMenu: o ditado de sempre não relê nada no fim, e
// escreve mesmo com o modo de menu ligado (a ADR-025 só muda a gravação solta).
func TestSolta_OHoldNaoOlhaOMenu(t *testing.T) {
	b := novaBancadaSolta(t)
	consultas := 0
	b.rt.menuAberto = func() bool { consultas++; return true }
	b.tecla(true, 0, false, false)
	b.rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	b.rt.dispatch(<-b.rt.pending)
	b.tecla(false, 3000, true, false)
	b.transcrever(t)
	if consultas != 0 || len(pedidosDeInsercao(b.rt)) != 1 {
		t.Fatalf("hold: %d consulta(s) ao modo de menu e %d inserção(ões); quero 0 e 1", consultas, len(pedidosDeInsercao(b.rt)))
	}
}

// TestNew_LigaOModoDeMenu: o Runtime de produção consulta o modo de menu do
// Win32. Só confere que a consulta está ligada; não a chama.
func TestNew_LigaOModoDeMenu(t *testing.T) {
	machine, err := dictation.NewMachine(dictation.Config{
		Key:           dictation.KeyUnknown,
		Modifiers:     dictation.ModControl | dictation.ModMeta,
		HoldThreshold: 700 * time.Millisecond,
		MaxRecording:  dictation.DefaultMaxRecording,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt := New(machine, clock.NewFake(time.Now()), nil, nil, nil, fakeFocus{}, nil, nil); rt.menuAberto == nil {
		t.Fatal("New não ligou a consulta do modo de menu: um menu aberto no fim da gravação solta receberia o texto")
	}
}
