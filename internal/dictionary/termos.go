package dictionary

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Este arquivo é o lado PURO do "Adicionar termo" das Configurações (ADR-020):
// a pessoa digita a palavra certa ("RAG"), opcionalmente as grafias erradas
// que o reconhecimento de voz costuma escrever ("rague, hage"), e diz se a
// palavra vira DICA para o reconhecimento de voz (vai no prompt do ASR). E
// depois liga ou desliga a dica, ou remove o que pôs.
//
// Somado ao "Aprender" pelo histórico (painel.go), não no lugar dele. Quem lê e
// grava o arquivo é o Arquivo (arquivo_termos.go); quem troca o dicionário e o
// prompt do Agent em execução é o cmd/agent.
//
// # Nada novo no JSON (ADR-019)
//
// O termo da pessoa leva "origem": "adicionado" (um VALOR novo do campo que já
// existe) e cada grafia que ela digitou leva alias_origem[v] = "adicionado". A
// dica é a prioridade: com dica, PrioridadeDica; sem, PrioridadeTermoAprendido.
// Um Agent de antes carrega o arquivo e trata o termo como um termo comum.

// OrigemAdicionado marca o termo (EntradaSemente.Origem) e as variantes
// (alias_origem) que a pessoa pôs pelo "Adicionar termo".
const OrigemAdicionado = "adicionado"

// PrioridadeDica é a prioridade do termo adicionado COM a dica. Um termo
// adicionado com prioridade menor que esta nunca vai ao prompt do ASR
// (DisputaOPrompt), e um com esta ou mais vai antes de tudo
// (VocabularioTermos). Sem a dica, o termo fica com PrioridadeTermoAprendido.
const PrioridadeDica = 100

// Os tetos do "Adicionar termo".
const (
	// MaxTermosAdicionados é quantos termos a pessoa pode ter adicionado.
	MaxTermosAdicionados = 50
	// MaxVariantesPorPedido é quantas grafias erradas vão num Adicionar.
	MaxVariantesPorPedido = 5
	// MaxVariantesPorTermo é quantas grafias erradas que a pessoa digitou um
	// termo pode ter (as do painel e as do arquivo não contam).
	MaxVariantesPorTermo = 10
	// MaxRunasTermo é o tamanho máximo da palavra certa e de cada grafia.
	MaxRunasTermo = 40
	// MinRunasVariante é o tamanho mínimo de uma grafia errada: uma letra só
	// trocaria toda letra solta do texto.
	MinRunasVariante = 2
)

// Os erros do "Adicionar termo". Como os de painel.go, nenhum traz texto na
// mensagem: o que a tela precisa citar vem em campo, e quem trata o erro não o
// loga.
var (
	// ErrPalavraComum: a palavra é só de ligação, de polaridade ou número
	// ("de", "não", "2025"); ou, como grafia, tem palavra de polaridade ("não
	// rague") ou é só palavra funcional ("eu", "né"). Como termo ou como
	// variante, trocaria texto certo. Vem como *ErroPalavraComum.
	ErrPalavraComum = errors.New("dictionary: palavra comum demais para o dicionário")
	// ErrSemEspaco: a dica não cabe no que sobra do orçamento do prompt, contado
	// só sobre as dicas da pessoa. A dica nova nunca empurra outra dica dela.
	// Vem como *ErroSemEspaco.
	ErrSemEspaco = errors.New("dictionary: a dica não cabe no prompt do reconhecimento de voz")
	// ErrQuebraria: com a mudança, uma variante que agia deixaria de agir (o
	// termo de prioridade maior passa na frente dela na busca). Vem como
	// *ErroQuebraria. Quem confere é o Agent (Arquivo.ConferirMudanca).
	ErrQuebraria = errors.New("dictionary: a mudança faria uma variante que agia deixar de agir")
	// ErrNaoAdicionado: o termo não foi adicionado pelo painel, ou não tem
	// grafia que a pessoa pôs. Remover e a dica só mexem no que é dela.
	ErrNaoAdicionado = errors.New("dictionary: o termo não foi adicionado pelo painel")
	// ErrLimite: passou de um dos tetos. Vem como *ErroLimite.
	ErrLimite = errors.New("dictionary: passou do limite de termos ou de grafias")
	// ErrNadaAMudar: o pedido não muda nada (o termo já está assim).
	ErrNadaAMudar = errors.New("dictionary: o termo já está assim no dicionário")
	// ErrDicaDoDicionario: o pedido só pede a dica, num termo que já era do
	// dicionário de antes (sem origem), cuja dica não muda pelo painel
	// (decisão 3 da ADR-020). Dizer "já está assim" seria falso sobre a dica.
	ErrDicaDoDicionario = errors.New("dictionary: a dica de um termo do dicionário não muda pelo painel")
)

// ErroPalavraComum diz qual texto é comum demais. errors.Is(err,
// ErrPalavraComum) é verdadeiro.
type ErroPalavraComum struct {
	// Texto é a palavra certa ou a grafia, como foi limpa.
	Texto string
	// Motivo é o código de quem conferiu (o learning.Motivo, como texto):
	// "numero", "palavra_de_ligacao" ou, só na grafia, "muda_sentido".
	Motivo string
}

func (e *ErroPalavraComum) Error() string      { return ErrPalavraComum.Error() }
func (e *ErroPalavraComum) Is(alvo error) bool { return alvo == ErrPalavraComum }

// ErroSemEspaco diz quanto sobra do orçamento, contado sobre as dicas da
// pessoa (nunca negativo).
type ErroSemEspaco struct{ Sobram int }

func (e *ErroSemEspaco) Error() string      { return ErrSemEspaco.Error() }
func (e *ErroSemEspaco) Is(alvo error) bool { return alvo == ErrSemEspaco }

// ErroQuebraria diz qual variante, de qual termo, deixaria de agir. Com
// Variante igual a Termo, é a própria forma canônica do termo, escrita certa,
// que passaria a ser trocada por Vira quando o texto em volta citasse o termo
// mudado (a fonética corroborada pelo contexto).
type ErroQuebraria struct {
	Variante string
	Termo    string
	// Vira é o que a forma canônica passaria a virar; "" na variante.
	Vira string
}

func (e *ErroQuebraria) Error() string      { return ErrQuebraria.Error() }
func (e *ErroQuebraria) Is(alvo error) bool { return alvo == ErrQuebraria }

// ErroNaoAgiria diz qual grafia nova o dicionário não aplicaria a um ditado.
// errors.Is(err, ErrNaoAgiria) é verdadeiro.
type ErroNaoAgiria struct{ Variante string }

func (e *ErroNaoAgiria) Error() string      { return ErrNaoAgiria.Error() }
func (e *ErroNaoAgiria) Is(alvo error) bool { return alvo == ErrNaoAgiria }

// Limite diz qual teto o pedido passou.
type Limite string

const (
	// LimiteTermos: MaxTermosAdicionados.
	LimiteTermos Limite = "termos"
	// LimiteVariantesDoTermo: MaxVariantesPorTermo.
	LimiteVariantesDoTermo Limite = "variantes_do_termo"
	// LimiteVariantesDoPedido: MaxVariantesPorPedido.
	LimiteVariantesDoPedido Limite = "variantes_do_pedido"
)

// ErroLimite diz qual teto passou, e qual é ele.
type ErroLimite struct {
	Do     Limite
	Maximo int
}

func (e *ErroLimite) Error() string      { return ErrLimite.Error() }
func (e *ErroLimite) Is(alvo error) bool { return alvo == ErrLimite }

// ErroDaVariante diz de qual grafia é o erro de dentro (ErrSemNovidade,
// ErrJaExiste, *ErroVarianteDeOutroTermo, ErrVazio, ErrInvalido). Um pedido
// leva até MaxVariantesPorPedido grafias, e a tela tem de dizer qual não
// serviu. errors.Is e errors.As enxergam a causa.
type ErroDaVariante struct {
	Variante string
	// Termo é a forma canônica do termo do pedido, quando ele já existe (para
	// a tela dizer "“rague” já vira “RAG”"); "" quando o termo é novo.
	Termo string
	Causa error
}

func (e *ErroDaVariante) Error() string { return e.Causa.Error() }
func (e *ErroDaVariante) Unwrap() error { return e.Causa }

// ErroTermoInativoDe é o ErrTermoInativo com o termo, para a tela citar.
// errors.Is(err, ErrTermoInativo) é verdadeiro.
type ErroTermoInativoDe struct{ Termo string }

func (e *ErroTermoInativoDe) Error() string      { return ErrTermoInativo.Error() }
func (e *ErroTermoInativoDe) Is(alvo error) bool { return alvo == ErrTermoInativo }

// PedidoTermo é o que o "Adicionar termo" manda.
type PedidoTermo struct {
	// Termo é a palavra certa, como deve sair no texto ("RAG").
	Termo string
	// Variantes são as grafias erradas que o reconhecimento de voz costuma
	// escrever ("rague", "hage"). Opcional: de 0 a MaxVariantesPorPedido.
	Variantes []string
	// Dica liga a palavra no prompt do ASR. Num termo que já estava no
	// dicionário de antes (sem origem), não vale: a dica dele não muda pelo
	// painel. Com grafias, elas entram e a mudança diz DicaIgnorada; sem
	// grafias, o pedido é ErrDicaDoDicionario.
	Dica bool
}

// TermoAdicionado é uma linha da lista "Adicionados por você".
type TermoAdicionado struct {
	// Termo é a forma canônica, como está no arquivo.
	Termo string
	// Variantes são só as grafias que a pessoa pôs (origem "adicionado"), na
	// ordem do arquivo. Nunca nil.
	Variantes []string
	// Dica: o termo é dica da pessoa (EDicaDaPessoa). Falso no termo do
	// dicionário de antes, que não tem a dica mudada pelo painel.
	Dica bool
	// DoDicionario: o termo já estava no dicionário, e a pessoa só pôs
	// grafias nele. Remover tira só as grafias dela.
	DoDicionario bool
}

// Dicas é o orçamento do prompt contado sobre as dicas da pessoa.
type Dicas struct {
	// Usadas são as runas que as dicas dela ocupam (com o separador).
	Usadas int
	// Orcamento é OrcamentoRunasPadrao.
	Orcamento int
}

// ListaDeTermos é o que GET /api/termos devolve.
type ListaDeTermos struct {
	// Itens, na ordem do arquivo. Nunca nil.
	Itens []TermoAdicionado
	Dicas Dicas
}

// MudancaDeTermos é o que uma operação de termos fez.
type MudancaDeTermos struct {
	// Termo é a forma canônica do termo mexido, como ficou no arquivo.
	Termo string
	// JaExistia: no Adicionar, o termo já estava no dicionário.
	JaExistia bool
	// DicaIgnorada: no Adicionar, o pedido trazia a dica, e o termo já era do
	// dicionário de antes: as grafias entraram, a dica dele não mudou
	// (decisão 3 da ADR-020). A tela tem de dizer.
	DicaIgnorada bool
	// SairamDasDicas são os termos que estavam no prompt do ASR e saíram com
	// a mudança, sem contar o próprio termo. Nunca nil.
	SairamDasDicas []string
	// Novas são as variantes que a operação acrescentou: o Agent confere que
	// cada uma age (Arquivo.ConferirMudanca).
	Novas []Aprendida
	// Lista é a lista de depois da mudança.
	Lista ListaDeTermos
}

// Termos é o que a API do painel chama para o "Adicionar termo". Quem a cumpre
// grava o dicionario.json e troca o dicionário e o prompt do ASR em memória na
// hora. Separada de Aprendizado para os dublês de hoje não precisarem dela.
//
// Seguro de qualquer goroutine. Os erros são os de cima, os de painel.go e os
// de arquivo.go. Com ErrGravadoSemTroca, a MudancaDeTermos vem preenchida: o
// arquivo mudou, só a troca em memória falhou. Com qualquer outro erro, ela é o
// zero e nada foi gravado.
type Termos interface {
	ListarTermos(ctx context.Context) (ListaDeTermos, error)
	AdicionarTermo(ctx context.Context, p PedidoTermo) (MudancaDeTermos, error)
	DicaDoTermo(ctx context.Context, termo string, ligar bool) (MudancaDeTermos, error)
	RemoverTermo(ctx context.Context, termo string) (MudancaDeTermos, error)
}

// TermosAdicionados lista o que a pessoa pôs pelo "Adicionar termo": os termos
// de origem "adicionado" e os termos de antes que receberam grafias dela.
func (s Semente) TermosAdicionados() ListaDeTermos {
	out := ListaDeTermos{Itens: []TermoAdicionado{}, Dicas: s.dicas()}
	for _, e := range s.Entradas {
		vs := s.variantesAdicionadas(e)
		switch {
		case e.Origem == OrigemAdicionado:
			out.Itens = append(out.Itens, TermoAdicionado{
				Termo:     e.Canonica,
				Variantes: vs,
				Dica:      !e.Inativa && e.Prioridade >= PrioridadeDica,
			})
		case len(vs) > 0:
			out.Itens = append(out.Itens, TermoAdicionado{Termo: e.Canonica, Variantes: vs, DoDicionario: true})
		}
	}
	return out
}

// AdicionarTermo devolve a semente com o termo do pedido, e o que mudou. Não
// muda s, e não escreve em disco.
//
// palavraComum, se não nil, diz se um texto é comum demais (devolve o motivo,
// ou ""), com grafia verdadeiro para as grafias erradas: o Agent passa
// learning.Arriscada para o termo e learning.GrafiaArriscada, mais estrita
// (a polaridade em qualquer palavra, e as palavras funcionais), para a
// grafia. Vale para cada grafia e para a palavra certa quando ela VIRA termo
// agora (nova, ou a do painel adotada); o termo que já era do dicionário não
// é conferido ("DAS" é termo de verdade).
//
// Se o termo existe (a comparação ignora maiúscula, acento e espaço):
//   - de origem "adicionado": as grafias novas entram e a dica segue o pedido;
//   - de origem "painel": é adotado (a origem passa a "adicionado" e a
//     prioridade segue a dica), e as variantes do painel ficam como estão;
//   - de antes: as grafias entram, e a dica não muda (decisão 3 da ADR-020:
//     sem campo novo, Remover não teria onde achar a prioridade de antes). O
//     pedido com a dica diz isso: DicaIgnorada com grafias, e
//     ErrDicaDoDicionario sem elas.
func (s Semente) AdicionarTermo(p PedidoTermo, palavraComum func(texto string, grafia bool) string) (Semente, MudancaDeTermos, error) {
	termo, err := limparTermo(p.Termo, 1)
	if err != nil {
		return s, MudancaDeTermos{}, err
	}
	variantes, err := limparVariantes(p.Variantes, termo)
	if err != nil {
		return s, MudancaDeTermos{}, err
	}

	alvo := s.indiceDoTermo(termo)
	existia := alvo >= 0
	if existia && s.Entradas[alvo].Inativa {
		return s, MudancaDeTermos{}, &ErroTermoInativoDe{Termo: s.Entradas[alvo].Canonica}
	}
	if alvo < 0 {
		// O termo novo não pode ser variante de outro: a regra daquele termo
		// deixaria de valer sem aviso.
		for _, e := range s.Entradas {
			if jaTemMesma(e.Alias, termo) || jaTemMesma(e.AliasRestrito, termo) {
				return s, MudancaDeTermos{}, &ErroTermoEVarianteDeOutro{Dono: e.Canonica}
			}
		}
	}
	viraTermo := alvo < 0 || s.Entradas[alvo].Origem == OrigemPainel
	if palavraComum != nil {
		if viraTermo {
			if m := palavraComum(termo, false); m != "" {
				return s, MudancaDeTermos{}, &ErroPalavraComum{Texto: termo, Motivo: m}
			}
		}
		for _, v := range variantes {
			if m := palavraComum(v, true); m != "" {
				return s, MudancaDeTermos{}, &ErroPalavraComum{Texto: v, Motivo: m}
			}
		}
	}
	if err := s.conferirVariantesNovas(variantes, alvo); err != nil {
		return s, MudancaDeTermos{}, err
	}

	// Os tetos.
	if viraTermo && s.quantosAdicionados() >= MaxTermosAdicionados {
		return s, MudancaDeTermos{}, &ErroLimite{Do: LimiteTermos, Maximo: MaxTermosAdicionados}
	}
	ja := 0
	if alvo >= 0 {
		ja = len(s.variantesAdicionadas(s.Entradas[alvo]))
	}
	if ja+len(variantes) > MaxVariantesPorTermo {
		return s, MudancaDeTermos{}, &ErroLimite{Do: LimiteVariantesDoTermo, Maximo: MaxVariantesPorTermo}
	}

	copia := s.copiaProfunda()
	eraDica := false
	mudou := len(variantes) > 0
	deAntes := alvo >= 0 && s.Entradas[alvo].Origem != OrigemAdicionado && s.Entradas[alvo].Origem != OrigemPainel
	if deAntes && p.Dica && !mudou {
		return s, MudancaDeTermos{}, ErrDicaDoDicionario
	}
	if alvo < 0 {
		copia.Entradas = append(copia.Entradas, EntradaSemente{
			Canonica:   termo,
			Prioridade: prioridadeDaDica(p.Dica),
			Origem:     OrigemAdicionado,
		})
		alvo = len(copia.Entradas) - 1
		mudou = true
	} else {
		e := &copia.Entradas[alvo]
		eraDica = e.Origem == OrigemAdicionado && e.Prioridade >= PrioridadeDica
		switch e.Origem {
		case OrigemAdicionado:
			if eraDica != p.Dica {
				e.Prioridade = prioridadeDaDica(p.Dica)
				mudou = true
			}
		case OrigemPainel:
			e.Origem = OrigemAdicionado
			e.Prioridade = prioridadeDaDica(p.Dica)
			mudou = true
		}
	}
	if !mudou {
		return s, MudancaDeTermos{}, ErrNadaAMudar
	}

	e := &copia.Entradas[alvo]
	novas := make([]Aprendida, 0, len(variantes))
	for _, v := range variantes {
		e.Alias = inserirSemReordenar(e.Alias, v)
		if copia.AliasOrigem == nil {
			copia.AliasOrigem = map[string]string{}
		}
		copia.AliasOrigem[v] = OrigemAdicionado
		novas = append(novas, Aprendida{Termo: e.Canonica, Variante: v})
	}

	if e.Origem == OrigemAdicionado && e.Prioridade >= PrioridadeDica && !eraDica {
		if err := s.cabeNasDicas(copia); err != nil {
			return s, MudancaDeTermos{}, err
		}
	}
	m := mudancaEntre(s, copia, e.Canonica)
	m.JaExistia = existia
	m.DicaIgnorada = deAntes && p.Dica
	m.Novas = novas
	return copia, m, nil
}

// DicaDoTermo devolve a semente com a dica do termo ligada ou desligada. Só
// mexe no termo que a pessoa adicionou (ErrNaoAdicionado). Ligar a dica que
// não cabe é ErrSemEspaco. Pedir o estado em que o termo já está não é erro:
// a semente volta igual.
func (s Semente) DicaDoTermo(termo string, ligar bool) (Semente, MudancaDeTermos, error) {
	termo = strings.Join(strings.Fields(termo), " ")
	if Normalize(termo) == "" {
		return s, MudancaDeTermos{}, ErrVazio
	}
	alvo := s.indiceDoTermo(termo)
	if alvo < 0 || s.Entradas[alvo].Origem != OrigemAdicionado {
		return s, MudancaDeTermos{}, ErrNaoAdicionado
	}
	if s.Entradas[alvo].Inativa {
		return s, MudancaDeTermos{}, &ErroTermoInativoDe{Termo: s.Entradas[alvo].Canonica}
	}
	copia := s.copiaProfunda()
	e := &copia.Entradas[alvo]
	eraDica := e.Prioridade >= PrioridadeDica
	if eraDica != ligar {
		e.Prioridade = prioridadeDaDica(ligar)
	}
	if ligar && !eraDica {
		if err := s.cabeNasDicas(copia); err != nil {
			return s, MudancaDeTermos{}, err
		}
	}
	m := mudancaEntre(s, copia, e.Canonica)
	m.JaExistia = true
	return copia, m, nil
}

// RemoverTermo devolve a semente sem o que a pessoa pôs no termo: as grafias
// de origem "adicionado" e, se o termo é dela, o próprio termo. Não muda s, e
// não escreve em disco.
//
// Num termo dela que também tem variantes aprendidas pelo painel, o termo
// fica, de volta como "painel" e com PrioridadeTermoAprendido: essas
// variantes são da lista "Aprendido no painel", e saem pelo Desfazer de lá.
// Num termo do dicionário de antes, só saem as grafias dela.
func (s Semente) RemoverTermo(termo string) (Semente, MudancaDeTermos, error) {
	termo = strings.Join(strings.Fields(termo), " ")
	if Normalize(termo) == "" {
		return s, MudancaDeTermos{}, ErrVazio
	}
	alvo := s.indiceDoTermo(termo)
	if alvo < 0 {
		return s, MudancaDeTermos{}, ErrNaoAdicionado
	}
	copia := s.copiaProfunda()
	e := copia.Entradas[alvo]
	tiradas := 0
	tirar := func(as []string) []string {
		ficam := as[:0]
		for _, a := range as {
			if s.AliasOrigem[a] == OrigemAdicionado {
				delete(copia.AliasOrigem, a)
				tiradas++
				continue
			}
			ficam = append(ficam, a)
		}
		if len(ficam) == 0 {
			return nil
		}
		return ficam
	}
	e.Alias = tirar(e.Alias)
	e.AliasRestrito = tirar(e.AliasRestrito)

	switch {
	case e.Origem == OrigemAdicionado && s.temDoPainel(e):
		e.Origem = OrigemPainel
		e.Prioridade = PrioridadeTermoAprendido
		copia.Entradas[alvo] = e
	case e.Origem == OrigemAdicionado:
		copia.Entradas = append(copia.Entradas[:alvo], copia.Entradas[alvo+1:]...)
	case tiradas == 0:
		return s, MudancaDeTermos{}, ErrNaoAdicionado
	default:
		copia.Entradas[alvo] = e
	}
	if len(copia.AliasOrigem) == 0 {
		copia.AliasOrigem = nil
	}
	m := mudancaEntre(s, copia, e.Canonica)
	m.JaExistia = true
	return copia, m, nil
}

// mudancaEntre monta o que mudou de antes para depois: a lista de depois e os
// termos que saíram do prompt do ASR (sem o próprio termo).
func mudancaEntre(antes, depois Semente, termo string) MudancaDeTermos {
	m := MudancaDeTermos{Termo: termo, SairamDasDicas: []string{}, Lista: depois.TermosAdicionados()}
	agora := VocabularioTermos(depois.ParaEntradas(), OrcamentoRunasPadrao)
	for _, e := range VocabularioTermos(antes.ParaEntradas(), OrcamentoRunasPadrao) {
		if mesmaCoisa(e.CanonicalForm, termo) {
			continue
		}
		ficou := false
		for _, d := range agora {
			if d.CanonicalForm == e.CanonicalForm {
				ficou = true
				break
			}
		}
		if !ficou {
			m.SairamDasDicas = append(m.SairamDasDicas, e.CanonicalForm)
		}
	}
	return m
}

// cabeNasDicas confere que as dicas da pessoa em depois cabem no orçamento. A
// conta do que sobrava é a de s, o antes.
func (s Semente) cabeNasDicas(depois Semente) error {
	if runasDasDicas(depois.ParaEntradas()) <= OrcamentoRunasPadrao {
		return nil
	}
	return &ErroSemEspaco{Sobram: max(0, OrcamentoRunasPadrao-runasDasDicas(s.ParaEntradas()))}
}

func (s Semente) dicas() Dicas {
	return Dicas{Usadas: runasDasDicas(s.ParaEntradas()), Orcamento: OrcamentoRunasPadrao}
}

// conferirVariantesNovas aplica as recusas de integridade de AprenderVariante
// a cada grafia: nenhuma é já do termo, outro termo, ou de outro termo.
func (s Semente) conferirVariantesNovas(variantes []string, alvo int) error {
	for _, v := range variantes {
		for i, e := range s.Entradas {
			if i == alvo {
				if jaTemMesma(e.Alias, v) || jaTemMesma(e.AliasRestrito, v) {
					return &ErroDaVariante{Variante: v, Termo: e.Canonica, Causa: ErrJaExiste}
				}
				continue
			}
			if mesmaCoisa(e.Canonica, v) {
				return &ErroDaVariante{Variante: v, Causa: &ErroVarianteDeOutroTermo{Dono: e.Canonica, ETermo: true}}
			}
			if jaTemMesma(e.Alias, v) || jaTemMesma(e.AliasRestrito, v) {
				return &ErroDaVariante{Variante: v, Causa: &ErroVarianteDeOutroTermo{Dono: e.Canonica}}
			}
		}
	}
	return nil
}

// variantesAdicionadas são as grafias de e que a pessoa pôs, na ordem do
// arquivo. Nunca nil.
func (s Semente) variantesAdicionadas(e EntradaSemente) []string {
	out := []string{}
	for _, lista := range [][]string{e.Alias, e.AliasRestrito} {
		for _, a := range lista {
			if s.AliasOrigem[a] == OrigemAdicionado {
				out = append(out, a)
			}
		}
	}
	return out
}

// temDoPainel diz se e ainda tem variante que o painel aprendeu.
func (s Semente) temDoPainel(e EntradaSemente) bool {
	for _, lista := range [][]string{e.Alias, e.AliasRestrito} {
		for _, a := range lista {
			if s.AliasOrigem[a] == OrigemPainel {
				return true
			}
		}
	}
	return false
}

func (s Semente) quantosAdicionados() int {
	n := 0
	for _, e := range s.Entradas {
		if e.Origem == OrigemAdicionado {
			n++
		}
	}
	return n
}

// indiceDoTermo acha a entrada do termo, comparando como o Search compara, ou
// -1.
func (s Semente) indiceDoTermo(termo string) int {
	for i, e := range s.Entradas {
		if mesmaCoisa(e.Canonica, termo) {
			return i
		}
	}
	return -1
}

func prioridadeDaDica(dica bool) int {
	if dica {
		return PrioridadeDica
	}
	return PrioridadeTermoAprendido
}

// limparTermo deixa a palavra como vai para o arquivo. Quebra de linha e tab
// são recusadas ANTES de juntar os espaços: strings.Fields as viraria espaço
// sem avisar, e a palavra gravada não seria a que a pessoa digitou. min é o
// mínimo de runas.
func limparTermo(s string, min int) (string, error) {
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", ErrInvalido
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if Normalize(s) == "" {
		return "", ErrVazio
	}
	n := utf8.RuneCountInString(s)
	if n < min || n > MaxRunasTermo || contarPalavras(s) > MaxPalavrasVariante {
		return "", ErrInvalido
	}
	return s, nil
}

// limparVariantes limpa as grafias do pedido, na ordem: as em branco (o que
// sobra de "rague, , hage") saem, e as repetidas entre si também. Cada erro
// diz de qual grafia é (*ErroDaVariante).
func limparVariantes(vs []string, termo string) ([]string, error) {
	out := make([]string, 0, len(vs))
	for _, bruta := range vs {
		if strings.TrimSpace(bruta) == "" {
			continue
		}
		v, err := limparTermo(bruta, MinRunasVariante)
		if err != nil {
			return nil, &ErroDaVariante{Variante: strings.Join(strings.Fields(bruta), " "), Causa: err}
		}
		if mesmaCoisa(v, termo) {
			return nil, &ErroDaVariante{Variante: v, Causa: ErrSemNovidade}
		}
		if jaTemMesma(out, v) {
			continue
		}
		out = append(out, v)
	}
	if len(out) > MaxVariantesPorPedido {
		return nil, &ErroLimite{Do: LimiteVariantesDoPedido, Maximo: MaxVariantesPorPedido}
	}
	return out, nil
}
