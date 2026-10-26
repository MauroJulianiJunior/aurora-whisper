package winruntime

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// UsarHistorico liga a persistência do histórico local. provedorASR é o
// identificador do provider que transcreveu (asr.ASRProvider.ID()), que
// o schema exige e o Runtime não tem como descobrir sozinho — o
// transcriber que ele recebe é uma interface de uma função só.
//
// Deve ser chamado antes de Run. Sem isto o Runtime só loga o desfecho,
// que foi o comportamento do Sprint 1 até 28-set-2026.
//
// usuario é o dono das linhas gravadas, e é explícito — em vez de reusar
// o do resolver — porque o histórico existe mesmo sem dicionário. Sem
// dicionário o usuário do resolver fica vazio, e uma linha gravada com
// usuário vazio nunca apareceria no painel: toda listagem é escopada por
// usuário e recusa o vazio.
//
// modeloASR é o nome do modelo que o provider carregou (asr_model). Vem
// de quem montou o provider pelo mesmo motivo de provedorASR; vazio grava
// a coluna como ausente, nunca como "".
func (rt *Runtime) UsarHistorico(h historico, provedorASR, modeloASR, usuario string) {
	rt.hist, rt.provedorASR, rt.modeloASR, rt.usuarioHist = h, provedorASR, modeloASR, usuario
}

// focoDaNota é o aviso, só em memória, de que a pessoa está com o cursor dentro
// de uma nota (internal/agent/notafoco). ParaOHistorico devolve o que gravar em
// virou_nota (o id da nota ou "nova:<token>") ou nil, quando não há aviso ou ele
// venceu.
type focoDaNota interface {
	ParaOHistorico() *string
}

// UsarNotaEmFoco liga o aviso de nota em foco (ADR-022, "Ditado feito dentro da
// nota"): o ditado feito com o cursor numa nota é gravado já escondido do
// histórico, como um "Virar nota" automático. A inserção no campo não muda.
// Deve ser chamado antes de Run. nil desliga.
func (rt *Runtime) UsarNotaEmFoco(f focoDaNota) {
	rt.notaEmFoco = f
}

// virouNotaDoDitado decide o virou_nota de um ditado que acaba de ser feito.
// Só o ditado que deu certo some do histórico: o que falhou (a inserção não
// aconteceu) fica à vista para a pessoa ver e recuperar o texto. O valor é
// contexto efêmero: sai daqui direto para a linha, sem passar por log.
func virouNotaDoDitado(f focoDaNota, desfecho dictation.Outcome) *string {
	if f == nil || desfecho != dictation.OutcomeSuccess {
		return nil
	}
	return f.ParaOHistorico()
}

// medicaoASR é o que a goroutine da transcrição mediu, a caminho do
// histórico. Imutável depois de criada: é publicada por ponteiro atômico
// (Runtime.asrMedido), e quem a lê nunca a altera.
type medicaoASR struct {
	attempt       dictation.AttemptID
	processamento time.Duration
}

// recordHistory atende RecordHistoryEffect: loga o desfecho e, quando há
// repositório ligado, PERSISTE a transcrição.
//
// # Por que o texto não entra no log
//
// 09-security-privacy.md proíbe log de áudio e de contexto de janela, e
// o texto ditado é conteúdo do usuário da mesma natureza. Vai para o
// banco, que é onde o AC-011 manda o histórico ficar, e para o log só o
// tamanho.
//
// # Por que o raw_text importa
//
// Até 28-set-2026 o laço de aprendizado só tinha o que a pessoa digitava
// de memória, e memória não guarda a grafia exata de um erro visto por
// meio segundo. Mauro ensinou "Wissper" e o ASR produzia "Whisperflow" —
// o alias cobria uma forma que nunca mais ocorreria. O raw_text gravado
// aqui é o que permite ensinar a partir do que a máquina DE FATO
// produziu.
func (rt *Runtime) recordHistory(e dictation.RecordHistoryEffect) {
	rt.log.Info("winruntime: dictation attempt finished",
		"attempt", e.Attempt,
		"outcome", e.Outcome.String(),
		"reason", e.Reason.String(),
		"text_runes", len([]rune(e.Text)),
	)

	if rt.hist == nil {
		return
	}
	// Tentativa sem texto não tem o que aprender nem o que mostrar:
	// bloqueio em campo protegido, tap, silêncio. Guardar linha vazia só
	// encheria o histórico de ruído.
	bruto := rt.brutoDaTentativa.texto
	if rt.brutoDaTentativa.attempt != e.Attempt {
		// O bruto é de outra tentativa (ou não houve resolução). Melhor
		// gravar sem ele do que gravar o de outro ditado.
		bruto = ""
	}
	rt.brutoDaTentativa.attempt, rt.brutoDaTentativa.texto = 0, ""

	// As medições seguem a mesma regra do bruto: só valem se forem DESTA
	// tentativa, e são consumidas aqui, gravando ou não.
	var duracaoAudio, processamentoASR time.Duration
	if rt.audioDaTentativa.attempt == e.Attempt {
		duracaoAudio = rt.audioDaTentativa.duracao
	}
	rt.audioDaTentativa.attempt, rt.audioDaTentativa.duracao = 0, 0
	// A medição só é consumida se for DESTA tentativa. A de outra fica: é a
	// de uma transcrição que estourou o prazo e ainda pode chegar, e a linha
	// tardia dela a usa (tardio.go). Nunca é atribuída a outra tentativa:
	// quem a lê confere o número.
	if m := rt.asrMedido.Load(); m != nil && m.attempt == e.Attempt {
		rt.asrMedido.CompareAndSwap(m, nil)
		processamentoASR = m.processamento
	}
	modo := rt.modoGravado(e.Attempt)
	// Consumida aqui, gravando ou não, como as medições acima.
	ditadoID, virouPorVoz, foiParaNota := rt.virouNotaPorVoz(e.Attempt)

	if e.Text == "" && bruto == "" {
		return
	}
	if bruto == "" {
		// Sem resolução, o final É o bruto. A coluna é NOT NULL, e
		// deixá-la vazia faria o laço de aprendizado ler "" como "o ASR
		// não devolveu nada", que é outra coisa.
		bruto = e.Text
	}

	status := "done"
	if e.Outcome != dictation.OutcomeSuccess {
		status = "error"
	}
	t := rt.novaLinha(bruto, e.Text, modo, status, duracaoAudio, processamentoASR, time.Time{})
	// Ditado feito dentro de uma nota não entra no histórico (a linha fica no
	// banco, escondida, e o "Aprender" a lê). A linha tardia (tardio.go) NÃO
	// passa por aqui: ela não foi inserida em campo nenhum e precisa aparecer.
	t.VirouNota = virouNotaDoDitado(rt.notaEmFoco, e.Outcome)
	// O ditado do gatilho "ditar para nota" (paranota.go): gravado na nota, sai
	// do histórico como no "Virar nota" (virou_nota = a nota); a nota que não
	// foi gravada deixa o ditado à vista, que é onde o texto está. O id é o que
	// a nota nova guardou em ditado_de_origem. A nota em foco no painel não
	// conta aqui: o texto não foi para o campo.
	if foiParaNota {
		t.ID, t.VirouNota = ditadoID, virouPorVoz
	}
	if err := rt.hist.Put(t); err != nil {
		// Não derruba o ditado: o texto já está no campo de quem usa, e
		// perder a linha do histórico é menos grave que transformar uma
		// falha de disco em ditado perdido. Mas não pode ser silencioso.
		rt.log.Warn("winruntime: não consegui gravar o histórico",
			"attempt", e.Attempt, "err", err)
	}
}

// novaLinha monta a linha do histórico de um ditado. criada é o created_at:
// zero é agora, o fim da tentativa, no caminho normal; a linha tardia
// (tardio.go) passa o começo da transcrição, porque chega quando outro
// ditado já pode ter sido gravado.
func (rt *Runtime) novaLinha(bruto, final, modo, status string, duracaoAudio, processamentoASR time.Duration, criada time.Time) storage.Transcription {
	agora := time.Now().UTC()
	if criada.IsZero() {
		criada = agora
	}
	return storage.Transcription{
		ID:             storage.NewID(),
		UserID:         rt.usuarioHist,
		SourcePlatform: "windows",
		RawText:        bruto,
		FinalText:      final,
		Mode:           modo,
		ASRProvider:    rt.provedorASR,
		// AC-043 e 08-ui-ux-spec ("provider/model, duração, processing
		// time"). Até a revisão de 29-set-2026 nenhuma das três era
		// gravada, e o painel caía para "data · provider" em todo ditado
		// real: o dado existia no Runtime e era jogado fora.
		//
		// duration_ms é a duração do áudio capturado. processing_ms é o
		// tempo que o provider de ASR mediu na transcrição
		// (asr.ASRResult.ProcessingTime) — o mesmo sentido que o
		// 06-api-contract dá ao processing_ms da resposta de
		// POST /asr/transcribe. NÃO é o release→insert: esse o Agent
		// ainda não mede, e um número montado aqui com relógios soltos
		// seria outro dado com o nome deste.
		ASRModel:     textoOuAusente(rt.modeloASR),
		DurationMs:   msOuAusente(duracaoAudio),
		ProcessingMs: msOuAusente(processamentoASR),
		// TargetApp fica ausente de propósito. O nome do app em foco é
		// contexto de outro aplicativo, e o 09-security-privacy.md manda
		// não persistir contexto ("Contexto UIA: sensível efêmero, RAM,
		// retenção: resolução"; REQ-025). A coluna existe no 05-data-model,
		// mas nada na spec autoriza gravá-la — e este mesmo pacote já
		// trata o nome do processo como contexto que nem o log pode ver
		// (ver resolver, em runtime.go). Gravar é decisão de quem é dono
		// da política, não deste adaptador.
		Status:    status,
		CreatedAt: criada.UTC(),
		UpdatedAt: agora,
		Version:   1,
		SyncState: "pending",
	}
}

// msOuAusente converte uma medição em milissegundos para o histórico.
//
// Zero ou negativo é AUSENTE, não "0 ms": zero aqui significa que nada
// foi medido — a medição era de outra tentativa, o provider não reporta
// tempo, o buffer veio sem taxa de amostragem —, e gravar 0 afirmaria um
// ditado instantâneo que ninguém mediu.
func msOuAusente(d time.Duration) *int64 {
	if d <= 0 {
		return nil
	}
	ms := d.Milliseconds()
	return &ms
}

// textoOuAusente devolve nil para vazio: uma coluna nullable gravada com
// "" se leria como "o modelo se chama vazio".
func textoOuAusente(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// modo é a política de resolução que vale agora. Sem resolver ligado — ou
// com o modo Literal escolhido na tela — o Runtime devolve o texto do ASR
// intocado, que é exatamente o modo Literal.
//
// É o que foi ESCOLHIDO. O histórico grava o que ACONTECEU em cada
// tentativa: ver modoGravado.
func (rt *Runtime) modo() string {
	switch {
	case rt.resolucao == nil || rt.literal:
		return "literal"
	case rt.polish:
		return "polish"
	default:
		return "smart"
	}
}

// modoGravado é o modo que o histórico registra para a tentativa: o que
// ACONTECEU, não o que foi escolhido (ADR-016, decisão 12). Com o Polish
// escolhido, "polish" só quando a reescrita desta tentativa foi aceita
// pelas guardas; em qualquer fallback — prazo, rede, sem chave, guarda,
// ditado curto —, "smart", que é o texto que entrou.
//
// Consome o desfecho do Polish, como recordHistory consome o bruto e as
// medições: um desfecho de outra tentativa nunca vale para esta.
func (rt *Runtime) modoGravado(attempt dictation.AttemptID) string {
	aceito := rt.polishDaTentativa.attempt == attempt && rt.polishDaTentativa.reescrito
	rt.polishDaTentativa.attempt, rt.polishDaTentativa.reescrito = 0, false
	m := rt.modo()
	if m == "polish" && !aceito {
		return "smart"
	}
	return m
}
