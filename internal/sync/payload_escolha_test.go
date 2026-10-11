package syncengine

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A escolha do selo no payload (ADR-019, "Decisões da F7c"): só com a versão
// da nuvem (mudar); o marcador só com a escolha manda só a escolha (sem o
// texto nem a lixeira); com campos, a escolha vai junto deles; criar nunca a
// leva.
func TestMutacaoDe_EscolhaDoSelo(t *testing.T) {
	seloEm := time.Date(2026, 10, 6, 10, 0, 0, 123456000, time.UTC)
	sv := int64(7)
	l := linhaDoPayload()
	l.ServerVersion = &sv

	so, err := MutacaoDe(storage.ItemDaFila{Marcador: storage.Marcador{SeloEscolha: storage.EscolhaTrocar, SeloEm: seloEm}, Linha: l}, "m")
	if err != nil {
		t.Fatal(err)
	}
	p, ks := chavesDe(t, so.Payload)
	if so.BaseVersion != 7 || strings.Join(ks, ",") != "conflict_at,resolver_conflito" ||
		p["resolver_conflito"] != "trocar" || p["conflict_at"] != "2026-10-06T10:00:00.123456Z" {
		t.Errorf("só a escolha: base %d, %v", so.BaseVersion, p)
	}

	junto, err := MutacaoDe(storage.ItemDaFila{Marcador: storage.Marcador{Campos: storage.CampoTexto,
		SeloEscolha: storage.EscolhaManter, SeloEm: seloEm}, Linha: l}, "m")
	if err != nil {
		t.Fatal(err)
	}
	p, ks = chavesDe(t, junto.Payload)
	if strings.Join(ks, ",") != "conflict_at,final_text,resolver_conflito" || p["final_text"] != "final" || p["resolver_conflito"] != "manter" {
		t.Errorf("a escolha com o texto: %v", p)
	}

	semNada, err := MutacaoDe(storage.ItemDaFila{Marcador: storage.Marcador{}, Linha: l}, "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, ks = chavesDe(t, semNada.Payload); strings.Join(ks, ",") != "deleted_at,final_text" {
		t.Errorf("o marcador sem campo nem escolha: %v", ks)
	}

	l.ServerVersion = nil
	criar, err := MutacaoDe(storage.ItemDaFila{Marcador: storage.Marcador{SeloEscolha: storage.EscolhaTrocar, SeloEm: seloEm}, Linha: l}, "m")
	if err != nil {
		t.Fatal(err)
	}
	if p, _ = chavesDe(t, criar.Payload); criar.BaseVersion != 0 || p["resolver_conflito"] != nil || p["conflict_at"] != nil {
		t.Errorf("criar levou a escolha: %v", p)
	}
}
