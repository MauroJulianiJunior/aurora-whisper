package nuvem_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O lado do histórico na seção Nuvem (ADR-019, "Decisões da F7c"), com um
// dublê do storage: o resumo no Estado, o "Enviar o histórico" (só
// conectado; a cópia que falha diz por quê, sem caminho no log) e o "Esquecer
// esta nuvem" (só desconectado; o token sai; a D3 deixa conectar a outra
// nuvem depois). O storage de verdade está em internal/storage e na ponta a
// ponta (internal/server/integration).

type historicoDeTeste struct {
	mu        sync.Mutex
	inst      *instalacaoMem
	resumo    storage.ResumoDoEnvio
	errEnviar error
	enviou    int
	esqueceu  int
}

func (h *historicoDeTeste) Resumo(context.Context, time.Time) (storage.ResumoDoEnvio, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.resumo, nil
}

func (h *historicoDeTeste) Enviar(context.Context, time.Time) (storage.ResultadoDoEnvio, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.errEnviar != nil {
		return storage.ResultadoDoEnvio{}, h.errEnviar
	}
	h.enviou++
	h.resumo.Aceito, h.resumo.Total, h.resumo.Previa = true, h.resumo.Previa, 0
	return storage.ResultadoDoEnvio{Marcados: h.resumo.Total, Copia: "historico.db.antes-da-nuvem-x.db"}, nil
}

// Esquecer faz o que o storage faz com as chaves da conexão (o resto é do
// banco).
func (h *historicoDeTeste) Esquecer(ctx context.Context, _ time.Time) error {
	h.mu.Lock()
	h.esqueceu++
	h.mu.Unlock()
	return h.inst.Apagar(ctx, "nuvem_url", "nuvem_conta", "nuvem_email", "nuvem_nome", "nuvem_aparelho", "nuvem_conectado_em")
}

// novoCenarioComHistorico monta o Servico com o dublê do histórico e o aviso
// do laço contado.
func novoCenarioComHistorico(t *testing.T) (*cenario, *historicoDeTeste, *int) {
	t.Helper()
	var h *historicoDeTeste
	conferido := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	c := novoCenario(t, func(cfg *nuvem.Config) {
		h = &historicoDeTeste{resumo: storage.ResumoDoEnvio{Previa: 800, ConferidoEm: &conferido}}
		cfg.Historico = h
	})
	h.inst = c.inst
	acordou := 0
	c.s.AoConectar(func() { acordou++ })
	return c, h, &acordou
}

func TestHistorico_ResumoNoEstado(t *testing.T) {
	c, h, _ := novoCenarioComHistorico(t)
	if e := c.estado(); e.Historico != nil {
		t.Fatalf("sem nuvem gravada, o estado trouxe o resumo: %+v", e.Historico)
	}
	c.conectarDeVez()
	conferido := time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC)
	h.resumo = storage.ResumoDoEnvio{Previa: 3, Trocas: 7, Feitos: 120, Total: 800, NaoSubiram: 2, Pendentes: 680, Mudancas: 4,
		ConferidoEm: &conferido, Falhou: true, OutraNuvem: true}
	e := c.estado()
	r := e.Historico
	if r == nil || r.Previa != 3 || r.PreviaTrocas != 7 || r.Envio == nil || r.Envio.Feitos != 120 || r.Envio.Total != 800 || r.NaoSubiram != 2 ||
		r.Pendentes != 680 || r.Mudancas != 4 || r.ConferidoEm != "2026-10-06T12:30:00Z" || !r.Falhou || !r.OutraNuvem {
		t.Fatalf("o resumo conectado: %+v", r)
	}
	h.resumo = storage.ResumoDoEnvio{Previa: 5}
	if r := c.estado().Historico; r == nil || r.Envio != nil || r.ConferidoEm != "" {
		t.Errorf("sem envio: %+v", r)
	}
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e := c.estado(); e.Estado != nuvem.Desconectado || e.Historico == nil || e.Historico.Previa != 5 || e.Email != emailFalso {
		t.Errorf("desconectado com a nuvem gravada (o resumo e a conta, para o esquecer): %+v", e)
	}
}

// Os nomes do historico no fio são os que a web lê (06, F8h), escritos aqui
// por extenso, e não pela struct do Go: previa_trocas sempre, e freio_trocas
// só conectado e com o freio (ausente sem ele, e desconectado).
func TestHistorico_OsNomesDoFio(t *testing.T) {
	c, h, _ := novoCenarioComHistorico(t)
	freio := 12
	c.s.UsarFreio(func() int { return freio })
	c.conectarDeVez()
	h.resumo = storage.ResumoDoEnvio{Previa: 0, Trocas: 3}
	campos := func() map[string]json.RawMessage {
		t.Helper()
		b, err := json.Marshal(c.estado())
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Historico map[string]json.RawMessage `json:"historico"`
		}
		if err := json.Unmarshal(b, &v); err != nil || v.Historico == nil {
			t.Fatalf("o estado sem historico: %s (%v)", b, err)
		}
		return v.Historico
	}
	m := campos()
	if string(m["previa_trocas"]) != "3" || string(m["freio_trocas"]) != "12" || string(m["previa"]) != "0" {
		t.Errorf("conectado, com o freio: %v", m)
	}
	if _, ok := m["trocas"]; ok {
		t.Error("o historico ainda traz o nome velho, trocas")
	}
	freio = 0
	if m := campos(); string(m["previa_trocas"]) != "3" {
		t.Errorf("sem o freio: %v", m)
	} else if _, ok := m["freio_trocas"]; ok {
		t.Errorf("sem o freio, o freio_trocas veio: %v", m)
	}
	freio = 12
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m := campos(); m["freio_trocas"] != nil {
		t.Errorf("desconectado, o freio_trocas veio: %v", m)
	}
}

func TestHistorico_EnviarSoConectado(t *testing.T) {
	c, h, acordou := novoCenarioComHistorico(t)
	_, err := c.s.EnviarHistorico(context.Background())
	querErro(t, err, http.StatusConflict, nuvem.MsgConecteAntes)
	if h.enviou != 0 || *acordou != 0 {
		t.Fatal("desconectado, o envio rodou")
	}
	c.conectarDeVez()
	// Antes da primeira rodada com esta conexão: espera.
	conferido := h.resumo.ConferidoEm
	h.resumo.ConferidoEm = nil
	_, err = c.s.EnviarHistorico(context.Background())
	querErro(t, err, http.StatusConflict, nuvem.MsgConferindo)
	if h.enviou != 0 {
		t.Fatal("antes da primeira rodada, o envio rodou")
	}
	h.resumo.ConferidoEm = conferido
	antes := *acordou
	e, err := c.s.EnviarHistorico(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.enviou != 1 || *acordou != antes+1 {
		t.Errorf("enviou %d, acordou %d", h.enviou, *acordou-antes)
	}
	if e.Estado != nuvem.Conectado || e.Historico == nil || !e.Historico.Aceito || e.Historico.Envio == nil || e.Historico.Envio.Total != 800 {
		t.Errorf("o estado depois do envio: %+v / %+v", e, e.Historico)
	}
	if log := c.textoDoLog(); !strings.Contains(log, "marcados=800 trocas=0") || strings.Contains(log, "antes-da-nuvem") {
		t.Errorf("o log do envio: %s", log)
	}
}

func TestHistorico_CopiaQueFalhaDizPorQue(t *testing.T) {
	for nome, caso := range map[string]struct {
		err    error
		status int
		msg    string
	}{
		"cópias demais": {&storage.ErroDaCopia{Motivo: storage.ErrCopiasDemais, Detalhe: "já há 3 cópias"}, http.StatusConflict,
			nuvem.MsgCopiaFalhou + " " + nuvem.MsgCopiasDemais},
		"disco cheio": {&storage.ErroDaCopia{Motivo: storage.ErrDiscoCheio, Detalhe: "database or disk is full"}, http.StatusInsufficientStorage,
			nuvem.MsgCopiaFalhou + " " + nuvem.MsgDiscoCheio},
		"outra falha": {&storage.ErroDaCopia{Motivo: storage.ErrCopiaDoEnvio, Detalhe: "a cópia não confere"}, http.StatusInternalServerError,
			nuvem.MsgCopiaFalhou},
	} {
		t.Run(nome, func(t *testing.T) {
			c, h, acordou := novoCenarioComHistorico(t)
			c.conectarDeVez()
			antes := *acordou
			h.errEnviar = caso.err
			_, err := c.s.EnviarHistorico(context.Background())
			querErro(t, err, caso.status, caso.msg)
			if *acordou != antes {
				t.Error("a cópia falhou e o laço foi acordado")
			}
			if !strings.Contains(c.textoDoLog(), "a cópia do histórico antes do envio falhou") {
				t.Errorf("o log: %s", c.textoDoLog())
			}
			// O erro que a Local API registra não leva o caminho nem o
			// detalhe: só o motivo.
			if inner := errors.Unwrap(err); inner == nil || strings.Contains(inner.Error(), "cópia não confere") {
				t.Errorf("a causa do erro: %v", inner)
			}
		})
	}
	if !strings.Contains(nuvem.MsgCopiasDemais, "3 cópias") {
		t.Errorf("a frase das cópias: %q", nuvem.MsgCopiasDemais)
	}
}

func TestHistorico_EsquecerSoDesconectado(t *testing.T) {
	c, h, _ := novoCenarioComHistorico(t)
	// Sem nuvem gravada: nada a esquecer.
	if _, err := c.s.Esquecer(context.Background()); err != nil || h.esqueceu != 0 {
		t.Fatalf("sem nuvem: %v, esqueceu %d", err, h.esqueceu)
	}
	tok := c.conectarDeVez()
	_, err := c.s.Esquecer(context.Background())
	querErro(t, err, http.StatusConflict, nuvem.MsgDesconecteAntes)
	if h.esqueceu != 0 {
		t.Fatal("conectado, esqueceu")
	}
	if got, _ := c.cofre.Ler(nuvem.NomeNoCofrePortatil); got != tok {
		t.Fatal("conectado, o token saiu")
	}
	// Com um pedido em curso (o Conectar de novo depois de desconectar).
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.conectar(urlDaNuvem)
	_, err = c.s.Esquecer(context.Background())
	querErro(t, err, http.StatusConflict, nuvem.MsgCancelePedido)
	if _, err := c.s.Desconectar(context.Background()); err != nil { // o Cancelar
		t.Fatal(err)
	}
}

// Desconectado: o token que ficou no Cofre sai (o desconectar o apaga, mas
// um que sobrou à mão também), a instalação esquece a nuvem e a conta, o
// aviso some, e a D3 deixa conectar a OUTRA nuvem.
func TestHistorico_EsquecerEConectarAOutraNuvem(t *testing.T) {
	c, h, acordou := novoCenarioComHistorico(t)
	c.conectarDeVez()
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A outra nuvem, antes: a D3 recusa.
	_, err := c.s.Conectar(context.Background(), "http://localhost:9090")
	querErro(t, err, http.StatusConflict, nuvem.MsgOutraConta)

	// Um token que sobrou no Cofre (sem o aparelho na instalação).
	if err := c.cofre.Gravar(nuvem.NomeNoCofrePortatil, "sobrou"); err != nil {
		t.Fatal(err)
	}
	antes := *acordou
	e, err := c.s.Esquecer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.esqueceu != 1 || *acordou != antes+1 {
		t.Errorf("esqueceu %d, acordou %d", h.esqueceu, *acordou-antes)
	}
	if e.Estado != nuvem.Desconectado || e.URL != "" || e.Aviso != "" || e.Historico != nil {
		t.Errorf("o estado depois de esquecer: %+v", e)
	}
	if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token ficou no Cofre")
	}
	if c.inst.valor("install_id") == "" {
		t.Error("o install_id saiu (ele fica: é o mesmo PC)")
	}
	if !strings.Contains(c.textoDoLog(), "este PC esqueceu a nuvem") {
		t.Errorf("o log: %s", c.textoDoLog())
	}
	// Agora a outra nuvem passa da D3 (o pedido sai para ela).
	e, err = c.s.Conectar(context.Background(), urlDaNuvem)
	if err != nil || e.Estado != nuvem.Conectando {
		t.Fatalf("conectar depois de esquecer: %+v, %v", e, err)
	}
}

// O Cofre que falha: nada é esquecido.
func TestHistorico_EsquecerComOCofreQueFalha(t *testing.T) {
	c, h, _ := novoCenarioComHistorico(t)
	c.conectarDeVez()
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	// O dublê de Cofre do cenário é o chaves.Memoria; para a falha, um
	// Servico novo sobre a mesma instalação.
	s := nuvem.Novo(nuvem.Config{Cofre: &cofreQueFalha{Memoria: c.cofre}, NomeNoCofre: nuvem.NomeNoCofrePortatil,
		Instalacao: c.inst, Abrir: func(string) error { return nil }, NovoID: func() string { return "x" }, Historico: h})
	_, err := s.Esquecer(context.Background())
	querErro(t, err, http.StatusInternalServerError, nuvem.MsgCofre)
	if h.esqueceu != 0 || c.inst.valor("nuvem_url") == "" {
		t.Error("o Cofre falhou e a nuvem foi esquecida")
	}
}

type cofreQueFalha struct{ *chaves.Memoria }

func (cofreQueFalha) Apagar(string) error { return errors.New("o Gerenciador não respondeu") }

// Sem o Historico, as duas ações são 503.
func TestHistorico_SemHistorico(t *testing.T) {
	c := novoCenario(t)
	_, err := c.s.EnviarHistorico(context.Background())
	querErro(t, err, http.StatusServiceUnavailable, nuvem.MsgSemHistorico)
	_, err = c.s.Esquecer(context.Background())
	querErro(t, err, http.StatusServiceUnavailable, nuvem.MsgSemHistorico)
}
