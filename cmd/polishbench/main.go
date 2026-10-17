// Command polishbench mede modelos de IA para o modo Polish e aplica a regra de
// escolha da ADR-016 (docs/ai-spec/19-eval-plan.md, "Polish").
//
// # Quem roda, e o que custa
//
// QUEM RODA É O MANTENEDOR, no terminal dele. O comando chama as APIs da OpenAI e da
// Anthropic com a chave que ele cola e GASTA DINHEIRO da conta dele (estimativa
// da rodada de 3 modelos com 3 rodadas: cerca de 1.160 chamadas (387 por modelo), abaixo de
// US$ 1). Por isso recusa rodar sem a flag -gastar, e nenhum agente nem
// nenhum go test o roda com chamada real: os testes usam só httptest.
//
// A chave é pedida por provedor, no terminal, com o eco desligado ("Chave
// OpenAI (Enter pula):"). Fica só na memória do processo: nunca em flag,
// variável de ambiente, arquivo, log ou relatório (docs/ai-spec/10-env-and-
// secrets.md), e o comando não toca no Gerenciador de Credenciais.
//
// # O comando exato (PowerShell 5.1, sem &&, a partir da raiz do repositório)
//
// Primeiro a ajuda, para conferir as flags e os preços padrão:
//
//	go run ./cmd/polishbench -help
//
// Depois a rodada, uma linha só:
//
//	go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5 -linha-de-base openai:gpt-5-nano@minimal
//
// -linha-de-base marca o candidato que só serve de referência (o gpt-5-nano, na
// ADR-016): é medido e julgado, e nunca vence. -conexao-nova mede a latência do
// pior caso (uma conexão nova a cada chamada); sem ela o p95 subestima o Agent
// com ditados espaçados por mais de 90 s.
//
// O esforço de raciocínio vai depois de "@" e só vale na OpenAI: sem "@" o
// pedido manda "none"; "@minimal" manda minimal; "@omitir" tira o campo. Se um
// modelo recusar o esforço, o relatório diz (400 com param=reasoning...) e o
// candidato para depois de 3 recusas seguidas; rode de novo com outro esforço.
// Preços: -preco openai:gpt-6-luna=0.10/0.50 (US$ por 1 milhão de tokens, com ponto decimal); os
// padrões da pesquisa de 29-set-2026 estão no -help.
//
// # O diagnóstico com o histórico (-historico)
//
// Para descobrir POR QUE a guarda de palavras recusa o Polish nos ditados
// longos (o log do Agent só guarda o motivo, nunca o texto), o comando lê os
// ditados do próprio histórico, só para leitura, e mostra no terminal o que o
// modelo mudou em cada resposta recusada, palavra por palavra, com a categoria
// (contração, acento, palavra de fala apagada, repetição, troca...):
//
//	go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna -historico local
//
// "local" é o historico.db do Agent instalado (%LOCALAPPDATA%\AuroraWhisper);
// um caminho também vale. Entram os ditados do modo smart (a entrada exata do
// Polish), com -min-caracteres ou mais (500), os -max-ditados mais recentes
// (15). O texto dos ditados aparece SÓ no terminal: nesse modo o comando não
// grava relatório nem arquivo nenhum, e o que sai da máquina é o mesmo que o
// Polish do Agent já manda ao provedor.
//
// A rodada demora: as chamadas são sequenciais (a latência é uma das métricas),
// 387 por modelo com -rodadas 3. Ctrl+C interrompe e grava o relatório
// do que foi medido, sem decisão.
//
// # O que ele faz
//
//	dataset -> resolver.Polir (Polidor de internal/ia + as guardas) -> métricas -> regra -> relatório
//
// O dataset é testdata/polish-dataset/frases.json (frases sintéticas) mais as
// `reference` do manifest do dataset de ASR (categoria ja_organizada). Cada
// frase roda pelo MESMO caminho do Agent; a única diferença é o prazo: cada
// chamada roda com um teto de 10 s, e o relatório registra as que passaram de
// 3,5 s (o prazo do Agent), porque medir só até 3,5 s esconderia o quanto elas
// demorariam.
//
// # As métricas, e por que a auditoria é separada das guardas
//
// A falha crítica é medida ANTES da guarda (o que o modelo devolveu) e DEPOIS
// (o que o Polir aceitou, o que chegaria ao campo). A auditoria (critica.go)
// compara a saída com o `esperado` do dataset sem chamar nada do resolver: se
// usasse as próprias guardas, o "depois" daria 0 sempre. Um caso "depois da
// guarda" é um buraco na guarda e é o achado mais importante da rodada.
//
// A regra de escolha (decisao.go): passa quem tem falha crítica depois da
// guarda = 0, fallback <= 5% e p95 morno <= 2,5 s; entre os que passam, vence o
// mais barato; no empate, o de maior F1 de pontuação; sai um vencedor por
// provedor. O vencedor é só uma sugestão pré-selecionada na tela: nunca é
// obrigatório, e o Smart segue padrão. A regra aprovada não tem piso de
// organização (um modelo que só devolve a entrada passa): o relatório avisa
// quando o vencedor organiza pouco, e -f1-min e -estrutura-min ligam o piso, se
// o mantenedor decidir.
package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer parar()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, depsReais()))
}
