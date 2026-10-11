package chavestest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A idempotência, os limites, as vagas e as corridas da rota de ditar (ADR-021,
// "Idempotência", "Limites", decisão I; C5b). Os provedores falsos contam as
// chamadas: "sem chamar o provedor" é a contagem que não sobe.

// porta é um canal que o caso abre quando quiser (abrir), e que se abre sozinho
// no fim do caso: um caso que falha antes de abrir não deixa o provedor falso
// preso, nem o Close do httptest.Server esperando para sempre.
func porta(t *testing.T) (ch chan struct{}, abrir func()) {
	t.Helper()
	ch = make(chan struct{})
	var uma sync.Once
	abrir = func() { uma.Do(func() { close(ch) }) }
	t.Cleanup(abrir)
	return ch, abrir
}

// avisar manda um sinal sem esperar (o primeiro basta).
func avisar(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Dois envios com o mesmo id dão uma chamada ao provedor: o segundo recebe o que
// está gravado (200), sem ler o corpo. O mesmo id em duas contas são dois
// ditados, com duas chamadas.
func testDitarMesmoIdUmaChamada(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	m.prepararDitado(celB, "openai", "", false)
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm())
	primeiro := m.ditadoOk(rec, http.StatusCreated)
	rec, corpo := m.ditar(celA, id, webm())
	segundo := m.ditadoOk(rec, http.StatusOK)
	if !corpo.naoLido() {
		t.Error("o reenvio leu o corpo")
	}
	if segundo.ID != primeiro.ID || segundo.Version != primeiro.Version || segundo.FinalText != primeiro.FinalText {
		t.Fatalf("o reenvio devolveu outro ditado: %+v / %+v", primeiro, segundo)
	}
	if n := m.voz.quantas(); n != 1 {
		t.Fatalf("%d chamadas ao provedor, quero 1", n)
	}
	// A outra conta, com o mesmo id: outro ditado.
	m.voz.responder("o ditado da outra conta é este aqui", 0)
	rec, _ = m.ditar(celB, id, webm())
	outro := m.ditadoOk(rec, http.StatusCreated)
	if outro.FinalText != "o ditado da outra conta é este aqui" || m.voz.quantas() != 2 {
		t.Fatalf("o mesmo id na conta B: %+v, %d chamadas", outro, m.voz.quantas())
	}
	if s := m.situacao(celA, id); s.Ditado == nil || s.Ditado.FinalText != primeiro.FinalText {
		t.Fatal("o ditado da conta B mexeu no da conta A")
	}
	// A resposta idempotente não conta no limite: com o limite do minuto
	// gasto, o reenvio ainda recebe o ditado.
	for i := 0; i < 20; i++ {
		rec, _ = m.ditar(celA, id, webm())
		m.ditadoOk(rec, http.StatusOK)
	}
}

// O reenvio durante o andamento espera o primeiro e recebe o mesmo ditado, sem
// ler o corpo nem chamar o provedor de novo.
func testDitarReenvioDuranteOAndamento(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	soltar, abrir := porta(t)
	entrou := make(chan struct{}, 1)
	m.voz.enquantoResponde(t, func() {
		avisar(entrou)
		<-soltar
	})
	id := cloudstore.NewUUIDv7()
	var wg sync.WaitGroup
	var primeiro, segundo *httptest.ResponseRecorder
	var corpo2 *corpoVigiado
	wg.Add(1)
	go func() {
		defer wg.Done()
		primeiro, _ = m.ditar(celA, id, webm())
	}()
	<-entrou
	wg.Add(1)
	go func() {
		defer wg.Done()
		segundo, corpo2 = m.ditar(celA, id, webm())
	}()
	// O segundo espera: dá um tempo para ele chegar à espera.
	time.Sleep(100 * time.Millisecond)
	abrir()
	wg.Wait()
	a, b := m.ditadoOk(primeiro, http.StatusCreated), m.ditadoOk(segundo, http.StatusOK)
	if a.ID != b.ID || a.FinalText != b.FinalText || a.Version != b.Version {
		t.Fatalf("o reenvio durante o andamento: %+v / %+v", a, b)
	}
	if !corpo2.naoLido() || m.voz.quantas() != 1 {
		t.Fatalf("o reenvio durante o andamento leu o corpo (%v) ou chamou o provedor (%d chamadas)", !corpo2.naoLido(), m.voz.quantas())
	}
}

// storeQueSegura é o Store da nuvem com a resposta de uma leitura da situação
// (Situacao) segurada quando o caso pede: a leitura já foi feita no banco, e a
// resposta chega atrasada, como num banco ocupado.
type storeQueSegura struct {
	cloudstore.Store
	mu sync.Mutex
	// segurar, se não for nil, é o que a próxima Situacao espera abrir antes de
	// voltar; leu é fechado quando ela já leu.
	segurar, leu chan struct{}
}

func (s *storeQueSegura) Situacao(ctx context.Context, userID, id string) (cloudstore.SituacaoDoId, error) {
	sit, err := s.Store.Situacao(ctx, userID, id)
	s.mu.Lock()
	segurar, leu := s.segurar, s.leu
	s.segurar, s.leu = nil, nil
	s.mu.Unlock()
	if segurar != nil {
		close(leu)
		<-segurar
	}
	return sit, err
}

// O reenvio que chega no fim do primeiro: ele lê a situação do id ("nada")
// enquanto o primeiro está no provedor, e a resposta do banco só chega depois
// de o primeiro gravar e sair da memória. O reenvio não pode pagar o provedor
// de novo nem ler o corpo: recebe o ditado gravado (200), com uma chamada só.
func testDitarReenvioQueChegaNoFimDoPrimeiro(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	var st *storeQueSegura
	m.embrulhar = func(s cloudstore.Store) cloudstore.Store {
		st = &storeQueSegura{Store: s}
		return st
	}
	m.montar(ChaveDeCifraA)
	soltar, abrir := porta(t)
	entrou := make(chan struct{}, 1)
	m.voz.enquantoResponde(t, func() {
		avisar(entrou)
		<-soltar
	})
	id := cloudstore.NewUUIDv7()
	primeiro := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r, _ := m.ditar(celA, id, webm())
		primeiro <- r
	}()
	<-entrou

	segurar, liberar := porta(t)
	leu := make(chan struct{})
	st.mu.Lock()
	st.segurar, st.leu = segurar, leu
	st.mu.Unlock()
	type resposta struct {
		rec   *httptest.ResponseRecorder
		corpo *corpoVigiado
	}
	segundo := make(chan resposta, 1)
	go func() {
		r, c := m.ditar(celA, id, webm())
		segundo <- resposta{r, c}
	}()
	<-leu
	// O primeiro sai do provedor, grava e responde; só então o banco devolve ao
	// reenvio a situação que leu antes ("nada").
	abrir()
	a := m.ditadoOk(<-primeiro, http.StatusCreated)
	liberar()
	s := <-segundo
	b := m.ditadoOk(s.rec, http.StatusOK)
	if a.ID != b.ID || a.FinalText != b.FinalText || a.Version != b.Version {
		t.Fatalf("o reenvio do fim do primeiro: %+v / %+v", a, b)
	}
	if n := m.voz.quantas(); n != 1 || !s.corpo.naoLido() {
		t.Fatalf("o reenvio do fim do primeiro pagou de novo: %d chamadas ao provedor, corpo lido=%v", n, !s.corpo.naoLido())
	}
	// A resposta idempotente não gastou ficha: com o limite de 10 por minuto,
	// ainda cabem 9 ditados novos depois do primeiro.
	for i := 0; i < 9; i++ {
		m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	}
}

// O celular cai depois da chamada ao provedor: o trabalho segue, desligado da
// conexão, e grava o ditado; o reenvio sai 200, sem chamada.
func testDitarCelularCaiDepoisDoProvedor(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	ctx, cair := context.WithCancel(context.Background())
	m.voz.enquantoResponde(t, cair)
	id := cloudstore.NewUUIDv7()
	r, _ := m.pedidoDeDitado(celA, id, webm())
	rec := httptest.NewRecorder()
	m.h.ServeHTTP(rec, r.WithContext(ctx))
	esperarAte(t, "o ditado gravado depois de o celular cair", func() bool {
		return m.situacao(celA, id).Estado == cloudstore.IdDitado
	})
	m.voz.enquantoResponde(t, nil)
	rec2, corpo := m.ditar(celA, id, webm())
	d := m.ditadoOk(rec2, http.StatusOK)
	if d.ID != id || !corpo.naoLido() || m.voz.quantas() != 1 {
		t.Fatalf("o reenvio depois da queda: %+v, lido=%v, %d chamadas", d, !corpo.naoLido(), m.voz.quantas())
	}
	if !strings.Contains(m.logs.String(), "desfecho=criado") {
		t.Error("a linha \"ditado\" do trabalho solto não saiu")
	}
}

// storeQueFalha é o Store da nuvem com a gravação (Create) falhando as vezes
// que o caso pedir, como um banco fora do ar por um instante.
type storeQueFalha struct {
	cloudstore.Store
	mu     sync.Mutex
	falhas int
}

func (s *storeQueFalha) Create(ctx context.Context, a cloudstore.Actor, epoch, id string, n cloudstore.NewDictation, now time.Time) (cloudstore.Transcription, bool, error) {
	s.mu.Lock()
	falhar := s.falhas > 0
	if falhar {
		s.falhas--
	}
	s.mu.Unlock()
	if falhar {
		return cloudstore.Transcription{}, false, errors.New("banco fora do ar")
	}
	return s.Store.Create(ctx, a, epoch, id, n, now)
}

func (m *mundo) comGravacaoQueFalha(vezes int) *storeQueFalha {
	m.t.Helper()
	var s *storeQueFalha
	m.embrulhar = func(st cloudstore.Store) cloudstore.Store {
		s = &storeQueFalha{Store: st, falhas: vezes}
		return s
	}
	m.montar(ChaveDeCifraA)
	return s
}

// A gravação que falha depois de o provedor cobrar: 500, e o desfecho pago fica
// na memória. O reenvio só grava, sem chamar o provedor nem o Polish, com o
// mesmo texto e o mesmo aviso. Depois de 10 minutos, o pago some e o reenvio
// paga de novo.
func testDitarGravacaoQueFalhaUmaVez(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "openai", false)
	m.comGravacaoQueFalha(1)
	m.pol.responder(http.StatusServiceUnavailable, nil)
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusInternalServerError, "INTERNAL")
	if m.voz.quantas() != 1 || m.pol.quantas() != 1 || m.situacao(celA, id).Estado != cloudstore.IdNada {
		t.Fatalf("a gravação que falhou: %d chamadas ao provedor, %d ao Polish", m.voz.quantas(), m.pol.quantas())
	}
	m.pol.responder(0, nil)
	rec, corpo := m.ditar(celA, id, webm())
	d := m.ditadoOk(rec, http.StatusCreated)
	if !corpo.naoLido() || m.voz.quantas() != 1 || m.pol.quantas() != 1 {
		t.Fatalf("o reenvio do pago: lido=%v, %d chamadas ao provedor, %d ao Polish", !corpo.naoLido(), m.voz.quantas(), m.pol.quantas())
	}
	if d.Mode != "smart" || d.Aviso == "" || d.FinalText != "hoje eu entreguei o relatório para o cliente da empresa" {
		t.Fatalf("o ditado regravado: %+v", d)
	}
	if !strings.Contains(m.logs.String(), "desfecho=regravado") {
		t.Error("a linha \"ditado\" do reenvio não diz regravado")
	}

	// Depois de 10 minutos, o pago some: o reenvio chama o provedor de novo.
	m.comGravacaoQueFalha(1)
	id2 := cloudstore.NewUUIDv7()
	rec, _ = m.ditar(celA, id2, webm())
	exigirErro(t, rec, http.StatusInternalServerError, "INTERNAL")
	m.clk.Advance(memoria() + time.Second)
	rec, _ = m.ditar(celA, id2, webm())
	m.ditadoOk(rec, http.StatusCreated)
	if n := m.voz.quantas(); n != 3 {
		t.Fatalf("depois dos 10 min, %d chamadas ao provedor, quero 3", n)
	}
}

// O id com lápide (o ditado apagado de vez): 410 APAGADO, sem ler o corpo nem
// chamar o provedor.
func testDitarLapide(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm())
	m.ditadoOk(rec, http.StatusCreated)
	m.exigirStatus(m.pedir(celA, http.MethodDelete, "/api/v1/transcriptions/"+id, ""), http.StatusNoContent, "lixeira")
	// Na lixeira, o reenvio ainda recebe o ditado.
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, id, webm()); return r }(), http.StatusOK)
	m.exigirStatus(m.pedir(celA, http.MethodPost, "/api/v1/transcriptions/"+id+"/purge", ""), http.StatusNoContent, "excluir de vez")
	rec, corpo := m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusGone, "APAGADO")
	if !corpo.naoLido() || m.voz.quantas() != 1 {
		t.Fatalf("a lápide: lido=%v, %d chamadas", !corpo.naoLido(), m.voz.quantas())
	}
}

// Os limites da conta: o 11º ditado no minuto é 429 LIMITE com Retry-After, sem
// ler o corpo nem chamar o provedor; e, com o minuto livre, ele passa.
func testDitarLimitePorMinuto(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	for i := 0; i < 10; i++ {
		rec, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
		m.ditadoOk(rec, http.StatusCreated)
	}
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusTooManyRequests, "LIMITE")
	if ra := rec.Header().Get("Retry-After"); ra == "" || ra == "0" {
		t.Fatalf("Retry-After %q", ra)
	}
	if !corpo.naoLido() || m.voz.quantas() != 10 {
		t.Fatalf("o 11º: lido=%v, %d chamadas", !corpo.naoLido(), m.voz.quantas())
	}
	// A outra conta tem o balde dela.
	m.prepararDitado(celB, "openai", "", false)
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celB, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	m.clk.Advance(7 * time.Second)
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
}

// O limite do dia (aqui, 3 para o teste): o 4º é 429 LIMITE, com o Retry-After
// das horas que faltam para uma ficha; no dia seguinte, passa.
func testDitarLimitePorDia(t *testing.T, m *mundo) {
	m.limites = server.Limites{DitarPorDia: 3, DitarPorMinuto: 100}
	m.prepararDitado(celA, "openai", "", false)
	for i := 0; i < 3; i++ {
		m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	}
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusTooManyRequests, "LIMITE")
	if ra := rec.Header().Get("Retry-After"); ra == "" || len(ra) < 3 {
		t.Fatalf("Retry-After %q, quero horas", ra)
	}
	if !corpo.naoLido() {
		t.Fatal("o 4º do dia leu o corpo")
	}
	m.clk.Advance(24 * time.Hour)
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
}

// Um ditado de cada vez por conta: o segundo, com outro id, enquanto o primeiro
// está no provedor, é 429 UM_POR_VEZ com Retry-After, sem ler o corpo. Sem vaga
// no total (aqui, 1 para o teste), o de outra conta é 503 OCUPADO com
// Retry-After, sem ler o corpo.
func testDitarUmPorVezEOcupado(t *testing.T, m *mundo) {
	m.limites = server.Limites{DitarNoTotal: 1}
	m.prepararDitado(celA, "openai", "", false)
	m.prepararDitado(celB, "openai", "", false)
	soltar, abrir := porta(t)
	entrou := make(chan struct{}, 1)
	m.voz.enquantoResponde(t, func() {
		avisar(entrou)
		<-soltar
	})
	feito := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
		feito <- r
	}()
	<-entrou
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusTooManyRequests, "UM_POR_VEZ")
	if rec.Header().Get("Retry-After") == "" || !corpo.naoLido() {
		t.Fatalf("UM_POR_VEZ: Retry-After %q, lido=%v", rec.Header().Get("Retry-After"), !corpo.naoLido())
	}
	rec, corpo = m.ditar(celB, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusServiceUnavailable, "OCUPADO")
	if rec.Header().Get("Retry-After") == "" || !corpo.naoLido() {
		t.Fatalf("OCUPADO: Retry-After %q, lido=%v", rec.Header().Get("Retry-After"), !corpo.naoLido())
	}
	abrir()
	m.ditadoOk(<-feito, http.StatusCreated)
	// A vaga voltou.
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celB, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if n := m.voz.quantas(); n != 2 {
		t.Fatalf("%d chamadas, quero 2", n)
	}
}

// A memória: 10 envios ao mesmo tempo (vagas da conta folgadas para o teste)
// nunca leem mais de 3 corpos, as vagas do total; os outros 7 recebem OCUPADO
// sem ler o corpo. O pico é medido no provedor (as chamadas ao mesmo tempo) e
// nos corpos lidos.
func testDitarDezAoMesmoTempo(t *testing.T, m *mundo) {
	m.limites = server.Limites{DitarAoMesmoTempo: 20, DitarPorMinuto: 100}
	m.prepararDitado(celA, "openai", "", false)
	soltar, abrir := porta(t)
	m.voz.enquantoResponde(t, func() { <-soltar })
	audio := webm(strings.Repeat("a", 512<<10))
	var wg sync.WaitGroup
	var respondidos atomic.Int32
	recs := make([]*httptest.ResponseRecorder, 10)
	corpos := make([]*corpoVigiado, 10)
	pedidos := make([]*http.Request, 10)
	for i := range pedidos {
		pedidos[i], corpos[i] = m.pedidoDeDitado(celA, cloudstore.NewUUIDv7(), audio)
		recs[i] = httptest.NewRecorder()
	}
	for i := range pedidos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.h.ServeHTTP(recs[i], pedidos[i])
			respondidos.Add(1)
		}()
	}
	// Os 7 recusados respondem; os 3 com vaga ficam no provedor. Só então o
	// provedor responde (senão uma vaga que vagou seria pega por um atrasado).
	esperarAte(t, "as 3 vagas no provedor e as 7 recusas", func() bool {
		return m.voz.quantas() == 3 && respondidos.Load() == 7
	})
	lidos, bytesLidos := 0, int64(0)
	for _, c := range corpos {
		if !c.naoLido() {
			lidos++
			bytesLidos += c.lidos.Load()
		}
	}
	abrir()
	wg.Wait()
	criados, ocupados := 0, 0
	for _, r := range recs {
		switch r.Code {
		case http.StatusCreated:
			criados++
		case http.StatusServiceUnavailable:
			ocupados++
			if code, _ := erroDe(t, r); code != "OCUPADO" {
				t.Errorf("o 503 com o código %q", code)
			}
		default:
			t.Errorf("status %d: %s", r.Code, r.Body.String())
		}
	}
	t.Logf("pico: %d chamadas ao provedor ao mesmo tempo, %d corpos lidos, %d bytes de áudio lidos", m.voz.picoEmVoo(), lidos, bytesLidos)
	if lidos > 3 || m.voz.picoEmVoo() > 3 || criados != 3 || ocupados != 7 || bytesLidos > 3*int64(len(audio)) {
		t.Fatalf("10 ao mesmo tempo: %d corpos lidos, pico %d, %d criados, %d ocupados", lidos, m.voz.picoEmVoo(), criados, ocupados)
	}
}

// Revogar o aceite ("Desligar o ditado") durante a chamada ao provedor: o Polish
// não é chamado, nada é gravado, e a resposta é 403 SEM_ACEITE.
func testDitarRevogarDuranteOProvedor(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "openai", false)
	m.voz.enquantoResponde(t, func() {
		m.exigirStatus(m.pedir(celA, http.MethodDelete, rotaAceite, ""), http.StatusNoContent, "revogar no meio")
	})
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if m.pol.quantas() != 0 || m.situacao(celA, id).Estado != cloudstore.IdNada {
		t.Fatalf("revogar no meio: %d chamadas ao Polish, %v", m.pol.quantas(), m.situacao(celA, id).Estado)
	}
	// Sem o Polish, a conferência antes de gravar também barra.
	m.voz.enquantoResponde(t, nil)
	m.prepararDitado(celB, "openai", "", false)
	m.voz.enquantoResponde(t, func() {
		m.exigirStatus(m.pedir(celB, http.MethodDelete, rotaAceite, ""), http.StatusNoContent, "revogar no meio")
	})
	rec, _ = m.ditar(celB, id, webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if m.situacao(celB, id).Estado != cloudstore.IdNada {
		t.Fatal("o ditado da conta que revogou no meio foi gravado")
	}
}

// O aceite trocado no meio por um que não cobre o provedor do Polish (a pessoa
// aceitou de novo só com o da transcrição, em outro aparelho): as chaves
// continuam guardadas, e quem barra o texto antes de ele ir ao Polish é a
// conferência do aceite antes do Polish. Nada é gravado.
func testDitarAceiteSemOPolishNoMeio(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "groq", "openai", false)
	m.voz.enquantoResponde(t, func() {
		m.exigirStatus(m.aceitar(celA, versaoDoAviso, "groq", nil), http.StatusOK, "aceitar de novo só a transcrição")
	})
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if m.pol.quantas() != 0 || m.situacao(celA, id).Estado != cloudstore.IdNada {
		t.Fatalf("o aceite sem o Polish no meio: %d chamadas ao Polish, %v", m.pol.quantas(), m.situacao(celA, id).Estado)
	}
	if l, _ := m.b.Chaves.ListarChavesDeIA(context.Background(), m.userA); len(l) != 2 {
		t.Fatalf("o caso não prova nada: a troca do aceite levou as chaves (%d)", len(l))
	}
}

// "Apagar meus dados" durante a chamada ao provedor: nada é gravado. Com o
// aceite de verdade, o apagar leva o aceite; com o dublê (que não está no
// banco), quem barra é o epoch: antes do Polish e dentro do Store.Create.
func testDitarApagarDuranteOProvedor(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	m.voz.enquantoResponde(t, func() { m.exigirStatus(m.apagarMeusDados(celA), http.StatusNoContent, "apagar no meio") })
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if s := m.situacao(celA, id); s.Estado != cloudstore.IdNada {
		t.Fatal("o ditado da conta que apagou os dados no meio foi gravado")
	}

	// Só o epoch: o aceite dublê continua dando o aceite depois do apagar.
	m.voz.enquantoResponde(t, nil)
	m.aceiteDeVerdade = false
	m.montar(ChaveDeCifraA)
	m.aceite.dar(m.userB, "openai")
	m.exigirStatus(m.put(celB, "openai", chaveDe("openai")), http.StatusOK, "PUT da chave")
	for _, polish := range []bool{false, true} {
		m.exigirStatus(m.put(celB, "openai", chaveDe("openai")), http.StatusOK, "PUT da chave")
		prefs := cloudstore.PreferenciasPadrao()
		prefs.PolishLigado, prefs.Polish.Modelo = polish, "modelo-novo"
		if _, err := m.b.Ditado.GravarPreferenciasDoDitado(context.Background(), m.userB, prefs, agora0); err != nil {
			t.Fatal(err)
		}
		m.voz.enquantoResponde(t, func() { m.exigirStatus(m.apagarMeusDados(celB), http.StatusNoContent, "apagar no meio") })
		id := cloudstore.NewUUIDv7()
		rec, _ := m.ditar(celB, id, webm())
		exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
		if s := m.situacao(celB, id); s.Estado != cloudstore.IdNada {
			t.Fatalf("Polish %v: o ditado da conta que apagou os dados foi gravado (epoch)", polish)
		}
		if m.pol.quantas() != 0 {
			t.Fatalf("Polish %v: o Polish foi chamado depois do apagar", polish)
		}
		m.voz.enquantoResponde(t, nil)
	}
}

// "Apagar meus dados" ou "Desligar o ditado" durante a chamada ao provedor: os
// dois esquecem o Smart da conta (montado com o aprendido dela), mas o trabalho
// do ditado leu o aprendido antes e monta o Smart depois do provedor. Ele não
// pode deixá-lo de volta na memória: o ditado acaba em SEM_ACEITE, e nenhuma
// conta fica com o Smart guardado.
func testDitarApagarOuDesligarNoMeioEsqueceOSmart(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	m.aprenderPeloPC("dirfi", "DIRF", nil)
	// Um ditado normal deixa o Smart da conta guardado: é o cache.
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if n := m.cloud.ContasComOSmartNaMemoria(); n != 1 {
		t.Fatalf("depois de um ditado, %d contas com o Smart na memória, quero 1", n)
	}
	m.voz.enquantoResponde(t, func() { m.exigirStatus(m.apagarMeusDados(celA), http.StatusNoContent, "apagar no meio") })
	rec, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if n := m.cloud.ContasComOSmartNaMemoria(); n != 0 {
		t.Fatalf("\"Apagar meus dados\" no meio do ditado: %d contas com o Smart na memória, quero 0", n)
	}

	m.voz.enquantoResponde(t, nil)
	m.prepararDitado(celB, "openai", "", false)
	m.voz.enquantoResponde(t, func() {
		m.exigirStatus(m.pedir(celB, http.MethodDelete, rotaAceite, ""), http.StatusNoContent, "desligar no meio")
	})
	rec, _ = m.ditar(celB, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if n := m.cloud.ContasComOSmartNaMemoria(); n != 0 {
		t.Fatalf("\"Desligar o ditado\" no meio do ditado: %d contas com o Smart na memória, quero 0", n)
	}
}
