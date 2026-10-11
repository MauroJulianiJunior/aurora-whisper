package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
)

// A configuração do "serve" (10-env-and-secrets.md; ADR-019, F3). Toda falta
// ou erro encerra com saída 2, e a mensagem diz o NOME da variável, nunca o
// valor: a configuração tem segredos, e a mensagem vai para o terminal e para
// o log do serviço.

// As variáveis.
const (
	envPublicBaseURL    = "PUBLIC_BASE_URL"
	envHTTPAddr         = "HTTP_ADDR"
	envSessionSecret    = "SESSION_SECRET"
	envDeviceTokenPep   = "DEVICE_TOKEN_PEPPER"
	envContasPermitidas = "CONTAS_PERMITIDAS"
	envGoogleSecretFile = "GOOGLE_CLIENT_SECRET_FILE"
	envProxyConfiavel   = "PROXY_CONFIAVEL"
	envLogLevel         = "LOG_LEVEL"
	envPainelDir        = "PAINEL_DIR"
)

// defaultHTTPAddr: só a própria máquina. Na VPS, quem fica na frente é o
// proxy do compose (F11).
const defaultHTTPAddr = "127.0.0.1:8080"

// maxArquivoGoogle: o JSON do client do Google tem menos de 1 KiB.
const maxArquivoGoogle = 64 << 10

// sufixoArquivo é o da variável que aponta para o arquivo de um segredo
// (F11c): DATABASE_URL_FILE, SESSION_SECRET_FILE e DEVICE_TOKEN_PEPPER_FILE.
// No contêiner, o segredo é um arquivo montado só de leitura, e não uma
// variável de ambiente, que o "docker inspect" mostra e que vaza para os
// processos filhos.
const sufixoArquivo = "_FILE"

// maxArquivoSegredo: um segredo é uma linha; a maior, a DATABASE_URL, tem
// algumas centenas de bytes.
const maxArquivoSegredo = 4 << 10

// lerSegredo devolve o valor de nome, lido da variável comum ou do arquivo de
// nome_FILE. As duas definidas juntas são erro de configuração (não dá para
// saber qual vale). Do arquivo, tira só o fim de linha do final (\n ou \r\n),
// recusa o vazio, o que tem mais de uma linha, o que não é um arquivo comum e
// o maior que maxArquivoSegredo. Nenhuma mensagem traz o valor nem o caminho.
// Sem nenhuma das duas, devolve "" sem erro: quem chama diz o que falta.
func lerSegredo(getenv func(string) string, nome string) (string, error) {
	valor, caminho := getenv(nome), getenv(nome+sufixoArquivo)
	switch {
	case valor != "" && caminho != "":
		return "", errConfig(nome, "está definida junto com "+nome+sufixoArquivo+": defina só uma das duas")
	case caminho == "":
		return valor, nil
	}
	variavel := nome + sufixoArquivo
	f, err := os.Open(caminho)
	if err != nil {
		return "", errConfig(variavel, "não consegui abrir o arquivo")
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return "", errConfig(variavel, "não é um arquivo comum")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxArquivoSegredo+1))
	if err != nil {
		return "", errConfig(variavel, "não consegui ler o arquivo")
	}
	if len(raw) > maxArquivoSegredo {
		return "", errConfig(variavel, "o arquivo é grande demais para ser um segredo de uma linha")
	}
	s := string(raw)
	if strings.HasSuffix(s, "\r\n") {
		s = s[:len(s)-2]
	} else if strings.HasSuffix(s, "\n") {
		s = s[:len(s)-1]
	}
	switch {
	case s == "":
		return "", errConfig(variavel, "o arquivo está vazio")
	case strings.ContainsAny(s, "\r\n"):
		return "", errConfig(variavel, "o arquivo tem mais de uma linha (o segredo é uma linha só)")
	}
	return s, nil
}

type serveConfig struct {
	base          *url.URL
	addr          string
	databaseURL   string
	sessionSecret []byte
	pepper        []byte
	contas        cloudauth.Contas
	google        *cloudauth.Google
	proxy         cloudauth.ProxyConfiavel
	logLevel      slog.Level
	painel        fs.FS
}

// configError é um erro de configuração: a variável e o que está errado, sem
// o valor.
type configError struct {
	variavel string
	problema string
}

func (e *configError) Error() string { return e.variavel + ": " + e.problema }

func errConfig(variavel, problema string) error { return &configError{variavel, problema} }

// loadConfig lê o ambiente e o JSON do client do Google.
func loadConfig(getenv func(string) string) (serveConfig, error) {
	var c serveConfig

	base, err := server.ParseBaseURL(getenv(envPublicBaseURL))
	if getenv(envPublicBaseURL) == "" {
		return c, errConfig(envPublicBaseURL, "não está definida (https://servidor, ou http://localhost:8080 no PC)")
	}
	if err != nil {
		return c, errConfig(envPublicBaseURL, "tem de ser uma origem https (ou http em localhost e 127.0.0.1), sem caminho")
	}
	if server.LoginDeTesteCompilado && base.Hostname() != "localhost" && base.Hostname() != "127.0.0.1" {
		return c, errConfig(envPublicBaseURL, "este binário tem o login de teste (build tag logindeteste) e só sobe em localhost")
	}
	c.base = base

	c.addr = getenv(envHTTPAddr)
	if c.addr == "" {
		c.addr = defaultHTTPAddr
	}
	host, port, err := net.SplitHostPort(c.addr)
	if err != nil || port == "" {
		return c, errConfig(envHTTPAddr, "tem de ser endereço:porta (por exemplo 127.0.0.1:8080)")
	}
	// A URL pública em localhost não basta: escutando em 0.0.0.0, o POST
	// /auth/teste fica exposto à rede local, e um cliente que não é navegador
	// manda o Host que quiser.
	if server.LoginDeTesteCompilado && !server.HostLoopback(host) {
		return c, errConfig(envHTTPAddr, "este binário tem o login de teste (build tag logindeteste) e só escuta na própria máquina (127.0.0.1, ::1 ou localhost)")
	}

	if c.databaseURL, err = lerDatabaseURL(getenv); err != nil {
		return c, err
	}

	for _, s := range []struct {
		name string
		v    *[]byte
	}{{envSessionSecret, &c.sessionSecret}, {envDeviceTokenPep, &c.pepper}} {
		v, err := lerSegredo(getenv, s.name)
		if err != nil {
			return c, err
		}
		*s.v = []byte(v)
	}
	for _, s := range []struct {
		name string
		v    []byte
	}{{envSessionSecret, c.sessionSecret}, {envDeviceTokenPep, c.pepper}} {
		if len(s.v) == 0 {
			return c, errConfig(s.name, "não está definida, nem "+s.name+sufixoArquivo+
				" (gere com o comando do roteiro: 32 bytes aleatórios)")
		}
		if len(s.v) < 32 {
			return c, errConfig(s.name, "precisa de pelo menos 32 caracteres")
		}
	}
	if string(c.sessionSecret) == string(c.pepper) {
		return c, errConfig(envSessionSecret, "tem de ser diferente de "+envDeviceTokenPep)
	}

	contas, err := cloudauth.ParseContas(getenv(envContasPermitidas))
	if errors.Is(err, cloudauth.ErrContasVazia) {
		return c, errConfig(envContasPermitidas, "está vazia: ponha os e-mails que podem entrar, separados por vírgula")
	}
	if err != nil {
		return c, errConfig(envContasPermitidas, err.Error())
	}
	c.contas = contas

	google, err := lerClientGoogle(getenv(envGoogleSecretFile), server.RedirectURL(base))
	if err != nil {
		return c, err
	}
	c.google = google

	if c.proxy, err = cloudauth.ParseProxyConfiavel(getenv(envProxyConfiavel)); err != nil {
		return c, errConfig(envProxyConfiavel, err.Error())
	}

	if c.painel, err = lerPainelDir(getenv(envPainelDir)); err != nil {
		return c, err
	}

	switch strings.ToLower(getenv(envLogLevel)) {
	case "", "info":
		c.logLevel = slog.LevelInfo
	case "debug":
		c.logLevel = slog.LevelDebug
	case "warn":
		c.logLevel = slog.LevelWarn
	case "error":
		c.logLevel = slog.LevelError
	default:
		return c, errConfig(envLogLevel, "tem de ser debug, info, warn ou error")
	}
	return c, nil
}

// lerPainelDir confere PAINEL_DIR: a pasta do build da nuvem do painel
// (pnpm build:nuvem, web/dist-nuvem), com o index.html. O servidor a lê para
// a memória na subida (server.NewCloud) e nunca serve caminho do pedido a
// partir dela. A mensagem não traz o caminho.
func lerPainelDir(dir string) (fs.FS, error) {
	if dir == "" {
		return nil, errConfig(envPainelDir, "não está definida (a pasta do build da nuvem do painel: rode pnpm build:nuvem em web, e aponte para web/dist-nuvem)")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, errConfig(envPainelDir, "não é uma pasta que dê para ler")
	}
	fsys := os.DirFS(dir)
	if info, err := fs.Stat(fsys, "index.html"); err != nil || !info.Mode().IsRegular() {
		return nil, errConfig(envPainelDir, "a pasta não tem o index.html do painel (rode pnpm build:nuvem em web)")
	}
	return fsys, nil
}

// lerClientGoogle lê o JSON que o Google Cloud dá ao baixar um client do tipo
// "Aplicativo da Web": {"web": {"client_id", "client_secret",
// "redirect_uris", ...}}. O redirect derivado de PUBLIC_BASE_URL tem de estar
// nos redirect_uris. Os endereços do Google (auth_uri, token_uri) do arquivo
// são ignorados: os do código valem (cloudauth.GoogleTokenURL). Nenhum erro
// traz o caminho nem o conteúdo do arquivo.
func lerClientGoogle(path, redirect string) (*cloudauth.Google, error) {
	if path == "" {
		return nil, errConfig(envGoogleSecretFile, "não está definida (o caminho do JSON do client baixado do Google Cloud, fora do repositório)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errConfig(envGoogleSecretFile, "não consegui abrir o arquivo")
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxArquivoGoogle+1))
	if err != nil || len(raw) > maxArquivoGoogle {
		return nil, errConfig(envGoogleSecretFile, "não consegui ler o arquivo (ou ele é grande demais para ser o JSON do client)")
	}
	var doc struct {
		Web *struct {
			ClientID     string   `json:"client_id"`
			ClientSecret string   `json:"client_secret"`
			RedirectURIs []string `json:"redirect_uris"`
		} `json:"web"`
		Installed json.RawMessage `json:"installed"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errConfig(envGoogleSecretFile, "o arquivo não é o JSON do client do Google")
	}
	if doc.Web == nil {
		if doc.Installed != nil {
			return nil, errConfig(envGoogleSecretFile, `o client é do tipo "computador" ("installed"); crie um do tipo "Aplicativo da Web"`)
		}
		return nil, errConfig(envGoogleSecretFile, `o arquivo não tem o client Web ("web")`)
	}
	if doc.Web.ClientID == "" || doc.Web.ClientSecret == "" {
		return nil, errConfig(envGoogleSecretFile, "o arquivo não tem client_id e client_secret")
	}
	found := false
	for _, u := range doc.Web.RedirectURIs {
		if u == redirect {
			found = true
		}
	}
	if !found {
		return nil, errConfig(envGoogleSecretFile, "o redirect derivado de "+envPublicBaseURL+
			" (ela + /auth/google/callback) não está nos redirect_uris do client: acrescente-o no Google Cloud e baixe o JSON de novo")
	}
	return cloudauth.NovoGoogle(doc.Web.ClientID, doc.Web.ClientSecret, redirect), nil
}
