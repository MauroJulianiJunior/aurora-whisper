package main

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// ---------------------------------------------------------------------
// Dois mutantes que a revisão da S3 deixou vivos: o descarte da chamada cortada
// por cancelamento e a máscara da chave no resumo do terminal.
// ---------------------------------------------------------------------

// Uma chamada cortada pelo Ctrl+C (ou pelo aborto do candidato, com chamadas em
// paralelo) não mediu nada: não pode entrar nos resultados como se o provedor
// tivesse falhado (transporte) nem contar nas métricas. O terceiro pedido a
// chegar ao provedor cancela a rodada; as chamadas em voo são cortadas.
func TestExecutor_ChamadaCortadaPorCancelamentoNaoEntraNosResultados(t *testing.T) {
	for _, paralelo := range []int{1, 2} {
		casos := casosPequenos()
		ctx, cancelar := context.WithCancel(t.Context())
		defer cancelar()
		var recebidos atomic.Int32
		prov := novoProvedorFalso(t, novoRelogio(), func(p pedidoFalso) respostaFalsa {
			if recebidos.Add(1) == 3 {
				cancelar() // o Ctrl+C chega com as chamadas 2 e 3 em voo (ou a 3 sozinha)
			}
			return respostaFalsa{Final: p.Ditado, Dormir: 300 * time.Millisecond}
		})
		e := &Executor{Cfg: Config{Casos: casos, Rodadas: 1, Paralelo: paralelo, PrazoMax: 10 * time.Second, PrazoAgente: 3500 * time.Millisecond, LimiteUSD: 10}, Agora: time.Now}
		cli := &ia.Cliente{HTTP: &http.Client{Transport: transporteDeTeste()}, BaseOpenAI: prov.URL(), Timeout: time.Hour}
		s := e.Rodar(ctx, Candidato{Provedor: ia.OpenAI, Modelo: "m"}, Preco{1, 1}, cli, nil, chaveOpenAI, &gasto{})

		if s.Aborto != AbortoInterrompido {
			t.Errorf("paralelo=%d: Aborto = %q, quero %q", paralelo, s.Aborto, AbortoInterrompido)
		}
		if len(s.Resultados) == 0 || len(s.Resultados) >= 3 {
			t.Errorf("paralelo=%d: %d resultados, quero 1 ou 2 (só as chamadas que terminaram antes do corte)", paralelo, len(s.Resultados))
		}
		for _, r := range s.Resultados {
			if r.Res.MotivoFallback != resolver.MotivoNenhum {
				t.Errorf("paralelo=%d: a chamada cortada entrou nos resultados como %q", paralelo, r.Res.MotivoFallback)
			}
		}
		m := calcular(Config{Casos: casos, Rodadas: 1, PrazoAgente: 3500 * time.Millisecond, PrazoMax: 10 * time.Second}, s)
		if m.Chamadas != len(s.Resultados) || m.FallbackTotal != 0 || m.FallbackPorMotivo["transporte"] != 0 {
			t.Errorf("paralelo=%d: a chamada cortada contou nas métricas: chamadas=%d fallback=%d %v", paralelo, m.Chamadas, m.FallbackTotal, m.FallbackPorMotivo)
		}
	}
}

// No comando inteiro: Ctrl+C no meio da rodada grava o relatório incompleto só
// com o que foi medido, e a chamada em voo não vira falha do provedor.
func TestRun_CtrlCNoMeioNaoContaAChamadaEmVoo(t *testing.T) {
	frases, refs := datasetPequeno()
	oraculo := comOraculo(append(frases, refs...))
	ctx, cancelar := context.WithCancel(t.Context())
	defer cancelar()
	var n atomic.Int32
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		r := oraculo(p)
		if n.Add(1) == 4 {
			cancelar()
			r.Dormir = 300 * time.Millisecond
		}
		return r
	}, nil)
	a.ctx = ctx
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 1 {
		t.Fatalf("saiu com %d, quero 1 (rodada interrompida)", cod)
	}
	rel := a.lerRelatorio()
	contem(t, rel, "RODADA INCOMPLETA")
	if got := celula(t, rel, "Chamadas ao provedor", 1); !strings.HasPrefix(got, "3 ") {
		t.Errorf("chamadas = %q, quero 3 (a quarta foi cortada e não conta)", got)
	}
	naoContem(t, rel, "`transporte`")
}

// O resumo do terminal passa por mascarar, mesmo que um dia carregue texto de
// fora: aqui o motivo do aborto (que vem do provedor) leva a chave.
func TestTextoDoResumo_MascaraAChave(t *testing.T) {
	r := rodadaDeTeste(t)
	r.Metricas[0].Aborto = "erro do provedor que citou a chave " + chaveOpenAI + " por engano"
	bruto := resumo(r)
	if !strings.Contains(bruto, chaveOpenAI) {
		t.Fatal("o teste supõe que o resumo bruto leva o motivo do aborto")
	}
	got := textoDoResumo(r, []string{chaveOpenAI})
	naoContem(t, got, chaveOpenAI)
	naoContem(t, got, "CANARIO")
	contem(t, got, "[chave]")
	// Sem chave conhecida, nada é trocado.
	if got := textoDoResumo(r, nil); got != bruto {
		t.Error("sem segredos o resumo devia sair como está")
	}
}
