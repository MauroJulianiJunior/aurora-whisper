// Package acceptance traduz os critérios de aceite de
// docs/ai-spec/03-acceptance-criteria.md em testes executáveis contra o
// núcleo de ditado (internal/agent/dictation), orquestrado pelo runtime
// síncrono de internal/agent/dictation/fakes.
//
// Cada teste nomeia o critério que cobre e cita, em comentário, o texto do
// critério na spec. A suíte não usa sleep nem polling: toda passagem de
// tempo é feita via clock.Fake.Advance, e toda resposta de porta (áudio,
// ASR, resolução, inserção) é entregue de forma síncrona pelos dublês do
// pacote fakes.
package acceptance
