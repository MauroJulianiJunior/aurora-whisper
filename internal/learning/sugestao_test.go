package learning_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
)

// par é o que um caso espera de uma sugestão, sem o contador.
type par struct{ De, Para, Termo string }

func pares(r learning.Resultado) []par {
	var out []par
	for _, s := range r.Sugestoes {
		out = append(out, par{s.De, s.Para, s.TermoExistente})
	}
	return out
}

// dicBase é um dicionário pequeno com o que a tabela precisa: um termo
// comum, um restrito a domínio com variante restrita, um com variante, um
// desativado. "SF" não é de ninguém aqui; o caso que precisa dele como
// variante usa comSF.
func dicBase() dictionary.Semente {
	return dictionary.Semente{
		Usuario: "mauro",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECF", Prioridade: 90, Dominio: "contabil"},
			{Canonica: "commit", Prioridade: 70, Dominio: "dev", Restrito: true,
				AliasRestrito: []string{"comite"}},
			{Canonica: "Wispr Flow", Prioridade: 80, Alias: []string{"whisper flow"}},
			{Canonica: "DCTFWeb", Prioridade: 90},
			{Canonica: "Supabase", Prioridade: 85},
			{Canonica: "Kubernetes", Prioridade: 60, Inativa: true},
		},
	}
}

// comSF é dicBase com "SF" como variante de Supabase.
func comSF() dictionary.Semente {
	d := dicBase()
	for i := range d.Entradas {
		if d.Entradas[i].Canonica == "Supabase" {
			d.Entradas[i].Alias = []string{"SF"}
		}
	}
	return d
}

// dicTermos tem termos de mais de uma palavra e termos cuja chave é palavra
// de ligação ("DAS"): os casos em que a troca conserta só um pedaço de um
// termo que o dicionário já tem.
func dicTermos() dictionary.Semente {
	return dictionary.Semente{
		Usuario: "mauro",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "EFD-Reinf", Prioridade: 100, Dominio: "contabil", Alias: []string{"EFDRinf"}},
			{Canonica: "Lucro Presumido", Prioridade: 80},
			{Canonica: "Simples Nacional", Prioridade: 85},
			{Canonica: "Ministério da Fazenda", Prioridade: 70},
			{Canonica: "Conselho Regional de Medicina", Prioridade: 60},
			{Canonica: "Conselho Federal de Contabilidade", Prioridade: 60},
			{Canonica: "Wispr Flow", Prioridade: 50},
			{Canonica: "eSocial", Prioridade: 95},
			{Canonica: "DAS", Prioridade: 90, Alias: []string{"DAAS"}},
			{Canonica: "Receita Federal", Prioridade: 70},
			{Canonica: "Receita Federal do Brasil", Prioridade: 70},
			{Canonica: "da Vinci", Prioridade: 40},
		},
	}
}

// sem é dicBase sem a entrada de forma canônica dada, para os casos em que
// aprender a troca CRIA o termo.
func sem(canonica string) dictionary.Semente {
	d := dicBase()
	var out []dictionary.EntradaSemente
	for _, e := range d.Entradas {
		if e.Canonica != canonica {
			out = append(out, e)
		}
	}
	d.Entradas = out
	return d
}

type caso struct {
	nome                 string
	bruto, antes, depois string
	dic                  dictionary.Semente
	want                 []par
	wantDescartes        []learning.Motivo
}

func rode(t *testing.T, cases []caso) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			r := learning.Sugerir(tc.bruto, tc.antes, tc.depois, tc.dic, nil)
			if got := pares(r); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sugestões = %+v, quer %+v (descartes %v)", got, tc.want, r.Descartes)
			}
			if !reflect.DeepEqual(r.Descartes, tc.wantDescartes) {
				t.Errorf("descartes = %v, quer %v", r.Descartes, tc.wantDescartes)
			}
		})
	}
}

// TestSugerir_Sugere: cada caso é uma edição real, com o bruto do ASR, o
// texto que o painel mostrava (já com o que o Smart e o Polish fizeram) e o
// que a pessoa salvou.
func TestSugerir_Sugere(t *testing.T) {
	rode(t, []caso{
		{
			nome:  "SF vira ECF, termo novo",
			bruto: "vou mandar o sf amanhã", antes: "Vou mandar o sf amanhã.", depois: "Vou mandar o ECF amanhã.",
			dic:  sem("ECF"),
			want: []par{{"sf", "ECF", ""}},
		},
		{
			nome:  "SF vira ECF, termo que já existe",
			bruto: "vou mandar o sf amanhã", antes: "Vou mandar o sf amanhã.", depois: "Vou mandar o ECF amanhã.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "ecf em minúscula cai no termo ECF que existe",
			bruto: "vou mandar o sf amanhã", antes: "Vou mandar o sf amanhã.", depois: "Vou mandar o ecf amanhã.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "o De é a forma do bruto, com a caixa do bruto",
			bruto: "Sf mandou o relatório", antes: "Sf mandou o relatório.", depois: "ECF mandou o relatório.",
			dic:  dicBase(),
			want: []par{{"Sf", "ECF", "ECF"}},
		},
		{
			nome:  "o SF e o RJ dão duas sugestões",
			bruto: "o sf e o rj", antes: "O sf e o rj.", depois: "O ECF e o RF.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}, {"rj", "RF", ""}},
		},
		{
			nome:  "transcrição vira descrição: troca curta de palavra comum sugere, quem barra é o clique",
			bruto: "a transcrição ficou pronta", antes: "A transcrição ficou pronta.", depois: "A descrição ficou pronta.",
			dic:  dicBase(),
			want: []par{{"transcrição", "descrição", ""}},
		},
		{
			nome:  "artigo junto da correção não entra no termo",
			bruto: "abri vispeflow agora", antes: "Abri vispeflow agora.", depois: "Abri o Wispr Flow agora.",
			dic:  dicBase(),
			want: []par{{"vispeflow", "Wispr Flow", "Wispr Flow"}},
		},
		{
			nome:  "artigo diferente nos dois lados da troca sai dos dois",
			bruto: "abri o sf agora", antes: "Abri o sf agora.", depois: "Abri a ECF agora.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "TEST-008: n oito n vira n8n",
			bruto: "configurei o n oito n", antes: "Configurei o n oito n.", depois: "Configurei o n8n.",
			dic:  dicBase(),
			want: []par{{"n oito n", "n8n", ""}},
		},
		{
			nome:  "palavra com dígito no meio não é número",
			bruto: "uso o vê dois", antes: "Uso o vê dois.", depois: "Uso o v2.",
			dic:  dicBase(),
			want: []par{{"vê dois", "v2", ""}},
		},
		{
			nome:  "espaço de termo composto, com o termo fora do dicionário",
			bruto: "enviei a dctf web", antes: "Enviei a dctf web.", depois: "Enviei a DCTFWeb.",
			dic:  sem("DCTFWeb"),
			want: []par{{"dctf web", "DCTFWeb", ""}},
		},
		{
			nome:  "ponto dentro do termo não é fim de frase",
			bruto: "uso o nodejs", antes: "Uso o nodejs.", depois: "Uso o Node.js.",
			dic:  dicBase(),
			want: []par{{"nodejs", "Node.js", ""}},
		},
		{
			nome:  "Polish já botou maiúscula, ponto e parágrafo: a pessoa só trocou o termo",
			bruto: "vou mandar o sf amanhã abraço", antes: "Vou mandar o sf amanhã.\n\nAbraço.", depois: "Vou mandar o ECF amanhã.\n\nAbraço.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "o sistema trocou e a pessoa trocou de novo: o De é o do bruto",
			bruto: "abri o whisper flow agora", antes: "Abri o Wispr Flow agora.", depois: "Abri o ECD agora.",
			dic: dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
				{Canonica: "Wispr Flow", Prioridade: 80}, // sem o alias: o De ainda não é de ninguém
			}},
			want: []par{{"whisper flow", "ECD", ""}},
		},
		{
			nome:  "uma troca e uma inserção: só a troca sugere",
			bruto: "vou mandar o sf amanhã", antes: "Vou mandar o sf amanhã.", depois: "Vou mandar o ECF amanhã cedo.",
			dic:           dicBase(),
			want:          []par{{"sf", "ECF", "ECF"}},
			wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga},
		},
		{
			nome:  "uma inserção, depois uma troca: os descartes saem na ordem do texto",
			bruto: "mande o sf amanhã", antes: "Mande o sf amanhã.", depois: "Mande já o ECF amanhã.",
			dic:           dicBase(),
			want:          []par{{"sf", "ECF", "ECF"}},
			wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga},
		},
		{
			nome:  "duas trocas com o mesmo De não pedem o alias duas vezes",
			bruto: "sf e sf", antes: "SF e SF.", depois: "ECF e ECF.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "artigo apagado junto da correção, na esquerda",
			bruto: "abri o sf agora", antes: "Abri o sf agora.", depois: "Abri ECF agora.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "artigo apagado junto da correção, na direita",
			bruto: "abri sf do relatório", antes: "Abri sf do relatório.", depois: "Abri ECF relatório.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "artigo diferente dos dois lados, na direita",
			bruto: "abri sf do relatório", antes: "Abri sf do relatório.", depois: "Abri ECF da relatório.",
			dic:  dicBase(),
			want: []par{{"sf", "ECF", "ECF"}},
		},
		{
			nome:  "a palavra de ligação do meio do termo fica",
			bruto: "falei com mefaz", antes: "Falei com mefaz.", depois: "Falei com o Ministério da Fazenda.",
			dic:  dicBase(),
			want: []par{{"mefaz", "Ministério da Fazenda", ""}},
		},
		{
			nome:  "espaço duplicado e tabulação no texto viram um espaço só, no De e no Para",
			bruto: "abri o vispe   flow", antes: "Abri o vispe   flow.", depois: "Abri o  Wispr\tFlux.",
			dic:  dicBase(),
			want: []par{{"vispe flow", "Wispr Flux", ""}},
		},
		{
			nome:  "o teto de caracteres conta o texto compactado, não os espaços a mais",
			bruto: "uso aaaaaaaaa    bbbbbbbbb    ccccccccc hoje", antes: "Uso aaaaaaaaa    bbbbbbbbb    ccccccccc hoje.",
			depois: "Uso novo hoje.",
			dic:    dicBase(),
			want:   []par{{"aaaaaaaaa bbbbbbbbb ccccccccc", "novo", ""}},
		},
		{
			nome:  "a forma canônica do dicionário vem sem os espaços das pontas",
			bruto: "abri o sf", antes: "Abri o sf.", depois: "Abri o ecf.",
			dic:  dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: "  ECF  "}}},
			want: []par{{"sf", "ECF", "ECF"}},
		},
	})
}

// TestSugerir_TermoDesativadoEDonoDasFormas: o termo desativado não corrige
// nada no ditado, mas o dicionário do Agent recusa a variante de QUALQUER
// termo, ligado ou não (ErroVarianteDeOutroTermo, ErroTermoEVarianteDeOutro,
// ErrTermoInativo). Sugerir o que ele sempre recusa mostraria um botão Aprender
// que só falha. Até a terceira revisão da etapa 3 o motor tratava o desativado
// como "não é de ninguém", e esses casos viravam sugestão.
func TestSugerir_TermoDesativadoEDonoDasFormas(t *testing.T) {
	inativo := []learning.Motivo{learning.MotivoTermoInativo}
	segredo := dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
		{Canonica: "Segredo", Alias: []string{"sf"}, Inativa: true},
		{Canonica: "ECF", Prioridade: 90},
	}}
	restrita := dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
		{Canonica: "Segredo", AliasRestrito: []string{"sf"}, Dominio: "dev", Restrito: true, Inativa: true},
		{Canonica: "ECF", Prioridade: 90},
	}}
	rode(t, []caso{
		{nome: "De é a forma canônica de termo desativado",
			bruto: "uso o kubernetes", antes: "Uso o kubernetes.", depois: "Uso o ECF.",
			dic: dicBase(), wantDescartes: inativo},
		{nome: "De é variante de termo desativado",
			bruto: "uso o k oito s", antes: "Uso o k oito s.", depois: "Uso o ECF.",
			dic: dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
				{Canonica: "Kubernetes", Inativa: true, Alias: []string{"k oito s"}},
			}},
			wantDescartes: inativo},
		{nome: "De é variante de termo desativado, e Para é um termo ativo",
			bruto: "mandei o sf amanhã", antes: "Mandei o sf amanhã.", depois: "Mandei o ECF amanhã.",
			dic: segredo, wantDescartes: inativo},
		{nome: "De é variante restrita de termo desativado",
			bruto: "mandei o sf amanhã", antes: "Mandei o sf amanhã.", depois: "Mandei o ECF amanhã.",
			dic: restrita, wantDescartes: inativo},
		{nome: "De é variante do mesmo termo de Para, desativado",
			bruto: "mandei o sf amanhã", antes: "Mandei o sf amanhã.", depois: "Mandei o Segredo amanhã.",
			dic: segredo, wantDescartes: inativo},
		{nome: "Para é variante de termo desativado",
			bruto: "mandei o ecd amanhã", antes: "Mandei o ecd amanhã.", depois: "Mandei o SF amanhã.",
			dic: segredo, wantDescartes: inativo},
		{nome: "Para é variante restrita de termo desativado",
			bruto: "mandei o ecd amanhã", antes: "Mandei o ecd amanhã.", depois: "Mandei o SF amanhã.",
			dic: restrita, wantDescartes: inativo},
		// O controle: o mesmo dicionário, com o termo ligado, dá o motivo de
		// sempre (variante de outro termo), e desligado não sugere nem
		// "de" nem "para" que nada tem a ver com o termo.
		{nome: "controle: o termo ligado é dono, como antes",
			bruto: "mandei o sf amanhã", antes: "Mandei o sf amanhã.", depois: "Mandei o ECF amanhã.",
			dic: ligado(segredo), wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "controle: o termo desativado não atrapalha outra troca",
			bruto: "mandei o ecd amanhã", antes: "Mandei o ecd amanhã.", depois: "Mandei o ECF amanhã.",
			dic: segredo, want: []par{{"ecd", "ECF", "ECF"}}},
	})
}

// TestSugerir_ConcordaComOAprenderDoDicionario: o motor descarta um termo
// desativado como dono exatamente onde o dicionário recusaria a variante
// (Semente.AprenderVariante), e o que ele sugere o dicionário aceita. Se um dos
// dois mudar sozinho, o painel volta a oferecer um botão que só falha (ou a
// esconder um que funcionaria).
func TestSugerir_ConcordaComOAprenderDoDicionario(t *testing.T) {
	segredo := dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
		{Canonica: "Segredo", Alias: []string{"sf"}, Inativa: true},
		{Canonica: "ECF", Prioridade: 90},
	}}
	kube := dicBase()
	for _, c := range []struct {
		nome, bruto, antes, depois string
		dic                        dictionary.Semente
		de, para                   string
	}{
		{"De é variante de desativado", "mandei o sf amanhã", "Mandei o sf amanhã.", "Mandei o ECF amanhã.", segredo, "sf", "ECF"},
		{"De é o mesmo termo de Para, desativado", "mandei o sf amanhã", "Mandei o sf amanhã.", "Mandei o Segredo amanhã.", segredo, "sf", "Segredo"},
		{"Para é variante de desativado", "mandei o ecd amanhã", "Mandei o ecd amanhã.", "Mandei o SF amanhã.", segredo, "ecd", "SF"},
		{"De é a forma canônica de desativado", "uso o kubernetes", "Uso o kubernetes.", "Uso o ECF.", kube, "kubernetes", "ECF"},
		{"Para é desativado", "uso o kubernet", "Uso o kubernet.", "Uso o Kubernetes.", kube, "kubernet", "Kubernetes"},
	} {
		t.Run("recusa/"+c.nome, func(t *testing.T) {
			if got := pares(learning.Sugerir(c.bruto, c.antes, c.depois, c.dic, nil)); len(got) != 0 {
				t.Errorf("o motor sugeriu %+v, e o dicionário recusaria", got)
			}
			if _, _, err := c.dic.AprenderVariante(c.para, c.de); err == nil {
				t.Errorf("AprenderVariante(%q, %q) aceitou: o motor descartou por engano", c.para, c.de)
			}
		})
	}
	for _, c := range []struct {
		nome, bruto, antes, depois string
		dic                        dictionary.Semente
	}{
		{"outra troca no dicionário com desativado", "mandei o ecd amanhã", "Mandei o ecd amanhã.", "Mandei o ECF amanhã.", segredo},
		{"termo novo", "vou mandar o sf amanhã", "Vou mandar o sf amanhã.", "Vou mandar o ECF amanhã.", sem("ECF")},
	} {
		t.Run("aceita/"+c.nome, func(t *testing.T) {
			got := pares(learning.Sugerir(c.bruto, c.antes, c.depois, c.dic, nil))
			if len(got) != 1 {
				t.Fatalf("o motor não sugeriu: %+v", got)
			}
			if _, _, err := c.dic.AprenderVariante(got[0].Para, got[0].De); err != nil {
				t.Errorf("o motor sugeriu %+v e o dicionário recusou: %v", got[0], err)
			}
		})
	}
}

// ligado devolve d com todas as entradas ligadas.
func ligado(d dictionary.Semente) dictionary.Semente {
	out := make([]dictionary.EntradaSemente, len(d.Entradas))
	for i, e := range d.Entradas {
		e.Inativa = false
		out[i] = e
	}
	d.Entradas = out
	return d
}

// TestSugerir_SemTroca: maiúscula, acento, pontuação e quebra de linha
// sozinhos nunca são troca. "está" ensinado faria "esta casa" virar "está
// casa" em todo texto.
func TestSugerir_SemTroca(t *testing.T) {
	nada := []learning.Motivo{learning.MotivoSemTroca}
	rode(t, []caso{
		{nome: "acento sozinho",
			bruto: "emiti o recíbo", antes: "Emiti o recíbo.", depois: "Emiti o recibo.", dic: dicBase(), wantDescartes: nada},
		{nome: "acento ao contrário",
			bruto: "ela esta aqui", antes: "Ela esta aqui.", depois: "Ela está aqui.", dic: dicBase(), wantDescartes: nada},
		{nome: "maiúscula sozinha",
			bruto: "mandei o sf", antes: "Mandei o sf.", depois: "Mandei o SF.", dic: dicBase(), wantDescartes: nada},
		{nome: "pontuação e maiúscula do Polish",
			bruto: "oi tudo bem", antes: "Oi, tudo bem.", depois: "Oi, tudo bem?", dic: dicBase(), wantDescartes: nada},
		{nome: "quebra de linha e espaço",
			bruto: "primeiro segundo", antes: "Primeiro.\n\nSegundo.", depois: "Primeiro. Segundo.", dic: dicBase(), wantDescartes: nada},
		{nome: "hífen e ponto entre as mesmas palavras",
			bruto: "uso o node js", antes: "Uso o node js.", depois: "Uso o Node.js.", dic: dicBase(), wantDescartes: nada},
		{nome: "texto igual",
			bruto: "oi", antes: "Oi.", depois: "Oi.", dic: dicBase(), wantDescartes: nada},
	})
}

func TestSugerir_SoAcrescentaOuApaga(t *testing.T) {
	so := []learning.Motivo{learning.MotivoSoAcrescentaOuApaga}
	rode(t, []caso{
		{nome: "só acrescenta",
			bruto: "mande o balancete", antes: "Mande o balancete.", depois: "Mande o balancete hoje.", dic: dicBase(), wantDescartes: so},
		{nome: "só apaga",
			bruto: "mande o balancete hoje", antes: "Mande o balancete hoje.", depois: "Mande o balancete.", dic: dicBase(), wantDescartes: so},
		{nome: "acrescenta e apaga em pontos diferentes, sem trocar nenhuma por outra",
			bruto: "mande o balancete hoje", antes: "Mande o balancete hoje.", depois: "Mande já o balancete.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga, learning.MotivoSoAcrescentaOuApaga}},
	})
}

// TestSugerir_PalavraDeLigacao: "de" virando "do" nunca vira regra, e
// nenhum dos dois lados pode ser só palavra de ligação.
func TestSugerir_PalavraDeLigacao(t *testing.T) {
	lig := []learning.Motivo{learning.MotivoPalavraDeLigacao}
	rode(t, []caso{
		{nome: "de vira do",
			bruto: "vou de carro", antes: "Vou de carro.", depois: "Vou do carro.", dic: dicBase(), wantDescartes: lig},
		{nome: "em vira no",
			bruto: "moro em são paulo", antes: "Moro em São Paulo.", depois: "Moro no São Paulo.", dic: dicBase(), wantDescartes: lig},
		{nome: "com vira sem (preposições que a lista do diff não tinha)",
			bruto: "vou com ele", antes: "Vou com ele.", depois: "Vou sem ele.", dic: dicBase(), wantDescartes: lig},
		{nome: "e vira ou (conjunções)",
			bruto: "pão e queijo", antes: "Pão e queijo.", depois: "Pão ou queijo.", dic: dicBase(), wantDescartes: lig},
		{nome: "termo vira artigo: o Para também não pode ser só ligação",
			bruto: "abri sf agora", antes: "Abri sf agora.", depois: "Abri a agora.", dic: dicBase(), wantDescartes: lig},
		{nome: "duas palavras de ligação de um lado, uma do outro",
			bruto: "vou de a pé", antes: "Vou de a pé.", depois: "Vou do pé.", dic: dicBase(), wantDescartes: lig},
	})
}

// TestSugerir_Numero: número não se aprende, de nenhum lado.
func TestSugerir_Numero(t *testing.T) {
	num := []learning.Motivo{learning.MotivoNumero}
	rode(t, []caso{
		{nome: "por extenso vira algarismo",
			bruto: "paguei cinco mil", antes: "Paguei cinco mil.", depois: "Paguei 5 mil.", dic: dicBase(), wantDescartes: num},
		{nome: "algarismo vira por extenso",
			bruto: "paguei 5 mil", antes: "Paguei 5 mil.", depois: "Paguei cinco mil.", dic: dicBase(), wantDescartes: num},
		{nome: "número vira outro número",
			bruto: "ano 2026", antes: "Ano 2026.", depois: "Ano 2027.", dic: dicBase(), wantDescartes: num},
		{nome: "algarismo com palavra junto ainda é número",
			bruto: "custa oito", antes: "Custa oito.", depois: "Custa 8 reais.", dic: dicBase(), wantDescartes: num},
		{nome: "número com separador",
			bruto: "custa mil e quinhentos", antes: "Custa mil e quinhentos.", depois: "Custa 1.500.", dic: dicBase(), wantDescartes: num},
		{nome: "o De é só palavra de ligação e o Para é número: o motivo é o número",
			bruto: "vou de carro", antes: "Vou de carro.", depois: "Vou 5 carro.", dic: dicBase(), wantDescartes: num},
	})
}

func TestSugerir_Reescrita(t *testing.T) {
	re := []learning.Motivo{learning.MotivoReescrita}
	a30, a31 := strings.Repeat("a", 30), strings.Repeat("a", 31)
	b30, b31 := strings.Repeat("b", 30), strings.Repeat("b", 31)
	rode(t, []caso{
		{nome: "cinco palavras de cada lado",
			bruto: "vou mandar alfa beta gama delta epsilon agora", antes: "Vou mandar alfa beta gama delta epsilon agora.",
			depois: "Vou mandar novo texto bem maior ainda agora.", dic: dicBase(), wantDescartes: re},
		{nome: "quatro palavras de cada lado ainda é termo",
			bruto: "vou mandar alfa beta gama delta agora", antes: "Vou mandar alfa beta gama delta agora.",
			depois: "Vou mandar novo texto bem maior agora.", dic: dicBase(),
			want: []par{{"alfa beta gama delta", "novo texto bem maior", ""}}},
		{nome: "só o antes passa de quatro palavras",
			bruto: "vou mandar alfa beta gama delta epsilon agora", antes: "Vou mandar alfa beta gama delta epsilon agora.",
			depois: "Vou mandar novo agora.", dic: dicBase(), wantDescartes: re},
		{nome: "só o depois passa de quatro palavras",
			bruto: "vou mandar alfa agora", antes: "Vou mandar alfa agora.",
			depois: "Vou mandar novo texto bem maior ainda agora.", dic: dicBase(), wantDescartes: re},
		{nome: "uma palavra de 30 letras ainda é termo",
			bruto: "uso " + a30 + " hoje", antes: "Uso " + a30 + " hoje.", depois: "Uso " + b30 + " hoje.", dic: dicBase(),
			want: []par{{a30, b30, ""}}},
		{nome: "uma palavra de 31 letras no antes é reescrita",
			bruto: "uso " + a31 + " hoje", antes: "Uso " + a31 + " hoje.", depois: "Uso " + b30 + " hoje.", dic: dicBase(), wantDescartes: re},
		{nome: "uma palavra de 31 letras no depois é reescrita",
			bruto: "uso aaaa hoje", antes: "Uso aaaa hoje.", depois: "Uso " + b31 + " hoje.", dic: dicBase(), wantDescartes: re},
		{nome: "três trocas na mesma edição",
			bruto: "sf e rj e cn", antes: "SF e RJ e CN.", depois: "ECF e RF e CNPJ.", dic: dicBase(), wantDescartes: re},
		{nome: "reescrita com várias trocas pequenas não deixa passar nenhuma",
			bruto: "mande o relatório do mês passado", antes: "Mande o relatório do mês passado.",
			depois: "Envie o balancete do ano anterior.", dic: dicBase(), wantDescartes: re},
		{nome: "uma troca grande e uma pequena: a grande faz da edição uma reescrita",
			bruto: "sf e alfa beta gama delta epsilon", antes: "SF e alfa beta gama delta epsilon.",
			depois: "ECF e novo texto bem maior ainda.", dic: dicBase(), wantDescartes: re},
		{nome: "o De do bruto é grande, mesmo com o antes pequeno",
			bruto: "uso alfa beta gama delta epsilon hoje", antes: "Uso sigma hoje.", depois: "Uso ECF hoje.", dic: dicBase(), wantDescartes: re},
		{nome: "o sistema escreveu seis palavras no lugar do bruto e a pessoa trocou por uma",
			bruto: "abri o sf agora", antes: "Abri o Sistema Fiscal Eletronico do Contribuinte Novo agora.", depois: "Abri o ECF agora.",
			dic: dicBase(), wantDescartes: re},
		{nome: "o De grande de uma troca barra a edição toda, não só a troca",
			bruto: "sf e alfa beta gama delta epsilon", antes: "SF e sigma.", depois: "ECF e novo.", dic: dicBase(), wantDescartes: re},
	})
}

func TestSugerir_AtravessaFrase(t *testing.T) {
	at := []learning.Motivo{learning.MotivoAtravessaFrase}
	rode(t, []caso{
		{nome: "a troca cruza fim de frase",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf. O balanço saiu.", depois: "Fechei o ECF. Novo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "a troca cruza quebra de linha",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf o balanço saiu.", depois: "Fechei o ECF\nNovo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "o bruto cruza fim de frase",
			bruto: "fechei sf. novo balanço saiu", antes: "Fechei sf. Novo balanço saiu.", depois: "Fechei ECF saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "reticências",
			bruto: "fechei sf… novo balanço saiu", antes: "Fechei sf… Novo balanço saiu.", depois: "Fechei ECF… Outro balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "exclamação",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf o balanço saiu.", depois: "Fechei o ECF! Novo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "interrogação",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf o balanço saiu.", depois: "Fechei o ECF? Novo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "ponto e vírgula",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf o balanço saiu.", depois: "Fechei o ECF; novo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "retorno de carro sozinho",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf o balanço saiu.", depois: "Fechei o ECF\rNovo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
		{nome: "retorno de carro e quebra de linha",
			bruto: "fechei o sf o balanço saiu", antes: "Fechei o sf o balanço saiu.", depois: "Fechei o ECF\r\nNovo balanço saiu.",
			dic: dicBase(), wantDescartes: at},
	})
}

// TestSugerir_DesfazTrocaDoSistema: o Smart trocou "comite" por "commit" e a
// pessoa voltou. Aprender o contrário seria ensinar o erro.
func TestSugerir_DesfazTrocaDoSistema(t *testing.T) {
	des := []learning.Motivo{learning.MotivoDesfazTrocaDoSistema}
	rode(t, []caso{
		{nome: "comite virou commit e a pessoa voltou para comitê",
			bruto: "fiz o comite ontem", antes: "Fiz o commit ontem.", depois: "Fiz o comitê ontem.", dic: dicBase(), wantDescartes: des},
		{nome: "comite virou commit e a pessoa voltou para comite",
			bruto: "fiz o comite ontem", antes: "Fiz o commit ontem.", depois: "Fiz o comite ontem.", dic: dicBase(), wantDescartes: des},
		{nome: "sem o termo no dicionário, o bruto sozinho basta",
			bruto: "fiz o comite ontem", antes: "Fiz o commit ontem.", depois: "Fiz o comitê ontem.",
			dic: dictionary.Semente{Usuario: "mauro"}, wantDescartes: des},
		{nome: "voltou para o bruto, com outra pontuação",
			bruto: "abri o sf", antes: "Abri o ECF.", depois: "Abri o SF!", dic: dicBase(), wantDescartes: des},
	})
}

func TestSugerir_Dicionario(t *testing.T) {
	rode(t, []caso{
		{nome: "trocou a troca do sistema por outra coisa: o De já é de um termo",
			bruto: "fiz o comite ontem", antes: "Fiz o commit ontem.", depois: "Fiz o registro ontem.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "De já é variante de outro termo",
			bruto: "abri o sf", antes: "Abri o Supabase.", depois: "Abri o ECD.", dic: comSF(),
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "De já é a forma canônica de outro termo",
			bruto: "abri o ecf", antes: "Abri o ECF.", depois: "Abri o ECD.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "De já é variante do mesmo termo",
			bruto: "abri o sf", antes: "Abri o sf.", depois: "Abri o Supabase.", dic: comSF(),
			wantDescartes: []learning.Motivo{learning.MotivoJaNoDicionario}},
		{nome: "De já é variante restrita do mesmo termo",
			bruto: "fiz o comite", antes: "Fiz o comite.", depois: "Fiz o commit.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoJaNoDicionario}},
		{nome: "De já é o mesmo termo, só com espaço diferente",
			bruto: "enviei a dctf web", antes: "Enviei a dctf web.", depois: "Enviei a DCTFWeb.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoJaNoDicionario}},
		{nome: "Para já é variante de um termo",
			bruto: "abri o ecd", antes: "Abri o ecd.", depois: "Abri o SF.", dic: comSF(),
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "termo de Para desativado",
			bruto: "uso o kubernet", antes: "Uso o kubernet.", depois: "Uso o Kubernetes.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoTermoInativo}},
		{nome: "termo de Para restrito a domínio",
			bruto: "fiz o comit", antes: "Fiz o comit.", depois: "Fiz o commit.", dic: dicBase(),
			wantDescartes: []learning.Motivo{learning.MotivoTermoRestrito}},
		{nome: "De é variante de um termo e Para é a forma canônica de outro",
			bruto: "abri o sf", antes: "Abri o sf.", depois: "Abri o ECF.", dic: comSF(),
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "a forma canônica de uma entrada ganha de uma variante igual em outra",
			bruto: "abri o ecf", antes: "Abri o ecf.", depois: "Abri o Supabase.",
			dic: dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
				{Canonica: "Supabase", Alias: []string{"ECF"}},
				{Canonica: "ECF"},
			}},
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "dicionário vazio",
			bruto: "abri o sf", antes: "Abri o sf.", depois: "Abri o ECF.", dic: dictionary.Semente{},
			want: []par{{"sf", "ECF", ""}}},
		{nome: "entrada sem forma canônica no dicionário é ignorada",
			bruto: "abri o sf", antes: "Abri o sf.", depois: "Abri o ECF.",
			dic:  dictionary.Semente{Entradas: []dictionary.EntradaSemente{{Canonica: "  ", Alias: []string{"sf"}}}},
			want: []par{{"sf", "ECF", ""}}},
	})
}

// TestSugerir_Origem: a troca só sai se as palavras ao lado dela são as do
// bruto. Se o sistema trocou a vizinha, a troca pode estar no meio de um
// termo que o sistema escreveu.
func TestSugerir_Origem(t *testing.T) {
	semOrigem := []learning.Motivo{learning.MotivoSemOrigemNoBruto}
	rode(t, []caso{
		{nome: "a troca está no meio de um termo que o sistema escreveu",
			bruto: "abri o whisper flow agora", antes: "Abri o Wispr Flow agora.", depois: "Abri o Wispr Flux agora.",
			dic: dicBase(), wantDescartes: semOrigem},
		{nome: "a vizinha da direita foi trocada pelo sistema",
			bruto: "abri o sf whisper agora", antes: "Abri o sf Wispr agora.", depois: "Abri o ECF Wispr agora.",
			dic: dicBase(), wantDescartes: semOrigem},
		{nome: "a vizinha da esquerda foi trocada pelo sistema",
			bruto: "abri o whisper sf agora", antes: "Abri o Wispr sf agora.", depois: "Abri o Wispr ECF agora.",
			dic: dicBase(), wantDescartes: semOrigem},
		{nome: "a troca está numa palavra que o sistema pôs e não tem par no bruto",
			bruto: "abri o sf", antes: "Abri o sf novo.", depois: "Abri o sf velho.",
			dic: dicBase(), wantDescartes: semOrigem},
	})
}

// TestSugerir_JuntaOuSeparaPalavraDeLigacao: quando a pessoa cola ou descola
// uma palavra de ligação ("se não" para "senão"), a palavra faz parte do que
// mudou. Aparar a ponta daria "não" para "senão", um alias que ninguém fez e
// que trocaria o "não" de outras frases.
func TestSugerir_JuntaOuSeparaPalavraDeLigacao(t *testing.T) {
	lig := []learning.Motivo{learning.MotivoPalavraDeLigacao}
	rode(t, []caso{
		{nome: "se não vira senão, no começo da frase",
			bruto: "se não fizer isso vai dar erro", antes: "Se não fizer isso vai dar erro.", depois: "Senão fizer isso vai dar erro.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "se não vira senão, no meio da frase",
			bruto: "faça logo se não vai atrasar", antes: "Faça logo, se não vai atrasar.", depois: "Faça logo, senão vai atrasar.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "com tudo vira contudo: o m vira n, então a junção não é exata",
			bruto: "gostei com tudo não deu", antes: "Gostei, com tudo não deu.", depois: "Gostei, contudo não deu.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "de mais vira demais",
			bruto: "isso é bom de mais", antes: "Isso é bom de mais.", depois: "Isso é bom demais.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "a gente vira agente",
			bruto: "liguei para a gente de viagem", antes: "Liguei para a gente de viagem.", depois: "Liguei para agente de viagem.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "por tanto vira portanto",
			bruto: "chegou por tanto vamos", antes: "Chegou, por tanto vamos.", depois: "Chegou, portanto vamos.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "sobre tudo vira sobretudo",
			bruto: "gosto sobre tudo de café", antes: "Gosto sobre tudo de café.", depois: "Gosto sobretudo de café.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "em baixo vira embaixo",
			bruto: "está em baixo da mesa", antes: "Está em baixo da mesa.", depois: "Está embaixo da mesa.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "em fim vira enfim: o m vira n",
			bruto: "em fim chegou", antes: "Em fim chegou.", depois: "Enfim chegou.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "a fim vira afim",
			bruto: "é um tema a fim", antes: "É um tema a fim.", depois: "É um tema afim.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "a nota vira anota",
			bruto: "ele a nota agora", antes: "Ele a nota agora.", depois: "Ele anota agora.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "de vagar vira devagar",
			bruto: "ande de vagar", antes: "Ande de vagar.", depois: "Ande devagar.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "demais vira de mais: a separação também",
			bruto: "comi demais hoje", antes: "Comi demais hoje.", depois: "Comi de mais hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "e social vira eSocial: com o termo no dicionário, o pedaço também não vale",
			bruto: "mandei o e social ontem", antes: "Mandei o e social ontem.", depois: "Mandei o eSocial ontem.",
			dic: dicTermos(), wantDescartes: lig},
		{nome: "a palavra de ligação da direita colada",
			bruto: "uso o cast em hoje", antes: "Uso o cast em hoje.", depois: "Uso o Castem hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "uma troca barrada e outra que sugere: o motivo e a sugestão seguem o texto",
			bruto: "se não mandei o sf", antes: "Se não mandei o sf.", depois: "Senão mandei o ECF.",
			dic: dicBase(), want: []par{{"sf", "ECF", "ECF"}}, wantDescartes: lig},
		{nome: "a sugestão vem antes da troca barrada, na ordem do texto",
			bruto: "mandei o sf depois se não der", antes: "Mandei o sf depois, se não der.", depois: "Mandei o ECF depois, senão der.",
			dic: dicBase(), want: []par{{"sf", "ECF", "ECF"}}, wantDescartes: lig},
	})
}

// TestSugerir_JuntaComFlexaoOuAssimilacao: a junção não precisa ser exata. O
// ASR escreve "a nota" para "anotar" porque o -r do infinitivo cai na fala, e
// "com firma" para "confirmar" porque o m vira n. Aparada, a troca seria "nota"
// para "anotar": um alias que trocaria o "nota" de toda frase futura ("emiti a
// nota fiscal" viraria "emiti a anotar fiscal"). Ninguém fez esse par.
func TestSugerir_JuntaComFlexaoOuAssimilacao(t *testing.T) {
	lig := []learning.Motivo{learning.MotivoPalavraDeLigacao}
	junta := func(nome, antes, depois string) caso {
		return caso{nome: nome,
			bruto: strings.ToLower(strings.TrimSuffix(antes, ".")), antes: antes, depois: depois,
			dic: dicBase(), wantDescartes: lig}
	}
	rode(t, []caso{
		junta("a nota vira anotar: o r do infinitivo", "Ele a nota hoje.", "Ele anotar hoje."),
		junta("com firma vira confirmar: o m vira n e sobra o r", "Ele com firma hoje.", "Ele confirmar hoje."),
		junta("de mora vira demorar", "Ele de mora hoje.", "Ele demorar hoje."),
		junta("a gente vira agentes: o plural", "Ele a gente hoje.", "Ele agentes hoje."),
		junta("a prova vira aprovar", "Ele a prova hoje.", "Ele aprovar hoje."),
		junta("a corda vira acordar", "Ele a corda hoje.", "Ele acordar hoje."),
		junta("em presa vira empresas", "Ele em presa hoje.", "Ele empresas hoje."),
		junta("com sulta vira consultar", "Ele com sulta hoje.", "Ele consultar hoje."),
		junta("a companha vira acompanhar", "Ele a companha hoje.", "Ele acompanhar hoje."),
		junta("a sure vira Azure: o s vira z", "Ele a sure hoje.", "Ele Azure hoje."),
		junta("a palavra de ligação saiu do Para e o De é uma palavra só: ensina vira em cima",
			"Ele ensina hoje.", "Ele em cima hoje."),
		{nome: "no começo da frase também",
			bruto: "a nota fiscal saiu", antes: "A nota fiscal saiu.", depois: "Anotar fiscal saiu.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "os dois lados perderam a palavra de ligação: vale a letra do que sobrou do outro",
			bruto: "ele a nota hoje", antes: "Ele a nota hoje.", depois: "Ele de anotar hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "o mesmo, com os lados trocados",
			bruto: "ele de anotar hoje", antes: "Ele de anotar hoje.", depois: "Ele a nota hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "o outro lado tem mais de uma palavra: vale a primeira",
			bruto: "ele a nota hoje", antes: "Ele a nota hoje.", depois: "Ele anotar fiscal hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "o mesmo, com os lados trocados e o outro lado de mais de uma palavra",
			bruto: "ele anotar fiscal hoje", antes: "Ele anotar fiscal hoje.", depois: "Ele a nota hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "a separação do que o bruto tinha junto, com a palavra de ligação na direita",
			bruto: "uso o castem hoje", antes: "Uso o Castem hoje.", depois: "Uso o cast em hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "duas palavras de ligação retiradas: vale a que encosta",
			bruto: "ele de a nota hoje", antes: "Ele de a nota hoje.", depois: "Ele anotar hoje.",
			dic: dicBase(), wantDescartes: lig},
		{nome: "o artigo que não encosta continua saindo da ponta",
			bruto: "abri vispeflow agora", antes: "Abri vispeflow agora.", depois: "Abri o Wispr Flow agora.",
			dic: sem("Wispr Flow"), want: []par{{"vispeflow", "Wispr Flow", ""}}},
		{nome: "postgres vira o PostgreSQL: o artigo é da frase, e o p não é o o",
			bruto: "uso postgres aqui", antes: "Uso postgres aqui.", depois: "Uso o PostgreSQL aqui.",
			dic: dicBase(), want: []par{{"postgres", "PostgreSQL", ""}}},
		{nome: "custo conhecido: otlook vira o Outlook e o artigo encosta na primeira letra",
			bruto: "uso otlook aqui", antes: "Uso otlook aqui.", depois: "Uso o Outlook aqui.",
			dic: dicBase(), wantDescartes: lig},
	})
}

// TestSugerir_PalavraDeLigacaoDaPontaSai: a palavra de ligação da ponta só é
// barrada quando o outro lado a colou. Se não, ela é da frase, e sai.
func TestSugerir_PalavraDeLigacaoDaPontaSai(t *testing.T) {
	rode(t, []caso{
		{nome: "fim de vira enfim: o de sobrou na direita, e enfim só termina igual a fim",
			bruto: "chegou fim de hoje", antes: "Chegou fim de hoje.", depois: "Chegou enfim hoje.",
			dic: dicBase(), want: []par{{"fim", "enfim", ""}}},
		{nome: "de sf vira S.F: o que sobra é o termo inteiro, não um pedaço dele",
			bruto: "abri de sf agora", antes: "Abri de sf agora.", depois: "Abri S.F agora.",
			dic: dicBase(), want: []par{{"sf", "S.F", ""}}},
		{nome: "cada lado com a sua palavra de ligação na ponta, nenhuma colada no outro: sai das duas",
			bruto: "abri de zz agora", antes: "Abri de zz agora.", depois: "Abri xzz da agora.",
			dic: dicBase(), want: []par{{"zz", "xzz", ""}}},
		{nome: "o mesmo, com a palavra de ligação do antes na direita e a do depois na esquerda",
			bruto: "abri xzz de agora", antes: "Abri xzz de agora.", depois: "Abri da zz agora.",
			dic: dicBase(), want: []par{{"xzz", "zz", ""}}},
		{nome: "de sf vira sfx: o que sobra só começa o outro lado, e o de saiu da esquerda",
			bruto: "abri de sf agora", antes: "Abri de sf agora.", depois: "Abri sfx agora.",
			dic: dicBase(), want: []par{{"sf", "sfx", ""}}},
	})
}

// TestSugerir_TermoComHifenOuPonto: o "e" de "NF-e" não é a conjunção, e o
// hífen não é espaço. A palavra de ligação só sai da ponta quando está solta.
func TestSugerir_TermoComHifenOuPonto(t *testing.T) {
	rode(t, []caso{
		{nome: "nfe vira NF-e: o e é a última palavra do termo",
			bruto: "emiti a nfe ontem", antes: "Emiti a nfe ontem.", depois: "Emiti a NF-e ontem.",
			dic: dicBase(), want: []par{{"nfe", "NF-e", ""}}},
		{nome: "nota fiscal eletrônica vira NF-e",
			bruto: "emiti a nota fiscal eletrônica", antes: "Emiti a nota fiscal eletrônica.", depois: "Emiti a NF-e.",
			dic: dicBase(), want: []par{{"nota fiscal eletrônica", "NF-e", ""}}},
		{nome: "cte vira CT-e",
			bruto: "gerei o cte agora", antes: "Gerei o cte agora.", depois: "Gerei o CT-e agora.",
			dic: dicBase(), want: []par{{"cte", "CT-e", ""}}},
		{nome: "imeil vira e-mail: o e é a primeira palavra do termo",
			bruto: "mandei um imeil", antes: "Mandei um imeil.", depois: "Mandei um e-mail.",
			dic: dicBase(), want: []par{{"imeil", "e-mail", ""}}},
		{nome: "ecac vira e-CAC",
			bruto: "entrei no ecac", antes: "Entrei no ecac.", depois: "Entrei no e-CAC.",
			dic: dicBase(), want: []par{{"ecac", "e-CAC", ""}}},
	})
}

// TestSugerir_NumeroGrudadoEmLetra: o número não muda num alias, mesmo
// grudado em letra ("10h", "1º", "v2"). O que a spec permite (TEST-008) é
// "n oito n" virar "n8n": o De não tem dígito.
func TestSugerir_NumeroGrudadoEmLetra(t *testing.T) {
	num := []learning.Motivo{learning.MotivoNumero}
	rode(t, []caso{
		{nome: "10h vira 11h",
			bruto: "entrega às 10h amanhã", antes: "Entrega às 10h amanhã.", depois: "Entrega às 11h amanhã.", dic: dicBase(), wantDescartes: num},
		{nome: "1º vira 2º",
			bruto: "ficou em 1º lugar", antes: "Ficou em 1º lugar.", depois: "Ficou em 2º lugar.", dic: dicBase(), wantDescartes: num},
		{nome: "1ª vira 2ª",
			bruto: "é a 1ª fase", antes: "É a 1ª fase.", depois: "É a 2ª fase.", dic: dicBase(), wantDescartes: num},
		{nome: "v2 vira v3",
			bruto: "uso a v2 da api", antes: "Uso a v2 da api.", depois: "Uso a v3 da api.", dic: dicBase(), wantDescartes: num},
		{nome: "2fa vira 3FA",
			bruto: "ativei o 2fa", antes: "Ativei o 2fa.", depois: "Ativei o 3FA.", dic: dicBase(), wantDescartes: num},
		{nome: "10mil vira 12mil",
			bruto: "custa 10mil", antes: "Custa 10mil.", depois: "Custa 12mil.", dic: dicBase(), wantDescartes: num},
		{nome: "10h vira por extenso",
			bruto: "às 10h", antes: "Às 10h.", depois: "Às dez horas.", dic: dicBase(), wantDescartes: num},
		{nome: "por extenso vira ordinal",
			bruto: "ficou em primeiro lugar", antes: "Ficou em primeiro lugar.", depois: "Ficou em 1º lugar.", dic: dicBase(), wantDescartes: num},
		{nome: "dois números viram um",
			bruto: "uso o ab1cd2", antes: "Uso o ab1cd2.", depois: "Uso o ef1gh.", dic: dicBase(), wantDescartes: num},
		{nome: "os números trocam de ordem",
			bruto: "uso o ab1cd2", antes: "Uso o ab1cd2.", depois: "Uso o ef2gh1.", dic: dicBase(), wantDescartes: num},
		{nome: "o número do Para não é a primeira palavra",
			bruto: "valor cinco", antes: "Valor cinco.", depois: "Valor total 5.", dic: dicBase(), wantDescartes: num},
		{nome: "o número do De vira número com letra",
			bruto: "tenho 1 caneta", antes: "Tenho 1 caneta.", depois: "Tenho 1x caneta.", dic: dicBase(), wantDescartes: num},
		{nome: "o Para tem mais números que o De",
			bruto: "uso o ab1cd", antes: "Uso o ab1cd.", depois: "Uso o ef1gh2.", dic: dicBase(), wantDescartes: num},
		{nome: "o segundo número muda e o primeiro não",
			bruto: "uso o ab1cd2", antes: "Uso o ab1cd2.", depois: "Uso o ef1gh3.", dic: dicBase(), wantDescartes: num},
		{nome: "o mesmo número dos dois lados não muda: só a letra",
			bruto: "uso o ab7cd", antes: "Uso o ab7cd.", depois: "Uso o xy7zw.", dic: dicBase(),
			want: []par{{"ab7cd", "xy7zw", ""}}},
	})
}

// TestSugerir_NumeralPorExtenso: o valor não muda num alias. "oito" para
// "seis" faria "comprei oito notas" virar "comprei seis notas" em todo texto
// (spec 16: mudar número sem evidência é proibido, e numeral por extenso é
// protegido). O que a spec permite (TEST-008) é "n oito n" virar "n8n".
func TestSugerir_NumeralPorExtenso(t *testing.T) {
	num := []learning.Motivo{learning.MotivoNumero}
	vazio := dictionary.Semente{Usuario: "mauro"}
	troca := func(nome, bruto, depois string) caso {
		return caso{nome: nome, bruto: bruto, antes: maiuscula1(bruto) + ".", depois: depois,
			dic: vazio, wantDescartes: num}
	}
	rode(t, []caso{
		troca("oito vira seis", "comprei oito notas", "Comprei seis notas."),
		troca("mil vira milhão", "o saldo é de dois mil reais", "O saldo é de dois milhão reais."),
		troca("dois vira duas", "tenho dois contratos", "Tenho duas contratos."),
		troca("primeiro vira terceiro: ordinal", "ficou em primeiro lugar", "Ficou em terceiro lugar."),
		troca("quinta vira sexta: ordinal feminino", "na quinta parcela", "Na sexta parcela."),
		troca("três vira quatro: o acento não tira o numeral", "são três contas", "São quatro contas."),
		troca("décimo vira nono", "o décimo mês", "O nono mês."),
		troca("o numeral vira palavra comum", "comprei oito notas", "Comprei novas notas."),
		troca("o numeral vira palavra comum, com a conjunção no meio", "custa mil e quinhentos", "Custa xpto."),
		troca("os dois lados têm numeral, e o De tem palavra que não é numeral",
			"comprei oito notas", "Vendi seis contas."),
		troca("os dois lados têm numeral, e o Para tem dígito no meio de uma palavra",
			"tenho oito", "Tenho n9 dois."),
		{nome: "o numeral dentro de um nome soletrado vira outro nome: o valor não é o que muda",
			bruto: "uso o k oito s", antes: "Uso o k oito s.", depois: "Uso o Kubernetes.",
			dic: vazio, want: []par{{"k oito s", "Kubernetes", ""}}},
		{nome: "o numeral só no Para: a pessoa consertou uma palavra que o ASR errou",
			bruto: "são ceis contas", antes: "São ceis contas.", depois: "São seis contas.",
			dic: vazio, want: []par{{"ceis", "seis", ""}}},
		{nome: "TEST-008: o numeral vira dígito no meio de uma palavra",
			bruto: "uso o n oito n", antes: "Uso o n oito n.", depois: "Uso o n8n.",
			dic: vazio, want: []par{{"n oito n", "n8n", ""}}},
	})
}

// maiuscula1 põe a primeira letra em maiúscula: o texto de antes que o Polish
// entregaria.
func maiuscula1(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

// TestSugerir_Polaridade: um alias com palavra de negação ou de afirmação,
// em qualquer lado, inverteria o sentido de frases futuras.
func TestSugerir_Polaridade(t *testing.T) {
	pol := []learning.Motivo{learning.MotivoPalavraDeLigacao}
	cases := []caso{
		{nome: "não vira sim",
			bruto: "a resposta é não", antes: "A resposta é não.", depois: "A resposta é sim.", dic: dicBase(), wantDescartes: pol},
		{nome: "nunca vira sempre",
			bruto: "eu nunca faço isso", antes: "Eu nunca faço isso.", depois: "Eu sempre faço isso.", dic: dicBase(), wantDescartes: pol},
		{nome: "não posso vira consigo",
			bruto: "eu não posso ir", antes: "Eu não posso ir.", depois: "Eu consigo ir.", dic: dicBase(), wantDescartes: pol},
		{nome: "a negação fora da troca não impede a sugestão",
			bruto: "não mandei o sf", antes: "Não mandei o sf.", depois: "Não mandei o ECF.", dic: dicBase(),
			want: []par{{"sf", "ECF", "ECF"}}},
	}
	cases = append(cases,
		caso{nome: "mais vira menos",
			bruto: "custa mais", antes: "Custa mais.", depois: "Custa menos.", dic: dicBase(), wantDescartes: pol},
		caso{nome: "sempre vira às vezes: o aparar não deixa sobrar só o vezes",
			bruto: "eu sempre pago em dia", antes: "Eu sempre pago em dia.", depois: "Eu às vezes pago em dia.",
			dic: dicBase(), wantDescartes: pol},
		caso{nome: "mais vira mas: a troca de ouvido mais comum do português",
			bruto: "fui mais voltei", antes: "Fui mais voltei.", depois: "Fui mas voltei.", dic: dicBase(), wantDescartes: pol},
	)
	// A lista é a de palavras protegidas do Polish (resolver.protegidas) e os
	// dois lados valem. Quem mantém as duas em dia é
	// TestPolaridadeAcompanhaProtegidasDoResolver, no pacote resolver.
	for _, p := range []string{
		"não", "nunca", "jamais", "sim", "nada", "tudo", "nenhum", "nenhuma", "ninguém",
		"nem", "nenhuns", "nenhumas", "sempre", "só", "somente", "apenas", "mais", "menos",
		"todo", "toda", "todos", "todas",
	} {
		cases = append(cases,
			caso{nome: "De com " + p,
				bruto: "eu disse " + p + " hoje", antes: "Eu disse " + p + " hoje.", depois: "Eu disse xpto hoje.", dic: dicBase(), wantDescartes: pol},
			caso{nome: "Para com " + p,
				bruto: "eu disse xpto hoje", antes: "Eu disse xpto hoje.", depois: "Eu disse " + p + " hoje.", dic: dicBase(), wantDescartes: pol},
		)
	}
	rode(t, cases)
}

// TestSugerir_ParaQueEPalavraDeLigacao: "DAS" é um termo, e a chave dele
// ("das") está na lista de palavras de ligação. O Para só é barrado por isso
// quando não é um termo do dicionário.
func TestSugerir_ParaQueEPalavraDeLigacao(t *testing.T) {
	desativado := dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
		{Canonica: "DAS", Inativa: true},
	}}
	rode(t, []caso{
		{nome: "daz vira DAS, o termo do dicionário",
			bruto: "paguei o daz ontem", antes: "Paguei o daz ontem.", depois: "Paguei o DAS ontem.",
			dic: dicTermos(), want: []par{{"daz", "DAS", "DAS"}}},
		{nome: "DAS desativado: vale o motivo do termo, não o da palavra",
			bruto: "paguei o daz ontem", antes: "Paguei o daz ontem.", depois: "Paguei o DAS ontem.",
			dic: desativado, wantDescartes: []learning.Motivo{learning.MotivoTermoInativo}},
		{nome: "daz vira das sem o termo no dicionário: é só a palavra",
			bruto: "paguei daz ontem", antes: "Paguei daz ontem.", depois: "Paguei das ontem.",
			dic: dicBase(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		{nome: "o De é palavra de ligação, mesmo com o Para sendo um termo",
			bruto: "paguei de ontem", antes: "Paguei de ontem.", depois: "Paguei DAS ontem.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		// A pessoa escreveu a preposição "das", em minúscula: não é o termo
		// DAS, e o alias "diz" para "DAS" trocaria o "diz" de toda frase.
		{nome: "diz vira das: a preposição minúscula não é o termo DAS",
			bruto: "a maioria diz empresas", antes: "A maioria diz empresas.", depois: "A maioria das empresas.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		{nome: "dias vira das: idem",
			bruto: "faltam dias para o prazo", antes: "Faltam dias para o prazo.", depois: "Faltam das para o prazo.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		{nome: "Das com a maiúscula do começo de frase também não é o termo",
			bruto: "diz que foi pago", antes: "Diz que foi pago.", depois: "Das que foi pago.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
	})
}

// TestSugerir_NegacaoENumeroNaTrocaMinima: quando a troca é estendida até um
// termo do dicionário, a negação e o número que já estavam no termo não são
// o que a pessoa mudou. "nada costa" para "Nada Consta" conserta uma letra.
func TestSugerir_NegacaoENumeroNaTrocaMinima(t *testing.T) {
	termo := func(canonica string) dictionary.Semente {
		return dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: canonica}}}
	}
	rode(t, []caso{
		{nome: "nada costa vira Nada Consta: o nada não mudou",
			bruto: "certidão nada costa emitida", antes: "Certidão nada costa emitida.", depois: "Certidão Nada Consta emitida.",
			dic: termo("Nada Consta"), want: []par{{"nada costa", "Nada Consta", "Nada Consta"}}},
		{nome: "não cumulativo: o não não mudou",
			bruto: "regime não cumulatico", antes: "Regime não cumulatico.", depois: "Regime Não Cumulativo.",
			dic: termo("Não Cumulativo"), want: []par{{"não cumulatico", "Não Cumulativo", "Não Cumulativo"}}},
		{nome: "in rfv 2312 vira IN RFB 2312: o número não mudou",
			bruto: "conforme a in rfv 2312 hoje", antes: "Conforme a in rfv 2312 hoje.", depois: "Conforme a IN RFB 2312 hoje.",
			dic: termo("IN RFB 2312"), want: []par{{"in rfv 2312", "IN RFB 2312", "IN RFB 2312"}}},
		{nome: "o que a pessoa mudou é a negação, mesmo dentro de um termo",
			bruto: "disse não senhor", antes: "Disse não senhor.", depois: "Disse Sim Senhor.",
			dic: termo("Sim Senhor"), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		{nome: "o que a pessoa mudou é o número, mesmo dentro de um termo",
			bruto: "conforme lei 8112 hoje", antes: "Conforme lei 8112 hoje.", depois: "Conforme Lei 8212 hoje.",
			dic: termo("Lei 8212"), wantDescartes: []learning.Motivo{learning.MotivoNumero}},
		{nome: "o que a pessoa mudou é o numeral por extenso, mesmo dentro de um termo",
			bruto: "conforme lei oito hoje", antes: "Conforme lei oito hoje.", depois: "Conforme Lei Seis hoje.",
			dic: termo("Lei Seis"), wantDescartes: []learning.Motivo{learning.MotivoNumero}},
		{nome: "o termo com numeral por extenso, e a pessoa consertou outra palavra",
			bruto: "falei dos dois irmaus", antes: "Falei dos dois irmaus.", depois: "Falei dos Dois Irmãos.",
			dic: termo("Dois Irmãos"), want: []par{{"dois irmaus", "Dois Irmãos", "Dois Irmãos"}}},
	})
}

// TestSugerir_CaixaDeInicioDeFrase: o Polish capitaliza toda frase, então a
// primeira letra maiúscula do começo de uma frase não diz que o termo é um
// nome. O alias novo não leva essa maiúscula.
func TestSugerir_CaixaDeInicioDeFrase(t *testing.T) {
	descricao := []par{{"transcrição", "descrição", ""}}
	rode(t, []caso{
		{nome: "começo do texto",
			bruto: "transcrição ficou pronta", antes: "Transcrição ficou pronta.", depois: "Descrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de ponto",
			bruto: "uso o ecd. transcrição ficou pronta", antes: "Uso o ecd. Transcrição ficou pronta.", depois: "Uso o ecd. Descrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de exclamação",
			bruto: "uso o ecd! transcrição ficou pronta", antes: "Uso o ecd! Transcrição ficou pronta.", depois: "Uso o ecd! Descrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de interrogação",
			bruto: "uso o ecd? transcrição ficou pronta", antes: "Uso o ecd? Transcrição ficou pronta.", depois: "Uso o ecd? Descrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de reticências",
			bruto: "uso o ecd… transcrição ficou pronta", antes: "Uso o ecd… Transcrição ficou pronta.", depois: "Uso o ecd… Descrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de quebra de linha",
			bruto: "uso o ecd transcrição ficou pronta", antes: "Uso o ecd\nTranscrição ficou pronta.", depois: "Uso o ecd\nDescrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de retorno de carro",
			bruto: "uso o ecd transcrição ficou pronta", antes: "Uso o ecd\r\nTranscrição ficou pronta.", depois: "Uso o ecd\r\nDescrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "depois de aspas de abertura",
			bruto: "transcrição ficou pronta", antes: "\"Transcrição ficou pronta.\"", depois: "\"Descrição ficou pronta.\"",
			dic: dicBase(), want: descricao},
		{nome: "depois de parêntese de abertura",
			bruto: "transcrição ficou pronta", antes: "(Transcrição ficou pronta.)", depois: "(Descrição ficou pronta.)",
			dic: dicBase(), want: descricao},
		{nome: "depois de marcador de lista",
			bruto: "transcrição ficou pronta", antes: "- Transcrição ficou pronta.", depois: "- Descrição ficou pronta.",
			dic: dicBase(), want: descricao},
		{nome: "mais de uma palavra, só a primeira maiúscula",
			bruto: "transcrição automática ficou pronta", antes: "Transcrição automática ficou pronta.", depois: "Descrição manual ficou pronta.",
			dic: dicBase(), want: []par{{"transcrição automática", "descrição manual", ""}}},
		{nome: "meio de frase: a maiúscula é de quem digitou",
			bruto: "a transcrição ficou pronta", antes: "A transcrição ficou pronta.", depois: "A Descrição ficou pronta.",
			dic: dicBase(), want: []par{{"transcrição", "Descrição", ""}}},
		{nome: "depois de vírgula não é começo de frase",
			bruto: "falei transcrição ficou", antes: "Falei, transcrição ficou.", depois: "Falei, Descrição ficou.",
			dic: dicBase(), want: []par{{"transcrição", "Descrição", ""}}},
		{nome: "depois de ponto e vírgula não é começo de frase",
			bruto: "falei transcrição ficou", antes: "Falei; transcrição ficou.", depois: "Falei; Descrição ficou.",
			dic: dicBase(), want: []par{{"transcrição", "Descrição", ""}}},
		{nome: "a palavra de antes em minúscula: a maiúscula foi escolha de quem digitou",
			bruto: "visper ficou pronto", antes: "visper ficou pronto.", depois: "Wispr ficou pronto.",
			dic: dicBase(), want: []par{{"visper", "Wispr", ""}}},
		{nome: "sigla no começo da frase fica",
			bruto: "sf ficou pronto", antes: "Sf ficou pronto.", depois: "ECF ficou pronto.",
			dic: sem("ECF"), want: []par{{"sf", "ECF", ""}}},
		{nome: "nome com duas maiúsculas no começo da frase fica",
			bruto: "whisperflow ficou pronto", antes: "Whisperflow ficou pronto.", depois: "Wispr Flow ficou pronto.",
			dic: sem("Wispr Flow"), want: []par{{"whisperflow", "Wispr Flow", ""}}},
		{nome: "termo que o dicionário já tem usa a forma do dicionário",
			bruto: "superbase caiu", antes: "Superbase caiu.", depois: "Supabase caiu.",
			dic: dicBase(), want: []par{{"superbase", "Supabase", "Supabase"}}},
		{nome: "o De leva a caixa do bruto, não a do começo de frase",
			bruto: "Transcrição ficou pronta", antes: "Transcrição ficou pronta.", depois: "Descrição ficou pronta.",
			dic: dicBase(), want: []par{{"Transcrição", "descrição", ""}}},
		{nome: "a troca não começa a frase em depois (entrou um artigo): a maiúscula é de quem digitou",
			bruto: "transcrição ficou pronta", antes: "Transcrição ficou pronta.", depois: "A Descrição ficou pronta.",
			dic: dicBase(), want: []par{{"transcrição", "Descrição", ""}}},
		{nome: "a troca não começa a frase em antes: a maiúscula de antes já era escolha",
			bruto: "a transcrição ficou pronta", antes: "A Transcrição ficou pronta.", depois: "Descrição ficou pronta.",
			dic: dicBase(), want: []par{{"transcrição", "Descrição", ""}}},
		// A aspa de fechamento fica entre o ponto e a frase seguinte.
		{nome: "depois de aspas curvas de fechamento",
			bruto: "pronto transcrição ficou", antes: "“Pronto.” Transcrição ficou.", depois: "“Pronto.” Descrição ficou.",
			dic: dicBase(), want: descricao},
		{nome: "depois de aspa simples de fechamento",
			bruto: "pronto transcrição ficou", antes: "‘Pronto.’ Transcrição ficou.", depois: "‘Pronto.’ Descrição ficou.",
			dic: dicBase(), want: descricao},
		{nome: "depois de aspas angulares de fechamento",
			bruto: "pronto transcrição ficou", antes: "«Pronto.» Transcrição ficou.", depois: "«Pronto.» Descrição ficou.",
			dic: dicBase(), want: descricao},
	})
}

// TestSugerir_CaixaDeInicioComEvidencia: a maiúscula do começo de frase só sai
// do alias quando o texto não mostra que ela é do termo. Duas provas: a mesma
// forma em maiúscula no meio de outra frase, ou a palavra de logo depois,
// que não mudou, ter ganhado maiúscula na edição ("Cloud code" para "Claude
// Code": quem pôs o C de Code sabia que era um nome).
func TestSugerir_CaixaDeInicioComEvidencia(t *testing.T) {
	vazio := dictionary.Semente{Usuario: "mauro"}
	descricao := []par{{"transcrição", "descrição", ""}}
	rode(t, []caso{
		{nome: "a mesma forma em maiúscula no meio de outra frase: é um nome",
			bruto: "superbase caiu uso supabase", antes: "Superbase caiu. Uso Supabase.", depois: "Supabase caiu. Uso Supabase.",
			dic: vazio, want: []par{{"superbase", "Supabase", ""}}},
		{nome: "a mesma forma antes da troca, em maiúscula no meio da frase",
			bruto: "uso supabase superbase caiu", antes: "Uso Supabase. Superbase caiu.", depois: "Uso Supabase. Supabase caiu.",
			dic: vazio, want: []par{{"superbase", "Supabase", ""}}},
		{nome: "a mesma forma em maiúscula, mas em começo de frase: é só o Polish",
			bruto: "transcrição ficou pronta descrição final", antes: "Transcrição ficou pronta. Descrição final.",
			depois: "Descrição ficou pronta. Descrição final.", dic: vazio, want: descricao},
		{nome: "a mesma forma em minúscula no meio de outra frase: não é nome",
			bruto: "transcrição ficou pronta ver descrição", antes: "Transcrição ficou pronta. Ver descrição.",
			depois: "Descrição ficou pronta. Ver descrição.", dic: vazio, want: descricao},
		{nome: "o Para tem duas palavras: só a forma inteira prova",
			bruto:  "transcrição automática ficou pronta ver descrição final",
			antes:  "Transcrição automática ficou pronta. Ver Descrição final.",
			depois: "Descrição manual ficou pronta. Ver Descrição final.", dic: vazio,
			want: []par{{"transcrição automática", "descrição manual", ""}}},
		{nome: "o Para tem duas palavras e aparece inteiro, em maiúscula, no meio de outra frase",
			bruto:  "transcrição automática ficou pronta ver descrição manual",
			antes:  "Transcrição automática ficou pronta. Ver Descrição manual.",
			depois: "Descrição manual ficou pronta. Ver Descrição manual.", dic: vazio,
			want: []par{{"transcrição automática", "Descrição manual", ""}}},
		{nome: "a palavra de logo depois ganhou maiúscula na edição: Claude Code",
			bruto: "cloud code é bom", antes: "Cloud code é bom.", depois: "Claude Code é bom.",
			dic: vazio, want: []par{{"cloud", "Claude", ""}}},
		{nome: "a palavra de logo depois já era maiúscula em antes: não foi a pessoa",
			bruto: "transcrição ficou pronta", antes: "Transcrição Ficou pronta.", depois: "Descrição Ficou pronta.",
			dic: vazio, want: descricao},
		{nome: "a palavra de logo depois começa outra frase: a maiúscula é do Polish",
			bruto: "transcrição ficou pronta", antes: "Transcrição. ficou pronta.", depois: "Descrição. Ficou pronta.",
			dic: vazio, want: descricao},
		{nome: "a palavra de logo depois não é a mesma nos dois textos",
			bruto: "transcrição de ficou", antes: "Transcrição de ficou.", depois: "Descrição Do ficou.",
			dic: vazio, want: descricao},
		{nome: "a palavra de logo depois também é minúscula em depois",
			bruto: "cloud code é bom", antes: "Cloud code é bom.", depois: "Claude code é bom.",
			dic: vazio, want: []par{{"cloud", "claude", ""}}},
		{nome: "a troca é o texto inteiro: não há palavra depois em antes nem em depois",
			bruto: "transcrição", antes: "Transcrição.", depois: "Descrição.",
			dic: vazio, want: descricao},
		{nome: "não há palavra depois em depois, e em antes há uma que o aparar tirou",
			bruto: "transcrição de", antes: "Transcrição de.", depois: "Descrição.",
			dic: vazio, want: descricao},
		{nome: "não há palavra depois em antes, e em depois há uma que o aparar tirou",
			bruto: "transcrição", antes: "Transcrição.", depois: "Descrição de.",
			dic: vazio, want: descricao},
	})
}

// TestSugerir_TermoDoDicionarioConsertadoEmParte: a troca mínima de "EFD
// rain" para "EFD-Reinf" é "rain" para "Reinf", mas o que a pessoa consertou
// foi o termo, que o dicionário já tem. Aprender o pedaço criaria o termo
// "Reinf".
func TestSugerir_TermoDoDicionarioConsertadoEmParte(t *testing.T) {
	somenteReinf := func(e dictionary.EntradaSemente) dictionary.Semente {
		e.Canonica = "EFD-Reinf"
		return dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{e}}
	}
	reinf := []par{{"efd rain", "EFD-Reinf", "EFD-Reinf"}}
	rode(t, []caso{
		{nome: "EFD rain vira EFD-Reinf: a palavra que não mudou fica à esquerda",
			bruto: "entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: dicTermos(), want: reinf},
		{nome: "Lucro Presumido",
			bruto: "empresa no lucro pressumido", antes: "Empresa no lucro pressumido.", depois: "Empresa no Lucro Presumido.",
			dic: dicTermos(), want: []par{{"lucro pressumido", "Lucro Presumido", "Lucro Presumido"}}},
		{nome: "Simples Nacional: a palavra que não mudou fica à direita",
			bruto: "optante do simple nacional", antes: "Optante do simple nacional.", depois: "Optante do Simples Nacional.",
			dic: dicTermos(), want: []par{{"simple nacional", "Simples Nacional", "Simples Nacional"}}},
		{nome: "Ministério da Fazenda: duas palavras à esquerda, uma delas de ligação",
			bruto: "falei com o ministerio da fasenda", antes: "Falei com o ministerio da fasenda.", depois: "Falei com o Ministério da Fazenda.",
			dic: dicTermos(), want: []par{{"ministerio da fasenda", "Ministério da Fazenda", "Ministério da Fazenda"}}},
		{nome: "o pedaço errado está no meio do termo, com quatro palavras",
			bruto: "falei com o conselho regionau de medicina", antes: "Falei com o conselho regionau de medicina.", depois: "Falei com o Conselho Regional de Medicina.",
			dic: dicTermos(), want: []par{{"conselho regionau de medicina", "Conselho Regional de Medicina", "Conselho Regional de Medicina"}}},
		{nome: "termo com mais de 30 caracteres não se estende: fica a troca mínima",
			bruto: "falei com o conselho federau de contabilidade", antes: "Falei com o conselho federau de contabilidade.", depois: "Falei com o Conselho Federal de Contabilidade.",
			dic: dicTermos(), want: []par{{"federau", "Federal", ""}}},
		{nome: "a maior janela ganha: Wispr Flow, não Wispr",
			bruto: "abri o whisper flow agora", antes: "Abri o whisper flow agora.", depois: "Abri o Wispr Flow agora.",
			dic: dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
				{Canonica: "Wispr", Prioridade: 50}, {Canonica: "Wispr Flow", Prioridade: 50},
			}},
			want: []par{{"whisper flow", "Wispr Flow", "Wispr Flow"}}},
		{nome: "termo que o dicionário não tem: só a troca mínima (risco conhecido, quem barra é o clique)",
			bruto: "abri o whisper flow agora", antes: "Abri o whisper flow agora.", depois: "Abri o Wispr Flow agora.",
			dic: dictionary.Semente{Usuario: "mauro"}, want: []par{{"whisper", "Wispr", ""}}},
		{nome: "a janela não cruza fim de frase",
			bruto: "entreguei a efd. rain chegou", antes: "Entreguei a efd. rain chegou.", depois: "Entreguei a efd. Reinf chegou.",
			dic: dicTermos(), want: []par{{"rain", "Reinf", ""}}},
		{nome: "a palavra vizinha mais longe foi trocada pelo sistema: não estende",
			bruto: "falei com o ministro da fasenda", antes: "Falei com o Ministério da fasenda.", depois: "Falei com o Ministério da Fazenda.",
			dic: dicTermos(), want: []par{{"fasenda", "Fazenda", ""}}},
		{nome: "duas trocas, cada uma estendida ao seu termo",
			bruto: "entreguei efd rain e simple nacional", antes: "Entreguei efd rain e simple nacional.", depois: "Entreguei EFD-Reinf e Simples Nacional.",
			dic:  dicTermos(),
			want: []par{{"efd rain", "EFD-Reinf", "EFD-Reinf"}, {"simple nacional", "Simples Nacional", "Simples Nacional"}}},
		{nome: "o termo desativado vale: não cria outro por cima",
			bruto: "entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: somenteReinf(dictionary.EntradaSemente{Inativa: true}), wantDescartes: []learning.Motivo{learning.MotivoTermoInativo}},
		{nome: "o termo restrito a domínio vale: não cria outro por cima",
			bruto: "entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: somenteReinf(dictionary.EntradaSemente{Restrito: true}), wantDescartes: []learning.Motivo{learning.MotivoTermoRestrito}},
		{nome: "o De inteiro já é variante do mesmo termo",
			bruto: "entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: somenteReinf(dictionary.EntradaSemente{Alias: []string{"EFD rain"}}), wantDescartes: []learning.Motivo{learning.MotivoJaNoDicionario}},
		{nome: "o De inteiro já é variante de outro termo",
			bruto: "entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
				{Canonica: "EFD-Reinf"}, {Canonica: "Outro", Alias: []string{"efd rain"}},
			}},
			wantDescartes: []learning.Motivo{learning.MotivoVarianteDeOutroTermo}},
		{nome: "o artigo vizinho da direita mudou de um texto para o outro: não entra na janela",
			bruto: "abri vinsi a agora", antes: "Abri vinsi a agora.", depois: "Abri vinci da agora.",
			dic:  dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: "Vinci da"}}},
			want: []par{{"vinsi", "vinci", ""}}},
		{nome: "o teto de palavras conta cada pedaço de NF-e: cinco palavras não se estendem",
			bruto: "emiti nfe de servico nacional", antes: "Emiti nfe de servico nacional.", depois: "Emiti NF-e de Serviço Nacional.",
			dic:  dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: "NF-e de Serviço Nacional"}}},
			want: []par{{"nfe", "NF-e", ""}}},
		{nome: "sem o termo no dicionário, o pedaço é o que a pessoa fez",
			bruto: "entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: dictionary.Semente{Usuario: "mauro"}, want: []par{{"rain", "Reinf", ""}}},
		{nome: "dois termos que começam igual: a janela maior ganha",
			bruto: "falei com a receita federau do brasil", antes: "Falei com a receita federau do brasil.", depois: "Falei com a Receita Federal do Brasil.",
			dic: dicTermos(), want: []par{{"receita federau do brasil", "Receita Federal do Brasil", "Receita Federal do Brasil"}}},
		{nome: "a palavra da direita foi trocada pelo sistema: só vale a janela até onde o bruto é o mesmo",
			bruto: "falei com a receita federau do brazil", antes: "Falei com a receita federau do Brasil.", depois: "Falei com a Receita Federal do Brasil.",
			dic: dicTermos(), want: []par{{"receita federau", "Receita Federal", "Receita Federal"}}},
		{nome: "o artigo vizinho mudou de um texto para o outro: não entra na janela",
			bruto: "abri a vinsi agora", antes: "Abri a vinsi agora.", depois: "Abri da Vinci agora.",
			dic: dicTermos(), want: []par{{"vinsi", "Vinci", ""}}},
		{nome: "o bruto da janela tem uma palavra que o sistema tirou: não cria o termo Regional por cima",
			bruto: "falei com o conselho hum regionau de medicina", antes: "Falei com o conselho regionau de medicina.", depois: "Falei com o Conselho Regional de Medicina.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoSemOrigemNoBruto}},
		{nome: "o sistema tirou a gagueira entre a vizinha da esquerda e a troca",
			bruto: "entreguei a efd efd rain", antes: "Entreguei a EFD rain.", depois: "Entreguei a EFD-Reinf.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoSemOrigemNoBruto}},
		{nome: "o sistema tirou a hesitação entre a troca e a vizinha da direita",
			bruto: "optante do simple hum nacional", antes: "Optante do simple nacional.", depois: "Optante do Simples Nacional.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoSemOrigemNoBruto}},
		{nome: "o sistema tirou uma palavra entre duas vizinhas da esquerda",
			bruto: "falei com o ministerio hum da fasenda", antes: "Falei com o ministerio da fasenda.", depois: "Falei com o Ministério da Fazenda.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoSemOrigemNoBruto}},
		{nome: "o sistema tirou uma palavra entre duas vizinhas da direita",
			bruto: "falei com o conselho regionau de hum medicina", antes: "Falei com o conselho regionau de medicina.", depois: "Falei com o Conselho Regional de Medicina.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoSemOrigemNoBruto}},
		{nome: "a palavra que o sistema tirou fica fora da janela: a janela é contígua e estende",
			bruto: "hum entreguei a efd rain", antes: "Entreguei a efd rain.", depois: "Entreguei a EFD-Reinf.",
			dic: dicTermos(), want: reinf},
		{nome: "o artigo de ligação sozinho virou o primeiro nome do termo: o artigo não é termo",
			bruto: "consultei o nacional", antes: "Consultei o nacional.", depois: "Consultei Simples Nacional.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		{nome: "o artigo virou a primeira palavra de um termo de duas",
			bruto: "entreguei o reinf", antes: "Entreguei o reinf.", depois: "Entreguei a EFD-Reinf.",
			dic: dicTermos(), wantDescartes: []learning.Motivo{learning.MotivoPalavraDeLigacao}},
		{nome: "a palavra de ligação trocada por outra, dentro do termo: estende",
			bruto: "abri conta no banco de brasil", antes: "Abri conta no banco de brasil.", depois: "Abri conta no Banco do Brasil.",
			dic:  dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: "Banco do Brasil"}}},
			want: []par{{"banco de brasil", "Banco do Brasil", "Banco do Brasil"}}},
		{nome: "só o De da janela passa do teto: o bruto tem uma palavra muito longa",
			bruto: "falei com o conselho regionaaaaaaaaal de medicina", antes: "Falei com o conselho regionaaaaaaaaal de medicina.", depois: "Falei com o Conselho Regional de Medicina.",
			dic: dicTermos(), want: []par{{"regionaaaaaaaaal", "Regional", ""}}},
		{nome: "só o Para da janela passa do teto",
			bruto: "falei com o conselho federal de contab", antes: "Falei com o conselho federal de contab.", depois: "Falei com o Conselho Federal de Contabilidade.",
			dic: dicTermos(), want: []par{{"contab", "Contabilidade", ""}}},
		{nome: "só o bruto da janela cruza fim de frase",
			bruto: "entreguei a efd. rain chegou", antes: "Entreguei a efd rain chegou.", depois: "Entreguei a EFD-Reinf chegou.",
			dic: dicTermos(), want: []par{{"rain", "Reinf", ""}}},
		{nome: "só o depois da janela cruza fim de frase",
			bruto: "entreguei a efd rain chegou", antes: "Entreguei a efd rain chegou.", depois: "Entreguei a efd. Reinf chegou.",
			dic: dicTermos(), want: []par{{"rain", "Reinf", ""}}},
		{nome: "a janela não sai do começo do texto de depois",
			bruto: "oi tudo efd rain", antes: "Oi tudo efd rain.", depois: "EFD-Reinf.",
			dic: dicTermos(), want: reinf, wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga}},
		{nome: "a janela não passa do fim do texto de depois",
			bruto: "efd rain hoje oi tudo", antes: "EFD rain hoje oi tudo.", depois: "EFD-Reinf hoje.",
			dic: dicTermos(), want: reinf, wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga}},
		{nome: "a janela não sai do começo do texto de antes",
			bruto: "efd rain", antes: "EFD rain.", depois: "Oi tudo EFD-Reinf.",
			dic: dicTermos(), want: reinf, wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga}},
		{nome: "a janela não passa do fim do texto de antes",
			bruto: "efd rain hoje", antes: "EFD rain hoje.", depois: "EFD-Reinf hoje oi tudo.",
			dic: dicTermos(), want: reinf, wantDescartes: []learning.Motivo{learning.MotivoSoAcrescentaOuApaga}},
	})
}

func TestSugerir_EntradaVazia(t *testing.T) {
	vazia := []learning.Motivo{learning.MotivoEntradaVazia}
	rode(t, []caso{
		{nome: "bruto vazio", bruto: "", antes: "Oi.", depois: "Olá.", dic: dicBase(), wantDescartes: vazia},
		{nome: "texto de antes vazio", bruto: "oi", antes: "", depois: "Olá.", dic: dicBase(), wantDescartes: vazia},
		{nome: "a pessoa apagou tudo", bruto: "oi", antes: "Oi.", depois: "  \n ", dic: dicBase(), wantDescartes: vazia},
		{nome: "só pontuação", bruto: "...", antes: "...", depois: "!", dic: dicBase(), wantDescartes: vazia},
	})
}

// TestSugerir_Pura: não muda o dicionário, e a mesma entrada dá a mesma
// saída. É o que deixa a etapa de gravar ser a única com efeito.
func TestSugerir_Pura(t *testing.T) {
	dic := dicBase()
	antesJSON, _ := json.Marshal(dic)

	chama := func() learning.Resultado {
		return learning.Sugerir("o sf e o rj", "O sf e o rj.", "O ECF e o RF.", dic, nil)
	}
	a, b := chama(), chama()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("duas chamadas iguais deram saídas diferentes: %+v e %+v", a, b)
	}
	if len(a.Sugestoes) != 2 {
		t.Fatalf("esperava 2 sugestões, veio %+v", a)
	}
	if depoisJSON, _ := json.Marshal(dic); string(depoisJSON) != string(antesJSON) {
		t.Fatalf("Sugerir mudou o dicionário:\n antes: %s\ndepois: %s", antesJSON, depoisJSON)
	}
}

// TestSugerir_MotivosSaoCodigos: o registro de funcionamento só guarda
// contagens e códigos (AC-027). Cada motivo devolvido é um dos códigos
// fixos, nunca um trecho do texto.
func TestSugerir_MotivosSaoCodigos(t *testing.T) {
	codigos := map[learning.Motivo]bool{}
	for _, m := range []learning.Motivo{
		learning.MotivoEntradaVazia, learning.MotivoSemTroca, learning.MotivoSoAcrescentaOuApaga,
		learning.MotivoReescrita, learning.MotivoSemOrigemNoBruto, learning.MotivoNumero,
		learning.MotivoPalavraDeLigacao, learning.MotivoAtravessaFrase, learning.MotivoDesfazTrocaDoSistema,
		learning.MotivoJaNoDicionario, learning.MotivoVarianteDeOutroTermo, learning.MotivoTermoInativo,
		learning.MotivoTermoRestrito,
	} {
		if strings.ContainsAny(string(m), " .,;:ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			t.Errorf("motivo %q não é um código", m)
		}
		codigos[m] = true
	}
	if len(codigos) != 13 {
		t.Fatalf("esperava 13 motivos distintos, vieram %d", len(codigos))
	}

	// Um texto com marcas fáceis de reconhecer: nenhuma pode aparecer na saída.
	r := learning.Sugerir("segredoBruto sf 123", "SegredoAntes sf 123.", "SegredoDepois ECF 456.", dicBase(), nil)
	for _, m := range r.Descartes {
		if !codigos[m] {
			t.Errorf("motivo fora do conjunto fechado: %q", m)
		}
	}
}

// TestSugerir_Contador: o contador é injetado, recebe o De e só é chamado
// para o que virou sugestão.
func TestSugerir_Contador(t *testing.T) {
	var chamadas []string
	contar := func(de string) int {
		chamadas = append(chamadas, de)
		return len(de)
	}
	r := learning.Sugerir("a transcrição e o de", "A transcrição e o de.", "A descrição e o do.", dicBase(), contar)
	if len(r.Sugestoes) != 1 || r.Sugestoes[0].De != "transcrição" {
		t.Fatalf("sugestões = %+v", r.Sugestoes)
	}
	if got := r.Sugestoes[0].OcorrenciasNoHistorico; got != len("transcrição") {
		t.Errorf("OcorrenciasNoHistorico = %d, quer o que o contador devolveu (%d)", got, len("transcrição"))
	}
	if !reflect.DeepEqual(chamadas, []string{"transcrição"}) {
		t.Errorf("o contador foi chamado com %v, quer só o De da sugestão", chamadas)
	}

	// Sem contador, zero.
	r = learning.Sugerir("abri o sf", "Abri o sf.", "Abri o ECF.", dicBase(), nil)
	if len(r.Sugestoes) != 1 || r.Sugestoes[0].OcorrenciasNoHistorico != 0 {
		t.Errorf("sem contador: %+v", r.Sugestoes)
	}
}

func TestOcorrenciasEm(t *testing.T) {
	brutos := []string{
		"mandei o SF hoje",
		"o sf, de novo",
		"o sf e o sf",      // conta uma vez por texto
		"a sfera é bonita", // "sf" não é palavra inteira aqui
		"isso é outra coisa",
		"",
	}
	cases := []struct {
		de   string
		want int
	}{
		{"sf", 3},
		{"SF", 3},
		{"Sf.", 3},
		{"o sf", 3},
		{"sf e o sf", 1},
		{"sf o", 0},
		{"sfera", 1},
		{"coisa", 1},
		{"", 0},
		{"...", 0},
		{"inexistente", 0},
	}
	for _, tc := range cases {
		if got := learning.OcorrenciasEm(tc.de, brutos); got != tc.want {
			t.Errorf("OcorrenciasEm(%q) = %d, quer %d", tc.de, got, tc.want)
		}
	}
	if got := learning.OcorrenciasEm("sf", nil); got != 0 {
		t.Errorf("sem histórico = %d, quer 0", got)
	}
	// Acento e caixa não separam.
	if got := learning.OcorrenciasEm("transcrição", []string{"A TRANSCRICAO acabou"}); got != 1 {
		t.Errorf("acento e caixa: %d, quer 1", got)
	}
}

// TestSugerir_TextoLongo: um ditado longo com correção no começo e no fim
// continua funcionando, e um texto enorme não pede memória sem limite.
func TestSugerir_TextoLongo(t *testing.T) {
	palavras := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString("palavra")
			b.WriteByte(byte('a' + i%26))
			b.WriteByte(byte('a' + (i/26)%26))
			b.WriteByte(' ')
		}
		return b.String()
	}

	meio := palavras(1000)
	bruto := "sf " + meio + "rj"
	antes := "SF " + meio + "rj."
	depois := "ECF " + meio + "RF."
	r := learning.Sugerir(bruto, antes, depois, dicBase(), nil)
	if got := pares(r); !reflect.DeepEqual(got, []par{{"sf", "ECF", "ECF"}, {"rj", "RF", ""}}) {
		t.Errorf("mil palavras: sugestões = %+v, descartes %v", got, r.Descartes)
	}

	meio = palavras(3000)
	r = learning.Sugerir("sf "+meio+"rj", "SF "+meio+"rj.", "ECF "+meio+"RF.", dicBase(), nil)
	if len(r.Sugestoes) != 0 || !reflect.DeepEqual(r.Descartes, []learning.Motivo{learning.MotivoReescrita}) {
		t.Errorf("três mil palavras: %+v", r)
	}

	// O bruto não tem nada em comum com o texto final (3000 palavras de um
	// lado, 3000 de outro): a edição em si é pequena e cabe na tabela, mas
	// não há como achar a origem no bruto. Sem origem, não sugere; e não
	// quebra.
	bruto = strings.Repeat("x ", 3000)
	antes = strings.Repeat("y ", 3000) + "sf"
	depois = strings.Repeat("y ", 3000) + "ecf"
	r = learning.Sugerir(bruto, antes, depois, dicBase(), nil)
	if len(r.Sugestoes) != 0 || !reflect.DeepEqual(r.Descartes, []learning.Motivo{learning.MotivoSemOrigemNoBruto}) {
		t.Errorf("bruto grande demais para alinhar: %+v", r)
	}
}
