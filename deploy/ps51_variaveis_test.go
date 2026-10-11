package deploy

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No PowerShell, o nome da variável não diferencia maiúsculas: $Aprovado e
// $aprovado são a mesma. No deploy-teste.ps1, o caminho do arquivo das
// imagens aprovadas ($Aprovado) foi sobrescrito por "passou ou não"
// ($aprovado = $false), e a gravação no fim caiu num caminho vazio (o FALHOU
// da segunda rodada do Mauro, 09-out-2026). Regra: uma variável atribuída no
// nível do script (na coluna 0) não pode ser atribuída em nenhum outro lugar
// com outra grafia. Os parâmetros de função ficam de fora: são locais.
func TestPC_VariavelDoScriptNaoTemOutraGrafia(t *testing.T) {
	scripts, err := filepath.Glob(caminho("scripts", "*.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("nenhum script .ps1 em scripts/")
	}
	atribuicao := regexp.MustCompile(`^(\s*)\$([A-Za-z_][A-Za-z0-9_]*)\s*(?:\+|-)?=[^=]`)
	for _, s := range scripts {
		rel, err := filepath.Rel(caminho(), s)
		if err != nil {
			t.Fatal(err)
		}
		texto := ler(t, filepath.ToSlash(rel))
		type uso struct {
			grafia string
			n      int
		}
		doScript := map[string]uso{}
		var todas []uso
		for i, l := range strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			m := atribuicao.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			u := uso{grafia: m[2], n: i + 1}
			todas = append(todas, u)
			if m[1] == "" {
				if _, ja := doScript[strings.ToLower(u.grafia)]; !ja {
					doScript[strings.ToLower(u.grafia)] = u
				}
			}
		}
		for _, u := range todas {
			primeira, ok := doScript[strings.ToLower(u.grafia)]
			if ok && primeira.grafia != u.grafia {
				t.Errorf("%s:%d atribui $%s, e a linha %d atribui $%s no nível do script: no PowerShell são a mesma variável",
					rel, u.n, u.grafia, primeira.n, primeira.grafia)
			}
		}
	}
}
