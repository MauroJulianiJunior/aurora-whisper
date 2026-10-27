package contracttest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Run roda a suíte de contrato contra o Store de b. Cada caso cria os seus
// usuários: o seq é por usuário, então os casos não se enxergam e podem
// conferir números exatos.
func Run(t *testing.T, b Backend) {
	cases := []struct {
		name string
		fn   func(t *testing.T, w *world)
	}{
		{"AC016_EdicaoPelaAPIChegaAoPullDoPC", testAC016EditViaAPIReachesPull},
		{"Push_CriaEPullDevolveOEstadoAtual", testPushCreatePullState},
		{"Push_ReenvioEhDuplicateComOMesmoResultado", testDuplicate},
		{"Push_MutationIDReusadoParaOutraEntidadeEhRecusado", testReusedMutation},
		{"Push_MesclaComPullNoMeioEhAppliedMesclado", testMergeWithPullInBetween},
		{"Push_OCampoQueOProprioPCMudouEhConhecidoPorEle", testOwnMergedChange},
		{"Push_ConflitoDeTextoValeOQueChegouPrimeiro", testTextConflict},
		{"Selo_EscolherAVersaoPeloCelular", testResolveConflict},
		{"Selo_EscolherAVersaoPeloPC", testResolveConflictByPush},
		{"Push_ApagarVenceEdicaoAntiga", testDeleteWinsOldEdit},
		{"Push_Gone", testGone},
		{"Push_Recusas", testRejections},
		{"SyncSoComTokenDeAparelho", testSyncNeedsDevice},
		{"Pull_PaginasEmOrdemDeSeqComLapides", testPullPages},
		{"Limites_TextoPorCampoEPaginaPorBytes", testTextLimits},
		{"Isolamento_UmUsuarioNuncaVeNemAlteraODeOutro", testIsolation},
		{"CRUD_PeloPontoUnicoDeGravacao", testCRUD},
		{"CRUD_ExcluirDefinitivamente", testPurgeRoute},
		{"RotasNaoMontadasNaEtapa5", testUnmountedRoutes},
		{"NadaDeTextoDitadoEmLog", testNoDictatedTextInLogs},
		{"Concorrencia_SeqSemBuracoNemRepeticao", testConcurrentWriters},
		{"Concorrencia_ReenvioSimultaneoAplicaUmaVezSo", testConcurrentDuplicate},
		// O aprendido (F8b; learned.go).
		{"Aprendido_OLearnedSincronizaDesdeAF8", testLearnedSyncs},
		{"Aprendido_ATabelaDaNuvemLinhaALinha", testLearnedTable},
		{"Aprendido_PreChecagensSemGravarNemGastarSeq", testLearnedPrechecks},
		{"Aprendido_ReenvioConfereAOperacao", testLearnedDuplicateOperation},
		{"Aprendido_DesfazerSemRespostaSeguidoDeReaprender", testLearnedUndoLostThenRelearn},
		{"Aprendido_OMesmoIDEmDuasContas", testLearnedTwoAccounts},
		{"Aprendido_PullFundidoPorSeqComOsDitados", testLearnedPullMerged},
		{"Aprendido_OPCDaF7DecodificaENaoAplica", testLearnedDecodesOnF7PC},
		{"Aprendido_ApagarMeusDadosLevaOAprendido", testLearnedEraseAccount},
		{"Aprendido_NadaDeVarianteNemTermoEmLog", testLearnedNoTextInLogs},
		// O aprendido no celular (F8d; learned_celular.go).
		{"Celular_ListaEDesfazOAprendido", testPhoneListAndUndo},
		{"Celular_SoComSessao", testPhoneOnlySession},
		{"Celular_OutraContaEOIdQueNaoEhDelaDao404", testPhoneOtherAccount404},
		{"Celular_DesfazerRepetidoNaoGastaSeq", testPhoneUndoRepeatedDoesNotSpendSeq},
		{"Celular_ODesfazerChegaAoPullDoPC", testPhoneUndoReachesPull},
		{"Celular_TetoEHasMore", testPhoneCeilingAndHasMore},
		{"Celular_CSRFEHost", testPhoneCSRFAndHost},
		{"Celular_NaoHaRotaParaAprender", testPhoneCannotWrite},
		{"Celular_NadaDeTextoEmLogNemNaURL", testPhoneNoTextInLogs},
		// A retenção (F9b; retencao.go).
		{"Retencao_AtivoComSeisMesesSaiSemLapide", testRetencaoAtivo},
		{"Retencao_LixeiraVencidaDeixaLapideEEvento", testRetencaoLixeira},
		{"Retencao_ALixeiraNaoComecaAntesDoDitado", testRetencaoLixeiraAntesDoDitado},
		// O piso da lixeira que chega pelo push (a decisão do Mauro de
		// 08-out-2026).
		{"Retencao_ALixeiraAntigaQueChegaFica24hEEntaoSai", testRetencaoLixeiraAntigaQueChega},
		{"Retencao_ORestaurarNas24hTrazDeVoltaEALixeiraNovaCalculaDeNovo", testRetencaoRestaurarNoPiso},
		{"Retencao_ALixeiraNovaNoMeioFicaPeloPiso", testRetencaoLixeiraNovaNoMeio},
		{"Retencao_AJanelaDeSeisMesesGanhaDoPiso", testRetencaoJanelaGanhaDoPiso},
		{"Retencao_OReenvioNaoMexeNoPiso", testRetencaoReenvioNaoMexeNoPiso},
		{"Retencao_ALixeiraAntigaQueChegaApareceNoFimDaLixeira", testRetencaoLixeiraAntigaNoFimDaLixeira},
		{"Retencao_SobreposicaoEExcluirDefinitivamenteForaDaJanela", testRetencaoSobreposicao},
		{"Retencao_ALapideViveAJanelaMaisUmDia", testRetencaoLapideNoFimDoMes},
		{"Retencao_OMesmoIDEmDuasContas", testRetencaoDuasContas},
		{"Retencao_OPCParadoNaoRessuscita", testRetencaoPCParado},
		{"Retencao_ORestauradoNoMeioFica", testRetencaoRestauradoNoMeio},
		{"Retencao_OAprendidoDesfeitoNaoEhTocado", testRetencaoAprendidoDesfeito},
		{"Retencao_SeqSemBuracoComOPushJunto", testRetencaoSeqComOPushJunto},
		{"Retencao_CtxCanceladoParaEntreItens", testRetencaoCtxCancelado},
		// A arrumação (F9c; arrumacao.go).
		{"Arrumacao_OEventoDoLastSeqVivoFicaEOOrfaoSai", testRetencaoArrumacaoDosEventos},
		{"Arrumacao_ARespostaGuardadaViveAVidaDoToken", testRetencaoArrumacaoDasMutacoes},
		// Depois de restaurar um backup (F11d; restauracao.go).
		{"Restauracao_OBackupMaisVelhoTrocaOEpochERevogaTudo", testRestauracaoBackupMaisVelho},
		// O ditado que nasce na nuvem (ADR-021, C1; criar.go).
		{"Criar_GravaPeloPontoUnicoSemAparelho", testCriarGravaPeloPontoUnico},
		{"Criar_OMesmoIdNaoCriaDois", testCriarOMesmoIdNaoCriaDois},
		{"Criar_OMesmoIdEmDuasContas", testCriarOMesmoIdEmDuasContas},
		{"Criar_ComEpochVelhoRecusaENaoGrava", testCriarComEpochVelhoRecusaENaoGrava},
		{"Criar_RecusaOActorComAparelho", testCriarRecusaOActorComAparelho},
		{"Criar_InvalidoNaoGastaSeq", testCriarInvalidoNaoGastaSeq},
		{"Criar_ComLapideNaoRecria", testCriarComLapide},
		{"Criar_OPCMudaOQueNasceuNoApp", testCriarOPCMudaOQueONascimentoDoApp},
		{"Criar_Concorrente", testCriarConcorrente},
		{"Situacao_DitadoLapideOuNada", testSituacao},
		// As notas e os marcadores de nota (ADR-022, N7; notas.go).
		{"Notas_CriaEPullComACapacidade", testNotasCriaEPull},
		{"Notas_ConflitoComSeloEMesclaDaListaDeMarcar", testNotasConflitoEMescla},
		{"Notas_ApagarDeVezDeixaALapide", testNotasApagarDeVez},
		{"Notas_MarcadoresRenomearEApagar", testNotasMarcadores},
		{"Notas_VirouNotaDoDitado", testNotasVirouNota},
		{"Notas_ListagensDoCelular", testNotasListagens},
		{"Notas_PullCortaNoOrcamentoDeTexto", testNotasOrcamentoDoPull},
		{"Notas_ApagarMeusDadosLevaAsNotas", testNotasApagarMeusDados},
		{"Notas_ArrumacaoDosEventos", testNotasArrumacao},
		{"Notas_NadaDeTextoEmLog", testNotasNadaDeTextoEmLog},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, newWorld(t, b)) })
	}
}

// AC-016: uma edição feita pela API (o celular) chega ao PC no pull. Até a
// F2, o CRUD não gravava SyncEvent e a edição nunca chegava (ADR-009).
func testAC016EditViaAPIReachesPull(t *testing.T, w *world) {
	id := newID()
	w.mustCreate(pcA1, id, "ditado original")
	first := w.pull(pcA1, 0, 0)
	if len(first.Changes) != 1 || first.LastSeq != 1 {
		t.Fatalf("pull inicial = %+v, quero o ditado no seq 1", first)
	}
	cursor := first.LastSeq

	rec := w.edit(celA, id, 1, "editado no celular")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH pelo celular: %d %s", rec.Code, rec.Body.String())
	}
	if it := decode[itemJSON](t, rec); it.Version != 2 || it.FinalText != "editado no celular" {
		t.Fatalf("PATCH devolveu %+v, quero v2 com o texto novo", it)
	}

	p := w.pull(pcA1, cursor, 0)
	if len(p.Changes) != 1 {
		t.Fatalf("pull depois da edição = %+v, quero exatamente a edição", p)
	}
	c := p.Changes[0]
	if c.Seq != 2 || p.LastSeq != 2 || c.EntityID != id || c.Operation != "upsert" || c.EntityVersion != 2 {
		t.Fatalf("mudança = %+v, quero o ditado v2 no seq 2", c)
	}
	if c.Payload == nil || c.Payload.FinalText != "editado no celular" || c.Payload.Version != 2 {
		t.Fatalf("payload = %+v, quero o texto do celular", c.Payload)
	}
	if c.DeviceID != nil {
		t.Errorf("device_id da mudança = %v, quero nenhum (veio do celular)", *c.DeviceID)
	}
	if c.Payload.DeviceID == nil || *c.Payload.DeviceID != w.devA1 {
		t.Errorf("device_id do ditado = %v, quero o PC onde ele nasceu", c.Payload.DeviceID)
	}

	// Lixeira e restaurar pelo celular também chegam.
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body.String())
	}
	p = w.pull(pcA1, 2, 0)
	if len(p.Changes) != 1 || p.Changes[0].Seq != 3 || p.Changes[0].Payload.DeletedAt == nil {
		t.Fatalf("pull depois da lixeira = %+v, quero o item na lixeira no seq 3", p)
	}
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/restore", celA, nil); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	p = w.pull(pcA1, 3, 0)
	if len(p.Changes) != 1 || p.Changes[0].Seq != 4 || p.Changes[0].Payload.DeletedAt != nil || p.Changes[0].EntityVersion != 4 {
		t.Fatalf("pull depois de restaurar = %+v, quero o item fora da lixeira, v4 no seq 4", p)
	}
	requireContiguous(t, w.seqsOf(w.userA), 4)
}

func testPushCreatePullState(t *testing.T, w *world) {
	id := newID()
	res := w.push(pcA1, create(strings.ToUpper(id), "Bom dia, equipe."))
	if r := res.Results[0]; r.Status != "applied" || r.EntityVersion != 1 || r.Mesclado || r.State != nil {
		t.Fatalf("push = %+v, quero applied v1 sem mescla nem estado", r)
	}
	if _, ok := cloudstore.CanonicalUUID(res.SyncEpoch); !ok {
		t.Fatalf("sync_epoch = %q, quero um UUID", res.SyncEpoch)
	}

	p := w.pull(pcA2, 0, 0)
	if p.SyncEpoch != res.SyncEpoch || p.HasMore || p.LastSeq != 1 || len(p.Changes) != 1 {
		t.Fatalf("pull = %+v, quero um ditado, o mesmo epoch do push", p)
	}
	c := p.Changes[0]
	if c.EntityID != id || c.EntityType != "transcription" || c.Operation != "upsert" || c.EntityVersion != 1 {
		t.Fatalf("mudança = %+v (o id volta em minúsculas)", c)
	}
	if c.DeviceID == nil || *c.DeviceID != w.devA1 {
		t.Errorf("device_id da mudança = %v, quero o PC que enviou", c.DeviceID)
	}
	it := c.Payload
	if it == nil || it.ID != id || it.FinalText != "Bom dia, equipe." || it.RawText != "bruto: Bom dia, equipe." ||
		it.Mode != "smart" || it.Status != "done" || it.SourcePlatform != "windows" || it.Version != 1 {
		t.Fatalf("payload = %+v", it)
	}
	if !it.CreatedAt.Equal(now0.Add(-time.Hour)) || !it.UpdatedAt.Equal(now0) {
		t.Errorf("datas = %v / %v, quero a do ditado e a do servidor", it.CreatedAt, it.UpdatedAt)
	}
	if it.Confidence == nil || *it.Confidence < 0.8999 || *it.Confidence > 0.9001 {
		t.Errorf("confidence = %v, quero 0.9", it.Confidence)
	}
	if it.DeletedAt != nil || it.VersaoEmConflito != nil {
		t.Errorf("ditado novo com lixeira ou selo: %+v", it)
	}
	raw := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", pcA2, nil).Body.String()
	for _, banned := range []string{"target_app", "purge_at", "user_id", "text_version", "trash_version", "text_device_id", "trash_device_id"} {
		if strings.Contains(raw, banned) {
			t.Errorf("o pull expõe %q: %s", banned, raw)
		}
	}
	for _, needed := range []string{`"deleted_at":null`, `"versao_em_conflito":null`} {
		if !strings.Contains(raw, needed) {
			t.Errorf("o pull não traz %s explícito: %s", needed, raw)
		}
	}
}

func testDuplicate(t *testing.T, w *world) {
	// applied
	id := newID()
	m := create(id, "primeira vez")
	first := w.pushOne(pcA1, m)
	again := w.pushOne(pcA1, m)
	if again.Status != "duplicate" || again.Original != "applied" || again.EntityVersion != first.EntityVersion || again.Mesclado {
		t.Fatalf("reenvio = %+v, quero duplicate do applied v%d", again, first.EntityVersion)
	}
	// O reenvio não gastou seq: a próxima gravação é o seq 2.
	w.edit(celA, id, 1, "editado")
	if p := w.pull(pcA1, 1, 0); len(p.Changes) != 1 || p.Changes[0].Seq != 2 {
		t.Fatalf("pull = %+v, quero a edição no seq 2 (o reenvio não grava evento)", p)
	}

	// rejected: gravado, e o reenvio repete o código.
	bad := createWith(newID(), func() map[string]any { d := dictation("x"); d["target_app"] = "chrome"; return d }())
	r1, r2 := w.pushOne(pcA1, bad), w.pushOne(pcA1, bad)
	if r1.Status != "rejected" || r1.Code != "campo_desconhecido" {
		t.Fatalf("primeira vez = %+v, quero rejected campo_desconhecido", r1)
	}
	if r2.Status != "duplicate" || r2.Original != "rejected" || r2.Code != "campo_desconhecido" {
		t.Fatalf("reenvio do rejected = %+v", r2)
	}

	// gone: gravado, e o reenvio repete o motivo.
	old := dictation("antigo")
	old["created_at"] = now0.AddDate(0, -7, 0).Format(time.RFC3339)
	g := createWith(newID(), old)
	g1, g2 := w.pushOne(pcA1, g), w.pushOne(pcA1, g)
	if g1.Status != "gone" || g1.Motivo != "fora_da_janela" {
		t.Fatalf("primeira vez = %+v, quero gone fora_da_janela", g1)
	}
	if g2.Status != "duplicate" || g2.Original != "gone" || g2.Motivo != "fora_da_janela" {
		t.Fatalf("reenvio do gone = %+v", g2)
	}

	// conflict: o reenvio repete a versão, sem gravar de novo.
	w.pushOne(pcA2, change(id, 2, map[string]any{"final_text": "do PC 2"}))
	c := change(id, 2, map[string]any{"final_text": "do PC 1"})
	c1, c2 := w.pushOne(pcA1, c), w.pushOne(pcA1, c)
	if c1.Status != "conflict" || c1.EntityVersion != 4 {
		t.Fatalf("primeira vez = %+v, quero conflict v4", c1)
	}
	if c2.Status != "duplicate" || c2.Original != "conflict" || c2.EntityVersion != 4 {
		t.Fatalf("reenvio do conflict = %+v", c2)
	}

	// Mutação sem UUID nem é gravada: o reenvio é rejected de novo.
	inv := create(newID(), "x")
	inv.MutationID = "mut-1"
	for i := 0; i < 2; i++ {
		if r := w.pushOne(pcA1, inv); r.Status != "rejected" || r.Code != "mutacao_invalida" {
			t.Fatalf("tentativa %d com mutation_id inválido = %+v", i+1, r)
		}
	}
	// 1 criar + 1 edição + 1 do PC 2 + 1 conflito: 4 eventos, sem buraco.
	requireContiguous(t, w.seqsOf(w.userA), 4)
}

func testReusedMutation(t *testing.T, w *world) {
	id1, id2 := newID(), newID()
	m := create(id1, "um")
	w.pushOne(pcA1, m)
	reused := create(id2, "dois")
	reused.MutationID = m.MutationID
	if r := w.pushOne(pcA1, reused); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("mutation_id reusado = %+v, quero rejected mutacao_reusada", r)
	}
	p := w.pull(pcA1, 0, 0)
	if _, ok := changeOf(p, id2); ok || len(p.Changes) != 1 {
		t.Fatalf("pull = %+v, quero só o primeiro ditado", p)
	}
	// O primeiro continua respondendo duplicate.
	if r := w.pushOne(pcA1, m); r.Status != "duplicate" {
		t.Fatalf("reenvio do original = %+v", r)
	}

	// O mesmo mutation_id, para a MESMA entidade, mandado por OUTRO aparelho do
	// mesmo usuário e com outro conteúdo (um historico.db copiado para um
	// segundo PC com o marcador pendente): duplicate perderia a lixeira dele
	// em silêncio. É recusado, e nada muda.
	mv := change(id1, 1, map[string]any{"deleted_at": now0.Format(time.RFC3339)})
	if r := w.pushOne(pcA1, mv); r.Status != "applied" {
		t.Fatalf("lixeira do PC 1 = %+v", r)
	}
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+id1+"/restore", celA, nil); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d", rec.Code)
	}
	if r := w.pushOne(pcA2, mv); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("o mesmo mutation_id pelo PC 2 = %+v, quero rejected mutacao_reusada", r)
	}
	if it := w.get(celA, id1); it.DeletedAt != nil || it.Version != 3 {
		t.Fatalf("o item = %+v, quero fora da lixeira, v3 (a recusa não grava)", it)
	}
	if r := w.pushOne(pcA1, mv); r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 2 {
		t.Fatalf("reenvio pelo PC 1 = %+v, quero duplicate do applied v2", r)
	}
}

// A prova da F2 para "Eco e mescla": um pull no meio mostra a mudança do
// celular (o PC a pula, porque tem marcador), o push com a base velha é
// mesclado, e o estado mesclado chega no pull seguinte com versão nova e o
// device_id do próprio PC. Por isso o PC reconhece o eco pela versão, nunca
// pelo device_id.
func testMergeWithPullInBetween(t *testing.T, w *world) {
	id := newID()
	w.mustCreate(pcA1, id, "texto do PC")
	cursor := w.pull(pcA1, 0, 0).LastSeq

	if rec := w.edit(celA, id, 1, "texto do celular"); rec.Code != http.StatusOK {
		t.Fatalf("edição do celular: %d", rec.Code)
	}
	mid := w.pull(pcA1, cursor, 0)
	if c, ok := changeOf(mid, id); !ok || c.EntityVersion != 2 {
		t.Fatalf("pull no meio = %+v, quero a edição do celular (v2)", mid)
	}
	cursor = mid.LastSeq

	trash := change(id, 1, map[string]any{"deleted_at": now0.Add(-time.Minute).Format(time.RFC3339Nano)})
	rec := w.pushRaw(pcA1, trash)
	if !strings.Contains(rec.Body.String(), `"mesclado":true`) {
		t.Fatalf("o applied da mescla não traz \"mesclado\":true: %s", rec.Body.String())
	}
	r := decode[pushJSON](t, rec).Results[0]
	if r.Status != "applied" || !r.Mesclado || r.EntityVersion != 3 || r.State != nil {
		t.Fatalf("push da lixeira com base velha = %+v, quero applied mesclado v3 sem estado", r)
	}
	if again := w.pushOne(pcA1, trash); again.Status != "duplicate" || again.Original != "applied" || !again.Mesclado || again.EntityVersion != 3 {
		t.Fatalf("reenvio = %+v, quero duplicate que repete o mesclado", again)
	}

	after := w.pull(pcA1, cursor, 0)
	c, ok := changeOf(after, id)
	if !ok || c.EntityVersion != 3 || c.Seq != 3 {
		t.Fatalf("pull seguinte = %+v, quero o estado mesclado v3 no seq 3", after)
	}
	if c.Payload.FinalText != "texto do celular" || c.Payload.DeletedAt == nil {
		t.Errorf("estado mesclado = %+v, quero o texto do celular E a lixeira do PC", c.Payload)
	}
	if c.DeviceID == nil || *c.DeviceID != w.devA1 {
		t.Errorf("device_id = %v, quero o do PC que enviou (e por isso o eco não pode ser pelo device_id)", c.DeviceID)
	}
	// Sem mudança nova, a mesma base velha ainda é mescla e ainda grava.
	if r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "texto do celular"})); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 4 {
		t.Fatalf("mesma mudança com base velha = %+v, quero applied mesclado v4", r)
	}
}

func testTextConflict(t *testing.T, w *world) {
	id := newID()
	w.mustCreate(pcA1, id, "original")
	if r := w.pushOne(pcA2, change(id, 1, map[string]any{"final_text": "do PC 2"})); r.Status != "applied" || r.Mesclado {
		t.Fatalf("PC 2 = %+v, quero applied", r)
	}
	r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "do PC 1"}))
	if r.Status != "conflict" || r.EntityVersion != 3 || r.State == nil {
		t.Fatalf("PC 1 = %+v, quero conflict v3 com o estado", r)
	}
	st := r.State
	if st.FinalText != "do PC 2" || st.Version != 3 || st.VersaoEmConflito == nil ||
		st.VersaoEmConflito.FinalText != "do PC 1" || st.VersaoEmConflito.DeviceID == nil ||
		*st.VersaoEmConflito.DeviceID != w.devA1 || !st.VersaoEmConflito.ConflictAt.Equal(now0) {
		t.Fatalf("estado = %+v / selo %+v, quero o texto do PC 2 e o do PC 1 no selo", st, st.VersaoEmConflito)
	}
	// O selo chega a todos pelo pull, e aparece no CRUD.
	c, _ := changeOf(w.pull(pcA2, 2, 0), id)
	if c.EntityVersion != 3 || c.Payload.VersaoEmConflito == nil || c.Payload.VersaoEmConflito.FinalText != "do PC 1" {
		t.Fatalf("pull do PC 2 = %+v, quero o selo", c)
	}
	if it := w.get(celA, id); it.VersaoEmConflito == nil || it.FinalText != "do PC 2" {
		t.Fatalf("GET = %+v, quero o selo", it)
	}
	// O mesmo texto dos dois lados não é conflito.
	if r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "do PC 2"})); r.Status != "applied" || !r.Mesclado {
		t.Fatalf("mesmo texto = %+v, quero applied mesclado", r)
	}
}

// O selo escolhido pelo celular (ADR-019, "Decisões da F4"): o PATCH com
// resolver_conflito, sobre a versão atual. As duas escolhas limpam o selo e
// chegam ao PC pelo pull; só "trocar" muda o texto.
func testResolveConflict(t *testing.T, w *world) {
	resolve := func(tok, id string, body map[string]any) *httptest.ResponseRecorder {
		return w.do(http.MethodPatch, "/api/v1/transcriptions/"+id, tok, body)
	}
	// sealed cria um ditado com selo: o PC 2 muda o texto, o PC 1 muda o
	// texto com a base velha e perde (o texto dele vai para o selo). v3.
	sealed := func() string {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		if r := w.pushOne(pcA2, change(id, 1, map[string]any{"final_text": "do PC 2"})); r.Status != "applied" {
			t.Fatalf("PC 2 = %+v", r)
		}
		if r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "do PC 1"})); r.Status != "conflict" || r.EntityVersion != 3 {
			t.Fatalf("PC 1 = %+v, quero conflict v3", r)
		}
		return id
	}

	id := sealed() // seq 1 a 3
	for name, body := range map[string]map[string]any{
		"o texto e a escolha juntos": {"base_version": 3, "final_text": "x", "resolver_conflito": "trocar"},
		"escolha desconhecida":       {"base_version": 3, "resolver_conflito": "os dois"},
		"escolha vazia":              {"base_version": 3, "resolver_conflito": ""},
		"sem base":                   {"resolver_conflito": "manter"},
		"escolha que não é texto":    {"base_version": 3, "resolver_conflito": true},
	} {
		if rec := resolve(celA, id, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, quero 400", name, rec.Code)
		}
	}
	if rec := resolve(celA, id, map[string]any{"base_version": 2, "resolver_conflito": "trocar"}); rec.Code != http.StatusConflict {
		t.Errorf("base velha: %d, quero 409", rec.Code)
	}
	if rec := resolve(celB, id, map[string]any{"base_version": 3, "resolver_conflito": "trocar"}); rec.Code != http.StatusNotFound {
		t.Errorf("o selo de outro usuário: %d, quero 404", rec.Code)
	}
	if rec := resolve(pcA1, id, map[string]any{"base_version": 3, "resolver_conflito": "trocar"}); rec.Code != http.StatusForbidden {
		t.Errorf("com o token do PC: %d, quero 403", rec.Code)
	}
	if it := w.get(celA, id); it.Version != 3 || it.VersaoEmConflito == nil {
		t.Fatalf("as recusas mudaram o item: %+v", it)
	}
	requireContiguous(t, w.seqsOf(w.userA), 3)

	// "trocar": o texto do selo vale, e o selo sai. seq 4.
	rec := resolve(celA, id, map[string]any{"base_version": 3, "resolver_conflito": "trocar"})
	if rec.Code != http.StatusOK {
		t.Fatalf("trocar: %d %s", rec.Code, rec.Body.String())
	}
	if it := decode[itemJSON](t, rec); it.Version != 4 || it.FinalText != "do PC 1" || it.VersaoEmConflito != nil {
		t.Fatalf("trocar devolveu %+v, quero v4 com o texto do PC 1 e sem selo", it)
	}
	c, ok := changeOf(w.pull(pcA2, 3, 0), id)
	if !ok || c.Seq != 4 || c.EntityVersion != 4 || c.DeviceID != nil || c.Payload.FinalText != "do PC 1" || c.Payload.VersaoEmConflito != nil {
		t.Fatalf("pull do PC 2 depois de trocar = %+v, quero v4 no seq 4, do celular, sem selo", c)
	}
	// De novo, sem selo: 409, e nada gravado.
	if rec := resolve(celA, id, map[string]any{"base_version": 4, "resolver_conflito": "manter"}); rec.Code != http.StatusConflict {
		t.Errorf("escolher sem selo: %d, quero 409", rec.Code)
	}
	requireContiguous(t, w.seqsOf(w.userA), 4)
	// "trocar" é uma mudança do texto feita no celular: o PC 2, que ainda
	// conhece só a v2 (o texto dele), edita e perde para ela.
	if r := w.pushOne(pcA2, change(id, 2, map[string]any{"final_text": "de novo do PC 2"})); r.Status != "conflict" {
		t.Errorf("PC 2 editando sobre a v2 depois do trocar = %+v, quero conflict", r)
	}

	// "manter": o texto fica, o selo sai, e o texto continua sendo do PC 2,
	// que o escreveu: a edição seguinte dele, com a base velha, não é
	// conflito.
	id2 := sealed()
	rec = resolve(celA, id2, map[string]any{"base_version": 3, "resolver_conflito": "manter"})
	if rec.Code != http.StatusOK {
		t.Fatalf("manter: %d %s", rec.Code, rec.Body.String())
	}
	if it := decode[itemJSON](t, rec); it.Version != 4 || it.FinalText != "do PC 2" || it.VersaoEmConflito != nil {
		t.Fatalf("manter devolveu %+v, quero v4 com o texto do PC 2 e sem selo", it)
	}
	if r := w.pushOne(pcA2, change(id2, 2, map[string]any{"final_text": "PC 2 continua"})); r.Status != "applied" || !r.Mesclado {
		t.Errorf("PC 2 editando o texto que ficou = %+v, quero applied mesclado", r)
	}
}

// O selo escolhido no PC, pelo push (ADR-019, "Decisões da F7c"): a mudança
// com resolver_conflito e o conflict_at do selo. Sobre o mesmo selo, vale
// (applied, sem mescla) e chega aos outros pelo pull; quando o celular
// escolheu antes, ou o texto mudou depois da base, ela cai: vale o que
// chegou primeiro, sem perder texto, e a resposta é mesclada com versão nova
// (o pull leva o estado ao PC). Com a base 0, é recusada.
func testResolveConflictByPush(t *testing.T, w *world) {
	sealed := func() string {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		if r := w.pushOne(pcA2, change(id, 1, map[string]any{"final_text": "do PC 2"})); r.Status != "applied" {
			t.Fatalf("PC 2 = %+v", r)
		}
		if r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "do PC 1"})); r.Status != "conflict" || r.EntityVersion != 3 {
			t.Fatalf("PC 1 = %+v, quero conflict v3", r)
		}
		return id
	}
	escolha := func(e string) map[string]any {
		return map[string]any{"resolver_conflito": e, "conflict_at": now0.Format(time.RFC3339Nano)}
	}

	// O PC 1 fica com a outra: o texto dele (o do selo) vale, e o selo sai
	// para todos.
	id := sealed()
	r := w.pushOne(pcA1, change(id, 3, escolha("trocar")))
	if r.Status != "applied" || r.Mesclado || r.EntityVersion != 4 {
		t.Fatalf("trocar pelo push = %+v, quero applied v4 sem mescla", r)
	}
	c, ok := changeOf(w.pull(pcA2, 3, 0), id)
	if !ok || c.EntityVersion != 4 || c.Payload.FinalText != "do PC 1" || c.Payload.VersaoEmConflito != nil {
		t.Fatalf("pull do PC 2 = %+v, quero v4 com o texto do PC 1 e sem selo", c)
	}
	if it := w.get(celA, id); it.FinalText != "do PC 1" || it.VersaoEmConflito != nil {
		t.Fatalf("o celular vê %+v", it)
	}

	// O celular escolheu antes ("manter"): a escolha do PC 1 cai, o texto
	// do PC 2 fica, e nada some.
	id2 := sealed()
	if rec := w.do(http.MethodPatch, "/api/v1/transcriptions/"+id2, celA, map[string]any{"base_version": 3, "resolver_conflito": "manter"}); rec.Code != http.StatusOK {
		t.Fatalf("o celular escolhe: %d %s", rec.Code, rec.Body.String())
	}
	r = w.pushOne(pcA1, change(id2, 3, escolha("trocar")))
	if r.Status != "applied" || !r.Mesclado || r.EntityVersion != 5 {
		t.Fatalf("a escolha do PC depois da do celular = %+v, quero applied mesclado v5", r)
	}
	if it := w.get(celA, id2); it.FinalText != "do PC 2" || it.VersaoEmConflito != nil {
		t.Fatalf("depois das duas escolhas: %+v, quero a do celular", it)
	}

	// O PC 2 mudou o texto depois da base (o selo continua): a escolha do PC
	// 1 cai, e o selo fica para escolher de novo.
	id3 := sealed()
	if r := w.pushOne(pcA2, change(id3, 3, map[string]any{"final_text": "PC 2 de novo"})); r.Status != "applied" || r.Mesclado {
		t.Fatalf("PC 2 edita = %+v", r)
	}
	r = w.pushOne(pcA1, change(id3, 3, escolha("manter")))
	if r.Status != "applied" || !r.Mesclado {
		t.Fatalf("a escolha sobre o texto que mudou = %+v, quero applied mesclado", r)
	}
	it := w.get(celA, id3)
	if it.FinalText != "PC 2 de novo" || it.VersaoEmConflito == nil || it.VersaoEmConflito.FinalText != "do PC 1" {
		t.Fatalf("depois da escolha que caiu: %+v / %+v, quero o texto do PC 2 e o selo", it, it.VersaoEmConflito)
	}
	c, ok = changeOf(w.pull(pcA1, 0, 0), id3)
	if !ok || c.EntityVersion != it.Version || c.Payload.VersaoEmConflito == nil {
		t.Fatalf("o pull do PC 1 não traz o selo de volta: %+v", c)
	}

	// Com a base 0, a escolha é um campo desconhecido.
	full := dictation("x")
	full["resolver_conflito"] = "manter"
	full["conflict_at"] = now0.Format(time.RFC3339Nano)
	if r := w.pushOne(pcA1, createWith(sealed(), full)); r.Status != "rejected" || r.Code != "campo_desconhecido" {
		t.Errorf("a escolha com a base 0 = %+v, quero campo_desconhecido", r)
	}
}

func testDeleteWinsOldEdit(t *testing.T, w *world) {
	t.Run("a lixeira chega antes", func(t *testing.T) {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE: %d", rec.Code)
		}
		r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "edição antiga"}))
		if r.Status != "conflict" || r.State == nil || r.State.DeletedAt == nil || r.State.FinalText != "original" ||
			r.State.VersaoEmConflito == nil || r.State.VersaoEmConflito.FinalText != "edição antiga" {
			t.Fatalf("edição antiga = %+v, quero conflict: na lixeira, a edição no selo", r)
		}
		if it := w.get(celA, id); it.DeletedAt == nil {
			t.Fatal("a edição antiga tirou o item da lixeira")
		}
	})
	t.Run("a edição chega antes", func(t *testing.T) {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		w.pushOne(pcA2, change(id, 1, map[string]any{"final_text": "editado no PC 2"}))
		r := w.pushOne(pcA1, change(id, 1, map[string]any{"deleted_at": now0.Format(time.RFC3339)}))
		if r.Status != "applied" || !r.Mesclado || r.EntityVersion != 3 {
			t.Fatalf("lixeira com base velha = %+v, quero applied mesclado", r)
		}
		it := w.get(celA, id)
		if it.DeletedAt == nil || it.FinalText != "editado no PC 2" || it.VersaoEmConflito != nil {
			t.Fatalf("item = %+v, quero na lixeira com o texto do PC 2, sem selo", it)
		}
	})
}

func testGone(t *testing.T, w *world) {
	// Apagado de vez: lápide no pull e gone apagado no push.
	id := newID()
	w.mustCreate(pcA1, id, "vai ser apagado")
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil)
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, id, now0); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	p := w.pull(pcA1, 0, 0)
	c, ok := changeOf(p, id)
	if !ok || c.Operation != "tombstone" || c.Seq != 3 || c.Payload != nil || c.EntityVersion != 0 {
		t.Fatalf("pull = %+v, quero a lápide no seq 3, sem payload", p)
	}
	for _, m := range []mut{change(id, 2, map[string]any{"final_text": "x"}), create(id, "de novo")} {
		if r := w.pushOne(pcA1, m); r.Status != "gone" || r.Motivo != "apagado" {
			t.Fatalf("push para o apagado (base %d) = %+v, quero gone apagado", m.BaseVersion, r)
		}
	}
	if rec := w.do(http.MethodGet, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET do apagado: %d, quero 404", rec.Code)
	}
	// Purge de quem não está na lixeira é recusado.
	other := newID()
	w.mustCreate(pcA1, other, "fica")
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, other, now0); !errors.Is(err, cloudstore.ErrNotInTrash) {
		t.Fatalf("Purge fora da lixeira: %v, quero ErrNotInTrash", err)
	}

	// Fora da janela.
	old := dictation("de sete meses atrás")
	old["created_at"] = now0.AddDate(0, -7, 0).Format(time.RFC3339)
	oldID := newID()
	if r := w.pushOne(pcA1, createWith(oldID, old)); r.Status != "gone" || r.Motivo != "fora_da_janela" {
		t.Fatalf("ditado de 7 meses = %+v, quero gone fora_da_janela", r)
	}
	if _, ok := changeOf(w.pull(pcA1, 0, 0), oldID); ok {
		t.Fatal("o ditado fora da janela entrou no pull")
	}
	// Mudança de um ditado que a nuvem não tem (nem lápide).
	if r := w.pushOne(pcA1, change(newID(), 3, map[string]any{"final_text": "x"})); r.Status != "gone" || r.Motivo != "fora_da_janela" {
		t.Fatalf("mudança de ditado desconhecido = %+v, quero gone fora_da_janela", r)
	}
	// Um ditado que saiu da janela depois de criado. O PC sincroniza no meio
	// (um pull em três meses): o token que ninguém usa há 180 dias deixa de
	// valer (F6), e um PC de verdade o usa a cada volta do sync.
	w.clk.Set(now0.AddDate(0, 3, 0))
	w.pull(pcA1, 0, 0)
	w.clk.Set(now0.AddDate(0, 6, 0))
	if r := w.pushOne(pcA1, change(other, 1, map[string]any{"final_text": "tarde"})); r.Status != "gone" || r.Motivo != "fora_da_janela" {
		t.Fatalf("ditado que saiu da janela = %+v, quero gone fora_da_janela", r)
	}
}

func testRejections(t *testing.T, w *world) {
	// O caso "learned antes da F8" invertido (F8b; no Postgres desde a F8c):
	// o learned não é mais tipo_ainda_nao_sincroniza; o payload vazio é
	// campo_invalido, como no ditado.
	cases := []struct {
		name string
		m    mut
		code string
	}{
		{"target_app", createWith(newID(), func() map[string]any { d := dictation("x"); d["target_app"] = "chrome"; return d }()), "campo_desconhecido"},
		{"contexto", createWith(newID(), func() map[string]any { d := dictation("x"); d["nearby_text"] = "x"; return d }()), "campo_desconhecido"},
		{"learned com o payload vazio (antes da F8, tipo_ainda_nao_sincroniza)", mut{MutationID: newID(), EntityType: "learned", EntityID: newID(), Operation: "upsert", Payload: map[string]any{}}, "campo_invalido"},
		{"tipo desconhecido", mut{MutationID: newID(), EntityType: "dictionary_entry", EntityID: newID(), Operation: "upsert"}, "tipo_desconhecido"},
		{"delete de ditado", mut{MutationID: newID(), EntityType: "transcription", EntityID: newID(), Operation: "delete"}, "operacao_invalida"},
		{"campo inválido", createWith(newID(), func() map[string]any { d := dictation("x"); d["mode"] = "turbo"; return d }()), "campo_invalido"},
		{"data no futuro", createWith(newID(), func() map[string]any {
			d := dictation("x")
			d["created_at"] = now0.Add(25 * time.Hour).Format(time.RFC3339)
			return d
		}()), "data_no_futuro"},
		{"entity_id inválido", mut{MutationID: newID(), EntityType: "transcription", EntityID: "t1", Operation: "upsert", Payload: dictation("x")}, "mutacao_invalida"},
	}
	for _, c := range cases {
		if r := w.pushOne(pcA1, c.m); r.Status != "rejected" || r.Code != c.code {
			t.Errorf("%s: %+v, quero rejected %s", c.name, r, c.code)
		}
	}
	// Base do futuro.
	id := newID()
	w.mustCreate(pcA1, id, "a")
	if r := w.pushOne(pcA1, change(id, 5, map[string]any{"final_text": "b"})); r.Status != "rejected" || r.Code != "base_invalida" {
		t.Errorf("base do futuro: %+v", r)
	}
	// Dentro da folga, vale.
	soon := dictation("relógio adiantado")
	soon["created_at"] = now0.Add(23 * time.Hour).Format(time.RFC3339)
	if r := w.pushOne(pcA1, createWith(newID(), soon)); r.Status != "applied" {
		t.Errorf("dentro da folga: %+v, quero applied", r)
	}
	// Nenhuma recusa gravou ditado: só os dois applied estão no pull.
	if p := w.pull(pcA1, 0, 0); len(p.Changes) != 2 || p.LastSeq != 2 {
		t.Errorf("pull = %+v, quero só os dois ditados aceitos", p)
	}
	// Lote grande demais e corpo quebrado.
	big := make([]mut, cloudstore.MaxPushMutations+1)
	for i := range big {
		big[i] = create(newID(), "x")
	}
	if rec := w.pushRaw(pcA1, big...); rec.Code != http.StatusBadRequest {
		t.Errorf("lote de %d: %d, quero 400", len(big), rec.Code)
	}
	if rec := w.do(http.MethodPost, "/api/v1/sync/push", pcA1, `{"mutations":`); rec.Code != http.StatusBadRequest {
		t.Errorf("corpo quebrado: %d, quero 400", rec.Code)
	}
	if p := w.push(pcA1); len(p.Results) != 0 || p.SyncEpoch == "" {
		t.Errorf("push vazio = %+v, quero nenhum resultado e o epoch", p)
	}
}

func testSyncNeedsDevice(t *testing.T, w *world) {
	if rec := w.pushRaw(celA, create(newID(), "x")); rec.Code != http.StatusForbidden {
		t.Errorf("push com a sessão do celular: %d, quero 403", rec.Code)
	}
	if rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", celA, nil); rec.Code != http.StatusForbidden {
		t.Errorf("pull com a sessão do celular: %d, quero 403", rec.Code)
	}
	if rec := w.pushRaw("", create(newID(), "x")); rec.Code != http.StatusUnauthorized {
		t.Errorf("push sem credencial: %d, quero 401", rec.Code)
	}
	if rec := w.do(http.MethodGet, "/api/v1/sync/pull", "token-que-nao-existe", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("pull com token desconhecido: %d, quero 401", rec.Code)
	}
	if got := w.b.Events(t, w.userA); len(got) != 0 {
		t.Errorf("eventos = %v, quero nenhum", got)
	}
}

func testPullPages(t *testing.T, w *world) {
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = newID()
		w.mustCreate(pcA1, ids[i], fmt.Sprintf("ditado %d", i+1)) // seq 1..5
	}
	w.edit(celA, ids[1], 1, "o segundo, editado")                        // seq 6
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+ids[3], celA, nil) // seq 7
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, ids[3], now0); err != nil {
		t.Fatalf("Purge: %v", err) // seq 8, lápide
	}

	type page struct {
		after   int64
		seqs    []int64
		last    int64
		hasMore bool
	}
	for _, pg := range []page{
		{0, []int64{1, 3}, 3, true},
		{3, []int64{5, 6}, 6, true},
		{6, []int64{8}, 8, false},
		{8, nil, 8, false},
		{100, nil, 100, false},
	} {
		p := w.pull(pcA1, pg.after, 2)
		var seqs []int64
		for _, c := range p.Changes {
			seqs = append(seqs, c.Seq)
		}
		if fmt.Sprint(seqs) != fmt.Sprint(pg.seqs) || p.LastSeq != pg.last || p.HasMore != pg.hasMore {
			t.Errorf("pull depois de %d = seqs %v last %d more %v; quero %v %d %v",
				pg.after, seqs, p.LastSeq, p.HasMore, pg.seqs, pg.last, pg.hasMore)
		}
	}
	if c, _ := changeOf(w.pull(pcA1, 6, 2), ids[3]); c.Operation != "tombstone" {
		t.Errorf("seq 8 = %+v, quero a lápide do quarto ditado", c)
	}
	if c, _ := changeOf(w.pull(pcA1, 3, 2), ids[1]); c.Payload == nil || c.Payload.FinalText != "o segundo, editado" {
		t.Errorf("seq 6 = %+v, quero o estado atual do segundo", c)
	}
	// Sem limit, vem tudo de uma vez.
	if p := w.pull(pcA1, 0, 0); len(p.Changes) != 5 || p.HasMore || p.LastSeq != 8 {
		t.Errorf("pull sem limit = %+v", p)
	}
	for _, q := range []string{"after_seq=-1", "after_seq=x", "after_seq=0&limit=-1", "after_seq=0&limit=x"} {
		if rec := w.do(http.MethodGet, "/api/v1/sync/pull?"+q, pcA1, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("pull?%s: %d, quero 400", q, rec.Code)
		}
	}
}

func testIsolation(t *testing.T, w *world) {
	shared := newID()
	w.mustCreate(pcA1, shared, "segredo de A")
	// O mesmo id na outra conta é outra linha, e não revela a primeira.
	if r := w.pushOne(pcB, create(shared, "de B")); r.Status != "applied" || r.EntityVersion != 1 {
		t.Fatalf("o mesmo id na conta B = %+v, quero applied v1 (outra linha)", r)
	}
	if rec := w.edit(celB, shared, 1, "B edita a dele"); rec.Code != http.StatusOK {
		t.Fatalf("B edita a própria linha: %d", rec.Code)
	}
	if it := w.get(celA, shared); it.FinalText != "segredo de A" || it.Version != 1 {
		t.Fatalf("a linha de A mudou: %+v", it)
	}
	pa, pb := w.pull(pcA1, 0, 0), w.pull(pcB, 0, 0)
	if len(pa.Changes) != 1 || pa.Changes[0].Payload.FinalText != "segredo de A" {
		t.Fatalf("pull de A = %+v", pa)
	}
	if len(pb.Changes) != 1 || pb.Changes[0].Payload.FinalText != "B edita a dele" || pb.LastSeq != 2 {
		t.Fatalf("pull de B = %+v (o seq de B é dele: 1 e 2)", pb)
	}
	if pa.SyncEpoch == pb.SyncEpoch {
		t.Error("as duas contas têm o mesmo sync_epoch")
	}

	// Um id que só A tem: B não vê nem altera, nem pelo CRUD nem pelo push.
	onlyA := newID()
	w.mustCreate(pcA1, onlyA, "só de A")
	for _, rec := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/transcriptions/" + onlyA, nil},
		{http.MethodPatch, "/api/v1/transcriptions/" + onlyA, map[string]any{"base_version": 1, "final_text": "invadido"}},
		{http.MethodDelete, "/api/v1/transcriptions/" + onlyA, nil},
		{http.MethodPost, "/api/v1/transcriptions/" + onlyA + "/restore", nil},
	} {
		if got := w.do(rec.method, rec.path, celB, rec.body); got.Code != http.StatusNotFound {
			t.Errorf("%s %s por B: %d, quero 404", rec.method, rec.path, got.Code)
		}
	}
	if r := w.pushOne(pcB, change(onlyA, 1, map[string]any{"final_text": "invadido"})); r.Status != "gone" {
		t.Errorf("push de B para o ditado de A = %+v, quero gone (para B ele não existe)", r)
	}
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userB}, onlyA, now0); !errors.Is(err, cloudstore.ErrNotFound) {
		t.Errorf("Purge de B no ditado de A: %v, quero ErrNotFound", err)
	}
	if it := w.get(celA, onlyA); it.FinalText != "só de A" || it.Version != 1 || it.DeletedAt != nil {
		t.Fatalf("o ditado de A mudou: %+v", it)
	}

	// O mesmo mutation_id nas duas contas: cada uma aplica o seu.
	ma := create(newID(), "A")
	mb := create(newID(), "B")
	mb.MutationID = ma.MutationID
	if ra, rb := w.pushOne(pcA1, ma), w.pushOne(pcB, mb); ra.Status != "applied" || rb.Status != "applied" {
		t.Fatalf("o mesmo mutation_id em A e B = %+v / %+v, quero applied nos dois", ra, rb)
	}
	// Um user_id no corpo é ignorado: grava na conta do token.
	forged := map[string]any{"user_id": w.userB, "mutations": []mut{create(newID(), "forjado")}}
	if rec := w.do(http.MethodPost, "/api/v1/sync/push", pcA1, forged); rec.Code != http.StatusOK {
		t.Fatalf("push com user_id no corpo: %d", rec.Code)
	}
	listB := decode[struct {
		Items []itemJSON `json:"items"`
	}](t, w.do(http.MethodGet, "/api/v1/transcriptions?limit=100", celB, nil))
	var textsB []string
	for _, it := range listB.Items {
		textsB = append(textsB, it.FinalText)
	}
	if fmt.Sprint(textsB) != fmt.Sprint([]string{"B edita a dele", "B"}) &&
		fmt.Sprint(textsB) != fmt.Sprint([]string{"B", "B edita a dele"}) {
		t.Errorf("a lista de B = %q, quero só os dois ditados dela", textsB)
	}
	requireContiguous(t, w.seqsOf(w.userB), 3)

	// O mesmo id nas duas contas, na lixeira das duas, e A exclui de vez: a
	// lápide e o DELETE são de A. A linha de B fica; o pull de B não traz a
	// lápide de A; o push de B no próprio item não é gone.
	both := newID()
	w.mustCreate(pcA1, both, "de A, vai ser apagado")
	w.mustCreate(pcB, both, "de B, fica") // seq 4 de B
	for _, tok := range []string{celA, celB} {
		if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+both, tok, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE do mesmo id (%s): %d", tok, rec.Code)
		}
	} // seq 5 de B
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, both, now0); err != nil {
		t.Fatalf("Purge de A: %v", err)
	}
	if it := w.get(celB, both); it.FinalText != "de B, fica" || it.DeletedAt == nil || it.Version != 2 {
		t.Fatalf("a linha de B depois do Purge de A = %+v, quero a dela, na lixeira, v2", it)
	}
	for _, c := range w.pull(pcB, 0, 0).Changes {
		if c.Operation == "tombstone" {
			t.Fatalf("o pull de B traz a lápide %+v, que é de A", c)
		}
	}
	if r := w.pushOne(pcB, change(both, 2, map[string]any{"deleted_at": nil})); r.Status != "applied" || r.EntityVersion != 3 {
		t.Fatalf("B restaura o próprio item depois do Purge de A = %+v, quero applied v3 (a lápide é de A)", r)
	} // seq 6 de B
	// Um id que só A teve e apagou de vez: para B, é um ditado novo.
	goneInA := newID()
	w.mustCreate(pcA1, goneInA, "de A")
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+goneInA, celA, nil)
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, goneInA, now0); err != nil {
		t.Fatalf("Purge de A: %v", err)
	}
	if r := w.pushOne(pcB, create(goneInA, "de B")); r.Status != "applied" || r.EntityVersion != 1 {
		t.Fatalf("B cria um id que só A apagou = %+v, quero applied v1", r)
	} // seq 7 de B
	if c, ok := changeOf(w.pull(pcA1, 0, 0), goneInA); !ok || c.Operation != "tombstone" {
		t.Fatalf("a lápide de A sumiu: %+v", c)
	}
	requireContiguous(t, w.seqsOf(w.userB), 7)
}

func testCRUD(t *testing.T, w *world) {
	id := newID()
	w.mustCreate(pcA1, id, "a")
	if rec := w.edit(celA, id, 7, "b"); rec.Code != http.StatusConflict {
		t.Errorf("PATCH com base errada: %d, quero 409", rec.Code)
	}
	if rec := w.do(http.MethodPatch, "/api/v1/transcriptions/"+id, celA, map[string]any{"final_text": "b"}); rec.Code != http.StatusBadRequest {
		t.Errorf("PATCH sem base_version: %d, quero 400", rec.Code)
	}
	if rec := w.edit(celA, id, 1, "a\x00b"); rec.Code != http.StatusBadRequest {
		t.Errorf("PATCH com nulo: %d, quero 400", rec.Code)
	}
	for _, bad := range []string{newID(), "nao-e-uuid"} {
		if rec := w.edit(celA, bad, 1, "b"); rec.Code != http.StatusNotFound {
			t.Errorf("PATCH de %q: %d, quero 404", bad, rec.Code)
		}
	}
	// Nenhuma recusa gastou seq.
	requireContiguous(t, w.seqsOf(w.userA), 1)

	// Lixeira duas vezes e restaurar quem não está: sem evento novo.
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil)
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE de novo: %d, quero 204", rec.Code)
	}
	requireContiguous(t, w.seqsOf(w.userA), 2)
	w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/restore", celA, nil)
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/restore", celA, nil); rec.Code != http.StatusOK {
		t.Errorf("restore de novo: %d, quero 200", rec.Code)
	}
	requireContiguous(t, w.seqsOf(w.userA), 3)

	// Listagem: histórico por created_at, lixeira por deleted_at, filtros e
	// cursor.
	older, newer := newID(), newID()
	po := dictation("mais velho")
	po["created_at"] = now0.Add(-48 * time.Hour).Format(time.RFC3339)
	po["mode"] = "literal"
	w.pushOne(pcA2, createWith(older, po))
	w.mustCreate(pcA1, newer, "mais novo")
	list := func(q string) (items []itemJSON, next string, code int) {
		rec := w.do(http.MethodGet, "/api/v1/transcriptions"+q, celA, nil)
		if rec.Code != http.StatusOK {
			return nil, "", rec.Code
		}
		v := decode[struct {
			Items []itemJSON `json:"items"`
			Next  string     `json:"next_cursor"`
		}](t, rec)
		return v.Items, v.Next, rec.Code
	}
	all, _, _ := list("")
	if len(all) != 3 || all[2].ID != older {
		t.Fatalf("lista = %+v, quero 3 com o mais velho por último", all)
	}
	first, next, _ := list("?limit=2")
	rest, last, _ := list("?limit=2&cursor=" + next)
	if len(first) != 2 || next == "" || len(rest) != 1 || last != "" || rest[0].ID != older {
		t.Fatalf("paginação = %d + %d (cursor %q/%q)", len(first), len(rest), next, last)
	}
	if got, _, _ := list("?mode=literal"); len(got) != 1 || got[0].ID != older {
		t.Errorf("filtro mode = %+v", got)
	}
	if got, _, _ := list("?device_id=" + w.devA2); len(got) != 1 || got[0].ID != older {
		t.Errorf("filtro device_id = %+v", got)
	}
	// A busca é por POST, com o q no corpo (F3): o texto nunca vai na URL.
	search := func(body any) (items []itemJSON, next string, code int) {
		rec := w.do(http.MethodPost, "/api/v1/transcriptions/search", celA, body)
		if rec.Code != http.StatusOK {
			return nil, "", rec.Code
		}
		v := decode[struct {
			Items []itemJSON `json:"items"`
			Next  string     `json:"next_cursor"`
		}](t, rec)
		return v.Items, v.Next, rec.Code
	}
	if got, _, _ := search(map[string]any{"q": "MAIS NOVO"}); len(got) != 1 || got[0].ID != newer {
		t.Errorf("busca q = %+v", got)
	}
	if got, _, _ := search(map[string]any{"q": "mais", "mode": "literal"}); len(got) != 1 || got[0].ID != older {
		t.Errorf("busca q com mode = %+v", got)
	}
	// A busca pagina como o GET.
	first2, next2, _ := search(map[string]any{"q": "mais", "limit": 1})
	rest2, last2, _ := search(map[string]any{"q": "mais", "limit": 1, "cursor": next2})
	if len(first2) != 1 || next2 == "" || len(rest2) != 1 || last2 != "" || rest2[0].ID != older {
		t.Errorf("paginação da busca = %d + %d (cursor %q/%q)", len(first2), len(rest2), next2, last2)
	}
	// O q na URL é 400, com qualquer valor, e nada é listado.
	for _, q := range []string{"?q=MAIS+NOVO", "?q=", "?limit=10&q=x"} {
		if _, _, code := list(q); code != http.StatusBadRequest {
			t.Errorf("lista%s: %d, quero 400 (a busca é por POST)", q, code)
		}
	}
	// A busca é por trecho literal: % e _ não são curingas.
	for _, q := range []string{"%", "_", "mais_novo", "mais%novo"} {
		if got, _, _ := search(map[string]any{"q": q}); len(got) != 0 {
			t.Errorf("busca %q achou %d ditados, quero nenhum (curinga do LIKE vazou)", q, len(got))
		}
	}
	if got, _, _ := list("?from=" + now0.Add(-3*time.Hour).Format(time.RFC3339)); len(got) != 2 {
		t.Errorf("filtro from = %d itens, quero 2", len(got))
	}
	for _, q := range []string{"?cursor=lixo", "?device_id=nao-e-uuid", "?limit=x", "?from=ontem", "?mode=%00", "?mode=smart%FF"} {
		if _, _, code := list(q); code != http.StatusBadRequest {
			t.Errorf("lista%s: %d, quero 400", q, code)
		}
	}
	// O caractere nulo na busca e no modo: o Postgres recusaria a consulta
	// (500); os dois Stores respondem 400. (O UTF-8 inválido não chega pelo
	// corpo: o encoding/json o troca por U+FFFD.)
	for _, body := range []any{
		map[string]any{"q": "\x00"}, map[string]any{"q": "a\x00b"},
		map[string]any{"mode": "\x00"}, map[string]any{"cursor": "lixo"}, map[string]any{"device_id": "nao-e-uuid"},
		map[string]any{"from": "ontem"}, map[string]any{"limit": -1}, `{"q":`, `{"limit":"x"}`,
	} {
		if _, _, code := search(body); code != http.StatusBadRequest {
			t.Errorf("busca %v: %d, quero 400", body, code)
		}
	}
	// A lixeira sai pela data de exclusão: o último a entrar vem primeiro.
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+newer, celA, nil)
	w.clk.Set(now0.Add(time.Minute))
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+older, celA, nil)
	if trash, _, _ := list("?trash=true"); len(trash) != 2 || trash[0].ID != older || trash[1].ID != newer {
		t.Errorf("lixeira = %+v, quero o mais velho (excluído por último) primeiro", trash)
	}
}

func testUnmountedRoutes(t *testing.T, w *world) {
	// D1: POST /transcriptions não cria nada (o ditado nasce no PC), e é 404,
	// não 405 (F3: os padrões de resto respondem 404 a qualquer método).
	for _, tok := range []string{celA, pcA1, ""} {
		if rec := w.do(http.MethodPost, "/api/v1/transcriptions", tok, map[string]any{"id": newID(), "source_platform": "windows"}); rec.Code != http.StatusNotFound {
			t.Errorf("POST /transcriptions (%q): %d, quero 404", tok, rec.Code)
		}
	}
	// O /devices é montado na F6 (a lista e o revogar); o renomear (PATCH)
	// continua de fora.
	for _, path := range []string{"/api/v1/dictionary", "/api/v1/settings", "/api/v1/updates/windows/latest"} {
		if rec := w.do(http.MethodGet, path, celA, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, quero 404", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/v1/corrections", "/api/v1/asr/transcribe", "/api/v1/resolve"} {
		if rec := w.do(http.MethodPost, path, celA, map[string]any{}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: %d, quero 404", path, rec.Code)
		}
	}
	if rec := w.do(http.MethodPatch, "/api/v1/devices/"+newID(), celA, map[string]any{"nome": "outro"}); rec.Code != http.StatusNotFound {
		t.Errorf("PATCH /devices/{id}: %d, quero 404", rec.Code)
	}
	if got := w.b.Events(t, w.userA); len(got) != 0 {
		t.Errorf("eventos = %v, quero nenhum", got)
	}
	if rec := w.do(http.MethodGet, "/api/v1/me", celA, nil); rec.Code != http.StatusOK {
		t.Errorf("GET /me: %d", rec.Code)
	}
}

// Nenhum caminho do servidor, do Store ou das regras escreve texto ditado em
// log: nem nos caminhos felizes, nem nas recusas, nem nos conflitos, nem nos
// erros. Captura o log padrão e o slog enquanto exercita todos eles.
func testNoDictatedTextInLogs(t *testing.T, w *world) {
	var buf bytes.Buffer
	var mu sync.Mutex
	sink := lockedWriter{w: &buf, mu: &mu}
	prevOut, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(sink)
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})

	const secret = "SENHA-DITADA-7731"
	id := newID()
	w.mustCreate(pcA1, id, "o "+secret+" original")
	w.edit(celA, id, 1, "editado "+secret)
	w.edit(celA, id, 1, "conflito 409 "+secret)
	w.pushOne(pcA2, change(id, 1, map[string]any{"final_text": "perde " + secret}))
	bad := dictation("recusado " + secret)
	bad["target_app"] = secret
	w.pushOne(pcA1, createWith(newID(), bad))
	w.pushOne(pcA1, createWith(newID(), map[string]any{"final_text": secret}))
	w.do(http.MethodPost, "/api/v1/sync/push", pcA1, `{"mutations":[{"payload":"`+secret)
	w.do(http.MethodGet, "/api/v1/transcriptions?q="+secret, celA, nil)
	w.do(http.MethodPost, "/api/v1/transcriptions/search", celA, map[string]any{"q": secret})
	w.do(http.MethodPost, "/api/v1/transcriptions/search", celA, `{"q":"`+secret)
	w.do(http.MethodGet, "/api/v1/"+secret, celA, nil)
	w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/purge", celA, nil)
	w.pull(pcA1, 0, 0)
	// Um erro de verdade do Store (o aparelho não é do usuário: no Postgres,
	// a chave estrangeira recusa).
	_, err := w.b.Store.Push(context.Background(), cloudstore.Actor{UserID: w.userA, DeviceID: w.devB},
		[]cloudstore.Mutation{{MutationID: newID(), EntityType: "transcription", EntityID: newID(),
			Operation: "upsert", Payload: []byte(`{"source_platform":"windows","raw_text":"` + secret +
				`","final_text":"` + secret + `","mode":"smart","asr_provider":"x","status":"done","created_at":"` +
				now0.Format(time.RFC3339) + `"}`)}}, now0)
	if err == nil {
		t.Error("push com o aparelho de outro usuário não falhou")
	} else if strings.Contains(err.Error(), secret) {
		t.Errorf("o erro do Store carrega o texto ditado: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("texto ditado no log:\n%s", buf.String())
	}
	server := w.logs.String()
	if strings.Contains(server, secret) {
		t.Fatalf("texto ditado no log do servidor:\n%s", server)
	}
	// E o log do servidor existe, com a rota pelo padrão e as contagens.
	for _, want := range []string{`rota="POST /api/v1/sync/push"`, "mutacoes=1", `rota="POST /api/v1/transcriptions/search"`,
		`rota=/api/v1/`, "mudancas="} {
		if !strings.Contains(server, want) {
			t.Errorf("o log do servidor não tem %s:\n%s", want, server)
		}
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Escritores concorrentes de verdade (pushes de dois PCs e edições do
// celular, ao mesmo tempo) e um leitor fazendo pull sem parar: no fim, os seq
// do usuário são 1..N sem buraco nem repetição, cada ditado aponta para o
// evento da sua última mudança, e o leitor, só seguindo o cursor, termina com
// o estado final de todos os ditados (se o cursor tivesse pulado uma
// gravação confirmada fora de ordem, o leitor ficaria com uma versão velha).
func testConcurrentWriters(t *testing.T, w *world) {
	const (
		writers  = 8
		perW     = 6
		editsPer = 3
	)
	all := make([]string, writers*perW)
	for i := range all {
		all[i] = newID()
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2*writers)

	// O leitor começa antes dos escritores e só para depois deles.
	stop := make(chan struct{})
	seen := make(map[string]int64)
	var readerErr error
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	cursor := int64(0)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rec := w.do(http.MethodGet, fmt.Sprintf("/api/v1/sync/pull?after_seq=%d&limit=7", cursor), pcA1, nil)
			if rec.Code != http.StatusOK {
				readerErr = fmt.Errorf("pull: %d", rec.Code)
				return
			}
			p := decodeNoFatal(rec)
			for _, c := range p.Changes {
				if c.Seq <= cursor {
					readerErr = fmt.Errorf("pull devolveu seq %d com cursor %d", c.Seq, cursor)
					return
				}
				seen[c.EntityID] = c.EntityVersion
			}
			cursor = p.LastSeq
		}
	}()

	// Cada escritor cria os seus ditados por um dos dois PCs e os edita pelo
	// celular, sempre sobre a versão que acabou de gravar. Os oito rodam ao
	// mesmo tempo, e as gravações deles se intercalam no contador do usuário.
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int, mine []string) {
			defer wg.Done()
			tok := []string{pcA1, pcA2}[g%2]
			for _, id := range mine {
				rec := w.pushRawNoFatal(tok, create(id, "concorrente "+id))
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"applied"`) {
					errs <- fmt.Errorf("push %s: %d %s", id, rec.Code, rec.Body.String())
					return
				}
			}
			for _, id := range mine {
				for base := int64(1); base <= editsPer; base++ {
					rec := w.editNoFatal(celA, id, base, fmt.Sprintf("edição %d", base))
					if rec.Code != http.StatusOK {
						errs <- fmt.Errorf("edição %s v%d: %d %s", id, base, rec.Code, rec.Body.String())
						return
					}
				}
			}
		}(g, all[g*perW:(g+1)*perW])
	}
	wg.Wait()
	close(stop)
	readerWG.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if readerErr != nil {
		t.Fatal(readerErr)
	}

	total := len(all) * (1 + editsPer)
	events := w.b.Events(t, w.userA)
	seqs := make([]int64, 0, len(events))
	for _, e := range events {
		seqs = append(seqs, e.Seq)
	}
	requireContiguous(t, seqs, total)

	// O leitor alcança o fim só seguindo o cursor (com teto de voltas: um
	// cursor que não anda não pode prender o teste).
	for rounds := 0; ; rounds++ {
		if rounds > total {
			t.Fatalf("o pull não terminou em %d voltas (cursor %d)", total, cursor)
		}
		p := w.pull(pcA1, cursor, 7)
		for _, c := range p.Changes {
			seen[c.EntityID] = c.EntityVersion
		}
		cursor = p.LastSeq
		if !p.HasMore {
			break
		}
	}
	if cursor != int64(total) {
		t.Errorf("cursor final = %d, quero %d", cursor, total)
	}
	for _, id := range all {
		if seen[id] != 1+editsPer {
			t.Errorf("o leitor terminou com %s na v%d, quero v%d (o cursor pulou uma gravação)", id, seen[id], 1+editsPer)
		}
	}
	// Cada ditado aponta para o evento da sua última mudança.
	final := w.pull(pcA1, 0, 0)
	bySeq := make(map[int64]cloudstore.Event, len(events))
	for _, e := range events {
		bySeq[e.Seq] = e
	}
	for _, c := range final.Changes {
		e, ok := bySeq[c.Seq]
		if !ok || e.EntityID != c.EntityID || e.EntityVersion != c.EntityVersion {
			t.Errorf("ditado %s no seq %d v%d, e o evento desse seq é %+v", c.EntityID, c.Seq, c.EntityVersion, e)
		}
	}
}

// O mesmo mutation_id mandado ao mesmo tempo por vários pedidos: um applied,
// o resto duplicate, um evento só.
func testConcurrentDuplicate(t *testing.T, w *world) {
	const n = 8
	id := newID()
	m := create(id, "uma vez só")
	statuses := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := w.pushRawNoFatal(pcA1, m)
			p := decodeNoFatalPush(rec)
			if rec.Code != http.StatusOK || len(p.Results) != 1 {
				statuses <- fmt.Sprintf("http %d", rec.Code)
				return
			}
			statuses <- p.Results[0].Status
		}()
	}
	wg.Wait()
	close(statuses)
	count := map[string]int{}
	for s := range statuses {
		count[s]++
	}
	if count["applied"] != 1 || count["duplicate"] != n-1 {
		t.Fatalf("resultados = %v, quero 1 applied e %d duplicate", count, n-1)
	}
	requireContiguous(t, w.seqsOf(w.userA), 1)
}

// O teto de cada texto (cloudstore.MaxTextBytes) e o orçamento de texto de
// uma página (cloudstore.PageTextBudget). Um texto no teto entra; um byte
// acima é recusado no push (campo_invalido) e no CRUD (400). E ditados no
// teto não saem numa página só: o pull e a listagem param no item que chega
// ao orçamento e dizem que há mais, sem perder nem repetir nada, com uma
// lápide no meio das entidades.
func testTextLimits(t *testing.T, w *world) {
	teto := strings.Repeat("a", cloudstore.MaxTextBytes)
	acima := teto + "a"
	small := newID()
	w.mustCreate(pcA1, small, "pequeno") // seq 1
	for _, field := range []string{"raw_text", "final_text"} {
		d := dictation("x")
		d[field] = acima
		if r := w.pushOne(pcA1, createWith(newID(), d)); r.Status != "rejected" || r.Code != "campo_invalido" {
			t.Fatalf("%s acima do teto = %+v, quero rejected campo_invalido", field, r)
		}
	}
	if r := w.pushOne(pcA1, change(small, 1, map[string]any{"final_text": acima})); r.Status != "rejected" || r.Code != "campo_invalido" {
		t.Fatalf("mudança acima do teto = %+v, quero rejected campo_invalido", r)
	}
	if rec := w.edit(celA, small, 1, acima); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH acima do teto: %d, quero 400", rec.Code)
	}

	// Cada ditado grande tem o bruto e o final no teto. perPage deles enchem
	// o orçamento; são perPage+4 no total, metade antes e metade depois de
	// uma lápide (seq de bigs[k]: k+2 na primeira metade, k+4 na segunda).
	perItem := 2 * cloudstore.MaxTextBytes
	perPage := (cloudstore.PageTextBudget + perItem - 1) / perItem
	bigs := make([]string, perPage+4)
	half := len(bigs) / 2
	push := func(ids []string) {
		for len(ids) > 0 {
			n := min(4, len(ids)) // 4 x 128 KiB cabem no teto de 1 MiB do corpo
			var muts []mut
			for _, id := range ids[:n] {
				d := dictation("x")
				d["raw_text"], d["final_text"] = teto, teto
				muts = append(muts, createWith(id, d))
			}
			for _, r := range w.push(pcA1, muts...).Results {
				if r.Status != "applied" {
					t.Fatalf("ditado grande = %+v, quero applied", r)
				}
			}
			ids = ids[n:]
		}
	}
	for i := range bigs {
		bigs[i] = newID()
	}
	push(bigs[:half]) // seq 2..half+1
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+small, celA, nil)
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, small, now0); err != nil {
		t.Fatalf("Purge: %v", err) // a lápide fica no seq half+3
	}
	push(bigs[half:])
	total := int64(len(bigs) + 3)
	requireContiguous(t, w.seqsOf(w.userA), int(total))
	seqOf := func(k int) int64 {
		if k < half {
			return int64(k + 2)
		}
		return int64(k + 4)
	}

	first := w.pull(pcA1, 0, 0)
	var textLen int
	var gotBigs []string
	for _, c := range first.Changes {
		if c.Payload != nil {
			textLen += len(c.Payload.RawText) + len(c.Payload.FinalText)
			gotBigs = append(gotBigs, c.EntityID)
		}
	}
	if !first.HasMore || len(gotBigs) != perPage || len(first.Changes) != perPage+1 || first.LastSeq != seqOf(perPage-1) {
		t.Fatalf("primeira página: %d mudanças (%d ditados), last_seq %d, has_more %v; quero %d ditados e a lápide até o seq %d, e has_more",
			len(first.Changes), len(gotBigs), first.LastSeq, first.HasMore, perPage, seqOf(perPage-1))
	}
	if textLen < cloudstore.PageTextBudget || textLen-perItem >= cloudstore.PageTextBudget {
		t.Errorf("a primeira página tem %d bytes de texto: devia parar no item que chega a %d", textLen, cloudstore.PageTextBudget)
	}
	if c, ok := changeOf(first, small); !ok || c.Operation != "tombstone" || c.Seq != int64(half+3) {
		t.Errorf("a lápide no meio = %+v, quero no seq %d", c, half+3)
	}
	rest := w.pull(pcA1, first.LastSeq, 0)
	if rest.HasMore || rest.LastSeq != total || len(rest.Changes) != len(bigs)-perPage {
		t.Fatalf("segunda página: %d mudanças, last_seq %d, has_more %v; quero %d até %d, sem mais",
			len(rest.Changes), rest.LastSeq, rest.HasMore, len(bigs)-perPage, total)
	}
	for _, c := range rest.Changes {
		gotBigs = append(gotBigs, c.EntityID)
	}
	if fmt.Sprint(gotBigs) != fmt.Sprint(bigs) {
		t.Errorf("o pull, seguindo o cursor, trouxe %d ditados fora de ordem ou repetidos", len(gotBigs))
	}

	// A listagem corta igual, e o cursor dela continua de onde parou.
	type listPage struct {
		Items []itemJSON `json:"items"`
		Next  string     `json:"next_cursor"`
	}
	p1 := decode[listPage](t, w.do(http.MethodGet, "/api/v1/transcriptions?limit=100", celA, nil))
	if len(p1.Items) != perPage || p1.Next == "" {
		t.Fatalf("listagem: %d itens (cursor %q), quero %d e um cursor", len(p1.Items), p1.Next, perPage)
	}
	p2 := decode[listPage](t, w.do(http.MethodGet, "/api/v1/transcriptions?limit=100&cursor="+p1.Next, celA, nil))
	if len(p2.Items) != len(bigs)-perPage || p2.Next != "" {
		t.Fatalf("listagem, segunda página: %d itens (cursor %q), quero %d e nenhum cursor", len(p2.Items), p2.Next, len(bigs)-perPage)
	}
	seen := map[string]bool{}
	for _, it := range append(p1.Items, p2.Items...) {
		seen[it.ID] = true
	}
	for _, id := range bigs {
		if !seen[id] {
			t.Errorf("a listagem perdeu o ditado %s", id)
		}
	}
}

// Depois de um applied mesclado, o PC não guarda a versão devolvida e manda a
// mudança seguinte do mesmo item com a base velha ("Eco e mescla"). O campo
// que ele mesmo mudou por último não pode contar como "mudou depois da base":
// o valor é o que ele mandou. Sem isso (as sondas S1 e S2 da revisão da F2),
// o restaurar sumia sem aviso e a segunda edição virava conflito com a
// primeira. O campo mudado por OUTRO aparelho continua valendo as regras de
// sempre: apagar vence, e texto contra texto é conflito.
func testOwnMergedChange(t *testing.T, w *world) {
	trash := map[string]any{"deleted_at": now0.Add(-time.Minute).Format(time.RFC3339Nano)}
	restore := map[string]any{"deleted_at": nil}

	t.Run("S1: restaurar a própria lixeira mesclada", func(t *testing.T) {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		cursor := w.pull(pcA1, 0, 0).LastSeq
		if rec := w.edit(celA, id, 1, "do celular"); rec.Code != http.StatusOK {
			t.Fatalf("edição do celular: %d", rec.Code)
		}
		if r := w.pushOne(pcA1, change(id, 1, trash)); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 3 {
			t.Fatalf("lixeira do PC com base velha = %+v, quero applied mesclado v3", r)
		}
		// O PC restaura antes do pull: a base ainda é 1.
		if r := w.pushOne(pcA1, change(id, 1, restore)); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 4 {
			t.Fatalf("restaurar a própria lixeira = %+v, quero applied mesclado v4", r)
		}
		it := w.get(celA, id)
		if it.DeletedAt != nil || it.FinalText != "do celular" || it.VersaoEmConflito != nil || it.Version != 4 {
			t.Fatalf("item = %+v, quero fora da lixeira, com o texto do celular, sem selo, v4", it)
		}
		c, ok := changeOf(w.pull(pcA1, cursor, 0), id)
		if !ok || c.EntityVersion != 4 || c.Payload.DeletedAt != nil || c.Payload.FinalText != "do celular" {
			t.Fatalf("pull seguinte = %+v, quero v4 fora da lixeira, com o texto do celular", c)
		}
	})
	t.Run("S1 por outro PC: a lixeira do PC 1 vence o restaurar do PC 2", func(t *testing.T) {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		w.edit(celA, id, 1, "do celular")
		w.pushOne(pcA1, change(id, 1, trash))
		if r := w.pushOne(pcA2, change(id, 1, restore)); r.Status != "applied" || !r.Mesclado {
			t.Fatalf("restaurar do PC 2 = %+v, quero applied mesclado", r)
		}
		if it := w.get(celA, id); it.DeletedAt == nil {
			t.Fatalf("o restaurar do PC 2, com base anterior à lixeira do PC 1, tirou o item da lixeira: %+v", it)
		}
	})
	t.Run("S2: segunda edição sobre a própria edição mesclada", func(t *testing.T) {
		id := newID()
		w.mustCreate(pcA1, id, "original")
		w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil)          // v2
		w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/restore", celA, nil) // v3
		if r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "A"})); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 4 {
			t.Fatalf("primeira edição = %+v, quero applied mesclado v4", r)
		}
		if r := w.pushOne(pcA1, change(id, 1, map[string]any{"final_text": "B"})); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 5 {
			t.Fatalf("segunda edição do mesmo PC = %+v, quero applied mesclado v5 (e não conflito com a primeira)", r)
		}
		if it := w.get(celA, id); it.FinalText != "B" || it.VersaoEmConflito != nil {
			t.Fatalf("item = %+v, quero o texto mais novo do PC, sem selo", it)
		}
		// Outro PC, com a mesma base velha, continua em conflito.
		r := w.pushOne(pcA2, change(id, 1, map[string]any{"final_text": "C"}))
		if r.Status != "conflict" || r.State == nil || r.State.FinalText != "B" ||
			r.State.VersaoEmConflito == nil || r.State.VersaoEmConflito.FinalText != "C" {
			t.Fatalf("edição do PC 2 = %+v, quero conflict com o texto do PC 1 e o do PC 2 no selo", r)
		}
	})
	t.Run("recomeço: o ditado inteiro com base 0 sobre o que o próprio PC criou", func(t *testing.T) {
		// O recomeço manda o ditado inteiro sem base. O texto que o próprio
		// PC criou (e ninguém mudou depois) ele conhece: o texto novo dele
		// vale. De outro PC, o mesmo envio é conflito.
		id := newID()
		w.mustCreate(pcA1, id, "original")
		if r := w.pushOne(pcA1, create(id, "editado no PC 1")); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 2 {
			t.Fatalf("recomeço do PC que criou = %+v, quero applied mesclado v2", r)
		}
		if it := w.get(celA, id); it.FinalText != "editado no PC 1" || it.VersaoEmConflito != nil {
			t.Fatalf("item = %+v, quero o texto do recomeço, sem selo", it)
		}
		if r := w.pushOne(pcA2, create(id, "do PC 2")); r.Status != "conflict" || r.State == nil ||
			r.State.FinalText != "editado no PC 1" || r.State.VersaoEmConflito == nil {
			t.Fatalf("recomeço de outro PC = %+v, quero conflict com o texto do PC 2 no selo", r)
		}
	})
}

// "Excluir definitivamente" pela rota (F3): só da lixeira (fora dela, 409),
// deixa a lápide, chega ao PC no pull e não alcança o ditado de outro usuário.
func testPurgeRoute(t *testing.T, w *world) {
	id := newID()
	w.mustCreate(pcA1, id, "vai ser apagado de vez") // seq 1
	purge := func(tok, id string) int {
		return w.do(http.MethodPost, "/api/v1/transcriptions/"+id+"/purge", tok, nil).Code
	}
	if code := purge(celA, id); code != http.StatusConflict {
		t.Fatalf("purge fora da lixeira: %d, quero 409", code)
	}
	w.do(http.MethodDelete, "/api/v1/transcriptions/"+id, celA, nil) // seq 2
	if code := purge(celB, id); code != http.StatusNotFound {
		t.Fatalf("purge pelo outro usuário: %d, quero 404", code)
	}
	for _, bad := range []string{newID(), "nao-e-uuid"} {
		if code := purge(celA, bad); code != http.StatusNotFound {
			t.Errorf("purge de %q: %d, quero 404", bad, code)
		}
	}
	if code := purge(celA, id); code != http.StatusNoContent {
		t.Fatalf("purge da lixeira: %d, quero 204", code)
	} // seq 3
	if rec := w.do(http.MethodGet, "/api/v1/transcriptions/"+id, celA, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("depois do purge, GET: %d, quero 404", rec.Code)
	}
	c, ok := changeOf(w.pull(pcA1, 2, 0), id)
	if !ok || c.Operation != "tombstone" || c.Seq != 3 {
		t.Fatalf("o pull depois do purge = %+v, quero a lápide no seq 3", c)
	}
	if r := w.pushOne(pcA1, change(id, 2, map[string]any{"final_text": "de volta"})); r.Status != "gone" || r.Motivo != "apagado" {
		t.Fatalf("push depois do purge = %+v, quero gone apagado", r)
	}
	if code := purge(celA, id); code != http.StatusNotFound {
		t.Fatalf("purge de novo: %d, quero 404", code)
	}
	requireContiguous(t, w.seqsOf(w.userA), 3)
}
