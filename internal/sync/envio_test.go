package syncengine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O envio inicial do lado do motor e do laço (ADR-019, "Decisões da F7c"): a
// rodada completa o que ficou de fora depois do aceite, o histórico sobe em
// lotes por bytes atrás do ditado novo, e a bandeja diz "enviando N de M".

// historicoDeAntes grava, direto no banco, ditados de antes da conexão (sem
// marcador), com texto de tamanho kb KiB.
func historicoDeAntes(t *testing.T, p pcDeTeste, n, kb int) []string {
	t.Helper()
	agora := time.Now().UTC()
	texto := strings.Repeat("a", kb<<10)
	var ids []string
	for i := 0; i < n; i++ {
		id := storage.NewID()
		criado := agora.Add(-time.Duration(n-i) * time.Hour).Format("2006-01-02T15:04:05.000000000Z")
		if _, err := p.db.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at, version, sync_state)
			VALUES (?, ?, 'windows', ?, ?, 'smart', 'local', 'done', ?, ?, 1, 'pending')`,
			id, donoDoMotor, texto, texto, criado, criado); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// A rodada: sem o aceite, o histórico de antes não sobe; depois dele, sobe
// em lotes por bytes (12 ditados de 2 x 60 KiB: quatro por lote de 512 KiB), o
// ditado feito "desconectado" depois do aceite entra pela completação e sai
// no primeiro lote, na frente; o AoLote é chamado uma vez por lote, e a
// bandeja vê o progresso andar.
func TestMotor_RodadaCompletaOEnvioEmLotesPorBytes(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	historico := historicoDeAntes(t, p, 12, 60)
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
	m := novoMotor(p, n)
	m.NovoID = storage.NewID

	rel, err := m.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Enviados != 0 || rel.Completados != 0 {
		t.Fatalf("sem o aceite, subiu: %+v", rel)
	}

	if _, err := p.fila.EnviarHistorico(ctx, filepath.Join(t.TempDir(), "historico.db"), time.Now()); err != nil {
		t.Fatal(err)
	}
	// Um ditado de antes, que ficou de fora do clique (feito desconectado
	// depois do aceite).
	novo := historicoDeAntes(t, p, 1, 1)[0]
	var progresso []string
	m.AoLote = func() {
		f, tot, err := p.fila.Progresso(ctx)
		if err != nil {
			t.Error(err)
		}
		progresso = append(progresso, strings.Join([]string{itoa(f), itoa(tot)}, "/"))
	}
	n.pushes = nil
	rel, err = m.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Completados != 1 || rel.Enviados != 13 {
		t.Fatalf("a rodada: %+v", rel)
	}
	if len(n.pushes) != 3 {
		t.Fatalf("%d lotes; quero 3 (512 KiB cada)", len(n.pushes))
	}
	if n.pushes[0][0].EntityID != novo {
		t.Errorf("o ditado novo não saiu na frente: %s", n.pushes[0][0].EntityID)
	}
	var subiram []string
	for _, lote := range n.pushes {
		total := 0
		for _, mut := range lote {
			total += len(mut.Payload)
			subiram = append(subiram, mut.EntityID)
			var corpo map[string]any
			if err := json.Unmarshal(mut.Payload, &corpo); err != nil || mut.BaseVersion != 0 || corpo["raw_text"] == nil {
				t.Errorf("a mutação do histórico não é de criar: base %d, %v", mut.BaseVersion, err)
			}
		}
		if total > MaxBytesPorLote {
			t.Errorf("um lote com %d bytes de payload", total)
		}
	}
	if strings.Join(subiram[1:], ",") != strings.Join(historico, ",") {
		t.Errorf("o histórico não subiu inteiro, do mais antigo ao mais novo")
	}
	if strings.Join(progresso, " ") != "4/12 8/12 0/0" {
		t.Errorf("o progresso por lote: %v", progresso)
	}
	if c, _ := p.fila.Contar(ctx); c != 0 {
		t.Errorf("a fila depois: %d", c)
	}
}

func itoa(n int) string { return milhar(int64(n)) }

// A linha da bandeja com o envio inicial: "enviando N de M" também no 429;
// sem conexão, desconectada e a resposta inesperada vencem; no fim, volta a
// "em dia".
func TestLaco_BandejaComOEnvioInicial(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	historicoDeAntes(t, p, 3, 1)
	if _, err := p.fila.EnviarHistorico(ctx, filepath.Join(t.TempDir(), "historico.db"), time.Now()); err != nil {
		t.Fatal(err)
	}
	l := NovoLaco(ConfigDoLaco{Fila: p.fila, Conexao: conectada(), NovoID: storage.NewID})
	l.contarPendentes(ctx)
	casos := []struct {
		situacao int32
		quero    string
	}{
		{situacaoNenhuma, "Nuvem: enviando 0 de 3"},
		{situacaoEmDia, "Nuvem: enviando 0 de 3"},
		{situacaoAguardando, "Nuvem: enviando 0 de 3"},
		{situacaoSemConexao, "Nuvem: sem conexão"},
		{situacaoDesconectada, "Nuvem: desconectada"},
		{situacaoRespostaInesperada, "Nuvem: resposta inesperada"},
	}
	for _, c := range casos {
		l.situacao.Store(c.situacao)
		if got := l.Resumo(); got != c.quero {
			t.Errorf("situação %d: %q; quero %q", c.situacao, got, c.quero)
		}
	}
	if _, err := p.db.Exec(`UPDATE local_transcriptions SET server_version = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.Exec(`DELETE FROM sync_outbox WHERE entity_id IN (SELECT entity_id FROM sync_outbox LIMIT 2)`); err != nil {
		t.Fatal(err)
	}
	l.contarPendentes(ctx)
	l.situacao.Store(situacaoEmDia)
	if got := l.Resumo(); got != "Nuvem: enviando 2 de 3" {
		t.Errorf("no meio: %q", got)
	}
	if _, err := p.db.Exec(`DELETE FROM sync_outbox`); err != nil {
		t.Fatal(err)
	}
	l.contarPendentes(ctx)
	if got := l.Resumo(); got != "Nuvem: em dia" {
		t.Errorf("no fim: %q", got)
	}
	l.situacao.Store(situacaoAguardando)
	if got := l.Resumo(); got != "Nuvem: aguardando" {
		t.Errorf("o 429 sem envio: %q", got)
	}
}

// A reconciliação na rodada (D3): o pull traz um ditado que este PC tem sem a
// versão da nuvem, com outro texto; ele não é sobrescrito, e sobe inteiro
// (base 0) numa volta a mais da MESMA rodada, com o texto daqui. O ditado é
// de outro PC (device_id), para a completação do envio inicial não o marcar
// antes do pull: o caso aqui é só o do pull.
func TestMotor_ReconciliaESobeNaMesmaRodada(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	id := historicoDeAntes(t, p, 1, 1)[0]
	if _, err := p.db.Exec(`UPDATE local_transcriptions SET final_text = 'o texto daqui', device_id = 'outro-pc' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	criado := time.Now().UTC().Add(-time.Hour)
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(after int64) (ChangePage, error) {
		if after > 0 {
			return ChangePage{LastSeq: after, Epoch: "e1"}, nil
		}
		return ChangePage{LastSeq: 5, Epoch: "e1", Changes: []Change{{
			Seq: 5, EntityType: storage.EntidadeDitado, EntityID: id, Operation: OpUpsert, EntityVersion: 3,
			Payload: &Item{ID: id, SourcePlatform: "windows", RawText: "r", FinalText: "o texto da nuvem", Mode: "smart",
				ASRProvider: "local", Status: "done", CreatedAt: criado, UpdatedAt: criado, Version: 3},
		}}}, nil
	}}
	m := novoMotor(p, n)
	m.NovoID = storage.NewID
	rel, err := m.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Pull[TipoReconciliado] != 1 || len(n.pushes) != 1 || len(n.pushes[0]) != 1 {
		t.Fatalf("a rodada: %+v, pushes %v", rel, n.pushes)
	}
	mut := n.pushes[0][0]
	if mut.EntityID != id || mut.BaseVersion != 0 || !strings.Contains(string(mut.Payload), "o texto daqui") {
		t.Errorf("a mutação da reconciliação: %+v %s", mut, mut.Payload)
	}
	l, err := p.repo.Get(ctx, donoDoMotor, id)
	if err != nil || l.FinalText != "o texto daqui" {
		t.Errorf("o texto daqui foi sobrescrito: %q, %v", l.FinalText, err)
	}
	if got := strings.Join(n.pedidos, " "); got != "pull push pull" {
		t.Errorf("os pedidos: %s", got)
	}
}

func TestMilhar(t *testing.T) {
	for n, quero := range map[int64]string{0: "0", 999: "999", 1000: "1.000", 7000: "7.000", 12345: "12.345", 1234567: "1.234.567"} {
		if got := milhar(n); got != quero {
			t.Errorf("milhar(%d) = %q; quero %q", n, got, quero)
		}
	}
}
