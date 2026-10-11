package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"unicode"
	"unicode/utf8"
)

// CloudClient chama o gateway cloud POST /resolve, modo "smart"
// (docs/ai-spec/06-api-contract.md, "Resolve cloud"). Devolve o corpo
// BRUTO da resposta (JSON) em vez de já decodificado: a decodificação e a
// validação campo a campo acontecem em parseAndValidateCloudResponse,
// porque "replacement inválido" inclui explicitamente "JSON inválido ou
// campos faltando" — a defesa não pode confiar em um decode já feito por
// outra camada (essa é a última linha de defesa contra o cloud devolver
// lixo ou ser manipulado).
type CloudClient interface {
	ResolveSmart(ctx context.Context, req CloudSmartRequest) ([]byte, error)
}

// CloudUncertainSpan é um span de docs/ai-spec/06-api-contract.md,
// "Resolve cloud" → uncertain_spans.
type CloudUncertainSpan struct {
	Start      int      `json:"start"`
	End        int      `json:"end"`
	Original   string   `json:"original"`
	Candidates []string `json:"candidates"`
}

// CloudContext é docs/ai-spec/06-api-contract.md, "Resolve cloud" →
// context. NearbyText é DADO NÃO CONFIÁVEL do ponto de vista de quem o
// recebe de volta — aqui só sai como string dentro do payload JSON, nunca
// interpretado.
type CloudContext struct {
	App        string `json:"app"`
	NearbyText string `json:"nearby_text"`
}

// CloudSmartRequest é o corpo de POST /resolve no modo "smart"
// (docs/ai-spec/06-api-contract.md).
type CloudSmartRequest struct {
	Mode            string               `json:"mode"`
	Transcript      string               `json:"transcript"`
	UncertainSpans  []CloudUncertainSpan `json:"uncertain_spans"`
	Context         CloudContext         `json:"context"`
	DictionaryTerms []string             `json:"dictionary_terms"`
}

// validReplacement é um replacement do cloud já validado, pronto para
// virar offsetReplacement + AppliedReplacement.
type validReplacement struct {
	Start, End  int
	Replacement string
	Confidence  float64
	original    string
}

// parseAndValidateCloudResponse decodifica e valida a resposta bruta do
// cloud contra o texto local e os spans marcados como incertos, conforme
// as regras da tarefa (item 6):
//
//   - JSON inválido no nível do envelope (`{"replacements": [...]}`) faz a
//     função inteira falhar (err != nil): o chamador cai para o resultado
//     local (fallback "resolver falhou -> local result").
//   - Cada replacement individual é validado isoladamente. Um replacement
//     com JSON inválido ou campo obrigatório faltando, span fora dos
//     limites do transcript, span que não corresponde a um span marcado
//     como incerto, sobreposição com outro replacement já aceito, ou que
//     muda dígitos (números/datas) sem essa mudança estar no span
//     original, é REJEITADO — mas replacements válidos no mesmo lote
//     continuam sendo aplicados. Uma resposta parcialmente inválida nunca
//     corrompe o texto: cada replacement rejeitado simplesmente não é
//     aplicado.
//
// invalidReplacementRune procura no replacement uma runa que jamais pode
// chegar ao texto inserido no campo do usuário.
//
// Dois casos, ambos encontrados por ataque adversarial:
//
//   - Caracteres de controle C0, em especial U+0000. As APIs Win32 de
//     inserção de texto trabalham com C-strings terminadas em NUL, então um
//     NUL embutido truncaria o texto em silêncio na hora de inserir.
//   - U+FFFD, que é o que encoding/json produz quando a resposta trazia um
//     surrogate UTF-16 solto ou bytes malformados. O JSON decodifica sem
//     erro, passa por todas as outras checagens, e a palavra do usuário
//     chega à tela como um glifo ilegível.
//
// Nenhum dos dois é pego pelas validações de span: são válidos em posição e
// não alteram dígitos. Por isso a checagem precisa ser de CONTEÚDO.
func invalidReplacementRune(s string) (rune, bool) {
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return r, true
		}
	}
	return 0, false
}

func parseAndValidateCloudResponse(raw []byte, text string, pending []UncertainSpan) (valid []validReplacement, warnings []string, err error) {
	var envelope struct {
		Replacements []json.RawMessage `json:"replacements"`
	}
	if e := json.Unmarshal(raw, &envelope); e != nil {
		return nil, nil, fmt.Errorf("resolver: cloud resolve respondeu JSON inválido: %w", e)
	}

	textRunes := []rune(text)
	markedByRange := make(map[[2]int]UncertainSpan, len(pending))
	for _, p := range pending {
		markedByRange[[2]int{p.Start, p.End}] = p
	}

	type candidate struct {
		Start, End  int
		Replacement string
		Confidence  float64
	}
	var candidates []candidate

	for i, rm := range envelope.Replacements {
		var fields struct {
			Start       *int     `json:"start"`
			End         *int     `json:"end"`
			Replacement *string  `json:"replacement"`
			Confidence  *float64 `json:"confidence"`
		}
		if e := json.Unmarshal(rm, &fields); e != nil {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: JSON inválido", i))
			continue
		}
		if fields.Start == nil || fields.End == nil || fields.Replacement == nil {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: campo obrigatório faltando", i))
			continue
		}
		if bad, found := invalidReplacementRune(*fields.Replacement); found {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: caractere inválido U+%04X no replacement", i, bad))
			continue
		}
		conf := 0.0
		if fields.Confidence != nil {
			conf = *fields.Confidence
		}
		candidates = append(candidates, candidate{Start: *fields.Start, End: *fields.End, Replacement: *fields.Replacement, Confidence: conf})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Start < candidates[j].Start })

	lastEnd := -1
	for i, c := range candidates {
		if c.Start < 0 || c.End > len(textRunes) || c.Start >= c.End {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: span fora dos limites do transcript", i))
			continue
		}
		span, marked := markedByRange[[2]int{c.Start, c.End}]
		if !marked {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: span não foi marcado como incerto", i))
			continue
		}
		if c.Start < lastEnd {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: sobrepõe outro replacement", i))
			continue
		}
		if !sameDigits(span.Original, c.Replacement) {
			warnings = append(warnings, fmt.Sprintf("cloud replacement[%d] descartado: altera números/datas sem evidência", i))
			continue
		}
		valid = append(valid, validReplacement{Start: c.Start, End: c.End, Replacement: c.Replacement, Confidence: c.Confidence, original: span.Original})
		lastEnd = c.End
	}

	return valid, warnings, nil
}
