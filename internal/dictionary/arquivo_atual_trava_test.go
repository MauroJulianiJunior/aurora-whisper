package dictionary

import (
	"testing"
	"time"
)

// TestArquivo_AtualEAprendidasEsperamAGravacaoEmCurso: Atual (da F3) e
// Aprendidas (da F2) usam a mesma trava de Arquivo. No Windows, renomear por
// cima de um arquivo que alguém está lendo falha, e por isso quem só lê espera
// quem grava. O teste segura a trava como uma gravação em curso faria: as duas
// leituras têm de esperar, e terminar quando ela solta. Se a F2 mudar a trava,
// é aqui que Atual acusa.
func TestArquivo_AtualEAprendidasEsperamAGravacaoEmCurso(t *testing.T) {
	leituras := map[string]func(*Arquivo){
		"Atual":      func(a *Arquivo) { _, _ = a.Atual() },
		"Aprendidas": func(a *Arquivo) { _, _ = a.Aprendidas() },
	}
	for nome, ler := range leituras {
		t.Run(nome, func(t *testing.T) {
			a, _ := arquivoDeTeste(t, sementeDoPainelJSON)
			a.mu.Lock() // a gravação em curso
			terminou := make(chan struct{})
			go func() {
				ler(a)
				close(terminou)
			}()
			select {
			case <-terminou:
				a.mu.Unlock()
				t.Fatalf("%s leu com uma gravação em curso: não esperou a trava", nome)
			case <-time.After(50 * time.Millisecond):
			}
			a.mu.Unlock()
			select {
			case <-terminou:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s não terminou depois que a trava soltou", nome)
			}
		})
	}
}
