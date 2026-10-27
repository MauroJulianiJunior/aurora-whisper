package storage

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Este arquivo é o contrato das notas e dos marcadores locais (ADR-022,
// fase N1; docs/ai-spec/05-data-model.md, tabelas local_notes, local_labels
// e local_note_labels).
//
// # O sync (N7)
//
// Toda gravação local de nota ou marcador põe o marcador da entidade na
// sync_outbox na MESMA transação (nota_sync.go: emitirNota,
// emitirMarcadorDeNota e a lápide do apagar de vez), e o motor
// (internal/sync) só os lê depois de a nuvem dizer que conhece notas
// (ChaveNuvemNotas). O que vem da nuvem é gravado por Fila.AplicarTudo, que
// nunca põe marcador.

// Erros sentinela das notas e dos marcadores (além de ErrNotFound e
// ErrVersionConflict, de transcription.go). Chamadores comparam com
// errors.Is.
var (
	// ErrCorInvalida: a cor não é uma das CoresDaNota.
	ErrCorInvalida = errors.New("storage: cor da nota inválida")
	// ErrNotaVazia: criar uma nota sem título e sem corpo.
	ErrNotaVazia = errors.New("storage: a nota está vazia")
	// ErrTituloGrande e ErrCorpoGrande: o texto passa do teto.
	ErrTituloGrande = errors.New("storage: título da nota grande demais")
	ErrCorpoGrande  = errors.New("storage: corpo da nota grande demais")
	// ErrTextoInvalido: caractere nulo no título, no corpo ou no nome.
	ErrTextoInvalido = errors.New("storage: texto com caractere inválido")
	// ErrNotaNaLixeira: editar, marcar ou arquivar uma nota que está na
	// lixeira. A pessoa restaura antes.
	ErrNotaNaLixeira = errors.New("storage: a nota está na lixeira")
	// ErrNotaForaDaLixeira: apagar de vez uma nota que não está na lixeira.
	// Apagar de vez é só do que já foi para a lixeira.
	ErrNotaForaDaLixeira = errors.New("storage: a nota não está na lixeira")
	// ErrTokenInvalido: o token do "Criar uma nota" fora de TokenDaNotaValido.
	ErrTokenInvalido = errors.New("storage: token da nota inválido")
	// ErrDitadoNaLixeira: "Virar nota" num ditado que está na lixeira.
	ErrDitadoNaLixeira = errors.New("storage: o ditado está na lixeira")
	// ErrDitadoJaVirouNota: "Virar nota" num ditado que já virou nota. Dois
	// cliques, ou duas abas: a segunda não cria uma segunda nota.
	ErrDitadoJaVirouNota = errors.New("storage: o ditado já virou nota")
	// ErrNotaNaoVeioDeDitado: desfazer o "Virar nota" de uma nota que não
	// nasceu de um ditado, ou cujo ditado já voltou ao histórico (o Desfazer
	// repetido).
	ErrNotaNaoVeioDeDitado = errors.New("storage: a nota não está virada de um ditado")
	// ErrVizinhoInvalido: mover uma nota para entre duas vizinhas que não valem:
	// uma não existe, está na lixeira, é de outro grupo (fixadas e outras se
	// ordenam cada uma no seu; ativas e Arquivo também), é a própria nota, ou as
	// duas estão fora de ordem (a lista da tela estava desatualizada).
	ErrVizinhoInvalido = errors.New("storage: vizinha inválida para mover a nota")
	// ErrMoverSemVizinho: mover sem dizer nenhuma vizinha (antes e depois vazios).
	ErrMoverSemVizinho = errors.New("storage: mover pede ao menos uma vizinha")
	// ErrNomeDeMarcadorInvalido: nome vazio, grande demais ou com quebra de
	// linha.
	ErrNomeDeMarcadorInvalido = errors.New("storage: nome de marcador inválido")
	// ErrMarcadorDuplicado: já existe um marcador com esse nome (sem
	// diferenciar maiúscula nem acento) entre os não apagados.
	ErrMarcadorDuplicado = errors.New("storage: já existe um marcador com esse nome")
)

// PrefixoDaNotaNova marca, em local_transcriptions.virou_nota, o ditado feito
// dentro de uma nota que ainda não existe (o "Criar uma nota" aberto): o que vem
// depois é o token que o painel inventou para aquela caixa.
const PrefixoDaNotaNova = "nova:"

// TokenDaNotaValido diz se s serve de token do "Criar uma nota": de 1 a 64
// caracteres de [A-Za-z0-9_-] (um UUID cabe). Nada de espaço, dois-pontos nem
// curinga: o token entra numa comparação de texto no banco.
func TokenDaNotaValido(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		ok := r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// Os tetos de texto. O corpo tem o mesmo teto do texto do ditado (e da
// nuvem, MaxTextoBytes): o PC não aceita o que a nuvem recusaria. O título e
// o nome do marcador são decisão da N1 (a ADR não fixa): o título é uma
// linha, e o nome do marcador é curto como no Keep (50 caracteres).
const (
	MaxTituloDaNotaBytes = 1 << 10
	MaxCorpoDaNotaBytes  = MaxTextoBytes
	MaxNomeDoMarcador    = 50 // em caracteres, não em bytes
)

// CoresDaNota são os oito nomes de cor (ADR-022, E5). A cor é um nome, não
// um hex: cada tema do painel tem o seu tom. Sem cor é nil.
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

// Nota é a nota local: título (pode ser vazio), corpo (uma linha por bloco,
// com o prefixo do tipo; o banco não interpreta), cor, fixada, arquivada e os
// marcadores. Nunca carrega áudio nem contexto de outras janelas (AC-027,
// AC-028).
type Nota struct {
	ID     string
	UserID string
	Titulo string
	Corpo  string
	// Cor é um dos CoresDaNota, ou nil.
	Cor       *string
	Fixada    bool
	Arquivada bool
	// Marcadores são os ids dos marcadores da nota (os não apagados), na
	// ordem do nome. Nunca nil.
	Marcadores []string
	// Posicao é a ordem manual (ADR-022, "Ordem manual (N2b)"): um índice
	// fracionário (posicao.go). Dentro do grupo (fixadas, ou as outras), a menor
	// chave é o topo. Só Criar, fixar/desafixar, arquivar/desarquivar, restaurar
	// da lixeira e Mover a mudam; editar o texto, a cor ou os marcadores não.
	Posicao string
	// DitadoDeOrigem é o id do ditado de que a nota nasceu ("Virar nota", N3).
	DitadoDeOrigem *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// DeletedAt e PurgeAt: a lixeira (30 dias, a dos ditados). A nota não
	// tem a retenção de 6 meses.
	DeletedAt *time.Time
	PurgeAt   *time.Time
	Version   int64
	// SyncState, ServerVersion e LastSyncError são do sync (N7): a situação
	// (SituacaoNormal, SituacaoRecusado), a versão da nuvem que o PC conhece
	// (nil: a nuvem não tem a nota, ou o PC não sabe a versão) e o código da
	// recusa.
	SyncState     string
	ServerVersion *int64
	LastSyncError *string
	// Conflito é o selo (N7): o título e o corpo que perderam um conflito de
	// conteúdo na nuvem. nil sem selo.
	Conflito *ConflitoDaNota
	// CorpoNuvem é o corpo da última versão que a nuvem confirmou (o
	// corpo_base do envio do conteúdo); nil quando o PC não sabe.
	CorpoNuvem *string
}

// ConflitoDaNota é o selo da nota: a versão do título e do corpo que perdeu
// um conflito na nuvem, quem a escreveu (nil: o aparelho saiu da conta) e
// quando (o conflict_at, que a escolha devolve à nuvem).
type ConflitoDaNota struct {
	Titulo   string
	Corpo    string
	DeviceID *string
	Em       time.Time
}

// MarcadorDeNota é o marcador (rótulo) das notas: entidade própria, com nome único
// por pessoa sem diferenciar maiúscula nem acento. (O nome leva "DeNota" porque
// storage.Marcador já é o marcador da fila do sync.)
type MarcadorDeNota struct {
	ID            string
	UserID        string
	Nome          string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	Version       int64
	SyncState     string
	ServerVersion *int64
}

// NovaNota é o pedido de criar uma nota.
type NovaNota struct {
	UserID string
	Titulo string
	Corpo  string
	// Cor é "" (sem cor) ou uma das CoresDaNota.
	Cor            string
	Fixada         bool
	Arquivada      bool
	DitadoDeOrigem string
	// Marcadores são ids de marcadores do mesmo usuário, não apagados.
	Marcadores []string
	// Token é o do "Criar uma nota" que estava aberto enquanto a pessoa ditava
	// dentro dele (ADR-022, "Ditado feito dentro da nota"): na mesma transação em
	// que a nota nasce, os ditados marcados com "nova:<token>" passam a apontar
	// para ela. Vazio, nenhum. Não é a origem da nota (DitadoDeOrigem é só do
	// "Virar nota").
	Token string
}

// MudancaDaNota é o PATCH de uma nota: só muda o que está preenchido.
// Fixar, desafixar, arquivar, desarquivar e a cor passam todos por aqui, com
// a concorrência otimista de Atualizar.
type MudancaDaNota struct {
	Titulo *string
	Corpo  *string
	// MudarCor liga a troca da cor: Cor "" tira a cor.
	MudarCor  bool
	Cor       string
	Fixada    *bool
	Arquivada *bool
}

// Vazia diz se a mudança não pede nada.
func (m MudancaDaNota) Vazia() bool {
	return m.Titulo == nil && m.Corpo == nil && !m.MudarCor && m.Fixada == nil && m.Arquivada == nil
}

// VistaDasNotas é qual lista das notas se quer.
type VistaDasNotas string

const (
	// VistaAtivas: nem arquivadas nem na lixeira (a tela principal).
	VistaAtivas VistaDasNotas = "ativas"
	// VistaArquivo: arquivadas que não estão na lixeira.
	VistaArquivo VistaDasNotas = "arquivo"
	// VistaLixeira: as que estão na lixeira, arquivadas ou não.
	VistaLixeira VistaDasNotas = "lixeira"
)

// Valida diz se v é uma das três vistas.
func (v VistaDasNotas) Valida() bool {
	return v == VistaAtivas || v == VistaArquivo || v == VistaLixeira
}

// FiltroDeNotas descreve a lista das notas.
type FiltroDeNotas struct {
	// UserID é obrigatório.
	UserID string
	// Vista vazia é VistaAtivas.
	Vista VistaDasNotas
	// MarcadorID filtra pelas notas que têm esse marcador. Vazio não filtra.
	MarcadorID string
	// Busca procura no título e no corpo, sem diferenciar maiúscula nem
	// acento. Vazio não filtra.
	Busca string
}

// NotaRepository é o contrato de persistência das notas e dos marcadores.
//
// A lista é ordenada com as fixadas primeiro e, dentro de cada grupo, pela
// posição manual (Posicao asc: a menor é o topo; a criação mais recente
// desempata); na lixeira, pelo que foi apagado por último. Editar a nota não a
// move. Não há paginação: nota é coisa que a pessoa guardou de
// propósito, e a lista cabe inteira.
type NotaRepository interface {
	// Criar grava a nota (versão 1). Sem título e sem corpo, ErrNotaVazia.
	Criar(ctx context.Context, n NovaNota) (Nota, error)

	// Get devolve a nota userID/id, inclusive a da lixeira. ErrNotFound se
	// não existir ou for de outro usuário.
	Get(ctx context.Context, userID, id string) (Nota, error)

	// Listar devolve as notas do filtro. Nunca nil.
	Listar(ctx context.Context, f FiltroDeNotas) ([]Nota, error)

	// Atualizar aplica a mudança com concorrência otimista: baseVersion
	// precisa ser a versão atual, senão ErrVersionConflict. Numa nota da
	// lixeira, ErrNotaNaLixeira.
	Atualizar(ctx context.Context, userID, id string, baseVersion int64, m MudancaDaNota) (Nota, error)

	// SoftDelete move a nota para a lixeira: DeletedAt = now e PurgeAt = now
	// + 30 dias. Na que já está lá, não faz nada (não reinicia o prazo).
	SoftDelete(ctx context.Context, userID, id string, now time.Time) error

	// Restaurar tira a nota da lixeira. Na que não está lá, só devolve a nota.
	Restaurar(ctx context.Context, userID, id string) (Nota, error)

	// ApagarDeVez apaga a nota da lixeira para sempre. Fora da lixeira,
	// ErrNotaForaDaLixeira.
	ApagarDeVez(ctx context.Context, userID, id string) error

	// PurgeExpired apaga de vez as notas cujo PurgeAt venceu e devolve quantas.
	PurgeExpired(ctx context.Context, now time.Time) (int, error)

	// Mover é arrastar a nota (ADR-022, N2b): ela passa a ficar entre antesID e
	// depoisID, que são as vizinhas DEPOIS de soltar, no mesmo grupo (fixadas, ou
	// as outras; ativas, ou Arquivo). "" no lugar de uma delas é "nada de
	// lado": antesID vazio é o topo do grupo, depoisID vazio é o fim. As duas
	// vazias, ErrMoverSemVizinho. Vizinha que não vale: ErrVizinhoInvalido. Na
	// lixeira, ErrNotaNaLixeira. Grava só a posição desta nota (a versão sobe, a
	// hora de edição não); se ela já está entre as duas, não faz nada. Sem
	// base_version: mover não é editar o texto, e a posição sobrevive a quem
	// estava com a nota aberta. A posição entra na fila do sync
	// (CampoNotaPosicao).
	Mover(ctx context.Context, userID, id, antesID, depoisID string) (Nota, error)

	// Marcar põe o marcador na nota; Desmarcar tira. Os dois são
	// idempotentes e só sobem a versão da nota quando algo mudou. O marcador
	// precisa existir (não apagado) para Marcar.
	Marcar(ctx context.Context, userID, notaID, marcadorID string) (Nota, error)
	Desmarcar(ctx context.Context, userID, notaID, marcadorID string) (Nota, error)

	// CriarMarcador cria um marcador. Nome já usado (sem diferenciar
	// maiúscula nem acento) é ErrMarcadorDuplicado.
	CriarMarcador(ctx context.Context, userID, nome string) (MarcadorDeNota, error)

	// RenomearMarcador troca o nome, com concorrência otimista.
	RenomearMarcador(ctx context.Context, userID, id string, baseVersion int64, nome string) (MarcadorDeNota, error)

	// ApagarMarcador apaga o marcador (soft) e o tira de todas as notas; não
	// apaga nota. No que já foi apagado, não faz nada.
	ApagarMarcador(ctx context.Context, userID, id string) error

	// ListarMarcadores devolve os marcadores não apagados, por nome. Nunca nil.
	ListarMarcadores(ctx context.Context, userID string) ([]MarcadorDeNota, error)

	// VirarNota é a ação "Virar nota" do histórico (ADR-022, "Ditado vira
	// nota"): cria uma nota com o texto final do ditado (o bruto, se o final
	// está vazio) no corpo, sem título nem cor, com DitadoDeOrigem = ditadoID,
	// e marca o ditado (virou_nota = id da nota), numa transação só. Marcado,
	// o ditado sai do histórico SEM ir para a lixeira. Recusa o ditado
	// inexistente (ErrNotFound), na lixeira (ErrDitadoNaLixeira) e o que já
	// virou nota (ErrDitadoJaVirouNota). Na fila do sync, a nota nova e o
	// virou_nota do ditado (CampoVirouNota, num ditado que a nuvem tem).
	VirarNota(ctx context.Context, userID, ditadoID string) (Nota, error)

	// SoltarDitadosDaNotaNova devolve ao histórico os ditados feitos dentro de um
	// "Criar uma nota" que fechou sem criar a nota (virou_nota = "nova:<token>"
	// volta a nulo) e devolve quantos. Nada se perde calado: o texto que a pessoa
	// ditou e apagou continua no histórico.
	SoltarDitadosDaNotaNova(ctx context.Context, userID, token string) (int, error)

	// DesfazerVirarNota é o Desfazer do aviso: devolve o ditado ao histórico e
	// apaga a nota SE ela não foi editada desde que nasceu (versão 1, fora da
	// lixeira); editada, a nota fica (a pessoa já mexeu nela) e só o ditado
	// volta. Numa transação só. A nota inexistente é ErrNotFound (apagada de
	// vez, o ditado não volta); a que não nasceu de um ditado, ou cujo ditado
	// já voltou, é ErrNotaNaoVeioDeDitado.
	DesfazerVirarNota(ctx context.Context, userID, notaID string) (DesfeitoOVirarNota, error)

	// ResolverConflitoDaNota é a escolha do selo da nota (N7, como a do
	// ditado): só sobre a versão atual e só numa nota com selo (senão
	// ErrVersionConflict). EscolhaManter fica com o título e o corpo atuais;
	// EscolhaTrocar põe os do selo. As duas limpam o selo e sobem a versão, e
	// a escolha entra na fila do sync (selo_escolha, selo_em) numa nota que a
	// nuvem tem. Escolha fora das duas é ErrEscolhaInvalida.
	ResolverConflitoDaNota(ctx context.Context, userID, id string, baseVersion int64, escolha string) (Nota, error)
}

// DesfeitoOVirarNota é o resultado do DesfazerVirarNota.
type DesfeitoOVirarNota struct {
	// DitadoID é o ditado que voltou ao histórico.
	DitadoID string
	// NotaApagada: a nota não tinha sido editada e foi apagada de vez. Falso:
	// ela tinha sido editada e continua nas notas.
	NotaApagada bool
}

// validarTextoDaNota confere título e corpo contra os tetos e o caractere
// nulo. Os ponteiros nil são pulados.
func validarTextoDaNota(titulo, corpo *string) error {
	if titulo != nil {
		if len(*titulo) > MaxTituloDaNotaBytes {
			return ErrTituloGrande
		}
		if strings.ContainsRune(*titulo, 0) {
			return ErrTextoInvalido
		}
	}
	if corpo != nil {
		if len(*corpo) > MaxCorpoDaNotaBytes {
			return ErrCorpoGrande
		}
		if strings.ContainsRune(*corpo, 0) {
			return ErrTextoInvalido
		}
	}
	return nil
}

// validarCor aceita "" (sem cor) ou uma das CoresDaNota.
func validarCor(cor string) error {
	if cor != "" && !CorValida(cor) {
		return ErrCorInvalida
	}
	return nil
}

// nomeDoMarcador limpa o nome (espaços das bordas fora, sequências de espaço
// viram um) e o confere. Devolve o nome e a chave de unicidade.
func nomeDoMarcador(bruto string) (nome, chave string, err error) {
	if strings.ContainsRune(bruto, 0) {
		return "", "", ErrTextoInvalido
	}
	for _, r := range bruto {
		// Espaço comum se aceita; quebra de linha, tabulação e outros
		// controles, não.
		if r != ' ' && (unicode.IsControl(r) || r == ' ' || r == ' ') {
			return "", "", ErrNomeDeMarcadorInvalido
		}
	}
	nome = strings.Join(strings.Fields(bruto), " ")
	if nome == "" || utf8.RuneCountInString(nome) > MaxNomeDoMarcador {
		return "", "", ErrNomeDeMarcadorInvalido
	}
	return nome, dobrar(nome), nil
}

// semAcento leva as letras acentuadas do português (e as de nomes próprios
// latinos mais comuns) à letra de base. Só minúsculas: dobrar passa tudo
// para minúscula antes.
var semAcento = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// Dobrar é dobrar para quem está fora do pacote: o "ditar para nota" (ADR-022,
// N9) casa o título da nota na mesma forma em que a busca compara.
func Dobrar(s string) string { return dobrar(s) }

// dobrar põe s em minúsculas e sem acento: a forma em que se compara o nome
// do marcador e se busca no texto das notas. Não mexe em pontuação nem em
// espaço ("C#" e "C++" continuam diferentes).
func dobrar(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1 // o acento solto, de quem escreveu em NFD
		}
		r = unicode.ToLower(r)
		if f, ok := semAcento[r]; ok {
			return f
		}
		return r
	}, s)
}
