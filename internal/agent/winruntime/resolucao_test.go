package winruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// resolverQueFalha existe para provar a regra mais importante deste
// caminho: dicionário com problema degrada para "sem correção", nunca
// para "sem texto".
type resolverQueFalha struct{}

func (resolverQueFalha) Resolve(context.Context, resolver.ResolveRequest) (resolver.ResolveResult, error) {
	return resolver.ResolveResult{}, errors.New("dicionário quebrado")
}

func efeito(texto string) dictation.ResolveEffect {
	return dictation.ResolveEffect{
		Attempt:   dictation.AttemptID(1),
		ASRResult: dictation.ASRResultDTO{Text: texto},
	}
}

func textoDe(t *testing.T, ev dictation.Event) string {
	t.Helper()
	rr, ok := ev.(dictation.ResolveResultEvent)
	if !ok {
		t.Fatalf("evento = %T, want ResolveResultEvent", ev)
	}
	return rr.Result.Text
}

func runtimeDeTeste(t *testing.T) *Runtime {
	t.Helper()
	return New(nil, nil, nil, nil, nil, nil, nil, nil)
}

// TestResolver_SemResolverDevolveOTextoDoASR: é o comportamento que o
// Runtime teve do Sprint 1 até 27-set-2026, e continua sendo o default
// quando não há dicionário configurado.
func TestResolver_SemResolverDevolveOTextoDoASR(t *testing.T) {
	rt := runtimeDeTeste(t)
	const bruto = "precisa transmitir a DCTF Web hoje"
	if got := textoDe(t, rt.resolver(efeito(bruto))); got != bruto {
		t.Errorf("sem resolver = %q, want %q", got, bruto)
	}
}

// TestResolver_FalhaNaoPerdeODitado é a regra que protege o produto.
//
// Um dicionário com problema — arquivo corrompido, termo malformado,
// bug no resolver — não pode fazer a pessoa PERDER o que falou. O texto
// do ASR tem de seguir para inserção de qualquer jeito.
//
// Sem esta garantia, o modo de falha seria: a pessoa fala, o overlay
// mostra sucesso, e nada aparece no campo.
func TestResolver_FalhaNaoPerdeODitado(t *testing.T) {
	rt := runtimeDeTeste(t)
	rt.UsarResolver(resolverQueFalha{}, "mauro")

	const bruto = "manda o balancete do mes passado"
	got := textoDe(t, rt.resolver(efeito(bruto)))
	if got != bruto {
		t.Errorf("com resolver quebrado = %q, want o texto do ASR intocado (%q)", got, bruto)
	}
	if got == "" {
		t.Error("o ditado foi PERDIDO — é o pior desfecho possível deste caminho")
	}
}

// TestResolver_AplicaODicionario: o outro lado — com resolver bom, a
// correção acontece de verdade.
func TestResolver_AplicaODicionario(t *testing.T) {
	s, err := dictionary.CarregarSemente(strings.NewReader(
		`{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}

	rt := runtimeDeTeste(t)
	rt.UsarResolver(resolver.NewSmartResolver(repo), "mauro")

	got := textoDe(t, rt.resolver(efeito("precisa transmitir a DCTF Web hoje")))
	if !strings.Contains(got, "DCTFWeb") {
		t.Errorf("resolvido = %q — o dicionário não foi aplicado", got)
	}
}

// TestResolver_UsuarioErradoNaoCorrigeNada trava, no Runtime, o mesmo
// defeito que já foi pego em cmd/entitybench: dictionary.Search filtra
// por UserID, e uma divergência entre o usuário da semente e o passado a
// UsarResolver faz o dicionário não achar nada — sem erro, sem log, só
// um número que não melhora.
func TestResolver_UsuarioErradoNaoCorrigeNada(t *testing.T) {
	s, _ := dictionary.CarregarSemente(strings.NewReader(
		`{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`))
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	res := resolver.NewSmartResolver(repo)
	const bruto = "precisa transmitir a DCTF Web hoje"

	certo := runtimeDeTeste(t)
	certo.UsarResolver(res, "mauro")
	if got := textoDe(t, certo.resolver(efeito(bruto))); !strings.Contains(got, "DCTFWeb") {
		t.Fatalf("com o usuário CERTO não corrigiu (%q) — o resto do teste não vale nada", got)
	}

	errado := runtimeDeTeste(t)
	errado.UsarResolver(res, "outro-usuario")
	if got := textoDe(t, errado.resolver(efeito(bruto))); strings.Contains(got, "DCTFWeb") {
		t.Errorf("com o usuário ERRADO corrigiu (%q) — o escopo por usuário não vale", got)
	}
}

// TestUsarVocabulario_ChegaNoASR: o vocabulário só vale se for parar nas
// opções de transcrição. Guardá-lo num campo que ninguém lê seria pagar
// o custo de montá-lo sem nenhum efeito.
func TestUsarVocabulario_ChegaNoASR(t *testing.T) {
	rt := runtimeDeTeste(t)
	const vocab = "DCTFWeb, PGDAS, eSocial"
	rt.UsarVocabulario(vocab)
	if rt.vocabulario != vocab {
		t.Fatalf("vocabulario = %q, want %q", rt.vocabulario, vocab)
	}
	// O uso real está em transcrever(), que monta asr.ASROptions com este
	// campo; transcrever depende de provider e slot de áudio reais e é
	// exercitado em transcribe_test.go.
}

// sementeComRestrito é o cenário que a TASK-009 existe para tornar
// seguro: um alias que colide com palavra comum do português.
const sementeComRestrito = `{
  "usuario": "mauro",
  "dominios": {"dev": ["Code.exe"]},
  "entradas": [
    {"canonica": "commit", "dominio": "dev", "alias": ["comite"], "restrito": true}
  ]
}`

func efeitoNoApp(texto, app string) dictation.ResolveEffect {
	e := efeito(texto)
	e.Focus = dictation.FocusTarget{ProcessName: app}
	return e
}

// TestResolver_AliasPerigosoSoDisparaNoDominioCerto é o teste que dá
// sentido à TASK-009 inteira.
//
// "comite" -> "commit" vale os pontos que faltam para o portão de 95%, e
// quebraria "o comitê aprovou" em qualquer e-mail. A diferença entre as
// duas coisas é saber onde a pessoa está digitando.
//
// Repare no caso do app DESCONHECIDO: ele tem de se comportar como o app
// errado, não como o certo. Domínio desconhecido é motivo para não
// arriscar.
func TestResolver_AliasPerigosoSoDisparaNoDominioCerto(t *testing.T) {
	s, err := dictionary.CarregarSemente(strings.NewReader(sementeComRestrito))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}

	rt := runtimeDeTeste(t)
	rt.UsarResolver(resolver.NewSmartResolver(repo), "mauro")
	rt.UsarDominioDoApp(s.DominioDoApp)

	const frase = "esse comite quebrou o pipeline"

	noEditor := textoDe(t, rt.resolver(efeitoNoApp(frase, "Code.exe")))
	if !strings.Contains(noEditor, "commit") {
		t.Fatalf("no editor = %q — a correção não aconteceu onde ela É certa, "+
			"e o resto deste teste não vale nada", noEditor)
	}

	noEmail := textoDe(t, rt.resolver(efeitoNoApp(frase, "OUTLOOK.EXE")))
	if strings.Contains(noEmail, "commit") {
		t.Errorf("no e-mail = %q — 'o comitê aprovou' viraria 'o commit aprovou'", noEmail)
	}

	desconhecido := textoDe(t, rt.resolver(efeitoNoApp(frase, "")))
	if strings.Contains(desconhecido, "commit") {
		t.Errorf("app DESCONHECIDO = %q — domínio desconhecido tem de se comportar "+
			"como o app errado, nunca como o certo", desconhecido)
	}
}

// TestResolver_SemMapaDeDominioNaoAplicaRestrito: se o Agent não ligar a
// tradução de app para domínio, entrada restrita simplesmente não vale.
// É o default SEGURO — não uma falha silenciosa.
func TestResolver_SemMapaDeDominioNaoAplicaRestrito(t *testing.T) {
	s, _ := dictionary.CarregarSemente(strings.NewReader(sementeComRestrito))
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}

	rt := runtimeDeTeste(t)
	rt.UsarResolver(resolver.NewSmartResolver(repo), "mauro")
	// UsarDominioDoApp NÃO chamado de propósito.

	got := textoDe(t, rt.resolver(efeitoNoApp("esse comite quebrou o pipeline", "Code.exe")))
	if strings.Contains(got, "commit") {
		t.Errorf("sem mapa de domínio = %q — entrada restrita foi aplicada sem que "+
			"nada soubesse onde a pessoa estava", got)
	}
}
