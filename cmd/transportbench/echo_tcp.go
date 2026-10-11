package main

import (
	"fmt"
	"io"
	"net"
)

// echoTCPServe aceita conexões em ln e, para cada uma, devolve os mesmos
// frames que recebe (protocolo de frame.go) até a conexão fechar. Cada
// conexão é servida numa goroutine própria — uma sessão warm mantém UMA
// conexão aberta e faz várias idas-e-voltas nela; um round cold abre,
// usa e fecha uma conexão por medição. Retorna quando ln é fechado por
// fora (ver runEchoTCPSubcommand).
func echoTCPServe(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener fechado: fim do servidor de eco
		}
		go echoTCPConn(conn)
	}
}

func echoTCPConn(conn net.Conn) {
	defer conn.Close()
	for {
		payload, err := readFrame(conn)
		if err != nil {
			return
		}
		if err := writeFrame(conn, payload); err != nil {
			return
		}
	}
}

// runEchoTCPSubcommand é o ponto de entrada do subcomando oculto
// "echo-tcp": sobe um listener TCP em 127.0.0.1:0 (porta escolhida pelo
// SO, para nunca colidir com algo já ouvindo numa porta fixa), imprime a
// porta escolhida como a primeira linha de stdout — é assim que
// transport_tcp.go (o processo pai) descobre onde conectar — e serve
// conexões até stdin fechar. Fechar o stdin do PROCESSO FILHO é o sinal
// de "encerre": não há porta separada para administração, e é
// exatamente o custo que ADR-012 chama de "encerrar" no modo cold.
func runEchoTCPSubcommand(stdin io.Reader, stdout io.Writer) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stdout, "0") // linha de porta ainda esperada pelo pai; 0 sinaliza falha
		return 1
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	fmt.Fprintln(stdout, port)

	go echoTCPServe(ln)

	// Bloqueia até o pai fechar o stdin deste processo — sinal de
	// encerrar. io.Discard porque o conteúdo de stdin não importa aqui
	// (o dado de verdade vai pelo socket TCP, não por stdin/stdout).
	_, _ = io.Copy(io.Discard, stdin)
	return 0
}
