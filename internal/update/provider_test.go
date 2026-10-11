package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPUpdateProvider_Latest(t *testing.T) {
	var gotCurrent, gotArch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCurrent = r.URL.Query().Get("current")
		gotArch = r.URL.Query().Get("arch")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"update_available":  true,
			"version":           "0.10.0",
			"release_notes_url": "https://example.com/notes",
			"download_url":      "https://example.com/aurora-0.10.0.exe",
			"sha256":            "deadbeef",
		})
	}))
	defer srv.Close()

	p := HTTPUpdateProvider{BaseURL: srv.URL}
	info, err := p.Latest(context.Background(), "0.9.0", "x64")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if gotCurrent != "0.9.0" || gotArch != "x64" {
		t.Errorf("query = current=%q arch=%q, want current=0.9.0 arch=x64", gotCurrent, gotArch)
	}
	if !info.Available || info.Version != "0.10.0" || info.SHA256 != "deadbeef" {
		t.Fatalf("info inesperado: %+v", info)
	}
}

func TestHTTPUpdateProvider_Latest_NoUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"update_available": false})
	}))
	defer srv.Close()

	p := HTTPUpdateProvider{BaseURL: srv.URL}
	info, err := p.Latest(context.Background(), "0.9.0", "x64")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if info.Available {
		t.Fatal("esperava update_available=false")
	}
}

func TestHTTPUpdateProvider_Latest_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{isso nao e json"))
	}))
	defer srv.Close()

	p := HTTPUpdateProvider{BaseURL: srv.URL}
	if _, err := p.Latest(context.Background(), "0.9.0", "x64"); err == nil {
		t.Fatal("esperava erro para JSON inválido")
	}
}

func TestHTTPUpdateProvider_Latest_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := HTTPUpdateProvider{BaseURL: srv.URL}
	if _, err := p.Latest(context.Background(), "0.9.0", "x64"); err == nil {
		t.Fatal("esperava erro para status não-200")
	}
}

func TestHTTPUpdateProvider_Latest_EmptyBaseURL(t *testing.T) {
	p := HTTPUpdateProvider{}
	if _, err := p.Latest(context.Background(), "0.9.0", "x64"); err == nil {
		t.Fatal("esperava erro para BaseURL vazio")
	}
}

func TestHTTPUpdateProvider_Latest_WeirdURL(t *testing.T) {
	p := HTTPUpdateProvider{BaseURL: "://nao-e-uma-url"}
	if _, err := p.Latest(context.Background(), "0.9.0", "x64"); err == nil {
		t.Fatal("esperava erro para BaseURL estranha")
	}
}
