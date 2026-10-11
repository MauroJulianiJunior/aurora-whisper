package dictionary

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Este arquivo é o lado PURO do aprendizado pelo painel: acrescentar uma
// variante a um termo, e tirá-la de volta (Desfazer), sobre uma Semente em
// memória. Quem lê e grava o arquivo é o Arquivo (arquivo.go); quem troca o
// dicionário do Agent em execução é o cmd/agent. Separar decidir de gravar
// é o que deixa cada regra testável sem tocar em disco.
//
// # O que a pessoa decidiu (30-set-2026)
//
//   - só um clique dela ensina (nunca sozinho);
//   - o aprendido mora no dicionario.json, com a origem "painel";
//   - acento ou maiúscula sozinhos não se aprendem;
//   - a variante vale em qualquer aplicativo: nenhum contexto de janela é
//     guardado, e por isso a variante aprendida nunca é "restrita".
//
// Quem chama deve ter aplicado as regras de ELEGIBILIDADE da correção (troca
// curta, forma do ASR, sem número...): isso é do motor de sugestão. As
// guardas daqui são as de INTEGRIDADE do arquivo: nada vazio, nada igual ao
// termo, nada que já pertença a outro termo.

// OrigemPainel marca o que o painel gravou: em alias_origem (por variante)
// e em EntradaSemente.Origem (por entrada criada por ele). É o que a
// nuvem, na etapa 5, vai chamar de "aprendido".
const OrigemPainel = "painel"

// PrioridadeTermoAprendido é a prioridade de um termo que o aprendizado
// cria. É a mesma do AcrescentarAlias (dictlearn): alta o bastante para o
// termo valer no Smart, baixa o bastante para não atropelar o que a pessoa
// priorizou à mão. Fica abaixo do corte do vocabulário do ASR, de propósito:
// o prompt do ASR só muda por decisão manual.
const PrioridadeTermoAprendido = 50

// maxRunasAprendido é um teto de INTEGRIDADE do arquivo, e não uma regra de
// aprendizado: as regras (até 4 palavras e 30 letras) são do motor de
// sugestão, e este teto só impede que uma chamada torta grave um parágrafo
// no dicionário.
const maxRunasAprendido = 80

// MaxPalavrasVariante é o tamanho máximo de uma variante, em palavras (as
// sequências de letras e dígitos, como o Smart as conta). O Smart só procura
// frases de até 4 palavras (maxPhraseWords, em internal/resolver, e um teste
// de lá confere que os dois valores são o mesmo): uma variante maior seria
// gravada e nunca agiria, e "aprendido" seria mentira, como no termo inativo.
const MaxPalavrasVariante = 4

// Os erros de quem chama (a API do painel traduz cada um). Nenhum traz o
// texto da variante: o texto ditado não vai parar em log por um erro.
var (
	// ErrVazio: termo ou variante sem nada que se compare (vazio, ou só
	// pontuação).
	ErrVazio = errors.New("dictionary: termo e variante não podem ser vazios")
	// ErrInvalido: texto grande demais (mais de 80 caracteres, ou variante de
	// mais de MaxPalavrasVariante palavras) ou com caractere de controle.
	ErrInvalido = errors.New("dictionary: termo ou variante grande demais ou com caractere de controle")
	// ErrSemNovidade: a variante é o próprio termo, ou só difere dele em
	// maiúscula, acento, pontuação ou espaço. O match exato já resolve isso;
	// e ensinar só o acento quebra texto certo ("está" faz "esta casa" virar
	// "está casa").
	ErrSemNovidade = errors.New("dictionary: a variante é o próprio termo, só muda maiúscula, acento ou espaço")
	// ErrJaExiste: a variante já é variante desse mesmo termo.
	ErrJaExiste = errors.New("dictionary: a variante já é variante deste termo")
	// ErrVarianteDeOutroTermo: a variante já pertence a outro termo. Aceitar
	// apagaria a regra anterior sem avisar. Vem como *ErroVarianteDeOutroTermo.
	ErrVarianteDeOutroTermo = errors.New("dictionary: a variante já pertence a outro termo do dicionário")
	// ErrTermoEVarianteDeOutro: o termo que o painel criaria já é variante de
	// outro termo. Criá-lo sombrearia essa regra (o termo novo casa exato, e
	// leva a melhor sobre o alias dele quando a prioridade é maior) e a
	// regra anterior deixaria de valer sem aviso. Vem como
	// *ErroTermoEVarianteDeOutro.
	ErrTermoEVarianteDeOutro = errors.New("dictionary: o termo novo já é variante de outro termo do dicionário")
	// ErrTermoInativo: o termo está desligado no dicionário, e uma variante
	// dele nunca agiria — "aprendido" seria mentira.
	ErrTermoInativo = errors.New("dictionary: o termo está inativo no dicionário")
	// ErrNaoAgiria: a variante seria gravada, mas o Smart não a aplicaria a um
	// ditado. Acontece quando outro termo leva a melhor na busca (a prioridade
	// vem antes do casamento: um termo de prioridade maior com casamento
	// fonético fraco passa na frente do alias confirmado de um termo de
	// prioridade 50), ou quando uma palavra da variante já é termo do
	// dicionário e veta a frase ("pedro das", com o termo DAS). É o mesmo
	// motivo de ErrTermoInativo: "aprendido" seria mentira. Quem a confere é
	// Arquivo.ValidarAprendida (o Agent roda o Smart sobre a variante
	// sozinha); o dicionário sozinho não sabe resolver.
	ErrNaoAgiria = errors.New("dictionary: a variante não agiria: o dicionário não a aplicaria a um ditado")
	// ErrNaoAprendida: Desfazer só desfaz o que o painel aprendeu. Variante
	// que não existe, ou que a pessoa ou o dictlearn cadastraram, não sai por
	// aqui.
	ErrNaoAprendida = errors.New("dictionary: não há variante aprendida pelo painel com esse texto")
	// ErrGravadoSemTroca: o arquivo FOI gravado (Aprender ou Desfazer valeram),
	// mas o dicionário em memória não pôde ser trocado, e a mudança só vale
	// depois de reiniciar o Agent. Não é falha: o Aprendida que acompanha o
	// erro é válido, e a tela tem de dizer que aprendeu (ou desfez) e que
	// falta reiniciar, e não que "não deu para aprender", porque a lista do
	// painel já mostra o par. É o único erro de Aprendizado que vem junto com
	// um Aprendida preenchido.
	ErrGravadoSemTroca = errors.New("dictionary: gravado, mas o dicionário em memória não foi trocado; vale depois de reiniciar o Agent")
)

// Aprendida é uma variante que o painel ensinou: Variante vira Termo.
type Aprendida struct {
	// Termo é a forma canônica como está no dicionário (a do termo que já
	// existia, e não a caixa que a chamada trouxe).
	Termo    string
	Variante string
}

// ErroVarianteDeOutroTermo diz a quem a variante já pertence, para a tela
// poder explicar ("SF já vira ECD"). errors.Is(err, ErrVarianteDeOutroTermo)
// é verdadeiro.
type ErroVarianteDeOutroTermo struct {
	// Dono é a forma canônica do termo que já tem a variante.
	Dono string
	// ETermo é verdadeiro quando a variante não é alias de Dono: é o próprio
	// Dono (um termo do dicionário).
	ETermo bool
}

func (e *ErroVarianteDeOutroTermo) Error() string { return ErrVarianteDeOutroTermo.Error() }

func (e *ErroVarianteDeOutroTermo) Is(alvo error) bool { return alvo == ErrVarianteDeOutroTermo }

// ErroTermoEVarianteDeOutro diz de quem o termo já é variante, para a tela
// poder explicar ("SF já vira ECF; não dá para criar SF como termo").
// errors.Is(err, ErrTermoEVarianteDeOutro) é verdadeiro. Como no
// ErroVarianteDeOutroTermo, o Dono é um termo do dicionário: quem trata o
// erro não o loga.
type ErroTermoEVarianteDeOutro struct {
	// Dono é a forma canônica do termo que tem o termo novo como variante.
	Dono string
}

func (e *ErroTermoEVarianteDeOutro) Error() string { return ErrTermoEVarianteDeOutro.Error() }

func (e *ErroTermoEVarianteDeOutro) Is(alvo error) bool { return alvo == ErrTermoEVarianteDeOutro }

// Aprendizado é o que a API do painel chama. Quem a cumpre grava o
// dicionario.json e troca o dicionário em memória na hora.
//
// Seguro de qualquer goroutine. Os erros são os de cima (errors.Is), mais
// ErrSemArquivo, ErrArquivoInvalido e ErrUsuarioDiferente, de arquivo.go.
//
// Com ErrGravadoSemTroca, Aprender e Desfazer devolvem o Aprendida preenchido:
// o arquivo mudou, só a troca em memória falhou. Com qualquer outro erro, o
// Aprendida é o zero e nada foi gravado.
type Aprendizado interface {
	// Aprender acrescenta variante ao termo (criando o termo, com
	// PrioridadeTermoAprendido, se ele não existir) e devolve o que ficou
	// gravado. "ecf" cai no termo "ECF" que já existe.
	Aprender(ctx context.Context, termo, variante string) (Aprendida, error)
	// Desfazer tira do dicionário uma variante que o painel aprendeu. Sem
	// mais nada ter mudado, deixa o arquivo idêntico, byte a byte, ao de
	// antes de aprendê-la.
	Desfazer(ctx context.Context, variante string) (Aprendida, error)
	// Aprendidas lista o que o painel aprendeu, na ordem do arquivo. Nunca
	// nil.
	Aprendidas(ctx context.Context) ([]Aprendida, error)
}

// AprenderVariante devolve a semente com variante acrescentada ao termo, e
// o que ficou gravado. Não muda s, e não escreve em disco.
//
// Se o termo existe (a comparação ignora maiúscula, acento e espaço), a
// variante entra na lista Alias dele, que herda a restrição de domínio que
// a pessoa deu à entrada. Se não existe, nasce uma entrada nova, sem
// domínio, e que age só pela variante (EntradaSemente.Origem, e
// DictionaryEntry.AliasOnly). Em ambos os casos alias_origem[variante] =
// "painel".
//
// A entrada nova pode NÃO ser o termo de uma variante de outro termo
// (ErrTermoEVarianteDeOutro), e a variante não pode ter mais de
// MaxPalavrasVariante palavras (ErrInvalido).
//
// Uma chave antiga em alias_origem com o mesmo texto da variante (uma
// origem órfã, de um alias que já não está em termo nenhum) é sobrescrita
// por "painel", e o Desfazer a apaga: a procedência antiga se perde, e essa
// é a única exceção à volta idêntica do Desfazer.
func (s Semente) AprenderVariante(termo, variante string) (Semente, Aprendida, error) {
	return s.aprenderEm(termo, variante, false)
}

// aprenderEm é AprenderVariante com o lugar da variante: em alias (emRestrito
// falso, o do painel) ou em alias_restrito de um termo que já existe (o PC que
// recebe da nuvem um par restrito a um domínio cujo termo, aqui, não é
// restrito; ver lote.go). As integridades são as mesmas nos dois. O termo novo
// só nasce com a variante em alias: emRestrito com um termo que não existe é
// ErrInvalido (quem chama não pede isso).
func (s Semente) aprenderEm(termo, variante string, emRestrito bool) (Semente, Aprendida, error) {
	termo, err := limparAprendido(termo)
	if err != nil {
		return s, Aprendida{}, err
	}
	variante, err = limparAprendido(variante)
	if err != nil {
		return s, Aprendida{}, err
	}
	if mesmaCoisa(termo, variante) {
		return s, Aprendida{}, ErrSemNovidade
	}
	if contarPalavras(variante) > MaxPalavrasVariante {
		return s, Aprendida{}, ErrInvalido
	}

	alvo := -1
	for i, e := range s.Entradas {
		if mesmaCoisa(e.Canonica, termo) {
			alvo = i
			break
		}
	}

	// A variante não pode pertencer a outro termo, nem ser outro termo.
	// Comparar como o Search compara (Normalize e depois sem espaços): o que
	// o Search confundiria, aqui também é o mesmo.
	for _, e := range s.Entradas {
		if mesmaCoisa(e.Canonica, termo) {
			if jaTemMesma(e.Alias, variante) || jaTemMesma(e.AliasRestrito, variante) {
				return s, Aprendida{}, ErrJaExiste
			}
			continue
		}
		if mesmaCoisa(e.Canonica, variante) {
			return s, Aprendida{}, &ErroVarianteDeOutroTermo{Dono: e.Canonica, ETermo: true}
		}
		if jaTemMesma(e.Alias, variante) || jaTemMesma(e.AliasRestrito, variante) {
			return s, Aprendida{}, &ErroVarianteDeOutroTermo{Dono: e.Canonica}
		}
	}

	// O termo novo também não pode ser variante de outro termo: a regra
	// daquele termo deixaria de valer sem aviso.
	if alvo < 0 {
		for _, e := range s.Entradas {
			if jaTemMesma(e.Alias, termo) || jaTemMesma(e.AliasRestrito, termo) {
				return s, Aprendida{}, &ErroTermoEVarianteDeOutro{Dono: e.Canonica}
			}
		}
	}

	copia := s.copiaProfunda()
	var canonica string
	if alvo >= 0 {
		e := copia.Entradas[alvo]
		if e.Inativa {
			return s, Aprendida{}, ErrTermoInativo
		}
		if emRestrito {
			e.AliasRestrito = inserirSemReordenar(e.AliasRestrito, variante)
		} else {
			e.Alias = inserirSemReordenar(e.Alias, variante)
		}
		copia.Entradas[alvo] = e
		canonica = e.Canonica
	} else {
		if emRestrito {
			return s, Aprendida{}, ErrInvalido
		}
		copia.Entradas = append(copia.Entradas, EntradaSemente{
			Canonica:   termo,
			Prioridade: PrioridadeTermoAprendido,
			Alias:      []string{variante},
			Origem:     OrigemPainel,
		})
		canonica = termo
	}
	if copia.AliasOrigem == nil {
		copia.AliasOrigem = map[string]string{}
	}
	copia.AliasOrigem[variante] = OrigemPainel
	return copia, Aprendida{Termo: canonica, Variante: variante}, nil
}

// DesfazerVariante devolve a semente sem a variante que o painel aprendeu,
// e o que saiu. Não muda s, e não escreve em disco.
//
// Só sai variante cuja origem é "painel": o que a pessoa ou o dictlearn
// cadastraram não se desfaz por aqui (ErrNaoAprendida). A entrada some
// junto quando o painel a criou (Origem "painel") e ela ficou vazia e
// intocada (sem alias, na prioridade padrão, sem domínio, idioma, restrição
// ou desligamento). Uma entrada que a pessoa mexeu à mão depois fica.
//
// Desfazer a única coisa que mudou deixa a semente como estava antes de
// AprenderVariante: a lista de alias nunca é reordenada, e nada sobra em
// alias_origem. A igualdade de bytes do ARQUIVO vale quando ele está no
// formato que Gravar escreve (ver Arquivo).
func (s Semente) DesfazerVariante(variante string) (Semente, Aprendida, error) {
	variante = strings.Join(strings.Fields(variante), " ")
	if variante == "" {
		return s, Aprendida{}, ErrVazio
	}

	copia := s.copiaProfunda()
	for i := range copia.Entradas {
		e := copia.Entradas[i]
		var achada string
		var ok bool
		if e.Alias, achada, ok = tirarDoPainel(e.Alias, variante, s.AliasOrigem); !ok {
			e.AliasRestrito, achada, ok = tirarDoPainel(e.AliasRestrito, variante, s.AliasOrigem)
		}
		if !ok {
			continue
		}

		delete(copia.AliasOrigem, achada)
		if len(copia.AliasOrigem) == 0 {
			copia.AliasOrigem = nil
		}
		if entradaDoPainelVazia(e) {
			copia.Entradas = append(copia.Entradas[:i], copia.Entradas[i+1:]...)
		} else {
			copia.Entradas[i] = e
		}
		return copia, Aprendida{Termo: e.Canonica, Variante: achada}, nil
	}
	return s, Aprendida{}, ErrNaoAprendida
}

// VariantesDoPainel lista o que o painel aprendeu, na ordem do arquivo (as
// entradas e, dentro delas, os alias). Nunca devolve nil.
func (s Semente) VariantesDoPainel() []Aprendida {
	out := []Aprendida{}
	for _, e := range s.Entradas {
		for _, lista := range [][]string{e.Alias, e.AliasRestrito} {
			for _, a := range lista {
				if s.AliasOrigem[a] == OrigemPainel {
					out = append(out, Aprendida{Termo: e.Canonica, Variante: a})
				}
			}
		}
	}
	return out
}

// tirarDoPainel tira de as a variante que bate com alvo E veio do painel,
// preservando a ordem das que ficam. Devolve as inalteradas (e false) se não
// houver.
func tirarDoPainel(as []string, alvo string, origem map[string]string) ([]string, string, bool) {
	for j, a := range as {
		if origem[a] != OrigemPainel || !mesmaCoisa(a, alvo) {
			continue
		}
		return append(as[:j], as[j+1:]...), a, true
	}
	return as, "", false
}

// entradaDoPainelVazia diz se Desfazer deve apagar a entrada inteira: foi o
// painel quem a criou, e nada nela foi tocado desde então.
func entradaDoPainelVazia(e EntradaSemente) bool {
	return e.Origem == OrigemPainel &&
		len(e.Alias) == 0 && len(e.AliasRestrito) == 0 &&
		e.Prioridade == PrioridadeTermoAprendido &&
		e.Dominio == "" && e.Idioma == "" && !e.Restrito && !e.Inativa
}

// copiaProfunda copia o que AprenderVariante e DesfazerVariante mexem: as
// entradas, as listas de alias e o mapa de origens. Dominios não é mexido e
// fica compartilhado.
func (s Semente) copiaProfunda() Semente {
	c := s
	if s.Entradas != nil {
		c.Entradas = make([]EntradaSemente, len(s.Entradas))
		for i, e := range s.Entradas {
			e.Alias = append([]string(nil), e.Alias...)
			e.AliasRestrito = append([]string(nil), e.AliasRestrito...)
			c.Entradas[i] = e
		}
	}
	if s.AliasOrigem != nil {
		c.AliasOrigem = make(map[string]string, len(s.AliasOrigem)+1)
		for k, v := range s.AliasOrigem {
			c.AliasOrigem[k] = v
		}
	}
	return c
}

// inserirSemReordenar põe alias em as (uma cópia que é nossa). Se a lista
// está em ordem alfabética, como o dictlearn a deixa, a variante entra no
// lugar dela, e o diff do arquivo continua estável. Se a pessoa a ordenou à
// mão de outro jeito, a variante vai para o fim: reordenar a lista toda
// mexeria no que a pessoa arrumou, e o Desfazer não voltaria ao arquivo de
// antes.
func inserirSemReordenar(as []string, alias string) []string {
	if !sort.StringsAreSorted(as) {
		return append(as, alias)
	}
	i := sort.SearchStrings(as, alias)
	as = append(as, "")
	copy(as[i+1:], as[i:])
	as[i] = alias
	return as
}

// mesmaCoisa compara como o Search: Normalize (caixa, acento, pontuação) e
// depois sem espaços ("DCTF Web" e "DCTFWeb" são o mesmo termo).
func mesmaCoisa(a, b string) bool {
	return CollapseSpaces(Normalize(a)) == CollapseSpaces(Normalize(b))
}

func jaTemMesma(as []string, alias string) bool {
	for _, a := range as {
		if mesmaCoisa(a, alias) {
			return true
		}
	}
	return false
}

// contarPalavras conta as sequências de letras e dígitos de s: a mesma
// medida com que o Smart tokeniza o texto ditado.
func contarPalavras(s string) int {
	n, dentro := 0, false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !dentro {
				n++
			}
			dentro = true
		} else {
			dentro = false
		}
	}
	return n
}

// limparAprendido deixa s como vai para o arquivo: espaços internos em um
// só, sem as bordas. Recusa o que não tem nada comparável e o que não cabe
// num dicionário.
func limparAprendido(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if Normalize(s) == "" {
		return "", ErrVazio
	}
	if utf8.RuneCountInString(s) > maxRunasAprendido {
		return "", ErrInvalido
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", ErrInvalido
		}
	}
	return s, nil
}

// Ausentes conta o que s tem e base não: termos (forma canônica que base não
// tem) e variantes (alias de um termo que as duas têm, e que o termo de base
// não tem). Só contagens: é para dizer "há 3 termos e 2 variantes a
// trazer" sem citar nada do dicionário.
//
// O asrstage usa para avisar quando o dicionário do repositório (onde o
// dictlearn grava por padrão) tem o que o bin/dicionario.json, que vence, não
// tem.
func (s Semente) Ausentes(base Semente) (termos, variantes int) {
	for _, e := range s.Entradas {
		var dele *EntradaSemente
		for i := range base.Entradas {
			if mesmaCoisa(base.Entradas[i].Canonica, e.Canonica) {
				dele = &base.Entradas[i]
				break
			}
		}
		if dele == nil {
			termos++
			continue
		}
		for _, lista := range [][]string{e.Alias, e.AliasRestrito} {
			for _, a := range lista {
				if !jaTemMesma(dele.Alias, a) && !jaTemMesma(dele.AliasRestrito, a) {
					variantes++
				}
			}
		}
	}
	return termos, variantes
}
