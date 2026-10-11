package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
)

// A montagem do PACOTE do instalador (ADR-018): -destino e -sem-dicionario.
// Tudo roda em pastas temporárias, com arquivos de mentira e o modelo
// "conferido" por um dublê: nunca no bin\ de quem desenvolve.

func argumentos(t *testing.T, args ...string) montagem {
	t.Helper()
	m, err := montagemDeArgumentos(args, io.Discard)
	if err != nil {
		t.Fatalf("montagemDeArgumentos(%q): %v", args, err)
	}
	return m
}

func TestArgumentos_SemNadaEOBinDeDesenvolvimento(t *testing.T) {
	m := argumentos(t)
	quer := montagemDeDesenvolvimento()
	if m.destino != "bin" || !m.portatil || m.semDicionario {
		t.Errorf("sem argumentos: destino=%q portatil=%v semDicionario=%v; quero bin, com marcador e com dicionario", m.destino, m.portatil, m.semDicionario)
	}
	if m.origemDicionario != quer.origemDicionario || m.origemWhisper != quer.origemWhisper || m.origemModelo != quer.origemModelo {
		t.Errorf("as origens mudaram: %+v", m)
	}
	if m.conferirModelo == nil || m.saida == nil {
		t.Error("o conferidor do modelo ou a saida ficaram nulos")
	}
}

// -destino é a pasta do pacote: qualquer pasta dada, e nunca com o marcador.
func TestArgumentos_DestinoEAPastaDoPacoteSemMarcador(t *testing.T) {
	m := argumentos(t, "-destino", filepath.Join("dist", "staging"))
	if m.destino != filepath.Join("dist", "staging") {
		t.Errorf("destino = %q", m.destino)
	}
	if m.portatil {
		t.Error("a pasta do pacote levaria o marcador portatil: os dados iriam para dentro da pasta que o Setup troca")
	}
	if m.semDicionario {
		t.Error("-destino sozinho ligou -sem-dicionario")
	}
	// Mesmo dado como "bin": explicitar a pasta e pedir o pacote.
	if m := argumentos(t, "-destino", "bin"); m.portatil {
		t.Error("-destino bin explicito virou montagem de desenvolvimento")
	}
	// A forma com = também.
	if m := argumentos(t, "-destino=outra"); m.destino != "outra" || m.portatil {
		t.Errorf("-destino=outra: %+v", m)
	}
}

// -sem-dicionario sozinho não muda a pasta nem o marcador.
func TestArgumentos_SemDicionarioSozinhoMantemOBin(t *testing.T) {
	m := argumentos(t, "-sem-dicionario")
	if !m.semDicionario || m.destino != "bin" || !m.portatil {
		t.Errorf("-sem-dicionario: %+v", m)
	}
}

func TestArgumentos_OsDoisEOPacote(t *testing.T) {
	m := argumentos(t, "-destino", "p", "-sem-dicionario")
	if !m.semDicionario || m.destino != "p" || m.portatil {
		t.Errorf("os dois: %+v", m)
	}
}

func TestArgumentos_Invalidos(t *testing.T) {
	casos := map[string][]string{
		"destino vazio":         {"-destino", ""},
		"destino so espacos":    {"-destino", "   "},
		"destino sem valor":     {"-destino"},
		"opcao desconhecida":    {"-sem-dicionaro"},
		"argumento sobrando":    {"bin2"},
		"sobrando depois":       {"-sem-dicionario", "bin2"},
		"valor para a booleana": {"-sem-dicionario=talvez"},
	}
	for nome, args := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := montagemDeArgumentos(args, io.Discard); err == nil {
				t.Errorf("%q foi aceito", args)
			}
		})
	}
}

func TestArgumentos_AjudaImprimeAsOpcoes(t *testing.T) {
	var uso bytes.Buffer
	_, err := montagemDeArgumentos([]string{"-h"}, &uso)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h: err = %v, quero flag.ErrHelp", err)
	}
	for _, quer := range []string{"-destino", "-sem-dicionario"} {
		if !strings.Contains(uso.String(), quer) {
			t.Errorf("o uso nao descreve %s:\n%s", quer, uso.String())
		}
	}
}

// listar devolve os nomes (relativos, com /) de tudo o que há em dir.
func listar(t *testing.T, dir string) []string {
	t.Helper()
	var nomes []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		nomes = append(nomes, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(nomes)
	return nomes
}

// A montagem do pacote: o servidor, as DLLs e o modelo, e MAIS NADA. Sem
// marcador, sem dicionário (mesmo com um padrão no repositório), sem banco.
func TestMontarPacote_LevaSoOProgramaEOModelo(t *testing.T) {
	m := origemDeMentira(t)
	// Há um dicionário padrão no repositório: é o pessoal de quem desenvolve.
	escrever(t, m.origemDicionario, `{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`)
	m.portatil = false
	m.semDicionario = true
	var saida bytes.Buffer
	m.saida = &saida

	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}

	quer := append([]string{"whisper-server.exe", model.SmallQ4_0.Arquivo}, dllsNecessarias...)
	sort.Strings(quer)
	if got := listar(t, m.destino); strings.Join(got, ",") != strings.Join(quer, ",") {
		t.Errorf("a pasta do pacote tem:\n  %s\nquero:\n  %s", strings.Join(got, "\n  "), strings.Join(quer, "\n  "))
	}
	for _, proibido := range []string{pastas.Marcador, nomeDoDicionario, pastas.BancoDoHistorico} {
		if existe(filepath.Join(m.destino, proibido)) {
			t.Errorf("o pacote leva %s", proibido)
		}
	}
	if !strings.Contains(saida.String(), "sem dicionario") || !strings.Contains(saida.String(), "pasta do pacote") {
		t.Errorf("a saida nao diz o que fez:\n%s", saida.String())
	}
	// A mensagem do bin\ de desenvolvimento (go build do Agent sem trimpath nem
	// versao) nao serve ao pacote.
	if strings.Contains(saida.String(), "go -C") {
		t.Errorf("a saida do pacote manda compilar o Agent como no bin de desenvolvimento:\n%s", saida.String())
	}
}

// A pasta do pacote nunca leva o dicionário pessoal do repositório, mesmo sem
// -sem-dicionario: quem decide é a pasta (-destino), e não a opção. Sem o
// marcador nenhum Agent leria o dicionário dali, e a cópia só abriria a chance
// de vazar o de quem desenvolve.
func TestMontarPacote_NuncaLevaODicionarioPessoalMesmoSemAOpcao(t *testing.T) {
	m := origemDeMentira(t)
	escrever(t, m.origemDicionario, `{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`)
	m.portatil = false
	m.semDicionario = false // nao pediu -sem-dicionario
	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}
	if existe(filepath.Join(m.destino, nomeDoDicionario)) {
		t.Error("o pacote levou o dicionario pessoal do repositorio")
	}
}

// O mesmo, de ponta a ponta pela linha de comando: `asrstage -destino X` (sem
// -sem-dicionario) lê os argumentos, e a montagem que sai deles nao copia o
// dicionario. As origens e o conferidor do modelo sao os de mentira.
func TestMontarPacote_DestinoSozinhoNaoCopiaODicionario(t *testing.T) {
	falso := origemDeMentira(t)
	escrever(t, falso.origemDicionario, `{"usuario":"mauro","entradas":[]}`)
	m := argumentos(t, "-destino", filepath.Join(t.TempDir(), "pacote"))
	m.origemWhisper, m.origemModelo, m.origemDicionario = falso.origemWhisper, falso.origemModelo, falso.origemDicionario
	m.conferirModelo, m.saida = falso.conferirModelo, io.Discard
	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}
	if existe(filepath.Join(m.destino, nomeDoDicionario)) {
		t.Error("asrstage -destino copiou o dicionario pessoal para a pasta do pacote")
	}
	if !existe(filepath.Join(m.destino, "whisper-server.exe")) {
		t.Error("a montagem de teste nao copiou o servidor: o teste nao provou nada")
	}
}

// O bin\ de desenvolvimento, sem -sem-dicionario, leva o dicionário padrão do
// repositório (é o que sempre levou): só a pasta do pacote não o leva.
func TestMontarDev_SemAOpcaoCopiaODicionarioPadrao(t *testing.T) {
	m := origemDeMentira(t)
	padrao := `{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`
	escrever(t, m.origemDicionario, padrao)
	m.portatil = true
	m.semDicionario = false
	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}
	if got := ler(t, filepath.Join(m.destino, nomeDoDicionario)); got != padrao {
		t.Errorf("o bin de desenvolvimento nao levou o dicionario padrao: %q", got)
	}
	if !existe(filepath.Join(m.destino, pastas.Marcador)) {
		t.Error("a montagem de desenvolvimento perdeu o marcador")
	}
}

// Dev com -sem-dicionario: o marcador vem, o padrão não, e o dicionário que a
// pessoa já tem em bin\ não é nem aberto (poderia estar inválido) nem mexido.
func TestMontarDev_SemDicionarioNaoCopiaNemToca(t *testing.T) {
	m := origemDeMentira(t)
	escrever(t, m.origemDicionario, `{"usuario":"mauro","entradas":[]}`)
	m.portatil = true
	m.semDicionario = true
	if err := os.MkdirAll(m.destino, 0o755); err != nil {
		t.Fatal(err)
	}
	existente := filepath.Join(m.destino, nomeDoDicionario)
	escrever(t, existente, "{ isto nem e json")

	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}
	if got := ler(t, existente); got != "{ isto nem e json" {
		t.Errorf("o dicionario existente foi tocado: %q", got)
	}
	if !existe(filepath.Join(m.destino, pastas.Marcador)) {
		t.Error("a montagem de desenvolvimento perdeu o marcador")
	}

	// E sem dicionário nenhum em bin\: continua sem.
	m2 := origemDeMentira(t)
	escrever(t, m2.origemDicionario, `{"usuario":"mauro","entradas":[]}`)
	m2.portatil, m2.semDicionario = true, true
	if err := montar(m2); err != nil {
		t.Fatal(err)
	}
	if existe(filepath.Join(m2.destino, nomeDoDicionario)) {
		t.Error("-sem-dicionario copiou o padrao para o bin")
	}
}

// A pasta do pacote tem de estar limpa: recusa, antes de copiar, e nunca apaga.
func TestMontarPacote_RecusaPastaComDadosOuMarcador(t *testing.T) {
	casos := []struct {
		nome          string
		arquivo       string
		semDicionario bool
	}{
		{"o marcador", pastas.Marcador, true},
		{"o historico", pastas.BancoDoHistorico, true},
		{"o journal do historico", pastas.BancoDoHistorico + "-journal", true},
		{"uma copia antes do dono", pastas.BancoDoHistorico + ".antes-do-dono-20260930T120000", true},
		{"o log do Agent", "agent.log", true},
		{"o log girado do Agent", "agent.log.1", true},
		{"o log do servidor", "whisper-server.log", true},
		{"um dicionario, com -sem-dicionario", nomeDoDicionario, true},
		{"um dicionario, mesmo sem -sem-dicionario", nomeDoDicionario, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			m := origemDeMentira(t)
			m.portatil = false
			m.semDicionario = c.semDicionario
			if err := os.MkdirAll(m.destino, 0o755); err != nil {
				t.Fatal(err)
			}
			intruso := filepath.Join(m.destino, c.arquivo)
			escrever(t, intruso, "de alguem")

			err := montar(m)
			if err == nil || !strings.Contains(err.Error(), c.arquivo) {
				t.Fatalf("montar = %v; quero recusar citando %s", err, c.arquivo)
			}
			if got := ler(t, intruso); got != "de alguem" {
				t.Errorf("o arquivo foi alterado: %q", got)
			}
			// Falhou antes de copiar o modelo.
			if existe(filepath.Join(m.destino, model.SmallQ4_0.Arquivo)) || existe(filepath.Join(m.destino, "whisper-server.exe")) {
				t.Error("copiou antes de conferir a pasta")
			}
		})
	}
}

// A montagem de desenvolvimento NÃO é recusada por ter dados: o bin\ de quem
// desenvolve tem o historico.db e os logs, é o normal.
func TestMontarDev_NaoRecusaOBinComDados(t *testing.T) {
	m := origemDeMentira(t)
	m.portatil = true
	if err := os.MkdirAll(m.destino, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{pastas.BancoDoHistorico, "agent.log", "whisper-server.log", pastas.Marcador} {
		escrever(t, filepath.Join(m.destino, n), "dados")
	}
	if err := montar(m); err != nil {
		t.Fatalf("o bin de desenvolvimento com dados foi recusado: %v", err)
	}
	if got := ler(t, filepath.Join(m.destino, pastas.BancoDoHistorico)); got != "dados" {
		t.Errorf("o historico foi alterado: %q", got)
	}
}
