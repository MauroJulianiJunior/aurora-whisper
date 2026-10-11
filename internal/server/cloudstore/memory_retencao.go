package cloudstore

import (
	"context"
	"sort"
	"time"
)

// A retenção no MemoryStore (ADR-019, "Decisões da F9"): o mesmo desenho do
// Postgres (pgstore/retencao.go), com o mutex no lugar da trava do contador.
// Os candidatos são escolhidos com o mutex solto e cada um é decidido de novo
// com ele preso, pelo ponto único de gravação (write): o ditado restaurado
// entre a escolha e a trava fica.

// AntesDaTravaDaRetencao liga f para rodar antes de cada candidato do Reter
// ser decidido de novo, com o mutex solto (f pode chamar o Store). Só os
// testes a usam, para restaurar um ditado no meio ou cancelar o ctx; nil
// desliga.
func (s *MemoryStore) AntesDaTravaDaRetencao(f func(id string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ganchoDaRetencao = f
}

func (s *MemoryStore) UsuariosDaRetencao(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.users))
	for id := range s.users {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *MemoryStore) Reter(ctx context.Context, userID string, now time.Time) (Retido, error) {
	now = Normalize(now)
	var r Retido
	ids, gancho, err := s.candidatosDaRetencao(userID, now)
	if err != nil {
		return r, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if gancho != nil {
			gancho(id)
			if err := ctx.Err(); err != nil {
				return r, err
			}
		}
		err := s.write(userID, func(u *memUser, seq int64) (bool, error) {
			cur, ok := u.items[id]
			if !ok {
				return false, nil
			}
			d := DecideRetencao(cur.CreatedAt, cur.DeletedAt, cur.PurgeAt, now)
			if d == RetencaoFica {
				return false, nil
			}
			lapide := d == RetencaoComLapide
			u.apagarDeVez(cur, lapide, seq, nil, now)
			r.Somar(d.Contou(cur.DeletedAt != nil))
			return lapide, nil
		})
		if err != nil {
			return r, err
		}
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	// As lápides vencidas e a arrumação, de uma vez com o mutex (no Postgres,
	// em lotes, cada um numa transação curta que trava o contador). A marca
	// d'água não sobe (D2).
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return r, ErrUnknownUser
	}
	for id, tb := range u.tombs {
		if LapideVencida(tb.dictatedAt, now) {
			delete(u.tombs, id)
			r.LapidesVencidas++
		}
	}
	r.Eventos, r.Mutacoes = u.arrumar(now)
	return r, nil
}

// arrumar apaga o sync_event órfão vencido (PrazoDosEventos) e a resposta
// guardada vencida (PrazoDasMutacoes). O evento que é o last_seq de um ditado
// ou de um aprendido vivo (o desfeito inclusive) fica: o pull tira dele o
// aparelho de origem. Os de lápide e os de entidade apagada são órfãos.
func (u *memUser) arrumar(now time.Time) (eventos, mutacoes int64) {
	vivos := make(map[int64]bool, len(u.items)+len(u.learned))
	for _, t := range u.items {
		vivos[t.LastSeq] = true
	}
	for _, l := range u.learned {
		vivos[l.LastSeq] = true
	}
	kept := make([]Event, 0, len(u.events))
	for _, e := range u.events {
		if !vivos[e.Seq] && EventoVencido(e.ChangedAt, now) {
			eventos++
			continue
		}
		kept = append(kept, e)
	}
	u.events = kept
	for id, a := range u.applied {
		if MutacaoVencida(a.at, now) {
			delete(u.applied, id)
			mutacoes++
		}
	}
	return eventos, mutacoes
}

// candidatosDaRetencao escolhe, com o pré-filtro do SQL (CorteDoAtivo e o
// purge_at) e DecideRetencao, os ditados que parecem vencidos, em ordem de
// (created_at, id), como o Postgres os lê. Devolve também o gancho dos testes.
func (s *MemoryStore) candidatosDaRetencao(userID string, now time.Time) ([]string, func(string), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return nil, nil, ErrUnknownUser
	}
	corte := CorteDoAtivo(now)
	var cands []Transcription
	for _, t := range u.items {
		pre := t.DeletedAt == nil && !t.CreatedAt.After(corte) ||
			t.DeletedAt != nil && (t.PurgeAt == nil || !t.PurgeAt.After(now))
		if pre && DecideRetencao(t.CreatedAt, t.DeletedAt, t.PurgeAt, now) != RetencaoFica {
			cands = append(cands, t)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].CreatedAt.Equal(cands[j].CreatedAt) {
			return cands[i].ID < cands[j].ID
		}
		return cands[i].CreatedAt.Before(cands[j].CreatedAt)
	})
	ids := make([]string, len(cands))
	for i, t := range cands {
		ids[i] = t.ID
	}
	return ids, s.ganchoDaRetencao, nil
}
