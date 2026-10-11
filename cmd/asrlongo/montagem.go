package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// trecho é um áudio com a referência dele: uma amostra do dataset, ou um
// áudio longo montado com várias. Só RAM: a montagem nunca grava áudio, a
// não ser que o subcomando montar seja pedido (para ouvir).
type trecho struct {
	id         string
	referencia string
	taxa       int
	pcm        []int16
	// fontes são os IDs das amostras do dataset que o compõem, em ordem.
	// Vazio numa amostra do próprio dataset.
	fontes []string
	// alvo é a duração pedida na montagem; zero numa amostra do dataset.
	alvo time.Duration
}

func (t trecho) duracao() time.Duration {
	if t.taxa <= 0 {
		return 0
	}
	return time.Duration(len(t.pcm)) * time.Second / time.Duration(t.taxa)
}

// compor monta porAlvo áudios para cada duração em alvos, concatenando
// amostras do dataset em ordem, com pausa de silêncio entre elas, até
// passar do alvo. A referência é a das amostras, na mesma ordem, separadas
// por espaço.
//
// As amostras são percorridas por um cursor só, em ciclo, que continua de
// um áudio para o seguinte: os áudios de mesma duração não repetem a mesma
// fala, e o mesmo comando monta sempre os mesmos áudios (nada é sorteado).
//
// O silêncio entre as amostras é digital (zeros): a fala do dataset já vem
// sem as bordas de silêncio, e a pausa imita o respiro entre duas frases
// de um ditado longo.
func compor(amostras []trecho, alvos []time.Duration, porAlvo int, pausa time.Duration) ([]trecho, error) {
	if len(amostras) == 0 {
		return nil, errors.New("nenhuma amostra para montar")
	}
	if porAlvo < 1 {
		return nil, fmt.Errorf("por-duracao = %d: quero pelo menos 1", porAlvo)
	}
	if pausa < 0 {
		return nil, fmt.Errorf("pausa negativa: %v", pausa)
	}
	taxa := amostras[0].taxa
	for _, a := range amostras {
		if a.taxa != taxa {
			return nil, fmt.Errorf("%s tem %d Hz e %s tem %d Hz: o dataset normalizado tem uma taxa só", amostras[0].id, taxa, a.id, a.taxa)
		}
		if len(a.pcm) == 0 {
			return nil, fmt.Errorf("%s não tem áudio", a.id)
		}
	}
	if taxa <= 0 {
		return nil, fmt.Errorf("taxa inválida: %d", taxa)
	}
	silencio := make([]int16, int(int64(pausa)*int64(taxa)/int64(time.Second)))

	var longos []trecho
	cursor := 0
	for _, alvo := range alvos {
		if alvo <= 0 {
			return nil, fmt.Errorf("duração alvo inválida: %v", alvo)
		}
		for n := 1; n <= porAlvo; n++ {
			l := trecho{id: fmt.Sprintf("longo-%s-%d", rotuloDuracao(alvo), n), taxa: taxa, alvo: alvo}
			var refs []string
			for l.duracao() < alvo {
				a := amostras[cursor%len(amostras)]
				cursor++
				if len(l.pcm) > 0 {
					l.pcm = append(l.pcm, silencio...)
				}
				l.pcm = append(l.pcm, a.pcm...)
				refs = append(refs, strings.TrimSpace(a.referencia))
				l.fontes = append(l.fontes, a.id)
			}
			l.referencia = strings.Join(refs, " ")
			longos = append(longos, l)
		}
	}
	return longos, nil
}

// rotuloDuracao escreve 30 s como "30s" e 1m30s como "90s": o ID do áudio
// diz a duração alvo em segundos.
func rotuloDuracao(d time.Duration) string {
	return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
}

// lerDuracoes lê "30s,60s,90s".
func lerDuracoes(s string) ([]time.Duration, error) {
	var ds []time.Duration
	for _, parte := range strings.Split(s, ",") {
		parte = strings.TrimSpace(parte)
		if parte == "" {
			continue
		}
		d, err := time.ParseDuration(parte)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("duração inválida %q", parte)
		}
		ds = append(ds, d)
	}
	if len(ds) == 0 {
		return nil, errors.New("nenhuma duração")
	}
	return ds, nil
}
