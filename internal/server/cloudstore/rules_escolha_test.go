package cloudstore

import (
	"encoding/json"
	"testing"
	"time"
)

// A escolha do selo feita no PC, pelo push (ADR-019, "Decisões da F7c"): o
// payload de mudar com resolver_conflito e conflict_at. Ela vale só sobre o
// mesmo selo e sem o texto mudado depois da base (por outro aparelho); senão
// cai, vale o que chegou primeiro, e a resposta é mesclada (com versão nova,
// para o pull levar o estado ao PC). Nada de texto se perde.
func TestDecidePush_EscolhaDoSeloPeloPush(t *testing.T) {
	seloEm := testNow.Add(-2 * time.Hour)
	outroSelo := testNow.Add(-3 * time.Hour)
	// selado: v3, o texto "do PC 2" (mudado na v2 pelo outro PC), e o selo
	// "do PC 1" (este), que o PC conhece pela v3.
	selado := func() *Transcription {
		c := by(existing(3, 2, 1, "do PC 2", false), otherDevice, "")
		d := testDevice
		c.Conflict = &Conflict{FinalText: "do PC 1", DeviceID: &d, At: seloEm}
		return c
	}
	escolha := func(e string, at time.Time, mais map[string]any) map[string]any {
		p := map[string]any{"resolver_conflito": e, "conflict_at": at.Format(time.RFC3339Nano)}
		for k, v := range mais {
			p[k] = v
		}
		return p
	}
	type want struct {
		status      Status
		merged      bool
		text        string
		selo        string // "" = sem selo
		trashed     bool
		textVersion int64
		textDevice  string
	}
	cases := []struct {
		name  string
		cur   func() *Transcription
		base  int64
		patch map[string]any
		want  want
	}{
		{"manter: o selo sai, o texto e quem o escreveu ficam", selado, 3, escolha("manter", seloEm, nil),
			want{status: StatusApplied, text: "do PC 2", textVersion: 2, textDevice: otherDevice}},
		{"trocar: o texto do selo vale, como mudança deste PC", selado, 3, escolha("trocar", seloEm, nil),
			want{status: StatusApplied, text: "do PC 1", textVersion: 4, textDevice: testDevice}},
		{"outro selo chegou: a escolha cai, o selo de agora fica", selado, 3, escolha("trocar", outroSelo, nil),
			want{status: StatusApplied, merged: true, text: "do PC 2", selo: "do PC 1", textVersion: 2, textDevice: otherDevice}},
		{"outro aparelho já escolheu: a escolha cai", func() *Transcription {
			c := selado()
			c.Conflict, c.Version = nil, 4
			return c
		}, 3, escolha("trocar", seloEm, nil),
			want{status: StatusApplied, merged: true, text: "do PC 2", textVersion: 2, textDevice: otherDevice}},
		{"o celular mudou o texto depois da base: a escolha cai", func() *Transcription {
			c := by(selado(), "", "")
			c.Version, c.TextVersion, c.FinalText = 4, 4, "do celular"
			return c
		}, 3, escolha("manter", seloEm, nil),
			want{status: StatusApplied, merged: true, text: "do celular", selo: "do PC 1", textVersion: 4}},
		{"este PC mudou o texto depois da base: a escolha vale", func() *Transcription {
			c := by(selado(), testDevice, "")
			c.Version, c.TextVersion, c.FinalText = 4, 4, "editado aqui"
			return c
		}, 3, escolha("manter", seloEm, nil),
			want{status: StatusApplied, merged: true, text: "editado aqui", textVersion: 4, textDevice: testDevice}},
		{"a lixeira mudou depois da base: a escolha vale, mesclada", func() *Transcription {
			c := selado()
			d := testNow.Add(-time.Minute)
			c.Version, c.TrashVersion, c.DeletedAt, c.PurgeAt = 4, 4, &d, purgeAt(c.CreatedAt, &d, d)
			return c
		}, 3, escolha("trocar", seloEm, nil),
			want{status: StatusApplied, merged: true, text: "do PC 1", trashed: true, textVersion: 5, textDevice: testDevice}},
		{"manter com o texto novo junto: o selo sai e o texto vale", selado, 3,
			escolha("manter", seloEm, map[string]any{"final_text": "depois de escolher"}),
			want{status: StatusApplied, text: "depois de escolher", textVersion: 4, textDevice: testDevice}},
		{"a escolha que cai com um texto junto: o texto perde e vai para o selo", func() *Transcription {
			c := by(selado(), "", "")
			c.Version, c.TextVersion, c.FinalText = 4, 4, "do celular"
			return c
		}, 3, escolha("manter", outroSelo, map[string]any{"final_text": "daqui"}),
			want{status: StatusConflict, text: "do celular", selo: "daqui", textVersion: 4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cur := c.cur()
			out := DecidePush(mutation(c.base, rawJSON(t, c.patch)), cur, false, pc, testNow)
			r, n, w := out.Result, out.Next, c.want
			if r.Status != w.status || r.Merged != w.merged {
				t.Fatalf("resultado = %+v, quero %s mesclado=%v", r, w.status, w.merged)
			}
			if out.Write != WriteUpdate || n.Version != cur.Version+1 || r.EntityVersion != n.Version {
				t.Fatalf("gravação: write %v, versão %d (resultado %d), quero %d", out.Write, n.Version, r.EntityVersion, cur.Version+1)
			}
			if n.FinalText != w.text || (n.DeletedAt != nil) != w.trashed {
				t.Errorf("texto %q, lixeira %v; quero %q, %v", n.FinalText, n.DeletedAt, w.text, w.trashed)
			}
			switch {
			case w.selo == "" && n.Conflict != nil:
				t.Errorf("o selo ficou: %+v", n.Conflict)
			case w.selo != "" && (n.Conflict == nil || n.Conflict.FinalText != w.selo):
				t.Errorf("o selo: %+v; quero %q", n.Conflict, w.selo)
			}
			if n.TextVersion != w.textVersion || deref(n.TextDeviceID) != w.textDevice {
				t.Errorf("texto da versão %d por %q; quero %d por %q", n.TextVersion, deref(n.TextDeviceID), w.textVersion, w.textDevice)
			}
		})
	}

	// As recusas: a escolha com a base 0 (o ditado inteiro não a tem), a
	// escolha sem a data ou a data sem a escolha, uma escolha fora da lista,
	// que não é texto, ou uma data inválida. Nada é gravado.
	quando := seloEm.Format(time.RFC3339Nano)
	recusas := []struct {
		nome  string
		base  int64
		patch json.RawMessage
		code  string
	}{
		{"base 0", 0, createPayload(t, map[string]any{"resolver_conflito": "manter", "conflict_at": quando}), CodeUnknownField},
		{"sem a data", 3, rawJSON(t, map[string]any{"resolver_conflito": "manter"}), CodeInvalidField},
		{"só a data", 3, rawJSON(t, map[string]any{"conflict_at": quando}), CodeInvalidField},
		{"escolha fora da lista", 3, rawJSON(t, escolha("as duas", seloEm, nil)), CodeInvalidField},
		{"escolha vazia", 3, rawJSON(t, escolha("", seloEm, nil)), CodeInvalidField},
		{"escolha que não é texto", 3, rawJSON(t, map[string]any{"resolver_conflito": true, "conflict_at": quando}), CodeInvalidField},
		{"data inválida", 3, rawJSON(t, map[string]any{"resolver_conflito": "manter", "conflict_at": "ontem"}), CodeInvalidField},
		{"data nula", 3, rawJSON(t, map[string]any{"resolver_conflito": "manter", "conflict_at": nil}), CodeInvalidField},
	}
	for _, c := range recusas {
		out := DecidePush(mutation(c.base, c.patch), selado(), false, pc, testNow)
		if out.Result.Status != StatusRejected || out.Result.Code != c.code || out.Write != WriteNone {
			t.Errorf("%s: %+v (write %v); quero rejected %s sem gravar", c.nome, out.Result, out.Write, c.code)
		}
	}
}
