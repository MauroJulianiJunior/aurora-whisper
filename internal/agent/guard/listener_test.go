package guard

import (
	"net"
	"testing"
)

// TestListen_BindsLoopbackOnly afirma que o listener está mesmo em
// loopback (127.0.0.1), não em todas as interfaces. É a base de todo o
// resto: se isso falhar, nenhuma outra defesa deste pacote importa.
func TestListen_BindsLoopbackOnly(t *testing.T) {
	lis, err := Listen(0)
	if err != nil {
		t.Fatalf("Listen(0) retornou erro: %v", err)
	}
	defer lis.Close()

	addr, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("Addr() não é *net.TCPAddr: %T", lis.Addr())
	}
	if !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("listener não está em 127.0.0.1, está em %s", addr.IP)
	}
	if addr.Port == 0 {
		t.Fatal("porta 0 deveria ter sido resolvida pelo SO para uma porta real")
	}
}

// TestListen_PortZeroPicksDistinctPorts confirma que port 0 é aceito e
// que chamadas sucessivas recebem portas livres distintas (útil para
// rodar testes em paralelo sem colisão).
func TestListen_PortZeroPicksDistinctPorts(t *testing.T) {
	lis1, err := Listen(0)
	if err != nil {
		t.Fatalf("primeiro Listen(0) falhou: %v", err)
	}
	defer lis1.Close()

	lis2, err := Listen(0)
	if err != nil {
		t.Fatalf("segundo Listen(0) falhou: %v", err)
	}
	defer lis2.Close()

	p1 := lis1.Addr().(*net.TCPAddr).Port
	p2 := lis2.Addr().(*net.TCPAddr).Port
	if p1 == p2 {
		t.Fatalf("esperava portas distintas, ambas vieram %d", p1)
	}
}
