package main

import (
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/permissao"
)

func TestMicrofoneDoAgent_Estado(t *testing.T) {
	for _, c := range []struct {
		motivo    permissao.Motivo
		bloqueado bool
	}{
		{permissao.Liberado, false}, {permissao.Desconhecido, false},
		{permissao.Empresa, true}, {permissao.Aparelho, true}, {permissao.Conta, true},
		{permissao.Programas, true}, {permissao.EsteApp, true},
	} {
		m := microfoneDoAgent{consultar: func() permissao.Motivo { return c.motivo }}
		bloqueado, motivo := m.Estado()
		if bloqueado != c.bloqueado || motivo != string(c.motivo) {
			t.Errorf("%q: Estado = %v, %q; quero %v, %q", c.motivo, bloqueado, motivo, c.bloqueado, c.motivo)
		}
	}
}

func TestMicrofoneDoAgent_AbrirRepassaOErro(t *testing.T) {
	falhou := errors.New("falhou")
	if err := (microfoneDoAgent{abrir: func() error { return falhou }}).AbrirConfiguracoes(); err != falhou {
		t.Errorf("err = %v", err)
	}
}
