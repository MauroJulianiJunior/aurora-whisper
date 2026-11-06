//go:build !race

package storage_test

// fatorDeTempo alonga os prazos dos testes de tempo sob o detector de corrida,
// que deixa tudo de 5 a 10 vezes mais lento (no runner do CI, mais ainda): o
// prazo continua pegando o que trava, mas não o que só ficou mais lento.
const fatorDeTempo = 1
