package win32

import "time"

// Este arquivo não tem build tag de propósito (ver docs/adr/ADR-007,
// "Pendência para o Sprint 1", e ADR-010, ponto 2 do sprint): é
// aritmética pura sobre inteiros e time.Time, sem nenhuma chamada de
// sistema, e por isso roda — e é testada — tanto em Windows quanto no
// runner Linux do CI.
//
// KBDLLHOOKSTRUCT.Time é o contador de ticks do Win32 (GetTickCount):
// milissegundos desde o boot, em uint32, que estoura (dá a volta) a
// cada 2^32 ms ≈ 49,7 dias. Não é hora de parede. Para virar
// time.Time, cada tick precisa ser ancorado contra um par de referência
// (tick de referência, wall time de referência) capturado uma única vez.

// TickReference ancora a conversão de ticks GetTickCount para
// time.Time. Deve ser capturada uma vez — tipicamente na instalação do
// hook de teclado — lendo o tick atual e time.Now() o mais próximo
// possível um do outro.
type TickReference struct {
	Tick uint32
	Wall time.Time
}

// NewTickReference constrói uma TickReference a partir de um tick e do
// wall time correspondente.
func NewTickReference(tick uint32, wall time.Time) TickReference {
	return TickReference{Tick: tick, Wall: wall}
}

// ToTime converte tick (por exemplo, KBDLLHOOKSTRUCT.Time) em time.Time,
// ancorado em ref.
//
// A subtração tick-ref.Tick é feita em aritmética uint32 (módulo 2^32,
// exatamente como o contador do Win32), e o resultado é reinterpretado
// como int32 antes de virar a duração. Essa reinterpretação é o que
// resolve o wraparound: ela extrai sempre o menor delta com sinal
// consistente com a aritmética modular, tanto para tick > ref.Tick
// (delta positivo comum) quanto para tick < ref.Tick — seja porque o
// evento é anterior à referência (delta negativo verdadeiro), seja
// porque o contador deu a volta entre a referência e o evento (delta
// positivo que "parece" negativo em uint32 puro).
//
// A conversão só é correta se |tick verdadeiro - ref.Tick| for menor
// que 2^31 ms (~24,85 dias) — metade do período de overflow. Isso é
// garantido em uso real porque a referência é recapturada a cada
// instalação do hook (o Agent não fica meses rodando sem reiniciar o
// hook), e a distância entre um KeyDown e sua referência mais recente é
// no máximo a duração do próprio processo do Agent entre reinícios.
func (ref TickReference) ToTime(tick uint32) time.Time {
	delta := int32(tick - ref.Tick)
	return ref.Wall.Add(time.Duration(delta) * time.Millisecond)
}
