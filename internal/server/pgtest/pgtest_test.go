package pgtest

import (
	"context"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDatabaseURL_TrocaSoOBanco(t *testing.T) {
	got, err := databaseURL("postgres://postgres:s3nh4@127.0.0.1:54329/postgres?sslmode=disable", "aw_test_abc")
	if err != nil {
		t.Fatalf("databaseURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("a saída não é uma URL: %v", err)
	}
	if u.Path != "/aw_test_abc" {
		t.Errorf("Path = %q, quero /aw_test_abc", u.Path)
	}
	if u.Host != "127.0.0.1:54329" {
		t.Errorf("Host = %q, quero 127.0.0.1:54329", u.Host)
	}
	if pw, _ := u.User.Password(); u.User.Username() != "postgres" || pw != "s3nh4" {
		t.Errorf("usuário ou senha mudaram")
	}
	if u.Query().Get("sslmode") != "disable" {
		t.Errorf("os parâmetros se perderam: %q", u.RawQuery)
	}
}

func TestDatabaseURL_Recusa(t *testing.T) {
	for _, bad := range []string{
		"host=127.0.0.1 user=postgres", // key=value não é URL
		"http://127.0.0.1/postgres",
		"://sem-esquema",
	} {
		_, err := databaseURL(bad, "aw_test_abc")
		if err == nil {
			t.Errorf("databaseURL(%q) deveria falhar", bad)
			continue
		}
		if strings.Contains(err.Error(), bad) {
			t.Errorf("o erro cita a URL (que traz a senha): %v", err)
		}
	}
}

func TestRequireLocalHosts(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "127.1.2.3", "::1", "/var/run/postgresql"} {
		if err := requireLocalHosts([]string{host}); err != nil {
			t.Errorf("host %q é local e foi recusado: %v", host, err)
		}
	}
	for _, host := range []string{"", "db.exemplo.com", "10.0.0.5", "192.168.0.10", "8.8.8.8", "localhost.exemplo.com"} {
		if err := requireLocalHosts([]string{host}); err == nil {
			t.Errorf("host %q não é local e foi aceito", host)
		}
	}
	// Um servidor de fallback fora da máquina também derruba.
	if err := requireLocalHosts([]string{"127.0.0.1", "db.exemplo.com"}); err == nil {
		t.Error("um host remoto no meio da lista foi aceito")
	}
}

func TestNewDatabaseName_UnicoELegivel(t *testing.T) {
	shape := regexp.MustCompile(`^aw_test_[0-9a-f]{16}$`)
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		name, err := newDatabaseName()
		if err != nil {
			t.Fatalf("newDatabaseName: %v", err)
		}
		if !shape.MatchString(name) {
			t.Fatalf("nome %q fora do formato aw_test_<16 hex>", name)
		}
		if seen[name] {
			t.Fatalf("nome repetido: %q", name)
		}
		seen[name] = true
	}
}

// TestNewDB_CriaEApagaOBanco é o teste com Postgres do próprio pacote: o
// banco existe durante o teste e desaparece quando ele termina.
func TestNewDB_CriaEApagaOBanco(t *testing.T) {
	var dbName string
	t.Run("durante", func(t *testing.T) {
		dbURL := NewDB(t)
		u, err := url.Parse(dbURL)
		if err != nil {
			t.Fatalf("NewDB devolveu uma URL inválida: %v", err)
		}
		dbName = strings.TrimPrefix(u.Path, "/")

		conn := Connect(t, dbURL)
		var current string
		if err := conn.QueryRow(context.Background(), "SELECT current_database()").Scan(&current); err != nil {
			t.Fatalf("SELECT current_database(): %v", err)
		}
		if current != dbName {
			t.Fatalf("conectou em %q, quero %q", current, dbName)
		}
	})
	if dbName == "" {
		t.Skip(SkipMessage) // o subteste foi pulado (sem AURORA_TEST_POSTGRES_URL)
	}

	// O subteste terminou, e o Cleanup dele já apagou o banco.
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	cfg, err := pgx.ParseConfig(os.Getenv(EnvURL))
	if err != nil {
		t.Fatal("URL de teste inválida")
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("conectar como administrador: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists); err != nil {
		t.Fatalf("consultar pg_database: %v", err)
	}
	if exists {
		t.Errorf("o banco %s continua existindo depois do teste", dbName)
	}
}
