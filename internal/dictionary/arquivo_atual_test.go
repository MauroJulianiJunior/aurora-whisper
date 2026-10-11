package dictionary

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestArquivo_AtualLeOQueEstaNoDisco: Atual devolve o dicionário de agora
// (inclusive o que outro processo gravou no meio) e não muda um byte do
// arquivo.
func TestArquivo_AtualLeOQueEstaNoDisco(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)
	antes := lerBytes(t, caminho)

	s, err := a.Atual()
	if err != nil {
		t.Fatalf("Atual: %v", err)
	}
	if len(s.Entradas) == 0 {
		t.Fatal("Atual devolveu um dicionário sem entradas")
	}
	if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
		t.Fatal("Atual mexeu no arquivo")
	}

	if _, _, err := a.Aprender("ECF", "SF"); err != nil {
		t.Fatal(err)
	}
	depois, err := a.Atual()
	if err != nil {
		t.Fatal(err)
	}
	if got := depois.VariantesDoPainel(); len(got) != 1 || got[0].Variante != "SF" {
		t.Errorf("Atual não viu o que foi aprendido: %+v", got)
	}
}

func TestArquivo_AtualRecusaSemNadaTocar(t *testing.T) {
	sem := &Arquivo{Caminho: filepath.Join(t.TempDir(), "nao-existe.json")}
	if _, err := sem.Atual(); !errors.Is(err, ErrSemArquivo) {
		t.Errorf("sem arquivo: %v, quero ErrSemArquivo", err)
	}
	if _, err := os.Stat(sem.Caminho); err == nil {
		t.Error("Atual criou o arquivo")
	}

	a, caminho := arquivoDeTeste(t, "{ isto não é json")
	antes := lerBytes(t, caminho)
	if _, err := a.Atual(); !errors.Is(err, ErrArquivoInvalido) {
		t.Errorf("arquivo inválido: %v, quero ErrArquivoInvalido", err)
	}
	if !bytes.Equal(lerBytes(t, caminho), antes) {
		t.Error("Atual reescreveu o arquivo inválido")
	}

	b, _ := arquivoDeTeste(t, sementeDoPainelJSON)
	b.Usuario = "outra-pessoa"
	if _, err := b.Atual(); !errors.Is(err, ErrUsuarioDiferente) {
		t.Errorf("outro usuário: %v, quero ErrUsuarioDiferente", err)
	}
}
