package cloudstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A versão do aviso é a MESMA no app e no servidor (ADR-021, decisão H e C3b):
// o app manda a versão do texto que mostrou, e o servidor só aceita a atual.
// Este teste lê o arquivo do app (web/src/nuvem/avisoDoDitado.ts) e prende os
// dois números: subir um sem o outro quebra aqui, e não o primeiro aceite de
// alguém.
func TestVersaoDoAviso_AppEServidorConferem(t *testing.T) {
	caminho := filepath.Join("..", "..", "..", "web", "src", "nuvem", "avisoDoDitado.ts")
	raw, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatalf("ler o arquivo do app que guarda a versão do aviso: %v", err)
	}
	// Sem os comentários: eles citam o nome da constante.
	var codigo []string
	for _, linha := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(linha), "//") {
			codigo = append(codigo, linha)
		}
	}
	m := regexp.MustCompile(`(?m)^export const VERSAO_DO_AVISO = (\d+)\s*$`).FindStringSubmatch(strings.Join(codigo, "\n"))
	if m == nil {
		t.Fatal("web/src/nuvem/avisoDoDitado.ts perdeu a linha \"export const VERSAO_DO_AVISO = N\"")
	}
	doApp, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if doApp != VersaoDoAviso {
		t.Errorf("o app mostra a versão %d do aviso e o servidor aceita a %d: suba as duas juntas", doApp, VersaoDoAviso)
	}
	if VersaoDoAviso < 1 {
		t.Errorf("VersaoDoAviso = %d: a coluna notice_version tem CHECK >= 1", VersaoDoAviso)
	}
}

func TestPreferenciasPadraoSaoValidas(t *testing.T) {
	p := PreferenciasPadrao()
	// Os padrões são as escolhas da ADR (OpenAI, sem dica, sem Polish), mas o
	// Polish não tem modelo: ligar um Polish sem modelo é erro, e o padrão não
	// o liga.
	if p.Transcricao.Provedor != ProvedorOpenAI || p.Transcricao.Modelo != ModeloPadraoOpenAI || p.Dica || p.PolishLigado ||
		p.Polish.Provedor != ProvedorOpenAI || p.Polish.Modelo != "" || !p.CriadaEm.IsZero() || !p.AtualizadaEm.IsZero() {
		t.Errorf("PreferenciasPadrao = %+v", p)
	}
	if err := ConferirPreferencias(p); err != nil {
		t.Errorf("os padrões não passam na conferência: %v", err)
	}
	if ModeloPadraoDaTranscricao(ProvedorGroq) != ModeloPadraoGroq || ModeloPadraoDaTranscricao(ProvedorAnthropic) != "" ||
		ModeloPadraoDaTranscricao("") != "" || ModeloPadraoDaTranscricao("xai") != "" {
		t.Error("ModeloPadraoDaTranscricao")
	}
	for _, p := range ProvedoresDeTranscricao {
		if !ModeloValido(ModeloPadraoDaTranscricao(p)) {
			t.Errorf("o modelo padrão da %s não tem a forma de um modelo", p)
		}
	}
}

func TestConferirPreferencias(t *testing.T) {
	boa := func() PreferenciasDoDitado {
		return PreferenciasDoDitado{
			Transcricao: ProvedorEModelo{Provedor: ProvedorGroq, Modelo: "whisper-large-v3-turbo"},
			Polish:      ProvedorEModelo{Provedor: ProvedorAnthropic, Modelo: "claude-x"}, PolishLigado: true,
		}
	}
	if err := ConferirPreferencias(boa()); err != nil {
		t.Fatalf("a boa: %v", err)
	}
	for nome, c := range map[string]struct {
		mudar func(p *PreferenciasDoDitado)
		quero error
	}{
		"transcrição pela anthropic": {func(p *PreferenciasDoDitado) { p.Transcricao.Provedor = ProvedorAnthropic }, ErrProvedorDeTranscricao},
		"transcrição vazia":          {func(p *PreferenciasDoDitado) { p.Transcricao.Provedor = "" }, ErrProvedorDeTranscricao},
		"transcrição em maiúsculas":  {func(p *PreferenciasDoDitado) { p.Transcricao.Provedor = "Groq" }, ErrProvedorDeTranscricao},
		"Polish pela groq":           {func(p *PreferenciasDoDitado) { p.Polish.Provedor = ProvedorGroq }, ErrProvedorDePolish},
		"Polish vazio":               {func(p *PreferenciasDoDitado) { p.Polish.Provedor = "" }, ErrProvedorDePolish},
		"modelo vazio":               {func(p *PreferenciasDoDitado) { p.Transcricao.Modelo = "" }, ErrModeloInvalido},
		"modelo com espaço":          {func(p *PreferenciasDoDitado) { p.Transcricao.Modelo = "a b" }, ErrModeloInvalido},
		"modelo com quebra no fim":   {func(p *PreferenciasDoDitado) { p.Transcricao.Modelo = "ab\n" }, ErrModeloInvalido},
		"modelo de 101 caracteres":   {func(p *PreferenciasDoDitado) { p.Transcricao.Modelo = strings.Repeat("a", 101) }, ErrModeloInvalido},
		"modelo do Polish ruim":      {func(p *PreferenciasDoDitado) { p.Polish.Modelo = "é" }, ErrModeloInvalido},
		"Polish ligado sem modelo":   {func(p *PreferenciasDoDitado) { p.Polish.Modelo = "" }, ErrPolishSemModelo},
	} {
		p := boa()
		c.mudar(&p)
		if err := ConferirPreferencias(p); err != c.quero {
			t.Errorf("%s: %v, quero %v", nome, err, c.quero)
		}
	}
	// O Polish desligado pode ficar sem modelo; o modelo de 100 vale.
	p := boa()
	p.PolishLigado, p.Polish.Modelo = false, ""
	if err := ConferirPreferencias(p); err != nil {
		t.Errorf("Polish desligado sem modelo: %v", err)
	}
	p.Transcricao.Modelo = strings.Repeat("a", TamanhoMaximoDoModelo)
	if err := ConferirPreferencias(p); err != nil {
		t.Errorf("modelo no teto: %v", err)
	}
}

func TestConferirAceite(t *testing.T) {
	for nome, n := range map[string]NovoAceite{
		"openai":                {Versao: 1, Transcricao: "openai"},
		"groq com a anthropic":  {Versao: 7, Transcricao: "groq", Polish: "anthropic"},
		"openai dos dois lados": {Versao: 1, Transcricao: "openai", Polish: "openai"},
	} {
		if err := ConferirAceite(n); err != nil {
			t.Errorf("%s: %v", nome, err)
		}
	}
	for nome, n := range map[string]NovoAceite{
		"versão zero":            {Versao: 0, Transcricao: "openai"},
		"versão negativa":        {Versao: -3, Transcricao: "openai"},
		"sem provedor":           {Versao: 1},
		"anthropic transcreve":   {Versao: 1, Transcricao: "anthropic"},
		"groq faz o Polish":      {Versao: 1, Transcricao: "openai", Polish: "groq"},
		"provedor em maiúsculas": {Versao: 1, Transcricao: "OpenAI"},
	} {
		if err := ConferirAceite(n); err != ErrAceiteInvalido {
			t.Errorf("%s: %v, quero ErrAceiteInvalido", nome, err)
		}
	}
}

// Vigente é: em aberto e da versão atual do aviso. Inclui é o conjunto: o
// provedor da transcrição ou o do Polish, nunca o vazio.
func TestAceite_VigenteEInclui(t *testing.T) {
	a := Aceite{Versao: VersaoDoAviso, Transcricao: "openai", Polish: "anthropic"}
	if !a.Aberto() || !a.Vigente() {
		t.Errorf("o aceite em aberto da versão atual: aberto %v, vigente %v", a.Aberto(), a.Vigente())
	}
	for p, quero := range map[string]bool{"openai": true, "anthropic": true, "groq": false, "": false, "OpenAI": false} {
		if got := a.Inclui(p); got != quero {
			t.Errorf("Inclui(%q) = %v, quero %v", p, got, quero)
		}
	}
	semPolish := Aceite{Versao: VersaoDoAviso, Transcricao: "groq"}
	if semPolish.Inclui("") || !semPolish.Inclui("groq") || semPolish.Inclui("anthropic") {
		t.Errorf("Inclui sem o Polish: %+v", semPolish)
	}
	velho := a
	velho.Versao = VersaoDoAviso + 1
	if velho.Vigente() || !velho.Aberto() {
		t.Error("o aceite de outra versão do aviso não vale, mesmo em aberto")
	}
	terminado := a
	terminado.EncerradoEm, terminado.Motivo = a.AceitoEm.Add(1), FimRevogado
	if terminado.Vigente() || terminado.Aberto() {
		t.Error("o aceite que terminou não vale")
	}
}
