package storage

import (
	"testing"
	"time"
)

// esperaANuvem, pura (ADR-019, "Decisões da F10", "A guarda da D3"): uma
// linha por regra. Os casos com o banco de verdade, um por linha da tabela de
// situações, estão em lixeira_nuvem_test.go.
func TestEsperaANuvem_AsRegras(t *testing.T) {
	agora := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	criado := agora.AddDate(0, -2, 0)
	esquecida := agora.AddDate(0, -1, 0)
	depois := esquecida.Add(time.Hour)
	antes := esquecida.Add(-time.Hour)
	velho := agora.AddDate(0, -6, 0) // a janela venceu exatamente agora
	quaseVelho := velho.Add(time.Nanosecond)

	base := itemDaLixeira{criadoEm: criado, naLixeiraEm: &depois, situacao: SituacaoNormal}
	com := func(f func(*itemDaLixeira)) itemDaLixeira {
		it := base
		f(&it)
		return it
	}
	cases := []struct {
		nome      string
		it        itemDaLixeira
		esquecida *time.Time
		agora     time.Time
		freio     bool
		fica      bool
	}{
		{"nada: os 30 dias de hoje", base, nil, agora, false, false},
		{"P1, o marcador", com(func(i *itemDaLixeira) { i.comMarcador = true }), nil, agora, false, true},
		{"P1 com server_version", com(func(i *itemDaLixeira) { i.comMarcador, i.comVersao = true, true }), nil, agora, false, true},
		{"P2, anotado sem server_version", com(func(i *itemDaLixeira) { i.anotado = true }), nil, agora, false, true},
		{"P2 não vale com server_version", com(func(i *itemDaLixeira) { i.anotado, i.comVersao = true, true }), nil, agora, false, false},
		{"P3, a lixeira depois do Esquecer num ditado de antes", base, &esquecida, agora, false, true},
		{"P3 não vale com server_version", com(func(i *itemDaLixeira) { i.comVersao = true }), &esquecida, agora, false, false},
		{"P3 não vale com a lixeira de antes do Esquecer", com(func(i *itemDaLixeira) { i.naLixeiraEm = &antes }), &esquecida, agora, false, false},
		{"P3 não vale no ditado criado depois do Esquecer", com(func(i *itemDaLixeira) { i.criadoEm = depois }), &esquecida, agora, false, false},
		{"P3 vale no ditado criado no instante do Esquecer", com(func(i *itemDaLixeira) { i.criadoEm = esquecida }), &esquecida, agora, false, true},
		{"P3 não vale sem deleted_at", com(func(i *itemDaLixeira) { i.naLixeiraEm = nil }), &esquecida, agora, false, false},
		{"P3 não vale sem Esquecer", base, nil, agora, false, false},
		{"recusado sai, mesmo com marcador", com(func(i *itemDaLixeira) { i.comMarcador, i.situacao = true, SituacaoRecusado }), nil, agora, false, false},
		{"fora_da_janela sai, mesmo com marcador", com(func(i *itemDaLixeira) { i.comMarcador, i.situacao = true, SituacaoForaDaJanela }), nil, agora, false, false},
		{"o teto: fora da janela sai, mesmo com marcador", com(func(i *itemDaLixeira) { i.comMarcador, i.criadoEm = true, velho }), nil, agora, false, false},
		{"o teto: 1 ns antes da janela, fica", com(func(i *itemDaLixeira) { i.comMarcador, i.criadoEm = true, quaseVelho }), nil, agora, false, true},
		{"o freio: fora da janela com marcador fica mais uma rodada", com(func(i *itemDaLixeira) { i.comMarcador, i.criadoEm = true, velho }), nil, agora, true, true},
		{"o freio não segura o que não espera a nuvem", com(func(i *itemDaLixeira) { i.criadoEm = velho }), nil, agora, true, false},
		{"o freio não segura o recusado", com(func(i *itemDaLixeira) { i.comMarcador, i.criadoEm, i.situacao = true, velho, SituacaoRecusado }), nil, agora, true, false},
		{"a janela em UTC com o agora em outro fuso", com(func(i *itemDaLixeira) { i.comMarcador, i.criadoEm = true, quaseVelho }), nil,
			agora.In(time.FixedZone("UTC+14", 14*3600)), false, true},
	}
	for _, c := range cases {
		if got := esperaANuvem(c.it, c.esquecida, c.agora, c.freio); got != c.fica {
			t.Errorf("%s: esperaANuvem = %v, quero %v", c.nome, got, c.fica)
		}
	}
}
