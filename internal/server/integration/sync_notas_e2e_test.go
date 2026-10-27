package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A ponta a ponta das notas e dos marcadores de nota (ADR-022, N7): dois PCs
// e o celular contra a nuvem de verdade (o palco é o de
// sync_harness_test.go). O que só se vê com os dois lados juntos: a nota e o
// marcador indo e voltando, o conflito com selo, a mescla da lista de marcar
// sem selo, a lápide do "apagar de vez" e o "virou nota" escondendo o ditado
// no celular.

func (pc *pcE2E) notas() *storage.SQLiteNotaRepository {
	return storage.NewSQLiteNotaRepository(pc.db)
}

func (pc *pcE2E) nota(id string) storage.Nota {
	pc.t.Helper()
	n, err := pc.notas().Get(context.Background(), pc.dono, id)
	if err != nil {
		pc.t.Fatalf("o PC não tem a nota %s: %v", id, err)
	}
	return n
}

func (pc *pcE2E) temNota(id string) bool {
	_, err := pc.notas().Get(context.Background(), pc.dono, id)
	return err == nil
}

func (pc *pcE2E) editarCorpo(id, corpo string) {
	pc.t.Helper()
	if _, err := pc.notas().Atualizar(context.Background(), pc.dono, id, pc.nota(id).Version, storage.MudancaDaNota{Corpo: &corpo}); err != nil {
		pc.t.Fatal(err)
	}
}

// duasRodadas: a primeira de um PC novo vê a capacidade das notas e lê de
// novo do 0; a segunda já é a de sempre.
func (pc *pcE2E) duasRodadas() {
	pc.t.Helper()
	pc.rodadaOK()
	pc.rodadaOK()
}

func TestSyncNotasE2E_NotaEMarcadorChegamAoOutroPC(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	mk, err := pc1.notas().CriarMarcador(ctx, pc1.dono, "Mercado")
	if err != nil {
		t.Fatal(err)
	}
	nota, err := pc1.notas().Criar(ctx, storage.NovaNota{UserID: pc1.dono, Titulo: "Compras", Corpo: "- [ ] leite\n- [ ] pão",
		Cor: "verde", Marcadores: []string{mk.ID}})
	if err != nil {
		t.Fatal(err)
	}
	pc1.duasRodadas()
	pc2.duasRodadas()

	got := pc2.nota(nota.ID)
	if got.Titulo != "Compras" || got.Corpo != "- [ ] leite\n- [ ] pão" || got.Cor == nil || *got.Cor != "verde" ||
		got.Posicao != nota.Posicao || len(got.Marcadores) != 1 || got.Marcadores[0] != mk.ID {
		t.Fatalf("no PC2: %+v", got)
	}
	ls, err := pc2.notas().ListarMarcadores(ctx, pc2.dono)
	if err != nil || len(ls) != 1 || ls[0].Nome != "Mercado" {
		t.Errorf("os marcadores no PC2: %+v %v", ls, err)
	}

	// O celular lê as notas e os marcadores (as rotas novas, só leitura).
	st, b := n.celular(http.MethodGet, "/api/v1/notes", nil)
	if st != http.StatusOK || !strings.Contains(string(b), nota.ID) {
		t.Errorf("GET /notes = %d %s", st, b)
	}
	st, b = n.celular(http.MethodGet, "/api/v1/labels", nil)
	if st != http.StatusOK || !strings.Contains(string(b), "Mercado") {
		t.Errorf("GET /labels = %d %s", st, b)
	}

	// Ninguém ecoa.
	n.porteiro.zerar()
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "push") {
		t.Errorf("houve push sem mudança: %v", pedidos)
	}

	// Mover no PC2 (o último que escreve vence, sem selo) chega ao PC1.
	outra, err := pc2.notas().Criar(ctx, storage.NovaNota{UserID: pc2.dono, Corpo: "outra"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc2.notas().Mover(ctx, pc2.dono, nota.ID, "", outra.ID); err != nil {
		t.Fatal(err)
	}
	pc2.rodadaOK()
	pc1.rodadaOK()
	if p1, p2 := pc1.nota(nota.ID).Posicao, pc2.nota(nota.ID).Posicao; p1 != p2 || pc1.nota(nota.ID).Conflito != nil {
		t.Errorf("a posição: PC1 %q, PC2 %q", p1, p2)
	}
}

func TestSyncNotasE2E_ConflitoComSeloEEscolha(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	nota, err := pc1.notas().Criar(ctx, storage.NovaNota{UserID: pc1.dono, Corpo: "reunião às 10"})
	if err != nil {
		t.Fatal(err)
	}
	pc1.duasRodadas()
	pc2.duasRodadas()

	pc1.editarCorpo(nota.ID, "reunião às 11")
	pc2.editarCorpo(nota.ID, "reunião às 9")
	pc1.rodadaOK() // chegou primeiro: vence
	pc2.rodadaOK() // perde: o texto dele vai para o selo

	l2 := pc2.nota(nota.ID)
	if l2.Corpo != "reunião às 11" || l2.Conflito == nil || l2.Conflito.Corpo != "reunião às 9" {
		t.Fatalf("no PC2 depois do conflito: corpo %q, selo %+v", l2.Corpo, l2.Conflito)
	}
	pc1.rodadaOK()
	if c := pc1.nota(nota.ID).Conflito; c == nil || c.Corpo != "reunião às 9" {
		t.Errorf("o selo não chegou ao PC1: %+v", c)
	}

	// A pessoa escolhe a outra versão no PC2: vale em todo lugar.
	if _, err := pc2.notas().ResolverConflitoDaNota(ctx, pc2.dono, nota.ID, l2.Version, storage.EscolhaTrocar); err != nil {
		t.Fatal(err)
	}
	pc2.rodadaOK()
	pc1.rodadaOK()
	pc2.rodadaOK()
	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		if l := pc.nota(nota.ID); l.Corpo != "reunião às 9" || l.Conflito != nil {
			t.Errorf("%s depois da escolha: corpo %q, selo %+v", nome, l.Corpo, l.Conflito)
		}
	}
}

func TestSyncNotasE2E_ListaDeMarcarMesclaSemSelo(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	nota, err := pc1.notas().Criar(ctx, storage.NovaNota{UserID: pc1.dono, Titulo: "Mercado", Corpo: "- [ ] leite\n- [ ] pão\n- [ ] café"})
	if err != nil {
		t.Fatal(err)
	}
	pc1.duasRodadas()
	pc2.duasRodadas()

	// Cada lado marca um item diferente (o marcado desce para o fim).
	pc1.editarCorpo(nota.ID, "- [ ] pão\n- [ ] café\n- [x] leite")
	pc2.editarCorpo(nota.ID, "- [ ] leite\n- [ ] café\n- [x] pão")
	pc1.rodadaOK()
	pc2.rodadaOK()
	pc1.rodadaOK()
	pc2.rodadaOK()

	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		l := pc.nota(nota.ID)
		if l.Conflito != nil {
			t.Errorf("%s ficou com selo: %+v", nome, l.Conflito)
		}
		if !strings.Contains(l.Corpo, "- [x] leite") || !strings.Contains(l.Corpo, "- [x] pão") || !strings.Contains(l.Corpo, "- [ ] café") {
			t.Errorf("%s: o corpo mesclado é %q", nome, l.Corpo)
		}
	}
	if a, b := pc1.nota(nota.ID).Corpo, pc2.nota(nota.ID).Corpo; a != b {
		t.Errorf("os dois PCs divergem: %q e %q", a, b)
	}

	// Texto diferente não mescla: vira selo.
	pc1.editarCorpo(nota.ID, pc1.nota(nota.ID).Corpo+"\n- [ ] açúcar")
	pc2.editarCorpo(nota.ID, strings.Replace(pc2.nota(nota.ID).Corpo, "café", "café em pó", 1))
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pc2.nota(nota.ID).Conflito == nil {
		t.Errorf("texto diferente nos dois lados mesclou sem selo: %q", pc2.nota(nota.ID).Corpo)
	}
}

func TestSyncNotasE2E_ApagarDeVezDeixaLapideEApagaNoOutroPC(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	nota, err := pc1.notas().Criar(ctx, storage.NovaNota{UserID: pc1.dono, Corpo: "some daqui"})
	if err != nil {
		t.Fatal(err)
	}
	mk, err := pc1.notas().CriarMarcador(ctx, pc1.dono, "Velho")
	if err != nil {
		t.Fatal(err)
	}
	pc1.duasRodadas()
	pc2.duasRodadas()
	if !pc2.temNota(nota.ID) {
		t.Fatal("a nota não chegou ao PC2")
	}

	if err := pc1.notas().SoftDelete(ctx, pc1.dono, nota.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := pc1.notas().ApagarDeVez(ctx, pc1.dono, nota.ID); err != nil {
		t.Fatal(err)
	}
	if err := pc1.notas().ApagarMarcador(ctx, pc1.dono, mk.ID); err != nil {
		t.Fatal(err)
	}
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pc2.temNota(nota.ID) {
		t.Errorf("a nota apagada de vez continua no PC2")
	}
	if ls, _ := pc2.notas().ListarMarcadores(ctx, pc2.dono); len(ls) != 0 {
		t.Errorf("o marcador apagado continua no PC2: %+v", ls)
	}
	st, b := n.celular(http.MethodGet, "/api/v1/notes?vista=lixeira", nil)
	if st != http.StatusOK || strings.Contains(string(b), nota.ID) {
		t.Errorf("a nota apagada de vez continua na nuvem: %d %s", st, b)
	}
	// A fila do PC1 esvaziou (a lápide subiu e terminou).
	if ms, _ := pc1.repo.Fila(pc1.dono).Marcadores(ctx); len(ms) != 0 {
		t.Errorf("a fila do PC1: %+v", ms)
	}
}

func TestSyncNotasE2E_VirarNotaEscondeODitadoNoCelular(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := pc1.ditar("comprar ração para o gato")
	pc1.duasRodadas()
	pc2.duasRodadas()
	if !strings.Contains(listaDoCelular(n), id) {
		t.Fatal("o ditado não está no histórico do celular")
	}

	nota, err := pc1.notas().VirarNota(ctx, pc1.dono, id)
	if err != nil {
		t.Fatal(err)
	}
	pc1.rodadaOK()
	if strings.Contains(listaDoCelular(n), id) {
		t.Errorf("o ditado que virou nota continua no histórico do celular")
	}
	// O PC2 recebe a nota e o ditado escondido.
	pc2.rodadaOK()
	if l := pc2.linha(id); l.VirouNota == nil || *l.VirouNota != nota.ID {
		t.Errorf("o virou_nota no PC2: %v", l.VirouNota)
	}
	if !pc2.temNota(nota.ID) {
		t.Errorf("a nota não chegou ao PC2")
	}

	// O Desfazer devolve o ditado ao histórico do celular e apaga a nota.
	if _, err := pc1.notas().DesfazerVirarNota(ctx, pc1.dono, nota.ID); err != nil {
		t.Fatal(err)
	}
	pc1.rodadaOK()
	if !strings.Contains(listaDoCelular(n), id) {
		t.Errorf("o Desfazer não devolveu o ditado ao celular")
	}
	pc2.rodadaOK()
	if pc2.temNota(nota.ID) {
		t.Errorf("a nota do Desfazer continua no PC2")
	}
	if l := pc2.linha(id); l.VirouNota != nil {
		t.Errorf("o virou_nota no PC2 depois do Desfazer: %v", *l.VirouNota)
	}
}

func listaDoCelular(n *nuvemE2E) string {
	n.t.Helper()
	st, b := n.celular(http.MethodGet, "/api/v1/transcriptions", nil)
	if st != http.StatusOK {
		n.t.Fatalf("celular lista = %d %s", st, b)
	}
	var p struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(b, &p)
	var ids []string
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}
