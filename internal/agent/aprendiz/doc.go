// Package aprendiz é o dicionario.json do Agent como o painel e o sync o
// usam (ADR-019, "Decisões da F8", F8g): o Aprender e o Desfazer do painel
// (etapa 3, ADR-017), o "Adicionar termo" (ADR-020) e o aplicador do
// aprendido que chega da nuvem (o lote do PC que recebe, na varredura e no
// Receber do sync), todos sob UMA trava, com uma gravação e uma troca do
// dicionário e do prompt no runtime por operação.
//
// Saiu do package main (cmd/agent) para a ponta a ponta com dois PCs usar o
// aplicador de verdade. O que depende do runtime do Windows (o
// winruntime.Dicionario, o Polish) fica no Agent, atrás de Config.Trocar.
//
// # As conferências moram em outro pacote
//
// As que o dicionário sozinho não sabe fazer (a montagem do Smart, o
// Validar, o "agiria" do Aprender e o do par recebido, os domínios da
// conferência e a palavra comum) saíram daqui para internal/smartconta (ADR-021,
// C2), um pacote puro que o servidor também usa para montar o Smart da conta.
// O Aplicador as importa de lá e não muda de comportamento: é o mesmo código,
// no mesmo caminho.
//
// # Nada de texto em log
//
// Só contagens e códigos de listas fechadas. A variante, o termo e o domínio
// são texto que a pessoa ditou: nunca vão a log (CLAUDE.md, "Safety rules").
package aprendiz
