package audio

import "time"

// O relógio das medições do microfone (ADR-015) conta em unidades de
// 100 ns do contador de performance do Windows (QPC) — a MESMA unidade que
// IAudioCaptureClient::GetBuffer escreve em pu64QPCPosition: o instante em
// que o dispositivo gravou o primeiro frame do pacote. É isso que permite
// subtrair o carimbo do pacote da marca do keydown sem converter nada.
//
// Em produção é agoraQPC (qpc_windows.go); fora do Windows, um relógio
// monotônico na mesma unidade (qpc_other.go). Nos testes, um relógio falso
// que o teste controla.

// qpcPara100ns converte uma leitura do contador de performance para
// unidades de 100 ns, pela conta que a documentação de GetBuffer descreve
// (contador × 10^7 ÷ frequência).
//
// Não multiplica antes de dividir: o contador passa de 10^14 depois de
// algumas semanas ligado, e 10^14 × 10^7 estoura int64 (9,2 × 10^18).
func qpcPara100ns(contador, frequencia int64) int64 {
	if frequencia <= 0 {
		return 0
	}
	const porSegundo = 10_000_000
	return contador/frequencia*porSegundo + contador%frequencia*porSegundo/frequencia
}

// duracaoDeTicks converte unidades de 100 ns em time.Duration.
func duracaoDeTicks(t int64) time.Duration { return time.Duration(t) * 100 }

// ticksDe converte time.Duration em unidades de 100 ns.
func ticksDe(d time.Duration) int64 { return int64(d / 100) }

// duracaoDeAmostras é quanto tempo n amostras duram na taxa dada.
func duracaoDeAmostras(n int64, taxa int) time.Duration {
	if taxa <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second / time.Duration(taxa)
}
