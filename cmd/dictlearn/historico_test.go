package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	_ "modernc.org/sqlite"
)

// inicioDosDitados é o instante do primeiro ditado de todo banco de
// teste; o i-ésimo é gravado i minutos depois.
var inicioDosDitados = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

// linhaDeHistorico é um ditado gravado por um dono.
type linhaDeHistorico struct {
	dono, bruto string
}

// bancoComLinhas cria um histórico de verdade, com as transcrições na
// ordem dada — a última do slice é a mais recente —, cada uma do seu
// dono. É o que o Agent produz quando o dono muda entre um ditado e
// outro (dicionário tirado, ou trocado).
func bancoComLinhas(t *testing.T, linhas ...linhaDeHistorico) string {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatalf("abrir: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := storage.NewSQLiteTranscriptionRepository(db)
	for i, l := range linhas {
		quando := inicioDosDitados.Add(time.Duration(i) * time.Minute)
		if err := repo.Put(storage.Transcription{
			ID: storage.NewID(), UserID: l.dono, SourcePlatform: "windows",
			RawText: l.bruto, FinalText: l.bruto, Mode: "smart", ASRProvider: "whisper-local",
			Status: "done", CreatedAt: quando, UpdatedAt: quando, Version: 1, SyncState: "pending",
		}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	return caminho
}

// comBanco abre o banco em caminho para escrita, e roda f nele — é o
// papel do Agent nestes testes, nunca o do dictlearn.
func comBanco(t *testing.T, caminho string, f func(*sql.DB)) {
	t.Helper()
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	f(db)
}

// bancoComDonoFixo é bancoComLinhas depois de o Agent desta versão subir
// sobre ele: todas as linhas migradas para o dono fixo, que é devolvido.
func bancoComDonoFixo(t *testing.T, linhas ...linhaDeHistorico) (string, string) {
	t.Helper()
	caminho := bancoComLinhas(t, linhas...)
	var dono string
	comBanco(t, caminho, func(db *sql.DB) {
		r, err := storage.GarantirDonoFixo(context.Background(), db,
			storage.CaminhoDaCopia(caminho, time.Now()))
		if err != nil {
			t.Fatalf("GarantirDonoFixo: %v", err)
		}
		dono = r.Dono
	})
	return caminho, dono
}

// bancoAntigo é bancoComLinhas como um Agent anterior ao dono fixo o
// deixava: sem a tabela instalacao, e sem a migration dela registrada.
func bancoAntigo(t *testing.T, linhas ...linhaDeHistorico) string {
	t.Helper()
	caminho := bancoComLinhas(t, linhas...)
	comBanco(t, caminho, func(db *sql.DB) {
		for _, q := range []string{
			`DROP TABLE instalacao`,
			`DELETE FROM schema_migrations WHERE version = '0002_instalacao'`,
		} {
			if _, err := db.Exec(q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	})
	return caminho
}

// paraLer abre o banco como o dictlearn abre: somente leitura.
func paraLer(t *testing.T, caminho string) *sql.DB {
	t.Helper()
	db, _, err := abrirHistorico(caminho)
	if err != nil {
		t.Fatalf("abrirHistorico: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// bancoComDitados é bancoComLinhas com um dono só.
func bancoComDitados(t *testing.T, usuario string, brutos ...string) string {
	t.Helper()
	linhas := make([]linhaDeHistorico, len(brutos))
	for i, b := range brutos {
		linhas[i] = linhaDeHistorico{dono: usuario, bruto: b}
	}
	return bancoComLinhas(t, linhas...)
}

// TestDoUltimo_PegaOBrutoDoDitadoMaisRecente é a razão da fatia inteira.
//
// Até 28-set-2026 o "saiu" só podia vir do que a pessoa digitava de
// memória, e memória não guarda a grafia exata de um erro visto por meio
// segundo. Mauro ensinou "Wissper" e o ASR produzia "Whisperflow" — o
// alias cobria uma forma que nunca mais ocorreria.
func TestDoUltimo_PegaOBrutoDoDitadoMaisRecente(t *testing.T) {
	banco := bancoComDitados(t, "mauro",
		"Precisa transmitir a DCTFWeb.",
		"Agora estamos fazendo um teste com Whisperflow aberto.")

	got, quando, err := ultimoBruto(paraLer(t, banco), "mauro")
	if err != nil {
		t.Fatalf("ultimoBruto: %v", err)
	}
	quero := "Agora estamos fazendo um teste com Whisperflow aberto."
	if got != quero {
		t.Errorf("ultimoBruto = %q, want %q", got, quero)
	}
	// O instante é o DESTE ditado, que é o que vai para a tela.
	if want := inicioDosDitados.Add(time.Minute); !quando.Equal(want) {
		t.Errorf("quando = %v, want %v", quando, want)
	}
}

// TestDoUltimo_EscopaPorUsuario: o histórico é escopado, e ler o ditado
// de outro usuário seria vazamento de conteúdo.
func TestDoUltimo_EscopaPorUsuario(t *testing.T) {
	banco := bancoComDitados(t, "mauro", "texto do mauro")
	if _, _, err := ultimoBruto(paraLer(t, banco), "outro"); err == nil {
		t.Error("ultimoBruto devolveu o ditado de outro usuário")
	}
}

// Revisão de 29-set-2026: o dono do histórico era "mauro" fixo, e o
// Agent gravava com o usuario do dicionário ou "local". Depois, no mesmo
// dia, o dono fixo da instalação (decisão do Mauro): o Agent grava com
// ele, e a regra antiga só vale num banco que ainda não o tem. Os testes
// abaixo travam a MESMA regra do Agent, e a data do ditado na tela.

// dicionarioDe grava um dicionário mínimo cujo dono é usuario.
func dicionarioDe(t *testing.T, usuario string) string {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	conteudo := `{"usuario": "` + usuario + `", "entradas": [{"canonica": "Supabase", "prioridade": 75}]}`
	if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
	return caminho
}

// TestRun_DoUltimo_DonoFixoVenceODicionario é o caso normal desde o dono
// fixo: o Agent migrou o banco, e o dicionário — de "ana" aqui — não
// decide mais de quem é o histórico. Todos os ditados, inclusive os
// antigos de "mauro", são do dono fixo, e o último vem.
func TestRun_DoUltimo_DonoFixoVenceODicionario(t *testing.T) {
	banco, dono := bancoComDonoFixo(t,
		linhaDeHistorico{dono: "ana", bruto: "ditado mais antigo"},
		linhaDeHistorico{dono: "mauro", bruto: "Supabaese está fora do ar"},
	)
	dict := dicionarioDe(t, "ana")

	cod, out, errOut := rodar(t, "n\n",
		"-do-ultimo", "-historico", banco, "-dict", dict, "-queria", "Supabase está fora do ar")
	if cod != 0 {
		t.Fatalf("código %d; stderr: %s", cod, errOut)
	}
	if !strings.Contains(out, `"Supabaese está fora do ar"`) {
		t.Errorf("não trouxe o último ditado do dono fixo:\n%s", out)
	}
	if !strings.Contains(out, dono) || !strings.Contains(out, "dono fixo") {
		t.Errorf("a saída não diz que o histórico é do dono fixo %s:\n%s", dono, out)
	}
	if strings.Contains(errOut, "aviso") {
		t.Errorf("avisou da regra antiga num banco com dono fixo:\n%s", errOut)
	}
}

// TestRun_DoUltimo_BancoAntigoAvisa: o banco de antes do dono fixo, sem a
// tabela, é lido pela regra antiga — o mesmo que o Agent gravava nele —, e
// o comando avisa, apontando o agent.log.
func TestRun_DoUltimo_BancoAntigoAvisa(t *testing.T) {
	banco := bancoAntigo(t,
		linhaDeHistorico{dono: "ana", bruto: "Supabaese está fora do ar"},
		linhaDeHistorico{dono: "mauro", bruto: "ditado de outro dono"},
	)
	dict := dicionarioDe(t, "ana")

	cod, out, errOut := rodar(t, "n\n",
		"-do-ultimo", "-historico", banco, "-dict", dict, "-queria", "Supabase está fora do ar")
	if cod != 0 {
		t.Fatalf("código %d; stderr: %s", cod, errOut)
	}
	if !strings.Contains(out, `"Supabaese está fora do ar"`) || strings.Contains(out, "ditado de outro dono") {
		t.Errorf("não leu pela regra antiga (o usuario do dicionário):\n%s", out)
	}
	for _, trecho := range []string{"aviso", "dono fixo", "regra antiga", "agent.log"} {
		if !strings.Contains(errOut, trecho) {
			t.Errorf("o aviso não contém %q:\n%s", trecho, errOut)
		}
	}
}

// TestRun_DoUltimo_DonoVemDoDicionario é a regra antiga, num banco que já
// tem a tabela do dono fixo mas ainda não o dono — a cópia falhou na
// subida do Agent, e ele gravou pela regra antiga. O dono do dicionário
// não é "mauro"; o ditado mais recente do banco é de "mauro", e é
// exatamente o que NÃO pode vir — o Agent, com este dicionário, gravou o
// de agora como "ana".
func TestRun_DoUltimo_DonoVemDoDicionario(t *testing.T) {
	banco := bancoComLinhas(t,
		linhaDeHistorico{dono: "ana", bruto: "Supabaese está fora do ar"},
		linhaDeHistorico{dono: "mauro", bruto: "ditado de outro dono"},
	)
	dict := dicionarioDe(t, "ana")

	cod, out, errOut := rodar(t, "n\n",
		"-do-ultimo", "-historico", banco, "-dict", dict, "-queria", "Supabase está fora do ar")
	if cod != 0 {
		t.Fatalf("código %d; stderr: %s", cod, errOut)
	}
	if !strings.Contains(out, `"Supabaese está fora do ar"`) {
		t.Errorf("não trouxe o ditado do dono do dicionário:\n%s", out)
	}
	if strings.Contains(out, "ditado de outro dono") {
		t.Errorf("trouxe o ditado de outro dono como se fosse o último:\n%s", out)
	}
	// A data que aparece é a do ditado devolvido — o primeiro do banco —,
	// não a do mais recente.
	if want := quandoLegivel(inicioDosDitados); !strings.Contains(out, want) {
		t.Errorf("a saída não mostra quando o ditado foi gravado (%s):\n%s", want, out)
	}
	if !strings.Contains(out, `"ana"`) {
		t.Errorf("a saída não diz de quem é o histórico lido:\n%s", out)
	}
	if !strings.Contains(errOut, "regra antiga") {
		t.Errorf("não avisou que leu pela regra antiga:\n%s", errOut)
	}
}

// TestRun_DoUltimo_UsuarioExplicitoSobrescreve: -usuario vence a regra.
// É o jeito de ler o histórico de um dono que o dicionário não diz.
func TestRun_DoUltimo_UsuarioExplicitoSobrescreve(t *testing.T) {
	banco := bancoComLinhas(t,
		linhaDeHistorico{dono: "local", bruto: "gravado sem dicionario"},
		linhaDeHistorico{dono: "ana", bruto: "gravado com dicionario"},
	)
	dict := dicionarioDe(t, "ana")

	_, out, errOut := rodar(t, "n\n",
		"-do-ultimo", "-historico", banco, "-dict", dict, "-usuario", "local", "-queria", "outra coisa")
	if !strings.Contains(out, "gravado sem dicionario") || strings.Contains(out, "gravado com dicionario") {
		t.Errorf("-usuario não sobrescreveu o dono do dicionário:\nstdout: %s\nstderr: %s", out, errOut)
	}
	if !strings.Contains(out, "-usuario") {
		t.Errorf("a saída não diz que o dono veio de -usuario:\n%s", out)
	}
}

// TestRun_DoUltimo_SemDonoNoBancoNomeiaODono: quando o dono não tem
// ditado nenhum, o erro diz qual dono foi usado, de onde ele veio, e
// como pedir outro.
func TestRun_DoUltimo_SemDonoNoBancoNomeiaODono(t *testing.T) {
	banco := bancoComDitados(t, "local", "gravado sem dicionario")
	dict := dicionarioDe(t, "ana")

	cod, _, errOut := rodar(t, "", "-do-ultimo", "-historico", banco, "-dict", dict, "-queria", "x")
	if cod == 0 {
		t.Fatal("aceitou um dono sem ditado nenhum")
	}
	for _, trecho := range []string{`"ana"`, dict, "-usuario"} {
		if !strings.Contains(errOut, trecho) {
			t.Errorf("o erro não contém %q: %s", trecho, errOut)
		}
	}
}

// TestDonoDoHistorico é a regra em si, caso a caso, contra a do Agent
// (cmd/agent/main.go, escolherDono: o dono fixo do banco; sem ele, a regra
// antiga — usuario do dicionário, "local" sem dicionário, dicionário
// inválido é erro).
func TestDonoDoHistorico(t *testing.T) {
	invalido := filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(invalido, []byte(`{"usuario": ""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ausente := filepath.Join(t.TempDir(), "nao-existe.json")

	// Três bancos: com dono fixo, com a tabela e sem o dono (a migração
	// não rodou), e anterior à tabela.
	comFixo, fixo := bancoComDonoFixo(t, linhaDeHistorico{dono: "mauro", bruto: "x"})
	semFixo := bancoComDitados(t, "mauro", "x")
	antigo := bancoAntigo(t, linhaDeHistorico{dono: "mauro", bruto: "x"})

	casos := []struct {
		nome      string
		banco     string
		explicito string
		dict      string
		want      string
		wantAviso bool
		wantErro  bool
	}{
		// O dono fixo vence o dicionário, e o dicionário nem é lido: um
		// inválido não atrapalha.
		{nome: "dono fixo vence o dicionário", banco: comFixo, dict: dicionarioDe(t, "ana"), want: fixo},
		{nome: "dono fixo sem dicionário", banco: comFixo, dict: ausente, want: fixo},
		{nome: "dono fixo com dicionário inválido", banco: comFixo, dict: invalido, want: fixo},
		{nome: "-usuario vence o dono fixo", banco: comFixo, explicito: "mauro", dict: ausente, want: "mauro"},

		// Sem dono fixo, a regra antiga, com aviso — nos dois bancos.
		{nome: "sem dono: usuario do dicionário", banco: semFixo, dict: dicionarioDe(t, "ana"), want: "ana", wantAviso: true},
		{nome: "banco antigo: usuario do dicionário", banco: antigo, dict: dicionarioDe(t, "ana"), want: "ana", wantAviso: true},
		// O cenário da revisão: sem dicionário o Agent gravava "local", e
		// ler "mauro" devolvia o ditado de ontem.
		{nome: "sem dono: sem dicionário é local", banco: semFixo, dict: ausente, want: donoSemDicionario, wantAviso: true},
		{nome: "banco antigo: sem dicionário é local", banco: antigo, dict: ausente, want: donoSemDicionario, wantAviso: true},
		{nome: "-usuario vence o dicionário", banco: semFixo, explicito: "mauro", dict: dicionarioDe(t, "ana"), want: "mauro"},
		{nome: "-usuario vence a falta de dicionário", banco: antigo, explicito: "mauro", dict: ausente, want: "mauro"},
		{nome: "-usuario em branco não conta", banco: semFixo, explicito: "  ", dict: dicionarioDe(t, "ana"), want: "ana", wantAviso: true},
		// Presente mas inválido: o Agent recusa subir com ele, e adivinhar
		// um dono aqui seria ler o histórico de outra pessoa.
		{nome: "sem dono: dicionário inválido é erro", banco: semFixo, dict: invalido, wantErro: true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			dono, origem, aviso, err := donoDoHistorico(context.Background(), c.explicito, c.dict, paraLer(t, c.banco), c.banco)
			if c.wantErro {
				if err == nil {
					t.Fatalf("dono = %q, want erro", dono)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro: %v", err)
			}
			if dono != c.want {
				t.Errorf("dono = %q, want %q", dono, c.want)
			}
			if origem == "" {
				t.Error("origem vazia: a tela não diria por que este dono")
			}
			if (aviso != "") != c.wantAviso {
				t.Errorf("aviso = %q, want aviso: %v", aviso, c.wantAviso)
			}
		})
	}
}

// TestAbrirHistorico_SomenteLeitura: o dictlearn lê o histórico e nunca
// escreve nele — nem migra o dono, que é trabalho do Agent.
func TestAbrirHistorico_SomenteLeitura(t *testing.T) {
	db := paraLer(t, bancoComDitados(t, "mauro", "x"))
	if _, err := db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('dono_local', 'intruso')`); err == nil {
		t.Error("o banco aberto pelo dictlearn aceitou escrita")
	}
}

// TestDoUltimo_BancoAusenteExplica: o erro precisa dizer o que fazer. O
// caso normal é o Agent ainda não ter rodado com esta versão.
func TestDoUltimo_BancoAusenteExplica(t *testing.T) {
	_, _, err := abrirHistorico(filepath.Join(t.TempDir(), "nao-existe.db"))
	if err == nil {
		t.Fatal("banco ausente não deu erro")
	}
	if !strings.Contains(err.Error(), "Agent") {
		t.Errorf("erro não diz o que fazer: %v", err)
	}
}

// TestRun_DoUltimoEhExclusivoComSaiu: aceitar os dois faria o comando
// escolher um em silêncio, e o ponto de -do-ultimo é justamente não
// depender do que se digita.
func TestRun_DoUltimoEhExclusivoComSaiu(t *testing.T) {
	banco := bancoComDitados(t, "mauro", "texto qualquer")
	var out, errBuf bytes.Buffer
	code := run([]string{
		"-do-ultimo", "-historico", banco, "-usuario", "mauro",
		"-saiu", "digitado a mao", "-queria", "outra coisa",
	}, strings.NewReader(""), &out, &errBuf)
	if code == 0 {
		t.Errorf("aceitou -do-ultimo com -saiu (saída %q)", out.String())
	}
	if !strings.Contains(errBuf.String(), "exclusiv") {
		t.Errorf("erro não explica a exclusividade: %q", errBuf.String())
	}
}
