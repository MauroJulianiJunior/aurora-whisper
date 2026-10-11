package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// A primeira rodada de Arrumar e Reter ANTES do net.Listen (F11c; ADR-019,
// "Decisões da F11": a ordem "restaurar, reter, abrir" sem depender do
// proxy). Sobre o Store em memória, sem Docker: o abrir é o fim do serve
// depois do Postgres (serve.go).

// retencaoPresa é o Store em memória com um Reter que avisa que entrou e
// espera o teste soltar (ou o ctx acabar); depois falha com err, se houver.
type retencaoPresa struct {
	*cloudstore.MemoryStore
	entrou chan struct{}
	soltar chan struct{}
	err    error
	rodou  atomic.Int32
}

func (r *retencaoPresa) Reter(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	r.rodou.Add(1)
	select {
	case r.entrou <- struct{}{}:
	default:
	}
	select {
	case <-r.soltar:
	case <-ctx.Done():
		return cloudstore.Retido{}, ctx.Err()
	}
	if r.err != nil {
		return cloudstore.Retido{}, r.err
	}
	return r.MemoryStore.Reter(ctx, userID, now)
}

func novaRetencaoPresa() *retencaoPresa {
	mem := cloudstore.NewMemoryStore()
	mem.AddUser(contracttest.NewID())
	return &retencaoPresa{MemoryStore: mem, entrou: make(chan struct{}, 1), soltar: make(chan struct{})}
}

// portaLivre devolve um endereço do loopback com uma porta que estava livre.
func portaLivre(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// subindo é um abrir em andamento.
type subindo struct {
	addr     string
	logs     *safeBuffer
	escutou  chan net.Addr
	fim      chan int
	cancelar context.CancelFunc
	erros    *safeBuffer
}

func subirAbrir(t *testing.T, store *retencaoPresa) *subindo {
	t.Helper()
	s := &subindo{addr: portaLivre(t), logs: &safeBuffer{}, erros: &safeBuffer{}, escutou: make(chan net.Addr, 1), fim: make(chan int, 1)}
	onListen = func(a net.Addr) { s.escutou <- a }
	t.Cleanup(func() { onListen = nil })
	logger := slog.New(slog.NewJSONHandler(s.logs, nil))
	cloud := nuvemDoServir(t, store, store.MemoryStore, logger)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelar = cancel
	t.Cleanup(cancel)
	go func() {
		s.fim <- abrir(ctx, s.addr, cloud, logger, func(code int, format string, a ...any) int {
			fmt.Fprintf(s.erros, format, a...)
			return code
		}, "proxy_confiavel", false)
	}()
	return s
}

func (s *subindo) esperarEntrar(t *testing.T, store *retencaoPresa) {
	t.Helper()
	select {
	case <-store.entrou:
	case code := <-s.fim:
		t.Fatalf("o abrir saiu com %d antes da primeira rodada: %s %s", code, s.erros.String(), s.logs.String())
	case <-time.After(20 * time.Second):
		t.Fatal("a primeira rodada não começou em 20 s")
	}
}

// portaFechada confere que ninguém atende no endereço.
func portaFechada(t *testing.T, addr, quando string) {
	t.Helper()
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatalf("%s: a porta já aceita conexão", quando)
	}
}

// Enquanto a primeira rodada está presa, a porta não aceita conexão, o
// onListen não foi chamado e a linha "servindo" não saiu. Solta a rodada: a
// linha "retenção" sai, só então a porta abre e a "servindo" sai. O tique de
// hora em hora não repete a rodada logo depois.
func TestAbrir_APrimeiraRodadaVemAntesDaPorta(t *testing.T) {
	store := novaRetencaoPresa()
	s := subirAbrir(t, store)
	s.esperarEntrar(t, store)

	for range 3 {
		portaFechada(t, s.addr, "com a rodada presa")
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case a := <-s.escutou:
		t.Fatalf("escutou em %s com a rodada presa", a)
	default:
	}
	if strings.Contains(s.logs.String(), `"msg":"servindo"`) || strings.Contains(s.logs.String(), `"msg":"retenção"`) {
		t.Fatalf("com a rodada presa, o log já tem a linha da rodada ou a \"servindo\":\n%s", s.logs.String())
	}
	if !strings.Contains(s.logs.String(), `"msg":"arrumação"`) {
		t.Errorf("a arrumação não veio antes da retenção:\n%s", s.logs.String())
	}

	close(store.soltar)
	var addr net.Addr
	select {
	case addr = <-s.escutou:
	case code := <-s.fim:
		t.Fatalf("o abrir saiu com %d: %s", code, s.erros.String())
	case <-time.After(20 * time.Second):
		t.Fatal("a porta não abriu em 20 s depois de soltar a rodada")
	}
	if addr.String() != s.addr {
		t.Fatalf("escutou em %s, quero %s", addr, s.addr)
	}
	resp, err := http.Get("http://" + s.addr + "/api/v1/health/live")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health/live: %d", resp.StatusCode)
	}
	logs := s.logs.String()
	ret, serv := strings.Index(logs, `"msg":"retenção"`), strings.Index(logs, `"msg":"servindo"`)
	arr := strings.Index(logs, `"msg":"arrumação"`)
	if arr < 0 || ret < 0 || serv < 0 || arr > ret || ret > serv {
		t.Fatalf("a ordem tem de ser arrumação (%d), retenção (%d), servindo (%d):\n%s", arr, ret, serv, logs)
	}
	if !strings.Contains(logs, `"falhas":0`) {
		t.Errorf("a primeira rodada teve falha:\n%s", logs)
	}
	var linha map[string]any
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, `"msg":"servindo"`) {
			if err := json.Unmarshal([]byte(l), &linha); err != nil {
				t.Fatal(err)
			}
		}
	}
	if linha["proxy_confiavel"] != false || linha["endereco"] != s.addr {
		t.Errorf("a linha servindo = %v", linha)
	}
	time.Sleep(100 * time.Millisecond)
	if n := store.rodou.Load(); n != 1 {
		t.Errorf("o Reter rodou %d vezes; quero 1 (a primeira rodada não se repete no tique)", n)
	}
	s.cancelar()
	select {
	case code := <-s.fim:
		if code != 0 {
			t.Fatalf("o desligamento saiu com %d: %s", code, s.erros.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("não desligou em 20 s")
	}
}

// A primeira rodada com falha grave (banco ou outra): o serve sai com 1, sem
// abrir a porta (escolha menor 4 da F11).
func TestAbrir_FalhaGraveNaPrimeiraRodadaNaoAbre(t *testing.T) {
	for nome, err := range map[string]error{
		"outra categoria": errors.New("pgstore: dial tcp 10.0.0.9:5432: recusada"),
		"banco":           erroDoBancoDeTeste{},
	} {
		t.Run(nome, func(t *testing.T) {
			store := novaRetencaoPresa()
			store.err = err
			close(store.soltar)
			s := subirAbrir(t, store)
			select {
			case code := <-s.fim:
				if code != 1 {
					t.Fatalf("saiu com %d, quero 1", code)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("o abrir não voltou em 20 s")
			}
			select {
			case a := <-s.escutou:
				t.Fatalf("escutou em %s com a primeira rodada falhando", a)
			default:
			}
			portaFechada(t, s.addr, "depois da falha")
			if !strings.Contains(s.erros.String(), "primeira rodada da retenção") || strings.Contains(s.erros.String(), "10.0.0.9") {
				t.Errorf("a mensagem = %q", s.erros.String())
			}
			if strings.Contains(s.logs.String(), `"msg":"servindo"`) || strings.Contains(s.logs.String(), "10.0.0.9") {
				t.Errorf("o log:\n%s", s.logs.String())
			}
		})
	}
}

// erroDoBancoDeTeste imita o pgstore.DBError (a operação e o SQLSTATE).
type erroDoBancoDeTeste struct{}

func (erroDoBancoDeTeste) Error() string    { return "ERROR: deadlock detected" }
func (erroDoBancoDeTeste) Operacao() string { return "apagar o ditado" }
func (erroDoBancoDeTeste) SQLState() string { return "40P01" }

// O SIGTERM no meio da primeira rodada: sai com 0, sem abrir a porta.
func TestAbrir_DesligarNaPrimeiraRodadaNaoAbre(t *testing.T) {
	store := novaRetencaoPresa()
	s := subirAbrir(t, store)
	s.esperarEntrar(t, store)
	s.cancelar()
	select {
	case code := <-s.fim:
		if code != 0 {
			t.Fatalf("saiu com %d, quero 0 (%s)", code, s.erros.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("o abrir não voltou em 20 s")
	}
	select {
	case a := <-s.escutou:
		t.Fatalf("escutou em %s", a)
	default:
	}
	portaFechada(t, s.addr, "depois do desligamento")
	if !strings.Contains(s.logs.String(), `"msg":"desligado"`) || strings.Contains(s.logs.String(), `"msg":"servindo"`) {
		t.Errorf("o log:\n%s", s.logs.String())
	}
}
