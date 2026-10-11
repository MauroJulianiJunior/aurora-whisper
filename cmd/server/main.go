// Command server é o ponto de entrada do backend cloud do Aurora Whisper
// (ADR-019): o servidor da nuvem e o comando das migrations.
//
//	aurora-whisper-server            imprime nome e versão
//	aurora-whisper-server serve      sobe o servidor da nuvem (ver serve.go e config.go)
//	aurora-whisper-server migrate    aplica as migrations pendentes (ver migrate.go)
//	aurora-whisper-server healthcheck  a checagem do contêiner (ver healthcheck.go)
//	aurora-whisper-server pos-restauracao -confirmo
//	                                 depois de restaurar um backup (ver posrestauracao.go)
//	aurora-whisper-server teste      só com a build tag logindeteste: a nuvem de
//	                                 teste no PC, na memória (ver teste.go)
//
// As migrations rodam por um comando SEPARADO da subida do servidor
// (05-data-model.md, "Integridade"; ADR-019, decisão 13): quem sobe o servidor
// nunca altera o esquema do banco, e o serve recusa subir com migration
// pendente.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// appName identifica o binário nas mensagens de diagnóstico.
const appName = "aurora-whisper-server"

// version é a versão do backend cloud. Em sprints futuros deverá ser
// injetada via ldflags no processo de build/release; por ora é fixa.
const version = "0.0.0-sprint0"

func main() {
	// O Ctrl+C e o SIGTERM (o docker stop) param o serve com o desligamento
	// limpo.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run é o main sem os.Exit nem os globais, para os testes chamarem. Devolve o
// código de saída: 0 deu certo, 1 falhou, 2 foi usado errado ou faltou
// configuração.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stdout, "%s %s\n", appName, version)
		return 0
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], getenv, stdout, stderr)
	case "migrate":
		return runMigrate(args[1:], getenv, stdout, stderr)
	case "healthcheck":
		return runHealthcheck(args[1:], getenv, stdout, stderr)
	case "pos-restauracao":
		return runPosRestauracao(args[1:], getenv, stdout, stderr)
	case "teste":
		// Sem a build tag logindeteste, runTeste responde como a um comando
		// desconhecido (teste_off.go).
		return runTeste(ctx, args[1:], getenv, stdout, stderr)
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "%s: comando desconhecido %q\n\n", appName, args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `uso: %s [comando]

comandos:
  serve             sobe o servidor da nuvem (configuração pelo ambiente:
                    PUBLIC_BASE_URL, HTTP_ADDR, DATABASE_URL, SESSION_SECRET,
                    DEVICE_TOKEN_PEPPER, CONTAS_PERMITIDAS,
                    GOOGLE_CLIENT_SECRET_FILE, PAINEL_DIR, PROXY_CONFIAVEL,
                    LOG_LEVEL); DATABASE_URL, SESSION_SECRET e
                    DEVICE_TOKEN_PEPPER também vêm de arquivo, pela variável
                    com _FILE no fim (nunca as duas juntas)
  migrate           aplica as migrations pendentes no Postgres de DATABASE_URL
  migrate -status   mostra o que já está aplicado e o que falta, sem aplicar nada
  healthcheck       a checagem do contêiner: GET no health/live da porta de
                    HTTP_ADDR, no loopback; sai com 0 (de pé) ou 1
  healthcheck -retencao
                    sai com 1 se a última rodada da retenção tem mais de 2 h
                    ou teve falha de banco
  pos-restauracao -confirmo
                    depois de restaurar um backup, com a API parada: troca o
                    epoch de todas as contas e revoga todas as sessões e todos
                    os aparelhos (sem -confirmo, não faz nada)
%s  help              mostra este texto

Sem comando, imprime o nome e a versão.
`, appName, usoDoTeste)
}
