package cloudstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/notas"
)

// As escritas das notas e dos marcadores pela sessão do navegador (ADR-022,
// N8): o celular escreve pela /api/v1, e não pelo push. Cada pedido vira a
// mutação que um PC mandaria, com o Actor sem aparelho, e passa pelas MESMAS
// regras da N7 (DecideNote e DecideLabel: o conflito com selo, a mescla da
// lista de marcar, a lápide que nunca vence). O Store grava pelo ponto único,
// com o evento na note_sync_events, e os PCs recebem pelo pull.
//
// Este arquivo é a parte pura: o que cada pedido vira (a posição no topo do
// grupo, as vizinhas do mover, o conjunto de marcadores), o destino do "Ditar
// para nota" e o Desfazer dele. Nada aqui toca banco, rede ou log, e nenhum
// erro traz texto.

// Os erros das escritas do celular. Nenhum traz texto.
var (
	// ErrNoteNotFound: a nota não existe (ou é de outro usuário, ou o id não é
	// um UUID): os três casos são o mesmo erro, de propósito.
	ErrNoteNotFound = errors.New("cloudstore: nota não encontrada")
	// ErrLabelNotFound: o marcador não existe (ou tem lápide, ou é de outro
	// usuário).
	ErrLabelNotFound = errors.New("cloudstore: marcador não encontrado")
	// ErrNoteInTrash: marcar, desmarcar e mover uma nota da lixeira (o PC
	// recusa igual: restaure antes).
	ErrNoteInTrash = errors.New("cloudstore: a nota está na lixeira")
	// ErrNoteNotInTrash: apagar de vez uma nota fora da lixeira.
	ErrNoteNotInTrash = errors.New("cloudstore: a nota não está na lixeira")
	// ErrNameInUse: o nome do marcador já existe (sem diferenciar maiúscula
	// nem acento), como no PC.
	ErrNameInUse = errors.New("cloudstore: já existe um marcador com esse nome")
	// ErrInvalidLabelName: o nome do marcador não vale (NomeDoMarcador).
	ErrInvalidLabelName = errors.New("cloudstore: nome de marcador inválido")
	// ErrNeighbors: as vizinhas do mover não valem (não existem, estão na
	// lixeira, são de outro grupo, são a própria nota ou estão fora de ordem).
	ErrNeighbors = errors.New("cloudstore: as vizinhas não valem")
	// ErrNoNeighbor: o mover sem nenhuma vizinha.
	ErrNoNeighbor = errors.New("cloudstore: mover pede uma vizinha")
	// ErrEmptyNote: criar uma nota sem título e sem corpo.
	ErrEmptyNote = errors.New("cloudstore: a nota está vazia")
	// ErrNothingToChange: o pedido de edição não traz nada.
	ErrNothingToChange = errors.New("cloudstore: nada a mudar")
	// ErrInvalidNote: a mutação foi recusada pelas regras (um tamanho, a cor,
	// a data). A mensagem traz só o código de recusa, de uma lista fechada.
	ErrInvalidNote = errors.New("cloudstore: nota inválida")
	// ErrNotLinked: o Desfazer de um ditado que não está ligado a nota
	// nenhuma (outro aparelho já o desfez, ou ele nunca foi para uma nota).
	ErrNotLinked = errors.New("cloudstore: o ditado não está numa nota")
)

// OperacaoNaNota é o que o celular pede para uma nota que existe.
type OperacaoNaNota int

const (
	// NotaEditar: o PATCH do título, do corpo, da cor, de fixar e de
	// arquivar.
	NotaEditar OperacaoNaNota = iota + 1
	// NotaEscolher: a escolha do selo (manter ou trocar).
	NotaEscolher
	// NotaLixeira e NotaRestaurar: a lixeira das notas.
	NotaLixeira
	NotaRestaurar
	// NotaMover: o arrastar (a ordem manual, N2b).
	NotaMover
	// NotaMarcar e NotaDesmarcar: pôr e tirar um marcador.
	NotaMarcar
	NotaDesmarcar
	// NotaApagarDeVez: só da lixeira; deixa a lápide.
	NotaApagarDeVez
)

// PedidoDaNota é o pedido do celular. Cada operação lê só os campos dela.
type PedidoDaNota struct {
	Op OperacaoNaNota
	// BaseVersion: a versão que o celular conhece (NotaEditar e
	// NotaEscolher). As outras operações escrevem sobre a versão atual: são
	// intenções que não dependem do texto (a lixeira, a posição, o marcador).
	BaseVersion int64

	// NotaEditar: só o que mudou. Título e corpo são o conteúdo: o que não
	// veio é o da nota atual. CorpoBase é o corpo da versão base, que o
	// celular conhece: é o que permite a mescla da lista de marcar
	// (MesclarListaDeMarcar).
	Titulo, Corpo, CorpoBase *string
	MudarCor                 bool
	Cor                      *string
	Fixada, Arquivada        *bool

	// NotaEscolher: EscolhaManter ou EscolhaTrocar.
	Escolha string

	// NotaMover: as vizinhas DEPOIS de soltar; "" é a ponta.
	Antes, Depois string

	// NotaMarcar e NotaDesmarcar: o id do marcador.
	Marcador string
}

// ContextoDaNota é o que o plano lê do Store, dentro da transação que vai
// gravar (o contador travado: nada do mesmo usuário muda no meio).
type ContextoDaNota struct {
	// Nota lê outra nota do usuário pelo id canônico; nil quando não existe.
	Nota func(id string) (*Note, error)
	// Topo é a menor posição do grupo (arquivada, fixada) fora da lixeira,
	// sem a nota exceto; "" com o grupo vazio.
	Topo func(arquivada, fixada bool, exceto string) (string, error)
	// MarcadorVivo diz se o marcador existe (a linha, sem lápide).
	MarcadorVivo func(id string) (bool, error)
}

// Conteudo é o título e o corpo que o pedido mandou.
type Conteudo struct {
	Titulo, Corpo string
}

// PlanoDaNota é o que um pedido vira: a mutação para DecideNote, ou nada.
type PlanoDaNota struct {
	Mutation Mutation
	// Nada: o pedido não muda nada (já está na lixeira, já está entre as
	// vizinhas, o marcador já está ou já saiu). Nada se grava.
	Nada bool
	// Enviado é o conteúdo que o pedido mandou (só NotaEditar com título ou
	// corpo): é com ele que o Store diz se a gravação foi mesclada.
	Enviado *Conteudo
}

// NotaGravada é o que uma escrita do celular fez com a nota.
type NotaGravada struct {
	// Nota é a nota como ficou (vazia no apagar de vez).
	Nota Note
	// Mesclada: a gravação valeu, mas o conteúdo que ficou não é o que o
	// celular mandou (a lista de marcar foi juntada com a de outro aparelho):
	// o celular passa a mostrar o que ficou.
	Mesclada bool
	// NoSelo: o conteúdo mandado perdeu (outro aparelho mudou o texto depois
	// da base, ou a nota foi para a lixeira) e ficou guardado no selo.
	NoSelo bool
}

// patchDaNota monta a mutação de nota de um pedido do celular.
func patchDaNota(id string, base int64, fields map[string]any) (Mutation, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return Mutation{}, fmt.Errorf("%w: %s", ErrInvalidNote, CodeInvalidField)
	}
	return Mutation{MutationID: NewUUIDv7(), EntityType: EntityNote, EntityID: id, Operation: OpUpsert, BaseVersion: base, Payload: raw}, nil
}

// posicaoNoTopo é a chave que põe a nota acima da primeira do grupo.
func posicaoNoTopo(c ContextoDaNota, arquivada, fixada bool, exceto string) (string, error) {
	primeira, err := c.Topo(arquivada, fixada, exceto)
	if err != nil {
		return "", err
	}
	k, err := notas.PosicaoEntre("", primeira)
	if err != nil {
		// Uma posição torta gravada (não acontece: o push e o PC as validam).
		return "", fmt.Errorf("%w: %s", ErrInvalidNote, CodeInvalidField)
	}
	return k, nil
}

// PlanejarNota transforma o pedido do celular, sobre a nota atual cur, na
// mutação que um PC mandaria. As regras de cada operação são as do PC (a API
// local, internal/agent/localapi/notas.go):
//   - fixar, desafixar, arquivar, desarquivar e restaurar levam a nota ao topo
//     do grupo de destino; editar o resto não mexe na posição;
//   - marcar, desmarcar e mover recusam a nota da lixeira;
//   - o mover recusa a vizinha que não existe, que está na lixeira, que é de
//     outro grupo, que é a própria nota, ou as duas fora de ordem, e não grava
//     nada se a nota já está entre elas;
//   - apagar de vez é só da lixeira.
//
// A edição e a escolha do selo levam a base que o celular conhece; o resto
// escreve sobre a versão atual.
func PlanejarNota(p PedidoDaNota, cur Note, c ContextoDaNota, now time.Time) (PlanoDaNota, error) {
	now = Normalize(now)
	switch p.Op {
	case NotaEditar:
		return planejarEdicao(p, cur, c)
	case NotaEscolher:
		if !EscolhaValida(p.Escolha) {
			return PlanoDaNota{}, ErrInvalidChoice
		}
		if cur.Conflict == nil {
			return PlanoDaNota{}, ErrNoConflict
		}
		if p.BaseVersion != cur.Version {
			return PlanoDaNota{}, ErrVersionConflict
		}
		m, err := patchDaNota(cur.ID, cur.Version, map[string]any{
			"resolver_conflito": p.Escolha, "conflict_at": cur.Conflict.At.Format(time.RFC3339Nano),
		})
		return PlanoDaNota{Mutation: m}, err
	case NotaLixeira:
		if cur.DeletedAt != nil {
			return PlanoDaNota{Nada: true}, nil
		}
		m, err := patchDaNota(cur.ID, cur.Version, map[string]any{"deleted_at": now.Format(time.RFC3339Nano)})
		return PlanoDaNota{Mutation: m}, err
	case NotaRestaurar:
		if cur.DeletedAt == nil {
			return PlanoDaNota{Nada: true}, nil
		}
		k, err := posicaoNoTopo(c, cur.Arquivada, cur.Fixada, cur.ID)
		if err != nil {
			return PlanoDaNota{}, err
		}
		m, err := patchDaNota(cur.ID, cur.Version, map[string]any{"deleted_at": nil, "posicao": k})
		return PlanoDaNota{Mutation: m}, err
	case NotaMover:
		return planejarMover(p, cur, c)
	case NotaMarcar, NotaDesmarcar:
		return planejarMarcador(p, cur, c)
	case NotaApagarDeVez:
		if cur.DeletedAt == nil {
			return PlanoDaNota{}, ErrNoteNotInTrash
		}
		return PlanoDaNota{Mutation: Mutation{
			MutationID: NewUUIDv7(), EntityType: EntityNote, EntityID: cur.ID, Operation: OpDelete,
		}}, nil
	}
	return PlanoDaNota{}, ErrNothingToChange
}

func planejarEdicao(p PedidoDaNota, cur Note, c ContextoDaNota) (PlanoDaNota, error) {
	if p.BaseVersion < 1 {
		return PlanoDaNota{}, ErrVersionConflict
	}
	fields := map[string]any{}
	var enviado *Conteudo
	if p.Titulo != nil || p.Corpo != nil {
		// O conteúdo é o título e o corpo juntos (N7): o que não veio é o de
		// agora. Com a base velha, as regras comparam o par inteiro.
		e := Conteudo{Titulo: cur.Titulo, Corpo: cur.Corpo}
		if p.Titulo != nil {
			e.Titulo = *p.Titulo
		}
		if p.Corpo != nil {
			e.Corpo = *p.Corpo
		}
		fields["titulo"], fields["corpo"] = e.Titulo, e.Corpo
		if p.CorpoBase != nil {
			fields["corpo_base"] = *p.CorpoBase
		}
		enviado = &e
	}
	if p.MudarCor {
		fields["cor"] = p.Cor
	}
	if p.Fixada != nil {
		fields["fixada"] = *p.Fixada
	}
	if p.Arquivada != nil {
		fields["arquivada"] = *p.Arquivada
	}
	if len(fields) == 0 {
		return PlanoDaNota{}, ErrNothingToChange
	}
	// Mudar de grupo leva ao topo do grupo de destino (N2b), como no PC.
	destArquivada, destFixada := cur.Arquivada, cur.Fixada
	if p.Arquivada != nil {
		destArquivada = *p.Arquivada
	}
	if p.Fixada != nil {
		destFixada = *p.Fixada
	}
	if destArquivada != cur.Arquivada || destFixada != cur.Fixada {
		k, err := posicaoNoTopo(c, destArquivada, destFixada, cur.ID)
		if err != nil {
			return PlanoDaNota{}, err
		}
		fields["posicao"] = k
	}
	m, err := patchDaNota(cur.ID, p.BaseVersion, fields)
	return PlanoDaNota{Mutation: m, Enviado: enviado}, err
}

func planejarMover(p PedidoDaNota, cur Note, c ContextoDaNota) (PlanoDaNota, error) {
	if p.Antes == "" && p.Depois == "" {
		return PlanoDaNota{}, ErrNoNeighbor
	}
	if cur.DeletedAt != nil {
		return PlanoDaNota{}, ErrNeighbors
	}
	vizinha := func(id string) (string, error) {
		if id == "" {
			return "", nil
		}
		canon, ok := CanonicalUUID(id)
		if !ok || canon == cur.ID {
			return "", ErrNeighbors
		}
		n, err := c.Nota(canon)
		if err != nil {
			return "", err
		}
		if n == nil || n.DeletedAt != nil || n.Fixada != cur.Fixada || n.Arquivada != cur.Arquivada {
			return "", ErrNeighbors
		}
		return n.Posicao, nil
	}
	antes, err := vizinha(p.Antes)
	if err != nil {
		return PlanoDaNota{}, err
	}
	depois, err := vizinha(p.Depois)
	if err != nil {
		return PlanoDaNota{}, err
	}
	if antes != "" && depois != "" && antes >= depois {
		return PlanoDaNota{}, ErrNeighbors
	}
	if (antes == "" || antes < cur.Posicao) && (depois == "" || cur.Posicao < depois) {
		return PlanoDaNota{Nada: true}, nil
	}
	k, err := notas.PosicaoEntre(antes, depois)
	if err != nil {
		return PlanoDaNota{}, ErrNeighbors
	}
	m, err := patchDaNota(cur.ID, cur.Version, map[string]any{"posicao": k})
	return PlanoDaNota{Mutation: m}, err
}

func planejarMarcador(p PedidoDaNota, cur Note, c ContextoDaNota) (PlanoDaNota, error) {
	id, ok := CanonicalUUID(p.Marcador)
	if !ok {
		return PlanoDaNota{}, ErrLabelNotFound
	}
	vivo, err := c.MarcadorVivo(id)
	if err != nil {
		return PlanoDaNota{}, err
	}
	if !vivo {
		return PlanoDaNota{}, ErrLabelNotFound
	}
	if cur.DeletedAt != nil {
		return PlanoDaNota{}, ErrNoteInTrash
	}
	tem := false
	set := make([]string, 0, len(cur.Marcadores)+1)
	for _, m := range cur.Marcadores {
		if m == id {
			tem = true
			if p.Op == NotaDesmarcar {
				continue
			}
		}
		set = append(set, m)
	}
	if tem == (p.Op == NotaMarcar) {
		return PlanoDaNota{Nada: true}, nil
	}
	if p.Op == NotaMarcar {
		set = append(set, id)
	}
	m, err := patchDaNota(cur.ID, cur.Version, map[string]any{"marcadores": set})
	return PlanoDaNota{Mutation: m}, err
}

// NovaNotaDoCelular é a nota que o celular cria ("Criar uma nota...").
type NovaNotaDoCelular struct {
	Titulo, Corpo     string
	Cor               *string
	Fixada, Arquivada bool
	// Marcadores: ids que têm de existir (MarcadorVivo).
	Marcadores []string
}

// PlanejarNovaNota é a mutação de criar a nota id no topo do grupo dela (topo
// é a menor posição do grupo, "" se vazio). A nota sem título e sem corpo é
// ErrEmptyNote; o resto da validação é a do push (DecideNote).
func PlanejarNovaNota(id string, n NovaNotaDoCelular, topo string, now time.Time) (Mutation, error) {
	if strings.TrimSpace(n.Titulo) == "" && strings.TrimSpace(n.Corpo) == "" {
		return Mutation{}, ErrEmptyNote
	}
	k, err := notas.PosicaoEntre("", topo)
	if err != nil {
		return Mutation{}, fmt.Errorf("%w: %s", ErrInvalidNote, CodeInvalidField)
	}
	marcadores := n.Marcadores
	if marcadores == nil {
		marcadores = []string{}
	}
	raw, err := json.Marshal(map[string]any{
		"titulo": n.Titulo, "corpo": n.Corpo, "cor": n.Cor, "fixada": n.Fixada, "arquivada": n.Arquivada,
		"posicao": k, "marcadores": marcadores, "created_at": Normalize(now).Format(time.RFC3339Nano),
	})
	if err != nil {
		return Mutation{}, fmt.Errorf("%w: %s", ErrInvalidNote, CodeInvalidField)
	}
	return Mutation{MutationID: NewUUIDv7(), EntityType: EntityNote, EntityID: id, Operation: OpUpsert, Payload: raw}, nil
}

// ResultadoDaNota traduz o que DecideNote respondeu a um pedido do celular:
// a recusa vira o erro da rota; o conflict, o selo; o applied, mesclado
// quando o conteúdo que ficou não é o enviado.
func ResultadoDaNota(out Outcome, enviado *Conteudo) (NotaGravada, error) {
	switch out.Result.Status {
	case StatusRejected:
		if out.Result.Code == CodeInvalidBase {
			return NotaGravada{}, ErrVersionConflict
		}
		return NotaGravada{}, fmt.Errorf("%w: %s", ErrInvalidNote, out.Result.Code)
	case StatusGone:
		return NotaGravada{}, ErrNoteNotFound
	case StatusConflict:
		return NotaGravada{Nota: out.NextNote, NoSelo: true}, nil
	}
	g := NotaGravada{Nota: out.NextNote}
	if enviado != nil && out.Write != WriteNone && (out.NextNote.Titulo != enviado.Titulo || out.NextNote.Corpo != enviado.Corpo) {
		g.Mesclada = true
	}
	return g, nil
}

// --- Os marcadores. ---

// nomeLivre diz se nome (já limpo) não é o de outro marcador, sem diferenciar
// maiúscula nem acento (a regra do PC). exceto é o próprio marcador.
func nomeLivre(nome string, existentes []Label, exceto string) bool {
	chave := notas.Dobrar(nome)
	for _, l := range existentes {
		if l.ID != exceto && notas.Dobrar(l.Nome) == chave {
			return false
		}
	}
	return true
}

// PlanejarMarcadorNovo é a mutação de criar o marcador id com o nome (limpo
// como no PC). existentes são os marcadores do usuário.
func PlanejarMarcadorNovo(id, nome string, existentes []Label, now time.Time) (Mutation, error) {
	limpo, ok := NomeDoMarcador(nome)
	if !ok {
		return Mutation{}, ErrInvalidLabelName
	}
	if !nomeLivre(limpo, existentes, "") {
		return Mutation{}, ErrNameInUse
	}
	raw, _ := json.Marshal(map[string]any{"nome": limpo, "created_at": Normalize(now).Format(time.RFC3339Nano)})
	return Mutation{MutationID: NewUUIDv7(), EntityType: EntityLabel, EntityID: id, Operation: OpUpsert, Payload: raw}, nil
}

// PlanejarRenomear é a mutação de renomear cur. O nome do celular vence, como o
// do PC (DecideLabel: sem selo); base é a versão que o celular conhece.
func PlanejarRenomear(cur Label, base int64, nome string, existentes []Label) (Mutation, bool, error) {
	limpo, ok := NomeDoMarcador(nome)
	if !ok {
		return Mutation{}, false, ErrInvalidLabelName
	}
	if !nomeLivre(limpo, existentes, cur.ID) {
		return Mutation{}, false, ErrNameInUse
	}
	if limpo == cur.Nome {
		return Mutation{}, true, nil
	}
	if base < 1 || base > cur.Version {
		base = cur.Version
	}
	raw, _ := json.Marshal(map[string]any{"nome": limpo})
	return Mutation{MutationID: NewUUIDv7(), EntityType: EntityLabel, EntityID: cur.ID, Operation: OpUpsert, BaseVersion: base, Payload: raw}, false, nil
}

// --- O "Ditar para nota" do celular. ---

// DestinoDoDitado é para onde o "Ditar para nota" levou o ditado: a nota que o
// recebeu (a de destino, ou a nova), quantas linhas entraram e, no
// acrescentar, o corpo de antes (para o Desfazer).
type DestinoDoDitado struct {
	NotaID string
	// Nova: nenhum título casou, e o ditado virou uma nota nova.
	Nova bool
	// Linhas: quantas linhas entraram na nota de destino (0 na nova).
	Linhas int
	// CorpoAntes: o corpo da nota de destino antes do ditado ("" na nova).
	CorpoAntes string
}

// PlanoDoParaNota é o que o "Ditar para nota" grava na nota: a mutação, a nota
// de destino (nil na nova) e o destino.
type PlanoDoParaNota struct {
	Mutation Mutation
	Alvo     *Note
	Destino  DestinoDoDitado
}

// PlanejarParaNota aplica a regra do "Ditar para nota" (notas.Escolher e
// notas.Acrescentar, as mesmas do Ctrl+Win+Alt do PC) ao texto final do ditado
// ditadoID, entre as notas ativas: o título no começo leva o resto ao fim da
// nota (como itens, se ela é lista de marcar); sem título, nasce a nota novaID
// com o ditado inteiro, sem título, com o ditado de origem, no topo das outras
// (topo é a menor posição delas). Se o título casa mas nada vira linha (não
// acontece com o resto que Escolher devolve), nasce a nota nova: nada se perde.
func PlanejarParaNota(ativas []Note, texto, ditadoID, novaID, topo string, now time.Time) (PlanoDoParaNota, error) {
	cands := make([]notas.Candidata, len(ativas))
	for i, n := range ativas {
		cands[i] = notas.Candidata{ID: n.ID, Titulo: n.Titulo, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
	}
	if i, resto := notas.Escolher(cands, texto); i >= 0 {
		alvo := ativas[i]
		if corpo, linhas := notas.Acrescentar(alvo.Corpo, resto); linhas > 0 {
			m, err := patchDaNota(alvo.ID, alvo.Version, map[string]any{"titulo": alvo.Titulo, "corpo": corpo})
			if err != nil {
				return PlanoDoParaNota{}, err
			}
			return PlanoDoParaNota{Mutation: m, Alvo: &alvo,
				Destino: DestinoDoDitado{NotaID: alvo.ID, Linhas: linhas, CorpoAntes: alvo.Corpo}}, nil
		}
	}
	k, err := notas.PosicaoEntre("", topo)
	if err != nil {
		return PlanoDoParaNota{}, fmt.Errorf("%w: %s", ErrInvalidNote, CodeInvalidField)
	}
	raw, err := json.Marshal(map[string]any{
		"titulo": "", "corpo": texto, "cor": nil, "fixada": false, "arquivada": false,
		"posicao": k, "marcadores": []string{}, "ditado_de_origem": ditadoID,
		"created_at": Normalize(now).Format(time.RFC3339Nano),
	})
	if err != nil {
		return PlanoDoParaNota{}, fmt.Errorf("%w: %s", ErrInvalidNote, CodeInvalidField)
	}
	return PlanoDoParaNota{
		Mutation: Mutation{MutationID: NewUUIDv7(), EntityType: EntityNote, EntityID: novaID, Operation: OpUpsert, Payload: raw},
		Destino:  DestinoDoDitado{NotaID: novaID, Nova: true},
	}, nil
}

// DestinoGravado lê, de um ditado que já foi para uma nota, o destino que o
// reenvio responde: a nota nova é a que tem o ditado como origem. As linhas e
// o corpo de antes não se sabem mais (o Desfazer do reenvio só devolve o
// ditado ao histórico, a não ser na nota nova intacta).
func DestinoGravado(t Transcription, n *Note) DestinoDoDitado {
	if t.VirouNota == nil {
		return DestinoDoDitado{}
	}
	d := DestinoDoDitado{NotaID: *t.VirouNota}
	if n != nil && n.DitadoDeOrigem != nil && *n.DitadoDeOrigem == t.ID {
		d.Nova = true
	}
	return d
}

// PedidoDeDesfazer é o Desfazer do aviso do "Ditar para nota": a versão da
// nota logo depois do ditado e o corpo de antes (os dois vieram na resposta do
// ditado). Sem eles, só a nota nova intacta sai.
type PedidoDeDesfazer struct {
	Versao     int64
	CorpoAntes *string
}

// Desfeito é o que o Desfazer fez: o ditado de volta ao histórico e, com a
// nota, NotaVoltou (ela voltou ao que era: apagada, se nasceu do ditado, ou
// com o corpo de antes) e NotaApagada (a nota nova saiu).
type Desfeito struct {
	Ditado      Transcription
	NotaVoltou  bool
	NotaApagada bool
}

// PlanoDoDesfazer é o que o Desfazer grava: a mutação da nota (nil: a nota
// fica como está) e a do ditado, que volta ao histórico.
type PlanoDoDesfazer struct {
	Nota        *Mutation
	NotaApagada bool
	Ditado      Mutation
}

// PlanejarDesfazer é o Desfazer do "Ditar para nota" (como o do "Virar nota",
// N3): o ditado sai da nota e volta ao histórico, e a nota volta a ser o que
// era se ninguém mexeu nela desde o ditado. A nota nova nascida do ditado e
// ainda na versão 1 sai (lápide); o acrescentar volta o corpo de antes se a
// versão é a do ditado. Mexida depois, a nota fica, e só o ditado volta.
// n é a nota ligada (nil: não existe mais).
func PlanejarDesfazer(t Transcription, n *Note, p PedidoDeDesfazer) (PlanoDoDesfazer, error) {
	if t.VirouNota == nil {
		return PlanoDoDesfazer{}, ErrNotLinked
	}
	raw, _ := json.Marshal(map[string]any{"virou_nota": nil})
	plano := PlanoDoDesfazer{Ditado: Mutation{
		MutationID: NewUUIDv7(), EntityType: EntityTranscription, EntityID: t.ID, Operation: OpUpsert,
		BaseVersion: t.Version, Payload: raw,
	}}
	if n == nil || n.DeletedAt != nil {
		return plano, nil
	}
	if n.DitadoDeOrigem != nil && *n.DitadoDeOrigem == t.ID {
		if n.Version == 1 {
			plano.Nota = &Mutation{MutationID: NewUUIDv7(), EntityType: EntityNote, EntityID: n.ID, Operation: OpDelete}
			plano.NotaApagada = true
		}
		return plano, nil
	}
	if p.CorpoAntes != nil && p.Versao == n.Version {
		m, err := patchDaNota(n.ID, n.Version, map[string]any{"titulo": n.Titulo, "corpo": *p.CorpoAntes})
		if err != nil {
			return PlanoDoDesfazer{}, err
		}
		plano.Nota = &m
	}
	return plano, nil
}
