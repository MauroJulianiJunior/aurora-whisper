package tray

import (
	"testing"
	"unicode/utf16"
)

func item(t *testing.T, menu []Item, id ID) Item {
	t.Helper()
	for _, it := range menu {
		if it.ID == id && !it.Separador {
			return it
		}
	}
	t.Fatalf("item %d ausente do menu", id)
	return Item{}
}

// TestMenu_RefleteOEstado: o check de "Pausar" e o de "Iniciar com o
// Windows" vêm do estado, e o status muda de texto com a pausa.
func TestMenu_RefleteOEstado(t *testing.T) {
	ativo := Menu(Estado{Provider: "whisper-local · small-q4_0"})
	if item(t, ativo, IDPausar).Marcado {
		t.Error("Pausar marcado com o ditado ativo")
	}
	if got := item(t, ativo, IDStatus).Texto; got != "Ditado ativo" {
		t.Errorf("status = %q", got)
	}
	if got := item(t, ativo, IDProvider).Texto; got != "ASR: whisper-local · small-q4_0" {
		t.Errorf("provider = %q", got)
	}

	pausado := Menu(Estado{Pausado: true, IniciarComWindows: true})
	if !item(t, pausado, IDPausar).Marcado {
		t.Error("Pausar desmarcado com o ditado pausado")
	}
	if !item(t, pausado, IDIniciarComWindows).Marcado {
		t.Error("Iniciar com o Windows desmarcado com o início automático ligado")
	}
	if got := item(t, pausado, IDStatus).Texto; got != "Ditado pausado" {
		t.Errorf("status pausado = %q", got)
	}
}

// TestMenu_InformacaoNaoEhClicavel: status e provider são rótulos. Se
// fossem habilitados, parecer clicáveis sem fazer nada é pior que não
// estarem ali.
func TestMenu_InformacaoNaoEhClicavel(t *testing.T) {
	m := Menu(Estado{})
	for _, id := range []ID{IDStatus, IDProvider} {
		if item(t, m, id).Habilitado {
			t.Errorf("item informativo %d está habilitado", id)
		}
	}
	for _, id := range []ID{IDPausar, IDIniciarComWindows, IDSair} {
		if !item(t, m, id).Habilitado {
			t.Errorf("ação %d está desabilitada", id)
		}
	}
}

// TestMenu_InicioIndisponivelDizOMotivo cobre os dois motivos que existem.
//
// Registro ilegível: o item não pode aparecer desmarcado (seria afirmar
// "desligado" sem ter lido), nem clicável (o clique gravaria o contrário
// do que talvez esteja lá) — mesmo que o estado traga IniciarComWindows
// verdadeiro de algum lugar.
//
// Build de console (revisão de 29-set-2026): clicável, ela gravava a si
// mesma no Run, e o logon seguinte abria uma janela preta.
func TestMenu_InicioIndisponivelDizOMotivo(t *testing.T) {
	for _, motivo := range []string{"não consegui ler", "só na versão de bandeja"} {
		it := item(t, Menu(Estado{IniciarComWindows: true, InicioIndisponivel: motivo}), IDIniciarComWindows)
		if it.Habilitado {
			t.Errorf("%q: o item continua clicável", motivo)
		}
		if it.Marcado {
			t.Errorf("%q: o item afirma um estado", motivo)
		}
		if want := "Iniciar com o Windows (" + motivo + ")"; it.Texto != want {
			t.Errorf("%q: texto = %q, want %q — desabilitado sem motivo parece defeito", motivo, it.Texto, want)
		}
	}
}

// TestMenu_InicioDisponivelSemMotivo: motivo vazio é o caso normal — o
// item clicável, marcado conforme o registro, com o texto limpo.
func TestMenu_InicioDisponivelSemMotivo(t *testing.T) {
	it := item(t, Menu(Estado{IniciarComWindows: true}), IDIniciarComWindows)
	if !it.Habilitado || !it.Marcado || it.Texto != "Iniciar com o Windows" {
		t.Errorf("item = %+v, want habilitado, marcado e \"Iniciar com o Windows\"", it)
	}
}

// TestMenu_NenhumIDReservado: zero é o que TrackPopupMenu devolve quando
// o menu fecha sem escolha. Um item com ID zero seria indistinguível de
// "não escolheu nada".
func TestMenu_NenhumIDReservado(t *testing.T) {
	for _, it := range Menu(Estado{}) {
		if !it.Separador && it.ID == IDNenhum {
			t.Errorf("item %q tem o ID reservado", it.Texto)
		}
	}
}

// TestDicaUTF16_CabeNoCampo: szTip tem 128 unidades com o terminador.
// Um caractere fora do plano básico ocupa duas; cortá-lo pela metade
// deixaria um surrogate solto, que o Windows desenha como quadrado.
func TestDicaUTF16_CabeNoCampo(t *testing.T) {
	longa := ""
	for i := 0; i < 200; i++ {
		longa += "😀" // fora do plano básico: 2 unidades cada
	}
	d := dicaUTF16(longa)
	if d[maxDica-1] != 0 {
		t.Fatal("sem terminador na última posição")
	}
	n := 0
	for n < maxDica && d[n] != 0 {
		n++
	}
	if n%2 != 0 {
		t.Errorf("cortou no meio de um par de surrogates: %d unidades", n)
	}
	if runes := utf16.Decode(d[:n]); len(runes) != n/2 {
		t.Errorf("decodificou %d caracteres, want %d", len(runes), n/2)
	}

	curta := dicaUTF16(Dica(Estado{Pausado: true}))
	if got := string(utf16.Decode(curta[:len(utf16.Encode([]rune(Dica(Estado{Pausado: true}))))])); got != "Aurora Whisper — ditado pausado" {
		t.Errorf("dica pausada = %q", got)
	}
}
