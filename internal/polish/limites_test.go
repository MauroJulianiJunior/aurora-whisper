package polish

// Os limites do pacote (ADR-021, C5a): ele é comum ao PC e ao servidor, que
// roda em Linux. Estes testes travam o que o faz compilar e carregar lá:
// nenhum arquivo preso a um sistema, nenhum import de Windows, de sistema de
// entrada ou do Agent além do núcleo puro do ditado.

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const modulo = "github.com/MauroJulianiJunior/aurora-whisper"

// importProibido diz se um import quebra a regra do pacote: nada de Agent
// (menos o núcleo puro do ditado), de binário ou de Windows. Com direto,
// vale também para o que o próprio código do pacote importa: nada de chamada
// de sistema nem de processo (a biblioteca padrão os carrega por baixo, e
// isso não é da conta deste pacote).
func importProibido(caminho string, direto bool) bool {
	if caminho == modulo+"/internal/agent/dictation" {
		return false
	}
	switch {
	case strings.HasPrefix(caminho, modulo+"/internal/agent"),
		strings.HasPrefix(caminho, modulo+"/cmd/"),
		strings.HasPrefix(caminho, "golang.org/x/sys"):
		return true
	}
	return direto && (caminho == "syscall" || caminho == "unsafe" || caminho == "os/exec")
}

// arquivosDeProducao lista os .go do pacote que não são de teste.
func arquivosDeProducao(t *testing.T) []string {
	t.Helper()
	entradas, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var arqs []string
	for _, e := range entradas {
		if n := e.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			arqs = append(arqs, n)
		}
	}
	if len(arqs) < 4 {
		t.Fatalf("achei só %v: a conferência não valeu", arqs)
	}
	return arqs
}

// TestPacote_SemBuildTagNemImportDeAgentOuWindows: o código de produção do
// pacote não tem restrição de sistema (nem no nome do arquivo, nem em
// //go:build) e só importa o que o servidor pode carregar.
func TestPacote_SemBuildTagNemImportDeAgentOuWindows(t *testing.T) {
	sufixosDeSistema := []string{"_windows", "_linux", "_darwin", "_unix", "_other", "_amd64", "_arm64"}
	fset := token.NewFileSet()
	for _, nome := range arquivosDeProducao(t) {
		base := strings.TrimSuffix(nome, ".go")
		for _, s := range sufixosDeSistema {
			if strings.HasSuffix(base, s) {
				t.Errorf("%s: o nome restringe o arquivo a um sistema (%s)", nome, s)
			}
		}
		f, err := parser.ParseFile(fset, nome, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", nome, err)
		}
		for _, g := range f.Comments {
			if g.Pos() > f.Package {
				break
			}
			for _, c := range g.List {
				if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
					t.Errorf("%s: restrição de build (%s)", nome, c.Text)
				}
			}
		}
		for _, imp := range f.Imports {
			caminho, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if importProibido(caminho, true) {
				t.Errorf("%s importa %s, que o servidor não pode carregar", nome, caminho)
			}
		}
	}
}

// TestPacote_ODesenhoDoLinuxCarregaSemAgentNemWindows: o grafo de
// dependências do pacote para GOOS=linux resolve (nenhum pacote sem arquivo
// para o sistema) e não traz nada do Agent além do núcleo do ditado, nem
// Windows. É o "go list" do próprio repositório, sem compilar nada: o
// go vet e o go build para Linux ficam na linha de comando da subfatia.
func TestPacote_ODesenhoDoLinuxCarregaSemAgentNemWindows(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("sem o go no PATH")
	}
	cmd := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", ".")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list para linux: %v\n%s", err, stderr.String())
	}
	vistos := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		vistos[l] = true
		if importProibido(l, false) {
			t.Errorf("para linux, o pacote carrega %s", l)
		}
	}
	// A conferência só vale se listou o que o pacote usa de verdade.
	for _, quero := range []string{
		modulo + "/internal/polish",
		modulo + "/internal/ia",
		modulo + "/internal/resolver",
		modulo + "/internal/agent/dictation",
	} {
		if !vistos[quero] {
			t.Errorf("o go list para linux não trouxe %s: a conferência não valeu", quero)
		}
	}
}
