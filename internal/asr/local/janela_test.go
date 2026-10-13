package local

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// A janela do encoder por requisição (incidente de 01-out-2026,
// docs/defeito-ditado-longo.md). Nada aqui sobe servidor: a escolha é pura,
// e o que vai na requisição é conferido contra um servidor de mentira.

func TestJanelaPorDuracao_AudioCtx(t *testing.T) {
	for _, c := range []struct {
		nome   string
		j      JanelaPorDuracao
		d      time.Duration
		valor  int
		enviar bool
	}{
		{"sem janela não manda o campo", JanelaPorDuracao{}, time.Second, 0, false},
		{"sem janela não manda nem no longo", JanelaPorDuracao{}, time.Minute, 0, false},
		{"fala curta usa a reduzida", JanelaRecomendada, time.Second, 768, true},
		{"o maior áudio do dataset medido", JanelaRecomendada, 9300 * time.Millisecond, 768, true},
		{"no limite ainda é curta", JanelaRecomendada, 12 * time.Second, 768, true},
		{"logo acima do limite é a inteira", JanelaRecomendada, 12*time.Second + time.Millisecond, 0, true},
		{"o ditado de 59 s do incidente", JanelaRecomendada, 59300 * time.Millisecond, 0, true},
		{"o de 89 s", JanelaRecomendada, 88900 * time.Millisecond, 0, true},
		{"duração desconhecida conta como curta", JanelaRecomendada, 0, 768, true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			valor, enviar := c.j.AudioCtx(c.d)
			if valor != c.valor || enviar != c.enviar {
				t.Errorf("AudioCtx(%v) = %d, %v; want %d, %v", c.d, valor, enviar, c.valor, c.enviar)
			}
		})
	}
}

// TestJanelaRecomendada_CobreOLimiteComFolga trava as razões da escolha:
// múltiplo de 256 (nenhuma linha de K/V lida sem ser gravada), dentro do
// que o servidor aceita (até 1500; acima ele devolve erro), e uma janela que
// cobre o limite com folga — a janela que não cobre o áudio é justamente o
// defeito.
func TestJanelaRecomendada_CobreOLimiteComFolga(t *testing.T) {
	j := JanelaRecomendada
	if j.Curta%256 != 0 {
		t.Errorf("Curta = %d, não é múltiplo de 256: sobram linhas de K/V da requisição anterior", j.Curta)
	}
	if j.Curta <= 0 || j.Curta > 1500 {
		t.Errorf("Curta = %d, fora de (0, 1500]", j.Curta)
	}
	janela := time.Duration(j.Curta) * 20 * time.Millisecond
	if folga := janela - j.Limite; folga < 3*time.Second {
		t.Errorf("a janela de %d (%v) cobre o limite de %v com só %v de folga; quero pelo menos 3 s", j.Curta, janela, j.Limite, folga)
	}
	if j.Limite < 9300*time.Millisecond {
		t.Errorf("Limite = %v, abaixo do maior áudio medido (9,3 s): a fala curta perderia o ganho medido", j.Limite)
	}
}

func TestCamposDeDecodificacao(t *testing.T) {
	junta := func(cs []campoForm) string {
		var s []string
		for _, c := range cs {
			s = append(s, c.nome+"="+c.valor)
		}
		return strings.Join(s, " ")
	}
	for _, c := range []struct {
		nome string
		cfg  Config
		d    time.Duration
		want string
	}{
		{"nada configurado não manda nada", Config{}, time.Minute, ""},
		{"o -ac da linha de comando não vira campo", Config{AudioCtx: 750}, time.Second, ""},
		{"curta", Config{Janela: JanelaRecomendada}, 5 * time.Second, "audio_ctx=768"},
		{"longa", Config{Janela: JanelaRecomendada}, 30 * time.Second, "audio_ctx=0"},
		{"ajustes do decodificador", Config{Janela: JanelaRecomendada, Decodificacao: Decodificacao{EntropyThold: 2.8, TemperatureInc: 0.4}},
			30 * time.Second, "audio_ctx=0 entropy_thold=2.8 temperature_inc=0.4"},
		{"só um ajuste", Config{Decodificacao: Decodificacao{TemperatureInc: 0.4}}, time.Second, "temperature_inc=0.4"},
		{"prompt fixo", Config{Janela: JanelaRecomendada, Decodificacao: Decodificacao{PromptFixo: true}},
			30 * time.Second, "audio_ctx=0 carry_initial_prompt=true"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			if got := junta(camposDeDecodificacao(c.cfg, c.d)); got != c.want {
				t.Errorf("campos = %q, want %q", got, c.want)
			}
		})
	}
}

// TestServerArgs_JanelaPorRequisicaoNaoVaiNaLinhaDeComando: com a escolha
// por requisição, o servidor sobe SEM -ac, e quem esquecer o campo cai na
// janela inteira, a segura.
func TestServerArgs_JanelaPorRequisicaoNaoVaiNaLinhaDeComando(t *testing.T) {
	linha := strings.Join(serverArgs(Config{ModelPath: "m.bin", Janela: JanelaRecomendada}, "127.0.0.1", 1, caminhoDeTeste), " ")
	if strings.Contains(linha, "-ac") || strings.Contains(linha, "768") {
		t.Errorf("argv = %q: a janela por requisição foi parar na linha de comando", linha)
	}
}

func providerComConfig(t *testing.T, cfg Config, proc serverProcess) *Provider {
	t.Helper()
	cfg.ServerPath, cfg.ModelPath, cfg.Language = "x", "y", "pt"
	p := New(cfg, "small-q4_0")
	p.novoProcesso = func(Config) serverProcess { return proc }
	return p
}

// TestTranscribe_AudioCtxPelaDuracao confere o que de fato vai para a rede:
// 768 na fala curta, 0 (janela inteira) no ditado longo, e nada quando a
// escolha não está ligada — o caminho antigo, byte a byte.
func TestTranscribe_AudioCtxPelaDuracao(t *testing.T) {
	for _, c := range []struct {
		nome     string
		janela   JanelaPorDuracao
		amostras int
		want     []string // nil: o campo não pode ir
	}{
		{"fala curta", JanelaRecomendada, 3 * 16000, []string{"768"}},
		{"ditado longo", JanelaRecomendada, 37 * 16000, []string{"0"}},
		{"sem a escolha", JanelaPorDuracao{}, 37 * 16000, nil},
	} {
		t.Run(c.nome, func(t *testing.T) {
			var campos map[string][]string
			srv := servidorQueCapturaCampos(t, "ok", &campos)
			p := providerComConfig(t, Config{Janela: c.janela}, &procFalso{baseURL: srv.URL})

			if _, err := p.Transcribe(context.Background(), audioDeTeste(c.amostras),
				asr.ASROptions{Language: "pt-BR", Vocabulary: "DCTFWeb"}); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			got, veio := campos["audio_ctx"]
			if c.want == nil {
				if veio {
					t.Errorf("audio_ctx = %q, mas a escolha não está ligada", got)
				}
			} else if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("audio_ctx = %q, want %q", got, c.want)
			}
			// O vocabulário continua indo, junto.
			if v := campos["prompt"]; len(v) != 1 || v[0] != "DCTFWeb" {
				t.Errorf("prompt = %q: o vocabulário se perdeu", v)
			}
		})
	}
}

// TestTranscribe_DecodificacaoVaiNosCampos: os ajustes que o benchmark mede
// chegam ao servidor com os nomes que o whisper-server b5130 lê
// (server.cpp:518, :582).
func TestTranscribe_DecodificacaoVaiNosCampos(t *testing.T) {
	var campos map[string][]string
	srv := servidorQueCapturaCampos(t, "ok", &campos)
	cfg := Config{Decodificacao: Decodificacao{EntropyThold: 2.8, TemperatureInc: 0.4}}
	p := providerComConfig(t, cfg, &procFalso{baseURL: srv.URL})

	if _, err := p.Transcribe(context.Background(), audioDeTeste(16000), asr.ASROptions{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if v := campos["entropy_thold"]; len(v) != 1 || v[0] != "2.8" {
		t.Errorf("entropy_thold = %q, want 2.8", v)
	}
	if v := campos["temperature_inc"]; len(v) != 1 || v[0] != "0.4" {
		t.Errorf("temperature_inc = %q, want 0.4", v)
	}
	if _, veio := campos["audio_ctx"]; veio {
		t.Error("audio_ctx foi sem a escolha ligada")
	}
}

// TestTranscribe_ReenvioLevaOsMesmosCampos: quando o servidor morre e o
// provider o sobe de novo, a segunda requisição leva a mesma janela.
func TestTranscribe_ReenvioLevaOsMesmosCampos(t *testing.T) {
	proc := &procFalso{}
	var mu sync.Mutex
	var recebidos [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		mu.Lock()
		recebidos = append(recebidos, r.MultipartForm.Value["audio_ctx"])
		primeira := len(recebidos) == 1
		mu.Unlock()
		if primeira {
			// Morre no meio, como em TestTranscribe_ServidorMorreNoMeioDaRequisicao.
			proc.vivo.Store(false)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		fmt.Fprint(w, `{"text":"ok"}`)
	}))
	t.Cleanup(srv.Close)
	proc.baseURL = srv.URL
	p := providerComConfig(t, Config{Janela: JanelaRecomendada}, proc)

	if _, err := p.Transcribe(context.Background(), audioDeTeste(30*16000), asr.ASROptions{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recebidos) != 2 {
		t.Fatalf("requisições = %d, want 2 (a que morreu e o reenvio)", len(recebidos))
	}
	for i, v := range recebidos {
		if len(v) != 1 || v[0] != "0" {
			t.Errorf("audio_ctx na requisição %d = %q, want 0", i+1, v)
		}
	}
}

func TestDuracaoDoAudio(t *testing.T) {
	if got := duracaoDoAudio(make([]int16, 24000), 16000); got != 1500*time.Millisecond {
		t.Errorf("24000 amostras a 16 kHz = %v, want 1,5 s", got)
	}
	if got := duracaoDoAudio(make([]int16, 100), 0); got != 0 {
		t.Errorf("taxa zero = %v, want 0", got)
	}
}
