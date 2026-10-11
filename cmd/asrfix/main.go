// Command asrfix recupera as gravações do dataset de voz do benchmark de
// ASR (testdata/asr-dataset/audio/*.wav) feitas enquanto um defeito, já
// corrigido, de layout da struct WAVEFORMATEXTENSIBLE fazia a captura
// WASAPI gravar o padrão de bits IEEE-754 de cada amostra em vez do
// áudio real — ver o comentário de pacote em
// internal/agent/audio/recover.go, que faz a recuperação em si.
//
// Para cada amostra do manifesto (internal/benchmark/dataset), asrfix lê
// o WAV original, aplica internal/agent/audio.RecoverBitPatternFloat32,
// e escreve o áudio recuperado em -out/<id>.wav — NUNCA no arquivo
// original. Recusa rodar se -out for o mesmo diretório dos originais.
//
// Este comando só lê e escreve arquivo: nenhum microfone, nenhum hook,
// nenhuma janela, nenhum estado global do sistema (compare com
// cmd/asrrec e cmd/agent, que abrem microfone de verdade e por isso
// nunca devem ser executados num teste automático nem por um agente).
//
// asrfix é uma ferramenta de uma vez só: quando o dataset for regravado
// com o Agent já corrigido, ele deixa de ter propósito e deve ser
// removido junto com RecoverBitPatternFloat32.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
