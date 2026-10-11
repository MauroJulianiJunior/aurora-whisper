package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
)

// asrDeMentira faz o papel do whisper-server: responde por duração do
// áudio, guarda a ordem das chamadas e quantas vezes foi fechado. Nenhum
// servidor sobe.
type asrDeMentira struct {
	cfg       local.Config
	chamadas  []time.Duration // duração de cada áudio pedido, em ordem
	vocab     []string
	fechou    int
	respostas map[time.Duration]string
	tempo     time.Duration
	falhar    error
}

func (a *asrDeMentira) Transcribe(_ context.Context, buf asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	d := time.Duration(len(buf.PCM)) * time.Second / time.Duration(buf.SampleRateHz)
	a.chamadas = append(a.chamadas, d)
	a.vocab = append(a.vocab, opts.Vocabulary)
	if a.falhar != nil {
		return asr.ASRResult{}, a.falhar
	}
	texto, ok := a.respostas[d]
	if !ok {
		texto = "qualquer coisa"
	}
	return asr.ASRResult{Text: texto, ProcessingTime: a.tempo}, nil
}

func (a *asrDeMentira) Close() error { a.fechou++; return nil }

// abridorDeMentira devolve um asrDeMentira novo por variante, como o
// servidor recém-subido de verdade, e guarda todos.
type abridorDeMentira struct {
	abertos   []*asrDeMentira
	respostas map[time.Duration]string
	tempo     time.Duration
	falhaAo   func(local.Config) error
}

func (m *abridorDeMentira) abrir(cfg local.Config) (transcritor, error) {
	if m.falhaAo != nil {
		if err := m.falhaAo(cfg); err != nil {
			return nil, err
		}
	}
	a := &asrDeMentira{cfg: cfg, respostas: m.respostas, tempo: m.tempo}
	m.abertos = append(m.abertos, a)
	return a, nil
}

func TestMedir_CadaVarianteNumServidorProprioComAConfiguracaoDela(t *testing.T) {
	m := &abridorDeMentira{}
	longo := amostraSintetica("longo", "texto", 30, 1)
	p := plano{base: local.Config{ServerPath: "s", ModelPath: "m", Threads: 12}, variantes: variantesLongas, audios: []trecho{longo}}

	rs := medir(context.Background(), p, m.abrir, &bytes.Buffer{})

	if len(m.abertos) != len(variantesLongas) {
		t.Fatalf("%d servidores subidos, quero um por variante (%d)", len(m.abertos), len(variantesLongas))
	}
	quero := map[string]func(local.Config) bool{
		"ac750":                   func(c local.Config) bool { return c.AudioCtx == 750 && c.Janela == (local.JanelaPorDuracao{}) },
		"por-duracao":             func(c local.Config) bool { return c.AudioCtx == 0 && c.Janela == local.JanelaRecomendada },
		"por-duracao-intercalada": func(c local.Config) bool { return c.Janela == local.JanelaRecomendada && !c.SemFlashAttention },
		"nfa-por-duracao": func(c local.Config) bool {
			return c.AudioCtx == 0 && c.Janela == local.JanelaRecomendada && c.SemFlashAttention
		},
		"nfa-por-duracao-intercalada": func(c local.Config) bool {
			return c.Janela == local.JanelaRecomendada && c.SemFlashAttention
		},
		"entropia-2.8": func(c local.Config) bool {
			return c.Decodificacao.EntropyThold == 2.8 && c.Janela == local.JanelaRecomendada
		},
		"temperatura-0.4": func(c local.Config) bool {
			return c.Decodificacao.TemperatureInc == 0.4 && c.Janela == local.JanelaRecomendada
		},
		"prompt-fixo": func(c local.Config) bool {
			return c.Decodificacao == (local.Decodificacao{PromptFixo: true}) && c.Janela == local.JanelaRecomendada
		},
	}
	for i, v := range variantesLongas {
		// Só as duas primeiras rodam com o servidor só de áudio longo; as
		// outras se comparam com a intercalada, e têm de ser intercaladas
		// também.
		if quer := v.nome != "ac750" && v.nome != "por-duracao" && v.nome != "nfa-por-duracao"; v.intercalar != quer {
			t.Errorf("%s: intercalar = %v, quero %v", v.nome, v.intercalar, quer)
		}
		cfg := m.abertos[i].cfg
		if ok := quero[v.nome]; ok == nil || !ok(cfg) {
			t.Errorf("%s subiu com %+v", v.nome, cfg)
		}
		if cfg.ServerPath != "s" || cfg.ModelPath != "m" || cfg.Threads != 12 {
			t.Errorf("%s perdeu a base: %+v", v.nome, cfg)
		}
		if m.abertos[i].fechou != 1 {
			t.Errorf("%s: o servidor foi fechado %d vez(es), quero 1", v.nome, m.abertos[i].fechou)
		}
	}
	if len(rs) != len(variantesLongas) {
		t.Errorf("%d resultados, quero um por variante", len(rs))
	}
	// A base não pode ter sido alterada por uma variante para a seguinte.
	if p.base.AudioCtx != 0 || p.base.Janela != (local.JanelaPorDuracao{}) {
		t.Errorf("a base mudou: %+v", p.base)
	}
}

func TestMedir_IntercaladaMandaAFalaCurtaAntesDeCadaLongo(t *testing.T) {
	m := &abridorDeMentira{}
	curta := amostraSintetica("curta", "c", 3, 1)
	p := plano{
		variantes:   []variante{variantesLongas[0], variantesLongas[2]}, // ac750 e por-duracao-intercalada
		audios:      []trecho{amostraSintetica("l1", "x", 30, 1), amostraSintetica("l2", "y", 60, 1)},
		curta:       &curta,
		vocabulario: "DCTFWeb",
	}
	medir(context.Background(), p, m.abrir, &bytes.Buffer{})

	if got := m.abertos[0].chamadas; len(got) != 2 || got[0] != 30*time.Second || got[1] != 60*time.Second {
		t.Errorf("ac750 (sem intercalar) pediu %v", got)
	}
	want := []time.Duration{3 * time.Second, 30 * time.Second, 3 * time.Second, 60 * time.Second}
	if got := m.abertos[1].chamadas; len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("a intercalada pediu %v, quero %v", got, want)
	}
	for _, a := range m.abertos {
		for _, v := range a.vocab {
			if v != "DCTFWeb" {
				t.Errorf("o vocabulário não foi junto: %q", v)
			}
		}
	}
}

func TestMedir_ServidorQueNaoSobeViraFalhaEmCadaAudio(t *testing.T) {
	m := &abridorDeMentira{falhaAo: func(c local.Config) error {
		if c.AudioCtx == 750 {
			return errors.New("não subiu")
		}
		return nil
	}}
	p := plano{variantes: []variante{variantesLongas[0], variantesLongas[1]},
		audios: []trecho{amostraSintetica("l1", "x", 30, 1), amostraSintetica("l2", "y", 60, 1)}}
	var progresso bytes.Buffer
	rs := medir(context.Background(), p, m.abrir, &progresso)

	falhas := 0
	for _, r := range rs {
		if r.variante == "ac750" {
			if r.err == nil {
				t.Errorf("%s: sem erro com o servidor fora", r.audio)
			}
			falhas++
		} else if r.err != nil {
			t.Errorf("a variante seguinte falhou junto: %v", r.err)
		}
	}
	if falhas != 2 || len(rs) != 4 {
		t.Errorf("%d resultados, %d falhas; quero 4 e 2", len(rs), falhas)
	}
	if !strings.Contains(progresso.String(), "não subiu") {
		t.Errorf("o progresso não disse que o servidor não subiu:\n%s", progresso.String())
	}
}

// TestMedirUm_LacoEWER: o texto com laço é contado, e o WER depois da
// guarda é o do texto sem o laço.
func TestMedirUm_LacoEWER(t *testing.T) {
	ref := "precisamos fechar a folha de pagamento hoje e depois conferir os impostos"
	laco := "precisamos fechar a folha de pagamento hoje" + strings.Repeat(" precisamos fechar a folha de pagamento hoje", 5)
	a := &asrDeMentira{respostas: map[time.Duration]string{30 * time.Second: laco}, tempo: 31 * time.Second}
	r := medirUm(context.Background(), a, "x", local.Config{Janela: local.JanelaRecomendada}, amostraSintetica("l", ref, 30, 1), "", time.Minute)

	if r.err != nil || !r.werOK {
		t.Fatalf("r = %+v", r)
	}
	if r.lacos != 1 || r.palavrasRemovidas != 35 {
		t.Errorf("laços = %d, palavras tiradas = %d; quero 1 e 35", r.lacos, r.palavrasRemovidas)
	}
	if !(r.werGuarda < r.wer) {
		t.Errorf("WER com a guarda %.3f não é menor que sem %.3f", r.werGuarda, r.wer)
	}
	if r.audioCtx != "0" {
		t.Errorf("audio_ctx = %q, quero 0 para 30 s", r.audioCtx)
	}
	// 30 s de áudio têm 27,5 s de prazo no Agent; 31 s estouraria.
	if !r.estouraPrazo() {
		t.Error("31 s de ASR para 30 s de áudio tinham de estourar o prazo do Agent")
	}
	r.tempo = 10 * time.Second
	if r.estouraPrazo() {
		t.Error("10 s cabem no prazo de 30 s de áudio")
	}
}

func TestAudioCtxDe(t *testing.T) {
	for _, c := range []struct {
		cfg  local.Config
		d    time.Duration
		want string
	}{
		{local.Config{AudioCtx: 750}, time.Minute, "750 (-ac)"},
		{local.Config{}, time.Minute, "0 (padrão)"},
		{local.Config{Janela: local.JanelaRecomendada}, 5 * time.Second, "768"},
		{local.Config{Janela: local.JanelaRecomendada, AudioCtx: 750}, time.Minute, "0"},
	} {
		if got := audioCtxDe(c.cfg, c.d); got != c.want {
			t.Errorf("audioCtxDe(%+v, %v) = %q, want %q", c.cfg, c.d, got, c.want)
		}
	}
}

func TestEscolherVariantes(t *testing.T) {
	if vs, err := escolherVariantes(variantesLongas, nil); err != nil || len(vs) != len(variantesLongas) {
		t.Errorf("vazio = %d variantes, %v; quero todas", len(vs), err)
	}
	vs, err := escolherVariantes(variantesLongas, []string{"ac750", "por-duracao-intercalada"})
	if err != nil || len(vs) != 2 || vs[0].nome != "ac750" || !vs[1].intercalar {
		t.Errorf("escolha = %+v, %v", vs, err)
	}
	if _, err := escolherVariantes(variantesLongas, []string{"ac-750"}); err == nil {
		t.Error("um nome com erro de digitação foi aceito")
	}
}

func TestDivergencias(t *testing.T) {
	rs := []resultado{
		{variante: "por-duracao", audio: "a", texto: "igual"},
		{variante: "por-duracao-intercalada", audio: "a", texto: "igual"},
		{variante: "por-duracao", audio: "b", texto: "um"},
		{variante: "por-duracao-intercalada", audio: "b", texto: "outro"},
		{variante: "por-duracao", audio: "c", texto: "x"},
		{variante: "por-duracao-intercalada", audio: "c", err: errors.New("falhou")},
		{variante: "ac750", audio: "a", texto: "não entra"},
	}
	if i, d, a := divergencias(rs, "por-duracao", "por-duracao-intercalada"); i != 1 || d != 1 || a != 1 {
		t.Errorf("divergencias = %d iguais, %d diferentes, %d ausentes; quero 1, 1, 1", i, d, a)
	}
}
