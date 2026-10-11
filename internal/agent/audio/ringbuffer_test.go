package audio

import (
	"reflect"
	"testing"
)

func TestRingBuffer_WriteWraps(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]int16{1, 2, 3, 4, 5, 6})

	if got, want := rb.Cursor(), int64(6); got != want {
		t.Fatalf("Cursor() = %d, want %d", got, want)
	}

	got := rb.Range(0, rb.Cursor())
	want := []int16{3, 4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() = %v, want %v (só a cauda deveria sobreviver ao wraparound)", got, want)
	}
}

func TestRingBuffer_ReadWithPreRoll(t *testing.T) {
	rb := NewRingBuffer(20)

	// Pre-roll: áudio que já estava rodando antes da marca.
	preRoll := []int16{1, 2, 3, 4, 5}
	rb.Write(preRoll)

	mark := rb.Cursor() // keydown acontece aqui

	live := []int16{10, 20, 30}
	rb.Write(live)

	// Stop pede pre-roll de até 5 amostras antes da marca.
	got := rb.Range(mark-5, rb.Cursor())
	want := append(append([]int16{}, preRoll...), live...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() = %v, want %v (pre-roll + ao vivo)", got, want)
	}
}

func TestRingBuffer_PreRollLongerThanRetained(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]int16{1, 2, 3, 4, 5, 6}) // cursor=6, só [3,4,5,6] restam

	mark := rb.Cursor()
	// Pede pre-roll de 100 amostras, mas só a capacidade (4) ainda existe.
	got := rb.Range(mark-100, rb.Cursor())
	want := []int16{3, 4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() = %v, want %v (recortado ao que ainda está retido, sem pânico)", got, want)
	}
}

func TestRingBuffer_Full(t *testing.T) {
	rb := NewRingBuffer(5)
	samples := []int16{1, 2, 3, 4, 5}
	rb.Write(samples)

	if got := rb.Cursor(); got != int64(len(samples)) {
		t.Fatalf("Cursor() = %d, want %d", got, len(samples))
	}
	got := rb.Range(0, rb.Cursor())
	if !reflect.DeepEqual(got, samples) {
		t.Fatalf("Range() = %v, want %v (buffer exatamente cheio, nada perdido)", got, samples)
	}

	// Uma amostra a mais deve empurrar a mais antiga para fora.
	rb.Write([]int16{6})
	got = rb.Range(0, rb.Cursor())
	want := []int16{2, 3, 4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() após overflow = %v, want %v", got, want)
	}
}

func TestRingBuffer_MarkAtCapacityBoundary(t *testing.T) {
	rb := NewRingBuffer(8)

	// Escreve exatamente a capacidade antes de marcar — a marca cai
	// justo no limite em que o buffer acabou de encher, sem ainda ter
	// dado a volta. É o caso mais fácil de acertar um off-by-one no
	// módulo do índice.
	rb.Write([]int16{1, 2, 3, 4, 5, 6, 7, 8})
	mark := rb.Cursor()
	if mark != 8 {
		t.Fatalf("mark = %d, want 8", mark)
	}

	// Consultado agora, antes de qualquer escrita nova, o pre-roll da
	// capacidade inteira deve devolver exatamente o que foi escrito, sem
	// perder nem deslocar nenhuma amostra.
	got := rb.Range(mark-8, rb.Cursor())
	want := []int16{1, 2, 3, 4, 5, 6, 7, 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() = %v, want %v (marca exatamente no limite, sem overflow ainda)", got, want)
	}

	// Duas amostras a mais empurram as duas mais antigas para fora — o
	// pre-roll pedido (8, a partir da marca original) precisa ser
	// recortado para o que ainda está retido.
	rb.Write([]int16{9, 10})
	got = rb.Range(mark-8, rb.Cursor())
	want = []int16{3, 4, 5, 6, 7, 8, 9, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() após overflow parcial = %v, want %v", got, want)
	}
}

func TestRingBuffer_EmptyRange(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]int16{1, 2})
	if got := rb.Range(2, 2); got != nil {
		t.Fatalf("Range(2,2) = %v, want nil (intervalo vazio)", got)
	}
	if got := rb.Range(5, 2); got != nil {
		t.Fatalf("Range(5,2) = %v, want nil (from > to)", got)
	}
}
