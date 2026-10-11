package cloudstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

// O MemoryStore como Accounts: as mesmas regras do Postgres (pgstore), sob o
// mesmo mutex do Store. Serve à suíte de login e sessão
// (internal/server/authtest) e à suíte de contrato.

type memSession struct {
	Sessao
	hmac []byte
}

type memDevice struct {
	userID     string
	installID  string
	name       string
	platform   string
	appVersion string
	version    int64
	createdAt  time.Time
	revoked    bool
}

// memToken é a linha de device_tokens: o aparelho, quando nasceu e o último
// uso (zero: nunca usado).
type memToken struct {
	deviceID string
	criado   time.Time
	usado    time.Time
}

// ultimoUso é o COALESCE(last_used_at, created_at) do Postgres.
func (t *memToken) ultimoUso() time.Time {
	if t.usado.IsZero() {
		return t.criado
	}
	return t.usado
}

// memPedido é a linha de pairing_requests.
type memPedido struct {
	PedidoDePareamento
	approvedBy string
	codeHash   []byte
	usado      bool
}

var _ Accounts = (*MemoryStore)(nil)

func key(b []byte) string { return hex.EncodeToString(b) }

func (s *MemoryStore) CriarLoginPendente(_ context.Context, p LoginPendente) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(p.StateHash)
	if _, ok := s.pending[k]; ok {
		return ErrLoginPendente
	}
	if p.ReturnTo == "" {
		p.ReturnTo = "/"
	}
	s.pending[k] = p
	return nil
}

func (s *MemoryStore) ConsumirLoginPendente(_ context.Context, stateHash, preHash []byte, now time.Time) (LoginPendente, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(stateHash)
	p, ok := s.pending[k]
	// Como o WHERE do Postgres: o pre errado não acha a linha, e ela fica.
	if !ok || !now.Before(p.ExpiraEm) || !bytes.Equal(p.PreHash, preHash) {
		return LoginPendente{}, ErrLoginPendente
	}
	// Uso único: a linha some (no Postgres, used_at é preenchido e a linha
	// fica até a arrumação; para quem chama, é o mesmo).
	delete(s.pending, k)
	return p, nil
}

func (s *MemoryStore) EntrarComGoogle(_ context.Context, id Identidade, _ time.Time) (Usuario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userID, ok := s.bySub[id.Sub]; ok {
		u := s.users[userID]
		if u.disabled {
			return Usuario{}, ErrContaDesativada
		}
		if u.email != id.Email {
			if other, taken := s.byEmail[id.Email]; taken && other != userID {
				return Usuario{}, ErrEmailEmUso
			}
			delete(s.byEmail, u.email)
			u.email = id.Email
			s.byEmail[id.Email] = userID
		}
		u.nome = id.Nome
		return Usuario{ID: userID, Email: u.email, Nome: u.nome}, nil
	}
	if _, taken := s.byEmail[id.Email]; taken {
		return Usuario{}, ErrSubDiferente
	}
	userID := NewUUIDv7()
	u := newMemUser()
	u.sub, u.email, u.nome = id.Sub, id.Email, id.Nome
	s.users[userID] = u
	s.bySub[id.Sub] = userID
	s.byEmail[id.Email] = userID
	return Usuario{ID: userID, Email: u.email, Nome: u.nome}, nil
}

func (s *MemoryStore) CriarSessao(_ context.Context, n NovaSessao) (Sessao, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[n.UserID]; !ok {
		return Sessao{}, ErrUnknownUser
	}
	if n.Substitui != nil {
		delete(s.sessions, key(n.Substitui))
	}
	k := key(n.TokenHMAC)
	if _, ok := s.sessions[k]; ok || len(n.TokenHMAC) != 32 {
		return Sessao{}, ErrCredencial
	}
	sess := Sessao{
		ID: NewUUIDv7(), UserID: n.UserID, Rotulo: n.Rotulo,
		CriadaEm: n.Agora, UltimoUso: n.Agora, ExpiraEm: n.ExpiraEm,
	}
	s.sessions[k] = &memSession{Sessao: sess, hmac: append([]byte(nil), n.TokenHMAC...)}
	return sess, nil
}

func (s *MemoryStore) ResolverSessao(_ context.Context, tokenHMAC []byte, now time.Time, p PoliticaDeSessao) (SessaoResolvida, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[key(tokenHMAC)]
	if !ok || !now.Before(sess.ExpiraEm) || !now.Before(sess.CriadaEm.Add(p.Teto)) {
		return SessaoResolvida{}, ErrCredencial
	}
	u, ok := s.users[sess.UserID]
	if !ok || u.disabled {
		return SessaoResolvida{}, ErrCredencial
	}
	sess.UltimoUso = now
	sess.ExpiraEm = p.ExpiracaoDaSessao(sess.CriadaEm, now)
	return SessaoResolvida{Sessao: sess.Sessao, Email: u.email, Nome: u.nome}, nil
}

func (s *MemoryStore) ListarSessoes(_ context.Context, userID string, now time.Time) ([]Sessao, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Sessao{}
	for _, sess := range s.sessions {
		if sess.UserID == userID && now.Before(sess.ExpiraEm) {
			out = append(out, sess.Sessao)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UltimoUso.Equal(out[j].UltimoUso) {
			return out[i].ID < out[j].ID
		}
		return out[i].UltimoUso.After(out[j].UltimoUso)
	})
	return out, nil
}

func (s *MemoryStore) DerrubarSessao(_ context.Context, userID, sessionID string) error {
	canon, ok := CanonicalUUID(sessionID)
	if !ok {
		return ErrSessaoNaoEncontrada
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sess := range s.sessions {
		if sess.ID == canon && sess.UserID == userID {
			delete(s.sessions, k)
			return nil
		}
	}
	return ErrSessaoNaoEncontrada
}

func (s *MemoryStore) DerrubarSessaoPorHMAC(_ context.Context, tokenHMAC []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key(tokenHMAC))
	return nil
}

func (s *MemoryStore) ResolverTokenDeAparelho(_ context.Context, tokenHMAC []byte, now time.Time) (AparelhoResolvido, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[key(tokenHMAC)]
	if !ok || TokenVencido(t.ultimoUso(), now) {
		return AparelhoResolvido{}, ErrCredencial
	}
	d, ok := s.devices[t.deviceID]
	if !ok || d.revoked {
		return AparelhoResolvido{}, ErrCredencial
	}
	u, ok := s.users[d.userID]
	if !ok || u.disabled {
		return AparelhoResolvido{}, ErrCredencial
	}
	// Como o UPDATE ... SET last_used_at do Postgres: o uso anda o prazo.
	t.usado = now
	return AparelhoResolvido{
		UserID: d.userID, Email: u.email, Nome: u.nome,
		DeviceID: t.deviceID, DeviceNome: d.name, Plataforma: d.platform,
	}, nil
}

func (s *MemoryStore) CriarPedidoDePareamento(_ context.Context, p PedidoDePareamento) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pedidos[p.ID]; ok {
		return ErrPedido
	}
	s.pedidos[p.ID] = &memPedido{PedidoDePareamento: p}
	return nil
}

// pedidoEsperando: o pedido existe, não foi aprovado nem usado, e não
// venceu em now (o WHERE do Postgres).
func (s *MemoryStore) pedidoEsperando(id string, now time.Time) (*memPedido, bool) {
	canon, ok := CanonicalUUID(id)
	if !ok {
		return nil, false
	}
	p, ok := s.pedidos[canon]
	if !ok || p.approvedBy != "" || p.usado || !now.Before(p.ExpiraEm) {
		return nil, false
	}
	return p, true
}

func (s *MemoryStore) LerPedidoDePareamento(_ context.Context, id string, now time.Time) (PedidoDePareamento, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pedidoEsperando(id, now)
	if !ok {
		return PedidoDePareamento{}, ErrPedido
	}
	return p.PedidoDePareamento, nil
}

func (s *MemoryStore) AprovarPedidoDePareamento(_ context.Context, id, userID string, codeHash []byte, now time.Time) (PedidoDePareamento, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pedidoEsperando(id, now)
	if !ok {
		return PedidoDePareamento{}, ErrPedido
	}
	if _, ok := s.users[userID]; !ok || len(codeHash) != 32 {
		return PedidoDePareamento{}, ErrPedido
	}
	p.approvedBy = userID
	p.codeHash = append([]byte(nil), codeHash...)
	return p.PedidoDePareamento, nil
}

func (s *MemoryStore) TrocarCodigoDePareamento(_ context.Context, t TrocaDeCodigo) (AparelhoPareado, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Como o UPDATE ... WHERE do Postgres: o code, o desafio, aprovado, não
	// usado e não vencido, tudo na busca. Um verificador errado não acha a
	// linha e não a consome.
	var p *memPedido
	for _, x := range s.pedidos {
		if x.approvedBy != "" && !x.usado && t.Agora.Before(x.ExpiraEm) &&
			bytes.Equal(x.codeHash, t.CodeHash) && x.CodeChallenge == t.Desafio {
			p = x
			break
		}
	}
	if p == nil {
		return AparelhoPareado{}, ErrCodigo
	}
	u, ok := s.users[p.approvedBy]
	if !ok || u.disabled || t.Permite == nil || !t.Permite(u.email) || len(t.TokenHMAC) != 32 {
		return AparelhoPareado{}, ErrCodigo
	}
	if _, ok := s.tokens[key(t.TokenHMAC)]; ok {
		return AparelhoPareado{}, ErrCodigo
	}
	p.usado = true

	// O aparelho: o mesmo de antes pelo usuário e o install_id (o UNIQUE
	// (user_id, install_id) do Postgres), sem revogação e com a versão mais
	// um; senão, um novo.
	var deviceID string
	for id, d := range s.devices {
		if d.userID == p.approvedBy && d.installID == p.InstallID {
			deviceID = id
			d.name, d.platform, d.appVersion = p.Nome, p.Plataforma, p.Versao
			d.revoked = false
			d.version++
			break
		}
	}
	if deviceID == "" {
		deviceID = NewUUIDv7()
		s.devices[deviceID] = &memDevice{
			userID: p.approvedBy, installID: p.InstallID, name: p.Nome, platform: p.Plataforma,
			appVersion: p.Versao, version: 1, createdAt: t.Agora,
		}
		u.devices[deviceID] = true
	}
	// Um token por aparelho: o de antes sai.
	for k, tok := range s.tokens {
		if tok.deviceID == deviceID {
			delete(s.tokens, k)
		}
	}
	s.tokens[key(t.TokenHMAC)] = &memToken{deviceID: deviceID, criado: t.Agora}
	return AparelhoPareado{DeviceID: deviceID, UserID: p.approvedBy, Email: u.email}, nil
}

func (s *MemoryStore) ListarAparelhos(_ context.Context, userID string, now time.Time) ([]Aparelho, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Aparelho{}
	for _, t := range s.tokens {
		d, ok := s.devices[t.deviceID]
		if !ok || d.userID != userID || d.revoked || TokenVencido(t.ultimoUso(), now) {
			continue
		}
		out = append(out, Aparelho{
			ID: t.deviceID, Nome: d.name, Plataforma: d.platform, Versao: d.appVersion,
			ConectadoEm: t.criado, UltimoUso: t.ultimoUso(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UltimoUso.Equal(out[j].UltimoUso) {
			return out[i].ID < out[j].ID
		}
		return out[i].UltimoUso.After(out[j].UltimoUso)
	})
	return out, nil
}

func (s *MemoryStore) RevogarAparelho(_ context.Context, userID, deviceID string, _ time.Time) error {
	canon, ok := CanonicalUUID(deviceID)
	if !ok {
		return ErrAparelhoNaoEncontrado
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[canon]
	if !ok || d.userID != userID || d.revoked {
		return ErrAparelhoNaoEncontrado
	}
	d.revoked = true
	d.version++
	for k, t := range s.tokens {
		if t.deviceID == canon {
			delete(s.tokens, k)
		}
	}
	return nil
}

func (s *MemoryStore) ApagarDadosDaConta(_ context.Context, userID, keepSessionID string, _ time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return "", ErrUnknownUser
	}
	u.epoch = NewUUIDv7()
	if u.lastSeq > u.watermark {
		u.watermark = u.lastSeq
	}
	u.items = make(map[string]Transcription)
	u.tombs = make(map[string]memTomb)
	u.learned = make(map[string]Learned)
	u.events = nil
	u.applied = make(map[string]memApplied)
	// As chaves da IA (ADR-021, C3a): o DELETE FROM ai_provider_keys do
	// Postgres.
	u.chavesDeIA = nil
	for deviceID := range u.devices {
		delete(s.devices, deviceID)
	}
	u.devices = make(map[string]bool)
	for k, t := range s.tokens {
		if _, ok := s.devices[t.deviceID]; !ok {
			delete(s.tokens, k)
		}
	}
	// Os pedidos de pareamento que a conta aprovou (o DELETE ... WHERE
	// approved_by do Postgres).
	for id, p := range s.pedidos {
		if p.approvedBy == userID {
			delete(s.pedidos, id)
		}
	}
	for k, sess := range s.sessions {
		if sess.UserID == userID && sess.ID != keepSessionID {
			delete(s.sessions, k)
		}
	}
	return u.epoch, nil
}

func (s *MemoryStore) Arrumar(_ context.Context, now time.Time) (Arrumacao, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Arrumacao
	for k, p := range s.pending {
		if !now.Before(p.ExpiraEm) {
			delete(s.pending, k)
			a.LoginsPendentes++
		}
	}
	for k, sess := range s.sessions {
		if !now.Before(sess.ExpiraEm) {
			delete(s.sessions, k)
			a.Sessoes++
		}
	}
	for id, p := range s.pedidos {
		if !now.Before(p.ExpiraEm) {
			delete(s.pedidos, id)
			a.Pedidos++
		}
	}
	return a, nil
}

// --- Ajudas para os testes (os que não passam pelo pareamento; desativar
// uma conta não tem rota). ---

// AddConta cria a conta com o id escolhido, como o primeiro login a criaria
// (EntrarComGoogle, que depois a acha pelo sub). O servidor de teste (cmd/server
// teste) a usa para a conta de teste ter o mesmo id a cada subida: o PC ligado
// a ela (a D3 da ADR-019, F6) conecta de novo depois de a nuvem de teste
// reiniciar. Recusa um id, um sub ou um e-mail que já existam.
func (s *MemoryStore) AddConta(userID string, id Identidade) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, idUsado := s.users[userID]
	_, subUsado := s.bySub[id.Sub]
	_, emailUsado := s.byEmail[id.Email]
	if userID == "" || id.Sub == "" || id.Email == "" || idUsado || subUsado || emailUsado {
		return errContaExiste
	}
	u := newMemUser()
	u.sub, u.email, u.nome = id.Sub, id.Email, id.Nome
	s.users[userID] = u
	s.bySub[id.Sub] = userID
	s.byEmail[id.Email] = userID
	return nil
}

var errContaExiste = errors.New("cloudstore: a conta já existe (o id, o sub ou o e-mail) ou veio sem eles")

// AddDeviceToken grava o HMAC do token de um aparelho que AddDevice criou,
// nascido agora (time.Now, como o DEFAULT now() do Postgres).
func (s *MemoryStore) AddDeviceToken(deviceID string, tokenHMAC []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[key(tokenHMAC)] = &memToken{deviceID: deviceID, criado: time.Now()}
}

// RevokeDevice revoga um aparelho: o token dele deixa de valer.
func (s *MemoryStore) RevokeDevice(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.devices[deviceID]; ok {
		d.revoked = true
	}
}

// DisableUser desativa a conta (users.disabled_at).
func (s *MemoryStore) DisableUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		u.disabled = true
	}
}

// Contagem é quantas linhas há de cada coisa (para os testes de "não cria
// nada").
type Contagem struct {
	Usuarios, Contadores, Sessoes, LoginsPendentes, Aparelhos, Tokens, Ditados, Eventos, Pedidos int
}

// Contar conta as linhas de todos os usuários.
func (s *MemoryStore) Contar() Contagem {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := Contagem{
		Usuarios: len(s.users), Contadores: len(s.users), Sessoes: len(s.sessions),
		LoginsPendentes: len(s.pending), Aparelhos: len(s.devices), Tokens: len(s.tokens),
		Pedidos: len(s.pedidos),
	}
	for _, u := range s.users {
		c.Ditados += len(u.items)
		c.Eventos += len(u.events)
	}
	return c
}

// Despejo devolve, como texto, tudo o que o MemoryStore guarda das contas
// (e-mails, subs, rótulos, hashes e HMACs em hex, verificadores, nonces e
// voltas): o teste confere que um segredo em claro não está em lugar nenhum.
func (s *MemoryStore) Despejo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for id, u := range s.users {
		b.WriteString(id + "|" + u.sub + "|" + u.email + "|" + u.nome + "|" + u.epoch + "\n")
	}
	for k, p := range s.pending {
		b.WriteString(k + "|" + key(p.PreHash) + "|" + p.CodeVerifier + "|" + p.Nonce + "|" + p.ReturnTo + "\n")
	}
	for k, sess := range s.sessions {
		b.WriteString(k + "|" + sess.ID + "|" + sess.UserID + "|" + sess.Rotulo + "\n")
	}
	for k, t := range s.tokens {
		b.WriteString(k + "|" + t.deviceID + "\n")
	}
	for id, p := range s.pedidos {
		b.WriteString(id + "|" + p.CodeChallenge + "|" + p.InstallID + "|" + p.Nome + "|" + p.Conferencia + "|" +
			p.approvedBy + "|" + key(p.codeHash) + "\n")
	}
	for id, d := range s.devices {
		b.WriteString(id + "|" + d.userID + "|" + d.installID + "|" + d.name + "|" + d.appVersion + "\n")
	}
	// As chaves da IA (ADR-021, C3a): os bytes em hex, como o bytea do
	// Postgres sai no texto, e também crus, para a varredura achar a chave em
	// claro de qualquer jeito que ela estivesse guardada.
	for id, u := range s.users {
		for p, k := range u.chavesDeIA {
			b.WriteString(id + "|" + p + "|" + key(k.IDDaCifra) + "|" + key(k.Nonce) + "|" + key(k.Cifrado) + "|" +
				string(k.Cifrado) + "|" + k.Final + "\n")
		}
	}
	return b.String()
}
