package main

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
)

// Sob go test, version é o literal de main.go: o de um build sem -X. Ele tem
// de ser o versaoDeDesenvolvimento, ou o bin\ passaria por release diante da
// nuvem (ADR-019, D7).
func TestVersaoDeDesenvolvimento(t *testing.T) {
	if version != versaoDeDesenvolvimento {
		t.Fatalf("version = %q e versaoDeDesenvolvimento = %q: trocar um sem o outro faz o build de desenvolvimento passar por release", version, versaoDeDesenvolvimento)
	}
}

// A D7: o build de desenvolvimento só conecta no modo portátil; o release,
// em qualquer modo (só com https, que NormalizarURL confere).
func TestPoliticaDaNuvem(t *testing.T) {
	for _, c := range []struct {
		versao string
		modo   pastas.Modo
		quero  nuvem.Politica
	}{
		{versaoDeDesenvolvimento, pastas.ModoPortatil, nuvem.Politica{Dev: true, Portatil: true}},
		{versaoDeDesenvolvimento, pastas.ModoPortatilLegado, nuvem.Politica{Dev: true, Portatil: true}},
		{versaoDeDesenvolvimento, pastas.ModoLocalAppData, nuvem.Politica{Dev: true}},
		{"0.1.0", pastas.ModoLocalAppData, nuvem.Politica{}},
		{"0.1.0-rc.1", pastas.ModoPortatil, nuvem.Politica{Portatil: true}},
	} {
		if got := politicaDaNuvem(c.versao, c.modo); got != c.quero {
			t.Errorf("politicaDaNuvem(%q, %s) = %+v; quero %+v", c.versao, c.modo, got, c.quero)
		}
	}
	// E o alvo do token segue o modo.
	if nuvem.NomeNoCofre(politicaDaNuvem("0.1.0", pastas.ModoLocalAppData).Portatil) != "nuvem" ||
		nuvem.NomeNoCofre(politicaDaNuvem(versaoDeDesenvolvimento, pastas.ModoPortatil).Portatil) != "nuvem-portatil" {
		t.Error("o alvo do token não segue o modo")
	}
}

// A montagem de verdade usa o alvo do modo: o instalado (localappdata) grava
// em AuroraWhisper/nuvem, o que o desinstalador apaga; o bin\ (portátil e
// portátil legado), em AuroraWhisper/nuvem-portatil. Montar não toca o
// sistema (nada abre nem lê o Gerenciador antes de um Conectar).
func TestMontarNuvem_AlvoDoModo(t *testing.T) {
	for modo, quero := range map[pastas.Modo]string{
		pastas.ModoLocalAppData:   nuvem.NomeNoCofreInstalado,
		pastas.ModoPortatil:       nuvem.NomeNoCofrePortatil,
		pastas.ModoPortatilLegado: nuvem.NomeNoCofrePortatil,
	} {
		if got := montarNuvem(instalacaoVazia{}, nil, modo, nil).NomeNoCofre(); got != quero {
			t.Errorf("modo %s: o token vai para %q; quero %q", modo, got, quero)
		}
	}
}

// instalacaoVazia: a tabela instalacao sem nada (montar não a lê).
type instalacaoVazia struct{}

func (instalacaoVazia) Ler(context.Context, string) (string, bool, error) { return "", false, nil }
func (instalacaoVazia) Gravar(context.Context, map[string]string) error   { return nil }
func (instalacaoVazia) Apagar(context.Context, ...string) error           { return nil }
