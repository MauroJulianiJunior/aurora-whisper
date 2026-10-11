package cloudauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSegredo(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		s := NovoSegredo()
		if len(s) != TamanhoSegredo || !PareceSegredo(s) {
			t.Fatalf("NovoSegredo = %q", s)
		}
		raw, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil || len(raw) != 32 {
			t.Fatalf("NovoSegredo não é 32 bytes em base64url: %q", s)
		}
		if seen[s] {
			t.Fatal("NovoSegredo repetiu")
		}
		seen[s] = true
	}
	for _, bad := range []string{"", "curto", strings.Repeat("a", 42), strings.Repeat("a", 44),
		strings.Repeat("a", 42) + "=", strings.Repeat("a", 42) + "+", strings.Repeat("a", 42) + "/", strings.Repeat("a", 42) + "é"} {
		if PareceSegredo(bad) {
			t.Errorf("PareceSegredo(%q) = true", bad)
		}
	}
	if !PareceSegredo(strings.Repeat("-", 21) + strings.Repeat("_", 22)) {
		t.Error("PareceSegredo recusa - e _")
	}
}

func TestHashEHMAC(t *testing.T) {
	want := sha256.Sum256([]byte("abc"))
	if !bytes.Equal(Hash("abc"), want[:]) {
		t.Error("Hash não é o SHA-256")
	}
	m := hmac.New(sha256.New, []byte("chave"))
	m.Write([]byte("abc"))
	if !bytes.Equal(HMAC([]byte("chave"), "abc"), m.Sum(nil)) {
		t.Error("HMAC não é o HMAC-SHA256")
	}
	if bytes.Equal(HMAC([]byte("chave"), "abc"), HMAC([]byte("outra"), "abc")) {
		t.Error("o HMAC não depende da chave")
	}
	if bytes.Equal(HMAC([]byte("chave"), "abc"), Hash("abc")) {
		t.Error("o HMAC é o hash puro")
	}
	// RFC 7636, apêndice B.
	if got := DesafioS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Errorf("DesafioS256 = %q", got)
	}
}

func TestContas(t *testing.T) {
	c, err := ParseContas(" Mauro@Exemplo.test , ,outra.conta+teste@gmail.com,")
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d", c.Len())
	}
	for _, ok := range []string{"mauro@exemplo.test", "MAURO@EXEMPLO.TEST", " mauro@exemplo.test ", "outra.conta+teste@gmail.com"} {
		if !c.Permite(ok) {
			t.Errorf("Permite(%q) = false", ok)
		}
	}
	for _, no := range []string{"", "mauro", "outro@exemplo.test", "mauro@exemplo.test.evil", "outraconta+teste@gmail.com"} {
		if c.Permite(no) {
			t.Errorf("Permite(%q) = true", no)
		}
	}
	if (Contas{}).Permite("mauro@exemplo.test") {
		t.Error("a lista zero permite alguém")
	}
	for _, empty := range []string{"", " ", ",, ,"} {
		if _, err := ParseContas(empty); !errors.Is(err, ErrContasVazia) {
			t.Errorf("ParseContas(%q) = %v, quero ErrContasVazia", empty, err)
		}
	}
	for _, bad := range []string{"mauro", "@exemplo.test", "mauro@", "a@b@c.test", "mauro@exemplo", "mauro@.exemplo.test",
		"mauro@exemplo.test.", "mauro@exemplo..test", "Mauro <mauro@exemplo.test>", "mau ro@exemplo.test", "mauro@exemplo.test;x@y.z",
		"ma\x00uro@exemplo.test", strings.Repeat("a", 250) + "@b.test"} {
		_, err := ParseContas("ok@exemplo.test," + bad)
		if err == nil {
			t.Errorf("ParseContas aceitou %q", bad)
			continue
		}
		if strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), "entrada 2") {
			t.Errorf("o erro de %q traz o valor ou não diz a posição: %v", bad, err)
		}
	}
}

func TestValidarVolta(t *testing.T) {
	if v, ok := ValidarVolta(""); !ok || v != "/" {
		t.Errorf("volta vazia = %q, %v", v, ok)
	}
	// "/?janela=entrou": a volta da janela à parte do app (web/src/nuvem/entrar.ts).
	for _, good := range []string{"/", "/nuvem", "/?janela=entrou", "/device/authorize?pedido=0190b3a0-0000-7000-8000-00000000f001",
		"/a/b.c?x=1&y=a%20b", "/~x:y@z+w-", "/" + strings.Repeat("a", 255)} {
		if v, ok := ValidarVolta(good); !ok || v != good {
			t.Errorf("ValidarVolta(%q) = %q, %v", good, v, ok)
		}
	}
	for _, bad := range []string{"//evil.example", "///x", "https://evil.example", "evil.example", "/\\evil.example",
		"\\\\evil", "/ x", "/x\ny", "/x\r\nSet-Cookie:a=b", "/x#y", "/é", "/x\ty", "/" + strings.Repeat("a", 256),
		"javascript:alert(1)", " /x", "/x\x00"} {
		if _, ok := ValidarVolta(bad); ok {
			t.Errorf("ValidarVolta(%q) aceitou", bad)
		}
	}
}

func TestRotulo(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36":                   "Android · Chrome",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0":             "Windows · Edge",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36":                         "Windows · Chrome",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": "iPhone · Safari",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:131.0) Gecko/20100101 Firefox/131.0":                                                    "Mac · Firefox",
		"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0 Mobile Safari/537.36":  "Android · Samsung Internet",
		"curl/8.9.1":                          RotuloPadrao,
		"":                                    RotuloPadrao,
		"Windows qualquer":                    "Windows",
		"Firefox/1 sozinho":                   "Firefox",
		strings.Repeat("x", 5000) + "Android": "Android",
	}
	for ua, want := range cases {
		got := Rotulo(ua)
		if got != want {
			t.Errorf("Rotulo(%.60q) = %q, quero %q", ua, got, want)
		}
		if n := len([]rune(got)); n < 1 || n > 64 {
			t.Errorf("Rotulo(%.60q) tem %d caracteres", ua, n)
		}
	}
}

type relogio struct{ t time.Time }

func (r *relogio) agora() time.Time { return r.t }

func TestLimite(t *testing.T) {
	r := &relogio{t: agora0}
	l := NovoLimite(10, r.agora)
	for i := range 10 {
		if ok, _ := l.Permitir("ip-a", 1); !ok {
			t.Fatalf("pedido %d recusado; o balde começa cheio com 10", i+1)
		}
	}
	ok, wait := l.Permitir("ip-a", 1)
	if ok || wait != 6*time.Second {
		t.Fatalf("11º pedido = %v, espera %v; quero recusado e 6 s (10 por minuto)", ok, wait)
	}
	if ok, _ := l.Permitir("ip-b", 1); !ok {
		t.Fatal("outra chave foi recusada: o balde é por chave")
	}
	r.t = r.t.Add(6 * time.Second)
	if ok, _ := l.Permitir("ip-a", 1); !ok {
		t.Fatal("depois de 6 s, uma ficha voltou e o pedido devia passar")
	}
	if ok, _ := l.Permitir("ip-a", 1); ok {
		t.Fatal("só uma ficha voltou")
	}
	// Um minuto parado enche de novo, sem passar de 10.
	r.t = r.t.Add(10 * time.Minute)
	for i := range 10 {
		if ok, _ := l.Permitir("ip-a", 1); !ok {
			t.Fatalf("pedido %d depois de encher recusado", i+1)
		}
	}
	if ok, _ := l.Permitir("ip-a", 1); ok {
		t.Fatal("o balde passou de 10")
	}
	// n fichas de uma vez (as mutações do push), e n maior que o balde nunca.
	m := NovoLimite(5000, r.agora)
	if ok, _ := m.Permitir("pc", 500); !ok {
		t.Fatal("500 de 5000 recusado")
	}
	for range 9 {
		m.Permitir("pc", 500)
	}
	if ok, wait := m.Permitir("pc", 500); ok || wait != 6*time.Second {
		t.Fatalf("depois de 5000: %v, espera %v; quero recusado e 6 s", ok, wait)
	}
	if ok, _ := m.Permitir("outro", 5001); ok {
		t.Fatal("5001 de uma vez passou")
	}
	if RetryAfter(0) != 1 || RetryAfter(1500*time.Millisecond) != 2 || RetryAfter(6*time.Second) != 6 {
		t.Error("RetryAfter errado")
	}
}

func TestLimite_ArrumaAsChavesCheias(t *testing.T) {
	r := &relogio{t: agora0}
	l := NovoLimite(10, r.agora)
	for i := range maxChaves {
		l.Permitir(strings.Repeat("x", 1)+string(rune(0x1000+i)), 1)
	}
	if len(l.baldes) != maxChaves {
		t.Fatalf("%d baldes", len(l.baldes))
	}
	// Esta gasta o balde inteiro; as outras gastaram uma ficha só. Em 30 s,
	// as outras enchem de novo (e saem), e esta fica pela metade (e fica).
	l.Permitir("ainda-cheio", 10)
	r.t = r.t.Add(30 * time.Second)
	l.Permitir("nova", 1)
	if len(l.baldes) != 2 {
		t.Fatalf("depois de arrumar, %d baldes; quero 2 (a chave nova e a que ainda não encheu)", len(l.baldes))
	}
	// A chave esvaziada continua limitada depois de arrumar.
	if _, ok := l.baldes["ainda-cheio"]; !ok {
		t.Fatal("arrumar apagou um balde que não estava cheio")
	}
}

func TestIPDoCliente(t *testing.T) {
	none, err := ParseProxyConfiavel("")
	if err != nil {
		t.Fatal(err)
	}
	if got := none.IPDoCliente("203.0.113.7:5555", []string{"198.51.100.1"}); got != "203.0.113.7" {
		t.Errorf("sem proxy, o XFF valeu: %q", got)
	}
	one, err := ParseProxyConfiavel("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		remote string
		xff    []string
		want   string
	}{
		{"127.0.0.1:4000", []string{"198.51.100.9"}, "198.51.100.9"},
		{"127.0.0.1:4000", []string{"1.1.1.1, 198.51.100.9"}, "198.51.100.9"},
		{"127.0.0.1:4000", []string{"1.1.1.1", "2.2.2.2,  198.51.100.9 "}, "198.51.100.9"},
		{"127.0.0.1:4000", nil, "127.0.0.1"},
		{"127.0.0.1:4000", []string{"lixo"}, "127.0.0.1"},
		{"127.0.0.1:4000", []string{"198.51.100.9, "}, "198.51.100.9"},
		{"203.0.113.7:4000", []string{"198.51.100.9"}, "203.0.113.7"},
		{"[::ffff:127.0.0.1]:4000", []string{"2001:db8::1"}, "2001:db8::1"},
		{"sem-porta", []string{"198.51.100.9"}, "sem-porta"},
	}
	for _, c := range cases {
		if got := one.IPDoCliente(c.remote, c.xff); got != c.want {
			t.Errorf("IPDoCliente(%q, %q) = %q, quero %q", c.remote, c.xff, got, c.want)
		}
	}
	net, err := ParseProxyConfiavel(" 172.18.0.0/16 ")
	if err != nil {
		t.Fatal(err)
	}
	if got := net.IPDoCliente("172.18.3.4:80", []string{"198.51.100.9"}); got != "198.51.100.9" {
		t.Errorf("rede: %q", got)
	}
	if got := net.IPDoCliente("172.19.0.1:80", []string{"198.51.100.9"}); got != "172.19.0.1" {
		t.Errorf("fora da rede: %q", got)
	}
	// Ligado e Confia (F11c: a linha "servindo" e o aviso do X-Forwarded-For).
	if none.Ligado() || !one.Ligado() || !net.Ligado() {
		t.Errorf("Ligado: vazio %v, um IP %v, uma rede %v", none.Ligado(), one.Ligado(), net.Ligado())
	}
	for _, c := range []struct {
		p      ProxyConfiavel
		remote string
		want   bool
	}{
		{none, "127.0.0.1:80", false},
		{one, "127.0.0.1:80", true},
		{one, "[::ffff:127.0.0.1]:80", true},
		{one, "127.0.0.2:80", false},
		{net, "172.18.255.1:80", true},
		{net, "172.19.0.1:80", false},
		{net, "lixo", false},
	} {
		if got := c.p.Confia(c.remote); got != c.want {
			t.Errorf("Confia(%q) = %v, quero %v", c.remote, got, c.want)
		}
	}
	// A chave do limite: o IPv4 inteiro, o IPv6 por /64.
	for in, want := range map[string]string{
		"203.0.113.7":            "203.0.113.7",
		"::ffff:203.0.113.7":     "203.0.113.7",
		"2001:db8:1:2::1":        "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff:ab::": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":        "2001:db8:1:3::/64",
		"fe80::1%eth0":           "fe80::/64",
		"::1":                    "::/64",
		"sem-porta":              "sem-porta",
	} {
		if got := ChaveDoLimite(in); got != want {
			t.Errorf("ChaveDoLimite(%q) = %q, quero %q", in, got, want)
		}
	}
	for _, bad := range []string{"lixo", "1.2.3.4/99", "localhost"} {
		if _, err := ParseProxyConfiavel(bad); err == nil {
			t.Errorf("ParseProxyConfiavel(%q) aceitou", bad)
		} else if strings.Contains(err.Error(), bad) {
			t.Errorf("o erro traz o valor: %v", err)
		}
	}
}
