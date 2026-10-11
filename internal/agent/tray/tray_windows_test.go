//go:build windows

package tray

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// TestNotifyIconData_TamanhoDoWindows trava o layout que vai para o
// Shell_NotifyIconW. Com um byte de diferença o Windows lê os campos
// deslocados, e o ícone simplesmente não aparece — sem erro nenhum.
//
// 976 é sizeof(NOTIFYICONDATAW) completo no x64.
func TestNotifyIconData_TamanhoDoWindows(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skipf("tamanho medido para amd64; GOARCH=%s", runtime.GOARCH)
	}
	if got := unsafe.Sizeof(notifyIconData{}); got != 976 {
		t.Errorf("sizeof(notifyIconData) = %d, want 976", got)
	}
	if got := unsafe.Sizeof(iconInfo{}); got != 32 {
		t.Errorf("sizeof(iconInfo) = %d, want 32", got)
	}
}

// TestIniciar_RecusadoSobGoTest: pôr um ícone na bandeja é agir na tela
// de quem usa. Sob go test, nunca.
func TestIniciar_RecusadoSobGoTest(t *testing.T) {
	_, err := Iniciar(Acoes{
		Estado:            func() Estado { return Estado{} },
		Pausar:            func(bool) {},
		IniciarComWindows: func(bool) error { return nil },
		Sair:              func() {},
	})
	if !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("Iniciar sob go test = %v, want ErrDisabledInTest — o teste teria posto um ícone na bandeja", err)
	}
}

// dubleDefWindowProc põe no lugar do DefWindowProc um dublê que só anota
// o que recebeu. Nenhuma mensagem destes testes chega ao Windows.
func dubleDefWindowProc(t *testing.T) *[]uint32 {
	t.Helper()
	var repassadas []uint32
	original := defWindowProc
	defWindowProc = func(_ win32.HWND, msg uint32, _, _ uintptr) uintptr {
		repassadas = append(repassadas, msg)
		return 0
	}
	t.Cleanup(func() { defWindowProc = original })
	return &repassadas
}

// TestWndProc_AltF4NaoEncerraOAgent trava o defeito da revisão de
// 29-set-2026: depois do menu, a janela oculta fica ativa, e o Alt+F4 de
// quem achava que fechava outra janela chegava nela. O DefWindowProc
// transformava o SC_CLOSE em WM_CLOSE, e o WM_CLOSE em Sair.
//
// O wndProc é chamado direto, com hwnd zero: nos caminhos exercitados
// aqui ele não faz chamada de sistema nenhuma.
func TestWndProc_AltF4NaoEncerraOAgent(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	saiu := false
	tr := &Tray{acoes: Acoes{Sair: func() { saiu = true }}}

	for _, wp := range []uintptr{scClose, scClose | 0x2} {
		if r := tr.wndProc(0, wmSysCommand, wp, 0); r != 0 {
			t.Errorf("wndProc(WM_SYSCOMMAND, 0x%X) = %d, quer 0", wp, r)
		}
	}
	if saiu {
		t.Error("Alt+F4 na janela oculta encerrou o Agent")
	}
	if len(*repassadas) != 0 {
		t.Errorf("SC_CLOSE chegou ao DefWindowProc, que o transforma em WM_CLOSE e encerra o Agent: %v", *repassadas)
	}
}

// TestWndProc_WMCloseDeForaEncerra: o WM_CLOSE postado de fora direto
// nesta janela continua indo para Sair, e não para o DefWindowProc, que
// destruiria só a janela e deixaria o Agent ditando sem ícone. (O do
// taskkill sem /F cai na pílula do overlay, e não aqui — ver
// overlay.AoPedirFechar.)
func TestWndProc_WMCloseDeForaEncerra(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	saiu := false
	tr := &Tray{acoes: Acoes{Sair: func() { saiu = true }}}

	if r := tr.wndProc(0, wmClose, 0, 0); r != 0 {
		t.Errorf("wndProc(WM_CLOSE) = %d, quer 0", r)
	}
	if !saiu {
		t.Error("WM_CLOSE de fora não chamou Sair")
	}
	if len(*repassadas) != 0 {
		t.Errorf("WM_CLOSE chegou ao DefWindowProc, que destruiria a janela: %v", *repassadas)
	}
}

// TestWndProc_OutrosComandosDeSistemaSeguem: só o SC_CLOSE é engolido.
func TestWndProc_OutrosComandosDeSistemaSeguem(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	tr := &Tray{acoes: Acoes{Sair: func() { t.Error("SC_MINIMIZE chamou Sair") }}}

	const scMinimize = 0xF020
	tr.wndProc(0, wmSysCommand, scMinimize, 0)
	if len(*repassadas) != 1 || (*repassadas)[0] != wmSysCommand {
		t.Errorf("SC_MINIMIZE não chegou ao DefWindowProc: %v", *repassadas)
	}
}

// dubleSistema anota, em ordem, cada chamada de sistema do caminho do
// ícone, e falha as que o teste mandar. Nada chega ao Windows: nenhum
// ícone na bandeja, nenhum timer, nenhuma janela destruída.
type dubleSistema struct {
	chamadas []string
	// recusarIcone faz NIM_ADD e NIM_MODIFY falharem, como com o Explorer
	// ocupado. NIM_DELETE sempre "funciona".
	recusarIcone bool
	recusarTimer bool
	// iconeNaBandeja é o HICON do último Shell_NotifyIcon.
	iconeNaBandeja win32.HICON

	// tema é o que o "registro" diz da barra de tarefas.
	tema Tema
	// criados são os pixels de cada ícone criado, em ordem; o HICON do
	// n-ésimo é 100+n. falharCriacao faz a criação de número n falhar
	// (contando de 1; zero, nenhuma).
	criados       [][]byte
	falharCriacao int
}

func (d *dubleSistema) anotar(s string) { d.chamadas = append(d.chamadas, s) }

func (d *dubleSistema) conta(s string) int {
	n := 0
	for _, c := range d.chamadas {
		if c == s {
			n++
		}
	}
	return n
}

func (d *dubleSistema) sistema() sistema {
	nomes := map[uintptr]string{nimAdd: "NIM_ADD", nimModify: "NIM_MODIFY", nimDelete: "NIM_DELETE"}
	return sistema{
		notifyIcon: func(op uintptr, n *notifyIconData) error {
			d.anotar(nomes[op])
			d.iconeNaBandeja = n.HIcon
			if op != nimDelete && d.recusarIcone {
				return errors.New("o Explorer está ocupado")
			}
			return nil
		},
		setTimer: func(win32.HWND) error {
			d.anotar("SetTimer")
			if d.recusarTimer {
				return errors.New("sem timer")
			}
			return nil
		},
		killTimer:     func(win32.HWND) { d.anotar("KillTimer") },
		destroyWindow: func(win32.HWND) { d.anotar("DestroyWindow") },

		lerTema: func() Tema {
			d.anotar("LerTema")
			return d.tema
		},
		criarIcone: func(pix []byte, _ int) (win32.HICON, error) {
			d.criados = append(d.criados, pix)
			n := len(d.criados)
			d.anotar(fmt.Sprintf("CriarIcone:%d", 100+n))
			if n == d.falharCriacao {
				return 0, errors.New("CreateIconIndirect falhou")
			}
			return win32.HICON(100 + n), nil
		},
		destruirIcone: func(h win32.HICON) { d.anotar(fmt.Sprintf("DestruirIcone:%d", h)) },
	}
}

// trayDeTeste monta um Tray com o dublê no lugar do Windows. pronto tem
// folga para a resposta a Iniciar: avisar nunca bloqueia o teste.
func trayDeTeste(d *dubleSistema, aoFalhar func(error)) (*Tray, chan error) {
	pronto := make(chan error, 1)
	return &Tray{
		acoes:  Acoes{Estado: func() Estado { return Estado{} }, AoFalhar: aoFalhar},
		sis:    d.sistema(),
		pronto: pronto,
	}, pronto
}

// TestTentativas_DesistirTiraOIconeAntesDeDestruirAJanela: um NIM_ADD que
// "falhou" por tempo esgotado às vezes pôs o ícone mesmo assim. A
// desistência destruía a janela sem NIM_DELETE, e o ícone ficava morto na
// bandeja ao lado da janela de erro (revisão de 29-set-2026).
func TestTentativas_DesistirTiraOIconeAntesDeDestruirAJanela(t *testing.T) {
	d := &dubleSistema{recusarIcone: true}
	tr, pronto := trayDeTeste(d, nil)
	for i := 0; i < MaxTentativasDoIcone; i++ {
		tr.tentarPorOIcone() // a primeira é a da subida; as outras, do timer
	}

	select {
	case err := <-pronto:
		if err == nil {
			t.Fatal("desistiu e respondeu nil a Iniciar")
		}
	default:
		t.Fatalf("recusado %d vezes e Iniciar continua esperando: %v", MaxTentativasDoIcone, d.chamadas)
	}
	iDelete, iDestroy := slices.Index(d.chamadas, "NIM_DELETE"), slices.Index(d.chamadas, "DestroyWindow")
	if iDelete < 0 || iDestroy < 0 || iDelete > iDestroy {
		t.Errorf("a desistência precisa tirar o ícone ANTES de destruir a janela: %v", d.chamadas)
	}
}

// TestTentativas_SemTimerNaSubidaDesiste: o retorno do SetTimer era
// ignorado. Sem timer nenhuma tentativa seguinte chega, e Iniciar ficava
// esperando para sempre — com o hook instalado e nenhum ícone (revisão de
// 29-set-2026).
func TestTentativas_SemTimerNaSubidaDesiste(t *testing.T) {
	d := &dubleSistema{recusarIcone: true, recusarTimer: true}
	tr, pronto := trayDeTeste(d, func(err error) {
		t.Errorf("na subida o erro é de Iniciar, não de AoFalhar: %v", err)
	})
	tr.tentarPorOIcone()

	select {
	case err := <-pronto:
		if err == nil {
			t.Fatal("sem timer, respondeu nil a Iniciar")
		}
	default:
		t.Fatalf("sem timer, Iniciar ficaria esperando para sempre: %v", d.chamadas)
	}
	if d.conta("DestroyWindow") != 1 || d.conta("NIM_DELETE") != 1 {
		t.Errorf("sem timer na subida, o caminho é o da desistência: %v", d.chamadas)
	}
}

// TestTentativas_SemTimerDepoisDaSubidaMantemAJanela: depois da subida não
// há Iniciar esperando, e destruir a janela perderia o próximo
// TaskbarCreated — a única chance que sobra de o ícone voltar.
func TestTentativas_SemTimerDepoisDaSubidaMantemAJanela(t *testing.T) {
	d := &dubleSistema{recusarIcone: true, recusarTimer: true}
	var falhas []error
	tr, pronto := trayDeTeste(d, func(err error) { falhas = append(falhas, err) })
	tr.avisou, tr.tentativas = true, 1
	tr.taskbarCriada = 0xC0DE

	tr.wndProc(0, tr.taskbarCriada, 0, 0)

	if d.conta("DestroyWindow") != 0 {
		t.Errorf("sem timer depois da subida, destruiu a janela: %v", d.chamadas)
	}
	if len(falhas) != 1 {
		t.Errorf("AoFalhar recebeu %d erros, quer 1 — a falha precisa ficar no log", len(falhas))
	}
	if len(pronto) != 0 {
		t.Error("respondeu a Iniciar uma segunda vez")
	}
}

// TestWndProc_TaskbarCreatedDepoisDaSubidaNaoDesiste trava o defeito da
// revisão de 29-set-2026: depois da subida, o TaskbarCreated tentava UMA
// vez. Com o Explorer recém-reiniciado e ocupado, o NIM_ADD falhava, e o
// ícone sumia pelo resto da sessão, com o Agent ditando e sem jeito de
// sair.
func TestWndProc_TaskbarCreatedDepoisDaSubidaNaoDesiste(t *testing.T) {
	d := &dubleSistema{recusarIcone: true}
	var falhas []error
	tr, _ := trayDeTeste(d, func(err error) { falhas = append(falhas, err) })
	// A subida pôs o ícone na primeira tentativa.
	tr.avisou, tr.tentativas = true, 1
	tr.taskbarCriada = 0xC0DE

	tr.wndProc(0, tr.taskbarCriada, 0, 0)
	if d.conta("SetTimer") != 1 {
		t.Fatalf("o Explorer recusou o ícone e nada armou as próximas tentativas: %v", d.chamadas)
	}
	for i := 0; i < 3*MaxTentativasDoIcone; i++ {
		tr.wndProc(0, wmTimer, idTimerIcone, 0)
	}
	if d.conta("DestroyWindow") != 0 || d.conta("KillTimer") != 0 {
		t.Fatalf("depois da subida, a série desistiu: %v", d.chamadas)
	}
	if len(falhas) != 1 {
		t.Errorf("AoFalhar recebeu %d erros, quer 1 — uma linha por série, não uma por segundo", len(falhas))
	}

	d.recusarIcone = false
	tr.wndProc(0, wmTimer, idTimerIcone, 0)
	if d.conta("KillTimer") != 1 {
		t.Errorf("o ícone entrou e o timer da série continuou armado: %v", d.chamadas)
	}
}

// TestWndProc_IconeDeVoltaVaiParaOLog trava o achado da revisão de
// 29-set-2026: a recusa depois da subida ia para o log ("tentando de novo
// ... até conseguir"), mas a volta do ícone não — e a última linha da
// bandeja seguia dizendo que ela estava tentando.
func TestWndProc_IconeDeVoltaVaiParaOLog(t *testing.T) {
	d := &dubleSistema{recusarIcone: true}
	tr, _ := trayDeTeste(d, func(error) {})
	voltas := 0
	tr.acoes.AoVoltar = func() { voltas++ }
	tr.avisou, tr.tentativas = true, 1
	tr.taskbarCriada = 0xC0DE

	tr.wndProc(0, tr.taskbarCriada, 0, 0)
	tr.wndProc(0, wmTimer, idTimerIcone, 0)
	if voltas != 0 {
		t.Fatalf("o ícone ainda está fora e AoVoltar foi chamada %d vezes", voltas)
	}
	d.recusarIcone = false
	tr.wndProc(0, wmTimer, idTimerIcone, 0)
	if voltas != 1 {
		t.Fatalf("o ícone voltou e AoVoltar foi chamada %d vezes, quer 1", voltas)
	}

	// A série seguinte entra de primeira: nada foi dito de recusa, e não
	// há volta a registrar.
	tr.wndProc(0, tr.taskbarCriada, 0, 0)
	if voltas != 1 {
		t.Errorf("sem recusa no log, AoVoltar foi chamada de novo: %d", voltas)
	}
}

// TestWndProc_IconeDeVoltaDepoisDoTimerQueFalhou: sem timer, a recusa vai
// para o log e a série para. Se um TaskbarCreated posterior põe o ícone
// de primeira, a volta também precisa ir para o log.
func TestWndProc_IconeDeVoltaDepoisDoTimerQueFalhou(t *testing.T) {
	d := &dubleSistema{recusarIcone: true, recusarTimer: true}
	tr, _ := trayDeTeste(d, func(error) {})
	voltas := 0
	tr.acoes.AoVoltar = func() { voltas++ }
	tr.avisou, tr.tentativas = true, 1
	tr.taskbarCriada = 0xC0DE

	tr.wndProc(0, tr.taskbarCriada, 0, 0)
	d.recusarIcone = false
	tr.wndProc(0, tr.taskbarCriada, 0, 0)
	if voltas != 1 {
		t.Errorf("o ícone voltou e AoVoltar foi chamada %d vezes, quer 1", voltas)
	}
}

// TestTentativas_SubidaComRecusaNaoRegistraVolta: na subida a recusa não
// vai para o log (ela é de Iniciar), então o ícone que entra na terceira
// tentativa não é uma "volta".
func TestTentativas_SubidaComRecusaNaoRegistraVolta(t *testing.T) {
	d := &dubleSistema{recusarIcone: true}
	tr, pronto := trayDeTeste(d, func(err error) {
		t.Errorf("na subida o erro é de Iniciar, não de AoFalhar: %v", err)
	})
	tr.acoes.AoVoltar = func() { t.Error("a subida registrou uma volta do ícone que nunca saiu") }

	tr.tentarPorOIcone()
	tr.tentarPorOIcone()
	d.recusarIcone = false
	tr.tentarPorOIcone()
	select {
	case err := <-pronto:
		if err != nil {
			t.Fatalf("o ícone entrou e Iniciar recebeu %v", err)
		}
	default:
		t.Fatalf("o ícone entrou e Iniciar continua esperando: %v", d.chamadas)
	}
}

// trayNoAr monta um Tray já de pé — ícone na bandeja, subida respondida —
// com a barra de tarefas escura e os ícones 1 (ativo) e 2 (pausado).
func trayNoAr(d *dubleSistema, pausado bool, aoFalhar func(error)) *Tray {
	tr, _ := trayDeTeste(d, aoFalhar)
	tr.acoes.Estado = func() Estado { return Estado{Pausado: pausado} }
	tr.avisou, tr.tentativas = true, 1
	tr.tam, tr.tema = 16, TemaEscuro
	tr.iconeAtivo, tr.iconePausado = 1, 2
	return tr
}

// avisoDeSettingChange chama o wndProc com um WM_SETTINGCHANGE cujo lParam
// aponta para o texto area, como o Windows manda.
func avisoDeSettingChange(tr *Tray, area string) {
	p, err := windows.UTF16PtrFromString(area)
	if err != nil {
		panic(err)
	}
	tr.wndProc(0, wmSettingChange, 0, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}

// TestWndProc_TrocaDeTemaRedesenhaOIcone: a barra de tarefas foi de
// escura para clara com o Agent rodando. O ícone não tem fundo próprio, e
// a barra off-white dele sumiria na barra de tarefas clara: os dois ícones
// são recriados na variante clara e postos na bandeja, e só DEPOIS os
// velhos são destruídos — a bandeja nunca aponta para um ícone que não
// existe mais.
func TestWndProc_TrocaDeTemaRedesenhaOIcone(t *testing.T) {
	for _, pausado := range []bool{false, true} {
		repassadas := dubleDefWindowProc(t)
		d := &dubleSistema{tema: TemaClaro}
		tr := trayNoAr(d, pausado, func(err error) { t.Errorf("AoFalhar: %v", err) })

		avisoDeSettingChange(tr, "ImmersiveColorSet")

		if tr.tema != TemaClaro {
			t.Errorf("pausado=%v: o tema guardado continua %d", pausado, tr.tema)
		}
		if len(d.criados) != 2 ||
			!bytes.Equal(d.criados[0], PixelsDoIcone(16, TemaClaro, false)) ||
			!bytes.Equal(d.criados[1], PixelsDoIcone(16, TemaClaro, true)) {
			t.Fatalf("pausado=%v: os ícones novos não são a variante clara, ativo e pausado: %v", pausado, d.chamadas)
		}
		quer := []string{"LerTema", "CriarIcone:101", "CriarIcone:102", "NIM_MODIFY", "DestruirIcone:1", "DestruirIcone:2"}
		if !slices.Equal(d.chamadas, quer) {
			t.Errorf("pausado=%v: chamadas %v, quer %v", pausado, d.chamadas, quer)
		}
		naBandeja := win32.HICON(101)
		if pausado {
			naBandeja = 102
		}
		if d.iconeNaBandeja != naBandeja {
			t.Errorf("pausado=%v: a bandeja recebeu o ícone %d, quer %d", pausado, d.iconeNaBandeja, naBandeja)
		}
		if tr.iconeAtivo != 101 || tr.iconePausado != 102 {
			t.Errorf("pausado=%v: o Tray guarda os ícones %d e %d, quer 101 e 102", pausado, tr.iconeAtivo, tr.iconePausado)
		}
		// É um aviso para todas as janelas: segue para o DefWindowProc.
		if !slices.Equal(*repassadas, []uint32{wmSettingChange}) {
			t.Errorf("pausado=%v: WM_SETTINGCHANGE não seguiu para o DefWindowProc: %v", pausado, *repassadas)
		}
	}
}

// TestWndProc_OutroAvisoNaoRedesenha: WM_SETTINGCHANGE chega por muita
// coisa — idioma, política, variáveis de ambiente. Só a troca de tema
// redesenha; o resto nem lê o registro.
func TestWndProc_OutroAvisoNaoRedesenha(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	d := &dubleSistema{tema: TemaClaro}
	tr := trayNoAr(d, false, nil)

	avisoDeSettingChange(tr, "Policy")
	avisoDeSettingChange(tr, "intl")
	tr.wndProc(0, wmSettingChange, 0, 0) // sem texto

	if len(d.chamadas) != 0 {
		t.Errorf("um aviso que não é de tema mexeu no ícone: %v", d.chamadas)
	}
	if len(*repassadas) != 3 {
		t.Errorf("os avisos não seguiram para o DefWindowProc: %v", *repassadas)
	}
}

// TestAcompanharTema_MesmoTemaNaoRecria: "ImmersiveColorSet" chega também
// quando só a cor de destaque muda. Com a barra de tarefas da mesma cor,
// nada é recriado.
func TestAcompanharTema_MesmoTemaNaoRecria(t *testing.T) {
	d := &dubleSistema{tema: TemaEscuro}
	tr := trayNoAr(d, false, nil)

	tr.acompanharTema()

	if !slices.Equal(d.chamadas, []string{"LerTema"}) {
		t.Errorf("sem troca de tema, recriou ou mexeu na bandeja: %v", d.chamadas)
	}
}

// TestAcompanharTema_FalhaMantemOsIconesVelhos: se o segundo ícone novo
// não sai, o primeiro é destruído e os velhos ficam — um ícone de
// contraste errado ainda é melhor que nenhum. O tema guardado continua o
// velho, e o próximo aviso tenta de novo.
func TestAcompanharTema_FalhaMantemOsIconesVelhos(t *testing.T) {
	d := &dubleSistema{tema: TemaClaro, falharCriacao: 2}
	var falhas []error
	tr := trayNoAr(d, false, func(err error) { falhas = append(falhas, err) })

	tr.acompanharTema()

	quer := []string{"LerTema", "CriarIcone:101", "CriarIcone:102", "DestruirIcone:101"}
	if !slices.Equal(d.chamadas, quer) {
		t.Errorf("chamadas %v, quer %v", d.chamadas, quer)
	}
	if tr.iconeAtivo != 1 || tr.iconePausado != 2 || tr.tema != TemaEscuro {
		t.Errorf("a falha trocou o que estava de pé: ícones %d e %d, tema %d", tr.iconeAtivo, tr.iconePausado, tr.tema)
	}
	if len(falhas) != 1 {
		t.Errorf("AoFalhar recebeu %d erros, quer 1", len(falhas))
	}

	// O próximo aviso, com a criação funcionando, faz a troca.
	d.chamadas, d.falharCriacao = nil, 0
	tr.acompanharTema()
	if tr.tema != TemaClaro || tr.iconeAtivo != 103 || tr.iconePausado != 104 {
		t.Errorf("a segunda tentativa não trocou: ícones %d e %d, tema %d (%v)", tr.iconeAtivo, tr.iconePausado, tr.tema, d.chamadas)
	}
	if d.conta("DestruirIcone:1") != 1 || d.conta("DestruirIcone:2") != 1 {
		t.Errorf("os ícones velhos não foram destruídos depois da troca: %v", d.chamadas)
	}
}

// TestMontarIcones_PrimeiroFalha: sem o primeiro ícone, nada é criado nem
// destruído — quem chama fica com os dois ou com nenhum.
func TestMontarIcones_PrimeiroFalha(t *testing.T) {
	d := &dubleSistema{falharCriacao: 1}
	tr := trayNoAr(d, false, nil)

	ativo, pausado, err := tr.montarIcones(TemaClaro)

	if err == nil || ativo != 0 || pausado != 0 {
		t.Errorf("montarIcones = %d, %d, %v; quer 0, 0 e erro", ativo, pausado, err)
	}
	if !slices.Equal(d.chamadas, []string{"CriarIcone:101"}) {
		t.Errorf("chamadas %v, quer só a criação que falhou", d.chamadas)
	}
}
