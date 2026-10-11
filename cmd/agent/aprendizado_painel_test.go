package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A ligação da API local do painel com o aprendizado REAL do Agent (etapa 3,
// F3): o localapi.Server de verdade, sobre o aprendizadoDoAgent de verdade,
// com o histórico em memória, o dicionário numa pasta temporária e o runtime
// falso de aprendizado_test.go. Nenhum teste toca bin\, abre áudio, digita
// ou usa a área de transferência.

type inicioParado struct{}

func (inicioParado) Estado() (bool, string) { return false, "" }
func (inicioParado) Definir(bool) error     { return nil }

type modelosParados struct{}

func (modelosParados) ListarModelos(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func chamar(t *testing.T, h http.Handler, metodo, url string, corpo any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(corpo)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(metodo, url, bytes.NewReader(b)))
	return w
}

// TestAPIDoPainel_AprendeDeVerdadeEDesfazIdentico: salvar sugere sem gravar;
// o clique grava no arquivo e troca o dicionário do runtime; o próximo
// ditado já sai certo; desfazer devolve o arquivo idêntico, byte a byte.
func TestAPIDoPainel_AprendeDeVerdadeEDesfazIdentico(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	original := m.bytes(t)

	repo := storage.NewInMemoryTranscriptionRepository()
	now := time.Now().UTC()
	repo.Put(storage.Transcription{
		ID: "t1", UserID: "dono", SourcePlatform: "windows", Mode: "smart", ASRProvider: "local",
		RawText: "vou mandar o sf amanhã", FinalText: "Vou mandar o sf amanhã.",
		Status: "completed", CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	srv := localapi.New("dono", repo, storage.NewInMemorySettingsRepository(), localapi.Preferencias{
		Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{},
		Aprendizado: m.a, Log: nil,
	})
	h := srv.Handler()

	// Salvar sugere, e não grava.
	w := chamar(t, h, http.MethodPatch, "/api/history/t1",
		map[string]any{"base_version": 1, "final_text": "Vou mandar o ECF amanhã."})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", w.Code, w.Body.String())
	}
	var salvo struct {
		Sugestoes []struct {
			De, Para       string
			TermoExistente string `json:"termo_existente"`
		} `json:"sugestoes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &salvo); err != nil {
		t.Fatal(err)
	}
	// O termo existente só sai se o motor viu o dicionário REAL do arquivo:
	// com um dicionário vazio, a mesma troca sairia como termo novo.
	if len(salvo.Sugestoes) != 1 || salvo.Sugestoes[0].De != "sf" || salvo.Sugestoes[0].Para != "ECF" ||
		salvo.Sugestoes[0].TermoExistente != "ECF" {
		t.Fatalf("sugestoes = %+v", salvo.Sugestoes)
	}
	if !bytes.Equal(m.bytes(t), original) || m.rt.quantos() != 0 {
		t.Fatal("salvar mexeu no dicionário")
	}

	// O clique grava e troca na hora.
	w = chamar(t, h, http.MethodPost, "/api/aprendizado", map[string]string{"id": "t1", "de": "sf", "para": "ECF"})
	if w.Code != http.StatusOK {
		t.Fatalf("aprender: %d %s", w.Code, w.Body.String())
	}
	if bytes.Equal(m.bytes(t), original) || !strings.Contains(string(m.bytes(t)), `"sf": "painel"`) {
		t.Fatalf("o arquivo não guarda o que foi aprendido:\n%s", m.bytes(t))
	}
	if m.rt.quantos() != 1 {
		t.Fatalf("o runtime recebeu %d dicionários, quero 1", m.rt.quantos())
	}
	if got := resolve(t, m.rt.ultimo(t), "vou mandar o sf amanhã"); !strings.Contains(got, "ECF") {
		t.Errorf("o próximo ditado saiu %q, quero com ECF", got)
	}

	// A lista vem do arquivo.
	w = chamar(t, h, http.MethodGet, "/api/aprendizado", nil)
	if !strings.Contains(w.Body.String(), `"variante":"sf"`) || !strings.Contains(w.Body.String(), `"termo":"ECF"`) {
		t.Errorf("lista: %s", w.Body.String())
	}

	// Desfazer devolve o arquivo idêntico, e o runtime deixa de corrigir.
	w = chamar(t, h, http.MethodPost, "/api/aprendizado/desfazer", map[string]string{"variante": "sf"})
	if w.Code != http.StatusOK {
		t.Fatalf("desfazer: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(m.bytes(t), original) {
		t.Errorf("desfazer não deixou o arquivo idêntico:\n%s", m.bytes(t))
	}
	if got := resolve(t, m.rt.ultimo(t), "vou mandar o sf amanhã"); strings.Contains(got, "ECF") {
		t.Errorf("depois de desfazer o ditado saiu %q", got)
	}
}

// TestAPIDoPainel_SemDicionarioNaSubida: o Agent subiu sem dicionario.json
// (usuário vazio). Salvar segue igual e sem sugestão, e um clique mesmo assim
// não grava nem troca nada.
func TestAPIDoPainel_SemDicionarioNaSubida(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "")
	repo := storage.NewInMemoryTranscriptionRepository()
	now := time.Now().UTC()
	repo.Put(storage.Transcription{
		ID: "t1", UserID: "dono", SourcePlatform: "windows", Mode: "smart", ASRProvider: "local",
		RawText: "vou mandar o sf amanhã", FinalText: "Vou mandar o sf amanhã.",
		Status: "completed", CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	srv := localapi.New("dono", repo, storage.NewInMemorySettingsRepository(), localapi.Preferencias{
		Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{}, Aprendizado: m.a,
	})
	h := srv.Handler()

	w := chamar(t, h, http.MethodPatch, "/api/history/t1",
		map[string]any{"base_version": 1, "final_text": "Vou mandar o ECF amanhã."})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sugestoes":[]`) {
		t.Fatalf("PATCH: %d %s", w.Code, w.Body.String())
	}
	w = chamar(t, h, http.MethodPost, "/api/aprendizado", map[string]string{"id": "t1", "de": "sf", "para": "ECF"})
	if w.Code == http.StatusOK {
		t.Fatalf("aprendeu sem dicionário: %s", w.Body.String())
	}
	if m.rt.quantos() != 0 {
		t.Error("trocou o dicionário sem ter gravado nada")
	}
}
