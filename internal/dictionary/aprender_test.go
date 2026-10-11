package dictionary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sementeBase(t *testing.T) Semente {
	t.Helper()
	s, err := CarregarSemente(strings.NewReader(
		`{"usuario":"m","entradas":[{"canonica":"commit","dominio":"dev","prioridade":75}]}`))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	return s
}

func TestAcrescentarAlias_EmEntradaExistente(t *testing.T) {
	got, err := sementeBase(t).AcrescentarAlias("commit", "comite", true)
	if err != nil {
		t.Fatalf("AcrescentarAlias: %v", err)
	}
	if len(got.Entradas) != 1 {
		t.Fatalf("criou entrada nova para termo que já existia: %d entradas", len(got.Entradas))
	}
	e := got.Entradas[0]
	if len(e.AliasRestrito) != 1 || e.AliasRestrito[0] != "comite" {
		t.Errorf("AliasRestrito = %v", e.AliasRestrito)
	}
	if len(e.Alias) != 0 {
		t.Errorf("alias restrito foi para a lista dos comuns: %v", e.Alias)
	}
	if e.Prioridade != 75 {
		t.Errorf("a prioridade existente foi alterada: %d", e.Prioridade)
	}
}

// TestAcrescentarAlias_CriaEntradaQuandoNaoExiste: aprender
// "Supabaese" -> "Supabase" precisa funcionar mesmo que "Supabase"
// nunca tenha sido cadastrado. Exigir cadastro prévio faria a pessoa ter
// de configurar o dicionário ANTES de poder ensiná-lo, que é a ordem
// errada.
func TestAcrescentarAlias_CriaEntradaQuandoNaoExiste(t *testing.T) {
	got, err := sementeBase(t).AcrescentarAlias("Supabase", "Supabaese", false)
	if err != nil {
		t.Fatalf("AcrescentarAlias: %v", err)
	}
	if len(got.Entradas) != 2 {
		t.Fatalf("len(Entradas) = %d, want 2", len(got.Entradas))
	}
	nova := got.Entradas[1]
	if nova.Canonica != "Supabase" || len(nova.Alias) != 1 {
		t.Errorf("entrada nova = %+v", nova)
	}
	if nova.Prioridade <= 0 {
		t.Errorf("entrada nova ficou com prioridade %d — iria para o fim da fila do vocabulário", nova.Prioridade)
	}
}

// TestAcrescentarAlias_NaoMutaAOriginal: a semente recebida não pode ser
// alterada, senão um erro depois da chamada deixaria o estado em memória
// divergente do arquivo em disco.
func TestAcrescentarAlias_NaoMutaAOriginal(t *testing.T) {
	s := sementeBase(t)
	if _, err := s.AcrescentarAlias("commit", "comite", true); err != nil {
		t.Fatalf("AcrescentarAlias: %v", err)
	}
	if len(s.Entradas[0].AliasRestrito) != 0 {
		t.Errorf("a semente original foi mutada: %v", s.Entradas[0].AliasRestrito)
	}
}

func TestAcrescentarAlias_RecusaDuplicado(t *testing.T) {
	s, err := sementeBase(t).AcrescentarAlias("commit", "comite", true)
	if err != nil {
		t.Fatalf("primeira: %v", err)
	}
	if _, err := s.AcrescentarAlias("commit", "COMITE", true); err == nil {
		t.Error("aceitou o mesmo alias duas vezes (variando só a caixa)")
	}
}

// TestAcrescentarAlias_RecusaAliasIgualAoCanonico: o match exato já
// resolve esse caso. Guardar seria ruído no arquivo que a pessoa lê.
func TestAcrescentarAlias_RecusaAliasIgualAoCanonico(t *testing.T) {
	for _, alias := range []string{"commit", "COMMIT", "  commit  "} {
		if _, err := sementeBase(t).AcrescentarAlias("commit", alias, false); err == nil {
			t.Errorf("aceitou %q como alias de commit", alias)
		}
	}
}

func TestAcrescentarAlias_RecusaVazio(t *testing.T) {
	for _, c := range [][2]string{{"", "x"}, {"x", ""}, {"  ", "x"}} {
		if _, err := sementeBase(t).AcrescentarAlias(c[0], c[1], false); err == nil {
			t.Errorf("aceitou canonica=%q alias=%q", c[0], c[1])
		}
	}
}

// TestAcrescentarAlias_MantemOrdemEstavel: sem ordenação, cada
// aprendizado embaralharia a lista e o `git diff` do dicionário
// mostraria mudança onde não houve — o que destrói a auditabilidade que
// é a razão de o dicionário ser um JSON legível.
func TestAcrescentarAlias_MantemOrdemEstavel(t *testing.T) {
	s := sementeBase(t)
	var err error
	for _, a := range []string{"zeta", "alfa", "meio"} {
		if s, err = s.AcrescentarAlias("commit", a, false); err != nil {
			t.Fatalf("AcrescentarAlias(%q): %v", a, err)
		}
	}
	got := s.Entradas[0].Alias
	want := []string{"alfa", "meio", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Alias = %v, want %v", got, want)
		}
	}
}

// TestGravar_EhAtomico prova que uma escrita não deixa o arquivo pela
// metade: um dicionário truncado faria o Agent recusar subir (arquivo
// presente e inválido é erro), e a pessoa perderia o ditado inteiro por
// causa de um alias.
func TestGravar_EhAtomico(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dicionario.json")

	s := sementeBase(t)
	if err := s.Gravar(caminho); err != nil {
		t.Fatalf("Gravar: %v", err)
	}

	// Nenhum temporário ficou para trás.
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 1 {
		var nomes []string
		for _, e := range entradas {
			nomes = append(nomes, e.Name())
		}
		t.Errorf("sobrou lixo no diretório: %v", nomes)
	}

	// E o que foi gravado volta a carregar.
	f, err := os.Open(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lida, err := CarregarSemente(f)
	if err != nil {
		t.Fatalf("o arquivo gravado não recarrega: %v", err)
	}
	if lida.Usuario != s.Usuario || len(lida.Entradas) != len(s.Entradas) {
		t.Errorf("ida e volta perdeu dado: %+v", lida)
	}
}

// TestGravar_SobrescreveSemPerder: gravar por cima de um arquivo
// existente tem de substituí-lo inteiro, não mesclar bytes antigos.
func TestGravar_SobrescreveSemPerder(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "d.json")
	if err := os.WriteFile(caminho, []byte(strings.Repeat("x", 9000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sementeBase(t).Gravar(caminho); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	dados, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dados), "xxxx") {
		t.Error("sobrou conteúdo do arquivo anterior")
	}
}
