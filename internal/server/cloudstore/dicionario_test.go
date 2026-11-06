package cloudstore

import (
	"context"
	"testing"
	"time"
)

// A regra do PUT da cópia do dicionário (ADR-023, "C"): o tomar, o manter e o
// reenvio do mesmo SHA-256, em cada estado da fonte.
func TestDecidirCopia(t *testing.T) {
	const eu, outroPC = "pc-eu", "pc-outro"
	n := NovaCopia{SHA256: "novo"}
	deMim := &CopiaDoDicionario{DeviceID: eu, SHA256: "antigo"}
	deMimIgual := &CopiaDoDicionario{DeviceID: eu, SHA256: "novo"}
	doOutro := &CopiaDoDicionario{DeviceID: outroPC, SHA256: "antigo"}
	doOutroIgual := &CopiaDoDicionario{DeviceID: outroPC, SHA256: "novo"}
	for _, c := range []struct {
		nome        string
		atual       *CopiaDoDicionario
		modo        ModoDaCopia
		grava, naoE bool
	}{
		{"tomar sem cópia", nil, CopiaTomar, true, false},
		{"manter sem cópia", nil, CopiaManter, false, true},
		{"tomar da fonte", deMim, CopiaTomar, true, false},
		{"manter da fonte", deMim, CopiaManter, true, false},
		{"tomar do mesmo SHA-256 da fonte", deMimIgual, CopiaTomar, false, false},
		{"manter do mesmo SHA-256 da fonte", deMimIgual, CopiaManter, false, false},
		{"tomar com outro PC como fonte", doOutro, CopiaTomar, true, false},
		{"manter com outro PC como fonte", doOutro, CopiaManter, false, true},
		// O SHA-256 igual, mas de outro PC: tomar passa a fonte para este.
		{"tomar o mesmo SHA-256 de outro PC", doOutroIgual, CopiaTomar, true, false},
		{"manter o mesmo SHA-256 de outro PC", doOutroIgual, CopiaManter, false, true},
	} {
		grava, naoE := DecidirCopia(c.atual, eu, n, c.modo)
		if grava != c.grava || naoE != c.naoE {
			t.Errorf("%s: grava=%v naoEraAFonte=%v, quero %v e %v", c.nome, grava, naoE, c.grava, c.naoE)
		}
	}
}

func TestFontePara(t *testing.T) {
	var nenhuma *CopiaDoDicionario
	c := &CopiaDoDicionario{DeviceID: "a"}
	if nenhuma.FontePara("a") != FonteNenhuma || c.FontePara("a") != FonteEste || c.FontePara("b") != FonteOutro {
		t.Errorf("a fonte: %s %s %s", nenhuma.FontePara("a"), c.FontePara("a"), c.FontePara("b"))
	}
	if !CopiaTomar.Valido() || !CopiaManter.Valido() || ModoDaCopia("").Valido() || ModoDaCopia("TOMAR").Valido() {
		t.Error("os modos válidos são tomar e manter")
	}
}

// O MemoryStore: a cópia sai com a revogação do PC que é a fonte, no "apagar
// meus dados" e depois de restaurar; um PC revogado não grava; e a leitura de uma
// cópia devolve bytes que o chamador pode mudar sem mudar o Store.
func TestMemoryStore_CopiaDoDicionario(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s := NewMemoryStore()
	u := "00000000-0000-7000-8000-000000000001"
	pc1, pc2 := "00000000-0000-7000-8000-0000000000a1", "00000000-0000-7000-8000-0000000000a2"
	s.AddUser(u)
	s.AddDevice(u, pc1)
	s.AddDevice(u, pc2)
	a1, a2 := Actor{UserID: u, DeviceID: pc1}, Actor{UserID: u, DeviceID: pc2}
	nova := NovaCopia{Conteudo: []byte(`{"usuario":"copia","entradas":[]}`), SHA256: "x", Termos: 0}

	if _, err := s.PutDictionaryCopy(ctx, Actor{UserID: u}, nova, CopiaTomar, now); err != ErrDeviceNotAllowed {
		t.Errorf("PUT sem aparelho: %v", err)
	}
	if _, err := s.PutDictionaryCopy(ctx, a1, nova, CopiaTomar, now); err != nil {
		t.Fatal(err)
	}
	_, b, ok, err := s.GetDictionaryCopy(ctx, u)
	if err != nil || !ok || string(b) != string(nova.Conteudo) {
		t.Fatalf("GetDictionaryCopy = %q %v %v", b, ok, err)
	}
	b[0] = 'X'
	if _, b2, _, _ := s.GetDictionaryCopy(ctx, u); string(b2) != string(nova.Conteudo) {
		t.Error("mudar os bytes lidos mudou o Store")
	}
	// Revogar o PC que não é a fonte deixa a cópia; revogar a fonte a apaga.
	if err := s.RevogarAparelho(ctx, u, pc2, now); err != nil {
		t.Fatal(err)
	}
	if info, _ := s.DictionaryCopyInfo(ctx, u); info == nil {
		t.Error("revogar outro PC apagou a cópia")
	}
	if _, err := s.PutDictionaryCopy(ctx, a2, nova, CopiaTomar, now); err != ErrUnknownDevice {
		t.Errorf("PUT do PC revogado: %v, quero ErrUnknownDevice", err)
	}
	if err := s.RevogarAparelho(ctx, u, pc1, now); err != nil {
		t.Fatal(err)
	}
	if info, _ := s.DictionaryCopyInfo(ctx, u); info != nil {
		t.Errorf("revogar a fonte deixou a cópia: %+v", info)
	}

	// O apagar meus dados e a restauração.
	for nome, apaga := range map[string]func(*MemoryStore){
		"apagar meus dados": func(m *MemoryStore) { _, _ = m.ApagarDadosDaConta(ctx, u, "", now) },
		"restaurar":         func(m *MemoryStore) { _, _ = m.DepoisDeRestaurar(ctx, now) },
	} {
		m := NewMemoryStore()
		m.AddUser(u)
		m.AddDevice(u, pc1)
		if _, err := m.PutDictionaryCopy(ctx, a1, nova, CopiaTomar, now); err != nil {
			t.Fatal(err)
		}
		apaga(m)
		if info, _ := m.DictionaryCopyInfo(ctx, u); info != nil {
			t.Errorf("%s deixou a cópia: %+v", nome, info)
		}
		if m.users[u] != nil && m.users[u].copiaDoDicionario != nil {
			t.Errorf("%s deixou a linha da cópia", nome)
		}
	}
}
