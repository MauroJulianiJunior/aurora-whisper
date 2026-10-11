// Package chavedeapi tem as regras da chave de API de um provedor de IA que
// quem usa cola: a limpeza e a validação do que foi colado (Validar) e o fim
// que a tela mostra (Final). É um pacote neutro, sem Windows, sem banco e sem
// rede, para o Agent (internal/agent/chaves, a chave do PC no Gerenciador de
// Credenciais) e o servidor da nuvem (a chave do celular, cifrada no banco;
// ADR-021, decisão G e C3a) seguirem as mesmas regras sem o servidor importar
// um pacote do Agent.
//
// As regras saíram de internal/agent/chaves sem mudar: o Agent as chama por
// lá, com os mesmos erros (as variáveis de lá são estas) e as mesmas
// mensagens, e os testes dele continuam os mesmos.
//
// Nenhum erro daqui carrega a chave.
package chavedeapi

import "errors"

// Erros de Validar. As mensagens são as que o Agent sempre teve (o prefixo
// "chaves:" é o do pacote de onde as regras saíram): o Agent registra a
// mensagem de uma gravação recusada, e a linha do log dele continua igual.
var (
	ErrChaveVazia    = errors.New("chaves: chave vazia")
	ErrChaveLonga    = errors.New("chaves: chave longa demais")
	ErrChaveInvalida = errors.New("chaves: a chave tem espaço, quebra de linha ou caractere fora do ASCII")
)

// TamanhoMaximo é o maior tamanho de uma chave, em bytes: o maior blob que o
// Gerenciador de Credenciais do Windows aceita (CRED_MAX_CREDENTIAL_BLOB_SIZE =
// 5 * 512 bytes). Chave de API real tem uma ou duas centenas de caracteres. O
// servidor usa o mesmo teto (o texto cifrado da ai_provider_keys tem o teto
// dele mais os 16 bytes da etiqueta do GCM).
const TamanhoMaximo = 5 * 512

// TamanhoMaximoDoFinal é o maior Final: 4 caracteres.
const TamanhoMaximoDoFinal = 4

// Validar devolve a chave pronta para guardar: sem os espaços e quebras de
// linha das pontas, que vêm junto quando se copia de uma página.
//
// Por dentro, só ASCII imprimível sem espaço. Não é enfeite: a chave vira
// cabeçalho HTTP, e um caractere de controle ali é um cabeçalho quebrado — ou
// injetado. Espaço no meio é quase sempre cópia errada.
//
// Não confere o FORMATO de cada provedor (prefixo, tamanho): isso é suposição
// sobre o provedor, e o teste da chave pergunta a ele de verdade.
func Validar(chave string) (string, error) {
	inicio, fim := 0, len(chave)
	for inicio < fim && ehBranco(chave[inicio]) {
		inicio++
	}
	for fim > inicio && ehBranco(chave[fim-1]) {
		fim--
	}
	chave = chave[inicio:fim]
	switch {
	case chave == "":
		return "", ErrChaveVazia
	case len(chave) > TamanhoMaximo:
		return "", ErrChaveLonga
	}
	for i := 0; i < len(chave); i++ {
		if chave[i] < 0x21 || chave[i] > 0x7e {
			return "", ErrChaveInvalida
		}
	}
	return chave, nil
}

func ehBranco(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

// Final devolve os últimos caracteres da chave, para a tela mostrar QUAL chave
// está guardada sem mostrá-la: no máximo 4, e nunca mais que um quarto dela.
// Chave de API real tem dezenas de caracteres, e sai com 4; uma curta demais
// sai com menos, até nada.
//
// A chave já passou por Validar, então é ASCII: byte e caractere são a mesma
// coisa.
func Final(chave string) string {
	n := min(TamanhoMaximoDoFinal, len(chave)/4)
	return chave[len(chave)-n:]
}
