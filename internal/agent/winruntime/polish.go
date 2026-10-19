package winruntime

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Este arquivo liga o Polish (ADR-016, fatia S4) ao runtime: o Smart
// continua no laço de eventos (resolver, em runtime.go), e só a chamada de
// rede roda numa goroutine própria, com prazo, no mesmo padrão de
// transcrever. O resultado volta ao laço por rt.polidos, e o ditado nunca
// se perde: prazo estourado, erro, sem chave ou guarda reprovando, entra o
// texto do Smart.

// O prazo da metade de rede do Polish. O teto da chamada ao provedor é
// polish.Prazo(texto): 3,5 s para o texto curto, crescendo com o tamanho
// até 8 s (polish.PisoPrazo e o acréscimo de dictation.AcrescimoPeloTexto).
// Ele mora em internal/polish desde a C5a da ADR-021, porque o servidor usa
// o mesmo; aqui ficam o runtime que o aplica e o porquê de ele caber no
// núcleo. O texto que decide o tamanho é o do ResolveEffect, já sem o laço
// de repetição, o MESMO sobre o qual o núcleo arma o watchdog. O prazo é
// contado do COMEÇO da resolução (antes do Smart), e não do começo da
// chamada.
//
// Por que o prazo do Polish cabe no watchdog do núcleo. O núcleo encerra a tentativa sem texto se a resolução não responder em
// dictation.PrazoDeResolucao (5 s no texto curto). Esse timer é armado em
// onTranscribeResult, no MESMO lote do ResolveEffect, e aplicado logo
// DEPOIS dele — isto é, depois do Smart, que roda dentro de resolver. A
// transcrição tem timer próprio e não gasta nada destes 5 s: no log real
// de 29-set-2026 ela levou de 0,46 a 0,89 s (uma vez 2,8 s), sempre antes
// de o relógio da resolução começar.
//
// O prazo do Polish, ao contrário, é ancorado ANTES do Smart, no começo
// de resolver: o Smart (2,3 ms medidos; aviso acima de 25 ms) sai do teto,
// e o que sobra para a rede é o teto menos o Smart. A folga até o watchdog
// é, então, de 1,5 s mais o próprio Smart, para a entrega ao laço e o
// despacho (microssegundos). Um Smart patológico que gaste o teto inteiro
// não chega a chamar a rede: o ditado sai como Smart, com o motivo prazo.
//
// O benchmark de 30-set-2026 (docs/benchmarks/polish-2026-09-30.md) mediu
// p95 de 2,35 s no gpt-6-luna (conexão reaproveitada; a fria, 2,49 s) e
// 1,6% das chamadas acima de 3,5 s: o número fica (ADR-016, decisão 4).
// TestPrazoPolish_CabeNoWatchdogDoNucleo trava a folga.

// Polidor é a metade de rede do Polish de UM ditado: recebe o resultado do
// Smart e devolve o do Polish, que nunca é erro (no fallback, o do Smart
// com Reescrito=false e o motivo). *resolver.PolishResolver o cumpre.
//
// Roda na goroutine da chamada, com o ctx do ditado: derivado do de Run e
// com o prazo de polish.Prazo.
type Polidor interface {
	Reescrever(ctx context.Context, req resolver.ResolveRequest, smart resolver.ResolveResult) resolver.ResolveResult
}

// AbrirPolidor monta o Polidor de um ditado para o provedor e o modelo que
// valiam no keydown. cmd/agent o implementa com ia.Polidor, lendo a chave
// no Gerenciador de Credenciais a cada chamada (ADR-016, decisão 2).
//
// Devolve também o esforço de raciocínio que a chamada vai mandar, só para
// o log ("" quando não se aplica). contar, chamado da goroutine da
// chamada, recebe os tokens que o provedor informou: só números.
type AbrirPolidor func(provedor, modelo string, contar func(entrada, saida int)) (Polidor, string)

// UsarPolish liga a metade de rede do Polish. Deve ser chamado antes de
// Run. Sem isto, o Polish escolhido na tela cai no Smart com o motivo
// sem_cliente.
func (rt *Runtime) UsarPolish(abrir AbrirPolidor) { rt.abrirPolidor = abrir }

// EstadoPolish é o que a tela lê do último Polish (fatia S5,
// polish_ultimo_motivo). Motivo vazio: o último Polish agiu, ou ainda não
// houve nenhum. Só o motivo, um enum — nunca o texto ditado, o corpo de
// erro do provedor ou a chave.
type EstadoPolish struct {
	Motivo resolver.MotivoFallback
	// Provedor é o que valia no keydown dessa tentativa, para a tela nomear
	// o provedor certo mesmo que a pessoa o tenha trocado depois.
	Provedor string
	// Quando é o instante em que esse Polish terminou. Zero: nenhum ainda.
	Quando time.Time
}

// UltimoPolish diz por que o último Polish não agiu, e quando. Seguro de
// qualquer goroutine; vive só em memória e some ao reiniciar o Agent.
//
// Um ditado curto (menos de 4 palavras) não mexe nele: não foi ao
// provedor, e não é acerto nem falha. Um Polish aceito o limpa.
func (rt *Runtime) UltimoPolish() EstadoPolish {
	if e := rt.ultimoPolish.Load(); e != nil {
		return *e
	}
	return EstadoPolish{}
}

// polidoPronto é o resultado do Polish atravessando de volta para a
// goroutine de Run, com o que a linha de log precisa. Só números,
// identificadores e o resultado — o texto do resultado vai ao núcleo, e
// nunca ao log.
type polidoPronto struct {
	attempt          dictation.AttemptID
	res              resolver.ResolveResult
	provedor, modelo string
	esforco          string
	tokensEntrada    int
	tokensSaida      int
	entrada          int // caracteres do texto do Smart
	latencia         time.Duration
	prazo            time.Duration // o que sobrava do teto quando a chamada começou
}

// prazoDoTeto é o teto que vale para o texto que entrou na resolução:
// polish.Prazo(texto), ou o que o teste pôs.
func (rt *Runtime) prazoDoTeto(texto string) time.Duration {
	if rt.prazoDoPolish > 0 {
		return rt.prazoDoPolish
	}
	return polish.Prazo(texto)
}

// polir dispara a metade de rede do Polish numa goroutine própria e volta
// na hora. Roda na goroutine de Run: tudo o que ela lê do Runtime é lido
// aqui, antes do go, e a goroutine só fala com o laço por rt.polidos.
//
// inicio é o começo da resolução: o prazo vale de lá (ver polish.Prazo). O
// tamanho que decide o prazo é o de req.ASRResult.Text, o texto do
// ResolveEffect, que é o mesmo sobre o qual o núcleo armou o watchdog — e
// não o do Smart, que o dicionário pode ter mudado em alguns caracteres.
func (rt *Runtime) polir(attempt dictation.AttemptID, req resolver.ResolveRequest, smart resolver.ResolveResult, inicio time.Time) {
	pai := rt.ctxRun
	if pai == nil {
		// resolver chamado fora de Run (testes): sem encerramento a seguir.
		pai = context.Background()
	}
	if rt.polidos == nil {
		rt.polidos = make(chan polidoPronto, 1)
	}
	limite := inicio.Add(rt.prazoDoTeto(req.ASRResult.Text))
	abrir, provedor, modelo := rt.abrirPolidor, rt.provedorIA, rt.modeloIA
	polidos, parar := rt.polidos, rt.pararCapturas

	go func() {
		ctx, cancel := context.WithDeadline(pai, limite)
		defer cancel()
		comeco := time.Now()
		p := esperarPolish(ctx, abrir, provedor, modelo, req, smart)
		p.attempt, p.provedor, p.modelo = attempt, provedor, modelo
		p.entrada = utf8.RuneCountInString(smart.FinalText)
		p.latencia, p.prazo = time.Since(comeco), limite.Sub(comeco)
		// Run pode já ter saído (Agent encerrando): aí ninguém lê
		// polidos, e parar está fechado.
		select {
		case polidos <- p:
		case <-parar:
		}
	}()
}

// esperarPolish chama o Polidor e espera no máximo até o prazo do ctx.
//
// A chamada roda numa goroutine a mais, e não direto aqui, para o prazo
// valer mesmo que o Polidor ignore o ctx: resolver.Polir não confere o
// relógio depois que o cliente responde, e uma resposta boa que chegasse
// depois do watchdog do núcleo (5 s a 9,5 s, pelo texto) perderia o
// ditado inteiro. Aqui, vencido o prazo, entra o Smart, e a resposta
// tardia é descartada: a goroutine da chamada entrega num canal com folga
// e morre sozinha.
func esperarPolish(ctx context.Context, abrir AbrirPolidor, provedor, modelo string, req resolver.ResolveRequest, smart resolver.ResolveResult) polidoPronto {
	if ctx.Err() != nil {
		// O Smart já gastou o teto (ou o Agent está encerrando): nem abre
		// a rede, nem lê a chave.
		return polidoPronto{res: fallbackDoRuntime(smart, motivoDoCtx(ctx))}
	}
	if abrir == nil {
		return polidoPronto{res: fallbackDoRuntime(smart, resolver.MotivoSemCliente)}
	}
	// tokens é da goroutine da chamada: contar só é chamado de dentro de
	// Reescrever, e o valor sai pelo canal junto com o resultado. Esta
	// goroutine nunca o lê — nem quando desiste pelo prazo.
	var tokens [2]int
	pol, esforco := abrir(provedor, modelo, func(entrada, saida int) { tokens = [2]int{entrada, saida} })
	if pol == nil {
		return polidoPronto{res: fallbackDoRuntime(smart, resolver.MotivoSemCliente), esforco: esforco}
	}
	feito := make(chan polidoPronto, 1)
	go func() {
		res := pol.Reescrever(ctx, req, smart)
		feito <- polidoPronto{res: res, tokensEntrada: tokens[0], tokensSaida: tokens[1]}
	}()
	select {
	case p := <-feito:
		p.esforco = esforco
		return p
	case <-ctx.Done():
		// A resposta, se vier, chega tarde: é descartada. Os tokens dela
		// ficam fora do log (0): só a goroutine da chamada os conhece.
		return polidoPronto{res: fallbackDoRuntime(smart, motivoDoCtx(ctx)), esforco: esforco}
	}
}

// motivoDoCtx separa o prazo do encerramento, como resolver.Polir:
// cancelamento é o Agent saindo, e não o provedor demorando.
func motivoDoCtx(ctx context.Context) resolver.MotivoFallback {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return resolver.MotivoPrazo
	}
	return resolver.MotivoTransporte
}

// fallbackDoRuntime é o resultado do Smart marcado como Polish que não
// valeu, quando quem desiste é o runtime (prazo, encerramento, sem
// Polidor), e não resolver.Polir.
func fallbackDoRuntime(smart resolver.ResolveResult, m resolver.MotivoFallback) resolver.ResolveResult {
	r := smart
	r.Mode = resolver.ModePolish
	r.Reescrito = false
	r.MotivoFallback = m
	return r
}

// concluirPolish roda NA GOROUTINE DE RUN: registra o desfecho para o
// histórico e para a tela, escreve a linha de log e devolve o Event que o
// núcleo espera.
//
// Um resultado de uma tentativa que o núcleo já encerrou (o watchdog, se
// Run tiver ficado parado mais de 1,5 s) é registrado igual e descartado
// pelo núcleo, que não o reconhece mais — mesmo que outra tentativa já
// tenha começado: o AttemptID é o da tentativa que pediu o Polish.
func (rt *Runtime) concluirPolish(p polidoPronto) dictation.Event {
	rt.polishDaTentativa.attempt, rt.polishDaTentativa.reescrito = p.attempt, p.res.Reescrito
	rt.registrarPolish(p)
	switch {
	case p.res.Reescrito:
		rt.ultimoPolish.Store(&EstadoPolish{Provedor: p.provedor, Quando: time.Now()})
	case p.res.MotivoFallback == resolver.MotivoCurto:
		// Nem foi ao provedor: não diz nada sobre o Polish.
	default:
		rt.ultimoPolish.Store(&EstadoPolish{Motivo: p.res.MotivoFallback, Provedor: p.provedor, Quando: time.Now()})
	}
	return dictation.ResolveResultEvent{
		Attempt: p.attempt,
		Result:  dictation.ResolveResultDTO{Text: p.res.FinalText},
	}
}

// desfechoDoPolish é o que a linha de log diz: "aceito", ou o motivo.
func desfechoDoPolish(r resolver.ResolveResult) string {
	switch {
	case r.Reescrito:
		return "aceito"
	case r.MotivoFallback == resolver.MotivoNenhum:
		return "desconhecido"
	default:
		return string(r.MotivoFallback)
	}
}

// registrarPolish escreve a linha do ditado em Polish (ADR-016, decisão
// 12; 11-observability.md): só números e identificadores.
//
// Os campos são escolhidos um a um, e nenhum vem de texto: NUNCA o ditado
// (enviado ou devolvido), os warnings do modelo (resolver.Polir os
// descarta; os do resultado são o aviso fixo do motivo, que o desfecho já
// diz), o corpo de erro do provedor ou a chave. O modelo é o nome que a
// pessoa escolheu na tela.
func (rt *Runtime) registrarPolish(p polidoPronto) {
	rt.log.Info("winruntime: polish",
		"attempt", p.attempt,
		"prompt", resolver.PolishPromptRef,
		"prompt_hash", resolver.PolishPromptHash(),
		"provedor", p.provedor,
		"modelo", p.modelo,
		"esforco", p.esforco,
		"desfecho", desfechoDoPolish(p.res),
		"latencia_ms", p.latencia.Milliseconds(),
		"prazo_ms", p.prazo.Milliseconds(),
		"entrada_chars", p.entrada,
		"saida_chars", utf8.RuneCountInString(p.res.FinalText),
		"tokens_entrada", p.tokensEntrada,
		"tokens_saida", p.tokensSaida,
		// O Polish que valeu por frase (ADR-016, emenda de 19-out-2026):
		// quantas frases e quantas voltaram ao Smart. Zero quando valeu inteiro.
		"frases", p.res.Frases,
		"frases_revertidas", p.res.FrasesRevertidas,
	)
}
