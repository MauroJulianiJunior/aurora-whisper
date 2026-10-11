package syncengine

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões puras do aprendido (ADR-019, "Decisões da F8", F8f2), caso a
// caso: as tabelas "O que o PC faz com cada par do pull" e "O que o PC faz
// com a resposta do push", e a mutação. Nada aqui toca banco, rede ou
// arquivo; os percursos pelo storage de verdade e pelo lote do dicionário
// estão em aprendido_percurso_test.go.

const mutacaoDoPar = "0199a000-0000-7000-8000-0000000000e1"

var (
	idSF               = dictionary.IDDoPar("sf")
	desfeitoEm         = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	semSituacaoGravada = &storage.NovaSituacao{}
)

// fioDoPar é o par como a nuvem o manda (o payload do pull, o state).
func fioDoPar(variante, termo string, restrito bool, dominio string, desfeito bool) *Aprendido {
	a := &Aprendido{ID: dictionary.IDDoPar(variante), Variant: variante, Term: termo, Restricted: restrito}
	if dominio != "" {
		a.Domain = texto(dominio)
	}
	if desfeito {
		a.UndoneAt = &desfeitoEm
	}
	return a
}

// doPull é a mudança do pull com o par dado.
func doPull(versao int64, a *Aprendido) Change {
	return Change{Seq: 9, EntityType: storage.EntidadeAprendido, EntityID: a.ID, Operation: OpUpsert,
		EntityVersion: versao, PayloadAprendido: a}
}

// parDe é o par da nuvem (sem domínio, ativo).
func parDe(variante, termo string) dictionary.ParDaNuvem {
	return dictionary.ParDaNuvem{Variante: variante, Termo: termo}
}

// guardadoDe é o par da nuvem guardado no espelho, com a versão.
func guardadoDe(versao int64, p dictionary.ParDaNuvem) *storage.ParGuardado {
	return &storage.ParGuardado{Versao: i64(versao), Par: p}
}

// projDe é a projeção de uma variante sem domínio.
func projDe(variante, termo string) *dictionary.ParDoPainel {
	return &dictionary.ParDoPainel{ID: dictionary.IDDoPar(variante), Variante: variante, Termo: termo}
}

// confirmado é "sf" -> "ECF", que a nuvem confirmou na versão 3, sem mudança
// daqui (a versão local 2 é a coberta).
func confirmado() storage.EstadoDoAprendido {
	return storage.EstadoDoAprendido{ID: idSF, TemLinha: true, ServerVersion: i64(3), VersaoBase: 2, VersaoLocal: 2,
		Projecao: projDe("sf", "ECF")}
}

// semLinha é o id que o espelho não conhece.
func semLinha() storage.EstadoDoAprendido { return storage.EstadoDoAprendido{ID: idSF} }

// emVoo é e com o marcador em voo, enviado com a versão local dada.
func emVoo(e storage.EstadoDoAprendido, enviada int64) storage.EstadoDoAprendido {
	e.Marcador = &storage.Marcador{EntityType: storage.EntidadeAprendido, EntityID: e.ID, MutationID: mutacaoDoPar,
		VersaoEnviada: enviada}
	return e
}

func com(e storage.EstadoDoAprendido, f func(*storage.EstadoDoAprendido)) storage.EstadoDoAprendido {
	f(&e)
	return e
}

func noArquivo(p *dictionary.ParDoPainel) ArquivoDoPar { return ArquivoDoPar{Projecao: p} }

// ─── A mutação ───

// O upsert leva o par como o arquivo o tem, na lista fechada (domain null,
// nunca ausente), com a base do espelho; o delete vai sem payload; o par da
// nuvem guardado nunca é a base; e o envio em voo repete tudo.
func TestMutacaoDoAprendido(t *testing.T) {
	restrito := &dictionary.ParDoPainel{ID: dictionary.IDDoPar("nfe"), Variante: "nfe", Termo: "NF-e", Restrito: true,
		Dominio: "fiscal", EmAliasRestrito: true}
	for _, c := range []struct {
		nome    string
		e       storage.EstadoDoAprendido
		op      string
		base    int64
		payload string
	}{
		{"upsert sem domínio, com a base", confirmado(), OpUpsert, 3,
			`{"variant":"sf","term":"ECF","restricted":false,"domain":null}`},
		{"upsert com o domínio e a restrição efetiva (a forma local não sobe)",
			storage.EstadoDoAprendido{ID: restrito.ID, TemLinha: true, VersaoLocal: 1, Projecao: restrito}, OpUpsert, 0,
			`{"variant":"nfe","term":"NF-e","restricted":true,"domain":"fiscal"}`},
		{"upsert sem a base: 0 (o par da nuvem guardado não é base)",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.ServerVersion = nil
				e.Nuvem = guardadoDe(7, parDe("sf", "EFD"))
			}), OpUpsert, 0, `{"variant":"sf","term":"ECF","restricted":false,"domain":null}`},
		{"delete: sem payload, com a base", com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Projecao = nil }),
			OpDelete, 3, ""},
		{"delete sem a base", com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.Projecao, e.ServerVersion = nil, nil
			e.Nuvem = guardadoDe(4, parDe("sf", "ECF"))
		}), OpDelete, 0, ""},
	} {
		t.Run(c.nome, func(t *testing.T) {
			m, err := MutacaoDoAprendido(c.e, "0199A000-0000-7000-8000-0000000000E1")
			if err != nil {
				t.Fatal(err)
			}
			if m.MutationID != mutacaoDoPar || m.EntityType != storage.EntidadeAprendido || m.EntityID != c.e.ID ||
				m.Operation != c.op || m.BaseVersion != c.base {
				t.Errorf("a mutação: %+v", m)
			}
			if c.payload == "" {
				if m.Payload != nil {
					t.Errorf("o delete com payload: %s", m.Payload)
				}
				raw, _ := json.Marshal(m)
				if !json.Valid(raw) || !jsonTem(t, raw, "payload", nil) {
					t.Errorf("o delete no fio: %s", raw)
				}
				return
			}
			if string(m.Payload) != c.payload {
				t.Errorf("o payload: %s; quero %s", m.Payload, c.payload)
			}
		})
	}

	// O envio em voo: a projeção guardada não muda no voo, e o reenvio sai
	// igual, com o mesmo id, mesmo que a pessoa tenha mudado o par (a versão
	// local subiu) ou que um par da nuvem tenha sido guardado como pendente.
	voo := emVoo(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Projecao = nil; e.VersaoLocal = 3 }), 3)
	antes, _ := MutacaoDoAprendido(voo, voo.Marcador.MutationID)
	depois := com(voo, func(e *storage.EstadoDoAprendido) {
		e.VersaoLocal = 4 // o Aprender do mesmo par durante o voo
		e.Nuvem = guardadoDe(5, parDe("sf", "EFD"))
	})
	m, _ := MutacaoDoAprendido(depois, depois.Marcador.MutationID)
	if !reflect.DeepEqual(antes, m) || m.Operation != OpDelete {
		t.Errorf("o reenvio mudou: %+v\nantes %+v", m, antes)
	}
}

// jsonTem confere que o objeto JSON raw tem a chave com o valor dado.
func jsonTem(t *testing.T, raw []byte, chave string, valor any) bool {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	v, ok := obj[chave]
	return ok && reflect.DeepEqual(v, valor)
}

// ─── A resposta do push ───

// A tabela "O que o PC faz com a resposta do push", caso a caso: o applied
// guarda a versão; o conflict guarda o state e a etiqueta (o desfeito e o
// trocado); o base_invalida zera a base; o nada_a_desfazer tira o marcador;
// o mutacao_reusada vai com outro id; os outros rejected ficam "recusado",
// com o código e a projeção recusada; e a mudança feita no voo sempre sobe
// pela varredura, nunca pelo marcador.
func TestDecidirResultadoDoAprendido(t *testing.T) {
	// O upsert em voo: a varredura viu a mudança (a versão local 3) e o
	// mandou com a base 3.
	upsert := emVoo(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 3 }), 3)
	upsertMudou := com(upsert, func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 4 })
	// O delete em voo: o Desfazer daqui.
	del := com(upsert, func(e *storage.EstadoDoAprendido) {
		e.Projecao = nil
		e.Situacao = storage.EspelhoDesfeitoAqui
	})
	delMudou := com(del, func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 4 })
	upsertSemBase := com(upsert, func(e *storage.EstadoDoAprendido) { e.ServerVersion = nil; e.VersaoBase = 0 })

	desfeitoNaNuvem := fioDoPar("sf", "ECF", false, "", true)
	trocadoNaNuvem := fioDoPar("sf", "EFD", false, "", false)
	parDesfeito := dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true}
	// A troca enviada (a projeção guardada, que não muda em voo) é o par que
	// este PC perde num conflict: a linha de estado do painel.
	perdidoSF := &storage.ParPerdido{Variante: "sf", Termo: "ECF"}

	sai := storage.MarcadorSai
	r := func(status string, f func(*MutationResult)) MutationResult {
		res := MutationResult{MutationID: mutacaoDoPar, Status: status}
		if f != nil {
			f(&res)
		}
		return res
	}
	rej := func(code string) MutationResult {
		return r(StatusRejected, func(m *MutationResult) { m.Code = code })
	}
	conflito := func(v int64, a *Aprendido) MutationResult {
		return r(StatusConflict, func(m *MutationResult) { m.EntityVersion, m.StateAprendido = v, a })
	}
	recusa := func(code string) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoRecusado, Motivo: code},
			Marcador: sai}
	}

	for _, c := range []struct {
		nome  string
		e     storage.EstadoDoAprendido
		r     MutationResult
		quero storage.AcaoDoAprendido
		tipo  string
	}{
		// applied: a versão é guardada, sempre (nunca há mescla).
		{"applied do upsert: a base anda, e o marcador sai", upsert,
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4}, Cobre: i64(3), Marcador: sai}, TipoAplicado},
		{"applied do upsert com mudança no voo: a base cobre só a enviada, e a mudança sobe pela varredura", upsertMudou,
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4}, Cobre: i64(3), Marcador: sai}, TipoAplicado},
		{"applied do delete: a base desfeita, a situação e o texto saem (fica o id)",
			com(del, func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(3, parDe("sf", "ECF")) }),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4, Desfeita: true}, Cobre: i64(3),
				Marcador: sai, Situacao: semSituacaoGravada, LimparNuvem: true}, TipoAplicado},
		{"applied do delete com o Aprender no voo", delMudou,
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4, Desfeita: true}, Cobre: i64(3),
				Marcador: sai, Situacao: semSituacaoGravada}, TipoAplicado},
		{"applied não tira o par da nuvem mais novo que ele",
			com(upsert, func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(9, parDe("sf", "EFD")) }),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4}, Cobre: i64(3), Marcador: sai}, TipoAplicado},
		{"applied tira uma etiqueta velha",
			com(upsert, func(e *storage.EstadoDoAprendido) {
				e.Situacao, e.Nuvem = storage.EspelhoTrocadoEmOutro, guardadoDe(3, parDe("sf", "EFD"))
			}),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4}, Cobre: i64(3), Marcador: sai,
				Situacao: semSituacaoGravada, LimparNuvem: true}, TipoAplicado},
		{"applied do marcador do epoch novo (sem mudança daqui): a versão coberta não muda", emVoo(confirmado(), 2),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 1 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 1}, Marcador: sai}, TipoAplicado},
		{"duplicate de applied: a versão da primeira vez", upsert,
			r(StatusDuplicate, func(m *MutationResult) { m.Original, m.EntityVersion = StatusApplied, 4 }),
			storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4}, Cobre: i64(3), Marcador: sai}, TipoAplicado},
		{"applied mesclado (fora do contrato): a versão não é guardada", upsert,
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion, m.Mesclado = 4, true }),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoMesclado},
		{"applied sem versão (fora do contrato): nada vai ao executor que ele recusaria", upsert,
			r(StatusApplied, nil), storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoMesclado},

		// conflict: vale o primeiro na nuvem, e o desfazer vence (D4).
		{"conflict com o par desfeito: a etiqueta desfeito em outro aparelho, e o state fica para valer aqui", upsert,
			conflito(5, desfeitoNaNuvem),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai, Nuvem: guardadoDe(5, parDesfeito),
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoDesfeitoEmOutro}, Perdido: perdidoSF}, TipoConflito},
		{"conflict com outro termo: a etiqueta trocado em outro aparelho", upsertSemBase,
			conflito(5, trocadoNaNuvem),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai, Nuvem: guardadoDe(5, parDe("sf", "EFD")),
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoTrocadoEmOutro}, Perdido: perdidoSF}, TipoConflito},
		{"conflict com outra restrição: também trocado", upsert,
			conflito(5, fioDoPar("sf", "ECF", true, "fiscal", false)),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai,
				Nuvem:    guardadoDe(5, dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "fiscal"}),
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoTrocadoEmOutro}, Perdido: perdidoSF}, TipoConflito},
		{"conflict com mudança no voo: a base fica velha, o state fica guardado, e a etiqueta espera", upsertMudou,
			conflito(5, desfeitoNaNuvem),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai, Nuvem: guardadoDe(5, parDesfeito)}, TipoConflito},
		{"duplicate de conflict: o par guardado da mesma versão dá a etiqueta",
			com(upsert, func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(5, parDesfeito) }),
			r(StatusDuplicate, func(m *MutationResult) { m.Original, m.EntityVersion = StatusConflict, 5 }),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai,
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoDesfeitoEmOutro}, Perdido: perdidoSF}, TipoConflito},
		{"duplicate de conflict sem o par: o state chega no pull", upsert,
			r(StatusDuplicate, func(m *MutationResult) { m.Original, m.EntityVersion = StatusConflict, 5 }),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoConflito},
		{"duplicate de conflict com o par de outra versão: o state chega no pull",
			com(upsert, func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(4, parDesfeito) }),
			r(StatusDuplicate, func(m *MutationResult) { m.Original, m.EntityVersion = StatusConflict, 5 }),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoConflito},
		{"conflict com o state de outro id: nada dele é guardado", upsert,
			conflito(5, fioDoPar("nfe", "NF-e", false, "", false)),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoConflito},
		{"conflict com o state que o storage recusaria (restrito sem domínio)", upsert,
			conflito(5, fioDoPar("sf", "ECF", true, "", false)),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoConflito},
		{"conflict com o state sem versão", upsert, conflito(0, desfeitoNaNuvem),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoConflito},
		{"conflict sem state", upsert, conflito(5, nil), storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoConflito},

		// rejected
		{"base_invalida no upsert: zera a base e manda de novo com 0", upsert, rej(CodigoBaseInvalida),
			storage.AcaoDoAprendido{ZerarBase: true, Marcador: storage.MarcadorReenviar}, TipoReenviar},
		{"base_invalida com mudança no voo: zera a base, e a mudança sobe pela varredura", upsertMudou, rej(CodigoBaseInvalida),
			storage.AcaoDoAprendido{ZerarBase: true, Marcador: sai}, TipoReenviar},
		{"base_invalida com a base já 0 (fora do contrato): recusado, sem laço", upsertSemBase, rej(CodigoBaseInvalida),
			recusa(CodigoBaseInvalida), TipoRecusado},
		{"base_invalida no delete (que não olha a base): recusado", del, rej(CodigoBaseInvalida),
			recusa(CodigoBaseInvalida), TipoRecusado},
		{"nada_a_desfazer: o marcador sai, a base sai, e o desfazer fica confirmado",
			com(del, func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(3, parDe("sf", "ECF")) }),
			rej(CodigoNadaADesfazer),
			storage.AcaoDoAprendido{ZerarBase: true, Cobre: i64(3), Marcador: sai, Situacao: semSituacaoGravada,
				LimparNuvem: true}, TipoNadaADesfazer},
		{"nada_a_desfazer sem a base", com(del, func(e *storage.EstadoDoAprendido) {
			e.ServerVersion, e.Situacao = nil, storage.EspelhoNenhuma
		}), rej(CodigoNadaADesfazer),
			storage.AcaoDoAprendido{Cobre: i64(3), Marcador: sai}, TipoNadaADesfazer},
		{"nada_a_desfazer com o Aprender no voo: a mudança sobe pela varredura", delMudou, rej(CodigoNadaADesfazer),
			storage.AcaoDoAprendido{ZerarBase: true, Cobre: i64(3), Marcador: sai, Situacao: semSituacaoGravada},
			TipoNadaADesfazer},
		{"duplicate de nada_a_desfazer", del,
			r(StatusDuplicate, func(m *MutationResult) { m.Original, m.Code = StatusRejected, CodigoNadaADesfazer }),
			storage.AcaoDoAprendido{ZerarBase: true, Cobre: i64(3), Marcador: sai, Situacao: semSituacaoGravada},
			TipoNadaADesfazer},
		{"nada_a_desfazer num upsert (fora do contrato): recusado", upsert, rej(CodigoNadaADesfazer),
			recusa(CodigoNadaADesfazer), TipoRecusado},
		{"mutacao_reusada: outro id", upsert, rej(CodigoMutacaoReusada),
			storage.AcaoDoAprendido{Marcador: storage.MarcadorReenviar}, TipoReenviar},
		{"mutacao_reusada com mudança no voo", delMudou, rej(CodigoMutacaoReusada),
			storage.AcaoDoAprendido{Marcador: sai}, TipoReenviar},
		{"outra recusa: recusado, com o código, e a projeção guardada é a recusada", upsert, rej("campo_invalido"),
			recusa("campo_invalido"), TipoRecusado},
		{"a nuvem anterior à F8", del, rej("tipo_ainda_nao_sincroniza"), recusa("tipo_ainda_nao_sincroniza"), TipoRecusado},
		{"outra recusa com mudança no voo: o marcador sai mesmo assim", upsertMudou, rej("campo_desconhecido"),
			recusa("campo_desconhecido"), TipoRecusado},
		{"duplicate de rejected", upsert,
			r(StatusDuplicate, func(m *MutationResult) { m.Original, m.Code = StatusRejected, "campo_invalido" }),
			recusa("campo_invalido"), TipoRecusado},
		{"código fora do formato não vai ao banco", upsert, rej("Campo: <b>"), recusa(CodigoRespostaDesconhecida), TipoRecusado},
		{"gone (o aprendido não tem)", upsert, r(StatusGone, func(m *MutationResult) { m.Motivo = MotivoApagado }),
			recusa(CodigoRespostaDesconhecida), TipoRecusado},
		{"status desconhecido", upsert, r("talvez", nil), recusa(CodigoRespostaDesconhecida), TipoRecusado},

		// o que não é do envio em curso
		{"resposta de outro envio: ignorada", upsert,
			MutationResult{MutationID: mutacao1, Status: StatusApplied, EntityVersion: 4}, storage.AcaoDoAprendido{}, TipoIgnorado},
		{"sem marcador: ignorada", confirmado(),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }), storage.AcaoDoAprendido{}, TipoIgnorado},
		{"o marcador fora de voo: ignorada",
			com(upsert, func(e *storage.EstadoDoAprendido) { e.Marcador = &storage.Marcador{EntityID: idSF} }),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }), storage.AcaoDoAprendido{}, TipoIgnorado},
		{"sem a linha do espelho: ignorada", emVoo(semLinha(), 0),
			r(StatusApplied, func(m *MutationResult) { m.EntityVersion = 4 }), storage.AcaoDoAprendido{}, TipoIgnorado},
	} {
		t.Run(c.nome, func(t *testing.T) {
			d := DecidirResultadoDoAprendido(c.e, c.r)
			if d.Tipo != c.tipo {
				t.Errorf("tipo = %q; quero %q", d.Tipo, c.tipo)
			}
			if !reflect.DeepEqual(d.Acao, c.quero) {
				t.Errorf("ação:\n tenho %s\n quero %s", acaoEmTexto(d.Acao), acaoEmTexto(c.quero))
			}
		})
	}
}

// O Desfazer seguido do Aprender do mesmo par, que deixa o arquivo como era,
// também é uma mudança deste PC (a versão local sobe no ato) e também ganha
// a etiqueta quando perde na nuvem.
func TestDecidirResultadoDoAprendido_DesfazerEAprenderGanhaAEtiqueta(t *testing.T) {
	// A projeção é a confirmada (o arquivo voltou ao que era), e a versão
	// local subiu duas vezes no ato; a varredura pôs o upsert com a base 3.
	e := emVoo(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 4 }), 4)
	m, err := MutacaoDoAprendido(e, mutacaoDoPar)
	if err != nil || m.Operation != OpUpsert || m.BaseVersion != 3 {
		t.Fatalf("a mutação: %+v, %v", m, err)
	}
	d := DecidirResultadoDoAprendido(e, MutationResult{MutationID: mutacaoDoPar, Status: StatusConflict,
		EntityVersion: 4, StateAprendido: fioDoPar("sf", "ECF", false, "", true)})
	if d.Acao.Situacao == nil || d.Acao.Situacao.Situacao != storage.EspelhoDesfeitoEmOutro || d.Acao.Cobre == nil ||
		*d.Acao.Cobre != 4 || d.Acao.Marcador != storage.MarcadorSai {
		t.Errorf("a etiqueta: %s", acaoEmTexto(d.Acao))
	}
}

// ─── O pull ───

// O passo 1 do pull: a tabela "O que o PC faz com cada par do pull", na
// ordem, até onde ela decide sem o lote (o eco, o pulado, o arquivo
// indisponível), e a recusa por regra antes dela.
func TestPlanejarPar(t *testing.T) {
	ecf := fioDoPar("sf", "ECF", false, "", false)
	efd := fioDoPar("sf", "EFD", false, "", false)
	desfeito := fioDoPar("sf", "ECF", false, "", true)
	mudouAqui := com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 3 })
	esperadaEFD := projDe("sf", "EFD")
	aplicando := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, esperadaEFD, guardadoDe(4, parDe("sf", "EFD"))
	})
	etiquetado := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Nuvem = storage.EspelhoDesfeitoEmOutro, guardadoDe(5, dictionary.ParDaNuvem{Variante: "sf",
			Termo: "ECF", Desfeito: true})
	})

	// recusas por regra
	outroID := doPull(4, ecf)
	outroID.PayloadAprendido = &Aprendido{ID: dictionary.IDDoPar("nfe"), Variant: "sf", Term: "ECF"}
	varianteDeOutroID := doPull(4, &Aprendido{ID: idSF, Variant: "nfe", Term: "NF-e"})
	restritoSemDominio := doPull(4, fioDoPar("sf", "ECF", true, "", false))
	dominioVazio := doPull(4, &Aprendido{ID: idSF, Variant: "sf", Term: "ECF", Domain: texto("")})
	termoGrande := doPull(4, &Aprendido{ID: idSF, Variant: "sf", Term: string(make([]byte, 81))})
	tipoDitado := doPull(4, ecf)
	tipoDitado.EntityType = storage.EntidadeDitado
	lapide := doPull(4, ecf)
	lapide.Operation = OpTombstone
	semPayload := doPull(4, ecf)
	semPayload.PayloadAprendido = nil

	for _, c := range []struct {
		nome     string
		e        storage.EstadoDoAprendido
		c        Change
		arq      ArquivoDoPar
		lote     bool
		tipo     string
		etiqueta storage.SituacaoDoEspelho
	}{
		{"eco: a versão é a da base", confirmado(), doPull(3, ecf), noArquivo(projDe("sf", "ECF")), false, TipoEco, ""},
		{"mais velha que a base: eco", confirmado(), doPull(2, efd), noArquivo(projDe("sf", "ECF")), false, TipoEco, ""},
		{"mais velha que o par da nuvem guardado: eco",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(6, parDe("sf", "EFD")) }),
			doPull(5, efd), noArquivo(projDe("sf", "ECF")), false, TipoEco, ""},
		{"o eco vem antes do pulado", emVoo(mudouAqui, 3), doPull(3, ecf), noArquivo(nil), false, TipoEco, ""},

		{"com o marcador fora de voo: pulado",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Marcador = &storage.Marcador{EntityID: idSF} }),
			doPull(4, efd), noArquivo(projDe("sf", "ECF")), false, TipoPulado, ""},
		{"com o marcador em voo: pulado", emVoo(mudouAqui, 3), doPull(4, efd), noArquivo(projDe("sf", "ECF")), false, TipoPulado, ""},
		{"a versão local passou da base (o Desfazer e o Aprender, o arquivo igual): pulado", mudouAqui, doPull(4, efd),
			noArquivo(projDe("sf", "ECF")), false, TipoPulado, ""},
		{"o arquivo mudou e a varredura não viu: pulado", confirmado(), doPull(4, efd),
			noArquivo(projDe("sf", "ECD")), false, TipoPulado, ""},
		{"o par saiu do arquivo e a varredura não viu: pulado", confirmado(), doPull(4, efd), noArquivo(nil), false, TipoPulado, ""},
		{"a forma local mudou (alias_restrito): pulado", confirmado(), doPull(4, efd),
			noArquivo(&dictionary.ParDoPainel{ID: idSF, Variante: "sf", Termo: "ECF", EmAliasRestrito: true}), false, TipoPulado, ""},
		{"o par aprendido aqui que a varredura não viu: pulado", semLinha(), doPull(1, ecf),
			noArquivo(projDe("sf", "ECF")), false, TipoPulado, ""},
		{"o pulado vem antes do arquivo indisponível", mudouAqui, doPull(4, efd), ArquivoDoPar{Indisponivel: true},
			false, TipoPulado, ""},

		{"o arquivo indisponível: pendente", confirmado(), doPull(4, efd), ArquivoDoPar{Indisponivel: true}, false, TipoPendente, ""},
		{"o arquivo indisponível, sem linha: pendente", semLinha(), doPull(1, ecf), ArquivoDoPar{Indisponivel: true}, false, TipoPendente, ""},

		{"par ativo, ausente aqui: vai ao lote", semLinha(), doPull(1, ecf), noArquivo(nil), true, "", ""},
		{"par ativo, já vale aqui: vai ao lote (convergente)", confirmado(), doPull(4, ecf), noArquivo(projDe("sf", "ECF")), true, "", ""},
		{"par ativo com outro termo, o de antes vale aqui: vai ao lote (a troca)", confirmado(), doPull(4, efd),
			noArquivo(projDe("sf", "ECF")), true, "", ""},
		{"par desfeito: vai ao lote", confirmado(), doPull(4, desfeito), noArquivo(projDe("sf", "ECF")), true, "", ""},
		{"o par da nuvem guardado (ainda não aplicado) da mesma versão: vai ao lote",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(6, parDe("sf", "EFD")) }),
			doPull(6, efd), noArquivo(projDe("sf", "ECF")), true, "", ""},

		{"aplicando, o arquivo já é o esperado: vai ao lote", aplicando, doPull(5, ecf), noArquivo(esperadaEFD), true, "", ""},
		{"aplicando, o arquivo é o guardado: vai ao lote", aplicando, doPull(5, ecf), noArquivo(projDe("sf", "ECF")), true, "", ""},
		{"aplicando, o arquivo diferente das duas: pulado", aplicando, doPull(5, ecf), noArquivo(nil), false, TipoPulado, ""},

		{"a etiqueta do conflict vai com o par dela", etiquetado,
			doPull(5, desfeito), noArquivo(projDe("sf", "ECF")), true, "", storage.EspelhoDesfeitoEmOutro},
		{"a etiqueta não vai com outra versão", etiquetado,
			doPull(6, desfeito), noArquivo(projDe("sf", "ECF")), true, "", ""},

		{"recusa: o tipo não é learned", confirmado(), tipoDitado, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: a operação não é upsert", confirmado(), lapide, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: a versão zero", confirmado(), doPull(0, ecf), noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: sem payload", confirmado(), semPayload, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: o id do payload é outro", confirmado(), outroID, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: a variante é de outro id", confirmado(), varianteDeOutroID, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: restrito sem domínio", confirmado(), restritoSemDominio, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: o domínio vazio", confirmado(), dominioVazio, noArquivo(nil), false, TipoNaoAplicado, ""},
		{"recusa: o termo com 81 caracteres", confirmado(), termoGrande, noArquivo(nil), false, TipoNaoAplicado, ""},
	} {
		t.Run(c.nome, func(t *testing.T) {
			p := PlanejarPar(c.e, c.c, c.arq)
			if p.Lote != c.lote || p.Tipo != c.tipo || p.Etiqueta != c.etiqueta {
				t.Errorf("plano: lote %v, tipo %q, etiqueta %q; quero %v, %q, %q", p.Lote, p.Tipo, p.Etiqueta, c.lote, c.tipo, c.etiqueta)
			}
			if p.ID != c.e.ID || !mesmaProjecao(p.Atual, c.arq.Projecao) {
				t.Errorf("plano: %+v", p)
			}
			if c.tipo != TipoNaoAplicado && (p.Versao != c.c.EntityVersion || p.Par.Variante != c.c.PayloadAprendido.Variant ||
				p.Par.Termo != c.c.PayloadAprendido.Term || p.Par.Desfeito != (c.c.PayloadAprendido.UndoneAt != nil)) {
				t.Errorf("o par do plano: %+v", p)
			}
		})
	}

	// O domínio e a restrição chegam ao par do lote.
	p := PlanejarPar(semLinha(), doPull(2, &Aprendido{ID: dictionary.IDDoPar("sf"), Variant: "sf", Term: "ECF",
		Restricted: true, Domain: texto("contabil")}), noArquivo(nil))
	if !p.Lote || p.Par != (dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "contabil"}) {
		t.Errorf("o par com domínio: %+v", p)
	}
}

// O par da nuvem guardado que ainda não vale aqui é tentado de novo sem o eco
// (o não aplicado já anda a base até a versão dele), e com o pulado e o
// arquivo indisponível de sempre. O par de uma etiqueta que já vale aqui e o
// da intenção não são pendentes.
func TestPlanejarPendente(t *testing.T) {
	naoAplicado := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.ServerVersion, e.Projecao = i64(4), nil
		e.Situacao, e.Motivo = storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente)
		e.Nuvem = guardadoDe(4, dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "fiscal"})
	})
	conflito := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Nuvem = storage.EspelhoTrocadoEmOutro, guardadoDe(5, parDe("sf", "EFD"))
	})
	etiquetaAplicada := com(conflito, func(e *storage.EstadoDoAprendido) {
		e.ServerVersion, e.Projecao = i64(5), projDe("sf", "EFD")
	})
	for _, c := range []struct {
		nome     string
		e        storage.EstadoDoAprendido
		arq      ArquivoDoPar
		pendente bool
		lote     bool
		tipo     string
		etiqueta storage.SituacaoDoEspelho
	}{
		{"o não aplicado, com a base na versão dele: tenta de novo", naoAplicado, noArquivo(nil), true, true, "", ""},
		{"o state do conflict, com a etiqueta", conflito, noArquivo(projDe("sf", "ECF")), true, true, "", storage.EspelhoTrocadoEmOutro},
		{"o pulado mais novo que a base",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(4, parDe("sf", "EFD")) }),
			noArquivo(projDe("sf", "ECF")), true, true, "", ""},
		{"o pendente sem base (o par aprendido em outro PC)",
			com(semLinha(), func(e *storage.EstadoDoAprendido) {
				e.TemLinha, e.Nuvem = true, guardadoDe(2, parDe("sf", "ECF"))
				e.Situacao, e.Motivo = storage.EspelhoNaoAplicado, string(dictionary.MotivoArquivoIndisponivel)
			}), noArquivo(nil), true, true, "", ""},
		{"ainda mudado aqui: pulado", com(naoAplicado, func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 3 }),
			noArquivo(nil), true, false, TipoPulado, ""},
		{"o arquivo ainda indisponível: pendente", naoAplicado, ArquivoDoPar{Indisponivel: true}, true, false, TipoPendente, ""},
		{"a etiqueta que já vale aqui: nada pendente", etiquetaAplicada, noArquivo(projDe("sf", "EFD")), false, false, "", ""},
		{"o par da intenção: nada pendente",
			com(conflito, func(e *storage.EstadoDoAprendido) {
				e.Situacao, e.Esperada = storage.EspelhoAplicando, projDe("sf", "EFD")
			}),
			noArquivo(nil), false, false, "", ""},
		{"o par guardado mais velho que a base: nada pendente",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(2, parDe("sf", "EFD")) }),
			noArquivo(projDe("sf", "ECF")), false, false, "", ""},
		{"o par guardado sem versão (o epoch novo a esqueceu): nada pendente",
			com(naoAplicado, func(e *storage.EstadoDoAprendido) { e.Nuvem = &storage.ParGuardado{Par: e.Nuvem.Par} }),
			noArquivo(nil), false, false, "", ""},
		{"sem par guardado: nada pendente", confirmado(), noArquivo(projDe("sf", "ECF")), false, false, "", ""},
	} {
		t.Run(c.nome, func(t *testing.T) {
			if TemPendente(c.e) != c.pendente {
				t.Errorf("TemPendente = %v; quero %v", !c.pendente, c.pendente)
			}
			p, ok := PlanejarPendente(c.e, c.arq)
			if ok != c.pendente {
				t.Fatalf("ok = %v; quero %v", ok, c.pendente)
			}
			if !ok {
				return
			}
			if p.Lote != c.lote || p.Tipo != c.tipo || p.Etiqueta != c.etiqueta || p.Versao != *c.e.Nuvem.Versao ||
				p.Par != c.e.Nuvem.Par {
				t.Errorf("plano: %+v", p)
			}
		})
	}
}

// O passo 2: a intenção só nasce quando o lote muda o arquivo para o id, e o
// id entra no "veio da nuvem" só quando o par é escrito.
func TestIntencaoDoPar(t *testing.T) {
	ecf, efd := projDe("sf", "ECF"), projDe("sf", "EFD")
	novo := PlanejarPar(semLinha(), doPull(1, fioDoPar("sf", "ECF", false, "", false)), noArquivo(nil))
	troca := PlanejarPar(confirmado(), doPull(4, fioDoPar("sf", "EFD", false, "", false)), noArquivo(ecf))
	desfazer := PlanejarPar(confirmado(), doPull(4, fioDoPar("sf", "ECF", false, "", true)), noArquivo(ecf))
	res := func(s dictionary.SituacaoDoPar, p *dictionary.ParDoPainel) dictionary.ResultadoDoPar {
		return dictionary.ResultadoDoPar{ID: idSF, Situacao: s, Projecao: p}
	}
	intencao := func(v int64, par dictionary.ParDaNuvem, esperada *dictionary.ParDoPainel, veio int) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Nuvem: guardadoDe(v, par), Esperada: &storage.NovaProjecao{Par: esperada},
			Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicando}, VeioDaNuvem: veio}
	}
	for _, c := range []struct {
		nome  string
		p     PlanoDoPar
		r     dictionary.ResultadoDoPar
		ok    bool
		quero storage.AcaoDoAprendido
	}{
		{"o par novo (D2): a intenção, e o id no veio da nuvem", novo, res(dictionary.SituacaoAplicado, ecf), true,
			intencao(1, parDe("sf", "ECF"), ecf, storage.VeioEntra)},
		{"a troca", troca, res(dictionary.SituacaoAplicado, efd), true, intencao(4, parDe("sf", "EFD"), efd, storage.VeioEntra)},
		{"a troca que não vale aqui tira o de antes: a intenção que tira", troca,
			dictionary.ResultadoDoPar{ID: idSF, Situacao: dictionary.SituacaoNaoAplicado, Motivo: dictionary.MotivoDominioAusente},
			true, intencao(4, parDe("sf", "EFD"), nil, storage.VeioFica)},
		{"o desfazer: a intenção que tira, sem o veio da nuvem", desfazer, res(dictionary.SituacaoDesfeito, nil), true,
			intencao(4, dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true}, nil, storage.VeioFica)},
		{"convergente: o arquivo não muda", troca, res(dictionary.SituacaoConvergente, ecf), false, storage.AcaoDoAprendido{}},
		{"já vale aqui", novo, res(dictionary.SituacaoJaValeAqui, nil), false, storage.AcaoDoAprendido{}},
		{"ausente", novo, res(dictionary.SituacaoAusente, nil), false, storage.AcaoDoAprendido{}},
		{"o não aplicado que não tirou nada", novo,
			dictionary.ResultadoDoPar{ID: idSF, Situacao: dictionary.SituacaoNaoAplicado, Motivo: dictionary.MotivoNaoAgiria},
			false, storage.AcaoDoAprendido{}},
		{"o plano que não foi ao lote", PlanejarPar(confirmado(), doPull(3, fioDoPar("sf", "ECF", false, "", false)), noArquivo(ecf)),
			res(dictionary.SituacaoAplicado, efd), false, storage.AcaoDoAprendido{}},
		{"o resultado de outro id", novo, dictionary.ResultadoDoPar{ID: dictionary.IDDoPar("nfe"), Situacao: dictionary.SituacaoAplicado},
			false, storage.AcaoDoAprendido{}},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a, ok := IntencaoDoPar(c.p, c.r)
			if ok != c.ok || !reflect.DeepEqual(a, c.quero) {
				t.Errorf("ok %v, ação %s; quero %v, %s", ok, acaoEmTexto(a), c.ok, acaoEmTexto(c.quero))
			}
		})
	}
}

// O passo 3, caso a caso: o que o espelho grava de cada par do pull, pelo
// plano e pelo que o lote fez. As linhas da tabela "O que o PC faz com cada
// par do pull" que dependem do lote (o convergente, o aplicar, a troca, o
// desfazer, o desfeito posto à mão ou ausente, o não aplicado) e as que não
// dependem (o eco, o pulado, o arquivo indisponível).
func TestDecidirPar(t *testing.T) {
	ecf, efd := projDe("sf", "ECF"), projDe("sf", "EFD")
	fioECF := fioDoPar("sf", "ECF", false, "", false)
	fioEFD := fioDoPar("sf", "EFD", false, "", false)
	fioDesfeito := fioDoPar("sf", "ECF", false, "", true)
	parDesfeito := dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true}
	fiscal := &Aprendido{ID: idSF, Variant: "sf", Term: "ECF", Restricted: true, Domain: texto("fiscal")}
	parFiscal := dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "fiscal"}

	res := func(s dictionary.SituacaoDoPar, p *dictionary.ParDoPainel) *dictionary.ResultadoDoPar {
		return &dictionary.ResultadoDoPar{ID: idSF, Situacao: s, Projecao: p}
	}
	naoAplicado := func(m dictionary.MotivoDoPar, p *dictionary.ParDoPainel) *dictionary.ResultadoDoPar {
		return &dictionary.ResultadoDoPar{ID: idSF, Situacao: dictionary.SituacaoNaoAplicado, Motivo: m, Projecao: p}
	}
	base := func(v int64, desfeita bool) *storage.BaseDoAprendido {
		return &storage.BaseDoAprendido{ServerVersion: v, Desfeita: desfeita}
	}
	proj := func(p *dictionary.ParDoPainel) *storage.NovaProjecao { return &storage.NovaProjecao{Par: p} }
	sit := func(s storage.SituacaoDoEspelho, m string) *storage.NovaSituacao {
		return &storage.NovaSituacao{Situacao: s, Motivo: m}
	}
	// depoisDaIntencao é e como a intenção o deixou (o passo 2), na transação
	// da página.
	depoisDaIntencao := func(e storage.EstadoDoAprendido, p PlanoDoPar, r *dictionary.ResultadoDoPar) storage.EstadoDoAprendido {
		if _, ok := IntencaoDoPar(p, *r); !ok {
			t.Fatal("sem intenção")
		}
		e.TemLinha = true
		e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, r.Projecao, guardadoDe(p.Versao, p.Par)
		return e
	}

	// Os estados de antes.
	etiquetaDesfeito := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Nuvem = storage.EspelhoDesfeitoEmOutro, guardadoDe(5, parDesfeito)
	})
	etiquetaTrocado := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Nuvem = storage.EspelhoTrocadoEmOutro, guardadoDe(5, parDe("sf", "EFD"))
	})
	aplicadoAntes := com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Situacao = storage.EspelhoAplicado })

	type caso struct {
		nome  string
		antes storage.EstadoDoAprendido // o estado do passo 1
		c     Change
		arq   ArquivoDoPar
		r     *dictionary.ResultadoDoPar
		agora func(storage.EstadoDoAprendido, PlanoDoPar, *dictionary.ResultadoDoPar) storage.EstadoDoAprendido
		quero storage.AcaoDoAprendido
		tipo  string
	}
	mesmo := func(e storage.EstadoDoAprendido, _ PlanoDoPar, _ *dictionary.ResultadoDoPar) storage.EstadoDoAprendido {
		return e
	}
	for _, c := range []caso{
		// Sem o lote.
		{"eco: nada", confirmado(), doPull(3, fioECF), noArquivo(ecf), nil, mesmo, storage.AcaoDoAprendido{}, TipoEco},
		{"recusa por regra: nada", confirmado(), doPull(0, fioECF), noArquivo(ecf), nil, mesmo, storage.AcaoDoAprendido{}, TipoNaoAplicado},
		{"pulado: o par fica guardado; a base, a projeção e o arquivo ficam",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 3 }), doPull(4, fioEFD), noArquivo(ecf), nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(4, parDe("sf", "EFD"))}, TipoPulado},
		{"pulado sem linha (o par aprendido aqui que a varredura não viu)", semLinha(), doPull(1, fioEFD), noArquivo(ecf), nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(1, parDe("sf", "EFD"))}, TipoPulado},
		{"pulado com o mesmo par já guardado: nada",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VersaoLocal, e.Nuvem = 3, guardadoDe(4, parDe("sf", "EFD")) }),
			doPull(4, fioEFD), noArquivo(ecf), nil, mesmo, storage.AcaoDoAprendido{}, TipoPulado},
		{"pulado mais novo que a etiqueta: a etiqueta velha sai",
			com(etiquetaTrocado, func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 3 }), doPull(6, fioDesfeito), noArquivo(ecf), nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(6, parDesfeito), Situacao: semSituacaoGravada}, TipoPulado},
		// Na linha "aplicando", a intenção fica para a varredura; o par mais
		// novo que o dela fica guardado no lugar dele (a situação e a
		// esperada ficam), e é ele que a varredura deixa pendente.
		{"pulado na linha aplicando, mais novo: o par guardado troca, e a intenção fica",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, efd, guardadoDe(4, parDe("sf", "EFD"))
			}), doPull(5, fioDesfeito), noArquivo(nil), nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(5, parDesfeito)}, TipoPulado},
		{"pendente na linha aplicando, mais novo: o par guardado troca, e a intenção fica",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, nil, guardadoDe(4, parDesfeito)
			}), doPull(5, fioEFD), ArquivoDoPar{Indisponivel: true}, nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(5, parDe("sf", "EFD"))}, TipoPendente},
		{"pendente na linha aplicando, o par da intenção: nada",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, nil, guardadoDe(4, parDesfeito)
			}), doPull(4, fioDesfeito), ArquivoDoPar{Indisponivel: true}, nil, mesmo, storage.AcaoDoAprendido{}, TipoPendente},
		{"arquivo indisponível: pendente aqui, a base fica", confirmado(), doPull(4, fioEFD), ArquivoDoPar{Indisponivel: true}, nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(4, parDe("sf", "EFD")),
				Situacao: sit(storage.EspelhoNaoAplicado, string(dictionary.MotivoArquivoIndisponivel))}, TipoPendente},
		{"arquivo indisponível com o state do conflict já guardado: a etiqueta fica", etiquetaDesfeito, doPull(5, fioDesfeito),
			ArquivoDoPar{Indisponivel: true}, nil, mesmo, storage.AcaoDoAprendido{}, TipoPendente},
		{"o lote que não rodou: pendente", semLinha(), doPull(1, fioECF), noArquivo(nil), nil, mesmo,
			storage.AcaoDoAprendido{Nuvem: guardadoDe(1, parDe("sf", "ECF")),
				Situacao: sit(storage.EspelhoNaoAplicado, string(dictionary.MotivoArquivoIndisponivel))}, TipoPendente},

		// Com o lote: par ativo.
		{"par ativo, ausente aqui: aplicado (a base, a projeção, o veio da nuvem; o texto é o do arquivo)",
			semLinha(), doPull(1, fioECF), noArquivo(nil), res(dictionary.SituacaoAplicado, ecf), depoisDaIntencao,
			storage.AcaoDoAprendido{Base: base(1, false), Projecao: proj(ecf), Situacao: sit(storage.EspelhoAplicado, ""),
				LimparNuvem: true, VeioDaNuvem: storage.VeioEntra}, TipoAplicado},
		{"par ativo, ausente aqui, e não vale aqui: não aplicado, com o motivo, e o par guardado",
			semLinha(), doPull(2, fiscal), noArquivo(nil), naoAplicado(dictionary.MotivoDominioAusente, nil), mesmo,
			storage.AcaoDoAprendido{Base: base(2, false), Projecao: proj(nil),
				Situacao: sit(storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente)), Nuvem: guardadoDe(2, parFiscal)},
			TipoNaoAplicado},
		{"par ativo, já vale aqui igual: convergente, só a base anda", confirmado(), doPull(4, fioDoPar("sf", "ecf", false, "", false)),
			noArquivo(ecf), res(dictionary.SituacaoConvergente, ecf), mesmo,
			storage.AcaoDoAprendido{Base: base(4, false), Projecao: proj(ecf), Situacao: semSituacaoGravada}, TipoConvergente},
		{"convergente numa linha aplicada: continua aplicada", aplicadoAntes, doPull(4, fioECF), noArquivo(ecf),
			res(dictionary.SituacaoConvergente, ecf), mesmo,
			storage.AcaoDoAprendido{Base: base(4, false), Projecao: proj(ecf), Situacao: sit(storage.EspelhoAplicado, "")}, TipoConvergente},
		{"convergente com a etiqueta do conflict: nada mudou aqui, sem etiqueta", etiquetaTrocado, doPull(5, fioEFD), noArquivo(ecf),
			res(dictionary.SituacaoConvergente, ecf), mesmo,
			storage.AcaoDoAprendido{Base: base(5, false), Projecao: proj(ecf), Situacao: semSituacaoGravada, LimparNuvem: true},
			TipoConvergente},
		{"o par posto à mão: já vale aqui, sem adotar", semLinha(), doPull(1, fioECF), noArquivo(nil),
			res(dictionary.SituacaoJaValeAqui, nil), mesmo,
			storage.AcaoDoAprendido{Base: base(1, false), Projecao: proj(nil), Situacao: sit(storage.EspelhoJaValeAqui, "")}, TipoJaValeAqui},
		{"já vale aqui tira o par pendente guardado (a variante posta à mão não é adotada)",
			com(semLinha(), func(e *storage.EstadoDoAprendido) {
				e.TemLinha, e.Nuvem = true, guardadoDe(1, parDe("sf", "ECF"))
				e.Situacao, e.Motivo = storage.EspelhoNaoAplicado, string(dictionary.MotivoArquivoIndisponivel)
			}), doPull(2, fioECF), noArquivo(nil), res(dictionary.SituacaoJaValeAqui, nil), mesmo,
			storage.AcaoDoAprendido{Base: base(2, false), Projecao: proj(nil), Situacao: sit(storage.EspelhoJaValeAqui, ""),
				LimparNuvem: true}, TipoJaValeAqui},
		{"par ativo com outro termo, o de antes vale aqui: a troca, sem envio", confirmado(), doPull(4, fioEFD), noArquivo(ecf),
			res(dictionary.SituacaoAplicado, efd), depoisDaIntencao,
			storage.AcaoDoAprendido{Base: base(4, false), Projecao: proj(efd), Situacao: sit(storage.EspelhoAplicado, ""),
				LimparNuvem: true, VeioDaNuvem: storage.VeioEntra}, TipoTrocado},
		{"a troca que não vale aqui: o de antes sai, e a linha fica não aplicado", confirmado(), doPull(4, fiscal), noArquivo(ecf),
			naoAplicado(dictionary.MotivoDominioAusente, nil), depoisDaIntencao,
			storage.AcaoDoAprendido{Base: base(4, false), Projecao: proj(nil),
				Situacao: sit(storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente))}, TipoNaoAplicado},
		{"o state do conflict, trocado: aplicado com a etiqueta, que guarda o texto", etiquetaTrocado, doPull(5, fioEFD), noArquivo(ecf),
			res(dictionary.SituacaoAplicado, efd), depoisDaIntencao,
			storage.AcaoDoAprendido{Base: base(5, false), Projecao: proj(efd), Situacao: sit(storage.EspelhoTrocadoEmOutro, ""),
				VeioDaNuvem: storage.VeioEntra}, TipoTrocado},

		// Com o lote: par desfeito.
		{"par desfeito, vale aqui: desfaz aqui, sem envio e sem etiqueta; fica o id", confirmado(), doPull(4, fioDesfeito), noArquivo(ecf),
			res(dictionary.SituacaoDesfeito, nil), depoisDaIntencao,
			storage.AcaoDoAprendido{Base: base(4, true), Projecao: proj(nil), Situacao: semSituacaoGravada, LimparNuvem: true}, TipoDesfeito},
		{"o state do conflict, desfeito: desfaz com a etiqueta", etiquetaDesfeito, doPull(5, fioDesfeito), noArquivo(ecf),
			res(dictionary.SituacaoDesfeito, nil), depoisDaIntencao,
			storage.AcaoDoAprendido{Base: base(5, true), Projecao: proj(nil), Situacao: sit(storage.EspelhoDesfeitoEmOutro, "")},
			TipoDesfeito},
		{"par desfeito, posto à mão: nada sai", semLinha(), doPull(2, fioDesfeito), noArquivo(nil),
			res(dictionary.SituacaoJaValeAqui, nil), mesmo,
			storage.AcaoDoAprendido{Base: base(2, true), Projecao: proj(nil), Situacao: sit(storage.EspelhoJaValeAqui, "")}, TipoJaValeAqui},
		{"par desfeito, ausente aqui: nada sai", semLinha(), doPull(2, fioDesfeito), noArquivo(nil),
			res(dictionary.SituacaoAusente, nil), mesmo,
			storage.AcaoDoAprendido{Base: base(2, true), Projecao: proj(nil), Situacao: semSituacaoGravada}, TipoAusente},
		{"par desfeito com a variante em dois lugares: não aplicado", confirmado(), doPull(4, fioDesfeito), noArquivo(ecf),
			naoAplicado(dictionary.MotivoVarianteDeOutroTermo, ecf), mesmo,
			storage.AcaoDoAprendido{Base: base(4, true), Projecao: proj(ecf),
				Situacao: sit(storage.EspelhoNaoAplicado, string(dictionary.MotivoVarianteDeOutroTermo)), Nuvem: guardadoDe(4, parDesfeito)},
			TipoNaoAplicado},

		// O Aprender do painel depois do lote (fora da trava) fica acima da
		// base: a ação não mexe na versão coberta, e a varredura vê a mudança.
		{"o Aprender do painel depois do lote fica acima da base", confirmado(), doPull(4, fioEFD), noArquivo(ecf),
			res(dictionary.SituacaoAplicado, efd),
			func(e storage.EstadoDoAprendido, p PlanoDoPar, r *dictionary.ResultadoDoPar) storage.EstadoDoAprendido {
				e = depoisDaIntencao(e, p, r)
				e.VersaoLocal = 3
				return e
			},
			storage.AcaoDoAprendido{Base: base(4, false), Projecao: proj(efd), Situacao: sit(storage.EspelhoAplicado, ""),
				LimparNuvem: true, VeioDaNuvem: storage.VeioEntra}, TipoTrocado},
		{"o motivo fora da lista (defeito do lote): não agiria, para o executor não recusar a página", semLinha(), doPull(2, fiscal),
			noArquivo(nil), naoAplicado("inventado", nil), mesmo,
			storage.AcaoDoAprendido{Base: base(2, false), Projecao: proj(nil),
				Situacao: sit(storage.EspelhoNaoAplicado, string(dictionary.MotivoNaoAgiria)), Nuvem: guardadoDe(2, parFiscal)},
			TipoNaoAplicado},
		{"a situação que o PC não conhece: nada", semLinha(), doPull(1, fioECF), noArquivo(nil), res("talvez", nil), mesmo,
			storage.AcaoDoAprendido{}, TipoIgnorado},
		{"o resultado de outro id: nada", semLinha(), doPull(1, fioECF), noArquivo(nil),
			&dictionary.ResultadoDoPar{ID: dictionary.IDDoPar("nfe"), Situacao: dictionary.SituacaoAplicado}, mesmo,
			storage.AcaoDoAprendido{}, TipoIgnorado},
	} {
		t.Run(c.nome, func(t *testing.T) {
			p := PlanejarPar(c.antes, c.c, c.arq)
			e := c.agora(c.antes, p, c.r)
			d := DecidirPar(e, p, c.r)
			if d.Tipo != c.tipo {
				t.Errorf("tipo = %q; quero %q", d.Tipo, c.tipo)
			}
			if !reflect.DeepEqual(d.Acao, c.quero) {
				t.Errorf("ação:\n tenho %s\n quero %s", acaoEmTexto(d.Acao), acaoEmTexto(c.quero))
			}
			// Nenhuma decisão do pull põe marcador: aplicar não ecoa.
			if d.Acao.Marcador != storage.MarcadorFica {
				t.Error("o pull mexeu no marcador")
			}
		})
	}

	// O plano de outro id: nada.
	p := PlanejarPar(semLinha(), doPull(1, fioECF), noArquivo(nil))
	if d := DecidirPar(storage.EstadoDoAprendido{ID: dictionary.IDDoPar("nfe")}, p, res(dictionary.SituacaoAplicado, ecf)); d.Tipo != TipoIgnorado ||
		!reflect.DeepEqual(d.Acao, storage.AcaoDoAprendido{}) {
		t.Errorf("o plano de outro id: %+v", d)
	}
	// Em voo (não acontece), a projeção guardada é a do envio: não é tocada.
	p = PlanejarPar(confirmado(), doPull(4, fioEFD), noArquivo(ecf))
	if d := DecidirPar(emVoo(confirmado(), 2), p, res(dictionary.SituacaoAplicado, efd)); d.Acao.Projecao != nil {
		t.Errorf("a projeção trocada em voo: %s", acaoEmTexto(d.Acao))
	}
}

// acaoEmTexto mostra a ação com os ponteiros abertos.
func acaoEmTexto(a storage.AcaoDoAprendido) string {
	raw, _ := json.Marshal(a)
	return string(raw)
}
