package rss

// processCreatedAtFunc devolve o instante em que pid nasceu, numa unidade
// opaca só comparável entre si (quanto maior, mais recente — no Windows
// real isso vem de GetProcessTimes, ver rss_windows.go). ok é false
// quando o PID não existe mais ou não pôde ser consultado.
type processCreatedAtFunc func(pid int) (createdAt int64, ok bool)

// sampleFunc mede o RSS de um único PID. Em produção é a mesma sample()
// que Sample usa (GetProcessMemoryInfo no Windows); em teste, um dublê
// síncrono sobre um mapa.
type sampleFunc func(pid int) (bytes int64, ok bool)

// sumTree soma o RSS de root e de todos os seus descendentes genuínos.
//
// children é a topologia pid → filhos diretos, vinda de um único
// snapshot barato (ex.: CreateToolhelp32Snapshot no Windows — nenhum
// handle por processo). createdAt e sample só são chamados para os PIDs
// que a travessia realmente visita, nunca para todo processo do sistema:
// createdAt custa um OpenProcess+GetProcessTimes por chamada no Windows
// real, e isso roda a cada rssSampleInterval.
//
// "Genuíno" descarta duas armadilhas de topologia de processo:
//
//   - Reaproveitamento de PID. O Windows recicla PIDs. children é
//     montado a partir do PPID que cada processo declara — um número que
//     o SO nunca revalida depois que o pai original sai. Se um processo P
//     morre e o SO reaproveita o PID de P para o nosso root, uma entrada
//     antiga do snapshot cujo PPID aponte para esse PID ainda aparece em
//     children[root], mas não é filho do nosso root — é filho do P
//     antigo, que já não existe. Um filho de verdade nasce DEPOIS do pai;
//     por isso, antes de descer numa aresta pai→filho, sumTree exige
//     createdAt(filho) >= createdAt(pai). Se o "filho" for mais velho, o
//     PPID é lixo e a aresta é ignorada — sem isso, um processo antigo e
//     sem relação nenhuma com o candidato medido entraria na soma.
//   - Ciclos. Um mapa children corrompido (ou, em teste, deliberadamente
//     malicioso) não pode causar recursão infinita: visited garante que
//     cada PID entra na soma no máximo uma vez, mesmo que apareça como
//     filho de mais de um nó ou feche um laço de volta a um ancestral.
//
// Falha parcial: se createdAt ou sample falhar para QUALQUER nó da árvore
// genuína — incluindo a própria raiz —, sumTree devolve (0, false) para a
// chamada inteira, em vez de somar só o que deu. A alternativa (somar
// parcial) produziria um total que exclui silenciosamente um
// descendente — podendo ser exatamente o processo que segura o modelo —,
// e um número menor que parece uma medição real é o defeito que este
// pacote existe para evitar (ADR-012, "o que o benchmark não pode
// fazer"). Quem chama (SampleTree, e o watcher periódico de
// internal/benchmark/candidate) já trata ok == false como "esta amostra
// não conta", nunca como erro fatal — a próxima amostra, rssSampleInterval
// depois, tenta de novo.
func sumTree(root int, children map[int][]int, createdAt processCreatedAtFunc, sample sampleFunc) (bytes int64, ok bool) {
	rootBorn, ok := createdAt(root)
	if !ok {
		return 0, false
	}

	visited := make(map[int]bool)
	var total int64

	var walk func(pid int, bornAt int64) bool
	walk = func(pid int, bornAt int64) bool {
		if visited[pid] {
			return true // já contado — ciclo ou PID listado como filho de mais de um nó, não é erro
		}
		visited[pid] = true

		b, ok := sample(pid)
		if !ok {
			return false
		}
		total += b

		for _, child := range children[pid] {
			childBorn, ok := createdAt(child)
			if !ok {
				return false
			}
			if childBorn < bornAt {
				continue // reaproveitamento de PID: o "filho" nasceu antes do pai, o PPID é lixo
			}
			if !walk(child, childBorn) {
				return false
			}
		}
		return true
	}

	if !walk(root, rootBorn) {
		return 0, false
	}
	return total, true
}
