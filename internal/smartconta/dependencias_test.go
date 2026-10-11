package smartconta_test

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// O pacote é PURO (ADR-021, decisão F): compila no Linux, onde o servidor roda,
// e não puxa o storage (o SQLite do Agent), o sync, o Windows nem biblioteca de
// terceiros. Este teste prende a lista do que ele importa, de dentro do
// módulo: acrescentar um import é uma decisão, e quem a toma lê este arquivo.

const modulo = "github.com/MauroJulianiJunior/aurora-whisper/"

// permitidos são os pacotes do módulo que o Smart da conta pode importar,
// direta ou indiretamente.
var permitidos = map[string]bool{
	"internal/agent/dictation": true, // o resolver o usa (tipos do núcleo do ditado, sem Windows)
	"internal/asr":             true,
	"internal/asr/repeticao":   true,
	"internal/clock":           true,
	"internal/context":         true,
	"internal/dictionary":      true,
	"internal/ids":             true,
	"internal/learning":        true,
	"internal/resolver":        true,
	"internal/smartconta":      true,
}

func TestPacoteEPuro_CompilaNoLinuxESemStorageNemSyncNemWindows(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("sem o comando go no PATH")
	}
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", modulo+"internal/smartconta")
	// O alvo é o Linux, e sem cgo: o que compila aqui compila na VPS.
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	saida, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list para o Linux falhou (o pacote não compila lá?): %v", err)
	}

	var doModulo, deTerceiros []string
	for _, imp := range strings.Fields(string(saida)) {
		switch {
		case strings.HasPrefix(imp, modulo):
			doModulo = append(doModulo, strings.TrimPrefix(imp, modulo))
		case strings.Contains(strings.SplitN(imp, "/", 2)[0], "."):
			// A biblioteca padrão não tem ponto no primeiro elemento.
			deTerceiros = append(deTerceiros, imp)
		}
	}
	sort.Strings(doModulo)
	for _, imp := range doModulo {
		if !permitidos[imp] {
			t.Errorf("o Smart da conta importa %s, que não está na lista dos permitidos (storage, sync e Windows ficam de fora)", imp)
		}
	}
	for _, imp := range deTerceiros {
		t.Errorf("o Smart da conta importa a biblioteca de terceiros %s", imp)
	}
	if len(doModulo) == 0 {
		t.Fatal("go list não devolveu nenhum pacote do módulo: o teste não mediu nada")
	}

	// E compila de fato para o Linux: a lista de imports não pega um arquivo
	// _windows.go de que um pacote da cadeia depende.
	build := exec.Command("go", "build", modulo+"internal/smartconta")
	build.Env = cmd.Env
	if out, err := build.CombinedOutput(); err != nil {
		t.Errorf("go build para o Linux falhou: %v\n%s", err, out)
	}
}
