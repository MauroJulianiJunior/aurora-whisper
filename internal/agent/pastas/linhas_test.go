package pastas

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Tudo aqui é em memória: caminhos no formato do Windows escritos à mão (e não
// filepath.Join), para valer igual no Linux, e nenhum arquivo.

const (
	linhasDados    = `C:\Users\José Teste\AppData\Local\AuroraWhisper`
	linhasPrograma = `C:\Users\José Teste\AppData\Local\Programs\Aurora Whisper`
	linhasPerfil   = `C:\Users\José Teste`
)

func pastasDasLinhas() Pastas {
	return Pastas{programa: linhasPrograma, dados: linhasDados, modo: ModoLocalAppData}
}

// latin1 escreve s com um byte por caractere, como o servidor o escreveria na
// página de código do Windows. Feito à mão aqui, e não com emLatin1, para que o
// teste não confie no código que confere.
func latin1(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return string(b)
}

// A saída do servidor é cortada onde o pipe cortar. Aqui, byte a byte e em
// pedaços de vários tamanhos: o que sai é igual ao de uma escrita só, sem o
// caminho do perfil. É a prova do porquê de AnonimizarLinhas existir.
func TestAnonimizarLinhas_CaminhoPartidoEntreEscritas(t *testing.T) {
	entrada := "load_backend: loaded CPU backend from " + linhasPrograma + `\ggml-cpu-alderlake.dll` + "\n" +
		"whisper_init: loading model from 'ggml-small-q4_0.bin'\n" +
		"erro: open " + linhasDados + `\x: Acesso negado.` + "\n"
	quer := "load_backend: loaded CPU backend from <programa>" + `\ggml-cpu-alderlake.dll` + "\n" +
		"whisper_init: loading model from 'ggml-small-q4_0.bin'\n" +
		"erro: open <dados>" + `\x: Acesso negado.` + "\n"

	for _, tam := range []int{1, 2, 3, 7, 16, 41, len(entrada)} {
		t.Run(fmt.Sprintf("pedacos de %d", tam), func(t *testing.T) {
			var saida bytes.Buffer
			w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)
			for i := 0; i < len(entrada); i += tam {
				fim := min(i+tam, len(entrada))
				n, err := w.Write([]byte(entrada[i:fim]))
				if err != nil || n != fim-i {
					t.Fatalf("Write(%d bytes) = %d, %v; o contrato é o tamanho que entrou", fim-i, n, err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if saida.String() != quer {
				t.Errorf("saiu:\n%q\nquero:\n%q", saida.String(), quer)
			}
		})
	}
}

// Sem a quebra de linha nada sai: entregar a linha pela metade deixaria o
// caminho partido chegar ao filtro. O que falta sai no Close.
func TestAnonimizarLinhas_SoEntregaLinhaInteira(t *testing.T) {
	var saida bytes.Buffer
	w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)

	w.Write([]byte("lendo " + linhasPrograma[:20]))
	if saida.Len() != 0 {
		t.Fatalf("entregou uma linha incompleta: %q", saida.String())
	}
	w.Write([]byte(linhasPrograma[20:] + `\a.dll`))
	if saida.Len() != 0 {
		t.Fatalf("entregou uma linha sem a quebra: %q", saida.String())
	}
	w.Write([]byte("\ne a outra, sem quebra"))
	if got, quer := saida.String(), `lendo <programa>\a.dll`+"\n"; got != quer {
		t.Errorf("depois da quebra saiu %q, quero só a linha inteira %q", got, quer)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got, quer := saida.String(), `lendo <programa>\a.dll`+"\ne a outra, sem quebra"; got != quer {
		t.Errorf("depois do Close saiu %q, quero %q", got, quer)
	}
}

// Várias linhas numa escrita só saem todas: a entrega vai até a ÚLTIMA quebra.
func TestAnonimizarLinhas_VariasLinhasNumaEscrita(t *testing.T) {
	var saida bytes.Buffer
	w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)
	if _, err := w.Write([]byte("a " + linhasDados + "\nb " + linhasPerfil + "\nc")); err != nil {
		t.Fatal(err)
	}
	if got, quer := saida.String(), "a <dados>\nb <perfil>\n"; got != quer {
		t.Errorf("saiu %q, quero %q", got, quer)
	}
}

// O Close entrega o que sobrou, não fecha o destino e é idempotente; depois
// dele, Write recusa.
func TestAnonimizarLinhas_Close(t *testing.T) {
	var saida fechavel
	w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)
	w.Write([]byte("fim sem quebra em " + linhasPerfil))

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got, quer := saida.String(), "fim sem quebra em <perfil>"; got != quer {
		t.Errorf("o Close entregou %q, quero %q", got, quer)
	}
	if saida.fechou {
		t.Error("o Close fechou o destino: quem o abriu é quem o fecha")
	}

	if err := w.Close(); err != nil {
		t.Errorf("o segundo Close devolveu %v", err)
	}
	if got := saida.String(); got != "fim sem quebra em <perfil>" {
		t.Errorf("o segundo Close entregou de novo: %q", got)
	}

	n, err := w.Write([]byte("tarde demais\n"))
	if !errors.Is(err, os.ErrClosed) || n != 0 {
		t.Errorf("Write depois do Close = %d, %v; quero 0 e os.ErrClosed", n, err)
	}
	if got := saida.String(); strings.Contains(got, "tarde") {
		t.Errorf("escreveu depois do Close: %q", got)
	}
}

// Sem nada pendente o Close não escreve nada, nem uma escrita vazia.
func TestAnonimizarLinhas_CloseSemPendenteNaoEscreve(t *testing.T) {
	var saida contadora
	w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)
	w.Write([]byte("linha\n"))
	antes := saida.escritas
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if saida.escritas != antes {
		t.Errorf("o Close escreveu %d vez(es) sem nada pendente", saida.escritas-antes)
	}
}

// Uma saída sem quebra de linha não cresce sem limite: ao passar de maxLinha,
// entrega o que tem.
func TestAnonimizarLinhas_SemQuebraNaoCresceSemLimite(t *testing.T) {
	var saida bytes.Buffer
	w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)

	w.Write(bytes.Repeat([]byte("x"), maxLinha-1))
	if saida.Len() != 0 {
		t.Fatalf("entregou antes do limite (%d bytes)", saida.Len())
	}
	w.Write([]byte("y"))
	if saida.Len() != maxLinha {
		t.Errorf("no limite entregou %d bytes, quero %d", saida.Len(), maxLinha)
	}
	// E nada foi perdido nem repetido.
	if got := saida.String(); got != strings.Repeat("x", maxLinha-1)+"y" {
		t.Error("o conteúdo entregue no limite não é o que entrou")
	}
	// Depois do limite a fila está vazia: a escrita seguinte sai inteira, uma vez.
	w.Write([]byte("z\n"))
	if got, quer := saida.String(), strings.Repeat("x", maxLinha-1)+"yz\n"; got != quer {
		t.Errorf("depois do limite, a saída tem %d bytes, quero %d", len(got), len(quer))
	}
}

// Quando o destino falha, a linha não é repetida na escrita seguinte: o
// arquivo teria a mesma linha duas vezes.
func TestAnonimizarLinhas_ErroDoDestinoNaoRepeteALinha(t *testing.T) {
	saida := &quebrada{falhas: 1}
	w := AnonimizarLinhas(saida, pastasDasLinhas(), linhasPerfil)

	n, err := w.Write([]byte("primeira\nresto"))
	if err == nil || n != 0 {
		t.Fatalf("Write com o destino falhando = %d, %v; quero 0 e um erro", n, err)
	}
	if _, err := w.Write([]byte(" da segunda\n")); err != nil {
		t.Fatal(err)
	}
	if got, quer := saida.String(), "resto da segunda\n"; got != quer {
		t.Errorf("saiu %q, quero %q (a primeira linha se perdeu com o erro, e não pode voltar)", got, quer)
	}
}

// O os/exec escreve o stdout e o stderr do filho pelo mesmo Writer, de duas
// goroutines; o filtro não pode perder nem misturar linha.
func TestAnonimizarLinhas_EscritasConcorrentes(t *testing.T) {
	const (
		goroutines = 8
		linhas     = 300
	)
	var saida sobreposicao
	w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < linhas; i++ {
				w.Write([]byte(fmt.Sprintf("g%d linha %d em %s\n", g, i, linhasPrograma)))
			}
		}()
	}
	wg.Wait()
	w.Close()

	if saida.sobrepostas.Load() != 0 {
		t.Errorf("%d escrita(s) chegaram ao destino ao mesmo tempo", saida.sobrepostas.Load())
	}
	vistas := map[string]int{}
	for _, l := range strings.Split(strings.TrimSuffix(saida.buf.String(), "\n"), "\n") {
		vistas[l]++
	}
	for g := 0; g < goroutines; g++ {
		for i := 0; i < linhas; i++ {
			l := fmt.Sprintf("g%d linha %d em <programa>", g, i)
			if vistas[l] != 1 {
				t.Fatalf("a linha %q saiu %d vez(es); quero 1", l, vistas[l])
			}
		}
	}
	if len(vistas) != goroutines*linhas {
		t.Errorf("saíram %d linhas distintas; quero %d", len(vistas), goroutines*linhas)
	}
}

// O servidor pode escrever na página de código do Windows, e não em UTF-8: o
// "é" de José é um byte só (0xE9), e a forma UTF-8 do caminho não o acharia.
func TestAnonimizarLinhas_CaminhoEmCodificacaoANSI(t *testing.T) {
	formas := []struct{ nome, caminho, quer string }{
		{"barra invertida", linhasPrograma, `<programa>`},
		{"barra dobrada", strings.ReplaceAll(linhasPrograma, `\`, `\\`), `<programa>`},
		{"barra normal", strings.ReplaceAll(linhasPrograma, `\`, `/`), `<programa>`},
		{"o perfil", linhasPerfil, `<perfil>`},
		{"os dados", linhasDados, `<dados>`},
	}
	for _, f := range formas {
		t.Run(f.nome, func(t *testing.T) {
			ansi := latin1(f.caminho)
			if ansi == f.caminho {
				t.Fatal("o teste devia ter acento: sem ele as duas codificações são iguais")
			}
			var saida bytes.Buffer
			w := AnonimizarLinhas(&saida, pastasDasLinhas(), linhasPerfil)
			w.Write([]byte("lendo " + ansi + `\a.dll` + "\n"))
			w.Close()
			if got, quer := saida.String(), "lendo "+f.quer+`\a.dll`+"\n"; got != quer {
				t.Errorf("saiu %q, quero %q", got, quer)
			}
		})
	}
}

// Um caminho com algo que não cabe no Latin-1 continua trocado em UTF-8, e não
// gera a forma ANSI (que seria outra coisa).
func TestAnonimizar_CaminhoForaDoLatin1SoEmUTF8(t *testing.T) {
	const perfil = `C:\Users\Łukasz`
	p := Pastas{programa: perfil + `\Programa`, dados: perfil + `\Dados`, modo: ModoLocalAppData}
	var saida bytes.Buffer
	w := Anonimizar(&saida, p, perfil)
	w.Write([]byte("a " + perfil + `\x` + " b " + latin1("C:\\Users\\L") + `\x`))
	if got, quer := saida.String(), `a <perfil>\x b `+latin1("C:\\Users\\L")+`\x`; got != quer {
		t.Errorf("saiu %q, quero %q", got, quer)
	}
}

func TestEmLatin1(t *testing.T) {
	casos := []struct {
		entra string
		quer  string
		ok    bool
	}{
		{"", "", true},
		{"C:\\Users\\Maria", "C:\\Users\\Maria", true},
		{"José", "Jos\xe9", true},
		{"ç ã ü", "\xe7 \xe3 \xfc", true},
		{"\u007f", "\x7f", true},        // o último ASCII
		{"\u0080", "", false},           // Windows-1252 escreve outra coisa
		{"\u009f", "", false},           // idem, o último da faixa
		{"\u00a0", "\xa0", true},        // o primeiro depois dela
		{"\u00ff", "\xff", true},        // o último do Latin-1
		{"\u0100", "", false},           // não cabe
		{"Łukasz", "", false},           // não cabe
		{"a€b", "", false},              // o euro é U+20AC
		{"Jos\u00e9 \u0161", "", false}, // um que cabe e outro que não: nenhum
	}
	for _, c := range casos {
		got, ok := emLatin1(c.entra)
		if ok != c.ok || got != c.quer {
			t.Errorf("emLatin1(%q) = %q, %v; quero %q, %v", c.entra, got, ok, c.quer, c.ok)
		}
	}
}

// Um caminho só de ASCII não ganha forma ANSI: seria a mesma.
func TestFormasDoCaminho(t *testing.T) {
	if got := formasDoCaminho(`C:\Users\Maria`); len(got) != 3 {
		t.Errorf("caminho ASCII: %d formas %q, quero 3 (invertida, dobrada, normal)", len(got), got)
	}
	got := formasDoCaminho(`C:\Users\José`)
	if len(got) != 6 {
		t.Fatalf("caminho com acento: %d formas %q, quero 6 (as 3 em UTF-8 e as 3 em ANSI)", len(got), got)
	}
	quer := map[string]bool{
		`C:\Users\José`:          true,
		`C:\\Users\\José`:        true,
		`C:/Users/José`:          true,
		"C:\\Users\\Jos\xe9":     true,
		"C:\\\\Users\\\\Jos\xe9": true,
		"C:/Users/Jos\xe9":       true,
	}
	for _, f := range got {
		if !quer[f] {
			t.Errorf("forma inesperada %q", f)
		}
		delete(quer, f)
	}
	for f := range quer {
		t.Errorf("faltou a forma %q", f)
	}
}

// --- dublês de destino ---

// Os dublês NÃO embutem bytes.Buffer: io.WriteString usaria o WriteString
// promovido e passaria por cima do Write que o dublê define.

// fechavel é um destino que registra se alguém o fechou.
type fechavel struct {
	buf    bytes.Buffer
	fechou bool
}

func (f *fechavel) Write(b []byte) (int, error) { return f.buf.Write(b) }
func (f *fechavel) String() string              { return f.buf.String() }
func (f *fechavel) Close() error                { f.fechou = true; return nil }

// contadora conta as escritas que recebeu.
type contadora struct {
	buf      bytes.Buffer
	escritas int
}

func (c *contadora) Write(b []byte) (int, error) { c.escritas++; return c.buf.Write(b) }

// quebrada falha nas primeiras escritas e depois funciona.
type quebrada struct {
	buf    bytes.Buffer
	falhas int
}

func (q *quebrada) Write(b []byte) (int, error) {
	if q.falhas > 0 {
		q.falhas--
		return 0, errors.New("disco cheio")
	}
	return q.buf.Write(b)
}

func (q *quebrada) String() string { return q.buf.String() }

// sobreposicao acusa duas escritas ao mesmo tempo no destino: o destino de
// verdade, um arquivo, não promete serializá-las para quem o usa de vários
// lugares.
type sobreposicao struct {
	buf         bytes.Buffer
	dentro      atomic.Int32
	sobrepostas atomic.Int32
}

func (s *sobreposicao) Write(b []byte) (int, error) {
	if s.dentro.Add(1) > 1 {
		s.sobrepostas.Add(1)
	}
	// Segura a escrita por um instante: com duas goroutines soltas, elas se
	// encontram aqui.
	time.Sleep(20 * time.Microsecond)
	n, err := s.buf.Write(b)
	s.dentro.Add(-1)
	return n, err
}
