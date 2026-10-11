//go:build !windows

package audio

import "time"

// inicioDoRelogio ancora o relógio fora do Windows. Só existe para o pacote
// compilar e rodar os testes em CI (ubuntu-latest): nenhum caminho de
// produção captura áudio fora do Windows.
var inicioDoRelogio = time.Now()

// agoraQPC fora do Windows é um relógio monotônico na mesma unidade de
// 100 ns do QPC. O +1 mantém o valor sempre positivo: zero é "carimbo
// ausente" para quem lê (ver Session.receber).
func agoraQPC() int64 { return ticksDe(time.Since(inicioDoRelogio)) + 1 }
