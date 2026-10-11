package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
)

// echoHTTPHandler implementa o mesmo contrato de eco do pipe e do TCP,
// mas pelo protocolo que whisper-server e nemo-speech serve usam: POST
// multipart. Lê a primeira parte do multipart (o campo de arquivo) e
// devolve o mesmo conteúdo, byte a byte, como corpo da resposta — sem
// tocar em nenhum outro campo do form nem inspecionar o conteúdo.
//
// Lê o corpo INTEIRO (io.ReadAll) antes de escrever qualquer byte da
// resposta — não faz io.Copy(w, part) direto. Descoberto na prática
// (TestEchoHTTPHandler_RoundTrip_ByteExact quebrava com "http: invalid
// Read on closed Body" e payload truncado): um handler HTTP/1.1 que
// escreve na resposta ANTES de terminar de ler o corpo da requisição não
// é full-duplex por padrão em net/http — o servidor entende que o
// handler "já terminou de ler" assim que a primeira escrita sai, e
// fecha o resto do corpo da requisição por baixo. Dá pra contornar com
// http.NewResponseController(w).EnableFullDuplex(), mas para um
// processo de eco não há motivo para pagar esse risco: ler tudo, depois
// escrever tudo, é mais simples e não tem essa pegadinha.
func echoHTTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	part, err := mr.NextPart()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer part.Close()

	payload, err := io.ReadAll(part)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(payload)
}

// runEchoHTTPSubcommand é o ponto de entrada do subcomando oculto
// "echo-http": mesmo protocolo de descoberta de porta que echo-tcp
// (porta anunciada na primeira linha de stdout, fechar o stdin do filho
// é o sinal de encerrar) — só troca o transporte de dados por um
// http.Server servindo echoHTTPHandler.
func runEchoHTTPSubcommand(stdin io.Reader, stdout io.Writer) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stdout, "0")
		return 1
	}

	port := ln.Addr().(*net.TCPAddr).Port
	fmt.Fprintln(stdout, port)

	srv := &http.Server{Handler: http.HandlerFunc(echoHTTPHandler)}
	go func() { _ = srv.Serve(ln) }()

	_, _ = io.Copy(io.Discard, stdin)
	_ = srv.Close()
	return 0
}
