package dictionary

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Tudo aqui roda em t.TempDir(): nenhum teste toca o bin\dicionario.json,
// que é o dicionário de verdade de quem usa o Agent.

func arquivoDeTeste(t *testing.T, conteudo string) (*Arquivo, string) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Arquivo{Caminho: caminho}, caminho
}

func lerBytes(t *testing.T, caminho string) []byte {
	t.Helper()
	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// nomesNaPasta lista o que há na pasta do arquivo: depois de qualquer
// operação, só o dicionário (nenhum temporário ficou).
func nomesNaPasta(t *testing.T, caminho string) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Dir(caminho))
	if err != nil {
		t.Fatal(err)
	}
	var nomes []string
	for _, e := range es {
		nomes = append(nomes, e.Name())
	}
	sort.Strings(nomes)
	return nomes
}

func exigirSoOArquivo(t *testing.T, caminho string) {
	t.Helper()
	if got := nomesNaPasta(t, caminho); len(got) != 1 || got[0] != filepath.Base(caminho) {
		t.Errorf("a pasta tem %v, quero só o dicionário", got)
	}
}

// TestArquivo_AprenderEDesfazerDeixaOArquivoIdentico é a garantia central
// do Desfazer: sem mais nada ter mudado, o arquivo volta byte a byte.
func TestArquivo_AprenderEDesfazerDeixaOArquivoIdentico(t *testing.T) {
	versionado, err := os.ReadFile(filepath.Join("..", "..", "testdata", "asr-dataset", "dicionario.json"))
	if err != nil {
		t.Skipf("o dicionário versionado não está aqui: %v", err)
	}
	casos := []struct {
		nome, conteudo, termo, variante string
	}{
		{"termo que já existia", sementeDoPainelJSON, "ECF", "SF"},
		{"termo novo", sementeDoPainelJSON, "Wispr Flow", "Whisperflow"},
		{"termo que existia sem alias", sementeDoPainelJSON, "sped", "SPEDE"},
		{"sem alias_origem no arquivo", "{\n  \"usuario\": \"m\",\n  \"entradas\": [\n    {\n      \"canonica\": \"ECF\",\n      \"prioridade\": 90\n    }\n  ]\n}\n", "ECF", "SF"},
		{"sem entrada nenhuma", "{\n  \"usuario\": \"m\",\n  \"entradas\": []\n}\n", "ECF", "SF"},
		{"o dicionário versionado, termo novo", string(versionado), "ECF", "SF"},
		{"o dicionário versionado, termo que existe", string(versionado), "ECD", "EDC"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			a, caminho := arquivoDeTeste(t, c.conteudo)
			antes := lerBytes(t, caminho)

			if _, _, err := a.Aprender(c.termo, c.variante); err != nil {
				t.Fatalf("Aprender: %v", err)
			}
			depoisDeAprender := lerBytes(t, caminho)
			if bytes.Equal(antes, depoisDeAprender) {
				t.Fatal("aprender não mudou o arquivo")
			}
			if !bytes.Contains(depoisDeAprender, []byte(`"`+c.variante+`": "painel"`)) {
				t.Errorf("o arquivo não marca a origem painel:\n%s", depoisDeAprender)
			}

			if _, _, err := a.Desfazer(c.variante); err != nil {
				t.Fatalf("Desfazer: %v", err)
			}
			if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
				t.Errorf("desfazer não deixou o arquivo idêntico.\nantes:\n%s\ndepois:\n%s", antes, got)
			}
			exigirSoOArquivo(t, caminho)
		})
	}
}

// TestArquivo_AprenderNaoMudaOPromptDoASR: uma variante não entra no prompt
// do ASR, e um termo novo de prioridade 50 fica abaixo do corte do
// dicionário versionado (a menor prioridade que cabe hoje é 85). Por isso a
// troca do dicionário em memória não mexe no vocabulário da subida.
func TestArquivo_AprenderNaoMudaOPromptDoASR(t *testing.T) {
	versionado, err := os.ReadFile(filepath.Join("..", "..", "testdata", "asr-dataset", "dicionario.json"))
	if err != nil {
		t.Skipf("o dicionário versionado não está aqui: %v", err)
	}
	a, caminho := arquivoDeTeste(t, string(versionado))

	promptDe := func() string {
		s, err := CarregarSemente(bytes.NewReader(lerBytes(t, caminho)))
		if err != nil {
			t.Fatal(err)
		}
		return Vocabulario(s.ParaEntradas(), OrcamentoRunasPadrao)
	}
	antes := promptDe()
	if antes == "" {
		t.Fatal("pré-condição: o dicionário versionado não gera prompt")
	}

	if _, _, err := a.Aprender("ECF", "SF"); err != nil { // termo novo
		t.Fatal(err)
	}
	if _, _, err := a.Aprender("ECD", "EDC"); err != nil { // variante de termo que existe
		t.Fatal(err)
	}
	if depois := promptDe(); depois != antes {
		t.Errorf("o prompt do ASR mudou ao aprender:\nantes:  %s\ndepois: %s", antes, depois)
	}
}

// TestArquivo_ReleAntesDeGravar: o dictlearn (outro processo) acrescenta um
// alias depois de o Agent ter carregado o dicionário. O painel tem de
// gravar POR CIMA do que está no disco, e não da cópia que o Agent tem.
func TestArquivo_ReleAntesDeGravar(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)

	// "Outro processo": o que o dictlearn faz — carregar, acrescentar, gravar.
	f, err := os.Open(caminho)
	if err != nil {
		t.Fatal(err)
	}
	doDisco, err := CarregarSemente(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	comExterno, err := doDisco.AcrescentarAlias("SPED", "espedi", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := comExterno.Gravar(caminho); err != nil {
		t.Fatal(err)
	}

	nova, _, err := a.Aprender("ECF", "SF")
	if err != nil {
		t.Fatalf("Aprender: %v", err)
	}
	gravado := string(lerBytes(t, caminho))
	for _, quer := range []string{`"espedi"`, `"SF"`} {
		if !strings.Contains(gravado, quer) {
			t.Errorf("o arquivo perdeu %s:\n%s", quer, gravado)
		}
	}
	if e := entradaDe(t, nova, "SPED"); len(e.Alias) != 1 || e.Alias[0] != "espedi" {
		t.Errorf("a semente devolvida não traz o que o outro processo gravou: %+v", e)
	}

	// E o Desfazer também relê: tira SF e mantém o que o outro acrescentou.
	if _, _, err := a.Desfazer("SF"); err != nil {
		t.Fatalf("Desfazer: %v", err)
	}
	if got := string(lerBytes(t, caminho)); !strings.Contains(got, `"espedi"`) || strings.Contains(got, `"SF"`) {
		t.Errorf("depois de desfazer:\n%s", got)
	}
}

// TestArquivo_ArquivoInvalidoNaoETocado: o que não carrega não é
// reescrito — a pessoa pode estar no meio de uma edição à mão, e
// sobrescrever seria jogar fora o trabalho dela.
func TestArquivo_ArquivoInvalidoNaoETocado(t *testing.T) {
	invalidos := map[string]string{
		"JSON cortado":         `{"usuario":"m","entradas":[{"canonica":"EC`,
		"campo desconhecido":   `{"usuario":"m","entradas":[],"campo_novo":1}`,
		"sem usuário":          `{"entradas":[{"canonica":"ECF","prioridade":1}]}`,
		"arquivo vazio":        ``,
		"não é JSON":           `isto não é um dicionário`,
		"tipo errado no campo": `{"usuario":"m","entradas":[{"canonica":"ECF","prioridade":"alta"}]}`,
	}
	for nome, conteudo := range invalidos {
		t.Run(nome, func(t *testing.T) {
			a, caminho := arquivoDeTeste(t, conteudo)
			antes := lerBytes(t, caminho)

			if _, _, err := a.Aprender("ECF", "SF"); !errors.Is(err, ErrArquivoInvalido) {
				t.Errorf("Aprender: erro = %v, quero ErrArquivoInvalido", err)
			}
			if _, _, err := a.Desfazer("SF"); !errors.Is(err, ErrArquivoInvalido) {
				t.Errorf("Desfazer: erro = %v, quero ErrArquivoInvalido", err)
			}
			if _, err := a.Aprendidas(); !errors.Is(err, ErrArquivoInvalido) {
				t.Errorf("Aprendidas: erro = %v, quero ErrArquivoInvalido", err)
			}
			if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
				t.Errorf("o arquivo inválido foi tocado:\n%s", got)
			}
			exigirSoOArquivo(t, caminho)
		})
	}
}

func TestArquivo_SemArquivoNaoCriaUm(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	a := &Arquivo{Caminho: caminho}
	if _, _, err := a.Aprender("ECF", "SF"); !errors.Is(err, ErrSemArquivo) {
		t.Errorf("Aprender: erro = %v, quero ErrSemArquivo", err)
	}
	if _, _, err := a.Desfazer("SF"); !errors.Is(err, ErrSemArquivo) {
		t.Errorf("Desfazer: erro = %v, quero ErrSemArquivo", err)
	}
	if _, err := a.Aprendidas(); !errors.Is(err, ErrSemArquivo) {
		t.Errorf("Aprendidas: erro = %v, quero ErrSemArquivo", err)
	}
	if got := nomesNaPasta(t, caminho); len(got) != 0 {
		t.Errorf("o painel criou %v: sem dicionário não há usuário para pôr nele", got)
	}
}

// TestArquivo_OutroUsuarioNaoETocado: alguém trocou o dicionário com o
// Agent rodando. O resolver busca pelo usuário da subida; gravar e "trocar"
// um dicionário de outro dono o deixaria mudo sem aviso.
func TestArquivo_OutroUsuarioNaoETocado(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)
	a.Usuario = "outra-pessoa"
	antes := lerBytes(t, caminho)

	if _, _, err := a.Aprender("ECF", "SF"); !errors.Is(err, ErrUsuarioDiferente) {
		t.Errorf("Aprender: erro = %v, quero ErrUsuarioDiferente", err)
	}
	if _, _, err := a.Desfazer("SF"); !errors.Is(err, ErrUsuarioDiferente) {
		t.Errorf("Desfazer: erro = %v, quero ErrUsuarioDiferente", err)
	}
	if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
		t.Error("o arquivo de outro usuário foi tocado")
	}

	a.Usuario = "mauro"
	if _, _, err := a.Aprender("ECF", "SF"); err != nil {
		t.Errorf("com o usuário certo: %v", err)
	}
}

// TestArquivo_ValidarQueRecusaNaoGrava: o que o dicionário em memória não
// carregaria nunca chega ao disco.
func TestArquivo_ValidarQueRecusaNaoGrava(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)
	antes := lerBytes(t, caminho)

	var visto Semente
	recusa := errors.New("a validação recusou")
	a.Validar = func(s Semente) error {
		visto = s
		return recusa
	}
	if _, _, err := a.Aprender("ECF", "SF"); !errors.Is(err, ErrNaoCarrega) || !errors.Is(err, recusa) {
		t.Fatalf("erro = %v, quero o da validação", err)
	}
	if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
		t.Error("gravou apesar de a validação recusar")
	}
	if e := entradaDe(t, visto, "ECF"); len(e.Alias) != 2 {
		t.Errorf("a validação não viu a semente resultante: %+v", e)
	}
	exigirSoOArquivo(t, caminho)
}

// TestArquivo_RecusaNaoToca: uma recusa de regra (variante de outro termo,
// por exemplo) não reescreve o arquivo nem mexe na data dele.
func TestArquivo_RecusaNaoToca(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)
	antes := lerBytes(t, caminho)
	infoAntes, err := os.Stat(caminho)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := a.Aprender("ECF", "DAAS"); !errors.Is(err, ErrVarianteDeOutroTermo) {
		t.Fatalf("erro = %v, quero ErrVarianteDeOutroTermo", err)
	}
	if _, _, err := a.Desfazer("DAAS"); !errors.Is(err, ErrNaoAprendida) {
		t.Fatalf("erro = %v, quero ErrNaoAprendida", err)
	}
	infoDepois, err := os.Stat(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if got := lerBytes(t, caminho); !bytes.Equal(got, antes) || !infoDepois.ModTime().Equal(infoAntes.ModTime()) {
		t.Error("uma recusa reescreveu o arquivo")
	}
	exigirSoOArquivo(t, caminho)
}

func TestArquivo_AprendidasLeOQueEstaNoDisco(t *testing.T) {
	a, _ := arquivoDeTeste(t, sementeDoPainelJSON)
	if got, err := a.Aprendidas(); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("sem nada aprendido: %#v, %v", got, err)
	}
	for _, c := range [][2]string{{"ECF", "SF"}, {"Wispr Flow", "Whisperflow"}} {
		if _, _, err := a.Aprender(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := a.Aprendidas()
	if err != nil {
		t.Fatal(err)
	}
	want := []Aprendida{{Termo: "ECF", Variante: "SF"}, {Termo: "Wispr Flow", Variante: "Whisperflow"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Aprendidas = %+v, want %+v", got, want)
	}

	// Outro processo apagou uma à mão: a lista é a do disco, não a da memória.
	if _, _, err := a.Desfazer("SF"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.Aprendidas(); len(got) != 1 || got[0].Variante != "Whisperflow" {
		t.Errorf("depois de desfazer: %+v", got)
	}
}

// TestArquivo_Concorrencia: várias operações ao mesmo tempo (vários cliques
// em sequência rápida, ou o painel aberto em duas abas). Como cada uma lê,
// muda e grava, sem serialização uma sobrescreveria a outra e a variante
// sumiria; aqui nenhuma some.
//
// Não depende do detector de corrida (este ambiente não tem cgo): o que
// prova a serialização é o resultado — todas as variantes no arquivo, e
// voltando tudo, o arquivo idêntico. Rodar com -count=20 -cpu=1,2,8.
func TestArquivo_Concorrencia(t *testing.T) {
	const n = 24
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)
	antes := lerBytes(t, caminho)

	variantes := make([]string, n)
	for i := range variantes {
		// Metade cai num termo que existe, metade em termos que só o painel
		// cria — e duas goroutines disputam o MESMO termo novo.
		variantes[i] = fmt.Sprintf("v%02d", i)
	}
	termo := func(i int) string {
		if i%2 == 0 {
			return "ECF"
		}
		return fmt.Sprintf("Termo Novo %d", i%4)
	}

	var wg sync.WaitGroup
	erros := make(chan error, 4*n)
	for i := range variantes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := a.Aprender(termo(i), variantes[i]); err != nil {
				erros <- fmt.Errorf("Aprender %d: %w", i, err)
			}
		}()
		wg.Add(1)
		go func() { // leituras no meio das gravações
			defer wg.Done()
			if _, err := a.Aprendidas(); err != nil {
				erros <- fmt.Errorf("Aprendidas %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Error(err)
	}

	got, err := a.Aprendidas()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("aprendeu %d de %d: alguma gravação sobrescreveu outra", len(got), n)
	}
	exigirSoOArquivo(t, caminho)

	// Agora desfaz tudo, também ao mesmo tempo.
	erros = make(chan error, n)
	for i := range variantes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := a.Desfazer(variantes[i]); err != nil {
				erros <- fmt.Errorf("Desfazer %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Error(err)
	}
	if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
		t.Errorf("depois de desfazer tudo o arquivo não voltou ao que era:\n%s", got)
	}
	exigirSoOArquivo(t, caminho)
}
