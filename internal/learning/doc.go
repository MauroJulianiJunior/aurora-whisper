// Package learning implementa o fluxo de aprendizado por correção
// (docs/ai-spec/07-flows.md, "Learning"; TASK-019,
// docs/ai-spec/13-implementation-plan.md).
//
// # Fluxo
//
//  1. usuário edita o final_text no histórico;
//  2. a edição é salva (fora deste pacote — internal/storage);
//  3. Classify calcula o diff entre o texto original e o editado e
//     devolve uma Suggestion, já qualificada como elegível ou não a
//     virar alias de dicionário;
//  4. a UI mostra a Suggestion e pede confirmação explícita ao usuário;
//  5. só então Confirm é chamado, com uma Confirmation obtida de
//     ConfirmedByUser — o único jeito de produzir uma confirmação "sim"
//     (ver confirm.go);
//  6. Confirm grava a Correction e, quando a Suggestion é elegível,
//     tenta também criar/atualizar o DictionaryAlias correspondente;
//  7. sync (fora deste pacote — internal/sync) propaga a Correction.
//
// # Sugestão a partir da edição do painel (etapa 3)
//
// Sugerir (sugestao.go) é o motor do passo 3 quando a edição vem do painel.
// Ao contrário de Classify, que compara dois textos, ele recebe três: o
// bruto do ASR, o texto final de antes da edição e o de depois. Compara
// antes e depois palavra a palavra (sem maiúscula, acento nem pontuação),
// alinha cada troca com o bruto para achar a forma que o reconhecimento de
// voz escreveu, e devolve até duas trocas curtas de termo, ou nenhuma.
//
// É função pura: não grava nada, não lê histórico (o contador de ocorrências
// é injetado) e só devolve códigos de motivo, nunca texto, para o registro de
// funcionamento. O que grava é o passo 6, depois do clique. Classify
// continua sendo o que o cmd/dictlearn usa, sem mudança.
//
// # AC-022 — aprendizado exige confirmação
//
// Nada neste pacote grava dado sem uma Confirmation "sim". Confirmation
// tem seu único campo booleano não exportado: a única forma de obter um
// valor "confirmado" é chamar ConfirmedByUser, no ponto exato em que a UI
// recebeu o "sim" do usuário. O zero-value de Confirmation (variável
// esquecida, struct zerada, parâmetro não passado por engano) é sempre
// "não confirmado" — ao contrário de um bool opcional, cujo default
// silencioso poderia, por um erro de outro desenho, ser lido como "sim".
// Confirm verifica isso antes de tocar em qualquer repositório e devolve
// ErrNotConfirmed caso contrário.
//
// # Critério de "pequena e mapeável" (elegibilidade a alias)
//
// docs/ai-spec/07-flows.md exige que "edição grande nunca vire alias
// silenciosamente" e docs/ai-spec/16-ai-behavior-spec.md ("Dictionary")
// proíbe substituir "distantemente" só porque um termo existe. Este
// pacote traça uma linha explícita, em Classify (diff.go):
//
// Uma edição é elegível a alias (Suggestion.AliasEligible) somente
// quando TODAS as condições abaixo são verdadeiras:
//
//   - nem o texto original nem o editado passam de maxAliasChars runes
//     (30) — bloqueia frases longas reescritas, mesmo sem pontuação de
//     frase;
//   - nem o texto original nem o editado passam de maxAliasWords
//     palavras (4) — mesmo racional, medido em tokens;
//   - nenhum dos dois contém pontuação de fim de frase (. ! ? ;) — sinal
//     de que o texto é uma frase corrida, não um termo/expressão curta.
//
// Qualquer edição real (não vazia, não idêntica, não só espaço/pontuação)
// que viole uma dessas condições ainda pode virar uma Correction comum
// (Suggestion.Suggestible fica true — ver diff.go), só nunca um alias:
// "reescrever a frase inteira" sempre produz no máximo uma Suggestion,
// nunca uma com AliasEligible=true.
//
// Limites conhecidos: os limiares são um corte deliberadamente
// conservador, não uma análise semântica — uma edição curta mas sem
// relação real entre "antes" e "depois" (ex.: trocar um termo por outro
// completamente diferente, ambos curtos) ainda passa nesse critério.
// Isso é aceito porque o risco assimétrico é o oposto: um alias novo só
// existe depois de uma confirmação explícita do próprio usuário (AC-022),
// então o pior caso é um alias correto-mas-desnecessário, nunca um
// aprendido sem consentimento.
package learning
