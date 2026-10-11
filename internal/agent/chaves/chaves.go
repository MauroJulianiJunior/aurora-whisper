// Package chaves guarda a chave de API do provedor de IA (OpenAI,
// Anthropic) que quem usa cola na tela de Configurações do painel, e o
// token do PC na nuvem (internal/agent/nuvem; ADR-019, F6), com as mesmas
// regras.
//
// # Onde a chave mora
//
// No Gerenciador de Credenciais do Windows, como credencial genérica
// (CRED_TYPE_GENERIC) com o alvo "AuroraWhisper/<provedor>" — e em mais
// lugar nenhum: nunca em arquivo, nunca no banco (local_settings só tem a
// ESCOLHA do provedor e do modelo), nunca em log, nunca em resposta da API
// local, nunca em mensagem de erro. O Windows cifra a credencial com a
// chave do usuário (DPAPI); outro usuário da máquina não a lê.
//
// # Persistência: CRED_PERSIST_LOCAL_MACHINE
//
// Apesar do nome, LOCAL_MACHINE NÃO é "da máquina toda": toda credencial
// do Gerenciador é do usuário que a gravou. O que o Persist escolhe é
// QUANTO ela dura e ONDE ela vai:
//
//   - SESSION: some no logoff — a pessoa teria de colar a chave de novo a
//     cada logon.
//   - LOCAL_MACHINE: sobrevive a logoff e reinício, só neste computador.
//   - ENTERPRISE: tudo o de LOCAL_MACHINE e ainda viaja com o perfil
//     móvel do domínio para outras máquinas.
//
// LOCAL_MACHINE é o escopo do usuário atual NESTE computador, que é onde
// o Agent roda. ENTERPRISE espalharia um segredo de cobrança para máquinas
// em que o Aurora Whisper nem está instalado, sem ninguém pedir.
//
// # O portão
//
// Toda chamada ao Gerenciador — gravar, ler e apagar — passa por
// internal/agent/sysinput, que RECUSA sob go test. Ler também: um teste
// que lesse o Gerenciador real traria a chave de verdade de quem usa para
// dentro da saída de um teste. Os testes usam Memoria, o dublê deste
// pacote.
//
// Este arquivo não tem build tag: a interface, as regras (Validar, Final,
// o nome do alvo) e o dublê rodam e são testados no Linux do CI. A
// chamada ao Windows fica em chaves_windows.go.
//
// Validar e Final moram em internal/chavedeapi (ADR-021, C3a), um pacote
// neutro que o servidor da nuvem também usa para a chave do celular. Daqui
// eles são chamados sem mudança: os mesmos erros (as variáveis abaixo são as
// de lá), as mesmas mensagens e os mesmos testes.
package chaves

import (
	"errors"
	"strings"
	"sync"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/chavedeapi"
)

// Cofre guarda uma chave por provedor.
type Cofre interface {
	// Gravar guarda chave para provedor, substituindo a anterior. A
	// chave passa por Validar antes; o que é gravado é o que Validar
	// devolve.
	Gravar(provedor, chave string) error
	// Ler devolve a chave guardada, ou ErrAusente.
	Ler(provedor string) (string, error)
	// Apagar remove a chave. Não haver chave não é erro.
	Apagar(provedor string) error
}

// Erros de Validar, AlvoDe e Ler. Nenhum carrega a chave. Os três de Validar
// são os de internal/chavedeapi (a mesma variável: errors.Is vale dos dois
// lados).
var (
	ErrAusente       = errors.New("chaves: nenhuma chave guardada para este provedor")
	ErrProvedor      = errors.New("chaves: nome de provedor inválido")
	ErrChaveVazia    = chavedeapi.ErrChaveVazia
	ErrChaveLonga    = chavedeapi.ErrChaveLonga
	ErrChaveInvalida = chavedeapi.ErrChaveInvalida
	// ErrNaoSuportado é devolvido fora do Windows.
	ErrNaoSuportado = errors.New("chaves: o Gerenciador de Credenciais só existe no Windows")
)

// TamanhoMaximo é o maior blob que o Gerenciador de Credenciais aceita
// (CRED_MAX_CREDENTIAL_BLOB_SIZE = 5 * 512 bytes). Chave de API real tem
// uma ou duas centenas de caracteres. É o chavedeapi.TamanhoMaximo.
const TamanhoMaximo = chavedeapi.TamanhoMaximo

// PrefixoDoAlvo é o começo do nome da credencial no Gerenciador.
const PrefixoDoAlvo = "AuroraWhisper/"

// AlvoDe é o nome da credencial de provedor no Gerenciador:
// "AuroraWhisper/openai". O nome só aceita minúsculas, dígitos e hífen,
// até 32 caracteres: é o bastante para qualquer provedor, e impede que um
// nome vindo da rede monte um alvo de outro programa.
func AlvoDe(provedor string) (string, error) {
	if provedor == "" || len(provedor) > 32 {
		return "", ErrProvedor
	}
	for _, c := range provedor {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return "", ErrProvedor
		}
	}
	return PrefixoDoAlvo + provedor, nil
}

// ComentarioDe é o comentário da credencial de provedor, que aparece no
// Gerenciador de Credenciais para quem abrir e se perguntar de quem é
// aquilo. O token do PC na nuvem (os alvos "nuvem" e "nuvem-…" de
// internal/agent/nuvem, ADR-019, F6) não é chave de API, e o comentário diz.
func ComentarioDe(provedor string) string {
	if provedor == "nuvem" || strings.HasPrefix(provedor, "nuvem-") {
		return "Aurora Whisper: token deste PC na nuvem"
	}
	return "Aurora Whisper: chave de API do provedor de IA"
}

// Validar devolve a chave pronta para guardar: sem os espaços e quebras
// de linha das pontas, que vêm junto quando se copia de uma página.
//
// Por dentro, só ASCII imprimível sem espaço. Não é enfeite: a chave vira
// cabeçalho HTTP, e um caractere de controle ali é um cabeçalho quebrado
// — ou injetado. Espaço no meio é quase sempre cópia errada.
//
// Não confere o FORMATO de cada provedor (prefixo, tamanho): isso é
// suposição sobre o provedor, e o "Testar" da tela pergunta a ele de
// verdade.
//
// A regra é a de internal/chavedeapi, a mesma do servidor da nuvem.
func Validar(chave string) (string, error) { return chavedeapi.Validar(chave) }

// Final devolve os últimos caracteres da chave, para a tela mostrar QUAL
// chave está guardada sem mostrá-la: no máximo 4, e nunca mais que um
// quarto dela. Chave de API real tem dezenas de caracteres, e sai com 4;
// uma curta demais sai com menos, até nada.
//
// A chave já passou por Validar ao ser gravada, então é ASCII: byte e
// caractere são a mesma coisa. A regra é a de internal/chavedeapi.
func Final(chave string) string { return chavedeapi.Final(chave) }

// Memoria é o Cofre dos testes: um mapa, com as mesmas regras de
// validação do Gerenciador de Credenciais. Seguro para uso concorrente.
type Memoria struct {
	mu     sync.Mutex
	chaves map[string]string
}

// NovaMemoria devolve um Cofre em memória, vazio.
func NovaMemoria() *Memoria { return &Memoria{chaves: map[string]string{}} }

var _ Cofre = (*Memoria)(nil)

func (m *Memoria) Gravar(provedor, chave string) error {
	alvo, err := AlvoDe(provedor)
	if err != nil {
		return err
	}
	if chave, err = Validar(chave); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chaves[alvo] = chave
	return nil
}

func (m *Memoria) Ler(provedor string) (string, error) {
	alvo, err := AlvoDe(provedor)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	chave, ok := m.chaves[alvo]
	if !ok {
		return "", ErrAusente
	}
	return chave, nil
}

func (m *Memoria) Apagar(provedor string) error {
	alvo, err := AlvoDe(provedor)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.chaves, alvo)
	return nil
}
