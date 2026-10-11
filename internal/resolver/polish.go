package resolver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrPolishSemChave é o erro que um PolishClient devolve quando a pessoa não
// tem chave salva para o provedor escolhido. Polir o traduz em
// MotivoSemChave; qualquer outro erro vira MotivoTransporte (ou
// MotivoPrazo, se for de tempo).
var ErrPolishSemChave = errors.New("polish: sem chave salva para o provedor")

// ErroPolish é o erro de um PolishClient que sabe DIZER por que a chamada ao
// provedor falhou (chave recusada, sem crédito, modelo inexistente...). Polir
// devolve o Motivo no resultado, para a tela mostrar. O texto do erro é fixo,
// só o motivo: nunca o corpo da resposta do provedor, nunca a chave, nunca o
// ditado.
type ErroPolish struct {
	Motivo MotivoFallback
}

func (e *ErroPolish) Error() string { return "polish: " + string(e.Motivo) }

// PolishClient chama o provedor de IA escolhido pela pessoa (OpenAI ou
// Anthropic, com a chave dela) e devolve o JSON BRUTO que o modelo
// respondeu — {"final_text": "..."} —, não o envelope do provedor. A
// decodificação e a validação acontecem em Polir, nunca no cliente: o
// cliente não confia em nada, o resolver não confia no cliente.
//
// O contrato de erro é o que Polir enxerga: ErrPolishSemChave -> sem_chave;
// *ErroPolish -> o Motivo que ele traz; context.DeadlineExceeded ou net.Error
// de timeout -> prazo; o resto -> transporte. Erros NÃO podem carregar corpo
// de resposta do provedor nem a chave.
type PolishClient interface {
	Polish(ctx context.Context, req PolishRequest) ([]byte, error)
}

// PolishRequest é a entrada de PolishClient.Polish.
//
// Text é o resultado já resolvido pelo Smart (não o transcript bruto):
// Polish organiza um texto já corrigido, nunca parte do zero. O cliente NÃO
// deve montar prompt nem mensagem por conta própria — Instrucoes, Mensagem e
// Schema vêm prontos daqui, para o que sai da máquina ser exatamente o que
// as guardas e os testes conhecem. Não há campo de contexto: nada de janela,
// app ou dicionário chega ao provedor.
//
// PRIVACIDADE: a requisição carrega o ditado inteiro (Text e Mensagem), que
// NUNCA pode ir para log (docs/ai-spec/09-security-privacy.md). Por isso ela
// não sabe se serializar nem se imprimir: nenhum campo tem tag JSON, e
// String, GoString, Format e LogValue mostram só a versão do prompt e o
// tamanho do texto. Um slog.Info("...", "req", req), um %+v num erro ou um
// json.Marshal de depuração não levam o ditado junto. Quem loga uma chamada
// ao provedor escolhe os campos um a um (PromptRef, o tamanho) — nunca o
// PolishRequest inteiro por outro caminho que não estes.
type PolishRequest struct {
	Text     string `json:"-"`
	Language string `json:"-"`

	// Instrucoes é PolishPrompt: vai em `instructions` (OpenAI) ou `system`
	// (Anthropic).
	Instrucoes string `json:"-"`
	// Mensagem é a mensagem `user`: PolishMensagemUsuario(Text).
	Mensagem string `json:"-"`
	// Schema é PolishSchemaSaida(), com PolishSchemaNome como nome.
	Schema map[string]any `json:"-"`
	// PromptRef é PolishPromptRef, para o log do cliente ("polish-ptbr@1.0.2").
	PromptRef string `json:"-"`
}

// redigido é a única descrição de um PolishRequest que pode ir a log: o
// prompt e o tamanho do ditado em caracteres, nunca o texto.
func (r PolishRequest) redigido() string {
	return fmt.Sprintf("PolishRequest{prompt:%s caracteres:%d}", r.PromptRef, utf8.RuneCountInString(r.Text))
}

// String implementa fmt.Stringer sem o texto do ditado.
func (r PolishRequest) String() string { return r.redigido() }

// GoString implementa fmt.GoStringer (%#v) sem o texto do ditado.
func (r PolishRequest) GoString() string { return r.redigido() }

// Format implementa fmt.Formatter: qualquer verbo (%v, %+v, %s, %q, %d, %x)
// imprime só a versão redigida.
func (r PolishRequest) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, r.redigido())
}

// LogValue implementa slog.LogValuer: o log estruturado leva o prompt e o
// tamanho, nunca o texto.
func (r PolishRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("prompt", r.PromptRef),
		slog.Int("caracteres", utf8.RuneCountInString(r.Text)),
	)
}

// PolirOpcoes são os parâmetros de Polir. O zero valor é válido: usa os
// padrões de PolishMinPalavras e PolishMaxEdicao, sem termos de dicionário.
type PolirOpcoes struct {
	// Idioma é o código de idioma do ASR; só viaja no PolishRequest.
	Idioma string
	// MinPalavras: menos palavras que isto e a chamada nem acontece. Zero
	// usa PolishMinPalavras.
	MinPalavras int
	// MaxEdicao é a fração máxima das palavras não estruturais da entrada que
	// a saída pode APAGAR além das que têm licença (hesitação, gagueira,
	// pontuação falada, ordinal, o "e" da lista). Zero (ou negativo) usa
	// PolishMaxEdicao, que é zero: nenhuma palavra comum pode sair. Trocar ou
	// acrescentar palavra não tem folga, com nenhum valor.
	MaxEdicao float64
	// Termos são formas canônicas do dicionário pessoal, candidatas a estar
	// no texto, que a consulta LOCAL levantou; a guarda de siglas e termos
	// confere quais estão de fato presentes. Nunca são enviados ao provedor.
	Termos []string
}

func (o PolirOpcoes) minPalavras() int {
	if o.MinPalavras > 0 {
		return o.MinPalavras
	}
	return PolishMinPalavras
}

func (o PolirOpcoes) maxEdicao() float64 {
	if o.MaxEdicao > 0 {
		return o.MaxEdicao
	}
	return PolishMaxEdicao
}

// fallbackPolish devolve o resultado do Smart marcado com o motivo. Com
// aviso, acrescenta um Warning FIXO ("polish: mantido resultado smart
// (motivo=...)"): só o motivo, nunca texto do modelo, do provedor ou do
// ditado — o runtime loga os Warnings.
//
// Sem aviso quando o Polish nem foi tentado (curto, sem cliente): não é
// falha, e "ok, pode mandar" não deve poluir o log.
func fallbackPolish(res ResolveResult, motivo MotivoFallback, avisar bool) ResolveResult {
	res.Reescrito = false
	res.MotivoFallback = motivo
	if avisar {
		// Cópia: append direto poderia escrever no array de Warnings do
		// resultado do Smart, que o chamador ainda tem.
		avisos := make([]string, 0, len(res.Warnings)+1)
		avisos = append(avisos, res.Warnings...)
		res.Warnings = append(avisos, "polish: mantido resultado smart (motivo="+string(motivo)+")")
	}
	return res
}

// motivoDoErro classifica o erro do cliente sem olhar a mensagem dele.
//
// Um *ErroPolish (achado por errors.As, então vale embrulhado) traz o motivo
// que o cliente já classificou; só o motivo vazio é ignorado, porque "sem
// fallback" não é um motivo de falha.
//
// Prazo é: o contexto expirou, ou o erro é um net.Error de timeout —
// context.DeadlineExceeded e o timeout do net/http implementam net.Error com
// Timeout() verdadeiro, então um cliente que embrulha (%w) qualquer um deles
// cai aqui. Cancelamento (context.Canceled) NÃO é prazo: é o Agent encerrando.
func motivoDoErro(ctx context.Context, err error) MotivoFallback {
	var rede net.Error
	var polish *ErroPolish
	switch {
	case errors.Is(err, ErrPolishSemChave):
		return MotivoSemChave
	case errors.As(err, &polish) && polish != nil && polish.Motivo != MotivoNenhum:
		return polish.Motivo
	case errors.Is(ctx.Err(), context.DeadlineExceeded),
		errors.As(err, &rede) && rede.Timeout():
		return MotivoPrazo
	}
	return MotivoTransporte
}

// Polir organiza o texto do Smart pelo provedor e valida a resposta. É PURA
// no sentido que importa: recebe o resultado do Smart pronto e devolve o
// mesmo resultado, com FinalText trocado só se TODAS as guardas passarem.
// Assim o Smart continua no laço de eventos do Agent e só a chamada de rede
// roda numa goroutine própria, com prazo.
//
// Ordem: curto -> chamada -> JSON -> formato -> números -> siglas, termos e nomes ->
// palavras -> caixa. Qualquer falha devolve o texto do Smart intacto, com
// Reescrito=false e o MotivoFallback. Nunca devolve erro: "Polish falhou ->
// Smart" nunca perde o ditado (docs/ai-spec/16-ai-behavior-spec.md,
// "Fallback").
//
// Reescrito=true significa "a resposta do provedor foi aceita e FinalText
// vem dela" — inclusive quando ela devolveu o texto igual (texto que já
// estava organizado). O modo do histórico pode ser "polish" nesse caso.
//
// Só o texto do Smart sai da máquina (Text/Mensagem). Os `warnings` do
// modelo não existem no schema e, se vierem, são ignorados: nada do que o
// modelo escreve fora de final_text chega ao resultado nem ao log.
func Polir(ctx context.Context, cliente PolishClient, smart ResolveResult, opts PolirOpcoes) ResolveResult {
	res := smart
	res.Mode = ModePolish
	res.Reescrito = false
	res.MotivoFallback = MotivoNenhum

	if cliente == nil {
		return fallbackPolish(res, MotivoSemCliente, false)
	}
	entrada := smart.FinalText
	if !guardaCurto(entrada, opts.minPalavras()) {
		return fallbackPolish(res, MotivoCurto, false)
	}

	bruto, err := cliente.Polish(ctx, PolishRequest{
		Text:       entrada,
		Language:   opts.Idioma,
		Instrucoes: PolishPrompt,
		Mensagem:   PolishMensagemUsuario(entrada),
		Schema:     PolishSchemaSaida(),
		PromptRef:  PolishPromptRef,
	})
	if err != nil {
		return fallbackPolish(res, motivoDoErro(ctx, err), true)
	}

	texto, ok := guardaJSON(bruto)
	if !ok {
		return fallbackPolish(res, MotivoJSON, true)
	}
	if !guardaFormato(entrada, texto) {
		return fallbackPolish(res, MotivoFormato, true)
	}
	if !guardaNumeros(entrada, texto) {
		return fallbackPolish(res, MotivoNumeros, true)
	}
	if !guardaSiglasTermos(entrada, texto, opts.Termos) {
		return fallbackPolish(res, MotivoSiglasTermos, true)
	}
	if motivo := guardaPalavras(entrada, texto, opts.maxEdicao()); motivo != MotivoNenhum {
		return fallbackPolish(res, motivo, true)
	}
	// A caixa vem por último: o motivo dela só aparece quando NENHUMA das outras
	// guardas recusou a saída, e assim o benchmark conta, em "caixa", só o que
	// esta guarda barrou sozinha (o preço dela, inclusive "cnpj" -> "CNPJ").
	if !guardaCaixa(entrada, texto) {
		return fallbackPolish(res, MotivoCaixa, true)
	}

	res.FinalText = texto
	res.Reescrito = true
	return res
}

// PolishResolver implementa a política Polish (AC-025): roda Smart
// primeiro, e só tenta a reescrita quando ResolveRequest.PolishOptIn é true
// — nunca por default (docs/ai-spec/16-ai-behavior-spec.md, "Polish": "não
// default"). Se a reescrita falhar por qualquer motivo, degrada para o
// resultado do Smart ("Fallback": "Polish falhou -> Smart").
type PolishResolver struct {
	// Smart é obrigatório: Polish sempre roda Smart primeiro.
	Smart *SmartResolver
	// Client é opcional. nil desativa a reescrita — PolishResolver se
	// comporta como o SmartResolver (com Mode == ModePolish).
	Client PolishClient
	// MinPalavras e MaxEdicao sobrescrevem os padrões das guardas (ver
	// PolirOpcoes). Zero usa o padrão.
	MinPalavras int
	MaxEdicao   float64
}

// NewPolishResolver constrói um PolishResolver sobre o smart dado.
func NewPolishResolver(smart *SmartResolver, client PolishClient) *PolishResolver {
	return &PolishResolver{Smart: smart, Client: client}
}

var _ Resolver = (*PolishResolver)(nil)

// Resolve implementa Resolver: Smart e, com opt-in, Reescrever.
func (p *PolishResolver) Resolve(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	smartResult, err := p.Smart.Resolve(ctx, req)
	if err != nil {
		return ResolveResult{}, err
	}
	if !req.PolishOptIn {
		// Opt-in ausente: Polish nunca é default. RawText e FinalText
		// continuam ambos presentes (AC-025) — FinalText é o do Smart.
		smartResult.Mode = ModePolish
		return smartResult, nil
	}
	return p.Reescrever(ctx, req, smartResult), nil
}

// Reescrever é a metade Polish de Resolve: recebe o resultado do Smart e
// chama Polir com os termos do dicionário achados LOCALMENTE no texto. É o
// que o Agent roda na goroutine com prazo, depois de executar o Smart no
// laço de eventos.
//
// Só consulta o dicionário quando há cliente e o texto é longo o bastante
// para a chamada acontecer.
func (p *PolishResolver) Reescrever(ctx context.Context, req ResolveRequest, smart ResolveResult) ResolveResult {
	opts := PolirOpcoes{
		Idioma:      req.ASRResult.Language,
		MinPalavras: p.MinPalavras,
		MaxEdicao:   p.MaxEdicao,
	}
	if p.Client != nil && guardaCurto(smart.FinalText, opts.minPalavras()) {
		termos, err := p.Smart.termosNoTexto(ctx, req, smart.FinalText)
		if err != nil {
			// Sem como conferir os termos da pessoa, o texto não sai da
			// máquina: falha FECHADO, fica o Smart.
			smart.Mode = ModePolish
			return fallbackPolish(smart, MotivoDicionario, true)
		}
		opts.Termos = comTermosTrocados(termos, smart.AppliedReplacements)
	}
	return Polir(ctx, p.Client, smart, opts)
}

// comTermosTrocados acrescenta a termos as formas canônicas que o Smart
// escreveu NESTE ditado no lugar de uma variante do dicionário.
//
// Um termo que o painel inventou (AliasOnly) não entra em termosNoTexto só por
// estar escrito: se entrasse, "e", "ponto" ou "de", aprendidos por um clique,
// travariam as listas e a pontuação falada de todo ditado que tem a palavra.
// Mas quando a variante age, o termo está no texto POR CAUSA do dicionário, e
// o Polish não pode desfazer a troca. Só o ditado em que isso aconteceu ganha a
// proteção. Para um termo comum o acréscimo é inócuo: ele já estava na lista.
//
// Duas razões contam como troca pelo dicionário: ReasonDictionary (a variante
// casou sozinha) e ReasonContext (a variante veio da alternativa do ASR, ou a
// fonética do termo, e o contexto a corroborou: o ramo N-best do Smart). Nos
// dois o termo está no texto por causa do dicionário. As demais (fonética sem
// corroboração, autocorreção, cloud) não são troca de variante e ficam de fora.
//
// A proteção é por termo (todas as ocorrências dele no texto, como para
// qualquer termo do dicionário), e não só pela ocorrência que a variante
// trocou: no pior caso o Polish cai no texto do Smart, que é seguro.
func comTermosTrocados(termos []string, aplicadas []AppliedReplacement) []string {
	for _, a := range aplicadas {
		if (a.Reason != ReasonDictionary && a.Reason != ReasonContext) || strings.TrimSpace(a.Replacement) == "" {
			continue
		}
		if !slices.Contains(termos, a.Replacement) {
			termos = append(termos, a.Replacement)
		}
	}
	return termos
}

// termosNoTexto devolve as formas canônicas dos termos do dicionário
// pessoal que a busca local achou olhando o texto em janelas de 1 a
// maxPhraseWords palavras — o mesmo desenho do Smart. A consulta é local:
// nenhum termo sai da máquina.
//
// O termo que o painel inventou (AliasOnly) não entra só por estar escrito:
// ver comTermosTrocados.
//
// A lista é de CANDIDATOS. Quem decide se um termo está de fato PRESENTE é
// guardaSiglasTermos, que o procura literalmente (sem distinguir caixa) na
// entrada; um candidato que só casou por alias ou fonética, ou por uma
// janela que atravessa pontuação, não aparece no texto e não trava nada.
// Por isso aqui não há filtro por tipo de casamento nem por pontuação.
func (s *SmartResolver) termosNoTexto(ctx context.Context, req ResolveRequest, text string) ([]string, error) {
	if s.Dictionary == nil {
		return nil, nil
	}
	tokens := tokenizeWords(text)
	runes := []rune(text)
	vistos := map[string]bool{}
	var termos []string
	for i := range tokens {
		maxLen := maxPhraseWords
		if restam := len(tokens) - i; restam < maxLen {
			maxLen = restam
		}
		for n := 1; n <= maxLen; n++ {
			frase := string(runes[tokens[i].Start:tokens[i+n-1].End])
			top, ok, err := s.searchTop(ctx, req, frase)
			if err != nil {
				return nil, err
			}
			if ok && !vistos[top.CanonicalForm] {
				vistos[top.CanonicalForm] = true
				termos = append(termos, top.CanonicalForm)
			}
		}
	}
	return termos, nil
}
