package localapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As linhas de estado de GET /api/aprendizado (ADR-019, "Decisões da F8",
// F8g): o espelho do sync é um dublê em memória (os estados são os do
// storage), e o aprendizado é o falso de aprendizado_test.go.

type espelhoFalso struct {
	es  []storage.EstadoDoAprendido
	err error
}

func (e espelhoFalso) Espelho(context.Context) ([]storage.EstadoDoAprendido, error) {
	return e.es, e.err
}

type itemComEstado struct {
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
	Situacao string `json:"situacao"`
	Motivo   string `json:"motivo"`
}

func listarComEstado(t *testing.T, ap *aprendizadoFalso, esp localapi.EspelhoDoAprendido) ([]itemComEstado, []map[string]any, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	pref := prefsDeTeste()
	pref.Aprendizado = ap
	pref.Espelho = esp
	pref.Log = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref)
	w := fazer(t, srv.Handler(), http.MethodGet, "/api/aprendizado", nil)
	exigirStatus(t, w, http.StatusOK)
	var tipado struct{ Items []itemComEstado }
	var cru struct{ Items []map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &tipado); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cru); err != nil {
		t.Fatal(err)
	}
	return tipado.Items, cru.Items, buf.String()
}

func quando(d time.Duration) *time.Time {
	t := time.Now().Add(-d)
	return &t
}

func versao(v int64) *int64 { return &v }

func guardado(variante, termo, dominio string, desfeito bool) *storage.ParGuardado {
	return &storage.ParGuardado{Versao: versao(2), Par: dictionary.ParDaNuvem{Variante: variante, Termo: termo, Dominio: dominio, Desfeito: desfeito}}
}

// perdido é a troca deste PC que perdeu na nuvem (a 0008).
func perdido(variante, termo string) *storage.ParPerdido {
	return &storage.ParPerdido{Variante: variante, Termo: termo}
}

// O arquivo vem como antes, na ordem dele, e SEM situacao em nenhuma linha
// (é o par que vale aqui, com o Desfazer), mesmo quando o espelho do id tem
// uma situação. Depois, as linhas de estado, da mais recente para a mais
// antiga, com os nomes do fio (06): o "trocado em outro aparelho" mostra a
// troca que este PC perdeu (o par perdido), também quando o id está no
// arquivo (a linha do arquivo é a troca da nuvem); o "desfeito em outro
// aparelho" fora do arquivo mostra o par perdido (numa linha sem ele, a da
// nuvem); o "não aplicado neste PC", o par da nuvem com o motivo. Não
// aparecem: as etiquetas vencidas (30 dias, no arquivo e fora dele), o
// "trocado" sem o par perdido (de antes da 0008), o "desfeito" e o "não
// aplicado" de um id que está no arquivo, o par sem texto (o esquecer o
// apagou), e as outras situações. Os campos novos só existem onde valem.
func TestAprendizado_GetComAsLinhasDeEstado(t *testing.T) {
	ap := novoFalso()
	for _, p := range [][2]string{{"ECF", "sf"}, {"ECD", "ecedê"}, {"ECF", "esse efe"}, {"EFD", "reinfe"}} {
		var err error
		if ap.dic, _, err = ap.dic.AprenderVariante(p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	id := dictionary.IDDoPar
	esp := espelhoFalso{es: []storage.EstadoDoAprendido{
		{ID: id("sf"), Situacao: storage.EspelhoAplicado, SituacaoEm: quando(time.Hour)},
		// No arquivo: a troca da nuvem (ECD) vale aqui, e a deste PC (ECF)
		// perdeu.
		{ID: id("ecedê"), Situacao: storage.EspelhoTrocadoEmOutro, SituacaoEm: quando(time.Hour),
			Nuvem: guardado("ecedê", "ECD", "", false), Perdido: perdido("ecedê", "ECF")},
		// No arquivo, com a etiqueta do desfazer: a varredura seguinte a
		// resolve, e a linha espera.
		{ID: id("esse efe"), Situacao: storage.EspelhoDesfeitoEmOutro, SituacaoEm: quando(time.Hour),
			Nuvem: guardado("esse efe", "ECF", "", true), Perdido: perdido("esse efe", "ECF")},
		// No arquivo, com a etiqueta "trocado" vencida (o PC desconectado há
		// mais de 30 dias: a varredura não a tirou).
		{ID: id("reinfe"), Situacao: storage.EspelhoTrocadoEmOutro, SituacaoEm: quando(31 * 24 * time.Hour),
			Nuvem: guardado("reinfe", "EFD", "", false), Perdido: perdido("reinfe", "EFD-daqui")},
		{ID: id("efedê"), Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoDominioAusente),
			SituacaoEm: quando(3 * time.Hour), Nuvem: guardado("efedê", "EFD", "fiscal", false)},
		{ID: id("dirfi"), Situacao: storage.EspelhoDesfeitoEmOutro, SituacaoEm: quando(2 * time.Hour),
			Nuvem: guardado("dirfi", "DIRF", "", true), Perdido: perdido("dirfi", "Dirf")},
		{ID: id("lalurr"), Situacao: storage.EspelhoTrocadoEmOutro, SituacaoEm: quando(time.Minute),
			Nuvem: guardado("lalurr", "LALUR", "contabil", false), Perdido: perdido("lalurr", "Lalur")},
		// De antes da 0008: o desfeito mostra o par da nuvem.
		{ID: id("efessê"), Situacao: storage.EspelhoDesfeitoEmOutro, SituacaoEm: quando(4 * time.Hour),
			Nuvem: guardado("efessê", "EFS", "", true)},
		// Não aparecem.
		{ID: id("vencida"), Situacao: storage.EspelhoDesfeitoEmOutro, SituacaoEm: quando(31 * 24 * time.Hour),
			Nuvem: guardado("vencida", "ECD", "", true), Perdido: perdido("vencida", "ECF")},
		{ID: id("trocada sem par"), Situacao: storage.EspelhoTrocadoEmOutro, SituacaoEm: quando(time.Minute),
			Nuvem: guardado("trocada sem par", "ECD", "", false)},
		{ID: id("esquecida"), Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoNaoAgiria), SituacaoEm: quando(time.Minute)},
		{ID: id("recusada"), Situacao: storage.EspelhoRecusado, Motivo: "campo_invalido", SituacaoEm: quando(time.Minute), Nuvem: guardado("recusada", "ECD", "", false)},
		{ID: id("à mão"), Situacao: storage.EspelhoJaValeAqui, SituacaoEm: quando(time.Minute), Nuvem: guardado("à mão", "ECD", "", false)},
		{ID: id("aplicando"), Situacao: storage.EspelhoAplicando, SituacaoEm: quando(time.Minute),
			Nuvem: guardado("aplicando", "ECD", "", false), Perdido: perdido("aplicando", "ECF")},
	}}
	itens, cru, _ := listarComEstado(t, ap, esp)
	quer := []itemComEstado{
		// O arquivo, na ordem dele (as entradas, e os alias de cada uma).
		{Termo: "ECF", Variante: "esse efe"},
		{Termo: "ECF", Variante: "sf"},
		{Termo: "ECD", Variante: "ecedê"},
		{Termo: "EFD", Variante: "reinfe"},
		// As linhas de estado, da mais recente para a mais antiga.
		{Termo: "Lalur", Variante: "lalurr", Situacao: "trocado_em_outro_aparelho"},
		{Termo: "ECF", Variante: "ecedê", Situacao: "trocado_em_outro_aparelho"},
		{Termo: "Dirf", Variante: "dirfi", Situacao: "desfeito_em_outro_aparelho"},
		{Termo: "EFD", Variante: "efedê", Situacao: "nao_aplicado", Motivo: "dominio_ausente"},
		{Termo: "EFS", Variante: "efessê", Situacao: "desfeito_em_outro_aparelho"},
	}
	if !reflect.DeepEqual(itens, quer) {
		t.Fatalf("a lista:\n%+v\nquero:\n%+v", itens, quer)
	}
	for i, it := range cru {
		_, temSituacao := it["situacao"]
		_, temMotivo := it["motivo"]
		if temSituacao != (quer[i].Situacao != "") || temMotivo != (quer[i].Motivo != "") {
			t.Errorf("item %d: os campos %v; situacao e motivo só onde valem", i, it)
		}
	}
}

// A tradução do espelho para o fio (06): cada situação do espelho, num id
// fora do arquivo e no arquivo, com o par da nuvem e o par perdido. Só as três
// de estado viram linha, com os nomes que a web lê, escritos aqui por extenso;
// as outras cinco (e a nenhuma) nunca. A linha do arquivo nunca leva
// situacao, qualquer que seja a do espelho: a web trataria o par que vale
// aqui como linha de estado, sem o Desfazer.
func TestAprendizado_ATraducaoDoEspelhoParaOFio(t *testing.T) {
	foraDoArquivo := map[storage.SituacaoDoEspelho][]itemComEstado{
		storage.EspelhoNaoAplicado:     {{Termo: "ECD", Variante: "sf", Situacao: "nao_aplicado", Motivo: "nao_agiria"}},
		storage.EspelhoDesfeitoEmOutro: {{Termo: "ECF", Variante: "sf", Situacao: "desfeito_em_outro_aparelho"}},
		storage.EspelhoTrocadoEmOutro:  {{Termo: "ECF", Variante: "sf", Situacao: "trocado_em_outro_aparelho"}},
	}
	noArquivo := map[storage.SituacaoDoEspelho][]itemComEstado{
		storage.EspelhoTrocadoEmOutro: {{Termo: "ECF", Variante: "sf", Situacao: "trocado_em_outro_aparelho"}},
	}
	situacoes := append([]storage.SituacaoDoEspelho{storage.EspelhoNenhuma}, storage.SituacoesDoEspelho()...)
	if len(situacoes) != 9 {
		t.Fatalf("a lista fechada do espelho mudou (%d): a tradução tem de ser revista", len(situacoes))
	}
	for _, s := range situacoes {
		e := storage.EstadoDoAprendido{ID: dictionary.IDDoPar("sf"), Situacao: s, SituacaoEm: quando(time.Hour),
			Nuvem: guardado("sf", "ECD", "", s == storage.EspelhoDesfeitoEmOutro), Perdido: perdido("sf", "ECF")}
		switch s {
		case storage.EspelhoNaoAplicado:
			e.Motivo = string(dictionary.MotivoNaoAgiria)
		case storage.EspelhoRecusado:
			e.Motivo = "campo_invalido"
		}
		esp := espelhoFalso{es: []storage.EstadoDoAprendido{e}}

		// Fora do arquivo: o arquivo só tem outra troca.
		ap := novoFalso()
		var err error
		if ap.dic, _, err = ap.dic.AprenderVariante("ECF", "esse efe"); err != nil {
			t.Fatal(err)
		}
		itens, _, _ := listarComEstado(t, ap, esp)
		quer := append([]itemComEstado{{Termo: "ECF", Variante: "esse efe"}}, foraDoArquivo[s]...)
		if !reflect.DeepEqual(itens, quer) {
			t.Errorf("%q fora do arquivo:\n%+v\nquero:\n%+v", s, itens, quer)
		}

		// No arquivo: a linha do arquivo, sem situacao.
		if ap.dic, _, err = ap.dic.AprenderVariante("ECD", "sf"); err != nil {
			t.Fatal(err)
		}
		itens, _, _ = listarComEstado(t, ap, esp)
		quer = append([]itemComEstado{{Termo: "ECF", Variante: "esse efe"}, {Termo: "ECD", Variante: "sf"}}, noArquivo[s]...)
		if !reflect.DeepEqual(itens, quer) {
			t.Errorf("%q no arquivo:\n%+v\nquero:\n%+v", s, itens, quer)
		}
	}
}

// O arquivo que existe e não se lê (inválido, de outro usuário) é um 503, com
// a frase pronta, mesmo com linhas de estado no espelho: a lista vazia
// esconderia a seção com o que a pessoa precisa desfazer (a revisão do
// ADR-017), e o 06 diz que, nesse caso, as linhas de estado não saem. O
// arquivo ausente (ErrSemArquivo) é a lista vazia, e as linhas de estado
// saem, com o "não aplicado" do arquivo indisponível.
func TestAprendizado_GetComOArquivoIndisponivel(t *testing.T) {
	esp := espelhoFalso{es: []storage.EstadoDoAprendido{
		{ID: dictionary.IDDoPar("efedê"), Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoArquivoIndisponivel),
			SituacaoEm: quando(time.Hour), Nuvem: guardado("efedê", "EFD", "fiscal", false)},
	}}
	for _, c := range []struct {
		nome string
		err  error
	}{{"inválido", dictionary.ErrArquivoInvalido}, {"de outro usuário", dictionary.ErrUsuarioDiferente}} {
		ap := novoFalso()
		ap.aprendidaErr = c.err
		pref := prefsDeTeste()
		pref.Aprendizado, pref.Espelho = ap, esp
		srv := localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref)
		w := fazer(t, srv.Handler(), http.MethodGet, "/api/aprendizado", nil)
		exigirStatus(t, w, http.StatusServiceUnavailable)
		if strings.Contains(w.Body.String(), "efedê") {
			t.Errorf("%s: o 503 trouxe a linha de estado: %s", c.nome, w.Body.String())
		}
	}

	// Sem o arquivo, o aprendizado do Agent devolve a lista vazia
	// (aprendiz.Aplicador.Aprendidas); aqui, o falso sem troca nenhuma.
	itens, _, _ := listarComEstado(t, novoFalso(), esp)
	quer := []itemComEstado{{Termo: "EFD", Variante: "efedê", Situacao: "nao_aplicado", Motivo: "arquivo_indisponivel"}}
	if !reflect.DeepEqual(itens, quer) {
		t.Errorf("sem o arquivo: %+v", itens)
	}
}

// Sem o espelho (o Agent sem histórico), a lista é a do arquivo, como antes
// da F8; o espelho que não se lê não derruba a lista, e o log não tem texto.
func TestAprendizado_GetSemEspelhoOuComEspelhoQueFalha(t *testing.T) {
	ap := novoFalso()
	var err error
	if ap.dic, _, err = ap.dic.AprenderVariante("ECF", "sf"); err != nil {
		t.Fatal(err)
	}
	quer := []itemComEstado{{Termo: "ECF", Variante: "sf"}}
	if itens, _, _ := listarComEstado(t, ap, nil); !reflect.DeepEqual(itens, quer) {
		t.Errorf("sem o espelho: %+v", itens)
	}
	itens, _, log := listarComEstado(t, ap, espelhoFalso{err: errors.New("storage: o banco sf está ocupado")})
	if !reflect.DeepEqual(itens, quer) {
		t.Errorf("com o espelho que falha: %+v", itens)
	}
	if !strings.Contains(log, "não consegui ler o espelho do sync") || strings.Contains(log, "sf") {
		t.Errorf("o log:\n%s", log)
	}
}

// Só linhas de estado: a lista aparece mesmo sem nada no arquivo (a seção da
// tela aparece também nesse caso).
func TestAprendizado_GetSoComLinhasDeEstado(t *testing.T) {
	esp := espelhoFalso{es: []storage.EstadoDoAprendido{
		{ID: dictionary.IDDoPar("efedê"), Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoRestricaoNaoCabe),
			SituacaoEm: quando(time.Hour), Nuvem: guardado("efedê", "EFD", "fiscal", false)},
	}}
	itens, _, _ := listarComEstado(t, novoFalso(), esp)
	quer := []itemComEstado{{Termo: "EFD", Variante: "efedê", Situacao: "nao_aplicado", Motivo: "restricao_nao_cabe"}}
	if !reflect.DeepEqual(itens, quer) {
		t.Errorf("a lista: %+v", itens)
	}
}
