// Package fakes fornece um runtime síncrono e determinístico para o núcleo
// de ditado (internal/agent/dictation), junto com dublês configuráveis das
// portas que esse runtime consome: captura de áudio no modo "sempre pronto",
// com pre-roll (ADR-007, seção 4; ADR-015), ASR, resolução e inserção de
// texto.
//
// Nada aqui cria goroutine, faz sleep ou depende do relógio de parede: tudo
// é orquestrado através de um internal/clock.Fake compartilhado, para que
// os testes de aceite (internal/agent/dictation/acceptance) sejam
// reproduzíveis e possam exercitar ordenações críticas (ex.: watchdog vs.
// resposta) sem polling.
package fakes
