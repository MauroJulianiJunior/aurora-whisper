package storage_test

import (
	"errors"
	"math/rand"
	"sort"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

func posicaoOuFalha(t *testing.T, antes, depois string) string {
	t.Helper()
	k, err := storage.PosicaoEntre(antes, depois)
	if err != nil {
		t.Fatalf("PosicaoEntre(%q, %q): %v", antes, depois, err)
	}
	if !storage.PosicaoValida(k) {
		t.Fatalf("PosicaoEntre(%q, %q) = %q, que não é uma posição válida", antes, depois, k)
	}
	if antes != "" && !(antes < k) {
		t.Fatalf("PosicaoEntre(%q, %q) = %q, que não é maior que a de antes", antes, depois, k)
	}
	if depois != "" && !(k < depois) {
		t.Fatalf("PosicaoEntre(%q, %q) = %q, que não é menor que a de depois", antes, depois, k)
	}
	return k
}

func TestPosicao_Valida(t *testing.T) {
	for _, ok := range []string{"V", "1", "0V", "zz", "a1b2", "Ab"} {
		if !storage.PosicaoValida(ok) {
			t.Errorf("%q deveria valer", ok)
		}
	}
	for _, ruim := range []string{"", "0", "V0", "a b", "é", "V-", "a\x00"} {
		if storage.PosicaoValida(ruim) {
			t.Errorf("%q não deveria valer", ruim)
		}
	}
	longa := make([]byte, storage.MaxPosicao+1)
	for i := range longa {
		longa[i] = 'V'
	}
	if storage.PosicaoValida(string(longa)) {
		t.Error("passou do teto e valeu")
	}
}

func TestPosicao_ListaVaziaEOsDoisLados(t *testing.T) {
	primeira := posicaoOuFalha(t, "", "")
	if primeira != "V" {
		t.Errorf("a primeira chave é a do meio, deu %q", primeira)
	}
	// Antes da primeira e depois da última.
	antes := posicaoOuFalha(t, "", primeira)
	depois := posicaoOuFalha(t, primeira, "")
	if !(antes < primeira && primeira < depois) {
		t.Errorf("fora de ordem: %q %q %q", antes, primeira, depois)
	}
}

func TestPosicao_ComVizinhasDeTodoTipo(t *testing.T) {
	// Pares de chaves, ordenados, de formato variado: dígitos vizinhos, prefixo
	// comum, uma curta contra uma longa, zeros no meio, o último dígito.
	chaves := []string{"1", "10V", "11", "1z", "2", "2V", "V", "V1", "V1z", "VV", "Vz", "W", "z", "zV", "zz", "zzzV"}
	sort.Strings(chaves)
	for i := range chaves {
		for j := i + 1; j < len(chaves); j++ {
			posicaoOuFalha(t, chaves[i], chaves[j])
		}
		posicaoOuFalha(t, "", chaves[i])
		posicaoOuFalha(t, chaves[i], "")
	}
}

func TestPosicao_RecusaOQueNaoVale(t *testing.T) {
	if _, err := storage.PosicaoEntre("V", "V"); !errors.Is(err, storage.ErrPosicaoInvertida) {
		t.Errorf("iguais: %v", err)
	}
	if _, err := storage.PosicaoEntre("W", "V"); !errors.Is(err, storage.ErrPosicaoInvertida) {
		t.Errorf("trocadas: %v", err)
	}
	for _, ruim := range [][2]string{{"V0", "W"}, {"V", "W0"}, {"é", "W"}} {
		if _, err := storage.PosicaoEntre(ruim[0], ruim[1]); !errors.Is(err, storage.ErrPosicaoInvalida) {
			t.Errorf("%q,%q: %v", ruim[0], ruim[1], err)
		}
	}
}

// Muitas notas no mesmo vão: a mesma ponta de cima ("antes da primeira"), de
// baixo, e sempre entre as mesmas duas. Nenhuma chave se repete, a ordem se
// mantém e a chave cresce devagar.
func TestPosicao_MuitasNoMesmoVao(t *testing.T) {
	t.Run("sempre no topo", func(t *testing.T) {
		primeira := posicaoOuFalha(t, "", "")
		todas := []string{primeira}
		for i := 0; i < 2000; i++ {
			todas = append([]string{posicaoOuFalha(t, "", todas[0])}, todas...)
		}
		if !sort.StringsAreSorted(todas) {
			t.Fatal("as chaves do topo saíram fora de ordem")
		}
		// 2000 notas novas: ~30 por caractere, então algo em torno de 70.
		if n := len(todas[0]); n > 80 {
			t.Errorf("a chave do topo tem %d caracteres depois de 2000 notas novas", n)
		}
	})
	t.Run("sempre no fim", func(t *testing.T) {
		todas := []string{posicaoOuFalha(t, "", "")}
		for i := 0; i < 2000; i++ {
			todas = append(todas, posicaoOuFalha(t, todas[len(todas)-1], ""))
		}
		if !sort.StringsAreSorted(todas) {
			t.Fatal("as chaves do fim saíram fora de ordem")
		}
		if n := len(todas[len(todas)-1]); n > 80 {
			t.Errorf("a chave do fim tem %d caracteres", n)
		}
	})
	t.Run("sempre entre as mesmas duas", func(t *testing.T) {
		a, b := "V", "W"
		vistas := map[string]bool{a: true, b: true}
		// Cada nova fica logo abaixo da anterior: o vão de cima encolhe pela metade
		// a cada vez, o caso mais duro do ponto médio.
		for i := 0; i < 500; i++ {
			k := posicaoOuFalha(t, a, b)
			if vistas[k] {
				t.Fatalf("a chave %q se repetiu na rodada %d", k, i)
			}
			vistas[k] = true
			b = k
		}
		if len(b) > 130 {
			t.Errorf("o vão apertado deu uma chave de %d caracteres", len(b))
		}
		// E o outro lado: sempre logo acima da anterior.
		a, b = "V", "W"
		for i := 0; i < 500; i++ {
			k := posicaoOuFalha(t, a, b)
			a = k
		}
	})
}

// Inserções ao acaso numa lista que cresce: a chave nova cai exatamente onde se
// pediu, e a lista segue ordenada e sem repetição.
func TestPosicao_InsercoesAoAcaso(t *testing.T) {
	rng := rand.New(rand.NewSource(26102026))
	lista := []string{}
	for i := 0; i < 5000; i++ {
		pos := rng.Intn(len(lista) + 1) // 0 = topo, len = fim
		antes, depois := "", ""
		if pos > 0 {
			antes = lista[pos-1]
		}
		if pos < len(lista) {
			depois = lista[pos]
		}
		k := posicaoOuFalha(t, antes, depois)
		lista = append(lista, "")
		copy(lista[pos+1:], lista[pos:])
		lista[pos] = k
	}
	for i := 1; i < len(lista); i++ {
		if !(lista[i-1] < lista[i]) {
			t.Fatalf("fora de ordem ou repetida em %d: %q %q", i, lista[i-1], lista[i])
		}
	}
	maior := 0
	for _, k := range lista {
		if len(k) > maior {
			maior = len(k)
		}
	}
	// Ao acaso é o caso gentil; o teto aqui só pega uma regressão grosseira.
	if maior > 40 {
		t.Errorf("a maior chave tem %d caracteres", maior)
	}
}
