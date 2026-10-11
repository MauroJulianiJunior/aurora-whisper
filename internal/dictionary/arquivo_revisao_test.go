package dictionary

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Testes da revisão de 30-set-2026 sobre o arquivo do painel. Tudo em
// t.TempDir(): nenhum toca bin\dicionario.json.

// TestArquivo_ConteudoDepoisDoObjetoNaoETocado: CarregarSemente lê só o
// primeiro valor JSON, e gravar escreve o arquivo inteiro por cima. O que
// viesse depois do objeto (um segundo objeto, marcas de conflito do git,
// lixo) sumiria em silêncio, contra o contrato de que arquivo inválido não é
// tocado.
func TestArquivo_ConteudoDepoisDoObjetoNaoETocado(t *testing.T) {
	sobras := map[string]string{
		"segundo objeto":    "{\"usuario\":\"outro\"}\n",
		"marca de conflito": "<<<<<<< HEAD\n",
		"lixo":              "x",
		"o objeto repetido": sementeDoPainelJSON,
		"vírgula":           ",",
		"depois de espaços": "\n\n   \n{}",
		"comentário":        "// nota\n",
		"nulo":              "\x00",
	}
	for nome, sobra := range sobras {
		t.Run(nome, func(t *testing.T) {
			a, caminho := arquivoDeTeste(t, sementeDoPainelJSON+sobra)
			antes := lerBytes(t, caminho)

			if _, _, err := a.Aprender("ECF", "SF"); !errors.Is(err, ErrArquivoInvalido) {
				t.Errorf("Aprender: erro = %v, quero ErrArquivoInvalido", err)
			}
			if _, _, err := a.Desfazer("SF"); !errors.Is(err, ErrArquivoInvalido) {
				t.Errorf("Desfazer: erro = %v, quero ErrArquivoInvalido", err)
			}
			if _, err := a.Aprendidas(); !errors.Is(err, ErrArquivoInvalido) {
				t.Errorf("Aprendidas: erro = %v, quero ErrArquivoInvalido", err)
			}
			if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
				t.Errorf("o arquivo foi tocado:\n%s", got)
			}
			exigirSoOArquivo(t, caminho)
		})
	}
}

// TestArquivo_SoEspacoDepoisDoObjetoEValido: o arquivo terminado em nova
// linha (o que Gravar escreve), ou com linhas em branco sobrando, é o caso
// comum e continua valendo.
func TestArquivo_SoEspacoDepoisDoObjetoEValido(t *testing.T) {
	for nome, sobra := range map[string]string{
		"nada":             "",
		"linhas em branco": "\n\n  \t\n",
		"CRLF":             "\r\n",
	} {
		t.Run(nome, func(t *testing.T) {
			a, _ := arquivoDeTeste(t, strings.TrimRight(sementeDoPainelJSON, "\n")+sobra)
			if _, _, err := a.Aprender("ECF", "SF"); err != nil {
				t.Errorf("Aprender: %v", err)
			}
		})
	}
}

// TestArquivo_ValidarQueRecusaTemMensagemFixa: o erro de Validar não leva no
// texto a causa, que pode citar termo e alias (o Popular os cita com %q). A
// causa continua alcançável por errors.Is.
func TestArquivo_ValidarQueRecusaTemMensagemFixa(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDoPainelJSON)
	antes := lerBytes(t, caminho)

	causa := errors.New(`gravando alias "termo-secreto" de "outro-secreto"`)
	a.Validar = func(Semente) error { return causa }
	_, _, err := a.Aprender("ECF", "SF")
	if !errors.Is(err, ErrNaoCarrega) {
		t.Fatalf("erro = %v, quero ErrNaoCarrega", err)
	}
	if !errors.Is(err, causa) {
		t.Error("a causa não está em Unwrap")
	}
	if strings.Contains(err.Error(), "secreto") || err.Error() != ErrNaoCarrega.Error() {
		t.Errorf("o texto do erro traz a causa: %q", err.Error())
	}
	if !bytes.Equal(lerBytes(t, caminho), antes) {
		t.Error("gravou apesar de a validação recusar")
	}
}

// --- rename que tropeça ---------------------------------------------------

// trocarRename põe um rename de teste (sem pausas de verdade) pelo tempo do
// teste e devolve o contador de chamadas.
func trocarRename(t *testing.T, falha func(chamada int) error) *int {
	t.Helper()
	antesFn, antesPausas := renomear, pausasEntreRenames
	t.Cleanup(func() { renomear, pausasEntreRenames = antesFn, antesPausas })

	pausasEntreRenames = make([]time.Duration, len(antesPausas)) // mesmas tentativas, sem esperar
	chamadas := 0
	renomear = func(de, para string) error {
		chamadas++
		if err := falha(chamadas); err != nil {
			return &os.LinkError{Op: "rename", Old: de, New: para, Err: err}
		}
		return os.Rename(de, para)
	}
	return &chamadas
}

func gravarUmaSemente(t *testing.T) (caminho string, antes []byte, erro error) {
	t.Helper()
	caminho = filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(caminho, []byte(sementeDoPainelJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _, err := sementeDoPainel(t).AprenderVariante("ECF", "SF")
	if err != nil {
		t.Fatal(err)
	}
	return caminho, lerBytes(t, caminho), s.Gravar(caminho)
}

// TestGravar_TentaDeNovoQuandoODestinoEstaPreso: o antivírus ou o indexador
// seguram o arquivo por milissegundos e o rename falha com "Acesso negado".
// Repetir, com pausa curta, evita que o clique do painel falhe por isso.
func TestGravar_TentaDeNovoQuandoODestinoEstaPreso(t *testing.T) {
	presos := map[string]error{
		"acesso negado":                fs.ErrPermission,
		"violação de compartilhamento": syscall.Errno(32),
	}
	for nome, preso := range presos {
		t.Run(nome, func(t *testing.T) {
			chamadas := trocarRename(t, func(n int) error {
				if n <= 2 {
					return preso
				}
				return nil
			})
			caminho, antes, err := gravarUmaSemente(t)
			if err != nil {
				t.Fatalf("Gravar: %v", err)
			}
			if *chamadas != 3 {
				t.Errorf("rename chamado %d vezes, quero 3 (duas falhas e o acerto)", *chamadas)
			}
			if got := lerBytes(t, caminho); bytes.Equal(got, antes) || !bytes.Contains(got, []byte(`"SF"`)) {
				t.Errorf("o arquivo não foi substituído:\n%s", got)
			}
			exigirSoOArquivo(t, caminho)
		})
	}
}

// TestGravar_DestinoPresoParaSempreFalhaSemTocarNemSobrarTemporario: depois
// das tentativas o erro volta, o arquivo fica como estava e nenhum
// temporário sobra.
func TestGravar_DestinoPresoParaSempreFalhaSemTocarNemSobrarTemporario(t *testing.T) {
	chamadas := trocarRename(t, func(int) error { return fs.ErrPermission })
	caminho, antes, err := gravarUmaSemente(t)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Gravar: erro = %v, quero o de permissão", err)
	}
	if want := 1 + len(pausasEntreRenames); *chamadas != want {
		t.Errorf("rename chamado %d vezes, quero %d", *chamadas, want)
	}
	if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
		t.Errorf("o arquivo foi tocado:\n%s", got)
	}
	exigirSoOArquivo(t, caminho)
}

// TestGravar_ErroQueNaoEDePresoNaoRepete: pasta apagada, disco cheio e o
// resto voltam na hora: repetir só atrasaria o erro.
func TestGravar_ErroQueNaoEDePresoNaoRepete(t *testing.T) {
	chamadas := trocarRename(t, func(int) error { return fs.ErrNotExist })
	caminho, _, err := gravarUmaSemente(t)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Gravar: erro = %v", err)
	}
	if *chamadas != 1 {
		t.Errorf("rename chamado %d vezes, quero 1", *chamadas)
	}
	exigirSoOArquivo(t, caminho)
}

// TestGravar_AsPausasSomamMenosDeMeioSegundo: o clique do painel não pode
// ficar pendurado por causa da nova tentativa.
func TestGravar_AsPausasSomamMenosDeMeioSegundo(t *testing.T) {
	var total time.Duration
	for _, p := range pausasEntreRenames {
		total += p
	}
	if total <= 0 || total >= 500*time.Millisecond {
		t.Errorf("as pausas somam %v, quero entre 0 e 500ms", total)
	}
}
