package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/entitygate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Nenhum teste deste arquivo invoca o ASR real.
//
// Isso não é economia de tempo, é correção: o áudio do dataset é
// gitignorado, e um teste que dependesse dele passaria nesta máquina e
// NUNCA em outra. Já aconteceu neste projeto — o CI ficou vermelho desde
// b8881ee sem ninguém notar, porque eu reportava verde de execução
// local. O que se testa aqui é a lógica de ligação; a medição de verdade
// é uma execução manual do comando.

// TestAplicar_ResolverNilNaoMexeNoTexto é o controle da medição inteira.
//
// Se o caminho "sem resolver" alterasse o texto, toda diferença entre
// bruto e resolvido ficaria contaminada — o comando reportaria ganho ou
// regressão que vieram da própria ferramenta.
func TestAplicar_ResolverNilNaoMexeNoTexto(t *testing.T) {
	const bruto = "precisa transmitir a  DCTF Web   antes do dia quinze"
	got, err := aplicar(context.Background(), nil, "u", "", bruto)
	if err != nil {
		t.Fatalf("aplicar(nil) err = %v", err)
	}
	if got != bruto {
		t.Errorf("aplicar(nil) alterou o texto:\n  antes:  %q\n  depois: %q", bruto, got)
	}
}

// TestAplicar_LiteralSoNormalizaEspaco: o Literal existe para NÃO
// corrigir nada (AC-023). Se ele mexesse em palavra, o modo "literal"
// deixaria de ser a linha de base honesta contra a qual o Smart é
// comparado.
func TestAplicar_LiteralSoNormalizaEspaco(t *testing.T) {
	got, err := aplicar(context.Background(), resolver.NewLiteralResolver(), "u", "",
		"o  DCTF Web   e   o PGDAS")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "o DCTF Web e o PGDAS" {
		t.Errorf("Literal = %q, want %q", got, "o DCTF Web e o PGDAS")
	}
}

// TestAplicar_PropagaErroDoResolver: erro de resolver não pode virar
// texto vazio medido como "tudo errado".
func TestAplicar_PropagaErroDoResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := aplicar(ctx, resolver.NewLiteralResolver(), "u", "", "qualquer coisa"); err == nil {
		t.Error("contexto cancelado não propagou erro — a amostra entraria na medição como texto vazio")
	}
}

// TestAplicar_UsuarioErradoNaoAchaNada trava o defeito que quase entrou
// em 27-set-2026: a semente usa usuario "mauro" e o resolver era chamado
// com UserID "bench".
//
// dictionary.Search filtra por UserID. Com a divergência, o Smart não
// acharia NENHUM termo, a medição mostraria ganho zero, e a leitura
// natural seria "o dicionário não funciona" — quando na verdade ele nem
// tinha sido consultado. Nada falharia; o número é que estaria errado.
func TestAplicar_UsuarioErradoNaoAchaNada(t *testing.T) {
	const usuario = "mauro"
	semente, err := dictionary.CarregarSemente(strings.NewReader(
		`{"usuario":"` + usuario + `","entradas":[{"canonica":"DCTFWeb","alias":["DCTF Web"]}]}`))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	res, err := resolverDe("smart", &semente)
	if err != nil {
		t.Fatalf("resolverDe: %v", err)
	}

	const bruto = "transmitir a DCTF Web hoje"
	certo, err := aplicar(context.Background(), res, usuario, "", bruto)
	if err != nil {
		t.Fatalf("aplicar (usuário certo): %v", err)
	}
	if !strings.Contains(certo, "DCTFWeb") {
		t.Fatalf("com o usuário CERTO o alias não foi aplicado: %q — o resto deste teste não vale nada", certo)
	}

	errado, err := aplicar(context.Background(), res, "outro", "", bruto)
	if err != nil {
		t.Fatalf("aplicar (usuário errado): %v", err)
	}
	if errado != normalizado(bruto) {
		t.Errorf("com o usuário ERRADO algo foi corrigido (%q) — o escopo por usuário não está sendo respeitado", errado)
	}
}

// normalizado aplica só o que o resolver faz com espaço, para o teste
// acima comparar contra o texto realmente esperado.
func normalizado(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestResolverDe(t *testing.T) {
	casos := []struct {
		modo    string
		querNil bool
		querErr bool
	}{
		{"nenhum", true, false},
		{"literal", false, false},
		{"smart", false, false},
		{"inexistente", true, true},
	}
	for _, c := range casos {
		got, err := resolverDe(c.modo, nil)
		if c.querErr && err == nil {
			t.Errorf("resolverDe(%q) err = nil, want erro", c.modo)
		}
		if !c.querErr && err != nil {
			t.Errorf("resolverDe(%q) err = %v", c.modo, err)
		}
		if (got == nil) != c.querNil {
			t.Errorf("resolverDe(%q) nil = %v, want %v", c.modo, got == nil, c.querNil)
		}
	}
}

// TestImprimir_MostraOsDoisPortoes: o relatório não pode mostrar a
// acurácia sem a correção falsa. Ver TestPassouNoPortao_ExigeOsDois — um
// dicionário agressivo bate a primeira estragando a segunda, e quem lê o
// relatório precisa ver as duas de uma vez.
func TestImprimir_MostraOsDoisPortoes(t *testing.T) {
	r := entitygate.Resultado{
		Bruto:      metrics.MatchResult{Correct: 30, Total: 49},
		Resolvido:  metrics.MatchResult{Correct: 47, Total: 49},
		Ganhos:     17,
		Regressoes: 0,
	}
	var buf bytes.Buffer
	imprimir(&buf, r, "smart", "DCTFWeb", "dev", 0, false)
	out := buf.String()

	for _, esperado := range []string{"BRUTA", "RESOLVIDA", "correção falsa", "PORTÃO", "ganhos", "regressões"} {
		if !strings.Contains(out, esperado) {
			t.Errorf("relatório não menciona %q:\n%s", esperado, out)
		}
	}
	if !strings.Contains(out, "30/49") || !strings.Contains(out, "47/49") {
		t.Errorf("relatório não mostra as duas contagens:\n%s", out)
	}
}

// TestImprimir_FalhaDeAmostraApareceNoRelatorio.
//
// Amostra que falhou não entra em nenhum número — e é justamente por
// isso que precisa aparecer. Um dataset em que metade falhou pode dar
// 100% de acurácia nas que sobraram, e sem esta linha o relatório leria
// como sucesso.
func TestImprimir_FalhaDeAmostraApareceNoRelatorio(t *testing.T) {
	var buf bytes.Buffer
	imprimir(&buf, entitygate.Resultado{
		Bruto:     metrics.MatchResult{Correct: 2, Total: 2},
		Resolvido: metrics.MatchResult{Correct: 2, Total: 2},
	}, "literal", "", "", 44, false)

	if !strings.Contains(buf.String(), "FALHARAM: 44") {
		t.Errorf("44 amostras falharam e o relatório não disse:\n%s", buf.String())
	}
}

// TestImprimir_DiffPrioriazaRegressao: quem lê o diff está procurando
// dano antes de procurar ganho. Regressão tem de vir primeiro na tela.
func TestImprimir_DiffPriorizaRegressao(t *testing.T) {
	r := entitygate.Resultado{
		Bruto:     metrics.MatchResult{Correct: 2, Total: 3},
		Resolvido: metrics.MatchResult{Correct: 2, Total: 3},
		Ocorrencias: []entitygate.Ocorrencia{
			{AmostraID: "a", Entidade: "Supabase", Status: entitygate.Corrigida,
				TextoBruto: "Supabaese", TextoResolvido: "Supabase"},
			{AmostraID: "b", Entidade: "DRE", Status: entitygate.Regrediu,
				TextoBruto: "a DRE", TextoResolvido: "a DAS"},
		},
	}
	var buf bytes.Buffer
	imprimir(&buf, r, "smart", "", "", 0, true)
	out := buf.String()

	posRegressao := strings.Index(out, "REGREDIU")
	posGanho := strings.Index(out, "CORRIGIDA")
	if posRegressao < 0 {
		t.Fatalf("diff não trouxe a seção de regressão:\n%s", out)
	}
	if posGanho >= 0 && posRegressao > posGanho {
		t.Error("ganho apareceu antes de regressão — quem lê procura dano primeiro")
	}
	if !strings.Contains(out, "resolver: a DAS") {
		t.Errorf("o diff não mostra o texto DEPOIS do resolver, que é o que permite achar o alias culpado:\n%s", out)
	}
}

// TestImprimir_MostraODominio.
//
// Entrada restrita do dicionário só vale no domínio certo, então duas
// execuções com o mesmo dataset podem dar números diferentes só por
// causa da flag -domain. Sem o domínio no cabeçalho, as duas saídas
// ficam indistinguíveis — e foi o que aconteceu em 27-set-2026: uma
// medição rodou com dominio=dev e eu não tinha como saber pela saída.
func TestImprimir_MostraODominio(t *testing.T) {
	r := entitygate.Resultado{
		Bruto:     metrics.MatchResult{Correct: 38, Total: 49},
		Resolvido: metrics.MatchResult{Correct: 40, Total: 49},
	}
	var comDominio, semDominio bytes.Buffer
	imprimir(&comDominio, r, "smart", "", "dev", 0, false)
	imprimir(&semDominio, r, "smart", "", "", 0, false)

	if !strings.Contains(comDominio.String(), "dominio: dev") {
		t.Errorf("cabeçalho não mostra o domínio:\n%s", comDominio.String())
	}
	if !strings.Contains(semDominio.String(), "dominio: desconhecido") {
		t.Errorf("domínio ausente não é rotulado como desconhecido:\n%s", semDominio.String())
	}
	// A prova de que o campo distingue as duas execuções.
	if comDominio.String() == semDominio.String() {
		t.Error("as duas saídas são idênticas — o domínio não aparece em lugar nenhum")
	}
}

// TestDominioDaAmostra_AutoDeduzDaCategoria trava a correção do erro de
// método de 27-set-2026: simular UM domínio para um dataset que mistura
// domínios desliga metade do dicionário.
//
// Rodando tudo como "dev", a medição ganhou "commit" e perdeu "DAS" e
// "EFD-Reinf" (ambos do domínio contábil), e o total não mexeu — o que se
// leu como "o mecanismo não funciona" quando ele funcionava.
func TestDominioDaAmostra_AutoDeduzDaCategoria(t *testing.T) {
	casos := []struct {
		categoria dataset.Category
		quer      string
	}{
		{"contabilidade", "contabil"},
		{"desenvolvimento", "dev"},
		{"code-switching", "dev"},
		{"nomes-proprios", ""}, // nome próprio não é de domínio nenhum
		{"pt-br-geral", ""},    // e conversa comum menos ainda
		{"self-corrections", ""},
	}
	for _, c := range casos {
		if got := dominioDaAmostra("auto", c.categoria); got != c.quer {
			t.Errorf("auto/%s = %q, want %q", c.categoria, got, c.quer)
		}
	}
}

// TestDominioDaAmostra_ModoFixoIgnoraACategoria: passar um domínio
// explícito continua valendo para todas as amostras — é o modo que
// responde "e se a pessoa estivesse SEMPRE no editor?".
func TestDominioDaAmostra_ModoFixoIgnoraACategoria(t *testing.T) {
	for _, cat := range []dataset.Category{"contabilidade", "desenvolvimento", "pt-br-geral"} {
		if got := dominioDaAmostra("dev", cat); got != "dev" {
			t.Errorf("modo fixo dev com categoria %s = %q", cat, got)
		}
	}
	if got := dominioDaAmostra("", "desenvolvimento"); got != "" {
		t.Errorf("modo vazio = %q, want vazio (desconhecido é o caso seguro)", got)
	}
}
