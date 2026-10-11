// Package pgtest dá aos testes um banco PostgreSQL descartável, e só quando
// quem roda os testes pediu um.
//
// A regra (ADR-019, decisão 15; CLAUDE.md): o "go test ./..." normal NUNCA
// sobe contêiner nem toca no Docker. Os testes que precisam de um Postgres
// chamam NewDB, que:
//
//   - sem a variável AURORA_TEST_POSTGRES_URL, PULA o teste (t.Skip) com uma
//     mensagem que diz como ligar;
//   - com ela, conecta ao Postgres que a pessoa subiu
//     (scripts/postgres-de-teste.ps1), cria um banco com nome único
//     (CREATE DATABASE aw_test_<aleatório>) e o apaga no fim do teste
//     (t.Cleanup), para nenhuma execução sujar a seguinte.
//
// Só aceita um servidor na própria máquina (localhost, 127.0.0.1, ::1 ou
// socket Unix): criar bancos de teste num servidor de verdade é o tipo de
// engano que este pacote existe para impedir.
//
// Este pacote é só para testes: nenhum binário do projeto o importa.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnvURL é a variável com a URL de um Postgres de teste, de um usuário que
// pode criar bancos (o do script, "postgres", pode). A URL aponta para um
// banco qualquer do servidor, em geral "postgres"; NewDB cria os seus.
const EnvURL = "AURORA_TEST_POSTGRES_URL"

// SkipMessage é o texto do t.Skip quando a variável não está definida.
const SkipMessage = EnvURL + " não está definida: teste com Postgres pulado. " +
	"Para ligar: abra o Docker Desktop, rode scripts/postgres-de-teste.ps1 no PowerShell e cole a linha " +
	"que ele imprime ($env:" + EnvURL + " = ...); veja scripts/README.md."

const opTimeout = 30 * time.Second

// Require pula o teste quando AURORA_TEST_POSTGRES_URL não está definida. NewDB
// já faz isso; Require serve a um teste que só cria bancos nos subtestes, para
// o teste de cima aparecer como pulado, e não como aprovado sem ter provado
// nada.
func Require(t testing.TB) {
	t.Helper()
	if os.Getenv(EnvURL) == "" {
		t.Skip(SkipMessage)
	}
}

// NewDB cria um banco descartável e devolve a URL de conexão a ele. O banco
// nasce vazio (sem as migrations) e é apagado quando o teste termina.
func NewDB(t testing.TB) string {
	t.Helper()
	Require(t)
	adminURL := os.Getenv(EnvURL)

	cfg, err := pgx.ParseConfig(adminURL)
	if err != nil {
		// Não imprime o erro nem a URL: ela traz a senha.
		t.Fatalf("%s não é uma URL de conexão válida (esperado postgres://usuario:senha@127.0.0.1:porta/postgres)", EnvURL)
	}
	hosts := []string{cfg.Host}
	for _, fb := range cfg.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	if err := requireLocalHosts(hosts); err != nil {
		t.Fatalf("%s: %v", EnvURL, err)
	}

	name, err := newDatabaseName()
	if err != nil {
		t.Fatalf("pgtest: gerar o nome do banco: %v", err)
	}
	dbURL, err := databaseURL(adminURL, name)
	if err != nil {
		t.Fatalf("%s: %v", EnvURL, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("não consegui conectar ao Postgres de teste (%s). O contêiner está de pé? "+
			"Rode scripts/postgres-de-teste.ps1. Erro: %v", EnvURL, err)
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("pgtest: CREATE DATABASE %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(t, cfg, name) })
	return dbURL
}

// Connect abre uma conexão ao banco de dbURL (a saída de NewDB) e a fecha
// quando o teste termina.
func Connect(t testing.TB, dbURL string) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgtest: conectar ao banco de teste: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), opTimeout)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	})
	return conn
}

// dropDatabase apaga o banco do teste. WITH (FORCE) derruba as conexões que
// o teste tenha deixado abertas.
func dropDatabase(t testing.TB, cfg *pgx.ConnConfig, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Errorf("pgtest: não consegui conectar para apagar o banco %s (apague à mão): %v", name, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
		t.Errorf("pgtest: não consegui apagar o banco %s (apague à mão): %v", name, err)
	}
}

// newDatabaseName devolve "aw_test_" mais 16 dígitos hexadecimais aleatórios:
// único entre execuções e entre pacotes de teste que rodam ao mesmo tempo, em
// minúsculas e bem abaixo do limite de 63 bytes do Postgres.
func newDatabaseName() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "aw_test_" + hex.EncodeToString(b[:]), nil
}

// databaseURL troca o banco de uma URL de conexão, mantendo usuário, senha,
// servidor e parâmetros. O erro nunca cita a URL (ela traz a senha).
func databaseURL(adminURL, dbName string) (string, error) {
	u, err := url.Parse(adminURL)
	if err != nil {
		return "", errors.New("não é uma URL válida")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", errors.New("só URLs postgres://... são aceitas")
	}
	u.Path = "/" + dbName
	u.RawPath = ""
	return u.String(), nil
}

// requireLocalHosts recusa qualquer servidor que não seja a própria máquina.
func requireLocalHosts(hosts []string) error {
	for _, host := range hosts {
		if !isLocalHost(host) {
			return fmt.Errorf("o servidor %q não é local: os testes só criam bancos num Postgres da própria máquina (localhost, 127.0.0.1, ::1 ou socket Unix)", host)
		}
	}
	return nil
}

func isLocalHost(host string) bool {
	if strings.HasPrefix(host, "/") { // socket Unix
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
