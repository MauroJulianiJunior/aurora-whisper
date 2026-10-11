package syncengine

import (
	"reflect"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As tabelas das decisões puras (ADR-019, F7a). Rodam no Linux também: nada
// aqui toca banco, rede ou sistema.

const idDoTeste = "0199a000-0000-7000-8000-0000000000a1"

func i64(v int64) *int64 { return &v }

// estado monta o estado de um ditado com a version local, a server_version e
// o marcador (em voo com m-1 e a versão enviada; vazio sem marcador).
func estado(version int64, sv *int64, campos int, versaoEnviada int64) storage.EstadoDoItem {
	e := storage.EstadoDoItem{ID: idDoTeste, Linha: &storage.Transcription{ID: idDoTeste, Version: version, ServerVersion: sv, SyncState: storage.SituacaoNormal}}
	if versaoEnviada > 0 {
		e.Marcador = &storage.Marcador{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Campos: campos, MutationID: "m-1", VersaoEnviada: versaoEnviada}
	}
	return e
}

func itemDoTeste(versao int64) *Item {
	pc := "0199a000-0000-7000-8000-0000000000c1"
	return &Item{
		ID: idDoTeste, DeviceID: &pc, SourcePlatform: "windows", RawText: "bruto", FinalText: "venceu",
		Mode: "smart", ASRProvider: "whisper-local", Status: "done",
		CreatedAt: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC),
		Version: versao, VersaoEmConflito: &Selo{FinalText: "perdeu", DeviceID: &pc, ConflictAt: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)},
	}
}

// A tabela do push: os cinco status, com e sem "mudou" (a version local
// diferente da enviada), com e sem mescla, e o duplicate pelo original.
func TestDecidirResultado(t *testing.T) {
	estadoConflito := itemDoTeste(5)
	gravado := LinhaDoItem(*estadoConflito)
	for _, c := range []struct {
		nome  string
		e     storage.EstadoDoItem
		r     MutationResult
		quero storage.AcaoLocal
		tipo  string
	}{
		// applied
		{"applied", estado(3, i64(2), 1, 3), MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 3},
			storage.AcaoLocal{ServerVersion: i64(3), Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoAplicado},
		{"applied e mudou", estado(4, i64(2), 1, 3), MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 3},
			storage.AcaoLocal{ServerVersion: i64(3), Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorReenviar}, TipoAplicado},
		{"applied mesclado: a versão não é guardada", estado(3, i64(1), 2, 3), MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 4, Mesclado: true},
			storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoMesclado},
		{"applied mesclado e mudou", estado(5, i64(1), 2, 3), MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 4, Mesclado: true},
			storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorReenviar}, TipoMesclado},
		{"criar (sem server_version)", estado(1, nil, 0, 1), MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 1},
			storage.AcaoLocal{ServerVersion: i64(1), Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoAplicado},
		// duplicate: segue o original
		{"duplicate de applied", estado(3, i64(2), 1, 3), MutationResult{MutationID: "m-1", Status: StatusDuplicate, Original: StatusApplied, EntityVersion: 3},
			storage.AcaoLocal{ServerVersion: i64(3), Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoAplicado},
		{"duplicate de applied mesclado", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusDuplicate, Original: StatusApplied, EntityVersion: 4, Mesclado: true},
			storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoMesclado},
		{"duplicate de conflict: sem state, a versão não é gravada", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusDuplicate, Original: StatusConflict, EntityVersion: 5},
			storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoConflito},
		{"duplicate de conflict e mudou", estado(4, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusDuplicate, Original: StatusConflict, EntityVersion: 5},
			storage.AcaoLocal{Marcador: storage.MarcadorReenviar}, TipoConflito},
		{"duplicate de rejected", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusDuplicate, Original: StatusRejected, Code: "campo_invalido"},
			storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: "campo_invalido", Marcador: storage.MarcadorSai}, TipoRecusado},
		{"duplicate de gone apagado", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusDuplicate, Original: StatusGone, Motivo: MotivoApagado},
			storage.AcaoLocal{ApagarLinha: true}, TipoApagado},
		// conflict
		{"conflict: aplica o state, sem marcador", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusConflict, EntityVersion: 5, State: estadoConflito},
			storage.AcaoLocal{Gravar: &gravado, ServerVersion: i64(5), Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoConflito},
		{"conflict e mudou: fica com a base velha", estado(4, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusConflict, EntityVersion: 5, State: estadoConflito},
			storage.AcaoLocal{Marcador: storage.MarcadorReenviar}, TipoConflito},
		{"conflict com state de outro id: não grava", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusConflict, EntityVersion: 5,
			State: &Item{ID: "0199a000-0000-7000-8000-0000000000ff", Version: 5, CreatedAt: time.Now()}},
			storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}, TipoConflito},
		// rejected
		{"mutacao_reusada: outro id", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusRejected, Code: CodigoMutacaoReusada},
			storage.AcaoLocal{Marcador: storage.MarcadorReenviar}, TipoReenviar},
		{"base_invalida: sobe inteiro", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusRejected, Code: CodigoBaseInvalida},
			storage.AcaoLocal{ZerarServerVersion: true, Marcador: storage.MarcadorReenviar}, TipoReenviar},
		{"outra recusa", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusRejected, Code: "data_no_futuro"},
			storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: "data_no_futuro", Marcador: storage.MarcadorSai}, TipoRecusado},
		{"outra recusa e mudou: a edição nova vai", estado(4, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusRejected, Code: "campo_invalido"},
			storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: "campo_invalido", Marcador: storage.MarcadorReenviar}, TipoRecusado},
		{"código fora do formato não vai ao banco", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusRejected, Code: "Texto: <b>"},
			storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: CodigoRespostaDesconhecida, Marcador: storage.MarcadorSai}, TipoRecusado},
		// gone
		{"gone apagado: apaga a linha e o marcador", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusGone, Motivo: MotivoApagado},
			storage.AcaoLocal{ApagarLinha: true}, TipoApagado},
		{"gone fora da janela: mantém e para", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusGone, Motivo: MotivoForaDaJanela},
			storage.AcaoLocal{Situacao: storage.SituacaoForaDaJanela, Marcador: storage.MarcadorSai}, TipoForaDaJanela},
		// o que não é do envio em curso
		{"resposta de outro envio: ignorada", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-velho", Status: StatusGone, Motivo: MotivoApagado},
			storage.AcaoLocal{}, TipoIgnorado},
		{"sem marcador: ignorada", estado(3, i64(1), 0, 0), MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 9},
			storage.AcaoLocal{}, TipoIgnorado},
		{"o ditado sumiu no voo: o marcador sai", storage.EstadoDoItem{ID: idDoTeste, Marcador: &storage.Marcador{MutationID: "m-1", VersaoEnviada: 3}},
			MutationResult{MutationID: "m-1", Status: StatusApplied, EntityVersion: 4}, storage.AcaoLocal{Marcador: storage.MarcadorSai}, TipoIgnorado},
		{"status desconhecido", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: "talvez"},
			storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: CodigoRespostaDesconhecida, Marcador: storage.MarcadorSai}, TipoRecusado},
		{"gone com motivo desconhecido", estado(3, i64(1), 1, 3), MutationResult{MutationID: "m-1", Status: StatusGone, Motivo: "talvez"},
			storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: CodigoRespostaDesconhecida, Marcador: storage.MarcadorSai}, TipoRecusado},
	} {
		t.Run(c.nome, func(t *testing.T) {
			d := DecidirResultado(c.e, c.r)
			if d.Tipo != c.tipo {
				t.Errorf("tipo = %q; quero %q", d.Tipo, c.tipo)
			}
			if !reflect.DeepEqual(d.Acao, c.quero) {
				t.Errorf("ação:\n tenho %+v\n quero %+v", d.Acao, c.quero)
			}
		})
	}
}

// A tabela do aplicador: o novo, o com marcador, o eco (pela versão, NUNCA
// pelo device_id), o mais novo, a lápide, a recusa por regra.
func TestDecidirMudanca(t *testing.T) {
	item := itemDoTeste(4)
	linhaDoItem := LinhaDoItem(*item)
	semMarcador := func(sv *int64) storage.EstadoDoItem { return estado(2, sv, 0, 0) }
	comMarcador := estado(2, i64(1), 1, 2)
	upsert := Change{Seq: 9, EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4, Payload: item}
	outroPC := "0199a000-0000-7000-8000-0000000000c2"
	upsertDeOutroPC := upsert
	upsertDeOutroPC.DeviceID = &outroPC

	grande := *item
	grande.FinalText = string(make([]byte, storage.MaxTextoBytes+1))
	invalidoUTF8 := *item
	invalidoUTF8.FinalText = "\xff"
	semData := *item
	semData.CreatedAt = time.Time{}
	outroID := *item
	outroID.ID = "0199a000-0000-7000-8000-0000000000ff"

	recusado := semMarcador(i64(2))
	recusado.Linha.SyncState = storage.SituacaoRecusado

	// A reconciliação (F7c, D3): sem server_version e sem marcador; mudou
	// aqui (depois do esquecer, ou sem esquecer) ou não.
	semVersaoIgual := semMarcador(nil)
	semVersaoIgual.Linha.FinalText = item.FinalText
	semVersaoIgual.MudouAqui = true
	semVersaoOutroTexto := semMarcador(nil)
	semVersaoOutroTexto.Linha.FinalText = "editado aqui"
	semVersaoOutroTexto.MudouAqui = true
	semVersaoNaLixeira := semMarcador(nil)
	semVersaoNaLixeira.Linha.FinalText = item.FinalText
	lixeira := time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC)
	semVersaoNaLixeira.Linha.DeletedAt = &lixeira
	semVersaoNaLixeira.MudouAqui = true
	semVersaoComMarcador := estado(2, nil, 0, 2)
	semVersaoComMarcador.Linha.FinalText = "editado aqui"
	semVersaoComMarcador.MudouAqui = true
	velhoOutroTexto := semVersaoOutroTexto
	velhoOutroTexto.MudouAqui = false
	velhoNaLixeira := semVersaoNaLixeira
	velhoNaLixeira.MudouAqui = false
	naLixeiraDaNuvem := *item
	naLixeiraDaNuvem.DeletedAt = &lixeira
	upsertNaLixeira := upsert
	upsertNaLixeira.Payload = &naLixeiraDaNuvem
	linhaNaLixeira := LinhaDoItem(naLixeiraDaNuvem)

	for _, c := range []struct {
		nome  string
		e     storage.EstadoDoItem
		c     Change
		quero storage.AcaoLocal
		tipo  string
	}{
		{"novo aqui", storage.EstadoDoItem{ID: idDoTeste}, upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"mais novo que o guardado", semMarcador(i64(2)), upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"sem server_version, igual: aplica e guarda a versão", semVersaoIgual, upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"sem server_version, igual na lixeira dos dois lados", semVersaoNaLixeira, upsertNaLixeira, storage.AcaoLocal{Gravar: &linhaNaLixeira, ServerVersion: i64(4)}, TipoRecebido},
		{"sem server_version, outro texto: reconcilia", semVersaoOutroTexto, upsert, storage.AcaoLocal{Marcador: storage.MarcadorCriar}, TipoReconciliado},
		{"sem server_version, na lixeira só aqui: reconcilia", semVersaoNaLixeira, upsert, storage.AcaoLocal{Marcador: storage.MarcadorCriar}, TipoReconciliado},
		// Só a lixeira difere, e quem está na lixeira é a nuvem: o lado daqui
		// nunca venceria (restaurar contra a lixeira de outro aparelho não
		// vale), e reconciliar não convergiria. Vale o da nuvem.
		{"sem server_version, na lixeira só na nuvem: aplica o da nuvem", semVersaoIgual, upsertNaLixeira, storage.AcaoLocal{Gravar: &linhaNaLixeira, ServerVersion: i64(4)}, TipoRecebido},
		{"sem server_version, outro texto e na lixeira só na nuvem: reconcilia (o texto vai para o selo)", semVersaoOutroTexto, upsertNaLixeira, storage.AcaoLocal{Marcador: storage.MarcadorCriar}, TipoReconciliado},
		{"sem server_version e com marcador: pula", semVersaoComMarcador, upsert, storage.AcaoLocal{}, TipoPulado},
		{"sem server_version, outro texto, mas não mudou aqui: vale o da nuvem, sem selo", velhoOutroTexto, upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"sem server_version, na lixeira só aqui, mas não mudou aqui: vale o da nuvem", velhoNaLixeira, upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"com server_version e outro texto: aplica (a mudança de outro aparelho)", semVersaoOutroTexto2(), upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"eco: a versão é a guardada", semMarcador(i64(4)), upsert, storage.AcaoLocal{}, TipoEco},
		{"eco de outro PC na mudança: é a versão que manda", semMarcador(i64(4)), upsertDeOutroPC, storage.AcaoLocal{}, TipoEco},
		{"o device_id deste PC não é eco se a versão difere", semMarcador(i64(3)), upsertDeOutroPC, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4)}, TipoRecebido},
		{"com marcador: pula", comMarcador, upsert, storage.AcaoLocal{}, TipoPulado},
		{"recusado volta ao normal", recusado, upsert, storage.AcaoLocal{Gravar: &linhaDoItem, ServerVersion: i64(4), Situacao: storage.SituacaoNormal}, TipoRecebido},
		{"lápide: apaga", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpTombstone}, storage.AcaoLocal{ApagarLinha: true}, TipoApagado},
		{"lápide com marcador: pula (o push recebe o gone)", comMarcador, Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpTombstone}, storage.AcaoLocal{}, TipoPulado},
		{"lápide do que não está aqui", storage.EstadoDoItem{ID: idDoTeste}, Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpTombstone}, storage.AcaoLocal{}, TipoEco},
		// recusa por regra: não aplica, e o cursor anda (quem anda é o motor)
		{"tipo desconhecido", semMarcador(i64(2)), Change{EntityType: "learned", EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4, Payload: item}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"operação desconhecida", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: "delete"}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"sem payload", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"texto acima do teto", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4, Payload: &grande}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"UTF-8 inválido", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4, Payload: &invalidoUTF8}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"sem data", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4, Payload: &semData}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"payload de outro id", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: 4, Payload: &outroID}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"versão zero", semMarcador(i64(2)), Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, Payload: item}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"id que não é UUID", storage.EstadoDoItem{ID: "nao-e-uuid"}, Change{EntityType: storage.EntidadeDitado, EntityID: "nao-e-uuid", Operation: OpTombstone}, storage.AcaoLocal{}, TipoNaoAplicado},
		{"linha de outro dono", storage.EstadoDoItem{ID: idDoTeste, DeOutroDono: true}, upsert, storage.AcaoLocal{}, TipoNaoAplicado},
	} {
		t.Run(c.nome, func(t *testing.T) {
			d := DecidirMudanca(c.e, c.c)
			if d.Tipo != c.tipo {
				t.Errorf("tipo = %q; quero %q", d.Tipo, c.tipo)
			}
			if !reflect.DeepEqual(d.Acao, c.quero) {
				t.Errorf("ação:\n tenho %+v\n quero %+v", d.Acao, c.quero)
			}
		})
	}
}

// LinhaDoItem leva tudo o que a nuvem manda, menos o que é de quem grava, e
// nunca target_app.
func TestLinhaDoItem(t *testing.T) {
	it := itemDoTeste(4)
	apagado := time.Date(2026, 10, 6, 11, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	it.DeletedAt = &apagado
	l := LinhaDoItem(*it)
	if l.ID != idDoTeste || l.FinalText != "venceu" || l.RawText != "bruto" || *l.DeviceID != *it.DeviceID ||
		l.TargetApp != nil || l.Version != 0 || l.ServerVersion != nil || l.UserID != "" {
		t.Errorf("linha: %+v", l)
	}
	if l.DeletedAt.Location() != time.UTC || !l.DeletedAt.Equal(apagado) {
		t.Errorf("deleted_at: %v", l.DeletedAt)
	}
	if l.Conflito == nil || l.Conflito.FinalText != "perdeu" {
		t.Errorf("selo: %+v", l.Conflito)
	}
	it.UpdatedAt = time.Time{}
	if l := LinhaDoItem(*it); !l.UpdatedAt.Equal(it.CreatedAt) {
		t.Errorf("sem updated_at: %v", l.UpdatedAt)
	}
}

// O ditado que o celular criou na nuvem (ADR-021, C1) chega sem aparelho e com
// a origem web (ou android, do app nativo): o PC o aplica como "recebido", como
// o de outro PC, guarda a origem como veio e não o põe na fila (aplicar não
// ecoa). Uma mudança dele que chega depois também se aplica, e a mesma versão
// é o eco.
func TestDecidirMudanca_DitadoDoApp(t *testing.T) {
	for _, origem := range []string{"web", "android"} {
		t.Run(origem, func(t *testing.T) {
			item := &Item{
				ID: idDoTeste, DeviceID: nil, SourcePlatform: origem, RawText: "bruto do app", FinalText: "Dito no celular.",
				Mode: "smart", ASRProvider: "openai", ASRModel: strPtr("gpt-4o-mini-transcribe"), Status: "done",
				CreatedAt: time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC),
				Version: 1,
			}
			upsert := func(versao int64) Change {
				it := *item
				it.Version = versao
				return Change{Seq: 9, EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpUpsert, EntityVersion: versao, DeviceID: nil, Payload: &it}
			}

			d := DecidirMudanca(storage.EstadoDoItem{ID: idDoTeste}, upsert(1))
			if d.Tipo != TipoRecebido || d.Acao.Gravar == nil {
				t.Fatalf("o ditado novo do app: tipo %q, ação %+v; quero recebido com a linha", d.Tipo, d.Acao)
			}
			g := d.Acao.Gravar
			if g.SourcePlatform != origem || g.DeviceID != nil || g.FinalText != "Dito no celular." || g.ASRProvider != "openai" || g.TargetApp != nil {
				t.Errorf("linha gravada: %+v", g)
			}
			if d.Acao.ServerVersion == nil || *d.Acao.ServerVersion != 1 || d.Acao.Marcador != storage.MarcadorFica || d.Acao.ApagarLinha {
				t.Errorf("ação: %+v; quero a versão 1 da nuvem, sem marcador (aplicar não ecoa)", d.Acao)
			}

			// A mudança seguinte (o celular editou) se aplica sobre a versão 1.
			if d := DecidirMudanca(semMarcadorComVersao(1), upsert(2)); d.Tipo != TipoRecebido || d.Acao.Gravar == nil || d.Acao.Gravar.SourcePlatform != origem {
				t.Errorf("a versão 2: tipo %q, ação %+v", d.Tipo, d.Acao)
			}
			// A mesma versão é o eco.
			if d := DecidirMudanca(semMarcadorComVersao(1), upsert(1)); d.Tipo != TipoEco {
				t.Errorf("a mesma versão: tipo %q, quero eco", d.Tipo)
			}
			// A lápide dele apaga, como a de qualquer ditado.
			tomb := Change{EntityType: storage.EntidadeDitado, EntityID: idDoTeste, Operation: OpTombstone}
			if d := DecidirMudanca(semMarcadorComVersao(1), tomb); d.Tipo != TipoApagado || !d.Acao.ApagarLinha {
				t.Errorf("a lápide: tipo %q, ação %+v", d.Tipo, d.Acao)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// semMarcadorComVersao é o ditado que a nuvem já confirmou na versão sv, sem
// marcador na fila.
func semMarcadorComVersao(sv int64) storage.EstadoDoItem { return estado(1, i64(sv), 0, 0) }

// semVersaoOutroTexto2 é um ditado que a nuvem já confirmou (versão 2) com
// outro texto: a mudança de outro aparelho é aplicada, sem reconciliar.
func semVersaoOutroTexto2() storage.EstadoDoItem {
	e := estado(2, i64(2), 0, 0)
	e.Linha.FinalText = "editado aqui"
	return e
}
