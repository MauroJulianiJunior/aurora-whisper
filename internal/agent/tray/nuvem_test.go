package tray

import "testing"

// A linha da nuvem (ADR-019, F7b, decisão 3): informação desabilitada, logo
// depois do provider, com o texto que o laço de sync dá; sem a nuvem, ela não
// existe (o menu de quem nunca conectou fica igual ao de antes).
func TestMenu_LinhaDaNuvem(t *testing.T) {
	sem := Menu(Estado{})
	for _, it := range sem {
		if it.ID == IDNuvem {
			t.Fatal("a linha da nuvem aparece sem nuvem")
		}
	}
	for _, texto := range []string{"Nuvem: em dia", "Nuvem: 3 para enviar", "Nuvem: sem conexão", "Nuvem: aguardando", "Nuvem: resposta inesperada", "Nuvem: desconectada"} {
		m := Menu(Estado{Nuvem: texto})
		if len(m) != len(sem)+1 {
			t.Fatalf("%q: %d itens; quero %d", texto, len(m), len(sem)+1)
		}
		var antes ID
		for i, it := range m {
			if it.ID == IDNuvem {
				antes = m[i-1].ID
				if it.Texto != texto || it.Habilitado || it.Marcado {
					t.Errorf("a linha: %+v", it)
				}
			}
		}
		if antes != IDProvider {
			t.Errorf("%q: a linha vem depois de %d; quero depois do provider", texto, antes)
		}
	}
}
