package cloudstore

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// Este arquivo é a nota e o marcador de nota na nuvem (ADR-022, N7): os tipos,
// os limites, o payload do push e as validações. As regras do push estão em
// notas_regras.go, e a mescla da lista de marcar em lista_de_marcar.go. Nada
// aqui toca banco, rede ou log, e nenhum código de recusa traz texto.
//
// Os limites e as validações são os do PC (internal/storage, nota.go e
// posicao.go), reescritos aqui porque o servidor não importa o armazenamento
// do PC: o título até 1 KiB, o corpo até MaxTextBytes, as oito cores, o nome
// do marcador de 1 a 50 caracteres (limpo como o nomeDoMarcador do PC) e a
// posição do índice fracionário (PosicaoValida). O teste
// TestNotas_AsValidacoesSaoAsDoPC confere, numa tabela, que os dois lados
// concordam.

// Note é a nota na nuvem (a 0021). Os ponteiros e a fatia Marcadores nunca são
// alterados no lugar: as regras trocam o valor inteiro, então copiar a struct
// é copiar o estado (como em Transcription).
type Note struct {
	ID     string
	UserID string
	Titulo string
	Corpo  string
	// Cor é um dos CoresDaNota, ou nil.
	Cor       *string
	Fixada    bool
	Arquivada bool
	// Posicao é a chave do índice fracionário (PosicaoValida): a ordem manual
	// da N2b, que viaja com a nota.
	Posicao string
	// Marcadores são os ids dos marcadores da nota, sem repetição e em ordem
	// de id (a note_labels). Nunca nil depois de passar pelas regras.
	Marcadores []string
	// DitadoDeOrigem é o ditado de que a nota nasceu (ADR-022, "Ditado vira
	// nota"), ou nil. Só se grava na criação.
	DitadoDeOrigem *string
	CreatedAt      time.Time
	// UpdatedAt muda quando o conteúdo (título ou corpo) muda, e na criação.
	UpdatedAt time.Time
	DeletedAt *time.Time
	// PurgeAt é deleted_at + 30 dias: a nota não tem a janela de 6 meses.
	PurgeAt *time.Time

	// Version sobe a cada gravação. ContentVersion e TrashVersion são a versão
	// em que o conteúdo (título e corpo juntos) e a lixeira mudaram por último,
	// e ContentDeviceID e TrashDeviceID, o aparelho dessas mudanças: o mesmo
	// desenho do ditado (TextVersion, TrashVersion).
	Version         int64
	ContentVersion  int64
	TrashVersion    int64
	ContentDeviceID *string
	TrashDeviceID   *string
	LastSeq         int64

	// Conflict é o selo: o título e o corpo que perderam um conflito.
	Conflict *NoteConflict
}

// NoteConflict é a versão da nota que perdeu um conflito de conteúdo: o título
// e o corpo juntos, o aparelho (nulo quando não houve) e quando.
type NoteConflict struct {
	Titulo   string
	Corpo    string
	DeviceID *string
	At       time.Time
}

// Label é o marcador de nota na nuvem (a 0020). A nuvem não impõe nome único:
// dois PCs podem criar "Mercado" offline, e quem resolve é o PC, ao gravar o
// que veio.
type Label struct {
	ID        string
	UserID    string
	Nome      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
	LastSeq   int64
}

// NoteTextBytes é o tamanho do texto de uma nota no orçamento de uma página:
// o título, o corpo e o selo.
func NoteTextBytes(n Note) int {
	s := len(n.Titulo) + len(n.Corpo)
	if n.Conflict != nil {
		s += len(n.Conflict.Titulo) + len(n.Conflict.Corpo)
	}
	return s
}

// Os limites da nota e do marcador (os do PC, internal/storage).
const (
	// MaxNoteTitleBytes é o teto do título, em bytes (o MaxTituloDaNotaBytes
	// do PC). O corpo tem o MaxTextBytes, o mesmo do ditado.
	MaxNoteTitleBytes = 1 << 10
	// MaxLabelName é o teto do nome do marcador, em caracteres (runas).
	MaxLabelName = 50
	// MaxPosicao é o teto da chave da posição (o MaxPosicao do PC).
	MaxPosicao = 128
	// MaxNoteLabels é quantos marcadores uma nota leva no máximo.
	// [ASSUMPTION] O PC não tem teto; 100 é folga de sobra para uma pessoa, e
	// impede que um cliente com defeito grave milhares de ligações numa nota.
	MaxNoteLabels = 100
	// MaxNotesList é o teto fixo do GET /notes e do GET /labels (o celular):
	// 500, como o do aprendido e o da página do pull.
	MaxNotesList = 500
)

// As vistas da listagem das notas (GET /notes?vista=).
const (
	// VistaAtivas: fora da lixeira e não arquivadas.
	VistaAtivas = "ativas"
	// VistaArquivo: fora da lixeira e arquivadas.
	VistaArquivo = "arquivo"
	// VistaLixeira: na lixeira.
	VistaLixeira = "lixeira"
)

// VistaValida diz se v é uma das vistas da listagem das notas.
func VistaValida(v string) bool {
	return v == VistaAtivas || v == VistaArquivo || v == VistaLixeira
}

// NotePage é a lista de notas do celular; LabelPage, a de marcadores. Items
// nunca é nil, e HasMore diz que passou do teto (ou do orçamento de texto).
type NotePage struct {
	Items   []Note
	HasMore bool
}

type LabelPage struct {
	Items   []Label
	HasMore bool
}

// NotesListLimit é o limite efetivo das listagens das notas e dos marcadores:
// o pedido, ou o teto quando vem zero, negativo ou acima dele.
func NotesListLimit(limit int) int {
	if limit <= 0 || limit > MaxNotesList {
		return MaxNotesList
	}
	return limit
}

// NoteBefore é a ordem da listagem das notas numa vista: na lixeira, a que foi
// para lá por último primeiro (deleted_at desc, id); nas outras, as fixadas
// primeiro, depois a posição (na ordem dos bytes) e o id. Os dois Stores a
// usam (o do Postgres a escreve em SQL, com a posição em COLLATE "C").
func NoteBefore(vista string, a, b Note) bool {
	if vista == VistaLixeira {
		if !a.DeletedAt.Equal(*b.DeletedAt) {
			return a.DeletedAt.After(*b.DeletedAt)
		}
		return a.ID < b.ID
	}
	if a.Fixada != b.Fixada {
		return a.Fixada
	}
	if a.Posicao != b.Posicao {
		return a.Posicao < b.Posicao
	}
	return a.ID < b.ID
}

// NaVista diz se a nota aparece na vista.
func NaVista(vista string, n Note) bool {
	switch vista {
	case VistaLixeira:
		return n.DeletedAt != nil
	case VistaArquivo:
		return n.DeletedAt == nil && n.Arquivada
	default:
		return n.DeletedAt == nil && !n.Arquivada
	}
}

// LabelBefore é a ordem da listagem dos marcadores: o nome na ordem dos bytes
// (a mesma no Go e no COLLATE "C" do Postgres; a tela ordena como quiser), e o
// id no empate.
func LabelBefore(a, b Label) bool {
	if a.Nome != b.Nome {
		return a.Nome < b.Nome
	}
	return a.ID < b.ID
}

// CoresDaNota são os oito nomes de cor (ADR-022, E5), os mesmos do PC.
var CoresDaNota = []string{"amarelo", "laranja", "vermelho", "rosa", "roxo", "azul", "verde", "cinza"}

// CorValida diz se cor é uma das CoresDaNota.
func CorValida(cor string) bool {
	for _, c := range CoresDaNota {
		if c == cor {
			return true
		}
	}
	return false
}

// alfabetoDaPosicao é o do PC (posicao.go): os 62 dígitos, em ordem de byte.
const alfabetoDaPosicao = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// PosicaoValida é a regra do PC (storage.PosicaoValida): de 1 a MaxPosicao
// caracteres do alfabeto, sem '0' no fim.
func PosicaoValida(s string) bool {
	if s == "" || len(s) > MaxPosicao || s[len(s)-1] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(alfabetoDaPosicao, s[i]) < 0 {
			return false
		}
	}
	return true
}

// NomeDoMarcador limpa o nome do marcador como o nomeDoMarcador do PC: as
// bordas sem espaço e cada sequência de espaços virando um; recusa o nulo, os
// controles (fora o espaço comum), o separador de linha e o de parágrafo, o
// vazio e o que passa de MaxLabelName caracteres. ok=false: o nome não vale.
func NomeDoMarcador(bruto string) (string, bool) {
	if strings.ContainsRune(bruto, 0) || !utf8.ValidString(bruto) {
		return "", false
	}
	for _, r := range bruto {
		if r != ' ' && (unicode.IsControl(r) || r == '\u2028' || r == '\u2029') {
			return "", false
		}
	}
	nome := strings.Join(strings.Fields(bruto), " ")
	if nome == "" || utf8.RuneCountInString(nome) > MaxLabelName {
		return "", false
	}
	return nome, true
}

// noteTrashPurgeAt é o purge_at de uma nota que entra na lixeira em deletedAt:
// 30 dias depois (retention.TrashRetentionDays), em UTC. A nota não tem a
// janela de 6 meses nem o piso da chegada do ditado: a nuvem não expurga nota
// sozinha (quem apaga de vez é o PC, com o delete).
func noteTrashPurgeAt(deletedAt *time.Time) *time.Time {
	if deletedAt == nil {
		return nil
	}
	p := Normalize(deletedAt.UTC().AddDate(0, 0, retention.TrashRetentionDays))
	return &p
}

// --- O payload ---
//
// A nota tem dois formatos, como o ditado:
//
//   - criar (base 0, e o recomeço sobre uma nota que a nuvem tem): a nota
//     inteira, com noteCreateFields. Todos obrigatórios menos ditado_de_origem e
//     deleted_at (ausente é null).
//   - mudar (base >= 1): só as chaves do que o PC mudou (os bits do marcador da
//     fila), entre as de notePatchFields. titulo e corpo vêm juntos (o conteúdo),
//     e corpo_base, o corpo da versão base, só com eles e só quando o PC o
//     conhece (é o que permite a mescla da lista de marcar). A escolha do selo é
//     resolver_conflito com conflict_at, os dois juntos, como no ditado.
//
// O apagar de vez é a operação delete, com o payload vazio (ausente, null ou
// {}), como o desfazer do aprendido.
//
// O marcador: criar com nome e created_at; mudar com nome; apagar com delete.

var noteCreateFields = map[string]bool{
	"titulo":           true,
	"corpo":            true,
	"cor":              true,
	"fixada":           true,
	"arquivada":        true,
	"posicao":          true,
	"marcadores":       true,
	"ditado_de_origem": false,
	"created_at":       true,
	"deleted_at":       false,
}

var notePatchFields = map[string]bool{
	"titulo":            false,
	"corpo":             false,
	"corpo_base":        false,
	"deleted_at":        false,
	"cor":               false,
	"fixada":            false,
	"arquivada":         false,
	"marcadores":        false,
	"posicao":           false,
	"resolver_conflito": false,
	"conflict_at":       false,
}

var labelCreateFields = map[string]bool{"nome": true, "created_at": true}

var labelPatchFields = map[string]bool{"nome": true}

// notePatch é o que o PC mandou de uma nota: no criar, tudo (has* todos
// verdadeiros, sem corpo_base); no mudar, só o que veio.
type notePatch struct {
	hasContent bool
	titulo     string
	corpo      string
	// hasBase: veio o corpo_base (o corpo da versão base, que o PC conhece).
	hasBase bool
	base    string

	hasTrash bool
	trash    *time.Time

	hasCor bool
	cor    *string

	hasFixada bool
	fixada    bool

	hasArquivada bool
	arquivada    bool

	hasMarcadores bool
	marcadores    []string

	hasPosicao bool
	posicao    string

	hasChoice bool
	choice    string
	choiceAt  time.Time

	// Só do criar.
	createdAt      time.Time
	ditadoDeOrigem *string
}

// noteText lê um texto da nota: string JSON com UTF-8 válido nos bytes crus
// (rawTextValid), sem o nulo e até max bytes.
func noteText(v json.RawMessage, max int) (string, bool) {
	s, ok := jsonString(v)
	if !ok || strings.ContainsRune(s, 0) || len(s) > max {
		return "", false
	}
	return s, true
}

// jsonBool lê um booleano JSON; null não é booleano.
func jsonBool(v json.RawMessage) (bool, bool) {
	var b bool
	if isNull(v) || json.Unmarshal(v, &b) != nil {
		return false, false
	}
	return b, true
}

// parseCor lê a cor: null (sem cor) ou um dos CoresDaNota.
func parseCor(v json.RawMessage) (*string, bool) {
	if isNull(v) {
		return nil, true
	}
	s, ok := jsonString(v)
	if !ok || !CorValida(s) {
		return nil, false
	}
	return &s, true
}

// parseMarcadores lê o conjunto de marcadores: um array (nunca null) de UUIDs,
// no máximo MaxNoteLabels, sem repetir. Devolve em minúsculas e em ordem.
func parseMarcadores(v json.RawMessage) ([]string, bool) {
	t := bytes.TrimSpace(v)
	if len(t) == 0 || t[0] != '[' {
		return nil, false
	}
	var raw []json.RawMessage
	if json.Unmarshal(t, &raw) != nil || len(raw) > MaxNoteLabels {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		s, ok := jsonString(r)
		if !ok {
			return nil, false
		}
		id, ok := CanonicalUUID(s)
		if !ok || seen[id] {
			return nil, false
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out, true
}

// parseNoteCreate lê a nota inteira (o criar, e o recomeço com base 0).
func parseNoteCreate(raw json.RawMessage) (notePatch, string) {
	obj, code := decodeObject(raw, noteCreateFields)
	if code != "" {
		return notePatch{}, code
	}
	p := notePatch{hasContent: true, hasTrash: true, hasCor: true, hasFixada: true, hasArquivada: true,
		hasMarcadores: true, hasPosicao: true}
	var ok bool
	if p.titulo, ok = noteText(obj["titulo"], MaxNoteTitleBytes); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.corpo, ok = noteText(obj["corpo"], MaxTextBytes); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.cor, ok = parseCor(obj["cor"]); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.fixada, ok = jsonBool(obj["fixada"]); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.arquivada, ok = jsonBool(obj["arquivada"]); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.posicao, ok = jsonString(obj["posicao"]); !ok || !PosicaoValida(p.posicao) {
		return notePatch{}, CodeInvalidField
	}
	if p.marcadores, ok = parseMarcadores(obj["marcadores"]); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.ditadoDeOrigem, _, ok = optionalUUID(obj, "ditado_de_origem"); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.createdAt, ok = parseTime(obj["created_at"]); !ok {
		return notePatch{}, CodeInvalidField
	}
	if p.trash, ok = optionalTime(obj, "deleted_at"); !ok {
		return notePatch{}, CodeInvalidField
	}
	return p, ""
}

// parseNotePatch lê a mudança de uma nota que existe. Payload vazio, título
// sem corpo (ou o contrário), corpo_base sem o conteúdo, e a escolha do selo
// sem o conflict_at (ou o contrário) são campo_invalido.
func parseNotePatch(raw json.RawMessage) (notePatch, string) {
	obj, code := decodeObject(raw, notePatchFields)
	if code != "" {
		return notePatch{}, code
	}
	if len(obj) == 0 {
		return notePatch{}, CodeInvalidField
	}
	var p notePatch
	var ok bool
	tv, temTitulo := obj["titulo"]
	cv, temCorpo := obj["corpo"]
	bv, temBase := obj["corpo_base"]
	if temTitulo != temCorpo || temBase && !temCorpo {
		return notePatch{}, CodeInvalidField
	}
	if temTitulo {
		p.hasContent = true
		if p.titulo, ok = noteText(tv, MaxNoteTitleBytes); !ok {
			return notePatch{}, CodeInvalidField
		}
		if p.corpo, ok = noteText(cv, MaxTextBytes); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if temBase {
		p.hasBase = true
		if p.base, ok = noteText(bv, MaxTextBytes); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if _, present := obj["deleted_at"]; present {
		p.hasTrash = true
		if p.trash, ok = optionalTime(obj, "deleted_at"); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if v, present := obj["cor"]; present {
		p.hasCor = true
		if p.cor, ok = parseCor(v); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if v, present := obj["fixada"]; present {
		p.hasFixada = true
		if p.fixada, ok = jsonBool(v); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if v, present := obj["arquivada"]; present {
		p.hasArquivada = true
		if p.arquivada, ok = jsonBool(v); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if v, present := obj["marcadores"]; present {
		p.hasMarcadores = true
		if p.marcadores, ok = parseMarcadores(v); !ok {
			return notePatch{}, CodeInvalidField
		}
	}
	if v, present := obj["posicao"]; present {
		p.hasPosicao = true
		if p.posicao, ok = jsonString(v); !ok || !PosicaoValida(p.posicao) {
			return notePatch{}, CodeInvalidField
		}
	}
	escolha, temEscolha := obj["resolver_conflito"]
	quando, temQuando := obj["conflict_at"]
	if temEscolha != temQuando {
		return notePatch{}, CodeInvalidField
	}
	if temEscolha {
		s, ok := jsonString(escolha)
		if !ok || !EscolhaValida(s) {
			return notePatch{}, CodeInvalidField
		}
		at, ok := parseTime(quando)
		if !ok {
			return notePatch{}, CodeInvalidField
		}
		p.hasChoice, p.choice, p.choiceAt = true, s, at
	}
	return p, ""
}

// labelPayload é o marcador que o PC mandou: o nome já limpo e, no criar, a
// data.
type labelPayload struct {
	nome      string
	createdAt time.Time
}

// parseLabel lê o payload do marcador: o de criar (create) ou o de mudar.
func parseLabel(raw json.RawMessage, create bool) (labelPayload, string) {
	fields := labelPatchFields
	if create {
		fields = labelCreateFields
	}
	obj, code := decodeObject(raw, fields)
	if code != "" {
		return labelPayload{}, code
	}
	var p labelPayload
	bruto, ok := jsonString(obj["nome"])
	if !ok {
		return labelPayload{}, CodeInvalidField
	}
	if p.nome, ok = NomeDoMarcador(bruto); !ok {
		return labelPayload{}, CodeInvalidField
	}
	if create {
		if p.createdAt, ok = parseTime(obj["created_at"]); !ok {
			return labelPayload{}, CodeInvalidField
		}
	}
	return p, ""
}

// NoteLabelIDs devolve os ids de marcador que uma mutação de nota cita
// (marcadores, no criar ou no mudar), para o Store saber quais têm lápide antes
// de chamar DecideNote. Payload que não se lê, ou sem marcadores, é nil: as
// regras recusam o que não vale.
func NoteLabelIDs(m Mutation) []string {
	if m.EntityType != EntityNote || m.Operation != OpUpsert {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(m.Payload), &obj) != nil {
		return nil
	}
	v, ok := obj["marcadores"]
	if !ok {
		return nil
	}
	ids, ok := parseMarcadores(v)
	if !ok {
		return nil
	}
	return ids
}
