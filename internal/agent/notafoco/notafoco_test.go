package notafoco

import (
	"sync"
	"testing"
	"time"
)

// relogio é um relógio na mão do teste.
type relogio struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relogio) agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogio) passa(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

func novoComRelogio() (*Marcador, *relogio) {
	r := &relogio{t: time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)}
	return Novo(r.agora), r
}

func valorDe(m *Marcador) string {
	if p := m.ParaOHistorico(); p != nil {
		return *p
	}
	return "<nil>"
}

func TestSemAvisoODitadoEComum(t *testing.T) {
	m, _ := novoComRelogio()
	if v := valorDe(m); v != "<nil>" {
		t.Fatalf("sem aviso: %s", v)
	}
	var nulo *Marcador
	if nulo.ParaOHistorico() != nil {
		t.Fatal("marcador nil deve dar nil (sem painel ligado)")
	}
}

func TestNotaExistenteEOCaixaNova(t *testing.T) {
	m, _ := novoComRelogio()
	if !m.DefinirNota("nota-123") {
		t.Fatal("id válido recusado")
	}
	if v := valorDe(m); v != "nota-123" {
		t.Fatalf("nota: %s", v)
	}
	if !m.DefinirNova("0b6f1c2e-aaaa-bbbb") {
		t.Fatal("token válido recusado")
	}
	if v := valorDe(m); v != "nova:0b6f1c2e-aaaa-bbbb" {
		t.Fatalf("nova: %s", v)
	}
}

func TestVenceSemRenovacaoERenovando(t *testing.T) {
	m, r := novoComRelogio()
	m.DefinirNota("n1")

	r.passa(TTL - time.Second)
	if valorDe(m) != "n1" {
		t.Fatal("ainda valia antes do TTL")
	}
	// A renovação (o painel manda de novo) estende a validade.
	m.DefinirNota("n1")
	r.passa(TTL - time.Second)
	if valorDe(m) != "n1" {
		t.Fatal("renovado, ainda vale")
	}
	r.passa(time.Second)
	if v := valorDe(m); v != "<nil>" {
		t.Fatalf("no TTL exato o aviso venceu: %s", v)
	}
	// Vencido uma vez, continua vazio: não revive com o relógio parado.
	if valorDe(m) != "<nil>" {
		t.Fatal("aviso vencido voltou")
	}
}

func TestLimparSoSeForOMesmo(t *testing.T) {
	m, _ := novoComRelogio()
	m.DefinirNota("n1")
	// O DELETE da nota antiga chega depois do PUT da nova: não pode apagar o novo.
	m.DefinirNota("n2")
	if m.LimparNota("n1") {
		t.Fatal("limpou o aviso de outra nota")
	}
	if valorDe(m) != "n2" {
		t.Fatal("o aviso de n2 sumiu")
	}
	if !m.LimparNota("n2") || valorDe(m) != "<nil>" {
		t.Fatal("não limpou o próprio aviso")
	}

	m.DefinirNova("tk")
	if m.LimparNota("tk") {
		t.Fatal("id e token não se confundem")
	}
	if m.LimparNova("outro") {
		t.Fatal("limpou o token de outra caixa")
	}
	if !m.LimparNova("tk") || valorDe(m) != "<nil>" {
		t.Fatal("não limpou a caixa")
	}

	m.DefinirNota("n3")
	m.Limpar()
	if valorDe(m) != "<nil>" {
		t.Fatal("Limpar deixou o aviso")
	}
}

func TestRecusaIdentificadorsRuins(t *testing.T) {
	m, _ := novoComRelogio()
	ruins := []string{"", "com espaço", "a:b", "%", "x/y", "ç", string(make([]byte, 65))}
	longo := ""
	for i := 0; i < 65; i++ {
		longo += "a"
	}
	ruins = append(ruins, longo)
	for _, s := range ruins {
		if m.DefinirNota(s) {
			t.Errorf("DefinirNota(%q) aceitou", s)
		}
		if m.DefinirNova(s) {
			t.Errorf("DefinirNova(%q) aceitou", s)
		}
	}
	if valorDe(m) != "<nil>" {
		t.Fatal("recusar não pode deixar aviso")
	}
}

func TestConcorrencia(t *testing.T) {
	m := Novo(nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				m.DefinirNota("n")
				_ = m.ParaOHistorico()
				m.LimparNota("n")
				m.DefinirNova("t")
				m.Limpar()
			}
		}()
	}
	wg.Wait()
}
