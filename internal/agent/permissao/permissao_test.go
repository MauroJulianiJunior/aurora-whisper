package permissao

import "testing"

func TestDecidir(t *testing.T) {
	livre := Valores{PoliticaEmpresa: SemPolitica}
	com := func(f func(*Valores)) Valores { v := livre; f(&v); return v }
	for _, c := range []struct {
		nome string
		v    Valores
		quer Motivo
	}{
		{"tudo ausente é liberado", livre, Liberado},
		{"allow em tudo", Valores{PoliticaEmpresa: SemPolitica, Aparelho: "Allow", Conta: "Allow", Programas: "Allow", EsteApp: "Allow"}, Liberado},
		{"política 0 não bloqueia", com(func(v *Valores) { v.PoliticaEmpresa = 0 }), Liberado},
		{"política 1 não bloqueia", com(func(v *Valores) { v.PoliticaEmpresa = 1 }), Liberado},
		{"política 2 é empresa", com(func(v *Valores) { v.PoliticaEmpresa = 2 }), Empresa},
		{"aparelho", com(func(v *Valores) { v.Aparelho = "Deny" }), Aparelho},
		{"conta", com(func(v *Valores) { v.Conta = "Deny" }), Conta},
		{"programas", com(func(v *Valores) { v.Programas = "Deny" }), Programas},
		{"este app", com(func(v *Valores) { v.EsteApp = "Deny" }), EsteApp},
		{"caixa e espaço não importam", com(func(v *Valores) { v.Conta = " deny " }), Conta},
		{"empresa vence tudo", Valores{PoliticaEmpresa: 2, Aparelho: "Deny", Conta: "Deny", Programas: "Deny", EsteApp: "Deny"}, Empresa},
		{"aparelho vence conta", Valores{PoliticaEmpresa: SemPolitica, Aparelho: "Deny", Conta: "Deny", Programas: "Deny", EsteApp: "Deny"}, Aparelho},
		{"conta vence programas", Valores{PoliticaEmpresa: SemPolitica, Conta: "Deny", Programas: "Deny", EsteApp: "Deny"}, Conta},
		{"programas vence este app", Valores{PoliticaEmpresa: SemPolitica, Programas: "Deny", EsteApp: "Deny"}, Programas},
	} {
		if got := Decidir(c.v); got != c.quer {
			t.Errorf("%s: Decidir = %q, quero %q", c.nome, got, c.quer)
		}
	}
}

func TestMotivo_Bloqueado(t *testing.T) {
	for m, quer := range map[Motivo]bool{
		Liberado: false, Desconhecido: false,
		Empresa: true, Aparelho: true, Conta: true, Programas: true, EsteApp: true,
	} {
		if m.Bloqueado() != quer {
			t.Errorf("%q.Bloqueado() = %v, quero %v", m, !quer, quer)
		}
	}
}

// As strings vão na API e no log: mudar uma quebra o painel.
func TestMotivo_StringsEstaveis(t *testing.T) {
	for m, s := range map[Motivo]string{
		Empresa: "empresa", Aparelho: "aparelho", Conta: "conta",
		Programas: "programas", EsteApp: "este_app", Liberado: "", Desconhecido: "desconhecido",
	} {
		if string(m) != s {
			t.Errorf("motivo %q, quero %q", m, s)
		}
	}
}

func TestChaveDoApp(t *testing.T) {
	got := ChaveDoApp(`C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`)
	if want := `C:#Desenvolvimento#aurora-whisper#bin#aurora-whisper-agent.exe`; got != want {
		t.Errorf("ChaveDoApp = %q, quero %q", got, want)
	}
}
