package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/chavestest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// TestChaves_Memoria roda a suíte das chaves da IA (ADR-021, C3a) e do aceite
// e das preferências do ditado no celular (C3b) contra o MemoryStore. A mesma
// suíte roda contra o Postgres em internal/server/pgstore (TestChaves_Postgres,
// com AURORA_TEST_POSTGRES_URL).
func TestChaves_Memoria(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	chavestest.Run(t, chavestest.Backend{
		Store: mem, Accounts: mem, Chaves: mem, Ditado: mem,
		NewDevice: func(_ *testing.T, userID string, tokenHMAC []byte) string {
			id := contracttest.NewID()
			mem.AddDevice(userID, id)
			mem.AddDeviceToken(id, tokenHMAC)
			return id
		},
		Despejo: func(*testing.T) string { return mem.Despejo() },
	})
}

// O ModelosPelaRede de produção fala com cada provedor pelo endereço e pelo
// cabeçalho dele: a OpenAI e a Groq com o Bearer (a Groq na API compatível,
// /openai/v1/models), a Anthropic com o x-api-key. Contra um httptest, nunca a
// rede de verdade.
func TestModelosPelaRede_CadaProvedorNoSeuEndereco(t *testing.T) {
	type pedido struct{ caminho, bearer, xapikey string }
	var vistos []pedido
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vistos = append(vistos, pedido{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-api-key")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1","created":2},{"id":"m0","created":1}]}`))
	}))
	defer srv.Close()
	const chave = "sk-de-mentira-0123456789abcdef"
	m := server.ModelosPelaRede{
		Cliente:  ia.Cliente{BaseOpenAI: srv.URL + "/oa", BaseAnthropic: srv.URL + "/an"},
		BaseGroq: srv.URL + "/groq/openai",
	}
	for _, p := range []string{"openai", "groq", "anthropic"} {
		got, err := m.ListarModelos(context.Background(), p, chave)
		if err != nil || strings.Join(got, ",") != "m1,m0" {
			t.Fatalf("%s: %v, %v", p, got, err)
		}
	}
	quero := []pedido{
		{"/oa/v1/models", "Bearer " + chave, ""},
		{"/groq/openai/v1/models", "Bearer " + chave, ""},
		{"/an/v1/models", "", chave},
	}
	if len(vistos) != len(quero) {
		t.Fatalf("pedidos: %+v", vistos)
	}
	for i := range quero {
		if vistos[i] != quero[i] {
			t.Errorf("pedido %d = %+v, quero %+v", i, vistos[i], quero[i])
		}
	}
	if _, err := m.ListarModelos(context.Background(), "xai", chave); err == nil || strings.Contains(err.Error(), chave) {
		t.Errorf("provedor fora da lista: %v", err)
	}
	// A Groq sem endereço é a oficial (a constante), e não a da OpenAI.
	if server.BaseGroq != "https://api.groq.com/openai" {
		t.Errorf("BaseGroq = %q", server.BaseGroq)
	}
}

// O provedor que recusa a chave, pelo ModelosPelaRede: o motivo do internal/ia
// vira PROVEDOR_RECUSOU_CHAVE, e o corpo de erro dele (que pode trazer um
// pedaço da chave) não passa.
func TestModelosPelaRede_RecusaViraOMotivo(t *testing.T) {
	const chave = "sk-recusada-0123456789abcdefghij"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + chave + `"}}`))
	}))
	defer srv.Close()
	m := server.ModelosPelaRede{BaseGroq: srv.URL}
	_, err := m.ListarModelos(context.Background(), "groq", chave)
	var e *ia.Erro
	if !errors.As(err, &e) || e.Motivo != ia.MotivoChaveRecusada || strings.Contains(err.Error(), chave) {
		t.Fatalf("err = %v", err)
	}
	if status, code, frase := server.FalhaDoProvedor(err); status != http.StatusUnprocessableEntity ||
		code != "PROVEDOR_RECUSOU_CHAVE" || strings.Contains(frase, chave) {
		t.Fatalf("FalhaDoProvedor = %d %s %q", status, code, frase)
	}
}
