package inserter

// A regra de quebra de linha (ADR-016, decisão 11; ADR-011, decisão 3,
// emenda): texto com "\n" ou "\r" vai SEMPRE pelo clipboard, com CRLF, e
// nunca cai para o SendInput — pelo SendInput um "\n" pode virar Enter e
// enviar a mensagem num chat (WhatsApp Web) antes de o texto acabar.
//
// Tudo aqui é lógica pura contra os dublês de strategy_test.go: nenhuma
// chamada de sistema, nenhuma escrita no clipboard, nenhum SendInput
// (CLAUDE.md, "Testes que agem no sistema real"). Que o aplicativo trate a
// colagem como quebra, e não como envio, só o roteiro manual confere.

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestNormalizarQuebras(t *testing.T) {
	casos := []struct {
		nome, entrada, quero string
	}{
		{"sem quebra fica igual", "uma linha só", "uma linha só"},
		{"vazio", "", ""},
		{"LF solto vira CRLF", "a\nb", "a\r\nb"},
		{"CRLF fica CRLF, sem duplicar", "a\r\nb", "a\r\nb"},
		{"CR solto vira CRLF", "a\rb", "a\r\nb"},
		{"duas quebras seguidas (parágrafo)", "a\n\nb", "a\r\n\r\nb"},
		{"dois CRLF seguidos", "a\r\n\r\nb", "a\r\n\r\nb"},
		{"mistura no mesmo texto", "a\r\nb\nc\rd", "a\r\nb\r\nc\r\nd"},
		{"quebra no fim", "a\n", "a\r\n"},
		{"quebra no começo", "\na", "\r\na"},
		{"só uma quebra", "\n", "\r\n"},
		{"CR CR LF são duas quebras", "\r\r\n", "\r\n\r\n"},
		{"LF CR são duas quebras", "\n\r", "\r\n\r\n"},
		{"lista do Polish", "Pendências:\n- conferir o extrato\n- emitir o DAS", "Pendências:\r\n- conferir o extrato\r\n- emitir o DAS"},
		{"acento e emoji passam intactos", "olá 😀\nação", "olá 😀\r\nação"},
		{"tab e outros controles não são quebra", "a\tb\vc\fd", "a\tb\vc\fd"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := normalizarQuebras(c.entrada)
			if got != c.quero {
				t.Fatalf("normalizarQuebras(%q) = %q, quero %q", c.entrada, got, c.quero)
			}
			// Idempotente, e nada de LF ou CR soltos na saída.
			if again := normalizarQuebras(got); again != got {
				t.Errorf("não é idempotente: %q -> %q", got, again)
			}
			if lfSolto(got) || crSolto(got) {
				t.Errorf("sobrou LF ou CR solto em %q", got)
			}
		})
	}
}

// lfSolto: algum "\n" que não vem logo depois de um "\r".
func lfSolto(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			return true
		}
	}
	return false
}

// crSolto: algum "\r" que não vem logo antes de um "\n".
func crSolto(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' && (i+1 >= len(s) || s[i+1] != '\n') {
			return true
		}
	}
	return false
}

func TestTemQuebraDeLinha(t *testing.T) {
	casos := map[string]bool{
		"":            false,
		"uma linha":   false,
		"a\tb":        false,
		"a\nb":        true,
		"a\rb":        true,
		"a\r\nb":      true,
		"\n":          true,
		"fim\n":       true,
		"olá 😀 mundo": false,
	}
	for texto, quero := range casos {
		if got := temQuebraDeLinha(texto); got != quero {
			t.Errorf("temQuebraDeLinha(%q) = %v, quero %v", texto, got, quero)
		}
	}
}

// TestInsert_TextoComQuebraVaiSempreParaOClipboard: curto, longo, com o
// limiar no padrão ou generoso, com CR, LF ou CRLF — o SendInput nunca é
// tocado, e o clipboard recebe o texto com CRLF.
func TestInsert_TextoComQuebraVaiSempreParaOClipboard(t *testing.T) {
	casos := []struct {
		nome, texto, colado string
		limiar              int
	}{
		{"curto com LF, limiar padrão", "a\nb", "a\r\nb", 0},
		{"curto com LF, limiar generoso", "a\nb", "a\r\nb", 200},
		{"só uma quebra", "\n", "\r\n", 200},
		{"quebra no fim", "oi\n", "oi\r\n", 200},
		{"curto com CRLF", "a\r\nb", "a\r\nb", 200},
		{"curto com CR", "a\rb", "a\r\nb", 200},
		{"parágrafos", "Primeiro.\n\nSegundo.", "Primeiro.\r\n\r\nSegundo.", 200},
		{"lista", "Faça:\n- um\n- dois", "Faça:\r\n- um\r\n- dois", 200},
		{"longo com LF", strings.Repeat("palavra ", 30) + "\nfim", strings.Repeat("palavra ", 30) + "\r\nfim", 0},
		{"emoji e quebra", "😀\n😀", "😀\r\n😀", 200},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			u, cb := &fakeUnicode{}, &fakeClipboard{}
			ins := newTestInserter(Config{UnicodeThreshold: c.limiar}, u, cb)

			got := ins.Insert(c.texto)

			if !got.Success || got.Method != MethodClipboard {
				t.Fatalf("Insert(%q) = %+v, quero Success=true Method=MethodClipboard", c.texto, got)
			}
			if u.calls != 0 {
				t.Fatalf("unicode.calls = %d: texto com quebra não pode passar pelo SendInput (o \\n vira Enter)", u.calls)
			}
			if cb.calls != 1 || cb.gotText != c.colado {
				t.Fatalf("clipboard recebeu (calls=%d, %q), quero (1, %q)", cb.calls, cb.gotText, c.colado)
			}
		})
	}
}

// TestInsert_QuebraNaoCaiParaOUnicodeQuandoOClipboardFalha: o Enter é
// justamente o risco. Falhou o clipboard, falhou a inserção — o texto não
// é digitado caractere a caractere, e a regra de falha do clipboard vale
// como sempre (ClipboardRetained diz se o texto ficou lá para colar).
func TestInsert_QuebraNaoCaiParaOUnicodeQuandoOClipboardFalha(t *testing.T) {
	for _, retido := range []bool{false, true} {
		t.Run(map[bool]string{false: "nada retido", true: "texto retido no clipboard"}[retido], func(t *testing.T) {
			u := &fakeUnicode{}
			cb := &fakeClipboard{err: errors.New("colar bloqueado"), retained: retido}
			ins := newTestInserter(Config{UnicodeThreshold: 200}, u, cb)

			got := ins.Insert("linha um\nlinha dois")

			if got.Success {
				t.Fatal("Success = true, quero false: o clipboard falhou")
			}
			if u.calls != 0 {
				t.Fatalf("unicode.calls = %d: o texto com quebra não pode cair para o SendInput", u.calls)
			}
			if cb.calls != 1 {
				t.Fatalf("clipboard.calls = %d, quero 1 (sem retry)", cb.calls)
			}
			if got.Method != MethodNone || got.Partial {
				t.Fatalf("Insert = %+v, quero Method=MethodNone e Partial=false", got)
			}
			if got.ClipboardRetained != retido {
				t.Fatalf("ClipboardRetained = %v, quero %v (o que o clipboard disse)", got.ClipboardRetained, retido)
			}
		})
	}
}

// TestInsert_SemQuebraSegueARegraDoLimiar: a regra nova não pode ter
// mexido no texto de uma linha só — curto vai pelo SendInput, o do limiar
// exato também, e o que passa dele vai para o clipboard, sem CRLF nenhum.
func TestInsert_SemQuebraSegueARegraDoLimiar(t *testing.T) {
	t.Run("curto vai pelo SendInput", func(t *testing.T) {
		u, cb := &fakeUnicode{}, &fakeClipboard{}
		got := newTestInserter(DefaultConfig(), u, cb).Insert("ok, pode mandar")
		if got.Method != MethodUnicode || u.calls != 1 || cb.calls != 0 {
			t.Fatalf("Insert = %+v (unicode=%d, clip=%d), quero MethodUnicode", got, u.calls, cb.calls)
		}
	})
	t.Run("no limiar exato ainda vai pelo SendInput", func(t *testing.T) {
		u, cb := &fakeUnicode{}, &fakeClipboard{}
		got := newTestInserter(Config{UnicodeThreshold: 5}, u, cb).Insert("abcde")
		if got.Method != MethodUnicode || cb.calls != 0 {
			t.Fatalf("Insert = %+v (clip=%d), quero MethodUnicode", got, cb.calls)
		}
	})
	t.Run("acima do limiar vai para o clipboard, texto intacto", func(t *testing.T) {
		u, cb := &fakeUnicode{}, &fakeClipboard{}
		texto := "abcdef ghi"
		got := newTestInserter(Config{UnicodeThreshold: 5}, u, cb).Insert(texto)
		if got.Method != MethodClipboard || u.calls != 0 || cb.gotText != texto {
			t.Fatalf("Insert = %+v (unicode=%d, colado=%q), quero MethodClipboard com %q", got, u.calls, cb.gotText, texto)
		}
	})
}

// TestInsert_QuebraNaoVazaParaOLog: o log diz que houve quebra e quantas
// unidades, nunca o texto (AC-027).
func TestInsert_QuebraNaoVazaParaOLog(t *testing.T) {
	const canario = "CANARIO-DO-DITADO"
	logDe := func(clip *fakeClipboard, texto string) string {
		var buf bytes.Buffer
		ins := NewWithConfig(Config{UnicodeThreshold: 200}, slog.New(slog.NewTextHandler(&buf, nil)))
		ins.unicode, ins.clip = &fakeUnicode{}, clip
		ins.Insert(texto)
		return buf.String()
	}

	sucesso := logDe(&fakeClipboard{}, canario+"\n"+canario)
	if !strings.Contains(sucesso, "texto inserido") || !strings.Contains(sucesso, "multilinha=true") {
		t.Errorf("o log do sucesso não diz que o texto tinha quebra de linha:\n%s", sucesso)
	}
	falha := logDe(&fakeClipboard{err: errors.New("bloqueado"), retained: true}, canario+"\n"+canario)
	if !strings.Contains(falha, "caminho clipboard falhou") || !strings.Contains(falha, "multilinha=true") {
		t.Errorf("o log da falha não diz que o texto tinha quebra de linha:\n%s", falha)
	}
	// Texto de uma linha só, mas longo o bastante para ir por colar: não é multilinha.
	umaLinha := logDe(&fakeClipboard{}, strings.Repeat(canario+" ", 20))
	if !strings.Contains(umaLinha, "metodo=clipboard") || !strings.Contains(umaLinha, "multilinha=false") {
		t.Errorf("texto sem quebra devia registrar multilinha=false:\n%s", umaLinha)
	}

	for nome, out := range map[string]string{"sucesso": sucesso, "falha": falha, "uma linha": umaLinha} {
		if strings.Contains(out, canario) {
			t.Errorf("o CONTEÚDO do ditado vazou para o log (%s, AC-027):\n%s", nome, out)
		}
	}
}
