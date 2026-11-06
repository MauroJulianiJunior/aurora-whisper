package dictionary

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sementeDeCopia é um dicionário com tudo o que a cópia corta ou guarda: o mapa
// de domínios, um id de amostra, as duas origens, uma entrada desligada, uma
// restrita, variantes restritas e uma variante vazia.
func sementeDeCopia() Semente {
	return Semente{
		Usuario:  "mauro-local",
		Dominios: map[string][]string{"dev": {"Code.exe"}, "contabil": {"Dominio.exe"}},
		AliasOrigem: map[string]string{
			"DAAS":     "cont-04",
			"promptes": OrigemAdicionado,
			"SF":       OrigemPainel,
		},
		Entradas: []EntradaSemente{
			{Canonica: "DAS", Prioridade: 100, Dominio: "contabil", Alias: []string{"DAAS"}},
			{Canonica: "prompts", Prioridade: 100, Alias: []string{"promptes", "  "}, Origem: OrigemAdicionado},
			{Canonica: "ECF", Prioridade: PrioridadeTermoAprendido, Alias: []string{"SF"}, Origem: OrigemPainel},
			{Canonica: "commit", Dominio: "dev", Restrito: true, AliasRestrito: []string{"comite"}},
			{Canonica: "Desligado", Inativa: true, Alias: []string{"desligadu"}},
		},
	}
}

func motivoDe(t *testing.T, err error) MotivoDaCopia {
	t.Helper()
	var e *ErrCopia
	if !errors.As(err, &e) {
		t.Fatalf("erro %v não é *ErrCopia", err)
	}
	return e.Motivo
}

func TestParaACopia_CortaProgramasAmostrasEUsuario(t *testing.T) {
	s := sementeDeCopia()
	c := s.ParaACopia()

	if c.Usuario != UsuarioDaCopia {
		t.Errorf("usuario = %q, quero %q", c.Usuario, UsuarioDaCopia)
	}
	if c.Dominios != nil {
		t.Errorf("o mapa de domínios foi para a cópia: %v", c.Dominios)
	}
	quero := map[string]string{"promptes": OrigemAdicionado, "SF": OrigemPainel}
	if !reflect.DeepEqual(c.AliasOrigem, quero) {
		t.Errorf("alias_origem = %v, quero %v", c.AliasOrigem, quero)
	}
	if len(c.Entradas) != len(s.Entradas) {
		t.Fatalf("entradas = %d, quero %d (as desligadas também vão)", len(c.Entradas), len(s.Entradas))
	}
	if !c.Entradas[4].Inativa || !c.Entradas[3].Restrito || c.Entradas[3].Dominio != "dev" {
		t.Errorf("a cópia mudou os campos das entradas: %+v", c.Entradas)
	}
	if !reflect.DeepEqual(c.Entradas[1].Alias, []string{"promptes"}) {
		t.Errorf("a variante vazia não saiu: %q", c.Entradas[1].Alias)
	}
	// A original não muda.
	if s.Usuario != "mauro-local" || len(s.Dominios) != 2 || len(s.AliasOrigem) != 3 ||
		len(s.Entradas[1].Alias) != 2 {
		t.Errorf("ParaACopia mexeu na semente original: %+v", s)
	}
}

func TestBytesDaCopia_OsMesmosTermosDaoOsMesmosBytes(t *testing.T) {
	a, shaA, err := sementeDeCopia().BytesDaCopia()
	if err != nil {
		t.Fatal(err)
	}
	// Os mapas são novos a cada chamada: a ordem de inserção muda e os bytes não.
	for i := 0; i < 20; i++ {
		b, shaB, err := sementeDeCopia().BytesDaCopia()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) || shaA != shaB {
			t.Fatalf("a mesma semente deu bytes diferentes:\n%s\n%s", a, b)
		}
	}
	if shaA != SHA256DaCopia(a) || len(shaA) != 64 || strings.ToLower(shaA) != shaA {
		t.Errorf("sha256 = %q, não é o SHA-256 dos bytes em hexadecimal minúsculo", shaA)
	}
	for _, nunca := range []string{"Code.exe", "Dominio.exe", "dominios", "cont-04", "mauro-local"} {
		if bytes.Contains(a, []byte(nunca)) {
			t.Errorf("a cópia leva %q:\n%s", nunca, a)
		}
	}
}

func TestLerCopia_DevolveACopiaQueOPCMandou(t *testing.T) {
	b, _, err := sementeDeCopia().BytesDaCopia()
	if err != nil {
		t.Fatal(err)
	}
	got, err := LerCopia(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sementeDeCopia().ParaACopia()) {
		t.Errorf("ida e volta mudou a cópia:\n got %+v\nwant %+v", got, sementeDeCopia().ParaACopia())
	}
}

// O dicionário de verdade do dataset (o da medição do portão) vira cópia, e a
// nuvem a lê de volta.
func TestBytesDaCopia_ODicionarioDoDataset(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "testdata", "asr-dataset", "dicionario.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := CarregarSemente(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.BytesDaCopia()
	if err != nil {
		t.Fatalf("o dicionário do dataset não vira cópia: %v", err)
	}
	if bytes.Contains(b, []byte(".exe")) {
		t.Errorf("a cópia leva um executável")
	}
	if _, err := LerCopia(b); err != nil {
		t.Fatalf("a nuvem recusou a cópia do dataset: %v", err)
	}
}

func TestLerCopia_RecusaOQueNaoEhCopia(t *testing.T) {
	inteiro := `{"usuario":"copia","dominios":{"dev":["Code.exe"]},"entradas":[{"canonica":"DAS","prioridade":1}]}`
	casos := map[string]string{
		"lixo":              `não é json`,
		"campo novo":        `{"usuario":"copia","entradas":[],"versao":2}`,
		"sem usuario":       `{"entradas":[]}`,
		"o usuario local":   `{"usuario":"mauro","entradas":[]}`,
		"o mapa de domínio": inteiro,
		"id de amostra":     `{"usuario":"copia","alias_origem":{"DAAS":"cont-04"},"entradas":[]}`,
		"dois objetos":      `{"usuario":"copia","entradas":[]}{"usuario":"copia","entradas":[]}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := LerCopia([]byte(corpo))
			if err == nil {
				t.Fatal("aceitou")
			}
			if m := motivoDe(t, err); m != MotivoCopiaJSON {
				t.Errorf("motivo = %q, quero %q", m, MotivoCopiaJSON)
			}
		})
	}
}

func TestLerCopia_GrandeDemais(t *testing.T) {
	b := []byte(`{"usuario":"copia","entradas":[]}` + strings.Repeat(" ", MaxBytesDaCopia))
	if m := motivoDe(t, func() error { _, err := LerCopia(b); return err }()); m != MotivoCopiaGrandeDemais {
		t.Errorf("motivo = %q, quero %q", m, MotivoCopiaGrandeDemais)
	}
	// No limite exato, passa (o espaço depois do objeto é JSON válido).
	pad := MaxBytesDaCopia - len(`{"usuario":"copia","entradas":[]}`)
	if _, err := LerCopia([]byte(`{"usuario":"copia","entradas":[]}` + strings.Repeat(" ", pad))); err != nil {
		t.Errorf("no limite exato: %v", err)
	}
}

func termos(n int) []EntradaSemente {
	out := make([]EntradaSemente, n)
	for i := range out {
		out[i] = EntradaSemente{Canonica: "termo" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + itoa(i)}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

func variantes(n int, prefixo string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefixo + itoa(i)
	}
	return out
}

// Cada limite recusa no número exato acima e aceita no próprio limite.
func TestValidarCopia_OsLimites(t *testing.T) {
	ok := func(nome string, s Semente) {
		t.Helper()
		if err := ValidarCopia(s); err != nil {
			t.Errorf("%s: recusou no limite: %v", nome, err)
		}
	}
	recusa := func(nome string, s Semente, quero MotivoDaCopia) {
		t.Helper()
		err := ValidarCopia(s)
		if err == nil {
			t.Errorf("%s: aceitou acima do limite", nome)
			return
		}
		if m := motivoDe(t, err); m != quero {
			t.Errorf("%s: motivo = %q, quero %q", nome, m, quero)
		}
	}

	ok("termos", Semente{Entradas: termos(MaxTermosDaCopia)})
	recusa("termos", Semente{Entradas: termos(MaxTermosDaCopia + 1)}, MotivoCopiaTermosDemais)

	um := func(alias, restrito []string) Semente {
		return Semente{Entradas: []EntradaSemente{{Canonica: "x", Alias: alias, AliasRestrito: restrito}}}
	}
	ok("variantes por termo", um(variantes(30, "a"), variantes(20, "r")))
	recusa("variantes por termo", um(variantes(30, "a"), variantes(21, "r")), MotivoCopiaVariantesDemais)

	cheio := termos(MaxVariantesDaCopia / MaxVariantesPorTermoDaCopia)
	for i := range cheio {
		cheio[i].Alias = variantes(MaxVariantesPorTermoDaCopia, "v"+itoa(i)+"-")
	}
	ok("variantes no total", Semente{Entradas: cheio})
	recusa("variantes no total",
		Semente{Entradas: append(cheio, EntradaSemente{Canonica: "mais", Alias: []string{"maiss"}})},
		MotivoCopiaVariantesDemais)

	// 80 caracteres, não 80 bytes: "é" tem 2 bytes.
	oitenta := strings.Repeat("é", 80)
	ok("termo", Semente{Entradas: []EntradaSemente{{Canonica: oitenta}}})
	recusa("termo", Semente{Entradas: []EntradaSemente{{Canonica: oitenta + "é"}}}, MotivoCopiaTermoInvalido)
	ok("variante", um([]string{oitenta}, nil))
	recusa("variante", um([]string{oitenta + "é"}, nil), MotivoCopiaTermoInvalido)
	recusa("variante restrita", um(nil, []string{oitenta + "é"}), MotivoCopiaTermoInvalido)
	recusa("domínio", Semente{Entradas: []EntradaSemente{{Canonica: "x", Dominio: oitenta + "é"}}}, MotivoCopiaTermoInvalido)
	recusa("idioma", Semente{Entradas: []EntradaSemente{{Canonica: "x", Idioma: oitenta + "é"}}}, MotivoCopiaTermoInvalido)
	recusa("termo vazio", Semente{Entradas: []EntradaSemente{{Canonica: "  "}}}, MotivoCopiaTermoInvalido)
}

// O PC confere antes de mandar: o arquivo fora do limite não vira bytes.
func TestBytesDaCopia_ForaDoLimiteNaoSobe(t *testing.T) {
	s := Semente{Usuario: "local", Entradas: termos(MaxTermosDaCopia + 1)}
	b, sha, err := s.BytesDaCopia()
	if b != nil || sha != "" {
		t.Errorf("devolveu bytes fora do limite")
	}
	if m := motivoDe(t, err); m != MotivoCopiaTermosDemais {
		t.Errorf("motivo = %q, quero %q", m, MotivoCopiaTermosDemais)
	}
}

// O erro não leva o termo.
func TestErrCopia_SemOTexto(t *testing.T) {
	segredo := "Cliente Fulano de Tal " + strings.Repeat("z", 80)
	err := ValidarCopia(Semente{Entradas: []EntradaSemente{{Canonica: segredo}}})
	if err == nil || strings.Contains(err.Error(), "Fulano") {
		t.Errorf("erro = %v", err)
	}
}
