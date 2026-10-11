package asr_test

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/mock"
)

// fatalOnCallCloud é o espião de cloud gateway exigido por AC-031: prova
// por construção — não por inspeção de log — que local_only nunca envia
// áudio (nem contexto) para fora, chamando t.Fatal caso qualquer método
// seja invocado.
type fatalOnCallCloud struct {
	t *testing.T
}

func (f fatalOnCallCloud) ID() string { return "spy-cloud-must-not-be-called" }
func (f fatalOnCallCloud) Capabilities() asr.ASRCapabilities {
	f.t.Fatal("cloud gateway espião: Capabilities() chamado, mas policy é local_only")
	return asr.ASRCapabilities{}
}
func (f fatalOnCallCloud) Warmup(ctx context.Context) error {
	f.t.Fatal("cloud gateway espião: Warmup() chamado, mas policy é local_only")
	return nil
}
func (f fatalOnCallCloud) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	f.t.Fatal("cloud gateway espião: Transcribe() chamado — áudio teria saído do dispositivo em policy local_only")
	return asr.ASRResult{}, nil
}

// recordingProvider envolve um asr.ASRProvider e registra, a cada
// Transcribe, o endereço do primeiro elemento de AudioBuffer.PCM — usado
// para provar que o MESMO array em RAM (não uma cópia) chega a cada
// provider consultado (TEST-010).
type recordingProvider struct {
	asr.ASRProvider
	ptrs []uintptr
}

func (r *recordingProvider) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	if len(audio.PCM) > 0 {
		r.ptrs = append(r.ptrs, uintptr(unsafe.Pointer(&audio.PCM[0])))
	}
	return r.ASRProvider.Transcribe(ctx, audio, opts)
}

// bufferWithCounter cria um AudioBuffer cujo Release incrementa
// *releases, para provar alocado-vs-liberado (TEST-010).
func bufferWithCounter(releases *int) asr.AudioBuffer {
	return asr.AudioBuffer{
		PCM:          []int16{1, 2, 3, 4},
		SampleRateHz: 16000,
		Release:      func() { *releases++ },
	}
}

func TestEngine_LocalOnly_NeverCallsCloud_AC030_AC031(t *testing.T) {
	// Resultado local propositalmente inadequado (texto vazio): se o
	// Engine ignorasse a policy e tentasse um fallback, o teste falharia
	// via t.Fatal dentro do espião.
	local := mock.NewUnscored("")
	spyCloud := fatalOnCallCloud{t: t}

	releases := 0
	e := &asr.Engine{Local: local, Cloud: spyCloud}

	_, err := e.Transcribe(context.Background(), asr.PolicyLocalOnly, bufferWithCounter(&releases), asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if local.Calls != 1 {
		t.Fatalf("local.Calls = %d, want 1", local.Calls)
	}
	if releases != 1 {
		t.Fatalf("releases = %d, want 1 (buffer deve ser liberado exatamente uma vez)", releases)
	}
}

func TestEngine_LocalOnly_RequiresLocalProvider(t *testing.T) {
	e := &asr.Engine{}
	_, err := e.Transcribe(context.Background(), asr.PolicyLocalOnly, asr.AudioBuffer{}, asr.ASROptions{})
	if !errors.Is(err, asr.ErrNoLocalProvider) {
		t.Fatalf("err = %v, want ErrNoLocalProvider", err)
	}
}

func TestEngine_LocalThenCloud_AdequateLocal_SkipsCloud_AC030(t *testing.T) {
	local := &recordingProvider{ASRProvider: mock.NewConfident("resultado bom", 0.95)}
	spyCloud := fatalOnCallCloud{t: t}

	e := &asr.Engine{
		Local:           local,
		Cloud:           spyCloud,
		CloudCredential: asr.CloudCredential{Token: "tok"},
		Adequate:        asr.DefaultAdequate(0.5),
	}

	result, err := e.Transcribe(context.Background(), asr.PolicyLocalThenCloud, bufferWithCounter(new(int)), asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "resultado bom" {
		t.Fatalf("result.Text = %q, want resultado local (cloud não deveria ter sido chamado)", result.Text)
	}
}

func TestEngine_LocalThenCloud_InadequateLocal_FallsBackToCloud_TEST010(t *testing.T) {
	local := &recordingProvider{ASRProvider: mock.NewConfident("baixa confianca", 0.1)} // abaixo do threshold 0.5
	cloud := &recordingProvider{ASRProvider: mock.NewConfident("resultado cloud", 0.99)}

	releases := 0
	e := &asr.Engine{
		Local:           local,
		Cloud:           cloud,
		CloudCredential: asr.CloudCredential{Token: "tok"},
		Adequate:        asr.DefaultAdequate(0.5),
	}

	buf := bufferWithCounter(&releases)
	result, err := e.Transcribe(context.Background(), asr.PolicyLocalThenCloud, buf, asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "resultado cloud" {
		t.Fatalf("result.Text = %q, want resultado do cloud (fallback deveria ter ocorrido)", result.Text)
	}

	// Ambos os providers precisam ter sido consultados exatamente uma vez.
	if len(local.ptrs) != 1 || len(cloud.ptrs) != 1 {
		t.Fatalf("local calls=%d cloud calls=%d, want 1 e 1", len(local.ptrs), len(cloud.ptrs))
	}
	// TEST-010: o MESMO array em RAM (não uma cópia) chega a ambos.
	if local.ptrs[0] != cloud.ptrs[0] {
		t.Fatal("local e cloud receberam arrays diferentes — o buffer foi copiado em vez de reaproveitado")
	}
	// TEST-010: o buffer é liberado exatamente uma vez, ao final — nunca
	// duas vezes (uma por provider) nem zero.
	if releases != 1 {
		t.Fatalf("releases = %d, want 1", releases)
	}
}

func TestEngine_LocalThenCloud_InadequateLocal_NoCloudConfigured_DegradesToLocal(t *testing.T) {
	local := mock.NewUnscored("") // texto vazio: inadequado por DefaultAdequate

	releases := 0
	e := &asr.Engine{Local: local, Adequate: asr.DefaultAdequate(0.5)} // sem Cloud

	result, err := e.Transcribe(context.Background(), asr.PolicyLocalThenCloud, bufferWithCounter(&releases), asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe não deveria falhar ao degradar para local: %v", err)
	}
	if result.Text != "" {
		t.Fatalf("result.Text = %q, want resultado local degradado", result.Text)
	}
	if releases != 1 {
		t.Fatalf("releases = %d, want 1", releases)
	}
}

func TestEngine_LocalThenCloud_InadequateLocal_CloudNotAuthenticated_DegradesToLocal(t *testing.T) {
	local := mock.NewUnscored("")
	spyCloud := fatalOnCallCloud{t: t} // sem credencial, nem deve ser tocado

	e := &asr.Engine{Local: local, Cloud: spyCloud, Adequate: asr.DefaultAdequate(0.5)}

	result, err := e.Transcribe(context.Background(), asr.PolicyLocalThenCloud, asr.AudioBuffer{}, asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "" {
		t.Fatalf("result.Text = %q, want resultado local degradado", result.Text)
	}
}

func TestEngine_CloudOnly_RequiresAuthentication_AC032(t *testing.T) {
	spyCloud := fatalOnCallCloud{t: t}
	e := &asr.Engine{Cloud: spyCloud} // sem CloudCredential

	_, err := e.Transcribe(context.Background(), asr.PolicyCloudOnly, asr.AudioBuffer{}, asr.ASROptions{})
	if !errors.Is(err, asr.ErrCloudNotAuthenticated) {
		t.Fatalf("err = %v, want ErrCloudNotAuthenticated", err)
	}
}

func TestEngine_CloudOnly_RequiresCloudProvider(t *testing.T) {
	e := &asr.Engine{CloudCredential: asr.CloudCredential{Token: "tok"}} // sem Cloud
	_, err := e.Transcribe(context.Background(), asr.PolicyCloudOnly, asr.AudioBuffer{}, asr.ASROptions{})
	if !errors.Is(err, asr.ErrNoCloudProvider) {
		t.Fatalf("err = %v, want ErrNoCloudProvider", err)
	}
}

func TestEngine_CloudOnly_Authenticated_CallsCloud(t *testing.T) {
	cloud := mock.NewConfident("resultado cloud", 0.9)
	e := &asr.Engine{Cloud: cloud, CloudCredential: asr.CloudCredential{Token: "tok"}}

	result, err := e.Transcribe(context.Background(), asr.PolicyCloudOnly, asr.AudioBuffer{}, asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "resultado cloud" || cloud.Calls != 1 {
		t.Fatalf("result=%+v cloud.Calls=%d", result, cloud.Calls)
	}
}

func TestEngine_UnknownPolicy(t *testing.T) {
	e := &asr.Engine{}
	_, err := e.Transcribe(context.Background(), asr.Policy(99), asr.AudioBuffer{}, asr.ASROptions{})
	if !errors.Is(err, asr.ErrUnknownPolicy) {
		t.Fatalf("err = %v, want ErrUnknownPolicy", err)
	}
}

// TestEngine_FailingProviderDegradesWithoutPanic prova que um provider que
// sempre falha faz a policy degradar (retornar erro) em vez de gerar
// panic, em qualquer policy.
func TestEngine_FailingProviderDegradesWithoutPanic(t *testing.T) {
	t.Run("local_only", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic inesperado: %v", r)
			}
		}()
		e := &asr.Engine{Local: &mock.Failing{}}
		_, err := e.Transcribe(context.Background(), asr.PolicyLocalOnly, asr.AudioBuffer{}, asr.ASROptions{})
		if !errors.Is(err, mock.ErrMockFailure) {
			t.Fatalf("err = %v, want ErrMockFailure", err)
		}
	})

	t.Run("local_then_cloud falling over to cloud", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic inesperado: %v", r)
			}
		}()
		cloud := mock.NewConfident("resultado cloud", 0.9)
		e := &asr.Engine{
			Local:           &mock.Failing{},
			Cloud:           cloud,
			CloudCredential: asr.CloudCredential{Token: "tok"},
		}
		result, err := e.Transcribe(context.Background(), asr.PolicyLocalThenCloud, asr.AudioBuffer{}, asr.ASROptions{})
		if err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if result.Text != "resultado cloud" {
			t.Fatalf("result.Text = %q, want fallback para cloud", result.Text)
		}
	})

	t.Run("cloud_only", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic inesperado: %v", r)
			}
		}()
		e := &asr.Engine{Cloud: &mock.Failing{}, CloudCredential: asr.CloudCredential{Token: "tok"}}
		_, err := e.Transcribe(context.Background(), asr.PolicyCloudOnly, asr.AudioBuffer{}, asr.ASROptions{})
		if !errors.Is(err, mock.ErrMockFailure) {
			t.Fatalf("err = %v, want ErrMockFailure", err)
		}
	})
}

// TestDefaultAdequate_NeverInventsConfidence prova que providers sem
// confidence não são penalizados pelo critério de threshold — a ausência
// de confidence simplesmente não participa da decisão.
func TestDefaultAdequate_NeverInventsConfidence(t *testing.T) {
	adequate := asr.DefaultAdequate(0.9) // threshold alto
	result := asr.ASRResult{Text: "texto valido", Confidence: asr.NoConfidence}

	if !adequate(result, nil) {
		t.Fatal("resultado sem confidence e com texto não vazio deveria ser adequado, mesmo com threshold alto")
	}
}
