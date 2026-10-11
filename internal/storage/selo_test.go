package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A escolha do selo no PC (ADR-019, "Decisões da F7c"): as regras, nos dois
// repositórios (a mesma suíte), e a fila, só no SQLite.

// selado é um ditado com selo, na versão 2.
func selado(id, dono string, criado time.Time) storage.Transcription {
	tr := newTranscription(id, dono, criado)
	tr.FinalText = "o texto que ficou"
	tr.Version = 2
	pc := "0199a000-0000-7000-8000-0000000000c2"
	tr.Conflito = &storage.Conflito{FinalText: "a outra versão", DeviceID: &pc, Em: criado.Add(time.Minute)}
	return tr
}

func TestResolverConflito_Regras(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		ctx := context.Background()
		agora := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
		for _, s := range []storage.Transcription{selado("s-manter", "u", agora), selado("s-trocar", "u", agora),
			newTranscription("sem-selo", "u", agora)} {
			if err := repo.Put(s); err != nil {
				t.Fatal(err)
			}
		}
		casos := []struct {
			nome     string
			id, dono string
			base     int64
			escolha  string
			quero    error
		}{
			{"sem selo", "sem-selo", "u", 1, storage.EscolhaManter, storage.ErrVersionConflict},
			{"base velha", "s-manter", "u", 1, storage.EscolhaManter, storage.ErrVersionConflict},
			{"base do futuro", "s-manter", "u", 3, storage.EscolhaManter, storage.ErrVersionConflict},
			{"de outro dono", "s-manter", "outro", 2, storage.EscolhaManter, storage.ErrNotFound},
			{"que não existe", "nada", "u", 2, storage.EscolhaManter, storage.ErrNotFound},
			{"escolha inválida", "s-manter", "u", 2, "as duas", storage.ErrEscolhaInvalida},
			{"escolha vazia", "s-manter", "u", 2, "", storage.ErrEscolhaInvalida},
		}
		for _, c := range casos {
			if _, err := repo.ResolverConflito(ctx, c.dono, c.id, c.base, c.escolha); !errors.Is(err, c.quero) {
				t.Errorf("%s: %v; quero %v", c.nome, err, c.quero)
			}
		}
		if g, _ := repo.Get(ctx, "u", "s-manter"); g.Version != 2 || g.Conflito == nil {
			t.Fatalf("as recusas mudaram o ditado: %+v", g)
		}

		m, err := repo.ResolverConflito(ctx, "u", "s-manter", 2, storage.EscolhaManter)
		if err != nil || m.FinalText != "o texto que ficou" || m.Conflito != nil || m.Version != 3 {
			t.Fatalf("manter: %+v, %v", m, err)
		}
		tr, err := repo.ResolverConflito(ctx, "u", "s-trocar", 2, storage.EscolhaTrocar)
		if err != nil || tr.FinalText != "a outra versão" || tr.Conflito != nil || tr.Version != 3 || tr.RawText != "raw s-trocar" {
			t.Fatalf("trocar: %+v, %v", tr, err)
		}
		if g, _ := repo.Get(ctx, "u", "s-trocar"); g.FinalText != "a outra versão" || g.Conflito != nil {
			t.Errorf("trocar não ficou gravado: %+v", g)
		}
		// Escolher de novo: o selo já saiu.
		if _, err := repo.ResolverConflito(ctx, "u", "s-trocar", 3, storage.EscolhaManter); !errors.Is(err, storage.ErrVersionConflict) {
			t.Errorf("escolher de novo: %v", err)
		}
	})
}

// A fila: num ditado que a nuvem tem, a escolha entra no marcador (com o
// conflict_at do selo), também desconectado, e junta com os campos que já
// estavam lá; sem a versão da nuvem, nada entra (a escolha é só daqui); num
// ditado fora da janela, também não.
func TestResolverConflito_AFila(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	agora := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	sv := int64(5)
	com := selado(idA, donoDaFila, agora)
	com.ServerVersion = &sv
	semVersao := selado(idB, donoDaFila, agora)
	foraDaJanela := selado(idC, donoDaFila, agora)
	foraDaJanela.ServerVersion = &sv
	foraDaJanela.SyncState = storage.SituacaoForaDaJanela
	comCampos := selado(idD, donoDaFila, agora)
	comCampos.ServerVersion = &sv
	for _, s := range []storage.Transcription{com, semVersao, foraDaJanela, comCampos} {
		if err := f.repo.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	// Desconectado: o Put põe o marcador de quem tem a versão (o campo
	// nenhum); tira-se para começar do zero, e o D ganha uma edição de
	// lixeira pendente.
	executar(t, f.db, `DELETE FROM sync_outbox`)
	executar(t, f.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('transcription', ?, 2, ?)`,
		idD, "2026-10-06T00:00:00.000000000Z")
	antes := f.avisos.Load()

	for _, x := range []string{idA, idB, idC, idD} {
		if _, err := f.repo.ResolverConflito(ctx, donoDaFila, x, 2, storage.EscolhaTrocar); err != nil {
			t.Fatalf("%s: %v", x, err)
		}
	}
	ms := f.marcadores(t)
	if len(ms) != 2 {
		t.Fatalf("os marcadores: %+v", ms)
	}
	a := ms[idA]
	if a.SeloEscolha != storage.EscolhaTrocar || !a.SeloEm.Equal(agora.Add(time.Minute)) || a.Campos != 0 || a.Inicial {
		t.Errorf("a escolha no marcador: %+v", a)
	}
	if d := ms[idD]; d.SeloEscolha != storage.EscolhaTrocar || d.Campos != storage.CampoLixeira {
		t.Errorf("a escolha junto da lixeira pendente: %+v", d)
	}
	if n := f.avisos.Load() - antes; n != 2 {
		t.Errorf("%d avisos ao laço; quero 2", n)
	}
	// A escolha seguinte (outro selo, depois) troca a do marcador.
	if err := f.repo.Put(func() storage.Transcription {
		s := selado(idE, donoDaFila, agora)
		s.ServerVersion = &sv
		return s
	}()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ResolverConflito(ctx, donoDaFila, idE, 2, storage.EscolhaManter); err != nil {
		t.Fatal(err)
	}
	if e := f.marcadores(t)[idE]; e.SeloEscolha != storage.EscolhaManter {
		t.Errorf("a escolha manter: %+v", e)
	}
	// A lixeira depois da escolha, num ditado que a nuvem tem: o marcador
	// (sem campo, com a escolha) fica, e ganha a lixeira. Só o marcador de
	// criar de um ditado que a nuvem NÃO tem sai com a lixeira.
	if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agora); err != nil {
		t.Fatal(err)
	}
	if a := f.marcadores(t)[idA]; a.SeloEscolha != storage.EscolhaTrocar || a.Campos != storage.CampoLixeira {
		t.Errorf("a lixeira depois da escolha: %+v", a)
	}
}
