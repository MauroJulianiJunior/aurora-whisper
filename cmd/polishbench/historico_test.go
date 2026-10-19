package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// historicoDeTeste cria um historico.db temporário só com as colunas que o
// modo -historico lê (e as obrigatórias da tabela), com os ditados dados.
func historicoDeTeste(t *testing.T, ditados []linhaDeTeste) string {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE local_transcriptions (
		id TEXT PRIMARY KEY, user_id TEXT NOT NULL, source_platform TEXT NOT NULL,
		raw_text TEXT NOT NULL, final_text TEXT NOT NULL, mode TEXT NOT NULL,
		asr_provider TEXT NOT NULL, status TEXT NOT NULL,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, d := range ditados {
		var apagado any
		if d.apagado {
			apagado = "2026-10-16T10:00:00Z"
		}
		if _, err := db.Exec(`INSERT INTO local_transcriptions
			(id, user_id, source_platform, raw_text, final_text, mode, asr_provider, status, created_at, updated_at, deleted_at)
			VALUES (?, 'u', ?, ?, ?, ?, 'whisper-local', 'done', ?, ?, ?)`,
			d.id, d.origem, d.texto, d.texto, d.modo, d.criado, d.criado, apagado); err != nil {
			t.Fatal(err)
		}
	}
	return caminho
}

type linhaDeTeste struct {
	id, origem, texto, modo, criado string
	apagado                         bool
}

func TestLerDitadosLongos_SoSmartLongoNaoApagado(t *testing.T) {
	longo := strings.Repeat("palavra ", 80) // 640 caracteres
	caminho := historicoDeTeste(t, []linhaDeTeste{
		{"aaaaaaaa-1", "windows", longo, "smart", "2026-10-15T10:00:00Z", false},
		{"bbbbbbbb-2", "web", longo, "smart", "2026-10-16T10:00:00Z", false},
		{"cccccccc-3", "windows", "curto demais", "smart", "2026-10-16T11:00:00Z", false},
		{"dddddddd-4", "windows", longo, "polish", "2026-10-16T12:00:00Z", false},
		{"eeeeeeee-5", "windows", longo, "smart", "2026-10-16T13:00:00Z", true},
	})
	db, err := abrirHistorico(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ditados, err := lerDitadosLongos(context.Background(), db, 500, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range ditados {
		ids = append(ids, d.ID)
	}
	// Os dois smart longos e não apagados, o mais recente primeiro.
	if strings.Join(ids, ",") != "bbbbbbbb-2,aaaaaaaa-1" {
		t.Fatalf("ids = %v", ids)
	}
	casos := casosDoHistorico(ditados)
	if casos[0].ID != "h-bbbbbbbb" || casos[0].Categoria != CatHistorico || casos[0].Entrada != longo {
		t.Errorf("caso = %+v", casos[0])
	}
	// O limite vale.
	um, err := lerDitadosLongos(context.Background(), db, 500, 1)
	if err != nil || len(um) != 1 {
		t.Fatalf("com limite 1: %d ditados, erro %v", len(um), err)
	}
}

// O banco abre só para leitura: o comando nunca escreve no histórico.
func TestAbrirHistorico_SoLeitura(t *testing.T) {
	caminho := historicoDeTeste(t, nil)
	db, err := abrirHistorico(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM local_transcriptions`); err == nil {
		t.Fatal("o banco aberto pelo diagnóstico aceitou escrita")
	}
}

func TestCaminhoDoHistorico(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "LOCALAPPDATA" {
				return v
			}
			return ""
		}
	}
	got, err := caminhoDoHistorico("local", env(`C:\Users\x\AppData\Local`))
	if err != nil || got != filepath.Join(`C:\Users\x\AppData\Local`, "AuroraWhisper", "historico.db") {
		t.Errorf("local = %q, %v", got, err)
	}
	if _, err := caminhoDoHistorico("local", env("")); err == nil {
		t.Error("sem LOCALAPPDATA, o local tem de recusar")
	}
	if got, _ := caminhoDoHistorico("outro.db", env("")); got != "outro.db" {
		t.Errorf("caminho explícito = %q", got)
	}
}

// O comando inteiro, contra o provedor falso: o diagnóstico mostra a troca
// que o modelo fez, nada é gravado e a chave não aparece em lugar nenhum.
func TestRun_Historico_MostraOQueMudouENaoGrava(t *testing.T) {
	texto := "eu preciso mandar pra você o relatorio do mês até sexta-feira sem falta, combinado com a equipe"
	caminho := historicoDeTeste(t, []linhaDeTeste{
		{"12345678-abcd", "web", texto, "smart", "2026-10-12T14:32:00Z", false},
	})
	// O modelo troca "pra" por "para": a guarda de palavras recusa.
	troca := func(p pedidoFalso) respostaFalsa {
		return respostaFalsa{Final: strings.Replace(p.Ditado, "pra", "para", 1)}
	}
	a := novoAmbiente(t, troca, nil)
	cod := a.rodar("-gastar", "-modelos", "openai:gpt-6-luna", "-historico", caminho, "-min-caracteres", "50")
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	out := a.stdout.String()
	contem(t, out, "h-12345678")
	contem(t, out, "celular")
	contem(t, out, "rodada 1: palavras")
	contem(t, out, "[pra → para]")
	contem(t, out, DifContracao)
	contem(t, out, "nada foi gravado")
	if _, err := os.Stat(a.relatorio); err == nil {
		t.Error("o modo -historico gravou relatório")
	}
	if strings.Contains(a.tudoQueSaiu(), "CANARIO") {
		t.Error("a chave apareceu na saída")
	}
	if got := a.leitor.Prompts(); len(got) != 1 || got[0] != promptOpenAI {
		t.Errorf("chaves pedidas = %v, quero só a da OpenAI", got)
	}
}

// A resposta recusada pela guarda de siglas e termos mostra a sigla que não
// reapareceu, e o resumo conta o tipo.
func TestRun_Historico_MostraASiglaQueSumiu(t *testing.T) {
	texto := "eu preciso mandar o PGDAS do mês para o cliente até sexta-feira sem falta, combinado com a equipe"
	caminho := historicoDeTeste(t, []linhaDeTeste{
		{"12345678-abcd", "windows", texto, "smart", "2026-10-12T14:32:00Z", false},
	})
	minuscula := func(p pedidoFalso) respostaFalsa {
		return respostaFalsa{Final: strings.Replace(p.Ditado, "PGDAS", "pgdas", 1)}
	}
	a := novoAmbiente(t, minuscula, nil)
	cod := a.rodar("-gastar", "-modelos", "openai:gpt-6-luna", "-historico", caminho, "-min-caracteres", "50")
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	out := a.stdout.String()
	contem(t, out, "rodada 1: siglas_termos")
	contem(t, out, `sigla "PGDAS": 1 na entrada, 0 na saída; na saída, "pgdas" 1x`)
	contem(t, out, "o que a guarda de siglas e termos não achou na saída: sigla 1")
}

// O comando que o -help imprime roda como está escrito, com "-historico
// local" apontando para o LOCALAPPDATA (aqui, uma pasta temporária).
func TestComandoHistorico_RodaComAsFlagsImpressas(t *testing.T) {
	const prefixo = "go run ./cmd/polishbench "
	if !strings.HasPrefix(comandoHistorico, prefixo) || strings.Contains(comandoHistorico, "&&") {
		t.Fatalf("comandoHistorico = %q", comandoHistorico)
	}
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "AuroraWhisper"), 0o755); err != nil {
		t.Fatal(err)
	}
	origem := historicoDeTeste(t, []linhaDeTeste{
		{"12345678-abcd", "windows", strings.Repeat("ditado comprido ", 40), "smart", "2026-10-15T09:00:00Z", false},
	})
	dados, err := os.ReadFile(origem)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "AuroraWhisper", "historico.db"), dados, 0o644); err != nil {
		t.Fatal(err)
	}
	a := novoAmbiente(t, nil, nil)
	d := a.deps()
	d.Getenv = func(k string) string {
		if k == "LOCALAPPDATA" {
			return local
		}
		return ""
	}
	cod := run(a.ctx, strings.Fields(strings.TrimPrefix(comandoHistorico, prefixo)), &a.stdout, &a.stderr, d)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	contem(t, a.stdout.String(), "rodada 3: aceito")
	contem(t, a.stdout.String(), "nada foi gravado")
	var ajuda strings.Builder
	run(a.ctx, []string{"-help"}, &ajuda, &ajuda, d)
	contem(t, ajuda.String(), comandoHistorico)
}

// Sem ditado longo no histórico, nada é medido e nenhuma chave é pedida.
func TestRun_Historico_SemDitadoLongoNaoPedeChave(t *testing.T) {
	caminho := historicoDeTeste(t, []linhaDeTeste{
		{"12345678-abcd", "windows", "curto", "smart", "2026-10-12T14:32:00Z", false},
	})
	a := novoAmbiente(t, nil, nil)
	cod := a.rodar("-gastar", "-modelos", "openai:gpt-6-luna", "-historico", caminho)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	contem(t, a.stdout.String(), "nada a medir")
	if len(a.leitor.Prompts()) != 0 || len(a.openai.Pedidos()) != 0 {
		t.Error("pediu chave ou chamou o provedor sem ditado para medir")
	}
}

// Sem o arquivo, a mensagem diz onde procurou e lembra do modo portátil (o
// SQLite sozinho só diria "unable to open database file (14)"), e nenhuma
// chave é pedida.
func TestRun_Historico_ArquivoQueNaoExisteExplica(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	sumido := filepath.Join(t.TempDir(), "historico.db")
	cod := a.rodar("-gastar", "-modelos", "openai:gpt-6-luna", "-historico", sumido)
	if cod != 1 {
		t.Fatalf("saiu com %d, quero 1", cod)
	}
	contem(t, a.stderr.String(), "não achei o histórico em "+sumido)
	contem(t, a.stderr.String(), "modo portátil")
	if len(a.leitor.Prompts()) != 0 {
		t.Error("pediu a chave sem histórico")
	}
}

// Sem -gastar, o modo -historico recusa antes de abrir o banco.
func TestRun_Historico_SemGastarRecusaAntesDeAbrir(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	cod := a.rodar("-modelos", "openai:gpt-6-luna", "-historico", filepath.Join(t.TempDir(), "nao-existe.db"))
	if cod != 2 {
		t.Fatalf("saiu com %d, quero 2", cod)
	}
	contem(t, a.stderr.String(), "recusado")
}
