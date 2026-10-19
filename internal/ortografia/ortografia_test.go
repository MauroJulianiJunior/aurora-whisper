package ortografia

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

// Os arquivos embutidos são os do VERO sem nenhuma alteração: o gzip guarda
// o original byte a byte, e o hash é o registrado em vero/LEIAME.md.
func TestArquivosSaoOsDoVero(t *testing.T) {
	quero := map[string]string{
		"vero/pt_BR.aff.gz": "21d8ad2a769a60e17e2b5ea4ef11d4d593a58b9e2a82d642ef82d6a4c5523865",
		"vero/pt_BR.dic.gz": "a38bfb26b68ece2834e79fe83e48d5792652970ace12db89d1b9674bf9933183",
	}
	for nome, hash := range quero {
		r, err := abrirGz(nome)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, r); err != nil {
			t.Fatal(err)
		}
		r.Close()
		if got := hex.EncodeToString(h.Sum(nil)); got != hash {
			t.Errorf("%s: sha256 = %s, quero %s", nome, got, hash)
		}
	}
}

func TestDisponivel(t *testing.T) {
	if err := Disponivel(); err != nil {
		t.Fatal(err)
	}
}

func TestExiste(t *testing.T) {
	casos := []struct {
		palavra string
		quero   bool
	}{
		// Raiz, e as flexões que saem dos afixos.
		{"relatório", true}, {"relatórios", true}, {"vigente", true}, {"sazonalidade", true},
		{"pagou", true}, {"pegou", true}, {"compõe", true}, {"compõem", true},
		{"naquela", true}, {"contábeis", true}, {"aluguéis", true},
		// Prefixo e sufixo juntos (a classe "Y" do VERO).
		{"reorganizaram", true}, {"desorganizadas", true},
		// A caixa não conta; o nome próprio existe com maiúscula.
		{"Relatório", true}, {"RELATÓRIO", true}, {"Débora", true},
		// Hífen: inteira, ou cada parte.
		{"e-mail", true}, {"guarda-chuva", true},
		// O que o ASR inventa.
		{"relatorio", false}, {"vejante", false}, {"sasonalidade", false},
		{"promptes", false}, {"derepente", false}, {"debora", false}, {"irpj", false},
		{"", false}, {"relatório-xyzq", false},
		// A FORBIDDENWORD do VERO não existe, nem gerada por outra regra.
		{"ague", false}, {"agues", false},
	}
	for _, c := range casos {
		if got := Existe(c.palavra); got != c.quero {
			t.Errorf("Existe(%q) = %v, quero %v", c.palavra, got, c.quero)
		}
	}
}

func TestSoNomeProprio(t *testing.T) {
	casos := []struct {
		palavra string
		quero   bool
	}{
		{"Débora", true}, {"débora", true}, {"relatório", false}, {"vejante", false},
	}
	for _, c := range casos {
		if got := SoNomeProprio(c.palavra); got != c.quero {
			t.Errorf("SoNomeProprio(%q) = %v, quero %v", c.palavra, got, c.quero)
		}
	}
}

func TestCondicao(t *testing.T) {
	c, err := lerCondicao("[^aeiou]ar")
	if err != nil {
		t.Fatal(err)
	}
	if !c.casaNoFim("falar") || c.casaNoFim("suar") || c.casaNoFim("ar") {
		t.Error("casaNoFim errou")
	}
	if !c.casaNoComeco("lareira") || c.casaNoComeco("arara") {
		t.Error("casaNoComeco errou")
	}
	if _, err := lerCondicao("[ab"); err == nil {
		t.Error("condição sem ']' passou")
	}
	ponto, _ := lerCondicao(".")
	if !ponto.casaNoFim("") {
		t.Error("'.' tem de casar com tudo")
	}
}
