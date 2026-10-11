package update

import (
	"context"
	"errors"
	"fmt"
)

// State é a fase em que um ciclo de update se encontra.
type State int

const (
	// StateNoUpdate: nenhuma versão mais nova que a instalada — seja
	// porque o servidor não anunciou uma, seja porque a versão anunciada
	// não é, de fato, semanticamente maior que a atual (guarda contra
	// downgrade e contra reinstalar a mesma versão em loop).
	StateNoUpdate State = iota
	// StateAvailable: há uma versão mais nova anunciada e com todos os
	// campos necessários para prosseguir (versão, URL de download, hash);
	// ainda não foi baixada.
	StateAvailable
	// StateVerified: o pacote de update foi baixado e seu SHA-256
	// conferiu — ver VerifiedUpdate em download.go. Ainda não instalado.
	StateVerified
	// StateRejected: um download foi tentado mas rejeitado (hash
	// divergente, hash mal formado ou tamanho divergente do anunciado) —
	// THR-010. O arquivo rejeitado é removido e nunca fica acessível como
	// válido.
	StateRejected
)

// String nomeia o State para logs/depuração.
func (s State) String() string {
	switch s {
	case StateNoUpdate:
		return "no_update"
	case StateAvailable:
		return "available"
	case StateVerified:
		return "verified"
	case StateRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// ErrIncompleteUpdateInfo é devolvido quando o servidor sinaliza
// update_available=true mas omite um campo necessário para prosseguir
// (versão, URL de download ou hash). Tratado como falha limpa: nada é
// baixado nem instalado.
var ErrIncompleteUpdateInfo = errors.New("update: resposta do servidor sinaliza atualização mas está incompleta")

// Result é o resultado de uma checagem de update.
type Result struct {
	State State
	Info  UpdateInfo
}

// CheckForUpdate consulta provider e decide, LOCALMENTE, se há uma
// atualização de fato mais nova que currentVersion — nunca confia apenas no
// campo update_available do servidor. Isso é defesa em profundidade: um
// servidor com bug (ou comprometido) não consegue, só por essa resposta,
// empurrar um downgrade nem fazer o cliente reinstalar a versão já
// corrente (regressão proibida em docs/ai-spec/03-acceptance-criteria.md:
// "update instalar sozinho" — aqui a garantia irmã é "update nunca decide
// versão só pelo que o servidor mandou").
//
// Qualquer erro do provider (rede, JSON inválido, status não-200), campo
// obrigatório faltando, ou versão que não parseia como semver vira erro
// aqui — "falha limpa, sem instalar" (State fica StateNoUpdate).
func CheckForUpdate(ctx context.Context, provider UpdateProvider, currentVersion, arch string) (Result, error) {
	if provider == nil {
		return Result{State: StateNoUpdate}, errors.New("update: provider nil")
	}

	info, err := provider.Latest(ctx, currentVersion, arch)
	if err != nil {
		return Result{State: StateNoUpdate}, err
	}

	if !info.Available {
		return Result{State: StateNoUpdate}, nil
	}

	if info.Version == "" || info.DownloadURL == "" || info.SHA256 == "" {
		return Result{State: StateNoUpdate}, ErrIncompleteUpdateInfo
	}

	newer, err := IsNewer(currentVersion, info.Version)
	if err != nil {
		return Result{State: StateNoUpdate}, fmt.Errorf("update: não foi possível comparar versões: %w", err)
	}
	if !newer {
		return Result{State: StateNoUpdate}, nil
	}

	return Result{State: StateAvailable, Info: info}, nil
}
