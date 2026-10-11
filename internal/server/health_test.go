package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// As duas rotas de saúde são públicas (a tabela as declara AcessoPublico, e a
// suíte do authtest confere o acesso). Esta prova é a do que elas respondem:
// o servidor em memória da F1-D, que a conferia, saiu na C5c da ADR-021.

func TestHealthLive_Responde200SemDependencia(t *testing.T) {
	rec := httptest.NewRecorder()
	handleHealthLive(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health/live: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHealthReady_SoDaOkSeTodosPassam(t *testing.T) {
	const segredoDoErro = "senha-do-banco-no-erro"
	cases := []struct {
		nome      string
		checkers  []ReadyChecker
		quer      int
		querCorpo string
	}{
		{"sem dependência", nil, http.StatusOK, `"status":"ok"`},
		{"todos passam", []ReadyChecker{func() error { return nil }, func() error { return nil }}, http.StatusOK, `"status":"ok"`},
		{"um falha", []ReadyChecker{func() error { return nil }, func() error { return errors.New(segredoDoErro) }}, http.StatusServiceUnavailable, codeInternal},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handleHealthReady(c.checkers...)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil))
			if rec.Code != c.quer || !strings.Contains(rec.Body.String(), c.querCorpo) {
				t.Fatalf("health/ready: %d %s, quero %d com %s", rec.Code, rec.Body.String(), c.quer, c.querCorpo)
			}
			// O motivo da falha (que pode trazer um pedaço da conexão) não vai à resposta.
			if strings.Contains(rec.Body.String(), segredoDoErro) {
				t.Errorf("o erro do checker vazou na resposta: %s", rec.Body.String())
			}
		})
	}
}
