package main

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
)

// preRollLeitura é o pre-roll da gravação do dataset.
//
// A interação aqui é a oposta do ditado: a pessoa aperta ENTER e SÓ ENTÃO
// fala, lendo uma frase da tela. Quando este comando herdava o pre-roll de
// 2 s que o ditado usava na época, os 2 s viravam áudio de antes do ENTER
// — nas 46 primeiras gravações isso somou ~92 s dos 311 s do dataset,
// quase 30% de áudio que ninguém pediu. Infla o denominador do RTF (tempo
// de processamento ÷ duração do áudio), fazendo todo candidato parecer
// melhor do que é, e desloca as amostras das categorias de duração: as
// três frases de "1-3s" saíram com 4,5 a 5,2 s.
//
// 300 ms é a margem para quem começa a falar em cima do ENTER. Não é
// medição, é julgamento — se alguém cortar sílaba inicial, aumente. (Hoje
// coincide com audio.DefaultPreRoll; fica explícito para que uma mudança
// lá não mude o dataset sem ninguém ver.)
const preRollLeitura = 300 * time.Millisecond

// configDeGravacao é a captura que este comando pede.
//
// Captura CONTÍNUA, com pre-roll, pedida explicitamente: desde a ADR-015 o
// valor zero de audio.Config é o microfone só enquanto dita, que é o do
// Agent. Aqui o microfone fica aberto do começo ao fim da sessão de
// gravação, e o pre-roll antes do ENTER é o que se quer.
func configDeGravacao(maxRecording time.Duration) audio.Config {
	return audio.Config{
		SemprePronto: true,
		PreRoll:      preRollLeitura,
		MaxRecording: maxRecording,
	}
}
