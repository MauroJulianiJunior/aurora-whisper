package main

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TestAbrirHistorico_VarreAsImagensDasNotasSemDizerNadaDaNota: as imagens das
// notas (ADR-022, N10) moram na pasta da tabela de caminhos, ao lado do banco. A
// subida põe a pasta e a tabela de acordo (o arquivo sem linha e a linha sem
// arquivo saem) e avisa num WARN só com as contagens: nem o título, nem o id da
// nota ou da imagem, nem o caminho. SQLite de verdade num diretório temporário.
func TestAbrirHistorico_VarreAsImagensDasNotasSemDizerNadaDaNota(t *testing.T) {
	ctx := context.Background()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	relogio := clock.NewFake(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))

	h, err := abrirHistoricoComRetencao(logger, caminho, imagensAoLado(caminho), "mauro", relogio)
	if err != nil {
		t.Fatal(err)
	}
	n, err := h.notas.Criar(ctx, storage.NovaNota{UserID: h.dono, Titulo: "Recibo do táxi", Corpo: "cliente Silva"})
	if err != nil {
		t.Fatal(err)
	}
	var foto bytes.Buffer
	if err := jpeg.Encode(&foto, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	fica, err := h.notas.AnexarImagem(ctx, h.dono, n.ID, foto.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sai, err := h.notas.AnexarImagem(ctx, h.dono, n.ID, foto.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	h.fechar()

	pasta := imagensAoLado(caminho)
	if _, err := os.Stat(filepath.Join(pasta, fica.ID+".jpg")); err != nil {
		t.Fatalf("o arquivo da imagem não está na pasta da tabela: %v", err)
	}
	// Um desligamento no meio: um arquivo que nenhuma linha cita, e uma linha cujo
	// arquivo sumiu.
	orfa := "01900000-0000-7000-8000-0000000000bb"
	if err := os.WriteFile(filepath.Join(pasta, orfa+".jpg"), foto.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(pasta, sai.ID+".jpg")); err != nil {
		t.Fatal(err)
	}
	log.Reset()

	h, err = abrirHistoricoComRetencao(logger, caminho, imagensAoLado(caminho), "mauro", relogio)
	if err != nil {
		t.Fatal(err)
	}
	saida := log.String()
	if !strings.Contains(saida, "level=WARN") || !strings.Contains(saida, "arquivos_sem_linha=1") || !strings.Contains(saida, "linhas_sem_arquivo=1") {
		t.Errorf("o WARN da varredura:\n%s", saida)
	}
	for _, proibido := range []string{"Recibo", "táxi", "Silva", n.ID, fica.ID, sai.ID, orfa, "notas-imagens"} {
		if strings.Contains(saida, proibido) {
			t.Errorf("o log diz %q:\n%s", proibido, saida)
		}
	}
	if forma, tem := trazOCaminho(saida, pasta); tem {
		t.Errorf("o log leva o caminho da pasta (%s)", forma)
	}
	got, err := h.notas.Get(ctx, h.dono, n.ID)
	if err != nil || len(got.Imagens) != 1 || got.Imagens[0].ID != fica.ID {
		t.Fatalf("a nota depois da varredura: %v, %+v", err, got.Imagens)
	}
	if _, err := os.Stat(filepath.Join(pasta, orfa+".jpg")); !os.IsNotExist(err) {
		t.Errorf("o arquivo sem linha ficou: %v", err)
	}

	// A subida sem nada a varrer não avisa.
	h.fechar()
	log.Reset()
	h, err = abrirHistoricoComRetencao(logger, caminho, imagensAoLado(caminho), "mauro", relogio)
	if err != nil {
		t.Fatal(err)
	}
	defer h.fechar()
	if strings.Contains(log.String(), "varredura das imagens") {
		t.Errorf("a subida limpa avisou:\n%s", log.String())
	}
}
