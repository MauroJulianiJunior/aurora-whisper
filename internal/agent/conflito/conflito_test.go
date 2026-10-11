package conflito

import (
	"strings"
	"testing"
)

// TestDetectar_AchaOWisprFlowComoOWindowsOReporta é o caso que deu
// origem a este pacote.
//
// O Get-Process reporta "Wispr Flow" — com espaço e SEM a extensão. O
// executável se chama "Wispr Flow.exe". Comparar os dois cruamente não
// casa, e o aviso nunca dispararia justamente no programa que causou o
// problema.
func TestDetectar_AchaOWisprFlowComoOWindowsOReporta(t *testing.T) {
	for _, comoAparece := range []string{
		"Wispr Flow",     // como o Get-Process reporta
		"Wispr Flow.exe", // como o executável se chama
		"wispr flow.exe",
		"WISPR FLOW.EXE",
		`C:\Users\alguem\AppData\Local\WisprFlow\app-1.6.957\Wispr Flow.exe`,
		"  Wispr Flow  ",
	} {
		got := Detectar([]string{"explorer.exe", comoAparece, "chrome.exe"})
		if len(got) != 1 || got[0].Nome != "Wispr Flow" {
			t.Errorf("Detectar com %q = %v, want [Wispr Flow]", comoAparece, got)
		}
	}
}

// TestDetectar_NaoInventaConflito: falso positivo gasta a credibilidade
// do aviso para quando ele importar.
func TestDetectar_NaoInventaConflito(t *testing.T) {
	got := Detectar([]string{
		"explorer.exe", "chrome.exe", "Code.exe", "aurora-whisper-agent.exe",
		"whisper-server.exe", "flow.exe", "voice.exe", "dragon.exe",
	})
	if len(got) != 0 {
		t.Errorf("Detectar inventou conflito: %v", got)
	}
}

func TestDetectar_ListaVaziaOuNil(t *testing.T) {
	if got := Detectar(nil); len(got) != 0 {
		t.Errorf("Detectar(nil) = %v", got)
	}
	if got := Detectar([]string{}); len(got) != 0 {
		t.Errorf("Detectar([]) = %v", got)
	}
}

// TestDetectar_NaoRepete: o Wispr Flow roda com uma dúzia de processos
// do mesmo nome. O aviso tem de sair uma vez, não doze.
func TestDetectar_NaoRepete(t *testing.T) {
	muitos := make([]string, 12)
	for i := range muitos {
		muitos[i] = "Wispr Flow"
	}
	if got := Detectar(muitos); len(got) != 1 {
		t.Errorf("Detectar com 12 processos iguais devolveu %d avisos, want 1", len(got))
	}
}

// TestDetectar_EhDeterministico: o aviso não pode mudar de forma entre
// duas subidas sem nada ter mudado na máquina.
func TestDetectar_EhDeterministico(t *testing.T) {
	processos := []string{"talon.exe", "Wispr Flow", "natspeak.exe"}
	primeiro := nomes(Detectar(processos))
	for i := 0; i < 20; i++ {
		if got := nomes(Detectar(processos)); got != primeiro {
			t.Fatalf("execução %d: %q, primeira: %q", i, got, primeiro)
		}
	}
	if primeiro != "Dragon NaturallySpeaking|Talon|Wispr Flow" {
		t.Errorf("ordem = %q", primeiro)
	}
}

func nomes(ps []Programa) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Nome)
	}
	return strings.Join(out, "|")
}

// TestAviso_DizOQueFazerENaoSoOQueAconteceu.
//
// "Wispr Flow detectado" não ajuda ninguém. O aviso precisa explicar o
// SINTOMA — texto duplicado — porque é por ele que a pessoa vai procurar
// quando o problema aparecer, e é a única forma de ela ligar uma coisa
// à outra.
func TestAviso_DizOQueFazerENaoSoOQueAconteceu(t *testing.T) {
	var wispr Programa
	for _, p := range Conhecidos {
		if p.Nome == "Wispr Flow" {
			wispr = p
		}
	}
	aviso := wispr.Aviso()

	for _, esperado := range []string{"Wispr Flow", "Ctrl+Win", "duplicado"} {
		if !strings.Contains(aviso, esperado) {
			t.Errorf("aviso não menciona %q:\n%s", esperado, aviso)
		}
	}
}

// TestAviso_SemAtalhoConhecidoNaoInventa: afirmar um atalho errado seria
// pior que omitir — mandaria a pessoa procurar no lugar errado.
func TestAviso_SemAtalhoConhecidoNaoInventa(t *testing.T) {
	p := Programa{Nome: "Qualquer", Executaveis: []string{"q.exe"}}
	// "usa o atalho X" é a AFIRMAÇÃO sobre qual gesto o programa usa. A
	// frase genérica "se o atalho for o mesmo" continua valendo e contém
	// a palavra — por isso a checagem é pelo trecho que afirma, não pela
	// palavra solta.
	if strings.Contains(p.Aviso(), "usa o atalho") {
		t.Errorf("inventou atalho para programa sem atalho conhecido:\n%s", p.Aviso())
	}
	if !strings.Contains(p.Aviso(), "Qualquer") {
		t.Errorf("aviso não nomeia o programa:\n%s", p.Aviso())
	}
}
