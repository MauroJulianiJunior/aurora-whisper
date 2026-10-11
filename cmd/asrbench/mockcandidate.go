package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/mock"
)

// runMockCandidate implementa o subcomando oculto "mockcandidate" — ver
// main.go. Ele existe SÓ para testar o harness de ponta a ponta sem
// nenhum modelo de ASR instalado, usando internal/asr/mock de verdade
// (nunca um atalho que finge chamar o provider).
//
// A "transcrição" que ele produz para cada amostra vem de um arquivo
// fixture opcional ao lado do áudio: <audioPath>.transcript.txt. Sem
// fixture, transcreve como string vazia — um candidato mock não inventa
// texto que ninguém pediu.
//
//	asrbench mockcandidate cold <audioPath>   -- uma transcrição, imprime e sai
//	asrbench mockcandidate warm               -- lê um caminho de áudio por
//	                                              linha em stdin, imprime uma
//	                                              transcrição por linha em
//	                                              stdout, até stdin fechar
func runMockCandidate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "mockcandidate: uso: mockcandidate cold <audio> | mockcandidate warm")
		return 2
	}

	switch args[0] {
	case "cold":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "mockcandidate cold: uso: mockcandidate cold <audio>")
			return 2
		}
		text, err := mockTranscribe(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "mockcandidate cold:", err)
			return 1
		}
		fmt.Fprintln(stdout, text)
		return 0

	case "warm":
		scanner := bufio.NewScanner(stdin)
		for scanner.Scan() {
			audioPath := strings.TrimSpace(scanner.Text())
			text, err := mockTranscribe(audioPath)
			if err != nil {
				// Protocolo de linha (candidate/warm.go): uma linha de
				// resposta é obrigatória por linha de requisição, mesmo
				// em erro — uma linha vazia sinaliza "sem transcrição"
				// sem quebrar o protocolo para a próxima requisição.
				fmt.Fprintln(stdout, "")
				continue
			}
			fmt.Fprintln(stdout, text)
		}
		return 0

	default:
		fmt.Fprintf(stderr, "mockcandidate: subcomando desconhecido %q\n", args[0])
		return 2
	}
}

// mockTranscribe chama internal/asr/mock.Confident de verdade — não uma
// simulação — com o texto do fixture (ou vazio) como resultado esperado.
func mockTranscribe(audioPath string) (string, error) {
	text := ""
	if raw, err := os.ReadFile(audioPath + ".transcript.txt"); err == nil {
		text = strings.TrimSpace(string(raw))
	}

	provider := mock.NewConfident(text, 0.9)
	res, err := provider.Transcribe(context.Background(), asr.AudioBuffer{}, asr.ASROptions{})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
