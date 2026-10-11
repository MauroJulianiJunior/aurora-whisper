package nuvem_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
)

// O que o laço de sync (F7b) usa do Servico: a credencial, o 401 do sync e o
// aviso ao conectar.

// A credencial do laço de sync: só conectado, com o token do Cofre e o
// pareamento; desconectado, nem o token é lido.
func TestCredencial(t *testing.T) {
	ctx := context.Background()
	c := novoCenario(t)
	cred, err := c.s.Credencial(ctx)
	if err != nil || cred.Conectado || cred.Configurada || cred.Token != "" {
		t.Fatalf("nunca conectou: %+v, %v", cred, err)
	}
	tok := c.conectarDeVez()
	cred, err = c.s.Credencial(ctx)
	if err != nil || !cred.Conectado || !cred.Configurada || cred.Token != tok || cred.URL != urlDaNuvem ||
		cred.Aparelho != aparelhoFalso || cred.PareadoEm != c.inst.valor("nuvem_conectado_em") || cred.PareadoEm == "" {
		t.Fatalf("conectado: %+v, %v", cred, err)
	}
	// O token apagado à mão: desconectado, e a instalação limpa (como no
	// Estado).
	if err := c.cofre.Apagar(nuvem.NomeNoCofrePortatil); err != nil {
		t.Fatal(err)
	}
	cred, err = c.s.Credencial(ctx)
	if err != nil || cred.Conectado || !cred.Configurada || cred.Token != "" || c.inst.valor("nuvem_aparelho") != "" {
		t.Fatalf("sem o token: %+v, %v (%s)", cred, err, c.inst.tudo())
	}
}

// O 401 do sync: com o token que ainda é o guardado, desconecta como o
// Conferir (o aviso revogado); com um token velho (o PC reconectou enquanto o
// pedido estava na rede), não mexe em nada.
func TestTokenRecusado(t *testing.T) {
	ctx := context.Background()
	c := novoCenario(t)
	tok := c.conectarDeVez()

	if err := c.s.TokenRecusado(ctx, "tokenvelhotokenvelhotokenvelhotokenvelho123"); err != nil {
		t.Fatal(err)
	}
	if e := c.estado(); e.Estado != nuvem.Conectado {
		t.Fatalf("um token velho desconectou: %+v", e)
	}
	if err := c.s.TokenRecusado(ctx, tok); err != nil {
		t.Fatal(err)
	}
	e := c.estado()
	if e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoRevogado || e.URL != urlDaNuvem {
		t.Fatalf("depois do 401: %+v", e)
	}
	if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token recusado ficou no Cofre")
	}
	if c.inst.valor("nuvem_aparelho") != "" || c.inst.valor("nuvem_conta") != contaFalsa {
		t.Errorf("instalação = %s", c.inst.tudo())
	}
	// De novo (o 401 de outra rodada): nada a fazer, sem erro.
	if err := c.s.TokenRecusado(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.textoDoLog(), tok) {
		t.Error("o log tem o token")
	}
}

// O aviso ao conectar: só depois de uma troca que deu certo, uma vez.
func TestAoConectar(t *testing.T) {
	c := novoCenario(t)
	var avisos atomic.Int32
	c.s.AoConectar(func() { avisos.Add(1) })
	c.conectar(urlDaNuvem)
	if rr := c.retorno(c.nuvem.aprovar(), "0192f0a1-0000-7000-8000-0000000000ff"); rr.Code != http.StatusBadRequest {
		t.Fatalf("state errado = %d", rr.Code)
	}
	if avisos.Load() != 0 {
		t.Fatal("avisou sem conectar")
	}
	if rr := c.retorno(c.nuvem.aprovar(), pedidoFalso); rr.Code != http.StatusOK {
		t.Fatalf("retorno = %d", rr.Code)
	}
	if avisos.Load() != 1 {
		t.Fatalf("%d avisos; quero 1", avisos.Load())
	}
	// A página recarregada não conecta de novo, e não avisa de novo.
	c.retorno(c.nuvem.aprovar(), pedidoFalso)
	if avisos.Load() != 1 {
		t.Errorf("%d avisos depois de recarregar", avisos.Load())
	}
}
