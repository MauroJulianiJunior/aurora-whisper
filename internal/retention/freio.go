package retention

import "time"

// FreioDaRodadaApos é quanto tempo sem rodada do ciclo do expurgo dispara o
// freio dos 6 meses no PC (ADR-019, "Decisões da F10", D2): o Agent ficou
// parado mais que isso, ou o relógio do PC pulou para a frente.
const FreioDaRodadaApos = 7 * 24 * time.Hour

// FreioDaRodada diz se o freio dispara em agora, sabendo que a última rodada
// do ciclo do expurgo foi em rodadaEm (nil: nenhuma rodada gravada).
//
// Dispara só quando a última rodada foi há MAIS de FreioDaRodadaApos. Sem
// rodada gravada (a primeira da instalação, a depois do Desligar ou da pausa)
// nunca dispara: não há com o que comparar. Só o salto para a frente freia: o
// relógio que volta (agora antes da rodada) apaga menos, nunca mais cedo.
//
// Quem dispara o freio, e o que ele faz, é de quem chama: na lixeira (o teto
// da D3) adia por uma rodada a liberação pela janela; nos 6 meses, pausa a
// regra (tira o OK, e a pergunta volta).
//
// Pura: o relógio vem de fora, como em todo o pacote.
func FreioDaRodada(rodadaEm *time.Time, agora time.Time) bool {
	if rodadaEm == nil {
		return false
	}
	return agora.Sub(*rodadaEm) > FreioDaRodadaApos
}

// DiasParado é quantos dias inteiros se passaram entre a última rodada e
// agora (zero sem rodada, ou com o relógio que voltou): só para o log do
// freio.
func DiasParado(rodadaEm *time.Time, agora time.Time) int {
	if rodadaEm == nil || agora.Before(*rodadaEm) {
		return 0
	}
	return int(agora.Sub(*rodadaEm) / (24 * time.Hour))
}
