package dictionary

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Tudo aqui roda em t.TempDir(): nenhum teste toca o dicionario.json de quem
// usa o Agent.

// arquivosDe lista os nomes na pasta, para provar que nenhum temporário ficou.
func arquivosDe(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var nomes []string
	for _, e := range es {
		nomes = append(nomes, e.Name())
	}
	return nomes
}

func TestCriarSeFaltar_CriaOVazioValido(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")

	criou, err := CriarSeFaltar(caminho, "local")
	if err != nil || !criou {
		t.Fatalf("CriarSeFaltar = %v, %v; quero criou=true e nenhum erro", criou, err)
	}

	dados, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	// Carrega como qualquer dicionário: com usuario e sem termo nenhum.
	s, err := CarregarSemente(bytes.NewReader(dados))
	if err != nil {
		t.Fatalf("o dicionário vazio não carrega: %v\n%s", err, dados)
	}
	if s.Usuario != "local" || len(s.Entradas) != 0 {
		t.Errorf("usuario %q, %d entradas; quero \"local\" e nenhuma", s.Usuario, len(s.Entradas))
	}
	// Lista vazia, e não null: é o que Gravar escreveria para uma semente sem
	// termo que veio de um arquivo, e o que um Desfazer deixa.
	if !strings.Contains(string(dados), `"entradas": []`) {
		t.Errorf("o arquivo não traz a lista vazia de entradas:\n%s", dados)
	}
	// O Repositório em memória o aceita (é o que o Agent faz na subida).
	if err := s.Popular(t.Context(), NewInMemoryDictionaryRepository(nil)); err != nil {
		t.Errorf("Popular: %v", err)
	}
	// Nenhum temporário ficou ao lado.
	if nomes := arquivosDe(t, dir); len(nomes) != 1 || nomes[0] != "dicionario.json" {
		t.Errorf("a pasta tem %v; quero só dicionario.json", nomes)
	}
}

// O formato é o de Semente.Gravar: assim o arquivo é tratado como qualquer
// outro e o Desfazer devolve os mesmos bytes.
func TestCriarSeFaltar_TemOFormatoDeGravar(t *testing.T) {
	dir := t.TempDir()
	criado := filepath.Join(dir, "criado.json")
	if _, err := CriarSeFaltar(criado, "local"); err != nil {
		t.Fatal(err)
	}
	gravado := filepath.Join(dir, "gravado.json")
	if err := (Semente{Usuario: "local", Entradas: []EntradaSemente{}}).Gravar(gravado); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(criado)
	b, _ := os.ReadFile(gravado)
	if !bytes.Equal(a, b) {
		t.Errorf("formato diferente do de Gravar:\n%s\n---\n%s", a, b)
	}
}

// O ciclo completo sobre o arquivo criado: aprender, e desfazer devolve o
// arquivo idêntico, byte a byte.
func TestCriarSeFaltar_AprenderEDesfazerSobreOCriado(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if _, err := CriarSeFaltar(caminho, "local"); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(caminho)

	arq := &Arquivo{Caminho: caminho, Usuario: "local"}
	if _, feito, err := arq.Aprender("ECF", "sf"); err != nil || feito.Termo != "ECF" || feito.Variante != "sf" {
		t.Fatalf("Aprender sobre o dicionário vazio: %+v, %v", feito, err)
	}
	if lista, err := arq.Aprendidas(); err != nil || len(lista) != 1 {
		t.Fatalf("Aprendidas = %+v, %v", lista, err)
	}
	if _, _, err := arq.Desfazer("sf"); err != nil {
		t.Fatalf("Desfazer: %v", err)
	}
	depois, _ := os.ReadFile(caminho)
	if !bytes.Equal(original, depois) {
		t.Errorf("desfazer não devolveu o arquivo criado:\n%s\n---\n%s", original, depois)
	}
}

// NUNCA sobrescreve: o que já está no caminho fica byte a byte como estava,
// seja um dicionário válido, lixo, um arquivo vazio, de outro usuário ou
// uma pasta.
func TestCriarSeFaltar_NuncaSobrescreve(t *testing.T) {
	casos := []struct {
		nome     string
		conteudo string // vazio com pasta=true: uma pasta
		pasta    bool
	}{
		{"valido de outro usuario", `{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`, false},
		{"valido do mesmo usuario", `{"usuario":"local","entradas":[{"canonica":"ECF","prioridade":50}]}`, false},
		{"json quebrado", `{`, false},
		{"lixo", "isto nao e json", false},
		{"arquivo vazio", "", false},
		{"marcas de conflito do git", "<<<<<<< HEAD\n{}\n=======\n{}\n>>>>>>> x\n", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			dir := t.TempDir()
			caminho := filepath.Join(dir, "dicionario.json")
			if err := os.WriteFile(caminho, []byte(c.conteudo), 0o644); err != nil {
				t.Fatal(err)
			}
			criou, err := CriarSeFaltar(caminho, "local")
			if err != nil || criou {
				t.Fatalf("CriarSeFaltar = %v, %v; quero criou=false e nenhum erro", criou, err)
			}
			depois, err := os.ReadFile(caminho)
			if err != nil {
				t.Fatal(err)
			}
			if string(depois) != c.conteudo {
				t.Errorf("o arquivo existente foi alterado: %q", depois)
			}
			if nomes := arquivosDe(t, dir); len(nomes) != 1 {
				t.Errorf("a pasta tem %v; quero só o dicionario.json", nomes)
			}
		})
	}

	t.Run("pasta com o nome do arquivo", func(t *testing.T) {
		dir := t.TempDir()
		caminho := filepath.Join(dir, "dicionario.json")
		if err := os.Mkdir(caminho, 0o755); err != nil {
			t.Fatal(err)
		}
		criou, err := CriarSeFaltar(caminho, "local")
		if err != nil || criou {
			t.Fatalf("CriarSeFaltar = %v, %v; quero criou=false e nenhum erro", criou, err)
		}
		if info, err := os.Stat(caminho); err != nil || !info.IsDir() {
			t.Errorf("a pasta foi trocada: %v, %v", info, err)
		}
	})
}

// Chamar de novo depois de criado não muda nada: segunda subida, mesma coisa.
func TestCriarSeFaltar_SegundaChamadaNaoMexe(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if _, err := CriarSeFaltar(caminho, "local"); err != nil {
		t.Fatal(err)
	}
	// A pessoa aprendeu algo no meio tempo.
	arq := &Arquivo{Caminho: caminho, Usuario: "local"}
	if _, _, err := arq.Aprender("ECF", "sf"); err != nil {
		t.Fatal(err)
	}
	antes, _ := os.ReadFile(caminho)

	criou, err := CriarSeFaltar(caminho, "local")
	if err != nil || criou {
		t.Fatalf("segunda chamada = %v, %v; quero criou=false", criou, err)
	}
	depois, _ := os.ReadFile(caminho)
	if !bytes.Equal(antes, depois) {
		t.Error("a segunda chamada apagou o que foi aprendido")
	}
}

func TestCriarSeFaltar_RecusaUsuarioVazio(t *testing.T) {
	for _, usuario := range []string{"", "   ", "\t"} {
		dir := t.TempDir()
		caminho := filepath.Join(dir, "dicionario.json")
		criou, err := CriarSeFaltar(caminho, usuario)
		if !errors.Is(err, ErrUsuarioVazio) || criou {
			t.Errorf("usuario %q: %v, %v; quero ErrUsuarioVazio", usuario, criou, err)
		}
		if nomes := arquivosDe(t, dir); len(nomes) != 0 {
			t.Errorf("usuario %q: criou %v", usuario, nomes)
		}
	}
}

func TestCriarSeFaltar_PastaQueNaoExisteDaErro(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "nao-existe", "dicionario.json")
	criou, err := CriarSeFaltar(caminho, "local")
	if err == nil || criou {
		t.Fatalf("CriarSeFaltar = %v, %v; quero erro", criou, err)
	}
}

// Duas chamadas ao mesmo tempo (o Agent e o dictlearn, ou dois processos):
// uma cria, as outras veem que já existe, e o arquivo está íntegro.
func TestCriarSeFaltar_ConcorrenciaUmSoCria(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")

	const n = 16
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		criou  int
		falhas []error
	)
	partida := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-partida
			c, err := CriarSeFaltar(caminho, "local")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				falhas = append(falhas, err)
			}
			if c {
				criou++
			}
		}()
	}
	close(partida)
	wg.Wait()

	if len(falhas) != 0 {
		t.Fatalf("falhas: %v", falhas)
	}
	if criou != 1 {
		t.Errorf("%d chamadas criaram o arquivo; quero exatamente 1", criou)
	}
	dados, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CarregarSemente(bytes.NewReader(dados)); err != nil {
		t.Errorf("o arquivo ficou inválido: %v\n%s", err, dados)
	}
	if nomes := arquivosDe(t, dir); len(nomes) != 1 {
		t.Errorf("a pasta tem %v; quero só o dicionario.json", nomes)
	}
}

// Alguém cria o arquivo entre a conferência e o link: o dele fica, sem erro.
func TestCriarSeFaltar_ApareceuNoMeioDoCaminho(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")
	alheio := `{"usuario":"mauro","entradas":[]}`

	antigo := ligar
	defer func() { ligar = antigo }()
	ligar = func(de, para string) error {
		// O outro processo chega primeiro.
		if err := os.WriteFile(para, []byte(alheio), 0o644); err != nil {
			t.Fatal(err)
		}
		return antigo(de, para) // o link de verdade recusa: o destino existe
	}

	criou, err := CriarSeFaltar(caminho, "local")
	if err != nil || criou {
		t.Fatalf("CriarSeFaltar = %v, %v; quero criou=false e nenhum erro", criou, err)
	}
	if dados, _ := os.ReadFile(caminho); string(dados) != alheio {
		t.Errorf("o arquivo do outro processo foi sobrescrito: %q", dados)
	}
	if nomes := arquivosDe(t, dir); len(nomes) != 1 {
		t.Errorf("a pasta tem %v; quero só o dicionario.json", nomes)
	}
}

// Sistema de arquivos sem link físico (FAT, exFAT, alguns compartilhamentos):
// cai para criar o arquivo final de forma exclusiva, e ainda assim o resultado
// é o mesmo arquivo.
func TestCriarSeFaltar_SemLinkFisicoCriaDeUmaVez(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")

	antigo := ligar
	defer func() { ligar = antigo }()
	ligar = func(string, string) error { return errors.New("o sistema de arquivos não faz link") }

	criou, err := CriarSeFaltar(caminho, "local")
	if err != nil || !criou {
		t.Fatalf("CriarSeFaltar = %v, %v; quero criou=true", criou, err)
	}
	dados, _ := os.ReadFile(caminho)
	s, err := CarregarSemente(bytes.NewReader(dados))
	if err != nil || s.Usuario != "local" || len(s.Entradas) != 0 {
		t.Errorf("o arquivo criado sem link: %+v, %v\n%s", s, err, dados)
	}
	if nomes := arquivosDe(t, dir); len(nomes) != 1 {
		t.Errorf("a pasta tem %v; quero só o dicionario.json", nomes)
	}
}

// O mesmo caminho sem link físico, com outro processo chegando primeiro: o
// O_EXCL recusa, e o arquivo dele fica.
func TestCriarSeFaltar_SemLinkFisicoNaoSobrescreve(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")
	alheio := `{"usuario":"mauro","entradas":[]}`

	antigo := ligar
	defer func() { ligar = antigo }()
	ligar = func(_, para string) error {
		if err := os.WriteFile(para, []byte(alheio), 0o644); err != nil {
			t.Fatal(err)
		}
		return errors.New("o sistema de arquivos não faz link")
	}

	criou, err := CriarSeFaltar(caminho, "local")
	if err != nil || criou {
		t.Fatalf("CriarSeFaltar = %v, %v; quero criou=false e nenhum erro", criou, err)
	}
	if dados, _ := os.ReadFile(caminho); string(dados) != alheio {
		t.Errorf("o arquivo do outro processo foi sobrescrito: %q", dados)
	}
}

// Escrita que falha depois de o arquivo nascer (sem link físico): o arquivo
// truncado é apagado, e o erro volta. Um dicionário truncado travaria a subida.
func TestCriarSeFaltar_SemLinkFisicoEscritaFalhaApagaOTruncado(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")

	antigoLigar, antigoCriar := ligar, criarExclusivo
	defer func() { ligar, criarExclusivo = antigoLigar, antigoCriar }()
	ligar = func(string, string) error { return errors.New("sem link") }
	criarExclusivo = func(c string) (*os.File, error) {
		f, err := os.OpenFile(c, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		// Um arquivo aberto só para leitura: o Write falha.
		f.Close()
		return os.Open(c)
	}

	criou, err := CriarSeFaltar(caminho, "local")
	if err == nil || criou {
		t.Fatalf("CriarSeFaltar = %v, %v; quero erro", criou, err)
	}
	if _, err := os.Stat(caminho); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("o arquivo truncado ficou: %v", err)
	}
}
