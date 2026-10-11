package learning

import (
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// MotivoMudaSentido: a grafia tem palavra que inverte ou muda o sentido da
// frase ("não rague", "nunca rague", "mais rague"). Só GrafiaArriscada o
// devolve: o motor de sugestão recusa o mesmo caso com MotivoPalavraDeLigacao,
// e este código existe para a tela dizer o porquê certo. Como os outros, é um
// código, nunca o texto.
const MotivoMudaSentido Motivo = "muda_sentido"

// Arriscada diz se texto é arriscado demais para virar a PALAVRA CERTA (o
// termo) pelo "Adicionar termo" das Configurações (ADR-020), e por quê.
// Devolve "" quando pode. Para as grafias erradas, a regra é a mais estrita
// de GrafiaArriscada.
//
// Recusa o texto em que NENHUMA palavra é de conteúdo: só palavra de ligação
// ("de", "das", "no"), de polaridade ("não", "sempre", "mais"), número
// ("2025", "1º") ou numeral por extenso ("dois"). Como termo, o casamento
// exato e o fonético o achariam no meio de qualquer texto. O dicionário
// recusa (dictionary.ErrPalavraComum).
//
// Basta uma palavra de conteúdo para passar: "Nada Consta" (a certidão) é um
// termo legítimo. As listas são as do motor de sugestão (sugestao.go), e os
// códigos também, para o log continuar num conjunto fechado: MotivoNumero
// quando há número ou numeral, MotivoPalavraDeLigacao no resto.
//
// Pura, sem texto em log.
func Arriscada(texto string) Motivo {
	ks := chaves(tokenizar(texto))
	if len(ks) == 0 {
		return ""
	}
	numero := false
	for _, k := range ks {
		switch {
		case ehNumero(k) || numeraisPorExtenso[k]:
			numero = true
		case ligacaoNorm[k] || polaridade[k]:
		default:
			return ""
		}
	}
	if numero {
		return MotivoNumero
	}
	return MotivoPalavraDeLigacao
}

// GrafiaArriscada diz se texto é arriscado demais para virar GRAFIA ERRADA
// (variante) de um termo pelo "Adicionar termo", e por quê. Devolve "" quando
// pode.
//
// A grafia é trocada pelo termo em todo ditado, e o que ela tiver a mais some
// do texto. Por isso, além do que Arriscada recusa:
//
//   - uma palavra de polaridade em QUALQUER lugar (MotivoMudaSentido): com
//     "não rague" para "RAG", "eu não rague no projeto" viraria "eu RAG no
//     projeto", e a negação sumiria sem aviso. É a regra do motor de sugestão
//     ("um alias com uma delas em qualquer lado nunca é a correção de um
//     termo", em avaliar);
//   - a grafia em que toda palavra é funcional e frequente (palavrasFuncionais,
//     somadas às de ligação e aos números): "eu", "né", "isso", "já", "foi".
//     Com "eu" para "RAG", "eu acho que eu vou" viraria "RAG acho que RAG
//     vou". O código é MotivoPalavraDeLigacao.
//
// Uma palavra de conteúdo ao lado de uma funcional passa ("n oito n", "eu
// rague"): a conferência "age?" do Agent ainda confere cada grafia.
//
// Pura, sem texto em log.
func GrafiaArriscada(texto string) Motivo {
	if m := Arriscada(texto); m != "" {
		return m
	}
	ks := chaves(tokenizar(texto))
	if temPolaridade(ks) {
		return MotivoMudaSentido
	}
	if len(ks) == 0 {
		return ""
	}
	for _, k := range ks {
		if !palavrasFuncionais[k] && !ligacaoNorm[k] && !ehNumero(k) && !numeraisPorExtenso[k] {
			return ""
		}
	}
	return MotivoPalavraDeLigacao
}

// palavrasFuncionais são palavras de classe fechada, ou quase, que estão entre
// as mais frequentes do ditado em português e nunca são a grafia errada de um
// termo: pronomes pessoais, possessivos e demonstrativos; ser, estar, ir, ter
// e haver nas formas curtas; advérbios curtos, interrogativos e as muletas da
// fala. Na forma de chave (sem acento), como ligacaoNorm. Não é um léxico de
// frequência: é a lista fechada do que a revisão de 01-out-2026 achou passando
// ("eu", "né", "isso", "já", "tá", "aí", "lá", "foi") e os vizinhos de classe.
var palavrasFuncionais = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(
		// pronomes pessoais e oblíquos
		"eu tu ele ela nós vós eles elas você vocês me te lhe lhes mim ti comigo contigo conosco " +
			"nele nela neles nelas dele dela deles delas " +
			// possessivos
			"meu minha meus minhas teu tua teus tuas seu sua seus suas nosso nossa nossos nossas " +
			// demonstrativos e as contrações
			"isso isto aquilo esse essa esses essas este esta estes estas aquele aquela aqueles aquelas " +
			"nisso nisto naquilo nesse nessa neste nesta naquele naquela " +
			"disso disto daquilo desse dessa deste desta daquele daquela " +
			// ser, estar, ir, ter e haver, nas formas curtas
			"sou és é somos são era eram fui foi fomos foram ser será seja sejam " +
			"estou está estamos estão estava estavam estive esteve estar tô tá tão tava tavam " +
			"vou vai vamos vão ia iam ir " +
			"tenho tem temos têm tinha tinham tive teve ter há havia houve " +
			// advérbios curtos e interrogativos
			"já lá cá aí ali aqui bem mal assim então agora ainda também muito pouco " +
			"quando onde como porque pois qual quais quem cada outro outra mesmo mesma " +
			// as contrações que faltam em ligacaoNorm
			"pro pros pras num numa dum duma " +
			// muletas e interjeições
			"né tipo cara olha bom ok oi ah eh uh hum aham uhum") {
		m[dictionary.Normalize(w)] = true
	}
	return m
}()
