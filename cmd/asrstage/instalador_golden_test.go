package main

import (
	"regexp"
	"strings"
	"testing"
)

// O .iss nunca foi compilado nem executado: o Pascal Script que apaga a pasta de
// dados, fecha o Agent e baixa o Visual C++ não tem outro teste além destes. Os
// testes de instalador_test.go conferem pedaços de texto ("existe a chamada X"),
// e isso deixa passar uma guarda invertida, sumida ou trocada por outra. Aqui
// cada função do [Code], as seções que decidem o que o Setup faz e as etapas do
// workflow que conferem integridade são comparadas INTEIRAS (sem comentários e
// sem recuo) com o texto que foi revisado.
//
// Se um destes testes falhar, o texto do arquivo mudou. Se a mudança é de
// propósito, releia a guarda inteira, e só então atualize o texto esperado aqui:
// o que este arquivo pede é uma segunda leitura de quem muda uma proteção.
//
// Estes testes só LEEM os arquivos. Nada é executado, instalado ou gravado.

// semComentariosPascal tira os comentários {...} e //... do Pascal Script,
// sem tocar no que está entre aspas simples (aspas dobradas dentro da string
// fecham e reabrem, o que mantém a conta certa) nem no {#Nome} do
// pré-processador do Inno, que se expande em qualquer lugar.
func semComentariosPascal(s string) string {
	var b strings.Builder
	rs := []rune(s)
	emString := false
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case emString:
			b.WriteRune(c)
			if c == '\'' {
				emString = false
			}
		case c == '\'':
			emString = true
			b.WriteRune(c)
		case c == '{' && !(i+1 < len(rs) && rs[i+1] == '#'):
			for i < len(rs) && rs[i] != '}' {
				i++
			}
		case c == '/' && i+1 < len(rs) && rs[i+1] == '/':
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			b.WriteRune('\n')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// linhasLimpas tira o recuo e as linhas em branco.
func linhasLimpas(s string) string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

// secaoBruta devolve o texto de [nome] até a próxima seção, comentários e tudo.
func secaoBruta(iss, nome string) string {
	var linhas []string
	dentro := false
	for _, l := range strings.Split(iss, "\n") {
		if regexp.MustCompile(`^\[[A-Za-z]+\]\s*$`).MatchString(l) {
			dentro = strings.EqualFold(strings.TrimSpace(l), "["+nome+"]")
			continue
		}
		if dentro {
			linhas = append(linhas, l)
		}
	}
	return strings.Join(linhas, "\n")
}

type unidadeDoCodigo struct{ nome, texto string }

// unidadesDoCodigo separa o [Code] em cabeçalho (const e var, antes da primeira
// função) e uma unidade por function/procedure, da declaração ao "end;" na
// coluna 0 (ou, numa declaração externa, à linha do stdcall).
func unidadesDoCodigo(iss string) (cabecalho string, unidades []unidadeDoCodigo) {
	linhas := strings.Split(semComentariosPascal(secaoBruta(iss, "Code")), "\n")
	declaracao := regexp.MustCompile(`^(function|procedure)\s+(\w+)`)
	i := 0
	var cab []string
	for i < len(linhas) && !declaracao.MatchString(linhas[i]) {
		cab = append(cab, linhas[i])
		i++
	}
	for i < len(linhas) {
		m := declaracao.FindStringSubmatch(linhas[i])
		if m == nil {
			i++
			continue
		}
		j := i
		for j < len(linhas) && strings.TrimRight(linhas[j], " \t") != "end;" && !strings.HasSuffix(strings.TrimSpace(linhas[j]), "stdcall';") {
			j++
		}
		if j >= len(linhas) {
			j = len(linhas) - 1
		}
		unidades = append(unidades, unidadeDoCodigo{m[2], strings.Join(linhas[i:j+1], "\n")})
		i = j + 1
	}
	return strings.Join(cab, "\n"), unidades
}

// semGoldenDoCodigo: o que de propósito não é comparado inteiro (só aparência).
var semGoldenDoCodigo = map[string]bool{
	"UpdateReadyMemo": true, // monta o texto da página "Pronto para instalar"
}

const mensagemDoGolden = "\n(Se a mudança é de propósito: releia a proteção inteira e atualize o texto esperado em instalador_golden_test.go)"

func TestIss_GoldenDoCodigo(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	cabecalho, unidades := unidadesDoCodigo(iss)
	if got, quer := linhasLimpas(cabecalho), linhasLimpas(goldenCabecalhoDoCodigo); got != quer {
		t.Errorf("o cabeçalho do [Code] (const e var) mudou:\n--- no .iss\n%s\n--- esperado\n%s%s", got, quer, mensagemDoGolden)
	}
	visto := map[string]bool{}
	for _, u := range unidades {
		visto[u.nome] = true
		t.Run(u.nome, func(t *testing.T) {
			quer, ok := goldenDoCodigo[u.nome]
			if !ok {
				if !semGoldenDoCodigo[u.nome] {
					t.Errorf("a função %s não tem texto esperado em goldenDoCodigo: toda função do [Code] é conferida inteira", u.nome)
				}
				return
			}
			if got, quer := linhasLimpas(u.texto), linhasLimpas(quer); got != quer {
				t.Errorf("%s mudou:\n--- no .iss\n%s\n--- esperado\n%s%s", u.nome, got, quer, mensagemDoGolden)
			}
		})
	}
	for nome := range goldenDoCodigo {
		if !visto[nome] {
			t.Errorf("goldenDoCodigo tem %s, que não existe mais no [Code]", nome)
		}
	}
}

// As seções que decidem o que o Setup instala, cria e executa.
func TestIss_GoldenDasSecoes(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	for nome, quer := range goldenDasSecoes {
		t.Run(nome, func(t *testing.T) {
			if got, quer := strings.Join(secao(iss, nome), "\n"), linhasLimpas(quer); got != quer {
				t.Errorf("a seção [%s] mudou:\n--- no .iss\n%s\n--- esperado\n%s%s", nome, got, quer, mensagemDoGolden)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// O workflow de release, etapa por etapa.
// ---------------------------------------------------------------------------

type etapaDoWorkflow struct{ nome, texto string }

// etapasDoWorkflow separa o job em etapas ("      - name: ..."), sem os
// comentários: a linha inteira de comentário sai, e o comentário ao fim de uma
// chave do YAML também (dentro do script, só a linha inteira de comentário).
func etapasDoWorkflow(wf string) []etapaDoWorkflow {
	inicio := regexp.MustCompile(`^      - name: (.*)$`)
	var etapas []etapaDoWorkflow
	var atual *etapaDoWorkflow
	var linhasDaEtapa []string
	fechar := func() {
		if atual != nil {
			atual.texto = strings.Join(linhasDaEtapa, "\n")
			etapas = append(etapas, *atual)
		}
	}
	emScript, recuoDoScript := false, 0
	for _, l := range strings.Split(wf, "\n") {
		if m := inicio.FindStringSubmatch(l); m != nil {
			fechar()
			atual = &etapaDoWorkflow{nome: strings.TrimSpace(m[1])}
			linhasDaEtapa = nil
			emScript = false
		}
		if atual == nil {
			continue
		}
		recuo := len(l) - len(strings.TrimLeft(l, " "))
		if emScript && strings.TrimSpace(l) != "" && recuo <= recuoDoScript {
			emScript = false
		}
		t := strings.TrimSpace(l)
		switch {
		case t == "":
		case strings.HasPrefix(t, "#"):
		case emScript:
			linhasDaEtapa = append(linhasDaEtapa, t)
		default:
			linhasDaEtapa = append(linhasDaEtapa, regexp.MustCompile(`\s+#.*$`).ReplaceAllString(t, ""))
			if strings.HasSuffix(t, "run: |") {
				emScript, recuoDoScript = true, recuo
			}
		}
	}
	fechar()
	return etapas
}

func TestWorkflow_GoldenDasEtapas(t *testing.T) {
	etapas := etapasDoWorkflow(lerTexto(t, caminhoDoWorkflow))
	visto := map[string]bool{}
	for _, e := range etapas {
		visto[e.nome] = true
		quer, ok := goldenDasEtapas[e.nome]
		if !ok {
			continue // as etapas de montagem e de aparência não são conferidas inteiras
		}
		t.Run(e.nome, func(t *testing.T) {
			if got, quer := e.texto, linhasLimpas(quer); got != quer {
				t.Errorf("a etapa %q do workflow mudou:\n--- no workflow\n%s\n--- esperado\n%s%s", e.nome, got, quer, mensagemDoGolden)
			}
		})
	}
	for nome := range goldenDasEtapas {
		if !visto[nome] {
			t.Errorf("o workflow não tem mais a etapa %q", nome)
		}
	}
}
