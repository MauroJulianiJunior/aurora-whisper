// Command asrtrim normaliza a margem de silêncio das gravações do
// dataset de voz do benchmark de ASR (testdata/asr-dataset/audio/*.wav):
// cada gravação tem silêncio de borda porque a pessoa aperta ENTER e só
// depois lê a frase da tela (medido: mediana 1,16 s de silêncio inicial,
// 0,25 s final, cerca de 25% do dataset em silêncio de borda).
//
// Isso enviesa a métrica primária do benchmark: RTF = tempo de
// processamento ÷ duração do áudio, e o harness usa dataset.Duration, que
// é a duração do ARQUIVO — com 25% de silêncio, todo candidato de ASR sai
// com RTF sistematicamente otimista. Também descasa as categorias de
// duração de docs/ai-spec/19-eval-plan.md (ex.: "1-3s") da duração de
// FALA real de cada amostra.
//
// Para cada amostra do manifesto (internal/benchmark/dataset), asrtrim lê
// o WAV original, localiza onde a fala começa e termina (trim.go), corta
// para deixar exatamente -margin de silêncio de cada lado — sem inventar
// silêncio onde já há menos que isso —, e escreve o resultado em
// -out/<id>.wav via dataset.WriteWAV — NUNCA no arquivo original. Recusa
// rodar se -out for o mesmo diretório dos originais (antes de escrever
// qualquer coisa), e recusa arquivo por arquivo quando a detecção de
// borda não é confiável o bastante para cortar sem risco de comer sílaba
// (ver decideTrim em trim.go, e o comentário de cmd/asrfix/trim.go, cujo
// padrão de decisão-com-trava este comando espelha deliberadamente).
//
// Este comando só lê e escreve arquivo: nenhum microfone, nenhum hook,
// nenhuma janela, nenhum estado global do sistema (compare com
// cmd/asrrec e cmd/agent, que abrem microfone de verdade e por isso nunca
// devem ser executados num teste automático nem por um agente).
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
