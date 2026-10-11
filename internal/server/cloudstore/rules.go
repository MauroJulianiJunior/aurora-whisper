package cloudstore

import (
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// Este arquivo tem as regras puras: dado o estado lido do banco e a mudança
// pedida, o que gravar e o que responder. Nada aqui toca banco, rede ou log.
//
// As regras do conflito (ADR-019, "Conflito" e "Eco e mescla"):
//
//   - Vale o que chegou primeiro. Se o servidor mudou o texto depois da base
//     do PC, e o PC mandou outro texto, o texto do servidor fica, e o do PC
//     vai para o selo (conflict).
//   - Não é conflito a mesma mudança (o mesmo texto; os dois na lixeira).
//   - Não é conflito mudar campos diferentes: a lixeira (ou o restaurar) de um
//     lado e o texto do outro. O servidor junta as duas e responde applied com
//     mesclado: true.
//   - Apagar vence. Pôr na lixeira sempre vale; restaurar contra uma lixeira
//     mais nova que a base não vale (o item fica na lixeira, e isso também é
//     mescla: nada de texto se perde). E uma edição de texto feita sobre uma
//     base anterior à ida para a lixeira (a "edição antiga") não muda o texto
//     do item na lixeira: vai para o selo (conflict), como o painel já faz
//     (web/src/edicao.ts: gravar num item da lixeira é conflito).
//   - "Depois da base" é de OUTRO aparelho. O campo (texto ou lixeira) cuja
//     última mudança veio do próprio aparelho que manda a mutação conta como
//     conhecido por ele, mesmo com a versão acima da base: o valor é o que
//     ele mandou, e a mudança nova parte dele. É o que acontece depois de um
//     applied mesclado, quando o PC continua com a base velha (abaixo).
//
// O applied com base velha SEMPRE grava uma versão nova, mesmo quando nada
// mudou nos campos: o PC pode ter pulado, num pull anterior, a mudança que
// veio depois da base (o item tinha marcador pendente), e só uma versão nova,
// com seq novo, a traz no pull seguinte. É por isso também que o PC não guarda
// a versão de um applied mesclado: a do pull é outra.

// Write diz o que uma decisão do push manda gravar na entidade.
type Write int

const (
	WriteNone Write = iota
	WriteInsert
	WriteUpdate
)

// Outcome é a decisão sobre uma mutação do push.
type Outcome struct {
	Result Result
	Write  Write
	// Next é a linha do ditado a gravar quando Write não é WriteNone (o
	// LastSeq é do Store, que tem o seq). Só de DecidePush.
	Next Transcription
	// NextLearned é a linha do aprendido a gravar, do mesmo jeito. Só de
	// DecideLearned.
	NextLearned Learned
}

// Record é o que fica em applied_mutations para esta decisão, mandada pelo
// aparelho de a.
func (o Outcome) Record(m Mutation, a Actor) Applied {
	r := o.Result
	code := r.Code
	if r.Status == StatusGone {
		code = r.Reason
	}
	entityID, _ := CanonicalUUID(m.EntityID)
	return Applied{
		EntityType: m.EntityType,
		EntityID:   entityID,
		DeviceID:   a.DeviceID,
		Operation:  RecordedOperation(m.Operation),
		Status:     r.Status,
		Version:    r.EntityVersion,
		Merged:     r.Merged,
		Code:       code,
	}
}

// RecordedOperation é a operação como applied_mutations a guarda (a coluna
// operation da 0009, F8c): upsert ou delete, e vazia para qualquer outra (a
// recusa operacao_invalida). Assim o reenvio de uma operação fora da lista
// compara igual a ela mesma, e o banco nunca guarda texto livre do cliente.
func RecordedOperation(op string) string {
	if op == OpUpsert || op == OpDelete {
		return op
	}
	return ""
}

// CheckMutation confere o que não depende do banco nem do payload. code
// vazio: a mutação segue. recordable=false: a recusa não pode ser gravada em
// applied_mutations (mutation_id ou entity_id que não é UUID), e o Store
// responde sem abrir transação.
//
// O tipo é transcription ou learned (F8). O delete só vale para o learned (o
// desfazer), e ele não olha a base: nunca é base_invalida (ADR-019, "Decisões
// da F8"). O payload do learned é conferido por DecideLearned, antes de
// qualquer gravação.
func CheckMutation(m Mutation) (code string, recordable bool) {
	if _, ok := CanonicalUUID(m.MutationID); !ok {
		return CodeInvalidMutation, false
	}
	if _, ok := CanonicalUUID(m.EntityID); !ok {
		return CodeInvalidMutation, false
	}
	if m.EntityType == "" || len(m.EntityType) > maxEntityTypeLen || strings.ContainsRune(m.EntityType, 0) {
		return CodeInvalidMutation, false
	}
	switch m.EntityType {
	case EntityTranscription, EntityLearned:
	default:
		return CodeUnknownType, true
	}
	switch m.Operation {
	case OpUpsert:
	case OpDelete:
		if m.EntityType != EntityLearned {
			// A lixeira é o deleted_at do upsert; apagar de vez é só da
			// nuvem ("excluir definitivamente" e a retenção).
			return CodeInvalidOperation, true
		}
		// O desfazer é idempotente e não olha a base.
		return "", true
	default:
		return CodeInvalidOperation, true
	}
	if m.BaseVersion < 0 {
		return CodeInvalidBase, true
	}
	return "", true
}

// Rejected é a decisão de recusar com code, sem gravar entidade.
func Rejected(m Mutation, code string) Outcome {
	return Outcome{Result: Result{MutationID: m.MutationID, Status: StatusRejected, Code: code}}
}

// Duplicate é a resposta a um reenvio, a partir do que ficou gravado da
// primeira vez. O mesmo mutation_id para outra entidade, ou mandado por outro
// aparelho, é recusado: só o aparelho que gerou o mutation_id o reenvia, e
// responder duplicate a outro perderia a mudança dele em silêncio (um
// historico.db copiado para um segundo PC com marcadores pendentes, por
// exemplo).
//
// Desde a F8, também com outra operação (RecordedOperation dos dois lados):
// o PC repete o envio em voo com a mesma operação, e o reenvio de um
// desfazer como reaprender, com o mesmo id, perderia para o próprio desfazer
// (ADR-019, "Decisões da F8").
func Duplicate(prev Applied, m Mutation, a Actor) Result {
	entityID, _ := CanonicalUUID(m.EntityID)
	if prev.EntityType != m.EntityType || prev.EntityID != entityID || !strings.EqualFold(prev.DeviceID, a.DeviceID) ||
		prev.Operation != RecordedOperation(m.Operation) {
		return Result{MutationID: m.MutationID, Status: StatusRejected, Code: CodeReusedMutation}
	}
	r := Result{
		MutationID:    m.MutationID,
		Status:        StatusDuplicate,
		Original:      prev.Status,
		EntityVersion: prev.Version,
		Merged:        prev.Merged,
	}
	switch prev.Status {
	case StatusRejected:
		r.Code = prev.Code
	case StatusGone:
		r.Reason = prev.Code
	}
	return r
}

// OutOfWindow diz se um ditado com essa data já passou da janela de 6 meses
// (a mesma conta da retenção, internal/retention). Em UTC: o AddDate soma
// meses no relógio de parede do fuso da data, e o pgx devolve timestamptz no
// fuso local (ADR-019, "Decisões da F9").
func OutOfWindow(createdAt, now time.Time) bool {
	return !now.Before(retention.ActiveExpiryAt(createdAt.UTC()))
}

func tooFarInFuture(t, now time.Time) bool {
	return t.After(now.Add(FutureTolerance))
}

// purgeAt é quando um ditado que entra na lixeira em chegada (o relógio do
// servidor) some de vez: 30 dias depois de ir para a lixeira, ou os 6 meses
// do ditado, o que vier primeiro, e nunca antes de 24 h depois da chegada
// (VencimentoDaLixeira, o vencimento que o job lê).
func purgeAt(createdAt time.Time, deletedAt *time.Time, chegada time.Time) *time.Time {
	if deletedAt == nil {
		return nil
	}
	p := Normalize(VencimentoDaLixeira(createdAt, *deletedAt, chegada))
	return &p
}

// DecidePush decide uma mutação de ditado que passou por CheckMutation. cur é
// o estado atual (nulo se não existe) e tombstoned diz se há lápide.
func DecidePush(m Mutation, cur *Transcription, tombstoned bool, a Actor, now time.Time) Outcome {
	now = Normalize(now)
	if tombstoned {
		return gone(m, ReasonDeleted)
	}
	if cur == nil {
		if m.BaseVersion != 0 {
			// O PC conhece uma versão de um ditado que não existe e não tem
			// lápide: a lápide já venceu (o ditado saiu da janela) ou os
			// dados foram trocados (backup, "apagar meus dados"; aí o epoch
			// muda e o PC descarta a resposta). Nos dois casos, o PC fica com
			// a cópia e para de sincronizá-la.
			return gone(m, ReasonOutOfWindow)
		}
		return decideCreate(m, a, now)
	}
	if OutOfWindow(cur.CreatedAt, now) {
		return gone(m, ReasonOutOfWindow)
	}
	if m.BaseVersion > cur.Version {
		return Rejected(m, CodeInvalidBase)
	}

	var p patch
	if m.BaseVersion == 0 {
		// O PC manda o ditado inteiro de algo que o servidor já tem (o
		// recomeço manda tudo de novo com a base vazia). Ele não conhece
		// versão nenhuma: o que ele tem de texto e de lixeira é comparado com
		// tudo o que o servidor mudou.
		full, code := parseCreate(m.Payload)
		if code != "" {
			return Rejected(m, code)
		}
		p = patch{hasText: true, text: full.FinalText, hasTrash: true, trash: full.DeletedAt}
	} else {
		var code string
		if p, code = parsePatch(m.Payload); code != "" {
			return Rejected(m, code)
		}
	}
	if p.trash != nil && tooFarInFuture(*p.trash, now) {
		return Rejected(m, CodeFutureDate)
	}

	// A escolha do selo feita no PC (F7c) vem antes do resto do payload: se
	// ela vale (o mesmo selo, e ninguém mudou o texto depois da base), o
	// selo sai como no PATCH do celular, e o resto se aplica sobre o
	// resultado. Se não vale (outro aparelho escolheu antes, chegou outro
	// selo, ou o texto mudou), ela cai: vale o que chegou primeiro, o resto
	// segue a mescla de sempre, e a resposta é mesclada, com versão nova,
	// para o pull levar ao PC o estado de verdade (o selo de volta, se ele
	// ainda existe). Nada de texto se perde: o selo continua guardado.
	start := *cur
	choiceApplied, choiceFell := false, false
	if p.hasChoice {
		if choiceHolds(*cur, m.BaseVersion, p, a.Device()) {
			start, choiceApplied = applyChoice(*cur, p.choice, a.Device()), true
		} else {
			choiceFell = true
		}
	}

	next, lostText, changed := mergePatch(start, m.BaseVersion, p, a.Device(), now)
	stale := m.BaseVersion < cur.Version
	switch {
	case lostText:
		next.Conflict = &Conflict{FinalText: p.text, DeviceID: a.Device(), At: now}
		bump(&next, now)
		state := next
		return Outcome{
			Result: Result{MutationID: m.MutationID, Status: StatusConflict, EntityVersion: next.Version, State: &state},
			Write:  WriteUpdate,
			Next:   next,
		}
	case stale || changed || choiceApplied || choiceFell:
		bump(&next, now)
		return Outcome{
			Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: next.Version, Merged: stale || choiceFell},
			Write:  WriteUpdate,
			Next:   next,
		}
	default:
		// A base é a atual e nada muda: nada a gravar.
		return Outcome{Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: cur.Version}}
	}
}

func gone(m Mutation, reason string) Outcome {
	return Outcome{Result: Result{MutationID: m.MutationID, Status: StatusGone, Reason: reason}}
}

func decideCreate(m Mutation, a Actor, now time.Time) Outcome {
	t, code := parseCreate(m.Payload)
	if code != "" {
		return Rejected(m, code)
	}
	if tooFarInFuture(t.CreatedAt, now) || t.DeletedAt != nil && tooFarInFuture(*t.DeletedAt, now) {
		return Rejected(m, CodeFutureDate)
	}
	if OutOfWindow(t.CreatedAt, now) {
		return gone(m, ReasonOutOfWindow)
	}
	t.ID, _ = CanonicalUUID(m.EntityID)
	t.UserID = a.UserID
	t.DeviceID = a.Device()
	t.UpdatedAt = now
	t.PurgeAt = purgeAt(t.CreatedAt, t.DeletedAt, now)
	t.Version, t.TextVersion, t.TrashVersion = 1, 1, 1
	t.TextDeviceID, t.TrashDeviceID = a.Device(), a.Device()
	return Outcome{
		Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 1},
		Write:  WriteInsert,
		Next:   t,
	}
}

// mergePatch aplica ao estado atual a mudança que o PC (device) fez sobre a
// versão base. Devolve o estado novo (com TextVersion e TrashVersion, e o
// aparelho de cada um, já como a gravação vai ficar), se o texto do PC
// perdeu, e se algum campo mudou. now é a chegada, o piso do vencimento da
// lixeira que entra agora.
func mergePatch(cur Transcription, base int64, p patch, device *string, now time.Time) (next Transcription, lostText, changed bool) {
	next = cur
	newVersion := cur.Version + 1
	// O PC conhece o campo quando a última mudança dele é da base ou de
	// antes, ou quando foi ele mesmo que a fez.
	knowsTrash := cur.TrashVersion <= base || sameDevice(cur.TrashDeviceID, device)
	knowsText := cur.TextVersion <= base || sameDevice(cur.TextDeviceID, device)

	// A lixeira primeiro: apagar vence.
	if p.hasTrash {
		want, have := p.trash != nil, cur.DeletedAt != nil
		switch {
		case want == have:
			// A mesma mudança: nada (fica o deleted_at que o servidor tem, e o
			// purge_at dele: a lixeira que já estava lá não ganha piso novo).
		case want:
			next.DeletedAt = p.trash
			next.PurgeAt = purgeAt(cur.CreatedAt, p.trash, now)
			next.TrashVersion, next.TrashDeviceID = newVersion, device
			changed = true
		case knowsTrash:
			// Restaurar: nenhum outro aparelho mexeu na lixeira depois da
			// base.
			next.DeletedAt, next.PurgeAt = nil, nil
			next.TrashVersion, next.TrashDeviceID = newVersion, device
			changed = true
		default:
			// Restaurar contra uma lixeira de outro aparelho, mais nova que a
			// base: a lixeira fica (apagar vence). Não é conflito: nenhum
			// texto se perde.
		}
	}

	if p.hasText && p.text != cur.FinalText {
		trashedAfterBase := cur.DeletedAt != nil && !knowsTrash
		pcTrashes := p.hasTrash && p.trash != nil
		switch {
		case !knowsText:
			// Os dois mudaram o texto: vale o que chegou primeiro.
			lostText = true
		case trashedAfterBase && !pcTrashes:
			// Edição antiga contra a lixeira: apagar vence, e a edição vai
			// para o selo.
			lostText = true
		default:
			next.FinalText = p.text
			next.TextVersion, next.TextDeviceID = newVersion, device
			changed = true
		}
	}
	return next, lostText, changed
}

// sameDevice diz se a última mudança de um campo (field) veio do aparelho que
// manda a mutação. O celular (nulo) nunca é "o mesmo": ele não manda push.
func sameDevice(field, device *string) bool {
	return field != nil && device != nil && strings.EqualFold(*field, *device)
}

// bump fecha a versão nova de uma gravação.
func bump(t *Transcription, now time.Time) {
	t.Version++
	t.UpdatedAt = now
}

// Edit é a edição do CRUD (o celular; até a F3, também o token de um PC, e
// então device é o dele): só sobre a versão atual.
func Edit(cur Transcription, device *string, baseVersion int64, finalText string, now time.Time) (Transcription, error) {
	if baseVersion != cur.Version {
		return Transcription{}, ErrVersionConflict
	}
	if strings.ContainsRune(finalText, 0) {
		return Transcription{}, ErrInvalidText
	}
	if len(finalText) > MaxTextBytes {
		return Transcription{}, ErrTextTooLong
	}
	next := cur
	next.FinalText = finalText
	next.TextVersion, next.TextDeviceID = cur.Version+1, device
	bump(&next, Normalize(now))
	return next, nil
}

// As escolhas do selo (ADR-019, "Decisões da F4"): o PATCH do CRUD com
// resolver_conflito, no lugar do final_text.
const (
	// EscolhaManter: fica o texto atual, e a versão do selo sai.
	EscolhaManter = "manter"
	// EscolhaTrocar: o texto do selo passa a ser o texto final.
	EscolhaTrocar = "trocar"
)

// EscolhaValida diz se s é uma das escolhas do selo.
func EscolhaValida(s string) bool {
	return s == EscolhaManter || s == EscolhaTrocar
}

// ResolveConflict é a escolha do selo pelo CRUD (o celular): só sobre a versão
// atual (ErrVersionConflict) e só num ditado com selo (ErrNoConflict). As duas
// escolhas limpam o selo e gravam uma versão nova, com seq novo, e o PC
// recebe a mudança pelo pull. "trocar" põe o texto do selo como texto final e
// conta como mudança do texto (TextVersion e o aparelho de quem escolheu);
// "manter" não toca no texto, então o aparelho que escreveu o texto que ficou
// continua o conhecendo (ADR-019, "Eco e mescla").
func ResolveConflict(cur Transcription, device *string, baseVersion int64, escolha string, now time.Time) (Transcription, error) {
	if !EscolhaValida(escolha) {
		return Transcription{}, ErrInvalidChoice
	}
	if baseVersion != cur.Version {
		return Transcription{}, ErrVersionConflict
	}
	if cur.Conflict == nil {
		return Transcription{}, ErrNoConflict
	}
	next := applyChoice(cur, escolha, device)
	bump(&next, Normalize(now))
	return next, nil
}

// applyChoice é a escolha do selo, sem fechar a versão (quem chama dá o
// bump): "trocar" põe o texto do selo como texto final, como uma mudança do
// texto feita por device; as duas limpam o selo. cur tem selo.
func applyChoice(cur Transcription, escolha string, device *string) Transcription {
	next := cur
	if escolha == EscolhaTrocar && cur.Conflict.FinalText != cur.FinalText {
		next.FinalText = cur.Conflict.FinalText
		next.TextVersion, next.TextDeviceID = cur.Version+1, device
	}
	next.Conflict = nil
	return next
}

// choiceHolds diz se a escolha do selo que o PC mandou pelo push ainda vale
// (F7c): o selo de agora é o mesmo que o PC viu (o mesmo conflict_at), e
// ninguém mudou o texto depois da base (o texto que o PC viu ao escolher é o
// de agora; a última mudança dele é da base ou de antes, ou do próprio PC).
func choiceHolds(cur Transcription, base int64, p patch, device *string) bool {
	return cur.Conflict != nil && cur.Conflict.At.Equal(p.choiceAt) &&
		(cur.TextVersion <= base || sameDevice(cur.TextDeviceID, device))
}

// Trash põe na lixeira pelo CRUD. changed=false: já estava, nada a gravar.
func Trash(cur Transcription, device *string, now time.Time) (next Transcription, changed bool) {
	if cur.DeletedAt != nil {
		return cur, false
	}
	now = Normalize(now)
	next = cur
	next.DeletedAt = &now
	next.PurgeAt = purgeAt(cur.CreatedAt, &now, now)
	next.TrashVersion, next.TrashDeviceID = cur.Version+1, device
	bump(&next, now)
	return next, true
}

// Restore tira da lixeira pelo CRUD. changed=false: não estava, nada a
// gravar.
func Restore(cur Transcription, device *string, now time.Time) (next Transcription, changed bool) {
	if cur.DeletedAt == nil {
		return cur, false
	}
	next = cur
	next.DeletedAt, next.PurgeAt = nil, nil
	next.TrashVersion, next.TrashDeviceID = cur.Version+1, device
	bump(&next, Normalize(now))
	return next, true
}

// CheckPurge confere que o ditado pode ser apagado de vez: só da lixeira.
func CheckPurge(cur Transcription) error {
	if cur.DeletedAt == nil {
		return ErrNotInTrash
	}
	return nil
}
