//go:build windows

package chaves

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// advapi32!Cred*W: sem wrapper em golang.org/x/sys/windows, então é
// resolvido como o resto do Agent resolve (hook, beep, clipboard):
// NewLazySystemDLL/NewProc, sem dependência nova.
var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1 // CRED_TYPE_GENERIC
	credPersistLocalMachine = 2 // CRED_PERSIST_LOCAL_MACHINE — ver o doc do pacote
)

// credentialW é o CREDENTIALW de wincred.h, campo a campo. O alinhamento
// natural do Go é o mesmo do C em amd64 (e em 386), então o layout bate
// sem pragma nenhum.
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// gerenciador é o Cofre de produção: o Gerenciador de Credenciais.
type gerenciador struct{}

// NovoGerenciador devolve o Cofre do Gerenciador de Credenciais do
// Windows. Toda chamada passa pelo portão sysinput (ver o doc do pacote).
func NovoGerenciador() Cofre { return gerenciador{} }

func (gerenciador) Gravar(provedor, chave string) error {
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("chaves: %w", err)
	}
	if _, err := AlvoDe(provedor); err != nil {
		return err
	}
	var err error
	if chave, err = Validar(chave); err != nil {
		return err
	}

	// Uma cópia que dá para zerar: a string de Go não dá. O Gerenciador
	// copia o blob para dentro dele na chamada.
	blob := []byte(chave)
	defer clear(blob)

	cred, err := credencialDe(provedor, blob)
	if err != nil {
		return err
	}
	r1, _, errno := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	runtime.KeepAlive(blob)
	if r1 == 0 {
		// errno é o código do Windows; a chave não entra na mensagem.
		return fmt.Errorf("chaves: CredWriteW: %w", errno)
	}
	return nil
}

// credencialDe monta a credencial de provedor com blob: o alvo e o comentário
// dele (ComentarioDe, que aparece no Gerenciador para quem abrir e se
// perguntar de quem é aquilo). Não chama o sistema: o teste confere o alvo e
// o comentário de cada provedor.
func credencialDe(provedor string, blob []byte) (credentialW, error) {
	alvo, err := AlvoDe(provedor)
	if err != nil {
		return credentialW{}, err
	}
	alvo16, err := windows.UTF16PtrFromString(alvo)
	if err != nil {
		return credentialW{}, fmt.Errorf("chaves: alvo: %w", err)
	}
	comentario16, err := windows.UTF16PtrFromString(ComentarioDe(provedor))
	if err != nil {
		return credentialW{}, fmt.Errorf("chaves: comentário: %w", err)
	}
	return novaCredencial(alvo16, comentario16, blob), nil
}

// novaCredencial monta o CREDENTIALW que Gravar entrega ao CredWriteW. É
// separada para o teste conferir o que de fato vai ao Gerenciador — tipo
// genérico e escopo LOCAL_MACHINE (ver o doc do pacote) — sem chamar o
// sistema. blob não pode ser vazio (Validar garante).
func novaCredencial(alvo, comentario *uint16, blob []byte) credentialW {
	return credentialW{
		Type:               credTypeGeneric,
		TargetName:         alvo,
		Comment:            comentario,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
	}
}

func (gerenciador) Ler(provedor string) (string, error) {
	if err := sysinput.Guard(); err != nil {
		return "", fmt.Errorf("chaves: %w", err)
	}
	alvo, err := AlvoDe(provedor)
	if err != nil {
		return "", err
	}
	alvo16, err := windows.UTF16PtrFromString(alvo)
	if err != nil {
		return "", fmt.Errorf("chaves: alvo: %w", err)
	}

	var cred *credentialW
	r1, _, errno := procCredReadW.Call(uintptr(unsafe.Pointer(alvo16)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if r1 == 0 {
		if errors.Is(errno, windows.ERROR_NOT_FOUND) {
			return "", ErrAusente
		}
		return "", fmt.Errorf("chaves: CredReadW: %w", errno)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))

	if cred.CredentialBlobSize == 0 || cred.CredentialBlob == nil {
		return "", ErrAusente
	}
	// string(...) copia para a memória do Go antes do CredFree acima.
	return string(unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)), nil
}

func (gerenciador) Apagar(provedor string) error {
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("chaves: %w", err)
	}
	alvo, err := AlvoDe(provedor)
	if err != nil {
		return err
	}
	alvo16, err := windows.UTF16PtrFromString(alvo)
	if err != nil {
		return fmt.Errorf("chaves: alvo: %w", err)
	}
	r1, _, errno := procCredDeleteW.Call(uintptr(unsafe.Pointer(alvo16)), credTypeGeneric, 0)
	if r1 == 0 && !errors.Is(errno, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("chaves: CredDeleteW: %w", errno)
	}
	return nil
}
