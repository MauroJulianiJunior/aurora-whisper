package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/expurgo"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os 6 meses no ciclo do Agent (ADR-019, "Decisões da F10", F10c), pelo mesmo
// abrirHistoricoComRetencao que o Agent usa (abrirHistoricoEm), com um SQLite
// de verdade na pasta temporária e o relógio falso: nada age no sistema, e
// meses passam num Advance. Nenhum teste abre o bin\ de ninguém.
//
// O freio não se testa com Advance: o clock.Fake dispara cada tique de hora
// dentro do Advance, então "8 dias sem rodar" por Advance são 192 rodadas, e
// nunca freiam. O teste fecha o Agent, avança e reabre.

// inicioDaRetencao é a primeira subida destes testes.
var inicioDaRetencao = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// agentDaRetencao é o histórico aberto, com o log e o relógio.
type agentDaRetencao struct {
	t       *testing.T
	caminho string
	relogio *clock.Fake
	log     *bytes.Buffer
	h       historicoAberto
	aberto  bool
}

func novoAgentDaRetencao(t *testing.T) *agentDaRetencao {
	t.Helper()
	a := &agentDaRetencao{t: t, caminho: filepath.Join(t.TempDir(), "historico.db"),
		relogio: clock.NewFake(inicioDaRetencao), log: &bytes.Buffer{}}
	a.abrir()
	t.Cleanup(a.fechar)
	return a
}

func (a *agentDaRetencao) abrir() {
	a.t.Helper()
	h, err := abrirHistoricoComRetencao(slog.New(slog.NewTextHandler(a.log, nil)), a.caminho, "mauro", a.relogio)
	if err != nil {
		a.t.Fatalf("abrirHistoricoComRetencao: %v", err)
	}
	a.h, a.aberto = h, true
}

func (a *agentDaRetencao) fechar() {
	if a.aberto {
		a.h.fechar()
		a.aberto = false
	}
}

// ditado grava um ditado do dono criado em criado, com o texto dado.
func (a *agentDaRetencao) ditado(id string, criado time.Time, texto string) {
	a.t.Helper()
	if err := a.h.hist.Put(storage.Transcription{
		ID: id, UserID: a.h.dono, SourcePlatform: "windows", RawText: "bruto: " + texto, FinalText: texto,
		Mode: "smart", ASRProvider: "local", Status: "done", CreatedAt: criado, UpdatedAt: criado,
		Version: 1, SyncState: storage.SituacaoNormal,
	}); err != nil {
		a.t.Fatal(err)
	}
}

func (a *agentDaRetencao) tem(id string) bool {
	a.t.Helper()
	_, err := a.h.hist.Get(context.Background(), a.h.dono, id)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		a.t.Fatal(err)
	}
	return err == nil
}

func (a *agentDaRetencao) chave(nome string) bool {
	a.t.Helper()
	db, err := storage.AbrirSomenteLeitura(a.caminho)
	if err != nil {
		a.t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instalacao WHERE nome = ?`, nome).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n == 1
}

func (a *agentDaRetencao) ligar(previa int) storage.ResultadoDoLigar {
	a.t.Helper()
	r, err := a.h.retencao.Ligar(context.Background(), previa)
	if err != nil {
		a.t.Fatalf("Ligar: %v\n%s", err, a.log.String())
	}
	return r
}

// vencido é um ditado criado 7 meses antes de agora.
func vencido(agora time.Time) time.Time { return agora.AddDate(0, -7, 0) }

// ids e textos dos testes: o log nunca pode ter nenhum deles.
const (
	idVelho    = "0199f000-0000-7000-8000-000000000001"
	idAmanha   = "0199f000-0000-7000-8000-000000000002"
	idNovo     = "0199f000-0000-7000-8000-000000000003"
	idOutro    = "0199f000-0000-7000-8000-000000000004"
	textoDoPC  = "a jabuticaba ficou na geladeira"
	textoVelho = "o caju azedou no balcão"
)

// semNadaNoLog confere que o log não tem id, texto nem caminho.
func (a *agentDaRetencao) semNadaNoLog() {
	a.t.Helper()
	l := a.log.String()
	for _, proibido := range []string{idVelho, idAmanha, idNovo, idOutro, textoDoPC, textoVelho, "jabuticaba", "caju",
		filepath.Dir(a.caminho), filepath.ToSlash(filepath.Dir(a.caminho))} {
		if strings.Contains(l, proibido) {
			a.t.Errorf("o log tem %q:\n%s", proibido, l)
		}
	}
}

// Sem o OK, nada sai: 60 dias de Advance (cada hora é uma rodada com escrita),
// e o ano inteiro pelo passo chamado direto, com o relógio um ano à frente.
func TestRetencao_SemOKNadaSai(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	a.ditado(idAmanha, inicioDaRetencao.AddDate(0, -6, 1), textoDoPC)
	a.relogio.Advance(60 * 24 * time.Hour)
	if !a.tem(idVelho) || !a.tem(idAmanha) {
		t.Fatal("sem o OK, o ciclo apagou pelos 6 meses")
	}
	sm := a.h.hist.SeisMeses(a.caminho)
	longe := a.relogio.Now().AddDate(1, 0, 0)
	if p, err := sm.Passo(context.Background(), longe); err != nil || p.Ligada || p.Apagados != 0 {
		t.Fatalf("o passo um ano à frente = %+v, %v", p, err)
	}
	if n, err := sm.ApagarForaDaJanela(context.Background(), longe); err != nil || n != 0 {
		t.Fatalf("o apagar um ano à frente = %d, %v", n, err)
	}
	if !a.tem(idVelho) || !a.tem(idAmanha) {
		t.Fatal("sem o OK, o passo apagou")
	}
	if !a.chave(storage.ChaveRetencaoRodadaEm) {
		t.Fatal("o ciclo não gravou a rodada sem o OK")
	}
	a.semNadaNoLog()
}

// Com o OK, o ditado que faz 6 meses amanhã sai no tique seguinte ao
// vencimento, sem reiniciar o Agent. O Ligar apaga o que já venceu na hora.
func TestRetencao_ComOKSaiNoTiqueSeguinte(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	// Vence em inicio + 24 h + 30 min.
	a.ditado(idAmanha, inicioDaRetencao.AddDate(0, -6, 0).Add(24*time.Hour+30*time.Minute), textoDoPC)
	a.ditado(idNovo, inicioDaRetencao, textoDoPC)
	if r := a.ligar(1); r.Apagados != 1 || r.Copia == "" {
		t.Fatalf("o Ligar = %+v", r)
	}
	if a.tem(idVelho) {
		t.Fatal("o Ligar não apagou o vencido")
	}
	a.relogio.Advance(24 * time.Hour)
	if !a.tem(idAmanha) {
		t.Fatal("o ditado saiu antes de completar 6 meses")
	}
	a.relogio.Advance(time.Hour)
	if a.tem(idAmanha) {
		t.Fatal("o ditado que completou 6 meses não saiu no tique seguinte")
	}
	if !a.tem(idNovo) {
		t.Fatal("o ditado novo saiu")
	}
	if !strings.Contains(a.log.String(), "historico: 6 meses ligados") || !strings.Contains(a.log.String(), "historico: 6 meses apagados") {
		t.Fatalf("o log não diz o Ligar e o apagado:\n%s", a.log.String())
	}
	a.semNadaNoLog()
}

// O Desligar volta ao de antes: 60 dias de Advance depois dele, nada sai.
func TestRetencao_DepoisDoDesligarNadaSai(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	a.ditado(idAmanha, inicioDaRetencao.AddDate(0, -6, 10), textoDoPC)
	a.ligar(1)
	if err := a.h.retencao.Desligar(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.relogio.Advance(60 * 24 * time.Hour)
	if !a.tem(idAmanha) {
		t.Fatal("depois do Desligar, o ciclo apagou pelos 6 meses")
	}
	a.semNadaNoLog()
}

// O freio: com o OK, o Agent fecha, o relógio anda 8 dias, e o Agent abre. A
// rodada da subida pausa: nada apagado (o ditado que venceu nesse tempo
// fica), o OK e a rodada tirados, a pausa gravada, e o WARN só com contagens.
func TestRetencao_OFreioNaSubida(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	a.ditado(idAmanha, inicioDaRetencao.AddDate(0, -6, 2), textoDoPC) // vence em 2 dias
	a.ligar(1)
	a.fechar()
	a.relogio.Advance(8 * 24 * time.Hour)
	a.abrir()
	if !a.tem(idAmanha) {
		t.Fatal("o freio não segurou: o ditado que venceu com o Agent parado saiu na subida")
	}
	if a.chave(storage.ChaveRetencaoAceitaEm) || !a.chave(storage.ChaveRetencaoPausadaEm) {
		t.Fatal("a pausa não tirou o OK, ou não ficou gravada")
	}
	if l := linhaDoLog(a.log.String(), "6 meses pausados"); !strings.Contains(l, "level=WARN") ||
		!strings.Contains(l, "dias_parado=8") || !strings.Contains(l, "previa=1") {
		t.Fatalf("o WARN da pausa:\n%s", a.log.String())
	}
	// A rodada que pausou não grava a rodada; a seguinte grava, e nada sai
	// (sem o OK).
	if a.chave(storage.ChaveRetencaoRodadaEm) {
		t.Fatal("a rodada que pausou gravou a rodada")
	}
	a.relogio.Advance(time.Hour)
	if !a.chave(storage.ChaveRetencaoRodadaEm) || !a.tem(idAmanha) {
		t.Fatal("a rodada depois da pausa")
	}
	a.semNadaNoLog()
}

// O freio vale também para o teto da lixeira (a D3), sem o OK dos 6 meses: a
// lixeira com o marcador na fila (a nuvem ainda não soube) cuja janela venceu
// com o Agent parado fica na rodada da subida, e sai na seguinte, depois de a
// rodada ser gravada. É a rodada gravada pelo ciclo que faz o freio existir
// para quem nunca ligou os 6 meses.
func TestRetencao_OFreioAdiaOTetoDaLixeira(t *testing.T) {
	a := novoAgentDaRetencao(t)
	ctx := context.Background()
	sv := int64(1)
	criado := inicioDaRetencao.AddDate(0, -6, 3) // a janela vence em 3 dias
	if err := a.h.hist.Put(storage.Transcription{
		ID: idVelho, UserID: a.h.dono, SourcePlatform: "windows", RawText: "b", FinalText: textoVelho,
		Mode: "smart", ASRProvider: "local", Status: "done", CreatedAt: criado, UpdatedAt: criado,
		Version: 1, SyncState: storage.SituacaoNormal, ServerVersion: &sv,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.h.hist.SoftDelete(ctx, a.h.dono, idVelho, inicioDaRetencao.AddDate(0, 0, -31)); err != nil {
		t.Fatal(err)
	}
	a.relogio.Advance(time.Hour)
	if !a.tem(idVelho) {
		t.Fatal("a lixeira com o marcador saiu dentro da janela")
	}
	a.fechar()
	a.relogio.Advance(8 * 24 * time.Hour)
	a.abrir()
	if !a.tem(idVelho) {
		t.Fatal("o freio não adiou o teto: a lixeira saiu na rodada da subida")
	}
	a.relogio.Advance(time.Hour)
	if a.tem(idVelho) {
		t.Fatal("a lixeira fora da janela não saiu na rodada seguinte ao freio")
	}
}

// Desligar, fechar, 30 dias parado, reabrir, Ligar e uma hora: continua
// ligado (a subida sem o OK grava a rodada, e o Ligar a grava de novo; nada
// freia o que acabou de ligar).
func TestRetencao_DesligarParadoELigarDeNovo(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	a.ligar(1)
	if err := a.h.retencao.Desligar(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.fechar()
	a.relogio.Advance(30 * 24 * time.Hour)
	a.abrir()
	agora := a.relogio.Now()
	a.ditado(idOutro, vencido(agora), textoVelho)
	a.ditado(idAmanha, agora.AddDate(0, -6, 0).Add(30*time.Minute), textoDoPC) // vence em 30 min
	a.ligar(1)
	a.relogio.Advance(time.Hour)
	if !a.chave(storage.ChaveRetencaoAceitaEm) || a.chave(storage.ChaveRetencaoPausadaEm) {
		t.Fatal("o Ligar depois de 30 dias parado não continuou ligado")
	}
	if a.tem(idAmanha) {
		t.Fatal("ligado, o ditado que venceu não saiu na hora seguinte")
	}
}

// Sem o dono fixo (a migração não rodou: a regra antiga), o passo pula e a
// subida diz quanto pulou; nada sai, nem com o OK gravado.
func TestRetencao_SemDonoFixoPula(t *testing.T) {
	caminho, db := historicoDeTeste(t, "mauro")
	if _, err := db.Exec(`UPDATE local_transcriptions SET created_at = ?`,
		inicioDaRetencao.AddDate(-1, 0, 0).UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO instalacao (nome, valor) VALUES (?, 'x')`, storage.ChaveRetencaoAceitaEm); err != nil {
		t.Fatal(err)
	}
	// O destino da cópia do dono fixo já existe: a migração não roda.
	if err := os.WriteFile(storage.CaminhoDaCopia(caminho, inicioDaRetencao), []byte("outra"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	h, err := abrirHistoricoComRetencao(slog.New(slog.NewTextHandler(&buf, nil)), caminho, "mauro", clock.NewFake(inicioDaRetencao))
	if err != nil {
		t.Fatal(err)
	}
	defer h.fechar()
	if h.dono != "mauro" {
		t.Fatalf("o dono = %q; o caso precisa da regra antiga", h.dono)
	}
	if l := linhaDoLog(buf.String(), "6 meses pulados"); !strings.Contains(l, "sem_dono_fixo") || !strings.Contains(l, "fora_da_janela=1") {
		t.Fatalf("o log dos pulados:\n%s", buf.String())
	}
	if noPainel(t, db, "mauro") != 1 {
		t.Fatal("sem o dono fixo, o passo apagou")
	}
	if _, err := h.retencao.Ligar(context.Background(), 1); !errors.Is(err, storage.ErrSemDonoFixo) {
		t.Fatalf("o Ligar sem o dono fixo = %v", err)
	}
	if strings.Contains(buf.String(), "abacaxi") {
		t.Fatalf("o log tem o texto:\n%s", buf.String())
	}
}

// O texto apagado sai de verdade do arquivo (PRAGMA secure_delete): depois de
// os 6 meses apagarem o ditado, os bytes dele não estão no historico.db. A
// cópia do Ligar, ao lado, ainda o tem (é o que ela guarda).
func TestRetencao_OTextoApagadoNaoFicaNoArquivo(t *testing.T) {
	a := novoAgentDaRetencao(t)
	const segredo = "kumquat-F10c-texto-que-sai-do-disco"
	a.ditado(idVelho, vencido(inicioDaRetencao), segredo)
	a.ditado(idNovo, inicioDaRetencao, textoDoPC)
	r := a.ligar(1)
	a.fechar()
	b, err := os.ReadFile(a.caminho)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(segredo)) {
		t.Fatal("o texto apagado ficou nas páginas livres do historico.db")
	}
	if !bytes.Contains(b, []byte(textoDoPC)) {
		t.Fatal("o teste não acha nem o texto que ficou: a busca nos bytes não vale")
	}
	c, err := os.ReadFile(filepath.Join(filepath.Dir(a.caminho), r.Copia))
	if err != nil || !bytes.Contains(c, []byte(segredo)) {
		t.Fatalf("a cópia do Ligar não tem o texto de antes: %v", err)
	}
}

// O secure_delete e o foreign_keys valem também na conexão que o database/sql
// abre depois de trocar a primeira. O modernc descarta a conexão que teve uma
// consulta interrompida por contexto cancelado (a busca do histórico que o
// painel aborta, a aba fechada no meio do GET /api/retencao): a conexão nova
// sai do DSN, e um pragma ligado por Exec na primeira se perderia, até o
// Agent reiniciar. O teste cancela uma consulta, confere os dois pragmas e só
// então apaga e procura o texto nos bytes do arquivo.
func TestRetencao_OsPragmasValemDepoisDeUmaConsultaCancelada(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := abrirBancoDoHistorico(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	pragma := func(nome string) int {
		t.Helper()
		var v int
		if err := db.QueryRow(`PRAGMA ` + nome).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if pragma("secure_delete") != 1 || pragma("foreign_keys") != 1 {
		t.Fatal("os pragmas não valem nem na primeira conexão")
	}

	// Uma consulta sem fim, interrompida pelo contexto.
	ctx, cancelar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelar()
	var n int64
	err = db.QueryRowContext(ctx, `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT max(x) FROM c`).Scan(&n)
	if err == nil {
		t.Fatal("a consulta sem fim terminou: o teste não cancelou nada")
	}

	if v := pragma("secure_delete"); v != 1 {
		t.Fatalf("depois da consulta cancelada, secure_delete = %d; quero 1 em toda conexão", v)
	}
	if v := pragma("foreign_keys"); v != 1 {
		t.Fatalf("depois da consulta cancelada, foreign_keys = %d; quero 1 em toda conexão", v)
	}
	const segredo = "pitanga-F10-pragma-da-conexao-nova"
	if _, err := db.Exec(`CREATE TABLE rascunho (texto TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO rascunho (texto) VALUES (?), (?)`, segredo, textoDoPC); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM rascunho WHERE texto = ?`, segredo); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(segredo)) {
		t.Fatal("o texto apagado ficou nas páginas livres do historico.db depois de a conexão ser trocada")
	}
	if !bytes.Contains(b, []byte(textoDoPC)) {
		t.Fatal("o teste não acha nem o texto que ficou: a busca nos bytes não vale")
	}
}

// A cópia que falha (3 cópias da marca ao lado do banco): o Ligar recusa com
// cópias demais, nada sai nem é gravado, e o WARN diz o motivo e o nome do
// arquivo, nunca o caminho.
func TestRetencao_ACopiaQueFalhaNoLogSemCaminho(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	for i := 1; i <= storage.MaxCopias; i++ {
		falsa := storage.CaminhoDaCopiaDosSeisMeses(a.caminho, inicioDaRetencao.AddDate(0, 0, -i))
		if err := os.WriteFile(falsa, []byte("cópia"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.h.retencao.Ligar(context.Background(), 1); !errors.Is(err, storage.ErrCopiasDemais) {
		t.Fatalf("o Ligar com 3 cópias = %v", err)
	}
	if !a.tem(idVelho) || a.chave(storage.ChaveRetencaoAceitaEm) {
		t.Fatal("o Ligar recusado apagou, ou gravou o OK")
	}
	if l := linhaDoLog(a.log.String(), "os 6 meses não ligaram"); !strings.Contains(l, "motivo=copias_demais") ||
		!strings.Contains(l, "antes-dos-6-meses") {
		t.Fatalf("o WARN da cópia:\n%s", a.log.String())
	}
	a.semNadaNoLog()
}

// O ciclo para antes de o banco fechar: depois do fechar, o Ligar recusa.
func TestRetencao_DepoisDeFecharOLigarRecusa(t *testing.T) {
	a := novoAgentDaRetencao(t)
	ciclo := a.h.retencao
	a.fechar()
	if _, err := ciclo.Ligar(context.Background(), 1); !errors.Is(err, expurgo.ErrParado) {
		t.Fatalf("o Ligar depois de fechar = %v", err)
	}
}
