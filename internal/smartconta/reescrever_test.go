package smartconta

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O Polish do ditado do celular parte do MESMO resultado do Smart da conta
// (ADR-021, decisões F e N; C5b): as guardas protegem os termos que o Smart
// trocou, e o texto que vai ao provedor é o do Smart, nunca o do ASR.

// polishQueEcoa é um resolver.PolishClient de mentira: devolve o texto que
// recebeu (ou o que o caso mandar) e anota o que chegou. Sem rede.
type polishQueEcoa struct {
	recebido string
	chamadas int
	devolver func(entrada string) string
	err      error
}

func (p *polishQueEcoa) Polish(_ context.Context, req resolver.PolishRequest) ([]byte, error) {
	p.chamadas++
	p.recebido = req.Text
	if p.err != nil {
		return nil, p.err
	}
	saida := req.Text
	if p.devolver != nil {
		saida = p.devolver(req.Text)
	}
	return json.Marshal(map[string]string{"final_text": saida})
}

func TestReescrever_PartirDoTextoDoSmart(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	d := c.ResolverParaPolir(context.Background(), "hoje eu entreguei a dirfi e o rague para o cliente")
	if d.Texto != "hoje eu entreguei a DIRF e o RAG para o cliente" {
		t.Fatalf("o Smart deu %q", d.Texto)
	}
	cli := &polishQueEcoa{devolver: func(s string) string { return "Hoje eu entreguei a DIRF e o RAG para o cliente." }}
	res := c.Reescrever(context.Background(), &resolver.PolishResolver{Smart: c.Smart(), Client: cli}, d)
	if cli.chamadas != 1 || cli.recebido != d.Texto {
		t.Fatalf("o Polish recebeu %q em %d chamadas, quero o texto do Smart uma vez", cli.recebido, cli.chamadas)
	}
	if !res.Reescrito || res.FinalText != "Hoje eu entreguei a DIRF e o RAG para o cliente." || res.RawText != d.Bruto {
		t.Fatalf("resultado %+v", res)
	}
}

// Os termos que o Smart trocou ficam protegidos: o Polish que desfaz a troca cai
// no texto do Smart (a guarda de siglas e termos, sobre o Smart da conta).
func TestReescrever_ATrocaDoSmartFicaProtegida(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	d := c.ResolverParaPolir(context.Background(), "hoje eu entreguei a dirfi para o cliente da empresa")
	cli := &polishQueEcoa{devolver: func(string) string { return "Hoje eu entreguei a Dirf para o cliente da empresa." }}
	res := c.Reescrever(context.Background(), &resolver.PolishResolver{Smart: c.Smart(), Client: cli}, d)
	if res.Reescrito || res.FinalText != d.Texto {
		t.Fatalf("o Polish que desfaz a troca valeu: %+v", res)
	}
	if res.MotivoFallback == resolver.MotivoNenhum {
		t.Fatal("o Polish recusado veio sem motivo")
	}
}

// O texto curto nem vai ao provedor, como no PC.
func TestReescrever_TextoCurtoNaoChamaOProvedor(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	d := c.ResolverParaPolir(context.Background(), "ok pode")
	cli := &polishQueEcoa{}
	res := c.Reescrever(context.Background(), &resolver.PolishResolver{Smart: c.Smart(), Client: cli}, d)
	if cli.chamadas != 0 || res.Reescrito || res.MotivoFallback != resolver.MotivoCurto || res.FinalText != d.Texto {
		t.Fatalf("texto curto: %d chamadas, %+v", cli.chamadas, res)
	}
}

// A falha do provedor cai no texto do Smart, com o motivo.
func TestReescrever_FalhaCaiNoSmart(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	d := c.ResolverParaPolir(context.Background(), "hoje eu entreguei a dirfi para o cliente da empresa")
	cli := &polishQueEcoa{err: &resolver.ErroPolish{Motivo: resolver.MotivoChaveRecusada}}
	res := c.Reescrever(context.Background(), &resolver.PolishResolver{Smart: c.Smart(), Client: cli}, d)
	if res.Reescrito || res.FinalText != d.Texto || res.MotivoFallback != resolver.MotivoChaveRecusada {
		t.Fatalf("falha do provedor: %+v", res)
	}
	cli = &polishQueEcoa{err: errors.New("rede")}
	res = c.Reescrever(context.Background(), &resolver.PolishResolver{Smart: c.Smart(), Client: cli}, d)
	if res.Reescrito || res.FinalText != d.Texto || res.MotivoFallback != resolver.MotivoTransporte {
		t.Fatalf("falha de rede: %+v", res)
	}
}

// Com o Smart falhado, o texto não sai: o Polish nem é chamado, e o motivo é o
// da consulta local que falhou (dicionario). Sem Polish montado, sem_cliente.
func TestReescrever_SmartFalhouOuSemPolishNaoChama(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	d := c.ResolverParaPolir(context.Background(), "hoje eu entreguei a dirfi para o cliente da empresa")
	falhado := d
	falhado.SmartFalhou = true
	cli := &polishQueEcoa{}
	res := c.Reescrever(context.Background(), &resolver.PolishResolver{Smart: c.Smart(), Client: cli}, falhado)
	if cli.chamadas != 0 || res.Reescrito || res.MotivoFallback != resolver.MotivoDicionario || res.FinalText != d.Texto {
		t.Fatalf("Smart falhado: %d chamadas, %+v", cli.chamadas, res)
	}
	res = c.Reescrever(context.Background(), nil, d)
	if res.Reescrito || res.MotivoFallback != resolver.MotivoSemCliente || res.FinalText != d.Texto {
		t.Fatalf("sem Polish: %+v", res)
	}
}
