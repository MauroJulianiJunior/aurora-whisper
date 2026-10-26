package winruntime

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/notafoco"
)

// Ditado feito dentro de uma nota (ADR-022): a decisão é pura e o teste não
// toca o sistema real (nem teclado, nem clipboard, nem janela). O desfecho da
// inserção entra direto como RecordHistoryEffect, como nos outros testes deste
// pacote.

type focoFalso struct{ v *string }

func (f focoFalso) ParaOHistorico() *string { return f.v }

func ptr(s string) *string { return &s }

func TestVirouNotaDoDitado_Decisao(t *testing.T) {
	casos := []struct {
		nome     string
		foco     focoDaNota
		desfecho dictation.Outcome
		quer     string // "" = nil
	}{
		{"sem painel ligado", nil, dictation.OutcomeSuccess, ""},
		{"sem nota em foco", focoFalso{}, dictation.OutcomeSuccess, ""},
		{"nota em foco, deu certo", focoFalso{ptr("nota-1")}, dictation.OutcomeSuccess, "nota-1"},
		{"caixa nova em foco, deu certo", focoFalso{ptr("nova:tk")}, dictation.OutcomeSuccess, "nova:tk"},
		{"nota em foco, a inserção falhou: fica à vista", focoFalso{ptr("nota-1")}, dictation.OutcomeError, ""},
	}
	for _, c := range casos {
		got := virouNotaDoDitado(c.foco, c.desfecho)
		switch {
		case c.quer == "" && got != nil:
			t.Errorf("%s: %q, queria nil", c.nome, *got)
		case c.quer != "" && (got == nil || *got != c.quer):
			t.Errorf("%s: %v, queria %q", c.nome, got, c.quer)
		}
	}
}

func TestRecordHistory_DitadoDentroDaNotaNasceEscondido(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "lembrar de ligar para o contador"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	m := notafoco.Novo(nil)
	rt.UsarNotaEmFoco(m)
	m.DefinirNota("nota-9")

	ditarAteOHistorico(t, rt, 1, 16000, "brave.exe")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: "lembrar de ligar para o contador"})

	l := hist.unica(t)
	if l.VirouNota == nil || *l.VirouNota != "nota-9" {
		t.Fatalf("VirouNota = %v, queria nota-9", l.VirouNota)
	}
	// O resto da linha é o de sempre: o "Aprender" continua lendo o bruto.
	if l.RawText == "" || l.FinalText == "" || l.Status != "done" {
		t.Errorf("linha mudou além do virou_nota: bruto %q, final %q, status %q", l.RawText, l.FinalText, l.Status)
	}
}

func TestRecordHistory_CaixaNovaGravaNovaDoisPontosToken(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "primeira linha"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	m := notafoco.Novo(nil)
	rt.UsarNotaEmFoco(m)
	m.DefinirNova("0b6f1c2e")

	ditarAteOHistorico(t, rt, 1, 16000, "brave.exe")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: "primeira linha"})

	if l := hist.unica(t); l.VirouNota == nil || *l.VirouNota != "nova:0b6f1c2e" {
		t.Fatalf("VirouNota = %v", l.VirouNota)
	}
}

func TestRecordHistory_AvisoVencidoOuLimpoDaDitadoComum(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "texto comum"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	agora := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	m := notafoco.Novo(func() time.Time { return agora })
	rt.UsarNotaEmFoco(m)

	// Venceu: o painel morreu sem avisar.
	m.DefinirNota("nota-9")
	agora = agora.Add(notafoco.TTL)
	ditarAteOHistorico(t, rt, 1, 16000, "brave.exe")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: "texto comum"})
	if l := hist.linhas[0]; l.VirouNota != nil {
		t.Errorf("aviso vencido escondeu o ditado: %q", *l.VirouNota)
	}

	// Limpo: o painel avisou que o foco saiu da nota.
	m.DefinirNota("nota-9")
	m.LimparNota("nota-9")
	ditarAteOHistorico(t, rt, 2, 16000, "brave.exe")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 2, Outcome: dictation.OutcomeSuccess, Text: "texto comum"})
	if l := hist.linhas[1]; l.VirouNota != nil {
		t.Errorf("aviso limpo escondeu o ditado: %q", *l.VirouNota)
	}
}

func TestRecordHistory_FalhaDaInsercaoDentroDaNotaFicaNoHistorico(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "não coube"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	m := notafoco.Novo(nil)
	rt.UsarNotaEmFoco(m)
	m.DefinirNota("nota-9")

	ditarAteOHistorico(t, rt, 1, 16000, "brave.exe")
	rt.recordHistory(dictation.RecordHistoryEffect{
		Attempt: 1, Outcome: dictation.OutcomeError,
		Reason: dictation.ErrorReasonInsertClipboard, Text: "não coube",
	})

	l := hist.unica(t)
	if l.Status != "error" || l.VirouNota != nil {
		t.Fatalf("status %q, VirouNota %v: o erro tem de aparecer no histórico", l.Status, l.VirouNota)
	}
}
