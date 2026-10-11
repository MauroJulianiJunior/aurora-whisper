package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// datasetSintetico grava, numa pasta temporária, um manifesto com n amostras
// de seg segundos cada. Nunca o dataset de voz real.
func datasetSintetico(t *testing.T, n int, seg float64) string {
	t.Helper()
	dir := t.TempDir()
	type amostra struct {
		ID        string   `json:"id"`
		Audio     string   `json:"audio"`
		Category  string   `json:"category"`
		Reference string   `json:"reference"`
		Entities  []string `json:"entities"`
	}
	var as []amostra
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("s%02d", i)
		a := amostraSintetica(id, "", seg, int16(i))
		if err := dataset.WriteWAV(filepath.Join(dir, id+".wav"), a.taxa, a.pcm); err != nil {
			t.Fatal(err)
		}
		as = append(as, amostra{ID: id, Audio: id + ".wav", Category: "pt-br-geral", Reference: "frase " + id, Entities: []string{}})
	}
	dados, _ := json.Marshal(map[string]any{"samples": as})
	caminho := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(caminho, dados, 0o644); err != nil {
		t.Fatal(err)
	}
	return caminho
}

func TestRun_SubcomandoObrigatorio(t *testing.T) {
	for _, args := range [][]string{nil, {"qualquer"}} {
		var stderr bytes.Buffer
		if got := run(args, &bytes.Buffer{}, &stderr, nil); got != 2 || !strings.Contains(stderr.String(), "uso:") {
			t.Errorf("run(%v) = %d, stderr %q", args, got, stderr.String())
		}
	}
}

func TestMontar_GravaOsWAVEOManifesto(t *testing.T) {
	manifest := datasetSintetico(t, 5, 4)
	saida := filepath.Join(t.TempDir(), "longos")
	var stdout, stderr bytes.Buffer
	if got := run([]string{"montar", "-manifest", manifest, "-saida", saida, "-duracoes", "10s,20s", "-por-duracao", "1", "-pausa", "500ms"}, &stdout, &stderr, nil); got != 0 {
		t.Fatalf("montar = %d: %s", got, stderr.String())
	}
	var m struct {
		Longos []struct {
			ID, Audio, Referencia string
			DuracaoMs             int64 `json:"duracao_ms"`
			Fontes                []string
		} `json:"longos"`
	}
	dados, err := os.ReadFile(filepath.Join(saida, "manifesto-longo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(dados, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Longos) != 2 || m.Longos[0].ID != "longo-10s-1" || m.Longos[1].ID != "longo-20s-1" {
		t.Fatalf("manifesto = %+v", m.Longos)
	}
	// 10 s: s01 + s02 + s03 com duas pausas = 13 s.
	if l := m.Longos[0]; l.DuracaoMs != 13000 || l.Referencia != "frase s01 frase s02 frase s03" {
		t.Errorf("primeiro = %+v", l)
	}
	for _, l := range m.Longos {
		d, err := dataset.Duration(filepath.Join(saida, l.Audio))
		if err != nil || d.Milliseconds() != l.DuracaoMs {
			t.Errorf("%s: WAV com %v (%v), manifesto diz %d ms", l.ID, d, err, l.DuracaoMs)
		}
	}
}

func TestMontar_SemSaidaRecusa(t *testing.T) {
	var stderr bytes.Buffer
	if got := run([]string{"montar", "-manifest", "x"}, &bytes.Buffer{}, &stderr, nil); got != 2 {
		t.Errorf("montar sem -saida = %d", got)
	}
}

func TestMedir_FlagsObrigatoriasEVarianteDesconhecida(t *testing.T) {
	for _, args := range [][]string{
		{"medir", "-manifest", "x"},
		{"medir", "-manifest", "x", "-servidor", "s", "-modelo", "m"},
		{"medir", "-manifest", "x", "-servidor", "s", "-modelo", "m", "-out", "o.md", "-variantes", "nao-existe"},
	} {
		var stderr bytes.Buffer
		if got := run(args, &bytes.Buffer{}, &stderr, nil); got != 2 {
			t.Errorf("run(%v) = %d, quero 2; stderr %q", args, got, stderr.String())
		}
	}
}

// TestMedir_PontaAPontaComServidorDeMentira: o caminho inteiro do
// subcomando, do manifesto ao relatório, com um ASR de mentira no lugar do
// whisper-server.
func TestMedir_PontaAPontaComServidorDeMentira(t *testing.T) {
	manifest := datasetSintetico(t, 6, 5)
	out := filepath.Join(t.TempDir(), "rel.md")
	m := &abridorDeMentira{tempo: 2 * time.Second}
	var nomeModelo string
	abrir := func(nome string) abridor { nomeModelo = nome; return m.abrir }
	var stdout, stderr bytes.Buffer
	args := []string{"medir", "-manifest", manifest, "-servidor", "srv.exe", "-modelo", filepath.Join("models", "small-q4_0.bin"),
		"-out", out, "-duracoes", "12s,20s", "-por-duracao", "1", "-threads", "4"}
	if got := run(args, &stdout, &stderr, abrir); got != 0 {
		t.Fatalf("medir = %d: %s", got, stderr.String())
	}
	if nomeModelo != "small-q4_0.bin" {
		t.Errorf("nome do modelo = %q", nomeModelo)
	}
	// Cada variante longa e cada curta num servidor próprio.
	if quer := len(variantesLongas) + len(variantesCurtas); len(m.abertos) != quer {
		t.Errorf("%d servidores, quero %d", len(m.abertos), quer)
	}
	for _, a := range m.abertos {
		if a.cfg.ServerPath != "srv.exe" || a.cfg.Threads != 4 || a.cfg.Language != "pt" || a.cfg.SaidaDoServidor == nil {
			t.Errorf("servidor com %+v", a.cfg)
		}
	}
	rel, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, trecho := range []string{"# Benchmark de áudio longo", "`ac750`", "`por-duracao-intercalada`", "longo-12s-1", "longo-20s-1", "Fala curta", "6 amostras"} {
		if !strings.Contains(string(rel), trecho) {
			t.Errorf("o relatório não tem %q", trecho)
		}
	}
	if _, err := os.Stat(out + ".json"); err != nil {
		t.Errorf("sem o JSON ao lado: %v", err)
	}
	if _, err := os.Stat(out + ".servidor.log"); err != nil {
		t.Errorf("sem o log do servidor ao lado: %v", err)
	}
}
