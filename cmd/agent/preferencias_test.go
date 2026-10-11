package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/autostart"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TestPreferenciasPadrao_BatemComODefaultConfig: a tela mostra, antes da
// primeira gravação, o que o Agent de fato faz ao subir.
func TestPreferenciasPadrao_BatemComODefaultConfig(t *testing.T) {
	cfg, p := defaultConfig(), preferenciasPadrao()
	if p.HoldThresholdMs != cfg.HoldThreshold.Milliseconds() || p.BeepEnabled != cfg.BeepEnabled {
		t.Fatalf("padrão da tela = %dms beep=%v; defaultConfig = %v beep=%v",
			p.HoldThresholdMs, p.BeepEnabled, cfg.HoldThreshold, cfg.BeepEnabled)
	}
	// O microfone que a tela mostra antes da primeira gravação é o que
	// configDeAudio abre: sob demanda (ADR-015).
	if p.MicrophoneAlwaysReady != configDeAudio(cfg).SemprePronto {
		t.Fatalf("a tela diz sempre pronto = %v, e o Agent sobe com %v", p.MicrophoneAlwaysReady, configDeAudio(cfg).SemprePronto)
	}
	if p.TextMode != "smart" || p.AIProvider != "" || p.AIModel != "" {
		t.Fatalf("modo/IA padrão = %q/%q/%q, quero smart sem provedor (REQ-022)", p.TextMode, p.AIProvider, p.AIModel)
	}
	// "ctrl+win" só é verdade enquanto o gatilho for essa combinação.
	if cfg.Key != dictation.KeyUnknown || cfg.Modifiers != dictation.ModControl|dictation.ModMeta || p.Hotkey != "ctrl+win" {
		t.Fatalf("a tela diz %q, e o gatilho é Key=%v Modifiers=%v", p.Hotkey, cfg.Key, cfg.Modifiers)
	}
}

// TestPreferenciasPadrao_SaoAceitasPelaAPI: o primeiro PUT grava sobre o
// padrão. Um padrão que a validação recusasse travaria a tela na
// primeira gravação, com qualquer mudança.
func TestPreferenciasPadrao_SaoAceitasPelaAPI(t *testing.T) {
	api := localapi.New("local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(),
		localapi.Preferencias{
			Padrao: preferenciasPadrao(), Chaves: chaves.NovaMemoria(),
			Inicio: inicioComWindows{console: true, log: slog.Default()}, Modelos: &ia.Cliente{},
		})
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"base_version":0}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT sobre o padrão: %d %s", w.Code, w.Body.String())
	}
}

func TestPreferenciasDoRuntime(t *testing.T) {
	for _, c := range []struct {
		s     storage.Settings
		quero winruntime.Preferencias
	}{
		{storage.Settings{HoldThresholdMs: 900, BeepEnabled: false, TextMode: "literal"},
			winruntime.Preferencias{HoldThreshold: 900 * time.Millisecond, BeepEnabled: false, Literal: true}},
		{storage.Settings{HoldThresholdMs: 300, BeepEnabled: true, TextMode: "smart"},
			winruntime.Preferencias{HoldThreshold: 300 * time.Millisecond, BeepEnabled: true}},
		// Polish (ADR-016, S4): o modo e a escolha de IA chegam ao runtime.
		{storage.Settings{HoldThresholdMs: 2000, BeepEnabled: true, TextMode: "polish", AIProvider: "openai", AIModel: "modelo-x"},
			winruntime.Preferencias{HoldThreshold: 2 * time.Second, BeepEnabled: true, Polish: true, Provedor: "openai", Modelo: "modelo-x"}},
		{storage.Settings{HoldThresholdMs: 700, TextMode: "polish", AIProvider: "anthropic", AIModel: "outro"},
			winruntime.Preferencias{HoldThreshold: 700 * time.Millisecond, Polish: true, Provedor: "anthropic", Modelo: "outro"}},
		// A escolha de IA gravada viaja também fora do Polish: o runtime só
		// a usa com o Polish ligado, e o Smart e o Literal não chamam ninguém.
		{storage.Settings{HoldThresholdMs: 700, TextMode: "smart", AIProvider: "openai", AIModel: "m"},
			winruntime.Preferencias{HoldThreshold: 700 * time.Millisecond, Provedor: "openai", Modelo: "m"}},
		{storage.Settings{HoldThresholdMs: 700, TextMode: "literal", AIProvider: "openai", AIModel: "m"},
			winruntime.Preferencias{HoldThreshold: 700 * time.Millisecond, Literal: true, Provedor: "openai", Modelo: "m"}},
	} {
		if got := preferenciasDoRuntime(c.s); got != c.quero {
			t.Errorf("preferenciasDoRuntime(%+v) = %+v, quero %+v", c.s, got, c.quero)
		}
	}
}

// micFalso é a Session no que as preferências mudam nela.
type micFalso struct {
	pedidos []bool
	sempre  bool
	falha   error
}

func (m *micFalso) Falha() error { return m.falha }

func (m *micFalso) DefinirSemprePronto(v bool) {
	m.pedidos = append(m.pedidos, v)
	m.sempre = v
}

func (m *micFalso) Modo() string {
	if m.sempre {
		return audio.ModoSemprePronto
	}
	return audio.ModoSobDemanda
}

// TestAplicarNoRuntime: cada gravação chega ao runtime, traduzida, e ao
// microfone, e nada fica pendente para o próximo início.
func TestAplicarNoRuntime(t *testing.T) {
	var pedidos []winruntime.Preferencias
	mic := &micFalso{}
	aplicar := aplicarNoRuntime(aplicarPreferencias(func(p winruntime.Preferencias) { pedidos = append(pedidos, p) }, mic, slog.New(slog.DiscardHandler)))
	s := preferenciasPadrao()
	s.HoldThresholdMs, s.TextMode = 450, "literal"
	if pendente := aplicar(s); len(pendente) != 0 {
		t.Fatalf("reinicio_pendente = %v, quero vazio: tudo vale no próximo ditado", pendente)
	}
	quero := winruntime.Preferencias{HoldThreshold: 450 * time.Millisecond, BeepEnabled: true, Literal: true}
	if len(pedidos) != 1 || pedidos[0] != quero {
		t.Fatalf("pedidos = %+v, quero [%+v]", pedidos, quero)
	}
	if len(mic.pedidos) != 1 || mic.pedidos[0] {
		t.Fatalf("microfone recebeu %v, quero [false]", mic.pedidos)
	}
}

// TestAplicarPreferencias_MicrofoneNaHora: o "sempre pronto" vai para a
// Session no próprio Salvar, nos dois sentidos, sem esperar um ditado
// como o runtime — e o log diz quando o modo mudou, e só então.
func TestAplicarPreferencias_MicrofoneNaHora(t *testing.T) {
	var log bytes.Buffer
	mic := &micFalso{}
	var ajustes int
	aplicar := aplicarNoRuntime(aplicarPreferencias(func(winruntime.Preferencias) { ajustes++ }, mic, slog.New(slog.NewTextHandler(&log, nil))))
	s := preferenciasPadrao()
	const mudou = "audio: modo do microfone mudou"

	s.MicrophoneAlwaysReady = true
	if pendente := aplicar(s); len(pendente) != 0 {
		t.Fatalf("ligar o sempre pronto deixou %v para o próximo início; vale na hora", pendente)
	}
	if mic.Modo() != audio.ModoSemprePronto {
		t.Fatalf("depois de ligar, modo = %q", mic.Modo())
	}
	if n := strings.Count(log.String(), mudou); n != 1 || !strings.Contains(log.String(), "modo=sempre_pronto") {
		t.Fatalf("log ao ligar (%d linha[s]):\n%s", n, log.String())
	}

	// Salvar de novo sem mexer no microfone não é mudança.
	aplicar(s)
	if n := strings.Count(log.String(), mudou); n != 1 {
		t.Fatalf("o mesmo modo gravado de novo saiu no log como mudança:\n%s", log.String())
	}

	s.MicrophoneAlwaysReady = false
	aplicar(s)
	if mic.Modo() != audio.ModoSobDemanda || !strings.Contains(log.String(), "modo=sob_demanda") {
		t.Fatalf("depois de desligar, modo = %q, log:\n%s", mic.Modo(), log.String())
	}
	if quero := []bool{true, true, false}; !slices.Equal(mic.pedidos, quero) || ajustes != 3 {
		t.Fatalf("microfone = %v (quero %v), runtime ajustado %d vez(es) (quero 3)", mic.pedidos, quero, ajustes)
	}
}

// TestAplicarPreferencias_SessaoMortaNaoDizQueMudou: com a sessão de áudio
// morta, o Salvar grava o modo, mas nenhum microfone liga até reiniciar o
// Agent. O log não pode dizer "mudou" — diz, em WARN, que o microfone não
// responde. O resto das preferências segue para o runtime normalmente.
func TestAplicarPreferencias_SessaoMortaNaoDizQueMudou(t *testing.T) {
	var log bytes.Buffer
	mic := &micFalso{falha: errors.New("audio: dispositivo de captura falhou ou foi removido")}
	var ajustes int
	aplicar := aplicarPreferencias(func(winruntime.Preferencias) { ajustes++ }, mic, slog.New(slog.NewTextHandler(&log, nil)))
	s := preferenciasPadrao()
	s.MicrophoneAlwaysReady = true
	aplicar(s)

	if strings.Contains(log.String(), "audio: modo do microfone mudou") {
		t.Errorf("o log diz que o modo mudou com a sessão de áudio morta:\n%s", log.String())
	}
	for _, trecho := range []string{"level=WARN", "o microfone não responde", "modo=sempre_pronto"} {
		if !strings.Contains(log.String(), trecho) {
			t.Errorf("o log não diz %q:\n%s", trecho, log.String())
		}
	}
	if ajustes != 1 || !slices.Equal(mic.pedidos, []bool{true}) {
		t.Errorf("runtime ajustado %d vez(es), microfone %v; quero 1 e [true]: o modo fica gravado", ajustes, mic.pedidos)
	}

	// Salvar de novo sem mudar o modo não repete o aviso.
	aplicar(s)
	if n := strings.Count(log.String(), "o microfone não responde"); n != 1 {
		t.Errorf("%d avisos, quero 1: o mesmo modo gravado de novo não é mudança", n)
	}
}

// settingsQueFalha é um repositório com o banco quebrado.
type settingsQueFalha struct{}

func (settingsQueFalha) Get(context.Context) (storage.Settings, error) {
	return storage.Settings{}, errors.New("disco cheio")
}
func (settingsQueFalha) Put(context.Context, storage.Settings, int64) (storage.Settings, error) {
	return storage.Settings{}, errors.New("disco cheio")
}

// TestCarregarPreferencias: o que foi gravado chega ao runtime na
// subida; sem nada gravado, ou com o banco quebrado, nada é pedido e
// valem as de defaultConfig.
func TestCarregarPreferencias(t *testing.T) {
	repo := storage.NewInMemorySettingsRepository()
	gravado := preferenciasPadrao()
	gravado.HoldThresholdMs, gravado.BeepEnabled, gravado.TextMode = 1200, false, "literal"
	gravado.MicrophoneAlwaysReady = true

	var pedidos []winruntime.Preferencias
	mic := &micFalso{}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	aplicar := aplicarPreferencias(func(p winruntime.Preferencias) { pedidos = append(pedidos, p) }, mic, logger)

	var esperas []time.Duration
	dormir := func(d time.Duration) { esperas = append(esperas, d) }

	carregarPreferencias(repo, aplicar, logger, dormir)
	if len(pedidos) != 0 || len(mic.pedidos) != 0 || len(esperas) != 0 {
		t.Fatalf("sem nada gravado, pediu %+v e %v ao microfone, e esperou %v", pedidos, mic.pedidos, esperas)
	}
	carregarPreferencias(settingsQueFalha{}, aplicar, logger, dormir)
	if len(pedidos) != 0 || len(mic.pedidos) != 0 || !strings.Contains(log.String(), "disco cheio") {
		t.Fatalf("com o banco quebrado: pedidos %+v, microfone %v, log:\n%s", pedidos, mic.pedidos, log.String())
	}
	// Tentou de novo uma vez só, e disse que desistiu.
	if !slices.Equal(esperas, []time.Duration{esperaParaReler}) || !strings.Contains(log.String(), "nem tentando de novo") {
		t.Fatalf("com o banco quebrado: esperas %v, log:\n%s", esperas, log.String())
	}

	if _, err := repo.Put(context.Background(), gravado, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	carregarPreferencias(repo, aplicar, logger, dormir)
	quero := winruntime.Preferencias{HoldThreshold: 1200 * time.Millisecond, BeepEnabled: false, Literal: true}
	if len(pedidos) != 1 || pedidos[0] != quero {
		t.Fatalf("pedidos = %+v, quero [%+v]", pedidos, quero)
	}
	// Gravado "sempre pronto": o microfone liga já na subida.
	if len(mic.pedidos) != 1 || !mic.pedidos[0] || !strings.Contains(log.String(), "microfone_sempre_pronto=true") {
		t.Fatalf("microfone = %v, quero [true] na subida; log:\n%s", mic.pedidos, log.String())
	}
}

// settingsQueFalhaUmaVez é o banco ocupado na subida: a primeira leitura
// falha, a segunda lê o que foi gravado.
type settingsQueFalhaUmaVez struct {
	storage.SettingsRepository
	leituras int
}

func (s *settingsQueFalhaUmaVez) Get(ctx context.Context) (storage.Settings, error) {
	s.leituras++
	if s.leituras == 1 {
		return storage.Settings{}, errors.New("database is locked")
	}
	return s.SettingsRepository.Get(ctx)
}

// TestCarregarPreferencias_ErroPassageiroTentaDeNovo: um erro passageiro na
// subida não pode deixar o microfone sob demanda com o banco dizendo
// "sempre pronto". Uma segunda leitura, depois de uma espera curta, lê o
// que foi gravado.
func TestCarregarPreferencias_ErroPassageiroTentaDeNovo(t *testing.T) {
	gravado := preferenciasPadrao()
	gravado.MicrophoneAlwaysReady = true
	repo := &settingsQueFalhaUmaVez{SettingsRepository: storage.NewInMemorySettingsRepository()}
	if _, err := repo.Put(context.Background(), gravado, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	mic := &micFalso{}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	var esperas []time.Duration

	carregarPreferencias(repo, aplicarPreferencias(func(winruntime.Preferencias) {}, mic, logger), logger,
		func(d time.Duration) { esperas = append(esperas, d) })

	if repo.leituras != 2 || !slices.Equal(esperas, []time.Duration{esperaParaReler}) {
		t.Fatalf("leituras = %d, esperas = %v; quero 2 leituras e uma espera de %s", repo.leituras, esperas, esperaParaReler)
	}
	if !slices.Equal(mic.pedidos, []bool{true}) {
		t.Errorf("microfone = %v, quero [true]: o sempre pronto gravado não chegou à subida", mic.pedidos)
	}
	for _, trecho := range []string{"tento de novo", "database is locked", "preferencias: carregadas", "microfone_sempre_pronto=true"} {
		if !strings.Contains(log.String(), trecho) {
			t.Errorf("o log não tem %q:\n%s", trecho, log.String())
		}
	}
	if esperaParaReler > 500*time.Millisecond {
		t.Errorf("esperaParaReler = %s: a subida não pode ficar esperando o banco", esperaParaReler)
	}
}

// TestInicioComWindows_ConsoleNaoTocaORegistro: a mesma regra da bandeja
// vale para o painel — a build de console nem lê o registro, e recusa
// mudar nos dois sentidos.
func TestInicioComWindows_ConsoleNaoTocaORegistro(t *testing.T) {
	i := inicioComWindows{exe: `C:\x\agent-console.exe`, console: true, log: slog.Default()}
	if ligado, motivo := i.Estado(); ligado || motivo != motivoSoNaBandeja {
		t.Fatalf("Estado = %v, %q; quero desligado e %q", ligado, motivo, motivoSoNaBandeja)
	}
	for _, ligar := range []bool{true, false} {
		if err := i.Definir(ligar); err == nil || !strings.Contains(err.Error(), "recusado") {
			t.Fatalf("Definir(%v) na build de console: err = %v", ligar, err)
		}
	}
}

// TestInicioComWindows_SemExecutavelFicaIndisponivel: quando
// os.Executable falha, run segue com exe vazio em vez de derrubar o Agent
// — e aí o início automático fica indisponível, com o motivo, e nunca
// chega ao registro (gravaria um caminho vazio no Run).
func TestInicioComWindows_SemExecutavelFicaIndisponivel(t *testing.T) {
	i := inicioComWindows{log: slog.Default()}
	if ligado, motivo := i.Estado(); ligado || motivo != motivoSemExecutavel {
		t.Fatalf("Estado = %v, %q; quero desligado e %q", ligado, motivo, motivoSemExecutavel)
	}
	for _, ligar := range []bool{true, false} {
		// O motivo na mensagem prova que a recusa veio daqui, antes do
		// pacote autostart — e não do portão dele.
		if err := i.Definir(ligar); err == nil || !strings.Contains(err.Error(), motivoSemExecutavel) {
			t.Fatalf("Definir(%v) sem executável: err = %v; quero recusado antes do registro", ligar, err)
		}
	}
	// Na build de console, o motivo continua o dela.
	i.console = true
	if _, motivo := i.Estado(); motivo != motivoSoNaBandeja {
		t.Fatalf("console sem executável: motivo = %q, quero %q", motivo, motivoSoNaBandeja)
	}
}

// TestInicioComWindows_DefinirPassaPeloPortao: na build de bandeja, o
// pedido chega ao pacote autostart — que, sob go test, recusa no portão
// antes de tocar o registro.
func TestInicioComWindows_DefinirPassaPeloPortao(t *testing.T) {
	quero := sysinput.ErrDisabledInTest
	if runtime.GOOS != "windows" {
		quero = autostart.ErrNaoSuportado
	}
	i := inicioComWindows{exe: `C:\x\agent.exe`, log: slog.Default()}
	for _, ligar := range []bool{true, false} {
		if err := i.Definir(ligar); !errors.Is(err, quero) {
			t.Fatalf("Definir(%v): err = %v, quero %v", ligar, err, quero)
		}
	}
}
