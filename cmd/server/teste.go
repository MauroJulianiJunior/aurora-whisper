//go:build logindeteste

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O servidor de teste da nuvem (ADR-019, F4; decisão 2 do plano da F4): o
// mesmo servidor do serve, no PC, para ver o app sem Docker e sem o client do
// Google. Guarda tudo na memória (some ao desligar), não tem o login do
// Google, entra só com a conta de teste, e já abre com ditados de exemplo
// inventados, de dois PCs, um na lixeira e outro com o selo de conflito de
// verdade (criado pelo push, pelas mesmas regras da nuvem).
//
// Só existe com a build tag logindeteste, que também só deixa subir em
// localhost e escutar no loopback. As chaves do servidor nascem aqui, a cada
// subida, e nunca são impressas.
//
//	go -C C:\Desenvolvimento\aurora-whisper run -tags logindeteste ./cmd/server teste
//
// Configuração pelo ambiente: PAINEL_DIR (obrigatória, o build da nuvem do
// painel) e HTTP_ADDR (opcional; padrão 127.0.0.1:8080, só loopback). O
// endereço do app é http://localhost:<porta>.

const usoDoTeste = `  teste             (só no binário com a build tag logindeteste) sobe a nuvem de
                    teste no PC: na memória, sem Google, com a conta de teste
                    e ditados de exemplo (PAINEL_DIR e, opcional, HTTP_ADDR)
`

// ContaDeTeste é a conta do servidor de teste: inventada, de um domínio que
// não existe.
const ContaDeTeste = "voce@teste.local"

// IDDaContaDeTeste é o id da conta de teste, o mesmo a cada subida. O PC
// conectado guarda a conta e recusa outra (a D3 da ADR-019, F6): com um id
// novo a cada subida, a nuvem de teste reiniciada prenderia o bin\ ("Este PC
// já está ligado a outra conta."), sem saída pelo painel.
const IDDaContaDeTeste = "00000000-7e57-7000-8000-000000000001"

// envSemExemplos, com "1", sobe a nuvem de teste vazia (só a conta).
const envSemExemplos = "TESTE_SEM_EXEMPLOS"

func runTeste(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "teste: argumento inesperado %q (a configuração vem do ambiente: PAINEL_DIR e HTTP_ADDR)\n", args[0])
		return 2
	}
	fail := func(code int, format string, a ...any) int {
		fmt.Fprintln(stderr, "teste: "+fmt.Sprintf(format, a...))
		return code
	}
	painel, err := lerPainelDir(getenv(envPainelDir))
	if err != nil {
		return fail(2, "%v", err)
	}
	addr := getenv(envHTTPAddr)
	if addr == "" {
		addr = defaultHTTPAddr
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !server.HostLoopback(host) {
		return fail(2, "%s: tem de ser um endereço da própria máquina, com porta (por exemplo 127.0.0.1:8080)", envHTTPAddr)
	}
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Warn("servidor de teste (build tag logindeteste): na memória, sem Google, só neste PC; nunca em produção")

	mem := cloudstore.NewMemoryStore()
	// TESTE_SEM_EXEMPLOS=1: só a conta de teste, sem os ditados de exemplo.
	// Um PC conectado puxa tudo o que a nuvem tem, e o bin\ do Mauro é o histórico
	// de uso real: os exemplos inventados ficariam lá (roteiro da F7).
	comExemplos := getenv(envSemExemplos) != "1"
	if err := semearExemplos(ctx, mem, time.Now(), comExemplos); err != nil {
		return fail(1, "não consegui criar os ditados de exemplo: %v", err)
	}
	contas, err := cloudauth.ParseContas(ContaDeTeste)
	if err != nil {
		return fail(1, "%v", err)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fail(1, "não consegui escutar em %s: %v", envHTTPAddr, err)
	}
	porta := ln.Addr().(*net.TCPAddr).Port
	base := fmt.Sprintf("http://localhost:%d", porta)
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: base,
		// As duas chaves valem só enquanto o processo vive: reiniciar derruba
		// as sessões, como a memória que some.
		SessionSecret: []byte(cloudauth.NovoSegredo()), DeviceTokenPepper: []byte(cloudauth.NovoSegredo()),
		Contas: contas, Painel: painel, Logger: logger,
	})
	if err != nil {
		_ = ln.Close()
		return fail(2, "%v", err)
	}
	if onListen != nil {
		onListen(ln.Addr())
	}
	logger.Info("servindo", "endereco", ln.Addr().String(), "teste", true)
	fmt.Fprintf(stdout, "\nNuvem de teste em %s\nEntre com a conta de teste (%s). Para desligar: Ctrl+C.\n\n", base, ContaDeTeste)
	return servir(ctx, cloud, ln, logger, fail, cloud.ArrumarSempre)
}

// exemplo é um ditado inventado: há quanto tempo, de qual PC, o texto bruto e
// o final.
type exemplo struct {
	ha    time.Duration
	pc    int
	bruto string
	final string
	// lixeira: foi para a lixeira há esse tempo (zero: não foi).
	lixeira time.Duration
	// outros: o selo. O outro PC muda o texto, e este PC muda com a base
	// velha: o texto deste vai para o selo.
	selo [2]string
}

// exemplos: nada pessoal, nada de cliente; só o bastante para ver o
// histórico em dias diferentes (de hoje a três dias atrás; o dia de cada um
// depende da hora em que o servidor sobe), a diferença do bruto, a lixeira e
// o selo.
var exemplos = []exemplo{
	{ha: 25 * time.Minute, pc: 0, bruto: "bom dia pessoal a reunião de hoje passa para as quinze horas", final: "Bom dia, pessoal. A reunião de hoje passa para as 15h."},
	{ha: 95 * time.Minute, pc: 1, bruto: "lembrar de mandar o relatório mensal até sexta feira", final: "Lembrar de mandar o relatório mensal até sexta-feira."},
	{ha: 3*time.Hour + 10*time.Minute, pc: 0, bruto: "comprar café pão e frutas na volta para casa", final: "Comprar café, pão e frutas na volta para casa."},
	{ha: 26 * time.Hour, pc: 1, bruto: "o orçamento revisado já está na pasta compartilhada", final: "O orçamento revisado já está na pasta compartilhada."},
	{ha: 29 * time.Hour, pc: 0, bruto: "confirmo a entrega para quinta de manhã", final: "Confirmo a entrega para quinta de manhã.",
		selo: [2]string{"Confirmo a entrega para quinta à tarde.", "Confirmo a entrega para sexta de manhã."}},
	{ha: 50 * time.Hour, pc: 1, bruto: "rascunho antigo que não serve mais", final: "Rascunho antigo que não serve mais.", lixeira: 20 * time.Hour},
	{ha: 74 * time.Hour, pc: 0, bruto: "ideia para o fim de semana trilha curta e almoço no centro", final: "Ideia para o fim de semana: trilha curta e almoço no centro."},
}

// semearExemplos cria a conta de teste (com o id fixo e o sub do login de
// teste, para o POST /auth/teste achar a mesma), dois PCs, e os ditados pelo
// push de cada um, como um PC de verdade os mandaria.
func semearExemplos(ctx context.Context, mem *cloudstore.MemoryStore, agora time.Time, comExemplos bool) error {
	ident := cloudstore.Identidade{Sub: "teste:" + ContaDeTeste, Email: ContaDeTeste, Nome: "Conta de teste"}
	if err := mem.AddConta(IDDaContaDeTeste, ident); err != nil {
		return err
	}
	u, err := mem.EntrarComGoogle(ctx, ident, agora)
	if err != nil {
		return err
	}
	if !comExemplos {
		return nil
	}
	pcs := [2]cloudstore.Actor{{UserID: u.ID, DeviceID: cloudstore.NewUUIDv7()}, {UserID: u.ID, DeviceID: cloudstore.NewUUIDv7()}}
	for _, pc := range pcs {
		mem.AddDevice(u.ID, pc.DeviceID)
	}
	push := func(pc cloudstore.Actor, id string, base int64, payload map[string]any) (cloudstore.Result, error) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return cloudstore.Result{}, err
		}
		resp, err := mem.Push(ctx, pc, []cloudstore.Mutation{{
			MutationID: cloudstore.NewUUIDv7(), EntityType: cloudstore.EntityTranscription, EntityID: id,
			Operation: cloudstore.OpUpsert, BaseVersion: base, Payload: raw,
		}}, agora)
		if err != nil {
			return cloudstore.Result{}, err
		}
		return resp.Results[0], nil
	}
	for i, e := range exemplos {
		criado := agora.Add(-e.ha)
		payload := map[string]any{
			"source_platform": "windows", "raw_text": e.bruto, "final_text": e.final, "mode": "smart",
			"language": "pt-BR", "asr_provider": "whisper-local", "asr_model": "large-v3-turbo",
			"duration_ms": 2400 + 300*i, "processing_ms": 380 + 20*i, "confidence": 0.9,
			"status": "done", "created_at": criado.UTC().Format(time.RFC3339Nano),
		}
		if e.lixeira > 0 {
			payload["deleted_at"] = agora.Add(-e.lixeira).UTC().Format(time.RFC3339Nano)
		}
		id := cloudstore.NewUUIDv7()
		pc := pcs[e.pc]
		if r, err := push(pc, id, 0, payload); err != nil || r.Status != cloudstore.StatusApplied {
			return errors.Join(err, fmt.Errorf("criar o exemplo %d: %s %s", i+1, r.Status, r.Code))
		}
		if e.selo[0] == "" {
			continue
		}
		outro := pcs[1-e.pc]
		if r, err := push(outro, id, 1, map[string]any{"final_text": e.selo[0]}); err != nil || r.Status != cloudstore.StatusApplied {
			return errors.Join(err, fmt.Errorf("mudar o exemplo %d: %s %s", i+1, r.Status, r.Code))
		}
		if r, err := push(pc, id, 1, map[string]any{"final_text": e.selo[1]}); err != nil || r.Status != cloudstore.StatusConflict {
			return errors.Join(err, fmt.Errorf("o selo do exemplo %d: %s %s", i+1, r.Status, r.Code))
		}
	}
	return nil
}
