package syncengine

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O aviso do freio do aprendido para o painel (Laco.Freio, o freio_trocas do
// GET /api/nuvem): o da última varredura que comparou o arquivo, que aparece
// quando ela segura mais de LimiteDoFreio pares saindo à mão, e some na
// varredura que o solta (o arquivo reposto). Uma rodada que não compara o
// arquivo (o dicionário indisponível) não o muda.
func TestLaco_OFreioDaUltimaVarredura(t *testing.T) {
	p := novoPCQueVarre(t)
	p.aceitar()
	variantes := make([]string, LimiteDoFreio+1)
	for i := range variantes {
		variantes[i] = fmt.Sprintf("v%c", 'a'+i)
		p.aprender("ECF", variantes[i])
	}
	l := NovoLaco(ConfigDoLaco{Fila: p.fila, Conexao: conectada(),
		Cliente: func(string, string) SyncClient { return p.nuvem }, NovoID: storage.NewID,
		Agora: func() time.Time { return p.agora }, Dicionario: p.arq})
	rodada := func() {
		t.Helper()
		if err := l.RodadaAgora(context.Background()); err != nil {
			t.Fatalf("rodada: %v", err)
		}
	}
	rodada()
	if l.Freio() != 0 || len(p.nuvem.enviadas) != len(variantes) {
		t.Fatalf("antes: o freio %d, %d enviadas", l.Freio(), len(p.nuvem.enviadas))
	}

	antes := p.bytesDoArquivo()
	p.aMao(func(s *dictionary.Semente) {
		for _, v := range variantes {
			sem, _, err := s.DesfazerVariante(v)
			if err != nil {
				t.Fatal(err)
			}
			*s = sem
		}
	})
	enviadas := len(p.nuvem.enviadas)
	rodada()
	if l.Freio() != len(variantes) || len(p.nuvem.enviadas) != enviadas {
		t.Fatalf("o freio: %d, e subiram %d; quero %d e nada", l.Freio(), len(p.nuvem.enviadas)-enviadas, len(variantes))
	}

	// O arquivo indisponível não compara nada: o aviso fica.
	if err := os.WriteFile(p.arq.Caminho, []byte("{ inválido"), 0o600); err != nil {
		t.Fatal(err)
	}
	rodada()
	if l.Freio() != len(variantes) {
		t.Errorf("com o arquivo indisponível, o freio virou %d", l.Freio())
	}

	// O arquivo reposto solta o freio, e o aviso some.
	if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
		t.Fatal(err)
	}
	rodada()
	if l.Freio() != 0 || len(p.nuvem.enviadas) != enviadas {
		t.Errorf("reposto: o freio %d, e subiram %d", l.Freio(), len(p.nuvem.enviadas)-enviadas)
	}
}
