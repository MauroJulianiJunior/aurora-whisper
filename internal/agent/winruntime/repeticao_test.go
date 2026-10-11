package winruntime

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
)

// A guarda contra o laço de repetição do ASR, ligada ao runtime: depois do
// ASR, antes da resolução, com aviso na pílula e contagens no log
// (incidente de 01-out-2026, docs/defeito-ditado-longo.md).

const (
	comecoComLaco = "Pense como um especialista em RAG e me explique como cada um deles vai ser utilizado na hora de récu,"
	copiaDoLaco   = " e como cada um deles vai ser utilizado na hora de récu,"
)

func textoComLaco() string { return comecoComLaco + strings.Repeat(copiaDoLaco, 11) }

// ditarComTexto faz um ditado inteiro até o sucesso, com o ASR devolvendo
// texto. A inserção (que a bancada não tem) responde à mão.
func (b *bancada) ditarComTexto(t *testing.T, texto string) dictation.AttemptID {
	t.Helper()
	prov := &asrFalso{texto: texto, entrou: make(chan struct{}), liberar: make(chan struct{})}
	b.rt.asr = prov
	attempt := b.ditarAteOASR(t, prov, 20*time.Second)
	b.receberTardio(t, prov) // a tempo: o prazo não venceu
	if got := b.rt.machine.State(); got != dictation.StateInserting {
		t.Fatalf("State = %v, quero StateInserting", got)
	}
	b.rt.dispatch(dictation.InsertResultEvent{Attempt: attempt, Result: dictation.InsertResultDTO{Success: true}})
	return attempt
}

func TestRepeticao_LacoSaiAntesDaResolucaoEAPilulaPedeParaConferir(t *testing.T) {
	b, _, hist, smart := bancadaTardia(t)
	b.rt.polish = false

	b.ditarComTexto(t, textoComLaco())

	if smart.chamadas != 1 {
		t.Fatalf("Smart chamado %d vez(es)", smart.chamadas)
	}
	l := hist.unica(t)
	// O bruto é o texto do ASR como chegou, com o laço: é dele que um falso
	// positivo da guarda se recupera.
	if l.RawText != textoComLaco() {
		t.Errorf("bruto = %q, quero o texto do ASR inteiro, com o laço", l.RawText)
	}
	if l.FinalText != "smart("+comecoComLaco+")" {
		t.Errorf("final = %q: a resolução recebeu o texto com o laço", l.FinalText)
	}
	if got := b.pilula.ultima(t); got != overlay.FaseRepeticaoRemovida {
		t.Errorf("pílula = %v, quero FaseRepeticaoRemovida", got)
	}
	log := b.log.String()
	for _, campo := range []string{"laço de repetição do ASR removido", "lacos=1", "palavras_removidas=132", "palavras_da_unidade=12", "motivo=repeticao_removida"} {
		if !strings.Contains(log, campo) {
			t.Errorf("o log não tem %q:\n%s", campo, log)
		}
	}
	if strings.Contains(log, "utilizado") || strings.Contains(log, "especialista") {
		t.Errorf("o texto ditado foi para o log:\n%s", log)
	}
}

// TestRepeticao_AMarcaNaoPassaParaODitadoSeguinte: o próximo ditado, sem
// laço, mostra o "Inserido" de sempre.
func TestRepeticao_AMarcaNaoPassaParaODitadoSeguinte(t *testing.T) {
	b, _, hist, _ := bancadaTardia(t)
	b.rt.polish = false
	b.ditarComTexto(t, textoComLaco())
	b.ditarComTexto(t, "Precisa transmitir a DCTFWeb até sexta.")

	if got := b.pilula.ultima(t); got != dictation.OverlaySuccess {
		t.Errorf("pílula do segundo ditado = %v, quero Inserido", got)
	}
	if len(hist.linhas) != 2 || hist.linhas[1].RawText != "Precisa transmitir a DCTFWeb até sexta." {
		t.Errorf("histórico = %+v: o original do laço passou para o ditado seguinte", hist.linhas)
	}
}

// TestRepeticao_TextoNormalPassaIntacto: sem laço, nada muda — nem o texto,
// nem a pílula, nem o log.
func TestRepeticao_TextoNormalPassaIntacto(t *testing.T) {
	b, _, hist, _ := bancadaTardia(t)
	b.rt.polish = false
	const texto = "Não, não, não. Primeiro o balanço, depois a DRE, depois o fluxo de caixa."
	b.ditarComTexto(t, texto)

	if l := hist.unica(t); l.RawText != texto {
		t.Errorf("bruto = %q, quero o texto intacto", l.RawText)
	}
	if got := b.pilula.ultima(t); got != dictation.OverlaySuccess {
		t.Errorf("pílula = %v, quero Inserido", got)
	}
	if strings.Contains(b.log.String(), "laço de repetição") {
		t.Errorf("a guarda agiu num texto sem laço:\n%s", b.log.String())
	}
}

// TestRepeticao_TextoTardioTambemPassaPelaGuarda: o texto que chega depois
// do prazo vai ao histórico já sem o laço.
func TestRepeticao_TextoTardioTambemPassaPelaGuarda(t *testing.T) {
	b, prov, hist, _ := bancadaTardia(t)
	prov.texto = textoComLaco()
	b.ditarAteOASR(t, prov, 30*time.Second)
	b.estourarOPrazo(t, 30*time.Second)
	b.receberTardio(t, prov)

	l := hist.unica(t)
	if l.RawText != textoComLaco() {
		t.Errorf("bruto tardio = %q, quero o texto do ASR inteiro, com o laço", l.RawText)
	}
	if l.FinalText != "smart("+comecoComLaco+")" {
		t.Errorf("final tardio = %q: o Smart recebeu o texto com o laço", l.FinalText)
	}
	if got := b.pilula.ultima(t); got != overlay.FaseTextoNoHistorico {
		t.Errorf("pílula = %v, quero a do texto no histórico", got)
	}
}

// TestRepeticao_FalhaDoASRNaoPassaPelaGuarda: resultado com erro não tem
// texto, e segue para o núcleo como veio.
func TestRepeticao_FalhaDoASRNaoPassaPelaGuarda(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{})
	e := dictation.TranscribeResultEvent{Attempt: 3, Reason: dictation.ErrorReasonTranscribeFailed, Result: dictation.ASRResultDTO{Text: textoComLaco()}}
	if got := rt.guardaDeRepeticao(e); got != e {
		t.Errorf("guardaDeRepeticao mexeu num resultado com erro: %+v", got)
	}
	if rt.repeticaoEm != dictation.NoAttempt {
		t.Errorf("repeticaoEm = %d, quero nenhuma", rt.repeticaoEm)
	}
}

// TestRepeticao_OriginalValeUmaVezEParaATentativaDele: o original do ASR só
// vira o bruto da tentativa em que a guarda agiu, e uma vez só; depois sai
// da memória.
func TestRepeticao_OriginalValeUmaVezEParaATentativaDele(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{})
	e := dictation.TranscribeResultEvent{Attempt: 7, Result: dictation.ASRResultDTO{Text: textoComLaco()}}
	limpo := rt.guardaDeRepeticao(e).Result.Text
	if limpo != comecoComLaco {
		t.Fatalf("texto da guarda = %q", limpo)
	}
	if got := rt.brutoParaOHistorico(8, "outro ditado"); got != "outro ditado" {
		t.Errorf("bruto da tentativa 8 = %q: levou o original da 7", got)
	}
	if got := rt.brutoParaOHistorico(7, limpo); got != textoComLaco() {
		t.Errorf("bruto da tentativa 7 = %q, quero o original", got)
	}
	if got := rt.brutoParaOHistorico(7, limpo); got != limpo {
		t.Errorf("segunda leitura = %q: o original não foi consumido", got)
	}
	if rt.originalDoASR.texto != "" {
		t.Errorf("o original ficou na memória depois de consumido")
	}
}
