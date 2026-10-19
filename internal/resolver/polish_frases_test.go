package resolver_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// polirCom roda Polir inteiro, com o cliente falso devolvendo o final_text.
func polirCom(t *testing.T, entrada, saida string, termos ...string) resolver.ResolveResult {
	t.Helper()
	bruto, _ := json.Marshal(map[string]string{"final_text": saida})
	return resolver.Polir(context.Background(), &fakePolishClient{resp: bruto}, smartPronto(entrada), resolver.PolirOpcoes{Termos: termos})
}

// O aceite por frase (ADR-016, emenda de 19-out-2026): a frase que passa fica
// com o Polish, a que não passa volta ao Smart, e o resto do texto não perde
// a organização.
func TestPolir_PorFrase(t *testing.T) {
	casos := []struct {
		nome, entrada, saida string
		termos               []string
		final                string // vazio: o Smart inteiro (fallback)
		frases, revertidas   int
		motivo               resolver.MotivoFallback
	}{
		{
			nome:    "a frase do meio apagou uma palavra",
			entrada: "o cliente mandou o extrato ontem e você confere hoje se bater a gente fecha o mês amanhã cedo",
			saida:   "O cliente mandou o extrato ontem. E confere hoje. Se bater, a gente fecha o mês amanhã cedo.",
			final:   "O cliente mandou o extrato ontem. E você confere hoje. Se bater, a gente fecha o mês amanhã cedo.",
			frases:  3, revertidas: 1,
		},
		{
			nome:    "o número trocado volta ao Smart só na frase dele",
			entrada: "o aluguel subiu para R$ 1.500 este mês e o condomínio ficou igual então a gente ajusta a planilha hoje",
			saida:   "O aluguel subiu para R$ 1,500 este mês. E o condomínio ficou igual. Então a gente ajusta a planilha hoje.",
			final:   "O aluguel subiu para R$ 1.500 este mês. E o condomínio ficou igual. Então a gente ajusta a planilha hoje.",
			frases:  3, revertidas: 1,
		},
		{
			nome:    "a sigla da frase de fora começa sem virar maiúscula",
			entrada: "o cliente mandou tudo ontem eSocial travou de novo então a gente reenvia amanhã",
			saida:   "O cliente mandou tudo ontem. ESocial travou de novo. Então a gente reenvia amanhã.",
			final:   "O cliente mandou tudo ontem. eSocial travou de novo. Então a gente reenvia amanhã.",
			frases:  3, revertidas: 1,
		},
		{
			nome:    "o trecho do Smart que fecha com vírgula ganha o ponto",
			entrada: "a guia venceu ontem, você paga hoje, e amanhã a gente confere o recibo",
			saida:   "Guia venceu ontem. Você paga hoje. E amanhã a gente confere o recibo.",
			final:   "A guia venceu ontem. Você paga hoje. E amanhã a gente confere o recibo.",
			frases:  3, revertidas: 1,
		},
		{
			nome:    "mais da metade cai: o Smart inteiro",
			entrada: "eu mandei o extrato ontem e você confere hoje e ele fecha amanhã",
			saida:   "Mandei o extrato ontem. E você confere hoje. E fecha amanhã.",
			motivo:  resolver.MotivoPalavras,
		},
		{
			nome:    "uma frase só: o tudo ou nada de sempre",
			entrada: "eu mandei o extrato ontem para o cliente conferir",
			saida:   "Mandei o extrato ontem para o cliente conferir.",
			motivo:  resolver.MotivoPalavras,
		},
		{
			nome:    "lista: fica de fora",
			entrada: "faltam o contrato social o extrato e você manda o recibo",
			saida:   "Faltam:\n- O contrato social\n- O extrato\n- Manda o recibo",
			motivo:  resolver.MotivoPalavras,
		},
		{
			nome:    "quebra de linha sem lista: fica de fora",
			entrada: "o cliente mandou o extrato ontem e você confere hoje se bater a gente fecha o mês amanhã cedo",
			saida:   "O cliente mandou o extrato ontem.\nE confere hoje.\nSe bater, a gente fecha o mês amanhã cedo.",
			motivo:  resolver.MotivoPalavras,
		},
		{
			nome:    "frase acrescentada no meio: o Smart inteiro",
			entrada: "o cliente mandou o extrato ontem e a gente fecha o mês amanhã cedo sem falta",
			saida:   "O cliente mandou o extrato ontem. Está tudo certo. E a gente fecha o mês amanhã cedo sem falta.",
		},
		{
			nome:    "corte sem âncora junta as frases",
			entrada: "o cliente mandou o extrato ontem e você confere hoje se bater a gente fecha o mês amanhã cedo",
			saida:   "O cliente mandou o extrato ontem. Confere hoje? Se bater, a gente fecha o mês amanhã cedo.",
			final:   "O cliente mandou o extrato ontem e você confere hoje. Se bater, a gente fecha o mês amanhã cedo.",
			frases:  2, revertidas: 1,
		},
		{
			nome:    "termo do dicionário trocado volta só na frase dele",
			entrada: "a DCTFWeb venceu ontem e o cliente pagou hoje cedo então a gente arquiva o recibo",
			saida:   "A DCTFweb venceu ontem. E o cliente pagou hoje cedo. Então a gente arquiva o recibo.",
			termos:  []string{"DCTFWeb"},
			final:   "A DCTFWeb venceu ontem. E o cliente pagou hoje cedo. Então a gente arquiva o recibo.",
			frases:  3, revertidas: 1,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := polirCom(t, c.entrada, c.saida, c.termos...)
			if c.final == "" {
				if got.Reescrito || got.FinalText != c.entrada {
					t.Fatalf("quero o Smart inteiro; Reescrito=%v, final %q", got.Reescrito, got.FinalText)
				}
				if c.motivo != "" && got.MotivoFallback != c.motivo {
					t.Errorf("motivo = %q, quero %q", got.MotivoFallback, c.motivo)
				}
				if got.Frases != 0 || got.FrasesRevertidas != 0 {
					t.Errorf("no fallback as frases ficam zeradas: %d/%d", got.FrasesRevertidas, got.Frases)
				}
				return
			}
			if !got.Reescrito || got.MotivoFallback != resolver.MotivoNenhum {
				t.Fatalf("quero o aceite por frase; Reescrito=%v, motivo %q", got.Reescrito, got.MotivoFallback)
			}
			if got.FinalText != c.final {
				t.Errorf("final = %q\n quero %q", got.FinalText, c.final)
			}
			if got.Frases != c.frases || got.FrasesRevertidas != c.revertidas {
				t.Errorf("frases = %d revertidas de %d, quero %d de %d", got.FrasesRevertidas, got.Frases, c.revertidas, c.frases)
			}
		})
	}
}

// A resposta que passa inteira não vira aceite por frase.
func TestPolir_InteiraNaoContaFrases(t *testing.T) {
	got := polirCom(t, "o cliente mandou o extrato ontem e a gente fecha amanhã", "O cliente mandou o extrato ontem. E a gente fecha amanhã.")
	if !got.Reescrito || got.Frases != 0 || got.FrasesRevertidas != 0 {
		t.Errorf("Reescrito=%v, frases %d/%d", got.Reescrito, got.FrasesRevertidas, got.Frases)
	}
}
