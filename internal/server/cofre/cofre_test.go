package cofre

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// As chaves de cifra dos testes são montadas aqui, de mentira: nenhuma é de
// verdade.
var (
	chaveDeCifraA = strings.Repeat("0123456789abcdef", 4)
	chaveDeCifraB = strings.Repeat("fedcba9876543210", 4)
)

// chaveDaPessoa tem cara de chave de API sem ser uma.
const chaveDaPessoa = "sk-canario-do-cofre-0123456789abcdefghijklmnopqrstuvwxyz"

const (
	usuarioA = "0190b3a0-0000-7000-8000-00000000000a"
	usuarioB = "0190b3a0-0000-7000-8000-00000000000b"
)

func novo(t *testing.T, h string) *Cofre {
	t.Helper()
	c, err := Novo(h)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return c
}

func TestSelarEAbrir(t *testing.T) {
	c := novo(t, chaveDeCifraA)
	s, err := c.Selar(usuarioA, "openai", chaveDaPessoa)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.IDDaCifra) != TamanhoDoID || len(s.Nonce) != TamanhoDoNonce || len(s.Cifrado) != len(chaveDaPessoa)+TamanhoDaEtiqueta {
		t.Fatalf("tamanhos: id %d, nonce %d, cifrado %d", len(s.IDDaCifra), len(s.Nonce), len(s.Cifrado))
	}
	if bytes.Contains(s.Cifrado, []byte(chaveDaPessoa)) || bytes.Contains(s.Cifrado, []byte(chaveDaPessoa[:12])) {
		t.Fatal("a chave aparece em claro no cifrado")
	}
	got, err := c.Abrir(usuarioA, "openai", s)
	if err != nil || got != chaveDaPessoa {
		t.Fatalf("Abrir = %q, %v", got, err)
	}
	// Outro cofre com a MESMA chave de cifra (o servidor reiniciado) abre.
	if got, err := novo(t, strings.ToUpper(chaveDeCifraA)).Abrir(usuarioA, "openai", s); err != nil || got != chaveDaPessoa {
		t.Fatalf("o mesmo arquivo em maiúsculas não abriu: %v", err)
	}
}

// O nonce é novo a cada gravação: a mesma chave, cifrada duas vezes, dá dois
// cifrados diferentes.
func TestSelar_NonceNovoACadaVez(t *testing.T) {
	c := novo(t, chaveDeCifraA)
	vistos := map[string]bool{}
	for range 200 {
		s, err := c.Selar(usuarioA, "openai", chaveDaPessoa)
		if err != nil {
			t.Fatal(err)
		}
		if vistos[string(s.Nonce)] || vistos[string(s.Cifrado)] {
			t.Fatal("nonce ou cifrado repetido")
		}
		vistos[string(s.Nonce)], vistos[string(s.Cifrado)] = true, true
	}
}

// O AAD prende o cifrado ao usuário, ao provedor e ao id: copiado para outra
// conta ou outro provedor, não decifra; trocado um byte, não decifra.
func TestAbrir_OAADPrendeUsuarioEProvedor(t *testing.T) {
	c := novo(t, chaveDeCifraA)
	s, err := c.Selar(usuarioA, "openai", chaveDaPessoa)
	if err != nil {
		t.Fatal(err)
	}
	for nome, abrir := range map[string]func() (string, error){
		"outro usuário":         func() (string, error) { return c.Abrir(usuarioB, "openai", s) },
		"outro provedor":        func() (string, error) { return c.Abrir(usuarioA, "groq", s) },
		"usuário em maiúsculas": func() (string, error) { return c.Abrir(strings.ToUpper(usuarioA), "openai", s) },
		"usuário vazio":         func() (string, error) { return c.Abrir("", "openai", s) },
		"nonce trocado": func() (string, error) {
			x := s
			x.Nonce = append([]byte(nil), s.Nonce...)
			x.Nonce[0] ^= 1
			return c.Abrir(usuarioA, "openai", x)
		},
		"cifrado trocado": func() (string, error) {
			x := s
			x.Cifrado = append([]byte(nil), s.Cifrado...)
			x.Cifrado[3] ^= 1
			return c.Abrir(usuarioA, "openai", x)
		},
		"etiqueta cortada": func() (string, error) {
			x := s
			x.Cifrado = s.Cifrado[:len(s.Cifrado)-1]
			return c.Abrir(usuarioA, "openai", x)
		},
		"nonce curto": func() (string, error) {
			x := s
			x.Nonce = s.Nonce[:11]
			return c.Abrir(usuarioA, "openai", x)
		},
	} {
		got, err := abrir()
		if !errors.Is(err, ErrNaoDecifra) || got != "" {
			t.Errorf("%s: %q, %v; quero ErrNaoDecifra", nome, got, err)
		}
		if err != nil && strings.Contains(err.Error(), chaveDaPessoa[:12]) {
			t.Errorf("%s: o erro carrega a chave", nome)
		}
	}
	// Cada parte vai com o tamanho na frente: o usuário "ab" com o provedor
	// "c" não é o usuário "a" com o provedor "bc".
	if !bytes.Equal(aad("a", "bc", nil), aad("a", "bc", nil)) || bytes.Equal(aad("ab", "c", nil), aad("a", "bc", nil)) {
		t.Error("o AAD não separa o usuário do provedor")
	}
}

// Trocar o arquivo leva a chave guardada a ErrOutraChaveDeCifra, sem tentar
// decifrar; e o id adulterado para o da chave nova também não abre (o AAD o
// prende).
func TestAbrir_OutraChaveDeCifra(t *testing.T) {
	a, b := novo(t, chaveDeCifraA), novo(t, chaveDeCifraB)
	if bytes.Equal(a.ID(), b.ID()) {
		t.Fatal("duas chaves de cifra com o mesmo id")
	}
	s, err := a.Selar(usuarioA, "openai", chaveDaPessoa)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Abrir(usuarioA, "openai", s); !errors.Is(err, ErrOutraChaveDeCifra) || got != "" {
		t.Fatalf("com a chave de cifra nova: %q, %v; quero ErrOutraChaveDeCifra", got, err)
	}
	s.IDDaCifra = b.ID()
	if got, err := b.Abrir(usuarioA, "openai", s); !errors.Is(err, ErrNaoDecifra) || got != "" {
		t.Fatalf("com o id trocado: %q, %v; quero ErrNaoDecifra", got, err)
	}
	if a.MesmoID(b.ID()) || !a.MesmoID(a.ID()) || a.MesmoID(nil) || a.MesmoID(a.ID()[:7]) {
		t.Error("MesmoID")
	}
}

// A derivação, conferida por fora: a subchave é o HKDF-SHA256 da chave de
// cifra com o rótulo da cifra, e o id é o HMAC-SHA256 dela com o rótulo do id,
// truncado. A chave de cifra crua NUNCA cifra: um cifrado feito com ela não
// abre.
func TestDerivacao(t *testing.T) {
	segredo, err := hex.DecodeString(chaveDeCifraA)
	if err != nil {
		t.Fatal(err)
	}
	c := novo(t, chaveDeCifraA)
	m := hmac.New(sha256.New, segredo)
	m.Write([]byte("aurora-whisper/ai-keys/id/v1"))
	if !bytes.Equal(c.ID(), m.Sum(nil)[:8]) {
		t.Fatal("o id não é o HMAC truncado com o rótulo do id")
	}
	sub, err := hkdf.Key(sha256.New, segredo, nil, "aurora-whisper/ai-keys/v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sub, segredo) {
		t.Fatal("a subchave é a chave crua")
	}
	s, err := c.Selar(usuarioA, "openai", chaveDaPessoa)
	if err != nil {
		t.Fatal(err)
	}
	abrirCom := func(chave []byte) error {
		bloco, err := aes.NewCipher(chave)
		if err != nil {
			t.Fatal(err)
		}
		gcm, err := cipher.NewGCM(bloco)
		if err != nil {
			t.Fatal(err)
		}
		_, err = gcm.Open(nil, s.Nonce, s.Cifrado, aad(usuarioA, "openai", s.IDDaCifra))
		return err
	}
	if err := abrirCom(sub); err != nil {
		t.Fatalf("a subchave do HKDF não abre o cifrado: %v", err)
	}
	if err := abrirCom(segredo); err == nil {
		t.Fatal("a chave de cifra crua abriu o cifrado: ela foi usada sem a derivação")
	}
}

// O arquivo malformado é recusado sem mostrar o conteúdo.
func TestNovo_Malformado(t *testing.T) {
	for nome, conteudo := range map[string]string{
		"vazio":                 "",
		"63 caracteres":         chaveDeCifraA[:63],
		"65 caracteres":         chaveDeCifraA + "0",
		"128 caracteres":        chaveDeCifraA + chaveDeCifraB,
		"não hexadecimal":       "zz" + chaveDeCifraA[2:],
		"com espaço":            " " + chaveDeCifraA[1:],
		"com fim de linha":      chaveDeCifraA[:63] + "\n",
		"base64 de 32 bytes":    strings.Repeat("QUJD", 10) + "QUJDREVGR0hJSktMTU5PUFFSU1RVVlc=",
		"32 bytes crus":         strings.Repeat("\x01", 32),
		"64 caracteres não-hex": strings.Repeat("g", 64),
	} {
		c, err := Novo(conteudo)
		if !errors.Is(err, ErrChaveDeCifraMalformada) || c != nil {
			t.Errorf("%s: %v, %v; quero ErrChaveDeCifraMalformada", nome, c, err)
			continue
		}
		if len(conteudo) > 4 && strings.Contains(err.Error(), conteudo[1:5]) {
			t.Errorf("%s: a mensagem mostra o conteúdo: %q", nome, err)
		}
	}
}

func TestSelar_Vazio(t *testing.T) {
	c := novo(t, chaveDeCifraA)
	for _, p := range [][3]string{{"", "openai", chaveDaPessoa}, {usuarioA, "", chaveDaPessoa}, {usuarioA, "openai", ""}} {
		if _, err := c.Selar(p[0], p[1], p[2]); !errors.Is(err, ErrVazia) {
			t.Errorf("Selar(%q, %q, ...): %v, quero ErrVazia", p[0], p[1], err)
		}
	}
}

// ID devolve uma cópia: quem a muda não muda o cofre.
func TestID_EhUmaCopia(t *testing.T) {
	c := novo(t, chaveDeCifraA)
	id := c.ID()
	id[0] ^= 0xff
	if bytes.Equal(id, c.ID()) {
		t.Fatal("ID devolveu o array do cofre")
	}
}
