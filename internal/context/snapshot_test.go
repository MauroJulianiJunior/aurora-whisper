package uiacontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestContextSnapshot_MarshalJSON_Fails prova AC-027: ContextSnapshot
// nunca serializa. json.Marshal deve falhar mesmo quando o snapshot tem
// conteúdo preenchido.
func TestContextSnapshot_MarshalJSON_Fails(t *testing.T) {
	snap := newSnapshot("notepad.exe", "Edit", "Bloco de notas", "seleção secreta", "prefixo ", " sufixo", []string{"campo"})

	_, err := json.Marshal(snap)
	if err == nil {
		t.Fatal("json.Marshal(ContextSnapshot) deveria falhar, mas teve sucesso")
	}
	if !errors.Is(err, ErrSnapshotNotSerializable) {
		t.Fatalf("erro esperado ErrSnapshotNotSerializable, obtido: %v", err)
	}
}

// TestContextSnapshot_MarshalJSON_FailsEvenEmbedded prova que a garantia é
// estrutural: um struct pai que embute (por valor) ContextSnapshot como
// campo também falha ao serializar, então nenhum wrapper esquece a regra
// por acidente.
func TestContextSnapshot_MarshalJSON_FailsEvenEmbedded(t *testing.T) {
	type wrapper struct {
		ID       string
		Snapshot ContextSnapshot
	}
	w := wrapper{ID: "attempt-1", Snapshot: newSnapshot("app.exe", "Edit", "", "sel", "pre", "suf", nil)}

	_, err := json.Marshal(w)
	if err == nil {
		t.Fatal("json.Marshal do struct pai deveria falhar por causa do campo ContextSnapshot")
	}
}

// TestContextSnapshot_MarshalJSON_FailsWhenBlocked garante que até o
// snapshot vazio/bloqueado recusa serialização — a regra não depende do
// conteúdo.
func TestContextSnapshot_MarshalJSON_FailsWhenBlocked(t *testing.T) {
	_, err := json.Marshal(blockedSnapshot())
	if !errors.Is(err, ErrSnapshotNotSerializable) {
		t.Fatalf("erro esperado ErrSnapshotNotSerializable, obtido: %v", err)
	}
}

// TestContextSnapshot_String_DoesNotLeak prova AC-027: String() (e,
// portanto, qualquer %v/%s por cima dele) nunca revela o conteúdo
// capturado.
func TestContextSnapshot_String_DoesNotLeak(t *testing.T) {
	const secretPrefix = "senha-do-banco-1234"
	const secretSuffix = "numero-do-cartao-5678"
	const secretSelection = "trecho-confidencial-do-documento"
	const secretApp = "app-sigiloso.exe"
	const secretLabel = "campo-de-segredo"

	snap := newSnapshot(secretApp, "Edit", "titulo-sigiloso", secretSelection, secretPrefix, secretSuffix, []string{secretLabel})

	for _, rendered := range []string{
		snap.String(),
		fmt.Sprintf("%v", snap),
		fmt.Sprintf("%s", snap),
	} {
		for _, secret := range []string{secretPrefix, secretSuffix, secretSelection, secretApp, secretLabel} {
			if strings.Contains(rendered, secret) {
				t.Fatalf("representação de string vazou conteúdo sensível %q: %q", secret, rendered)
			}
		}
	}
}

// TestContextSnapshot_String_BlockedVsRedacted verifica que String()
// distingue bloqueado de não-bloqueado sem nunca expor conteúdo — a
// distinção em si não é sensível.
func TestContextSnapshot_String_BlockedVsRedacted(t *testing.T) {
	if got := blockedSnapshot().String(); !strings.Contains(got, "blocked") {
		t.Fatalf("snapshot bloqueado deveria mencionar 'blocked', obtido: %q", got)
	}
	normal := newSnapshot("app.exe", "Edit", "", "", "", "", nil)
	if got := normal.String(); !strings.Contains(got, "redacted") {
		t.Fatalf("snapshot normal deveria mencionar 'redacted', obtido: %q", got)
	}
}

// TestTruncateRunes_RespectsLimit prova o truncamento exigido por
// 09-security-privacy.md ("bounded/minimized"), com runes multibyte para
// garantir que o corte não parte um caractere ao meio.
func TestTruncateRunes_RespectsLimit(t *testing.T) {
	long := strings.Repeat("ç", MaxNearCaretRunes+50) // 'ç' é multibyte em UTF-8
	got := truncateRunes(long, MaxNearCaretRunes)

	if n := len([]rune(got)); n != MaxNearCaretRunes {
		t.Fatalf("esperado %d runes após truncamento, obtido %d", MaxNearCaretRunes, n)
	}
	if !strings.HasPrefix(long, got) {
		t.Fatal("truncamento deveria manter o prefixo original")
	}

	short := "abc"
	if got := truncateRunes(short, MaxNearCaretRunes); got != short {
		t.Fatalf("string menor que o limite não deveria ser alterada: %q", got)
	}
}

// TestNewSnapshot_TruncatesAllBoundedFields prova que o construtor
// compartilhado aplica todos os limites documentados (prefixo, sufixo,
// seleção e labels), não apenas um deles.
func TestNewSnapshot_TruncatesAllBoundedFields(t *testing.T) {
	longPrefix := strings.Repeat("p", MaxNearCaretRunes+10)
	longSuffix := strings.Repeat("s", MaxNearCaretRunes+10)
	longSelection := strings.Repeat("x", MaxSelectionRunes+10)
	manyLabels := make([]string, MaxAccessibleLabels+5)
	for i := range manyLabels {
		manyLabels[i] = strings.Repeat("l", MaxAccessibleLabelRunes+10)
	}

	snap := newSnapshot("app.exe", "Edit", "", longSelection, longPrefix, longSuffix, manyLabels)

	if n := len([]rune(snap.PrefixNearCaret)); n != MaxNearCaretRunes {
		t.Errorf("PrefixNearCaret: esperado %d runes, obtido %d", MaxNearCaretRunes, n)
	}
	if n := len([]rune(snap.SuffixNearCaret)); n != MaxNearCaretRunes {
		t.Errorf("SuffixNearCaret: esperado %d runes, obtido %d", MaxNearCaretRunes, n)
	}
	if n := len([]rune(snap.Selection)); n != MaxSelectionRunes {
		t.Errorf("Selection: esperado %d runes, obtido %d", MaxSelectionRunes, n)
	}
	if n := len(snap.AccessibleLabels); n != MaxAccessibleLabels {
		t.Errorf("AccessibleLabels: esperado %d itens, obtido %d", MaxAccessibleLabels, n)
	}
	for i, label := range snap.AccessibleLabels {
		if n := len([]rune(label)); n != MaxAccessibleLabelRunes {
			t.Errorf("AccessibleLabels[%d]: esperado %d runes, obtido %d", i, MaxAccessibleLabelRunes, n)
		}
	}
}

// TestNewSnapshot_DoesNotAliasInputLabels garante que o slice de labels
// devolvido é uma cópia — mutar o slice original do chamador depois não
// pode afetar um snapshot já criado.
func TestNewSnapshot_DoesNotAliasInputLabels(t *testing.T) {
	labels := []string{"a", "b"}
	snap := newSnapshot("app.exe", "Edit", "", "", "", "", labels)

	labels[0] = "mutado"

	if snap.AccessibleLabels[0] != "a" {
		t.Fatalf("ContextSnapshot.AccessibleLabels não deveria compartilhar o backing array do slice de entrada; obtido %q", snap.AccessibleLabels[0])
	}
}
