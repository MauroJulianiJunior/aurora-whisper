package audio

// RingBuffer é um buffer circular de amostras PCM int16, de capacidade
// fixa, alocado uma única vez (ADR-010, "Ring buffer de pre-roll"): a
// capacidade é escolhida pelo chamador como o pior caso do trecho que um
// Stop pode pedir — Config.capacitySamples, cerca de 3,95 MB a 16 kHz mono.
//
// Em vez de uma flag "já deu a volta", o buffer rastreia um cursor de
// escrita monotonicamente crescente (total de amostras já escritas desde
// sempre). É esse cursor que permite expressar "tudo a partir desta
// marca" mesmo atravessando um wraparound: MarkStart guarda Cursor() no
// instante da marca, e Stop lê Range(marca-preRoll, Cursor()) — pre-roll
// e áudio ao vivo numa única chamada, sem precisar de mapas separados por
// tentativa (ADR-007, seção 4).
//
// Não é seguro para uso concorrente — mesma regra de projeto do
// dictation.Machine (um único consumidor serializado).
type RingBuffer struct {
	buf    []int16
	cursor int64
}

// NewRingBuffer aloca um buffer que retém até capacity amostras. capacity
// menor que 1 é tratado como 1.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{buf: make([]int16, capacity)}
}

// Cap é a capacidade fixa, em amostras.
func (r *RingBuffer) Cap() int { return len(r.buf) }

// Cursor é o total de amostras já escritas desde a criação do buffer.
// Nunca reseta e é a base de todo cálculo de marca/janela em Range.
func (r *RingBuffer) Cursor() int64 { return r.cursor }

// Write anexa samples, sobrescrevendo a amostra mais antiga retida a cada
// nova escrita, uma vez que o buffer esteja cheio.
func (r *RingBuffer) Write(samples []int16) {
	n := int64(len(r.buf))
	if n == 0 {
		return
	}
	for _, s := range samples {
		r.buf[r.cursor%n] = s
		r.cursor++
	}
}

// Range devolve as amostras retidas no intervalo [from, to), da mais
// antiga para a mais nova, recortado ao que o buffer ainda guarda: to é
// limitado a Cursor(), e from é elevado até a amostra mais antiga ainda
// retida (Cursor()-Cap()). Pedir um from anterior a isso não é erro — é
// simplesmente pre-roll maior do que a capacidade permite, e o chamador
// recebe o que ainda existe, nunca dado obsoleto ou sobrescrito.
func (r *RingBuffer) Range(from, to int64) []int16 {
	if to > r.cursor {
		to = r.cursor
	}
	oldest := r.cursor - int64(len(r.buf))
	if oldest < 0 {
		oldest = 0
	}
	if from < oldest {
		from = oldest
	}
	if from >= to {
		return nil
	}

	n := int64(len(r.buf))
	out := make([]int16, to-from)
	for i := range out {
		out[i] = r.buf[(from+int64(i))%n]
	}
	return out
}
