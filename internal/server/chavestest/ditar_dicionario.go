package chavestest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O ditado no celular com a cópia do dicionário do PC (ADR-023, D e E): o PC A
// sobe a cópia pela rota do sync, e o ditado da conta passa a escrever os termos
// dela, com o aprendido por cima; a dica vira o prompt do PC; o estado do
// celular diz de que PC ela é; apagar a cópia volta tudo ao de antes. O log
// nunca traz um termo.

// dicionarioDoPCA é a cópia que o PC A sobe: um termo com dica, um termo de
// domínio (vale fora dele), uma variante restrita (não vale sem domínio).
func dicionarioDoPCA(extras ...dictionary.EntradaSemente) dictionary.Semente {
	return dictionary.Semente{
		Usuario:  "local",
		Dominios: map[string][]string{"contabil": {"Dominio.exe"}},
		Entradas: append([]dictionary.EntradaSemente{
			{Canonica: "DCTFWeb", Prioridade: dictionary.PrioridadeDica, Alias: []string{"dectefe web"}, Origem: dictionary.OrigemAdicionado},
			{Canonica: "Vitti", Prioridade: 90, Dominio: "contabil", Alias: []string{"viti"}, AliasRestrito: []string{"vite"}},
		}, extras...),
	}
}

// subirCopia manda a cópia pelo PUT do sync, com o token do PC A.
func (m *mundo) subirCopia(s dictionary.Semente, fonte string) *httptest.ResponseRecorder {
	m.t.Helper()
	corpo, sha, err := s.BytesDaCopia()
	if err != nil {
		m.t.Fatalf("BytesDaCopia: %v", err)
	}
	return m.pedirCom(pcA, http.MethodPut, "/api/v1/sync/dictionary", string(corpo), func(r *http.Request) {
		r.Header.Set("Dicionario-SHA256", sha)
		r.Header.Set("Dicionario-Fonte", fonte)
	})
}

func (m *mundo) ditarOk(texto string) ditadoResp {
	m.t.Helper()
	m.voz.responder(texto, 0)
	r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	return m.ditadoOk(r, http.StatusCreated)
}

func testDitarComACopiaDoDicionario(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", true)
	m.aprenderPeloPC("rague", "RAG", nil)

	// Sem a cópia: o celular não conhece o dicionário do PC, e a dica é a do
	// aprendido.
	if e := m.estado(celA); e.Dicionario != nil {
		t.Fatalf("o estado traz um dicionário sem cópia: %+v", e.Dicionario)
	}
	if d := m.ditarOk("mande a dectefe web"); d.FinalText != "mande a dectefe web" {
		t.Fatalf("sem a cópia, o termo do PC agiu: %q", d.FinalText)
	}
	if prompt := m.voz.ultimo().campos["prompt"]; prompt != "RAG" {
		t.Fatalf("sem a cópia, a dica = %q, quero a do aprendido", prompt)
	}

	// O PC A sobe a cópia.
	copia := dicionarioDoPCA()
	m.exigirStatus(m.subirCopia(copia, "tomar"), http.StatusOK, "PUT da cópia")
	e := m.estado(celA)
	if e.Dicionario == nil || e.Dicionario.Termos != 2 || e.Dicionario.Aparelho == "" || e.Dicionario.AtualizadoEm.IsZero() {
		t.Fatalf("o estado com a cópia: %+v", e.Dicionario)
	}

	// O termo do dicionário age, o de domínio sem restrição também, a variante
	// restrita não, e o aprendido por cima também.
	d := m.ditarOk("mande a dectefe web do viti e a vite, e o rague")
	if d.FinalText != "mande a DCTFWeb do Vitti e a vite, e o RAG" {
		t.Fatalf("com a cópia: %q", d.FinalText)
	}
	// A dica é o prompt do PC: a mesma função sobre as mesmas entradas.
	quero := dictionary.Vocabulario(copia.ParaEntradas(), dictionary.OrcamentoRunasPadrao)
	if prompt := m.voz.ultimo().campos["prompt"]; prompt != quero || !strings.HasPrefix(prompt, "DCTFWeb") {
		t.Fatalf("com a cópia, a dica = %q, quero %q", prompt, quero)
	}

	// A cópia muda (o PC editou o arquivo): o próximo ditado a usa, sem cache velho.
	nova := dicionarioDoPCA(dictionary.EntradaSemente{Canonica: "ECD", Prioridade: 80, Alias: []string{"ecedê"}})
	m.exigirStatus(m.subirCopia(nova, "manter"), http.StatusOK, "PUT da cópia nova")
	if d := m.ditarOk("o ecedê e a dectefe web"); d.FinalText != "o ECD e a DCTFWeb" {
		t.Fatalf("a cópia nova não agiu: %q", d.FinalText)
	}
	if e := m.estado(celA); e.Dicionario == nil || e.Dicionario.Termos != 3 {
		t.Fatalf("o estado não mudou com a cópia nova: %+v", e.Dicionario)
	}

	// A conta B não vê a cópia da A.
	if e := m.estado(celB); e.Dicionario != nil {
		t.Fatalf("a conta B vê a cópia da A: %+v", e.Dicionario)
	}

	// O log diz que a cópia foi usada e quantos termos, nunca um termo.
	logs := m.logs.String()
	var situacoes []string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, "msg=ditado ") {
			for _, s := range []string{"dicionario=sem", "dicionario=copia", "dicionario=falhou"} {
				if strings.Contains(l, s) {
					situacoes = append(situacoes, s)
				}
			}
		}
	}
	if strings.Join(situacoes, " ") != "dicionario=sem dicionario=copia dicionario=copia" {
		t.Errorf("as linhas do ditado dizem %v, quero sem, copia, copia", situacoes)
	}
	for _, nunca := range []string{"DCTFWeb", "dectefe", "Vitti", "viti", "ECD", "ecedê", "Dominio.exe"} {
		if strings.Contains(logs, nunca) {
			t.Errorf("o log traz %q", nunca)
		}
	}

	// O PC A desliga a opção: a cópia sai, e o ditado volta ao de antes.
	m.exigirStatus(m.pedir(pcA, http.MethodDelete, "/api/v1/sync/dictionary", ""), http.StatusNoContent, "DELETE da cópia")
	if e := m.estado(celA); e.Dicionario != nil {
		t.Fatalf("o estado traz a cópia apagada: %+v", e.Dicionario)
	}
	if d := m.ditarOk("mande a dectefe web e o rague"); d.FinalText != "mande a dectefe web e o RAG" {
		t.Fatalf("depois de apagar a cópia: %q", d.FinalText)
	}
	if prompt := m.voz.ultimo().campos["prompt"]; prompt != "RAG" {
		t.Fatalf("depois de apagar a cópia, a dica = %q, quero a do aprendido", prompt)
	}
}
