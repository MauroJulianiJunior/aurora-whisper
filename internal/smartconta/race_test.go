//go:build race

package smartconta

// emRace diz que o teste roda com o detector de corrida, de 5 a 10 vezes mais
// lento: os testes de tempo não valem nele.
const emRace = true
