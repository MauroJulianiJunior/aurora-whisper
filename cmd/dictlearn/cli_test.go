package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dicionarioDeTeste = `{
  "usuario": "m",
  "entradas": [{"canonica": "commit", "dominio": "dev", "prioridade": 75}]
}`

func dicionarioTemp(t *testing.T) string {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(caminho, []byte(dicionarioDeTeste), 0o644); err != nil {
		t.Fatal(err)
	}
	return caminho
}

func rodar(t *testing.T, entrada string, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	cod := run(args, strings.NewReader(entrada), &out, &errBuf)
	return cod, out.String(), errBuf.String()
}

func aliasDe(t *testing.T, caminho, canonica string) (comuns, restritos []string) {
	t.Helper()
	dados, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Entradas []struct {
			Canonica      string   `json:"canonica"`
			Alias         []string `json:"alias"`
			AliasRestrito []string `json:"alias_restrito"`
		} `json:"entradas"`
	}
	if err := json.Unmarshal(dados, &d); err != nil {
		t.Fatalf("o dicionário gravado não é JSON válido: %v", err)
	}
	for _, e := range d.Entradas {
		if e.Canonica == canonica {
			return e.Alias, e.AliasRestrito
		}
	}
	return nil, nil
}

// TestRun_SemConfirmacaoNaoGravaNada é o AC-022 verificado neste
// caminho.
//
// O pacote learning garante isso estruturalmente (Confirmation não tem
// como ser forjada), mas este comando é quem decide QUANDO chamar
// ConfirmedByUser. Um erro aqui — chamar antes de perguntar — passaria
// por cima da garantia sem que nenhum teste do learning percebesse.
func TestRun_SemConfirmacaoNaoGravaNada(t *testing.T) {
	caminho := dicionarioTemp(t)
	antes, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}

	for _, resposta := range []string{"n\n", "\n", "qualquer coisa\n", ""} {
		cod, out, _ := rodar(t, resposta,
			"-dict", caminho, "-saiu", "comite", "-queria", "commit")
		if cod != 0 {
			t.Errorf("resposta %q: código %d", resposta, cod)
		}
		if !strings.Contains(out, "cancelado") {
			t.Errorf("resposta %q: não disse que cancelou:\n%s", resposta, out)
		}
		depois, err := os.ReadFile(caminho)
		if err != nil {
			t.Fatal(err)
		}
		if string(depois) != string(antes) {
			t.Fatalf("resposta %q GRAVOU sem confirmação", resposta)
		}
	}
}

// TestRun_ConfirmadoGravaOAlias — o outro lado.
func TestRun_ConfirmadoGravaOAlias(t *testing.T) {
	caminho := dicionarioTemp(t)
	cod, out, errOut := rodar(t, "s\n",
		"-dict", caminho, "-saiu", "comite", "-queria", "commit")
	if cod != 0 {
		t.Fatalf("código %d\nstdout:%s\nstderr:%s", cod, out, errOut)
	}
	comuns, _ := aliasDe(t, caminho, "commit")
	if len(comuns) != 1 || comuns[0] != "comite" {
		t.Errorf("alias gravado = %v, want [comite]", comuns)
	}
	if !strings.Contains(out, "aprendido") {
		t.Errorf("não confirmou o aprendizado na saída:\n%s", out)
	}
}

// TestRun_RestritoVaiParaAListaCerta: alias que colide com palavra comum
// tem de ir para alias_restrito, senão "o comitê aprovou" viraria "o
// commit aprovou" em qualquer app.
func TestRun_RestritoVaiParaAListaCerta(t *testing.T) {
	caminho := dicionarioTemp(t)
	if cod, out, e := rodar(t, "", "-dict", caminho, "-confirmar", "-restrito",
		"-saiu", "comite", "-queria", "commit"); cod != 0 {
		t.Fatalf("código %d\n%s\n%s", cod, out, e)
	}
	comuns, restritos := aliasDe(t, caminho, "commit")
	if len(restritos) != 1 || restritos[0] != "comite" {
		t.Errorf("alias_restrito = %v, want [comite]", restritos)
	}
	if len(comuns) != 0 {
		t.Errorf("o alias perigoso foi para a lista dos comuns: %v", comuns)
	}
}

// TestRun_EdicaoGrandeNaoViraAlias trava a regra do 07-flows.md:
// "edição grande nunca vira alias silenciosamente".
//
// Uma reescrita de frase inteira viraria uma regra de substituição que
// corrigiria frases sem nenhuma relação com esta.
func TestRun_EdicaoGrandeNaoViraAlias(t *testing.T) {
	caminho := dicionarioTemp(t)
	antes, _ := os.ReadFile(caminho)

	cod, out, _ := rodar(t, "s\n", "-dict", caminho,
		"-saiu", "manda o balancete e o razao do mes passado tudo junto",
		"-queria", "preciso que voce envie a demonstracao de resultado revisada ate sexta")
	if cod != 0 {
		t.Fatalf("código %d: %s", cod, out)
	}
	if !strings.Contains(out, "NÃO vira alias") {
		t.Errorf("não explicou por que a edição foi recusada:\n%s", out)
	}
	depois, _ := os.ReadFile(caminho)
	if string(depois) != string(antes) {
		t.Error("uma edição grande virou alias")
	}
}

// TestRun_TextosEquivalentesNaoAprendemNada: diferença só de espaço ou
// pontuação não é correção.
func TestRun_TextosEquivalentesNaoAprendemNada(t *testing.T) {
	caminho := dicionarioTemp(t)
	antes, _ := os.ReadFile(caminho)
	cod, out, _ := rodar(t, "s\n", "-dict", caminho,
		"-saiu", "commit", "-queria", " commit ")
	if cod != 0 {
		t.Fatalf("código %d: %s", cod, out)
	}
	if !strings.Contains(out, "nada a aprender") {
		t.Errorf("saída inesperada:\n%s", out)
	}
	depois, _ := os.ReadFile(caminho)
	if string(depois) != string(antes) {
		t.Error("gravou algo para textos equivalentes")
	}
}

// TestRun_DicionarioAusenteFalhaAntesDePerguntar: não faz sentido
// perguntar "pode gravar?" e só então descobrir que não há onde gravar.
func TestRun_DicionarioAusenteFalhaAntesDePerguntar(t *testing.T) {
	cod, out, errOut := rodar(t, "s\n", "-dict", filepath.Join(t.TempDir(), "nao-existe.json"),
		"-saiu", "comite", "-queria", "commit")
	if cod == 0 {
		t.Error("dicionário ausente não falhou")
	}
	if strings.Contains(out, "Confirma?") {
		t.Error("perguntou antes de saber se tinha onde gravar")
	}
	if errOut == "" {
		t.Error("falhou sem dizer por quê")
	}
}

func TestRun_ArgumentosObrigatorios(t *testing.T) {
	caminho := dicionarioTemp(t)
	for _, args := range [][]string{
		{"-dict", caminho, "-saiu", "x"},
		{"-dict", caminho, "-queria", "x"},
		{"-dict", caminho, "-saiu", "  ", "-queria", "x"},
	} {
		if cod, _, _ := rodar(t, "", args...); cod != 2 {
			t.Errorf("args %v: código %d, want 2", args, cod)
		}
	}
}

// TestRun_ExtraiOTermoDeUmaFraseLonga é o limite que Mauro mandou
// resolver em 27-set-2026.
//
// Até então o alias usava os textos INTEIROS, e como a elegibilidade
// limita por número de palavras, uma correção de uma palavra dentro de
// uma frase longa nunca virava alias — o que esvaziava na prática o
// fluxo do 07-flows.md (editar o final_text no histórico), que é o
// caminho previsto para ensinar o dicionário.
func TestRun_ExtraiOTermoDeUmaFraseLonga(t *testing.T) {
	caminho := dicionarioTemp(t)
	cod, out, errOut := rodar(t, "s\n", "-dict", caminho,
		"-saiu", "sobe a migration no Supabaese e confere se aceitou",
		"-queria", "sobe a migration no Supabase e confere se aceitou")
	if cod != 0 {
		t.Fatalf("código %d\n%s\n%s", cod, out, errOut)
	}
	comuns, _ := aliasDe(t, caminho, "Supabase")
	if len(comuns) != 1 || comuns[0] != "Supabaese" {
		t.Fatalf("alias = %v, want [Supabaese] — devia extrair só o termo que mudou", comuns)
	}
	// E o alias NÃO pode ser a frase inteira: uma frase só casaria se o
	// ASR produzisse exatamente ela de novo.
	for _, c := range comuns {
		if strings.Contains(c, "migration") {
			t.Errorf("o alias virou a frase inteira: %q", c)
		}
	}
}

// TestRun_AvisaQuandoOsTermosSaoDistantes: o aviso é a informação que
// substitui o limiar automático que NÃO existe.
//
// "certo" -> "errado" tem proximidade 0,33 e "n oito n" -> "n8n" tem
// 0,25 — o caso exigido pela spec está do lado MAIS distante. Não há
// corte que aceite um e recuse o outro, então quem decide é quem
// confirma, e ele precisa ver o número.
func TestRun_AvisaQuandoOsTermosSaoDistantes(t *testing.T) {
	caminho := dicionarioTemp(t)
	_, out, _ := rodar(t, "n\n", "-dict", caminho,
		"-saiu", "isso está certo", "-queria", "isso está errado")

	if !strings.Contains(out, "ATENÇÃO") {
		t.Errorf("não avisou sobre termos distantes:\n%s", out)
	}
	if !strings.Contains(out, "MUDANÇA DE IDEIA") {
		t.Errorf("o aviso não explica o risco:\n%s", out)
	}
}

// TestRun_NaoAvisaQuandoSaoProximos: aviso que aparece sempre vira
// ruído, e ninguém lê.
func TestRun_NaoAvisaQuandoSaoProximos(t *testing.T) {
	caminho := dicionarioTemp(t)
	_, out, _ := rodar(t, "n\n", "-dict", caminho, "-saiu", "Supabaese", "-queria", "Supabase")
	if strings.Contains(out, "ATENÇÃO") {
		t.Errorf("avisou para um par claramente próximo (0,89):\n%s", out)
	}
}

// TestRun_InsercaoPuraNaoViraAlias: "manda o balancete" -> "manda o
// balancete hoje" não mapeia termo nenhum em termo nenhum. Inventar um
// mapeamento ali seria criar regra a partir de nada.
func TestRun_InsercaoPuraNaoViraAlias(t *testing.T) {
	caminho := dicionarioTemp(t)
	antes, _ := os.ReadFile(caminho)

	cod, out, _ := rodar(t, "s\n", "-dict", caminho,
		"-saiu", "manda o balancete", "-queria", "manda o balancete hoje")
	if cod != 0 {
		t.Fatalf("código %d: %s", cod, out)
	}
	if !strings.Contains(out, "NÃO vira alias") {
		t.Errorf("inserção pura não foi recusada:\n%s", out)
	}
	depois, _ := os.ReadFile(caminho)
	if string(depois) != string(antes) {
		t.Error("uma inserção pura virou alias")
	}
}

// leitorQueMexeNoArquivo faz o papel de quem responde "s": na primeira leitura
// (a pergunta "Confirma?") grava novo no arquivo, como um clique do painel do
// Agent durante a espera.
type leitorQueMexeNoArquivo struct {
	caminho, novo string
	mexeu         bool
}

func (l *leitorQueMexeNoArquivo) Read(p []byte) (int, error) {
	if !l.mexeu {
		l.mexeu = true
		if err := os.WriteFile(l.caminho, []byte(l.novo), 0o644); err != nil {
			return 0, err
		}
	}
	return copy(p, "s\n"), nil
}

// TestRun_GravaSobreOQueEstaNoDiscoDepoisDaConfirmacao: o que foi gravado no
// arquivo enquanto a pergunta esperava não some. Antes, o dictlearn gravava a
// cópia lida ANTES da pergunta, e o aprendizado do painel (alias_origem
// "painel") desaparecia do disco.
func TestRun_GravaSobreOQueEstaNoDiscoDepoisDaConfirmacao(t *testing.T) {
	caminho := dicionarioTemp(t)
	const comOPainel = `{
  "usuario": "m",
  "alias_origem": {"cometi": "painel"},
  "entradas": [{"canonica": "commit", "dominio": "dev", "prioridade": 75, "alias": ["cometi"]}]
}`
	var out, errBuf bytes.Buffer
	cod := run([]string{"-dict", caminho, "-saiu", "comite", "-queria", "commit"},
		&leitorQueMexeNoArquivo{caminho: caminho, novo: comOPainel}, &out, &errBuf)
	if cod != 0 {
		t.Fatalf("código %d\nstdout:%s\nstderr:%s", cod, out.String(), errBuf.String())
	}

	comuns, _ := aliasDe(t, caminho, "commit")
	if len(comuns) != 2 {
		t.Fatalf("alias = %v, quero os dois: o do painel e o do dictlearn", comuns)
	}
	dados, _ := os.ReadFile(caminho)
	if !strings.Contains(string(dados), `"cometi": "painel"`) {
		t.Errorf("a origem do alias do painel sumiu:\n%s", dados)
	}
}

// TestRun_DonoQueMudouNaEsperaNaoGrava: se o arquivo virou de outro usuário
// enquanto a pergunta esperava, nada é gravado por cima.
func TestRun_DonoQueMudouNaEsperaNaoGrava(t *testing.T) {
	caminho := dicionarioTemp(t)
	const deOutro = `{"usuario": "outro", "entradas": [{"canonica": "commit", "prioridade": 75}]}`
	var out, errBuf bytes.Buffer
	cod := run([]string{"-dict", caminho, "-saiu", "comite", "-queria", "commit"},
		&leitorQueMexeNoArquivo{caminho: caminho, novo: deOutro}, &out, &errBuf)
	if cod == 0 {
		t.Fatalf("saiu com 0; stderr:%s", errBuf.String())
	}
	if got, _ := os.ReadFile(caminho); string(got) != deOutro {
		t.Errorf("o dictlearn gravou por cima do arquivo de outro dono:\n%s", got)
	}
}
