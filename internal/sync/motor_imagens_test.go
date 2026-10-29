package syncengine

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O motor com as imagens das notas (N10b), contra o SQLite de verdade, a pasta
// das imagens e uma nuvem de roteiro: a imagem espera a nota subir, a rede que
// cai no PUT só para a fase (a rodada termina, e a imagem sobe na seguinte), o
// 401 derruba a rodada, e a imagem que vem pelo pull baixa conferida.

// nuvemComImagens é a nuvem de roteiro com o lado das imagens.
type nuvemComImagens struct {
	*nuvemDeRoteiro
	mu       sync.Mutex
	subidas  []string
	baixadas []string
	subir    func(id string, sha string) (RespostaDaImagem, error)
	baixar   func(id string) ([]byte, RespostaDaImagem, error)
}

func (n *nuvemComImagens) SubirImagem(_ context.Context, _, id string, _ []byte, sha string, _ time.Time) (RespostaDaImagem, error) {
	n.mu.Lock()
	n.subidas = append(n.subidas, id)
	f := n.subir
	n.mu.Unlock()
	return f(id, sha)
}

func (n *nuvemComImagens) BaixarImagem(_ context.Context, _, id string) ([]byte, RespostaDaImagem, error) {
	n.mu.Lock()
	n.baixadas = append(n.baixadas, id)
	f := n.baixar
	n.mu.Unlock()
	return f(id)
}

func (n *nuvemComImagens) TirarImagem(context.Context, string, string) (RespostaDaImagem, error) {
	return RespostaDaImagem{Status: http.StatusNoContent}, nil
}

// comNotasEImagens responde a página dizendo que a nuvem sabe de notas e de
// imagens, com as mudanças dadas.
func comNotasEImagens(epoch string, mudancas func(after int64) []Change) func(int64) (ChangePage, error) {
	return func(after int64) (ChangePage, error) {
		p := ChangePage{LastSeq: after, Epoch: epoch, Entidades: []string{"transcription", "learned", "note", "label", "note_image"}}
		if mudancas != nil {
			p.Changes = mudancas(after)
			for _, c := range p.Changes {
				if c.Seq > p.LastSeq {
					p.LastSeq = c.Seq
				}
			}
		}
		return p, nil
	}
}

func jpegDoMotor(t *testing.T, tom uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{R: tom, G: uint8(x * 9), B: uint8(y * 11), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// rejeitaNotas recusa as mutações de nota (a nota fica sem a versão da nuvem) e
// aplica o resto.
func rejeitaNotas(epoch string) func([]Mutation) (PushResult, error) {
	return func(muts []Mutation) (PushResult, error) {
		r := PushResult{Epoch: epoch}
		for _, m := range muts {
			if m.EntityType == storage.EntidadeNota {
				r.Results = append(r.Results, MutationResult{MutationID: m.MutationID, Status: StatusRejected, Code: "campo_invalido"})
				continue
			}
			r.Results = append(r.Results, MutationResult{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 1})
		}
		return r, nil
	}
}

func TestMotor_AImagemEsperaANotaEARedeQueCaiSoParaAFase(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	notas := notasDoPC(p)
	notas.UsarPastaDasImagens(filepath.Join(t.TempDir(), "notas-imagens"))
	n, err := notas.Criar(ctx, storage.NovaNota{UserID: donoDoMotor, Titulo: "Recibos"})
	if err != nil {
		t.Fatal(err)
	}
	im, err := notas.AnexarImagem(ctx, donoDoMotor, n.ID, jpegDoMotor(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	nuvem := &nuvemComImagens{nuvemDeRoteiro: &nuvemDeRoteiro{push: rejeitaNotas("e1"), pull: comNotasEImagens("e1", nil)}}
	nuvem.subir = func(string, string) (RespostaDaImagem, error) {
		return RespostaDaImagem{Status: http.StatusCreated}, nil
	}
	m := novoMotor(p, nuvem)
	m.Imagens = notas

	// A nota recusada fica sem a versão da nuvem: a imagem espera.
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if len(nuvem.subidas) != 0 {
		t.Fatalf("a imagem subiu antes da nota: %v", nuvem.subidas)
	}

	// A nota sobe; na mesma rodada, a imagem. A rede cai no PUT: a rodada
	// termina sem erro, e a imagem fica na fila.
	nuvem.mu.Lock()
	nuvem.nuvemDeRoteiro.mu.Lock()
	nuvem.nuvemDeRoteiro.push = aplicaTudo("e1")
	nuvem.nuvemDeRoteiro.mu.Unlock()
	nuvem.subir = func(_ string, sha string) (RespostaDaImagem, error) {
		if sha != im.SHA256 {
			t.Errorf("o PUT levou o SHA-256 %q, quero %q", sha, im.SHA256)
		}
		return RespostaDaImagem{}, transitorio(CodigoRede)
	}
	nuvem.mu.Unlock()
	if _, err := notas.Atualizar(ctx, donoDoMotor, n.ID, 1, storage.MudancaDaNota{Titulo: strPtr("Recibos!")}); err != nil {
		t.Fatal(err)
	}
	rel, err := m.Rodada(ctx)
	if err != nil {
		t.Fatalf("a rede que caiu no PUT derrubou a rodada: %v", err)
	}
	if len(nuvem.subidas) != 1 || rel.Imagens[TipoImagemAdiada] != 1 {
		t.Fatalf("as subidas: %v; o relatório: %v", nuvem.subidas, rel.Imagens)
	}
	if !temMarcadorDeImagem(t, p, im.ID) {
		t.Fatal("a imagem saiu da fila sem subir")
	}

	// A seguinte sobe.
	nuvem.mu.Lock()
	nuvem.subir = func(string, string) (RespostaDaImagem, error) {
		return RespostaDaImagem{Status: http.StatusCreated}, nil
	}
	nuvem.mu.Unlock()
	if rel, err = m.Rodada(ctx); err != nil || rel.Imagens[TipoImagemSubiu] != 1 || temMarcadorDeImagem(t, p, im.ID) {
		t.Fatalf("a rodada seguinte: %v, %v", rel.Imagens, err)
	}

	// O 401 derruba a rodada (o token caiu).
	outra, err := notas.AnexarImagem(ctx, donoDoMotor, n.ID, jpegDoMotor(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	nuvem.mu.Lock()
	nuvem.subir = func(string, string) (RespostaDaImagem, error) {
		return RespostaDaImagem{Status: http.StatusUnauthorized}, ErrNaoAutorizado
	}
	nuvem.mu.Unlock()
	if _, err := m.Rodada(ctx); !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("o 401 no PUT: %v", err)
	}
	if !temMarcadorDeImagem(t, p, outra.ID) {
		t.Error("o 401 tirou a imagem da fila")
	}
}

func TestMotor_AImagemDoPullBaixaConferida(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	notas := notasDoPC(p)
	notas.UsarPastaDasImagens(filepath.Join(t.TempDir(), "notas-imagens"))
	n, err := notas.Criar(ctx, storage.NovaNota{UserID: donoDoMotor, Titulo: "Do celular"})
	if err != nil {
		t.Fatal(err)
	}
	foto := jpegDoMotor(t, 7)
	limpa := conferirOuFalhar(t, foto)
	id := "0199a000-0000-7000-8000-00000000e001"
	meta := &ImagemDaNuvem{ID: id, NoteID: n.ID, Largura: 16, Altura: 12, Bytes: int64(len(limpa.Bytes)), SHA256: limpa.SHA256,
		CreatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
	nuvem := &nuvemComImagens{nuvemDeRoteiro: &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: comNotasEImagens("e1", func(after int64) []Change {
		if after >= 5 {
			return nil
		}
		return []Change{{Seq: 5, EntityType: storage.EntidadeImagemDaNota, EntityID: id, Operation: OpUpsert, EntityVersion: 1, PayloadImagem: meta}}
	})}}
	// A primeira resposta é a de um 500; a segunda, os bytes.
	vezes := 0
	nuvem.baixar = func(string) ([]byte, RespostaDaImagem, error) {
		vezes++
		if vezes == 1 {
			return nil, RespostaDaImagem{Status: http.StatusInternalServerError}, nil
		}
		return limpa.Bytes, RespostaDaImagem{Status: http.StatusOK}, nil
	}
	m := novoMotor(p, nuvem)
	m.Imagens = notas
	agora := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	m.Agora = func() time.Time { return agora }

	rel, err := m.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Imagens[TipoImagemRecebida] != 1 || rel.Imagens[TipoImagemEsperandoBaixa] != 1 {
		t.Fatalf("a primeira rodada: %v", rel.Imagens)
	}
	if got, _ := notas.Get(ctx, donoDoMotor, n.ID); len(got.Imagens) != 1 || got.Imagens[0].Estado != storage.EstadoACaminho {
		t.Fatalf("a imagem a caminho: %+v", got.Imagens)
	}
	// Antes da vez (1 min depois da falha), nada; depois, baixa.
	if rel, err = m.Rodada(ctx); err != nil || len(nuvem.baixadas) != 1 {
		t.Fatalf("baixou antes da vez: %v, %v", nuvem.baixadas, err)
	}
	agora = agora.Add(2 * time.Minute)
	if rel, err = m.Rodada(ctx); err != nil || rel.Imagens[TipoImagemBaixada] != 1 || rel.AplicouDaNuvem() == 0 {
		t.Fatalf("a rodada da vez: %v, %v", rel.Imagens, err)
	}
	if got, _ := notas.Get(ctx, donoDoMotor, n.ID); len(got.Imagens) != 1 || got.Imagens[0].Estado != "" {
		t.Fatalf("a imagem baixada: %+v", got.Imagens)
	}
	if _, b, err := notas.LerImagem(ctx, donoDoMotor, n.ID, id); err != nil || !bytes.Equal(b, limpa.Bytes) {
		t.Fatalf("os bytes baixados: %v", err)
	}
}

func temMarcadorDeImagem(t *testing.T, p pcDeTeste, id string) bool {
	t.Helper()
	var n int
	if err := p.db.QueryRow(`SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'note_image' AND entity_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func conferirOuFalhar(t *testing.T, b []byte) imagemdanota.Limpa {
	t.Helper()
	l, err := imagemdanota.Conferir(b)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
