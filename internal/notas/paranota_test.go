package notas

import (
	"strings"
	"testing"
	"time"
)

// Os casos da regra do "ditar para nota" (a N9 do PC; a N8 do celular usa a
// mesma regra). Vieram de internal/agent/paranota com a regra.

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func nota(id, titulo string, editada time.Duration) Candidata {
	return Candidata{ID: id, Titulo: titulo, CreatedAt: t0, UpdatedAt: t0.Add(editada)}
}

func TestEscolher(t *testing.T) {
	notas := []Candidata{
		nota("compras", "Lista de compras", 0),
		nota("lista", "Lista", 0),
		nota("acao", "Ações da semana", 0),
		nota("semtitulo", "", 0),
		nota("pontuada", "Ideias:", 0),
		nota("cpp", "C++", 0),
	}
	casos := []struct {
		nome, ditado, quer, resto string
	}{
		{"dois-pontos", "Lista de compras: leite, pão e café", "compras", "leite, pão e café"},
		{"vírgula", "lista de compras, leite", "compras", "leite"},
		{"só espaço", "lista de compras leite", "compras", "leite"},
		{"ponto do ASR", "Lista de compras. Leite e ovos.", "compras", "Leite e ovos."},
		{"caixa", "LISTA DE COMPRAS: arroz", "compras", "arroz"},
		{"acento no ditado e não no título", "acoes da semana: revisar", "acao", "revisar"},
		{"acento no título e não no ditado", "Ações da semana revisar", "acao", "revisar"},
		{"o mais longo vence o prefixo curto", "lista de compras: sal", "compras", "sal"},
		{"o curto casa quando o longo não", "lista de tarefas: lavar", "lista", "de tarefas: lavar"},
		{"fim de palavra: Lista não casa com Listar", "Listar os pendentes", "", ""},
		{"só o título: não casa, vira nota nova", "Lista de compras.", "", ""},
		{"pontuação do título não precisa ser ditada", "ideias comprar um barco", "pontuada", "comprar um barco"},
		{"pontuação no meio do título é exata", "C++ ponteiros", "cpp", "ponteiros"},
		{"nenhum título no começo", "Comprar leite na lista de compras", "", ""},
		{"lista do Polish em linhas", "Lista de compras:\n- leite\n- pão", "compras", "- leite\n- pão"},
		{"vazio", "", "", ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			i, resto := Escolher(notas, c.ditado)
			got := ""
			if i >= 0 {
				got = notas[i].ID
			}
			if got != c.quer || resto != c.resto {
				t.Fatalf("Escolher(%q) = %q, %q; quer %q, %q", c.ditado, got, resto, c.quer, c.resto)
			}
		})
	}
}

// TestEscolher_EmpateVenceAEditadaPorUltimo: dois títulos iguais (sem caixa e
// sem acento): vence a nota editada por último, não a primeira da lista.
func TestEscolher_EmpateVenceAEditadaPorUltimo(t *testing.T) {
	notas := []Candidata{
		nota("antiga", "Mercado", 0),
		nota("nova", "mercado", time.Hour),
		nota("meio", "MERCADO", time.Minute),
	}
	i, _ := Escolher(notas, "mercado: tomate")
	if i < 0 || notas[i].ID != "nova" {
		t.Fatalf("empate: escolheu %d, quer a editada por último", i)
	}
	// Ordem inversa na lista: o mesmo resultado.
	notas[0], notas[1] = notas[1], notas[0]
	i, _ = Escolher(notas, "mercado: tomate")
	if i < 0 || notas[i].ID != "nova" {
		t.Fatalf("empate (ordem invertida): escolheu %d", i)
	}
}

func TestFormaDoCorpo(t *testing.T) {
	casos := []struct {
		corpo string
		quer  Forma
	}{
		{"", FormaParagrafo},
		{"um parágrafo\noutro", FormaParagrafo},
		{"- a\n- b", FormaMarcadores},
		{"1. a\n2. b", FormaNumerada},
		{"texto\n- [x] feito", FormaTarefas},
		{"- a\n- [ ] b", FormaTarefas},
		{"# Seção\n- a", FormaMarcadores},
	}
	for _, c := range casos {
		if got := FormaDoCorpo(linhasDe(c.corpo)); got != c.quer {
			t.Errorf("FormaDoCorpo(%q) = %v, quer %v", c.corpo, got, c.quer)
		}
	}
}

// TestAcrescentar é a tabela de como o texto entra.
func TestAcrescentar(t *testing.T) {
	casos := []struct {
		nome, corpo, resto, quer string
		n                        int
	}{
		{"lista de marcar com enumeração",
			"- [ ] Arroz\n- [x] Feijão", "leite, pão e café",
			"- [ ] Arroz\n- [ ] Leite\n- [ ] Pão\n- [ ] Café\n- [x] Feijão", 3},
		{"lista de marcar sem enumeração: um item só",
			"- [ ] Arroz", "duas dúzias de ovos.",
			"- [ ] Arroz\n- [ ] Duas dúzias de ovos", 1},
		{"todos marcados: os novos entram antes do bloco marcado",
			"Mercado do sábado\n- [x] Arroz\n- [x] Feijão", "sal",
			"Mercado do sábado\n- [ ] Sal\n- [x] Arroz\n- [x] Feijão", 1},
		{"itens abertos, um parágrafo e os marcados: depois do último aberto",
			"- [ ] A\n- [ ] B\nObservação\n- [x] C", "d",
			"- [ ] A\n- [ ] B\n- [ ] D\nObservação\n- [x] C", 1},
		{"lista do Polish em linhas numa lista de marcar",
			"- [ ] Arroz", "- leite\n- pão",
			"- [ ] Arroz\n- [ ] Leite\n- [ ] Pão", 2},
		{"número com vírgula não parte",
			"- [ ] Arroz", "1,5 kg de carne",
			"- [ ] Arroz\n- [ ] 1,5 kg de carne", 1},
		{"sigla mantém a caixa",
			"- [ ] X", "eSocial, DCTFWeb",
			"- [ ] X\n- [ ] eSocial\n- [ ] DCTFWeb", 2},
		{"marcadores",
			"Ideias\n- Barco\nfim", "carro, moto e avião",
			"Ideias\n- Barco\n- Carro\n- Moto\n- Avião\nfim", 3},
		{"sem vírgula não é enumeração para a regra: um item só",
			"- Barco", "carro e moto",
			"- Barco\n- Carro e moto", 1},
		{"numerada continua a contagem",
			"1. Um\n2. Dois", "três",
			"1. Um\n2. Dois\n3. Três", 1},
		{"parágrafo vai para o fim, como foi ditado",
			"Primeira ideia.", "comprar um barco, um carro e uma moto.",
			"Primeira ideia.\nComprar um barco, um carro e uma moto.", 1},
		{"parágrafo em nota vazia",
			"", "texto",
			"Texto", 1},
		{"parágrafo com a lista do Polish fica como veio",
			"Notas soltas", "Hoje:\n- a\n- b",
			"Notas soltas\nHoje:\n- a\n- b", 3},
		{"corpo com \\r\\n",
			"- [ ] A\r\n- [x] B", "c",
			"- [ ] A\n- [ ] C\n- [x] B", 1},
		{"resto vazio não muda nada",
			"- [ ] A", "  ",
			"- [ ] A", 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, n := Acrescentar(c.corpo, c.resto)
			if got != c.quer || n != c.n {
				t.Fatalf("Acrescentar(%q, %q) =\n%q, %d\nquer\n%q, %d", c.corpo, c.resto, got, n, c.quer, c.n)
			}
		})
	}
}

func TestTextoDaPilula(t *testing.T) {
	if got := TextoDaPilula("Lista de compras", 3); got != "Lista de compras +3" {
		t.Fatalf("got %q", got)
	}
	if got := TextoDaPilula("  Lista   de\tcompras ", 1); got != "Lista de compras +1" {
		t.Fatalf("espaços: got %q", got)
	}
	longo := strings.Repeat("palavra ", 12)
	got := TextoDaPilula(longo, 12)
	if !strings.HasSuffix(got, "… +12") || len([]rune(got)) > maxTituloNaPilula+4 {
		t.Fatalf("título longo: %q", got)
	}
	if got := TituloCurto(longo); !strings.HasSuffix(got, "…") || len([]rune(got)) != maxTituloNaPilula {
		t.Fatalf("título curto: %q", got)
	}
}
