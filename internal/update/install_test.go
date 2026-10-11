package update

import (
	"context"
	"errors"
	"testing"
)

// spyInstaller espiona se Install foi chamado — é a prova exigida por
// AC-039: sem confirmação explícita, o instalador NUNCA é invocado.
type spyInstaller struct {
	called  bool
	gotPath string
}

func (s *spyInstaller) Install(_ context.Context, filePath string) error {
	s.called = true
	s.gotPath = filePath
	return nil
}

func TestInstall_WithoutConfirmation_NeverCallsInstaller(t *testing.T) {
	spy := &spyInstaller{}
	verified := VerifiedUpdate{Version: "0.10.0", FilePath: "C:\\temp\\aurora-update.exe", SHA256: "abc"}

	err := Install(context.Background(), spy, verified, false)
	if !errors.Is(err, ErrInstallNotConfirmed) {
		t.Fatalf("erro = %v, want ErrInstallNotConfirmed", err)
	}
	if spy.called {
		t.Fatal("AC-039: installer.Install NÃO pode ser chamado sem confirmação explícita")
	}
}

func TestInstall_Confirmed_CallsInstaller(t *testing.T) {
	spy := &spyInstaller{}
	verified := VerifiedUpdate{Version: "0.10.0", FilePath: "C:\\temp\\aurora-update.exe", SHA256: "abc"}

	if err := Install(context.Background(), spy, verified, true); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !spy.called {
		t.Fatal("esperava que installer.Install fosse chamado após confirmação explícita")
	}
	if spy.gotPath != verified.FilePath {
		t.Errorf("gotPath = %q, want %q", spy.gotPath, verified.FilePath)
	}
}

func TestInstall_NilInstaller(t *testing.T) {
	verified := VerifiedUpdate{FilePath: "C:\\temp\\x.exe"}
	if err := Install(context.Background(), nil, verified, true); err == nil {
		t.Fatal("esperava erro para installer nil")
	}
}

func TestInstall_MissingFilePath(t *testing.T) {
	spy := &spyInstaller{}
	verified := VerifiedUpdate{} // sem FilePath: nunca deveria ter chegado até aqui, mas o portão protege mesmo assim.
	err := Install(context.Background(), spy, verified, true)
	if err == nil {
		t.Fatal("esperava erro para VerifiedUpdate sem FilePath")
	}
	if spy.called {
		t.Fatal("não deveria chamar installer sem FilePath válido, mesmo confirmado")
	}
}
