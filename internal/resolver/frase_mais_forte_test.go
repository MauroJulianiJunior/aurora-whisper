package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

func repoDe(t *testing.T, semente string) dictionary.DictionaryRepository {
	t.Helper()
	s, err := dictionary.CarregarSemente(strings.NewReader(semente))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	return repo
}

func resolva(t *testing.T, repo dictionary.DictionaryRepository, usuario, texto string) ResolveResult {
	t.Helper()
	out, err := NewSmartResolver(repo).Resolve(context.Background(), ResolveRequest{
		UserID:    usuario,
		ASRResult: asr.ASRResult{Text: texto, Language: "pt"},
	})
	if err != nil {
		t.Fatalf("Resolve(%q): %v", texto, err)
	}
	return out
}

// TestSmart_MatchExatoNaoEhSombreadoPorFoneticoMaisLongo trava um defeito
// que só aparecia em texto REAL.
//
// O laço de frases ia da mais longa para a mais curta e parava no
// primeiro acerto. Medido em 27-set-2026 com a frase de teste do dataset:
//
//	"DCTF Web hoje" -> phonetic (0,48)  <- ganhava e CONSUMIA os tokens
//	"DCTF Web"      -> exact    (1,00)  <- nunca era tentado
//
// Evidência fraca sobre três palavras engolia evidência forte sobre duas.
// Isolado, "DCTF Web" era corrigido; dentro de uma frase, não — que é o
// único jeito que o produto é usado.
func TestSmart_MatchExatoNaoEhSombreadoPorFoneticoMaisLongo(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`)

	isolado := resolva(t, repo, "m", "DCTF Web")
	if isolado.FinalText != "DCTFWeb" {
		t.Fatalf("isolado = %q, want %q — o resto deste teste não vale nada sem isto",
			isolado.FinalText, "DCTFWeb")
	}

	naFrase := resolva(t, repo, "m", "transmitir a DCTF Web hoje")
	if !strings.Contains(naFrase.FinalText, "DCTFWeb") {
		t.Errorf("dentro da frase = %q — o termo foi corrigido isolado mas não em texto real",
			naFrase.FinalText)
	}
}

// TestSmart_ExatoComEspacamentoDiferenteEhCanonicalizado.
//
// dictionary.Search tolera espaçamento (CollapseSpaces compara
// "dctf web" com "dctfweb"), então um match EXATO pode estar grafado
// diferente da forma canônica. Até 27-set-2026 o ramo exato não fazia
// nada, e o resolver era estruturalmente incapaz de consertar erro de
// fronteira de token — "DCTF Web", "End Point", "Time Out" —, que é boa
// parte do que o ASR erra de verdade.
func TestSmart_ExatoComEspacamentoDiferenteEhCanonicalizado(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[
		{"canonica":"endpoint","prioridade":100},
		{"canonica":"timeout","prioridade":100}]}`)

	got := resolva(t, repo, "m", "o End Point esta retornando Time Out").FinalText
	for _, termo := range []string{"endpoint", "timeout"} {
		if !strings.Contains(strings.ToLower(got), termo) {
			t.Errorf("%q não foi canonicalizado: %q", termo, got)
		}
	}
}

// TestSmart_DiferencaSoDeCaixaSoReescreveGrafiaPropria.
//
// metrics.EntityAccuracy — que é como o portão do produto é medido — é
// insensível a maiúscula, e até 01-out-2026 a diferença só de caixa nunca
// era reescrita. Desde o teste do Mauro daquele dia ("ESOCIAL" com "eSocial"
// no dicionário), o termo de GRAFIA PRÓPRIA sai na caixa do dicionário; a
// sigla toda maiúscula, o termo só com a inicial maiúscula e a palavra comum
// minúscula continuam como vieram. O termo de grafia própria escrito TODO em
// minúscula também fica ("npm install typescript" é a grafia certa do
// pacote): só conta quando o ASR escreveu alguma maiúscula.
// Ver precisaCanonicalizar.
func TestSmart_DiferencaSoDeCaixaSoReescreveGrafiaPropria(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[
		{"canonica":"DCTFWeb","prioridade":100},
		{"canonica":"DAS","prioridade":90},
		{"canonica":"Supabase","prioridade":90},
		{"canonica":"commit","prioridade":90}]}`)

	for _, escrito := range []string{"DCTFWEB", "Dctfweb"} {
		out := resolva(t, repo, "m", "transmitir a "+escrito+" hoje")
		if out.FinalText != "transmitir a DCTFWeb hoje" {
			t.Errorf("%q -> %q: o termo de grafia própria não saiu na caixa do dicionário",
				escrito, out.FinalText)
		}
	}
	for _, escrito := range []string{"DCTFWeb", "dctfweb", "das", "Das", "supabase", "SUPABASE", "Commit", "COMMIT"} {
		out := resolva(t, repo, "m", "transmitir a "+escrito+" hoje")
		if len(out.AppliedReplacements) != 0 {
			t.Errorf("%q gerou substituição %+v — só a caixa difere, e o termo não tem grafia própria "+
				"ou foi escrito todo em minúscula", escrito, out.AppliedReplacements)
		}
	}
}

// TestSmart_FoneticoContinuaSendoEvidenciaFraca: a correção acima não
// pode ter afrouxado o freio. Match fonético sozinho, sem contexto que
// corrobore, continua NÃO sendo aplicado — é o que protege o portão de
// correção falsa.
func TestSmart_FoneticoContinuaSendoEvidenciaFraca(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[{"canonica":"Supabase","prioridade":100}]}`)

	out := resolva(t, repo, "m", "sobe a migration no Supabaese agora")
	if strings.Contains(out.FinalText, "Supabase agora") {
		t.Error("match fonético foi auto-aplicado sem contexto — o freio contra correção falsa caiu")
	}
	if len(out.UncertainSpans) == 0 {
		t.Error("o quase-acerto fonético não virou span incerto — a informação se perdeu")
	}
}

// TestSmart_AliasDePalavraNaoEhSombreadoPorFrasFonetica trava o mesmo
// defeito de TestSmart_MatchExatoNaoEhSombreadoPorFoneticoMaisLongo, um
// degrau abaixo.
//
// A primeira correção comparou a força do match ENTRE FRASES (2 a 4
// palavras). Mas o ramo de palavra única só era alcançado se nenhuma
// frase tivesse consumido o token — então uma frase de evidência FRACA
// continuava engolindo uma palavra de evidência FORTE. Medido:
//
//	"Supabaese"   -> alias    (auto-aplicável)
//	"Supabaese e" -> phonetic (vira span incerto E consome o token)
//
// Valia três entidades no dataset de referência: Supabase e dois
// PostgreSQL.
func TestSmart_AliasDePalavraNaoEhSombreadoPorFraseFonetica(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[
		{"canonica":"Supabase","prioridade":100,"alias":["Supabaese"]},
		{"canonica":"PostgreSQL","prioridade":100,"alias":["Postgre","postgres"]}]}`)

	casos := []struct{ texto, quer string }{
		{"Sobe a Migration no Supabaese e confere se aceitou", "Supabase"},
		{"confere seu postgres que ele aceitou", "PostgreSQL"},
		{"no Postgre local mesmo", "PostgreSQL"},
	}
	for _, c := range casos {
		got := resolva(t, repo, "m", c.texto).FinalText
		if !strings.Contains(got, c.quer) {
			t.Errorf("%q\n  -> %q, faltou %q", c.texto, got, c.quer)
		}
	}
}

// TestSmart_EmEmpateDeForcaAFraseVence: "pull request" diz mais que
// "pull". A palavra isolada só ganha quando é ESTRITAMENTE mais forte.
func TestSmart_EmEmpateDeForcaAFraseVence(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[
		{"canonica":"pull request","prioridade":100},
		{"canonica":"pull","prioridade":100}]}`)

	got := resolva(t, repo, "m", "abre um pull request agora").FinalText
	if !strings.Contains(got, "pull request") {
		t.Errorf("= %q, want a frase inteira preservada", got)
	}
}

// TestSmart_PalavraForteEmQualquerPosicaoDaFrase é a generalização das
// duas correções anteriores.
//
// O token de casamento forte pode estar em QUALQUER posição dentro da
// frase fraca, não só na primeira. Medido em 27-set-2026:
//
//	"EFDRinf"     -> alias    (forte)
//	"e o EFDRinf" -> phonetic (fraco, consome TRÊS tokens)
//
// A correção anterior comparava a frase com a palavra INICIAL ("e"), que
// não casa com nada — então a frase fraca vencia e levava o token forte
// junto. Era a última entidade entre 93,9% e o portão de 95%.
func TestSmart_PalavraForteEmQualquerPosicaoDaFrase(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[
		{"canonica":"EFD-Reinf","prioridade":100,"alias":["EFDRinf"]}]}`)

	isolado := resolva(t, repo, "m", "EFDRinf").FinalText
	if !strings.Contains(isolado, "EFD-Reinf") {
		t.Fatalf("isolado = %q — o resto do teste não vale nada sem isto", isolado)
	}

	got := resolva(t, repo, "m", "manda o eSocial e o EFDRinf junto").FinalText
	if !strings.Contains(got, "EFD-Reinf") {
		t.Errorf("dentro da frase = %q — o token forte estava na terceira posição "+
			"de uma frase de casamento fraco", got)
	}
}

// TestSmart_FraseExataContinuaVencendo: a generalização não pode ter
// afrouxado o caso oposto. Match exato de frase é o teto — nada dentro
// dela pode ganhar, e "pull request" precisa continuar sendo tratado
// como uma coisa só.
func TestSmart_FraseExataContinuaVencendo(t *testing.T) {
	repo := repoDe(t, `{"usuario":"m","entradas":[
		{"canonica":"pull request","prioridade":100},
		{"canonica":"request","prioridade":100,"alias":["requesto"]}]}`)

	got := resolva(t, repo, "m", "abre um pull request agora").FinalText
	if !strings.Contains(got, "pull request") {
		t.Errorf("= %q — a frase exata deveria ter vencido o termo interno", got)
	}
}
