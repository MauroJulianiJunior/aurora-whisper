package local

import (
	"strconv"
	"time"
)

// JanelaPorDuracao escolhe, a cada requisição, a janela do encoder do
// whisper.cpp (o campo audio_ctx do whisper-server): a reduzida para fala
// curta, e a inteira acima de Limite.
//
// # Por que por requisição
//
// O -ac 750 na linha de comando do servidor valia para todo áudio. Ele foi
// medido em 18-set-2026 com o dataset de 46 amostras, a maior de 9,3 s
// (docs/benchmarks/asr-decisao-2026-09-18.md), e corta 40% do tempo ali.
// Com 750 o encoder só vê 15 s de áudio por vez (750 × 20 ms), e o resto
// do laço de janelas do whisper.cpp continua supondo janelas de 30 s
// (whisper.cpp b5130: o avanço padrão é 3000 quadros, :7212, :7523, :7878).
// Acima de 15 s a transcrição fica frágil: no incidente de 01-out-2026
// (docs/defeito-ditado-longo.md) um ditado de 59 s entrou em laço de
// repetição e perdeu o resto da fala. O whisper-server b5130 aceita
// audio_ctx por requisição (server.cpp:510-513; whisper.cpp:7091 o grava a
// cada chamada), então dá para manter o ganho na fala curta e dar a janela
// inteira à longa, sem reiniciar o servidor.
type JanelaPorDuracao struct {
	// Curta é o audio_ctx da fala curta. Zero desliga a escolha: o campo
	// não vai na requisição, e vale o -ac do servidor (Config.AudioCtx).
	Curta int
	// Limite é a maior duração de áudio que ainda usa Curta. Acima dele,
	// audio_ctx 0: a janela inteira de 30 s.
	Limite time.Duration
}

// JanelaRecomendada é a escolha do Agent: 768 até 12 s, janela inteira
// acima.
//
// 768, e não os 750 medidos, por dois motivos do código do b5130:
//
//   - com flash attention (ligada), o K/V é lido em tamanho arredondado
//     para múltiplos de 256 sem máscara (whisper.cpp:2173-2191 e
//     :2729-2744), e só n_ctx linhas são gravadas. Com 750 sobram 18 linhas
//     lidas e não gravadas por camada; quando o servidor alterna entre
//     janelas, essas linhas guardam o K/V da requisição anterior. 768 é
//     múltiplo de 256: nenhuma linha sobra, e a fala curta não depende do
//     ditado anterior;
//   - é a sugestão do autor do whisper.cpp para janela reduzida (issue
//     #137), pelo alinhamento.
//
// O custo é cerca de 2% a mais de encoder que 750. A janela de 768 cobre
// 15,36 s; o limite de 12 s deixa 3,36 s de folga, porque passar da janela
// por pouco já basta para o laço (#1951: 11 s de áudio com janela de
// 10 s). O validado de verdade é até 9,3 s (o
// maior áudio do dataset): de 9,3 a 12 s, e o próprio 768, esperam o
// benchmark de áudio longo (cmd/asrlongo).
//
// O que sobra: a janela inteira (1500) é arredondada para 1536, e depois de
// uma requisição de 768 as 36 linhas finais de 6 das 12 camadas do
// decodificador guardam o K/V daquela requisição. É o mesmo mecanismo, na
// requisição longa. O benchmark mede se muda o texto (a variante
// intercalada de cmd/asrlongo); se mudar, a saída é desligar a flash
// attention (-nfa, medido: custa 22%).
var JanelaRecomendada = JanelaPorDuracao{Curta: 768, Limite: 12 * time.Second}

// AudioCtx devolve o audio_ctx para um áudio de duração d, e se o campo deve
// ir na requisição. Sem Curta, não vai: vale o padrão do servidor.
func (j JanelaPorDuracao) AudioCtx(d time.Duration) (valor int, enviar bool) {
	if j.Curta <= 0 {
		return 0, false
	}
	if d <= j.Limite {
		return j.Curta, true
	}
	return 0, true
}

// Decodificacao são ajustes do decodificador do whisper.cpp mandados por
// requisição. Zero em um campo é "não mandar": vale o padrão do servidor.
//
// O Agent não usa nenhum por enquanto. Existem para o benchmark de áudio
// longo (cmd/asrlongo) medir as sugestões da leitura do b5130 antes de
// alguém ligá-las (docs/defeito-ditado-longo.md, "O que falta medir").
type Decodificacao struct {
	// EntropyThold é o limiar de entropia dos últimos 32 tokens abaixo do
	// qual a tentativa é rejeitada e refeita com temperatura maior (padrão
	// 2,4). 2,8 pegaria laços de até 16 tokens; 2,4 só até 11.
	EntropyThold float64
	// TemperatureInc é o passo do fallback de temperatura (padrão 0,2: até
	// 6 tentativas). 0,4 dá 3 tentativas e corta o pior caso de latência
	// pela metade. Desligar o fallback (0) não é oferecido aqui.
	TemperatureInc float64
	// PromptFixo manda carry_initial_prompt=true: o vocabulário (o prompt
	// inicial) entra no começo do contexto de TODA janela de 30 s, e não só
	// da primeira. Sem ele, no b5130, o prompt vai para o contexto que rola
	// (prompt_past1) e é empurrado para fora pelo texto já decodificado
	// depois de uns 223 tokens (whisper.cpp:7046, 7077-7081, 7246,
	// 7730-7740): da segunda ou terceira janela em diante, os termos do
	// dicionário deixam de enviesar o decodificador. O custo é o contexto
	// do texto anterior encolher pelo tamanho do vocabulário.
	PromptFixo bool
}

// campoForm é um campo de texto do multipart, na ordem em que vai.
type campoForm struct{ nome, valor string }

// camposDeDecodificacao são os campos que a requisição de um áudio de
// duração d leva além do arquivo e do vocabulário. Puro: é a regra inteira
// do que vai para o servidor, testável sem servidor.
func camposDeDecodificacao(cfg Config, d time.Duration) []campoForm {
	var campos []campoForm
	if ctx, ok := cfg.Janela.AudioCtx(d); ok {
		campos = append(campos, campoForm{"audio_ctx", strconv.Itoa(ctx)})
	}
	if v := cfg.Decodificacao.EntropyThold; v > 0 {
		campos = append(campos, campoForm{"entropy_thold", strconv.FormatFloat(v, 'f', -1, 64)})
	}
	if v := cfg.Decodificacao.TemperatureInc; v > 0 {
		campos = append(campos, campoForm{"temperature_inc", strconv.FormatFloat(v, 'f', -1, 64)})
	}
	if cfg.Decodificacao.PromptFixo {
		// server.cpp:570-573 do b5130 lê o campo com parse_str_to_bool.
		campos = append(campos, campoForm{"carry_initial_prompt", "true"})
	}
	return campos
}

// duracaoDoAudio é a duração das amostras. Taxa inválida dá zero (o
// encodeWAV já recusa esse áudio antes).
func duracaoDoAudio(pcm []int16, taxa int) time.Duration {
	if taxa <= 0 {
		return 0
	}
	return time.Duration(len(pcm)) * time.Second / time.Duration(taxa)
}
