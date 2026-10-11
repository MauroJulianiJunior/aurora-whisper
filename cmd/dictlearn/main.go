// Command dictlearn fecha o laço de aprendizado: recebe o que o ditado
// produziu e o que deveria ter produzido, classifica a edição, pede
// confirmação e — só então — grava o alias no dicionário pessoal.
//
// # Por que ele existe
//
// Medido em 27-set-2026 (docs/sprint-5-portao-de-entidades.md): com os
// alias aprendidos, a Entity Accuracy é 95,9%; para um erro NUNCA VISTO,
// 70,8%. A diferença entre os dois números é exatamente o valor deste
// laço.
//
// O produto não nasce em 95,9%. Ele chega lá, para cada pessoa, na
// medida em que ela o corrige — e até agora não havia nenhum jeito de
// corrigi-lo a não ser editar o JSON à mão.
//
// # O que este comando NÃO é
//
// O fluxo da spec (07-flows.md, "Learning") passa pelo histórico: a
// pessoa edita o final_text no dashboard, e a edição vira Suggestion.
// Isso exige o Agent persistir os ditados e servir a API local — duas
// coisas que ele ainda não faz.
//
// Este comando é a menor versão utilizável do mesmo laço, usando o MESMO
// internal/learning: a garantia do AC-022 (nada é gravado sem
// confirmação explícita) é a mesma, porque é estrutural, não uma
// disciplina que cada chamador precisa lembrar.
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
