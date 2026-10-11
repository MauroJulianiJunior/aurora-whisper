package main

import "io"

// echoPipeLoop lê frames de r (protocolo de frame.go) e escreve o mesmo
// payload de volta em w, até r fechar (io.EOF limpo entre frames). É o
// "processo de eco" da medição de transporte pipe:
// docs/benchmarks/candidatos-2026-09-17.md pede "um processo de eco que
// não faz nada além de devolver o que recebeu" — esta função não olha,
// não decodifica, não valida o conteúdo do payload, só devolve os
// mesmos bytes.
//
// Roda tanto para cold (um frame, depois o chamador fecha a escrita e
// esta função retorna) quanto para warm (vários frames na mesma
// chamada, até o processo residente ser desligado) — o mesmo loop serve
// os dois modos porque o protocolo não distingue "primeira chamada" de
// "enésima chamada".
func echoPipeLoop(r io.Reader, w io.Writer) error {
	for {
		payload, err := readFrame(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := writeFrame(w, payload); err != nil {
			return err
		}
	}
}

// runEchoPipeSubcommand é o ponto de entrada do subcomando oculto
// "echo-pipe" (ver cli.go): quando este binário é reexecutado como
// processo de eco por transport_pipe.go, stdin/stdout DO PROCESSO SÃO o
// canal de dados — não há porta para anunciar, diferente de echo-tcp e
// echo-http.
func runEchoPipeSubcommand(stdin io.Reader, stdout io.Writer) int {
	if err := echoPipeLoop(stdin, stdout); err != nil {
		return 1
	}
	return 0
}
