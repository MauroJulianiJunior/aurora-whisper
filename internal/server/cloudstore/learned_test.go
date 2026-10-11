package cloudstore

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Os testes puros do aprendido na nuvem (F8b): a tabela de DecideLearned,
// linha a linha, e as pré-checagens. A suíte de contrato
// (internal/server/contracttest) prova as mesmas regras pela API.

// learnedPayloadOf é o payload do upsert, com as trocas pedidas (nil apaga a
// chave; use jsonNull para mandar null).
func learnedPayloadOf(t *testing.T, variant, term string, changes map[string]any) json.RawMessage {
	t.Helper()
	p := map[string]any{"variant": variant, "term": term, "restricted": false, "domain": jsonNull{}}
	for k, v := range changes {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	return rawJSON(t, p)
}

// jsonNull vira null no JSON (um nil no mapa apaga a chave).
type jsonNull struct{}

func (jsonNull) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func learnedUpsert(t *testing.T, variant, term string, base int64, changes map[string]any) Mutation {
	t.Helper()
	return Mutation{
		MutationID: testMutation, EntityType: EntityLearned, EntityID: dictionary.IDDoPar(variant),
		Operation: OpUpsert, BaseVersion: base, Payload: learnedPayloadOf(t, variant, term, changes),
	}
}

func learnedUndo(variant string, base int64) Mutation {
	return Mutation{
		MutationID: testMutation, EntityType: EntityLearned, EntityID: dictionary.IDDoPar(variant),
		Operation: OpDelete, BaseVersion: base,
	}
}

// learnedRow é a linha que já está na nuvem, na versão v.
func learnedRow(variant, term string, v int64, undone bool) *Learned {
	created := testNow.Add(-48 * time.Hour)
	l := &Learned{
		ID: dictionary.IDDoPar(variant), UserID: testUser, Variant: variant,
		NormalizedVariant: dictionary.ChaveDoPar(variant), Term: term,
		CreatedAt: created, UpdatedAt: created, Version: v, LastSeq: 7,
	}
	if undone {
		u := testNow.Add(-time.Hour)
		l.UndoneAt = &u
	}
	return l
}

func strPtr(s string) *string { return &s }

// TestDecideLearned_ATabelaDaNuvem é a tabela da ADR-019 ("Decisões da F8",
// as regras da nuvem), uma linha por caso, na ordem. "Grava" é a versão nova,
// o seq e o evento: o Store grava quando Write não é WriteNone.
func TestDecideLearned_ATabelaDaNuvem(t *testing.T) {
	sf := func(term string, v int64, undone bool) *Learned { return learnedRow("sf", term, v, undone) }
	domFiscal := func(l *Learned) *Learned { l.Domain = strPtr("fiscal"); return l }
	restrictedFiscal := func(l *Learned) *Learned { l.Domain, l.Restricted = strPtr("fiscal"), true; return l }
	cases := []struct {
		name    string
		cur     *Learned
		m       Mutation
		status  Status
		code    string
		version int64
		write   Write
	}{
		// não existe
		{"não existe, upsert base 0: applied v1, insere", nil, learnedUpsert(t, "sf", "ECF", 0, nil), StatusApplied, "", 1, WriteInsert},
		{"não existe, upsert base > 0: base_invalida", nil, learnedUpsert(t, "sf", "ECF", 3, nil), StatusRejected, CodeInvalidBase, 0, WriteNone},
		// existe, base do futuro
		{"existe, upsert base > versão: base_invalida", sf("ECF", 2, false), learnedUpsert(t, "sf", "ECF", 3, nil), StatusRejected, CodeInvalidBase, 0, WriteNone},
		{"desfeita, upsert base > versão: base_invalida", sf("ECF", 2, true), learnedUpsert(t, "sf", "ECF", 9, nil), StatusRejected, CodeInvalidBase, 0, WriteNone},
		// ativa com o mesmo conteúdo: nada, nem seq
		{"ativa igual, upsert base = versão: applied com a versão atual", sf("ECF", 2, false), learnedUpsert(t, "sf", "ECF", 2, nil), StatusApplied, "", 2, WriteNone},
		{"ativa igual, upsert base < versão: applied com a versão atual", sf("ECF", 2, false), learnedUpsert(t, "sf", "ECF", 1, nil), StatusApplied, "", 2, WriteNone},
		{"ativa igual, upsert base 0: applied com a versão atual", sf("ECF", 2, false), learnedUpsert(t, "sf", "ECF", 0, nil), StatusApplied, "", 2, WriteNone},
		{"ativa igual, outra grafia da mesma variante: applied com a versão atual", sf("ECF", 2, false), learnedUpsert(t, "S.F.", "ECF", 0, nil), StatusApplied, "", 2, WriteNone},
		{"ativa igual com domínio e restrição, base < versão", restrictedFiscal(sf("ECF", 2, false)),
			learnedUpsert(t, "sf", "ECF", 1, map[string]any{"domain": "fiscal", "restricted": true}), StatusApplied, "", 2, WriteNone},
		{"ativa igual com domínio sem restrição, base 0", domFiscal(sf("ECF", 2, false)),
			learnedUpsert(t, "sf", "ECF", 0, map[string]any{"domain": "fiscal"}), StatusApplied, "", 2, WriteNone},
		// outro conteúdo, ou desfeita, com a base atual: grava
		{"outro termo, upsert base = versão: applied v+1", sf("ECF", 2, false), learnedUpsert(t, "sf", "SPED", 2, nil), StatusApplied, "", 3, WriteUpdate},
		{"desfeita, upsert base = versão: reaprende, v+1", sf("ECF", 2, true), learnedUpsert(t, "sf", "ECF", 2, nil), StatusApplied, "", 3, WriteUpdate},
		{"outra restrição, upsert base = versão: applied v+1", domFiscal(sf("ECF", 2, false)), learnedUpsert(t, "sf", "ECF", 2, map[string]any{"domain": "fiscal", "restricted": true}), StatusApplied, "", 3, WriteUpdate},
		{"sem a restrição agora, upsert base = versão: applied v+1", restrictedFiscal(sf("ECF", 2, false)), learnedUpsert(t, "sf", "ECF", 2, map[string]any{"domain": "fiscal"}), StatusApplied, "", 3, WriteUpdate},
		{"outro domínio, upsert base = versão: applied v+1", domFiscal(sf("ECF", 2, false)), learnedUpsert(t, "sf", "ECF", 2, map[string]any{"domain": "contábil"}), StatusApplied, "", 3, WriteUpdate},
		{"sem domínio agora, upsert base = versão: applied v+1", domFiscal(sf("ECF", 2, false)), learnedUpsert(t, "sf", "ECF", 2, nil), StatusApplied, "", 3, WriteUpdate},
		{"com domínio agora, upsert base = versão: applied v+1", sf("ECF", 2, false), learnedUpsert(t, "sf", "ECF", 2, map[string]any{"domain": "fiscal"}), StatusApplied, "", 3, WriteUpdate},
		{"outro domínio, upsert base < versão: conflict", domFiscal(sf("ECF", 2, false)), learnedUpsert(t, "sf", "ECF", 1, map[string]any{"domain": "contábil"}), StatusConflict, "", 2, WriteNone},
		{"o termo com outra caixa é outro conteúdo", sf("ECF", 2, false), learnedUpsert(t, "sf", "ecf", 2, nil), StatusApplied, "", 3, WriteUpdate},
		// outro conteúdo, ou desfeita, com a base velha: vale o primeiro
		{"outro termo, upsert base < versão: conflict", sf("ECF", 3, false), learnedUpsert(t, "sf", "SPED", 2, nil), StatusConflict, "", 3, WriteNone},
		{"outro termo, upsert base 0: conflict", sf("ECF", 3, false), learnedUpsert(t, "sf", "SPED", 0, nil), StatusConflict, "", 3, WriteNone},
		{"desfeita, upsert base < versão: conflict (o desfazer vence)", sf("ECF", 3, true), learnedUpsert(t, "sf", "ECF", 2, nil), StatusConflict, "", 3, WriteNone},
		{"desfeita, upsert base 0: conflict (o desfazer vence)", sf("ECF", 3, true), learnedUpsert(t, "sf", "ECF", 0, nil), StatusConflict, "", 3, WriteNone},
		// o desfazer
		{"ativa, delete com a base atual: applied v+1 com undone_at", sf("ECF", 2, false), learnedUndo("sf", 2), StatusApplied, "", 3, WriteUpdate},
		{"ativa, delete com a base velha: applied v+1 (não olha a base)", sf("ECF", 5, false), learnedUndo("sf", 1), StatusApplied, "", 6, WriteUpdate},
		{"ativa, delete com a base 0: applied v+1", sf("ECF", 5, false), learnedUndo("sf", 0), StatusApplied, "", 6, WriteUpdate},
		{"ativa, delete com a base do futuro: applied v+1, nunca base_invalida", sf("ECF", 2, false), learnedUndo("sf", 99), StatusApplied, "", 3, WriteUpdate},
		{"desfeita, delete: applied com a versão atual", sf("ECF", 4, true), learnedUndo("sf", 4), StatusApplied, "", 4, WriteNone},
		{"desfeita, delete com outra base: applied com a versão atual", sf("ECF", 4, true), learnedUndo("sf", 1), StatusApplied, "", 4, WriteNone},
		{"não existe, delete: nada_a_desfazer", nil, learnedUndo("sf", 0), StatusRejected, CodeNothingToUndo, 0, WriteNone},
		{"não existe, delete com base: nada_a_desfazer, nunca base_invalida", nil, learnedUndo("sf", 3), StatusRejected, CodeNothingToUndo, 0, WriteNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var before Learned
			if c.cur != nil {
				before = *c.cur
			}
			out := DecideLearned(c.m, c.cur, pc, testNow)
			r := out.Result
			if r.Status != c.status || r.Code != c.code || r.EntityVersion != c.version || out.Write != c.write {
				t.Fatalf("DecideLearned = %+v, Write %v; quero %s %q v%d, Write %v", r, out.Write, c.status, c.code, c.version, c.write)
			}
			if r.Merged {
				t.Error("o applied do aprendido nunca é mesclado")
			}
			if r.MutationID != c.m.MutationID || r.State != nil {
				t.Errorf("resultado = %+v", r)
			}
			if (r.Status == StatusConflict) != (r.LearnedState != nil) {
				t.Errorf("o state vem só no conflict: %+v", r)
			}
			if r.Status == StatusConflict && !reflect.DeepEqual(*r.LearnedState, *c.cur) {
				t.Errorf("state = %+v, quero a linha da nuvem como está", *r.LearnedState)
			}
			if c.cur != nil && !reflect.DeepEqual(before, *c.cur) {
				t.Errorf("a decisão mudou a linha atual: %+v", *c.cur)
			}
			if c.write == WriteNone && !reflect.DeepEqual(out.NextLearned, Learned{}) {
				t.Errorf("sem gravação, mas NextLearned = %+v", out.NextLearned)
			}
		})
	}
}

// O que cada gravação põe na linha.
func TestDecideLearned_ALinhaGravada(t *testing.T) {
	t.Run("insere", func(t *testing.T) {
		m := learnedUpsert(t, "DCTF Web", "DCTFWeb", 0, map[string]any{"domain": "fiscal", "restricted": true})
		m.EntityID = strings.ToUpper(m.EntityID)
		out := DecideLearned(m, nil, pc, testNow.Add(123*time.Nanosecond))
		want := Learned{
			ID: dictionary.IDDoPar("dctfweb"), UserID: testUser, Variant: "DCTF Web", NormalizedVariant: "dctfweb",
			Term: "DCTFWeb", Restricted: true, Domain: strPtr("fiscal"), CreatedAt: testNow, UpdatedAt: testNow, Version: 1,
		}
		if !reflect.DeepEqual(out.NextLearned, want) {
			t.Fatalf("linha nova = %+v\nquero       %+v", out.NextLearned, want)
		}
	})
	t.Run("troca o termo e limpa o desfeito", func(t *testing.T) {
		cur := learnedRow("sf", "ECF", 2, true)
		out := DecideLearned(learnedUpsert(t, "SF", "SPED", 2, nil), cur, pc, testNow)
		n := out.NextLearned
		if n.Term != "SPED" || n.Variant != "SF" || n.UndoneAt != nil || n.Version != 3 || !n.UpdatedAt.Equal(testNow) {
			t.Fatalf("linha = %+v, quero o termo novo, a grafia mandada, sem undone_at, v3", n)
		}
		if n.ID != cur.ID || n.NormalizedVariant != cur.NormalizedVariant || !n.CreatedAt.Equal(cur.CreatedAt) || n.UserID != cur.UserID {
			t.Errorf("a identidade da linha mudou: %+v", n)
		}
	})
	t.Run("o desfazer só põe undone_at", func(t *testing.T) {
		cur := learnedRow("sf", "ECF", 2, false)
		cur.Domain, cur.Restricted = strPtr("fiscal"), true
		out := DecideLearned(learnedUndo("sf", 2), cur, pc, testNow)
		want := *cur
		at := testNow
		want.UndoneAt, want.Version, want.UpdatedAt = &at, 3, testNow
		if !reflect.DeepEqual(out.NextLearned, want) {
			t.Fatalf("linha desfeita = %+v\nquero          %+v", out.NextLearned, want)
		}
	})
}

// As pré-checagens: o que a nuvem recusa antes do banco, sem gravar. Vêm
// antes das linhas da tabela: um payload inválido é campo_invalido mesmo
// quando a linha daria base_invalida ou nada_a_desfazer.
func TestDecideLearned_PreChecagens(t *testing.T) {
	long := strings.Repeat("é", dictionary.MaxRunasPar+1)
	edge := strings.Repeat("é", dictionary.MaxRunasPar) // 160 bytes, 80 runas: vale
	upsert := func(variant, term string, changes map[string]any) Mutation {
		return learnedUpsert(t, variant, term, 0, changes)
	}
	raw := func(payload string) Mutation {
		m := learnedUpsert(t, "sf", "ECF", 0, nil)
		m.Payload = json.RawMessage(payload)
		return m
	}
	// esc é o escape JSON "barra, u e quatro dígitos", montado aos pedaços:
	// o texto do payload fica exatamente o do fio, sem virar o caractere.
	esc := func(hex string) string { return `\` + "u" + hex }
	termo := func(term string) Mutation {
		return raw(`{"variant":"sf","term":"` + term + `","restricted":false,"domain":null}`)
	}
	undoWith := func(payload string) Mutation {
		m := learnedUndo("sf", 0)
		m.Payload = json.RawMessage(payload)
		return m
	}
	cases := []struct {
		name string
		m    Mutation
		code string
	}{
		{"campo fora da lista", upsert("sf", "ECF", map[string]any{"priority": 50}), CodeUnknownField},
		{"target_app", upsert("sf", "ECF", map[string]any{"target_app": "chrome.exe"}), CodeUnknownField},
		{"o mapa de domínios", upsert("sf", "ECF", map[string]any{"dominios": map[string]any{"fiscal": []string{"x.exe"}}}), CodeUnknownField},
		{"campo no delete", undoWith(`{"variant":"sf"}`), CodeUnknownField},
		{"objeto vazio é o payload sem variante", raw(`{}`), CodeInvalidField},
		{"payload que não é objeto", raw(`["sf","ECF"]`), CodeInvalidField},
		{"payload null no upsert", raw(`null`), CodeInvalidField},
		{"payload ausente no upsert", raw(``), CodeInvalidField},
		{"delete com payload que não é objeto", undoWith(`"sf"`), CodeInvalidField},
		{"sem variant", upsert("sf", "ECF", map[string]any{"variant": nil}), CodeInvalidField},
		{"sem term", upsert("sf", "ECF", map[string]any{"term": nil}), CodeInvalidField},
		{"sem restricted", upsert("sf", "ECF", map[string]any{"restricted": nil}), CodeInvalidField},
		{"sem domain", upsert("sf", "ECF", map[string]any{"domain": nil}), CodeInvalidField},
		{"variant null", upsert("sf", "ECF", map[string]any{"variant": jsonNull{}}), CodeInvalidField},
		{"variant número", upsert("sf", "ECF", map[string]any{"variant": 7}), CodeInvalidField},
		{"term null", upsert("sf", "ECF", map[string]any{"term": jsonNull{}}), CodeInvalidField},
		{"restricted null", upsert("sf", "ECF", map[string]any{"restricted": jsonNull{}}), CodeInvalidField},
		{"restricted texto", upsert("sf", "ECF", map[string]any{"restricted": "false"}), CodeInvalidField},
		{"domain número", upsert("sf", "ECF", map[string]any{"domain": 1}), CodeInvalidField},
		{"o id que não confere com a variante", func() Mutation {
			m := upsert("sf", "ECF", nil)
			m.EntityID = dictionary.IDDoPar("sfe")
			return m
		}(), CodeInvalidField},
		{"variante vazia", upsert("", "ECF", nil), CodeInvalidField},
		{"variante sem letra nem dígito", upsert("!!!", "ECF", nil), CodeInvalidField},
		{"variante de espaços", upsert("   ", "ECF", nil), CodeInvalidField},
		{"variante com 81 runas", upsert(long, "ECF", nil), CodeInvalidField},
		{"termo vazio", upsert("sf", "", nil), CodeInvalidField},
		{"termo com 81 runas", upsert("sf", long, nil), CodeInvalidField},
		{"domínio vazio", upsert("sf", "ECF", map[string]any{"domain": ""}), CodeInvalidField},
		{"domínio com 81 runas", upsert("sf", "ECF", map[string]any{"domain": long}), CodeInvalidField},
		{"restrito sem domínio", upsert("sf", "ECF", map[string]any{"restricted": true}), CodeInvalidField},
		{"variante com o caractere nulo", upsert("s\x00f", "ECF", nil), CodeInvalidField},
		{"termo com o caractere nulo", upsert("sf", "E\x00CF", nil), CodeInvalidField},
		{"domínio com o caractere nulo", upsert("sf", "ECF", map[string]any{"domain": "fis\x00cal"}), CodeInvalidField},
		// O texto que não é UTF-8 válido, nos bytes crus ou num escape
		// \u de surrogate solto. O encoding/json troca os dois por U+FFFD sem
		// erro: a conferência é nos bytes, antes de decodificar. Sem ela, "s\xfff"
		// teria a chave de "sf" (o Normalize lê o U+FFFD como espaço).
		{"variante com byte que não é UTF-8", raw("{\"variant\":\"s\xfff\",\"term\":\"ECF\",\"restricted\":false,\"domain\":null}"), CodeInvalidField},
		{"termo com byte que não é UTF-8", raw("{\"variant\":\"sf\",\"term\":\"E\xffCF\",\"restricted\":false,\"domain\":null}"), CodeInvalidField},
		{"domínio com byte que não é UTF-8", raw("{\"variant\":\"sf\",\"term\":\"ECF\",\"restricted\":false,\"domain\":\"fis\xc3cal\"}"), CodeInvalidField},
		{"variante com surrogate alto solto", raw(`{"variant":"s` + esc("d800") + `f","term":"ECF","restricted":false,"domain":null}`), CodeInvalidField},
		{"termo com surrogate baixo solto", termo("E" + esc("dc00") + "CF"), CodeInvalidField},
		{"termo com surrogate alto seguido de outro escape", termo("E" + esc("D800") + esc("0041") + "CF"), CodeInvalidField},
		{"termo com surrogate alto seguido de texto", termo("E" + esc("d800") + "dc00CF"), CodeInvalidField},
		{"termo com surrogate alto seguido de texto com u", termo("E" + esc("d800") + "xudc00CF"), CodeInvalidField},
		{"termo com surrogate alto seguido de outro escape de barra", termo("E" + esc("d800") + `\n` + "dc00CF"), CodeInvalidField},
		{"termo com surrogate alto seguido de um escape acima dos baixos", termo("E" + esc("d800") + esc("e000")), CodeInvalidField},
		{"termo com surrogate alto no fim", termo("ECF" + esc("d800")), CodeInvalidField},
		{"termo com dois surrogates baixos", termo("E" + esc("dc00") + esc("dc00")), CodeInvalidField},
		{"termo com o surrogate baixo antes do alto", termo("E" + esc("dc00") + esc("d800")), CodeInvalidField},
		{"domínio com dois surrogates altos", raw(`{"variant":"sf","term":"ECF","restricted":false,"domain":"fis` + esc("d800") + esc("d800") + `"}`), CodeInvalidField},
		// O que vale.
		{"termo com o par de surrogates escapado", termo("ECF" + esc("d83d") + esc("de00")), ""},
		{"termo com dois pares seguidos, em maiúsculas", termo("E" + esc("D83D") + esc("DE00") + esc("D83D") + esc("DE01") + "CF"), ""},
		{"termo com a barra escapada antes de ud800", termo(`ECF\\` + "ud800"), ""},
		{"termo com U+FFFD de verdade, cru e escapado (é UTF-8 válido)", termo("E\xef\xbf\xbd" + esc("fffd") + "CF"), ""},
		{"termo com escapes comuns", termo(`E\"C\\F` + esc("00e9") + `\/\n`), ""},
		{"variante, termo e domínio com 80 runas", upsert(edge, edge, map[string]any{"domain": edge}), ""},
		{"restrito com domínio", upsert("sf", "ECF", map[string]any{"domain": "fiscal", "restricted": true}), ""},
		{"delete sem payload", learnedUndo("sf", 0), ""},
		{"delete com null", undoWith(`null`), ""},
		{"delete com objeto vazio", undoWith(` { } `), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Sobre uma linha que não existe e uma base que daria
			// base_invalida: a pré-checagem vem antes.
			m := c.m
			if m.Operation == OpUpsert {
				m.BaseVersion = 5
			}
			out := DecideLearned(m, nil, pc, testNow)
			if c.code == "" {
				want := CodeInvalidBase
				if m.Operation == OpDelete {
					want = CodeNothingToUndo
				}
				if out.Result.Code != want {
					t.Fatalf("passou nas pré-checagens? %+v, quero %s (a linha da tabela)", out.Result, want)
				}
				return
			}
			if out.Result.Status != StatusRejected || out.Result.Code != c.code || out.Write != WriteNone {
				t.Fatalf("DecideLearned = %+v (Write %v), quero rejected %s", out.Result, out.Write, c.code)
			}
		})
	}
}

// O UTF-8 inválido é campo_invalido (ADR-019, as pré-checagens da nuvem),
// mesmo que o encoding/json o trocasse por U+FFFD sem erro: o par não é
// gravado com o texto trocado, nem sobre uma linha que já existe.
func TestDecideLearned_UTF8InvalidoNaoViraFFFD(t *testing.T) {
	m := learnedUpsert(t, "sf", "ECF", 0, nil)
	m.Payload = json.RawMessage("{\"variant\":\"sf\",\"term\":\"E\xffCF\",\"restricted\":false,\"domain\":null}")
	for _, cur := range []*Learned{nil, learnedRow("sf", "ECF", 3, false)} {
		m.BaseVersion = 0
		if cur != nil {
			m.BaseVersion = cur.Version
		}
		out := DecideLearned(m, cur, pc, testNow)
		if out.Result.Status != StatusRejected || out.Result.Code != CodeInvalidField || out.Write != WriteNone {
			t.Fatalf("DecideLearned = %+v (Write %v) / %+v, quero rejected campo_invalido", out.Result, out.Write, out.NextLearned)
		}
	}
}
