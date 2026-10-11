package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/expurgo"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/uia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TestVersionAndNameNotEmpty é um teste de fumaça: garante apenas que as
// constantes de identificação do binário estão definidas. Não valida
// comportamento de negócio (ainda inexistente neste sprint).
func TestVersionAndNameNotEmpty(t *testing.T) {
	if appName == "" {
		t.Error("appName não deve ser vazio")
	}
	if version == "" {
		t.Error("version não deve ser vazio")
	}
}

// TestInicioIndisponivel trava o defeito da revisão de 29-set-2026: a
// build de console mostrava "Iniciar com o Windows" clicável e gravava a
// si mesma no Run. Com console, o item fica indisponível — mesmo que o
// registro tenha sido lido sem erro.
func TestInicioIndisponivel(t *testing.T) {
	ilegivel := errors.New("acesso negado")
	casos := []struct {
		nome    string
		console bool
		err     error
		quer    string
	}{
		{"build de bandeja, registro lido", false, nil, ""},
		{"build de bandeja, registro ilegível", false, ilegivel, motivoRegistroIlegivel},
		{"build de console", true, nil, motivoSoNaBandeja},
		{"build de console com registro ilegível", true, ilegivel, motivoSoNaBandeja},
	}
	for _, c := range casos {
		if got := inicioIndisponivel(c.console, c.err); got != c.quer {
			t.Errorf("%s: inicioIndisponivel = %q, quer %q", c.nome, got, c.quer)
		}
	}
}

// TestEncerrarComErro_SoltaAInstanciaAntesDeAvisar trava o defeito da
// revisão de 29-set-2026: o aviso (uma MessageBox que bloqueia) vinha
// antes de soltar a instância, e quem reabria o Agent com a caixa ainda
// aberta ouvia "já está rodando", sem Agent nenhum rodando.
func TestEncerrarComErro_SoltaAInstanciaAntesDeAvisar(t *testing.T) {
	var ordem []string
	encerrarComErro(errors.New("modelo de ASR: arquivo não encontrado"),
		func() { ordem = append(ordem, "fecharLog") },
		func() { ordem = append(ordem, "liberarInstancia") },
		func(_ string, ehErro bool) {
			ordem = append(ordem, "avisar")
			if !ehErro {
				t.Error("o aviso de erro fatal saiu como informação")
			}
		})
	if quer := []string{"fecharLog", "liberarInstancia", "avisar"}; !slices.Equal(ordem, quer) {
		t.Errorf("ordem = %v, quer %v — a instância precisa estar solta enquanto o aviso está na tela", ordem, quer)
	}
}

// TestEncerrarComErro_SemInstanciaAindaAvisa: fora do Windows, ou quando
// o mutex não pôde ser criado, não há instância para soltar — e o aviso
// continua saindo.
func TestEncerrarComErro_SemInstanciaAindaAvisa(t *testing.T) {
	avisou := false
	encerrarComErro(errors.New("x"), func() {}, nil, func(string, bool) { avisou = true })
	if !avisou {
		t.Error("sem instância, o erro fatal não foi avisado")
	}
}

// TestMensagemDeErro_NaoDizQueNaoIniciou: o mesmo aviso sai para o erro
// da subida e para o que derruba o Agent depois de rodar. "Não conseguiu
// iniciar" era falso no segundo caso (revisão de 29-set-2026).
func TestMensagemDeErro_NaoDizQueNaoIniciou(t *testing.T) {
	msg := mensagemDeErro(errors.New("runtime: o canal do hook fechou"))
	if strings.Contains(strings.ToLower(msg), "não conseguiu iniciar") {
		t.Errorf("a mensagem afirma que o Agent não iniciou, e o erro pode ter vindo depois de rodar: %q", msg)
	}
	for _, quer := range []string{"runtime: o canal do hook fechou", "agent.log"} {
		if !strings.Contains(msg, quer) {
			t.Errorf("a mensagem não traz %q: %q", quer, msg)
		}
	}
}

// TestSemBandeja trava o defeito da revisão de 29-set-2026: um "taskkill"
// durante as tentativas do ícone cancelava o contexto, mas se o ícone
// nunca entrava, run falhava — e quem tinha pedido para fechar recebia
// uma janela de erro.
func TestSemBandeja(t *testing.T) {
	casos := []struct {
		nome                string
		encerrando, console bool
		quer                passoSemBandeja
	}{
		{"encerramento pedido, sem console", true, false, semBandejaSairCalado},
		{"encerramento pedido, com console", true, true, semBandejaSairCalado},
		{"com console segue sem ícone", false, true, semBandejaSeguir},
		{"sem console e sem bandeja não sobe", false, false, semBandejaFalhar},
	}
	for _, c := range casos {
		if got := semBandeja(c.encerrando, c.console); got != c.quer {
			t.Errorf("%s: semBandeja = %d, quer %d", c.nome, got, c.quer)
		}
	}
}

// TestDescartarPendentes trava o defeito da revisão de 29-set-2026: teclas
// apertadas durante a subida ficavam na fila e eram reprocessadas
// atrasadas quando rt.Run começava, cortando o começo do ditado.
func TestDescartarPendentes(t *testing.T) {
	ch := make(chan dictation.RawKeyEvent, 16)
	ch <- dictation.RawKeyEvent{Down: true}
	ch <- dictation.RawKeyEvent{Down: false}
	ch <- dictation.RawKeyEvent{Down: true}
	if n := descartarPendentes(ch); n != 3 {
		t.Errorf("descartou %d, quer 3", n)
	}
	if len(ch) != 0 {
		t.Errorf("sobraram %d eventos da subida na fila", len(ch))
	}

	// Fila vazia: volta na hora. Esperar aqui travaria a subida até a
	// próxima tecla.
	voltou := make(chan int, 1)
	go func() { voltou <- descartarPendentes(ch) }()
	select {
	case n := <-voltou:
		if n != 0 {
			t.Errorf("fila vazia: descartou %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("com a fila vazia, descartarPendentes ficou esperando")
	}

	// Fechada: descarta o que havia e para, em vez de girar no canal
	// fechado para sempre.
	ch <- dictation.RawKeyEvent{Down: true}
	close(ch)
	go func() { voltou <- descartarPendentes(ch) }()
	select {
	case n := <-voltou:
		if n != 1 {
			t.Errorf("fila fechada: descartou %d, quer 1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("com a fila fechada, descartarPendentes não voltou")
	}
}

// TestDonoPelaRegraAntiga: o dono do histórico e do painel sai no log da
// subida (revisão de 29-set-2026) — sem ele, um histórico que some do
// painel não deixa pista. A regra antiga fica travada aqui: é ela que vale
// quando a migração para o dono fixo não pôde rodar.
func TestDonoPelaRegraAntiga(t *testing.T) {
	casos := []struct {
		nome, doDicionario, quer string
	}{
		{"usuário do dicionário", "mauro", "mauro"},
		{"sem dicionário", "", "local"},
	}
	for _, c := range casos {
		var buf bytes.Buffer
		got := donoPelaRegraAntiga(slog.New(slog.NewTextHandler(&buf, nil)), c.doDicionario)
		if got != c.quer {
			t.Errorf("%s: dono = %q, quer %q", c.nome, got, c.quer)
		}
		linha := buf.String()
		if !strings.Contains(linha, "level=INFO") || !strings.Contains(linha, "usuario="+c.quer) {
			t.Errorf("%s: o log da subida não diz o dono: %q", c.nome, linha)
		}
	}
}

// camposDoRegistro transforma a lista chave/valor do slog num mapa, para
// o teste perguntar pela chave em vez de pela posição.
func camposDoRegistro(t *testing.T, args []any) map[string]any {
	t.Helper()
	if len(args)%2 != 0 {
		t.Fatalf("args com número ímpar de itens: %v", args)
	}
	m := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			t.Fatalf("chave %v não é string", args[i])
		}
		m[k] = args[i+1]
	}
	return m
}

// TestRegistroDoFoco trava em que nível cada inspeção de foco vai para o
// log e o que ela leva. As duas coisas novas de 29-set-2026: a chamada
// mais lenta sai sempre (lenta_em), e a volta de protegido para livre sai
// em Info, com quanto tempo o cache ficou protegido (protegido_por) —
// antes ela era uma inspeção OK como outra qualquer, em Debug.
func TestRegistroDoFoco(t *testing.T) {
	casos := []struct {
		nome       string
		d          uia.DiagnosticoFoco
		querNivel  slog.Level
		querCampos map[string]any
		semCampos  []string
	}{
		{
			nome:      "inspeção OK comum fica em Debug",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectOK, LentaEm: uia.ChamadaGetFocusedElement, LentaDuracao: 3 * time.Millisecond},
			querNivel: slog.LevelDebug,
			querCampos: map[string]any{
				"outcome": "ok", "lenta_em": "get_focused_element", "lenta_duracao": 3 * time.Millisecond,
			},
			semCampos: []string{"protegido_por", "falha_em", "hresult", "descartada", "reinspecao", "reinspeciona_em", "no_proprio_agent", "vigia", "leitura_atrasada", "janela_elevada"},
		},
		{
			nome: "volta a livre sai em Info com quanto tempo ficou protegido",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectOK, Liberou: true, ProtegidoPor: 61 * time.Second,
				LentaEm: uia.ChamadaIsPassword},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"protegido_por": 61 * time.Second, "protegido": false, "lenta_em": "is_password"},
			semCampos:  []string{"falha_em"},
		},
		{
			nome: "timeout de um minuto é WARN e diz onde foi o minuto",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectTimeout, Protected: true, Duracao: time.Minute,
				LentaEm: uia.ChamadaNativeWindowHandle, LentaDuracao: time.Minute},
			querNivel: slog.LevelWarn,
			querCampos: map[string]any{
				"outcome": "timeout", "lenta_em": "native_window_handle", "lenta_duracao": time.Minute,
			},
			semCampos: []string{"protegido_por", "falha_em"},
		},
		{
			nome: "falha diz qual chamada e o HRESULT",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, Falha: uia.FalhaGetFocusedElement,
				HRESULT: -2147467259, LentaEm: uia.ChamadaGetFocusedElement},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"falha_em": "get_focused_element", "hresult": "0x80004005"},
		},
		{
			nome:       "campo de senha de verdade é Info",
			d:          uia.DiagnosticoFoco{Outcome: uia.InspectOK, Protected: true, IsPassword: true},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"is_password": true, "protegido": true},
			semCampos:  []string{"protegido_por", "descartada"},
		},
		{
			nome: "falha que agendou reinspeção diz para quando",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, Falha: uia.FalhaGetFocusedElement,
				HRESULT: -2147220991, ReinspecaoEm: 100 * time.Millisecond},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"reinspeciona_em": 100 * time.Millisecond, "hresult": "0x80040201"},
			semCampos:  []string{"reinspecao"},
		},
		{
			nome: "reinspeção que liberou diz qual tentativa foi e quanto tempo ficou protegido",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectOK, Liberou: true, ProtegidoPor: 110 * time.Millisecond,
				Tentativa: 1},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"reinspecao": 1, "protegido_por": 110 * time.Millisecond},
			semCampos:  []string{"reinspeciona_em"},
		},
		{
			nome:       "última reinspeção, ainda em dúvida, não agenda outra",
			d:          uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, Falha: uia.FalhaGetFocusedElement, Tentativa: 3},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"reinspecao": 3},
			semCampos:  []string{"reinspeciona_em"},
		},
		{
			// Não mexeu no cache, então não é WARN de bloqueio; mas é a
			// prova de que o callback foi reentrado, e sai em Info.
			nome:       "inspeção descartada pela geração sai em Info e diz que foi descartada",
			d:          uia.DiagnosticoFoco{Outcome: uia.InspectTimeout, Protected: true, Descartada: true},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"descartada": true, "outcome": "timeout"},
		},
		{
			// Uma inspeção OK e livre sairia em Debug e pareceria uma
			// volta a livre que não houve: tem de sair em Info e dizer
			// que o elemento era do próprio Agent.
			nome:       "reinspeção que caiu no próprio Agent sai em Info e diz isso",
			d:          uia.DiagnosticoFoco{Outcome: uia.InspectOK, NoProprioAgent: true, Tentativa: 1},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"no_proprio_agent": true, "reinspecao": 1},
			semCampos:  []string{"protegido_por", "reinspeciona_em", "descartada", "vigia"},
		},
		{
			// A chamada prendeu além do orçamento e o vigia protegeu o cache;
			// quando ela terminou livre, a volta sai em Info contando o
			// intervalo do vigia.
			nome: "volta a livre depois do vigia diz que foi o vigia",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectOK, Liberou: true, ProtegidoPor: 59 * time.Second,
				Vigia: true},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"vigia": true, "protegido_por": 59 * time.Second},
		},
		{
			// Leitura completa acima do orçamento que valeu: até 29-set-2026
			// seria um WARN de timeout. Não é bloqueio, mas sai em Info.
			nome:       "leitura atrasada que valeu sai em Info e diz isso",
			d:          uia.DiagnosticoFoco{Outcome: uia.InspectOK, Atrasada: true, Duracao: 515 * time.Millisecond},
			querNivel:  slog.LevelInfo,
			querCampos: map[string]any{"leitura_atrasada": true, "protegido": false, "outcome": "ok"},
			semCampos:  []string{"protegido_por"},
		},
		{
			// As rajadas de E_FAIL do Gerenciador de Tarefas: agora o log diz
			// que a janela é de administrador.
			nome: "falha em janela de administrador diz isso",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, Falha: uia.FalhaGetFocusedElement,
				HRESULT: -2147467259, Elevacao: uia.ElevacaoSim},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"janela_elevada": "sim", "hresult": "0x80004005"},
		},
		{
			nome: "falha em que o token não abriu diz desconhecida",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, Falha: uia.FalhaGetFocusedElement,
				Elevacao: uia.ElevacaoDesconhecida},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"janela_elevada": "desconhecida"},
		},
		{
			nome:       "timeout em que o vigia agiu é WARN e diz isso",
			d:          uia.DiagnosticoFoco{Outcome: uia.InspectTimeout, Protected: true, Vigia: true},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"vigia": true, "outcome": "timeout"},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			nivel, args := registroDoFoco(c.d)
			if nivel != c.querNivel {
				t.Errorf("nível = %v, quero %v", nivel, c.querNivel)
			}
			campos := camposDoRegistro(t, args)
			for k, v := range c.querCampos {
				if got, ok := campos[k]; !ok || got != v {
					t.Errorf("%s = %v (presente=%v), quero %v", k, got, ok, v)
				}
			}
			for _, k := range c.semCampos {
				if _, ok := campos[k]; ok {
					t.Errorf("%s não deveria sair nesta inspeção: %v", k, args)
				}
			}
		})
	}
}

// TestAbrirHistorico_ExpurgaALixeira cobre o defeito de a lixeira nunca
// ser esvaziada: o PurgeExpired existia, mas o Agent não o chamava. O
// teste passa pelo mesmo abrirHistoricoEm que o Agent usa, com um SQLite
// de verdade num diretório temporário e o relógio falso — nada age no
// sistema, e os 30 dias passam num Advance.
func TestAbrirHistorico_ExpurgaALixeira(t *testing.T) {
	ctx := context.Background()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	inicio := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	relogio := clock.NewFake(inicio)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	hist, _, dono, fechar, err := abrirHistoricoEm(log, caminho, "mauro", relogio)
	if err != nil {
		t.Fatalf("abrirHistoricoEm: %v", err)
	}
	item := func(id string) storage.Transcription {
		return storage.Transcription{
			ID: id, UserID: dono, SourcePlatform: "windows",
			RawText: "bruto", FinalText: "final", Mode: "smart",
			ASRProvider: "local", Status: "done",
			CreatedAt: inicio.AddDate(0, 0, -40), UpdatedAt: inicio.AddDate(0, 0, -40),
			Version: 1, SyncState: "pending",
		}
	}

	for _, id := range []string{"vencido", "recente", "ativo"} {
		if err := hist.Put(item(id)); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}
	// "vencido" foi para a lixeira há 30 dias: o prazo é agora.
	if err := hist.SoftDelete(ctx, dono, "vencido", inicio.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	if err := hist.SoftDelete(ctx, dono, "recente", inicio); err != nil {
		t.Fatal(err)
	}
	fechar()

	existe := func(id string) bool {
		t.Helper()
		_, err := hist.Get(ctx, dono, id)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Get %s: %v", id, err)
		}
		return err == nil
	}

	// Na subida: o vencido sai, o resto fica.
	hist, _, _, fechar, err = abrirHistoricoEm(log, caminho, "mauro", relogio)
	if err != nil {
		t.Fatalf("reabrindo: %v", err)
	}
	defer fechar()
	if existe("vencido") {
		t.Error("item com o prazo da lixeira vencido deveria ser apagado na subida")
	}
	if !existe("recente") || !existe("ativo") {
		t.Fatal("a subida só pode apagar o que venceu")
	}

	// Um ciclo antes do prazo do "recente", ele continua restaurável.
	relogio.Advance(30*24*time.Hour - expurgo.Intervalo)
	if !existe("recente") {
		t.Fatal("item da lixeira apagado antes dos 30 dias")
	}

	// No ciclo em que o prazo chega, some — sem reiniciar o Agent.
	relogio.Advance(expurgo.Intervalo)
	if existe("recente") {
		t.Error("item da lixeira deveria ser apagado pelo ciclo periódico ao completar 30 dias")
	}
	if !existe("ativo") {
		t.Error("o expurgo da lixeira não pode apagar item que não está nela")
	}
}
