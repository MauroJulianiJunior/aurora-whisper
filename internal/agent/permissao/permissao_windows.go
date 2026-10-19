//go:build windows

package permissao

import (
	"errors"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	chavePolitica = `SOFTWARE\Policies\Microsoft\Windows\AppPrivacy`
	chaveConsent  = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone`
)

// Consultar lê o registro e devolve o motivo do bloqueio. Só lê: nada é
// gravado, então não passa pelo portão de sysinput. Erro de leitura que
// não seja "não existe" devolve Desconhecido — nunca afirma bloqueio.
func Consultar() Motivo {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return consultarDe(exe)
}

func consultarDe(exe string) Motivo {
	v := Valores{PoliticaEmpresa: SemPolitica}
	falhou := false

	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, chavePolitica, registry.QUERY_VALUE); err == nil {
		n, _, err := k.GetIntegerValue("LetAppsAccessMicrophone")
		switch {
		case err == nil:
			v.PoliticaEmpresa = int(n)
		case !errors.Is(err, registry.ErrNotExist):
			falhou = true
		}
		k.Close()
	} else if !errors.Is(err, registry.ErrNotExist) {
		falhou = true
	}

	// Chave ou valor ausente é "liberado" (o padrão do Windows), não falha.
	lerValor := func(raiz registry.Key, caminho string) string {
		k, err := registry.OpenKey(raiz, caminho, registry.QUERY_VALUE)
		if err != nil {
			if !errors.Is(err, registry.ErrNotExist) {
				falhou = true
			}
			return ""
		}
		defer k.Close()
		s, _, err := k.GetStringValue("Value")
		if err != nil && !errors.Is(err, registry.ErrNotExist) {
			falhou = true
		}
		return s
	}

	v.Aparelho = lerValor(registry.LOCAL_MACHINE, chaveConsent)
	v.Conta = lerValor(registry.CURRENT_USER, chaveConsent)
	v.Programas = lerValor(registry.CURRENT_USER, chaveConsent+`\NonPackaged`)
	if exe != "" {
		v.EsteApp = lerValor(registry.CURRENT_USER, chaveConsent+`\NonPackaged\`+ChaveDoApp(exe))
	}

	if m := Decidir(v); m != Liberado {
		return m
	}
	if falhou {
		return Desconhecido
	}
	return Liberado
}
