package polish

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// TestPoliticaEsforco_DescePorDegrau: o padrão é "none"; cada recusa desce
// um degrau (minimal, depois nenhum campo); o último fica. Nenhum nome de
// modelo no código: a memória é por provedor e modelo.
func TestPoliticaEsforco_DescePorDegrau(t *testing.T) {
	var buf bytes.Buffer
	p := NovaPoliticaEsforco(slog.New(slog.NewTextHandler(&buf, nil)))

	if got := p.Atual("openai", "m"); got != "" {
		t.Fatalf("modelo novo: %q, quero o padrão (vazio, que manda none)", got)
	}
	p.Recusou("openai", "m", "")
	if got := p.Atual("openai", "m"); got != "minimal" {
		t.Fatalf("depois de recusar o none: %q, quero minimal", got)
	}
	p.Recusou("openai", "m", "minimal")
	if got := p.Atual("openai", "m"); got != ia.EsforcoOmitido {
		t.Fatalf("depois de recusar o minimal: %q, quero %q", got, ia.EsforcoOmitido)
	}
	p.Recusou("openai", "m", ia.EsforcoOmitido)
	if got := p.Atual("openai", "m"); got != ia.EsforcoOmitido {
		t.Fatalf("no último degrau: %q, quero ficar em %q", got, ia.EsforcoOmitido)
	}
	// Outro modelo, e a mesma memória só deste: começam do padrão.
	if got := p.Atual("openai", "outro"); got != "" {
		t.Fatalf("outro modelo herdou %q", got)
	}
	// Uma linha por degrau descido, com o recusado e o próximo, e só isso.
	log := buf.String()
	if n := strings.Count(log, "recusou o esforço"); n != 2 ||
		!strings.Contains(log, "recusado=none proximo=minimal") ||
		!strings.Contains(log, "recusado=minimal proximo=omitir") {
		t.Fatalf("log da política (%d linha[s]):\n%s", n, log)
	}
}

// TestPoliticaEsforco_RecusaRepetidaNaoPulaDegrau: duas recusas do mesmo
// degrau (dois ditados em voo) descem um degrau só; a Anthropic não entra
// na política.
func TestPoliticaEsforco_RecusaRepetidaNaoPulaDegrau(t *testing.T) {
	var buf bytes.Buffer
	p := NovaPoliticaEsforco(slog.New(slog.NewTextHandler(&buf, nil)))
	p.Recusou("openai", "m", "")
	p.Recusou("openai", "m", "")
	if got := p.Atual("openai", "m"); got != "minimal" {
		t.Fatalf("depois de duas recusas do none: %q, quero minimal", got)
	}
	// Recusa de um degrau que não é o atual não faz nada.
	p.Recusou("openai", "m", ia.EsforcoOmitido)
	if got := p.Atual("openai", "m"); got != "minimal" {
		t.Fatalf("recusa de um degrau que não é o atual mexeu na política: %q", got)
	}
	p.Recusou("anthropic", "m", "")
	if got := p.Atual("anthropic", "m"); got != "" {
		t.Fatalf("Anthropic: %q, quero vazio (não recebe esforço)", got)
	}
	// Um degrau descido na OpenAI, nenhum na Anthropic.
	if n := strings.Count(buf.String(), "recusou o esforço"); n != 1 || strings.Contains(buf.String(), "provedor=anthropic") {
		t.Fatalf("log da política (%d linha[s]):\n%s", n, buf.String())
	}
}

// TestPoliticaEsforco_SeguraDeQualquerGoroutine: a chamada que o prazo
// descartou responde enquanto o ditado seguinte começa. Mapa escrito sem
// trava derruba o processo ("concurrent map writes").
func TestPoliticaEsforco_SeguraDeQualquerGoroutine(t *testing.T) {
	p := NovaPoliticaEsforco(slog.New(slog.DiscardHandler))
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				modelo := fmt.Sprintf("m%d", (g+i)%5)
				p.Recusou("openai", modelo, p.Atual("openai", modelo))
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 5; i++ {
		if got := p.Atual("openai", fmt.Sprintf("m%d", i)); got != ia.EsforcoOmitido {
			t.Errorf("m%d acabou em %q; com tantas recusas só pode estar no último degrau", i, got)
		}
	}
}

// TestPoliticaEsforco_LogNilUsaOPadrao: sem logger, não quebra.
func TestPoliticaEsforco_LogNilUsaOPadrao(t *testing.T) {
	p := NovaPoliticaEsforco(nil)
	p.Recusou("openai", "m", "")
	if got := p.Atual("openai", "m"); got != "minimal" {
		t.Fatalf("%q, quero minimal", got)
	}
}

func TestRotuloDoEsforco(t *testing.T) {
	for _, c := range []struct{ provedor, esforco, want string }{
		{"openai", "", "none"},
		{"openai", "minimal", "minimal"},
		{"openai", ia.EsforcoOmitido, "omitir"},
		{"anthropic", "", "nao_se_aplica"},
		{"anthropic", "minimal", "nao_se_aplica"},
		{"", "", "nao_se_aplica"},
	} {
		if got := RotuloDoEsforco(c.provedor, c.esforco); got != c.want {
			t.Errorf("RotuloDoEsforco(%q, %q) = %q, quero %q", c.provedor, c.esforco, got, c.want)
		}
	}
}
