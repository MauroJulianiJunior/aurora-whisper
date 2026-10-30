package deploy

import (
	"strings"
	"testing"
)

// O -ArquivoPolitica do enviar-para-servidor.ps1 (docs/deploy-privacidade.md):
// a cópia preenchida da política tem o nome e o e-mail do responsável, então
// fica fora do repositório (que é público), e o script aplica as mesmas regras
// do servidor antes de mandá-la para recebido/, de onde o subir.sh a leva a
// politica/. Sem o parâmetro, nada muda na política da VPS.
func TestPC2_EnviarAPoliticaDePrivacidade(t *testing.T) {
	c := codigo(ler(t, enviar))
	exigirContem(t, enviar, c,
		"[string]$ArquivoPolitica = ''",
		"$MaxPolitica = 98304",
		// fora do repositório (o GitHub é público)
		"if (Dentro-De $PoliticaCheia $Raiz) {",
		// as regras do servidor: tamanho, UTF-8 estrito, vazio, campo do
		// modelo (as duas formas, e a nota do topo) e caractere de controle
		"$bytesDaPolitica.Length -gt $MaxPolitica",
		"New-Object System.Text.UTF8Encoding($false, $true)",
		"$textoDaPolitica.Trim().Length -eq 0",
		"$textoDaPolitica.IndexOf('[A PREENCHER', [StringComparison]::Ordinal) -ge 0",
		`'[\x00-\x08\x0B\x0C\x0E-\x1F\x7F-\x9F]'`,
		// o destino que o subir.sh lê
		"($Servidor + ':aurora-whisper/recebido/politica-de-privacidade.md')",
		"if ($PoliticaCheia) {",
	)
	// O texto da política nunca vai para a tela.
	for _, l := range linhasDeCodigo(ler(t, enviar)) {
		if (strings.HasPrefix(l.texto, "Write-Host") || strings.HasPrefix(l.texto, "Ok ") || strings.HasPrefix(l.texto, "Falhar ")) &&
			strings.Contains(l.texto, "$textoDaPolitica") {
			t.Errorf("linha %d mostra o texto da política: %s", l.n, l.texto)
		}
	}
	// O destino é o RECEBIDO_POLITICA do subir.sh, e a ajuda descreve o parâmetro.
	exigirContem(t, subirSh, codigo(ler(t, subirSh)), `RECEBIDO_POLITICA="$BASE/recebido/politica-de-privacidade.md"`)
	exigirContem(t, enviar, ler(t, enviar), ".PARAMETER ArquivoPolitica", "-ArquivoPolitica C:\\Users\\voce\\Documents")
}
