package deploy

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No PowerShell, o que um programa imprime dentro de uma função vira também
// valor devolvido por ela. No enviar-para-servidor.ps1, o Rodar-AVista rodava
// "& $Programa @Argumentos" e devolvia o $codigo: o "Loaded image: ..." do
// docker load voltava junto, a tela ficava vazia, e o "-ne 0" acusava falha
// com as imagens carregadas (a primeira rodada do Mauro, 10-out-2026). Regra:
// numa função que devolve um valor ("return $..."), a chamada de programa
// solta ("& ..." no começo da linha) passa pelo Out-Host, ou o resultado vai
// para uma variável.
func TestPC_FuncaoQueDevolveValorNaoEngoleASaida(t *testing.T) {
	scripts, err := filepath.Glob(caminho("scripts", "*.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("nenhum script .ps1 em scripts/")
	}
	inicio := regexp.MustCompile(`^function\s+[\w-]+`)
	devolve := regexp.MustCompile(`\breturn\s+\$`)
	chamada := regexp.MustCompile(`^&\s`)
	for _, s := range scripts {
		rel, err := filepath.Rel(caminho(), s)
		if err != nil {
			t.Fatal(err)
		}
		linhas := strings.Split(strings.ReplaceAll(ler(t, filepath.ToSlash(rel)), "\r\n", "\n"), "\n")
		for i := 0; i < len(linhas); i++ {
			if !inicio.MatchString(linhas[i]) {
				continue
			}
			// O corpo vai até a primeira "}" na coluna 0.
			fim := i + 1
			for fim < len(linhas) && !strings.HasPrefix(linhas[fim], "}") {
				fim++
			}
			corpo := linhas[i+1 : fim]
			temReturn := false
			for _, l := range corpo {
				if devolve.MatchString(l) {
					temReturn = true
				}
			}
			if temReturn {
				for k, l := range corpo {
					l = strings.TrimSpace(l)
					if chamada.MatchString(l) && !strings.Contains(l, "| Out-Host") {
						t.Errorf("%s:%d chama um programa solto numa função que devolve valor (a saída volta junto; use \"| Out-Host\"): %s",
							rel, i+2+k, l)
					}
				}
			}
			i = fim
		}
	}
}
