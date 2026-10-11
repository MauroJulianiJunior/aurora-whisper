// Command asrlongo mede o ASR local em ditado LONGO, que o benchmark de
// 18-set-2026 nunca viu: o dataset tem no máximo 9,3 s, e o -ac 750 foi
// escolhido por ele (incidente de 01-out-2026, docs/defeito-ditado-longo.md).
//
// Monta áudios de 30, 60 e 90 s concatenando as amostras do dataset com
// pausas curtas, com a referência concatenada, e transcreve cada um com o
// mesmo provider do Agent (internal/asr/local), numa série de variantes:
// o -ac 750 antigo, a janela por duração de agora (com e sem uma fala curta
// antes, para ver se o K/V que sobra de uma requisição muda a seguinte) e
// os ajustes do decodificador que a leitura do whisper.cpp sugeriu. O
// relatório traz WER, tempo, razão tempo/áudio, laços de repetição achados
// pela guarda do Agent (internal/asr/repeticao) e quantos estourariam o
// prazo do Agent.
//
// Subcomandos:
//
//	asrlongo montar ...   grava os áudios longos numa pasta, para ouvir
//	asrlongo medir ...    sobe o whisper-server por variante e mede
//
// "medir" monta tudo em memória e só escreve o relatório, o JSON com os
// textos e o log do servidor. Usa a CPU inteira por vários minutos: rodar
// com a máquina livre (comando em docs/defeito-ditado-longo.md).
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, abrirServidor))
}
