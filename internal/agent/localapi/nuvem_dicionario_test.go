package localapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As rotas /api/nuvem/dicionario (ADR-023, F) com um serviço de roteiro: o JSON
// exato de cada resposta, o que cada POST chama, e o status e a frase de cada
// recusa.

type dicionarioFalso struct {
	estado   nuvem.EstadoDoDicionario
	err      error
	chamadas []string
}

func (d *dicionarioFalso) EstadoDoDicionario(context.Context) (nuvem.EstadoDoDicionario, error) {
	d.chamadas = append(d.chamadas, "estado")
	return d.estado, d.err
}

func (d *dicionarioFalso) LigarDicionario(context.Context) (nuvem.EstadoDoDicionario, error) {
	d.chamadas = append(d.chamadas, "ligar")
	return d.estado, d.err
}

func (d *dicionarioFalso) DesligarDicionario(context.Context) (nuvem.EstadoDoDicionario, error) {
	d.chamadas = append(d.chamadas, "desligar")
	return d.estado, d.err
}

func apiComDicionario(t *testing.T, d localapi.DicionarioDaNuvem) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.Nuvem = &nuvemFalsa{}
	pref.CopiaDoDicionario = d
	return localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
}

func TestDicionarioDaNuvem_GETDevolveOEstadoComOsCamposDoPainel(t *testing.T) {
	f := &dicionarioFalso{estado: nuvem.EstadoDoDicionario{
		Ligado: true, Fonte: "este", Aparelho: "DELL-G15", Termos: 49,
		AtualizadoEm: "2026-11-05T14:02:00Z", EnviadoEm: "2026-11-05T14:02:10Z", Aviso: "um aviso",
	}}
	w := fazer(t, apiComDicionario(t, f), "GET", "/api/nuvem/dicionario", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET = %d, cache %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var cru map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &cru); err != nil {
		t.Fatal(err)
	}
	quero := map[string]any{"ligado": true, "fonte": "este", "aparelho": "DELL-G15", "termos": float64(49),
		"atualizado_em": "2026-11-05T14:02:00Z", "enviado_em": "2026-11-05T14:02:10Z", "aviso": "um aviso"}
	if len(cru) != len(quero) {
		t.Errorf("campos: %v", cru)
	}
	for k, v := range quero {
		if cru[k] != v {
			t.Errorf("campo %q = %v, quero %v", k, cru[k], v)
		}
	}
	if len(f.chamadas) != 1 || f.chamadas[0] != "estado" {
		t.Errorf("chamadas: %v", f.chamadas)
	}
}

func TestDicionarioDaNuvem_OsCamposVaziosSaemDaResposta(t *testing.T) {
	// A nuvem que não pôde ser perguntada: só a opção.
	w := fazer(t, apiComDicionario(t, &dicionarioFalso{}), "GET", "/api/nuvem/dicionario", nil)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"ligado":false}` {
		t.Errorf("sem nada: %d %s", w.Code, w.Body)
	}
	// Sem cópia na nuvem: a fonte, e nada de aparelho, termos ou datas.
	w = fazer(t, apiComDicionario(t, &dicionarioFalso{estado: nuvem.EstadoDoDicionario{Fonte: "nenhuma"}}), "GET", "/api/nuvem/dicionario", nil)
	if strings.TrimSpace(w.Body.String()) != `{"ligado":false,"fonte":"nenhuma"}` {
		t.Errorf("fonte nenhuma: %s", w.Body)
	}
}

func TestDicionarioDaNuvem_POSTLigaEDesliga(t *testing.T) {
	for _, c := range []struct {
		corpo   map[string]any
		chamada string
	}{{map[string]any{"ligar": true}, "ligar"}, {map[string]any{"ligar": false}, "desligar"}} {
		f := &dicionarioFalso{estado: nuvem.EstadoDoDicionario{Ligado: c.chamada == "ligar", Fonte: "este", Termos: 2}}
		w := fazer(t, apiComDicionario(t, f), "POST", "/api/nuvem/dicionario", c.corpo)
		var got nuvem.EstadoDoDicionario
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != f.estado {
			t.Errorf("POST %v = %d %s", c.corpo, w.Code, w.Body)
		}
		if len(f.chamadas) != 1 || f.chamadas[0] != c.chamada {
			t.Errorf("POST %v chamou %v", c.corpo, f.chamadas)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Error("sem no-store")
		}
	}
}

func TestDicionarioDaNuvem_CorpoInvalidoNaoChamaOServico(t *testing.T) {
	for _, corpo := range []any{map[string]any{}, map[string]any{"ligar": "sim"}, map[string]any{"outra": true}, nil} {
		f := &dicionarioFalso{}
		w := fazer(t, apiComDicionario(t, f), "POST", "/api/nuvem/dicionario", corpo)
		var e erroDaAPI
		if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != "BAD_REQUEST" || len(f.chamadas) != 0 {
			t.Errorf("corpo %v = %d %s (chamou %v)", corpo, w.Code, w.Body, f.chamadas)
		}
	}
}

func TestDicionarioDaNuvem_AsRecusasTrazemAFraseEOStatus(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
		frase  string
	}{
		{&nuvem.Erro{Status: 409, Mensagem: nuvem.MsgDicionarioConecte}, 409, "CONFLICT", nuvem.MsgDicionarioConecte},
		{&nuvem.Erro{Status: 502, Mensagem: nuvem.MsgDicionarioNaoApagou}, 502, "UNAVAILABLE", "Não deu para apagar a cópia da nuvem agora. Tente de novo."},
		{&nuvem.Erro{Status: 503, Mensagem: nuvem.MsgDicionarioIndisponivel}, 503, "UNAVAILABLE", nuvem.MsgDicionarioIndisponivel},
		{&nuvem.Erro{Status: 500, Mensagem: nuvem.MsgInstalacao}, 500, "INTERNAL", nuvem.MsgInstalacao},
		{errors.New("detalhe com o termo Kubernetes e o token"), 500, "INTERNAL", "Não deu para falar com a nuvem agora."},
	} {
		for _, rota := range []struct {
			metodo string
			corpo  any
		}{{"GET", nil}, {"POST", map[string]any{"ligar": true}}, {"POST", map[string]any{"ligar": false}}} {
			w := fazer(t, apiComDicionario(t, &dicionarioFalso{err: c.err}), rota.metodo, "/api/nuvem/dicionario", rota.corpo)
			var e erroDaAPI
			if w.Code != c.status || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != c.code || e.Error.Message != c.frase {
				t.Errorf("%s %v com %v = %d %s", rota.metodo, rota.corpo, c.err, w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "Kubernetes") {
				t.Error("um erro inesperado vazou o detalhe")
			}
		}
	}
}

func TestDicionarioDaNuvem_SemOServico503(t *testing.T) {
	// Sem o dicionário montado.
	h := apiComDicionario(t, nil)
	for _, r := range [][2]string{{"GET", "/api/nuvem/dicionario"}, {"POST", "/api/nuvem/dicionario"}} {
		if w := fazer(t, h, r[0], r[1], map[string]any{"ligar": true}); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s sem o serviço = %d", r[0], r[1], w.Code)
		}
	}
	// Sem a nuvem montada.
	pref := prefsDeTeste()
	pref.CopiaDoDicionario = &dicionarioFalso{}
	h = localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
	if w := fazer(t, h, "GET", "/api/nuvem/dicionario", nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("sem a nuvem = %d", w.Code)
	}
}

// O PUT /api/settings não tem o campo do dicionário e o preserva: a opção só
// muda pelas rotas /api/nuvem/dicionario e pela fase do sync.
func TestDicionarioDaNuvem_OPutDasPreferenciasPreservaAOpcao(t *testing.T) {
	settings := storage.NewInMemorySettingsRepository()
	base := prefsDeTeste().Padrao
	base.DictionaryCopyEnabled = true
	salvo, err := settings.Put(context.Background(), base, 0)
	if err != nil {
		t.Fatal(err)
	}
	h := localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), settings, prefsDeTeste()).Handler()
	w := fazer(t, h, "PUT", "/api/settings", map[string]any{"base_version": salvo.Version, "beep_enabled": !salvo.BeepEnabled})
	if w.Code != 200 {
		t.Fatalf("PUT /api/settings = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "dictionary_copy") {
		t.Errorf("o campo do dicionário apareceu em /api/settings: %s", w.Body)
	}
	if got, _ := settings.Get(context.Background()); !got.DictionaryCopyEnabled || got.BeepEnabled == salvo.BeepEnabled {
		t.Errorf("depois do PUT: %+v", got)
	}
}
