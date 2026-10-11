package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O servir com o job de retenção (F9d; ADR-019, "Decisões da F9"), sobre o
// Store em memória, sem Docker: a linha "retenção" na subida, e o desligamento
// que espera o goroutine da arrumação antes de "desligado" (o job escreve, e
// no serve o pool fecha logo depois).

// retencaoLenta é o Store em memória com um Reter que segura até o ctx acabar
// e só então termina, devagar.
type retencaoLenta struct {
	*cloudstore.MemoryStore
	entrou    chan struct{}
	terminou  atomic.Bool
	terminouA atomic.Int64
}

func (r *retencaoLenta) Reter(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	select {
	case r.entrou <- struct{}{}:
	default:
	}
	<-ctx.Done()
	// Devagar: se o servir não esperasse, "desligado" sairia antes.
	time.Sleep(150 * time.Millisecond)
	r.terminouA.Store(time.Now().UnixNano())
	r.terminou.Store(true)
	return cloudstore.Retido{}, ctx.Err()
}

func nuvemDoServir(t *testing.T, store cloudstore.Store, acc cloudstore.Accounts, logger *slog.Logger) *server.Cloud {
	t.Helper()
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: store, Accounts: acc, BaseURL: contracttest.BaseURL,
		SessionSecret: []byte(testSessionSecret), DeviceTokenPepper: []byte(testPepper), Contas: contas,
		Google: cloudauth.NovoGoogle("cliente", testClientSecret, contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cloud
}

// servirAte sobe o servir num listener do loopback, espera pronto() e cancela,
// e devolve o código de saída.
func servirAte(t *testing.T, cloud *server.Cloud, logger *slog.Logger, pronto func()) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- servir(ctx, cloud, ln, logger, func(code int, format string, a ...any) int {
			t.Errorf("servir: "+format, a...)
			return code
		}, cloud.ArrumarSempre)
	}()
	pronto()
	cancel()
	select {
	case code := <-done:
		return code
	case <-time.After(20 * time.Second):
		t.Fatal("o servir não desligou em 20 s")
	}
	return -1
}

// O desligamento espera o goroutine da arrumação: quando o servir volta, o
// Reter que estava no meio já terminou, e a linha "retenção" dele sai antes
// de "desligado".
func TestServir_EsperaAArrumacaoAntesDeDesligado(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	mem.AddUser(contracttest.NewID())
	lenta := &retencaoLenta{MemoryStore: mem, entrou: make(chan struct{}, 1)}
	var logs safeBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cloud := nuvemDoServir(t, lenta, mem, logger)
	code := servirAte(t, cloud, logger, func() {
		select {
		case <-lenta.entrou:
		case <-time.After(20 * time.Second):
			t.Fatal("o job não rodou na subida")
		}
	})
	voltou := time.Now().UnixNano()
	if code != 0 {
		t.Fatalf("o servir saiu com %d: %s", code, logs.String())
	}
	if !lenta.terminou.Load() || lenta.terminouA.Load() > voltou {
		t.Fatal("o servir voltou antes de o Reter terminar")
	}
	out := logs.String()
	ret, desl := strings.Index(out, `"msg":"retenção"`), strings.Index(out, `"msg":"desligado"`)
	if ret < 0 || desl < 0 || ret > desl {
		t.Fatalf("a linha \"retenção\" (%d) tem de vir antes de \"desligado\" (%d):\n%s", ret, desl, out)
	}
	if !strings.Contains(out, `"categoria":"contexto"`) {
		t.Errorf("o Reter interrompido não saiu como contexto:\n%s", out)
	}
}

// Na subida, a linha "retenção" sai só com as chaves permitidas, mesmo com
// zero, sem id nem texto.
func TestServir_ALinhaDaRetencaoNaSubida(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	user := contracttest.NewID()
	mem.AddUser(user)
	var logs safeBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cloud := nuvemDoServir(t, mem, mem, logger)
	code := servirAte(t, cloud, logger, func() {
		deadline := time.Now().Add(20 * time.Second)
		for !strings.Contains(logs.String(), `"msg":"retenção"`) {
			if time.Now().After(deadline) {
				t.Fatal("a linha \"retenção\" não saiu na subida")
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	if code != 0 {
		t.Fatalf("o servir saiu com %d", code)
	}
	var linha map[string]any
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, `"msg":"retenção"`) {
			if err := json.Unmarshal([]byte(l), &linha); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	var ks []string
	for k := range linha {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	want := "ativos,eventos,falhas,lapides_criadas,lapides_vencidas,level,lixeira,ms,msg,mutacoes,time,usuarios"
	if strings.Join(ks, ",") != want {
		t.Fatalf("as chaves = %v, quero %s", ks, want)
	}
	if linha["usuarios"] != float64(1) || linha["ativos"] != float64(0) || linha["falhas"] != float64(0) {
		t.Errorf("a linha = %v, quero 1 usuário e zero no resto", linha)
	}
	if strings.Contains(logs.String(), user) {
		t.Error("o log traz o id do usuário")
	}
}
