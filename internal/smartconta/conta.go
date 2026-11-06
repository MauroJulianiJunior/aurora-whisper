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
	// TermosDaCopia são os termos da cópia do dicionário do PC (ADR-023, D), de
	// onde a montagem partiu; 0 sem cópia.
	TermosDaCopia int
	// ComCopia diz se a montagem partiu da cópia do dicionário do PC.
	ComCopia bool
	// JaNaCopia são os pares que a cópia já tem (a mesma variante, do mesmo
	// termo): não passam pelo lote, que não mudaria nada.
	JaNaCopia int
}

// Conta é o Smart da conta: o resolver montado do aprendido que a nuvem guarda,
// pronto para os ditados do celular (ADR-021, decisão F). Seguro para uso
// concorrente: depois de montado, só se lê.
type Conta struct {
	usuario string
	smart   *resolver.SmartResolver
	resumo  Resumo
}

// Montar monta o Smart da conta a partir do aprendido dela, sem a cópia do
// dicionário de um PC: é MontarComCopia com a cópia nil, o PC de dicionário
// vazio.
func Montar(usuario string, pares []dictionary.ParDaNuvem) (*Conta, error) {
	return MontarComCopia(usuario, nil, pares)
}

// MontarComCopia monta o Smart da conta pelo MESMO caminho do PC que recebe o
// aprendido da nuvem: a semente do dicionário, AplicarPares com as mesmas
// conferências (Conferencias), depois SmartDaSemente.
//
// A semente é a cópia do dicionário do PC que é a fonte (ADR-023, D), já
// conferida por dictionary.LerCopia, com o usuário trocado pelo dono da conta;
// sem cópia (nil), é a semente vazia, e os mesmos pares dão o mesmo resultado
// que dariam num PC de dicionário vazio. A cópia não é alterada: a montagem
// trabalha num clone.
//
// pares vêm na ordem do ListLearned: do mais recente (last_seq) ao mais antigo.
// O lote os aplica do mais antigo ao mais novo, a ordem em que o pull os entrega
// ao PC, porque quem entra primeiro leva a melhor num conflito (uma variante de
// um termo que é variante de outro). Só entram os pares ativos e sem domínio
// (veja Resumo.ComDominio); os outros não mudam nada, como no PC de dicionário
// vazio (dominio_ausente e ausente). Um par que a cópia já tem (o PC fonte já o
// recebeu pelo pull) não muda nada, como no PC.
//
// O erro nunca traz a variante, o termo nem o domínio.
func MontarComCopia(usuario string, copia *dictionary.Semente, pares []dictionary.ParDaNuvem) (*Conta, error) {
	if usuario == "" {
		return nil, ErrSemUsuario
	}
	base := dictionary.Semente{Usuario: usuario}
	if copia != nil {
		base = clonarSemente(*copia)
		base.Usuario = usuario
	}
	if len(pares) > MaxPares {
		pares = pares[:MaxPares]
	}
	resumo := Resumo{Recebidos: len(pares)}
	var jaTem map[string]string
	if copia != nil {
		resumo.ComCopia = true
		resumo.TermosDaCopia = len(copia.Entradas)
		jaTem = variantesDaCopia(base)
	}
	aplicaveis := make([]dictionary.ParDaNuvem, 0, len(pares))
	for i := len(pares) - 1; i >= 0; i-- {
		p := pares[i]
		switch {
		case p.Desfeito:
			resumo.Desfeitos++
		case p.Dominio != "":
			resumo.ComDominio++
		case jaTem != nil && jaTem[chaveDaCopia(p.Variante)] == chaveDaCopia(p.Termo):
			// O par que a cópia já tem (o PC fonte já o recebeu pelo pull): o
			// lote não mudaria nada, e conferi-lo de novo custa uma volta pelo
			// dicionário inteiro (ADR-023, "Riscos").
			resumo.JaNaCopia++
		default:
			aplicaveis = append(aplicaveis, p)
		}
	}

	// As conferências são as do PC; o "agiria" acompanha a semente em vez de
	// montar um Smart novo a cada par (acompanhante.go).
	conferencias, acompanha := conferenciasDaConta()
	semente, lote, err := base.AplicarPares(aplicaveis, conferencias)
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

// variantesDaCopia indexa as variantes SEM restrição da semente pela chave
// (chaveDaCopia), cada uma com a chave do termo dela. Os pares que chegam aqui
// são sem domínio e sem restrição, então só a variante comum é "o mesmo par": a
// restrita o lote poderia tratar de outro jeito, e fica com ele. Uma variante
// que aparece em dois termos também fica com o lote.
func variantesDaCopia(s dictionary.Semente) map[string]string {
	idx := map[string]string{}
	repetidas := map[string]bool{}
	for _, e := range s.Entradas {
		termo := chaveDaCopia(e.Canonica)
		for _, v := range e.Alias {
			k := chaveDaCopia(v)
			if k == "" || repetidas[k] {
				continue
			}
			if t, ok := idx[k]; ok && t != termo {
				delete(idx, k)
				repetidas[k] = true
				continue
			}
			idx[k] = termo
		}
	}
	return idx
}

// chaveDaCopia é a comparação do lote do Aprender (dictionary: mesmaCoisa): a
// forma normalizada, com os espaços colapsados.
func chaveDaCopia(s string) string {
	return dictionary.CollapseSpaces(dictionary.Normalize(s))
}

// clonarSemente copia a semente sem dividir as listas nem os mapas com ela: a
// cópia do dicionário pode estar em uso por outra montagem.
func clonarSemente(s dictionary.Semente) dictionary.Semente {
	c := dictionary.Semente{Usuario: s.Usuario, Entradas: make([]dictionary.EntradaSemente, 0, len(s.Entradas))}
	if s.Dominios != nil {
		c.Dominios = make(map[string][]string, len(s.Dominios))
		for d, apps := range s.Dominios {
			c.Dominios[d] = append([]string(nil), apps...)
		}
	}
	if s.AliasOrigem != nil {
		c.AliasOrigem = make(map[string]string, len(s.AliasOrigem))
		for v, o := range s.AliasOrigem {
			c.AliasOrigem[v] = o
		}
	}
	for _, e := range s.Entradas {
		c.Entradas = append(c.Entradas, copiaDaEntrada(e))
	}
	return c
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
