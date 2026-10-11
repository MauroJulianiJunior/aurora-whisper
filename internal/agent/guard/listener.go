package guard

import (
	"fmt"
	"net"
)

// Listen abre um listener TCP vinculado explicitamente a 127.0.0.1.
//
// Nunca usa "0.0.0.0" (todas as interfaces) nem ":porta" sozinho — em
// net.Listen, um endereço sem host também significa todas as
// interfaces. Ver NFR-007 e AC-012: bindar em todas as interfaces
// tornaria a Local API acessível a qualquer máquina na rede local, não
// só ao processo do próprio usuário.
//
// port 0 é aceito e delega ao sistema operacional a escolha de uma
// porta livre — é o que os testes deste pacote (e do restante do
// Agent) devem usar. A porta efetivamente escolhida fica disponível em
// Listener.Addr(), que o chamador usa tanto para servir quanto para
// configurar RequireHost/RequireOrigin/Protect com a mesma porta.
func Listen(port int) (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("guard: falha ao abrir listener em %s: %w", addr, err)
	}
	return lis, nil
}
