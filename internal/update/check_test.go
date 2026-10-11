package update

import (
	"context"
	"errors"
	"testing"
)

// fakeProvider é um UpdateProvider controlado pelo teste, sem rede.
type fakeProvider struct {
	info UpdateInfo
	err  error
}

func (f fakeProvider) Latest(_ context.Context, _, _ string) (UpdateInfo, error) {
	return f.info, f.err
}

func TestCheckForUpdate_NoUpdateAvailable(t *testing.T) {
	p := fakeProvider{info: UpdateInfo{Available: false}}
	res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.State != StateNoUpdate {
		t.Errorf("State = %v, want StateNoUpdate", res.State)
	}
}

func TestCheckForUpdate_NewerVersion(t *testing.T) {
	info := UpdateInfo{Available: true, Version: "0.10.0", DownloadURL: "https://x/a.exe", SHA256: "abc"}
	p := fakeProvider{info: info}
	res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.State != StateAvailable {
		t.Errorf("State = %v, want StateAvailable", res.State)
	}
	if res.Info.Version != "0.10.0" {
		t.Errorf("Info.Version = %q, want 0.10.0", res.Info.Version)
	}
}

func TestCheckForUpdate_SameVersion_NoUpdate(t *testing.T) {
	// Mesmo que o servidor (por bug) mande update_available=true, uma
	// versão igual à local nunca deve virar StateAvailable.
	info := UpdateInfo{Available: true, Version: "0.9.0", DownloadURL: "https://x/a.exe", SHA256: "abc"}
	p := fakeProvider{info: info}
	res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.State != StateNoUpdate {
		t.Errorf("State = %v, want StateNoUpdate para versão igual", res.State)
	}
}

func TestCheckForUpdate_ServerVersionLower_NoDowngrade(t *testing.T) {
	info := UpdateInfo{Available: true, Version: "0.8.0", DownloadURL: "https://x/a.exe", SHA256: "abc"}
	p := fakeProvider{info: info}
	res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.State != StateNoUpdate {
		t.Errorf("State = %v, want StateNoUpdate: nunca faz downgrade sozinho", res.State)
	}
}

func TestCheckForUpdate_IncompleteInfo(t *testing.T) {
	cases := []UpdateInfo{
		{Available: true, Version: "", DownloadURL: "https://x/a.exe", SHA256: "abc"},
		{Available: true, Version: "0.10.0", DownloadURL: "", SHA256: "abc"},
		{Available: true, Version: "0.10.0", DownloadURL: "https://x/a.exe", SHA256: ""},
	}
	for _, info := range cases {
		p := fakeProvider{info: info}
		res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
		if !errors.Is(err, ErrIncompleteUpdateInfo) {
			t.Errorf("info=%+v: erro = %v, want ErrIncompleteUpdateInfo", info, err)
		}
		if res.State != StateNoUpdate {
			t.Errorf("info=%+v: State = %v, want StateNoUpdate", info, res.State)
		}
	}
}

func TestCheckForUpdate_ProviderError(t *testing.T) {
	wantErr := errors.New("rede fora do ar")
	p := fakeProvider{err: wantErr}
	res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
	if !errors.Is(err, wantErr) {
		t.Errorf("erro = %v, want %v", err, wantErr)
	}
	if res.State != StateNoUpdate {
		t.Errorf("State = %v, want StateNoUpdate", res.State)
	}
}

func TestCheckForUpdate_InvalidVersionFromServer(t *testing.T) {
	info := UpdateInfo{Available: true, Version: "nao-e-versao", DownloadURL: "https://x/a.exe", SHA256: "abc"}
	p := fakeProvider{info: info}
	res, err := CheckForUpdate(context.Background(), p, "0.9.0", "x64")
	if err == nil {
		t.Fatal("esperava erro para versão do servidor inválida")
	}
	if res.State != StateNoUpdate {
		t.Errorf("State = %v, want StateNoUpdate", res.State)
	}
}

func TestCheckForUpdate_NilProvider(t *testing.T) {
	if _, err := CheckForUpdate(context.Background(), nil, "0.9.0", "x64"); err == nil {
		t.Fatal("esperava erro para provider nil")
	}
}
