package update

import (
	"context"
	"errors"
)

// ErrInstallNotConfirmed é devolvido por Install quando chamado sem
// confirmação explícita (AC-039). Install nunca chama installer.Install
// nesse caso — ver install_test.go, que prova isso com um espião.
var ErrInstallNotConfirmed = errors.New("update: instalação requer confirmação explícita do usuário (AC-039)")

// Installer aplica, de fato, um pacote de update já verificado. Este
// pacote (internal/update) não implementa Installer — quem sabe instalar
// um .exe/.msi no Windows é responsabilidade de outra camada, fora deste
// escopo. A única coisa que internal/update garante é NUNCA chamar
// Installer.Install sem confirmação explícita.
type Installer interface {
	Install(ctx context.Context, filePath string) error
}

// Install repassa verified para installer, mas SOMENTE quando confirmed é
// true. Este é o único portão de instalação do pacote inteiro: nenhuma
// outra função aqui chama Installer.Install.
//
// auto_update_install (docs/ai-spec/12-rollout.md) é off por padrão e é
// decisão de CONFIGURAÇÃO LOCAL de quem chama Install (o Agent, lendo a
// preferência do usuário) — nunca decidida a partir da resposta do
// servidor: UpdateInfo (provider.go) nem carrega esse campo, então não há
// como uma resposta HTTP ligar auto-instalação sozinha. Mesmo com
// auto_update_install ligado localmente, é o chamador quem decide o valor
// de confirmed — este pacote não assume que "auto" implica "confirmado
// sem checagem alguma".
func Install(ctx context.Context, installer Installer, verified VerifiedUpdate, confirmed bool) error {
	if !confirmed {
		return ErrInstallNotConfirmed
	}
	if installer == nil {
		return errors.New("update: installer nil")
	}
	if verified.FilePath == "" {
		return errors.New("update: VerifiedUpdate sem FilePath")
	}
	return installer.Install(ctx, verified.FilePath)
}
