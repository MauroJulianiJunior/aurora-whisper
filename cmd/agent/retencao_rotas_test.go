package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As rotas /api/retencao* (ADR-019, "Decisões da F10", F10d e F10e) ligadas ao
// ciclo do expurgo de verdade, pelo mesmo abrirHistoricoComRetencao que o Agent
// usa: um SQLite de verdade na pasta temporária, o relógio falso e o handler da
// Local API (sem a cadeia do guard, que tem teste próprio). Nenhum teste abre
// o bin\ de ninguém, roda o Agent ou apaga fora de t.TempDir().

// apiDaRetencao é a Local API montada sobre o histórico aberto, como run a
// monta (Preferencias.Retencao = o ciclo).
func (a *agentDaRetencao) api() http.Handler {
	a.t.Helper()
	return localapi.New(a.h.dono, a.h.hist, a.h.prefs, localapi.Preferencias{
		Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{},
		Retencao: a.h.retencao, Log: nil,
	}).Handler()
}

type retencaoNoFio struct {
	Previa      int  `json:"previa"`
	NaLixeira   int  `json:"na_lixeira"`
	ComSelo     int  `json:"com_selo"`
	DeOutroDono int  `json:"de_outro_dono"`
	Ligada      bool `json:"ligada"`
	Pausada     bool `json:"pausada"`
	Copias      int  `json:"copias"`
	Apagados    int  `json:"apagados"`
	Apagadas    int  `json:"apagadas"`
	Ficaram     int  `json:"ficaram"`
}

func lerRetencao(t *testing.T, w *httptest.ResponseRecorder, status int) retencaoNoFio {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d; quero %d: %s", w.Code, status, w.Body)
	}
	var r retencaoNoFio
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return r
}

func (a *agentDaRetencao) copiasNaPasta() []string {
	a.t.Helper()
	nomes, err := storage.CopiasAoLado(a.caminho)
	if err != nil {
		a.t.Fatal(err)
	}
	return nomes
}

// O caminho inteiro pelo HTTP: a contagem, o Ligar que recusa a contagem velha
// sem apagar nada, o OK, as cópias ao lado do banco e o botão que as apaga.
func TestRotas_ODoGetAoApagarAsCopias(t *testing.T) {
	a := novoAgentDaRetencao(t)
	api := a.api()
	a.ditado(idVelho, vencido(inicioDaRetencao), textoVelho)
	a.ditado(idOutro, vencido(inicioDaRetencao).Add(time.Hour), textoVelho)
	a.ditado(idNovo, inicioDaRetencao, textoDoPC)
	if err := a.h.hist.SoftDelete(t.Context(), a.h.dono, idOutro, inicioDaRetencao); err != nil {
		t.Fatal(err)
	}
	// Uma cópia de antes do dono, de uma instalação anterior.
	antiga := storage.CaminhoDaCopia(a.caminho, inicioDaRetencao.AddDate(0, -1, 0))
	if err := os.WriteFile(antiga, []byte("cópia antiga"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Desligada: a contagem, sem apagar nada (o GET só lê).
	g := lerRetencao(t, chamar(t, api, "GET", "/api/retencao", nil), 200)
	if g.Previa != 2 || g.NaLixeira != 1 || g.ComSelo != 0 || g.DeOutroDono != 0 || g.Ligada || g.Pausada || g.Copias != 1 {
		t.Fatalf("o GET = %+v", g)
	}
	if !a.tem(idVelho) || !a.tem(idOutro) || a.chave(storage.ChaveRetencaoAceitaEm) {
		t.Fatal("o GET apagou, ou gravou o OK")
	}

	// A contagem velha: 409 com o número novo, e nada gravado nem copiado.
	w := chamar(t, api, "POST", "/api/retencao/ligar", map[string]any{"previa": 99})
	var recusa struct {
		Error  struct{ Message string }
		Previa int
	}
	if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &recusa) != nil || recusa.Previa != g.Previa {
		t.Fatalf("o Ligar com a contagem velha = %d %s; quero 409 com previa %d", w.Code, w.Body, g.Previa)
	}
	if a.chave(storage.ChaveRetencaoAceitaEm) || len(a.copiasNaPasta()) != 1 || !a.tem(idVelho) {
		t.Fatal("o Ligar recusado gravou o OK, copiou ou apagou")
	}

	// O OK com a contagem certa: apaga exatamente o contado, com a cópia ao lado.
	l := lerRetencao(t, chamar(t, api, "POST", "/api/retencao/ligar", map[string]any{"previa": g.Previa}), 200)
	if l.Apagados != g.Previa || !l.Ligada || l.Previa != 0 || l.Copias != 2 {
		t.Fatalf("o Ligar = %+v", l)
	}
	if a.tem(idVelho) || a.tem(idOutro) || !a.tem(idNovo) {
		t.Fatal("o Ligar apagou o que não devia, ou não apagou o contado")
	}
	if !a.chave(storage.ChaveRetencaoAceitaEm) || !a.chave(storage.ChaveRetencaoCopia) {
		t.Fatal("o OK ou a cópia não ficaram gravados")
	}

	// Ligar de novo: 200, nada a fazer.
	if r := lerRetencao(t, chamar(t, api, "POST", "/api/retencao/ligar", map[string]any{"previa": 0}), 200); r.Apagados != 0 || !r.Ligada {
		t.Fatalf("o Ligar já ligado = %+v", r)
	}

	// O botão das cópias: as duas saem (a antiga e a dos 6 meses), a chave da
	// cópia sai, e a regra continua ligada.
	c := lerRetencao(t, chamar(t, api, "POST", "/api/retencao/copias/apagar", nil), 200)
	if c.Apagadas != 2 || c.Ficaram != 0 || c.Copias != 0 || !c.Ligada {
		t.Fatalf("apagar as cópias = %+v", c)
	}
	if got := a.copiasNaPasta(); len(got) != 0 {
		t.Fatalf("sobraram cópias: %v", got)
	}
	if a.chave(storage.ChaveRetencaoCopia) || !a.chave(storage.ChaveRetencaoAceitaEm) {
		t.Fatal("a chave da cópia ficou, ou o OK saiu junto")
	}

	// O Desligar: o estado volta a desligado, e nada mais sai com o tempo.
	d := lerRetencao(t, chamar(t, api, "POST", "/api/retencao/desligar", nil), 200)
	if d.Ligada || d.Pausada {
		t.Fatalf("o Desligar = %+v", d)
	}
	a.ditado(idAmanha, inicioDaRetencao.AddDate(0, -6, 5), textoDoPC)
	a.relogio.Advance(30 * 24 * time.Hour)
	if !a.tem(idAmanha) {
		t.Fatal("depois do Desligar, o ciclo apagou")
	}

	// O log do caminho todo: só contagens, nunca id, texto ou caminho.
	l2 := a.log.String()
	for _, esperado := range []string{"historico: 6 meses ligados", "historico: cópias apagadas", "historico: 6 meses desligados"} {
		if !strings.Contains(l2, esperado) {
			t.Errorf("o log não tem %q:\n%s", esperado, l2)
		}
	}
	a.semNadaNoLog()
}

// O Ligar sem nada a apagar: 409, sem cópia e sem OK; e o OK que a tela não deu
// (nenhum POST) nunca é gravado por um GET.
func TestRotas_NadaAApagarNaoCopiaNemLiga(t *testing.T) {
	a := novoAgentDaRetencao(t)
	api := a.api()
	a.ditado(idNovo, inicioDaRetencao, textoDoPC)
	g := lerRetencao(t, chamar(t, api, "GET", "/api/retencao", nil), 200)
	if g.Previa != 0 || g.Ligada || g.Copias != 0 {
		t.Fatalf("o GET = %+v", g)
	}
	w := chamar(t, api, "POST", "/api/retencao/ligar", map[string]any{"previa": 0})
	var e struct{ Error struct{ Message string } }
	if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message != localapi.MsgNadaAApagar {
		t.Fatalf("o Ligar sem nada a apagar = %d %s", w.Code, w.Body)
	}
	if a.chave(storage.ChaveRetencaoAceitaEm) || len(a.copiasNaPasta()) != 0 {
		t.Fatal("o Ligar sem nada a apagar copiou ou gravou o OK")
	}
	// Sem o corpo certo, nem chega ao ciclo.
	if w := chamar(t, api, "POST", "/api/retencao/ligar", map[string]any{}); w.Code != 400 {
		t.Fatalf("o Ligar sem a contagem = %d %s", w.Code, w.Body)
	}
	if a.chave(storage.ChaveRetencaoAceitaEm) {
		t.Fatal("o OK foi gravado sem a contagem")
	}
}

// A cópia do envio à nuvem que ainda não terminou não sai pelo botão; com o
// aceite, sai. E o botão não toca no historico.db, no -wal nem em outro arquivo.
func TestRotas_ACopiaDoEnvioEmCursoFica(t *testing.T) {
	a := novoAgentDaRetencao(t)
	a.ditado(idNovo, inicioDaRetencao, textoDoPC)
	envio := storage.CaminhoDaCopiaDoEnvio(a.caminho, inicioDaRetencao)
	dono := storage.CaminhoDaCopia(a.caminho, inicioDaRetencao.Add(-time.Hour))
	outro := filepath.Join(filepath.Dir(a.caminho), "anotacao.txt")
	for _, f := range []string{envio, dono, outro} {
		if err := os.WriteFile(f, []byte("conteúdo de "+filepath.Base(f)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// O envio em curso: a nuvem_copia registrada, sem o aceite, gravada com o
	// Agent fechado (o que o EnviarHistorico deixa).
	a.fechar()
	gravarNaInstalacao(t, a.caminho, storage.ChaveNuvemCopia, envio)
	a.abrir()
	api := a.api()

	g := lerRetencao(t, chamar(t, api, "GET", "/api/retencao", nil), 200)
	if g.Copias != 2 {
		t.Fatalf("o GET conta %d cópias; quero 2", g.Copias)
	}
	c := lerRetencao(t, chamar(t, api, "POST", "/api/retencao/copias/apagar", nil), 200)
	if c.Apagadas != 1 || c.Ficaram != 1 || c.Copias != 1 {
		t.Fatalf("apagar = %+v; quero 1 apagada e a do envio que fica", c)
	}
	if _, err := os.Stat(envio); err != nil {
		t.Fatal("a cópia do envio que não terminou saiu")
	}
	if _, err := os.Stat(dono); err == nil {
		t.Fatal("a cópia do dono não saiu")
	}
	for _, f := range []string{a.caminho, outro} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s saiu: %v", filepath.Base(f), err)
		}
	}

	// O envio terminou (o aceite gravado): a cópia sai.
	a.fechar()
	gravarNaInstalacao(t, a.caminho, storage.ChaveNuvemEnvioAceitoEm, inicioDaRetencao.UTC().Format(time.RFC3339))
	a.abrir()
	c = lerRetencao(t, chamar(t, a.api(), "POST", "/api/retencao/copias/apagar", nil), 200)
	if c.Apagadas != 1 || c.Ficaram != 0 || c.Copias != 0 {
		t.Fatalf("apagar depois do aceite = %+v", c)
	}
	a.semNadaNoLog()
}

// gravarNaInstalacao grava uma chave na tabela instalacao com o Agent fechado.
func gravarNaInstalacao(t *testing.T, caminho, nome, valor string) {
	t.Helper()
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`, nome, valor); err != nil {
		t.Fatal(err)
	}
}
