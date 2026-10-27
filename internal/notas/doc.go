// Package notas é a parte pura das notas que o PC e a nuvem dividem (ADR-022):
// a forma em que se compara o texto (Dobrar), a ordem manual (o índice
// fracionário, PosicaoEntre; N2b) e a regra do "ditar para nota" (Escolher,
// Acrescentar; N9 no PC, N8 no celular).
//
// Nada aqui toca banco, rede, teclado ou log. O PC usa este pacote pelo
// internal/storage e pelo internal/agent/paranota; a nuvem, pelo
// internal/server/cloudstore. Antes da N8, estas funções moravam no
// internal/storage e no internal/agent/paranota: vieram para cá para o servidor
// usar a mesma regra sem importar o armazenamento do PC nem duplicá-la.
//
// # Privacidade
//
// O texto da nota e o ditado são conteúdo da pessoa: nada deste pacote os
// escreve em log, e quem chama loga só números.
package notas
