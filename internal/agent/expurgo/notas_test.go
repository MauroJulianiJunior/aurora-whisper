package expurgo

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// notasFalsas registra o instante de cada rodada da lixeira das notas e
// devolve, em ordem, os resultados roteirizados.
type notasFalsas struct {
	chamadas []time.Time
	roteiro  []resultado
}

func (n *notasFalsas) PurgeExpired(_ context.Context, now time.Time) (int, error) {
	n.chamadas = append(n.chamadas, now)
	if len(n.roteiro) == 0 {
		return 0, nil
	}
	res := n.roteiro[0]
	n.roteiro = n.roteiro[1:]
	return res.apagados, res.err
}

// As notas entram na mesma rodada dos ditados (ADR-022, N1): na subida e a
// cada intervalo, com o mesmo agora do relógio.
func TestComNotas_ExpurgaNaMesmaRodadaDosDitados(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{}
	notas := &notasFalsas{}
	c := Iniciar(relogio, repo, nil, logEm(&bytes.Buffer{}), ComNotas(notas))
	defer c.Parar()

	if len(notas.chamadas) != 1 || !notas.chamadas[0].Equal(inicio) {
		t.Fatalf("a subida deveria expurgar as notas uma vez, com o agora do relógio: %v", notas.chamadas)
	}
	relogio.Advance(Intervalo - time.Nanosecond)
	if len(notas.chamadas) != 1 {
		t.Fatalf("antes do intervalo não há segunda rodada das notas: %d", len(notas.chamadas))
	}
	relogio.Advance(time.Nanosecond)
	relogio.Advance(2 * Intervalo)
	if len(notas.chamadas) != 4 || len(repo.chamadas) != 4 {
		t.Fatalf("as notas e os ditados andam juntos: %d e %d rodadas, quer 4 e 4", len(notas.chamadas), len(repo.chamadas))
	}
	for i := range notas.chamadas {
		if !notas.chamadas[i].Equal(repo.chamadas[i]) {
			t.Errorf("rodada %d: as notas usaram %v e os ditados %v", i, notas.chamadas[i], repo.chamadas[i])
		}
	}
}

// Uma falha numa lixeira não pula a outra, e nenhuma para o ciclo.
func TestComNotas_UmaFalhaNaoPulaAOutra(t *testing.T) {
	relogio := clock.NewFake(inicio)
	erro := errors.New("banco ocupado")
	repo := &repoFalso{roteiro: []resultado{{err: erro}}}
	notas := &notasFalsas{roteiro: []resultado{{err: erro}, {apagados: 2}}}
	var buf bytes.Buffer
	c := Iniciar(relogio, repo, nil, logEm(&buf), ComNotas(notas))
	defer c.Parar()

	if len(repo.chamadas) != 1 || len(notas.chamadas) != 1 {
		t.Fatalf("as duas lixeiras rodam na subida, mesmo com falha: ditados %d, notas %d", len(repo.chamadas), len(notas.chamadas))
	}
	log := buf.String()
	if !strings.Contains(log, "expurgo falhou") || !strings.Contains(log, "expurgo das notas falhou") {
		t.Fatalf("as duas falhas deveriam ir para o log:\n%s", log)
	}
	relogio.Advance(Intervalo)
	if len(notas.chamadas) != 2 {
		t.Fatalf("a falha não pode parar o ciclo das notas: %d rodadas", len(notas.chamadas))
	}
	if !strings.Contains(buf.String(), "apagadas=2") {
		t.Errorf("a contagem das notas apagadas deveria ir para o log:\n%s", buf.String())
	}
}

// Sem notas (o ciclo montado como antes), nada muda: o log não fala delas.
func TestSemNotas_NaoLogaNada(t *testing.T) {
	var buf bytes.Buffer
	c := Iniciar(clock.NewFake(inicio), &repoFalso{}, nil, logEm(&buf), ComNotas(nil))
	defer c.Parar()
	if strings.Contains(buf.String(), "notas") {
		t.Errorf("sem notas, o log não fala delas:\n%s", buf.String())
	}
}

// O log das notas só tem a contagem: nunca id nem texto. Na rodada periódica
// sem nada a apagar, nem a linha sai.
func TestComNotas_LogSoTemContagemESoQuandoApaga(t *testing.T) {
	relogio := clock.NewFake(inicio)
	var buf bytes.Buffer
	notas := &notasFalsas{roteiro: []resultado{{apagados: 5}, {apagados: 0}}}
	c := Iniciar(relogio, &repoFalso{}, nil, logEm(&buf), ComNotas(notas))
	defer c.Parar()

	conta := func() (int, string) {
		n, ultima := 0, ""
		for _, l := range strings.Split(buf.String(), "\n") {
			if strings.Contains(l, "expurgo das notas") {
				n++
				ultima = l
			}
		}
		return n, ultima
	}
	if n, l := conta(); n != 1 || !strings.Contains(l, "apagadas=5") {
		t.Fatalf("na subida: %d linha(s), última %q", n, l)
	}
	relogio.Advance(Intervalo)
	if n, _ := conta(); n != 1 {
		t.Fatalf("a rodada periódica sem nada a apagar não deveria logar: %d linha(s)", n)
	}
}
