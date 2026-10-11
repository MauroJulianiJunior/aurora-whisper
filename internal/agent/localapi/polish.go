package localapi

import (
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O que a tela sabe do Polish vem daqui: polish_disponivel e
// polish_ultimo_motivo em GET /api/settings, e modelo_sugerido em POST
// /api/ia/testar (docs/ai-spec/06-api-contract.md; ADR-016, decisões 10 e
// 14). Nada aqui carrega texto ditado: o Agent só entrega o MOTIVO, em
// forma de enum, e a frase sai desta tabela.

// DesfechoDoPolish é o que o Agent guarda, em memória, do último ditado
// que pediu o Polish. É o ponto de injeção de Preferencias.UltimoPolish:
// quem monta o Agent (cmd/agent, com o winruntime) o preenche a cada
// tentativa; a Local API só lê.
type DesfechoDoPolish struct {
	// Motivo é o resolver.MotivoFallback da tentativa, como texto
	// ("sem_credito", "prazo", "numeros"…). Vazio: a reescrita foi aceita.
	// Só o enum: um valor que a tabela não conhece vira uma frase genérica,
	// e o texto dele nunca chega à tela.
	Motivo string
	// Provedor é o que valia no keydown daquela tentativa ("openai" ou
	// "anthropic"), para a frase dizer a OpenAI ou a Anthropic mesmo que a
	// pessoa tenha trocado de provedor depois. Vazio ou desconhecido: "IA".
	Provedor string
	// Quando é o instante da tentativa. Zero: idade desconhecida, e o
	// desfecho vale como recente.
	Quando time.Time
}

// motivoRecente é por quanto tempo o último motivo continua na tela. A
// frase diz "o último ditado", e um "sem crédito" de anteontem, que a
// pessoa já resolveu sem ditar de novo, só confunde. O motivo também some
// ao reiniciar o Agent (vive só em memória).
const motivoRecente = 24 * time.Hour

// semPolish é o fecho de toda frase: o que aconteceu com o ditado.
const semPolish = "o último ditado saiu sem o Polish."

// frasesDoMotivo é o texto de cada motivo de fallback, em português simples
// e pronto para a tela. {provedor} vira o nome do provedor (OpenAI,
// Anthropic) ou "IA".
//
// Fica de fora, de propósito, o que NÃO é para mostrar:
//   - MotivoNenhum (o Polish agiu);
//   - MotivoCurto: ditado com menos de 4 palavras nem chega ao provedor, e
//     isso é o esperado — mostrar seria alarme falso.
//
// O 3,5 s de MotivoPrazo é o prazo do Agent na fala curta (ADR-016, decisão
// 4). No ditado longo o prazo cresce, até 8 s (emenda de 01-out-2026), e a
// frase continua verdadeira: quem passou do prazo passou dos 3,5 s. Ela não
// diz o prazo exato de propósito, para a fala curta continuar lendo o mesmo
// de antes. Se o piso mudar, esta frase muda junto.
var frasesDoMotivo = map[resolver.MotivoFallback]string{
	resolver.MotivoSemCliente:         "O Aurora Whisper não conseguiu chamar a IA: " + semPolish,
	resolver.MotivoSemChave:           "Sem chave de API da {provedor} guardada: " + semPolish,
	resolver.MotivoTransporte:         "Sem conexão com a {provedor}: " + semPolish,
	resolver.MotivoPrazo:              "A {provedor} demorou mais de 3,5 s: " + semPolish,
	resolver.MotivoChaveRecusada:      "A {provedor} recusou a chave: " + semPolish,
	resolver.MotivoSemCredito:         "Sem crédito na {provedor}: " + semPolish,
	resolver.MotivoLimite:             "Limite de uso da {provedor} atingido por agora: " + semPolish,
	resolver.MotivoModeloInexistente:  "A {provedor} não achou o modelo escolhido: " + semPolish,
	resolver.MotivoModeloIncompativel: "Este modelo da {provedor} não aceita o pedido do Polish: " + semPolish,
	resolver.MotivoSobrecarga:         "A {provedor} está fora do ar ou sobrecarregada: " + semPolish,
	resolver.MotivoPedidoRecusado:     "A {provedor} rejeitou o pedido do Polish: " + semPolish,
	resolver.MotivoRecusa:             "O modelo da {provedor} recusou responder: " + semPolish,
	resolver.MotivoTruncada:           "A resposta da {provedor} veio cortada: " + semPolish,
	resolver.MotivoRespostaInvalida:   "A {provedor} respondeu de um jeito inesperado: " + semPolish,

	// As guardas (ADR-016, decisão 7): a IA respondeu, mas o texto que voltou
	// não é o que a pessoa disse com sinais e quebras. O provedor não tem
	// culpa, e o nome dele só confundiria.
	resolver.MotivoJSON:         "A resposta da IA veio fora do formato: " + semPolish,
	resolver.MotivoFormato:      "A resposta da IA trouxe algo além do texto: " + semPolish,
	resolver.MotivoNumeros:      "A resposta da IA mudou os números: " + semPolish,
	resolver.MotivoSiglasTermos: "A resposta da IA mudou siglas, termos ou nomes: " + semPolish,
	resolver.MotivoPalavras:     "A resposta da IA mudou o texto: " + semPolish,
	resolver.MotivoTrechoNovo:   "A resposta da IA acrescentou palavras: " + semPolish,
	resolver.MotivoCaixa:        "A resposta da IA pôs palavras em maiúsculas: " + semPolish,
	resolver.MotivoDicionario:   "Não consegui ler o dicionário para conferir o texto: " + semPolish,
}

// fraseGenerica cobre o motivo que a tabela não conhece (um motivo novo no
// resolver que esta tabela ainda não ganhou): o texto do valor desconhecido
// nunca vai para a tela.
const fraseGenerica = "Algo impediu o Polish: " + semPolish

// fraseDoMotivo devolve a frase da tela para o desfecho, ou "" quando não
// há nada a mostrar: a reescrita foi aceita, o ditado era curto demais, ou
// já faz mais de motivoRecente.
func fraseDoMotivo(d DesfechoDoPolish, agora time.Time) string {
	m := resolver.MotivoFallback(d.Motivo)
	if m == resolver.MotivoNenhum || m == resolver.MotivoCurto {
		return ""
	}
	if !d.Quando.IsZero() && agora.Sub(d.Quando) > motivoRecente {
		return ""
	}
	frase, ok := frasesDoMotivo[m]
	if !ok {
		return fraseGenerica
	}
	nome := "IA"
	if ia.Valido(d.Provedor) {
		nome = ia.Nome(d.Provedor)
	}
	return strings.ReplaceAll(frase, "{provedor}", nome)
}

// modelosSugeridos é o modelo que o projeto SUGERE para o Polish em cada
// provedor. Fonte: o benchmark de 30-set-2026
// (docs/benchmarks/polish-2026-09-30.md, "Decisão"), pela regra da ADR-016,
// decisão 9 — falha crítica depois da guarda = 0, fallback ≤ 5% e p95 ≤
// 2,5 s; entre os que passam, o mais barato:
//   - OpenAI: gpt-6-luna passou (fallback 1,7%, p95 2,35 s, US$ 0,10 por
//     1.000 ditados);
//   - Anthropic: nenhum candidato passou (claude-haiku-4-5 ficou com
//     fallback de 6,4%, acima dos 5%), então não há sugestão.
//
// É só uma sugestão (ADR-016, decisão 10): a tela a marca e, se nenhum
// modelo foi escolhido e ela consta na lista da conta, a pré-seleciona no
// rascunho. Nada valida ai_model contra este mapa, e o Agent nunca a exige:
// os modelos baratos saem do ar (o gpt-5-nano em 11-dez-2026), e
// ai_model segue texto livre. Mudar o vencedor é rodar o polishbench de
// novo e atualizar esta linha.
var modelosSugeridos = map[string]string{
	ia.OpenAI: "gpt-6-luna",
}

// modeloSugerido devolve o modelo sugerido para o provedor, ou "" quando o
// projeto não sugere nenhum.
func modeloSugerido(provedor string) string { return modelosSugeridos[provedor] }
