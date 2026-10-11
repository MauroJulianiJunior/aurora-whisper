package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As peças puras da rota de ditar (ADR-021, C5b): o formato pelos primeiros
// bytes, os cabeçalhos, a memória do registro (as vagas, os limites, o "sem
// fala", o pago e não gravado, o desligamento), a versão do Smart no cache e a
// tradução das falhas. A rota inteira está na suíte chavestest (Ditar_*).

// cabecalhoWebm é o começo de um webm do MediaRecorder do Chrome.
var cabecalhoWebm = []byte{
	0x1A, 0x45, 0xDF, 0xA3, 0x9F,
	0x42, 0x86, 0x81, 0x01, 0x42, 0xF7, 0x81, 0x01, 0x42, 0xF2, 0x81, 0x04, 0x42, 0xF3, 0x81, 0x08,
	0x42, 0x82, 0x84, 'w', 'e', 'b', 'm',
	0x42, 0x87, 0x81, 0x04, 0x42, 0x85, 0x81, 0x02,
}

func TestPareceWebm(t *testing.T) {
	com := func(b []byte, mudar func([]byte)) []byte {
		c := append([]byte(nil), b...)
		mudar(c)
		return c
	}
	casos := map[string]struct {
		b    []byte
		quer bool
	}{
		"o do Chrome":                {append(append([]byte(nil), cabecalhoWebm...), 0x18, 0x53, 0x80, 0x67), true},
		"só o cabeçalho":             {cabecalhoWebm, true},
		"DocType com zeros no fim":   {[]byte{0x1A, 0x45, 0xDF, 0xA3, 0x8A, 0x42, 0x82, 0x86, 'w', 'e', 'b', 'm', 0, 0}, true},
		"Matroska":                   {com(cabecalhoWebm, func(c []byte) { copy(c[24:], "matr") }), false},
		"sem o EBML":                 {com(cabecalhoWebm, func(c []byte) { c[0] = 0x1B }), false},
		"ogg":                        {[]byte("OggS\x00\x02" + strings.Repeat("x", 60)), false},
		"vazio":                      {nil, false},
		"cortado no DocType":         {cabecalhoWebm[:25], false},
		"DocType com tamanho longo":  {com(cabecalhoWebm, func(c []byte) { c[23] = 0x40 }), false},
		"DocType curto demais":       {com(cabecalhoWebm, func(c []byte) { c[23] = 0x82 }), false},
		"webm só depois de 64 bytes": {append(append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 70)...), 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'), false},
	}
	for nome, c := range casos {
		if got := pareceWebm(c.b); got != c.quer {
			t.Errorf("%s: pareceWebm = %v, quero %v", nome, got, c.quer)
		}
	}
}

func TestIdDoDitado(t *testing.T) {
	v7 := cloudstore.NewUUIDv7()
	casos := map[string]struct {
		valores []string
		ok      bool
	}{
		"v7":              {[]string{v7}, true},
		"v7 maiúsculo":    {[]string{strings.ToUpper(v7)}, true},
		"v4":              {[]string{"3f2a1c4e-8b7d-4e6f-9a1b-2c3d4e5f6a7b"}, false},
		"variante errada": {[]string{v7[:19] + "c" + v7[20:]}, false},
		"sem":             {nil, false},
		"dois":            {[]string{v7, v7}, false},
		"lixo":            {[]string{"abc"}, false},
		"com chaves":      {[]string{"{" + v7 + "}"}, false},
	}
	for nome, c := range casos {
		h := http.Header{}
		for _, v := range c.valores {
			h.Add("Ditado-Id", v)
		}
		id, ok := idDoDitado(h)
		if ok != c.ok || (ok && id != strings.ToLower(c.valores[0])) {
			t.Errorf("%s: %q, %v", nome, id, ok)
		}
	}
}

func TestMedidasDoDitado(t *testing.T) {
	casos := []struct {
		duracao, fala string
		ok            bool
	}{
		{"4000", "2000", true}, {"0", "0", true}, {"4000", "4000", true},
		{"4000", "4001", false}, {"-1", "0", false}, {"+5", "0", false}, {"1e3", "0", false},
		{"", "0", false}, {"4000", "", false}, {"1234567890", "0", false}, {"4000.0", "0", false},
	}
	for _, c := range casos {
		h := http.Header{}
		if c.duracao != "" {
			h.Set("Ditado-Duracao-Ms", c.duracao)
		}
		if c.fala != "" {
			h.Set("Ditado-Fala-Ms", c.fala)
		}
		d, f, ok := medidasDoDitado(h)
		if ok != c.ok {
			t.Errorf("%q/%q: ok %v, quero %v", c.duracao, c.fala, ok, c.ok)
		}
		if ok && (d.Milliseconds() < f.Milliseconds()) {
			t.Errorf("%q/%q: %v %v", c.duracao, c.fala, d, f)
		}
	}
}

func novoRegistroDeTeste(lim limitesDoDitado) (*registroDoDitado, *clock.Fake) {
	clk := clock.NewFake(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	return novoRegistroDoDitado(clk.Now, clk.AfterFunc, lim), clk
}

// As vagas: o mesmo id em andamento é esperado; a conta com outro ditado é
// UM_POR_VEZ; sem vaga no total, OCUPADO; e só quem pega a vaga gasta ficha.
func TestRegistro_VagasEOMesmoId(t *testing.T) {
	g, _ := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 2})
	a1 := chaveDoDitado{"a", "1"}
	dono, esperar, recusa := g.entrar(a1)
	if dono == nil || esperar != nil || recusa != nil {
		t.Fatal("o primeiro não pegou a vaga")
	}
	if _, esperar, _ := g.entrar(a1); esperar != dono {
		t.Fatal("o mesmo id não espera o primeiro")
	}
	if o := g.olhar(a1, "e"); o.esperar != dono {
		t.Fatal("olhar não acha o andamento")
	}
	if _, _, r := g.entrar(chaveDoDitado{"a", "2"}); r == nil || r.code != codeUmPorVez || r.status != 429 || r.espera <= 0 {
		t.Fatalf("a mesma conta: %+v", r)
	}
	// O mesmo id em outra conta é outro ditado.
	b1 := chaveDoDitado{"b", "1"}
	if d, _, _ := g.entrar(b1); d == nil || d == dono {
		t.Fatal("o mesmo id na outra conta não é outro ditado")
	}
	if _, _, r := g.entrar(chaveDoDitado{"c", "1"}); r == nil || r.code != codeOcupado || r.status != 503 {
		t.Fatalf("sem vaga no total: %+v", r)
	}
	g.concluir(a1, dono, desfecho{aviso: "prazo"}, false, nil)
	<-dono.pronto
	if !dono.desfecho.ok() || dono.desfecho.aviso != "prazo" {
		t.Fatalf("o desfecho: %+v", dono.desfecho)
	}
	if d, _, r := g.entrar(chaveDoDitado{"c", "1"}); d == nil || r != nil {
		t.Fatal("a vaga não voltou")
	}
	// As fichas do minuto: a conta a gastou uma; o b, uma; o c, uma.
	if f := g.porMinuto["a"].fichas; f != 9 {
		t.Fatalf("a conta a tem %v fichas, quero 9", f)
	}
}

// Os limites da conta: 10 por minuto e o do dia, com a espera do Retry-After; a
// recusa não gasta ficha nem pega vaga.
func TestRegistro_LimitesDaConta(t *testing.T) {
	g, clk := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 12, total: 3})
	k := func(i int) chaveDoDitado { return chaveDoDitado{"a", string(rune('A' + i))} }
	for i := 0; i < 10; i++ {
		d, _, r := g.entrar(k(i))
		if d == nil || r != nil {
			t.Fatalf("o %dº: %+v", i+1, r)
		}
		g.concluir(k(i), d, desfecho{}, false, nil)
	}
	_, _, r := g.entrar(k(10))
	if r == nil || r.code != codeLimite || r.espera <= 0 || r.espera > 7*time.Second {
		t.Fatalf("o 11º no minuto: %+v", r)
	}
	if g.vagas != 0 || g.vagasPorConta["a"] != 0 {
		t.Fatal("a recusa pegou vaga")
	}
	clk.Advance(6 * time.Second)
	d, _, r := g.entrar(k(10))
	if d == nil || r != nil {
		t.Fatalf("6 s depois: %+v", r)
	}
	g.concluir(k(10), d, desfecho{}, false, nil)
	clk.Advance(time.Minute)
	d, _, _ = g.entrar(k(11))
	g.concluir(k(11), d, desfecho{}, false, nil)
	_, _, r = g.entrar(k(12))
	if r == nil || r.code != codeLimite || r.espera < time.Hour {
		t.Fatalf("o 13º no dia (12 por dia): %+v", r)
	}
}

// O "sem fala" e o pago e não gravado: 10 minutos, por conta e id; o pago só
// vale no mesmo epoch e é esquecido no "Apagar meus dados"; o desligamento não
// começa um reenvio.
func TestRegistro_SemFalaEPagoNaoGravado(t *testing.T) {
	g, clk := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	k := chaveDoDitado{"a", "1"}
	d, _, _ := g.entrar(k)
	g.concluir(k, d, desfecho{status: 422, code: codeSemFala}, true, nil)
	if !g.olhar(k, "e").semFala || g.olhar(chaveDoDitado{"b", "1"}, "e").semFala {
		t.Fatal("o sem fala não ficou, ou vazou para outra conta")
	}
	clk.Advance(10 * time.Minute)
	if g.olhar(k, "e").semFala {
		t.Fatal("o sem fala passou dos 10 minutos")
	}

	novo := cloudstore.NewDictation{SourcePlatform: "web", RawText: "texto", FinalText: "texto", Mode: "smart", ASRProvider: "openai"}
	k2 := chaveDoDitado{"a", "2"}
	d, _, _ = g.entrar(k2)
	g.concluir(k2, d, desfecho{status: 500, code: codeInternal}, false, &pagoNaoGravado{epoch: "e1", novo: novo, aviso: "prazo"})
	if o := g.olhar(k2, "e2"); o.regravar != nil || o.dono != nil {
		t.Fatal("o pago de outro epoch foi regravado")
	}
	if o := g.olhar(k2, "e1"); o.regravar != nil {
		t.Fatal("o pago de outro epoch não foi esquecido")
	}
	d, _, _ = g.entrar(k2)
	g.concluir(k2, d, desfecho{status: 500, code: codeInternal}, false, &pagoNaoGravado{epoch: "e1", novo: novo, aviso: "prazo"})
	o := g.olhar(k2, "e1")
	if o.regravar == nil || o.dono == nil || o.regravar.novo.FinalText != "texto" || o.regravar.aviso != "prazo" || o.dono.vaga {
		t.Fatalf("o pago: %+v", o)
	}
	if _, esperar, _ := g.entrar(k2); esperar != o.dono {
		t.Fatal("o reenvio que regrava não é esperado por outro com o mesmo id")
	}
	g.concluir(k2, o.dono, desfecho{}, false, nil)

	k3 := chaveDoDitado{"a", "3"}
	d, _, _ = g.entrar(k3)
	g.concluir(k3, d, desfecho{status: 500}, true, &pagoNaoGravado{epoch: "e1", novo: novo})
	g.esquecerConta("a")
	if o := g.olhar(k3, "e1"); o.semFala || o.regravar != nil {
		t.Fatal("o Apagar meus dados não esqueceu o que a conta tinha na memória")
	}
	d, _, _ = g.entrar(k3)
	g.concluir(k3, d, desfecho{status: 500}, false, &pagoNaoGravado{epoch: "e1", novo: novo})
	clk.Advance(10*time.Minute + time.Second)
	if o := g.olhar(k3, "e1"); o.regravar != nil {
		t.Fatal("o pago passou dos 10 minutos")
	}
}

// O "sem fala" e o pago e não gravado (que guarda o texto) vencem sozinhos aos
// 10 minutos, sem esperar um pedido novo: num servidor ocioso, o texto não passa
// dos 10 minutos na memória (C5b-revisao). Nenhuma olhada é feita aqui (o
// vencer de dentro dela não conta). Um desfecho novo do mesmo id depois do
// vencido tem o prazo dele: o agendado do antigo não o tira.
func TestRegistro_OSemFalaEOPagoVencemSozinhos(t *testing.T) {
	g, clk := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	novo := cloudstore.NewDictation{SourcePlatform: "web", RawText: "texto", FinalText: "texto", Mode: "smart", ASRProvider: "openai"}
	semFala, pago := chaveDoDitado{"a", "1"}, chaveDoDitado{"b", "1"}
	d, _, _ := g.entrar(semFala)
	g.concluir(semFala, d, desfecho{status: 422, code: codeSemFala}, true, nil)
	clk.Advance(time.Minute)
	d, _, _ = g.entrar(pago)
	g.concluir(pago, d, desfecho{status: 500, code: codeInternal}, false, &pagoNaoGravado{epoch: "e", novo: novo})
	tamanhos := func() (int, int) {
		g.mu.Lock()
		defer g.mu.Unlock()
		return len(g.semFala), len(g.pagos)
	}
	clk.Advance(9*time.Minute - time.Second)
	if s, p := tamanhos(); s != 1 || p != 1 {
		t.Fatalf("antes dos 10 minutos: %d sem fala e %d pagos, quero 1 e 1", s, p)
	}
	clk.Advance(time.Second)
	if s, p := tamanhos(); s != 0 || p != 1 {
		t.Fatalf("aos 10 minutos do sem fala: %d sem fala e %d pagos, quero 0 e 1", s, p)
	}
	clk.Advance(time.Minute)
	if s, p := tamanhos(); s != 0 || p != 0 {
		t.Fatalf("aos 10 minutos do pago: %d sem fala e %d pagos, quero nenhum (o texto não fica num servidor ocioso)", s, p)
	}

	// O vencido sai pela olhada antes do agendado (o relógio do agendado está
	// atrasado), e o mesmo id ganha outro desfecho: o agendado do antigo, que
	// dispara depois, não tira o novo.
	g2, clk2 := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	d, _, _ = g2.entrar(pago)
	g2.concluir(pago, d, desfecho{status: 500, code: codeInternal}, false, &pagoNaoGravado{epoch: "e", novo: novo})
	g2.mu.Lock()
	g2.pagos[pago] = pagoNaoGravado{epoch: "e", novo: novo, expira: clk2.Now().Add(memoriaDoDitado + time.Minute)}
	g2.mu.Unlock()
	clk2.Advance(memoriaDoDitado)
	g2.mu.Lock()
	_, ficou := g2.pagos[pago]
	g2.mu.Unlock()
	if !ficou {
		t.Fatal("o agendado de um desfecho tirou outro do mesmo id, com outro prazo")
	}
}

// O id que ganhou um desfecho na memória (o "sem fala", ou o pago e não
// gravado) entre a olhada do pedido e a vaga dele: o primeiro com o mesmo id
// terminou nesse meio. A vaga não é dada (o provedor seria chamado de novo para
// o mesmo ditado), nenhuma ficha é gasta, e o pedido olha a memória de novo.
func TestRegistro_EntrarDepoisDeUmDesfechoOlhaDeNovo(t *testing.T) {
	novo := cloudstore.NewDictation{SourcePlatform: "web", RawText: "texto", FinalText: "texto", Mode: "smart", ASRProvider: "openai"}
	for nome, c := range map[string]struct {
		semFala bool
		pago    *pagoNaoGravado
	}{
		"o sem fala": {semFala: true},
		"o pago":     {pago: &pagoNaoGravado{epoch: "e", novo: novo}},
	} {
		g, _ := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
		k := chaveDoDitado{"a", "1"}
		if o := g.olhar(k, "e"); o.esperar != nil || o.semFala || o.regravar != nil {
			t.Fatalf("%s: a memória não estava vazia", nome)
		}
		// O primeiro com o mesmo id pega a vaga, chama o provedor e termina
		// enquanto o segundo ia da olhada à vaga.
		d, _, _ := g.entrar(k)
		g.concluir(k, d, desfecho{status: 422, code: codeSemFala}, c.semFala, c.pago)
		fichas := g.porMinuto["a"].fichas
		dono, esperar, recusa := g.entrar(k)
		if dono != nil || recusa != nil || esperar == nil {
			t.Fatalf("%s: o segundo pegou a vaga (dono %v, recusa %+v)", nome, dono != nil, recusa)
		}
		<-esperar.pronto
		if !esperar.desfecho.reprocessar || esperar.desfecho.code != "" {
			t.Fatalf("%s: o segundo não olha de novo: %+v", nome, esperar.desfecho)
		}
		if g.porMinuto["a"].fichas != fichas || g.vagas != 0 || len(g.andamentos) != 0 {
			t.Fatalf("%s: a recusa gastou ficha (%v, era %v) ou pegou vaga (%d)", nome, g.porMinuto["a"].fichas, fichas, g.vagas)
		}
		// E a olhada de novo acha o desfecho.
		o := g.olhar(k, "e")
		if c.semFala && !o.semFala || c.pago != nil && o.regravar == nil {
			t.Fatalf("%s: a olhada de novo não acha o desfecho: %+v", nome, o)
		}
	}
}

// O dono que desiste (a releitura da situação achou o id gravado): a vaga e as
// fichas voltam (a resposta idempotente não conta no limite), quem espera olha
// de novo, e o id sai do andamento.
func TestRegistro_DesistirDevolveAVagaEAsFichas(t *testing.T) {
	g, _ := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	k := chaveDoDitado{"a", "1"}
	dono, _, _ := g.entrar(k)
	_, esperar, _ := g.entrar(k)
	if dono == nil || esperar != dono || g.porMinuto["a"].fichas != 9 || g.porDia["a"].fichas != 199 {
		t.Fatal("o dono não pegou a vaga")
	}
	g.desistir(k, dono)
	<-esperar.pronto
	if !esperar.desfecho.reprocessar || esperar.desfecho.code != "" {
		t.Fatalf("quem espera não olha de novo: %+v", esperar.desfecho)
	}
	if g.vagas != 0 || g.vagasPorConta["a"] != 0 || len(g.andamentos) != 0 {
		t.Fatalf("a vaga não voltou: %d, %d, %d andamentos", g.vagas, g.vagasPorConta["a"], len(g.andamentos))
	}
	if g.porMinuto["a"].fichas != 10 || g.porDia["a"].fichas != 200 {
		t.Fatalf("as fichas não voltaram: %v por minuto, %v por dia", g.porMinuto["a"].fichas, g.porDia["a"].fichas)
	}
	// O reenvio que só regrava (sem vaga) não ganha ficha a mais ao desistir.
	novo := cloudstore.NewDictation{SourcePlatform: "web", RawText: "texto", FinalText: "texto", Mode: "smart", ASRProvider: "openai"}
	d, _, _ := g.entrar(k)
	g.concluir(k, d, desfecho{status: 500, code: codeInternal}, false, &pagoNaoGravado{epoch: "e", novo: novo})
	o := g.olhar(k, "e")
	g.desistir(k, o.dono)
	if g.porMinuto["a"].fichas != 9 || g.porDia["a"].fichas != 199 {
		t.Fatalf("o regravar que desiste devolveu ficha: %v, %v", g.porMinuto["a"].fichas, g.porDia["a"].fichas)
	}
}

// A guarda do dono (o defer do handler): o pânico entre pegar a vaga e soltar o
// trabalho encerra o andamento como o corpo que não chegou (a vaga volta, a
// ficha não, quem espera olha de novo); o pânico de quem ia só regravar o pago
// devolve o pago à memória com o prazo que ele tinha; e o andamento entregue ao
// trabalho (largar) não é tocado.
func TestRegistro_GuardaDoDono(t *testing.T) {
	entrarEmPanico := func(guarda *guardaDoDono) {
		defer func() {
			if recover() == nil {
				t.Fatal("o pânico de mentira não aconteceu")
			}
		}()
		defer guarda.soltar()
		panic("defeito de mentira")
	}
	g, clk := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	k := chaveDoDitado{"a", "1"}
	dono, _, _ := g.entrar(k)
	_, esperar, _ := g.entrar(k)
	guarda := guardaDoDono{g: g}
	guarda.pegar(k, dono, nil)
	entrarEmPanico(&guarda)
	<-esperar.pronto
	if !esperar.desfecho.reprocessar || g.vagas != 0 || g.vagasPorConta["a"] != 0 || len(g.andamentos) != 0 {
		t.Fatalf("o pânico do dono: %+v, %d vagas, %d andamentos", esperar.desfecho, g.vagas, len(g.andamentos))
	}
	if g.porMinuto["a"].fichas != 9 {
		t.Fatalf("a ficha gasta voltou: %v", g.porMinuto["a"].fichas)
	}
	guarda.soltar() // de novo, sem andamento: nada
	if d, _, r := g.entrar(chaveDoDitado{"a", "2"}); d == nil || r != nil {
		t.Fatalf("a conta não pega a vaga depois do pânico: %+v", r)
	}

	// Quem ia só regravar o pago.
	g, clk = novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	novo := cloudstore.NewDictation{SourcePlatform: "web", RawText: "texto", FinalText: "texto", Mode: "smart", ASRProvider: "openai"}
	d, _, _ := g.entrar(k)
	g.concluir(k, d, desfecho{status: 500, code: codeInternal}, false, &pagoNaoGravado{epoch: "e", novo: novo, aviso: "prazo"})
	clk.Advance(4 * time.Minute)
	o := g.olhar(k, "e")
	_, esperar, _ = g.entrar(k)
	if o.regravar == nil || esperar != o.dono {
		t.Fatalf("o regravar: %+v", o)
	}
	guarda = guardaDoDono{g: g}
	guarda.pegar(k, o.dono, o.regravar)
	entrarEmPanico(&guarda)
	<-esperar.pronto
	if !esperar.desfecho.reprocessar || len(g.andamentos) != 0 {
		t.Fatalf("o pânico de quem regravava: %+v, %d andamentos", esperar.desfecho, len(g.andamentos))
	}
	if de := g.olhar(k, "e"); de.regravar == nil || de.regravar.novo.FinalText != "texto" || de.regravar.aviso != "prazo" {
		t.Fatalf("o pago não voltou à memória: %+v", de)
	} else {
		guarda.pegar(k, de.dono, de.regravar)
		entrarEmPanico(&guarda)
	}
	clk.Advance(6 * time.Minute)
	g.mu.Lock()
	n := len(g.pagos)
	g.mu.Unlock()
	if n != 0 {
		t.Fatal("o pago que voltou à memória ganhou prazo novo: passou dos 10 minutos do primeiro")
	}

	// O andamento entregue ao trabalho: a guarda o larga, e o trabalho o encerra.
	g, _ = novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	dono, _, _ = g.entrar(k)
	guarda = guardaDoDono{g: g}
	guarda.pegar(k, dono, nil)
	guarda.largar()
	guarda.soltar()
	if g.vagas != 1 || len(g.andamentos) != 1 {
		t.Fatalf("a guarda mexeu no andamento entregue ao trabalho: %d vagas, %d andamentos", g.vagas, len(g.andamentos))
	}
	g.concluir(k, dono, desfecho{}, false, nil)
}

// O desligamento: espera os trabalhos soltos, e nenhum ditado novo pega vaga
// nem começa um reenvio do pago.
func TestRegistro_Desligamento(t *testing.T) {
	g, _ := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	if err := g.esperarTrabalhos(context.Background()); err != nil {
		t.Fatal(err)
	}
	g2, _ := novoRegistroDeTeste(limitesDoDitado{porConta: 1, porMinuto: 10, porDia: 200, total: 3})
	k := chaveDoDitado{"a", "1"}
	d, _, _ := g2.entrar(k)
	g2.concluir(k, d, desfecho{status: 500}, false, &pagoNaoGravado{epoch: "e"})
	g2.comecarTrabalho()
	pronto := make(chan error, 1)
	go func() { pronto <- g2.esperarTrabalhos(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-pronto:
		t.Fatal("o desligamento não esperou o trabalho")
	default:
	}
	if _, _, r := g2.entrar(chaveDoDitado{"b", "1"}); r == nil || r.code != codeOcupado {
		t.Fatalf("um ditado novo pegou vaga no desligamento: %+v", r)
	}
	if o := g2.olhar(k, "e"); o.regravar != nil {
		t.Fatal("o desligamento começou um reenvio")
	}
	g2.terminarTrabalho()
	if err := <-pronto; err != nil {
		t.Fatal(err)
	}
	g2.comecarTrabalho()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := g2.esperarTrabalhos(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("o prazo da espera: %v", err)
	}
}

// A versão do Smart no cache: os mesmos pares, a mesma versão; qualquer mudança,
// outra.
func TestVersaoDosPares(t *testing.T) {
	base := []dictionary.ParDaNuvem{{Variante: "dirfi", Termo: "DIRF"}, {Variante: "rague", Termo: "RAG"}}
	v := versaoDosPares(base)
	if versaoDosPares(append([]dictionary.ParDaNuvem(nil), base...)) != v {
		t.Fatal("os mesmos pares deram outra versão")
	}
	for nome, mudar := range map[string]func([]dictionary.ParDaNuvem) []dictionary.ParDaNuvem{
		"um par a mais": func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem {
			return append(p, dictionary.ParDaNuvem{Variante: "x", Termo: "X"})
		},
		"um a menos":      func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem { return p[:1] },
		"o termo trocado": func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem { p[0].Termo = "Dirf"; return p },
		"desfeito":        func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem { p[1].Desfeito = true; return p },
		"restrito":        func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem { p[1].Restrito = true; return p },
		"com domínio":     func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem { p[1].Dominio = "contabil"; return p },
		"outra ordem":     func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem { p[0], p[1] = p[1], p[0]; return p },
		"a fronteira": func(p []dictionary.ParDaNuvem) []dictionary.ParDaNuvem {
			p[0].Variante, p[0].Termo = "dirfiD", "IRF"
			return p
		},
	} {
		if versaoDosPares(mudar(append([]dictionary.ParDaNuvem(nil), base...))) == v {
			t.Errorf("%s: a mesma versão", nome)
		}
	}
	if strings.Contains(v, "dirfi") || len(v) != 64 {
		t.Fatalf("a versão %q", v)
	}
}

// A tradução da falha do provedor (a mesma das chaves e da rota de ditar): o
// *ia.Erro da lista de modelos e o *nuvem.Erro da transcrição.
func TestFalhaDoProvedor_DaTranscricao(t *testing.T) {
	casos := []struct {
		err    error
		status int
		code   string
	}{
		{&nuvem.Erro{Motivo: nuvem.MotivoChaveRecusada}, 422, codeProvedorRecusouChave},
		{&nuvem.Erro{Motivo: nuvem.MotivoSemPermissao}, 422, codeProvedorRecusouChave},
		{&nuvem.Erro{Motivo: nuvem.MotivoLimite}, 502, codeProvedorLimite},
		{&nuvem.Erro{Motivo: nuvem.MotivoSemChave}, 409, codeSemChave},
		{&nuvem.Erro{Motivo: nuvem.MotivoArquivoGrande}, 502, codeProvedorFora},
		{&nuvem.Erro{Motivo: nuvem.MotivoIndisponivel}, 502, codeProvedorFora},
		{&nuvem.Erro{Motivo: nuvem.MotivoTempoEsgotado}, 502, codeProvedorFora},
		{&nuvem.Erro{Motivo: nuvem.MotivoSemRede}, 502, codeProvedorFora},
		{&nuvem.Erro{Motivo: nuvem.MotivoRespostaInesperada}, 502, codeProvedorFora},
		{&ia.Erro{Motivo: ia.MotivoChaveRecusada}, 422, codeProvedorRecusouChave},
		{&ia.Erro{Motivo: ia.MotivoLimite}, 502, codeProvedorLimite},
		{errors.New("qualquer"), 502, codeProvedorFora},
	}
	for _, c := range casos {
		status, code, frase := FalhaDoProvedor(c.err)
		if status != c.status || code != c.code || frase == "" {
			t.Errorf("%v: %d %s, quero %d %s", c.err, status, code, c.status, c.code)
		}
	}
	for err, quer := range map[error]resolver.MotivoFallback{
		&ia.Erro{Motivo: ia.MotivoChaveRecusada}: resolver.MotivoChaveRecusada,
		&ia.Erro{Motivo: ia.MotivoSemPermissao}:  resolver.MotivoChaveRecusada,
		&ia.Erro{Motivo: ia.MotivoLimite}:        resolver.MotivoLimite,
		&ia.Erro{Motivo: ia.MotivoTempoEsgotado}: resolver.MotivoPrazo,
		context.DeadlineExceeded:                 resolver.MotivoPrazo,
		errors.New("rede"):                       resolver.MotivoTransporte,
	} {
		if got := motivoDaListaDeModelos(err); got != quer {
			t.Errorf("%v: %s, quero %s", err, got, quer)
		}
	}
}

// Os modelos conhecidos: só os que um provedor da lista listou, com a forma de
// um nome de modelo, até o teto; os padrões da transcrição desde a subida; e o
// log escreve "outro" no lugar do que não é conhecido.
func TestModelosConhecidos(t *testing.T) {
	m := novosModelosConhecidos()
	if !m.conhece(cloudstore.ProvedorOpenAI, cloudstore.ModeloPadraoOpenAI) || !m.conhece(cloudstore.ProvedorGroq, cloudstore.ModeloPadraoGroq) {
		t.Fatal("os padrões da transcrição não são conhecidos")
	}
	m.anotar(cloudstore.ProvedorAnthropic, []string{"claude-x", "nome com espaço", strings.Repeat("a", 101)})
	m.anotar("xai", []string{"grok"})
	if !m.conhece("anthropic", "claude-x") || m.conhece("anthropic", "nome com espaço") || m.conhece("xai", "grok") ||
		m.conhece("openai", "claude-x") {
		t.Fatal("os conhecidos não são os listados, por provedor")
	}
	if m.paraOLog("anthropic", "claude-x") != "claude-x" || m.paraOLog("anthropic", "inventado") != "outro" || m.paraOLog("anthropic", "") != "outro" {
		t.Fatal("o log escreve o nome que o provedor não listou")
	}
	muitos := make([]string, 0, maxModelosConhecidos+10)
	for i := 0; i < maxModelosConhecidos+10; i++ {
		muitos = append(muitos, "m-"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+"-"+time.Duration(i).String())
	}
	m.anotar("openai", muitos)
	if n := len(m.porProvedor["openai"]); n > maxModelosConhecidos {
		t.Fatalf("%d modelos conhecidos, acima do teto", n)
	}
}

// Os prazos: zero é o padrão da ADR, e o que vier vale.
func TestPrazosDoDitado(t *testing.T) {
	p := PrazosDoDitado{}.comPadroes()
	if p != PrazosDoDitadoPadrao || p.Corpo != 30*time.Second || p.Transcricao != 20*time.Second ||
		p.Gravacao != 5*time.Second || p.Resposta != 45*time.Second || p.Polish != 0 {
		t.Fatalf("os padrões: %+v", p)
	}
	if q := (PrazosDoDitado{Corpo: time.Second}).comPadroes(); q.Corpo != time.Second || q.Transcricao != 20*time.Second {
		t.Fatalf("o que vier vale: %+v", q)
	}
	// O caminho da ADR (a transcrição, o Polish mais longo, de 8 s, e a
	// gravação: 30 + 20 + 8 + 5 contando o corpo) cabe no prazo da resposta, com
	// a folga de escrevê-la; o que passar disso é cortado pelo teto do trabalho.
	if soma := p.Transcricao + 8*time.Second + p.Gravacao + folgaDaResposta; soma >= p.Resposta {
		t.Fatalf("o trabalho do ditado (%v) não cabe no prazo da resposta (%v)", soma, p.Resposta)
	}
}
