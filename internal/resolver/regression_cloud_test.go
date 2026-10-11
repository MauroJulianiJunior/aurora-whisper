package resolver_test

import (
	"context"
	"strconv"
	"testing"
)

// Os escapes abaixo são montados por concatenação de propósito: escritos de
// forma adjacente, o byte NUL e o surrogate solto acabariam no ARQUIVO-FONTE
// em vez de no JSON, e o compilador Go rejeita NUL no código. Aqui quem
// carrega o escape é o texto JSON, e quem o decodifica é o encoding/json —
// exatamente o caminho que uma resposta real do gateway percorreria.
const (
	nulEscape      = `\` + `u0000`
	surrogateSolto = `\` + `ud800`
	newlineEscape  = `\` + `n`
)

// TestRegression_CloudReplacement_RejectsInvalidContent fixa dois defeitos
// encontrados por ataque adversarial no F1-C.
//
// Ambos passavam por TODAS as validações de span — posição válida, dentro
// de um span realmente marcado como incerto, sem sobreposição, sem alterar
// dígitos — porque nenhuma delas olhava o CONTEÚDO do replacement:
//
//   - byte NUL embutido: as APIs Win32 de inserção de texto usam C-strings
//     terminadas em NUL, então o texto seria truncado em silêncio na hora
//     de entrar no campo com foco;
//   - U+FFFD: é o que encoding/json produz ao decodificar um surrogate
//     UTF-16 solto. O decode não falha, e a palavra do usuário chegaria à
//     tela como um glifo ilegível.
//
// Em ambos os casos o texto local tem que sobreviver intacto.
func TestRegression_CloudReplacement_RejectsInvalidContent(t *testing.T) {
	cases := []struct {
		name    string
		payload string // conteúdo bruto do campo "replacement" no JSON
	}{
		{name: "byte NUL embutido", payload: "bad" + nulEscape + "null"},
		{name: "surrogate solto vira U+FFFD", payload: surrogateSolto},
		// Sem dígitos de propósito: com eles, a validação sameDigits já
		// rejeitaria o replacement e este caso não provaria nada sobre a
		// checagem de caractere de controle.
		{name: "caractere de controle", payload: "linha" + newlineEscape + "quebrada"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := `{"replacements":[{"start":` + strconv.Itoa(pendingSpanStart) +
				`,"end":` + strconv.Itoa(pendingSpanEnd) +
				`,"replacement":"` + tc.payload +
				`","confidence":0.9}]}`

			sr, req := newAmbiguousSmartRequest()
			sr.Cloud = &fakeCloudClient{resp: []byte(resp)}

			got, err := sr.Resolve(context.Background(), req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.FinalText != localText {
				t.Fatalf("FinalText = %q, want %q (conteúdo inválido não pode chegar ao campo do usuário)", got.FinalText, localText)
			}
		})
	}
}
