package cloudstore

import (
	"errors"
	"sort"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
)

// As imagens das notas na nuvem (ADR-022, "Decisões da N10b"; a 0026). Este
// arquivo é a parte pura: o tipo, os erros e as duas regras (anexar e tirar).
// Nada aqui toca banco, rede ou log, e nenhum erro traz bytes, tamanho ou texto.
//
// A imagem é uma entidade à parte da nota, IMUTÁVEL: nasce (o PUT de um PC, o
// POST do celular) e sai (o "Tirar da nota"), e nunca muda. Anexar e tirar não
// sobem a versão da nota (como no PC, N10a). O id é o UUID v7 de onde a imagem
// foi anexada, guardado como veio.
//
// Os bytes nunca viajam no JSON do push nem do pull: sobem pelo PUT (o PC) ou
// pelo POST (o celular), já conferidos por imagemdanota.Conferir antes de o
// Store ser chamado, e descem pelo GET. O pull leva só os metadados (a mudança
// note_image, e a lista imagens de cada nota).
//
// Tirar deixa a lápide NA PRÓPRIA LINHA (removed_at, sem os bytes): um PUT
// atrasado do mesmo id recebe ErrImageGone e não ressuscita a imagem. A linha
// sai com a nota (a chave estrangeira com cascata): o expurgo de 30 dias, o
// "Apagar para sempre", a lápide da nota e o "Apagar meus dados" levam as
// imagens, e a lápide da nota cobre o PUT atrasado de qualquer imagem dela.

// EntityNoteImage é a imagem da nota no pull (entity_type). O push não a aceita
// (tipo_desconhecido): a imagem sobe pelo PUT, e sai pelo DELETE.
const EntityNoteImage = "note_image"

// NoteImage é a imagem da nota na nuvem: só os metadados (os bytes são lidos à
// parte, Store.GetImage).
type NoteImage struct {
	ID     string
	UserID string
	NoteID string
	// Largura e Altura, em pixels; Bytes, o tamanho da cópia guardada (já
	// limpa); SHA256, o dos bytes guardados, em hexadecimal minúsculo. Os
	// quatro são zero na lápide de um id que a nuvem nunca teve
	// (DecidirTirar).
	Largura int
	Altura  int
	Bytes   int64
	SHA256  string
	// CreatedAt é a hora do anexo: a do PC que anexou (dentro da folga para o
	// futuro) ou a do servidor (o celular). A ordem das imagens na nota é esta,
	// e o id desempata.
	CreatedAt time.Time
	// RemovedAt é a lápide: a imagem foi tirada da nota, e os bytes já não
	// existem.
	RemovedAt *time.Time
	LastSeq   int64
}

// Viva diz se a imagem ainda está na nota (não tem lápide).
func (i NoteImage) Viva() bool { return i.RemovedAt == nil }

// OrdenarImagens põe as imagens na ordem da nota: created_at, e o id no empate.
func OrdenarImagens(is []NoteImage) {
	sort.Slice(is, func(a, b int) bool {
		if !is[a].CreatedAt.Equal(is[b].CreatedAt) {
			return is[a].CreatedAt.Before(is[b].CreatedAt)
		}
		return is[a].ID < is[b].ID
	})
}

// Os erros das imagens (além de ErrNoteNotFound e ErrNoteInTrash). Nenhum traz
// bytes, tamanho ou texto.
var (
	// ErrImageNotFound: a imagem não existe na nota (ou é de outra nota, de
	// outra conta, ou o id não é UUID), ou só tem a lápide (GetImage).
	ErrImageNotFound = errors.New("cloudstore: imagem não encontrada")
	// ErrImageGone: o anexo de um id com lápide (a imagem foi tirada da nota)
	// ou de uma nota com lápide (apagada de vez). Um reenvio atrasado nunca a
	// traz de volta.
	ErrImageGone = errors.New("cloudstore: a imagem foi tirada da nota")
	// ErrImageConflict: o id já é de outra imagem (outros bytes, ou outra nota).
	// Não acontece com o UUID v7 de um PC: é defeito de quem manda.
	ErrImageConflict = errors.New("cloudstore: o id da imagem já é de outra imagem")
	// ErrTooManyImages: a nota já tem imagemdanota.MaxPorNota imagens.
	ErrTooManyImages = errors.New("cloudstore: a nota já tem o máximo de imagens")
	// ErrImagesFull: com esta, o total das imagens da conta passaria de
	// imagemdanota.TetoDoTotal (as da lixeira contam: elas voltam com a nota).
	ErrImagesFull = errors.New("cloudstore: as imagens das notas chegaram ao teto")
)

// LugarDaImagem é o que o Store lê, na transação que trava o contador, antes
// de decidir um anexo ou um tirar.
type LugarDaImagem struct {
	// Nota é a nota (também a da lixeira), ou nil se ela não existe.
	Nota *Note
	// NotaComLapide: a nota foi apagada de vez (note_tombstones).
	NotaComLapide bool
	// Atual é a linha da imagem com o id pedido, viva ou com lápide, em
	// qualquer nota da conta; nil se não há.
	Atual *NoteImage
	// NaNota é quantas imagens vivas a nota tem.
	NaNota int
	// Total é a soma dos bytes das imagens vivas da conta, as das notas da
	// lixeira inclusive.
	Total int64
}

// ImagemNova é o anexo pedido: o id, a nota, a imagem já conferida (os bytes
// limpos de imagemdanota.Conferir) e a hora de criação que o PC mandou (zero:
// a de agora).
type ImagemNova struct {
	ID       string
	NoteID   string
	Limpa    imagemdanota.Limpa
	CriadaEm time.Time
}

// DecidirAnexo decide o anexo de n sobre o lugar lido. Devolve a imagem e se
// ela tem de ser gravada:
//   - o id já existe, vivo, na mesma nota e com os mesmos bytes (o SHA-256): é
//     o reenvio de um anexo que já valeu, e volta o que está gravado, sem gravar
//     nem gastar seq (também com a nota já na lixeira: o anexo valeu antes);
//   - o id com lápide: ErrImageGone; o id de outros bytes ou de outra nota:
//     ErrImageConflict;
//   - a nota que não existe: ErrImageGone com a lápide dela, ErrNoteNotFound
//     sem; na lixeira: ErrNoteInTrash (restaure antes);
//   - a nota com o máximo de imagens: ErrTooManyImages; acima do teto do total:
//     ErrImagesFull;
//   - senão, a imagem nova, com a hora do PC (a de agora se veio zero ou além da
//     folga para o futuro, FutureTolerance).
func DecidirAnexo(l LugarDaImagem, userID string, n ImagemNova, now time.Time) (NoteImage, bool, error) {
	if a := l.Atual; a != nil {
		switch {
		case !a.Viva():
			return NoteImage{}, false, ErrImageGone
		case a.NoteID != n.NoteID || a.SHA256 != n.Limpa.SHA256:
			return NoteImage{}, false, ErrImageConflict
		}
		return *a, false, nil
	}
	if l.Nota == nil {
		if l.NotaComLapide {
			return NoteImage{}, false, ErrImageGone
		}
		return NoteImage{}, false, ErrNoteNotFound
	}
	if l.Nota.DeletedAt != nil {
		return NoteImage{}, false, ErrNoteInTrash
	}
	if l.NaNota >= imagemdanota.MaxPorNota {
		return NoteImage{}, false, ErrTooManyImages
	}
	if l.Total+int64(len(n.Limpa.Bytes)) > imagemdanota.TetoDoTotal {
		return NoteImage{}, false, ErrImagesFull
	}
	agora := Normalize(now)
	criada := Normalize(n.CriadaEm)
	if n.CriadaEm.IsZero() || criada.After(agora.Add(FutureTolerance)) {
		criada = agora
	}
	return NoteImage{
		ID: n.ID, UserID: userID, NoteID: n.NoteID,
		Largura: n.Limpa.Largura, Altura: n.Limpa.Altura, Bytes: int64(len(n.Limpa.Bytes)), SHA256: n.Limpa.SHA256,
		CreatedAt: criada,
	}, true, nil
}

// O que DecidirTirar manda fazer.
const (
	// TirarNada: não há o que gravar (a imagem já tem a lápide, ou a nota já
	// não existe).
	TirarNada = iota
	// TirarLapide: a lápide na linha da imagem viva (os bytes saem).
	TirarLapide
	// TirarLapideNova: a lápide de um id que a nuvem nunca teve, numa nota que
	// existe: o PUT que ainda pode estar na rede (o PC mandou e não ouviu a
	// resposta) chega depois e recebe ErrImageGone.
	TirarLapideNova
)

// DecidirTirar decide o "Tirar da nota" da imagem id da nota noteID. Vale também
// na nota da lixeira (o Desfazer do painel pode vencer depois de a nota ir para
// lá). Idempotente: tirar de novo, ou tirar de uma nota apagada de vez, é
// TirarNada. A nota que não existe nem tem lápide é ErrNoteNotFound; o id que é
// de outra nota, ErrImageNotFound.
func DecidirTirar(l LugarDaImagem, noteID string) (int, error) {
	if a := l.Atual; a != nil {
		switch {
		case a.NoteID != noteID:
			return TirarNada, ErrImageNotFound
		case !a.Viva():
			return TirarNada, nil
		}
		return TirarLapide, nil
	}
	if l.Nota == nil {
		if l.NotaComLapide {
			return TirarNada, nil
		}
		return TirarNada, ErrNoteNotFound
	}
	return TirarLapideNova, nil
}
