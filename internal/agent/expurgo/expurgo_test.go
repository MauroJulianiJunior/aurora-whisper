package expurgo

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// repoFalso registra o instante de cada chamada e devolve, em ordem, os
// resultados roteirizados; esgotado o roteiro, devolve (0, nil).
type repoFalso struct {
	chamadas []time.Time
	roteiro  []resultado
}

type resultado struct {
	apagados  int
	esperando int
	err       error
}

func (r *repoFalso) PurgeExpired(_ context.Context, now time.Time) (storage.ResultadoDoExpurgo, error) {
	r.chamadas = append(r.chamadas, now)
	if len(r.roteiro) == 0 {
		return storage.ResultadoDoExpurgo{}, nil
	}
	res := r.roteiro[0]
	r.roteiro = r.roteiro[1:]
	return storage.ResultadoDoExpurgo{Apagados: res.apagados, EsperandoANuvem: res.esperando}, res.err
}

var inicio = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func logEm(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func TestIniciar_ExpurgaNaSubidaComOInstanteDoRelogio(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{}
	c := Iniciar(relogio, repo, nil, logEm(&bytes.Buffer{}))
	defer c.Parar()

	if len(repo.chamadas) != 1 {
		t.Fatalf("Iniciar deveria expurgar uma vez antes de devolver; foram %d chamadas", len(repo.chamadas))
	}
	if !repo.chamadas[0].Equal(inicio) {
		t.Fatalf("o expurgo deveria usar o Now() do relógio injetado: got %v, want %v", repo.chamadas[0], inicio)
	}
}

func TestCiclo_RodaACadaIntervaloENuncaAntes(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{}
	c := Iniciar(relogio, repo, nil, logEm(&bytes.Buffer{}))
	defer c.Parar()

	relogio.Advance(Intervalo - time.Nanosecond)
	if len(repo.chamadas) != 1 {
		t.Fatalf("antes do intervalo não deveria haver segunda rodada; foram %d chamadas", len(repo.chamadas))
	}

	relogio.Advance(time.Nanosecond)
	if len(repo.chamadas) != 2 {
		t.Fatalf("ao completar o intervalo deveria haver a segunda rodada; foram %d chamadas", len(repo.chamadas))
	}

	// E segue: não é uma rodada extra só, é um ciclo.
	relogio.Advance(3 * Intervalo)
	if len(repo.chamadas) != 5 {
		t.Fatalf("depois de 4 intervalos deveriam ser 5 rodadas; foram %d", len(repo.chamadas))
	}
	for i, got := range repo.chamadas {
		if want := inicio.Add(time.Duration(i) * Intervalo); !got.Equal(want) {
			t.Errorf("rodada %d: now = %v, want %v", i, got, want)
		}
	}
}

func TestCiclo_FalhaNaoParaOCiclo(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{roteiro: []resultado{{err: errors.New("banco ocupado")}, {err: errors.New("banco ocupado")}}}
	var buf bytes.Buffer
	c := Iniciar(relogio, repo, nil, logEm(&buf))
	defer c.Parar()

	relogio.Advance(2 * Intervalo)
	if len(repo.chamadas) != 3 {
		t.Fatalf("duas falhas seguidas não podem parar o ciclo; foram %d rodadas, want 3", len(repo.chamadas))
	}
	if !strings.Contains(buf.String(), "expurgo falhou") {
		t.Fatalf("a falha deveria ir para o log; log:\n%s", buf.String())
	}
}

func TestParar_CancelaAsProximasRodadas(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{}
	c := Iniciar(relogio, repo, nil, logEm(&bytes.Buffer{}))

	c.Parar()
	c.Parar() // idempotente
	relogio.Advance(10 * Intervalo)

	if len(repo.chamadas) != 1 {
		t.Fatalf("depois de Parar não pode haver rodada; foram %d chamadas, want 1 (a da subida)", len(repo.chamadas))
	}
}

func TestLog_NaSubidaSempreDepoisSoQuandoApaga(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{roteiro: []resultado{{apagados: 0}, {apagados: 0}, {apagados: 3}}}
	var buf bytes.Buffer
	c := Iniciar(relogio, repo, nil, logEm(&buf))
	defer c.Parar()

	linhas := func() []string { return strings.Split(strings.TrimSpace(buf.String()), "\n") }

	if l := linhas(); len(l) != 1 || !strings.Contains(l[0], "apagados=0") {
		t.Fatalf("na subida deveria sair uma linha com a contagem, mesmo zero; log:\n%s", buf.String())
	}

	relogio.Advance(Intervalo)
	if l := linhas(); len(l) != 1 {
		t.Fatalf("rodada periódica sem nada a apagar não deveria logar; log:\n%s", buf.String())
	}

	relogio.Advance(Intervalo)
	if l := linhas(); len(l) != 2 || !strings.Contains(l[1], "apagados=3") {
		t.Fatalf("rodada periódica que apagou deveria logar a contagem; log:\n%s", buf.String())
	}
}

// A lixeira que espera a nuvem (a D3 da F10): a linha sai na subida com as
// duas contagens, e depois só quando algo foi apagado ou quando o número dos
// que esperam muda. O PC que segura a mesma lixeira por meses não escreve uma
// linha por hora.
func TestLog_EsperandoANuvemSoQuandoMuda(t *testing.T) {
	relogio := clock.NewFake(inicio)
	repo := &repoFalso{roteiro: []resultado{
		{esperando: 2}, // a subida
		{esperando: 2}, // igual: nada
		{esperando: 2}, // igual: nada
		{esperando: 1}, // mudou: sai
		{esperando: 1}, // igual: nada
		{esperando: 0, apagados: 1},
	}}
	var buf bytes.Buffer
	c := Iniciar(relogio, repo, nil, logEm(&buf))
	defer c.Parar()
	relogio.Advance(5 * Intervalo)

	linhas := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(linhas) != 3 {
		t.Fatalf("quero 3 linhas (a subida, a espera que mudou e a que apagou); log:\n%s", buf.String())
	}
	for i, quer := range []string{"apagados=0 esperando_a_nuvem=2", "apagados=0 esperando_a_nuvem=1", "apagados=1 esperando_a_nuvem=0"} {
		if !strings.Contains(linhas[i], "lixeira: expurgo") || !strings.Contains(linhas[i], quer) {
			t.Errorf("linha %d: %s; quero %q", i, linhas[i], quer)
		}
	}
}
