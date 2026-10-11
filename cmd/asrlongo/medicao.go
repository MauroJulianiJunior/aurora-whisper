package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/repeticao"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// variante é uma configuração do servidor e do pedido a medir.
type variante struct {
	nome      string
	descricao string
	ajustar   func(*local.Config)
	// intercalar manda uma fala curta antes de cada áudio medido, como no
	// uso: com a janela por duração, a curta vai com 768 e a longa com a
	// janela inteira, e o servidor alterna entre as duas.
	intercalar bool
}

func janelaPorDuracao(c *local.Config) { c.Janela = local.JanelaRecomendada }

// variantesLongas são as do áudio longo. A primeira é o Agent até
// 01-out-2026; as outras, o de agora e o que a leitura do b5130 sugere medir
// antes de ligar (docs/defeito-ditado-longo.md).
var variantesLongas = []variante{
	{nome: "ac750", descricao: "-ac 750 na linha de comando (o Agent até 01-out-2026)",
		ajustar: func(c *local.Config) { c.AudioCtx = 750 }},
	{nome: "por-duracao", descricao: "audio_ctx por requisição (768 até 12 s, janela inteira acima), servidor só com áudio longo",
		ajustar: janelaPorDuracao},
	{nome: "por-duracao-intercalada", descricao: "o mesmo, com uma fala curta antes de cada áudio longo (como no uso)",
		ajustar: janelaPorDuracao, intercalar: true},
	{nome: "entropia-2.8", descricao: "por duração, intercalada, com entropy_thold 2,8 (padrão 2,4)",
		ajustar: func(c *local.Config) {
			janelaPorDuracao(c)
			c.Decodificacao.EntropyThold = 2.8
		}, intercalar: true},
	{nome: "temperatura-0.4", descricao: "por duração, intercalada, com temperature_inc 0,4 (padrão 0,2)",
		ajustar: func(c *local.Config) {
			janelaPorDuracao(c)
			c.Decodificacao.TemperatureInc = 0.4
		}, intercalar: true},
	{nome: "prompt-fixo", descricao: "por duração, intercalada, com carry_initial_prompt (o vocabulário em toda janela de 30 s)",
		ajustar: func(c *local.Config) {
			janelaPorDuracao(c)
			c.Decodificacao.PromptFixo = true
		}, intercalar: true},
	{nome: "nfa-por-duracao", descricao: "por duração, sem flash attention (-nfa), servidor só com áudio longo",
		ajustar: janelaPorDuracaoSemFA},
	{nome: "nfa-por-duracao-intercalada", descricao: "por duração, sem flash attention (-nfa), intercalada",
		ajustar: janelaPorDuracaoSemFA, intercalar: true},
}

func janelaPorDuracaoSemFA(c *local.Config) {
	janelaPorDuracao(c)
	c.SemFlashAttention = true
}

// variantesCurtas medem as amostras do próprio dataset: o 768 por
// requisição tem de manter o que o 750 medido dava na fala curta.
var variantesCurtas = []variante{
	{nome: "ac750", descricao: "-ac 750 na linha de comando (medido em 18-set-2026)",
		ajustar: func(c *local.Config) { c.AudioCtx = 750 }},
	{nome: "por-duracao", descricao: "audio_ctx 768 por requisição (toda amostra tem menos de 12 s)",
		ajustar: janelaPorDuracao},
	{nome: "nfa-por-duracao", descricao: "audio_ctx 768 por requisição, sem flash attention (-nfa)",
		ajustar: janelaPorDuracaoSemFA},
}

// escolherVariantes filtra por nome; vazio é todas. Nome desconhecido é
// erro, para um erro de digitação não virar medição incompleta.
func escolherVariantes(todas []variante, nomes []string) ([]variante, error) {
	if len(nomes) == 0 {
		return todas, nil
	}
	var vs []variante
	for _, n := range nomes {
		achou := false
		for _, v := range todas {
			if v.nome == n {
				vs = append(vs, v)
				achou = true
			}
		}
		if !achou {
			return nil, fmt.Errorf("variante desconhecida %q", n)
		}
	}
	return vs, nil
}

// transcritor é o que a medição precisa de um ASR: transcrever e fechar.
// Em produção, um *local.Provider de pé (abrirServidor); nos testes, um
// dublê, sem servidor nenhum.
type transcritor interface {
	Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error)
	Close() error
}

// abridor sobe um servidor com cfg e devolve o transcritor pronto.
type abridor func(cfg local.Config) (transcritor, error)

// resultado é a medição de um áudio numa variante.
type resultado struct {
	variante string
	audio    string
	duracao  time.Duration
	audioCtx string // o que foi na requisição, ou o -ac do servidor

	tempo  time.Duration // o que o provider mediu (ASRResult.ProcessingTime)
	parede time.Duration // a chamada inteira, de fora

	wer, werGuarda    float64 // contra a referência: o texto do ASR, e o texto depois da guarda
	werOK             bool
	lacos             int // laços que a guarda achou
	palavrasRemovidas int
	runas             int
	texto             string // o do ASR, para comparar variantes; não vai ao Markdown
	err               error
}

// estouraPrazo diz se o ASR levou mais que o prazo que o Agent dá à
// transcrição desse áudio (dictation.PrazoDeTranscricao).
func (r resultado) estouraPrazo() bool {
	return r.err == nil && r.tempo > dictation.PrazoDeTranscricao(r.duracao)
}

// audioCtxDe é o que a requisição de um áudio de duração d leva de janela.
func audioCtxDe(cfg local.Config, d time.Duration) string {
	if v, ok := cfg.Janela.AudioCtx(d); ok {
		return strconv.Itoa(v)
	}
	if cfg.AudioCtx > 0 {
		return strconv.Itoa(cfg.AudioCtx) + " (-ac)"
	}
	return "0 (padrão)"
}

// plano é o que medir faz: as variantes, os áudios, e o que vai junto.
type plano struct {
	base      local.Config
	variantes []variante
	audios    []trecho
	// curta é a fala que vai antes de cada áudio nas variantes
	// intercaladas. Nil desliga a intercalação.
	curta       *trecho
	vocabulario string
	// prazo é o teto de cada transcrição. Zero: 5 minutos.
	prazo time.Duration
}

// medir roda cada variante num servidor próprio, recém-subido, e
// transcreve cada áudio nele. Uma variante que não sobe vira falha em todos
// os áudios dela, e a medição segue para a próxima.
//
// progresso recebe uma linha por áudio, só com números: o benchmark roda
// por minutos, e quem espera precisa ver que anda.
func medir(ctx context.Context, p plano, abrir abridor, progresso io.Writer) []resultado {
	prazo := p.prazo
	if prazo <= 0 {
		prazo = 5 * time.Minute
	}
	var rs []resultado
	for _, v := range p.variantes {
		cfg := p.base
		v.ajustar(&cfg)
		tr, err := abrir(cfg)
		if err != nil {
			fmt.Fprintf(progresso, "%s: o servidor não subiu: %v\n", v.nome, err)
			for _, a := range p.audios {
				rs = append(rs, resultado{variante: v.nome, audio: a.id, duracao: a.duracao(), audioCtx: audioCtxDe(cfg, a.duracao()), err: err})
			}
			continue
		}
		for _, a := range p.audios {
			if v.intercalar && p.curta != nil {
				c, cancel := context.WithTimeout(ctx, prazo)
				_, err := tr.Transcribe(c, buffer(*p.curta), asr.ASROptions{Language: "pt-BR", Vocabulary: p.vocabulario})
				cancel()
				if err != nil {
					fmt.Fprintf(progresso, "%s: a fala curta antes de %s falhou: %v\n", v.nome, a.id, err)
				}
			}
			r := medirUm(ctx, tr, v.nome, cfg, a, p.vocabulario, prazo)
			rs = append(rs, r)
			if r.err != nil {
				fmt.Fprintf(progresso, "%s %s: falhou: %v\n", v.nome, a.id, r.err)
			} else {
				fmt.Fprintf(progresso, "%s %s: %.1f s de áudio em %.2f s, WER %.3f, laços %d\n",
					v.nome, a.id, r.duracao.Seconds(), r.tempo.Seconds(), r.wer, r.lacos)
			}
		}
		if err := tr.Close(); err != nil {
			fmt.Fprintf(progresso, "%s: fechando o servidor: %v\n", v.nome, err)
		}
	}
	return rs
}

func buffer(t trecho) asr.AudioBuffer { return asr.AudioBuffer{PCM: t.pcm, SampleRateHz: t.taxa} }

// medirUm transcreve um áudio e calcula as métricas.
func medirUm(ctx context.Context, tr transcritor, nome string, cfg local.Config, a trecho, vocab string, prazo time.Duration) resultado {
	r := resultado{variante: nome, audio: a.id, duracao: a.duracao(), audioCtx: audioCtxDe(cfg, a.duracao())}
	c, cancel := context.WithTimeout(ctx, prazo)
	defer cancel()
	inicio := time.Now()
	res, err := tr.Transcribe(c, buffer(a), asr.ASROptions{Language: "pt-BR", Vocabulary: vocab})
	r.parede = time.Since(inicio)
	if err != nil {
		r.err = err
		return r
	}
	r.tempo, r.texto = res.ProcessingTime, res.Text
	r.runas = len([]rune(res.Text))
	guarda := repeticao.Colapsar(res.Text)
	r.lacos, r.palavrasRemovidas = len(guarda.Colapsos), guarda.PalavrasRemovidas
	w, errW := metrics.WER(a.referencia, res.Text)
	wg, errG := metrics.WER(a.referencia, guarda.Texto)
	if errW == nil && errG == nil {
		r.wer, r.werGuarda, r.werOK = w, wg, true
	}
	return r
}

// abrirServidor é o abridor de verdade: sobe o whisper-server com cfg e
// espera ele aceitar requisição. É o mesmo provider do Agent.
func abrirServidor(nomeModelo string) abridor {
	return func(cfg local.Config) (transcritor, error) {
		p := local.New(cfg, nomeModelo)
		ctx, cancel := context.WithTimeout(context.Background(), local.DefaultReadyTimeout)
		defer cancel()
		if err := p.Warmup(ctx); err != nil {
			_ = p.Close()
			return nil, err
		}
		return p, nil
	}
}
