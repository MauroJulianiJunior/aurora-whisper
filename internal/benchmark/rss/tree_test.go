package rss

import (
	"testing"
	"time"
)

// mapCreatedAt e mapSample são dublês síncronos de processCreatedAtFunc e
// sampleFunc sobre um mapa fixo — exercitam sumTree sem nenhuma chamada
// de sistema, em qualquer SO.
func mapCreatedAt(m map[int]int64) processCreatedAtFunc {
	return func(pid int) (int64, bool) {
		v, ok := m[pid]
		return v, ok
	}
}

func mapSample(m map[int]int64) sampleFunc {
	return func(pid int) (int64, bool) {
		v, ok := m[pid]
		return v, ok
	}
}

// TestSumTree_SingleNode prova a garantia que candidate.startRSSWatcher
// depende dela para o modo cold: um processo sem filhos tem árvore de um
// nó só, e a soma bate exatamente com a amostra desse nó — SampleTree não
// muda o valor medido para um candidato cold, só o cobre também quando há
// filhos.
func TestSumTree_SingleNode(t *testing.T) {
	created := mapCreatedAt(map[int]int64{1: 100})
	sample := mapSample(map[int]int64{1: 5000})

	got, ok := sumTree(1, map[int][]int{}, created, sample)
	if !ok {
		t.Fatal("sumTree: want ok == true")
	}
	if got != 5000 {
		t.Fatalf("sumTree = %d, want 5000 (igual a Sample do único nó)", got)
	}
}

// TestSumTree_SimpleTree soma uma raiz com dois filhos diretos.
func TestSumTree_SimpleTree(t *testing.T) {
	created := mapCreatedAt(map[int]int64{1: 100, 2: 200, 3: 200})
	sample := mapSample(map[int]int64{1: 1000, 2: 2000, 3: 3000})
	children := map[int][]int{1: {2, 3}}

	got, ok := sumTree(1, children, created, sample)
	if !ok {
		t.Fatal("sumTree: want ok == true")
	}
	if want := int64(1000 + 2000 + 3000); got != want {
		t.Fatalf("sumTree = %d, want %d", got, want)
	}
}

// TestSumTree_Depth3 soma uma cadeia raiz → filho → neto → bisneto.
func TestSumTree_Depth3(t *testing.T) {
	created := mapCreatedAt(map[int]int64{1: 100, 2: 200, 3: 300, 4: 400})
	sample := mapSample(map[int]int64{1: 10, 2: 20, 3: 30, 4: 40})
	children := map[int][]int{
		1: {2},
		2: {3},
		3: {4},
	}

	got, ok := sumTree(1, children, created, sample)
	if !ok {
		t.Fatal("sumTree: want ok == true")
	}
	if want := int64(10 + 20 + 30 + 40); got != want {
		t.Fatalf("sumTree = %d, want %d", got, want)
	}
}

// TestSumTree_CycleDoesNotHang usa um mapa children corrompido, com um
// nó (3) listado como filho de outro nó (2) que já é seu descendente —
// fechando um laço 1→2→3→2. Sem a proteção de visited, isso recursaria
// para sempre. A idade de todos os nós é igual (100), então a checagem
// de "filho mais velho" (reuso de PID) não é o que impede o laço aqui —
// é visited.
func TestSumTree_CycleDoesNotHang(t *testing.T) {
	created := mapCreatedAt(map[int]int64{1: 100, 2: 100, 3: 100})
	sample := mapSample(map[int]int64{1: 1, 2: 2, 3: 3})
	children := map[int][]int{
		1: {2},
		2: {3},
		3: {2}, // laço: 3 aponta de volta para 2, já visitado
	}

	done := make(chan struct{})
	var got int64
	var ok bool
	go func() {
		got, ok = sumTree(1, children, created, sample)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sumTree: não retornou — provável recursão infinita num ciclo")
	}

	if !ok {
		t.Fatal("sumTree: want ok == true (ciclo não é uma falha, só é ignorado)")
	}
	if want := int64(1 + 2 + 3); got != want {
		t.Fatalf("sumTree = %d, want %d (cada PID contado uma vez só)", got, want)
	}
}

// TestSumTree_MissingRootPID cobre o processo já encerrado (ou PID que
// nunca existiu): createdAt falha para a própria raiz, e sumTree nunca
// inventa um total — devolve ok == false, o mesmo contrato de
// Sample/TestSample_InvalidPID.
func TestSumTree_MissingRootPID(t *testing.T) {
	created := mapCreatedAt(map[int]int64{}) // raiz ausente
	sample := mapSample(map[int]int64{1: 999})

	got, ok := sumTree(1, map[int][]int{}, created, sample)
	if ok {
		t.Fatal("sumTree: want ok == false para PID ausente")
	}
	if got != 0 {
		t.Fatalf("sumTree = %d, want 0 quando ok == false", got)
	}
}

// TestSumTree_OlderChildIsPIDReuseNotDescendant é o caso central de reuso
// de PID: o PPID de um processo aponta para a raiz, mas esse processo
// nasceu ANTES da raiz — não pode ser filho de verdade, só pode ser um
// PPID reciclado de um pai antigo já morto. sumTree precisa excluir esse
// nó da soma (e não descer nele), mantendo o irmão legítimo (mais novo
// que a raiz).
func TestSumTree_OlderChildIsPIDReuseNotDescendant(t *testing.T) {
	created := mapCreatedAt(map[int]int64{
		1: 100, // raiz
		2: 50,  // "filho" — mais velho que a raiz: PID reciclado, não é descendente
		3: 150, // filho legítimo — nasceu depois da raiz
	})
	sample := mapSample(map[int]int64{1: 100, 2: 999999, 3: 300})
	children := map[int][]int{1: {2, 3}}

	got, ok := sumTree(1, children, created, sample)
	if !ok {
		t.Fatal("sumTree: want ok == true")
	}
	if want := int64(100 + 300); got != want {
		t.Fatalf("sumTree = %d, want %d (PID 2 devia ter sido excluído por ser mais velho que a raiz)", got, want)
	}
}

// TestSumTree_PartialFailureOnDescendantFailsWhole documenta a decisão de
// falha parcial: se um descendente genuíno não pode ser amostrado (ex.:
// morreu entre o snapshot e a leitura, ou permissão negada), sumTree
// devolve (0, false) para a chamada inteira — nunca um total que já
// exclui silenciosamente esse descendente (ver doc de sumTree em tree.go).
func TestSumTree_PartialFailureOnDescendantFailsWhole(t *testing.T) {
	created := mapCreatedAt(map[int]int64{1: 100, 2: 200})
	sample := mapSample(map[int]int64{1: 100}) // PID 2 não tem entrada: sample(2) falha
	children := map[int][]int{1: {2}}

	got, ok := sumTree(1, children, created, sample)
	if ok {
		t.Fatal("sumTree: want ok == false quando um descendente não pôde ser amostrado")
	}
	if got != 0 {
		t.Fatalf("sumTree = %d, want 0 quando ok == false", got)
	}
}

// TestSumTree_PartialFailureCreatedAtOnDescendantFailsWhole é a mesma
// decisão do teste acima, mas para a falha em createdAt (não em sample):
// se não dá para validar a idade de uma aresta pai→filho, sumTree não
// pode nem decidir se o nó é genuíno — também falha a chamada inteira,
// não finge que o nó não existia.
func TestSumTree_PartialFailureCreatedAtOnDescendantFailsWhole(t *testing.T) {
	created := mapCreatedAt(map[int]int64{1: 100}) // PID 2 não tem createdAt
	sample := mapSample(map[int]int64{1: 100, 2: 5000})
	children := map[int][]int{1: {2}}

	got, ok := sumTree(1, children, created, sample)
	if ok {
		t.Fatal("sumTree: want ok == false quando createdAt falha para um descendente")
	}
	if got != 0 {
		t.Fatalf("sumTree = %d, want 0 quando ok == false", got)
	}
}
