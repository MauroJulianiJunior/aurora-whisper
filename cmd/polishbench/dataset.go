package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Caso é uma frase do benchmark.
type Caso struct {
	ID        string `json:"id"`
	Categoria string `json:"categoria"`
	// Entrada imita o texto que o Smart entrega ao Polish.
	Entrada string `json:"entrada"`
	// Esperado é o que o Polish IDEAL devolve sob a ADR-016.
	Esperado string `json:"esperado"`
	// Termos são as formas canônicas do dicionário presentes na entrada, as
	// mesmas que o Agent acharia pela consulta local ao dicionário.
	Termos []string `json:"termos"`
}

// As categorias do dataset.
const (
	CatLista          = "lista"
	CatValores        = "valores"
	CatSiglas         = "siglas"
	CatPontuacao      = "pontuacao_falada"
	CatPergunta       = "pergunta"
	CatInjecao        = "injecao"
	CatFalaLonga      = "fala_longa"
	CatHesitacao      = "hesitacao"
	CatCurta          = "curta"
	CatArmadilha      = "armadilha"
	CatGrafia         = "grafia"
	CatJaOrganizada   = "ja_organizada"
	prefixoJaOrganiza = "ja-"
)

// categoriasDasFrases são as categorias que frases.json pode ter, na ordem em
// que o relatório as mostra. ja_organizada vem do manifest do ASR.
var categoriasDasFrases = []string{
	CatLista, CatValores, CatSiglas, CatPontuacao, CatPergunta, CatInjecao,
	CatFalaLonga, CatHesitacao, CatCurta, CatArmadilha, CatGrafia,
}

// todasAsCategorias é categoriasDasFrases mais ja_organizada.
func todasAsCategorias() []string {
	return append(append([]string(nil), categoriasDasFrases...), CatJaOrganizada)
}

func categoriaValida(c string) bool {
	for _, v := range todasAsCategorias() {
		if v == c {
			return true
		}
	}
	return false
}

// arquivoFrases é o formato de frases.json.
type arquivoFrases struct {
	Versao int    `json:"versao"`
	Aviso  string `json:"aviso"`
	Frases []Caso `json:"frases"`
}

// carregarFrases lê frases.json. Campo desconhecido é erro: um erro de
// digitação num campo não pode passar calado.
func carregarFrases(dados []byte) ([]Caso, error) {
	var a arquivoFrases
	dec := json.NewDecoder(bytes.NewReader(dados))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("frases.json: %w", err)
	}
	if a.Versao != 1 {
		return nil, fmt.Errorf("frases.json: versao %d desconhecida (quero 1)", a.Versao)
	}
	for _, c := range a.Frases {
		if c.Categoria == CatJaOrganizada {
			return nil, fmt.Errorf("frases.json: %s: a categoria %s vem do manifest do ASR, não daqui", c.ID, CatJaOrganizada)
		}
	}
	return a.Frases, nil
}

// carregarReferencias lê o manifest do dataset de ASR e devolve as
// `reference` como a categoria ja_organizada: o Polish tem de devolvê-las
// iguais. As entidades do manifest são os termos do dicionário presentes.
func carregarReferencias(dados []byte) ([]Caso, error) {
	var m struct {
		Samples []struct {
			ID        string   `json:"id"`
			Reference string   `json:"reference"`
			Entities  []string `json:"entities"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(dados, &m); err != nil {
		return nil, fmt.Errorf("manifest do ASR: %w", err)
	}
	casos := make([]Caso, 0, len(m.Samples))
	for _, s := range m.Samples {
		casos = append(casos, Caso{
			ID:        prefixoJaOrganiza + s.ID,
			Categoria: CatJaOrganizada,
			Entrada:   s.Reference,
			Esperado:  s.Reference,
			Termos:    append([]string(nil), s.Entities...),
		})
	}
	return casos, nil
}

// validarCasos confere o dataset inteiro: ids únicos, categoria conhecida,
// entrada e esperado preenchidos, termos que existem na entrada.
func validarCasos(casos []Caso) error {
	if len(casos) == 0 {
		return errors.New("dataset vazio")
	}
	vistos := map[string]bool{}
	for _, c := range casos {
		switch {
		case strings.TrimSpace(c.ID) == "":
			return errors.New("caso sem id")
		case vistos[c.ID]:
			return fmt.Errorf("id repetido: %s", c.ID)
		case !categoriaValida(c.Categoria):
			return fmt.Errorf("%s: categoria desconhecida %q", c.ID, c.Categoria)
		case strings.TrimSpace(c.Entrada) == "":
			return fmt.Errorf("%s: entrada vazia", c.ID)
		case strings.TrimSpace(c.Esperado) == "":
			return fmt.Errorf("%s: esperado vazio", c.ID)
		}
		vistos[c.ID] = true
		for _, t := range c.Termos {
			if len(ocorrenciasDoTermo(c.Entrada, t)) == 0 {
				return fmt.Errorf("%s: o termo %q não está na entrada", c.ID, t)
			}
		}
	}
	return nil
}

// hashDoDataset é o sha256 (hexadecimal) do dataset: dos campos de cada caso,
// em ordem de id, separados por bytes que não aparecem no texto. Não depende
// da formatação dos arquivos nem da ordem em que os casos foram lidos, então
// um relatório antigo diz se as frases mudaram desde então.
func hashDoDataset(casos []Caso) string {
	ordenados := append([]Caso(nil), casos...)
	sort.Slice(ordenados, func(i, j int) bool { return ordenados[i].ID < ordenados[j].ID })
	h := sha256.New()
	for _, c := range ordenados {
		for _, campo := range []string{c.ID, c.Categoria, c.Entrada, c.Esperado} {
			h.Write([]byte(campo))
			h.Write([]byte{0})
		}
		for _, t := range c.Termos {
			h.Write([]byte(t))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// contagemPorCategoria conta os casos de cada categoria.
func contagemPorCategoria(casos []Caso) map[string]int {
	m := map[string]int{}
	for _, c := range casos {
		m[c.Categoria]++
	}
	return m
}
