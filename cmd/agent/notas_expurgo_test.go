package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/expurgo"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TestAbrirHistorico_ExpurgaALixeiraDasNotas: as notas (ADR-022, N1) moram no
// banco do histórico e a lixeira delas é expurgada no mesmo ciclo dos
// ditados: na subida e a cada hora, sem reiniciar o Agent. A ativa e a
// arquivada nunca saem (a nota não tem a retenção de 6 meses). SQLite de
// verdade num diretório temporário e relógio falso: nada age no sistema.
func TestAbrirHistorico_ExpurgaALixeiraDasNotas(t *testing.T) {
	ctx := context.Background()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	inicio := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	relogio := clock.NewFake(inicio)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	h, err := abrirHistoricoComRetencao(log, caminho, "mauro", relogio)
	if err != nil {
		t.Fatalf("abrirHistoricoComRetencao: %v", err)
	}
	nova := func(titulo string) storage.Nota {
		n, err := h.notas.Criar(ctx, storage.NovaNota{UserID: h.dono, Titulo: titulo, Corpo: "x"})
		if err != nil {
			t.Fatalf("Criar %s: %v", titulo, err)
		}
		return n
	}
	vencida, recente, ativa, arquivada := nova("vencida"), nova("recente"), nova("ativa"), nova("arquivada")
	arq := true
	if _, err := h.notas.Atualizar(ctx, h.dono, arquivada.ID, 1, storage.MudancaDaNota{Arquivada: &arq}); err != nil {
		t.Fatal(err)
	}
	// A "vencida" foi para a lixeira há 30 dias: o prazo é agora.
	if err := h.notas.SoftDelete(ctx, h.dono, vencida.ID, inicio.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	if err := h.notas.SoftDelete(ctx, h.dono, recente.ID, inicio); err != nil {
		t.Fatal(err)
	}
	h.fechar()

	h, err = abrirHistoricoComRetencao(log, caminho, "mauro", relogio)
	if err != nil {
		t.Fatalf("reabrindo: %v", err)
	}
	defer h.fechar()
	existe := func(id string) bool {
		t.Helper()
		_, err := h.notas.Get(ctx, h.dono, id)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Get %s: %v", id, err)
		}
		return err == nil
	}
	if existe(vencida.ID) {
		t.Error("a nota da lixeira com o prazo vencido deveria sair na subida")
	}
	if !existe(recente.ID) || !existe(ativa.ID) || !existe(arquivada.ID) {
		t.Fatal("a subida só pode apagar o que venceu na lixeira")
	}

	relogio.Advance(30*24*time.Hour - expurgo.Intervalo)
	if !existe(recente.ID) {
		t.Fatal("nota da lixeira apagada antes dos 30 dias")
	}
	relogio.Advance(expurgo.Intervalo)
	if existe(recente.ID) {
		t.Error("a nota da lixeira deveria sair no ciclo em que completa 30 dias")
	}

	// Um ano depois (bem além dos 6 meses dos ditados), a ativa e a arquivada
	// continuam: nota é coisa que a pessoa guardou de propósito.
	// (Direto no repositório: avançar o relógio do ciclo um ano seriam 8.760
	// rodadas.)
	if n, err := h.notas.PurgeExpired(ctx, inicio.AddDate(1, 0, 0)); err != nil || n != 0 {
		t.Errorf("um ano depois, nada mais estava vencido: n=%d err=%v", n, err)
	}
	if !existe(ativa.ID) || !existe(arquivada.ID) {
		t.Error("a nota fora da lixeira não expira, com ou sem arquivo")
	}
}
