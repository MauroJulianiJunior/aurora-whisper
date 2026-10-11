package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escreve cria um arquivo com o conteúdo dado e devolve o Descriptor que o
// descreve corretamente — a base para os testes mutarem UMA coisa por vez.
func escreve(t *testing.T, dir, nome string, conteudo []byte) Descriptor {
	t.Helper()
	caminho := filepath.Join(dir, nome)
	if err := os.WriteFile(caminho, conteudo, 0o644); err != nil {
		t.Fatal(err)
	}
	soma := sha256.Sum256(conteudo)
	return Descriptor{
		Nome:    "modelo-de-teste",
		Arquivo: nome,
		SHA256:  hex.EncodeToString(soma[:]),
		Bytes:   int64(len(conteudo)),
	}
}

func TestResolve_ModeloIntegro(t *testing.T) {
	dir := t.TempDir()
	d := escreve(t, dir, "m.bin", []byte("conteúdo do modelo"))

	caminho, err := Resolve(dir, d)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if caminho != filepath.Join(dir, "m.bin") {
		t.Errorf("caminho = %q", caminho)
	}
}

// TestResolve_AusenteEhErroDistinguivel: "instale de novo" e "o arquivo
// chegou quebrado" pedem ações diferentes de quem usa. Um erro genérico
// faria as duas virarem a mesma mensagem inútil.
func TestResolve_AusenteEhErroDistinguivel(t *testing.T) {
	dir := t.TempDir()
	d := Descriptor{Nome: "x", Arquivo: "nao-existe.bin", SHA256: "abc", Bytes: 10}

	_, err := Resolve(dir, d)
	if err == nil {
		t.Fatal("Resolve = nil, want erro")
	}
	if !errors.Is(err, ErrAusente) {
		t.Errorf("err = %v, want que embrulhe ErrAusente", err)
	}
	if errors.Is(err, ErrCorrompido) {
		t.Error("arquivo ausente foi classificado como corrompido")
	}
}

// TestResolve_ConteudoTrocadoEhCorrompido é o teste que justifica o
// checksum existir.
//
// O arquivo está lá, tem o nome certo e o tamanho certo — só o CONTEÚDO é
// outro. Sem verificar o hash, o Agent subiria feliz e produziria
// transcrição ruim, e a causa apareceria como "o modelo é ruim" em vez de
// "o modelo não é o que você pensa". Este projeto já confundiu essas duas
// coisas duas vezes.
func TestResolve_ConteudoTrocadoEhCorrompido(t *testing.T) {
	dir := t.TempDir()
	d := escreve(t, dir, "m.bin", []byte("modelo legitimo"))
	// Mesmo tamanho, conteúdo diferente.
	if err := os.WriteFile(filepath.Join(dir, "m.bin"), []byte("modelo trocado"+"!"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(dir, d)
	if err == nil {
		t.Fatal("Resolve = nil, want erro — conteúdo trocado passou pela verificação")
	}
	if !errors.Is(err, ErrCorrompido) {
		t.Errorf("err = %v, want que embrulhe ErrCorrompido", err)
	}
	// A mensagem precisa trazer os dois hashes: sem eles, quem lê não
	// consegue saber se o arquivo é outro modelo conhecido ou lixo.
	if !strings.Contains(err.Error(), d.SHA256) {
		t.Errorf("mensagem não traz o sha256 esperado: %v", err)
	}
}

func TestResolve_TamanhoDiferenteEhCorrompido(t *testing.T) {
	dir := t.TempDir()
	d := escreve(t, dir, "m.bin", []byte("conteudo completo do modelo"))
	if err := os.WriteFile(filepath.Join(dir, "m.bin"), []byte("cortad"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(dir, d)
	if !errors.Is(err, ErrCorrompido) {
		t.Fatalf("err = %v, want ErrCorrompido para download incompleto", err)
	}
	if !strings.Contains(err.Error(), "bytes") {
		t.Errorf("mensagem devia falar de tamanho: %v", err)
	}
}

func TestResolve_DiretorioNoLugarDoArquivo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "m.bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := Descriptor{Nome: "x", Arquivo: "m.bin", SHA256: "abc", Bytes: 1}

	if _, err := Resolve(dir, d); !errors.Is(err, ErrAusente) {
		t.Fatalf("err = %v, want ErrAusente para diretório no lugar do arquivo", err)
	}
}

// TestResolve_SemTamanhoEsperadoAindaVerificaChecksum: Bytes zero significa
// "não sei o tamanho", nunca "não precisa verificar".
func TestResolve_SemTamanhoEsperadoAindaVerificaChecksum(t *testing.T) {
	dir := t.TempDir()
	d := escreve(t, dir, "m.bin", []byte("original"))
	d.Bytes = 0
	if err := os.WriteFile(filepath.Join(dir, "m.bin"), []byte("outro"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve(dir, d); !errors.Is(err, ErrCorrompido) {
		t.Fatalf("err = %v, want ErrCorrompido — sem tamanho esperado o checksum ainda tem de barrar", err)
	}
}

// TestSmallQ4_0_DescritorCoerente confere o descritor do modelo real contra
// ele mesmo. Não valida o arquivo (que não está no git), mas pega erro de
// digitação no hash ou no tamanho, que de outro jeito só apareceria na
// máquina de quem instalasse.
func TestSmallQ4_0_DescritorCoerente(t *testing.T) {
	if len(SmallQ4_0.SHA256) != 64 {
		t.Errorf("SHA256 tem %d caracteres, want 64", len(SmallQ4_0.SHA256))
	}
	if _, err := hex.DecodeString(SmallQ4_0.SHA256); err != nil {
		t.Errorf("SHA256 não é hexadecimal válido: %v", err)
	}
	if SmallQ4_0.SHA256 != strings.ToLower(SmallQ4_0.SHA256) {
		t.Error("SHA256 precisa estar em minúsculas — a comparação em Resolve é literal")
	}
	if SmallQ4_0.Bytes <= 0 {
		t.Error("Bytes não foi preenchido")
	}
	if !strings.HasSuffix(SmallQ4_0.Arquivo, ".bin") {
		t.Errorf("Arquivo = %q, want terminando em .bin", SmallQ4_0.Arquivo)
	}
	if SmallQ4_0.Nome == "" {
		t.Error("Nome vazio — o AC-043 exige mostrar o modelo no histórico")
	}
}

// TestResolve_ModeloRealQuandoPresente roda contra o arquivo de verdade
// quando ele existe nesta máquina, e PULA quando não existe.
//
// O modelo não está no git (145 MB, e o .gitignore bloqueia *.bin), então
// amarrar a suíte a ele deixaria o CI vermelho para sempre — foi
// exatamente o erro que deixou o CI quebrado por vários commits no Sprint 3.
func TestResolve_ModeloRealQuandoPresente(t *testing.T) {
	const dir = "../../../models/quant"
	caminho := filepath.Join(dir, "small-q4_0.bin")
	if _, err := os.Stat(caminho); err != nil {
		t.Skipf("pulando: %s não existe nesta máquina (o modelo fica fora do git)", caminho)
	}

	d := SmallQ4_0
	d.Arquivo = "small-q4_0.bin" // nome local; o instalador usa SmallQ4_0.Arquivo
	if _, err := Resolve(dir, d); err != nil {
		t.Fatalf("Resolve no modelo real: %v\n"+
			"O descritor em SmallQ4_0 não bate com o arquivo medido no Sprint 3.", err)
	}
}
