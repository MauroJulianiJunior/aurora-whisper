package repeticao

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// incidente é a transcrição do ditado de 59 s de 01-out-2026, como o Mauro a
// colou (docs/defeito-ditado-longo.md): o começo certo e o laço, doze vezes,
// até o fim.
const incidente = "Pense como um especialista em RAG e me explique como eu posso utilizar esses três arquivos para construir uma base de conhecimento e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu,"

const comecoDoIncidente = "Pense como um especialista em RAG e me explique como eu posso utilizar esses três arquivos para construir uma base de conhecimento e como cada um deles vai ser utilizado na hora de récu,"

func TestColapsar_OIncidente(t *testing.T) {
	r := Colapsar(incidente)
	if r.Texto != comecoDoIncidente {
		t.Fatalf("Texto =\n%q\nquero\n%q", r.Texto, comecoDoIncidente)
	}
	if len(r.Colapsos) != 1 || r.Colapsos[0] != (Colapso{Palavras: 12, Repeticoes: 12}) {
		t.Errorf("Colapsos = %+v, quero um laço de 12 palavras repetido 12 vezes", r.Colapsos)
	}
	if r.PalavrasRemovidas != 11*12 {
		t.Errorf("PalavrasRemovidas = %d, quero %d", r.PalavrasRemovidas, 11*12)
	}
}

// TestColapsar_RepeticaoDeGenteFicaComoEsta: ênfase, hesitação e
// concordância repetem palavras de propósito. Nada disso é laço.
func TestColapsar_RepeticaoDeGenteFicaComoEsta(t *testing.T) {
	for _, texto := range []string{
		"Não, não, não, não é isso que eu quis dizer.",
		"Tá, tá, entendi.",
		"Ficou muito muito bom.",
		"Sim sim sim sim, pode mandar.",
		"É isso, é isso, é isso.",
		"Anda, anda, anda, anda, anda, que vai começar.",
		"Vai dar certo, vai dar certo, vai dar certo.",
		"Muito, muito, muito, muito, muito, muito, muito obrigado.", // 7: abaixo do mínimo de 8
		"um dois três, um dois três, um dois três, um dois três",    // 4 de 3 palavras: abaixo de 6
		"bem bem",
		// Frase curta (3 a 9 palavras) dita quatro ou cinco vezes de
		// propósito: numa aula, num ditado de lista.
		rep("Repitam comigo: o RAG busca antes de gerar.", 4, " "),
		rep("Crie quatro colunas: coluna do tipo texto livre,", 4, " "),
		rep("Item pendente revisar contrato.", 4, " "),
		rep("Item pendente revisar contrato.", 5, " "),
	} {
		if r := Colapsar(texto); r.Texto != texto || r.Colapsou() {
			t.Errorf("Colapsar(%q) = %q (%+v): repetição de gente não é laço", texto, r.Texto, r.Colapsos)
		}
	}
}

// TestColapsar_ListasEEnumeracoesFicamComoEstao: itens parecidos em
// sequência não são cópias, e números sozinhos nunca contam.
func TestColapsar_ListasEEnumeracoesFicamComoEstao(t *testing.T) {
	for _, texto := range []string{
		"Primeiro, o balanço. Segundo, a DRE. Terceiro, o fluxo de caixa. Quarto, as notas explicativas.",
		"item 1, item 2, item 3, item 4, item 5, item 6, item 7, item 8, item 9, item 10",
		"o cliente A, o cliente B, o cliente C, o cliente D, o cliente E, o cliente F",
		"10, 20, 10, 20, 10, 20, 10, 20, 10, 20, 10, 20, 10, 20, 10, 20, 10, 20",
		"0 0 0 0 0 0 0 0 0 0 0 0",
		"o CPF é 123.456.789-09 e o CNPJ é 12.345.678/0001-95",
		"R$ 100, R$ 100, R$ 100, R$ 100 e mais R$ 100",
		"- - - - - - - - - -",
	} {
		if r := Colapsar(texto); r.Texto != texto || r.Colapsou() {
			t.Errorf("Colapsar(%q) = %q (%+v): lista não é laço", texto, r.Texto, r.Colapsos)
		}
	}
}

func TestColapsar_TextoSemRepeticaoVoltaIgual(t *testing.T) {
	for _, texto := range []string{
		"",
		"   ",
		"Precisa transmitir a DCTFWeb até sexta.",
		"  espaços  duplos e\tTAB\nquebra de linha  ",
		comecoDoIncidente,
	} {
		if r := Colapsar(texto); r.Texto != texto || r.Colapsou() || r.PalavrasRemovidas != 0 {
			t.Errorf("Colapsar(%q) = %+v, quero o texto igual", texto, r)
		}
	}
}

// TestColapsar_FrasesReaisDosDatasets: as referências do dataset de voz e as
// frases do dataset do Polish são fala de verdade, em português, do jeito
// que o Mauro fala. Nenhuma pode ser tocada — sozinha, nem tudo junto num
// texto só (um ditado longo).
func TestColapsar_FrasesReaisDosDatasets(t *testing.T) {
	_, aqui, _, _ := runtime.Caller(0)
	raiz := filepath.Join(filepath.Dir(aqui), "..", "..", "..")
	var frases []string

	var manifesto struct {
		Samples []struct {
			Reference string `json:"reference"`
		} `json:"samples"`
	}
	lerJSON(t, filepath.Join(raiz, "testdata", "asr-dataset", "manifest-normalizado.json"), &manifesto)
	for _, s := range manifesto.Samples {
		frases = append(frases, s.Reference)
	}
	var polish struct {
		Frases []struct {
			Entrada  string `json:"entrada"`
			Esperado string `json:"esperado"`
		} `json:"frases"`
	}
	lerJSON(t, filepath.Join(raiz, "testdata", "polish-dataset", "frases.json"), &polish)
	for _, p := range polish.Frases {
		frases = append(frases, p.Entrada, p.Esperado)
	}
	if len(frases) < 100 {
		t.Fatalf("só %d frases lidas: o teste não prova nada", len(frases))
	}
	for _, f := range frases {
		if r := Colapsar(f); r.Texto != f {
			t.Errorf("Colapsar mudou uma frase real:\n  %q\n  %q", f, r.Texto)
		}
	}
	tudo := strings.Join(frases, " ")
	if r := Colapsar(tudo); r.Texto != tudo || r.Colapsou() {
		t.Errorf("Colapsar mudou o ditado longo feito das frases reais: %+v", r.Colapsos)
	}
}

func lerJSON(t *testing.T, caminho string, v any) {
	t.Helper()
	dados, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatalf("lendo %s: %v", caminho, err)
	}
	if err := json.Unmarshal(dados, v); err != nil {
		t.Fatalf("%s: %v", caminho, err)
	}
}

func TestColapsar_LacoDePalavraSolta(t *testing.T) {
	texto := "Muito obrigado." + strings.Repeat(" Obrigado.", 10)
	r := Colapsar(texto)
	// "obrigado" já aparece antes ("Muito obrigado."): o laço começa nela,
	// com 11 ocorrências, e é ela que fica.
	if r.Texto != "Muito obrigado." {
		t.Errorf("Texto = %q", r.Texto)
	}
	if len(r.Colapsos) != 1 || r.Colapsos[0].Palavras != 1 || r.Colapsos[0].Repeticoes != 11 {
		t.Errorf("Colapsos = %+v", r.Colapsos)
	}
}

// TestColapsar_Limiares trava os números do pacote, de um lado e do outro.
func TestColapsar_Limiares(t *testing.T) {
	frase := "o relatório do mês fechou com saldo positivo"                 // 8 palavras, 44 caracteres
	fraseNove := "o relatório do mês fechou com saldo positivo ontem"       // 9 palavras
	fraseLonga := "o relatório do mês fechou com saldo positivo ontem cedo" // 10 palavras
	for _, c := range []struct {
		nome   string
		texto  string
		laco   bool
		sobrou string
	}{
		{"frase 3 vezes é ênfase", rep(frase, 3, ", "), false, ""},
		{"frase 5 vezes ainda é fala", rep(frase, 5, ", "), false, ""},
		// A primeira ocorrência fica com a pontuação que tinha.
		{"frase 6 vezes é laço", rep(frase, 6, ", "), true, frase + ","},
		{"frase de 9 palavras 5 vezes ainda é fala", rep(fraseNove, 5, " "), false, ""},
		{"frase de 9 palavras 6 vezes é laço", rep(fraseNove, 6, " "), true, fraseNove},
		{"frase de 10 palavras 3 vezes é fala", rep(fraseLonga, 3, " "), false, ""},
		{"frase de 10 palavras 4 vezes é laço", rep(fraseLonga, 4, " "), true, fraseLonga},
		{"palavra 7 vezes é fala", rep("não", 7, " "), false, ""},
		{"palavra 8 vezes é laço", rep("não", 8, " "), true, "não"},
		{"duas palavras 8 vezes é laço", rep("pode ser", 8, " "), true, "pode ser"},
		// Duas palavras seguem a regra da palavra solta, e não a da frase:
		// 7 vezes somam 98 caracteres, e ainda assim é fala.
		{"duas palavras 7 vezes é fala", rep("muito obrigado", 7, ", "), false, ""},
		{"três palavras curtas 6 vezes: 42 caracteres", rep("é o tal", 6, " "), false, ""},
		{"três palavras curtas 8 vezes: 56 caracteres", rep("é o tal", 8, " "), false, ""},
		{"três palavras curtas 9 vezes: 63 caracteres", rep("é o tal", 9, " "), true, "é o tal"},
		{"três palavras 6 vezes: 72 caracteres", rep("um dois três", 6, " "), true, "um dois três"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := Colapsar(c.texto)
			if r.Colapsou() != c.laco {
				t.Fatalf("Colapsar(%q) colapsou = %v, quero %v", c.texto, r.Colapsou(), c.laco)
			}
			if c.laco && r.Texto != c.sobrou {
				t.Errorf("Texto = %q, quero %q", r.Texto, c.sobrou)
			}
		})
	}
}

func rep(s string, n int, sep string) string {
	partes := make([]string, n)
	for i := range partes {
		partes[i] = s
	}
	return strings.Join(partes, sep)
}

func TestColapsar_LacoNoMeioMantemOQueVemDepois(t *testing.T) {
	laco := "a gente precisa revisar o contrato antes"
	texto := "Bom dia. " + rep(laco, 6, " ") + " e depois manda para o cliente."
	r := Colapsar(texto)
	if want := "Bom dia. " + laco + " e depois manda para o cliente."; r.Texto != want {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, want)
	}
}

// TestColapsar_CopiaPelaMetadeNoFimSai: o decodificador para no meio do
// ciclo; a cópia cortada é o mesmo laço.
func TestColapsar_CopiaPelaMetadeNoFimSai(t *testing.T) {
	laco := "e como cada um deles vai ser utilizado na hora de recuperar,"
	texto := "Explique " + rep(laco, 5, " ") + " e como cada um"
	r := Colapsar(texto)
	if want := "Explique " + laco; r.Texto != want {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, want)
	}
	if r.PalavrasRemovidas != 4*12+4 {
		t.Errorf("PalavrasRemovidas = %d, quero %d", r.PalavrasRemovidas, 4*12+4)
	}
}

// TestColapsar_CopiaPelaMetadeComPontuacaoNaoGanhaDaInteira: quando a
// ocorrência inteira já termina em pontuação, é ela que fica, mesmo que a
// cópia pela metade também termine numa vírgula.
func TestColapsar_CopiaPelaMetadeComPontuacaoNaoGanhaDaInteira(t *testing.T) {
	laco := "vamos começar agora, e depois a gente revisa tudo com calma."
	texto := "Então: " + rep(laco, 5, " ") + " vamos começar agora,"
	if r := Colapsar(texto); r.Texto != "Então: "+laco {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, "Então: "+laco)
	}
}

// TestColapsar_CopiaPelaMetadeComVirgulaInternaNaoCortaAFala: a unidade não
// termina em pontuação, e o corte do decodificador cai logo depois de uma
// vírgula interna. A cópia pela metade termina em pontuação e a inteira não,
// como no laço achado girado, mas aqui a cauda que ela deixaria de fora ("vai
// ser usado na hora certa") é fala dita de verdade. Só a cauda curta (o
// conector do laço girado) troca a ocorrência inteira pela cópia pela metade.
func TestColapsar_CopiaPelaMetadeComVirgulaInternaNaoCortaAFala(t *testing.T) {
	const unidade = "Quero saber como cada um deles, vai ser usado na hora certa"
	texto := rep(unidade, 4, " ") + " Quero saber como cada um deles,"
	r := Colapsar(texto)
	if r.Texto != unidade {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, unidade)
	}
	if want := len(strings.Fields(texto)) - len(strings.Fields(unidade)); r.PalavrasRemovidas != want {
		t.Errorf("PalavrasRemovidas = %d, quero %d", r.PalavrasRemovidas, want)
	}
}

// TestColapsar_LacoGiradoComCaudaDeDuasPalavras: a regra do laço girado vale
// até duas palavras de cauda ("e então" no lugar do "e" do incidente). Com
// três, a ocorrência inteira fica (o caso de cima).
func TestColapsar_LacoGiradoComCaudaDeDuasPalavras(t *testing.T) {
	const comeco = "Me explique como cada um deles vai ser utilizado na hora de récu,"
	texto := comeco + strings.Repeat(" e então como cada um deles vai ser utilizado na hora de récu,", 6)
	if r := Colapsar(texto); r.Texto != comeco {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, comeco)
	}
	textoTres := comeco + strings.Repeat(" e então depois como cada um deles vai ser utilizado na hora de récu,", 6)
	want := comeco + " e então depois"
	if r := Colapsar(textoTres); r.Texto != want {
		t.Errorf("cauda de três palavras: Texto =\n%q\nquero\n%q", r.Texto, want)
	}
}

// TestColapsar_LacoAchadoNoMeioDaFrase: a fala antes do laço não tinha a
// primeira palavra do ciclo. A unidade achada começa em "como" e termina num
// "e" solto; quem termina a frase é a cópia pela metade, e é ela que fica.
func TestColapsar_LacoAchadoNoMeioDaFrase(t *testing.T) {
	const comeco = "Me explique como cada um deles vai ser utilizado na hora de récu,"
	texto := comeco + strings.Repeat(" e como cada um deles vai ser utilizado na hora de récu,", 11)
	r := Colapsar(texto)
	if r.Texto != comeco {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, comeco)
	}
	if want := len(strings.Fields(texto)) - len(strings.Fields(comeco)); r.PalavrasRemovidas != want {
		t.Errorf("PalavrasRemovidas = %d, quero %d", r.PalavrasRemovidas, want)
	}
}

// TestColapsar_ComecoDaFraseDepoisDoLacoFica: o que segue o laço começando
// igual a ele, mas indo para outro lado, é fala nova, e fica.
func TestColapsar_ComecoDaFraseDepoisDoLacoFica(t *testing.T) {
	laco := "e como cada um deles vai ser utilizado na hora de recuperar,"
	texto := rep(laco, 5, " ") + " e como eu disse antes, isso importa."
	r := Colapsar(texto)
	if want := laco + " e como eu disse antes, isso importa."; r.Texto != want {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, want)
	}
}

func TestColapsar_IgnoraMaiusculasEPontuacao(t *testing.T) {
	texto := "Vamos ver o resultado do trimestre agora. vamos ver o resultado do trimestre agora, " +
		"Vamos ver o resultado do trimestre agora; VAMOS ver o resultado do trimestre agora... " +
		"VAMOS VER O RESULTADO DO TRIMESTRE AGORA! Vamos ver o resultado do trimestre agora"
	r := Colapsar(texto)
	if r.Texto != "Vamos ver o resultado do trimestre agora." {
		t.Errorf("Texto = %q", r.Texto)
	}
}

func TestColapsar_DoisLacos(t *testing.T) {
	a := "precisamos fechar a folha de pagamento hoje cedo sem falta"
	b := "e conferir os impostos da competência anterior"
	texto := rep(a, 4, " ") + " ok. " + rep(b, 6, " ") + " fim."
	r := Colapsar(texto)
	if want := a + " ok. " + b + " fim."; r.Texto != want {
		t.Errorf("Texto =\n%q\nquero\n%q", r.Texto, want)
	}
	if len(r.Colapsos) != 2 || r.Colapsos[0].Repeticoes != 4 || r.Colapsos[1].Repeticoes != 6 {
		t.Errorf("Colapsos = %+v", r.Colapsos)
	}
}

// TestColapsar_Idempotente: o texto já colapsado não muda de novo.
func TestColapsar_Idempotente(t *testing.T) {
	for _, texto := range []string{incidente, rep("não", 20, " "), "Bom dia. " + rep("a gente precisa revisar o contrato antes", 6, " ")} {
		uma := Colapsar(texto).Texto
		if r := Colapsar(uma); r.Texto != uma || r.Colapsou() {
			t.Errorf("Colapsar não é idempotente em %q: %q", uma, r.Texto)
		}
	}
}
