package cloudstore_test

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As validações da nota na nuvem são as do PC (ADR-022, N7): o servidor não
// importa o armazenamento do PC, e as reescreve em cloudstore/notas.go. Só
// este teste olha os dois lados, numa tabela: o que o PC aceita a nuvem aceita,
// e o que a nuvem recusa o PC não deixaria subir. (O nome do marcador é a
// função não exportada nomeDoMarcador do PC; a tabela dele está em
// TestNotas_NomeDoMarcador, com as mesmas regras.)
func TestNotas_AsValidacoesSaoAsDoPC(t *testing.T) {
	if cloudstore.MaxNoteTitleBytes != storage.MaxTituloDaNotaBytes || cloudstore.MaxTextBytes != storage.MaxCorpoDaNotaBytes ||
		cloudstore.MaxLabelName != storage.MaxNomeDoMarcador || cloudstore.MaxPosicao != storage.MaxPosicao {
		t.Errorf("os tetos: título %d/%d, corpo %d/%d, nome %d/%d, posição %d/%d",
			cloudstore.MaxNoteTitleBytes, storage.MaxTituloDaNotaBytes, cloudstore.MaxTextBytes, storage.MaxCorpoDaNotaBytes,
			cloudstore.MaxLabelName, storage.MaxNomeDoMarcador, cloudstore.MaxPosicao, storage.MaxPosicao)
	}
	if strings.Join(cloudstore.CoresDaNota, ",") != strings.Join(storage.CoresDaNota, ",") {
		t.Errorf("as cores: %v e %v", cloudstore.CoresDaNota, storage.CoresDaNota)
	}
	for _, c := range []string{"amarelo", "verde", "cinza", "", "Verde", "preto", " azul", "#ff0000"} {
		if cloudstore.CorValida(c) != storage.CorValida(c) {
			t.Errorf("a cor %q: nuvem %v, PC %v", c, cloudstore.CorValida(c), storage.CorValida(c))
		}
	}
	for _, p := range []string{
		"V", "0V", "a0b", "zz", "1", "Az", strings.Repeat("z", 128),
		"", "0", "V0", "a-b", "é", " V", strings.Repeat("z", 129), "a\x00",
	} {
		if cloudstore.PosicaoValida(p) != storage.PosicaoValida(p) {
			t.Errorf("a posição %q: nuvem %v, PC %v", p, cloudstore.PosicaoValida(p), storage.PosicaoValida(p))
		}
	}
	// Toda chave que o PC gera é válida na nuvem.
	antes := ""
	for i := 0; i < 200; i++ {
		k, err := storage.PosicaoEntre("", antes)
		if err != nil {
			t.Fatal(err)
		}
		if !cloudstore.PosicaoValida(k) {
			t.Fatalf("a chave %q que o PC gerou não vale na nuvem", k)
		}
		antes = k
	}
}

// O nome do marcador: a limpeza e as recusas do nomeDoMarcador do PC.
func TestNotas_NomeDoMarcador(t *testing.T) {
	for bruto, want := range map[string]string{
		"Mercado":                       "Mercado",
		"  Mercado   do   mês ":         "Mercado do mês",
		strings.Repeat("é", 50):         strings.Repeat("é", 50),
		" " + strings.Repeat("a", 50):   strings.Repeat("a", 50),
		"":                              "",
		"   ":                           "",
		strings.Repeat("é", 51):         "",
		"a\nb":                          "",
		"a\tb":                          "",
		"a\u2028b":                      "",
		"a\u2029b":                      "",
		"a\x00b":                        "",
		"a\x7fb":                        "",
		"C#":                            "C#",
		"\xff":                          "",
		"Ação e reação, 100% (urgente)": "Ação e reação, 100% (urgente)",
	} {
		got, ok := cloudstore.NomeDoMarcador(bruto)
		if want == "" && ok || want != "" && (!ok || got != want) {
			t.Errorf("NomeDoMarcador(%q) = %q, %v; quero %q", bruto, got, ok, want)
		}
	}
}
