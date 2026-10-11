// Package dataset carrega e valida o manifesto do dataset de voz do
// benchmark de ASR (docs/ai-spec/19-eval-plan.md) e calcula seu hash.
//
// # Formato do manifesto: JSON, não TSV
//
// O manifesto é um arquivo JSON com um objeto por amostra. A alternativa
// óbvia seria TSV (uma linha por amostra, colunas separadas por tab), mas
// cada amostra carrega uma LISTA de entidades esperadas (zero, uma ou
// várias) — em TSV isso exigiria um delimitador secundário dentro de uma
// coluna (ex.: "DCTFWeb;Fisco"), que colide com o próprio conteúdo real
// do dataset: referências em PT-BR usam ponto-e-vírgula, vírgula e outros
// separadores dentro do texto normal (ex.: "R$1.500,00"), e uma entidade
// poderia legitimamente conter um desses caracteres. JSON representa a
// lista nativamente, sem inventar uma convenção de escape, e
// encoding/json (stdlib) já faz toda a validação de sintaxe.
//
// # Áudio fora do git
//
// O manifesto aponta para arquivos de áudio FORA do repositório
// (.gitignore bloqueia *.wav — invariante AC-028): o próprio manifesto,
// que contém só caminhos e texto de referência, pode ser versionado, mas
// os arquivos de áudio que ele referencia não. Caminhos relativos no
// manifesto são resolvidos relativos ao diretório do PRÓPRIO manifesto,
// não ao diretório de trabalho de quem roda o benchmark.
package dataset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Category identifica uma das 12 categorias do dataset definidas em
// docs/ai-spec/19-eval-plan.md, seção "Dataset". Os slugs abaixo são a
// forma canônica usada no manifesto e no relatório.
type Category string

// As 12 categorias do dataset, na ordem em que docs/ai-spec/19-eval-plan.md
// as lista.
const (
	CategoryPTBRGeral       Category = "pt-br-geral"
	CategoryIngles          Category = "ingles"
	CategoryCodeSwitching   Category = "code-switching"
	CategoryContabilidade   Category = "contabilidade"
	CategoryDesenvolvimento Category = "desenvolvimento"
	CategoryNomesProprios   Category = "nomes-proprios"
	CategoryNumerosDatas    Category = "numeros-datas-valores"
	CategorySelfCorrections Category = "self-corrections"
	CategoryRuidoModerado   Category = "ruido-moderado"
	CategoryFalaRapida      Category = "fala-rapida"
	CategoryCurta1a3s       Category = "1-3s"
	CategoryMedia3a10s      Category = "3-10s"
)

// Categories lista as 12 categorias válidas, na mesma ordem declarada em
// docs/ai-spec/19-eval-plan.md. É a fonte da verdade usada por
// validateCategory e pelo relatório (para listar categorias sem nenhuma
// amostra).
var Categories = []Category{
	CategoryPTBRGeral,
	CategoryIngles,
	CategoryCodeSwitching,
	CategoryContabilidade,
	CategoryDesenvolvimento,
	CategoryNomesProprios,
	CategoryNumerosDatas,
	CategorySelfCorrections,
	CategoryRuidoModerado,
	CategoryFalaRapida,
	CategoryCurta1a3s,
	CategoryMedia3a10s,
}

func validCategorySet() map[Category]bool {
	set := make(map[Category]bool, len(Categories))
	for _, c := range Categories {
		set[c] = true
	}
	return set
}

// Sample é uma amostra do dataset: um arquivo de áudio, seu texto de
// referência, a categoria (19-eval-plan.md) e as entidades que a
// referência deveria conter (para metrics.EntityAccuracy).
type Sample struct {
	// ID identifica a amostra no relatório e nos erros de validação. Se o
	// manifesto não fornecer um, LoadManifest deriva um a partir do
	// índice (ex.: "amostra[3]").
	ID string `json:"id"`
	// AudioPath é o caminho do arquivo de áudio, absoluto ou relativo ao
	// diretório do manifesto. LoadManifest o resolve para absoluto.
	AudioPath string `json:"audio"`
	// Reference é o texto de referência (transcrição correta).
	Reference string `json:"reference"`
	// Intended é o texto que a pessoa QUERIA produzir, quando ele difere
	// do que ela falou. Vazio significa "igual a Reference".
	//
	// Existe para as amostras de autocorreção de fala. Reference guarda a
	// fala LITERAL — "manda para a Rosângela... não, na verdade manda para
	// o Thiago" —, e o produto, corretamente, escreve só a intenção:
	// "manda para o Thiago". Medido contra Reference, o WER sobe
	// exatamente quando a autocorreção acerta, e por isso não podia barrar
	// o portão (ver entitygate.Resultado.PiorouTexto). Medido contra
	// Intended, ele pode.
	//
	// Os textos foram confirmados por Mauro em 28-set-2026. O da corr-04
	// é o que tem consequência: "São três mil e quinhentos reais." — o
	// "reais" só aparecia na parte retratada, e a autocorreção o perdia.
	Intended string `json:"intended,omitempty"`
	// Category é uma das 12 categorias em Categories.
	Category Category `json:"category"`
	// Entities são as entidades esperadas na referência desta amostra
	// (nomes de ferramentas, siglas, nomes próprios). Pode ser vazia.
	Entities []string `json:"entities"`
}

// manifestFile é o formato bruto do JSON no disco.
type manifestFile struct {
	Samples []Sample `json:"samples"`
}

// LoadManifest lê e valida o manifesto em path. Cada problema de validação
// vira um erro claro identificando a amostra (índice e/ou ID) — nunca um
// panic:
//
//   - arquivo de áudio ausente ou inacessível;
//   - referência vazia (ou só espaço) após TrimSpace;
//   - categoria desconhecida (fora de Categories);
//   - ID duplicado entre amostras.
//
// Caminhos de áudio relativos são resolvidos relativos ao diretório de
// path, e o Sample devolvido sempre carrega AudioPath absoluto.
func LoadManifest(path string) ([]Sample, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dataset: lendo manifesto %s: %w", path, err)
	}

	var mf manifestFile
	if err := json.Unmarshal(raw, &mf); err != nil {
		return nil, fmt.Errorf("dataset: manifesto %s não é JSON válido: %w", path, err)
	}
	if len(mf.Samples) == 0 {
		return nil, fmt.Errorf("dataset: manifesto %s não declara nenhuma amostra", path)
	}

	baseDir := filepath.Dir(path)
	validCategories := validCategorySet()
	seenIDs := make(map[string]bool, len(mf.Samples))

	samples := make([]Sample, len(mf.Samples))
	for i, s := range mf.Samples {
		label := s.ID
		if label == "" {
			label = fmt.Sprintf("amostra[%d]", i)
			s.ID = label
		}

		if seenIDs[s.ID] {
			return nil, fmt.Errorf("dataset: manifesto %s: ID de amostra duplicado %q", path, s.ID)
		}
		seenIDs[s.ID] = true

		if strings.TrimSpace(s.Reference) == "" {
			return nil, fmt.Errorf("dataset: manifesto %s: %s tem referência vazia", path, label)
		}

		if !validCategories[s.Category] {
			return nil, fmt.Errorf("dataset: manifesto %s: %s tem categoria desconhecida %q (válidas: %v)",
				path, label, s.Category, Categories)
		}

		if s.AudioPath == "" {
			return nil, fmt.Errorf("dataset: manifesto %s: %s não declara caminho de áudio", path, label)
		}
		audioPath := s.AudioPath
		if !filepath.IsAbs(audioPath) {
			audioPath = filepath.Join(baseDir, audioPath)
		}
		info, err := os.Stat(audioPath)
		if err != nil {
			return nil, fmt.Errorf("dataset: manifesto %s: %s: áudio %s inacessível: %w",
				path, label, audioPath, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("dataset: manifesto %s: %s: %s é um diretório, não um arquivo",
				path, label, audioPath)
		}
		s.AudioPath = audioPath

		samples[i] = s
	}

	return samples, nil
}
