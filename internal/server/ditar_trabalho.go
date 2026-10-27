package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// O trabalho solto de um ditado (ADR-021, decisão I; C5b): o provedor da
// transcrição, o Smart da conta, o Polish e a gravação, num contexto desligado
// da conexão (context.WithoutCancel), cada passo com o prazo dele. O celular
// cair não perde o que já foi pago: o ditado é gravado mesmo assim, e o reenvio
// o recebe de graça.
//
// O trabalho confere de novo, antes do Polish e antes de gravar, o aceite e o
// epoch da conta lido no começo: se a pessoa desligou o ditado ou apagou os dados
// no meio, ele descarta sem chamar o Polish e sem gravar. A conferência do epoch
// na gravação fica dentro do Store.Create, na transação que trava o contador, e
// não tem janela; a do aceite antes de gravar deixa uma janela de milissegundos
// (ADR-021, risco 16).
//
// Nada daqui vai a log além da linha "ditado", com contagens e códigos de listas
// fechadas: nunca o áudio, o texto, a chave, o id nem o corpo de erro do
// provedor.

// Os prazos da rota de ditar (ADR-021, "Prazos" e escolha menor 5). Zero em um
// campo é o padrão.
type PrazosDoDitado struct {
	// Corpo: ler o corpo, a partir de quando o pedido pega a vaga (30 s; o
	// readTimeout do servidor, repetido no handler).
	Corpo time.Duration
	// Transcricao: a chamada ao provedor (20 s).
	Transcricao time.Duration
	// Smart: ler o aprendido da conta e montar e rodar o Smart dela (5 s). A
	// ADR não fixa um prazo para o Smart (ele leva milissegundos com o Smart
	// pronto); este é a guarda da leitura do aprendido e da primeira montagem.
	Smart time.Duration
	// Polish: a chamada do Polish (10 s). No PC o prazo é o polish.Prazo do
	// texto (de 3,5 s a 8 s), curto porque o texto tem de entrar no campo. No
	// celular a pessoa já está olhando a tela, esperando: no primeiro ditado
	// real (12-out-2026) o modelo do Polish não respondeu em 3,5 s, e o Mauro
	// escolheu 10 s só no celular (ADR-021, escolha menor 5, a alternativa).
	Polish time.Duration
	// Modelos: perguntar ao provedor do Polish a lista de modelos, quando o
	// modelo da conta ainda não é conhecido (3 s).
	Modelos time.Duration
	// Gravacao: conferir o aceite e gravar (5 s).
	Gravacao time.Duration
	// Resposta: escrever a resposta, contado do fim do corpo (45 s; o
	// writeTimeout do servidor conta do fim dos cabeçalhos e não cobriria o
	// corpo, o provedor, o Polish e a gravação).
	Resposta time.Duration
}

// folgaDaResposta é o tempo que o trabalho solto deixa, dentro do prazo da
// resposta, para o pedido escrevê-la.
const folgaDaResposta = time.Second

// PrazosDoDitadoPadrao são os prazos da ADR-021.
var PrazosDoDitadoPadrao = PrazosDoDitado{
	Corpo: 30 * time.Second, Transcricao: 20 * time.Second, Smart: 5 * time.Second,
	Polish: 10 * time.Second, Modelos: 3 * time.Second, Gravacao: 5 * time.Second,
	Resposta: 45 * time.Second,
}

func (p PrazosDoDitado) comPadroes() PrazosDoDitado {
	d := PrazosDoDitadoPadrao
	for _, c := range []struct{ v, d *time.Duration }{
		{&p.Corpo, &d.Corpo}, {&p.Transcricao, &d.Transcricao}, {&p.Smart, &d.Smart},
		{&p.Polish, &d.Polish}, {&p.Modelos, &d.Modelos}, {&p.Gravacao, &d.Gravacao},
		{&p.Resposta, &d.Resposta},
	} {
		if *c.v <= 0 {
			*c.v = *c.d
		}
	}
	return p
}

// trabalhoDoDitado é o que o trabalho solto recebe do pedido.
type trabalhoDoDitado struct {
	k chaveDoDitado
	// a é o andamento de que o pedido é dono.
	a     *andamento
	epoch string
	prefs cloudstore.PreferenciasDoDitado
	// audio é o corpo, os bytes como chegaram. Nil no reenvio que só grava.
	audio             []byte
	duracaoMs, falaMs int64
	// pago é o desfecho pago e não gravado: o reenvio só grava.
	pago *pagoNaoGravado
	// destino é o Ditado-Nota (N8): o ditado comum, o "Ditar para nota" ou a
	// nota aberta. No reenvio que só grava, vale o que o pago guardou.
	destino destinoNaNota
	// fimDoCorpo conta o prazo da resposta e o processing_ms.
	fimDoCorpo time.Time
	// ctxDoPedido é o contexto do pedido, do qual o trabalho se desliga
	// (context.WithoutCancel): o fim do pedido não o cancela.
	ctxDoPedido context.Context
}

// resultadoDoDitado é o que o trabalho devolve ao dono do pedido. Carrega o
// ditado gravado (com o texto): vive só no canal do pedido, nunca na memória do
// registro.
type resultadoDoDitado struct {
	desfecho desfecho
	// motivo é o desfecho como o log o escreve.
	motivo string
	item   *cloudstore.Transcription
	criado bool
	// destino é para onde o "Ditar para nota" levou o ditado (N8); nil no
	// ditado comum e no microfone de dentro da nota.
	destino *cloudstore.DestinoDoDitado
}

// relatorioDoDitado é a linha "ditado" do log: só contagens e códigos.
type relatorioDoDitado struct {
	desfecho        string
	bytes           int
	provedorMs      int64
	motivoProvedor  string
	termosNaDica    int
	resumo          smartconta.Resumo
	smartFalhou     bool
	lacos, removida int
	substituicoes   int
	polish          string
	esforco         string
	tokensEntrada   int
	tokensSaida     int
	polishMs        int64
	// frases e frasesRevertidas: o Polish que valeu por frase (ADR-016,
	// emenda de 19-out-2026). Só números.
	frases, frasesRevertidas int
}

// rodarDitado é o trabalho solto. Roda numa goroutine própria, contada no
// registro para o desligamento esperar, e entrega o resultado no canal (com
// folga: o dono pode já ter ido embora).
func (c *Cloud) rodarDitado(t trabalhoDoDitado, saida chan<- resultadoDoDitado) {
	defer c.ditados.terminarTrabalho()
	inicio := time.Now()
	// Desligado da conexão, e com o teto do prazo da resposta (menos uma folga
	// para escrevê-la): cada passo tem o prazo dele, e o que sobrar do teto
	// vale se for menor. Um ditado que estoura o teto depois de o provedor
	// cobrar vira o desfecho pago e não gravado, e o reenvio só grava.
	teto := c.prazos.Resposta - folgaDaResposta
	if teto <= 0 {
		teto = c.prazos.Resposta
	}
	base, cancelBase := context.WithDeadline(context.WithoutCancel(t.ctxDoPedido), t.fimDoCorpo.Add(teto))
	defer cancelBase()
	rel := relatorioDoDitado{bytes: len(t.audio), polish: "desligado"}
	var (
		res     resultadoDoDitado
		semFala bool
		pago    *pagoNaoGravado
	)
	defer func() {
		if recover() != nil {
			// O valor do pânico não vai a log: pode citar o que a pessoa ditou.
			res = resultadoDoDitado{desfecho: desfecho{status: http.StatusInternalServerError, code: codeInternal, frase: "falha interna"}}
			semFala, pago = false, nil
			rel.desfecho = "panico"
		}
		res.motivo = rel.desfecho
		if res.desfecho.code == codeSemAceite {
			// O aceite deixou de valer, ou a conta apagou os dados, durante o
			// ditado. O "Desligar" e o "Apagar meus dados" já esqueceram o Smart
			// da conta, mas este trabalho leu o aprendido antes e pode tê-lo
			// montado (e guardado) de novo depois: ele sai outra vez.
			c.smart.Esquecer(t.k.usuario)
		}
		c.ditados.concluir(t.k, t.a, res.desfecho, semFala, pago)
		c.logDoDitado(t, rel, time.Since(inicio))
		saida <- res
	}()

	var novo cloudstore.NewDictation
	aviso := ""
	if t.pago != nil {
		novo, aviso = t.pago.novo, t.pago.aviso
		rel.polish = "regravado"
	} else {
		var falhou *desfecho
		novo, aviso, falhou, semFala = c.transcrever(base, &t, &rel)
		if falhou != nil {
			res.desfecho = *falhou
			return
		}
		// O destino na nota vai com o ditado: o pago e não gravado o guarda
		// para o reenvio que só grava.
		novo.VirouNota, novo.ParaNota = t.destino.nota, t.destino.paraNota
	}
	res, pago = c.gravarDitado(base, t, novo, aviso, &rel)
	if t.pago != nil && res.desfecho.ok() {
		rel.desfecho = "regravado"
	}
}

// transcrever leva o áudio ao provedor e o texto ao Smart e ao Polish. Devolve
// o ditado a gravar, ou o desfecho da falha (semFala diz que o provedor foi
// chamado e não ouviu fala: o reenvio em 10 minutos não o chama de novo).
func (c *Cloud) transcrever(base context.Context, t *trabalhoDoDitado, rel *relatorioDoDitado) (novo cloudstore.NewDictation, aviso string, falhou *desfecho, semFala bool) {
	user := t.k.usuario
	provT, modeloT := t.prefs.Transcricao.Provedor, t.prefs.Transcricao.Modelo

	// 1. O aprendido da conta: o Smart e a dica.
	ctxA, cancelA := context.WithTimeout(base, c.prazos.Smart)
	pagina, errA := c.cfg.Store.ListLearned(ctxA, user, smartconta.MaxPares)
	cancelA()
	pares := paresDaNuvem(pagina.Items)
	dica := ""
	if t.prefs.Dica && errA == nil {
		// Só com a preferência ligada (decisão E): os termos vão ao provedor
		// junto com a voz.
		dica = smartconta.Dica(pares)
		if dica != "" {
			rel.termosNaDica = strings.Count(dica, ", ") + 1
		}
	}

	// 2. O provedor da transcrição, montado a cada ditado, com a chave
	// decifrada só durante a chamada.
	prov := &nuvem.Provider{
		Cliente: c.cfg.Voz, Provedor: provT, Modelo: modeloT, Idioma: nuvem.IdiomaPadrao,
		Chave: func(ctx context.Context) (string, error) { return c.chaveDeIA(ctx, user, provT) },
	}
	if !prov.Capabilities().AceitaFormato(tipoDoAudio) {
		rel.desfecho = "formato"
		return novo, "", &desfecho{status: http.StatusUnsupportedMediaType, code: codeFormato, frase: fraseFormato}, false
	}
	ctxT, cancelT := context.WithTimeout(base, c.prazos.Transcricao)
	comeco := time.Now()
	out, err := prov.Transcribe(ctxT, asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: t.audio, Tipo: tipoDoAudio}},
		asr.ASROptions{Language: "pt-BR", Vocabulary: dica})
	cancelT()
	rel.provedorMs = time.Since(comeco).Milliseconds()
	// O áudio já foi: daqui em diante nada o segura.
	t.audio = nil
	if err != nil {
		var e *nuvem.Erro
		if errors.As(err, &e) {
			rel.motivoProvedor = string(e.Motivo)
		}
		switch {
		case errors.Is(err, nuvem.ErrSemFala):
			rel.desfecho = "sem_fala"
			return novo, "", &desfecho{status: http.StatusUnprocessableEntity, code: codeSemFala, frase: fraseSemFala}, true
		case errors.Is(err, asr.ErrComprimidoNaoSuportado), errors.Is(err, asr.ErrAudioInvalido):
			rel.desfecho = "formato"
			return novo, "", &desfecho{status: http.StatusUnsupportedMediaType, code: codeFormato, frase: fraseFormato}, false
		}
		status, code, frase := FalhaDoProvedor(err)
		rel.desfecho = strings.ToLower(code)
		return novo, "", &desfecho{status: status, code: code, frase: frase}, false
	}
	bruto := out.Text
	if strings.TrimSpace(bruto) == "" {
		rel.desfecho = "sem_fala"
		return novo, "", &desfecho{status: http.StatusUnprocessableEntity, code: codeSemFala, frase: fraseSemFala}, true
	}

	// 3. O Smart da conta (decisão F). Sem o aprendido, ou sem o Smart, segue o
	// texto do ASR (sem o laço de repetição), como no PC, e o Polish não roda.
	ctxS, cancelS := context.WithTimeout(base, c.prazos.Smart)
	conta, smartOk := c.contaDoSmart(ctxS, user, pares, errA == nil)
	var resolvido smartconta.Resolvido
	if conta != nil {
		resolvido = conta.ResolverParaPolir(ctxS, bruto)
		rel.resumo = conta.Resumo()
	} else {
		resolvido.Bruto, resolvido.Texto = bruto, bruto
	}
	cancelS()
	if !smartOk || conta == nil {
		resolvido.SmartFalhou = true
	}
	rel.smartFalhou = resolvido.SmartFalhou
	rel.lacos, rel.removida, rel.substituicoes = resolvido.Lacos, resolvido.PalavrasRemovidas, resolvido.Substituicoes

	final, modo := resolvido.Texto, "smart"
	if strings.TrimSpace(final) == "" {
		rel.desfecho = "sem_fala"
		return novo, "", &desfecho{status: http.StatusUnprocessableEntity, code: codeSemFala, frase: fraseSemFala}, true
	}

	// 4. O Polish, com o aceite e o epoch conferidos de novo antes.
	if t.prefs.PolishLigado {
		recusa, conferido := c.conferirNoMeio(base, *t)
		if recusa != nil {
			rel.desfecho = "sem_aceite"
			return novo, "", recusa, false
		}
		texto, reescrito, motivo := "", false, string(resolver.MotivoTransporte)
		if conferido {
			texto, reescrito, motivo = c.polir(base, *t, conta, resolvido, rel)
		}
		if reescrito {
			final, modo = texto, "polish"
			rel.polish = "aceito"
		} else {
			aviso, rel.polish = motivo, motivo
			if motivo == "" {
				rel.polish = string(resolver.MotivoCurto)
			}
		}
	}

	modelo := out.Model
	if modelo == "" {
		modelo = modeloT
	}
	idioma := "pt-BR"
	ms := time.Since(t.fimDoCorpo).Milliseconds()
	duracao := t.duracaoMs
	novo = cloudstore.NewDictation{
		SourcePlatform: cloudstore.SourcePlatformWeb,
		RawText:        bruto, FinalText: final, Mode: modo, Language: &idioma,
		ASRProvider: provT, ASRModel: &modelo, DurationMs: &duracao, ProcessingMs: &ms,
	}
	return novo, aviso, nil, false
}

// conferirNoMeio confere de novo, antes do Polish, o aceite e o epoch da conta
// lido no começo. Devolve a recusa (403 SEM_ACEITE: nada vai ao Polish nem é
// gravado) quando um deles não vale mais, e conferido quando os dois valem. Uma
// falha do banco aqui não descarta nem confere: o Polish não roda (o texto não
// sai sem o aceite conferido), e a gravação, que confere tudo de novo, decide.
func (c *Cloud) conferirNoMeio(base context.Context, t trabalhoDoDitado) (recusa *desfecho, conferido bool) {
	ctx, cancel := context.WithTimeout(base, c.prazos.Gravacao)
	defer cancel()
	ok, err := c.aceiteDoDitado(ctx, t.k.usuario, t.prefs)
	if err != nil {
		return nil, false
	}
	if ok {
		epoch, err := c.cfg.ChavesDeIA.EpochDaConta(ctx, t.k.usuario)
		if err != nil {
			return nil, false
		}
		ok = epoch == t.epoch
	}
	if !ok {
		return &desfecho{status: http.StatusForbidden, code: codeSemAceite, frase: fraseAceiteNoMeio}, false
	}
	return nil, true
}

// polir passa o texto do Smart pelo Polish da conta (decisão N; o adaptador da
// C5a). Devolve o texto do Polish e reescrito, ou o motivo de não ter reescrito
// (vazio quando o texto era curto demais para ir ao provedor: não é falha).
func (c *Cloud) polir(base context.Context, t trabalhoDoDitado, conta *smartconta.Conta, resolvido smartconta.Resolvido,
	rel *relatorioDoDitado) (texto string, reescrito bool, motivo string) {
	if conta == nil || resolvido.SmartFalhou {
		// Sem como conferir os termos da pessoa, o texto não sai.
		return "", false, string(resolver.MotivoDicionario)
	}
	user, provP, modeloP := t.k.usuario, t.prefs.Polish.Provedor, t.prefs.Polish.Modelo
	chave := func(ctx context.Context) (string, error) {
		k, err := c.chaveDeIA(ctx, user, provP)
		if err != nil {
			if semChave(err) {
				return "", resolver.ErrPolishSemChave
			}
			return "", err
		}
		return k, nil
	}

	// O modelo tem de ser um que o provedor listou (modelos_conhecidos.go)
	// antes de chegar à política de esforço, que lembra o modelo e o escreve
	// no log.
	if !c.conhecidos.conhece(provP, modeloP) {
		ctxM, cancel := context.WithTimeout(base, c.prazos.Modelos)
		k, err := chave(ctxM)
		if err != nil {
			cancel()
			if errors.Is(err, resolver.ErrPolishSemChave) {
				return "", false, string(resolver.MotivoSemChave)
			}
			return "", false, string(resolver.MotivoTransporte)
		}
		lista, err := c.cfg.Modelos.ListarModelos(ctxM, provP, k)
		cancel()
		if err != nil {
			return "", false, string(motivoDaListaDeModelos(err))
		}
		c.conhecidos.anotar(provP, lista)
		if !c.conhecidos.conhece(provP, modeloP) {
			return "", false, string(resolver.MotivoModeloInexistente)
		}
	}

	pr, rotulo := polish.Montagem{
		Smart: conta.Smart(), Cliente: c.cfg.Polish, Esforco: c.esforco,
		Provedor: provP, Modelo: modeloP, Chave: chave,
		Contar: func(entrada, saida int) { rel.tokensEntrada, rel.tokensSaida = entrada, saida },
	}.Abrir()
	rel.esforco = rotulo
	ctxP, cancel := context.WithTimeout(base, c.prazos.Polish)
	comeco := time.Now()
	out := conta.Reescrever(ctxP, pr, resolvido)
	cancel()
	rel.polishMs = time.Since(comeco).Milliseconds()
	if out.Reescrito {
		rel.frases, rel.frasesRevertidas = out.Frases, out.FrasesRevertidas
		return out.FinalText, true, ""
	}
	if out.MotivoFallback == resolver.MotivoCurto {
		return "", false, ""
	}
	if out.MotivoFallback == resolver.MotivoNenhum {
		return "", false, string(resolver.MotivoRespostaInvalida)
	}
	return "", false, string(out.MotivoFallback)
}

// motivoDaListaDeModelos traduz a falha da lista de modelos do Polish num motivo
// do Polish (a lista fechada do aviso).
func motivoDaListaDeModelos(err error) resolver.MotivoFallback {
	var e *ia.Erro
	if errors.As(err, &e) {
		switch e.Motivo {
		case ia.MotivoChaveRecusada, ia.MotivoSemPermissao:
			return resolver.MotivoChaveRecusada
		case ia.MotivoLimite:
			return resolver.MotivoLimite
		case ia.MotivoTempoEsgotado:
			return resolver.MotivoPrazo
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return resolver.MotivoPrazo
	}
	return resolver.MotivoTransporte
}

// contaDoSmart devolve o Smart da conta, do cache (a versão é a do aprendido que
// ele usa: versaoDosPares), esperando a montagem até ctx acabar (a montagem
// segue e fica no cache para o próximo ditado). Sem o aprendido (a leitura
// falhou), ou com a montagem falhando ou atrasada, devolve o Smart vazio (só o
// laço de repetição sai) e ok=false: o Polish não roda sem como conferir os
// termos da pessoa.
func (c *Cloud) contaDoSmart(ctx context.Context, user string, pares []dictionary.ParDaNuvem, aprendidoOk bool) (*smartconta.Conta, bool) {
	if aprendidoOk {
		conta, err := c.smart.Obter(ctx, user, versaoDosPares(pares), func() (*smartconta.Conta, error) {
			return smartconta.Montar(user, pares)
		})
		if err == nil {
			return conta, true
		}
	}
	conta, err := smartconta.Montar(user, nil)
	if err != nil {
		return nil, false
	}
	return conta, false
}

// paresDaNuvem converte o aprendido da nuvem (o ListLearned, do mais recente ao
// mais antigo) nos pares do Smart, na mesma ordem.
func paresDaNuvem(l []cloudstore.Learned) []dictionary.ParDaNuvem {
	pares := make([]dictionary.ParDaNuvem, 0, len(l))
	for _, x := range l {
		p := dictionary.ParDaNuvem{Variante: x.Variant, Termo: x.Term, Restrito: x.Restricted, Desfeito: x.UndoneAt != nil}
		if x.Domain != nil {
			p.Dominio = *x.Domain
		}
		pares = append(pares, p)
	}
	return pares
}

// versaoDosPares é a versão do Smart da conta no cache (smartconta.Cache): um
// resumo (SHA-256) dos pares que montam o Smart, na ordem em que chegam. Os
// mesmos pares dão a mesma versão, e qualquer mudança (um par novo, desfeito,
// trocado, ou o aprendido apagado e aprendido de novo) dá outra. O resumo não
// sai da memória e não vai a log.
func versaoDosPares(pares []dictionary.ParDaNuvem) string {
	h := sha256.New()
	var n [8]byte
	escrever := func(s string) {
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	for _, p := range pares {
		escrever(p.Variante)
		escrever(p.Termo)
		escrever(p.Dominio)
		flags := byte(0)
		if p.Restrito {
			flags |= 1
		}
		if p.Desfeito {
			flags |= 2
		}
		h.Write([]byte{flags})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// gravarDitado confere o aceite de novo e grava o ditado pelo ponto único de
// gravação (Store.Create), com o epoch lido no começo. Devolve o resultado e, se
// a gravação falhou depois de o provedor cobrar, o desfecho pago e não gravado
// (o reenvio em 10 minutos só grava).
func (c *Cloud) gravarDitado(base context.Context, t trabalhoDoDitado, novo cloudstore.NewDictation, aviso string,
	rel *relatorioDoDitado) (resultadoDoDitado, *pagoNaoGravado) {
	ctx, cancel := context.WithTimeout(base, c.prazos.Gravacao)
	defer cancel()
	naoGravado := func() (resultadoDoDitado, *pagoNaoGravado) {
		rel.desfecho = "nao_gravado"
		return resultadoDoDitado{desfecho: desfecho{status: http.StatusInternalServerError, code: codeInternal, frase: fraseNaoGravado}},
			&pagoNaoGravado{epoch: t.epoch, novo: novo, aviso: aviso}
	}
	ok, err := c.aceiteDoDitado(ctx, t.k.usuario, t.prefs)
	switch {
	case err != nil:
		return naoGravado()
	case !ok:
		rel.desfecho = "sem_aceite"
		return resultadoDoDitado{desfecho: desfecho{status: http.StatusForbidden, code: codeSemAceite, frase: fraseAceiteNoMeio}}, nil
	}
	var (
		item    cloudstore.Transcription
		criado  bool
		destino *cloudstore.DestinoDoDitado
	)
	if novo.ParaNota {
		// O "Ditar para nota" (N8): a nota e o ditado, na mesma transação.
		var d cloudstore.DestinoDoDitado
		item, criado, d, err = c.cfg.Store.CreateParaNota(ctx, cloudstore.Actor{UserID: t.k.usuario}, t.epoch, t.k.id, novo, c.now())
		destino = &d
	} else {
		item, criado, err = c.cfg.Store.Create(ctx, cloudstore.Actor{UserID: t.k.usuario}, t.epoch, t.k.id, novo, c.now())
	}
	switch {
	case errors.Is(err, cloudstore.ErrEpochChanged):
		// A conta apagou os dados (ou o banco foi restaurado) no meio: nada
		// gravado, e nada guardado para o reenvio.
		rel.desfecho = "sem_aceite"
		return resultadoDoDitado{desfecho: desfecho{status: http.StatusForbidden, code: codeSemAceite, frase: fraseAceiteNoMeio}}, nil
	case errors.Is(err, cloudstore.ErrTombstoned):
		rel.desfecho = "apagado"
		return resultadoDoDitado{desfecho: desfecho{status: http.StatusGone, code: codeApagado, frase: fraseApagado}}, nil
	case errors.Is(err, cloudstore.ErrInvalidDictation), errors.Is(err, cloudstore.ErrInvalidID), errors.Is(err, cloudstore.ErrDeviceNotAllowed):
		// Erro do servidor, não do cliente: gravar de novo daria o mesmo.
		rel.desfecho = "erro_interno"
		return resultadoDoDitado{desfecho: desfecho{status: http.StatusInternalServerError, code: codeInternal, frase: "falha interna"}}, nil
	case err != nil:
		return naoGravado()
	}
	rel.desfecho = "criado"
	if !criado {
		rel.desfecho = "ja_existia"
	}
	return resultadoDoDitado{desfecho: desfecho{aviso: aviso}, item: &item, criado: criado, destino: destino}, nil
}

// logDoDitado escreve a linha "ditado": só contagens e códigos de listas
// fechadas (ADR-021, "Memória, disco e log"). O modelo sai só se o provedor o
// listou (modelos_conhecidos.go); senão, "outro".
func (c *Cloud) logDoDitado(t trabalhoDoDitado, rel relatorioDoDitado, total time.Duration) {
	provT, provP := t.prefs.Transcricao.Provedor, ""
	if t.prefs.PolishLigado {
		provP = t.prefs.Polish.Provedor
	}
	attrs := []any{
		"desfecho", rel.desfecho, "duracao_ms", t.duracaoMs, "fala_ms", t.falaMs, "bytes", rel.bytes,
		"provedor", provT, "modelo", c.conhecidos.paraOLog(provT, t.prefs.Transcricao.Modelo),
		"provedor_ms", rel.provedorMs,
	}
	if rel.motivoProvedor != "" {
		attrs = append(attrs, "motivo_provedor", rel.motivoProvedor)
	}
	attrs = append(attrs,
		"termos_na_dica", rel.termosNaDica,
		"pares", rel.resumo.Recebidos, "pares_aplicados", rel.resumo.Aplicados, "pares_com_dominio", rel.resumo.ComDominio,
		"smart_falhou", rel.smartFalhou, "lacos", rel.lacos, "palavras_removidas", rel.removida, "substituicoes", rel.substituicoes,
		"polish", rel.polish,
	)
	if provP != "" {
		attrs = append(attrs, "polish_provedor", provP, "polish_modelo", c.conhecidos.paraOLog(provP, t.prefs.Polish.Modelo),
			"esforco", rel.esforco, "tokens_entrada", rel.tokensEntrada, "tokens_saida", rel.tokensSaida, "polish_ms", rel.polishMs)
		if rel.frases > 0 {
			attrs = append(attrs, "polish_frases", rel.frases, "polish_frases_revertidas", rel.frasesRevertidas)
		}
	}
	attrs = append(attrs, "ms", total.Milliseconds())
	c.log.Info("ditado", attrs...)
}
