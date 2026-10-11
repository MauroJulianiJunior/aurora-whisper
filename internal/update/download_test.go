package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("diretório deveria estar vazio após rejeição, contém: %v", names)
	}
}

func TestDownloader_DownloadAndVerify_Success(t *testing.T) {
	body := []byte("conteudo-fake-do-instalador")
	hash := sha256Hex(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "0.10.0", DownloadURL: srv.URL, SHA256: strings.ToUpper(hash)}

	got, err := d.DownloadAndVerify(context.Background(), info, dir)
	if err != nil {
		t.Fatalf("DownloadAndVerify: %v", err)
	}
	if got.SHA256 != hash {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, hash)
	}
	if got.Version != "0.10.0" {
		t.Errorf("Version = %q, want 0.10.0", got.Version)
	}
	data, err := os.ReadFile(got.FilePath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", got.FilePath, err)
	}
	if string(data) != string(body) {
		t.Error("conteúdo do arquivo baixado não confere com o servido")
	}
}

func TestDownloader_DownloadAndVerify_MixedCaseHash(t *testing.T) {
	body := []byte("outro-conteudo")
	hash := sha256Hex(body)
	mixed := strings.ToUpper(hash[:32]) + hash[32:]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "1.0.0", DownloadURL: srv.URL, SHA256: mixed}
	if _, err := d.DownloadAndVerify(context.Background(), info, dir); err != nil {
		t.Fatalf("DownloadAndVerify com hash de caixa mista: %v", err)
	}
}

func TestDownloader_DownloadAndVerify_HashMismatch(t *testing.T) {
	body := []byte("conteudo-real-do-arquivo")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "0.10.0", DownloadURL: srv.URL, SHA256: strings.Repeat("a", 64)}

	_, err := d.DownloadAndVerify(context.Background(), info, dir)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("erro = %v, want ErrHashMismatch", err)
	}
	assertDirEmpty(t, dir)
}

func TestDownloader_DownloadAndVerify_EmptyHash(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "0.10.0", DownloadURL: srv.URL, SHA256: ""}

	_, err := d.DownloadAndVerify(context.Background(), info, dir)
	if !errors.Is(err, ErrEmptyHash) {
		t.Fatalf("erro = %v, want ErrEmptyHash", err)
	}
	if called {
		t.Error("não deveria ter contactado o servidor com hash vazio")
	}
	assertDirEmpty(t, dir)
}

func TestDownloader_DownloadAndVerify_InvalidHashFormat(t *testing.T) {
	dir := t.TempDir()
	d := Downloader{}
	cases := []string{
		"nao-e-hexadecimal-mas-tem-64-caracteres-de-comprimento-aaaaaaaa",
		strings.Repeat("a", 63), // curto demais
		strings.Repeat("a", 65), // longo demais
		strings.Repeat("z", 64), // não-hex
	}
	for _, h := range cases {
		info := UpdateInfo{Version: "0.10.0", DownloadURL: "http://example.invalid/x", SHA256: h}
		_, err := d.DownloadAndVerify(context.Background(), info, dir)
		if !errors.Is(err, ErrInvalidHashFormat) {
			t.Errorf("hash %q: erro = %v, want ErrInvalidHashFormat", h, err)
		}
	}
	assertDirEmpty(t, dir)
}

func TestDownloader_DownloadAndVerify_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "0.10.0", DownloadURL: srv.URL, SHA256: strings.Repeat("a", 64)}
	if _, err := d.DownloadAndVerify(context.Background(), info, dir); err == nil {
		t.Fatal("esperava erro para status não-200")
	}
	assertDirEmpty(t, dir)
}

func TestDownloader_DownloadAndVerify_UnsupportedScheme(t *testing.T) {
	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "0.10.0", DownloadURL: "ftp://example.com/pacote.exe", SHA256: strings.Repeat("a", 64)}
	if _, err := d.DownloadAndVerify(context.Background(), info, dir); err == nil {
		t.Fatal("esperava erro para esquema de URL não suportado")
	}
	assertDirEmpty(t, dir)
}

func TestDownloader_DownloadAndVerify_WeirdURL(t *testing.T) {
	dir := t.TempDir()
	d := Downloader{}
	info := UpdateInfo{Version: "0.10.0", DownloadURL: "://nao-e-uma-url", SHA256: strings.Repeat("a", 64)}
	if _, err := d.DownloadAndVerify(context.Background(), info, dir); err == nil {
		t.Fatal("esperava erro para download_url estranha")
	}
	assertDirEmpty(t, dir)
}

func TestCheckDownloadSize(t *testing.T) {
	if err := checkDownloadSize(-1, 999); err != nil {
		t.Errorf("Content-Length desconhecido (-1) não deveria falhar: %v", err)
	}
	if err := checkDownloadSize(10, 10); err != nil {
		t.Errorf("tamanhos iguais não deveriam falhar: %v", err)
	}
	err := checkDownloadSize(10, 5)
	if !errors.Is(err, ErrSizeMismatch) {
		t.Errorf("erro = %v, want ErrSizeMismatch para tamanho divergente", err)
	}
}
