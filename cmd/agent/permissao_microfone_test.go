package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/permissao"
)

func TestAvisarMicrofoneBloqueado_SoAvisaQuandoBloqueado(t *testing.T) {
	for _, c := range []struct {
		motivo permissao.Motivo
		avisa  bool
	}{
		{permissao.Empresa, true}, {permissao.Aparelho, true}, {permissao.Conta, true},
		{permissao.Programas, true}, {permissao.EsteApp, true},
		{permissao.Liberado, false}, {permissao.Desconhecido, false},
	} {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))

		if got := avisarMicrofoneBloqueado(log, func() permissao.Motivo { return c.motivo }); got != c.motivo {
			t.Errorf("%q: devolveu %q", c.motivo, got)
		}

		saiu := buf.String()
		if c.avisa {
			if !strings.Contains(saiu, "level=WARN") || !strings.Contains(saiu, "motivo="+string(c.motivo)) {
				t.Errorf("%q: faltou o WARN com o motivo:\n%s", c.motivo, saiu)
			}
		} else if saiu != "" {
			t.Errorf("%q: não devia logar nada:\n%s", c.motivo, saiu)
		}
	}
}
