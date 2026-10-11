package dataset_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// manifestPath é o manifesto versionado do dataset de voz. O áudio que ele
// referencia NÃO está no git (.gitignore bloqueia *.wav — invariante
// AC-028); só o manifesto, que contém apenas texto, é versionado.
const manifestPath = "../../../testdata/asr-dataset/manifest.json"

// carregarTexto lê o manifesto SEM passar por dataset.LoadManifest, de
// propósito.
//
// LoadManifest valida que cada arquivo de áudio existe, e é isso que o
// asrbench precisa. Mas os testes abaixo checam a consistência do TEXTO, e
// precisam rodar antes de a primeira frase ser gravada — que é justamente
// quando um erro de grafia no manifesto ainda custa barato de corrigir.
// Amarrá-los a LoadManifest os tornaria vermelhos em toda máquina que não
// tem o áudio, incluindo o CI, e a reação previsível seria desligá-los.
func carregarTexto(t *testing.T) []dataset.Sample {
	t.Helper()
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("lendo o manifesto versionado: %v", err)
	}
	var mf struct {
		Samples []dataset.Sample `json:"samples"`
	}
	if err := json.Unmarshal(raw, &mf); err != nil {
		t.Fatalf("manifesto não é JSON válido: %v", err)
	}
	if len(mf.Samples) == 0 {
		t.Fatal("manifesto sem nenhuma amostra")
	}
	return mf.Samples
}

// TestManifestVersionado_EhInternamenteConsistente valida o manifesto
// contra as próprias métricas que vão pontuá-lo.
//
// O erro que este teste existe para pegar é silencioso e caro: uma entidade
// declarada com grafia diferente da que aparece na frase ("EFD Reinf" na
// lista, "EFD-Reinf" na referência) torna aquele acerto IMPOSSÍVEL — nenhum
// candidato poderia transcrever uma palavra que não foi dita. O benchmark
// não acusaria erro nenhum: reportaria Entity Accuracy mais baixa, e a culpa
// cairia no modelo de ASR em vez de no manifesto.
func TestManifestVersionado_EhInternamenteConsistente(t *testing.T) {
	vistos := map[string]bool{}
	for _, s := range carregarTexto(t) {
		t.Run(s.ID, func(t *testing.T) {
			if vistos[s.ID] {
				t.Fatalf("id duplicado: %s", s.ID)
			}
			vistos[s.ID] = true

			if m := metrics.EntityAccuracy(s.Entities, s.Reference); m.Correct != m.Total {
				t.Errorf("entidades declaradas que não aparecem na própria referência (%d/%d)\n"+
					"  entidades: %q\n  referência: %q\n"+
					"  corrija a grafia da entidade OU a frase — do jeito que está, esse acerto é inalcançável",
					m.Correct, m.Total, s.Entities, s.Reference)
			}

			if m := metrics.NumberDateAccuracy(s.Reference, s.Reference); m.Correct != m.Total {
				t.Errorf("números/datas da referência não fecham contra ela mesma (%d/%d) em %q",
					m.Correct, m.Total, s.Reference)
			}

			// Uma amostra da categoria de números sem nenhum dígito não
			// mediria nada: a métrica daria 100% sobre zero itens.
			if s.Category == dataset.CategoryNumerosDatas {
				if m := metrics.NumberDateAccuracy(s.Reference, s.Reference); m.Total == 0 {
					t.Errorf("amostra de %q sem número ou data extraível: %q", s.Category, s.Reference)
				}
			}

			if w, err := metrics.WER(s.Reference, s.Reference); err != nil || w != 0 {
				t.Errorf("WER(referência, referência) = %v (err=%v), want 0 — referência degenerada", w, err)
			}
		})
	}
}

// TestManifestVersionado_SoUsaCategoriasValidas replica a validação que
// LoadManifest faria, já que os testes de texto não passam por ele. Um slug
// de categoria digitado errado só apareceria na hora de rodar o benchmark,
// depois de todo o áudio já gravado.
func TestManifestVersionado_SoUsaCategoriasValidas(t *testing.T) {
	validas := make(map[dataset.Category]bool, len(dataset.Categories))
	for _, c := range dataset.Categories {
		validas[c] = true
	}
	for _, s := range carregarTexto(t) {
		if !validas[s.Category] {
			t.Errorf("%s: categoria %q não é uma das 12 do 19-eval-plan.md", s.ID, s.Category)
		}
	}
}

// TestManifestVersionado_CobreTodasAsCategorias falha quando uma das 12
// categorias fica sem nenhuma amostra. Uma categoria vazia não produz erro
// em lugar nenhum: o relatório simplesmente não teria linha para ela, e a
// lacuna passaria por "tudo certo".
func TestManifestVersionado_CobreTodasAsCategorias(t *testing.T) {
	porCategoria := map[dataset.Category]int{}
	for _, s := range carregarTexto(t) {
		porCategoria[s.Category]++
	}
	for _, c := range dataset.Categories {
		if porCategoria[c] == 0 {
			t.Errorf("categoria %q sem nenhuma amostra no manifesto", c)
		}
	}
}

// TestManifestVersionado_ApontaParaAudioForaDoGit protege o invariante
// AC-028 por onde ele é mais fácil de furar sem perceber: alguém acrescenta
// uma amostra apontando para um arquivo com extensão que o .gitignore não
// bloqueia, e o áudio acaba commitado junto com o manifesto.
func TestManifestVersionado_ApontaParaAudioForaDoGit(t *testing.T) {
	for _, s := range carregarTexto(t) {
		if ext := filepath.Ext(s.AudioPath); ext != ".wav" {
			t.Errorf("%s: extensão %q — só .wav está coberto pelo .gitignore deste dataset", s.ID, ext)
		}
	}
}
