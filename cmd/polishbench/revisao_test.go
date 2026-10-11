package main

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// Testes das correções da revisão da S3. Cada um nasceu de um mutante que
// sobreviveu ou de um caso que a revisão provou com dublês; nenhum sai da máquina.

// Achado alto: a auditoria era mais estrita que a guarda na caixa do termo e do
// nome. Um modelo perfeito que escreve "Lucro Presumido" (a grafia do dicionário),
// ou que deixa "a debora" como o Smart entregou, saía com "BURACO NA GUARDA" e
// reprovava por uma única chamada em ~387. Aqui a guarda de verdade aceita, e a
// auditoria tem de concordar que nada mudou de fato nem de entidade.
func TestCalcular_GrafiaQueAGuardaAceitaNaoViraBuracoNaGuarda(t *testing.T) {
	lucro := c(CatPergunta, "pode me explicar a diferença entre lucro presumido e lucro real",
		"Pode me explicar a diferença entre lucro presumido e lucro real?", "Lucro Presumido")
	debora := c(CatArmadilha, "a debora mandou o balancete de agosto", "A Debora mandou o balancete de agosto.")
	lucro.ID, debora.ID = "lucro", "debora"
	casos := []Caso{lucro, debora}

	saidas := []struct {
		caso  int
		saida string
	}{
		{0, "Pode me explicar a diferença entre Lucro Presumido e lucro real?"}, // a grafia do dicionário
		{0, "Pode me explicar a diferença entre lucro presumido e lucro real?"}, // a da entrada
		{1, "a debora mandou o balancete de agosto"},                            // a própria entrada
		{1, "A debora mandou o balancete de agosto."},                           // só a inicial da frase
	}
	var rs []Resultado
	for i, s := range saidas {
		r := resultadoDe(casos[s.caso], s.caso, i+1, s.saida, time.Second, ia.Tokens{Entrada: 1200, Saida: 80})
		if !r.Res.Reescrito {
			t.Fatalf("a guarda devia aceitar %q, mas caiu com %q (o teste supõe a guarda aceitando)", s.saida, r.Res.MotivoFallback)
		}
		rs = append(rs, r)
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})

	if m.ChamadasCriticasDepois != 0 || m.ChamadasCriticasAntes != 0 {
		t.Errorf("falha crítica depois/antes = %d/%d, quero 0/0 (críticas: %v)", m.ChamadasCriticasDepois, m.ChamadasCriticasAntes, m.CriticasDepois)
	}
	if m.LevesCaixa != 2 {
		t.Errorf("nomes sem a maiúscula do esperado = %d, quero 2 (as duas saídas da debora em minúscula)", m.LevesCaixa)
	}
	crit := Criterios{FallbackMax: 0.05, P95Max: 2500 * time.Millisecond}
	if v := avaliar(m, crit); !v.Passou {
		t.Errorf("o modelo perfeito não passou na regra: %v", v.Motivos)
	}
}

// No comando inteiro, com o provedor falso: o modelo que devolve o termo na
// grafia do dicionário passa na regra e vira o vencedor.
func TestRun_ModeloQueUsaAGrafiaDoDicionarioPassaEVence(t *testing.T) {
	lucro := Caso{ID: "lucro-1", Categoria: CatPergunta, Termos: []string{"Lucro Presumido"},
		Entrada:  "pode me explicar a diferença entre lucro presumido e lucro real",
		Esperado: "Pode me explicar a diferença entre lucro presumido e lucro real?"}
	debora := Caso{ID: "debora-1", Categoria: CatArmadilha,
		Entrada:  "a debora mandou o balancete de agosto",
		Esperado: "A Debora mandou o balancete de agosto."}
	ref := Caso{ID: prefixoJaOrganiza + "geral-01", Categoria: CatJaOrganizada,
		Entrada:  "Depois do almoço eu volto e a gente termina isso.",
		Esperado: "Depois do almoço eu volto e a gente termina isso."}
	modelo := func(p pedidoFalso) respostaFalsa {
		switch p.Ditado {
		case lucro.Entrada:
			return respostaFalsa{Final: "Pode me explicar a diferença entre Lucro Presumido e lucro real?"}
		case debora.Entrada:
			return respostaFalsa{Final: debora.Entrada}
		}
		return respostaFalsa{Final: p.Ditado}
	}
	a := novoAmbiente(t, modelo, nil)
	cod := a.rodar(juntar(escreverDataset(t, []Caso{lucro, debora}, []Caso{ref}), []string{"-gastar", "-rodadas", "3", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	if got := celula(t, rel, rotuloDepois, 1); !strings.HasPrefix(got, "0/9") {
		t.Errorf("falha crítica depois = %q, quero 0/9", got)
	}
	naoContem(t, rel, "BURACO NA GUARDA")
	contem(t, rel, "- **OpenAI:** `openai:m1`.")
	contem(t, rel, "3 nome(s) sem a maiúscula que o esperado põe")
}
