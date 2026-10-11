package sysinput

import (
	"errors"
	"sync/atomic"
	"testing"
)

// Erros devolvidos por Guard. São distintos de propósito: quem lê um log
// precisa saber se a ação foi barrada por estar num teste (o caso normal,
// esperado) ou por o Agent ainda não ter habilitado (possível bug de
// inicialização).
var (
	// ErrDisabledInTest é devolvido quando o processo roda sob "go test".
	// Esta recusa é incondicional e não pode ser desligada.
	ErrDisabledInTest = errors.New("sysinput: ação no sistema real é sempre recusada sob go test")

	// ErrDisabled é devolvido fora de teste quando Enable ainda não foi
	// chamado.
	ErrDisabled = errors.New("sysinput: ação no sistema real não habilitada neste processo")
)

// enabled guarda a habilitação da segunda camada. Atômico porque o Agent
// tem várias threads (hook, áudio, overlay, runtime) e qualquer uma delas
// pode consultar o portão.
var enabled atomic.Bool

// Enable libera as ações que atuam no sistema real.
//
// Só o binário do Agent chama, uma vez, na inicialização. Não existe
// caminho por variável de ambiente ou arquivo de configuração — a
// ausência é deliberada, para que não haja forma acidental de ligar isto.
//
// Chamar Enable sob "go test" não tem efeito: a primeira camada do portão
// recusa de qualquer forma.
func Enable() {
	enabled.Store(true)
}

// Disable desliga de novo. Existe para o encerramento do Agent e para
// simetria; não é caminho comum.
func Disable() {
	enabled.Store(false)
}

// Guard é a única pergunta que o código de injeção precisa fazer antes de
// agir. Devolve nil apenas quando é seguro atuar no sistema real.
//
// Toda função que chame SendInput, escreva no clipboard, ou altere
// qualquer estado global do sistema, começa com:
//
//	if err := sysinput.Guard(); err != nil {
//	    return err
//	}
func Guard() error {
	// Primeira camada, incondicional: nenhum teste injeta entrada, nem
	// que tenha chamado Enable. Ver o doc do pacote para o incidente que
	// originou esta regra.
	if testing.Testing() {
		return ErrDisabledInTest
	}
	if !enabled.Load() {
		return ErrDisabled
	}
	return nil
}

// Allowed é açúcar para quem só quer decidir um caminho, sem propagar
// erro.
func Allowed() bool { return Guard() == nil }
