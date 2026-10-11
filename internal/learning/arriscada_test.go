package learning

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

func TestArriscada(t *testing.T) {
	casos := []struct {
		texto string
		quer  Motivo
	}{
		// Sem palavra de conteúdo: recusado.
		{"de", MotivoPalavraDeLigacao},
		{"das", MotivoPalavraDeLigacao},
		{"no", MotivoPalavraDeLigacao},
		{"Não", MotivoPalavraDeLigacao},
		{"não de", MotivoPalavraDeLigacao},
		{"sempre mais", MotivoPalavraDeLigacao},
		{"à", MotivoPalavraDeLigacao},
		{"2025", MotivoNumero},
		{"1º", MotivoNumero},
		{"dois", MotivoNumero},
		{"de 2025", MotivoNumero},
		{"não dois", MotivoNumero},
		// Uma palavra de conteúdo basta.
		{"RAG", ""},
		{"rague", ""},
		{"Nada Consta", ""},
		{"n oito n", ""},
		{"Windows 11", ""},
		{"DCTFWeb", ""},
		{"n8n", ""},
		{"v2", ""},
		// Sem palavra nenhuma: não é com ela (o dicionário recusa como vazio).
		{"", ""},
		{"?!", ""},
	}
	for _, c := range casos {
		if got := Arriscada(c.texto); got != c.quer {
			t.Errorf("Arriscada(%q) = %q, quero %q", c.texto, got, c.quer)
		}
	}
}

// A grafia é mais estrita que o termo: a palavra de polaridade em qualquer
// lugar, e a grafia só de palavras funcionais frequentes, são recusadas
// (revisão de 01-out-2026: "não rague" apagava a negação do ditado, e "eu"
// trocava todo "eu").
func TestGrafiaArriscada(t *testing.T) {
	casos := []struct {
		texto string
		quer  Motivo
	}{
		// O que Arriscada recusa, a grafia também recusa, com o mesmo código.
		{"das", MotivoPalavraDeLigacao},
		{"não", MotivoPalavraDeLigacao},
		{"2025", MotivoNumero},
		{"não dois", MotivoNumero},
		// Polaridade em qualquer lugar.
		{"não rague", MotivoMudaSentido},
		{"Nunca rague", MotivoMudaSentido},
		{"mais rague", MotivoMudaSentido},
		{"sempre hage", MotivoMudaSentido},
		{"rague só", MotivoMudaSentido},
		{"Nada Consta", MotivoMudaSentido},
		{"todo rague", MotivoMudaSentido},
		// Toda palavra funcional, de ligação ou número.
		{"eu", MotivoPalavraDeLigacao},
		{"Eu", MotivoPalavraDeLigacao},
		{"né", MotivoPalavraDeLigacao},
		{"ne", MotivoPalavraDeLigacao},
		{"isso", MotivoPalavraDeLigacao},
		{"já", MotivoPalavraDeLigacao},
		{"tá", MotivoPalavraDeLigacao},
		{"aí", MotivoPalavraDeLigacao},
		{"lá", MotivoPalavraDeLigacao},
		{"foi", MotivoPalavraDeLigacao},
		{"vai", MotivoPalavraDeLigacao},
		{"ele", MotivoPalavraDeLigacao},
		{"esse", MotivoPalavraDeLigacao},
		{"pro", MotivoPalavraDeLigacao},
		{"eu já", MotivoPalavraDeLigacao},
		{"isso é", MotivoPalavraDeLigacao},
		{"pro 2", MotivoPalavraDeLigacao},
		{"eu de", MotivoPalavraDeLigacao},
		{"isso que", MotivoPalavraDeLigacao},
		// Uma palavra de conteúdo basta, como no termo.
		{"rague", ""},
		{"hage", ""},
		{"HACI", ""},
		{"n oito n", ""},
		{"dctf web", ""},
		{"eu rague", ""},
		{"Viti", ""},
		{"", ""},
		{"?!", ""},
	}
	for _, c := range casos {
		if got := GrafiaArriscada(c.texto); got != c.quer {
			t.Errorf("GrafiaArriscada(%q) = %q, quero %q", c.texto, got, c.quer)
		}
	}
}

// O termo continua com a regra do "só": a palavra funcional e a polaridade ao
// lado de uma de conteúdo passam ("Nada Consta" é a certidão).
func TestArriscada_OTermoNaoUsaARegraDaGrafia(t *testing.T) {
	for _, termo := range []string{"Nada Consta", "Mais Brasil", "Isso Aqui Software"} {
		if got := Arriscada(termo); got != "" {
			t.Errorf("Arriscada(%q) = %q, quero vazio", termo, got)
		}
	}
}

// As palavras funcionais são chaves, como as de ligação: sem acento e em
// minúscula, senão a busca no mapa nunca as acharia.
func TestPalavrasFuncionaisSaoChaves(t *testing.T) {
	for k := range palavrasFuncionais {
		if k != dictionary.Normalize(k) || strings.Contains(k, " ") || k == "" {
			t.Errorf("%q não é chave", k)
		}
	}
	for _, k := range []string{"eu", "ne", "isso", "ja", "ta", "ai", "la", "foi", "vai", "esta", "tao"} {
		if !palavrasFuncionais[k] {
			t.Errorf("falta %q", k)
		}
	}
}
