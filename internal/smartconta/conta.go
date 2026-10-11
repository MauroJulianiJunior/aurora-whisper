package smartconta

import (
	"context"
	"errors"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/repeticao"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// MaxPares é quantos pares aprendidos a conta usa: o teto do ListLearned
// (cloudstore.MaxLearnedList, 500). Um pedido com mais pares usa os MaxPares
// mais recentes, para o custo de montar o Smart ter um teto.
const MaxPares = 500

// ErrSemUsuario: a conta precisa de um usuário. O dicionário e a busca são
// escopados por ele, e o repositório recusa a entrada sem dono.
var ErrSemUsuario = errors.New("smartconta: a conta precisa de um usuário")

// ErrMontar: não deu para montar o Smart da conta. Não acontece com pares que o
// lote aceita, e a causa fica de fora de propósito: os erros do dicionário
// podem citar a variante ou o termo, que são texto da pessoa e não vão a log
// nem a mensagem de erro.
var ErrMontar = errors.New("smartconta: não consegui montar o Smart da conta")

// Resumo é o que a montagem do Smart da conta fez, só em contagens: a variante,
// o termo e o domínio são texto que a pessoa ditou e nunca vão a log nem a erro
// (CLAUDE.md, "Safety rules").
type Resumo struct {
	// Recebidos são os pares que chegaram (até MaxPares).
	Recebidos int
	// ComDominio são os pares presos a um domínio: não agem no celular, de
	// propósito. O PWA não sabe em que programa o texto vai entrar, e a troca
	// presa a um programa valeria em todos (ADR-021, decisão F, fail-closed).
	ComDominio int
	// Desfeitos são os pares desfeitos na nuvem: não agem.
	Desfeitos int
	// Aplicados são os pares que entraram no Smart da conta e agem.
	Aplicados int
	// NaoAplicados são os pares que o lote recusou (palavra comum, não agiria,
	// variante de outro termo...), pelas mesmas regras do PC.
	NaoAplicados int
	// Termos são os termos que o Smart da conta tem.
	Termos int
}

// Conta é o Smart da conta: o resolver montado do aprendido que a nuvem guarda,
// pronto para os ditados do celular (ADR-021, decisão F). Seguro para uso
// concorrente: depois de montado, só se lê.
type Conta struct {
	usuario string
	smart   *resolver.SmartResolver
	resumo  Resumo
}

// Montar monta o Smart da conta a partir do aprendido dela, pelo MESMO caminho
// do PC que recebe o aprendido da nuvem: Semente{}.AplicarPares com as mesmas
// conferências (Conferencias), depois SmartDaSemente. O dicionário completo de
// cada PC (os termos de antes, os adicionados, os pares presos a um domínio)
// não existe na nuvem, e com o mesmo caminho os mesmos pares dão o mesmo
// resultado que dariam num PC de dicionário vazio.
//
// pares vêm na ordem do ListLearned: do mais recente (last_seq) ao mais antigo.
// O lote os aplica do mais antigo ao mais novo, a ordem em que o pull os entrega
// ao PC, porque quem entra primeiro leva a melhor num conflito (uma variante de
// um termo que é variante de outro). Só entram os pares ativos e sem domínio
// (veja Resumo.ComDominio); os outros não mudam nada, como no PC de dicionário
// vazio (dominio_ausente e ausente).
//
// O erro nunca traz a variante, o termo nem o domínio.
func Montar(usuario string, pares []dictionary.ParDaNuvem) (*Conta, error) {
	if usuario == "" {
		return nil, ErrSemUsuario
	}
	if len(pares) > MaxPares {
		pares = pares[:MaxPares]
	}
	resumo := Resumo{Recebidos: len(pares)}
	aplicaveis := make([]dictionary.ParDaNuvem, 0, len(pares))
	for i := len(pares) - 1; i >= 0; i-- {
		p := pares[i]
		switch {
		case p.Desfeito:
			resumo.Desfeitos++
		case p.Dominio != "":
			resumo.ComDominio++
		default:
			aplicaveis = append(aplicaveis, p)
		}
	}

	// As conferências são as do PC; o "agiria" acompanha a semente em vez de
	// montar um Smart novo a cada par (acompanhante.go).
	conferencias, acompanha := conferenciasDaConta()
	semente, lote, err := dictionary.Semente{Usuario: usuario}.AplicarPares(aplicaveis, conferencias)
	if err != nil {
		return nil, ErrMontar
	}
	for _, r := range lote.Resultados {
		switch r.Situacao {
		case dictionary.SituacaoAplicado:
			resumo.Aplicados++
		case dictionary.SituacaoNaoAplicado:
			resumo.NaoAplicados++
		}
	}
	resumo.Termos = len(semente.Entradas)

	// O Smart da semente final: o que o acompanhante já tem, ou um novo.
	smart, err := acompanha.smartDe(semente)
	if err != nil {
		return nil, ErrMontar
	}
	return &Conta{usuario: usuario, smart: smart, resumo: resumo}, nil
}

// Resumo devolve as contagens da montagem.
func (c *Conta) Resumo() Resumo { return c.resumo }

// Ditado é o que o Smart da conta fez com o texto do ASR.
type Ditado struct {
	// Bruto é o texto do ASR como chegou: o raw_text do histórico.
	Bruto string
	// Texto é o texto final: sem o laço de repetição e com as trocas da conta.
	Texto string
	// Lacos é quantos laços de repetição o Colapsar desfez, e
	// PalavrasRemovidas quantas palavras saíram com eles. A pessoa precisa
	// conferir o texto quando saiu alguma (o laço costuma tomar o lugar de
	// parte da fala).
	Lacos             int
	PalavrasRemovidas int
	// Substituicoes é quantas trocas o Smart fez.
	Substituicoes int
	// SmartFalhou: o Smart devolveu erro, e Texto é o texto do ASR sem o laço,
	// como no PC (ADR-021, decisão F). O motivo nunca vem em texto.
	SmartFalhou bool
}

// Resolver passa o texto do ASR pelo Colapsar e pelo Smart da conta, como o
// ditado do PC (winruntime): o laço de repetição do ASR sai, e o texto sem ele
// vai ao Smart com Domain nulo (o desconhecido), sem contexto e com a
// CloudPolicy desligada. O ditado do celular não depende de rede para o Smart,
// nem sabe em que programa o texto vai entrar.
//
// Se o Smart falha, segue o texto do ASR sem o laço (SmartFalhou), como no PC.
// Não loga nada: o texto é o que a pessoa ditou.
func (c *Conta) Resolver(ctx context.Context, bruto string) Ditado {
	return c.ResolverParaPolir(ctx, bruto).Ditado
}

// Resolvido é o Ditado e o que o Smart recebeu e devolveu, para o Polish
// (Reescrever) partir do MESMO resultado, com as trocas que o Smart fez: as
// guardas do Polish protegem os termos trocados. Carrega texto ditado: não vai
// a log, e vive só enquanto o ditado é resolvido.
type Resolvido struct {
	Ditado
	pedido    resolver.ResolveRequest
	resultado resolver.ResolveResult
}

// ResolverParaPolir é o Resolver de quem vai passar o ditado pelo Polish depois
// (Reescrever): o mesmo Ditado, mais o pedido e o resultado do Smart.
func (c *Conta) ResolverParaPolir(ctx context.Context, bruto string) Resolvido {
	r := Resolvido{Ditado: Ditado{Bruto: bruto, Texto: bruto}}
	if col := repeticao.Colapsar(bruto); col.Colapsou() {
		r.Texto = col.Texto
		r.Lacos = len(col.Colapsos)
		r.PalavrasRemovidas = col.PalavrasRemovidas
	}
	r.pedido = resolver.ResolveRequest{
		UserID: c.usuario,
		// O idioma é o do PC (winruntime): o Smart não o lê hoje, e a
		// paridade fica de pé se ele passar a ler.
		ASRResult:   asr.ASRResult{Text: r.Texto, Language: "pt-BR"},
		CloudPolicy: resolver.CloudDisabled,
	}
	out, err := c.smart.Resolve(ctx, r.pedido)
	if err != nil {
		r.SmartFalhou = true
		return r
	}
	r.resultado = out
	r.Texto = out.FinalText
	r.Substituicoes = len(out.AppliedReplacements)
	return r
}

// Smart devolve o resolver do Smart da conta, para o Polish montar as guardas
// sobre ele (polish.Montagem.Smart): elas consultam o aprendido da conta
// localmente, e nada dele vai ao provedor.
func (c *Conta) Smart() *resolver.SmartResolver { return c.smart }

// Reescrever passa pelo Polish o ditado que ResolverParaPolir devolveu, como o PC
// passa o resultado do Smart (resolver.PolishResolver.Reescrever, com o mesmo
// pedido e as trocas que o Smart fez). pr é o Polish do ditado, montado sobre
// Smart() (no servidor, polish.Montagem.Abrir); o prazo é o do ctx, de quem
// chama.
//
// Devolve o resultado do Polish: FinalText é o do Polish quando Reescrito, e o do
// Smart com o MotivoFallback quando não. Se o Smart falhou (SmartFalhou), o
// Polish nem é chamado: sem como conferir os termos da pessoa, o texto não sai,
// e o motivo é resolver.MotivoDicionario, como no PC quando a consulta local
// falha. Sem Polish montado (pr nil), o motivo é resolver.MotivoSemCliente. Não
// loga nada.
func (c *Conta) Reescrever(ctx context.Context, pr *resolver.PolishResolver, d Resolvido) resolver.ResolveResult {
	if d.SmartFalhou || pr == nil {
		res := resolver.ResolveResult{RawText: d.Bruto, FinalText: d.Texto, Mode: resolver.ModePolish}
		res.MotivoFallback = resolver.MotivoDicionario
		if pr == nil {
			res.MotivoFallback = resolver.MotivoSemCliente
		}
		return res
	}
	return pr.Reescrever(ctx, d.pedido, d.resultado)
}
