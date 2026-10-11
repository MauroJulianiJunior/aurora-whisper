package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O job de retenção no servidor (F9d; ADR-019, "Decisões da F9"): a linha
// "retenção" só com contagens, mesmo com zero; o erro só com a categoria; e o
// ctx cancelado que para entre um usuário e outro. Sem banco: o Store em
// memória, às vezes embrulhado para falhar.

// As chaves da linha "retenção", e só elas (além das do slog).
var chavesDaRetencao = []string{
	"ativos", "eventos", "falhas", "lapides_criadas", "lapides_vencidas", "level", "lixeira", "ms", "msg",
	"mutacoes", "time", "usuarios",
}

func novaNuvemDoJob(t *testing.T, store cloudstore.Store, acc cloudstore.Accounts, logs io.Writer, now func() time.Time) *server.Cloud {
	t.Helper()
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: store, Accounts: acc, BaseURL: contracttest.BaseURL,
		SessionSecret: contracttest.SegredoDeSessao, DeviceTokenPepper: contracttest.PepperDeToken, Contas: contas,
		Google: cloudauth.NovoGoogle("cliente", "secret-do-client", contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Now: now,
		Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return cloud
}

// linhas lê o log JSON e devolve as linhas com a mensagem msg.
func linhas(t *testing.T, logs, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(logs), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("o log não é JSON: %q", l)
		}
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func chaves(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// A linha "retenção": uma por rodada, só com as contagens, mesmo com zero; com
// o que a rodada apagou, e sem id, texto nem e-mail.
func TestReter_ALinhaSoComContagens(t *testing.T) {
	ctx := context.Background()
	mem := cloudstore.NewMemoryStore()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	relogio := now
	var logs contracttest.LockedBuffer
	cloud := novaNuvemDoJob(t, mem, mem, &logs, func() time.Time { return relogio })

	// Sem usuário nenhum: a linha sai, com zero.
	cloud.Reter(ctx)
	got := linhas(t, logs.String(), "retenção")
	if len(got) != 1 {
		t.Fatalf("%d linhas \"retenção\", quero 1: %s", len(got), logs.String())
	}
	if strings.Join(chaves(got[0]), ",") != strings.Join(chavesDaRetencao, ",") {
		t.Fatalf("as chaves da linha = %v, quero %v", chaves(got[0]), chavesDaRetencao)
	}
	for _, k := range []string{"usuarios", "ativos", "lixeira", "lapides_criadas", "lapides_vencidas", "eventos", "mutacoes", "falhas"} {
		if got[0][k] != float64(0) {
			t.Errorf("%s = %v, quero 0", k, got[0][k])
		}
	}
	if got[0]["level"] != "INFO" {
		t.Errorf("o nível = %v, quero INFO", got[0]["level"])
	}

	// Com um ativo e uma lixeira vencidos.
	const segredo = "TEXTO-DITADO-SIGILOSO-9120"
	user := contracttest.NewID()
	mem.AddUser(user)
	device := contracttest.NewID()
	mem.AddDevice(user, device)
	pc := cloudstore.Actor{UserID: user, DeviceID: device}
	ativo, lixo := contracttest.NewID(), contracttest.NewID()
	for _, id := range []string{ativo, lixo} {
		raw, _ := json.Marshal(map[string]any{
			"source_platform": "windows", "raw_text": segredo, "final_text": segredo, "mode": "smart",
			"asr_provider": "whisper-local", "status": "done", "created_at": now.Add(-time.Hour).Format(time.RFC3339),
		})
		if _, err := mem.Push(ctx, pc, []cloudstore.Mutation{{MutationID: contracttest.NewID(), EntityType: "transcription",
			EntityID: id, Operation: "upsert", Payload: raw}}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mem.Trash(ctx, cloudstore.Actor{UserID: user}, lixo, now); err != nil {
		t.Fatal(err)
	}
	relogio = now.AddDate(0, 6, 0)
	cloud.Reter(ctx)
	got = linhas(t, logs.String(), "retenção")
	if len(got) != 2 {
		t.Fatalf("%d linhas, quero 2", len(got))
	}
	want := map[string]float64{"usuarios": 1, "ativos": 1, "lixeira": 1, "lapides_criadas": 1, "eventos": 3, "falhas": 0, "mutacoes": 0}
	for k, v := range want {
		if got[1][k] != v {
			t.Errorf("%s = %v, quero %v", k, got[1][k], v)
		}
	}
	for _, proibido := range []string{segredo, user, device, ativo, lixo, "mauro@exemplo.test"} {
		if strings.Contains(logs.String(), proibido) {
			t.Errorf("o log da retenção traz %q", proibido)
		}
	}
}

// O tique (a revisão da F9): depois da rodada da subida, o ArrumarSempre roda
// a arrumação e a retenção a cada intervalo, e a rodada do tique apaga o que
// venceu entre as duas. Sem ele, o job rodaria só na subida: o vencido
// ficaria visível e restaurável até o próximo restart, e o batimento do alerta
// "retention job missed" (F11) não existiria. O intervalo é o da CloudConfig
// (aqui 20 ms; o padrão é IntervaloDaArrumacao, 1 h).
func TestArrumarSempre_RodaARetencaoACadaTique(t *testing.T) {
	ctx := context.Background()
	mem := cloudstore.NewMemoryStore()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	relogio := now
	agora := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return relogio
	}
	user, device, lixo := contracttest.NewID(), contracttest.NewID(), contracttest.NewID()
	mem.AddUser(user)
	mem.AddDevice(user, device)
	raw, _ := json.Marshal(map[string]any{
		"source_platform": "windows", "raw_text": "vai vencer", "final_text": "vai vencer", "mode": "smart",
		"asr_provider": "whisper-local", "status": "done", "created_at": now.Add(-time.Hour).Format(time.RFC3339),
	})
	if _, err := mem.Push(ctx, cloudstore.Actor{UserID: user, DeviceID: device}, []cloudstore.Mutation{{
		MutationID: contracttest.NewID(), EntityType: "transcription", EntityID: lixo, Operation: "upsert", Payload: raw,
	}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Trash(ctx, cloudstore.Actor{UserID: user}, lixo, now); err != nil {
		t.Fatal(err)
	}

	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	var logs contracttest.LockedBuffer
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: contracttest.BaseURL,
		SessionSecret: contracttest.SegredoDeSessao, DeviceTokenPepper: contracttest.PepperDeToken, Contas: contas,
		Google: cloudauth.NovoGoogle("cliente", "secret-do-client", contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Now: agora, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		IntervaloDaArrumacao: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloud.ArrumarSempre(runCtx)
	}()
	esperar := func(oque string, ok func([]map[string]any) bool) []map[string]any {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			got := linhas(t, logs.String(), "retenção")
			if ok(got) {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s não aconteceu: %s", oque, logs.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	// A rodada da subida: nada venceu ainda.
	primeira := esperar("a rodada da subida", func(l []map[string]any) bool { return len(l) >= 1 })
	if primeira[0]["lixeira"] != float64(0) {
		t.Fatalf("a rodada da subida = %v, quero nada apagado", primeira[0])
	}
	// O relógio passa dos 30 dias da lixeira: um tique seguinte apaga.
	mu.Lock()
	relogio = now.AddDate(0, 0, 30)
	mu.Unlock()
	got := esperar("a rodada do tique que apaga a lixeira", func(l []map[string]any) bool {
		for _, x := range l {
			if x["lixeira"] == float64(1) {
				return true
			}
		}
		return false
	})
	cancel()
	<-done
	if len(got) < 2 {
		t.Fatalf("%d linhas \"retenção\", quero a da subida e a de pelo menos um tique", len(got))
	}
	if n := len(linhas(t, logs.String(), "arrumação")); n < 2 {
		t.Errorf("%d linhas \"arrumação\", quero a da subida e a de pelo menos um tique", n)
	}
	if _, err := mem.Get(ctx, user, lixo); !errors.Is(err, cloudstore.ErrNotFound) {
		t.Fatalf("a lixeira vencida continua: %v", err)
	}
}

// storeQueFalha embrulha o Store em memória e faz o Reter de alguns usuários
// (ou a listagem) falharem com o erro escolhido.
type storeQueFalha struct {
	*cloudstore.MemoryStore
	listar  error
	falhar  map[string]error
	durante func(userID string)
}

func (s *storeQueFalha) UsuariosDaRetencao(ctx context.Context) ([]string, error) {
	if s.listar != nil {
		return nil, s.listar
	}
	return s.MemoryStore.UsuariosDaRetencao(ctx)
}

func (s *storeQueFalha) Reter(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	if s.durante != nil {
		s.durante(userID)
	}
	if err := s.falhar[userID]; err != nil {
		return cloudstore.Retido{}, err
	}
	return s.MemoryStore.Reter(ctx, userID, now)
}

// erroDoDriver imita o erro do Store do Postgres: o texto cru (que o log não
// pode mostrar) e a operação e o SQLSTATE (que pode).
type erroDoDriver struct{ cru string }

func (e erroDoDriver) Error() string    { return e.cru }
func (e erroDoDriver) Operacao() string { return "apagar o ditado" }
func (e erroDoDriver) SQLState() string { return "40P01" }

// O erro de um Store que falha sai só com a categoria: a operação e o
// SQLSTATE do banco, ou "outro"; nunca o texto do erro, o id nem o texto
// ditado. Um usuário que falha não para os outros.
func TestReter_OErroSoComACategoria(t *testing.T) {
	ctx := context.Background()
	mem := cloudstore.NewMemoryStore()
	a, b, c := "00000000-0000-7000-8000-00000000000a", "00000000-0000-7000-8000-00000000000b", "00000000-0000-7000-8000-00000000000c"
	for _, u := range []string{a, b, c} {
		mem.AddUser(u)
	}
	const cru = "ERROR: deadlock detected DETAIL: Failing row contains (" + "00000000-0000-7000-8000-00000000000a" + ", TEXTO-DITADO-SIGILOSO) host=10.0.0.9"
	s := &storeQueFalha{MemoryStore: mem, falhar: map[string]error{
		a: erroDoDriver{cru: cru},
		b: errors.New("pgstore: abrir a transação: dial tcp 10.0.0.9:5432: " + cru),
	}}
	var logs contracttest.LockedBuffer
	cloud := novaNuvemDoJob(t, s, mem, &logs, time.Now)
	cloud.Reter(ctx)

	out := logs.String()
	for _, proibido := range []string{"TEXTO-DITADO-SIGILOSO", "10.0.0.9", "deadlock detected", a, b, c} {
		if strings.Contains(out, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, out)
		}
	}
	falhas := linhas(t, out, "retenção falhou")
	if len(falhas) != 2 {
		t.Fatalf("%d linhas de falha, quero 2:\n%s", len(falhas), out)
	}
	banco, outro := falhas[0], falhas[1]
	if banco["categoria"] != "banco" || banco["operacao"] != "apagar o ditado" || banco["sqlstate"] != "40P01" || banco["level"] != "ERROR" {
		t.Errorf("a falha do banco = %v", banco)
	}
	if outro["categoria"] != "outro" || outro["sqlstate"] != nil {
		t.Errorf("a falha de outro tipo = %v", outro)
	}
	linha := linhas(t, out, "retenção")
	if len(linha) != 1 || linha[0]["usuarios"] != float64(3) || linha[0]["falhas"] != float64(2) {
		t.Fatalf("a linha da rodada = %v, quero 3 usuários e 2 falhas", linha)
	}

	// A listagem que falha: a falha e a linha, com zero usuários.
	var logs2 contracttest.LockedBuffer
	s.listar = errors.New("pgstore: listar os usuários: " + cru)
	cloud = novaNuvemDoJob(t, s, mem, &logs2, time.Now)
	cloud.Reter(ctx)
	out = logs2.String()
	if strings.Contains(out, "TEXTO-DITADO-SIGILOSO") || strings.Contains(out, "10.0.0.9") {
		t.Errorf("o log traz o erro cru: %s", out)
	}
	if f := linhas(t, out, "retenção falhou"); len(f) != 1 || f[0]["etapa"] != "usuarios" || f[0]["categoria"] != "outro" {
		t.Errorf("a falha da listagem = %v", f)
	}
	if l := linhas(t, out, "retenção"); len(l) != 1 || l[0]["usuarios"] != float64(0) || l[0]["falhas"] != float64(1) {
		t.Errorf("a linha da rodada com a listagem falhando = %v", l)
	}
}

// O ctx cancelado no meio da rodada (o desligamento): para entre um usuário e
// outro, a falha do que foi interrompido sai como "contexto", e a linha da
// rodada sai assim mesmo.
func TestReter_CtxCanceladoNoMeioParaEntreUsuarios(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	users := []string{contracttest.NewID(), contracttest.NewID(), contracttest.NewID()}
	for _, u := range users {
		mem.AddUser(u)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var vistos []string
	s := &storeQueFalha{MemoryStore: mem, durante: func(u string) {
		vistos = append(vistos, u)
		cancel()
	}}
	var logs contracttest.LockedBuffer
	cloud := novaNuvemDoJob(t, s, mem, &logs, time.Now)
	cloud.Reter(ctx)
	if len(vistos) != 1 {
		t.Fatalf("o Reter rodou para %d usuários depois do cancelamento, quero 1", len(vistos))
	}
	if f := linhas(t, logs.String(), "retenção falhou"); len(f) != 1 || f[0]["categoria"] != "contexto" {
		t.Fatalf("a falha do interrompido = %v, quero a categoria contexto", f)
	}
	if l := linhas(t, logs.String(), "retenção"); len(l) != 1 || l[0]["usuarios"] != float64(1) || l[0]["falhas"] != float64(1) {
		t.Fatalf("a linha da rodada = %v", l)
	}
}
