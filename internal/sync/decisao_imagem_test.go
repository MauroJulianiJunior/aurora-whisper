package syncengine

import (
	"net/http"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões das imagens das notas (N10b), em tabela: o pull, o envio e o
// download. Sem banco nem rede.

const (
	idDaImagem   = "0199a000-0000-7000-8000-00000000a001"
	notaDaImagem = "0199a000-0000-7000-8000-00000000b001"
	shaDaImagem  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func metaDeTeste() *ImagemDaNuvem {
	return &ImagemDaNuvem{ID: idDaImagem, NoteID: notaDaImagem, Largura: 30, Altura: 20, Bytes: 100, SHA256: shaDaImagem,
		CreatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
}

func TestDecidirMudancaDaImagem(t *testing.T) {
	aqui := &storage.LinhaDaImagemNoSync{ImagemDaNota: storage.ImagemDaNota{ID: idDaImagem, NotaID: notaDaImagem, SHA256: shaDaImagem}}
	outra := &storage.LinhaDaImagemNoSync{ImagemDaNota: storage.ImagemDaNota{ID: idDaImagem, NotaID: notaDaImagem, SHA256: "ff" + shaDaImagem[2:]}}
	criar := &storage.Marcador{EntityType: storage.EntidadeImagemDaNota, EntityID: idDaImagem}
	lapide := &storage.Marcador{EntityType: storage.EntidadeImagemDaNota, EntityID: idDaImagem, Campos: storage.CampoApagado}
	maiusculas := metaDeTeste()
	maiusculas.SHA256 = "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
	semSHA := metaDeTeste()
	semSHA.SHA256 = "x"
	semTamanho := metaDeTeste()
	semTamanho.Bytes = 0
	outroID := metaDeTeste()
	outroID.ID = notaDaImagem

	casos := []struct {
		nome   string
		e      storage.EstadoDaImagem
		c      ImagemDoPull
		tipo   string
		acao   storage.AcaoDaImagem
		insere bool
	}{
		{"a nova, da nota que está aqui", storage.EstadoDaImagem{NotaAqui: true}, ImagemDoPull{Meta: metaDeTeste()}, TipoImagemRecebida, storage.AcaoDaImagem{}, true},
		{"o SHA-256 em maiúsculas vira minúsculas", storage.EstadoDaImagem{NotaAqui: true}, ImagemDoPull{Meta: maiusculas}, TipoImagemRecebida, storage.AcaoDaImagem{}, true},
		{"a nova, de uma nota que não está aqui", storage.EstadoDaImagem{}, ImagemDoPull{Meta: metaDeTeste()}, TipoImagemNaoAplicada, storage.AcaoDaImagem{}, false},
		{"o eco (o PUT cuja resposta se perdeu)", storage.EstadoDaImagem{NotaAqui: true, Linha: aqui, Marcador: criar}, ImagemDoPull{Meta: metaDeTeste()},
			TipoImagemEco, storage.AcaoDaImagem{NaNuvem: true, TirarCriar: true}, false},
		{"o eco sem marcador", storage.EstadoDaImagem{NotaAqui: true, Linha: aqui}, ImagemDoPull{Meta: metaDeTeste()},
			TipoImagemEco, storage.AcaoDaImagem{NaNuvem: true}, false},
		{"o mesmo id com outros bytes: fica a daqui", storage.EstadoDaImagem{NotaAqui: true, Linha: outra}, ImagemDoPull{Meta: metaDeTeste()},
			TipoImagemConflito, storage.AcaoDaImagem{}, false},
		{"tirada aqui: o DELETE vai subir", storage.EstadoDaImagem{NotaAqui: true, Marcador: lapide}, ImagemDoPull{Meta: metaDeTeste()},
			TipoImagemPulada, storage.AcaoDaImagem{}, false},
		{"a lápide da imagem daqui", storage.EstadoDaImagem{Linha: aqui, Marcador: criar}, ImagemDoPull{Tombstone: true},
			TipoImagemApagada, storage.AcaoDaImagem{ApagarLinha: true, TirarMarcador: true}, false},
		{"a lápide da imagem que aqui já saiu (a lápide local termina)", storage.EstadoDaImagem{Marcador: lapide}, ImagemDoPull{Tombstone: true},
			TipoImagemEco, storage.AcaoDaImagem{TirarMarcador: true}, false},
		{"a lápide de uma imagem que nunca esteve aqui", storage.EstadoDaImagem{}, ImagemDoPull{Tombstone: true},
			TipoImagemEco, storage.AcaoDaImagem{}, false},
		{"o upsert sem os metadados", storage.EstadoDaImagem{NotaAqui: true}, ImagemDoPull{}, TipoImagemNaoAplicada, storage.AcaoDaImagem{}, false},
		{"o SHA-256 que não é hexadecimal", storage.EstadoDaImagem{NotaAqui: true}, ImagemDoPull{Meta: semSHA}, TipoImagemNaoAplicada, storage.AcaoDaImagem{}, false},
		{"o tamanho zero", storage.EstadoDaImagem{NotaAqui: true}, ImagemDoPull{Meta: semTamanho}, TipoImagemNaoAplicada, storage.AcaoDaImagem{}, false},
		{"o id dos metadados não é o da mudança", storage.EstadoDaImagem{NotaAqui: true}, ImagemDoPull{Meta: outroID}, TipoImagemNaoAplicada, storage.AcaoDaImagem{}, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			c.e.ID = idDaImagem
			d := DecidirMudancaDaImagem(c.e, c.c)
			if d.Tipo != c.tipo {
				t.Errorf("o tipo = %q, quero %q", d.Tipo, c.tipo)
			}
			if (d.Acao.Inserir != nil) != c.insere {
				t.Fatalf("insere = %v, quero %v", d.Acao.Inserir != nil, c.insere)
			}
			if c.insere {
				im := d.Acao.Inserir
				if im.ID != idDaImagem || im.NotaID != notaDaImagem || im.Largura != 30 || im.Bytes != 100 || im.SHA256 != shaDaImagem ||
					!im.CreatedAt.Equal(metaDeTeste().CreatedAt) {
					t.Errorf("a imagem a inserir = %+v", im)
				}
				d.Acao.Inserir = nil
			}
			if d.Acao != c.acao {
				t.Errorf("a ação = %+v, quero %+v", d.Acao, c.acao)
			}
		})
	}
	if d := DecidirMudancaDaImagem(storage.EstadoDaImagem{ID: "não é uuid", NotaAqui: true}, ImagemDoPull{Meta: metaDeTeste()}); d.Tipo != TipoImagemNaoAplicada {
		t.Errorf("o id que não é UUID: %+v", d)
	}
}

func TestDecidirEnvioDaImagem(t *testing.T) {
	criar := storage.ItemDeImagemNaFila{ID: idDaImagem, NotaID: notaDaImagem}
	lapide := storage.ItemDeImagemNaFila{ID: idDaImagem, NotaID: notaDaImagem, Lapide: true}
	subiu := storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Subiu: true}
	for _, c := range []struct {
		nome string
		it   storage.ItemDeImagemNaFila
		r    RespostaDaImagem
		tipo string
		acao storage.AcaoDaImagemNoEnvio
	}{
		{"201", criar, RespostaDaImagem{Status: http.StatusCreated}, TipoImagemSubiu, subiu},
		{"200 (a nuvem já tinha)", criar, RespostaDaImagem{Status: http.StatusOK}, TipoImagemSubiu, subiu},
		{"410 (tirada na nuvem, ou a nota apagada de vez)", criar, RespostaDaImagem{Status: http.StatusGone, Codigo: "IMAGE_GONE"},
			TipoImagemSumiu, storage.AcaoDaImagemNoEnvio{TirarMarcador: true}},
		{"IMAGE_INVALID", criar, RespostaDaImagem{Status: http.StatusBadRequest, Codigo: "IMAGE_INVALID"},
			TipoImagemRecusada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Recusar: true, Erro: "IMAGE_INVALID"}},
		{"IMAGE_TOO_LARGE", criar, RespostaDaImagem{Status: http.StatusRequestEntityTooLarge, Codigo: "IMAGE_TOO_LARGE"},
			TipoImagemRecusada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Recusar: true, Erro: "IMAGE_TOO_LARGE"}},
		{"IMAGE_SHA_MISMATCH", criar, RespostaDaImagem{Status: http.StatusBadRequest, Codigo: "IMAGE_SHA_MISMATCH"},
			TipoImagemRecusada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Recusar: true, Erro: "IMAGE_SHA_MISMATCH"}},
		{"IMAGE_CONFLICT", criar, RespostaDaImagem{Status: http.StatusConflict, Codigo: "IMAGE_CONFLICT"},
			TipoImagemRecusada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Recusar: true, Erro: "IMAGE_CONFLICT"}},
		{"413 sem código (um proxy)", criar, RespostaDaImagem{Status: http.StatusRequestEntityTooLarge},
			TipoImagemRecusada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Recusar: true, Erro: CodigoImagemStatus}},
		{"TOO_MANY_IMAGES: espera", criar, RespostaDaImagem{Status: http.StatusConflict, Codigo: "TOO_MANY_IMAGES"},
			TipoImagemEsperando, storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: "TOO_MANY_IMAGES"}},
		{"IMAGES_FULL: espera", criar, RespostaDaImagem{Status: http.StatusConflict, Codigo: "IMAGES_FULL"},
			TipoImagemEsperando, storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: "IMAGES_FULL"}},
		{"a nota da lixeira na nuvem: espera", criar, RespostaDaImagem{Status: http.StatusConflict, Codigo: "CONFLICT"},
			TipoImagemEsperando, storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: "CONFLICT"}},
		{"a nota que a nuvem não tem: espera", criar, RespostaDaImagem{Status: http.StatusNotFound, Codigo: "NOT_FOUND"},
			TipoImagemEsperando, storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: "NOT_FOUND"}},
		{"500: espera", criar, RespostaDaImagem{Status: http.StatusInternalServerError},
			TipoImagemEsperando, storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: CodigoImagemStatus}},
		{"o DELETE 204", lapide, RespostaDaImagem{Status: http.StatusNoContent}, TipoImagemTirada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true}},
		{"o DELETE 404 (a nota já não existe)", lapide, RespostaDaImagem{Status: http.StatusNotFound}, TipoImagemTirada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true}},
		{"o DELETE 410", lapide, RespostaDaImagem{Status: http.StatusGone}, TipoImagemTirada, storage.AcaoDaImagemNoEnvio{TirarMarcador: true}},
		{"o DELETE 500: a lápide fica", lapide, RespostaDaImagem{Status: http.StatusInternalServerError}, TipoImagemEsperando, storage.AcaoDaImagemNoEnvio{}},
	} {
		d := DecidirEnvioDaImagem(c.it, c.r)
		if d.Tipo != c.tipo || d.Acao != c.acao {
			t.Errorf("%s: %+v, quero %s %+v", c.nome, d, c.tipo, c.acao)
		}
	}
}

func TestDecidirBaixaDaImagem(t *testing.T) {
	for _, c := range []struct {
		r    RespostaDaImagem
		tipo string
		acao storage.AcaoDaImagemNaBaixa
	}{
		{RespostaDaImagem{Status: http.StatusNotFound}, TipoImagemSumiuNaNuvem, storage.AcaoDaImagemNaBaixa{Sumiu: true}},
		{RespostaDaImagem{Status: http.StatusGone}, TipoImagemSumiuNaNuvem, storage.AcaoDaImagemNaBaixa{Sumiu: true}},
		{RespostaDaImagem{Status: http.StatusOK}, TipoImagemEstragada, storage.AcaoDaImagemNaBaixa{Esperar: true, Erro: CodigoImagemEstragada}},
		{RespostaDaImagem{Status: http.StatusInternalServerError}, TipoImagemEsperandoBaixa, storage.AcaoDaImagemNaBaixa{Esperar: true, Erro: CodigoImagemStatus}},
		{RespostaDaImagem{Status: http.StatusForbidden, Codigo: "FORBIDDEN"}, TipoImagemEsperandoBaixa, storage.AcaoDaImagemNaBaixa{Esperar: true, Erro: "FORBIDDEN"}},
	} {
		if d := DecidirBaixaDaImagem(c.r); d.Tipo != c.tipo || d.Acao != c.acao {
			t.Errorf("%+v: %+v, quero %s %+v", c.r, d, c.tipo, c.acao)
		}
	}
}

func TestEsperaDaImagem(t *testing.T) {
	for _, c := range []struct {
		n    int
		quer time.Duration
	}{{0, time.Minute}, {1, time.Minute}, {2, 2 * time.Minute}, {5, 16 * time.Minute}, {9, 256 * time.Minute}, {10, 6 * time.Hour}, {1000, 6 * time.Hour}} {
		if got := EsperaDaImagem(c.n); got != c.quer {
			t.Errorf("EsperaDaImagem(%d) = %v, quero %v", c.n, got, c.quer)
		}
	}
}

// As imagens de uma página do pull: a mudança note_image (a lápide e o upsert),
// e a lista imagens de cada nota, sem repetir a que veio como mudança.
func TestImagensDaPagina(t *testing.T) {
	m := metaDeTeste()
	outra := *m
	outra.ID = "0199a000-0000-7000-8000-00000000a002"
	nota := Nota{ID: notaDaImagem, Imagens: []ImagemDaNuvem{*m, outra}}
	lista, porID := imagensDaPagina([]Change{
		{EntityType: storage.EntidadeImagemDaNota, EntityID: "0199A000-0000-7000-8000-00000000A003", Operation: OpTombstone},
		{EntityType: storage.EntidadeImagemDaNota, EntityID: idDaImagem, Operation: OpUpsert, PayloadImagem: m},
		{EntityType: storage.EntidadeNota, EntityID: notaDaImagem, Operation: OpUpsert, PayloadNota: &nota},
		{EntityType: storage.EntidadeDitado, EntityID: "0199a000-0000-7000-8000-00000000c001", Operation: OpUpsert},
	})
	quero := []storage.ImagemNoPull{
		{ID: "0199a000-0000-7000-8000-00000000a003", Tombstone: true},
		{ID: idDaImagem, NotaID: notaDaImagem},
		{ID: outra.ID, NotaID: notaDaImagem},
	}
	if len(lista) != len(quero) {
		t.Fatalf("a lista = %+v", lista)
	}
	for i := range quero {
		if lista[i] != quero[i] {
			t.Errorf("lista[%d] = %+v, quero %+v", i, lista[i], quero[i])
		}
	}
	if !porID["0199a000-0000-7000-8000-00000000a003"].Tombstone || porID[outra.ID].Meta == nil || porID[outra.ID].Meta.NoteID != notaDaImagem {
		t.Errorf("as decisões por id = %+v", porID)
	}
}
