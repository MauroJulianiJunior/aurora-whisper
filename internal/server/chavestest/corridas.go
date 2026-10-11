package chavestest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Os casos da C3-correções (ADR-021): o que a revisão da C3a e da C3b deixou.
//
//   - A corrida do PUT da chave: o provedor leva até 10 s para responder, e nesse
//     tempo a pessoa apaga os dados, ou desliga o ditado, em outro aparelho. A
//     chave não pode ser gravada depois (conferência do epoch e do aceite DENTRO
//     da transação que trava a conta).
//   - O GET dos modelos tem efeito colateral (chama o provedor, gasta ficha) e só
//     atende o que o navegador diz ser da própria origem.
//   - O balde do limite é da CONTA: duas sessões dela dividem as 10 fichas.

const rotaApagarMeusDados = "/api/v1/me/data"

// apagarMeusDados é o "apagar meus dados" pela rota, na sessão dada.
func (m *mundo) apagarMeusDados(quem string) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.pedir(quem, http.MethodDelete, rotaApagarMeusDados, `{"confirmar":"apagar"}`)
}

func (m *mundo) exigirStatus(rec *httptest.ResponseRecorder, status int, o string) {
	m.t.Helper()
	if rec.Code != status {
		m.t.Fatalf("%s: %d %s, quero %d", o, rec.Code, rec.Body.String(), status)
	}
}

// semNenhumaChave exige que a conta não tenha chave guardada, de provedor
// nenhum.
func (m *mundo) semNenhumaChave(userID, quando string) {
	m.t.Helper()
	l, err := m.b.Chaves.ListarChavesDeIA(context.Background(), userID)
	if err != nil || len(l) != 0 {
		m.t.Fatalf("%s: a conta tem %d chaves guardadas (%v), quero nenhuma", quando, len(l), err)
	}
}

// linhaBoa é uma linha de chave com os tamanhos que o banco aceita (o conteúdo
// é qualquer coisa: nenhum destes casos decifra).
func linhaBoa(provedor string, b byte) cloudstore.ChaveDeIA {
	return cloudstore.ChaveDeIA{
		Provedor: provedor, IDDaCifra: bytes.Repeat([]byte{b}, 8), Nonce: bytes.Repeat([]byte{b + 1}, 12),
		Cifrado: bytes.Repeat([]byte{b + 2}, 40), Final: "WXYZ",
	}
}

// "Apagar meus dados" durante a chamada ao provedor: o PUT não grava a chave
// (409 CONFLICT, nada guardado), e a conta fica como o apagar a deixou. A chave
// que já havia sai junto, com o resto. Depois, sem o aceite (o apagar o levou),
// o PUT recusa; com o aceite de novo, grava.
func testApagarDuranteOProvedor(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	m.put(celA, "openai", "sk-antiga-"+strings.Repeat("a", 20))
	if _, err := m.linha(m.userA, "openai"); err != nil {
		t.Fatalf("a chave de antes: %v", err)
	}
	chamadasAntes := m.prov.quantas()

	m.prov.enquantoResponde(t, func() {
		m.exigirStatus(m.apagarMeusDados(celA), http.StatusNoContent, "apagar durante o provedor")
	})
	m.clk.Advance(time.Minute)
	rec := m.put(celA, "openai", canario)
	msg := exigirErro(t, rec, http.StatusConflict, "CONFLICT")
	if msg != server.FraseDaContaQueMudou {
		t.Errorf("a frase do 409 = %q, quero a fixa", msg)
	}
	if m.prov.quantas() != chamadasAntes+1 {
		t.Fatalf("o provedor foi chamado %d vezes, quero 1 (a do PUT)", m.prov.quantas()-chamadasAntes)
	}
	m.semNenhumaChave(m.userA, "depois do apagar durante o provedor")
	if !strings.Contains(m.logs.String(), "motivo=conta_mudou") {
		t.Error("o log de acesso não diz o motivo do 409")
	}
	semCanario(t, "a resposta do 409", rec.Body.String(), canario)
	semCanario(t, "o log", m.logs.String(), canario)

	// Sem o aceite (o apagar o levou), o PUT recusa antes do provedor.
	m.prov.enquantoResponde(t, nil)
	m.clk.Advance(time.Minute)
	chamadas := m.prov.quantas()
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	if m.prov.quantas() != chamadas {
		t.Fatal("o PUT sem aceite chamou o provedor")
	}
	// Aceito de novo, o mesmo PUT, sem ninguém apagando no meio, grava.
	m.aceitarOk(celA, "openai", nil)
	m.exigirStatus(m.put(celA, "openai", canario), http.StatusOK, "o PUT depois do aceite novo")
	if k, err := m.linha(m.userA, "openai"); err != nil || k.Final != "WXYZ" {
		t.Fatalf("a chave depois do aceite novo: %+v, %v", k, err)
	}
	// A conta B não foi tocada por nada disto.
	m.semNenhumaChave(m.userB, "a conta B")
}

// "Apagar meus dados" e aceitar de novo, os dois durante a chamada ao provedor:
// o aceite está vigente quando a chave chega, e mesmo assim ela não é gravada,
// porque o epoch da conta já não é o que o PUT leu. É o epoch, e não o aceite,
// que a barra.
func testApagarEAceitarDuranteOProvedor(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	m.prov.enquantoResponde(t, func() {
		m.exigirStatus(m.apagarMeusDados(celA), http.StatusNoContent, "apagar durante o provedor")
		m.aceitarOk(celA, "openai", nil)
	})
	exigirErro(t, m.put(celA, "openai", canario), http.StatusConflict, "CONFLICT")
	m.semNenhumaChave(m.userA, "depois do apagar e do aceite novo durante o provedor")
	if a, err := m.b.Ditado.LerAceiteAberto(context.Background(), m.userA); err != nil || !a.Vigente() {
		t.Fatalf("o aceite novo devia estar em aberto: %+v, %v", a, err)
	}
}

// Desligar o ditado (revogar o aceite, que apaga as chaves) durante a chamada ao
// provedor: o PUT não grava a chave (403 SEM_ACEITE), a de antes também já
// saiu, e o aceite fica revogado como prova.
func testRevogarDuranteOProvedor(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	m.put(celA, "openai", "sk-antiga-"+strings.Repeat("a", 20))
	if _, err := m.linha(m.userA, "openai"); err != nil {
		t.Fatalf("a chave de antes: %v", err)
	}

	m.prov.enquantoResponde(t, func() {
		m.exigirStatus(m.pedir(celA, http.MethodDelete, rotaAceite, ""), http.StatusNoContent, "revogar durante o provedor")
	})
	m.clk.Advance(time.Minute)
	rec := m.put(celA, "openai", canario)
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	m.semNenhumaChave(m.userA, "depois do revogar durante o provedor")
	semCanario(t, "a resposta do 403", rec.Body.String(), canario)
	l := m.aceites(m.userA)
	if len(l) != 1 || l[0].Aberto() || l[0].Motivo != cloudstore.FimRevogado {
		t.Fatalf("o aceite devia estar revogado: %+v", l)
	}
	if !strings.Contains(m.logs.String(), "motivo=sem_aceite") {
		t.Error("o log de acesso não diz o motivo do 403")
	}
}

// Aceitar de novo um aviso que NÃO cobre o provedor da chave durante a chamada
// ao provedor (a transcrição passou da OpenAI para a Groq): o aceite em aberto é
// outro, e o PUT da OpenAI não grava.
func testAceiteTrocadoDuranteOProvedor(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	m.prov.enquantoResponde(t, func() { m.aceitarOk(celA, "groq", nil) })
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	m.semNenhumaChave(m.userA, "depois do aceite trocado durante o provedor")
	// A troca para um aceite que ainda cobre o provedor (a OpenAI no Polish) não
	// atrapalha: a chave é gravada.
	m.aceitarOk(celA, "openai", nil)
	m.prov.enquantoResponde(t, func() { m.aceitarOk(celA, "groq", texto("openai")) })
	m.clk.Advance(time.Minute)
	m.exigirStatus(m.put(celA, "openai", canario), http.StatusOK, "o aceite que ainda cobre o provedor")
}

// A gravação conferida, direto no Store: o epoch esperado (vazio ou velho não
// confere), o aceite sob a trava (ausente, de outro provedor, de versão velha ou
// revogado não confere), o que não é gravado quando recusa, e o que não muda
// quando passa (as datas, a ordem das chaves, o contador de sync).
func testGravacaoConferida(t *testing.T, m *mundo) {
	ctx := context.Background()
	s, d := m.b.Chaves, m.b.Ditado
	nenhuma := func(quando string) {
		t.Helper()
		if _, err := s.LerChaveDeIA(ctx, m.userA, "openai"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
			t.Fatalf("%s: gravou (%v)", quando, err)
		}
	}
	conferir := func(epoch string, exige bool) cloudstore.CondicaoDaChave {
		return cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: exige}
	}
	epoch, err := s.EpochDaConta(ctx, m.userA)
	if err != nil || epoch == "" {
		t.Fatalf("EpochDaConta = %q, %v", epoch, err)
	}
	if outro, _ := s.EpochDaConta(ctx, m.userB); outro == epoch {
		t.Fatal("duas contas com o mesmo epoch")
	}
	for _, u := range []string{contracttest.NewID(), "nao-e-uuid", ""} {
		if _, err := s.EpochDaConta(ctx, u); !errors.Is(err, cloudstore.ErrUnknownUser) {
			t.Errorf("EpochDaConta(%q) = %v, quero ErrUnknownUser", u, err)
		}
		if _, err := s.GravarChaveDeIAConferindo(ctx, u, conferir(epoch, false), linhaBoa("openai", 1), agora0); !errors.Is(err, cloudstore.ErrUnknownUser) {
			t.Errorf("gravar para %q = %v, quero ErrUnknownUser", u, err)
		}
	}

	// O epoch: vazio nunca confere; um velho (qualquer outro), nem o da outra conta.
	outroEpoch, _ := s.EpochDaConta(ctx, m.userB)
	for nome, e := range map[string]string{"vazio": "", "inventado": contracttest.NewID(), "o da outra conta": outroEpoch} {
		if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(e, false), linhaBoa("openai", 1), agora0); !errors.Is(err, cloudstore.ErrEpochChanged) {
			t.Errorf("epoch %s: %v, quero ErrEpochChanged", nome, err)
		}
		nenhuma("com o epoch " + nome)
	}
	// A linha inválida é recusada antes de qualquer conferência.
	feia := linhaBoa("openai", 1)
	feia.Nonce = feia.Nonce[:5]
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir("", true), feia, agora0); !errors.Is(err, cloudstore.ErrChaveDeIAInvalida) {
		t.Errorf("linha inválida: %v, quero ErrChaveDeIAInvalida", err)
	}

	// O aceite sob a trava.
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("openai", 1), agora0); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("sem nenhum aceite: %v, quero ErrSemAceite", err)
	}
	nenhuma("sem aceite")
	aceitar := func(n cloudstore.NovoAceite) {
		t.Helper()
		if _, err := d.GravarAceite(ctx, m.userA, n, agora0); err != nil {
			t.Fatalf("GravarAceite(%+v): %v", n, err)
		}
	}
	aceitar(cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "groq"})
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("openai", 1), agora0); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("aceite só da Groq, chave da OpenAI: %v, quero ErrSemAceite", err)
	}
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("anthropic", 1), agora0); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("aceite só da Groq, chave da Anthropic: %v, quero ErrSemAceite", err)
	}
	aceitar(cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso + 1, Transcricao: "openai"})
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("openai", 1), agora0); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("aceite de outra versão do aviso: %v, quero ErrSemAceite", err)
	}
	nenhuma("com o aceite de outra versão")
	aceitar(cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"})
	got, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("openai", 1), agora0)
	if err != nil || !got.CriadaEm.Equal(agora0) || !got.AtualizadaEm.Equal(agora0) || got.Final != "WXYZ" {
		t.Fatalf("com o aceite que cobre: %+v, %v", got, err)
	}
	// O Polish também cobre (o aceite é do conjunto dos dois provedores).
	aceitar(cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "groq", Polish: "anthropic"})
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("anthropic", 7), agora0); err != nil {
		t.Errorf("o provedor do Polish do aceite: %v", err)
	}
	// Por cima: a data da primeira fica, a troca anda, e a chave de antes é a que
	// o Ler devolve com os bytes novos.
	depois := agora0.Add(time.Hour)
	if got, err = s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, false), linhaBoa("openai", 9), depois); err != nil ||
		!got.CriadaEm.Equal(agora0) || !got.AtualizadaEm.Equal(depois) {
		t.Fatalf("por cima = %+v, %v", got, err)
	}
	if lida, err := s.LerChaveDeIA(ctx, m.userA, "openai"); err != nil || !bytes.Equal(lida.Nonce, linhaBoa("openai", 9).Nonce) {
		t.Fatalf("Ler depois de gravar por cima = %+v, %v", lida, err)
	}
	// A recusa NÃO mexe na chave de antes.
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir("", false), linhaBoa("openai", 20), depois); !errors.Is(err, cloudstore.ErrEpochChanged) {
		t.Fatalf("epoch vazio: %v", err)
	}
	if lida, _ := s.LerChaveDeIA(ctx, m.userA, "openai"); !bytes.Equal(lida.Nonce, linhaBoa("openai", 9).Nonce) {
		t.Fatal("a gravação recusada mudou a chave guardada")
	}
	// Gravar a chave não é uma troca de dados da conta: o epoch é o mesmo.
	if sit, err := m.b.Store.Situacao(ctx, m.userA, contracttest.NewID()); err != nil || sit.Epoch != epoch {
		t.Fatalf("Situacao = %+v, %v; o epoch devia ser o mesmo", sit, err)
	}

	// Revogado, não confere; e o epoch velho depois do apagar também não.
	if _, err := d.RevogarAceite(ctx, m.userA, depois); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, true), linhaBoa("openai", 1), depois); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("aceite revogado: %v, quero ErrSemAceite", err)
	}
	nenhuma("com o aceite revogado")
	aceitar(cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"})
	m.exigirStatus(m.apagarMeusDados(celA), http.StatusNoContent, "apagar")
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(epoch, false), linhaBoa("openai", 1), depois); !errors.Is(err, cloudstore.ErrEpochChanged) {
		t.Errorf("epoch de antes do apagar: %v, quero ErrEpochChanged", err)
	}
	nenhuma("com o epoch de antes do apagar")
	novo, err := s.EpochDaConta(ctx, m.userA)
	if err != nil || novo == epoch {
		t.Fatalf("o epoch depois do apagar = %q (%v), quero outro", novo, err)
	}
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(novo, false), linhaBoa("openai", 1), depois); err != nil {
		t.Errorf("o epoch de agora, sem exigir aceite: %v", err)
	}
	if _, err := s.GravarChaveDeIAConferindo(ctx, m.userA, conferir(novo, true), linhaBoa("groq", 1), depois); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("o apagar levou o aceite: %v, quero ErrSemAceite", err)
	}
	// A outra conta não sentiu nada.
	if e, _ := s.EpochDaConta(ctx, m.userB); e != outroEpoch {
		t.Error("o apagar de A mudou o epoch de B")
	}
}

// Gravar a chave e apagar os dados (ou desligar o ditado) AO MESMO TEMPO, em
// goroutines, muitas vezes: seja qual for a ordem em que a trava cair, a conta
// termina sem chave. Quem grava primeiro é apagado junto com o resto; quem
// grava depois é recusado. O caso determinístico da trava, com o Postgres
// segurando a linha, está em internal/server/pgstore.
func testGravacaoEmCorrida(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	ctx := context.Background()
	for _, outra := range []struct {
		nome  string
		fazer func() *httptest.ResponseRecorder
	}{
		{"apagar meus dados", func() *httptest.ResponseRecorder { return m.apagarMeusDados(celA) }},
		{"desligar o ditado", func() *httptest.ResponseRecorder { return m.pedir(celA, http.MethodDelete, rotaAceite, "") }},
	} {
		for i := range 25 {
			m.aceitarOk(celA, "openai", nil)
			epoch, err := m.b.Chaves.EpochDaConta(ctx, m.userA)
			if err != nil {
				t.Fatal(err)
			}
			var (
				wg     sync.WaitGroup
				errGrv error
				recOut *httptest.ResponseRecorder
			)
			largada := make(chan struct{})
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-largada
				_, errGrv = m.b.Chaves.GravarChaveDeIAConferindo(ctx, m.userA,
					cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: true}, linhaBoa("openai", byte(i)), agora0)
			}()
			go func() {
				defer wg.Done()
				<-largada
				recOut = outra.fazer()
			}()
			close(largada)
			wg.Wait()
			if recOut.Code != http.StatusNoContent {
				t.Fatalf("%s, rodada %d: %d %s", outra.nome, i, recOut.Code, recOut.Body.String())
			}
			if errGrv != nil && !errors.Is(errGrv, cloudstore.ErrEpochChanged) && !errors.Is(errGrv, cloudstore.ErrSemAceite) {
				t.Fatalf("%s, rodada %d: a gravação deu %v", outra.nome, i, errGrv)
			}
			m.semNenhumaChave(m.userA, fmt.Sprintf("%s, rodada %d", outra.nome, i))
		}
	}
}

// O GET dos modelos chama o provedor com a chave da conta e gasta uma ficha do
// limite; por isso só atende o pedido que o navegador diz ser da própria origem
// (Sec-Fetch-Site same-origin), ou que não manda o cabeçalho. O resto é 403,
// antes do aceite, da chave, do limite e do provedor.
func testModelosSoDaMesmaOrigem(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	m.exigirStatus(m.put(celA, "openai", canario), http.StatusOK, "o PUT")
	chamadas, consultasAoAceite := m.prov.quantas(), m.aceite.chamadas

	com := func(site string, mandar bool, method string) *httptest.ResponseRecorder {
		return m.pedirCom(celA, method, modelsOpenAI, "", func(r *http.Request) {
			if mandar {
				r.Header.Set("Sec-Fetch-Site", site)
			} else {
				r.Header.Del("Sec-Fetch-Site")
			}
		})
	}
	// Bem mais que as 10 fichas: se a recusa gastasse ficha, o 200 do fim seria 429.
	for range 3 {
		for _, site := range []string{"cross-site", "same-site", "none", "", "Same-Origin", "same-origin ", "outra-coisa"} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rec := com(site, true, method)
				if msg := exigirErro(t, rec, http.StatusForbidden, "FORBIDDEN"); msg != "pedido de outra origem" {
					t.Errorf("%s com Sec-Fetch-Site %q: %q", method, site, msg)
				}
			}
		}
	}
	// Dois valores no cabeçalho (um só pode ser o do navegador) também recusam.
	rec := m.pedirCom(celA, http.MethodGet, modelsOpenAI, "", func(r *http.Request) {
		r.Header["Sec-Fetch-Site"] = []string{"same-origin", "cross-site"}
	})
	exigirErro(t, rec, http.StatusForbidden, "FORBIDDEN")
	if m.prov.quantas() != chamadas || m.aceite.chamadas != consultasAoAceite {
		t.Fatalf("o pedido de outra origem chegou ao provedor (%d) ou ao aceite (%d)",
			m.prov.quantas()-chamadas, m.aceite.chamadas-consultasAoAceite)
	}
	if !strings.Contains(m.logs.String(), "motivo=origem_cruzada") {
		t.Error("o log de acesso não diz o motivo do 403")
	}

	// A mesma origem, e quem não manda o cabeçalho (um cliente que não é
	// navegador), passam, e ainda há fichas.
	for nome, rec := range map[string]*httptest.ResponseRecorder{
		"same-origin":    com("same-origin", true, http.MethodGet),
		"sem cabeçalho":  com("", false, http.MethodGet),
		"pelo pedir":     m.modelos(celA, "openai"),
		"HEAD da origem": com("same-origin", true, http.MethodHead),
	} {
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s, quero 200", nome, rec.Code, rec.Body.String())
		}
	}
	if m.prov.quantas() != chamadas+4 {
		t.Errorf("o provedor foi chamado %d vezes pelos 4 pedidos que passam", m.prov.quantas()-chamadas)
	}
	// As rotas que mudam estado continuam com a proteção contra CSRF de sempre
	// (http.CrossOriginProtection): a de outro site é 403.
	rec = m.pedirCom(celA, http.MethodPut, rotaOpenAI, corpoDaChave(t, canario), func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	exigirErro(t, rec, http.StatusForbidden, "FORBIDDEN")
	// E o PUT e o DELETE não ficaram mais restritos: sem o cabeçalho, passam.
	rec = m.pedirCom(celA, http.MethodDelete, rotaOpenAI, "", func(r *http.Request) { r.Header.Del("Sec-Fetch-Site") })
	m.exigirStatus(rec, http.StatusNoContent, "DELETE sem Sec-Fetch-Site")
}

// O balde do limite das chaves é da CONTA, e não da sessão: duas sessões da
// mesma conta (dois aparelhos) gastam as mesmas 10 fichas, o PUT e o GET dos
// modelos juntos. O 11º pedido somando as duas é 429 LIMITE, sem chamar o
// provedor. A outra conta tem o balde dela.
func testLimiteDuasSessoesDaMesmaConta(t *testing.T, m *mundo) {
	const celA2 = "cel-a2"
	m.novaSessao(m.userA, celA2)
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	m.aceite.dar(m.userB, cloudstore.ProvedorOpenAI)

	m.exigirStatus(m.put(celA, "openai", canario), http.StatusOK, "o PUT da sessão 1")
	for i := range 9 {
		quem := celA
		if i%2 == 0 {
			quem = celA2
		}
		if rec := m.modelos(quem, "openai"); rec.Code != http.StatusOK {
			t.Fatalf("GET %d (%s): %d %s", i+2, quem, rec.Code, rec.Body.String())
		}
	}
	if m.prov.quantas() != 10 {
		t.Fatalf("%d chamadas ao provedor, quero 10 (somando as duas sessões)", m.prov.quantas())
	}
	for _, quem := range []string{celA, celA2} {
		rec := m.modelos(quem, "openai")
		exigirErro(t, rec, http.StatusTooManyRequests, "LIMITE")
		if ra := rec.Header().Get("Retry-After"); ra == "" || ra == "0" {
			t.Errorf("%s: o 429 sem Retry-After: %q", quem, ra)
		}
		exigirErro(t, m.put(quem, "openai", canario), http.StatusTooManyRequests, "LIMITE")
	}
	if m.prov.quantas() != 10 {
		t.Fatalf("o pedido acima do limite chamou o provedor (%d chamadas)", m.prov.quantas())
	}
	// A outra conta tem o balde dela.
	m.exigirStatus(m.put(celB, "openai", canario), http.StatusOK, "a conta B")
	// E um minuto depois, as duas sessões voltam a poder.
	m.clk.Advance(time.Minute)
	m.exigirStatus(m.modelos(celA2, "openai"), http.StatusOK, "um minuto depois")
}
