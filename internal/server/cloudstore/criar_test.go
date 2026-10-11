package cloudstore

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// Testes puros do ditado que nasce na nuvem (ADR-021, C1): a linha que a
// regra manda gravar e as recusas. A suíte de contrato
// (internal/server/contracttest) prova o Store.Create e o Store.Situacao
// contra o servidor em memória e contra o Postgres.

const testCelular = "0192a000-0000-7000-8000-0000000000c1"

// atorDoApp é o Actor da sessão do navegador: sem aparelho.
var atorDoApp = Actor{UserID: testUser}

// novoDitado é um ditado novo válido do app, com as trocas feitas por mudar.
func novoDitado(mudar ...func(*NewDictation)) NewDictation {
	pt, modelo, dur, proc, conf := "pt", "gpt-4o-mini-transcribe", int64(2500), int64(1800), float32(0.9)
	n := NewDictation{
		SourcePlatform: SourcePlatformWeb, RawText: "texto bruto do celular", FinalText: "Texto final do celular.",
		Mode: "smart", Language: &pt, ASRProvider: "openai", ASRModel: &modelo,
		DurationMs: &dur, ProcessingMs: &proc, Confidence: &conf,
	}
	for _, m := range mudar {
		m(&n)
	}
	return n
}

func TestNewTranscription_ALinhaDoCelular(t *testing.T) {
	agora := time.Date(2026, 10, 10, 15, 30, 0, 123456789, time.FixedZone("BRT", -3*3600))
	n := novoDitado()
	// O id em maiúsculas vira o canônico.
	got, err := NewTranscription(atorDoApp, strings.ToUpper(testCelular), n, agora)
	if err != nil {
		t.Fatalf("NewTranscription: %v", err)
	}
	quando := Normalize(agora)
	if got.ID != testCelular || got.UserID != testUser {
		t.Errorf("id/usuário = %q/%q", got.ID, got.UserID)
	}
	if got.DeviceID != nil || got.TextDeviceID != nil || got.TrashDeviceID != nil {
		t.Errorf("a linha do celular não tem aparelho: %v %v %v", got.DeviceID, got.TextDeviceID, got.TrashDeviceID)
	}
	if got.SourcePlatform != "web" || got.Status != "done" || got.Mode != "smart" || got.ASRProvider != "openai" {
		t.Errorf("origem/status/modo/provedor = %q %q %q %q", got.SourcePlatform, got.Status, got.Mode, got.ASRProvider)
	}
	if got.RawText != n.RawText || got.FinalText != n.FinalText {
		t.Errorf("os textos mudaram: %q / %q", got.RawText, got.FinalText)
	}
	if got.Language == nil || *got.Language != "pt" || got.ASRModel == nil || *got.ASRModel != "gpt-4o-mini-transcribe" ||
		got.DurationMs == nil || *got.DurationMs != 2500 || got.ProcessingMs == nil || *got.ProcessingMs != 1800 ||
		got.Confidence == nil || *got.Confidence != 0.9 {
		t.Errorf("os campos opcionais mudaram: %+v", got)
	}
	if !got.CreatedAt.Equal(quando) || got.CreatedAt.Location() != time.UTC || !got.UpdatedAt.Equal(quando) {
		t.Errorf("a data do ditado é a de agora, em UTC e em microssegundos: %v %v (quero %v)", got.CreatedAt, got.UpdatedAt, quando)
	}
	if got.Version != 1 || got.TextVersion != 1 || got.TrashVersion != 1 {
		t.Errorf("versões = %d %d %d, quero 1 1 1", got.Version, got.TextVersion, got.TrashVersion)
	}
	if got.DeletedAt != nil || got.PurgeAt != nil || got.Conflict != nil || got.LastSeq != 0 {
		t.Errorf("nasce fora da lixeira, sem selo e sem seq (o seq é do Store): %+v", got)
	}
}

func TestNewTranscription_SemOpcionais(t *testing.T) {
	n := novoDitado(func(n *NewDictation) {
		n.Language, n.ASRModel, n.DurationMs, n.ProcessingMs, n.Confidence = nil, nil, nil, nil, nil
	})
	got, err := NewTranscription(atorDoApp, testCelular, n, testNow)
	if err != nil {
		t.Fatalf("NewTranscription: %v", err)
	}
	if got.Language != nil || got.ASRModel != nil || got.DurationMs != nil || got.ProcessingMs != nil || got.Confidence != nil {
		t.Errorf("os opcionais ausentes ficam nulos: %+v", got)
	}
}

func TestNewTranscription_OAppNativoTambemNaoTemAparelho(t *testing.T) {
	got, err := NewTranscription(atorDoApp, testCelular, novoDitado(func(n *NewDictation) { n.SourcePlatform = SourcePlatformAndroid }), testNow)
	if err != nil || got.SourcePlatform != "android" || got.DeviceID != nil {
		t.Fatalf("android: %+v, %v", got, err)
	}
}

func TestNewTranscription_Recusas(t *testing.T) {
	invalido := "\xff\xfe"
	nulo := "a\x00b"
	longo := strings.Repeat("a", MaxTextBytes+1)
	nan := float32(math.NaN())
	neg, grande := int64(-1), int64(math.MaxInt32)+1
	acima, abaixo := float32(1.1), float32(-0.1)
	txt := func(s string) *string { return &s }

	for _, c := range []struct {
		nome   string
		actor  Actor
		id     string
		mudar  func(*NewDictation)
		quero  error
		codigo string
	}{
		{"com aparelho", Actor{UserID: testUser, DeviceID: testDevice}, testCelular, nil, ErrDeviceNotAllowed, ""},
		{"id que não é UUID", atorDoApp, "nao-e-uuid", nil, ErrInvalidID, ""},
		{"id vazio", atorDoApp, "", nil, ErrInvalidID, ""},
		{"origem do PC", atorDoApp, testCelular, func(n *NewDictation) { n.SourcePlatform = "windows" }, ErrInvalidDictation, CodeInvalidField},
		{"origem vazia", atorDoApp, testCelular, func(n *NewDictation) { n.SourcePlatform = "" }, ErrInvalidDictation, CodeInvalidField},
		{"origem de outra caixa", atorDoApp, testCelular, func(n *NewDictation) { n.SourcePlatform = "Web" }, ErrInvalidDictation, CodeInvalidField},
		{"modo desconhecido", atorDoApp, testCelular, func(n *NewDictation) { n.Mode = "turbo" }, ErrInvalidDictation, CodeInvalidField},
		{"modo vazio", atorDoApp, testCelular, func(n *NewDictation) { n.Mode = "" }, ErrInvalidDictation, CodeInvalidField},
		{"bruto com NUL", atorDoApp, testCelular, func(n *NewDictation) { n.RawText = nulo }, ErrInvalidDictation, CodeInvalidField},
		{"final com NUL", atorDoApp, testCelular, func(n *NewDictation) { n.FinalText = nulo }, ErrInvalidDictation, CodeInvalidField},
		{"bruto grande demais", atorDoApp, testCelular, func(n *NewDictation) { n.RawText = longo }, ErrInvalidDictation, CodeInvalidField},
		{"final grande demais", atorDoApp, testCelular, func(n *NewDictation) { n.FinalText = longo }, ErrInvalidDictation, CodeInvalidField},
		{"bruto com UTF-8 inválido", atorDoApp, testCelular, func(n *NewDictation) { n.RawText = invalido }, ErrInvalidDictation, CodeInvalidField},
		{"final com UTF-8 inválido", atorDoApp, testCelular, func(n *NewDictation) { n.FinalText = invalido }, ErrInvalidDictation, CodeInvalidField},
		{"provedor com UTF-8 inválido", atorDoApp, testCelular, func(n *NewDictation) { n.ASRProvider = invalido }, ErrInvalidDictation, CodeInvalidField},
		{"idioma com UTF-8 inválido", atorDoApp, testCelular, func(n *NewDictation) { n.Language = txt(invalido) }, ErrInvalidDictation, CodeInvalidField},
		{"modelo com UTF-8 inválido", atorDoApp, testCelular, func(n *NewDictation) { n.ASRModel = txt(invalido) }, ErrInvalidDictation, CodeInvalidField},
		{"provedor vazio", atorDoApp, testCelular, func(n *NewDictation) { n.ASRProvider = "  " }, ErrInvalidDictation, CodeInvalidField},
		{"provedor grande demais", atorDoApp, testCelular, func(n *NewDictation) { n.ASRProvider = strings.Repeat("p", maxASRProviderLen+1) }, ErrInvalidDictation, CodeInvalidField},
		{"modelo grande demais", atorDoApp, testCelular, func(n *NewDictation) { n.ASRModel = txt(strings.Repeat("m", maxASRModelLen+1)) }, ErrInvalidDictation, CodeInvalidField},
		{"idioma grande demais", atorDoApp, testCelular, func(n *NewDictation) { n.Language = txt(strings.Repeat("l", maxLanguageLen+1)) }, ErrInvalidDictation, CodeInvalidField},
		{"duração negativa", atorDoApp, testCelular, func(n *NewDictation) { n.DurationMs = &neg }, ErrInvalidDictation, CodeInvalidField},
		{"duração acima do INTEGER", atorDoApp, testCelular, func(n *NewDictation) { n.DurationMs = &grande }, ErrInvalidDictation, CodeInvalidField},
		{"processamento negativo", atorDoApp, testCelular, func(n *NewDictation) { n.ProcessingMs = &neg }, ErrInvalidDictation, CodeInvalidField},
		{"confiança NaN", atorDoApp, testCelular, func(n *NewDictation) { n.Confidence = &nan }, ErrInvalidDictation, CodeInvalidField},
		{"confiança acima de 1", atorDoApp, testCelular, func(n *NewDictation) { n.Confidence = &acima }, ErrInvalidDictation, CodeInvalidField},
		{"confiança abaixo de 0", atorDoApp, testCelular, func(n *NewDictation) { n.Confidence = &abaixo }, ErrInvalidDictation, CodeInvalidField},
	} {
		t.Run(c.nome, func(t *testing.T) {
			n := novoDitado()
			if c.mudar != nil {
				c.mudar(&n)
			}
			_, err := NewTranscription(c.actor, c.id, n, testNow)
			if !errors.Is(err, c.quero) {
				t.Fatalf("erro = %v, quero %v", err, c.quero)
			}
			if c.codigo != "" && !strings.Contains(err.Error(), c.codigo) {
				t.Errorf("o erro não diz o código %q: %v", c.codigo, err)
			}
			// O erro nunca carrega o texto ditado.
			for _, texto := range []string{n.RawText, n.FinalText} {
				if len(texto) > 3 && !strings.ContainsAny(texto, "\x00\xff") && strings.Contains(err.Error(), texto) {
					t.Errorf("o erro carrega o texto ditado: %v", err)
				}
			}
		})
	}
}

// Os limites são os do push: o que cabe no teto cabe, no byte exato.
func TestNewTranscription_NoTetoDoPush(t *testing.T) {
	exato := strings.Repeat("a", MaxTextBytes)
	vazio := ""
	n := novoDitado(func(n *NewDictation) {
		n.RawText, n.FinalText = exato, vazio
		n.ASRProvider = strings.Repeat("p", maxASRProviderLen)
	})
	if _, err := NewTranscription(atorDoApp, testCelular, n, testNow); err != nil {
		t.Fatalf("no teto e com o final vazio (o push aceita os dois): %v", err)
	}
}

// O NewDictation cobre a lista fechada do ditado novo, campo a campo: se um
// campo entrar em createFields, esta chamada falha em vez de o Create gravar
// sem ele (ou de recusar tudo em produção).
func TestNewDictation_PayloadCobreAListaFechadaDoPush(t *testing.T) {
	raw, err := novoDitado().payload(testNow)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	for campo := range createFields {
		// deleted_at é o único que o Create não manda: o ditado da nuvem não
		// nasce na lixeira.
		if _, ok := obj[campo]; !ok && campo != "deleted_at" {
			t.Errorf("o campo %q é do ditado novo do push e não do Create", campo)
		}
	}
	for campo := range obj {
		if _, ok := createFields[campo]; !ok {
			t.Errorf("o campo %q não é do ditado novo do push", campo)
		}
	}
	if _, ok := obj["target_app"]; ok {
		t.Error("target_app nunca vai")
	}
}

// A origem da nuvem é uma lista fechada: o PC se apoia nela para saber o que
// não nasceu nele.
func TestIsCloudPlatform(t *testing.T) {
	for p, quero := range map[string]bool{"web": true, "android": true, "windows": false, "": false, "WEB": false, "ios": false} {
		if got := IsCloudPlatform(p); got != quero {
			t.Errorf("IsCloudPlatform(%q) = %v, quero %v", p, got, quero)
		}
	}
}
