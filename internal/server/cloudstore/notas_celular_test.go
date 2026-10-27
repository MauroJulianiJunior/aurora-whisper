package cloudstore

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Os planos das escritas do celular (N8), puros: o que cada pedido vira, o
// destino do "Ditar para nota" e o Desfazer dele. A suíte de contrato roda os
// mesmos pedidos pela API, na memória e no Postgres.

var agoraDoCelular = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func notaDoCelular(id, titulo, corpo, posicao string, version int64) Note {
	return Note{ID: id, Titulo: titulo, Corpo: corpo, Posicao: posicao, Version: version, ContentVersion: 1, TrashVersion: 1,
		Marcadores: []string{}, CreatedAt: agoraDoCelular.Add(-time.Hour), UpdatedAt: agoraDoCelular.Add(-time.Hour)}
}

func payloadDe(t *testing.T, m Mutation) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return p
}

func contextoDe(notas ...Note) ContextoDaNota {
	return ContextoDaNota{
		Nota: func(id string) (*Note, error) {
			for _, n := range notas {
				if n.ID == id {
					return &n, nil
				}
			}
			return nil, nil
		},
		Topo: func(arquivada, fixada bool, exceto string) (string, error) {
			topo := ""
			for _, n := range notas {
				if n.DeletedAt == nil && n.Arquivada == arquivada && n.Fixada == fixada && n.ID != exceto && (topo == "" || n.Posicao < topo) {
					topo = n.Posicao
				}
			}
			return topo, nil
		},
		MarcadorVivo: func(id string) (bool, error) { return id == "01a17dd8-0000-7000-8000-000000000001", nil },
	}
}

const (
	idA = "01a17dd8-0000-7000-8000-00000000000a"
	idB = "01a17dd8-0000-7000-8000-00000000000b"
	idC = "01a17dd8-0000-7000-8000-00000000000c"
)

func TestPlanejarNota_EdicaoLevaOConteudoInteiroEAPosicaoSoAoMudarDeGrupo(t *testing.T) {
	a := notaDoCelular(idA, "Mercado", "- [ ] leite", "V", 3)
	corpo := "- [x] leite"
	p, err := PlanejarNota(PedidoDaNota{Op: NotaEditar, BaseVersion: 2, Corpo: &corpo, CorpoBase: &a.Corpo}, a, contextoDe(a), agoraDoCelular)
	if err != nil {
		t.Fatal(err)
	}
	got := payloadDe(t, p.Mutation)
	if got["titulo"] != "Mercado" || got["corpo"] != corpo || got["corpo_base"] != a.Corpo || p.Mutation.BaseVersion != 2 || got["posicao"] != nil {
		t.Fatalf("a edição = %v (base %d)", got, p.Mutation.BaseVersion)
	}
	if p.Enviado == nil || p.Enviado.Titulo != "Mercado" || p.Enviado.Corpo != corpo {
		t.Fatalf("o enviado = %+v", p.Enviado)
	}
	sim := true
	p, _ = PlanejarNota(PedidoDaNota{Op: NotaEditar, BaseVersion: 3, Fixada: &sim}, a, contextoDe(a, notaDoCelular(idB, "", "x", "M", 1)), agoraDoCelular)
	if pos, _ := payloadDe(t, p.Mutation)["posicao"].(string); pos == "" {
		t.Fatal("fixar não levou a nota ao topo das fixadas")
	}
	if _, err := PlanejarNota(PedidoDaNota{Op: NotaEditar, BaseVersion: 3}, a, contextoDe(a), agoraDoCelular); !errors.Is(err, ErrNothingToChange) {
		t.Fatalf("o PATCH vazio = %v", err)
	}
}

func TestPlanejarNota_MoverConfereAsVizinhas(t *testing.T) {
	a := notaDoCelular(idA, "A", "", "A", 1)
	b := notaDoCelular(idB, "B", "", "M", 1)
	c := notaDoCelular(idC, "C", "", "a", 1)
	ctx := contextoDe(a, b, c)
	p, err := PlanejarNota(PedidoDaNota{Op: NotaMover, Antes: idC}, a, ctx, agoraDoCelular)
	if err != nil || p.Nada {
		t.Fatalf("mover para o fim: %+v %v", p, err)
	}
	if pos := payloadDe(t, p.Mutation)["posicao"].(string); !(pos > c.Posicao) {
		t.Fatalf("a posição %q não é depois de %q", pos, c.Posicao)
	}
	if p, _ := PlanejarNota(PedidoDaNota{Op: NotaMover, Depois: idB}, a, ctx, agoraDoCelular); !p.Nada {
		t.Fatal("já estava antes de B e gravou")
	}
	fixada := c
	fixada.Fixada = true
	for nome, pedido := range map[string]PedidoDaNota{
		"sem vizinha":     {Op: NotaMover},
		"a própria":       {Op: NotaMover, Antes: idA},
		"fora de ordem":   {Op: NotaMover, Antes: idC, Depois: idB},
		"que não existe":  {Op: NotaMover, Antes: "01a17dd8-0000-7000-8000-0000000000ff"},
		"id que não vale": {Op: NotaMover, Antes: "x"},
	} {
		if _, err := PlanejarNota(pedido, a, ctx, agoraDoCelular); err == nil {
			t.Errorf("%s: aceitou", nome)
		}
	}
	if _, err := PlanejarNota(PedidoDaNota{Op: NotaMover, Antes: idC}, a, contextoDe(a, b, fixada), agoraDoCelular); !errors.Is(err, ErrNeighbors) {
		t.Fatalf("a vizinha de outro grupo = %v", err)
	}
}

func TestPlanejarParaNota_TituloAcrescentaSemTituloNasce(t *testing.T) {
	lista := notaDoCelular(idA, "Lista do sábado", "- [ ] Leite\n- [x] Pão", "V", 4)
	p, err := PlanejarParaNota([]Note{lista}, "lista do sábado: queijo, presunto e ovos", "d1", idB, "V", agoraDoCelular)
	if err != nil || p.Alvo == nil || p.Destino.NotaID != idA || p.Destino.Linhas != 3 || p.Destino.CorpoAntes != lista.Corpo || p.Mutation.BaseVersion != 4 {
		t.Fatalf("o acrescentar = %+v %v", p, err)
	}
	if corpo := payloadDe(t, p.Mutation)["corpo"]; corpo != "- [ ] Leite\n- [ ] Queijo\n- [ ] Presunto\n- [ ] Ovos\n- [x] Pão" {
		t.Fatalf("o corpo = %q", corpo)
	}
	p, err = PlanejarParaNota([]Note{lista}, "ligar para o contador", "d1", idB, "V", agoraDoCelular)
	if err != nil || p.Alvo != nil || !p.Destino.Nova || p.Destino.NotaID != idB {
		t.Fatalf("a nota nova = %+v %v", p, err)
	}
	nova := payloadDe(t, p.Mutation)
	if nova["corpo"] != "ligar para o contador" || nova["titulo"] != "" || nova["ditado_de_origem"] != "d1" || !(nova["posicao"].(string) < "V") {
		t.Fatalf("a nota nova = %v", nova)
	}
}

func TestDecidirParaNota_OCorpoQuePassaDoTetoViraNotaNova(t *testing.T) {
	cheia := notaDoCelular(idA, "Diário", strings.Repeat("x", MaxTextBytes-10), "V", 1)
	tr := Transcription{ID: "01a17dd8-0000-7000-8000-0000000000d1", FinalText: "diário: " + strings.Repeat("palavra ", 10)}
	plano, o, err := DecidirParaNota([]Note{cheia}, tr, "V", Actor{UserID: "u"}, agoraDoCelular)
	if err != nil || !plano.Destino.Nova || o.Write != WriteInsert || o.NextNote.Corpo != tr.FinalText {
		t.Fatalf("o corpo grande demais = %+v %+v %v", plano.Destino, o.Result, err)
	}
}

func TestPlanejarDesfazer(t *testing.T) {
	ditado := "01a17dd8-0000-7000-8000-0000000000d1"
	ligada := idA
	tr := Transcription{ID: ditado, Version: 1, VirouNota: &ligada}
	nova := notaDoCelular(idA, "", "texto", "V", 1)
	nova.DitadoDeOrigem = &ditado
	if p, err := PlanejarDesfazer(tr, &nova, PedidoDeDesfazer{}); err != nil || p.Nota == nil || p.Nota.Operation != OpDelete || !p.NotaApagada {
		t.Fatalf("a nota nova intacta = %+v %v", p, err)
	}
	nova.Version = 2
	if p, _ := PlanejarDesfazer(tr, &nova, PedidoDeDesfazer{}); p.Nota != nil || p.NotaApagada {
		t.Fatalf("a nota nova editada saiu: %+v", p)
	}
	lista := notaDoCelular(idA, "Lista", "- [ ] a\n- [ ] b", "V", 5)
	antes := "- [ ] a"
	p, _ := PlanejarDesfazer(tr, &lista, PedidoDeDesfazer{Versao: 5, CorpoAntes: &antes})
	if p.Nota == nil || payloadDe(t, *p.Nota)["corpo"] != antes || p.Nota.BaseVersion != 5 {
		t.Fatalf("o acrescentar desfeito = %+v", p)
	}
	if p, _ := PlanejarDesfazer(tr, &lista, PedidoDeDesfazer{Versao: 4, CorpoAntes: &antes}); p.Nota != nil {
		t.Fatal("a nota mexida depois do ditado voltou ao corpo de antes")
	}
	if p, _ := PlanejarDesfazer(tr, nil, PedidoDeDesfazer{}); p.Nota != nil || payloadDe(t, p.Ditado)["virou_nota"] != nil {
		t.Fatalf("sem a nota = %+v", p)
	}
	tr.VirouNota = nil
	if _, err := PlanejarDesfazer(tr, &lista, PedidoDeDesfazer{}); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("sem ligação = %v", err)
	}
}

func TestMarcadorNovo_ONomeEUnicoSemCaixaNemAcento(t *testing.T) {
	existentes := []Label{{ID: idA, Nome: "Mercado"}}
	if _, err := PlanejarMarcadorNovo(idB, "MÉRCADO", existentes, agoraDoCelular); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("o nome repetido = %v", err)
	}
	if _, err := PlanejarMarcadorNovo(idB, "  ", existentes, agoraDoCelular); !errors.Is(err, ErrInvalidLabelName) {
		t.Fatalf("o nome vazio = %v", err)
	}
	if _, nada, err := PlanejarRenomear(existentes[0], 1, "mercado", existentes); err != nil || nada {
		t.Fatalf("renomear a si mesmo só na caixa = %v %v", nada, err)
	}
}
