package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// TestSemCaminho: o erro do sistema de arquivos sai só com o motivo, sem o
// caminho (ele leva o nome de usuário do Windows). Vale para o PathError e para o
// LinkError do rename, que leva os dois caminhos (as imagens das notas, N10).
func TestSemCaminho(t *testing.T) {
	const pasta = `C:\Users\mauro\AppData\Local\AuroraWhisper\notas-imagens`
	for _, err := range []error{
		&fs.PathError{Op: "open", Path: pasta + `\x.jpg`, Err: fs.ErrPermission},
		&os.LinkError{Op: "rename", Old: pasta + `\.gravando-x.tmp`, New: pasta + `\x.jpg`, Err: fs.ErrPermission},
		fmt.Errorf("embrulhado: %w", &os.LinkError{Op: "rename", Old: pasta, New: pasta, Err: fs.ErrPermission}),
	} {
		got := semCaminho(err)
		if strings.Contains(got.Error(), "mauro") || !errors.Is(got, fs.ErrPermission) {
			t.Errorf("semCaminho(%v) = %v", err, got)
		}
	}
}
