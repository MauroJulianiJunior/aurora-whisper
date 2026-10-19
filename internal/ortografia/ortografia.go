// Package ortografia diz se uma palavra existe no português do Brasil,
// segundo o dicionário VERO do LibreOffice (vero/, MPL 2.0; ver
// vero/LEIAME.md).
//
// Serve a uma pergunta só, a da guarda de palavras do Polish (ADR-016,
// emenda de 18-out-2026): a palavra que o modelo corrigiu existia? Uma
// palavra que não existe ("vejante", "sasonalidade", "promptes") é erro de
// transcrição, e o Polish pode corrigi-la; uma que existe ("pagou",
// "compõe") pode ser o que a pessoa disse, e trocá-la muda o sentido.
//
// O dicionário vem no formato do Hunspell: as raízes (pt_BR.dic, 312 mil) e
// as regras de prefixo e sufixo (pt_BR.aff). Expandido, daria 10,5 milhões
// de formas e 158 MB; por isso a consulta desfaz o afixo da palavra e
// procura a raiz, como o Hunspell faz. O VERO não usa composição nem afixo
// de dois níveis, então o caminho é curto: raiz; sufixo; prefixo; prefixo
// e sufixo juntos (quando as duas regras admitem).
//
// O dicionário é carregado na primeira consulta, não no início do processo:
// o Polish só pergunta quando a saída do modelo não é a entrada menos
// palavras, e o Agent que nunca usa o Polish nunca o carrega.
package ortografia

import (
	"bufio"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed vero/pt_BR.aff.gz vero/pt_BR.dic.gz
var arquivos embed.FS

// flagProibida é a FORBIDDENWORD do pt_BR.aff: a raiz marcada com ela não
// existe, nem quando outra regra a gera ("ague", "agues").
const flagProibida = 'ý'

// bom é a marca de ordem de bytes que abre os dois arquivos do VERO.
var bom = string(rune(0xFEFF))

// separador divide a raiz das flags na tabela; a raiz nunca tem "/", que é
// o separador do próprio pt_BR.dic.
const separador = '/'

// elemento é uma posição da condição de uma regra: qualquer letra ("."), uma
// letra, ou um conjunto ("[aeiou]", "[^rs]").
type elemento struct {
	qualquer bool
	nega     bool
	runas    []rune
}

func (e elemento) casa(r rune) bool {
	if e.qualquer {
		return true
	}
	for _, x := range e.runas {
		if x == r {
			return !e.nega
		}
	}
	return e.nega
}

// condicao é a condição de uma regra, lida da esquerda para a direita. A do
// sufixo vale para o FIM da palavra, a do prefixo para o COMEÇO.
type condicao []elemento

func lerCondicao(s string) (condicao, error) {
	if s == "." {
		return nil, nil
	}
	var c condicao
	runas := []rune(s)
	for i := 0; i < len(runas); i++ {
		switch runas[i] {
		case '.':
			c = append(c, elemento{qualquer: true})
		case '[':
			fim := i + 1
			for fim < len(runas) && runas[fim] != ']' {
				fim++
			}
			if fim == len(runas) {
				return nil, fmt.Errorf("condição sem ']': %q", s)
			}
			conj := runas[i+1 : fim]
			e := elemento{}
			if len(conj) > 0 && conj[0] == '^' {
				e.nega, conj = true, conj[1:]
			}
			e.runas = append([]rune(nil), conj...)
			c = append(c, e)
			i = fim
		default:
			c = append(c, elemento{runas: []rune{runas[i]}})
		}
	}
	return c, nil
}

func (c condicao) casaNoFim(w string) bool {
	runas := []rune(w)
	if len(runas) < len(c) {
		return false
	}
	base := len(runas) - len(c)
	for i, e := range c {
		if !e.casa(runas[base+i]) {
			return false
		}
	}
	return true
}

func (c condicao) casaNoComeco(w string) bool {
	runas := []rune(w)
	if len(runas) < len(c) {
		return false
	}
	for i, e := range c {
		if !e.casa(runas[i]) {
			return false
		}
	}
	return true
}

// regra é uma linha SFX ou PFX: tira "tira" da raiz, põe "poe" no lugar,
// se a raiz satisfaz a condição. cruza diz se a classe admite prefixo e
// sufixo juntos (o "Y" do cabeçalho da classe).
type regra struct {
	flag  rune
	cruza bool
	tira  string
	poe   string
	cond  condicao
}

// dicionario é o VERO carregado.
type dicionario struct {
	// raizes leva cada raiz às flags dela (as de homônimos, juntas).
	raizes tabela
	// proibidas são as raízes com a flag proibida.
	proibidas map[string]bool
	// sufixos e prefixos indexam as regras pelo que elas PÕEM: a consulta
	// parte da palavra pronta.
	sufixos  map[string][]regra
	prefixos map[string][]regra
}

var (
	carga     sync.Once
	carregado *dicionario
	erroCarga error
)

func obter() (*dicionario, error) {
	carga.Do(func() { carregado, erroCarga = carregar() })
	return carregado, erroCarga
}

func abrirGz(nome string) (io.ReadCloser, error) {
	f, err := arquivos.Open(nome)
	if err != nil {
		return nil, err
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{z, f}, nil
}

func carregar() (*dicionario, error) {
	d := &dicionario{
		proibidas: map[string]bool{},
		sufixos:   map[string][]regra{},
		prefixos:  map[string][]regra{},
	}
	if err := d.lerAfixos("vero/pt_BR.aff.gz"); err != nil {
		return nil, fmt.Errorf("ortografia: pt_BR.aff: %w", err)
	}
	if err := d.lerRaizes("vero/pt_BR.dic.gz"); err != nil {
		return nil, fmt.Errorf("ortografia: pt_BR.dic: %w", err)
	}
	return d, nil
}

func (d *dicionario) lerAfixos(nome string) error {
	r, err := abrirGz(nome)
	if err != nil {
		return err
	}
	defer r.Close()
	type cabecalho struct {
		prefixo, cruza bool
	}
	classes := map[rune]cabecalho{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(strings.TrimPrefix(sc.Text(), bom))
		if len(f) < 4 || (f[0] != "SFX" && f[0] != "PFX") {
			continue
		}
		flag, _ := utf8.DecodeRuneInString(f[1])
		cab, visto := classes[flag]
		if !visto { // "SFX a Y 12": o cabeçalho da classe
			classes[flag] = cabecalho{prefixo: f[0] == "PFX", cruza: f[2] == "Y"}
			continue
		}
		tira, poe, cond := f[2], f[3], "."
		if len(f) > 4 {
			cond = f[4]
		}
		// A flag de continuação ("Alagoas/Ý") só marca NOSUGGEST no VERO: não
		// muda se a palavra existe.
		poe, _, _ = strings.Cut(poe, "/")
		if tira == "0" {
			tira = ""
		}
		if poe == "0" {
			poe = ""
		}
		c, err := lerCondicao(cond)
		if err != nil {
			return err
		}
		rg := regra{flag: flag, cruza: cab.cruza, tira: tira, poe: poe, cond: c}
		if cab.prefixo {
			d.prefixos[poe] = append(d.prefixos[poe], rg)
		} else {
			d.sufixos[poe] = append(d.sufixos[poe], rg)
		}
	}
	return sc.Err()
}

func (d *dicionario) lerRaizes(nome string) error {
	r, err := abrirGz(nome)
	if err != nil {
		return err
	}
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	primeira := true
	var lidas []entrada
	for sc.Scan() {
		linha := strings.TrimSpace(strings.TrimPrefix(sc.Text(), bom))
		if primeira { // a contagem de raízes
			primeira = false
			continue
		}
		raiz, flags, _ := strings.Cut(linha, "/")
		// As 119 entradas de várias palavras ("água de coco") não respondem a
		// pergunta de uma palavra só.
		if raiz == "" || strings.ContainsFunc(raiz, unicode.IsSpace) {
			continue
		}
		if strings.ContainsRune(flags, flagProibida) {
			d.proibidas[raiz] = true
			continue
		}
		lidas = append(lidas, entrada{raiz, flags})
	}
	if err := sc.Err(); err != nil {
		return err
	}
	d.raizes = montarTabela(lidas)
	return nil
}

func (d *dicionario) temFlag(raiz string, flag rune) bool {
	flags, ok := d.raizes.flags(raiz)
	return ok && strings.ContainsRune(flags, flag)
}

type entrada struct{ raiz, flags string }

// tabela guarda as raízes em ordem, numa string só: "raiz/flags" uma
// depois da outra, e ini aponta o começo de cada uma. Num map, as 312 mil
// raízes ocupariam uns 33 MB; assim, uns 7 MB, com busca binária.
type tabela struct {
	dados string
	ini   []uint32
}

func montarTabela(lidas []entrada) tabela {
	slices.SortStableFunc(lidas, func(a, b entrada) int { return strings.Compare(a.raiz, b.raiz) })
	var b strings.Builder
	ini := make([]uint32, 0, len(lidas))
	for i, e := range lidas {
		if i > 0 && lidas[i-1].raiz == e.raiz { // homônimo: as flags vão juntas
			b.WriteString(e.flags)
			continue
		}
		if i > 0 {
			b.WriteByte(separador)
		}
		ini = append(ini, uint32(b.Len()))
		b.WriteString(e.raiz)
		b.WriteByte(separador)
		b.WriteString(e.flags)
	}
	return tabela{dados: b.String(), ini: ini}
}

// entradaEm devolve a raiz e as flags da i-ésima entrada.
func (t tabela) entradaEm(i int) (raiz, flags string) {
	fim := len(t.dados)
	if i+1 < len(t.ini) {
		fim = int(t.ini[i+1]) - 1
	}
	raiz, flags, _ = strings.Cut(t.dados[t.ini[i]:fim], string(separador))
	return raiz, flags
}

func (t tabela) flags(raiz string) (string, bool) {
	i, achou := sort.Find(len(t.ini), func(i int) int {
		r, _ := t.entradaEm(i)
		return strings.Compare(raiz, r)
	})
	if !achou {
		return "", false
	}
	_, f := t.entradaEm(i)
	return f, true
}

// gerada diz se w sai de uma raiz do dicionário: ela mesma, com um sufixo,
// com um prefixo ou com os dois.
func (d *dicionario) gerada(w string) bool {
	if d.proibidas[w] {
		return false
	}
	if _, ok := d.raizes.flags(w); ok {
		return true
	}
	if d.porSufixo(w, 0) {
		return true
	}
	for k := 0; k <= len(w); k++ {
		if k < len(w) && !utf8.RuneStart(w[k]) {
			continue
		}
		for _, p := range d.prefixos[w[:k]] {
			base := p.tira + w[k:]
			if !p.cond.casaNoComeco(base) {
				continue
			}
			if d.temFlag(base, p.flag) {
				return true
			}
			if p.cruza && d.porSufixo(base, p.flag) {
				return true
			}
		}
	}
	return false
}

// porSufixo diz se w é uma raiz com um sufixo. Com comPrefixo diferente de
// zero, a raiz tem de ter também a flag desse prefixo, e a regra do sufixo
// tem de admitir o cruzamento.
func (d *dicionario) porSufixo(w string, comPrefixo rune) bool {
	for k := 0; k <= len(w); k++ {
		ini := len(w) - k
		if ini < len(w) && !utf8.RuneStart(w[ini]) {
			continue
		}
		for _, s := range d.sufixos[w[ini:]] {
			if comPrefixo != 0 && !s.cruza {
				continue
			}
			raiz := w[:ini] + s.tira
			if !s.cond.casaNoFim(raiz) || !d.temFlag(raiz, s.flag) {
				continue
			}
			if comPrefixo == 0 || d.temFlag(raiz, comPrefixo) {
				return true
			}
		}
	}
	return false
}

// Existe diz se a palavra existe no português do Brasil. A caixa não conta
// ("Brasília" e "brasília" existem; o dicionário guarda o nome próprio com
// maiúscula). A palavra com hífen existe se existe inteira ("e-mail") ou se
// cada parte existe ("fim-de-semana").
//
// Na falha de carga do dicionário (o arquivo embutido corrompido, o que um
// teste impede), Existe diz que SIM: quem pergunta para liberar uma
// correção recebe "não libere". Errar para o lado de cair no Smart é o
// combinado das guardas.
func Existe(palavra string) bool {
	d, err := obter()
	if err != nil {
		return true
	}
	return d.existe(palavra)
}

// SoNomeProprio diz se a palavra só existe com a inicial maiúscula: é nome
// de pessoa, lugar ou marca ("Débora", "Brasília"), e não palavra comum.
// Na falha de carga, diz que SIM, pelo mesmo motivo de Existe.
func SoNomeProprio(palavra string) bool {
	d, err := obter()
	if err != nil {
		return true
	}
	f := formas(palavra)
	return len(f) == 2 && !d.gerada(f[0]) && d.gerada(f[1])
}

// ExisteComum diz se a palavra existe escrita em minúscula: é palavra comum
// ("base", "pesquisa"), mesmo que também seja nome ("Rosa"). Na falha de
// carga, diz que NÃO: quem pergunta para liberar a minúscula não libera.
func ExisteComum(palavra string) bool {
	d, err := obter()
	if err != nil {
		return false
	}
	min := strings.ToLower(palavra)
	if min == "" {
		return false
	}
	if d.gerada(min) {
		return true
	}
	if !strings.Contains(min, "-") {
		return false
	}
	for _, p := range strings.Split(min, "-") {
		if p == "" || !d.gerada(p) {
			return false
		}
	}
	return true
}

// ConheceComoNome diz se o VERO tem a palavra com a inicial maiúscula, como
// nome próprio ("Maria", "Ana", "Natal", "Brasil"), mesmo que ela também
// exista como palavra comum. Na falha de carga, diz que SIM.
func ConheceComoNome(palavra string) bool {
	d, err := obter()
	if err != nil {
		return true
	}
	f := formas(palavra)
	return d.gerada(f[len(f)-1])
}

//go:embed nomes/prenomes.txt nomes/sobrenomes.txt nomes/lugares.txt
var arquivosDeNomes embed.FS

var (
	cargaNomes sync.Once
	nomes      map[string]bool
)

// EhNomeDePessoa diz se a palavra é um prenome ou sobrenome comum no Brasil,
// ou o nome de um município ou estado de uma palavra só (nomes/LEIAME.md),
// sem distinguir caixa nem acento: "João", "joao", "Silva", "Rosa",
// "Recife". Serve para o que o VERO não diz: ele tem "joão", "silva" e
// "recife" como palavras comuns, mas quem dita "o João" fala de uma pessoa.
func EhNomeDePessoa(palavra string) bool {
	cargaNomes.Do(func() {
		nomes = map[string]bool{}
		for _, arq := range []string{"nomes/prenomes.txt", "nomes/sobrenomes.txt", "nomes/lugares.txt"} {
			dados, err := arquivosDeNomes.ReadFile(arq)
			if err != nil {
				continue // a lista que falta não protege, e o teste a confere
			}
			for _, n := range strings.Fields(string(dados)) {
				nomes[n] = true
			}
		}
	})
	return nomes[semAcento(strings.ToLower(palavra))]
}

// semAcento tira acento e cedilha, como nas listas de nomes ("joao",
// "goncalves").
var tiraAcento = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "î", "i", "ì", "i", "ï", "i",
	"ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
	"ú", "u", "û", "u", "ù", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

func semAcento(w string) string { return tiraAcento.Replace(w) }

// Disponivel diz se o dicionário carregou. Existe já é seguro sem ele; isto
// é para o teste e para o diagnóstico.
func Disponivel() error {
	_, err := obter()
	return err
}

func (d *dicionario) existe(palavra string) bool {
	if palavra == "" {
		return false
	}
	for _, forma := range formas(palavra) {
		if d.gerada(forma) {
			return true
		}
	}
	if strings.Contains(palavra, "-") {
		partes := strings.Split(palavra, "-")
		for _, p := range partes {
			if p == "" {
				return false
			}
			ok := false
			for _, forma := range formas(p) {
				ok = ok || d.gerada(forma)
			}
			if !ok {
				return false
			}
		}
		return true
	}
	return false
}

// formas são as grafias a procurar: minúscula e com a inicial maiúscula.
func formas(palavra string) []string {
	min := strings.ToLower(palavra)
	r, n := utf8.DecodeRuneInString(min)
	titulo := string(unicode.ToUpper(r)) + min[n:]
	if titulo == min {
		return []string{min}
	}
	return []string{min, titulo}
}
