// Package sysinput é o portão único por onde passa qualquer ação que atue
// no sistema real do usuário: injeção de tecla (SendInput) e escrita no
// clipboard.
//
// Ele RECUSA por padrão.
//
// # Por que existe
//
// Em 17-set-2026, durante a construção do Sprint 2, um agente rodou um
// teste que chamou SendInput de verdade. O texto foi digitado no Bloco de
// Notas do usuário e um modificador ficou preso, travando o teclado dele.
//
// A causa não foi desatenção de quem escreveu o teste. Foi ausência de
// trava: código que injeta entrada é perigoso por natureza numa máquina
// com desktop em uso, e a proteção não pode depender da disciplina de
// quem escreve o próximo teste.
//
// # As duas camadas
//
// A primeira é intransponível: quando o processo está rodando sob
// "go test", Guard recusa SEMPRE — mesmo que alguém tenha chamado Enable.
// Isso vem de testing.Testing(), que a stdlib expõe justamente para este
// tipo de interlock. Nenhum teste da suíte consegue injetar entrada, nem
// por engano nem de propósito.
//
// A segunda é o estado de habilitação: fora de teste, as ações reais
// continuam desligadas até que o binário do Agent chame Enable uma vez,
// na inicialização. Não há variável de ambiente nem arquivo de
// configuração que ligue isso — deliberadamente, para que não exista um
// caminho acidental.
//
// # Como testar o que fica atrás do portão
//
// A lógica de decisão — qual caminho de inserção escolher, qual o
// limiar, o que fazer quando um caminho falha, política de retry — é
// pura e fica FORA do portão, testada contra dublês. É onde moram os
// bugs interessantes de qualquer forma.
//
// A chamada de sistema em si não tem teste automatizado. Ela é verificada
// no roteiro de teste manual, por uma pessoa que sabe o que vai
// acontecer.
//
// Ver CLAUDE.md, seção "Testes que agem no sistema real", e
// docs/adr/ADR-011-insercao-e-campo-protegido.md, decisão 6.
package sysinput
