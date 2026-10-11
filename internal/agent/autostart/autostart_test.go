package autostart

import "testing"

// TestComandoPara_PoeAspas: sem aspas, um caminho com espaço seria partido
// pelo Windows no primeiro espaço, e ele tentaria executar "C:\Program".
func TestComandoPara_PoeAspas(t *testing.T) {
	got := ComandoPara(`C:\Program Files\Aurora Whisper\aurora-whisper-agent.exe`)
	quero := `"C:\Program Files\Aurora Whisper\aurora-whisper-agent.exe"`
	if got != quero {
		t.Errorf("ComandoPara = %q, quero %q", got, quero)
	}
}

// TestAponta_ReconheceOProprioExecutavel nas formas em que o Windows o
// grava e o devolve.
func TestAponta_ReconheceOProprioExecutavel(t *testing.T) {
	exe := `C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`
	for _, gravado := range []string{
		ComandoPara(exe),
		exe,
		`"c:\desenvolvimento\AURORA-WHISPER\bin\aurora-whisper-agent.exe"`,
		`  "C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe"  `,
	} {
		if !Aponta(gravado, exe) {
			t.Errorf("Aponta(%q) = false: o item apareceria desmarcado com o início automático ligado", gravado)
		}
	}
}

// TestAponta_ExecutavelMovidoNaoContaComoLigado: se a pasta mudou, a
// entrada antiga aponta para um caminho que não existe mais. Mostrá-la
// como ligada seria afirmar um início automático que não vai acontecer.
func TestAponta_ExecutavelMovidoNaoContaComoLigado(t *testing.T) {
	exe := `C:\Aurora\aurora-whisper-agent.exe`
	if Aponta(`"C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe"`, exe) {
		t.Error("entrada de outra pasta contou como ligada")
	}
	if Aponta("", exe) || Aponta(`""`, exe) {
		t.Error("valor vazio contou como ligado")
	}
}

// TestDesativadoPeloWindows usa os bytes observados no registro desta
// máquina: itens que o Gerenciador de Tarefas desligou trazem 03 e a hora
// da desativação; os ativos, 02 e zeros.
func TestDesativadoPeloWindows(t *testing.T) {
	casos := []struct {
		nome  string
		dados []byte
		quer  bool
	}{
		{"ativo (02)", []byte{0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false},
		{"desativado (03 + hora)", []byte{0x03, 0, 0, 0, 0x5c, 0x1e, 0x3a, 0x7b, 0x2f, 0x19, 0xdb, 0x01}, true},
		{"ativo (06)", []byte{0x06, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false},
		{"desativado (07)", []byte{0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, true},
		// Sem valor: ninguém desligou.
		{"ausente", nil, false},
	}
	for _, c := range casos {
		if got := DesativadoPeloWindows(c.dados); got != c.quer {
			t.Errorf("%s: DesativadoPeloWindows = %v, quer %v", c.nome, got, c.quer)
		}
	}
}
