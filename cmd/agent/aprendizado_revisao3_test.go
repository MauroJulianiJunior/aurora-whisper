package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Segunda revisão da etapa 3 (30-set-2026): "aprendido" tem de AGIR. Tudo em
// t.TempDir(), com o runtime falso; o dicionário versionado é só lido.

const arquivoDicionarioVersionado = "../../testdata/asr-dataset/dicionario.json"

func montarComOVersionado(t *testing.T) (montagem, []byte) {
	t.Helper()
	b, err := os.ReadFile(arquivoDicionarioVersionado)
	if err != nil {
		t.Fatalf("lendo o dicionário versionado: %v", err)
	}
	s, err := dictionary.CarregarSemente(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return montarAprendizado(t, string(b), s.Usuario), b
}

// TestAprendizado_NaoAprendeVarianteQueNaoAge: os quatro casos em que a
// variante seria aceita e gravada, e o Smart nunca a aplicaria (um termo de
// prioridade maior, com casamento fonético fraco, passa na frente do alias
// confirmado de um termo de prioridade 50), mais os dois em que uma palavra da
// frase já é termo e veta a frase. O clique é recusado, nada é gravado e o
// runtime não recebe dicionário.
func TestAprendizado_NaoAprendeVarianteQueNaoAge(t *testing.T) {
	casos := []struct{ termo, variante, motivo string }{
		{"Vitti", "Viti", "Vite (90) leva a busca"},
		{"Dasa", "dassa", "DAS (90) leva a busca"},
		{"Dockside", "Dokker", "Docker (85) leva a busca"},
		{"Loggi", "logui", "log (55) leva a busca"},
		{"Sped Fiscal", "sped fiscau", "SPED veta a frase"},
		{"Pedro Dias", "pedro das", "DAS veta a frase"},
	}
	for _, c := range casos {
		t.Run(c.variante, func(t *testing.T) {
			m, original := montarComOVersionado(t)
			_, err := m.a.Aprender(context.Background(), c.termo, c.variante)
			if !errors.Is(err, dictionary.ErrNaoAgiria) {
				t.Fatalf("Aprender(%q, %q): erro = %v, quero ErrNaoAgiria (%s)", c.termo, c.variante, err, c.motivo)
			}
			if got := m.bytes(t); !bytes.Equal(got, original) {
				t.Error("a recusa gravou no arquivo")
			}
			if m.rt.quantos() != 0 {
				t.Errorf("o runtime recebeu %d dicionários por um pedido recusado", m.rt.quantos())
			}
			if strings.Contains(err.Error(), c.variante) || strings.Contains(err.Error(), c.termo) {
				t.Errorf("o erro traz o conteúdo do dicionário: %q", err)
			}
			// É uma recusa de regra, e não "o dicionário não carrega".
			if errors.Is(err, dictionary.ErrNaoCarrega) {
				t.Errorf("ErrNaoAgiria virou ErrNaoCarrega: %v", err)
			}
			lista, err := m.a.Aprendidas(context.Background())
			if err != nil || len(lista) != 0 {
				t.Errorf("Aprendidas = %v, %v; quero vazia", lista, err)
			}
		})
	}
}

// TestAprendizado_AprendeVarianteQueAge é o controle: variantes boas, nos
// mesmos moldes, são aceitas e agem no dicionário que o runtime recebe.
func TestAprendizado_AprendeVarianteQueAge(t *testing.T) {
	casos := []struct{ termo, variante, entra, quer string }{
		{"Claude Code", "clod codi", "abri o clod codi agora", "abri o Claude Code agora"},
		{"Vitti", "vitti x", "veja vitti x aqui", "veja Vitti aqui"},
		{"Sped Fiscal", "espede fiscau", "enviei o espede fiscau", "enviei o Sped Fiscal"},
		// A variante que abre a frase, com maiúscula: o Smart escreve o termo
		// com a inicial dela, e mesmo assim é o termo (a conferência não
		// distingue caixa).
		{"descrição", "Transcrição", "Transcrição do áudio", "Descrição do áudio"},
		{"descrição", "transcrição.", "veja a transcrição. Pronto", "veja a descrição. Pronto"},
	}
	for _, c := range casos {
		t.Run(c.variante, func(t *testing.T) {
			m, _ := montarComOVersionado(t)
			if _, err := m.a.Aprender(context.Background(), c.termo, c.variante); err != nil {
				t.Fatalf("Aprender(%q, %q): %v", c.termo, c.variante, err)
			}
			if got := resolve(t, m.rt.ultimo(t), c.entra); got != c.quer {
				t.Errorf("%q -> %q, quero %q", c.entra, got, c.quer)
			}
		})
	}
}

// TestAprendizado_ConfereAVarianteNoDominioDoTermo: num termo restrito a um
// domínio (Zulu), a variante só age nele. A conferência usa o domínio do
// termo; sem isso, toda variante de termo restrito seria recusada como "não
// agiria". No termo com domínio que não é restrito (Yankee), a conferência roda
// no domínio desconhecido: ver aprendizado_dominio_test.go.
func TestAprendizado_ConfereAVarianteNoDominioDoTermo(t *testing.T) {
	const conteudo = `{
  "usuario": "mauro",
  "entradas": [
    {"canonica": "Zulu", "prioridade": 60, "dominio": "dev", "restrito": true},
    {"canonica": "Yankee", "prioridade": 60, "dominio": "dev"}
  ]
}
`
	for _, termo := range []string{"Zulu", "Yankee"} {
		t.Run(termo, func(t *testing.T) {
			m := montarAprendizado(t, conteudo, "mauro")
			if _, err := m.a.Aprender(context.Background(), termo, "variante de teste"); err != nil {
				t.Fatalf("Aprender: %v", err)
			}
			if m.rt.quantos() != 1 {
				t.Errorf("o runtime recebeu %d dicionários", m.rt.quantos())
			}
		})
	}
}

// TestAprendizado_ErroDeQuemConfereNaoVazaEVaiComoNaoCarrega: um erro de
// Validar/ValidarAprendida que não é ErrNaoAgiria é ErrNaoCarrega, de mensagem
// fixa, e nada é gravado.
func TestAprendizado_ErroDeQuemConfereNaoVazaEVaiComoNaoCarrega(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	if err := os.WriteFile(caminho, []byte(dicionarioDoAprendizado), 0o644); err != nil {
		t.Fatal(err)
	}
	arq := &dictionary.Arquivo{
		Caminho: caminho,
		Usuario: "mauro",
		ValidarAprendida: func(dictionary.Semente, dictionary.Aprendida) error {
			return errors.New("segredo: texto ditado que não pode vazar")
		},
	}
	_, _, err := arq.Aprender("ECF", "esse efe")
	if !errors.Is(err, dictionary.ErrNaoCarrega) || strings.Contains(err.Error(), "segredo") {
		t.Errorf("erro = %v", err)
	}
	if got, _ := os.ReadFile(caminho); string(got) != dicionarioDoAprendizado {
		t.Error("o erro de quem confere gravou no arquivo")
	}

	// Desfazer não passa por ValidarAprendida.
	arq.ValidarAprendida = nil
	if _, _, err := arq.Aprender("ECF", "esse efe"); err != nil {
		t.Fatal(err)
	}
	arq.ValidarAprendida = func(dictionary.Semente, dictionary.Aprendida) error { return dictionary.ErrNaoAgiria }
	if _, _, err := arq.Desfazer("esse efe"); err != nil {
		t.Errorf("Desfazer foi barrado por ValidarAprendida: %v", err)
	}
}
