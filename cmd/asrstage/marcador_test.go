package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
)

// Estes testes montam em pastas temporárias, com arquivos de mentira e o
// modelo "conferido" por um dublê (os 139 MB do real não existem aqui):
// nunca no bin\ de quem desenvolve.

// origemDeMentira cria um thirdparty/whisper/Release de brinquedo, com o
// servidor e as DLLs que a montagem copia, e um "modelo".
func origemDeMentira(t *testing.T) montagem {
	t.Helper()
	raiz := t.TempDir()
	whisper := filepath.Join(raiz, "whisper")
	if err := os.MkdirAll(whisper, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, nome := range append([]string{"whisper-server.exe"}, dllsNecessarias...) {
		escrever(t, filepath.Join(whisper, nome), "x")
	}
	modelo := filepath.Join(raiz, "small-q4_0.bin")
	escrever(t, modelo, "modelo")
	return montagem{
		origemWhisper:    whisper,
		origemModelo:     modelo,
		origemDicionario: filepath.Join(raiz, "nao-existe.json"),
		destino:          filepath.Join(raiz, "bin"),
		conferirModelo:   func(string) error { return nil },
		saida:            io.Discard,
	}
}

func existe(caminho string) bool {
	_, err := os.Stat(caminho)
	return err == nil
}

// TestMontar_CriaOMarcadorPortatil: o bin\ continua igual para quem
// desenvolve — o Agent o reconhece como portátil e guarda os dados ali. Sem
// o marcador, um Agent de desenvolvimento abriria o histórico real de quem
// usa o programa instalado.
func TestMontar_CriaOMarcadorPortatil(t *testing.T) {
	m := origemDeMentira(t)
	m.portatil = true
	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}
	if !existe(filepath.Join(m.destino, pastas.Marcador)) {
		t.Fatalf("a montagem não criou %s em %s", pastas.Marcador, m.destino)
	}
	// O que o asrstage cria é o que o pacote pastas reconhece: os dois lados
	// usam o mesmo nome, e os dados vão para a pasta do executável.
	local := filepath.Join(t.TempDir(), "AppData", "Local")
	p, err := pastas.Resolver(pastas.Ambiente{
		Executavel: func() (string, error) { return filepath.Join(m.destino, "aurora-whisper-agent.exe"), nil },
		PastaLocal: func() (string, error) { return local, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Modo() != pastas.ModoPortatil || p.Dados() != m.destino {
		t.Errorf("o Agent leu Modo = %q, Dados = %q; quero portátil e %q", p.Modo(), p.Dados(), m.destino)
	}
	if existe(local) {
		t.Error("o modo portátil criou a pasta local")
	}
}

// TestMontar_OMarcadorVemAntesDaCopia: com o Agent aberto, o .exe e as DLLs
// estão travados e a cópia falha. O bin\ de quem já tem dados do Agent não
// pode ficar sem o marcador por isso.
func TestMontar_OMarcadorVemAntesDaCopia(t *testing.T) {
	m := origemDeMentira(t)
	m.portatil = true
	m.origemWhisper = filepath.Join(t.TempDir(), "nao-existe") // a cópia falha

	if err := montar(m); err == nil {
		t.Fatal("montar não falhou com a origem ausente")
	}
	if !existe(filepath.Join(m.destino, pastas.Marcador)) {
		t.Error("a cópia falhou e o bin\\ ficou sem o marcador: o Agent deixaria de achar os dados")
	}
}

// TestMontar_SemPortatilNaoCriaMarcador: a montagem do pacote do instalador
// (fatia seguinte) não leva o marcador: o programa instalado guarda os dados em
// %LOCALAPPDATA%, e o marcador o desviaria para dentro da pasta que o Setup
// troca na atualização.
func TestMontar_SemPortatilNaoCriaMarcador(t *testing.T) {
	m := origemDeMentira(t)
	m.portatil = false
	if err := montar(m); err != nil {
		t.Fatalf("montar: %v", err)
	}
	if existe(filepath.Join(m.destino, pastas.Marcador)) {
		t.Error("a montagem sem portatil criou o marcador")
	}
}

// TestMontar_ConferirOModeloQueFalha: a verificação do modelo continua
// derrubando a montagem.
func TestMontar_ConferirOModeloQueFalha(t *testing.T) {
	m := origemDeMentira(t)
	m.conferirModelo = func(string) error { return errors.New("checksum diferente") }
	err := montar(m)
	if err == nil || !strings.Contains(err.Error(), "não confere") {
		t.Fatalf("err = %v", err)
	}
}

// TestMontar_CopiaOModeloComONomeQueOAgentProcura guarda que o refator da
// montagem não mudou o que vai para o destino.
func TestMontar_CopiaOModeloEOServidor(t *testing.T) {
	m := origemDeMentira(t)
	if err := montar(m); err != nil {
		t.Fatal(err)
	}
	for _, nome := range append([]string{"whisper-server.exe", model.SmallQ4_0.Arquivo}, dllsNecessarias...) {
		if !existe(filepath.Join(m.destino, nome)) {
			t.Errorf("a montagem não copiou %s", nome)
		}
	}
}

// TestCriarMarcador_NaoReescreveOQueJaExiste: rodar o asrstage de novo não
// mexe no marcador.
func TestCriarMarcador_NaoReescreveOQueJaExiste(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, pastas.Marcador)
	escrever(t, caminho, "do Mauro")

	msg, err := criarMarcador(dir)
	if err != nil {
		t.Fatalf("criarMarcador: %v", err)
	}
	if got := ler(t, caminho); got != "do Mauro" {
		t.Errorf("o marcador foi reescrito: %q", got)
	}
	if !strings.Contains(msg, "ja existe") {
		t.Errorf("a mensagem não diz que já existia: %q", msg)
	}
}

// TestCriarMarcador_ExplicaParaQueServe: quem acha o arquivo ao lado do .exe
// e se pergunta o que é, lê.
func TestCriarMarcador_ExplicaParaQueServe(t *testing.T) {
	dir := t.TempDir()
	if _, err := criarMarcador(dir); err != nil {
		t.Fatal(err)
	}
	texto := ler(t, filepath.Join(dir, pastas.Marcador))
	for _, quer := range []string{"MODO PORTATIL", "historico.db", `%LOCALAPPDATA%\AuroraWhisper`} {
		if !strings.Contains(texto, quer) {
			t.Errorf("o marcador não diz %q:\n%s", quer, texto)
		}
	}
}

// TestCriarMarcador_PastaComOMesmoNomeFalha: uma PASTA chamada portatil não é o
// marcador, e a montagem diz o problema em vez de seguir sem ele.
func TestCriarMarcador_PastaComOMesmoNomeFalha(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, pastas.Marcador), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := criarMarcador(dir); err == nil {
		t.Error("criarMarcador não falhou com uma pasta no lugar do arquivo")
	}
}

// TestMontagemDeDesenvolvimento_LigaOPortatil: o run() de verdade monta o
// bin COM o marcador. É o elo que os outros testes (que chamam montar com a
// montagem que eles mesmos fazem) não cobrem: se run() parasse de pedir o
// modo portátil, todos passariam.
func TestMontagemDeDesenvolvimento_LigaOPortatil(t *testing.T) {
	m := montagemDeDesenvolvimento()
	if m.destino != "bin" || !m.portatil {
		t.Errorf("a montagem de desenvolvimento é destino=%q portatil=%v; quero bin e o marcador ligado", m.destino, m.portatil)
	}
	if m.conferirModelo == nil || m.saida == nil {
		t.Error("a montagem de desenvolvimento deixou o conferidor do modelo ou a saída nulos")
	}
}
