package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Os testes do dicionário rodam em t.TempDir(): nunca no bin\dicionario.json
// de verdade, que é o dicionário de quem usa o Agent.

func escrever(t *testing.T, caminho, conteudo string) {
	t.Helper()
	if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ler(t *testing.T, caminho string) string {
	t.Helper()
	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPrepararDicionario_CopiaOPadraoQuandoNaoHaNenhum: a primeira
// montagem continua trazendo o dicionário do repositório.
func TestPrepararDicionario_CopiaOPadraoQuandoNaoHaNenhum(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
	escrever(t, origem, `{"usuario":"padrao"}`)

	msg, err := prepararDicionario(origem, destino)
	if err != nil {
		t.Fatalf("prepararDicionario: %v", err)
	}
	if got := ler(t, destino); got != `{"usuario":"padrao"}` {
		t.Errorf("o padrão não foi copiado: %q", got)
	}
	if strings.Contains(msg, "MANTIDO") {
		t.Errorf("a mensagem diz que manteve o que não existia: %s", msg)
	}
}

// TestPrepararDicionario_PreservaOQueJaExiste é o que o painel precisa: o
// que a pessoa ensinou (ou o dictlearn gravou) vive em bin\dicionario.json,
// e a montagem não pode apagá-lo copiando o padrão por cima.
func TestPrepararDicionario_PreservaOQueJaExiste(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
	escrever(t, origem, `{"usuario":"padrao"}`)
	aprendido := `{"usuario":"mauro","alias_origem":{"SF":"painel"},"entradas":[{"canonica":"ECF","prioridade":90,"alias":["SF"]}]}` + "\n"
	escrever(t, destino, aprendido)

	for i := 0; i < 2; i++ { // e uma segunda montagem também
		msg, err := prepararDicionario(origem, destino)
		if err != nil {
			t.Fatalf("prepararDicionario: %v", err)
		}
		if got := ler(t, destino); got != aprendido {
			t.Fatalf("a montagem %d sobrescreveu o dicionário:\n%s", i+1, got)
		}
		if !strings.Contains(msg, "MANTIDO") || !strings.Contains(msg, "apague") {
			t.Errorf("a mensagem não diz que manteve nem como voltar ao padrão: %s", msg)
		}
	}
}

// TestPrepararDicionario_SemPadraoNaoFalha: o dicionário é opcional. Sem o
// padrão no repositório a montagem segue, não cria arquivo e diz o que vale;
// com um dicionário já em bin, ele continua lá.
func TestPrepararDicionario_SemPadraoNaoFalha(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "nao-existe.json"), filepath.Join(dir, "dicionario.json")

	msg, err := prepararDicionario(origem, destino)
	if err != nil {
		t.Fatalf("prepararDicionario: %v", err)
	}
	if !strings.Contains(msg, "sem correcao de termos") {
		t.Errorf("a mensagem não diz o que vale: %s", msg)
	}
	if _, err := os.Stat(destino); !os.IsNotExist(err) {
		t.Errorf("criou um dicionário sem ter de onde: %v", err)
	}

	do := `{"usuario":"mauro","entradas":[]}`
	escrever(t, destino, do)
	if _, err := prepararDicionario(origem, destino); err != nil {
		t.Fatalf("prepararDicionario: %v", err)
	}
	if got := ler(t, destino); got != do {
		t.Errorf("o dicionário existente mudou: %q", got)
	}
}

// TestPrepararDicionario_DestinoInvalidoFalhaENaoSobrescreve: um
// bin\dicionario.json vazio ou quebrado não é "preservado" em silêncio (o
// Agent recusa subir com ele, e a montagem dizia MANTIDO). Também não é
// sobrescrito, porque pode ter algo que a pessoa queira salvar: a montagem
// falha e diz como seguir.
func TestPrepararDicionario_DestinoInvalidoFalhaENaoSobrescreve(t *testing.T) {
	invalidos := map[string]string{
		"vazio":        "",
		"só espaço":    "  \n",
		"JSON cortado": `{"usuario":"mauro","entradas":[`,
		"sem usuario":  `{"entradas":[]}`,
		"campo errado": `{"usuario":"mauro","entrada":[]}`,
		"não é objeto": `do-usuario`,
		// Lê como JSON, mas o Agent o recusa ao popular o dicionário.
		"forma canônica vazia": `{"usuario":"mauro","entradas":[{"canonica":"   "}]}`,
		"sem forma canônica":   `{"usuario":"mauro","entradas":[{"prioridade":50}]}`,
	}
	for nome, conteudo := range invalidos {
		t.Run(nome, func(t *testing.T) {
			dir := t.TempDir()
			origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
			escrever(t, origem, `{"usuario":"padrao"}`)
			escrever(t, destino, conteudo)

			msg, err := prepararDicionario(origem, destino)
			if err == nil {
				t.Fatalf("manteve em silêncio um dicionário que o Agent recusa: %q", msg)
			}
			for _, quer := range []string{"NAO carrega", "recusa subir", "apague"} {
				if !strings.Contains(err.Error(), quer) {
					t.Errorf("o erro não diz %q: %v", quer, err)
				}
			}
			if got := ler(t, destino); got != conteudo {
				t.Errorf("sobrescreveu o dicionário inválido: %q", got)
			}
		})
	}
}

// TestPrepararDicionario_AvisaQuandoOPadraoTemOQueOBinNaoTem: o dictlearn
// grava, por padrão, no dicionário do repositório (a origem), e a montagem
// preserva o do bin. Sem aviso, o que o dictlearn ensinou nunca chegaria ao
// Agent e nada diria isso.
func TestPrepararDicionario_AvisaQuandoOPadraoTemOQueOBinNaoTem(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
	escrever(t, origem, `{"usuario":"mauro","entradas":[`+
		`{"canonica":"ECF","prioridade":90,"alias":["SF","Supabaese"]},`+
		`{"canonica":"Quarkus","prioridade":60}]}`)
	aprendido := `{"usuario":"mauro","alias_origem":{"SF":"painel"},"entradas":[{"canonica":"ECF","prioridade":90,"alias":["SF"]}]}` + "\n"
	escrever(t, destino, aprendido)

	msg, err := prepararDicionario(origem, destino)
	if err != nil {
		t.Fatalf("prepararDicionario: %v", err)
	}
	for _, quer := range []string{"MANTIDO", "AVISO", "1 termo(s) novo(s) e 1 variante(s)", "-dict " + destino, "apague " + destino} {
		if !strings.Contains(msg, quer) {
			t.Errorf("a mensagem não traz %q:\n%s", quer, msg)
		}
	}
	// Só contagens: nada do conteúdo do dicionário na mensagem.
	for _, nao := range []string{"Supabaese", "Quarkus", "Supabase"} {
		if strings.Contains(msg, nao) {
			t.Errorf("a mensagem cita o conteúdo do dicionário (%q):\n%s", nao, msg)
		}
	}
	if got := ler(t, destino); got != aprendido {
		t.Errorf("o dicionário do bin mudou: %q", got)
	}
}

// TestPrepararDicionario_AvisaTambemQuandoSoFaltaUmaVariante: os termos são
// os mesmos, e o padrão só tem uma variante a mais (o caso comum do
// dictlearn: ensinar um alias a um termo que já existe). Sem termo novo, o
// aviso tem de sair do mesmo jeito.
func TestPrepararDicionario_AvisaTambemQuandoSoFaltaUmaVariante(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
	escrever(t, origem, `{"usuario":"mauro","entradas":[{"canonica":"ECF","prioridade":90,"alias":["SF","EFC"]}]}`)
	escrever(t, destino, `{"usuario":"mauro","entradas":[{"canonica":"ECF","prioridade":90,"alias":["SF"]}]}`)

	msg, err := prepararDicionario(origem, destino)
	if err != nil {
		t.Fatalf("prepararDicionario: %v", err)
	}
	if !strings.Contains(msg, "AVISO") || !strings.Contains(msg, "0 termo(s) novo(s) e 1 variante(s)") {
		t.Errorf("não avisou da variante que só o padrão tem:\n%s", msg)
	}
}

// TestPrepararDicionario_NaoAvisaSeOBinJaTemTudo: sem divergência, nada de
// aviso (um aviso que aparece sempre ninguém lê).
func TestPrepararDicionario_NaoAvisaSeOBinJaTemTudo(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
	padrao := `{"usuario":"mauro","entradas":[{"canonica":"ECF","prioridade":90,"alias":["SF"]}]}`
	escrever(t, origem, padrao)
	// O bin tem tudo do padrão, mais o que o painel aprendeu.
	escrever(t, destino, `{"usuario":"mauro","entradas":[{"canonica":"ECF","prioridade":90,"alias":["SF","EFC"]},{"canonica":"Wispr Flow","prioridade":50,"origem":"painel"}]}`)

	msg, err := prepararDicionario(origem, destino)
	if err != nil {
		t.Fatalf("prepararDicionario: %v", err)
	}
	if strings.Contains(msg, "AVISO") {
		t.Errorf("avisou sem divergência:\n%s", msg)
	}
	if !strings.Contains(msg, "MANTIDO") {
		t.Errorf("a mensagem não diz que manteve:\n%s", msg)
	}
}

// TestPrepararDicionario_PadraoIlegivelNaoImpedeAMontagem: o padrão do
// repositório é só a referência do aviso. Se ele não carrega, o bin segue
// mantido e a montagem não falha por isso.
func TestPrepararDicionario_PadraoIlegivelNaoImpedeAMontagem(t *testing.T) {
	dir := t.TempDir()
	origem, destino := filepath.Join(dir, "padrao.json"), filepath.Join(dir, "dicionario.json")
	escrever(t, origem, "quebrado")
	escrever(t, destino, `{"usuario":"mauro","entradas":[]}`)
	msg, err := prepararDicionario(origem, destino)
	if err != nil || !strings.Contains(msg, "MANTIDO") || strings.Contains(msg, "AVISO") {
		t.Errorf("msg = %q, err = %v", msg, err)
	}
}

// TestProximosPassos_CitaOAgentEOPainel: a mensagem final terminava no go
// build do Agent, e quem a seguia subia um Agent cujo "Abrir Aurora
// Whisper" dava a página "O painel não foi construído" (revisão de
// 29-set-2026).
func TestProximosPassos_CitaOAgentEOPainel(t *testing.T) {
	msg := proximosPassos("C:/repo", "bin")
	for _, quer := range []string{
		`go -C C:/repo build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent`,
		"pnpm -C C:/repo/web install",
		"pnpm -C C:/repo/web build",
	} {
		if !strings.Contains(msg, quer) {
			t.Errorf("a mensagem não traz %q:\n%s", quer, msg)
		}
	}
	if i, b := strings.Index(msg, "/web install"), strings.Index(msg, "/web build"); i < 0 || b < i {
		t.Errorf("o pnpm install tem de vir antes do pnpm build:\n%s", msg)
	}
}

// TestProximosPassos_UmComandoPorLinha: o terminal aqui é o PowerShell
// 5.1, onde && não existe — um comando encadeado falharia inteiro.
func TestProximosPassos_UmComandoPorLinha(t *testing.T) {
	msg := proximosPassos("C:/repo", "bin")
	if strings.Contains(msg, "&&") {
		t.Errorf("a mensagem encadeia comandos com &&, que o PowerShell 5.1 não aceita:\n%s", msg)
	}
}
