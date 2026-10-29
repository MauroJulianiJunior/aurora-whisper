package cloudstore

import (
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
)

// As regras das imagens das notas (N10b; imagens.go), puras, em tabela: o
// reenvio idempotente, o id com outros bytes, a lápide que não deixa
// ressuscitar, a nota que não existe (com e sem lápide), a da lixeira, o máximo
// por nota, o teto do total (as da lixeira contam) e a hora do anexo.

func TestDecidirAnexo(t *testing.T) {
	agora := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ontem := agora.Add(-24 * time.Hour)
	nota := &Note{ID: "n1"}
	naLixeira := &Note{ID: "n1", DeletedAt: &ontem}
	limpa := imagemdanota.Limpa{Bytes: make([]byte, 1000), Largura: 30, Altura: 20, SHA256: "aa"}
	viva := &NoteImage{ID: "i1", NoteID: "n1", SHA256: "aa", Bytes: 1000, CreatedAt: ontem, LastSeq: 7}
	morta := &NoteImage{ID: "i1", NoteID: "n1", SHA256: "aa", RemovedAt: &ontem}
	deOutraNota := &NoteImage{ID: "i1", NoteID: "n2", SHA256: "aa"}
	outrosBytes := &NoteImage{ID: "i1", NoteID: "n1", SHA256: "bb"}
	pedido := ImagemNova{ID: "i1", NoteID: "n1", Limpa: limpa, CriadaEm: ontem}

	casos := []struct {
		nome   string
		lugar  LugarDaImagem
		pedido ImagemNova
		quer   error
		grava  bool
	}{
		{"a nova", LugarDaImagem{Nota: nota}, pedido, nil, true},
		{"o reenvio do mesmo id com os mesmos bytes", LugarDaImagem{Nota: nota, Atual: viva}, pedido, nil, false},
		{"o reenvio com a nota já na lixeira", LugarDaImagem{Nota: naLixeira, Atual: viva}, pedido, nil, false},
		{"o reenvio com a nota no teto", LugarDaImagem{Nota: nota, Atual: viva, NaNota: 10, Total: imagemdanota.TetoDoTotal}, pedido, nil, false},
		{"o id com lápide", LugarDaImagem{Nota: nota, Atual: morta}, pedido, ErrImageGone, false},
		{"o id com lápide e a nota apagada de vez", LugarDaImagem{NotaComLapide: true, Atual: morta}, pedido, ErrImageGone, false},
		{"o id de outra nota", LugarDaImagem{Nota: nota, Atual: deOutraNota}, pedido, ErrImageConflict, false},
		{"o id com outros bytes", LugarDaImagem{Nota: nota, Atual: outrosBytes}, pedido, ErrImageConflict, false},
		{"a nota que não existe", LugarDaImagem{}, pedido, ErrNoteNotFound, false},
		{"a nota apagada de vez", LugarDaImagem{NotaComLapide: true}, pedido, ErrImageGone, false},
		{"a nota na lixeira", LugarDaImagem{Nota: naLixeira}, pedido, ErrNoteInTrash, false},
		{"a nota com 9", LugarDaImagem{Nota: nota, NaNota: 9}, pedido, nil, true},
		{"a nota com 10", LugarDaImagem{Nota: nota, NaNota: 10}, pedido, ErrTooManyImages, false},
		{"o total que fica no teto", LugarDaImagem{Nota: nota, Total: imagemdanota.TetoDoTotal - 1000}, pedido, nil, true},
		{"o total que passa do teto por um byte", LugarDaImagem{Nota: nota, Total: imagemdanota.TetoDoTotal - 999}, pedido, ErrImagesFull, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			im, grava, err := DecidirAnexo(c.lugar, "u1", c.pedido, agora)
			if !errors.Is(err, c.quer) || (c.quer == nil) != (err == nil) {
				t.Fatalf("o erro = %v, quero %v", err, c.quer)
			}
			if grava != c.grava {
				t.Fatalf("grava = %v, quero %v", grava, c.grava)
			}
			switch {
			case err != nil:
			case grava:
				quer := NoteImage{ID: "i1", UserID: "u1", NoteID: "n1", Largura: 30, Altura: 20, Bytes: 1000, SHA256: "aa", CreatedAt: ontem}
				if im.ID != quer.ID || im.UserID != quer.UserID || im.NoteID != quer.NoteID || im.Largura != 30 || im.Altura != 20 ||
					im.Bytes != 1000 || im.SHA256 != "aa" || !im.CreatedAt.Equal(ontem) || im.RemovedAt != nil {
					t.Errorf("a imagem nova = %+v, quero %+v", im, quer)
				}
			default:
				if im != *c.lugar.Atual {
					t.Errorf("o reenvio devolveu %+v, quero o que está gravado", im)
				}
			}
		})
	}
}

func TestDecidirAnexo_AHoraDoAnexo(t *testing.T) {
	agora := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		nome   string
		criada time.Time
		quer   time.Time
	}{
		{"sem a hora (o celular)", time.Time{}, agora},
		{"a do PC", agora.Add(-48 * time.Hour), agora.Add(-48 * time.Hour)},
		{"a do PC dentro da folga", agora.Add(FutureTolerance), agora.Add(FutureTolerance)},
		{"a do PC além da folga", agora.Add(FutureTolerance + time.Second), agora},
		{"truncada em microssegundos", agora.Add(-time.Nanosecond * 1500), agora.Add(-2 * time.Microsecond)},
	} {
		im, _, err := DecidirAnexo(LugarDaImagem{Nota: &Note{ID: "n1"}}, "u1",
			ImagemNova{ID: "i1", NoteID: "n1", Limpa: imagemdanota.Limpa{Bytes: []byte{1}, SHA256: "aa"}, CriadaEm: c.criada}, agora)
		if err != nil || !im.CreatedAt.Equal(c.quer) {
			t.Errorf("%s: a hora = %v (%v), quero %v", c.nome, im.CreatedAt, err, c.quer)
		}
	}
}

func TestDecidirTirar(t *testing.T) {
	ontem := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	nota := &Note{ID: "n1"}
	naLixeira := &Note{ID: "n1", DeletedAt: &ontem}
	viva := &NoteImage{ID: "i1", NoteID: "n1"}
	morta := &NoteImage{ID: "i1", NoteID: "n1", RemovedAt: &ontem}
	for _, c := range []struct {
		nome  string
		lugar LugarDaImagem
		quer  int
		err   error
	}{
		{"a viva", LugarDaImagem{Nota: nota, Atual: viva}, TirarLapide, nil},
		{"a viva na nota da lixeira", LugarDaImagem{Nota: naLixeira, Atual: viva}, TirarLapide, nil},
		{"tirar de novo", LugarDaImagem{Nota: nota, Atual: morta}, TirarNada, nil},
		{"o id que a nuvem nunca teve", LugarDaImagem{Nota: nota}, TirarLapideNova, nil},
		{"o id que nunca chegou, na lixeira", LugarDaImagem{Nota: naLixeira}, TirarLapideNova, nil},
		{"a nota apagada de vez", LugarDaImagem{NotaComLapide: true}, TirarNada, nil},
		{"a nota que não existe", LugarDaImagem{}, TirarNada, ErrNoteNotFound},
		{"o id de outra nota", LugarDaImagem{Nota: nota, Atual: &NoteImage{ID: "i1", NoteID: "n2"}}, TirarNada, ErrImageNotFound},
	} {
		got, err := DecidirTirar(c.lugar, "n1")
		if got != c.quer || !errors.Is(err, c.err) || (c.err == nil) != (err == nil) {
			t.Errorf("%s: %d, %v; quero %d, %v", c.nome, got, err, c.quer, c.err)
		}
	}
}

// O lugar que o Store em memória lê conta, no teto, as imagens vivas de toda a
// conta (as das notas da lixeira inclusive) e não as lápides; e, na nota, só as
// vivas dela.
func TestMemoria_OLugarDaImagemContaALixeira(t *testing.T) {
	ontem := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	u := newMemUser()
	u.notes["n1"] = Note{ID: "n1"}
	u.notes["n2"] = Note{ID: "n2", DeletedAt: &ontem}
	u.images["a"] = NoteImage{ID: "a", NoteID: "n1", Bytes: 100}
	u.images["b"] = NoteImage{ID: "b", NoteID: "n2", Bytes: 200}
	u.images["c"] = NoteImage{ID: "c", NoteID: "n1", Bytes: 400, RemovedAt: &ontem}
	l := u.lugarDaImagem("n1", "novo")
	if l.Nota == nil || l.NaNota != 1 || l.Total != 300 || l.Atual != nil || l.NotaComLapide {
		t.Fatalf("o lugar = %+v, quero 1 na nota e 300 bytes (a da lixeira conta, a lápide não)", l)
	}
	if l := u.lugarDaImagem("n2", "c"); l.NaNota != 1 || l.Atual == nil || l.Atual.Viva() {
		t.Fatalf("o lugar da lápide = %+v", l)
	}
	// A nota que sai de vez leva as imagens dela, as lápides também.
	u.tirarImagensDaNota("n1")
	if _, ok := u.images["a"]; ok {
		t.Error("a imagem ficou depois de a nota sair")
	}
	if _, ok := u.images["c"]; ok {
		t.Error("a lápide ficou depois de a nota sair")
	}
	if _, ok := u.images["b"]; !ok {
		t.Error("a imagem da outra nota saiu")
	}
}
