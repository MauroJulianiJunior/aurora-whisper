package cloudstore

import (
	"time"
)

// As regras puras do push da nota e do marcador de nota (ADR-022, N7): dado o
// estado lido do banco e a mutação, o que gravar e o que responder. As mesmas
// no Store em memória e no do Postgres. Nada aqui toca banco, rede ou log.
//
// A nota segue o desenho do ditado (rules.go), campo por campo:
//
//   - A lixeira é a do ditado: pôr na lixeira sempre vale; restaurar só vale se
//     ninguém mais mexeu na lixeira depois da base (senão a lixeira fica, sem
//     conflito: nada de texto se perde).
//   - O conteúdo é o título e o corpo juntos. Vale o que chegou primeiro: se o
//     servidor mudou o conteúdo depois da base, e o PC mandou outro, o do
//     servidor fica e o do PC vai para o selo (conflict). A edição antiga
//     contra a lixeira também vai para o selo. Antes do selo, a mescla da lista
//     de marcar (MesclarListaDeMarcar): se os dois lados só marcaram e
//     desmarcaram itens, o servidor junta os dois, sem selo.
//   - A cor, fixada, arquivada, os marcadores e a posição: o último que escreve
//     vence, sem selo (enfeite e ordem não são conteúdo).
//   - "Depois da base" é de OUTRO aparelho: o campo que o próprio aparelho
//     mudou por último conta como conhecido por ele (o applied mesclado).
//   - A base velha sempre grava uma versão nova (applied mesclado), como no
//     ditado, para o pull seguinte levar ao PC o estado de verdade.
//
// O apagar de vez (delete) deixa a lápide, que nunca vence: a nota não tem a
// janela de 6 meses, e um create atrasado que chegue depois recebe gone.

// DecideNote decide uma mutação de nota que passou por CheckMutation. cur é a
// nota atual (nula se não existe), tombstoned diz se ela tem lápide, e
// deadLabels são os ids de marcador com lápide entre os que a mutação cita
// (NoteLabelIDs): saem do conjunto da nota. Um id que a nuvem não tem, e que
// não tem lápide, fica (o marcador pode chegar depois da nota).
func DecideNote(m Mutation, cur *Note, tombstoned bool, deadLabels map[string]bool, a Actor, now time.Time) Outcome {
	now = Normalize(now)
	if m.Operation == OpDelete {
		var version int64
		if cur != nil {
			version = cur.Version
		}
		return decideTombstone(m, version, tombstoned)
	}
	if tombstoned {
		return gone(m, ReasonDeleted)
	}
	if cur == nil {
		if m.BaseVersion != 0 {
			// A nota não tem janela: o PC que conhece uma versão de uma nota que
			// a nuvem não tem (o "apagar meus dados", um backup) zera a versão e
			// manda a nota inteira de novo.
			return Rejected(m, CodeInvalidBase)
		}
		return decideNoteCreate(m, deadLabels, a, now)
	}
	if m.BaseVersion > cur.Version {
		return Rejected(m, CodeInvalidBase)
	}

	var p notePatch
	var code string
	if m.BaseVersion == 0 {
		// O recomeço (ou a reconciliação): o PC manda a nota inteira de algo
		// que a nuvem tem e não conhece versão nenhuma. O conteúdo e a lixeira
		// são comparados com tudo o que a nuvem mudou; o resto vem do PC. O
		// ditado de origem e a data de criação ficam os da nuvem.
		p, code = parseNoteCreate(m.Payload)
	} else {
		p, code = parseNotePatch(m.Payload)
	}
	if code != "" {
		return Rejected(m, code)
	}
	if p.trash != nil && tooFarInFuture(*p.trash, now) {
		return Rejected(m, CodeFutureDate)
	}

	// A escolha do selo, como no ditado (DecidePush): se vale, o selo sai e o
	// resto se aplica sobre o resultado; se não vale, cai, e a resposta é
	// mesclada para o pull levar o estado de verdade.
	start := *cur
	choiceApplied, choiceFell := false, false
	if p.hasChoice {
		if noteChoiceHolds(*cur, m.BaseVersion, p, a.Device()) {
			start, choiceApplied = applyNoteChoice(*cur, p.choice, a.Device(), now), true
		} else {
			choiceFell = true
		}
	}

	next, lost, mesclado, changed := mergeNote(start, m.BaseVersion, p, deadLabels, a.Device(), now)
	stale := m.BaseVersion < cur.Version
	switch {
	case lost:
		next.Conflict = &NoteConflict{Titulo: p.titulo, Corpo: p.corpo, DeviceID: a.Device(), At: now}
		next.Version++
		state := next
		return Outcome{
			Result:   Result{MutationID: m.MutationID, Status: StatusConflict, EntityVersion: next.Version, NoteState: &state},
			Write:    WriteUpdate,
			NextNote: next,
		}
	case stale || changed || choiceApplied || choiceFell:
		next.Version++
		return Outcome{
			Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: next.Version,
				Merged: stale || choiceFell || mesclado},
			Write:    WriteUpdate,
			NextNote: next,
		}
	default:
		return Outcome{Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: cur.Version}}
	}
}

// decideTombstone é o apagar de vez da nota e do marcador (operation delete):
// a linha que existe (version > 0) sai, com a lápide e o evento tombstone na
// versão seguinte; sem linha e com lápide, gone; sem nenhuma das duas, a
// lápide nasce (e o evento, na versão 1), para um create atrasado que chegue
// depois receber gone. O payload é vazio (ausente, null ou {}).
func decideTombstone(m Mutation, version int64, tombstoned bool) Outcome {
	if code := checkUndoPayload(m.Payload); code != "" {
		return Rejected(m, code)
	}
	if version == 0 && tombstoned {
		return gone(m, ReasonDeleted)
	}
	return Outcome{
		Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: version + 1},
		Write:  WriteTombstone,
	}
}

func decideNoteCreate(m Mutation, deadLabels map[string]bool, a Actor, now time.Time) Outcome {
	p, code := parseNoteCreate(m.Payload)
	if code != "" {
		return Rejected(m, code)
	}
	if tooFarInFuture(p.createdAt, now) || p.trash != nil && tooFarInFuture(*p.trash, now) {
		return Rejected(m, CodeFutureDate)
	}
	id, _ := CanonicalUUID(m.EntityID)
	n := Note{
		ID: id, UserID: a.UserID, Titulo: p.titulo, Corpo: p.corpo, Cor: p.cor,
		Fixada: p.fixada, Arquivada: p.arquivada, Posicao: p.posicao,
		Marcadores: liveLabels(p.marcadores, deadLabels), DitadoDeOrigem: p.ditadoDeOrigem,
		CreatedAt: p.createdAt, UpdatedAt: now, DeletedAt: p.trash, PurgeAt: noteTrashPurgeAt(p.trash),
		Version: 1, ContentVersion: 1, TrashVersion: 1,
		ContentDeviceID: a.Device(), TrashDeviceID: a.Device(),
	}
	return Outcome{
		Result:   Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 1},
		Write:    WriteInsert,
		NextNote: n,
	}
}

// liveLabels tira do conjunto os marcadores com lápide. Nunca devolve nil.
func liveLabels(ids []string, dead map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !dead[id] {
			out = append(out, id)
		}
	}
	return out
}

// sameLabels diz se dois conjuntos de marcadores (já em ordem) são iguais.
func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeNote aplica à nota atual a mudança que o PC (device) fez sobre a base.
// Devolve o estado novo (sem fechar a versão: quem chama dá o Version++), se o
// conteúdo do PC perdeu (vai para o selo), se a lista de marcar foi mesclada e
// se algum campo mudou.
func mergeNote(cur Note, base int64, p notePatch, deadLabels map[string]bool, device *string, now time.Time) (next Note, lost, mesclado, changed bool) {
	next = cur
	newVersion := cur.Version + 1
	knowsTrash := cur.TrashVersion <= base || sameDevice(cur.TrashDeviceID, device)
	knowsContent := cur.ContentVersion <= base || sameDevice(cur.ContentDeviceID, device)

	// A lixeira primeiro: apagar vence (a regra do ditado, mergePatch).
	if p.hasTrash {
		want, have := p.trash != nil, cur.DeletedAt != nil
		switch {
		case want == have:
		case want:
			next.DeletedAt, next.PurgeAt = p.trash, noteTrashPurgeAt(p.trash)
			next.TrashVersion, next.TrashDeviceID = newVersion, device
			changed = true
		case knowsTrash:
			next.DeletedAt, next.PurgeAt = nil, nil
			next.TrashVersion, next.TrashDeviceID = newVersion, device
			changed = true
		default:
			// Restaurar contra a lixeira de outro aparelho, mais nova que a
			// base: a lixeira fica, sem conflito.
		}
	}

	if p.hasContent && (p.titulo != cur.Titulo || p.corpo != cur.Corpo) {
		trashedAfterBase := cur.DeletedAt != nil && !knowsTrash
		pcTrashes := p.hasTrash && p.trash != nil
		setContent := func(titulo, corpo string) {
			if titulo != cur.Titulo || corpo != cur.Corpo {
				next.Titulo, next.Corpo = titulo, corpo
				next.ContentVersion, next.ContentDeviceID = newVersion, device
				next.UpdatedAt = now
				changed = true
			}
		}
		switch {
		case knowsContent && !(trashedAfterBase && !pcTrashes):
			setContent(p.titulo, p.corpo)
		default:
			// Os dois mudaram o conteúdo, ou é a edição antiga contra a
			// lixeira. Antes do selo, a mescla da lista de marcar: só com o
			// corpo da base, o mesmo título dos dois lados e a nota fora da
			// lixeira (depois da lixeira deste push).
			if p.hasBase && p.titulo == cur.Titulo && next.DeletedAt == nil {
				if corpo, ok := MesclarListaDeMarcar(p.base, cur.Corpo, p.corpo); ok {
					setContent(cur.Titulo, corpo)
					mesclado = true
					break
				}
			}
			lost = true
		}
	}

	// O último que escreve vence, sem selo.
	if p.hasCor && !sameID(cur.Cor, p.cor) {
		next.Cor, changed = p.cor, true
	}
	if p.hasFixada && p.fixada != cur.Fixada {
		next.Fixada, changed = p.fixada, true
	}
	if p.hasArquivada && p.arquivada != cur.Arquivada {
		next.Arquivada, changed = p.arquivada, true
	}
	if p.hasMarcadores {
		if set := liveLabels(p.marcadores, deadLabels); !sameLabels(set, cur.Marcadores) {
			next.Marcadores, changed = set, true
		}
	}
	if p.hasPosicao && p.posicao != cur.Posicao {
		next.Posicao, changed = p.posicao, true
	}
	return next, lost, mesclado, changed
}

// noteChoiceHolds é o choiceHolds do ditado com o conteúdo no lugar do texto:
// o selo de agora é o que o PC viu (o mesmo conflict_at), e ninguém mudou o
// conteúdo depois da base (ou foi o próprio PC).
func noteChoiceHolds(cur Note, base int64, p notePatch, device *string) bool {
	return cur.Conflict != nil && cur.Conflict.At.Equal(p.choiceAt) &&
		(cur.ContentVersion <= base || sameDevice(cur.ContentDeviceID, device))
}

// applyNoteChoice é a escolha do selo, sem fechar a versão: "trocar" põe o
// título e o corpo do selo como conteúdo, como uma mudança do conteúdo feita
// por device; as duas limpam o selo. cur tem selo.
func applyNoteChoice(cur Note, escolha string, device *string, now time.Time) Note {
	next := cur
	c := cur.Conflict
	if escolha == EscolhaTrocar && (c.Titulo != cur.Titulo || c.Corpo != cur.Corpo) {
		next.Titulo, next.Corpo = c.Titulo, c.Corpo
		next.ContentVersion, next.ContentDeviceID = cur.Version+1, device
		next.UpdatedAt = now
	}
	next.Conflict = nil
	return next
}

// DecideLabel decide uma mutação de marcador de nota que passou por
// CheckMutation. cur é o marcador atual (nulo se não existe) e tombstoned diz
// se ele tem lápide.
//
//	O marcador na nuvem | A mutação                 | Resposta                          | Grava
//	com lápide          | upsert                    | gone apagado                      | nada
//	não existe          | upsert, base 0            | applied, versão 1                 | insere
//	não existe          | upsert, base > 0          | rejected base_invalida            | nada
//	existe              | upsert, base > versão     | rejected base_invalida            | nada
//	existe              | upsert, base = versão     | applied (nova se o nome mudou)    | só se mudou
//	existe              | upsert, base < versão     | applied mesclado, versão nova     | sim
//	existe              | delete                    | applied, versão + 1               | lápide
//	não existe, lápide  | delete                    | gone apagado                      | nada
//	não existe          | delete                    | applied, versão 1                 | lápide
//
// O nome do PC vence, sem selo; a nuvem não impõe nome único. A base 0 num
// marcador que existe é o recomeço: o nome do PC vence, com versão nova
// (mesclado).
func DecideLabel(m Mutation, cur *Label, tombstoned bool, a Actor, now time.Time) Outcome {
	now = Normalize(now)
	if m.Operation == OpDelete {
		var version int64
		if cur != nil {
			version = cur.Version
		}
		return decideTombstone(m, version, tombstoned)
	}
	if tombstoned {
		return gone(m, ReasonDeleted)
	}
	if cur == nil {
		if m.BaseVersion != 0 {
			return Rejected(m, CodeInvalidBase)
		}
		p, code := parseLabel(m.Payload, true)
		if code != "" {
			return Rejected(m, code)
		}
		if tooFarInFuture(p.createdAt, now) {
			return Rejected(m, CodeFutureDate)
		}
		id, _ := CanonicalUUID(m.EntityID)
		l := Label{ID: id, UserID: a.UserID, Nome: p.nome, CreatedAt: p.createdAt, UpdatedAt: now, Version: 1}
		return Outcome{
			Result:    Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 1},
			Write:     WriteInsert,
			NextLabel: l,
		}
	}
	if m.BaseVersion > cur.Version {
		return Rejected(m, CodeInvalidBase)
	}
	p, code := parseLabel(m.Payload, m.BaseVersion == 0)
	if code != "" {
		return Rejected(m, code)
	}
	if m.BaseVersion == 0 && tooFarInFuture(p.createdAt, now) {
		return Rejected(m, CodeFutureDate)
	}
	stale := m.BaseVersion < cur.Version
	if !stale && p.nome == cur.Nome {
		return Outcome{Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: cur.Version}}
	}
	next := *cur
	if p.nome != cur.Nome {
		next.Nome, next.UpdatedAt = p.nome, now
	}
	next.Version++
	return Outcome{
		Result:    Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: next.Version, Merged: stale},
		Write:     WriteUpdate,
		NextLabel: next,
	}
}
