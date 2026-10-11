package chavestest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Run roda a suíte das chaves da IA contra o Backend. Cada caso monta o seu
// mundo, com as suas contas.
func Run(t *testing.T, b Backend) {
	cases := []struct {
		name string
		fn   func(t *testing.T, m *mundo)
	}{
		{"Chaves_GravaCifradaEDevolveSoOFinal", testGravaCifradaEDevolveSoOFinal},
		{"Chaves_CanarioNuncaEmRespostaLogErroNemBanco", testCanario},
		{"Chaves_CopiadaParaOutraContaOuProvedorNaoDecifra", testCopiadaNaoDecifra},
		{"Chaves_TrocarAChaveDeCifraLevaASemChave", testTrocarAChaveDeCifra},
		{"Chaves_SemAceiteRecusaSemChamarOProvedor", testSemAceite},
		{"Chaves_LimitePorContaJuntaOPutEOsModelos", testLimitePorConta},
		{"Chaves_SoComSessao", testSoComSessao},
		{"Chaves_ProvedorForaDaListaDa404", testProvedorForaDaLista},
		{"Chaves_FalhasDoProvedorViramCodigos", testFalhasDoProvedor},
		{"Chaves_ApagarMeusDadosLevaAsChaves", testApagarMeusDados},
		{"Chaves_ValidarComoOPainelDoPC", testValidar},
		{"Chaves_SemOArquivoAsRotasDao404", testSemOArquivo},
		{"Chaves_OStoreGuardaLeListaEApaga", testStore},

		// O aceite e as preferências do ditado no celular (C3b; ditado.go).
		{"Ditado_EstadoInicialSemAceiteComOsPadroes", testEstadoInicial},
		{"Ditado_AceiteGravaAVersaoEOsProvedoresELigaOPutDasChaves", testAceiteGravaELigaAsChaves},
		{"Ditado_AceiteSoComAVersaoAtual", testAceiteSoComAVersaoAtual},
		{"Ditado_AceiteNovoSubstituiOAnterior", testAceiteNovoSubstitui},
		{"Ditado_AvisoNovoPedeAceiteNovo", testAvisoNovoPedeAceiteNovo},
		{"Ditado_PreferenciasRecusamProvedorForaDoAceite", testPreferenciasForaDoAceite},
		{"Ditado_PreferenciasValidam", testPreferenciasValidam},
		{"Ditado_RevogarEncerraOAceiteEApagaAsChaves", testRevogar},
		{"Ditado_ChaveQueNaoValeMaisPedeColarDeNovo", testChaveQueNaoVale},
		{"Ditado_ApagarMeusDadosApagaAsTresTabelas", testApagarAsTres},
		{"Ditado_SoComSessao", testDitadoSoComSessao},
		{"Ditado_SemOArquivoAsRotasDao404", testDitadoSemOArquivo},
		{"Ditado_OStoreGuardaLeELista", testDitadoStore},
		{"Ditado_GravacoesAoMesmoTempoNaoDeixamDoisAceitesEmAberto", testDitadoConcorrencia},

		// As correções da C3a e da C3b (C3-correções; corridas.go): a chave não
		// sobrevive a um apagar nem a um revogar que terminam durante a chamada ao
		// provedor, o GET dos modelos só atende a própria origem, e o limite é da conta.
		{"Correcoes_ApagarDuranteOProvedorNaoGravaAChave", testApagarDuranteOProvedor},
		{"Correcoes_ApagarEAceitarDeNovoDuranteOProvedorNaoGravaAChave", testApagarEAceitarDuranteOProvedor},
		{"Correcoes_RevogarDuranteOProvedorNaoGravaAChave", testRevogarDuranteOProvedor},
		{"Correcoes_AceiteTrocadoDuranteOProvedorNaoGravaAChave", testAceiteTrocadoDuranteOProvedor},
		{"Correcoes_GravacaoConferidaNoStore", testGravacaoConferida},
		{"Correcoes_GravarEApagarOuRevogarAoMesmoTempoNaoDeixamChave", testGravacaoEmCorrida},
		{"Correcoes_ModelosSoDaMesmaOrigem", testModelosSoDaMesmaOrigem},
		{"Correcoes_DuasSessoesDaMesmaContaDividemOLimite", testLimiteDuasSessoesDaMesmaConta},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, novoMundo(t, b)) })
	}
}

// canario é a chave de mentira que a suíte procura em todo lugar (resposta,
// log, banco). Montada por partes, para o arquivo não parecer uma chave.
var canario = "sk-" + "canario-c3a-" + strings.Repeat("Zx9", 12) + "WXYZ"

// pedacosDoCanario são os trechos que não podem aparecer em lugar nenhum: a
// chave inteira, o meio dela (um pedaço também é vazamento) e os mesmos em
// hex (o bytea do Postgres sai em hex no texto).
func pedacosDoCanario(c string) []string {
	meio := c[3 : len(c)-4]
	return []string{c, meio, hex.EncodeToString([]byte(c)), hex.EncodeToString([]byte(meio))}
}

func semCanario(t *testing.T, onde, texto string, c string) {
	t.Helper()
	for _, p := range pedacosDoCanario(c) {
		if strings.Contains(texto, p) || strings.Contains(strings.ToLower(texto), strings.ToLower(p)) {
			t.Errorf("a chave (ou um pedaço dela) aparece em %s", onde)
			return
		}
	}
}

const (
	rotaOpenAI   = "/api/v1/ai-keys/openai"
	modelsOpenAI = "/api/v1/ai-keys/openai/models"
)

// A chave colada vai ao provedor (limpa das pontas), é guardada só cifrada,
// pelo cofre desta chave de cifra, e o app recebe só o fim dela. A lista de
// modelos usa a chave guardada; trocar grava por cima; apagar tira.
func testGravaCifradaEDevolveSoOFinal(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	rec := m.put(celA, "openai", "  "+canario+"\r\n")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if dto := decodificar[chaveDTO](t, rec); dto != (chaveDTO{Provedor: "openai", Configurada: true, Final: "WXYZ"}) {
		t.Fatalf("PUT devolveu %+v, quero só o provedor, configurada e o fim", dto)
	}
	semCanario(t, "a resposta do PUT", rec.Body.String(), canario)
	if c := m.prov.ultima(); m.prov.quantas() != 1 || c != (chamada{"openai", canario}) {
		t.Fatalf("o provedor recebeu %+v (%d chamadas), quero a chave limpa, uma vez", c, m.prov.quantas())
	}

	k, err := m.linha(m.userA, "openai")
	if err != nil {
		t.Fatalf("a linha: %v", err)
	}
	if k.Final != "WXYZ" || !bytes.Equal(k.IDDaCifra, m.cofre.ID()) || len(k.Nonce) != cofre.TamanhoDoNonce ||
		len(k.Cifrado) != len(canario)+cofre.TamanhoDaEtiqueta || !k.CriadaEm.Equal(agora0) || !k.AtualizadaEm.Equal(agora0) {
		t.Fatalf("a linha guardada: final %q, id %x, nonce %d, cifrado %d, datas %v %v", k.Final, k.IDDaCifra, len(k.Nonce),
			len(k.Cifrado), k.CriadaEm, k.AtualizadaEm)
	}
	semCanario(t, "o texto cifrado", string(k.Cifrado), canario)
	if claro, err := m.cofre.Abrir(m.userA, "openai", cofre.Selada{IDDaCifra: k.IDDaCifra, Nonce: k.Nonce, Cifrado: k.Cifrado}); err != nil || claro != canario {
		t.Fatalf("a linha não decifra para a chave colada: %v", err)
	}

	rec = m.modelos(celA, "openai")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET models: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodificar[modelosDTO](t, rec); got.Provedor != "openai" || !slices.Equal(got.Modelos, []string{"modelo-novo", "modelo-velho"}) {
		t.Fatalf("GET models = %+v", got)
	}
	if c := m.prov.ultima(); m.prov.quantas() != 2 || c != (chamada{"openai", canario}) {
		t.Fatalf("a lista não usou a chave guardada: %+v", c)
	}
	if l, err := m.b.Chaves.ListarChavesDeIA(context.Background(), m.userA); err != nil || len(l) != 1 || l[0].Provedor != "openai" {
		t.Fatalf("Listar = %+v, %v", l, err)
	}

	// Trocar: grava por cima, com o fim novo; a data da primeira fica.
	m.clk.Advance(time.Hour)
	outra := "sk-" + "outra-chave-" + strings.Repeat("q", 20) + "AB12"
	if rec := m.put(celA, "openai", outra); rec.Code != http.StatusOK || decodificar[chaveDTO](t, rec).Final != "AB12" {
		t.Fatalf("trocar: %d %s", rec.Code, rec.Body.String())
	}
	if k2, err := m.linha(m.userA, "openai"); err != nil || k2.Final != "AB12" || !k2.CriadaEm.Equal(agora0) ||
		!k2.AtualizadaEm.Equal(agora0.Add(time.Hour)) || bytes.Equal(k2.Nonce, k.Nonce) {
		t.Fatalf("depois de trocar: %+v, %v", k2, err)
	}
	if rec := m.modelos(celA, "openai"); rec.Code != http.StatusOK || m.prov.ultima().chave != outra {
		t.Fatalf("a lista não usou a chave nova: %d", rec.Code)
	}

	// Apagar: 204, a linha sai, e a lista vira SEM_CHAVE sem chamar o
	// provedor. Apagar de novo também é 204.
	antes := m.prov.quantas()
	for range 2 {
		if rec := m.apagar(celA, "openai"); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("DELETE: %d %s", rec.Code, rec.Body.String())
		}
	}
	if _, err := m.linha(m.userA, "openai"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
		t.Fatalf("a linha ficou depois de apagar: %v", err)
	}
	exigirErro(t, m.modelos(celA, "openai"), http.StatusConflict, "SEM_CHAVE")
	if m.prov.quantas() != antes {
		t.Fatal("a lista sem chave chamou o provedor")
	}
	// O log de acesso tem só o padrão da rota.
	if !strings.Contains(m.logs.String(), "PUT /api/v1/ai-keys/{provedor}") {
		t.Errorf("o log de acesso não tem o padrão da rota:\n%s", m.logs.String())
	}
}

// corposQuebrados são JSONs que levam a chave e que o decodificador recusa.
// Em vários, a mensagem dele traria a chave (o campo desconhecido, o
// caractere inválido): a resposta é sempre a frase fixa.
func corposQuebrados(c string) map[string]string {
	return map[string]string{
		"string sem fim":               `{"chave":"` + c,
		"a chave como campo":           `{"` + c + `":"x"}`,
		"vírgula sobrando":             `{"chave":"` + c + `",}`,
		"a chave crua":                 c,
		"a chave sem aspas":            `{"chave":` + c + `}`,
		"lista no lugar do texto":      `{"chave":["` + c + `"]}`,
		"caractere de controle":        `{"chave":"` + c + "\x01" + `"}`,
		"escape inválido":              `{"chave":"` + c + `\x"}`,
		"sem fechar o objeto":          `{"chave":"` + c + `"`,
		"campo desconhecido depois":    `{"chave":"` + c + `","outro":"` + c + `"}`,
		"dois objetos":                 `{"chave":"` + c + `"} {"chave":"` + c + `"}`,
		"lixo depois":                  `{"chave":"` + c + `"} ` + c,
		"número no lugar":              `{"chave":1` + c + `}`,
		"o nome do campo em maiúscula": `{"CHAVE":"` + c + `","` + c + `":1}`,
	}
}

// A chave nunca aparece em resposta, log, erro nem em claro no banco: nos
// caminhos felizes, no JSON quebrado que a leva, na validação, e no provedor
// que a devolve no erro dele.
func testCanario(t *testing.T, m *mundo) {
	global := capturarLogs(t)
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI, cloudstore.ProvedorGroq, cloudstore.ProvedorAnthropic)
	var respostas strings.Builder
	anotar := func(rec *httptest.ResponseRecorder, corpo string) {
		for k, v := range rec.Header() {
			respostas.WriteString(k + ": " + strings.Join(v, ",") + "\n")
		}
		respostas.WriteString(corpo + "\n")
	}

	// O caminho feliz.
	rec := m.put(celA, "openai", canario)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	anotar(rec, rec.Body.String())
	rec = m.modelos(celA, "openai")
	anotar(rec, rec.Body.String())

	// O JSON quebrado que leva a chave: a frase fixa, sempre, sem chamar o
	// provedor e sem gravar.
	chamadas := m.prov.quantas()
	for nome, corpo := range corposQuebrados(canario) {
		for _, p := range []string{"groq", "anthropic"} {
			rec := m.pedir(celA, http.MethodPut, "/api/v1/ai-keys/"+p, corpo)
			if msg := exigirErro(t, rec, http.StatusBadRequest, "BAD_REQUEST"); msg != server.FraseDoCorpoDaChave {
				t.Errorf("%s: a frase é %q, quero a fixa", nome, msg)
			}
			anotar(rec, rec.Body.String())
			if _, err := m.linha(m.userA, p); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
				t.Errorf("%s: gravou a chave do JSON quebrado", nome)
			}
		}
	}
	if m.prov.quantas() != chamadas {
		t.Error("um JSON quebrado chamou o provedor")
	}
	// O teste vale: com a mensagem do decodificador (o que o decodeJSON comum
	// faria), a chave sairia.
	var v struct {
		Chave string `json:"chave"`
	}
	dec := json.NewDecoder(strings.NewReader(corposQuebrados(canario)["a chave como campo"]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err == nil || !strings.Contains(err.Error(), canario) {
		t.Fatalf("o decodificador não traria a chave na mensagem (%v): o caso do campo não prova nada", err)
	}

	// O corpo grande demais, cheio da chave.
	grande := `{"chave":"` + strings.Repeat(canario, (17<<10)/len(canario)+1) + `"}`
	rec = m.pedir(celA, http.MethodPut, "/api/v1/ai-keys/groq", grande)
	if msg := exigirErro(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"); msg != server.FraseDoCorpoGrande {
		t.Errorf("corpo grande: %q", msg)
	}
	anotar(rec, rec.Body.String())

	// A validação: a chave com espaço no meio.
	rec = m.put(celA, "groq", canario+" "+canario)
	exigirErro(t, rec, http.StatusBadRequest, "VALIDATION")
	anotar(rec, rec.Body.String())

	// O provedor que devolve a chave no erro, nas duas formas: o *ia.Erro
	// (com o motivo) e um erro qualquer.
	m.prov.falharCom(func(chave string) error {
		return &ia.Erro{Motivo: ia.MotivoChaveRecusada, Mensagem: "a chave " + chave + " foi recusada"}
	})
	rec = m.put(celA, "groq", canario)
	exigirErro(t, rec, http.StatusUnprocessableEntity, "PROVEDOR_RECUSOU_CHAVE")
	anotar(rec, rec.Body.String())
	rec = m.modelos(celA, "openai")
	exigirErro(t, rec, http.StatusUnprocessableEntity, "PROVEDOR_RECUSOU_CHAVE")
	anotar(rec, rec.Body.String())
	m.prov.falharCom(func(chave string) error { return fmt.Errorf("401 Incorrect API key provided: %s", chave) })
	m.clk.Advance(time.Minute)
	rec = m.put(celA, "anthropic", canario)
	exigirErro(t, rec, http.StatusBadGateway, "PROVEDOR_FORA")
	anotar(rec, rec.Body.String())
	rec = m.modelos(celA, "openai")
	exigirErro(t, rec, http.StatusBadGateway, "PROVEDOR_FORA")
	anotar(rec, rec.Body.String())

	semCanario(t, "as respostas", respostas.String(), canario)
	semCanario(t, "o log do servidor", m.logs.String(), canario)
	semCanario(t, "o log padrão e o slog padrão", global.String(), canario)
	despejo := m.b.Despejo(t)
	semCanario(t, "o banco", despejo, canario)
	// O despejo vale: a linha da chave está nele (o fim, que não é segredo).
	if !strings.Contains(despejo, "WXYZ") {
		t.Fatal("o despejo não tem a linha da chave: a varredura do banco não prova nada")
	}
}

// capturarLogs troca o log padrão e o slog padrão por um buffer até o fim do
// caso: nada do servidor pode escrever a chave por fora do logger dele.
func capturarLogs(t *testing.T) *contracttest.LockedBuffer {
	t.Helper()
	buf := &contracttest.LockedBuffer{}
	prevOut, prevFlags, prevSlog := log.Writer(), log.Flags(), slog.Default()
	log.SetOutput(buf)
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})
	return buf
}

// O texto cifrado copiado para outra conta ou para outro provedor (como um
// UPDATE no banco faria) não decifra: SEM_CHAVE, sem chamar o provedor. A
// linha de origem continua valendo.
func testCopiadaNaoDecifra(t *testing.T, m *mundo) {
	ctx := context.Background()
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI, cloudstore.ProvedorGroq)
	m.aceite.dar(m.userB, cloudstore.ProvedorOpenAI)
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	k, err := m.linha(m.userA, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.b.Chaves.GravarChaveDeIA(ctx, m.userB, k, agora0); err != nil {
		t.Fatalf("copiar para B: %v", err)
	}
	paraGroq := k
	paraGroq.Provedor = cloudstore.ProvedorGroq
	if _, err := m.b.Chaves.GravarChaveDeIA(ctx, m.userA, paraGroq, agora0); err != nil {
		t.Fatalf("copiar para o groq: %v", err)
	}
	chamadas := m.prov.quantas()
	exigirErro(t, m.modelos(celB, "openai"), http.StatusConflict, "SEM_CHAVE")
	exigirErro(t, m.pedir(celA, http.MethodGet, "/api/v1/ai-keys/groq/models", ""), http.StatusConflict, "SEM_CHAVE")
	if m.prov.quantas() != chamadas {
		t.Fatal("a chave copiada chegou ao provedor")
	}
	for _, c := range []struct{ user, prov string }{{m.userB, "openai"}, {m.userA, "groq"}} {
		if _, err := m.cofre.Abrir(c.user, c.prov, cofre.Selada{IDDaCifra: k.IDDaCifra, Nonce: k.Nonce, Cifrado: k.Cifrado}); !errors.Is(err, cofre.ErrNaoDecifra) {
			t.Errorf("Abrir(%s, %s) da cópia: %v, quero ErrNaoDecifra", c.user, c.prov, err)
		}
	}
	if rec := m.modelos(celA, "openai"); rec.Code != http.StatusOK || m.prov.ultima().chave != canario {
		t.Fatalf("a linha de origem deixou de valer: %d %s", rec.Code, rec.Body.String())
	}
}

// Trocar o arquivo da chave de cifra (o servidor reiniciado com outro
// AI_KEYS_SECRET_FILE, ou o backup restaurado sem o mesmo) leva a chave
// guardada a SEM_CHAVE, sem 500 e sem chamar o provedor; colar de novo volta
// a valer, e a chave de cifra antiga deixa de abrir a nova.
func testTrocarAChaveDeCifra(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d", rec.Code)
	}
	idAntigo := m.cofre.ID()
	m.montar(ChaveDeCifraB)
	chamadas := m.prov.quantas()
	msg := exigirErro(t, m.modelos(celA, "openai"), http.StatusConflict, "SEM_CHAVE")
	if !strings.Contains(msg, "cole") {
		t.Errorf("a frase não manda colar de novo: %q", msg)
	}
	if m.prov.quantas() != chamadas {
		t.Fatal("a chave de outra chave de cifra chegou ao provedor")
	}
	nova := "sk-" + "colada-de-novo-" + strings.Repeat("r", 20) + "CD34"
	if rec := m.put(celA, "openai", nova); rec.Code != http.StatusOK {
		t.Fatalf("colar de novo: %d %s", rec.Code, rec.Body.String())
	}
	if k, err := m.linha(m.userA, "openai"); err != nil || bytes.Equal(k.IDDaCifra, idAntigo) || !bytes.Equal(k.IDDaCifra, m.cofre.ID()) {
		t.Fatalf("a linha nova não tem o id da chave de cifra nova: %v", err)
	}
	if rec := m.modelos(celA, "openai"); rec.Code != http.StatusOK || m.prov.ultima().chave != nova {
		t.Fatalf("a chave colada de novo: %d", rec.Code)
	}
	m.montar(ChaveDeCifraA)
	exigirErro(t, m.modelos(celA, "openai"), http.StatusConflict, "SEM_CHAVE")
}

// Sem o aceite vigente daquele provedor, o PUT recusa antes de ler o corpo e
// sem chamar o provedor; o GET dos modelos também (a chave iria ao provedor).
// Aqui o aceite é o dublê da suíte; o de verdade (a dictation_consents) é
// provado em ditado.go.
func testSemAceite(t *testing.T, m *mundo) {
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	// O corpo nem é lido: o JSON quebrado também é SEM_ACEITE.
	exigirErro(t, m.pedir(celA, http.MethodPut, rotaOpenAI, `{"chave":`), http.StatusForbidden, "SEM_ACEITE")
	// O aceite de outro provedor, ou de outra conta, não serve.
	m.aceite.dar(m.userA, cloudstore.ProvedorGroq)
	m.aceite.dar(m.userB, cloudstore.ProvedorOpenAI)
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	if m.prov.quantas() != 0 {
		t.Fatalf("o PUT sem aceite chamou o provedor %d vezes", m.prov.quantas())
	}
	if _, err := m.linha(m.userA, "openai"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
		t.Fatalf("o PUT sem aceite gravou: %v", err)
	}

	// Com o aceite, grava; tirado o aceite, a lista recusa sem chamar.
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT com aceite: %d", rec.Code)
	}
	m.aceite.tirar(m.userA, cloudstore.ProvedorOpenAI)
	exigirErro(t, m.modelos(celA, "openai"), http.StatusForbidden, "SEM_ACEITE")
	if m.prov.quantas() != 1 {
		t.Fatalf("a lista sem aceite chamou o provedor (%d chamadas)", m.prov.quantas())
	}
	// Apagar não pede o aceite.
	if rec := m.apagar(celA, "openai"); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE sem aceite: %d", rec.Code)
	}

	// O aceite que falha (o banco fora): 500, sem chamar.
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	m.aceite.falhar(errors.New("banco fora"))
	exigirErro(t, m.put(celA, "openai", canario), http.StatusInternalServerError, "INTERNAL")
	if m.prov.quantas() != 1 {
		t.Fatal("o aceite que falhou deixou chamar o provedor")
	}
	m.aceite.falhar(nil)

	// Sem o AceiteDoDitado dublê (nil na configuração), o aceite é o de
	// verdade, o da dictation_consents: o dublê não vale, e a conta que nunca
	// aceitou não tem aceite.
	m.aceiteDeVerdade = true
	m.montar(ChaveDeCifraA)
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	if m.prov.quantas() != 1 {
		t.Fatal("sem o aceite de verdade, o PUT chamou o provedor")
	}
}

// 10 por minuto por conta, o PUT e o GET dos modelos juntos (escolha menor
// 24): o 11º é 429 LIMITE com Retry-After, sem chamar o provedor. Só conta o
// pedido que vai chamar o provedor. A outra conta tem o balde dela, e um
// minuto depois volta.
func testLimitePorConta(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI, cloudstore.ProvedorGroq)
	m.aceite.dar(m.userB, cloudstore.ProvedorOpenAI)
	// O que não chama o provedor não gasta: o JSON quebrado, a validação, a
	// lista sem chave.
	for range 12 {
		exigirErro(t, m.pedir(celA, http.MethodPut, rotaOpenAI, `{"x":1}`), http.StatusBadRequest, "BAD_REQUEST")
		exigirErro(t, m.put(celA, "openai", "a b"), http.StatusBadRequest, "VALIDATION")
		exigirErro(t, m.modelos(celA, "groq"), http.StatusConflict, "SEM_CHAVE")
	}
	for i := range 5 {
		if rec := m.put(celA, "openai", fmt.Sprintf("sk-limite-%02d-%s", i, strings.Repeat("x", 20))); rec.Code != http.StatusOK {
			t.Fatalf("PUT %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
		if rec := m.modelos(celA, "openai"); rec.Code != http.StatusOK {
			t.Fatalf("GET models %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if m.prov.quantas() != 10 {
		t.Fatalf("%d chamadas ao provedor, quero 10", m.prov.quantas())
	}
	for _, rec := range []*httptest.ResponseRecorder{m.put(celA, "groq", canario), m.modelos(celA, "openai")} {
		exigirErro(t, rec, http.StatusTooManyRequests, "LIMITE")
		if ra := rec.Header().Get("Retry-After"); ra == "" || ra == "0" {
			t.Errorf("o 429 sem Retry-After: %q", ra)
		}
	}
	if m.prov.quantas() != 10 {
		t.Fatalf("o pedido acima do limite chamou o provedor (%d chamadas)", m.prov.quantas())
	}
	if _, err := m.linha(m.userA, "groq"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
		t.Fatal("o PUT acima do limite gravou")
	}
	if !strings.Contains(m.logs.String(), "motivo=limite") {
		t.Error("o log de acesso não diz o motivo do 429")
	}
	// A outra conta tem o balde dela.
	if rec := m.put(celB, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("a conta B foi barrada pelo limite da A: %d", rec.Code)
	}
	// Um minuto depois, volta.
	m.clk.Advance(time.Minute)
	if rec := m.put(celA, "groq", canario); rec.Code != http.StatusOK {
		t.Fatalf("depois de um minuto: %d %s", rec.Code, rec.Body.String())
	}
}

// As três rotas são só da sessão: o token de um PC recebe 403, e sem
// credencial é 401. Nada chega ao aceite nem ao provedor.
func testSoComSessao(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	for _, r := range []struct{ method, path, corpo string }{
		{http.MethodPut, rotaOpenAI, corpoDaChave(t, canario)},
		{http.MethodDelete, rotaOpenAI, ""},
		{http.MethodGet, modelsOpenAI, ""},
	} {
		exigirErro(t, m.pedir(pcA, r.method, r.path, r.corpo), http.StatusForbidden, "FORBIDDEN")
		exigirErro(t, m.pedir("", r.method, r.path, r.corpo), http.StatusUnauthorized, "UNAUTHORIZED")
	}
	if m.prov.quantas() != 0 || m.aceite.chamadas != 0 {
		t.Fatalf("o pedido sem sessão chegou ao aceite (%d) ou ao provedor (%d)", m.aceite.chamadas, m.prov.quantas())
	}
	for _, rt := range m.cloud.Rotas() {
		if strings.Contains(rt.Padrao, "/ai-keys") && rt.Acesso != server.AcessoSessao {
			t.Errorf("%s com acesso %v, quero só a sessão", rt.Padrao, rt.Acesso)
		}
	}
}

// O provedor fora da lista fechada (ou com outra grafia) é 404, sem eco do
// valor e antes do aceite.
func testProvedorForaDaLista(t *testing.T, m *mundo) {
	for _, p := range []string{"OpenAI", "xai", "openai-x", "groq.", "anthropic%2F..", "%20openai"} {
		for _, r := range []struct{ method, path, corpo string }{
			{http.MethodPut, "/api/v1/ai-keys/" + p, corpoDaChave(t, canario)},
			{http.MethodDelete, "/api/v1/ai-keys/" + p, ""},
			{http.MethodGet, "/api/v1/ai-keys/" + p + "/models", ""},
		} {
			if msg := exigirErro(t, m.pedir(celA, r.method, r.path, r.corpo), http.StatusNotFound, "NOT_FOUND"); msg != "provedor desconhecido" {
				t.Errorf("%s %s: %q, quero a frase fixa (sem eco do provedor)", r.method, r.path, msg)
			}
		}
	}
	if m.prov.quantas() != 0 || m.aceite.chamadas != 0 {
		t.Fatalf("o provedor desconhecido chegou ao aceite (%d) ou ao provedor (%d)", m.aceite.chamadas, m.prov.quantas())
	}
}

// Cada falha do provedor vira um código da lista fechada, com uma frase fixa;
// o PUT que falha não grava, e a chave de antes continua.
func testFalhasDoProvedor(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d", rec.Code)
	}
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{&ia.Erro{Motivo: ia.MotivoChaveRecusada}, http.StatusUnprocessableEntity, "PROVEDOR_RECUSOU_CHAVE"},
		{&ia.Erro{Motivo: ia.MotivoSemPermissao}, http.StatusUnprocessableEntity, "PROVEDOR_RECUSOU_CHAVE"},
		{&ia.Erro{Motivo: ia.MotivoLimite}, http.StatusBadGateway, "PROVEDOR_LIMITE"},
		{&ia.Erro{Motivo: ia.MotivoIndisponivel}, http.StatusBadGateway, "PROVEDOR_FORA"},
		{&ia.Erro{Motivo: ia.MotivoTempoEsgotado}, http.StatusBadGateway, "PROVEDOR_FORA"},
		{&ia.Erro{Motivo: ia.MotivoSemRede}, http.StatusBadGateway, "PROVEDOR_FORA"},
		{&ia.Erro{Motivo: ia.MotivoRespostaInesperada}, http.StatusBadGateway, "PROVEDOR_FORA"},
		{fmt.Errorf("embrulhado: %w", &ia.Erro{Motivo: ia.MotivoLimite}), http.StatusBadGateway, "PROVEDOR_LIMITE"},
		{errors.New("qualquer outra"), http.StatusBadGateway, "PROVEDOR_FORA"},
		{context.DeadlineExceeded, http.StatusBadGateway, "PROVEDOR_FORA"},
	} {
		m.clk.Advance(time.Minute)
		m.prov.falharCom(func(string) error { return c.err })
		exigirErro(t, m.put(celA, "openai", "sk-"+"nova-que-falha-"+strings.Repeat("n", 20)), c.status, c.code)
		exigirErro(t, m.modelos(celA, "openai"), c.status, c.code)
		if k, err := m.linha(m.userA, "openai"); err != nil || k.Final != "WXYZ" {
			t.Fatalf("%v: o PUT que falhou mudou a chave guardada (%+v, %v)", c.err, k, err)
		}
		if status, code, _ := server.FalhaDoProvedor(c.err); status != c.status || code != c.code {
			t.Errorf("FalhaDoProvedor(%v) = %d %s", c.err, status, code)
		}
	}
}

// O "apagar meus dados" leva as chaves da conta, e só as dela.
func testApagarMeusDados(t *testing.T, m *mundo) {
	ctx := context.Background()
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI, cloudstore.ProvedorGroq)
	m.aceite.dar(m.userB, cloudstore.ProvedorOpenAI)
	for _, c := range []struct{ quem, prov string }{{celA, "openai"}, {celA, "groq"}, {celB, "openai"}} {
		if rec := m.put(c.quem, c.prov, canario); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s %s: %d", c.quem, c.prov, rec.Code)
		}
	}
	if rec := m.pedir(celA, http.MethodDelete, "/api/v1/me/data", `{"confirmar":"apagar"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me/data: %d %s", rec.Code, rec.Body.String())
	}
	if l, err := m.b.Chaves.ListarChavesDeIA(ctx, m.userA); err != nil || len(l) != 0 {
		t.Fatalf("depois de apagar, A tem %d chaves (%v)", len(l), err)
	}
	if l, err := m.b.Chaves.ListarChavesDeIA(ctx, m.userB); err != nil || len(l) != 1 {
		t.Fatalf("o apagar de A levou a chave de B: %d (%v)", len(l), err)
	}
	exigirErro(t, m.modelos(celA, "openai"), http.StatusConflict, "SEM_CHAVE")
}

// As regras do painel do PC (chavedeapi.Validar), sem chamar o provedor.
func testValidar(t *testing.T, m *mundo) {
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	for nome, chave := range map[string]string{
		"vazia":          "",
		"só espaço":      " \r\n\t ",
		"longa demais":   strings.Repeat("a", 2561),
		"espaço no meio": "sk-abc def",
		"fora do ASCII":  "sk-açaí-0123456789",
		"controle":       "sk-abc\x00def",
		"DEL":            "sk-abc\x7fdef",
	} {
		rec := m.put(celA, "openai", chave)
		if code, _ := erroDe(t, rec); rec.Code != http.StatusBadRequest || code != "VALIDATION" {
			t.Errorf("%s: %d %s, quero 400 VALIDATION", nome, rec.Code, code)
		}
	}
	exigirErro(t, m.pedir(celA, http.MethodPut, rotaOpenAI, `{}`), http.StatusBadRequest, "VALIDATION")
	exigirErro(t, m.pedir(celA, http.MethodPut, rotaOpenAI, `{"chave":null}`), http.StatusBadRequest, "VALIDATION")
	if m.prov.quantas() != 0 {
		t.Fatal("a chave recusada pela validação chegou ao provedor")
	}
	// No teto, vale (e o fim é o de chavedeapi.Final).
	if rec := m.put(celA, "openai", strings.Repeat("a", 2559)+"Z"); rec.Code != http.StatusOK || decodificar[chaveDTO](t, rec).Final != "aaaZ" {
		t.Fatalf("a chave no teto: %d %s", rec.Code, rec.Body.String())
	}
	// Uma chave curta tem o fim curto (nunca mais que um quarto).
	if rec := m.put(celA, "openai", "abcdefgh"); rec.Code != http.StatusOK || decodificar[chaveDTO](t, rec).Final != "gh" {
		t.Fatalf("a chave curta: %d %s", rec.Code, rec.Body.String())
	}
}

// Sem o arquivo da chave de cifra, as rotas não são montadas: 404 de rota não
// montada para qualquer credencial, sem chegar ao aceite nem ao provedor.
func testSemOArquivo(t *testing.T, m *mundo) {
	m.montar("")
	m.aceite.dar(m.userA, cloudstore.ProvedorOpenAI)
	for _, r := range []struct{ method, path, corpo string }{
		{http.MethodPut, rotaOpenAI, corpoDaChave(t, canario)},
		{http.MethodDelete, rotaOpenAI, ""},
		{http.MethodGet, modelsOpenAI, ""},
	} {
		for _, quem := range []string{celA, pcA, ""} {
			if msg := exigirErro(t, m.pedir(quem, r.method, r.path, r.corpo), http.StatusNotFound, "NOT_FOUND"); msg != "rota não encontrada" {
				t.Errorf("%s %s (%q): %q, quero o 404 de rota não montada", r.method, r.path, quem, msg)
			}
		}
	}
	if m.prov.quantas() != 0 || m.aceite.chamadas != 0 {
		t.Fatal("sem o arquivo, um pedido chegou ao aceite ou ao provedor")
	}
	for _, rt := range m.cloud.Rotas() {
		if strings.Contains(rt.Padrao, "/ai-keys") {
			t.Errorf("sem o arquivo, a tabela tem %s", rt.Padrao)
		}
	}
	// E a configuração com o cofre e sem onde guardar não sobe.
	c, err := cofre.Novo(ChaveDeCifraA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.NewCloud(server.CloudConfig{
		Store: m.b.Store, Accounts: m.b.Accounts, BaseURL: contracttest.BaseURL,
		SessionSecret: segredoDeSessao, DeviceTokenPepper: pepperDeToken, Contas: m.contas,
		Google: cloudauth.NovoGoogle("c", "s", contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Cofre: c, Modelos: m.prov,
	}); err == nil {
		t.Error("o cofre sem o ChavesDeIA subiu")
	}
}

// O ChavesDeIA direto: a conferência antes do banco, a gravação por cima com a
// data da primeira, a leitura, a lista na ordem fechada, o apagar, e as
// contas que não se veem.
func testStore(t *testing.T, m *mundo) {
	ctx := context.Background()
	s := m.b.Chaves
	boa := func(provedor string, b byte) cloudstore.ChaveDeIA {
		return cloudstore.ChaveDeIA{
			Provedor: provedor, IDDaCifra: bytes.Repeat([]byte{b}, 8), Nonce: bytes.Repeat([]byte{b + 1}, 12),
			Cifrado: bytes.Repeat([]byte{b + 2}, 40), Final: "WXYZ",
		}
	}
	for nome, mudar := range map[string]func(k *cloudstore.ChaveDeIA){
		"provedor em maiúsculas": func(k *cloudstore.ChaveDeIA) { k.Provedor = "OpenAI" },
		"provedor fora da lista": func(k *cloudstore.ChaveDeIA) { k.Provedor = "xai" },
		"id de 7 bytes":          func(k *cloudstore.ChaveDeIA) { k.IDDaCifra = k.IDDaCifra[:7] },
		"nonce de 11 bytes":      func(k *cloudstore.ChaveDeIA) { k.Nonce = k.Nonce[:11] },
		"cifrado só da etiqueta": func(k *cloudstore.ChaveDeIA) { k.Cifrado = k.Cifrado[:16] },
		"cifrado acima do teto":  func(k *cloudstore.ChaveDeIA) { k.Cifrado = make([]byte, 2577) },
		"fim de 5 caracteres":    func(k *cloudstore.ChaveDeIA) { k.Final = "ABCDE" },
		"fim com espaço":         func(k *cloudstore.ChaveDeIA) { k.Final = "a b" },
		"fim fora do ASCII":      func(k *cloudstore.ChaveDeIA) { k.Final = "é" },
	} {
		k := boa("openai", 1)
		mudar(&k)
		quero := cloudstore.ErrChaveDeIAInvalida
		if strings.HasPrefix(nome, "provedor") {
			quero = cloudstore.ErrProvedorDeIA
		}
		if _, err := s.GravarChaveDeIA(ctx, m.userA, k, agora0); !errors.Is(err, quero) {
			t.Errorf("%s: %v, quero %v", nome, err, quero)
		}
	}
	for _, u := range []string{contracttest.NewID(), "nao-e-uuid"} {
		if _, err := s.GravarChaveDeIA(ctx, u, boa("openai", 1), agora0); !errors.Is(err, cloudstore.ErrUnknownUser) {
			t.Errorf("usuário %q: %v, quero ErrUnknownUser", u, err)
		}
	}
	if _, err := s.LerChaveDeIA(ctx, m.userA, "openai"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
		t.Fatalf("Ler antes de gravar: %v", err)
	}
	if _, err := s.LerChaveDeIA(ctx, m.userA, "OpenAI"); !errors.Is(err, cloudstore.ErrProvedorDeIA) {
		t.Fatalf("Ler com o provedor fora da lista: %v", err)
	}

	got, err := s.GravarChaveDeIA(ctx, m.userA, boa("openai", 1), agora0)
	if err != nil || !got.CriadaEm.Equal(agora0) || !got.AtualizadaEm.Equal(agora0) || got.Final != "WXYZ" {
		t.Fatalf("Gravar = %+v, %v", got, err)
	}
	// O fim vazio vale (a chave curta).
	vazio := boa("anthropic", 7)
	vazio.Final = ""
	if _, err := s.GravarChaveDeIA(ctx, m.userA, vazio, agora0); err != nil {
		t.Fatalf("o fim vazio: %v", err)
	}
	if _, err := s.GravarChaveDeIA(ctx, m.userA, boa("groq", 4), agora0); err != nil {
		t.Fatal(err)
	}
	// Por cima: a data da primeira fica, a da troca anda, os bytes mudam.
	depois := agora0.Add(time.Hour)
	if got, err = s.GravarChaveDeIA(ctx, m.userA, boa("openai", 9), depois); err != nil || !got.CriadaEm.Equal(agora0) || !got.AtualizadaEm.Equal(depois) {
		t.Fatalf("por cima = %+v, %v", got, err)
	}
	lida, err := s.LerChaveDeIA(ctx, m.userA, "openai")
	if err != nil || !bytes.Equal(lida.Nonce, boa("openai", 9).Nonce) || !lida.CriadaEm.Equal(agora0) || !lida.AtualizadaEm.Equal(depois) {
		t.Fatalf("Ler depois de gravar por cima = %+v, %v", lida, err)
	}
	// O relógio que volta não põe a troca antes da criação.
	if got, err = s.GravarChaveDeIA(ctx, m.userA, boa("openai", 11), agora0.Add(-time.Hour)); err != nil || got.AtualizadaEm.Before(got.CriadaEm) {
		t.Fatalf("com o relógio voltando: %+v, %v", got, err)
	}
	// A cópia devolvida é de quem a recebe.
	lida.Cifrado[0] ^= 0xff
	if de, _ := s.LerChaveDeIA(ctx, m.userA, "openai"); bytes.Equal(de.Cifrado, lida.Cifrado) {
		t.Fatal("mudar a linha devolvida mudou a guardada")
	}

	lista, err := s.ListarChavesDeIA(ctx, m.userA)
	if err != nil || len(lista) != 3 || lista[0].Provedor != "openai" || lista[1].Provedor != "groq" || lista[2].Provedor != "anthropic" {
		t.Fatalf("Listar = %+v, %v; quero openai, groq, anthropic", lista, err)
	}
	// B não vê nada de A.
	if l, err := s.ListarChavesDeIA(ctx, m.userB); err != nil || l == nil || len(l) != 0 {
		t.Fatalf("Listar de B = %v, %v; quero vazia, não nil", l, err)
	}
	if _, err := s.LerChaveDeIA(ctx, m.userB, "openai"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
		t.Fatalf("B leu a chave de A: %v", err)
	}
	if apagou, err := s.ApagarChaveDeIA(ctx, m.userB, "openai"); err != nil || apagou {
		t.Fatalf("B apagou a chave de A: %v %v", apagou, err)
	}
	if apagou, err := s.ApagarChaveDeIA(ctx, m.userA, "openai"); err != nil || !apagou {
		t.Fatalf("Apagar: %v %v", apagou, err)
	}
	if apagou, err := s.ApagarChaveDeIA(ctx, m.userA, "openai"); err != nil || apagou {
		t.Fatalf("Apagar de novo: %v %v", apagou, err)
	}
	if _, err := s.ApagarChaveDeIA(ctx, m.userA, "xai"); !errors.Is(err, cloudstore.ErrProvedorDeIA) {
		t.Fatalf("Apagar fora da lista: %v", err)
	}
	if l, _ := s.ListarChavesDeIA(ctx, m.userA); len(l) != 2 || l[0].Provedor != "groq" {
		t.Fatalf("Listar depois de apagar = %+v", l)
	}
}
