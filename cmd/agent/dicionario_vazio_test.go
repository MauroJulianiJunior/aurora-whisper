package main

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O dicionário vazio que o Agent cria na subida (ADR-018, "O dicionário que não
// existe"). Tudo roda em t.TempDir(), com o runtime falso de aprendizado_test.go:
// nenhum teste toca o dicionario.json de quem usa.

func logDeTeste() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestGarantirDicionario_CriaOVazioQuandoFalta(t *testing.T) {
	pasta := t.TempDir()
	caminho := filepath.Join(pasta, nomeDicionario)
	log, buf := logDeTeste()

	garantirDicionario(log, caminho)

	dados, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatalf("o dicionário não foi criado: %v", err)
	}
	s, err := dictionary.CarregarSemente(bytes.NewReader(dados))
	if err != nil || s.Usuario != usuarioDoDicionarioNovo || len(s.Entradas) != 0 {
		t.Fatalf("o dicionário criado: %+v, %v", s, err)
	}
	saida := buf.String()
	if !strings.Contains(saida, "criei um dicionário vazio") || !strings.Contains(saida, "level=INFO") {
		t.Errorf("o log não diz que criou:\n%s", saida)
	}
	// Só o nome do arquivo, nunca o caminho (leva o nome de usuário do Windows).
	if strings.Contains(saida, pasta) || strings.Contains(saida, filepath.ToSlash(pasta)) {
		t.Errorf("o log traz o caminho:\n%s", saida)
	}
}

func TestGarantirDicionario_NaoMexeNoQueJaExiste(t *testing.T) {
	casos := map[string]string{
		"o de quem usa": `{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`,
		"inválido":      `{`,
		"vazio":         ``,
	}
	for nome, conteudo := range casos {
		t.Run(nome, func(t *testing.T) {
			caminho := filepath.Join(t.TempDir(), nomeDicionario)
			if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
				t.Fatal(err)
			}
			log, buf := logDeTeste()
			garantirDicionario(log, caminho)

			if dados, _ := os.ReadFile(caminho); string(dados) != conteudo {
				t.Errorf("o arquivo existente foi alterado: %q", dados)
			}
			// Nada a dizer: não criou, não falhou.
			if buf.Len() != 0 {
				t.Errorf("o log diz algo quando não havia o que fazer:\n%s", buf.String())
			}
		})
	}
}

// Subir duas vezes: a segunda não apaga o que a pessoa aprendeu na primeira.
func TestGarantirDicionario_SegundaSubidaPreservaOAprendido(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	log, _ := logDeTeste()
	garantirDicionario(log, caminho)

	arq := &dictionary.Arquivo{Caminho: caminho, Usuario: usuarioDoDicionarioNovo}
	if _, _, err := arq.Aprender("ECF", "sf"); err != nil {
		t.Fatal(err)
	}
	antes, _ := os.ReadFile(caminho)

	garantirDicionario(log, caminho)
	if depois, _ := os.ReadFile(caminho); !bytes.Equal(antes, depois) {
		t.Errorf("a segunda subida mexeu no dicionário:\n%s", depois)
	}
}

// Pasta sem permissão, disco cheio, pasta que sumiu: o Agent segue como
// seguia sem dicionário, e o aviso diz o que isso custa.
func TestGarantirDicionario_FalhaNaoImpedeASubida(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "nao-existe", nomeDicionario)
	log, buf := logDeTeste()

	garantirDicionario(log, caminho) // não pode entrar em pânico

	saida := buf.String()
	if !strings.Contains(saida, "level=WARN") || !strings.Contains(saida, "aprender do painel fica indisponível") {
		t.Errorf("o aviso não diz o que a falha custa:\n%s", saida)
	}
	if _, err := os.Stat(caminho); err == nil {
		t.Error("criou o arquivo numa pasta que não existe")
	}
	// E abrir o dicionário continua como antes: sem arquivo, o Smart nulo.
	smart, _, usuario, _, err := abrirDicionarioEm(slog.New(slog.DiscardHandler), caminho)
	if err != nil || smart == nil || smart.Dictionary != nil || usuario != "" {
		t.Fatalf("depois da falha: smart %v, usuário %q, err %v", smart, usuario, err)
	}
}

// O dicionário criado abre como qualquer outro: o usuario sai dele, o Smart
// tem repositório, e o log da subida não reclama de nada.
func TestDicionarioCriado_AbreComoQualquerDicionario(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	garantirDicionario(slog.New(slog.DiscardHandler), caminho)

	log, buf := logDeTeste()
	smart, vocab, usuario, dominio, err := abrirDicionarioEm(log, caminho)
	if err != nil {
		t.Fatalf("abrirDicionarioEm: %v", err)
	}
	if smart == nil || smart.Dictionary == nil {
		t.Fatalf("o Smart do dicionário criado não tem repositório: %+v", smart)
	}
	// Sem domínio nenhum no arquivo, nenhum aplicativo tem domínio (e a função
	// existe, como em qualquer dicionário carregado).
	if usuario != usuarioDoDicionarioNovo || vocab != "" || dominio == nil || dominio("Code.exe") != nil {
		t.Errorf("usuário %q, vocabulário %q, função de domínio nula: %v", usuario, vocab, dominio == nil)
	}
	saida := buf.String()
	if !strings.Contains(saida, "dicionario: carregado") || !strings.Contains(saida, "termos=0") {
		t.Errorf("o log da subida:\n%s", saida)
	}
	// Sem termo nenhum, "nenhum termo coube no prompt" seria mentira.
	if strings.Contains(saida, "NENHUM termo coube") || strings.Contains(saida, "level=WARN") {
		t.Errorf("o log reclama de um dicionário vazio:\n%s", saida)
	}
}

// O texto do ditado sai igual com o dicionário criado e sem dicionário nenhum:
// o que muda para quem não tinha dicionário é só que o aprender passa a existir.
func TestDicionarioCriado_NaoMudaOTextoDoDitado(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	garantirDicionario(slog.New(slog.DiscardHandler), caminho)
	vazio, _, usuario, _, err := abrirDicionarioEm(slog.New(slog.DiscardHandler), caminho)
	if err != nil {
		t.Fatal(err)
	}
	semDicionario := resolver.NewSmartResolver(nil)

	textos := []string{
		"manda  para o João",
		"vou mandar o sf amanhã",
		"o ecf e a dctf web de hoje",
		"quero dois, não, três ingressos",
		"transmitir a DCTF Web hoje",
		"",
	}
	pedidos := []resolver.ResolveRequest{}
	for _, tx := range textos {
		pedidos = append(pedidos, resolver.ResolveRequest{ASRResult: asr.ASRResult{Text: tx}})
		// Com confiança baixa e alternativa do N-best: os sinais de incerteza
		// também não podem trocar texto sem termo no dicionário.
		pedidos = append(pedidos, resolver.ResolveRequest{ASRResult: asr.ASRResult{
			Text:         tx,
			Confidence:   asr.Confidence{Present: true, Value: 0.1},
			Alternatives: []asr.Alternative{{Text: tx + " x"}, {Text: tx}},
		}})
	}
	for _, p := range pedidos {
		quero, err := semDicionario.Resolve(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		p.UserID = usuario
		got, err := vazio.Resolve(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		if got.FinalText != quero.FinalText {
			t.Errorf("%q: com o dicionário vazio saiu %q, sem dicionário saía %q", p.ASRResult.Text, got.FinalText, quero.FinalText)
		}
		if len(got.AppliedReplacements) != len(quero.AppliedReplacements) {
			t.Errorf("%q: %d troca(s) com o dicionário vazio, %d sem dicionário", p.ASRResult.Text,
				len(got.AppliedReplacements), len(quero.AppliedReplacements))
		}
	}
}

// montarDoDicionarioCriado faz o que run() faz com o dicionário, na mesma
// ordem: cria se faltar, abre, e monta o aprendizado com o usuario que saiu do
// arquivo.
func montarDoDicionarioCriado(t *testing.T) montagem {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	log, buf := logDeTeste()
	garantirDicionario(log, caminho)
	smart, _, usuario, _, err := abrirDicionarioEm(log, caminho)
	if err != nil {
		t.Fatalf("abrirDicionarioEm: %v", err)
	}
	polish := novoPolishDoAgent(smart, chaves.NovaMemoria(), &ia.Cliente{}, log)
	rt := &trocadorFalso{}
	return montagem{a: novoAprendizado(caminho, usuario, rt, polish, log), rt: rt, caminho: caminho, log: buf}
}

// O ponto de tudo: num Agent que subiu sem dicionário, o aprender funciona no
// primeiro dia, vale no ditado seguinte, e desfazer devolve o arquivo criado.
func TestDicionarioCriado_AprendeNoPrimeiroDia(t *testing.T) {
	m := montarDoDicionarioCriado(t)
	original := m.bytes(t)

	ap, err := m.a.Aprender(context.Background(), "ECF", "sf")
	if err != nil {
		t.Fatalf("Aprender no dicionário criado: %v", err)
	}
	if ap != (dictionary.Aprendida{Termo: "ECF", Variante: "sf"}) {
		t.Errorf("Aprendida = %+v", ap)
	}
	if m.rt.quantos() != 1 {
		t.Fatalf("o runtime recebeu %d dicionários, quero 1", m.rt.quantos())
	}
	// O runtime busca pelo usuario do dicionário criado (UsarResolver).
	out, err := m.rt.ultimo(t).Resolver.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:    usuarioDoDicionarioNovo,
		ASRResult: asr.ASRResult{Text: "vou mandar o sf amanhã"},
	})
	if err != nil || out.FinalText != "vou mandar o ECF amanhã" {
		t.Errorf("o próximo ditado saiu %q, %v; quero com ECF", out.FinalText, err)
	}
	if lista, err := m.a.Aprendidas(context.Background()); err != nil || len(lista) != 1 {
		t.Errorf("Aprendidas = %+v, %v", lista, err)
	}
	if !strings.Contains(string(m.bytes(t)), `"sf": "painel"`) {
		t.Errorf("o arquivo não guarda o aprendido:\n%s", m.bytes(t))
	}

	if _, err := m.a.Desfazer(context.Background(), "sf"); err != nil {
		t.Fatalf("Desfazer: %v", err)
	}
	if !bytes.Equal(m.bytes(t), original) {
		t.Errorf("desfazer não devolveu o dicionário criado:\n%s", m.bytes(t))
	}
}

// Pelo caminho do painel, de ponta a ponta: salvar uma edição oferece o
// aviso "Aprender" (antes, num Agent sem dicionário, a lista de sugestões vinha
// vazia), o clique grava, e a lista das Configurações o mostra.
func TestAPIDoPainel_ComODicionarioCriado(t *testing.T) {
	m := montarDoDicionarioCriado(t)
	repo := storage.NewInMemoryTranscriptionRepository()
	now := time.Now().UTC()
	repo.Put(storage.Transcription{
		ID: "t1", UserID: "dono", SourcePlatform: "windows", Mode: "smart", ASRProvider: "local",
		RawText: "vou mandar o sf amanhã", FinalText: "Vou mandar o sf amanhã.",
		Status: "completed", CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	srv := localapi.New("dono", repo, storage.NewInMemorySettingsRepository(), localapi.Preferencias{
		Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{}, Aprendizado: m.a,
	})
	h := srv.Handler()

	w := chamar(t, h, http.MethodPatch, "/api/history/t1",
		map[string]any{"base_version": 1, "final_text": "Vou mandar o ECF amanhã."})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"sugestoes":[]`) || !strings.Contains(w.Body.String(), `"de":"sf"`) {
		t.Fatalf("salvar a edição não ofereceu o Aprender: %s", w.Body.String())
	}

	w = chamar(t, h, http.MethodPost, "/api/aprendizado", map[string]string{"id": "t1", "de": "sf", "para": "ECF"})
	if w.Code != http.StatusOK {
		t.Fatalf("aprender: %d %s", w.Code, w.Body.String())
	}
	w = chamar(t, h, http.MethodGet, "/api/aprendizado", nil)
	if !strings.Contains(w.Body.String(), `"variante":"sf"`) || !strings.Contains(w.Body.String(), `"termo":"ECF"`) {
		t.Errorf("lista: %s", w.Body.String())
	}
}

// O usuario do dicionário criado é o mesmo da regra antiga do dono: se a
// migração do dono fixo falhar, o dono é "local" com ou sem dicionário criado.
func TestUsuarioDoDicionarioNovo_ECoerenteComARegraAntigaDoDono(t *testing.T) {
	log, _ := logDeTeste()
	if got := donoPelaRegraAntiga(log, ""); got != usuarioDoDicionarioNovo {
		t.Errorf("sem dicionário o dono é %q, e o dicionário criado é de %q", got, usuarioDoDicionarioNovo)
	}
	if got := donoPelaRegraAntiga(log, usuarioDoDicionarioNovo); got != usuarioDoDicionarioNovo {
		t.Errorf("com o dicionário criado o dono é %q, quero %q", got, usuarioDoDicionarioNovo)
	}
	// "local" é o valor que o dictlearn (donoSemDicionario) também usa.
	if usuarioDoDicionarioNovo != "local" {
		t.Errorf("usuarioDoDicionarioNovo = %q; o dictlearn e o histórico antigo usam \"local\"", usuarioDoDicionarioNovo)
	}
}

// run() não roda em teste, então a ordem é conferida no código de main.go: o
// dicionário é garantido ANTES de aberto (o usuario do arquivo entra no runtime
// e no aprendizado logo depois), com o caminho da tabela, e uma vez só.
func TestRun_GarantirDicionarioVemAntesDeAbrir(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string][]token.Pos{}
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		nome := types.ExprString(c.Fun)
		if nome == "garantirDicionario" || nome == "abrirDicionarioEm" || nome == "novoAprendizado" {
			pos[nome] = append(pos[nome], c.Pos())
			if len(c.Args) < 2 || types.ExprString(c.Args[1]) != "cam.Dicionario" {
				if nome != "novoAprendizado" { // novoAprendizado leva o caminho no argumento 0
					t.Errorf("%s: %s sem cam.Dicionario no argumento 1", fset.Position(c.Pos()), nome)
				}
			}
		}
		return true
	})
	for _, nome := range []string{"garantirDicionario", "abrirDicionarioEm", "novoAprendizado"} {
		if len(pos[nome]) != 1 {
			t.Fatalf("main.go chama %s %d vez(es); espero exatamente 1", nome, len(pos[nome]))
		}
	}
	if pos["garantirDicionario"][0] >= pos["abrirDicionarioEm"][0] {
		t.Error("main.go abre o dicionário ANTES de garanti-lo: na primeira subida o Agent ficaria sem usuario e sem aprendizado")
	}
}
