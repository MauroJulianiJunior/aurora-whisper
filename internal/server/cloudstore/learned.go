package cloudstore

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Este arquivo é o aprendido na nuvem (ADR-019, "Decisões da F8", F8b): o
// payload do push e as regras puras, a mesma decisão na memória e no
// Postgres. Nada aqui toca banco, rede ou log, e nenhum código de recusa traz
// texto.
//
// # O payload
//
// upsert (aprender, trocar o termo ou a restrição, reaprender), lista
// fechada, os quatro campos sempre presentes:
//
//	{ "variant": "sf", "term": "ECF", "restricted": false, "domain": null }
//
// delete (o desfazer): payload vazio (ausente, null ou {}).
//
// # As pré-checagens
//
// Vêm antes de qualquer gravação, e a recusa não grava entidade nem gasta
// seq (fica só em applied_mutations, como toda recusa do push, para o
// reenvio receber duplicate). Campo fora da lista, ou qualquer campo no
// delete, é campo_desconhecido. É campo_invalido: um campo da lista que falta
// ou tem o tipo errado; o texto que não é UTF-8 válido na variante, no termo
// ou no domínio, conferido nos bytes crus do payload (rawTextValid: o
// encoding/json o trocaria por U+FFFD sem erro); o que dictionary.ConferirPar
// recusa (o caractere nulo; a variante ou o termo fora de 1 a 80 runas; o
// domínio que não é nulo nem tem de 1 a 80 runas; a variante que não
// normaliza para nada; o restrito sem domínio); e o entity_id que não é o
// dictionary.IDDoPar da variante. As
// mesmas checagens o PC faz na projeção (dictionary.ParesDoPainel): o que a
// nuvem recusaria nem sobe. Com elas, nenhum push alcança o UNIQUE (user_id,
// normalized_variant) (23505), os CHECKs da 0016 (23514) nem o erro do byte
// nulo (22021), cujo DETAIL poria a variante no log do Postgres.

// learnedFields é a lista fechada do upsert do aprendido (true =
// obrigatório). Os quatro são obrigatórios: o PC sempre manda o par inteiro,
// e um campo que falta é defeito do cliente, e não "sem domínio".
var learnedFields = map[string]bool{
	"variant":    true,
	"term":       true,
	"restricted": true,
	"domain":     true,
}

// learnedPayload é o par que o PC mandou num upsert.
type learnedPayload struct {
	variant    string
	term       string
	restricted bool
	domain     *string
}

// jsonString lê uma string JSON; null, ou outro tipo, não é string; o texto
// que não é UTF-8 válido (rawTextValid) também não. O resto do conteúdo (o
// nulo, o tamanho) é conferido por ConferirPar.
func jsonString(v json.RawMessage) (string, bool) {
	if isNull(v) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	if !rawTextValid(v) {
		return "", false
	}
	return s, true
}

// rawTextValid diz se a string JSON v, já conferida pelo json.Unmarshal (os
// escapes bem formados), é texto UTF-8 válido: os bytes crus são UTF-8, e
// nenhum escape \u é um surrogate solto (só a metade alta, D800 a DBFF,
// seguida do escape da metade baixa, DC00 a DFFF, forma um caractere). O
// encoding/json troca os dois casos por U+FFFD sem erro, e o
// dictionary.ConferirPar, que vê a string já decodificada, nunca os veria:
// o par seria gravado com o texto trocado ("s\xfff" com a chave de "sf"). O
// U+FFFD de verdade (cru ou escapado) é UTF-8 válido e passa.
func rawTextValid(v json.RawMessage) bool {
	if !utf8.Valid(v) {
		return false
	}
	b := bytes.TrimSpace(v)
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++ // o caractere escapado: a barra, as aspas, o "u"...
		if i >= len(b) || b[i] != 'u' {
			continue
		}
		r, ok := hexEscape(b, i+1)
		if !ok {
			return false
		}
		i += 4 // o último dígito do escape
		if !utf16.IsSurrogate(r) {
			continue
		}
		if r >= 0xDC00 || i+2 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
			return false
		}
		r2, ok := hexEscape(b, i+3)
		if !ok || r2 < 0xDC00 || r2 > 0xDFFF {
			return false
		}
		i += 6 // o último dígito do segundo escape
	}
	return true
}

// hexEscape lê os quatro dígitos hexadecimais de um escape \u a partir de at.
func hexEscape(b []byte, at int) (rune, bool) {
	if at+4 > len(b) {
		return 0, false
	}
	n, err := strconv.ParseUint(string(b[at:at+4]), 16, 16)
	if err != nil {
		return 0, false
	}
	return rune(n), true
}

// parseLearned lê e confere o payload de um upsert do aprendido de m (as
// pré-checagens). Devolve o par ou o código de recusa.
func parseLearned(m Mutation) (learnedPayload, string) {
	obj, code := decodeObject(m.Payload, learnedFields)
	if code != "" {
		return learnedPayload{}, code
	}
	var p learnedPayload
	var ok bool
	if p.variant, ok = jsonString(obj["variant"]); !ok {
		return learnedPayload{}, CodeInvalidField
	}
	if p.term, ok = jsonString(obj["term"]); !ok {
		return learnedPayload{}, CodeInvalidField
	}
	// null num bool não dá erro no encoding/json: confere antes.
	if isNull(obj["restricted"]) || json.Unmarshal(obj["restricted"], &p.restricted) != nil {
		return learnedPayload{}, CodeInvalidField
	}
	if !isNull(obj["domain"]) {
		d, ok := jsonString(obj["domain"])
		if !ok {
			return learnedPayload{}, CodeInvalidField
		}
		p.domain = &d
	}
	if dictionary.ConferirPar(p.variant, p.term, p.restricted, p.domain) != nil {
		return learnedPayload{}, CodeInvalidField
	}
	if entityID, _ := CanonicalUUID(m.EntityID); entityID != dictionary.IDDoPar(p.variant) {
		return learnedPayload{}, CodeInvalidField
	}
	return p, ""
}

// checkUndoPayload confere o payload do delete: vazio (ausente, null ou {}).
// Qualquer campo é campo_desconhecido; o que não é objeto, campo_invalido.
func checkUndoPayload(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || isNull(raw) {
		return ""
	}
	_, code := decodeObject(raw, map[string]bool{})
	return code
}

// sameContent diz se a linha tem o mesmo termo, restrição e domínio que o
// par mandado (a grafia da variante não conta: o id já é o mesmo).
func sameContent(cur Learned, p learnedPayload) bool {
	if cur.Term != p.term || cur.Restricted != p.restricted || (cur.Domain == nil) != (p.domain == nil) {
		return false
	}
	return cur.Domain == nil || *cur.Domain == *p.domain
}

// DecideLearned decide uma mutação do aprendido que passou por CheckMutation
// (ADR-019, "Decisões da F8", a tabela da nuvem). cur é a linha atual (nula
// se não existe), lida e travada pelo Store. Vale o primeiro na nuvem, e o
// desfazer vence (D4). Primeiro as pré-checagens (parseLearned e
// checkUndoPayload); depois as linhas, na ordem (a primeira que casa decide):
//
//	A linha na nuvem                        | A mutação                     | Resposta                       | Grava
//	não existe                              | upsert, base 0                | applied, versão 1              | insere
//	não existe                              | upsert, base > 0              | rejected base_invalida         | nada
//	existe                                  | upsert, base > versão         | rejected base_invalida         | nada
//	ativa, mesmo termo, restrição e domínio | upsert, base ≤ versão         | applied com a versão atual     | nada, nem seq
//	com outro conteúdo, ou desfeita         | upsert, base = versão         | applied, versão + 1            | sim
//	com outro conteúdo, ou desfeita         | upsert, base < versão (0 inc.)| conflict, com a versão e state | nada
//	ativa                                   | delete, qualquer base         | applied, versão + 1, undone_at | sim
//	desfeita                                | delete, qualquer base         | applied com a versão atual     | nada
//	não existe                              | delete, qualquer base         | rejected nada_a_desfazer       | nada
//
// O applied do aprendido nunca é mesclado: o PC sempre guarda a versão que
// volta. O upsert que grava leva o par inteiro (a grafia da variante
// inclusive) e limpa undone_at (reaprender). O evento de toda gravação, o
// desfazer inclusive, é upsert: quem o grava é o Store.
func DecideLearned(m Mutation, cur *Learned, a Actor, now time.Time) Outcome {
	now = Normalize(now)
	if m.Operation == OpDelete {
		if code := checkUndoPayload(m.Payload); code != "" {
			return Rejected(m, code)
		}
		if cur == nil {
			return Rejected(m, CodeNothingToUndo)
		}
		// A mesma regra do desfazer do celular (Store.UndoLearned): a versão
		// e o undone_at saem dela.
		next, changed := UndoPair(*cur, now)
		if !changed {
			return appliedLearned(m, cur.Version)
		}
		return learnedWritten(m, next, WriteUpdate)
	}

	p, code := parseLearned(m)
	if code != "" {
		return Rejected(m, code)
	}
	if cur == nil {
		if m.BaseVersion != 0 {
			// O PC conhece uma versão que a nuvem não tem (o "apagar meus
			// dados", ou outra conta): ele zera a base e manda com 0.
			return Rejected(m, CodeInvalidBase)
		}
		entityID, _ := CanonicalUUID(m.EntityID)
		next := Learned{
			ID: entityID, UserID: a.UserID, NormalizedVariant: dictionary.ChaveDoPar(p.variant),
			CreatedAt: now,
		}
		setPayload(&next, p)
		return writeLearned(m, next, WriteInsert, now)
	}
	if m.BaseVersion > cur.Version {
		return Rejected(m, CodeInvalidBase)
	}
	if cur.UndoneAt == nil && sameContent(*cur, p) {
		// A mesma troca (outro PC aprendeu o mesmo par, ou o reenvio depois
		// de um recomeço): nada muda, e nada gasta seq.
		return appliedLearned(m, cur.Version)
	}
	if m.BaseVersion == cur.Version {
		next := *cur
		setPayload(&next, p)
		next.UndoneAt = nil
		return writeLearned(m, next, WriteUpdate, now)
	}
	// A nuvem mudou depois da base (outro termo, ou o desfazer): vale o
	// primeiro, e o PC segue o state.
	state := *cur
	return Outcome{Result: Result{
		MutationID: m.MutationID, Status: StatusConflict, EntityVersion: cur.Version, LearnedState: &state,
	}}
}

// setPayload põe o par mandado na linha.
func setPayload(l *Learned, p learnedPayload) {
	l.Variant, l.Term, l.Restricted, l.Domain = p.variant, p.term, p.restricted, p.domain
}

// appliedLearned é o applied sem gravação, com a versão atual.
func appliedLearned(m Mutation, version int64) Outcome {
	return Outcome{Result: Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: version}}
}

// writeLearned fecha a versão nova de next e decide gravá-la.
func writeLearned(m Mutation, next Learned, w Write, now time.Time) Outcome {
	next.Version++
	next.UpdatedAt = now
	return learnedWritten(m, next, w)
}

// learnedWritten é o applied de uma linha que já está na versão nova.
func learnedWritten(m Mutation, next Learned, w Write) Outcome {
	return Outcome{
		Result:      Result{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: next.Version},
		Write:       w,
		NextLearned: next,
	}
}

// UndoPair é a regra do desfazer: o par ganha undone_at (a linha continua, o
// aprendido não tem lápide) e a versão mais um. changed=false: já estava
// desfeito, e não há nada a gravar nem seq a gastar. É a regra do delete do
// PC (DecideLearned) e a do desfazer do celular (Store.UndoLearned): o par
// desfeito de um lado ou do outro é a mesma linha. Não mexe no texto (a
// variante, o termo e o domínio ficam: a 0016 exige o term, e reaprender
// reaproveita a linha).
func UndoPair(cur Learned, now time.Time) (next Learned, changed bool) {
	if cur.UndoneAt != nil {
		return cur, false
	}
	now = Normalize(now)
	next = cur
	next.UndoneAt = &now
	next.Version, next.UpdatedAt = cur.Version+1, now
	return next, true
}

// LearnedListLimit é o limite efetivo do ListLearned: o pedido, ou o teto
// (MaxLearnedList) quando vem zero, negativo ou acima dele. Os dois Stores o
// usam, para o teto ser o mesmo.
func LearnedListLimit(limit int) int {
	if limit <= 0 || limit > MaxLearnedList {
		return MaxLearnedList
	}
	return limit
}
