package main

import (
	"net/http"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------
// A latência morna é medida com a conexão reaproveitada, e o Agent, com ditados
// espaçados por mais de 90 s, paga TCP+TLS quase sempre. -conexao-nova mede esse
// pior caso; sem ela, o relatório diz que o p95 o subestima (revisão da S3).
// ---------------------------------------------------------------------

func TestSemReaproveitar_PedeFecharSemMexerNoPedidoOriginal(t *testing.T) {
	var visto *http.Request
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		visto = r
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", strings.NewReader("corpo"))
	resp, err := semReaproveitar{base: base}.RoundTrip(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resposta = %v, %v", resp, err)
	}
	if visto == nil || !visto.Close {
		t.Error("o transporte de baixo devia receber o pedido com Close = true")
	}
	if req.Close {
		t.Error("o pedido original foi alterado (um RoundTripper não pode mexer no pedido que recebe)")
	}
	if visto.URL.String() != "http://127.0.0.1:1/x" || visto.Method != http.MethodPost {
		t.Errorf("o clone perdeu o destino: %s %s", visto.Method, visto.URL)
	}
}

func TestRun_ConexaoNovaFechaACadaChamada_SemAFlagReaproveita(t *testing.T) {
	for _, nova := range []bool{false, true} {
		frases, refs := datasetPequeno()
		a := novoAmbiente(t, nil, nil)
		flags := []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"}
		if nova {
			flags = append(flags, "-conexao-nova")
		}
		if cod := a.rodar(juntar(escreverDataset(t, frases, refs), flags)...); cod != 0 {
			t.Fatalf("nova=%v: saiu com %d\n%s\n%s", nova, cod, a.stdout.String(), a.stderr.String())
		}
		pedidos := a.openai.Pedidos()
		if len(pedidos) != 7 {
			t.Fatalf("nova=%v: %d pedidos, quero 7", nova, len(pedidos))
		}
		for i, p := range pedidos {
			if p.ConexaoFechada != nova {
				t.Errorf("nova=%v: o pedido %d veio com Connection: close = %v", nova, i, p.ConexaoFechada)
			}
		}
		rel := a.lerRelatorio()
		if nova {
			contem(t, rel, "- **Conexão:** **nova a cada chamada** (`-conexao-nova`")
			contem(t, rel, "A latência foi medida com conexão nova a cada chamada")
			naoContem(t, rel, "subestimam esse caso")
			contem(t, rel, "-conexao-nova")
		} else {
			contem(t, rel, "- **Conexão:** **reaproveitada**")
			contem(t, rel, "o p95 daqui **subestima** esse caso")
			contem(t, rel, "o p95 e o fallback `prazo` medidos aqui subestimam esse caso")
			naoContem(t, rel, "A latência foi medida com conexão nova")
		}
	}
}

func TestComandoDe_ConexaoNovaFicaNoRelatorio(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	frases, refs := datasetPequeno()
	a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1", "-conexao-nova"})...)
	contem(t, a.lerRelatorio(), "Comando: `polishbench -gastar -modelos openai:m1 -preco openai:m1=1/1 -conexao-nova`")
}

func TestComandoDe_ConexaoNovaFalsaNaoApareceNoComando(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	frases, refs := datasetPequeno()
	a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1", "-conexao-nova=false"})...)
	rel := a.lerRelatorio()
	contem(t, rel, "Comando: `polishbench -gastar -modelos openai:m1 -preco openai:m1=1/1`")
	contem(t, rel, "- **Conexão:** **reaproveitada**")
}
