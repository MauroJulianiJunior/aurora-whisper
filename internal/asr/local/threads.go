package local

import "runtime"

// tetoDeThreads é onde a curva medida parou de melhorar.
//
// Medido no Dell G15 (i7-13650HX, 20 núcleos lógicos), `small` em q4_0 com
// audio-ctx 750, mesma fala de 1,62 s:
//
//	-t 4     1120 ms
//	-t 6     1006 ms
//	-t 8      951 ms
//	-t 12     876 ms   <- melhor
//	-t 16     890 ms   <- já piora
//
// Acima de 12 a disputa por cache e o custo de sincronizar entre threads
// comem o ganho de paralelismo.
const tetoDeThreads = 12

// ThreadsRecomendadas escolhe quantas threads pedir ao servidor de ASR.
//
// # Por que isto não é uma constante
//
// O valor 12 foi afinado numa máquina de 20 núcleos lógicos. Numa de 4,
// pedir 12 não acelera nada: cria disputa por núcleos que não existem. E o
// default do próprio whisper-server é 4, que numa máquina grande deixa
// metade da CPU parada — foi o que fez a primeira medição deste projeto dar
// 4,11 s e me levar a concluir, errado, que o caminho CPU não alcançava a
// meta.
//
// # O limite honesto desta função
//
// A curva acima veio de UMA máquina. `min(núcleos, 12)` reproduz o ótimo
// medido lá e degrada de forma previsível para baixo — numa máquina de 8
// núcleos usa os 8, que é o que a parte crescente da curva sugere. Mas isso
// é EXTRAPOLAÇÃO, não medição: ninguém rodou a varredura num laptop de 4
// núcleos, e é possível que lá o ótimo seja menor que o total.
//
// Quem for medir de novo: a varredura está em
// docs/benchmarks/asr-2026-09-18-cpu-otimizado.md, e repeti-la custa
// minutos.
func ThreadsRecomendadas() int {
	return threadsPara(runtime.NumCPU())
}

// threadsPara é a regra pura, separada de runtime.NumCPU para ser testável
// com qualquer contagem de núcleos — inclusive as que esta máquina não tem.
func threadsPara(nucleos int) int {
	if nucleos < 1 {
		// NumCPU nunca devolve menos que 1, mas quem chama threadsPara
		// direto pode passar qualquer coisa, e uma contagem inválida não
		// pode virar "zero threads".
		return 1
	}
	if nucleos > tetoDeThreads {
		return tetoDeThreads
	}
	return nucleos
}
