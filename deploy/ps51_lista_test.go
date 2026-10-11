package deploy

import (
	"path/filepath"
	"regexp"
	"testing"
)

// No PowerShell 5.1, "return @(...)" devolve a lista desembrulhada: com um
// item só, quem chama recebe um texto solto, e o .Count dele quebra no
// StrictMode ("A propriedade 'Count' não foi encontrada neste objeto"). Foi o
// FALHOU do deploy-teste.ps1 na primeira rodada do Mauro (09-out-2026), na
// conferência dos cabeçalhos. Toda função dos scripts do PC que devolve uma
// lista usa a vírgula na frente: "return ,@(...)".
func TestPC_FuncaoQueDevolveListaNaoADesembrulha(t *testing.T) {
	scripts, err := filepath.Glob(caminho("scripts", "*.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("nenhum script .ps1 em scripts/")
	}
	desembrulha := regexp.MustCompile(`\breturn\s+@\(`)
	for _, s := range scripts {
		rel, err := filepath.Rel(caminho(), s)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range linhasDeCodigo(ler(t, filepath.ToSlash(rel))) {
			if desembrulha.MatchString(l.texto) {
				t.Errorf("%s:%d devolve a lista sem a vírgula (use \"return ,@(...)\"): %s", rel, l.n, l.texto)
			}
		}
	}
}
