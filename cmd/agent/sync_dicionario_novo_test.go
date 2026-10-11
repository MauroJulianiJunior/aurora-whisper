package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// O dicionario.json que, num reinício, não é mais o que o espelho do sync
// conhece (a revisão da F8g): o recriado vazio pelo garantirDicionario (o
// arquivo sumiu entre duas execuções) e o de outro usuário. Tudo em
// t.TempDir(), com o runtime falso e a nuvem de roteiro: nada sai do teste.

// nuvemQueAnota é a nuvem de uma conta que aceita tudo: anota o que subiu, e
// o pull vem vazio.
type nuvemQueAnota struct{ muts []syncengine.Mutation }

func (n *nuvemQueAnota) Push(_ context.Context, muts []syncengine.Mutation) (syncengine.PushResult, error) {
	n.muts = append(n.muts, muts...)
	r := syncengine.PushResult{Epoch: "epoch-1"}
	for _, m := range muts {
		r.Results = append(r.Results, syncengine.MutationResult{MutationID: m.MutationID, Status: syncengine.StatusApplied, EntityVersion: 9})
	}
	return r, nil
}

func (n *nuvemQueAnota) Pull(_ context.Context, after int64, _ int) (syncengine.ChangePage, error) {
	return syncengine.ChangePage{LastSeq: after, Epoch: "epoch-1"}, nil
}

// subidaDoDicionario faz o que run() faz com o dicionário no caminho: cria se
// faltar, abre, e monta o aprendizado com o usuario que saiu do arquivo.
func subidaDoDicionario(t *testing.T, caminho string) (m montagem, criou bool, usuario string) {
	t.Helper()
	log, buf := logDeTeste()
	criou = garantirDicionario(log, caminho)
	smart, _, usuario, _, err := abrirDicionarioEm(log, caminho)
	if err != nil {
		t.Fatalf("abrirDicionarioEm: %v", err)
	}
	polish := novoPolishDoAgent(smart, chaves.NovaMemoria(), &ia.Cliente{}, log)
	rt := &trocadorFalso{}
	return montagem{a: novoAprendizado(caminho, usuario, rt, polish, log), rt: rt, caminho: caminho, log: buf}, criou, usuario
}

// paresDaExecucaoAnterior põe no espelho os pares que uma execução anterior
// deixou sincronizados (a base da nuvem, sem mudança daqui), e o usuario do
// dicionário dela, anotado; e o aceite do "Enviar o histórico".
func paresDaExecucaoAnterior(t *testing.T, fila *storage.Fila, inst *storage.Instalacao, usuario string, pares map[string]string) []string {
	t.Helper()
	ctx := context.Background()
	// O pareamento da execução anterior é o mesmo da rodada do teste
	// (conexaoConectada), e ela já falou com a nuvem (o epoch): a rodada do
	// teste continua do cursor, com as versões.
	cfg := configDoSync(fila, nil, nil, slog.New(slog.DiscardHandler))
	cfg.Conexao = conexaoConectada{}
	cfg.Cliente = func(string, string) syncengine.SyncClient { return &nuvemQueAnota{} }
	if err := syncengine.NovoLaco(cfg).RodadaAgora(ctx); err != nil {
		t.Fatal(err)
	}
	var ids []string
	proj := map[string]*dictionary.ParDoPainel{}
	for variante, termo := range pares {
		id := dictionary.IDDoPar(variante)
		ids = append(ids, id)
		proj[id] = &dictionary.ParDoPainel{ID: id, Variante: variante, Termo: termo}
	}
	sort.Strings(ids)
	if err := fila.MudarAprendido(ctx, time.Now(), ids, func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4}, Projecao: &storage.NovaProjecao{Par: proj[e.ID]},
			VeioDaNuvem: storage.VeioEntra}
	}); err != nil {
		t.Fatal(err)
	}
	if err := inst.Gravar(ctx, map[string]string{storage.ChaveNuvemDicionario: usuario,
		storage.ChaveNuvemEnvioAceitoEm: "2026-10-07T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	return ids
}

// No reinício, o arquivo novo (o recriado vazio, ou o de outro usuário) não
// vale como "a pessoa desfez tudo": nenhum delete sobe, e os pares da nuvem
// voltam ao arquivo pela primeira rodada. O mesmo arquivo de antes, com os
// pares tirados à mão, segue a regra de sempre (a remoção à mão propaga como
// desfazer, com o freio). E o controle: o recriado SEM a conferência manda o
// delete de cada par (era o defeito).
func TestSync_ODicionarioNovoNaoViraDesfazer(t *testing.T) {
	pares := map[string]string{"esse efe": "ECF", "ecedê": "ECD"}
	for _, c := range []struct {
		nome     string
		antes    string // o usuario do dicionário da execução anterior
		arquivo  string // o dicionario.json no reinício ("" é nenhum)
		conferir bool
		criou    bool
		usuario  string
		deletes  int
		voltaram bool
		anotacao string
	}{
		{nome: "recriado vazio", antes: "mauro", conferir: true, criou: true, usuario: usuarioDoDicionarioNovo,
			voltaram: true, anotacao: usuarioDoDicionarioNovo},
		// O dicionário que o próprio Agent tinha criado ("local"), apagado e
		// recriado: o usuario é o mesmo, e só o criou diz que é outro arquivo.
		{nome: "recriado vazio, do mesmo usuário", antes: usuarioDoDicionarioNovo, conferir: true, criou: true,
			usuario: usuarioDoDicionarioNovo, voltaram: true, anotacao: usuarioDoDicionarioNovo},
		{nome: "de outro usuário", antes: "mauro", arquivo: `{"usuario": "outro"}`, conferir: true, usuario: "outro",
			voltaram: true, anotacao: "outro"},
		{nome: "o mesmo arquivo, sem os pares (à mão)", antes: "mauro", arquivo: `{"usuario": "mauro"}`, conferir: true,
			usuario: "mauro", deletes: 2, anotacao: "mauro"},
		{nome: "o controle: recriado, sem a conferência", antes: "mauro", criou: true, usuario: usuarioDoDicionarioNovo,
			deletes: 2, anotacao: "mauro"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			ctx := context.Background()
			hist, prefs, dono := historicoComPrefs(t)
			fila := hist.Fila(dono)
			paresDaExecucaoAnterior(t, fila, prefs.Instalacao(), c.antes, pares)
			caminho := filepath.Join(t.TempDir(), nomeDicionario)
			if c.arquivo != "" {
				if err := os.WriteFile(caminho, []byte(c.arquivo), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m, criou, usuario := subidaDoDicionario(t, caminho)
			if criou != c.criou || usuario != c.usuario {
				t.Fatalf("a subida: criou %v, usuario %q", criou, usuario)
			}
			if c.conferir {
				log, buf := logDeTeste()
				if !prepararOAprendido(ctx, fila, prefs.Instalacao(), usuario, criou, time.Now(), log) {
					t.Fatalf("a conferência falhou:\n%s", buf)
				}
				for _, v := range []string{"esse efe", "ECF", "ecedê", "ECD"} {
					if strings.Contains(buf.String(), v) {
						t.Errorf("o log traz %q", v)
					}
				}
			}
			nv := &nuvemQueAnota{}
			rodadaPelaConfigDoAgent(t, hist, dono, m.a, nv)

			deletes := 0
			for _, mu := range nv.muts {
				if mu.EntityType == storage.EntidadeAprendido && mu.Operation == syncengine.OpDelete {
					deletes++
				}
			}
			if deletes != c.deletes {
				t.Errorf("subiram %d delete(s) do aprendido; quero %d (%+v)", deletes, c.deletes, nv.muts)
			}
			lista, err := m.a.Aprendidas(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if voltaram := len(lista) == len(pares); voltaram != c.voltaram {
				t.Errorf("os pares da nuvem no arquivo novo: %+v; quero que voltem: %v", lista, c.voltaram)
			}
			if v, _, err := prefs.Instalacao().Ler(ctx, storage.ChaveNuvemDicionario); err != nil || v != c.anotacao {
				t.Errorf("a anotação do dicionário: %q, %v; quero %q", v, err, c.anotacao)
			}
		})
	}
}

// A conferência na primeira subida (sem anotação): o arquivo é o que o
// espelho conhece, e só é anotado; o espelho não muda. A anotação vazia (a
// subida que caiu entre marcar e anotar) é refeita na seguinte. E sem
// dicionário (usuario vazio) não há o que conferir.
func TestSync_PrepararOAprendidoAnotaERefaz(t *testing.T) {
	ctx := context.Background()
	hist, prefs, dono := historicoComPrefs(t)
	fila := hist.Fila(dono)
	inst := prefs.Instalacao()
	log := slog.New(slog.DiscardHandler)
	ids := paresDaExecucaoAnterior(t, fila, inst, "mauro", map[string]string{"esse efe": "ECF"})
	if err := inst.Apagar(ctx, storage.ChaveNuvemDicionario); err != nil {
		t.Fatal(err)
	}
	projecao := func() *dictionary.ParDoPainel {
		t.Helper()
		es, err := fila.Espelho(ctx)
		if err != nil || len(es) != 1 || es[0].ID != ids[0] {
			t.Fatalf("o espelho: %+v, %v", es, err)
		}
		return es[0].Projecao
	}

	if !prepararOAprendido(ctx, fila, inst, "mauro", false, time.Now(), log) || projecao() == nil {
		t.Fatal("a primeira subida mexeu no espelho")
	}
	if v, ok, _ := inst.Ler(ctx, storage.ChaveNuvemDicionario); !ok || v != "mauro" {
		t.Fatalf("a primeira subida não anotou: %q %v", v, ok)
	}
	if !prepararOAprendido(ctx, fila, inst, "", true, time.Now(), log) || projecao() == nil {
		t.Fatal("sem dicionário, a conferência mexeu no espelho")
	}

	// A subida que caiu no meio deixou a anotação vazia: a seguinte solta o
	// espelho, mesmo sem ter criado o arquivo.
	if err := inst.Gravar(ctx, map[string]string{storage.ChaveNuvemDicionario: ""}); err != nil {
		t.Fatal(err)
	}
	if !prepararOAprendido(ctx, fila, inst, "mauro", false, time.Now(), log) || projecao() != nil {
		t.Fatal("a subida seguinte não refez")
	}
	if v, _, _ := inst.Ler(ctx, storage.ChaveNuvemDicionario); v != "mauro" {
		t.Errorf("a anotação depois de refazer: %q", v)
	}
}

// Sem conseguir conferir (o banco que não responde), o laço desta execução vê
// o dicionário como indisponível: nada vira delete, e o M fica desconhecido.
func TestSync_SemConferirOLacoVeOArquivoIndisponivel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&logTravado{}, nil))
	hist, prefs, dono, fechar, err := abrirHistoricoEm(logger, filepath.Join(t.TempDir(), "historico.db"), "", clock.NewSystem())
	if err != nil {
		t.Fatal(err)
	}
	fila, inst := hist.Fila(dono), prefs.Instalacao()
	fechar()
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	if prepararOAprendido(context.Background(), fila, inst, "mauro", true, time.Now(), slog.New(slog.DiscardHandler)) {
		t.Fatal("a conferência passou com o banco fechado")
	}
	m.a.semConferir = true
	cfg := configDoSync(fila, nil, m.a, nil)
	if _, _, err := cfg.Dicionario.AplicarPares(nil, nil); err != dictionary.ErrSemArquivo {
		t.Errorf("o Dicionario do laço sem a conferência: %T, %v; quero o arquivo indisponível", cfg.Dicionario, err)
	}
}

// run() não roda em teste, então a ligação é conferida no código de main.go:
// o que o garantirDicionario devolve (criou) vai ao prepararOAprendido, que
// roda uma vez, antes do montarSync, e o falso dele põe o aprendido de lado no
// laço (semConferir).
func TestRun_PrepararOAprendidoAntesDoSync(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var criou string
	pos := map[string][]token.Pos{}
	marcaSemConferir := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Rhs) == 1 {
				if c, ok := n.Rhs[0].(*ast.CallExpr); ok && types.ExprString(c.Fun) == "garantirDicionario" && len(n.Lhs) == 1 {
					criou = types.ExprString(n.Lhs[0])
				}
			}
			if len(n.Lhs) == 1 && types.ExprString(n.Lhs[0]) == "aprendizado.semConferir" {
				marcaSemConferir = true
			}
		case *ast.CallExpr:
			nome := types.ExprString(n.Fun)
			if nome == "prepararOAprendido" || nome == "montarSync" {
				pos[nome] = append(pos[nome], n.Pos())
				if nome == "prepararOAprendido" && (len(n.Args) < 5 || criou == "" || types.ExprString(n.Args[4]) != criou ||
					types.ExprString(n.Args[3]) != "usuario") {
					t.Errorf("prepararOAprendido sem o usuario e o criou do garantirDicionario: %s", fset.Position(n.Pos()))
				}
			}
		}
		return true
	})
	for _, nome := range []string{"prepararOAprendido", "montarSync"} {
		if len(pos[nome]) != 1 {
			t.Fatalf("main.go chama %s %d vez(es); espero exatamente 1", nome, len(pos[nome]))
		}
	}
	if pos["prepararOAprendido"][0] >= pos["montarSync"][0] {
		t.Error("main.go monta o laço ANTES de conferir o dicionário")
	}
	if !marcaSemConferir {
		t.Error("main.go não põe o aprendido de lado quando a conferência falha")
	}
}
