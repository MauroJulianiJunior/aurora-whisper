// Package conflito detecta outros programas de ditado rodando na
// máquina, para o Agent avisar antes de a pessoa descobrir sozinha.
//
// # Por que isto existe
//
// Em 27-set-2026 Mauro passou uma noite investigando "texto duplicado".
// Os números internos batiam em todas as etapas — o ASR devolvia 20
// runas, o inserter injetava 20, o histórico registrava 20 —, e mesmo
// assim o campo recebia o texto duas vezes.
//
// A causa era o Wispr Flow rodando em segundo plano, esquecido ligado
// depois de um teste, usando O MESMO atalho. Os dois gravavam, os dois
// transcreviam, os dois inseriam.
//
// O sintoma é péssimo de diagnosticar justamente porque tudo do lado de
// cá está correto: não há erro, não há log, não há nada fora do lugar.
// Quem instalar este produto um dia vai passar pelo mesmo — e sem o log
// que Mauro tinha.
//
// # O que este pacote NÃO faz
//
// Não mata processo, não muda atalho, não impede a subida. Dois
// programas de ditado no mesmo atalho podem ser intencional (comparar
// os dois lado a lado é exatamente o que Mauro estava fazendo). A
// decisão é de quem usa; o trabalho daqui é que ela seja informada.
package conflito

import (
	"sort"
	"strings"
)

// Programa é um software de ditado que compete pelo mesmo gesto.
type Programa struct {
	// Nome é como a pessoa conhece o programa.
	Nome string
	// Executaveis são nomes de processo, em minúsculas e sem caminho.
	Executaveis []string
	// Atalho é o gesto que ele usa por padrão, quando conhecido — é o
	// que torna o aviso acionável em vez de genérico.
	Atalho string
}

// Conhecidos são os programas de ditado que sabemos competir pelo mesmo
// gesto.
//
// A lista é curta de propósito: um falso positivo aqui é um aviso
// assustando quem não tem problema nenhum, e isso gasta a credibilidade
// do aviso para quando ele importar.
//
// Fica no código, e não em configuração, porque é conhecimento sobre o
// ecossistema — não preferência de quem usa.
var Conhecidos = []Programa{
	{
		Nome:        "Wispr Flow",
		Executaveis: []string{"wispr flow.exe", "wisprflow.exe"},
		Atalho:      "Ctrl+Win",
	},
	{
		Nome:        "Dragon NaturallySpeaking",
		Executaveis: []string{"natspeak.exe", "dragonbar.exe"},
	},
	{
		Nome:        "Windows Voice Access",
		Executaveis: []string{"voiceaccess.exe"},
	},
	{
		Nome:        "Talon",
		Executaveis: []string{"talon.exe"},
	},
}

// Detectar devolve quais programas conhecidos aparecem na lista de
// processos, sem repetir.
//
// A comparação ignora caixa porque o Windows não a distingue em nome de
// arquivo, e ignora o caminho porque o mesmo programa pode estar
// instalado em lugares diferentes.
//
// Recebe a lista pronta, em vez de enumerar ela mesma, para a regra ser
// testável no runner Linux do CI — a enumeração é que precisa de
// Windows (ver conflito_windows.go), e é só transporte.
func Detectar(processos []string) []Programa {
	presentes := make(map[string]bool, len(processos))
	for _, p := range processos {
		presentes[normalizar(p)] = true
	}

	var achados []Programa
	for _, prog := range Conhecidos {
		for _, exe := range prog.Executaveis {
			if presentes[normalizar(exe)] {
				achados = append(achados, prog)
				break
			}
		}
	}
	// Ordem determinística: o aviso não pode mudar de forma entre duas
	// subidas sem nada ter mudado na máquina.
	sort.Slice(achados, func(i, j int) bool { return achados[i].Nome < achados[j].Nome })
	return achados
}

// normalizar reduz um nome de processo à forma comparável: minúsculas,
// sem espaço nas bordas, sem caminho, e com ".exe" garantido.
//
// Sem o sufixo, "Wispr Flow" (como o Get-Process reporta) não casaria
// com "wispr flow.exe" (como o executável se chama) — e foi exatamente
// assim que o processo apareceu na máquina de Mauro.
func normalizar(nome string) string {
	n := strings.ToLower(strings.TrimSpace(nome))
	if i := strings.LastIndexAny(n, `\/`); i >= 0 {
		n = n[i+1:]
	}
	if !strings.HasSuffix(n, ".exe") {
		n += ".exe"
	}
	return n
}

// Aviso monta a mensagem de log para um programa detectado. Fora do
// Detectar para a frase ficar num lugar só, e testável.
func (p Programa) Aviso() string {
	var b strings.Builder
	b.WriteString(p.Nome)
	b.WriteString(" está rodando")
	if p.Atalho != "" {
		b.WriteString(" e usa o atalho ")
		b.WriteString(p.Atalho)
	}
	b.WriteString(". Se o atalho for o mesmo do ditado, OS DOIS vão gravar, ")
	b.WriteString("transcrever e inserir — e o texto aparece duplicado no campo.")
	return b.String()
}
