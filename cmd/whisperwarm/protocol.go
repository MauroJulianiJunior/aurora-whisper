package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// errorLinePrefix marca uma linha de saída como falha, não transcrição.
//
// O protocolo definido em internal/benchmark/candidate (comentário de
// pacote de candidate.go, e warm.go: WarmSession.Transcribe) não separa
// sucesso de erro dentro da linha — ele lê exatamente uma linha de
// resposta e a trata sempre como o texto transcrito. Não existe canal
// "fora de banda" para sinalizar falha sem inventar um protocolo novo, o
// que o ADR-012 e as instruções desta tarefa pedem para não fazer.
//
// A escolha feita aqui — deliberada, e registrada no relatório final —
// é: mesmo em erro, escrever exatamente uma linha (mantendo o protocolo
// em lockstep: uma linha lida, uma linha escrita, sempre), prefixada com
// algo que nunca aparece numa transcrição real e que reprova qualquer
// comparação de WER na hora, em vez de deixar a chamada do harness
// estourar por -ready-timeout. O erro completo vai para stderr, nunca
// truncado.
const errorLinePrefix = "[whisperwarm:error] "

// runProtocol implementa o protocolo de linha: cada linha não-vazia de
// in é um caminho de áudio; transcribe(caminho) é chamado, e exatamente
// uma linha é escrita em out por linha lida — nunca zero (isso
// travaria a próxima chamada do harness em ErrTimeout), nunca mais de
// uma (isso dessincronizaria as respostas seguintes).
//
// Retorna nil em EOF limpo (o caminho normal: harness fechou stdin).
// Só retorna erro se a própria leitura de in falhar de um jeito que não
// seja EOF.
func runProtocol(in io.Reader, out io.Writer, errOut io.Writer, transcribe func(string) (string, error)) error {
	scanner := bufio.NewScanner(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			fmt.Fprintln(errOut, "whisperwarm: linha vazia recebida no protocolo de linha, ignorando pedido")
			writeLine(writer, errorLinePrefix+"linha vazia")
			continue
		}

		text, err := transcribe(line)
		if err != nil {
			fmt.Fprintf(errOut, "whisperwarm: %s: %v\n", line, err)
			writeLine(writer, errorLinePrefix+sanitizeLine(err.Error()))
			continue
		}

		writeLine(writer, text)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("whisperwarm: lendo stdin: %w", err)
	}
	return nil
}

// writeLine escreve s como uma linha e libera o buffer imediatamente —
// o harness espera cada resposta assim que a envia (warm.go bloqueia em
// Scan() por linha), então segurar a escrita em buffer sem Flush()
// travaria a chamada do outro lado do pipe.
func writeLine(w *bufio.Writer, s string) {
	fmt.Fprintln(w, s)
	w.Flush()
}
