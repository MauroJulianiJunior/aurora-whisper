package expurgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os 6 meses no ciclo (ADR-019, "Decisões da F10", F10c), com dublês: a ordem
// da rodada, a rodada que pausou, os logs e o mutex que o Ligar e o Desligar
// compartilham com a rodada. Com o banco de verdade, o ciclo é provado em
// cmd/agent (retencao_test.go).

// chamadas registra, em ordem, o que a rodada chamou, nos dois dublês.
type chamadas struct {
	mu     sync.Mutex
	lista  []string
	quando []time.Time
}

func (c *chamadas) anotar(nome string, em time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lista = append(c.lista, nome)
	c.quando = append(c.quando, em)
}

func (c *chamadas) nomes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lista...)
}

type lixeiraAnotada struct{ c *chamadas }

func (l lixeiraAnotada) PurgeExpired(_ context.Context, now time.Time) (storage.ResultadoDoExpurgo, error) {
	l.c.anotar("lixeira", now)
	return storage.ResultadoDoExpurgo{}, nil
}

// seisFalso devolve, em ordem, os passos roteirizados (esgotado, o zero), cada
// um com o erro da mesma posição em errosDoPasso (sem ele, nil). O Ligar, com
// bloqueio, avisa em dentro e espera o bloqueio fechar.
type seisFalso struct {
	c            *chamadas
	passos       []storage.PassoDosSeisMeses
	errosDoPasso []error
	erroDoLigar  error
	// ligarIncompleto: o Ligar grava o OK, apaga um lote de 500 e o lote
	// seguinte cai (storage.ErrLigadoIncompleto).
	ligarIncompleto bool
	// erroDasCopias: o ApagarCopias devolve apagadas 1, ficaram 1 e este erro.
	erroDasCopias error
	dentro        chan struct{}
	bloqueio      chan struct{}
}

func (s *seisFalso) Passo(_ context.Context, agora time.Time) (storage.PassoDosSeisMeses, error) {
	s.c.anotar("passo", agora)
	if len(s.passos) == 0 {
		return storage.PassoDosSeisMeses{}, nil
	}
	p := s.passos[0]
	s.passos = s.passos[1:]
	var err error
	if len(s.errosDoPasso) > 0 {
		err = s.errosDoPasso[0]
		s.errosDoPasso = s.errosDoPasso[1:]
	}
	return p, err
}

func (s *seisFalso) GravarRodada(_ context.Context, agora time.Time) error {
	s.c.anotar("rodada", agora)
	return nil
}

func (s *seisFalso) Ligar(_ context.Context, agora time.Time, previa int) (storage.ResultadoDoLigar, error) {
	s.c.anotar("ligar", agora)
	if s.bloqueio != nil {
		close(s.dentro)
		<-s.bloqueio
	}
	if s.erroDoLigar != nil {
		return storage.ResultadoDoLigar{}, s.erroDoLigar
	}
	if s.ligarIncompleto {
		return storage.ResultadoDoLigar{Previa: previa, Apagados: 500, Incompleto: true, Copia: "historico.db.antes-dos-6-meses-x.db"},
			fmt.Errorf("%w: %w", storage.ErrLigadoIncompleto, errors.New("storage: abrindo a transação: context canceled"))
	}
	return storage.ResultadoDoLigar{Previa: previa, Apagados: previa, Copia: "historico.db.antes-dos-6-meses-x.db"}, nil
}

func (s *seisFalso) Desligar(context.Context) error {
	s.c.anotar("desligar", time.Time{})
	return nil
}

func (s *seisFalso) Estado(_ context.Context, agora time.Time) (storage.EstadoDosSeisMeses, error) {
	s.c.anotar("estado", agora)
	return storage.EstadoDosSeisMeses{Ligada: true, Copias: 2}, nil
}

func (s *seisFalso) ApagarCopias(context.Context) (storage.ResultadoDasCopias, error) {
	s.c.anotar("copias", time.Time{})
	if s.erroDasCopias != nil {
		return storage.ResultadoDasCopias{Apagadas: 1, Ficaram: 1}, s.erroDasCopias
	}
	return storage.ResultadoDasCopias{Apagadas: 2, Ficaram: 1}, nil
}

func novoCicloComSeis(t *testing.T, passos ...storage.PassoDosSeisMeses) (*Ciclo, *seisFalso, *chamadas, *clock.Fake, *bytes.Buffer) {
	t.Helper()
	c := &chamadas{}
	seis := &seisFalso{c: c, passos: passos}
	relogio := clock.NewFake(inicio)
	var buf bytes.Buffer
	ciclo := Iniciar(relogio, lixeiraAnotada{c}, seis, logEm(&buf))
	t.Cleanup(ciclo.Parar)
	return ciclo, seis, c, relogio, &buf
}

// A rodada, com um agora só: a lixeira, o passo dos 6 meses e a gravação da
// rodada, nessa ordem, na subida e a cada tique.
func TestRodada_AOrdemEOMesmoAgora(t *testing.T) {
	_, _, c, relogio, _ := novoCicloComSeis(t)
	relogio.Advance(2 * Intervalo)
	quer := []string{"lixeira", "passo", "rodada", "lixeira", "passo", "rodada", "lixeira", "passo", "rodada"}
	if got := c.nomes(); strings.Join(got, ",") != strings.Join(quer, ",") {
		t.Fatalf("a rodada chamou %v; quero %v", got, quer)
	}
	for i, em := range c.quando {
		if want := inicio.Add(time.Duration(i/3) * Intervalo); !em.Equal(want) {
			t.Errorf("chamada %d (%s) com %s; quero o agora da rodada, %s", i, c.lista[i], em, want)
		}
	}
}

// A rodada que pausou não grava a rodada (a seguinte, sem rodada, não freia),
// e diz no log, em WARN, só contagens.
func TestRodada_APausaNaoGravaARodada(t *testing.T) {
	_, _, c, relogio, buf := novoCicloComSeis(t,
		storage.PassoDosSeisMeses{Ligada: true, Pausada: true, DiasParado: 8, Previa: 12})
	relogio.Advance(Intervalo)
	quer := []string{"lixeira", "passo", "lixeira", "passo", "rodada"}
	if got := c.nomes(); strings.Join(got, ",") != strings.Join(quer, ",") {
		t.Fatalf("a rodada chamou %v; quero %v", got, quer)
	}
	if l := buf.String(); !strings.Contains(l, "level=WARN") || !strings.Contains(l, "6 meses pausados") ||
		!strings.Contains(l, "dias_parado=8") || !strings.Contains(l, "previa=12") {
		t.Fatalf("o log da pausa:\n%s", l)
	}
}

// O passo que falha não grava a rodada, nem quando o freio tinha disparado e a
// transação da pausa voltou atrás (o OK e a rodada velha continuam no banco):
// gravada a rodada, o freio da seguinte não dispararia mais, e ela apagaria
// sem pausar e sem a pergunta voltar (D2). Mantida a rodada velha, a seguinte
// freia de novo e tenta pausar outra vez. O WARN diz que falhou.
func TestRodada_OPassoQueFalhaNaoGravaARodada(t *testing.T) {
	c := &chamadas{}
	seis := &seisFalso{c: c,
		passos: []storage.PassoDosSeisMeses{
			{Ligada: true, Pausada: true, DiasParado: 8},
			{Ligada: true, Apagados: 2},
			{Ligada: true},
		},
		errosDoPasso: []error{
			errors.New("storage: pausando os 6 meses: database is locked"),
			errors.New("storage: apagando o que passou dos 6 meses: disk I/O error"),
		},
	}
	relogio := clock.NewFake(inicio)
	var buf bytes.Buffer
	ciclo := Iniciar(relogio, lixeiraAnotada{c}, seis, logEm(&buf))
	t.Cleanup(ciclo.Parar)
	relogio.Advance(2 * Intervalo)
	quer := []string{"lixeira", "passo", "lixeira", "passo", "lixeira", "passo", "rodada"}
	if got := c.nomes(); strings.Join(got, ",") != strings.Join(quer, ",") {
		t.Fatalf("a rodada chamou %v; quero %v (o passo que falha não grava a rodada)", got, quer)
	}
	if l := buf.String(); strings.Count(l, "o passo dos 6 meses falhou") != 2 || strings.Contains(l, "6 meses pausados") {
		t.Fatalf("o log do passo que falhou:\n%s", l)
	}
}

// Sem o dono fixo, o passo pula, e a subida diz quanto pulou; as rodadas
// seguintes não repetem a linha. A rodada é gravada do mesmo jeito.
func TestRodada_SemDonoFixoPulaELogaNaSubida(t *testing.T) {
	pulou := storage.PassoDosSeisMeses{SemDonoFixo: true, Pulados: 3}
	_, _, c, relogio, buf := novoCicloComSeis(t, pulou, pulou, pulou)
	relogio.Advance(2 * Intervalo)
	if n := strings.Count(buf.String(), "6 meses pulados"); n != 1 || !strings.Contains(buf.String(), "fora_da_janela=3") {
		t.Fatalf("quero a linha dos pulados uma vez, na subida, com a contagem; log:\n%s", buf.String())
	}
	if n := strings.Count(strings.Join(c.nomes(), ","), "rodada"); n != 3 {
		t.Fatalf("a rodada foi gravada %d vezes; quero 3", n)
	}
}

// O que os 6 meses apagaram sai no log, só a contagem; rodada sem nada
// apagado não escreve.
func TestRodada_ApagadosNoLog(t *testing.T) {
	_, _, _, relogio, buf := novoCicloComSeis(t,
		storage.PassoDosSeisMeses{Ligada: true}, storage.PassoDosSeisMeses{Ligada: true, Apagados: 4})
	if strings.Contains(buf.String(), "6 meses apagados") {
		t.Fatalf("a rodada sem nada apagado escreveu:\n%s", buf.String())
	}
	relogio.Advance(Intervalo)
	if !strings.Contains(buf.String(), "historico: 6 meses apagados") || !strings.Contains(buf.String(), "apagados=4") {
		t.Fatalf("o log dos apagados:\n%s", buf.String())
	}
}

// O Desligar durante um Ligar espera o mutex: o Ligar de uma aba segura o
// ciclo, e o Desligar da outra só roda depois (senão a gravação do OK o
// sobrescreveria).
func TestDesligar_DuranteUmLigarEsperaOMutex(t *testing.T) {
	ciclo, seis, c, _, _ := novoCicloComSeis(t)
	seis.dentro, seis.bloqueio = make(chan struct{}), make(chan struct{})
	ligou := make(chan error, 1)
	go func() {
		_, err := ciclo.Ligar(context.Background(), 5)
		ligou <- err
	}()
	<-seis.dentro
	desligou := make(chan error, 1)
	go func() { desligou <- ciclo.Desligar(context.Background()) }()
	select {
	case <-desligou:
		t.Fatal("o Desligar passou por cima de um Ligar em curso")
	case <-time.After(100 * time.Millisecond):
	}
	close(seis.bloqueio)
	if err := <-ligou; err != nil {
		t.Fatal(err)
	}
	if err := <-desligou; err != nil {
		t.Fatal(err)
	}
	got := c.nomes()
	if len(got) < 2 || got[len(got)-2] != "ligar" || got[len(got)-1] != "desligar" {
		t.Fatalf("a ordem: %v; quero o Ligar inteiro e depois o Desligar", got)
	}
}

// O Ligar diz no log só contagens e se a cópia foi feita; o recusado diz o
// motivo (lista fechada). Depois do Parar, nem o Ligar nem o Desligar rodam.
func TestLigar_LogEParado(t *testing.T) {
	ciclo, seis, _, _, buf := novoCicloComSeis(t)
	if _, err := ciclo.Ligar(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if l := buf.String(); !strings.Contains(l, "6 meses ligados") || !strings.Contains(l, "previa=7") ||
		!strings.Contains(l, "apagados=7") || !strings.Contains(l, "copia=feita") {
		t.Fatalf("o log do Ligar:\n%s", l)
	}
	seis.erroDoLigar = &storage.ErrPreviaMudou{Previa: 9}
	_, err := ciclo.Ligar(context.Background(), 7)
	var mudou *storage.ErrPreviaMudou
	if !errors.As(err, &mudou) || mudou.Previa != 9 {
		t.Fatalf("o Ligar recusado = %v; quero o número novo", err)
	}
	if !strings.Contains(buf.String(), "motivo=previa_mudou") {
		t.Fatalf("o log do recusado:\n%s", buf.String())
	}
	if err := ciclo.Desligar(context.Background()); err != nil || !strings.Contains(buf.String(), "6 meses desligados") {
		t.Fatalf("o Desligar = %v; log:\n%s", err, buf.String())
	}
	ciclo.Parar()
	if _, err := ciclo.Ligar(context.Background(), 7); !errors.Is(err, ErrParado) {
		t.Errorf("o Ligar depois do Parar = %v", err)
	}
	if err := ciclo.Desligar(context.Background()); !errors.Is(err, ErrParado) {
		t.Errorf("o Desligar depois do Parar = %v", err)
	}
}

// O Ligar que gravou o OK e apagou o primeiro lote, mas não o seguinte, ligou:
// não é recusa. O ciclo devolve o resultado sem erro, marcado incompleto, e o
// log diz "ligados" (WARN, com incompleto e a causa), nunca "não ligaram".
func TestLigar_IncompletoELigadoNoLog(t *testing.T) {
	ciclo, seis, _, _, buf := novoCicloComSeis(t)
	seis.ligarIncompleto = true
	r, err := ciclo.Ligar(context.Background(), 1240)
	if err != nil || !r.Incompleto || r.Apagados != 500 || r.Previa != 1240 {
		t.Fatalf("o Ligar incompleto = %+v, %v; quero ligado, sem erro, com os 500 que saíram", r, err)
	}
	l := linhaComTexto(buf.String(), "6 meses ligados")
	if !strings.Contains(l, "level=WARN") || !strings.Contains(l, "incompleto=true") || !strings.Contains(l, "apagados=500") ||
		!strings.Contains(l, "previa=1240") || !strings.Contains(l, "context canceled") {
		t.Fatalf("o log do Ligar incompleto:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "não ligaram") {
		t.Fatalf("o log diz que não ligou:\n%s", buf.String())
	}
}

// linhaComTexto é a primeira linha de log que tem texto (vazia, se nenhuma).
func linhaComTexto(log, texto string) string {
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, texto) {
			return l
		}
	}
	return ""
}

// Sem os 6 meses (o ciclo de antes), a rodada é só a lixeira, e o Ligar e o
// Desligar recusam.
func TestCiclo_SemSeisMeses(t *testing.T) {
	relogio := clock.NewFake(inicio)
	c := &chamadas{}
	ciclo := Iniciar(relogio, lixeiraAnotada{c}, nil, logEm(&bytes.Buffer{}))
	defer ciclo.Parar()
	relogio.Advance(Intervalo)
	if got := c.nomes(); strings.Join(got, ",") != "lixeira,lixeira" {
		t.Fatalf("a rodada chamou %v", got)
	}
	if _, err := ciclo.Ligar(context.Background(), 1); !errors.Is(err, ErrSemSeisMeses) {
		t.Errorf("o Ligar sem os 6 meses = %v", err)
	}
	if err := ciclo.Desligar(context.Background()); !errors.Is(err, ErrSemSeisMeses) {
		t.Errorf("o Desligar sem os 6 meses = %v", err)
	}
}

// O ciclo nunca apaga as cópias sozinho (D4): meses de rodadas, e o
// ApagarCopias não é chamado nenhuma vez.
func TestCopias_OCicloNuncaAsApagaSozinho(t *testing.T) {
	_, _, c, relogio, buf := novoCicloComSeis(t)
	relogio.Advance(90 * 24 * time.Hour)
	for _, n := range c.nomes() {
		if n == "copias" {
			t.Fatal("o ciclo apagou as cópias sozinho")
		}
	}
	if strings.Contains(buf.String(), "cópias apagadas") {
		t.Fatalf("o log diz que apagou:\n%s", buf.String())
	}
}

// O ApagarCopias entra no mutex: com um Ligar em curso (que pode estar
// copiando o banco), espera. O Estado é só leitura e não espera.
func TestCopias_DuranteUmLigarEsperaOMutexEOEstadoNao(t *testing.T) {
	ciclo, seis, c, _, _ := novoCicloComSeis(t)
	seis.dentro, seis.bloqueio = make(chan struct{}), make(chan struct{})
	ligou := make(chan error, 1)
	go func() {
		_, err := ciclo.Ligar(context.Background(), 5)
		ligou <- err
	}()
	<-seis.dentro

	leu := make(chan error, 1)
	go func() {
		_, err := ciclo.Estado(context.Background())
		leu <- err
	}()
	select {
	case err := <-leu:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o Estado esperou o Ligar terminar")
	}

	apagou := make(chan error, 1)
	go func() {
		_, err := ciclo.ApagarCopias(context.Background())
		apagou <- err
	}()
	select {
	case <-apagou:
		t.Fatal("o ApagarCopias passou por cima de um Ligar em curso")
	case <-time.After(100 * time.Millisecond):
	}
	close(seis.bloqueio)
	if err := <-ligou; err != nil {
		t.Fatal(err)
	}
	if err := <-apagou; err != nil {
		t.Fatal(err)
	}
	if got := c.nomes(); len(got) < 3 || strings.Join(got[len(got)-3:], ",") != "ligar,estado,copias" {
		t.Fatalf("a ordem: %v; quero o Ligar, o Estado que não esperou e, por último, as cópias", got)
	}
}

// O Estado usa o agora do relógio do ciclo, e o ApagarCopias diz no log só as
// contagens, com e sem erro.
func TestCopias_EstadoComOAgoraDoRelogioELogDeContagens(t *testing.T) {
	ciclo, seis, c, relogio, buf := novoCicloComSeis(t)
	relogio.Advance(3 * time.Hour)
	e, err := ciclo.Estado(context.Background())
	if err != nil || !e.Ligada || e.Copias != 2 {
		t.Fatalf("o Estado = %+v, %v", e, err)
	}
	var ultimo time.Time
	for i, n := range c.nomes() {
		if n == "estado" {
			ultimo = c.quando[i]
		}
	}
	if want := inicio.Add(3 * time.Hour); !ultimo.Equal(want) {
		t.Errorf("o Estado leu com %s; quero o agora do relógio, %s", ultimo, want)
	}

	r, err := ciclo.ApagarCopias(context.Background())
	if err != nil || r.Apagadas != 2 || r.Ficaram != 1 {
		t.Fatalf("o ApagarCopias = %+v, %v", r, err)
	}
	if l := buf.String(); !strings.Contains(l, "historico: cópias apagadas") || !strings.Contains(l, "apagadas=2") || !strings.Contains(l, "ficaram=1") {
		t.Fatalf("o log das cópias:\n%s", l)
	}
	seis.erroDasCopias = errors.New("storage: apagando uma cópia do histórico: Access is denied.")
	r, err = ciclo.ApagarCopias(context.Background())
	if err == nil || r.Apagadas != 1 || r.Ficaram != 1 {
		t.Fatalf("o ApagarCopias com falha = %+v, %v", r, err)
	}
	if l := buf.String(); !strings.Contains(l, "level=WARN") || !strings.Contains(l, "as cópias não saíram todas") {
		t.Fatalf("o log da falha:\n%s", l)
	}
}

// Depois do Parar, ou sem os 6 meses, o ApagarCopias recusa.
func TestCopias_ParadoESemSeisMeses(t *testing.T) {
	ciclo, _, _, _, _ := novoCicloComSeis(t)
	ciclo.Parar()
	if _, err := ciclo.ApagarCopias(context.Background()); !errors.Is(err, ErrParado) {
		t.Errorf("o ApagarCopias depois do Parar = %v", err)
	}

	sem := Iniciar(clock.NewFake(inicio), lixeiraAnotada{&chamadas{}}, nil, logEm(&bytes.Buffer{}))
	defer sem.Parar()
	if _, err := sem.ApagarCopias(context.Background()); !errors.Is(err, ErrSemSeisMeses) {
		t.Errorf("o ApagarCopias sem os 6 meses = %v", err)
	}
	if _, err := sem.Estado(context.Background()); !errors.Is(err, ErrSemSeisMeses) {
		t.Errorf("o Estado sem os 6 meses = %v", err)
	}
}
