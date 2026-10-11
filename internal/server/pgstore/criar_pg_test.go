package pgstore

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O Store.Create sobre um Postgres de verdade (ADR-021, C1). Os casos que as
// duas implementações dividem estão na suíte de contrato (contracttest/
// criar.go); aqui ficam os que só o Postgres tem: a linha como o banco a
// guarda e a trava do contador, onde a conferência do epoch não tem janela.

func ditadoNovo(texto string) cloudstore.NewDictation {
	pt, modelo, dur := "pt", "gpt-4o-mini-transcribe", int64(2500)
	return cloudstore.NewDictation{
		SourcePlatform: cloudstore.SourcePlatformWeb, RawText: "bruto: " + texto, FinalText: texto, Mode: "polish",
		Language: &pt, ASRProvider: "openai", ASRModel: &modelo, DurationMs: &dur,
	}
}

// A linha como o banco a guarda: sem aparelho em nenhuma coluna, a origem web,
// a data do servidor e o evento sem aparelho, na mesma transação.
func TestPostgres_Criar_ALinhaDoBanco(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	id := contracttest.NewID()
	sit, err := s.Situacao(e.ctx, u, id)
	if err != nil || sit.Estado != cloudstore.IdNada {
		t.Fatalf("Situacao: %+v, %v", sit, err)
	}
	got, created, err := s.Create(e.ctx, cloudstore.Actor{UserID: u}, sit.Epoch, id, ditadoNovo("do celular"), now0)
	if err != nil || !created {
		t.Fatalf("Create: created=%v err=%v", created, err)
	}

	var device, textDevice, trashDevice, conflictAt, deleted, purge *string
	var platform, mode, status, final string
	var version, textVersion, trashVersion, seq int64
	var createdAt, updatedAt time.Time
	if err := e.pool.QueryRow(e.ctx, `SELECT device_id::text, text_device_id::text, trash_device_id::text, source_platform, mode, status,
		final_text, version, text_version, trash_version, last_seq, created_at, updated_at,
		deleted_at::text, purge_at::text, conflict_at::text
		FROM transcriptions WHERE user_id = $1 AND id = $2`, u, id).
		Scan(&device, &textDevice, &trashDevice, &platform, &mode, &status, &final, &version, &textVersion, &trashVersion, &seq,
			&createdAt, &updatedAt, &deleted, &purge, &conflictAt); err != nil {
		t.Fatalf("ler a linha: %v", err)
	}
	if device != nil || textDevice != nil || trashDevice != nil {
		t.Errorf("aparelho nas colunas: %v %v %v, quero nulo nas três", device, textDevice, trashDevice)
	}
	if platform != "web" || mode != "polish" || status != "done" || final != "do celular" {
		t.Errorf("linha: %q %q %q %q", platform, mode, status, final)
	}
	if version != 1 || textVersion != 1 || trashVersion != 1 || seq != 1 || got.LastSeq != 1 {
		t.Errorf("versões %d %d %d, seq %d (devolvido %d), quero 1 1 1 1", version, textVersion, trashVersion, seq, got.LastSeq)
	}
	if !createdAt.Equal(now0) || !updatedAt.Equal(now0) || !got.CreatedAt.Equal(now0) {
		t.Errorf("datas: %v %v (devolvida %v), quero %v", createdAt, updatedAt, got.CreatedAt, now0)
	}
	if deleted != nil || purge != nil || conflictAt != nil {
		t.Errorf("lixeira/selo: %v %v %v", deleted, purge, conflictAt)
	}
	evs := e.events(t, u)
	if len(evs) != 1 || evs[0].DeviceID != nil || evs[0].Seq != 1 || evs[0].Operation != cloudstore.OpUpsert || evs[0].EntityVersion != 1 {
		t.Errorf("eventos: %+v", evs)
	}
	// O que o Get devolve é o que o Create devolveu.
	if back, err := s.Get(e.ctx, u, id); err != nil || back.FinalText != got.FinalText || back.LastSeq != 1 || back.DeviceID != nil {
		t.Errorf("Get: %+v, %v", back, err)
	}
}

// A conferência do epoch fica dentro da transação que trava o contador, sem
// janela: um "apagar meus dados" que já trocou o epoch, e ainda não terminou,
// segura o Create na trava; quando termina, o Create vê o epoch novo e recusa.
func TestPostgres_Criar_OEpochTrocadoEnquantoEsperaATrava(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	id := contracttest.NewID()
	lido, err := s.Situacao(e.ctx, u, id)
	if err != nil {
		t.Fatal(err)
	}

	// A transação do "apagar": o primeiro comando troca o epoch e trava o
	// contador, como ApagarDadosDaConta (sqlNovoEpoch).
	apagar, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = apagar.Rollback(e.ctx) })
	novo := cloudstore.NewUUIDv7()
	if _, err := apagar.Exec(e.ctx, sqlNovoEpoch, u, novo); err != nil {
		t.Fatal(err)
	}

	type saida struct {
		created bool
		err     error
	}
	fim := make(chan saida, 1)
	go func() {
		_, created, err := s.Create(e.ctx, cloudstore.Actor{UserID: u}, lido.Epoch, id, ditadoNovo("espera a trava"), now0)
		fim <- saida{created, err}
	}()
	select {
	case r := <-fim:
		t.Fatalf("o Create passou pela trava do contador: created=%v err=%v", r.created, r.err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := apagar.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	r := <-fim
	if !errors.Is(r.err, cloudstore.ErrEpochChanged) || r.created {
		t.Fatalf("depois do apagar: created=%v err=%v, quero ErrEpochChanged", r.created, r.err)
	}
	if c := e.count("SELECT count(*) FROM transcriptions WHERE user_id = $1", u); c != 0 {
		t.Errorf("%d ditados gravados com o epoch velho", c)
	}
	if got := e.lastSeq(u); got != 0 {
		t.Errorf("last_seq = %d, quero 0 (a recusa devolve o número)", got)
	}
}

// O Create correndo com o "apagar meus dados": em qualquer ordem, quando os
// dois terminam não sobra ditado nenhum, e nunca um gravado depois do apagar
// com o epoch de antes.
func TestPostgres_Criar_JuntoComOApagarMeusDados(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "criar", Email: "criar@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		id := contracttest.NewID()
		lido, err := s.Situacao(e.ctx, u.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var criarErr, apagarErr error
		wg.Go(func() {
			_, _, criarErr = s.Create(e.ctx, cloudstore.Actor{UserID: u.ID}, lido.Epoch, id, ditadoNovo("corrida"), now0)
		})
		_, apagarErr = s.ApagarDadosDaConta(e.ctx, u.ID, contracttest.NewID(), now0)
		wg.Wait()
		if apagarErr != nil {
			t.Fatalf("rodada %d: apagar: %v", i, apagarErr)
		}
		if criarErr != nil && !errors.Is(criarErr, cloudstore.ErrEpochChanged) {
			t.Fatalf("rodada %d: Create: %v, quero nil ou ErrEpochChanged", i, criarErr)
		}
		if c := e.count("SELECT count(*) FROM transcriptions WHERE user_id = $1", u.ID); c != 0 {
			t.Fatalf("rodada %d: %d ditados depois do apagar (Create: %v)", i, c, criarErr)
		}
	}
}
