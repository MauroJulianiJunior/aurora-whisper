//go:build windows && amd64

package chaves

import (
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestCredentialW_LayoutDoWincred confere o CREDENTIALW campo a campo
// contra o layout de wincred.h em x64. Um campo fora de ordem aqui não
// dá erro de compilação: o CredWriteW leria o tamanho do blob no lugar do
// ponteiro, e o que iria para o Gerenciador seria lixo — ou pedaço da
// memória do Agent. Não chama o sistema: só mede a struct.
func TestCredentialW_LayoutDoWincred(t *testing.T) {
	var c credentialW
	for _, campo := range []struct {
		nome       string
		got, quero uintptr
	}{
		{"Flags", unsafe.Offsetof(c.Flags), 0},
		{"Type", unsafe.Offsetof(c.Type), 4},
		{"TargetName", unsafe.Offsetof(c.TargetName), 8},
		{"Comment", unsafe.Offsetof(c.Comment), 16},
		{"LastWritten", unsafe.Offsetof(c.LastWritten), 24},
		{"CredentialBlobSize", unsafe.Offsetof(c.CredentialBlobSize), 32},
		{"CredentialBlob", unsafe.Offsetof(c.CredentialBlob), 40},
		{"Persist", unsafe.Offsetof(c.Persist), 48},
		{"AttributeCount", unsafe.Offsetof(c.AttributeCount), 52},
		{"Attributes", unsafe.Offsetof(c.Attributes), 56},
		{"TargetAlias", unsafe.Offsetof(c.TargetAlias), 64},
		{"UserName", unsafe.Offsetof(c.UserName), 72},
	} {
		if campo.got != campo.quero {
			t.Errorf("%s no offset %d, o wincred.h diz %d", campo.nome, campo.got, campo.quero)
		}
	}
	if got := unsafe.Sizeof(c); got != 80 {
		t.Errorf("sizeof(CREDENTIALW) = %d, o wincred.h diz 80", got)
	}
}

// TestNovaCredencial_OQueVaiAoCredWriteW confere, campo a campo, o
// CREDENTIALW que Gravar entrega ao sistema: tipo genérico, escopo
// LOCAL_MACHINE (2), e não ENTERPRISE (3) — que viajaria no perfil
// móvel do domínio para outras máquinas. Os números são os de wincred.h,
// escritos aqui, e não as constantes do pacote: trocar a constante ou o
// campo em Gravar quebra este teste. Não chama o sistema.
func TestNovaCredencial_OQueVaiAoCredWriteW(t *testing.T) {
	var alvo, comentario uint16
	blob := []byte("sk-teste-0123456789")
	c := novaCredencial(&alvo, &comentario, blob)
	if c.Type != 1 {
		t.Errorf("Type = %d, quero 1 (CRED_TYPE_GENERIC)", c.Type)
	}
	if c.Persist != 2 {
		t.Errorf("Persist = %d, quero 2 (CRED_PERSIST_LOCAL_MACHINE)", c.Persist)
	}
	if c.TargetName != &alvo || c.Comment != &comentario {
		t.Error("TargetName ou Comment não aponta para o que foi passado")
	}
	if c.CredentialBlob != &blob[0] || c.CredentialBlobSize != uint32(len(blob)) {
		t.Errorf("blob = %p/%d, quero %p/%d", c.CredentialBlob, c.CredentialBlobSize, &blob[0], len(blob))
	}
	if c.Flags != 0 || c.AttributeCount != 0 || c.Attributes != 0 || c.TargetAlias != nil || c.UserName != nil {
		t.Errorf("campo que devia ficar zerado não ficou: %+v", c)
	}
}

// TestConstantesDoWincred: os valores de wincred.h. LOCAL_MACHINE, e não
// ENTERPRISE: ver o doc do pacote.
func TestConstantesDoWincred(t *testing.T) {
	if credTypeGeneric != 1 {
		t.Errorf("CRED_TYPE_GENERIC = %d, quero 1", credTypeGeneric)
	}
	if credPersistLocalMachine != 2 {
		t.Errorf("CRED_PERSIST_LOCAL_MACHINE = %d, quero 2", credPersistLocalMachine)
	}
}

// TestCredencialDe_AlvoEComentario confere o alvo e o comentário que Gravar
// manda ao Gerenciador para cada provedor: o token da nuvem não aparece lá
// como "chave de API". Não chama o sistema.
func TestCredencialDe_AlvoEComentario(t *testing.T) {
	blob := []byte("sk-teste-0123456789")
	for provedor, quero := range map[string]string{
		"openai":         "Aurora Whisper: chave de API do provedor de IA",
		"anthropic":      "Aurora Whisper: chave de API do provedor de IA",
		"nuvem":          "Aurora Whisper: token deste PC na nuvem",
		"nuvem-portatil": "Aurora Whisper: token deste PC na nuvem",
	} {
		c, err := credencialDe(provedor, blob)
		if err != nil {
			t.Fatalf("%s: %v", provedor, err)
		}
		if alvo := windows.UTF16PtrToString(c.TargetName); alvo != "AuroraWhisper/"+provedor {
			t.Errorf("%s: alvo = %q", provedor, alvo)
		}
		if comentario := windows.UTF16PtrToString(c.Comment); comentario != quero {
			t.Errorf("%s: comentário = %q, quero %q", provedor, comentario, quero)
		}
		if c.CredentialBlob != &blob[0] || c.Type != 1 || c.Persist != 2 {
			t.Errorf("%s: a credencial = %+v", provedor, c)
		}
	}
	if _, err := credencialDe("../x", blob); !errors.Is(err, ErrProvedor) {
		t.Errorf("provedor inválido: %v", err)
	}
}
