//go:build windows

package autostart

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// Ligado reporta se o início automático está ligado PARA exe.
//
// Só lê: não passa pelo portão, porque ler o registro não muda nada no
// sistema de quem usa.
func Ligado(exe string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, ChaveRun, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: abrindo %s: %w", ChaveRun, err)
	}
	defer k.Close()

	valor, _, err := k.GetStringValue(NomeDoValor)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: lendo %s: %w", NomeDoValor, err)
	}
	if !Aponta(valor, exe) {
		return false, nil
	}
	desativado, err := desativadoPeloWindows()
	if err != nil {
		return false, err
	}
	return !desativado, nil
}

// desativadoPeloWindows lê a marca de ChaveAprovados. Sem chave ou sem
// valor, ninguém desligou.
func desativadoPeloWindows() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, ChaveAprovados, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: abrindo %s: %w", ChaveAprovados, err)
	}
	defer k.Close()
	dados, _, err := k.GetBinaryValue(NomeDoValor)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: lendo %s em %s: %w", NomeDoValor, ChaveAprovados, err)
	}
	return DesativadoPeloWindows(dados), nil
}

// Ligar grava a entrada que inicia exe com o Windows.
func Ligar(exe string) error {
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, ChaveRun, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: abrindo %s para escrita: %w", ChaveRun, err)
	}
	defer k.Close()
	if err := k.SetStringValue(NomeDoValor, ComandoPara(exe)); err != nil {
		return fmt.Errorf("autostart: gravando %s: %w", NomeDoValor, err)
	}

	// Se o Windows tinha desligado a entrada (Gerenciador de Tarefas), a
	// marca dele sobrevive à regravação do Run — e ligar pelo menu não
	// ligaria nada. Apagar a marca devolve a entrada ao estado de quem
	// nunca foi desligado. Só o valor do Aurora Whisper; nada mais na
	// chave é tocado.
	a, err := registry.OpenKey(registry.CURRENT_USER, ChaveAprovados, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("autostart: abrindo %s para escrita: %w", ChaveAprovados, err)
	}
	defer a.Close()
	if err := a.DeleteValue(NomeDoValor); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("autostart: removendo a marca de desligado em %s: %w", ChaveAprovados, err)
	}
	return nil
}

// Desligar remove a entrada. Já estar desligado não é erro.
func Desligar() error {
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, ChaveRun, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("autostart: abrindo %s para escrita: %w", ChaveRun, err)
	}
	defer k.Close()
	if err := k.DeleteValue(NomeDoValor); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("autostart: removendo %s: %w", NomeDoValor, err)
	}
	return nil
}
