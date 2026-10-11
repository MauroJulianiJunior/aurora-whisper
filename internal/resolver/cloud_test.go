package resolver_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// fakeCloudClient é o dublê determinístico de resolver.CloudClient usado
// pelos testes deste pacote: devolve exatamente os bytes configurados
// (ou o erro configurado), e registra a última requisição recebida.
type fakeCloudClient struct {
	resp    []byte
	err     error
	calls   int
	lastReq resolver.CloudSmartRequest
}

var _ resolver.CloudClient = (*fakeCloudClient)(nil)

func (f *fakeCloudClient) ResolveSmart(_ context.Context, req resolver.CloudSmartRequest) ([]byte, error) {
	f.calls++
	f.lastReq = req
	return f.resp, f.err
}

// newAmbiguousSmartRequest monta um cenário com exatamente um span
// pendente ("comersial", que colide foneticamente com "Comercial" sob
// dictionary.PTBRSoundex — ver comentário de TestSmartResolver_TEST004)
// e SEM corroboração de contexto, para forçar o SmartResolver a acionar o
// resolver cloud restrito.
func newAmbiguousSmartRequest() (*resolver.SmartResolver, resolver.ResolveRequest) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	e := dictionary.DictionaryEntry{
		ID: "e1", UserID: testUser, CanonicalForm: "Comercial",
		NormalizedForm: dictionary.Normalize("Comercial"), Active: true,
	}
	repo.Put(e)

	sr := resolver.NewSmartResolver(repo)
	req := resolver.ResolveRequest{
		UserID:      testUser,
		ASRResult:   asr.ASRResult{Text: "abra o modulo comersial"},
		Context:     uiacontext.ContextSnapshot{},
		CloudPolicy: resolver.CloudAllowed,
	}
	return sr, req
}

// localSpanBounds resolve, para o texto local "abra o modulo comersial",
// os offsets em runas do span pendente ("comersial"), para montar
// replacements do cloud que efetivamente casem (ou não) com o span
// marcado.
const localText = "abra o modulo comersial"

var pendingSpanStart, pendingSpanEnd = len("abra o modulo "), len("abra o modulo comersial")

func TestSmartResolver_CloudReplacement_Accepted(t *testing.T) {
	sr, req := newAmbiguousSmartRequest()
	cloud := &fakeCloudClient{resp: []byte(fmt.Sprintf(
		`{"replacements":[{"start":%d,"end":%d,"replacement":"Comercial","confidence":0.9,"reason_code":"context"}]}`,
		pendingSpanStart, pendingSpanEnd,
	))}
	sr.Cloud = cloud

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cloud.calls != 1 {
		t.Fatalf("cloud.calls = %d, want 1", cloud.calls)
	}
	if !got.UsedCloud {
		t.Fatal("UsedCloud = false, want true")
	}
	const want = "abra o modulo Comercial"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q", got.FinalText, want)
	}
	if len(cloud.lastReq.UncertainSpans) != 1 || cloud.lastReq.UncertainSpans[0].Original != "comersial" {
		t.Fatalf("requisição cloud não levou o span esperado: %+v", cloud.lastReq.UncertainSpans)
	}
}

// TestSmartResolver_CloudReplacement_Invalid cobre cada caso do item 6
// da tarefa: um replacement inválido nunca pode corromper o texto — o
// resultado local (localText, com "comersial" intocado) é preservado.
func TestSmartResolver_CloudReplacement_Invalid(t *testing.T) {
	cases := []struct {
		name string
		resp string
	}{
		{
			name: "span fora dos limites do transcript",
			resp: fmt.Sprintf(`{"replacements":[{"start":0,"end":%d,"replacement":"x","confidence":0.9}]}`, len([]rune(localText))+50),
		},
		{
			name: "span nao marcado como incerto",
			resp: `{"replacements":[{"start":0,"end":4,"replacement":"Digite","confidence":0.9}]}`, // [0,4) = "abra", nunca marcado.
		},
		{
			name: "json invalido",
			resp: `{"replacements": [ not valid json`,
		},
		{
			name: "campo obrigatorio faltando",
			resp: `{"replacements":[{"start":15,"replacement":"Comercial"}]}`, // falta "end".
		},
		{
			name: "altera numeros sem evidencia",
			resp: fmt.Sprintf(`{"replacements":[{"start":%d,"end":%d,"replacement":"Comercial2024","confidence":0.9}]}`, pendingSpanStart, pendingSpanEnd),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sr, req := newAmbiguousSmartRequest()
			cloud := &fakeCloudClient{resp: []byte(tc.resp)}
			sr.Cloud = cloud

			got, err := sr.Resolve(context.Background(), req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.FinalText != localText {
				t.Fatalf("FinalText = %q, want %q (resultado local preservado; replacement inválido não pode corromper o texto)", got.FinalText, localText)
			}
		})
	}
}

// TestSmartResolver_CloudReplacement_OverlapRejected cobre a regra
// "sobrepõe outro replacement" isoladamente: como os spans que o próprio
// Smart marca como incertos são sempre disjuntos por construção (um por
// token/frase), a única forma de um cloud (mal-intencionado ou com bug)
// produzir sobreposição é mandar dois replacements para o MESMO span
// marcado — só o primeiro (na ordem de validação) pode ser aceito.
func TestSmartResolver_CloudReplacement_OverlapRejected(t *testing.T) {
	sr, req := newAmbiguousSmartRequest()
	cloud := &fakeCloudClient{resp: []byte(fmt.Sprintf(
		`{"replacements":[{"start":%d,"end":%d,"replacement":"Comercial","confidence":0.9},{"start":%d,"end":%d,"replacement":"Empresarial","confidence":0.9}]}`,
		pendingSpanStart, pendingSpanEnd, pendingSpanStart, pendingSpanEnd,
	))}
	sr.Cloud = cloud

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	cloudApplied := 0
	for _, a := range got.AppliedReplacements {
		if a.Reason == resolver.ReasonCloud {
			cloudApplied++
		}
	}
	if cloudApplied != 1 {
		t.Fatalf("aplicou %d replacements do cloud para o mesmo span, want exatamente 1 (o segundo deve ser rejeitado por sobreposição)", cloudApplied)
	}
	if got.FinalText != "abra o modulo Comercial" && got.FinalText != "abra o modulo Empresarial" {
		t.Fatalf("FinalText = %q, want um dos dois candidatos aplicado uma única vez", got.FinalText)
	}
}

// TestSmartResolver_CloudTransportError garante o fallback "resolver
// falhou -> local result" (docs/ai-spec/16-ai-behavior-spec.md,
// "Fallback") quando a chamada ao cloud falha no transporte.
func TestSmartResolver_CloudTransportError(t *testing.T) {
	sr, req := newAmbiguousSmartRequest()
	cloud := &fakeCloudClient{err: fmt.Errorf("network down")}
	sr.Cloud = cloud

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.FinalText != localText {
		t.Fatalf("FinalText = %q, want %q", got.FinalText, localText)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("esperava warning documentando a falha do cloud")
	}
}

// TestSmartResolver_CloudNotCalledWhenPolicyDisallows garante AC-030: sem
// CloudPolicy == CloudAllowed, o resolver cloud restrito nunca é chamado,
// mesmo com CloudClient configurado e span pendente.
func TestSmartResolver_CloudNotCalledWhenPolicyDisallows(t *testing.T) {
	sr, req := newAmbiguousSmartRequest()
	req.CloudPolicy = resolver.CloudDisabled
	cloud := &fakeCloudClient{resp: []byte(`{"replacements":[]}`)}
	sr.Cloud = cloud

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cloud.calls != 0 {
		t.Fatalf("cloud.calls = %d, want 0 (CloudPolicy == CloudDisabled)", cloud.calls)
	}
	if got.UsedCloud {
		t.Fatal("UsedCloud = true, want false")
	}
}

// TestSmartResolver_CloudNeverCalledWhenNothingPending: nada incerto,
// nada para o cloud fazer — nunca chamado (AC-030, "resolver cloud
// restrito se necessário").
func TestSmartResolver_CloudNeverCalledWhenNothingPending(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	sr := resolver.NewSmartResolver(repo)
	cloud := &fakeCloudClient{resp: []byte(`{"replacements":[]}`)}
	sr.Cloud = cloud

	_, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:      testUser,
		ASRResult:   asr.ASRResult{Text: "bom dia como vai voce"},
		CloudPolicy: resolver.CloudAllowed,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cloud.calls != 0 {
		t.Fatalf("cloud.calls = %d, want 0 (nada pendente)", cloud.calls)
	}
}
