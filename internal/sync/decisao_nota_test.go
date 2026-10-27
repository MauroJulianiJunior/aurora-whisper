package syncengine

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões puras das notas e dos marcadores de nota (decisao_nota.go), em
// tabela: a resposta do push e a mudança do pull.

const idDaNota = "0199a000-0000-7000-8000-0000000000a1"

func notaLocal(versao int64, sv *int64) *storage.Nota {
	return &storage.Nota{ID: idDaNota, Corpo: "c", Posicao: "V", Version: versao, ServerVersion: sv, SyncState: storage.SituacaoNormal}
}

func notaEmVoo(campos int, versao int64) *storage.Marcador {
	return &storage.Marcador{EntityType: storage.EntidadeNota, EntityID: idDaNota, Campos: campos, MutationID: "m1", VersaoEnviada: versao}
}

func notaDaNuvemDeTeste(v int64) *Nota {
	t := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	return &Nota{ID: idDaNota, Titulo: "t", Corpo: "nuvem", Posicao: "V", CreatedAt: t, UpdatedAt: t, Version: v,
		Marcadores: []string{}, VersaoEmConflito: &SeloDaNota{Titulo: "t", Corpo: "pc", ConflictAt: t}}
}

func vn(n int64) *int64 { return &n }

func TestDecidirResultadoDaNota(t *testing.T) {
	casos := []struct {
		nome string
		e    storage.EstadoDaNota
		r    MutationResult
		tipo string
		conf func(t *testing.T, a storage.AcaoDaNota)
	}{
		{"outro envio é ignorado", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, nil), Marcador: notaEmVoo(0, 2)},
			MutationResult{MutationID: "outro", Status: StatusApplied}, TipoIgnorado, nil},
		{"applied grava a versão e o corpo confirmado", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, nil), Marcador: notaEmVoo(0, 2)},
			MutationResult{MutationID: "m1", Status: StatusApplied, EntityVersion: 1}, TipoAplicado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.ServerVersion == nil || *a.ServerVersion != 1 || !a.CorpoNuvemAtual || a.Marcador != storage.MarcadorSai {
					t.Errorf("%+v", a)
				}
			}},
		{"applied com mudança no voo reenvia e esquece o corpo", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(3, vn(1)), Marcador: notaEmVoo(1, 2)},
			MutationResult{MutationID: "m1", Status: StatusApplied, EntityVersion: 2}, TipoAplicado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Marcador != storage.MarcadorReenviar || !a.EsquecerCorpoNuvem || a.CorpoNuvemAtual {
					t.Errorf("%+v", a)
				}
			}},
		{"mesclado não guarda a versão (a mescla da lista de marcar)", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1)), Marcador: notaEmVoo(1, 2)},
			MutationResult{MutationID: "m1", Status: StatusApplied, EntityVersion: 5, Mesclado: true}, TipoMesclado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.ServerVersion != nil || !a.EsquecerCorpoNuvem || a.Marcador != storage.MarcadorSai {
					t.Errorf("%+v", a)
				}
			}},
		{"conflict grava o state com o selo", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1)), Marcador: notaEmVoo(1, 2)},
			MutationResult{MutationID: "m1", Status: StatusConflict, EntityVersion: 4, StateNota: notaDaNuvemDeTeste(4)}, TipoConflito,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Gravar == nil || a.Gravar.Conflito == nil || a.Gravar.Conflito.Corpo != "pc" || a.Gravar.Corpo != "nuvem" ||
					a.ServerVersion == nil || *a.ServerVersion != 4 {
					t.Errorf("%+v", a)
				}
			}},
		{"conflict com mudança no voo reenvia", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(3, vn(1)), Marcador: notaEmVoo(1, 2)},
			MutationResult{MutationID: "m1", Status: StatusConflict, EntityVersion: 4, StateNota: notaDaNuvemDeTeste(4)}, TipoConflito,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Gravar != nil || a.Marcador != storage.MarcadorReenviar {
					t.Errorf("%+v", a)
				}
			}},
		{"base_invalida sobe inteira", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1)), Marcador: notaEmVoo(1, 2)},
			MutationResult{MutationID: "m1", Status: StatusRejected, Code: CodigoBaseInvalida}, TipoReenviar,
			func(t *testing.T, a storage.AcaoDaNota) {
				if !a.ZerarServerVersion || a.Marcador != storage.MarcadorReenviar {
					t.Errorf("%+v", a)
				}
			}},
		{"nuvem sem notas adia", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, nil), Marcador: notaEmVoo(0, 2)},
			MutationResult{MutationID: "m1", Status: StatusRejected, Code: CodigoTipoDesconhecido}, TipoAdiado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Marcador != storage.MarcadorReenviar || a.Situacao != "" {
					t.Errorf("%+v", a)
				}
			}},
		{"outra recusa marca recusado", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, nil), Marcador: notaEmVoo(0, 2)},
			MutationResult{MutationID: "m1", Status: StatusRejected, Code: "campo_invalido"}, TipoRecusado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Situacao != storage.SituacaoRecusado || a.Erro != "campo_invalido" {
					t.Errorf("%+v", a)
				}
			}},
		{"gone apagado apaga a linha (apagar vence)", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1)), Marcador: notaEmVoo(1, 2)},
			MutationResult{MutationID: "m1", Status: StatusGone, Motivo: MotivoApagado}, TipoApagado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if !a.ApagarLinha {
					t.Errorf("%+v", a)
				}
			}},
		{"a lápide aplicada termina o marcador", storage.EstadoDaNota{ID: idDaNota, Marcador: notaEmVoo(storage.CampoApagado, 0)},
			MutationResult{MutationID: "m1", Status: StatusApplied, EntityVersion: 3}, TipoAplicado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Marcador != storage.MarcadorSai || a.ApagarLinha {
					t.Errorf("%+v", a)
				}
			}},
		{"a lápide com gone também termina", storage.EstadoDaNota{ID: idDaNota, Marcador: notaEmVoo(storage.CampoApagado, 0)},
			MutationResult{MutationID: "m1", Status: StatusGone, Motivo: MotivoApagado}, TipoApagado,
			func(t *testing.T, a storage.AcaoDaNota) {
				if a.Marcador != storage.MarcadorSai {
					t.Errorf("%+v", a)
				}
			}},
		{"a lápide com o id reusado vai com outro id", storage.EstadoDaNota{ID: idDaNota, Marcador: notaEmVoo(storage.CampoApagado, 0)},
			MutationResult{MutationID: "m1", Status: StatusRejected, Code: CodigoMutacaoReusada}, TipoReenviar, nil},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			d := DecidirResultadoDaNota(c.e, c.r)
			if d.Tipo != c.tipo {
				t.Fatalf("tipo %q, quero %q (%+v)", d.Tipo, c.tipo, d.Acao)
			}
			if c.conf != nil {
				c.conf(t, d.Acao)
			}
		})
	}
}

func TestDecidirMudancaDaNota(t *testing.T) {
	up := func(versao int64) Change {
		n := notaDaNuvemDeTeste(versao)
		n.VersaoEmConflito = nil
		return Change{EntityType: storage.EntidadeNota, EntityID: idDaNota, Operation: OpUpsert, EntityVersion: versao, PayloadNota: n}
	}
	ruim := up(2)
	ruim.PayloadNota.Posicao = "a0" // termina em 0: chave inválida
	outroID := up(2)
	outroID.PayloadNota.ID = "0199a000-0000-7000-8000-0000000000ff"
	lapide := Change{EntityType: storage.EntidadeNota, EntityID: idDaNota, Operation: OpTombstone}
	diferente := notaLocal(1, nil)
	diferente.Corpo = "daqui"
	igual := notaLocal(1, nil)
	igual.Titulo, igual.Corpo, igual.Marcadores = "t", "nuvem", []string{}
	casos := []struct {
		nome string
		e    storage.EstadoDaNota
		c    Change
		tipo string
	}{
		{"nova aqui", storage.EstadoDaNota{ID: idDaNota}, up(2), TipoRecebido},
		{"com marcador pendente, pula", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1)), Marcador: notaEmVoo(1, 2)}, up(2), TipoPulado},
		{"eco pela versão", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(2))}, up(2), TipoEco},
		{"mudou na nuvem", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1))}, up(2), TipoRecebido},
		{"daqui sem versão e diferente: reconcilia", storage.EstadoDaNota{ID: idDaNota, Linha: diferente}, up(2), TipoReconciliado},
		{"daqui sem versão e igual: adota", storage.EstadoDaNota{ID: idDaNota, Linha: igual}, up(2), TipoRecebido},
		{"posição inválida não se aplica", storage.EstadoDaNota{ID: idDaNota}, ruim, TipoNaoAplicado},
		{"outro id no payload não se aplica", storage.EstadoDaNota{ID: idDaNota}, outroID, TipoNaoAplicado},
		{"lápide apaga", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1))}, lapide, TipoApagado},
		{"lápide sem linha é eco", storage.EstadoDaNota{ID: idDaNota}, lapide, TipoEco},
		{"lápide com mudança pendente pula", storage.EstadoDaNota{ID: idDaNota, Linha: notaLocal(2, vn(1)), Marcador: notaEmVoo(1, 2)}, lapide, TipoPulado},
		{"lápide com a lápide daqui pendente é eco", storage.EstadoDaNota{ID: idDaNota, Marcador: notaEmVoo(storage.CampoApagado, 0)}, lapide, TipoEco},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			d := DecidirMudancaDaNota(c.e, c.c)
			if d.Tipo != c.tipo {
				t.Fatalf("tipo %q, quero %q (%+v)", d.Tipo, c.tipo, d.Acao)
			}
			if c.tipo == TipoRecebido && (d.Acao.Gravar == nil || d.Acao.ServerVersion == nil) {
				t.Errorf("recebido sem gravar: %+v", d.Acao)
			}
		})
	}
}

func TestDecidirMudancaDoMarcadorDeNota(t *testing.T) {
	const id = "0199a000-0000-7000-8000-0000000000b1"
	agora := time.Now().UTC()
	up := func(nome string, versao int64) Change {
		return Change{EntityType: storage.EntidadeMarcadorDeNota, EntityID: id, Operation: OpUpsert, EntityVersion: versao,
			PayloadMarcador: &MarcadorDaNuvem{ID: id, Nome: nome, CreatedAt: agora, UpdatedAt: agora, Version: versao}}
	}
	espera := &storage.MarcadorDeNota{ID: id, DeletedAt: &agora, SyncState: storage.SituacaoAguardando}
	daqui := &storage.MarcadorDeNota{ID: id, Nome: "Outro", SyncState: storage.SituacaoNormal}
	casos := []struct {
		nome string
		e    storage.EstadoDoMarcadorDeNota
		c    Change
		tipo string
	}{
		{"novo", storage.EstadoDoMarcadorDeNota{ID: id}, up("Casa", 1), TipoRecebido},
		{"o que espera recebe o estado", storage.EstadoDoMarcadorDeNota{ID: id, Linha: espera}, up("Casa", 1), TipoRecebido},
		{"daqui sem versão com outro nome reconcilia", storage.EstadoDoMarcadorDeNota{ID: id, Linha: daqui}, up("Casa", 1), TipoReconciliado},
		{"nome vazio não se aplica", storage.EstadoDoMarcadorDeNota{ID: id}, up("  ", 1), TipoNaoAplicado},
		{"nome com quebra de linha não se aplica", storage.EstadoDoMarcadorDeNota{ID: id}, up("a\nb", 1), TipoNaoAplicado},
		{"lápide apaga", storage.EstadoDoMarcadorDeNota{ID: id, Linha: daqui}, Change{EntityType: "label", EntityID: id, Operation: OpTombstone}, TipoApagado},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if d := DecidirMudancaDoMarcadorDeNota(c.e, c.c); d.Tipo != c.tipo {
				t.Fatalf("tipo %q, quero %q", d.Tipo, c.tipo)
			}
		})
	}
}

func TestMutacaoDoDitado_VirouNotaSoComACapacidade(t *testing.T) {
	sv := int64(3)
	nota := "0199A000-0000-7000-8000-0000000000A1"
	nova := storage.PrefixoDaNotaNova + "tok"
	it := storage.ItemDaFila{Marcador: storage.Marcador{EntityID: idDaNota, Campos: storage.CampoVirouNota},
		Linha: storage.Transcription{ID: idDaNota, ServerVersion: &sv, VirouNota: &nota}}
	sem, _ := MutacaoDoDitado(it, "m1", false)
	com, _ := MutacaoDoDitado(it, "m1", true)
	if string(com.Payload) != `{"virou_nota":"0199a000-0000-7000-8000-0000000000a1"}` {
		t.Errorf("com a capacidade: %s", com.Payload)
	}
	if string(sem.Payload) == string(com.Payload) {
		t.Errorf("sem a capacidade levou o virou_nota: %s", sem.Payload)
	}
	it.Linha.VirouNota = &nova
	if com, _ := MutacaoDoDitado(it, "m1", true); string(com.Payload) != `{"virou_nota":null}` {
		t.Errorf("o nova:<token> subiu: %s", com.Payload)
	}
}

func TestDecidirMudanca_VirouNota(t *testing.T) {
	sv := int64(2)
	nota := "0199a000-0000-7000-8000-0000000000a1"
	nova := storage.PrefixoDaNotaNova + "tok"
	base := Item{ID: idDaNota, CreatedAt: time.Now(), Version: 2, Status: "done"}
	eco := func(local *string, nuvem VirouNotaOpcional) Decisao {
		it := base
		it.VirouNota = nuvem
		return DecidirMudanca(storage.EstadoDoItem{ID: idDaNota, Linha: &storage.Transcription{ID: idDaNota, ServerVersion: &sv, VirouNota: local}},
			Change{EntityType: storage.EntidadeDitado, EntityID: idDaNota, Operation: OpUpsert, EntityVersion: 2, Payload: &it})
	}
	if d := eco(nil, VirouNotaOpcional{Presente: true, ID: &nota}); !d.Acao.MudarVirouNota || *d.Acao.VirouNota != nota {
		t.Errorf("o eco com virou_nota novo: %+v", d.Acao)
	}
	if d := eco(&nova, VirouNotaOpcional{Presente: true}); d.Acao.MudarVirouNota {
		t.Errorf("o nova:<token> daqui foi trocado pelo nulo da nuvem: %+v", d.Acao)
	}
	if d := eco(&nota, VirouNotaOpcional{}); d.Acao.MudarVirouNota {
		t.Errorf("a nuvem antiga (sem a chave) mexeu no virou_nota: %+v", d.Acao)
	}
	// Mudança de verdade de uma nuvem antiga: o virou_nota daqui fica.
	it := base
	it.Version = 3
	d := DecidirMudanca(storage.EstadoDoItem{ID: idDaNota, Linha: &storage.Transcription{ID: idDaNota, ServerVersion: &sv, VirouNota: &nota}},
		Change{EntityType: storage.EntidadeDitado, EntityID: idDaNota, Operation: OpUpsert, EntityVersion: 3, Payload: &it})
	if d.Acao.Gravar == nil || d.Acao.Gravar.VirouNota == nil || *d.Acao.Gravar.VirouNota != nota {
		t.Errorf("a nuvem antiga apagou o virou_nota daqui: %+v", d.Acao.Gravar)
	}
}
