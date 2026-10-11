package pgstore

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O pareamento no Postgres (ADR-019, F6): o que só o banco prova, a
// concorrência no mesmo code e na mesma aprovação. O comportamento pela API
// está na suíte internal/server/authtest (TestAuth_Postgres).

func (e *pgEnv) pedidoAprovado(t *testing.T, s *Store, userID, verificador string) (cloudstore.PedidoDePareamento, string) {
	t.Helper()
	p := cloudstore.PedidoDePareamento{
		ID: cloudstore.NewUUIDv7(), CodeChallenge: cloudauth.DesafioS256(verificador), InstallID: contracttest.NewID(),
		Nome: "PC", Plataforma: "windows", Versao: "0.2.0", Porta: 51234, Conferencia: cloudauth.NovaConferencia(),
		CriadoEm: now0, ExpiraEm: now0.Add(cloudstore.PrazoDoPedido),
	}
	if err := s.CriarPedidoDePareamento(e.ctx, p); err != nil {
		t.Fatalf("CriarPedidoDePareamento: %v", err)
	}
	code := cloudauth.NovoSegredo()
	if _, err := s.AprovarPedidoDePareamento(e.ctx, p.ID, userID, cloudauth.Hash(code), now0); err != nil {
		t.Fatalf("AprovarPedidoDePareamento: %v", err)
	}
	return p, code
}

func permiteTudo(string) bool { return true }

// Oito trocas simultâneas do mesmo code dão um token só: o UPDATE ... used_at
// IS NULL de uso único segura as outras sete na trava da linha, e elas não
// casam mais.
func TestPostgres_Pareamento_OitoTrocasDoMesmoCode(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-par", Email: "par@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	verificador := cloudauth.NovoSegredo()
	_, code := e.pedidoAprovado(t, s, u.ID, verificador)

	const n = 8
	var wg sync.WaitGroup
	oks, recusas := make(chan string, n), make(chan error, n)
	start := make(chan struct{})
	for range n {
		wg.Go(func() {
			<-start
			a, err := s.TrocarCodigoDePareamento(e.ctx, cloudstore.TrocaDeCodigo{
				CodeHash: cloudauth.Hash(code), Desafio: cloudauth.DesafioS256(verificador),
				TokenHMAC: cloudauth.HMAC([]byte("pepper-do-teste-0123456789abcdef0123"), cloudauth.NovoSegredo()),
				Agora:     now0.Add(time.Minute), Permite: permiteTudo,
			})
			if err != nil {
				recusas <- err
				return
			}
			oks <- a.DeviceID
		})
	}
	close(start)
	wg.Wait()
	close(oks)
	close(recusas)
	if len(oks) != 1 {
		t.Fatalf("%d trocas deram certo, quero 1", len(oks))
	}
	for err := range recusas {
		if !errors.Is(err, cloudstore.ErrCodigo) {
			t.Errorf("recusa = %v, quero ErrCodigo", err)
		}
	}
	if got := e.count("SELECT count(*) FROM device_tokens t JOIN devices d ON d.id = t.device_id WHERE d.user_id = $1", u.ID); got != 1 {
		t.Fatalf("%d tokens, quero 1", got)
	}
	if got := e.count("SELECT count(*) FROM devices WHERE user_id = $1", u.ID); got != 1 {
		t.Fatalf("%d aparelhos, quero 1", got)
	}
}

// Oito aprovações simultâneas do mesmo pedido: uma só grava o code.
func TestPostgres_Pareamento_OitoAprovacoes(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-apr", Email: "apr@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	p := cloudstore.PedidoDePareamento{
		ID: cloudstore.NewUUIDv7(), CodeChallenge: cloudauth.DesafioS256(cloudauth.NovoSegredo()), InstallID: contracttest.NewID(),
		Nome: "PC", Plataforma: "windows", Porta: 51234, Conferencia: cloudauth.NovaConferencia(),
		CriadoEm: now0, ExpiraEm: now0.Add(cloudstore.PrazoDoPedido),
	}
	if err := s.CriarPedidoDePareamento(e.ctx, p); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	certos := 0
	start := make(chan struct{})
	for range n {
		wg.Go(func() {
			<-start
			_, err := s.AprovarPedidoDePareamento(e.ctx, p.ID, u.ID, cloudauth.Hash(cloudauth.NovoSegredo()), now0)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				certos++
			case !errors.Is(err, cloudstore.ErrPedido):
				t.Errorf("aprovação: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	if certos != 1 {
		t.Fatalf("%d aprovações gravaram, quero 1", certos)
	}
	if got := e.count("SELECT count(*) FROM pairing_requests WHERE id = $1 AND code_hash IS NOT NULL", p.ID); got != 1 {
		t.Fatalf("pedido sem o code gravado: %d", got)
	}
}

// A conta desativada entre a aprovação e a troca: o code não vale, e a
// transação desfeita não deixa o used_at (o code continua sem uso, e sem
// aparelho).
func TestPostgres_Pareamento_ContaDesativadaNaoConsome(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-des", Email: "des@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	verificador := cloudauth.NovoSegredo()
	p, code := e.pedidoAprovado(t, s, u.ID, verificador)
	e.exec("UPDATE users SET disabled_at = now() WHERE id = $1", u.ID)
	_, err = s.TrocarCodigoDePareamento(e.ctx, cloudstore.TrocaDeCodigo{
		CodeHash: cloudauth.Hash(code), Desafio: cloudauth.DesafioS256(verificador),
		TokenHMAC: cloudauth.HMAC([]byte("pepper-do-teste-0123456789abcdef0123"), cloudauth.NovoSegredo()),
		Agora:     now0, Permite: permiteTudo,
	})
	if !errors.Is(err, cloudstore.ErrCodigo) {
		t.Fatalf("troca com a conta desativada: %v, quero ErrCodigo", err)
	}
	if got := e.count("SELECT count(*) FROM pairing_requests WHERE id = $1 AND used_at IS NULL", p.ID); got != 1 {
		t.Fatal("a troca recusada deixou o used_at")
	}
	if got := e.count("SELECT count(*) FROM devices WHERE user_id = $1", u.ID); got != 0 {
		t.Fatalf("%d aparelhos, quero 0", got)
	}
}

// A lista de aparelhos no SQL, o que a API não alcança: um aparelho com
// revoked_at e o token ainda na tabela (a revogação pelo Store apaga o token,
// mas a lista não pode depender disso) não aparece; a ordem é do usado por
// último para o mais antigo (um token nunca usado conta pela criação).
func TestPostgres_Aparelhos_ListaIgnoraORevogadoEOrdena(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-lis", Email: "lis@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	comToken := func(nome string, usadoEm any) string {
		dev := e.newDevice(t, u.ID)
		e.exec("UPDATE devices SET name = $2 WHERE id = $1", dev, nome)
		e.exec("INSERT INTO device_tokens (token_hmac, device_id, created_at, last_used_at) VALUES ($1, $2, $3, $4)",
			cloudauth.HMAC([]byte("pepper-do-teste-0123456789abcdef0123"), nome), dev, now0, usadoEm)
		return dev
	}
	comToken("antigo", nil)                    // nunca usado: conta pela criação (now0)
	comToken("recente", now0.Add(2*time.Hour)) // o último uso mais novo entre os que valem
	comToken("meio", now0.Add(time.Hour))
	revogado := comToken("revogado", now0.Add(3*time.Hour)) // o mais novo de todos, mas revogado
	e.exec("UPDATE devices SET revoked_at = $2 WHERE id = $1", revogado, now0.Add(3*time.Hour))
	// Um aparelho de outra conta, com o token mais novo ainda, fica de fora.
	outro := e.newUser(t)
	devOutro := e.newDevice(t, outro)
	e.exec("INSERT INTO device_tokens (token_hmac, device_id, created_at, last_used_at) VALUES ($1, $2, $3, $3)",
		cloudauth.HMAC([]byte("pepper-do-teste-0123456789abcdef0123"), "de-outro"), devOutro, now0.Add(3*time.Hour))

	list, err := s.ListarAparelhos(e.ctx, u.ID, now0.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var nomes []string
	for _, a := range list {
		nomes = append(nomes, a.Nome)
	}
	if got, want := strings.Join(nomes, ","), "recente,meio,antigo"; got != want {
		t.Fatalf("aparelhos = %s; quero %s (o revogado e o de outra conta fora, o usado por último primeiro)", got, want)
	}
}

// A version do aparelho sobe ao revogar e ao parear de novo o mesmo PC (o
// mesmo install_id reaproveita a linha, e revoked_at volta a nulo): é a
// concorrência otimista da tabela (migration 0002).
func TestPostgres_Aparelhos_VersaoSobeAoRevogarEAoParearDeNovo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-ver", Email: "ver@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	install := contracttest.NewID()
	parear := func(agora time.Time) string {
		t.Helper()
		verificador := cloudauth.NovoSegredo()
		p := cloudstore.PedidoDePareamento{
			ID: cloudstore.NewUUIDv7(), CodeChallenge: cloudauth.DesafioS256(verificador), InstallID: install,
			Nome: "PC", Plataforma: "windows", Versao: "0.2.0", Porta: 51234, Conferencia: cloudauth.NovaConferencia(),
			CriadoEm: agora, ExpiraEm: agora.Add(cloudstore.PrazoDoPedido),
		}
		if err := s.CriarPedidoDePareamento(e.ctx, p); err != nil {
			t.Fatalf("CriarPedidoDePareamento: %v", err)
		}
		code := cloudauth.NovoSegredo()
		if _, err := s.AprovarPedidoDePareamento(e.ctx, p.ID, u.ID, cloudauth.Hash(code), agora); err != nil {
			t.Fatalf("AprovarPedidoDePareamento: %v", err)
		}
		a, err := s.TrocarCodigoDePareamento(e.ctx, cloudstore.TrocaDeCodigo{
			CodeHash: cloudauth.Hash(code), Desafio: cloudauth.DesafioS256(verificador),
			TokenHMAC: cloudauth.HMAC([]byte("pepper-do-teste-0123456789abcdef0123"), cloudauth.NovoSegredo()),
			Agora:     agora, Permite: permiteTudo,
		})
		if err != nil {
			t.Fatalf("TrocarCodigoDePareamento: %v", err)
		}
		return a.DeviceID
	}
	versao := func(dev string) int64 {
		t.Helper()
		var v int64
		e.scalar(&v, "SELECT version FROM devices WHERE id = $1", dev)
		return v
	}

	dev := parear(now0)
	if v := versao(dev); v != 1 {
		t.Fatalf("version do aparelho novo = %d, quero 1", v)
	}
	if err := s.RevogarAparelho(e.ctx, u.ID, dev, now0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if v := versao(dev); v != 2 {
		t.Fatalf("version depois de revogar = %d, quero 2", v)
	}
	if again := parear(now0.Add(2 * time.Hour)); again != dev {
		t.Fatalf("parear de novo criou outro aparelho: %s, quero %s", again, dev)
	}
	if v := versao(dev); v != 3 {
		t.Fatalf("version depois de parear de novo = %d, quero 3", v)
	}
	if n := e.count("SELECT count(*) FROM devices WHERE id = $1 AND revoked_at IS NULL", dev); n != 1 {
		t.Fatal("o aparelho pareado de novo continua revogado")
	}
}
