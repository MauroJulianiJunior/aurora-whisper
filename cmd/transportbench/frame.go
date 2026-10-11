package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

// writeFrame escreve payload em w precedido por um cabeçalho de 4 bytes
// little-endian com o tamanho. É o protocolo mínimo que deixa um
// processo de eco vivo entre requisições (modo warm) e ainda assim
// delimita mensagens sucessivas sobre o mesmo stream — diferente do
// protocolo de linha que internal/benchmark/candidate usa para texto,
// aqui o payload é binário (simula áudio) e pode conter qualquer byte,
// inclusive '\n'.
func writeFrame(w io.Writer, payload []byte) error {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("transportbench: escrevendo cabecalho do frame: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("transportbench: escrevendo corpo do frame (%d bytes): %w", len(payload), err)
	}
	return nil
}

// readFrame lê um frame escrito por writeFrame. Devolve io.EOF sem
// decorar quando r fecha exatamente no limite entre frames (nenhum byte
// do cabeçalho seguinte foi lido ainda, o comportamento padrão de
// io.ReadFull) — é o sinal limpo de "quem escreve fechou a
// conexão/pipe", que o loop de eco (echo_pipe.go, echo_tcp.go) usa para
// encerrar sem tratar como erro. Um frame que começa mas não completa
// (conexão caiu no meio do corpo) devolve erro decorado, nunca um
// payload parcial silencioso.
func readFrame(r io.Reader) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("transportbench: frame truncado (esperava %d bytes de corpo): %w", n, err)
	}
	return buf, nil
}
