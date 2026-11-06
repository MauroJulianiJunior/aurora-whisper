package pgstore

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// A cópia do dicionário de um PC no Postgres (ADR-023; a 0027): o que só o banco
// prova (a linha única por conta, a transação do PUT, a chave estrangeira sem
// cascata, a revogação e o "apagar meus dados" na mesma transação). O
// comportamento pela API está na suíte de contrato (contracttest/dicionario.go),
// que roda contra o Postgres em TestContrato_Postgres.

// copiaDeTeste são bytes de cópia com acento (o TEXT do banco os devolve iguais).
func copiaDeTeste(termos int, sha string) cloudstore.NovaCopia {
	return cloudstore.NovaCopia{
		Conteudo: []byte(`{"usuario":"copia","entradas":[{"canonica":"Ação","prioridade":0}]}`),
		SHA256:   sha, Termos: termos,
	}
}

// sha de teste: 64 dígitos hexadecimais (o CHECK do banco).
func shaDeTeste(c byte) string { return string(bytes.Repeat([]byte{c}, 64)) }

func (e *pgEnv) copias(userID string) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM dictionary_copies WHERE user_id = $1", userID)
}

// Uma cópia só por conta: o tomar de outro PC troca a linha, não cria outra; e
// cada conta tem a sua.
func TestPostgres_UmaCopiaPorConta(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	pc1, pc2, pcB := e.newDevice(t, a), e.newDevice(t, a), e.newDevice(t, b)

	r1, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a, DeviceID: pc1}, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0)
	if err != nil || !r1.Gravou || r1.NaoEraAFonte || r1.Atual == nil || r1.Atual.DeviceID != pc1 || r1.Atual.Termos != 1 {
		t.Fatalf("tomar do PC 1: %+v, %v", r1, err)
	}
	r2, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a, DeviceID: pc2}, copiaDeTeste(2, shaDeTeste('b')), cloudstore.CopiaTomar, now0.Add(time.Minute))
	if err != nil || !r2.Gravou || r2.Atual == nil || r2.Atual.DeviceID != pc2 || r2.Atual.Termos != 2 {
		t.Fatalf("tomar do PC 2: %+v, %v", r2, err)
	}
	if _, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: b, DeviceID: pcB}, copiaDeTeste(3, shaDeTeste('c')), cloudstore.CopiaTomar, now0); err != nil {
		t.Fatalf("tomar na conta B: %v", err)
	}
	if n := e.copias(a); n != 1 {
		t.Errorf("a conta A tem %d cópias, quero 1", n)
	}
	if n := e.count("SELECT count(*) FROM dictionary_copies"); n != 2 {
		t.Errorf("o banco tem %d cópias, quero 2 (uma por conta)", n)
	}
	info, err := s.DictionaryCopyInfo(e.ctx, a)
	if err != nil || info == nil || info.DeviceID != pc2 || info.DeviceNome != "PC de teste" || info.Termos != 2 ||
		info.SHA256 != shaDeTeste('b') || !info.AtualizadaEm.Equal(now0.Add(time.Minute)) || info.AtualizadaEm.Location() != time.UTC {
		t.Errorf("o estado da conta A = %+v, %v", info, err)
	}
	// O PC que perdeu a fonte a vê como de outro.
	if info.FontePara(pc1) != cloudstore.FonteOutro || info.FontePara(pc2) != cloudstore.FonteEste {
		t.Errorf("a fonte vista dos dois PCs: %s e %s", info.FontePara(pc1), info.FontePara(pc2))
	}
	// Os bytes voltam iguais, com o acento.
	c, conteudo, ok, err := s.GetDictionaryCopy(e.ctx, a)
	if err != nil || !ok || c.DeviceID != pc2 || !bytes.Equal(conteudo, copiaDeTeste(2, "").Conteudo) {
		t.Errorf("GetDictionaryCopy = %+v %q %v, %v", c, conteudo, ok, err)
	}
	if _, _, ok, err := s.GetDictionaryCopy(e.ctx, e.newUser(t)); err != nil || ok {
		t.Errorf("GetDictionaryCopy de quem não tem: %v, %v", ok, err)
	}
	if info, err := s.DictionaryCopyInfo(e.ctx, e.newUser(t)); err != nil || info != nil {
		t.Errorf("DictionaryCopyInfo de quem não tem: %+v, %v", info, err)
	}
}

// O tomar, o manter, o mesmo SHA-256 e o delete, direto no Store.
func TestPostgres_TomarManterEDelete(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a := e.newUser(t)
	pc1, pc2 := e.newDevice(t, a), e.newDevice(t, a)
	a1, a2 := cloudstore.Actor{UserID: a, DeviceID: pc1}, cloudstore.Actor{UserID: a, DeviceID: pc2}
	put := func(ac cloudstore.Actor, c cloudstore.NovaCopia, modo cloudstore.ModoDaCopia, now time.Time) cloudstore.ResultadoDaCopia {
		t.Helper()
		r, err := s.PutDictionaryCopy(e.ctx, ac, c, modo, now)
		if err != nil {
			t.Fatalf("PutDictionaryCopy(%s): %v", modo, err)
		}
		return r
	}

	// Manter sem cópia: não grava, e o estado é nenhum.
	if r := put(a1, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaManter, now0); r.Gravou || !r.NaoEraAFonte || r.Atual != nil {
		t.Errorf("manter sem cópia: %+v", r)
	}
	if n := e.copias(a); n != 0 {
		t.Fatalf("o manter sem cópia gravou (%d)", n)
	}
	// Tomar grava.
	if r := put(a1, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); !r.Gravou || r.NaoEraAFonte || r.Atual.DeviceID != pc1 {
		t.Errorf("tomar: %+v", r)
	}
	// O mesmo SHA-256 do mesmo PC não grava (a hora fica), tomar ou manter.
	for _, modo := range []cloudstore.ModoDaCopia{cloudstore.CopiaManter, cloudstore.CopiaTomar} {
		r := put(a1, copiaDeTeste(1, shaDeTeste('a')), modo, now0.Add(time.Hour))
		if r.Gravou || r.NaoEraAFonte || r.Atual == nil || !r.Atual.AtualizadaEm.Equal(now0) {
			t.Errorf("o mesmo SHA-256 (%s): %+v", modo, r)
		}
	}
	// Manter da fonte com outro SHA-256 grava.
	if r := put(a1, copiaDeTeste(5, shaDeTeste('b')), cloudstore.CopiaManter, now0.Add(time.Minute)); !r.Gravou || r.Atual.Termos != 5 {
		t.Errorf("manter da fonte: %+v", r)
	}
	// Manter de outro PC: 409 (NaoEraAFonte) com o estado de agora, e nada muda.
	r := put(a2, copiaDeTeste(9, shaDeTeste('c')), cloudstore.CopiaManter, now0.Add(2*time.Minute))
	if r.Gravou || !r.NaoEraAFonte || r.Atual == nil || r.Atual.DeviceID != pc1 || r.Atual.SHA256 != shaDeTeste('b') {
		t.Errorf("manter de quem não é a fonte: %+v", r)
	}
	// Tomar de outro PC passa a fonte, mesmo com outro SHA-256 já lá.
	if r := put(a2, copiaDeTeste(9, shaDeTeste('c')), cloudstore.CopiaTomar, now0.Add(3*time.Minute)); !r.Gravou || r.Atual.DeviceID != pc2 {
		t.Errorf("tomar de outro PC: %+v", r)
	}

	// O delete: de quem não é a fonte é nada, sem erro; da fonte apaga; de novo é nada.
	if ok, err := s.DeleteDictionaryCopy(e.ctx, a1); err != nil || ok {
		t.Errorf("apagar de quem não é a fonte: %v, %v", ok, err)
	}
	if n := e.copias(a); n != 1 {
		t.Fatalf("o delete de quem não é a fonte apagou (%d)", n)
	}
	if ok, err := s.DeleteDictionaryCopy(e.ctx, a2); err != nil || !ok {
		t.Errorf("apagar da fonte: %v, %v", ok, err)
	}
	if ok, err := s.DeleteDictionaryCopy(e.ctx, a2); err != nil || ok {
		t.Errorf("apagar de novo: %v, %v", ok, err)
	}
	if n := e.copias(a); n != 0 {
		t.Errorf("sobrou %d cópia(s)", n)
	}
	// Sem aparelho no Actor (a sessão do navegador), nada vale.
	if _, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a}, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); !errors.Is(err, cloudstore.ErrDeviceNotAllowed) {
		t.Errorf("PUT sem aparelho: %v, quero ErrDeviceNotAllowed", err)
	}
	if _, err := s.DeleteDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a}); !errors.Is(err, cloudstore.ErrDeviceNotAllowed) {
		t.Errorf("DELETE sem aparelho: %v, quero ErrDeviceNotAllowed", err)
	}
	// O PC de outra conta não grava na conta A.
	pcOutro := e.newDevice(t, e.newUser(t))
	if _, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a, DeviceID: pcOutro}, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); !errors.Is(err, cloudstore.ErrUnknownDevice) {
		t.Errorf("PUT com o PC de outra conta: %v, quero ErrUnknownDevice", err)
	}
}

// Dois PCs tomando ao mesmo tempo: uma linha só, sem erro, e a vencedora é uma
// das duas (o ON CONFLICT e a trava da cópia).
func TestPostgres_DoisTomarAoMesmoTempo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a := e.newUser(t)
	pcs := []string{e.newDevice(t, a), e.newDevice(t, a), e.newDevice(t, a), e.newDevice(t, a)}
	var wg sync.WaitGroup
	erros := make([]error, len(pcs))
	for i, pc := range pcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, erros[i] = s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a, DeviceID: pc},
				copiaDeTeste(i, shaDeTeste(byte('a'+i))), cloudstore.CopiaTomar, now0)
		}()
	}
	wg.Wait()
	for i, err := range erros {
		if err != nil {
			t.Errorf("o PC %d: %v", i, err)
		}
	}
	if n := e.copias(a); n != 1 {
		t.Errorf("a conta tem %d cópias, quero 1", n)
	}
	info, err := s.DictionaryCopyInfo(e.ctx, a)
	if err != nil || info == nil {
		t.Fatalf("DictionaryCopyInfo: %+v, %v", info, err)
	}
	// A linha é inteira de um PC só: o termo e o SHA-256 batem com o dele.
	for i, pc := range pcs {
		if pc == info.DeviceID && (info.Termos != i || info.SHA256 != shaDeTeste(byte('a'+i))) {
			t.Errorf("a cópia do PC %d tem os campos de outro: %+v", i, info)
		}
	}
}

// Revogar o PC que é a fonte apaga a cópia na mesma transação; revogar outro PC
// da conta não toca nela; o PC revogado não grava.
func TestPostgres_RevogarOAparelhoApagaACopia(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	pc1, pc2, pcB := e.newDevice(t, a), e.newDevice(t, a), e.newDevice(t, b)
	for _, ac := range []cloudstore.Actor{{UserID: a, DeviceID: pc1}, {UserID: b, DeviceID: pcB}} {
		if _, err := s.PutDictionaryCopy(e.ctx, ac, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevogarAparelho(e.ctx, a, pc2, now0); err != nil {
		t.Fatalf("revogar o PC que não é a fonte: %v", err)
	}
	if n := e.copias(a); n != 1 {
		t.Fatalf("revogar outro PC apagou a cópia (%d)", n)
	}
	if err := s.RevogarAparelho(e.ctx, a, pc1, now0); err != nil {
		t.Fatalf("revogar a fonte: %v", err)
	}
	if n := e.copias(a); n != 0 {
		t.Errorf("revogar a fonte deixou %d cópia(s)", n)
	}
	if n := e.copias(b); n != 1 {
		t.Errorf("a cópia da conta B ficou com %d (revogar o PC de A não a toca)", n)
	}
	// O PC revogado não grava (a linha do aparelho continua, com revoked_at).
	_, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a, DeviceID: pc1}, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0)
	if !errors.Is(err, cloudstore.ErrUnknownDevice) {
		t.Errorf("PUT do PC revogado: %v, quero ErrUnknownDevice", err)
	}
	if n := e.copias(a); n != 0 {
		t.Errorf("o PC revogado gravou (%d)", n)
	}
}

// O "apagar meus dados" apaga a cópia (antes dos aparelhos, que ela referencia) e
// só a da conta que pediu.
func TestPostgres_ApagarMeusDadosApagaACopia(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	pcA, pcB := e.newDevice(t, a), e.newDevice(t, b)
	for _, ac := range []cloudstore.Actor{{UserID: a, DeviceID: pcA}, {UserID: b, DeviceID: pcB}} {
		if _, err := s.PutDictionaryCopy(e.ctx, ac, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ApagarDadosDaConta(e.ctx, a, contracttest.NewID(), now0); err != nil {
		t.Fatalf("ApagarDadosDaConta: %v", err)
	}
	if n := e.copias(a); n != 0 {
		t.Errorf("o apagar deixou %d cópia(s)", n)
	}
	if n := e.count("SELECT count(*) FROM devices WHERE user_id = $1", a); n != 0 {
		t.Errorf("o apagar deixou %d aparelho(s)", n)
	}
	if n := e.copias(b); n != 1 {
		t.Errorf("a cópia da conta B ficou com %d", n)
	}
}

// A leitura ignora a cópia de um aparelho revogado (a revogação já a apaga; aqui
// a linha é deixada de propósito, como num banco de antes de uma correção): o
// estado é nenhum, o manter de outro PC é "não é a fonte", e o tomar a troca.
func TestPostgres_ALeituraIgnoraOAparelhoRevogado(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a := e.newUser(t)
	pc1, pc2 := e.newDevice(t, a), e.newDevice(t, a)
	a1, a2 := cloudstore.Actor{UserID: a, DeviceID: pc1}, cloudstore.Actor{UserID: a, DeviceID: pc2}
	if _, err := s.PutDictionaryCopy(e.ctx, a1, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); err != nil {
		t.Fatal(err)
	}
	// Revoga sem passar pelo RevogarAparelho: a linha da cópia fica.
	e.exec("UPDATE devices SET revoked_at = now() WHERE id = $1", pc1)
	if n := e.copias(a); n != 1 {
		t.Fatalf("a linha da cópia sumiu (%d): o teste precisa dela", n)
	}
	if info, err := s.DictionaryCopyInfo(e.ctx, a); err != nil || info != nil {
		t.Errorf("DictionaryCopyInfo com a fonte revogada: %+v, %v", info, err)
	}
	if _, _, ok, err := s.GetDictionaryCopy(e.ctx, a); err != nil || ok {
		t.Errorf("GetDictionaryCopy com a fonte revogada: %v, %v", ok, err)
	}
	// O manter de outro PC não acha fonte, e o estado do 409 é nenhum.
	r, err := s.PutDictionaryCopy(e.ctx, a2, copiaDeTeste(2, shaDeTeste('b')), cloudstore.CopiaManter, now0)
	if err != nil || r.Gravou || !r.NaoEraAFonte || r.Atual != nil {
		t.Errorf("manter com a fonte revogada: %+v, %v", r, err)
	}
	// O tomar de outro PC troca a fonte.
	r, err = s.PutDictionaryCopy(e.ctx, a2, copiaDeTeste(2, shaDeTeste('b')), cloudstore.CopiaTomar, now0.Add(time.Minute))
	if err != nil || !r.Gravou || r.Atual == nil || r.Atual.DeviceID != pc2 {
		t.Errorf("tomar com a fonte revogada: %+v, %v", r, err)
	}
	if info, err := s.DictionaryCopyInfo(e.ctx, a); err != nil || info == nil || info.DeviceID != pc2 || info.Termos != 2 {
		t.Errorf("o estado depois do tomar: %+v, %v", info, err)
	}
	if n := e.copias(a); n != 1 {
		t.Errorf("a conta tem %d cópias, quero 1", n)
	}
}

// A chave estrangeira não tem cascata: o aparelho com cópia não se apaga por baixo.
func TestPostgres_OAparelhoComCopiaNaoSeApagaPorBaixo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a := e.newUser(t)
	pc := e.newDevice(t, a)
	if _, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: a, DeviceID: pc}, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(e.ctx, "DELETE FROM devices WHERE id = $1", pc); code(err) != "23503" {
		t.Errorf("apagar o aparelho com cópia: %v, quero a chave estrangeira (23503)", err)
	}
}

// Depois de restaurar um backup, todos os aparelhos saem, e as cópias do
// dicionário com eles.
func TestPostgres_DepoisDeRestaurarApagaAsCopias(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	for _, u := range []string{a, b} {
		pc := e.newDevice(t, u)
		if _, err := s.PutDictionaryCopy(e.ctx, cloudstore.Actor{UserID: u, DeviceID: pc}, copiaDeTeste(1, shaDeTeste('a')), cloudstore.CopiaTomar, now0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DepoisDeRestaurar(e.ctx, now0); err != nil {
		t.Fatalf("DepoisDeRestaurar: %v", err)
	}
	if n := e.count("SELECT count(*) FROM dictionary_copies"); n != 0 {
		t.Errorf("a restauração deixou %d cópia(s)", n)
	}
}
