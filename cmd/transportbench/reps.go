package main

// Repetições padrão de cada modo. ESCOLHA DO AUTOR DESTE BENCHMARK, NÃO
// MEDIÇÃO: cold gasta dezenas de milissegundos por repetição (start de
// processo), então um N menor já estabiliza p50/p95 sem tornar a
// execução inteira lenta; warm e o controle in-process são baratos por
// repetição (uma ida-e-volta já viva, ou nem isso), então usam N maior
// para o mesmo orçamento de tempo total. cli.go imprime estas contagens
// na própria saída do comando — nunca escondidas atrás de "medimos o
// suficiente".
const (
	defaultColdReps    = 20
	defaultWarmReps    = 200
	defaultControlReps = 2000
)

// repsFor decide quantas repetições usar para mode, dado um override do
// usuário (override <= 0 significa "sem override", usa o default de
// mode). Lógica pura — só decide um número, sem I/O — por isso é
// testável em qualquer SO sem build tag, junto com payload.go e
// table.go.
func repsFor(mode string, override int) int {
	if override > 0 {
		return override
	}
	switch mode {
	case "cold":
		return defaultColdReps
	case "warm":
		return defaultWarmReps
	case "control":
		return defaultControlReps
	default:
		return defaultWarmReps
	}
}
