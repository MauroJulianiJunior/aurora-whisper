package uiacontext

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenTokens lista fragmentos (case-insensitive) que não podem
// aparecer como identificador Go (nome de tipo/função/variável) ou como
// caminho de import em nenhum arquivo deste pacote. Cobre screenshot,
// captura de tela via GDI e OCR (AC-026, ADR-006: "Screenshot/OCR fica
// fora de escopo").
//
// Propositalmente NÃO é uma varredura textual ingênua do arquivo inteiro:
// isso apontaria falso positivo neste próprio pacote, cuja documentação
// (doc.go, provider.go) precisa *mencionar em prosa* que screenshot/OCR
// são proibidos. Em vez disso, o teste percorre a AST e só olha para
// identificadores de código e imports — nunca para o texto de comentários.
var forbiddenTokens = []string{
	"screenshot",
	"screencapture",
	"capturescreen",
	"bitblt",
	"printwindow",
	"gdi32",
	"hbitmap",
	"getdibits",
	"createcompatiblebitmap",
	"ocr",
	"tesseract",
	"robotgo",
}

func containsForbiddenToken(s string) (string, bool) {
	lower := strings.ToLower(s)
	for _, tok := range forbiddenTokens {
		if strings.Contains(lower, tok) {
			return tok, true
		}
	}
	return "", false
}

// TestNoPixelCaptureOrTextRecognition prova AC-026 por varredura: nenhum
// import ou símbolo (identificador) de captura de tela/reconhecimento
// óptico de caracteres existe no pacote uiacontext.
func TestNoPixelCaptureOrTextRecognition(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("não foi possível listar o diretório do pacote: %v", err)
	}

	scanned := 0
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("falha ao parsear %s: %v", path, err)
		}
		scanned++

		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if tok, bad := containsForbiddenToken(p); bad {
				t.Errorf("%s: import proibido %q contém token de captura de tela/OCR (%q)", path, p, tok)
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if tok, bad := containsForbiddenToken(ident.Name); bad {
				t.Errorf("%s: identificador %q contém token de captura de tela/OCR (%q)", path, ident.Name, tok)
			}
			return true
		})
	}

	if scanned == 0 {
		t.Fatal("nenhum arquivo .go foi varrido — o teste não está provando nada")
	}
}
