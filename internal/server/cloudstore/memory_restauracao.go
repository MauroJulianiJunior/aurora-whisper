package cloudstore

import (
	"context"
	"maps"
	"time"
)

// O MemoryStore como Restaurador (restauracao.go), e a cópia do que ele
// guarda, para os testes da restauração: Fotografar é o pg_dump da noite, e
// Restaurar, o pg_restore.

var _ Restaurador = (*MemoryStore)(nil)

func (s *MemoryStore) DepoisDeRestaurar(_ context.Context, _ time.Time) (Restauracao, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r Restauracao
	for _, u := range s.users {
		u.epoch = NewUUIDv7()
		if u.lastSeq > u.watermark {
			u.watermark = u.lastSeq
		}
		r.Contas++
	}
	r.Sessoes = int64(len(s.sessions))
	s.sessions = make(map[string]*memSession)
	for _, d := range s.devices {
		if !d.revoked {
			d.revoked = true
			d.version++
			r.Aparelhos++
		}
	}
	s.tokens = make(map[string]*memToken)
	return r, nil
}

// FotoDaMemoria é a cópia de tudo o que um MemoryStore guarda num instante.
type FotoDaMemoria struct{ s *MemoryStore }

// Fotografar copia o estado inteiro (as contas, os ditados, as lápides, os
// eventos, o aprendido, as sessões, os aparelhos, os tokens e os pedidos):
// o backup da noite, nos testes da restauração (F11d). Só testes.
func (s *MemoryStore) Fotografar() *FotoDaMemoria {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &FotoDaMemoria{s: s.copiar()}
}

// Restaurar volta o MemoryStore ao estado da foto, no lugar (quem aponta para
// ele, a nuvem montada, continua apontando): o pg_restore do backup. A foto
// continua valendo para outra restauração. Só testes.
func (s *MemoryStore) Restaurar(f *FotoDaMemoria) {
	c := f.s.copiarTravado()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users, s.bySub, s.byEmail = c.users, c.bySub, c.byEmail
	s.pending, s.sessions, s.devices, s.tokens, s.pedidos = c.pending, c.sessions, c.devices, c.tokens, c.pedidos
}

func (s *MemoryStore) copiarTravado() *MemoryStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copiar()
}

// copiar é a cópia funda (quem chama segura o mutex). Os ponteiros dentro de
// um ditado, de um evento ou do aprendido são compartilhados: o MemoryStore
// nunca muda o valor apontado, só troca o ponteiro.
func (s *MemoryStore) copiar() *MemoryStore {
	c := NewMemoryStore()
	for id, u := range s.users {
		nu := *u
		nu.devices = maps.Clone(u.devices)
		nu.items = maps.Clone(u.items)
		nu.tombs = maps.Clone(u.tombs)
		nu.learned = maps.Clone(u.learned)
		nu.applied = maps.Clone(u.applied)
		nu.events = append([]Event(nil), u.events...)
		c.users[id] = &nu
	}
	c.bySub, c.byEmail, c.pending = maps.Clone(s.bySub), maps.Clone(s.byEmail), maps.Clone(s.pending)
	for k, v := range s.sessions {
		nv := *v
		c.sessions[k] = &nv
	}
	for k, v := range s.devices {
		nv := *v
		c.devices[k] = &nv
	}
	for k, v := range s.tokens {
		nv := *v
		c.tokens[k] = &nv
	}
	for k, v := range s.pedidos {
		nv := *v
		c.pedidos[k] = &nv
	}
	return c
}
