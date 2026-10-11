package resolver_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O PolishRequest carrega o ditado inteiro. Nada que o serialize ou imprima
// — json.Marshal, fmt em qualquer verbo, slog — pode levar o texto junto: se
// a S2 ou a S4 logarem o request por descuido, o ditado não vai para o log
// (CLAUDE.md: "nunca gravar texto em logs").
func TestPolishRequest_NaoVazaODitadoEmNenhumFormato(t *testing.T) {
	const canario = "CANARIO-DO-DITADO-SECRETO"
	req := resolver.PolishRequest{
		Text:       "eu disse " + canario + " ontem, ação",
		Language:   "pt-BR",
		Instrucoes: resolver.PolishPrompt,
		Mensagem:   resolver.PolishMensagemUsuario("eu disse " + canario + " ontem, ação"),
		Schema:     resolver.PolishSchemaSaida(),
		PromptRef:  resolver.PolishPromptRef,
	}

	saidas := map[string]string{}

	bruto, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	saidas["json.Marshal"] = string(bruto)
	bruto, _ = json.Marshal(&req)
	saidas["json.Marshal(&req)"] = string(bruto)
	bruto, _ = json.MarshalIndent(map[string]any{"req": req, "ptr": &req}, "", " ")
	saidas["json.MarshalIndent aninhado"] = string(bruto)

	for _, verbo := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x"} {
		saidas["fmt "+verbo] = fmt.Sprintf(verbo, req)
		saidas["fmt ptr "+verbo] = fmt.Sprintf(verbo, &req)
	}
	saidas["fmt.Sprint"] = fmt.Sprint(req)
	saidas["fmt.Sprintf struct exportado"] = fmt.Sprintf("%+v", struct{ Req resolver.PolishRequest }{req})
	saidas["String()"] = req.String()
	saidas["GoString()"] = req.GoString()
	saidas["erro com %v"] = fmt.Errorf("falhou: %v", req).Error()
	saidas["erro com %w de outro erro"] = fmt.Errorf("falhou: %+v", &req).Error()

	var buf bytes.Buffer
	texto := slog.New(slog.NewTextHandler(&buf, nil))
	texto.Info("chamando o provedor", "req", req)
	texto.Info("chamando o provedor", "req", &req)
	texto.Info("chamando o provedor", slog.Any("req", req))
	saidas["slog texto"] = buf.String()

	buf.Reset()
	js := slog.New(slog.NewJSONHandler(&buf, nil))
	js.Info("chamando o provedor", "req", req)
	js.Info("chamando o provedor", "req", &req)
	saidas["slog json"] = buf.String()

	for nome, s := range saidas {
		if strings.Contains(s, canario) {
			t.Errorf("%s vaza o ditado: %s", nome, s)
		}
		if strings.Contains(s, "ditado") && strings.Contains(s, "{\"ditado\"") {
			t.Errorf("%s vaza a mensagem do usuário: %s", nome, s)
		}
	}

	// O que vai ao log é útil: a versão do prompt e o tamanho do texto.
	for nome, s := range map[string]string{
		"String()": req.String(), "GoString()": req.GoString(), "%v": fmt.Sprintf("%v", req),
		"%+v": fmt.Sprintf("%+v", req), "%#v": fmt.Sprintf("%#v", req), "%d": fmt.Sprintf("%d", req),
	} {
		if !strings.Contains(s, "polish-ptbr@") || !strings.Contains(s, "caracteres:") {
			t.Errorf("%s não diz o prompt nem o tamanho: %s", nome, s)
		}
	}
	if got := req.String(); !strings.Contains(got, fmt.Sprintf("caracteres:%d", len([]rune(req.Text)))) {
		t.Errorf("String() = %s, want o tamanho em caracteres (runas) do texto", got)
	}
	if !strings.Contains(saidas["slog texto"], "polish-ptbr@") || !strings.Contains(saidas["slog texto"], "caracteres=") {
		t.Errorf("slog texto não diz o prompt nem o tamanho: %s", saidas["slog texto"])
	}
	if !strings.Contains(saidas["slog json"], `"caracteres":`) {
		t.Errorf("slog json não diz o tamanho: %s", saidas["slog json"])
	}
	if saidas["json.Marshal"] != "{}" {
		t.Errorf("json.Marshal = %s, want {} (nenhum campo é serializável)", saidas["json.Marshal"])
	}
}

// O texto que o cliente REALMENTE precisa continua acessível nos campos.
func TestPolishRequest_OsCamposContinuamAcessiveis(t *testing.T) {
	req := resolver.PolishRequest{Text: "oi", Language: "pt-BR", Mensagem: `{"ditado":"oi"}`, PromptRef: "p@1"}
	if req.Text != "oi" || req.Language != "pt-BR" || req.Mensagem != `{"ditado":"oi"}` || req.PromptRef != "p@1" {
		t.Fatalf("campos: %+v", req.PromptRef)
	}
}
