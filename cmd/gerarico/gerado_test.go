package main

import (
	"bytes"
	"os"
	"testing"
)

// Os dois arquivos gerados ficam no repositório. Estes testes só os LEEM, e
// conferem que são os que o gerador produz hoje: se alguém mudar o desenho do
// ícone da bandeja (ou o ladrilho) e esquecer de regenerar, o executável e o Setup
// continuariam com o ícone velho sem que nada o dissesse.
//
// Comparam os PIXELS de cada camada, e não os bytes do arquivo: o PNG de 256 px
// pode mudar de bytes de uma versão do Go para outra sem mudar de desenho.

const (
	icoDoRepositorio  = "../../installer/aurora-whisper.ico"
	sysoDoRepositorio = "../../cmd/agent/icone_windows_amd64.syso"
	paraRegenerar     = "regenere com `go run ./cmd/gerarico`, da raiz do repositório"
)

func TestGerado_IcoDoRepositorioEstaAtualizado(t *testing.T) {
	b, err := os.ReadFile(icoDoRepositorio)
	if err != nil {
		t.Fatalf("%v (%s)", err, paraRegenerar)
	}
	camadas, _, err := lerICO(b)
	if err != nil {
		t.Fatalf("o .ico do repositório não é válido: %v (%s)", err, paraRegenerar)
	}
	if len(camadas) != len(tamanhosDoIcone) {
		t.Fatalf("o .ico tem %d camadas, quero %d: %s", len(camadas), len(tamanhosDoIcone), paraRegenerar)
	}
	for i, tam := range tamanhosDoIcone {
		if camadas[i].Tam != tam || !bytes.Equal(camadas[i].Pix, pixelsDoIcone(tam)) {
			t.Errorf("a camada de %d px do .ico do repositório não é o desenho de hoje: %s", tam, paraRegenerar)
		}
	}
}

func TestGerado_SysoDoRepositorioEstaAtualizado(t *testing.T) {
	b, err := os.ReadFile(sysoDoRepositorio)
	if err != nil {
		t.Fatalf("%v (%s)", err, paraRegenerar)
	}
	camadas, err := camadasDoSyso(b)
	if err != nil {
		t.Fatalf("o .syso do repositório não é válido: %v (%s)", err, paraRegenerar)
	}
	if len(camadas) != len(tamanhosDoIcone) {
		t.Fatalf("o .syso tem %d camadas, quero %d: %s", len(camadas), len(tamanhosDoIcone), paraRegenerar)
	}
	for i, tam := range tamanhosDoIcone {
		if camadas[i].Tam != tam || !bytes.Equal(camadas[i].Pix, pixelsDoIcone(tam)) {
			t.Errorf("a camada de %d px do .syso do repositório não é o desenho de hoje: %s", tam, paraRegenerar)
		}
	}
}

// Os ícones do app instalável (web/pwa) também ficam no repositório, e o build da
// nuvem só os copia: um PNG velho seria o ícone instalado no celular.
func TestGerado_IconesDoAppEstaoAtualizados(t *testing.T) {
	for _, ic := range iconesDoApp {
		b, err := os.ReadFile("../../web/pwa/" + ic.Nome)
		if err != nil {
			t.Errorf("%v (%s)", err, paraRegenerar)
			continue
		}
		c, err := decodificarCamada(b)
		if err != nil || !c.PNG {
			t.Errorf("web/pwa/%s não é um PNG válido (%v): %s", ic.Nome, err, paraRegenerar)
			continue
		}
		if c.Tam != ic.Tam || !bytes.Equal(c.Pix, ic.pixels()) {
			t.Errorf("web/pwa/%s (%d px) não é o desenho de hoje: %s", ic.Nome, c.Tam, paraRegenerar)
		}
	}
}

// O .ico e o .syso do repositório têm as mesmas camadas: um não pode ficar para
// trás do outro (o do Setup e o do executável são o mesmo símbolo).
func TestGerado_IcoESysoDoRepositorioTemAsMesmasCamadas(t *testing.T) {
	bi, err := os.ReadFile(icoDoRepositorio)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := os.ReadFile(sysoDoRepositorio)
	if err != nil {
		t.Fatal(err)
	}
	doIco, _, err := lerICO(bi)
	if err != nil {
		t.Fatal(err)
	}
	doSyso, err := camadasDoSyso(bs)
	if err != nil {
		t.Fatal(err)
	}
	if len(doIco) != len(doSyso) {
		t.Fatalf("o .ico tem %d camadas e o .syso, %d", len(doIco), len(doSyso))
	}
	for i := range doIco {
		if doIco[i].Tam != doSyso[i].Tam || !bytes.Equal(doIco[i].Pix, doSyso[i].Pix) {
			t.Errorf("a camada %d (%d px) difere entre o .ico e o .syso do repositório", i, doIco[i].Tam)
		}
	}
}
