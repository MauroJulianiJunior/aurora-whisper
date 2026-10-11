// Package smartconta é o Smart da conta: o pacote puro que monta, a partir do
// aprendido que a nuvem guarda, o mesmo Smart que um PC montaria, e que o
// Windows Agent e o servidor usam (ADR-021, decisão F e subfatia C2).
//
// É PURO de propósito: compila no Linux, onde o servidor roda, e não importa o
// storage (o SQLite do Agent), o sync, o Windows nem biblioteca de terceiros.
// O teste dependencias_test.go prende a lista do que ele importa.
//
// # O que há aqui
//
//   - As conferências do lote, que moravam em internal/agent/aprendiz e que o
//     Agent passou a importar daqui sem mudar de comportamento (conferir.go):
//     SmartDaSemente, Validar, Agiria, ValidarPar, AgeEm, AgeNoDominio,
//     DominiosDaConferencia, IgualNoDicionario, PalavraComum e Conferencias.
//   - O Smart da conta: Montar monta a Conta do aprendido (conta.go), e
//     Conta.Resolver passa o texto do ASR pelo Colapsar (internal/asr/repeticao)
//     e pelo Smart, como o ditado do PC.
//   - A dica do aprendido para o provedor de transcrição (dica.go), desligada por
//     padrão (DicaLigadaPorPadrao) até a C8 medir.
//   - O Cache da Conta montada, por conta (cache.go).
//
// # O mesmo caminho do PC
//
// Montar aplica os pares com Semente.AplicarPares e as MESMAS conferências do PC
// que recebe o aprendido da nuvem (Conferencias), numa semente vazia, e monta o
// Smart da semente que sobra. Os mesmos pares e o mesmo texto dão o mesmo
// resultado que no Smart de um PC de dicionário vazio (paridade_test.go, que usa
// o Aplicador do Agent de verdade). A única diferença é de velocidade: o
// "agiria" do lote acompanha a semente em vez de montar um Smart novo a cada par
// (acompanhante.go), e um teste de equivalência exige as mesmas respostas.
//
// O que o celular não tem, e por quê:
//
//   - o par preso a um domínio não age: o PWA não sabe em que programa o texto
//     vai entrar, e a troca presa a um programa valeria em todos. É fail-closed,
//     de propósito (Resumo.ComDominio);
//   - o par desfeito na nuvem não age (Resumo.Desfeitos);
//   - o par ausente não existe para o Smart, e o texto sem par passa igual;
//   - os termos do dicionario.json de cada PC (os de antes, os adicionados e as
//     dicas deles) ficam no PC, e o texto do celular pode sair pior que o do PC
//     num termo que só o PC conhece.
//
// # Desempenho (medido em 10-out-2026)
//
// A meta da ADR era montar e resolver 500 pares em menos de 25 ms. Resolver com o
// Smart pronto cabe (uns 4 ms, para um ditado de 40 palavras). Montar do zero não
// cabe (cerca de 1 s com 500 pares, e cerca de 50 ms com 100): o lote do PC confere
// um par de cada vez sobre a semente acumulada, e a paridade exige esse caminho.
// Por isso há o Cache, o "cache por conta" que a ADR previa para esse caso: a
// montagem custa uma vez por versão do aprendido. Antes dos ajustes de
// dictionary (o índice da busca em memória e o Normalize rápido), montar levava
// 21 s e resolver 818 ms.
//
// # Nada de texto em log nem em erro
//
// A variante, o termo e o domínio são texto que a pessoa ditou: este pacote não
// loga nada, e os erros que devolve são fixos (ErrMontar, ErrSemUsuario). Só há
// contagens e códigos de listas fechadas (Resumo).
//
// # A dica diverge do PC, de propósito
//
// No PC, o termo aprendido é AliasOnly e fica fora do prompt do ASR (ADR-017;
// ADR-020, decisão 3). Na nuvem não há outra fonte de dica, e por isso a Dica leva
// os termos do aprendido, no mesmo orçamento de 140 runas do PC
// (dictionary.Vocabulario). No PC nada muda.
package smartconta
