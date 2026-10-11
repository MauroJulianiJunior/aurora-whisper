package inserter

// Teste white-box (mesmo pacote) contra dublês de unicodeInjector e
// clipboardInserter — nenhuma chamada de sistema, nenhum go:build. Cobre
// exatamente as seis situações pedidas para esta fatia: sucesso no
// caminho 1; falha no 1 e sucesso no 2; falha nos dois; texto vazio;
// texto longo o bastante para pular direto ao caminho 2; e um code point
// fora do BMP (emoji), que em UTF-16 vira um par de surrogates.

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf16"
)

// fakeUnicode é o dublê de unicodeInjector. Registra a última chamada
// para os testes verificarem exatamente quais UTF-16 code units foram
// pedidos.
type fakeUnicode struct {
	calls int
	got   []uint16
	err   error
	// injetados é quantos eventos o dublê diz ter injetado quando err
	// != nil. Zero significa "não injetou nada" (fallback é seguro);
	// maior que zero significa injeção PARCIAL (fallback duplicaria).
	injetados int
}

func (f *fakeUnicode) InjectUnicode(units []uint16) (int, error) {
	f.calls++
	f.got = append([]uint16(nil), units...)
	if f.err != nil {
		return f.injetados, f.err
	}
	return len(units) * 2, nil
}

// fakeClipboard é o dublê de clipboardInserter.
type fakeClipboard struct {
	calls    int
	gotText  string
	retained bool
	err      error
}

func (f *fakeClipboard) InsertViaClipboard(text string) (bool, error) {
	f.calls++
	f.gotText = text
	return f.retained, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestInserter(cfg Config, u *fakeUnicode, c *fakeClipboard) *Inserter {
	return &Inserter{cfg: cfg.withDefaults(), unicode: u, clip: c, log: testLogger()}
}

// TestInsert_SucessoCaminhoUnicode cobre o caso feliz: texto curto,
// caminho 1 funciona de primeira, caminho 2 nunca é tocado.
func TestInsert_SucessoCaminhoUnicode(t *testing.T) {
	u := &fakeUnicode{}
	c := &fakeClipboard{}
	ins := newTestInserter(DefaultConfig(), u, c)

	result := ins.Insert("ola mundo")

	if !result.Success || result.Method != MethodUnicode {
		t.Fatalf("Insert() = %+v, want Success=true Method=MethodUnicode", result)
	}
	if u.calls != 1 {
		t.Fatalf("unicode.calls = %d, want 1", u.calls)
	}
	if c.calls != 0 {
		t.Fatalf("clipboard.calls = %d, want 0 (não deveria ser tocado num sucesso do caminho 1)", c.calls)
	}
}

// TestInsert_FalhaUnicode_SucessoClipboard cobre a queda do caminho 1
// para o caminho 2 (ADR-011, decisão 3: "clipboard... quando o SendInput
// falha").
func TestInsert_FalhaUnicode_SucessoClipboard(t *testing.T) {
	u := &fakeUnicode{err: errors.New("app bloqueia entrada sintética")}
	c := &fakeClipboard{}
	ins := newTestInserter(DefaultConfig(), u, c)

	result := ins.Insert("ola mundo")

	if !result.Success || result.Method != MethodClipboard {
		t.Fatalf("Insert() = %+v, want Success=true Method=MethodClipboard", result)
	}
	if u.calls != 1 {
		t.Fatalf("unicode.calls = %d, want 1 (precisa ter tentado antes de cair pro clipboard)", u.calls)
	}
	if c.calls != 1 || c.gotText != "ola mundo" {
		t.Fatalf("clipboard call = (calls=%d, text=%q), want (1, %q)", c.calls, c.gotText, "ola mundo")
	}
}

// TestInsert_FalhaAmbos cobre o terceiro desfecho do ADR-011, decisão 3:
// nem SendInput nem clipboard conseguem inserir. Result precisa deixar
// claro que o texto não foi perdido silenciosamente — só que a inserção
// automática falhou —, e repassar se o clipboard reteve o texto de
// propósito (ver Result.ClipboardRetained).
func TestInsert_FalhaAmbos(t *testing.T) {
	u := &fakeUnicode{err: errors.New("bloqueado")}
	c := &fakeClipboard{err: errors.New("paste bloqueado"), retained: true}
	ins := newTestInserter(DefaultConfig(), u, c)

	result := ins.Insert("ola mundo")

	if result.Success {
		t.Fatal("Insert() Success = true, want false (ambos os caminhos falharam)")
	}
	if result.Method != MethodNone {
		t.Fatalf("Insert().Method = %v, want MethodNone", result.Method)
	}
	if !result.ClipboardRetained {
		t.Fatal("Insert().ClipboardRetained = false, want true — o dublê de clipboard sinalizou retained=true")
	}
	if u.calls != 1 || c.calls != 1 {
		t.Fatalf("calls = (unicode=%d, clipboard=%d), want (1, 1) — os dois caminhos precisam ter sido tentados", u.calls, c.calls)
	}
}

// TestInsert_TextoVazio cobre o sucesso trivial: nada a inserir, nenhum
// caminho é tocado — nem para "tentar e não fazer nada".
func TestInsert_TextoVazio(t *testing.T) {
	u := &fakeUnicode{}
	c := &fakeClipboard{}
	ins := newTestInserter(DefaultConfig(), u, c)

	result := ins.Insert("")

	if !result.Success || result.Method != MethodNone {
		t.Fatalf("Insert(\"\") = %+v, want Success=true Method=MethodNone", result)
	}
	if u.calls != 0 || c.calls != 0 {
		t.Fatalf("calls = (unicode=%d, clipboard=%d), want (0, 0) — texto vazio não deveria tocar nenhum backend", u.calls, c.calls)
	}
}

// TestInsert_TextoMuitoLongo_PulaDireitoParaClipboard cobre a escolha
// primária (não fallback) do caminho 2 para texto longo (ADR-011, decisão
// 3): o caminho 1 nunca é sequer tentado.
func TestInsert_TextoMuitoLongo_PulaDireitoParaClipboard(t *testing.T) {
	u := &fakeUnicode{}
	c := &fakeClipboard{}
	cfg := Config{UnicodeThreshold: 5}
	ins := newTestInserter(cfg, u, c)

	text := strings.Repeat("a", 50)
	result := ins.Insert(text)

	if !result.Success || result.Method != MethodClipboard {
		t.Fatalf("Insert(texto longo) = %+v, want Success=true Method=MethodClipboard", result)
	}
	if u.calls != 0 {
		t.Fatalf("unicode.calls = %d, want 0 — texto acima do limiar não deveria tentar o caminho 1", u.calls)
	}
	if c.calls != 1 || c.gotText != text {
		t.Fatalf("clipboard call = (calls=%d, text len=%d), want (1, %d)", c.calls, len(c.gotText), len(text))
	}
}

// TestInsert_EmojiForaDoBMP_LimiarContaUTF16UnitsNaoRunes é o caso
// clássico de corrupção silenciosa: um code point fora do BMP (aqui,
// U+1F600, um emoji) vira DOIS UTF-16 code units (surrogate pair) em vez
// de um. Se o limiar fosse contado por rune, um único emoji (1 rune, 2
// units) pareceria "curto" e o caminho 1 seria tentado mesmo estourando o
// orçamento real de eventos SendInput. Este teste prova que o limiar é
// contado em UTF-16 units: com UnicodeThreshold=1, um único emoji (2
// units) já deve pular direto para o clipboard.
func TestInsert_EmojiForaDoBMP_LimiarContaUTF16UnitsNaoRunes(t *testing.T) {
	const emoji = "\U0001F600" // 😀 — 1 rune, 2 UTF-16 code units (surrogate pair)
	if len([]rune(emoji)) != 1 {
		t.Fatalf("setup do teste inválido: %q tem %d runes, want 1", emoji, len([]rune(emoji)))
	}

	u := &fakeUnicode{}
	c := &fakeClipboard{}
	cfg := Config{UnicodeThreshold: 1}
	ins := newTestInserter(cfg, u, c)

	result := ins.Insert(emoji)

	if !result.Success || result.Method != MethodClipboard {
		t.Fatalf("Insert(emoji) = %+v, want Success=true Method=MethodClipboard — limiar=1 unit, emoji=2 units", result)
	}
	if u.calls != 0 {
		t.Fatal("unicode.calls != 0 — o limiar foi contado por rune (1), não por UTF-16 unit (2); é exatamente o bug que este teste existe para pegar")
	}
}

// TestInsert_EmojiForaDoBMP_CodificaSurrogatePairSemCorrupcao prova que,
// quando o caminho 1 É tentado com um emoji no meio do texto, os UTF-16
// code units que chegam ao backend são exatamente os do surrogate pair
// correto — nenhuma truncagem, nenhuma inversão de ordem.
func TestInsert_EmojiForaDoBMP_CodificaSurrogatePairSemCorrupcao(t *testing.T) {
	const text = "a\U0001F600b" // "a" + 😀 + "b"

	u := &fakeUnicode{}
	c := &fakeClipboard{}
	cfg := Config{UnicodeThreshold: 100} // limiar generoso: força o caminho 1
	ins := newTestInserter(cfg, u, c)

	result := ins.Insert(text)
	if !result.Success || result.Method != MethodUnicode {
		t.Fatalf("Insert(%q) = %+v, want Success=true Method=MethodUnicode", text, result)
	}

	want := utf16.Encode([]rune(text))
	if len(want) != 4 {
		t.Fatalf("setup do teste inválido: utf16.Encode(%q) tem %d units, want 4", text, len(want))
	}
	if len(u.got) != len(want) {
		t.Fatalf("unicode backend recebeu %d units, want %d: got=%v want=%v", len(u.got), len(want), u.got, want)
	}
	for i := range want {
		if u.got[i] != want[i] {
			t.Fatalf("unicode backend recebeu units=%v, want %v (surrogate pair corrompido no índice %d)", u.got, want, i)
		}
	}
}

func TestMethod_String(t *testing.T) {
	cases := map[Method]string{
		MethodNone:      "none",
		MethodUnicode:   "unicode",
		MethodClipboard: "clipboard",
		Method(99):      "unknown",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("Method(%d).String() = %q, want %q", m, got, want)
		}
	}
}

// TestInsert_InjecaoParcialNaoCaiParaOClipboard é o defeito que Mauro
// encontrou usando o produto em 27-set-2026.
//
// Ele ditou "o vencimento é dia dez, desculpa, dia doze" e o campo
// recebeu "O vencimento é dia 12. O vencimento é dia 12." — o texto
// duplicado.
//
// A causa estrutural: InjectUnicode devolvia só um error, e Insert lia
// "falhou" como "nada foi inserido". Basta faltar o último key-up — que
// não produz caractere nenhum — para o unicode reportar falha com TODO
// o texto já digitado na tela. O fallback então inseria tudo de novo.
//
// Duplicar é pior que inserir parcial: o texto certo fica enterrado no
// meio do errado, e quem usa precisa achar onde um termina e o outro
// começa.
func TestInsert_InjecaoParcialNaoCaiParaOClipboard(t *testing.T) {
	uni := &fakeUnicode{err: errors.New("sendinput incompleto"), injetados: 43}
	clip := &fakeClipboard{}
	ins := NewWithConfig(Config{UnicodeThreshold: 200}, nil)
	ins.unicode, ins.clip = uni, clip

	got := ins.Insert("O vencimento é dia 12.")

	if clip.calls != 0 {
		t.Errorf("o clipboard foi acionado %d vez(es) depois de uma injeção PARCIAL — "+
			"é exatamente isso que duplica o texto", clip.calls)
	}
	if got.Success {
		t.Error("injeção parcial não pode ser reportada como sucesso")
	}
	if !got.Partial {
		t.Error("Result.Partial = false — quem chama não tem como saber que o texto " +
			"NÃO está no clipboard, e mandaria a pessoa colar")
	}
	if got.ClipboardRetained {
		t.Error("ClipboardRetained = true sem o clipboard ter sido tocado")
	}
}

// TestInsert_FalhaSemInjetarNadaAindaCaiParaOClipboard: a correção não
// pode ter matado o fallback. Quando o unicode falha SEM injetar nada —
// o caso comum, por exemplo campo que não aceita SendInput —, o
// clipboard continua sendo a segunda chance.
func TestInsert_FalhaSemInjetarNadaAindaCaiParaOClipboard(t *testing.T) {
	uni := &fakeUnicode{err: errors.New("bloqueado"), injetados: 0}
	clip := &fakeClipboard{}
	ins := NewWithConfig(Config{UnicodeThreshold: 200}, nil)
	ins.unicode, ins.clip = uni, clip

	got := ins.Insert("texto curto")

	if clip.calls != 1 {
		t.Errorf("clipboard acionado %d vez(es), want 1 — sem nada injetado o fallback é seguro", clip.calls)
	}
	if !got.Success {
		t.Errorf("Insert falhou mesmo com o clipboard tendo dado certo: %+v", got)
	}
}

// TestInsert_RegistraOSucesso: sem log de sucesso não há como responder
// "a inserção rodou quantas vezes?".
//
// Em 28-set-2026 essa pergunta ficou sem resposta: um campo de texto não
// esvaziado entre dois testes fez o resultado parecer duplicação, e a
// investigação passou por três hipóteses erradas antes de a aritmética
// mostrar que o Agent tinha inserido UMA vez.
func TestInsert_RegistraOSucesso(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	ins := NewWithConfig(Config{UnicodeThreshold: 200}, log)
	ins.unicode, ins.clip = &fakeUnicode{}, &fakeClipboard{}
	if got := ins.Insert("texto"); !got.Success {
		t.Fatalf("Insert falhou: %+v", got)
	}

	out := buf.String()
	if !strings.Contains(out, "texto inserido") {
		t.Errorf("sucesso não foi registrado:\n%s", out)
	}
	if strings.Contains(out, "texto\"") && strings.Contains(out, "msg=") &&
		strings.Contains(out, `"texto"`) {
		t.Errorf("o CONTEÚDO inserido vazou para o log (AC-027):\n%s", out)
	}
}
