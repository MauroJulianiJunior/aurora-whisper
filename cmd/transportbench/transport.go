package main

import "time"

// roundTripper é o contrato comum aos três transportes medidos
// (pipe/tcp/http, um arquivo transport_*.go cada): como fazer cold (um
// processo novo + uma ida-e-volta + encerrar, medidos como UM número,
// nunca decompostos) e como abrir uma sessão warm.
type roundTripper interface {
	name() string

	// coldRoundTrip inicia um processo de eco novo (reexecutando exePath
	// com o subcomando oculto correspondente — ver echo_*.go), faz
	// exatamente uma ida-e-volta com payload e espera o processo
	// terminar. elapsed cobre do início do processo até ele sair —
	// exatamente o que ADR-012 chama de cold: "processo iniciado a cada
	// transcrição".
	coldRoundTrip(exePath string, payload []byte) (elapsed time.Duration, echoed []byte, err error)

	// startWarm inicia um processo de eco uma única vez (esse custo NÃO
	// entra em nenhuma amostra warm) e devolve uma sessão reutilizável.
	startWarm(exePath string) (warmSession, error)
}

// warmSession é uma sessão contra um processo de eco já vivo (ADR-012,
// modo warm): cada roundTrip mede só a ida-e-volta. close() encerra o
// processo e espera sua saída — chamado uma vez, fora da amostra warm.
type warmSession interface {
	roundTrip(payload []byte) (elapsed time.Duration, echoed []byte, err error)
	close() error
}
