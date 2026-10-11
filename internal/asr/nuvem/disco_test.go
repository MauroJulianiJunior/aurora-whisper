package nuvem

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Nenhum arquivo novo em disco (AC-028, ADR-021 decisão L): uma transcrição
// inteira, com sucesso e com todas as falhas, roda com o diretório de trabalho
// e o diretório temporário apontados para pastas vazias, e elas continuam
// vazias. O servidor de teste lê o multipart na memória (multipart.Reader),
// nunca com ParseMultipartForm, que gravaria em disco: o que aparecer aqui
// veio do pacote.
func TestNenhumArquivoNovoEmDisco(t *testing.T) {
	trabalho, temp := t.TempDir(), t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP", "TEMPDIR"} {
		t.Setenv(v, temp)
	}
	t.Chdir(trabalho)

	// Um áudio grande o bastante para um multipart que gravasse em disco o
	// faria (o limite padrão do ParseMultipartForm é 32 MiB, mas o do
	// mime/multipart para partes em memória é bem menor).
	grande := asr.Comprimido{Bytes: make([]byte, 2<<20), Tipo: "audio/webm"}

	statusVariados := []int{200, 401, 403, 413, 429, 500, 302}
	for _, status := range statusVariados {
		srv := novoProvedorFalso(t, responder(status, respostaOK))
		providerDe(srv, OpenAI, "").Transcribe(context.Background(), asr.AudioBuffer{Comprimido: grande}, asr.ASROptions{Vocabulary: canarioDica})
		providerDe(srv, Groq, "").Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
		clienteDe(srv).ListarModelos(context.Background(), OpenAI, chaveDeTeste)
	}
	vazio := novoProvedorFalso(t, responder(200, `{"text":""}`))
	providerDe(vazio, Groq, "").Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})

	for nome, dir := range map[string]string{"o diretório de trabalho": trabalho, "o diretório temporário": temp} {
		entradas, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entradas) != 0 {
			var nomes []string
			for _, e := range entradas {
				nomes = append(nomes, e.Name())
			}
			t.Errorf("%s ganhou arquivos: %v", nome, nomes)
		}
	}
}

// A guarda de fonte: o código do pacote (fora dos testes) não importa nada que
// grave arquivo ou escreva log, e não chama o que grava o multipart em disco.
// Complementa o teste acima: ele prova o que aconteceu; este, o que o código
// poderia fazer.
func TestFonteNaoTocaDiscoNemLog(t *testing.T) {
	proibidosImport := map[string]string{
		"os":            "grava arquivo",
		"io/ioutil":     "grava arquivo",
		"path/filepath": "monta caminho de arquivo",
		"log":           "escreve log",
		"log/slog":      "escreve log",
		"runtime/debug": "imprime pilha",
	}
	proibidosChamada := []string{"ParseMultipartForm", "ReadForm", "CreateTemp", "MkdirTemp", "WriteFile", "OpenFile", "os.Create", "Printf(", "Println("}

	arquivos, err := filepath.Glob("*.go")
	if err != nil || len(arquivos) == 0 {
		t.Fatalf("Glob: %v (%d arquivos)", err, len(arquivos))
	}
	visto := 0
	for _, nome := range arquivos {
		if strings.HasSuffix(nome, "_test.go") {
			continue
		}
		visto++
		src, err := os.ReadFile(nome)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), nome, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			caminho := strings.Trim(imp.Path.Value, `"`)
			if motivo, ruim := proibidosImport[caminho]; ruim {
				t.Errorf("%s importa %q (%s): este pacote não pode", nome, caminho, motivo)
			}
		}
		// Sem os comentários: eles explicam o que o pacote NÃO faz e citam
		// esses nomes.
		completo, err := parser.ParseFile(token.NewFileSet(), nome, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(completo, func(n ast.Node) bool {
			var nome string
			switch x := n.(type) {
			case *ast.SelectorExpr:
				nome = x.Sel.Name
				if id, ok := x.X.(*ast.Ident); ok {
					nome = id.Name + "." + x.Sel.Name
				}
			case *ast.Ident:
				nome = x.Name
			default:
				return true
			}
			for _, p := range proibidosChamada {
				if strings.TrimSuffix(p, "(") == nome || p == nome {
					t.Errorf("%s usa %q", completo.Name.Name, p)
				}
			}
			return true
		})
	}
	if visto == 0 {
		t.Fatal("nenhum arquivo de fonte foi examinado")
	}
}
