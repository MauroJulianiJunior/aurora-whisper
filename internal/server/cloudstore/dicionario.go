package cloudstore

import (
	"context"
	"time"
)

// A cópia do dicionário de um PC na nuvem (ADR-023, "B" e "C"; a 0027). Este
// arquivo é a parte pura (os tipos, os modos de gravar e a regra que decide o
// PUT, DecidirCopia, a mesma no MemoryStore e no Postgres) e o MemoryStore.
// Nada aqui toca banco, rede ou log, e nenhum valor daqui é logado: o conteúdo
// (os bytes da cópia) só passa por GetDictionaryCopy e PutDictionaryCopy.
//
// Uma cópia por conta, e a nuvem sabe de que PC ela veio (a fonte). Quem grava
// é sempre o token de um PC (o Actor tem aparelho).

// A fonte da cópia, do ponto de vista de um PC: a cópia é deste PC, de outro PC
// da conta, ou não há cópia. É a palavra do JSON (o campo "fonte" do estado).
const (
	FonteEste    = "este"
	FonteOutro   = "outro"
	FonteNenhuma = "nenhuma"
)

// ModoDaCopia é o Dicionario-Fonte do PUT.
type ModoDaCopia string

const (
	// CopiaTomar é o clique em "Levar o dicionário para o celular": grava sempre,
	// e este PC passa a ser a fonte, mesmo que outro fosse.
	CopiaTomar ModoDaCopia = "tomar"
	// CopiaManter é o envio automático, quando o arquivo muda: só grava se este
	// PC já é a fonte.
	CopiaManter ModoDaCopia = "manter"
)

// Valido diz se o modo é um dos dois.
func (m ModoDaCopia) Valido() bool { return m == CopiaTomar || m == CopiaManter }

// CopiaDoDicionario é o estado da cópia da conta: de que PC veio, quantos
// termos tem, a versão (o SHA-256 dos bytes) e quando mudou. Os bytes são lidos
// à parte (GetDictionaryCopy).
type CopiaDoDicionario struct {
	// DeviceID é o PC que é a fonte; DeviceNome, o nome dele (o do aparelho no
	// pareamento). O nome nunca vai ao log.
	DeviceID     string
	DeviceNome   string
	Termos       int
	SHA256       string
	AtualizadaEm time.Time
}

// FontePara é a fonte da cópia vista do PC deviceID: FonteEste se a cópia é
// dele, FonteOutro se é de outro PC; c nil é FonteNenhuma.
func (c *CopiaDoDicionario) FontePara(deviceID string) string {
	switch {
	case c == nil:
		return FonteNenhuma
	case c.DeviceID == deviceID:
		return FonteEste
	}
	return FonteOutro
}

// NovaCopia é o corpo do PUT já conferido: os bytes (dictionary.LerCopia), o
// SHA-256 deles (o cabeçalho, igual ao dos bytes) e o número de termos.
type NovaCopia struct {
	Conteudo []byte
	SHA256   string
	Termos   int
}

// ResultadoDaCopia é o que o PUT fez.
type ResultadoDaCopia struct {
	// Gravou diz se a linha foi escrita (falso: o mesmo SHA-256 do mesmo PC, ou
	// o manter de quem não é a fonte).
	Gravou bool
	// NaoEraAFonte: o modo era manter e este PC não é a fonte (é outro PC, ou não
	// há cópia). Nada foi gravado.
	NaoEraAFonte bool
	// Atual é o estado da cópia depois do pedido (nil: não há cópia). É o que
	// o 200 e o 409 devolvem.
	Atual *CopiaDoDicionario
}

// DecidirCopia decide o PUT de n, do PC deviceID, no modo dado, sobre a cópia
// atual (nil: não há cópia, ou a que há é de um PC revogado):
//   - a cópia atual é deste PC e tem o mesmo SHA-256: é o reenvio, que não grava
//     (nos dois modos);
//   - tomar fica com a cópia: grava, e este PC passa a ser a fonte;
//   - manter só vale para a fonte: se a cópia atual não é deste PC (é de outro,
//     ou não há), não grava e é NaoEraAFonte; se é deste PC, grava.
func DecidirCopia(atual *CopiaDoDicionario, deviceID string, n NovaCopia, modo ModoDaCopia) (grava, naoEraAFonte bool) {
	eDeste := atual != nil && atual.DeviceID == deviceID
	switch {
	case eDeste && atual.SHA256 == n.SHA256:
		return false, false
	case modo == CopiaManter && !eDeste:
		return false, true
	}
	return true, false
}

// DicionarioDaConta é a cópia do dicionário na nuvem. O MemoryStore e o Store
// do Postgres a implementam (ambos dentro de Store), e a mesma suíte
// (internal/server/contracttest) roda contra os dois.
type DicionarioDaConta interface {
	// DictionaryCopyInfo lê o estado da cópia da conta, sem os bytes; nil se não
	// há. A cópia de um aparelho revogado não conta (nil), por garantia: a
	// revogação já a apaga.
	DictionaryCopyInfo(ctx context.Context, userID string) (*CopiaDoDicionario, error)
	// GetDictionaryCopy lê o estado e os bytes da cópia da conta (o ditado no
	// celular monta o Smart com ela). ok falso: não há cópia, ou ela é de um
	// aparelho revogado.
	GetDictionaryCopy(ctx context.Context, userID string) (c CopiaDoDicionario, conteudo []byte, ok bool, err error)
	// PutDictionaryCopy é o PUT (DecidirCopia), numa transação com a linha
	// travada: o Actor tem de ter aparelho (ErrDeviceNotAllowed senão) não
	// revogado (ErrUnknownDevice senão). O conteúdo já foi conferido por quem
	// chama (dictionary.LerCopia, o Smart montado, o SHA-256 contra o
	// cabeçalho).
	PutDictionaryCopy(ctx context.Context, a Actor, n NovaCopia, modo ModoDaCopia, now time.Time) (ResultadoDaCopia, error)
	// DeleteDictionaryCopy apaga a cópia só se o PC do Actor é a fonte.
	// Idempotente: de outro PC, ou sem cópia, não apaga nada e não é erro
	// (apagou falso).
	DeleteDictionaryCopy(ctx context.Context, a Actor) (apagou bool, err error)
}

var _ DicionarioDaConta = (*MemoryStore)(nil)

// memCopia é a linha de dictionary_copies de um usuário.
type memCopia struct {
	deviceID  string
	conteudo  []byte
	sha256    string
	termos    int
	updatedAt time.Time
}

// estadoDaCopia é a cópia vista de fora, ou nil se não há ou o PC foi revogado.
// Sob o mutex.
func (s *MemoryStore) estadoDaCopia(u *memUser) *CopiaDoDicionario {
	c := u.copiaDoDicionario
	if c == nil {
		return nil
	}
	d, ok := s.devices[c.deviceID]
	if !ok || d.revoked {
		return nil
	}
	return &CopiaDoDicionario{DeviceID: c.deviceID, DeviceNome: d.name, Termos: c.termos, SHA256: c.sha256,
		AtualizadaEm: c.updatedAt}
}

func (s *MemoryStore) DictionaryCopyInfo(_ context.Context, userID string) (*CopiaDoDicionario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		return s.estadoDaCopia(u), nil
	}
	return nil, nil
}

func (s *MemoryStore) GetDictionaryCopy(_ context.Context, userID string) (CopiaDoDicionario, []byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return CopiaDoDicionario{}, nil, false, nil
	}
	c := s.estadoDaCopia(u)
	if c == nil {
		return CopiaDoDicionario{}, nil, false, nil
	}
	return *c, append([]byte(nil), u.copiaDoDicionario.conteudo...), true, nil
}

func (s *MemoryStore) PutDictionaryCopy(_ context.Context, a Actor, n NovaCopia, modo ModoDaCopia, now time.Time) (ResultadoDaCopia, error) {
	if a.DeviceID == "" {
		return ResultadoDaCopia{}, ErrDeviceNotAllowed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[a.UserID]
	if !ok {
		return ResultadoDaCopia{}, ErrUnknownUser
	}
	if d, ok := s.devices[a.DeviceID]; !ok || d.userID != a.UserID || d.revoked {
		return ResultadoDaCopia{}, ErrUnknownDevice
	}
	grava, naoEra := DecidirCopia(s.estadoDaCopia(u), a.DeviceID, n, modo)
	if grava {
		u.copiaDoDicionario = &memCopia{deviceID: a.DeviceID, conteudo: append([]byte(nil), n.Conteudo...),
			sha256: n.SHA256, termos: n.Termos, updatedAt: now.UTC()}
	}
	return ResultadoDaCopia{Gravou: grava, NaoEraAFonte: naoEra, Atual: s.estadoDaCopia(u)}, nil
}

func (s *MemoryStore) DeleteDictionaryCopy(_ context.Context, a Actor) (bool, error) {
	if a.DeviceID == "" {
		return false, ErrDeviceNotAllowed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[a.UserID]
	if !ok || u.copiaDoDicionario == nil || u.copiaDoDicionario.deviceID != a.DeviceID {
		return false, nil
	}
	u.copiaDoDicionario = nil
	return true, nil
}
