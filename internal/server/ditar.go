package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A rota de ditar (ADR-021, decisão A e C5b; 06, "Ditar pelo celular"):
//
//	POST /api/v1/dictations
//
// O corpo é o áudio cru (audio/webm, até 1 MiB), e o que o app mediu vai em
// cabeçalhos: Ditado-Id (o UUID v7 do ditado, a chave da idempotência com a
// conta), Ditado-Duracao-Ms e Ditado-Fala-Ms. A resposta é o ditado gravado
// (201, o itemDTO do histórico, com o aviso do Polish quando ele caiu para o
// Smart) ou, no reenvio, o que já está gravado (200).
//
// Antes de ler o corpo, a rota confere, nesta ordem: a credencial (o
// AcessoDitar, no portão), o aceite dos provedores que as preferências usam, as
// chaves deles, o id e o que a nuvem e a memória já têm com ele, os cabeçalhos,
// o tipo e o tamanho, os limites da conta e a vaga, e, já como dono do id, a
// situação dele de novo (o primeiro com o mesmo id pode ter gravado nesse meio).
// Só então lê o corpo, sob o prazo dele, confere pelos primeiros bytes que é
// webm, e solta o trabalho do provedor, do Smart, do Polish e da gravação da
// conexão (ditar_trabalho.go): o celular cair não perde o que já foi pago.
//
// O áudio fica numa variável do pedido e do trabalho, e sai de escopo no fim:
// nunca vai a disco, a log, a erro, a resposta nem a uma estrutura de vida longa
// (a memória do registro não tem campo para ele). Nada do texto ditado, da chave
// ou do corpo de erro do provedor vai a log nem a mensagem de erro.

// Os códigos da rota de ditar que as rotas das chaves não têm (a lista fechada
// do ditado no celular, no 06).
const (
	codeUmPorVez = "UM_POR_VEZ"
	codeOcupado  = "OCUPADO"
	codeFormato  = "FORMATO"
	codeSemFala  = "SEM_FALA"
	codeApagado  = "APAGADO"
)

// As frases fixas da rota de ditar. Nenhuma depende do que veio no pedido.
const (
	fraseUmPorVez        = "um ditado de cada vez: espere o anterior terminar"
	fraseOcupado         = "o servidor está ocupado com outros ditados: tente de novo em alguns segundos"
	fraseLimiteDoDitado  = "ditados demais em pouco tempo: espere um pouco"
	fraseFormato         = "o áudio tem de ser webm (audio/webm)"
	fraseSemFalaMedida   = "o app não mediu fala bastante: nada foi enviado ao provedor"
	fraseSemFala         = "o provedor não ouviu fala no áudio"
	fraseApagado         = "esse ditado foi apagado"
	fraseCorpoGrande     = "o áudio passa de 1 MiB"
	fraseDuracaoLonga    = "o ditado passa do teto de duração"
	fraseCorpo           = "o áudio não chegou inteiro"
	fraseId              = "Ditado-Id ausente ou inválido: mande o UUID v7 do ditado"
	fraseCabecalhos      = "Ditado-Duracao-Ms e Ditado-Fala-Ms têm de ser milissegundos inteiros, com a fala até a duração"
	fraseAceiteNoMeio    = "o aceite do ditado no celular deixou de valer (ou os dados da conta foram apagados) durante o ditado: nada foi guardado"
	fraseNaoGravado      = "o texto saiu, mas não foi guardado: tente de novo com o mesmo ditado (sem pagar de novo)"
	fraseSemChaveDoDitar = "nenhuma chave guardada para o provedor das preferências"
)

// Os tetos e os mínimos da rota (ADR-021, "Limites", escolhas menores 3, 6 e 7).
const (
	// TetoDoCorpoDoDitado é o maior corpo aceito: 1 MiB (opus a 32 kbit/s dá
	// uns 0,5 MB em 120 s).
	TetoDoCorpoDoDitado = 1 << 20
	// TetoDaDuracaoDoDitado é o teto de Ditado-Duracao-Ms: os 120 s do PC, com
	// 5 s de folga para o fim da gravação no app.
	TetoDaDuracaoDoDitado = 125 * time.Second
	// FalaMinima é a fala medida abaixo da qual o ditado nem é processado
	// (SEM_FALA antes de ler o corpo). O app mede e já não envia; isto é defesa
	// contra defeito do app, e não contra ataque: o servidor não confere o
	// número, e quem paga é a própria pessoa (BYOK).
	FalaMinima = 300 * time.Millisecond
)

// maxVoltas: um pedido que esperou o mesmo id e viu o dono parar antes do
// provedor tenta com o próprio corpo; isto limita as voltas.
const maxVoltas = 3

// ditadoDTO é a resposta da rota: o item do histórico e, quando o Polish caiu
// para o Smart, o motivo (um resolver.MotivoFallback, nunca texto).
type ditadoDTO struct {
	itemDTO
	Aviso string `json:"aviso,omitempty"`
	// Nota e Destino: só o ditado que foi para uma nota (ADR-022, N8).
	Nota    *noteDTO    `json:"nota,omitempty"`
	Destino *destinoDTO `json:"destino,omitempty"`
}

// handleDitar é o POST /api/v1/dictations.
func (c *Cloud) handleDitar(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	ctx := r.Context()

	// 1. As preferências, o aceite e as chaves.
	prefs, err := c.cfg.Ditado.LerPreferenciasDoDitado(ctx, p.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	if ok, err := c.aceiteDoDitado(ctx, p.UserID, prefs); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	} else if !ok {
		c.negarSemAceite(w, r)
		return
	}
	if !c.conferirChavesDoDitado(w, r, p.UserID, prefs) {
		return
	}

	// 2. O id.
	id, ok := idDoDitado(r.Header)
	if !ok {
		anotarMotivo(ctx, "id")
		writeError(w, http.StatusBadRequest, codeBadRequest, fraseId)
		return
	}
	// O destino na nota (ADR-022, N8): o "Ditar para nota" ou o microfone de
	// dentro da nota aberta. Sem o cabeçalho, o ditado comum.
	destino, ok := destinoDoDitado(r.Header)
	if !ok {
		anotarMotivo(ctx, "destino")
		writeError(w, http.StatusBadRequest, codeBadRequest, fraseDestino)
		return
	}
	k := chaveDoDitado{usuario: p.UserID, id: id}
	rc := http.NewResponseController(w)
	// A guarda do andamento de que este pedido for dono: um pânico antes de ele
	// encerrar o andamento ou entregá-lo ao trabalho solto devolve a vaga e o
	// andamento (o pânico segue para o recuperar, que responde 500).
	guarda := guardaDoDono{g: c.ditados}
	defer guarda.soltar()

	// avisoDoPrimeiro é o aviso do Polish do ditado que este pedido esperou: o
	// reenvio o recebe junto com o ditado gravado.
	avisoDoPrimeiro := ""
	for range maxVoltas {
		// 3. O que a nuvem já tem com o id.
		sit, err := c.cfg.Store.Situacao(ctx, p.UserID, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
			return
		}
		switch sit.Estado {
		case cloudstore.IdDitado:
			anotarMotivo(ctx, "reenvio")
			c.responderDitado(w, r, http.StatusOK, *sit.Ditado, avisoDoPrimeiro, nil)
			return
		case cloudstore.IdLapide:
			anotarMotivo(ctx, "apagado")
			writeError(w, http.StatusGone, codeApagado, fraseApagado)
			return
		}

		// 4. O que a memória tem com o id.
		o := c.ditados.olhar(k, sit.Epoch)
		switch {
		case o.esperar != nil:
			d, ok := c.esperarOPrimeiro(w, r, rc, o.esperar)
			if !ok {
				return
			}
			if d.ok() || d.reprocessar {
				avisoDoPrimeiro = d.aviso
				continue
			}
			anotarMotivo(ctx, "reenvio")
			responderDesfecho(w, d)
			return
		case o.semFala:
			anotarMotivo(ctx, "sem_fala")
			writeError(w, http.StatusUnprocessableEntity, codeSemFala, fraseSemFala)
			return
		case o.regravar != nil:
			// O provedor já cobrou este ditado, e a gravação falhou: só grava.
			guarda.pegar(k, o.dono, o.regravar)
			anotarMotivo(ctx, "regravar")
			c.ditar(w, r, rc, trabalhoDoDitado{
				k: k, a: o.dono, epoch: sit.Epoch, prefs: prefs, pago: o.regravar, fimDoCorpo: time.Now(), destino: destino,
			}, &guarda)
			return
		}

		// 5. Os cabeçalhos, o tipo e o tamanho, ainda sem ler o corpo.
		duracao, fala, ok := medidasDoDitado(r.Header)
		switch {
		case !ok:
			anotarMotivo(ctx, "cabecalho")
			writeError(w, http.StatusBadRequest, codeBadRequest, fraseCabecalhos)
			return
		case duracao > TetoDaDuracaoDoDitado:
			anotarMotivo(ctx, "duracao")
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, fraseDuracaoLonga)
			return
		case fala < FalaMinima:
			anotarMotivo(ctx, "sem_fala")
			writeError(w, http.StatusUnprocessableEntity, codeSemFala, fraseSemFalaMedida)
			return
		case asr.NormalizarTipo(r.Header.Get("Content-Type")) != tipoDoAudio || len(r.Header.Values("Content-Type")) != 1 || r.ContentLength == 0:
			anotarMotivo(ctx, "formato")
			writeError(w, http.StatusUnsupportedMediaType, codeFormato, fraseFormato)
			return
		case r.ContentLength > TetoDoCorpoDoDitado:
			anotarMotivo(ctx, "corpo_grande")
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, fraseCorpoGrande)
			return
		}

		// 6. A vaga: o mesmo id que chegou agora é esperado; a conta com outro
		// ditado, os limites dela e as vagas do total recusam.
		dono, esperar, recusa := c.ditados.entrar(k)
		switch {
		case esperar != nil:
			d, ok := c.esperarOPrimeiro(w, r, rc, esperar)
			if !ok {
				return
			}
			if d.ok() || d.reprocessar {
				avisoDoPrimeiro = d.aviso
				continue
			}
			anotarMotivo(ctx, "reenvio")
			responderDesfecho(w, d)
			return
		case recusa != nil:
			anotarMotivo(ctx, recusa.motivo)
			w.Header().Set("Retry-After", strconv.Itoa(cloudauth.RetryAfter(recusa.espera)))
			writeError(w, recusa.status, recusa.code, recusa.frase)
			return
		}
		guarda.pegar(k, dono, nil)

		// 7. A situação de novo, agora como dono do id: enquanto este pedido o
		// for, ninguém mais grava o id, mas o primeiro com o mesmo id pode ter
		// gravado (e saído da memória) entre a leitura do passo 3 e a vaga. Sem
		// isto, o reenvio que chega no fim do primeiro pagaria o provedor de novo
		// pelo mesmo ditado. O epoch lido aqui é o que a gravação confere.
		sit, err = c.cfg.Store.Situacao(ctx, p.UserID, id)
		if err != nil || sit.Estado != cloudstore.IdNada {
			guarda.largar()
			c.ditados.desistir(k, dono)
			switch {
			case err != nil:
				writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
			case sit.Estado == cloudstore.IdDitado:
				anotarMotivo(ctx, "reenvio")
				c.responderDitado(w, r, http.StatusOK, *sit.Ditado, avisoDoPrimeiro, nil)
			default:
				anotarMotivo(ctx, "apagado")
				writeError(w, http.StatusGone, codeApagado, fraseApagado)
			}
			return
		}

		// 8. O corpo, sob o prazo dele, e o formato pelos primeiros bytes.
		_ = rc.SetReadDeadline(time.Now().Add(c.prazos.Corpo))
		audio, err := lerOAudio(w, r)
		if err != nil {
			var grande *http.MaxBytesError
			d := desfecho{status: http.StatusBadRequest, code: codeBadRequest, frase: fraseCorpo, reprocessar: true}
			motivo := "corpo"
			if errors.As(err, &grande) {
				d.status, d.code, d.frase, motivo = http.StatusRequestEntityTooLarge, codeTooLarge, fraseCorpoGrande, "corpo_grande"
			}
			guarda.largar()
			c.ditados.concluir(k, dono, d, false, nil)
			anotarMotivo(ctx, motivo)
			responderDesfecho(w, d)
			return
		}
		if !pareceWebm(audio) {
			d := desfecho{status: http.StatusUnsupportedMediaType, code: codeFormato, frase: fraseFormato, reprocessar: true}
			guarda.largar()
			c.ditados.concluir(k, dono, d, false, nil)
			anotarMotivo(ctx, "formato")
			responderDesfecho(w, d)
			return
		}

		// 9. O trabalho solto.
		c.ditar(w, r, rc, trabalhoDoDitado{
			k: k, a: dono, epoch: sit.Epoch, prefs: prefs, audio: audio,
			duracaoMs: duracao.Milliseconds(), falaMs: fala.Milliseconds(), fimDoCorpo: time.Now(), destino: destino,
		}, &guarda)
		return
	}
	anotarMotivo(ctx, "ocupado")
	w.Header().Set("Retry-After", strconv.Itoa(int(esperaDaVaga/time.Second)))
	writeError(w, http.StatusServiceUnavailable, codeOcupado, fraseOcupado)
}

// ditar solta o trabalho do ditado da conexão e espera o resultado para
// responder. O trabalho segue se o celular cair (o pedido acaba), e grava o
// ditado mesmo assim. O prazo de escrita da resposta é estendido a partir do fim
// do corpo: o writeTimeout do servidor conta do fim dos cabeçalhos, e não
// cobriria o corpo, o provedor, o Polish e a gravação. O andamento passa da
// guarda do pedido ao trabalho, que o encerra.
func (c *Cloud) ditar(w http.ResponseWriter, r *http.Request, rc *http.ResponseController, t trabalhoDoDitado, guarda *guardaDoDono) {
	_ = rc.SetWriteDeadline(t.fimDoCorpo.Add(c.prazos.Resposta))
	t.ctxDoPedido = r.Context()
	saida := make(chan resultadoDoDitado, 1)
	c.ditados.comecarTrabalho()
	go c.rodarDitado(t, saida)
	guarda.largar()
	// O áudio é do trabalho agora: este pedido não o segura mais.
	t.audio = nil
	select {
	case res := <-saida:
		if !res.desfecho.ok() {
			anotarMotivo(r.Context(), res.motivo)
			responderDesfecho(w, res.desfecho)
			return
		}
		status := http.StatusCreated
		if !res.criado {
			status = http.StatusOK
		}
		c.responderDitado(w, r, status, *res.item, res.desfecho.aviso, res.destino)
	case <-r.Context().Done():
		// O celular caiu: o trabalho segue e grava. O reenvio recebe o ditado.
	}
}

// esperarOPrimeiro espera o ditado em andamento com o mesmo id e devolve o
// desfecho dele. O prazo de escrita deste pedido é estendido antes: o
// writeTimeout dele conta do fim dos cabeçalhos dele, e a espera pode passar
// disso. ok é falso quando o pedido acabou antes (o celular caiu).
func (c *Cloud) esperarOPrimeiro(w http.ResponseWriter, r *http.Request, rc *http.ResponseController, a *andamento) (desfecho, bool) {
	_ = rc.SetWriteDeadline(time.Now().Add(c.prazos.Corpo + c.prazos.Resposta + folgaDeQuemEspera))
	select {
	case <-a.pronto:
		return a.desfecho, true
	case <-r.Context().Done():
		return desfecho{}, false
	}
}

// folgaDeQuemEspera soma-se ao prazo do primeiro, para quem espera o mesmo id.
const folgaDeQuemEspera = 5 * time.Second

// EsperarDitados é a espera do desligamento (ADR-021, escolha menor 22): nenhum
// ditado novo pega vaga (OCUPADO), e os trabalhos soltos em andamento (o
// provedor, o Smart, o Polish e a gravação) terminam, até ctx acabar. Quem sobe
// o servidor a chama depois do Shutdown do http.Server e antes de fechar o
// Store: o trabalho solto grava no banco. Devolve o erro do ctx se ele acabou
// antes.
func (c *Cloud) EsperarDitados(ctx context.Context) error {
	return c.ditados.esperarTrabalhos(ctx)
}

// DitadosEmAndamento diz quantos trabalhos soltos de ditado estão rodando.
func (c *Cloud) DitadosEmAndamento() int { return c.ditados.emAndamento() }

// ContasComOSmartNaMemoria diz quantas contas têm o Smart (montado com o
// aprendido delas) guardado na memória da rota de ditar: só a contagem, para os
// testes do esquecer.
func (c *Cloud) ContasComOSmartNaMemoria() int { return c.smart.Len() }

// esquecerDitadoDaConta tira da memória o que é da conta: o Smart montado (com o
// aprendido dela), o "sem fala" e o desfecho pago e não gravado (com o texto).
// É o "Apagar meus dados" e o "Desligar o ditado no celular".
func (c *Cloud) esquecerDitadoDaConta(userID string) {
	c.smart.Esquecer(userID)
	c.ditados.esquecerConta(userID)
}

// aceiteDoDitado diz se o aceite vigente da conta cobre o provedor da
// transcrição e, com o Polish ligado, o do Polish (decisão H): a mesma
// conferência antes de ler o corpo, antes do Polish e antes de gravar.
func (c *Cloud) aceiteDoDitado(ctx context.Context, userID string, prefs cloudstore.PreferenciasDoDitado) (bool, error) {
	ok, err := c.aceite.AceiteVigente(ctx, userID, prefs.Transcricao.Provedor)
	if err != nil || !ok {
		return false, err
	}
	if prefs.PolishLigado {
		return c.aceite.AceiteVigente(ctx, userID, prefs.Polish.Provedor)
	}
	return true, nil
}

// conferirChavesDoDitado recusa (409 SEM_CHAVE) as preferências que pedem um
// provedor sem chave guardada, ou com a chave cifrada por outra chave de cifra,
// sem decifrar nada: a chave só é decifrada durante a chamada ao provedor.
func (c *Cloud) conferirChavesDoDitado(w http.ResponseWriter, r *http.Request, userID string, prefs cloudstore.PreferenciasDoDitado) bool {
	provedores := []string{prefs.Transcricao.Provedor}
	if prefs.PolishLigado && prefs.Polish.Provedor != prefs.Transcricao.Provedor {
		provedores = append(provedores, prefs.Polish.Provedor)
	}
	for _, prov := range provedores {
		k, err := c.cfg.ChavesDeIA.LerChaveDeIA(r.Context(), userID, prov)
		switch {
		case errors.Is(err, cloudstore.ErrChaveDeIAAusente):
			anotarMotivo(r.Context(), "sem_chave")
			writeError(w, http.StatusConflict, codeSemChave, fraseSemChaveDoDitar)
			return false
		case err != nil:
			writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
			return false
		case !c.cofre.MesmoID(k.IDDaCifra):
			anotarMotivo(r.Context(), "sem_chave")
			writeError(w, http.StatusConflict, codeSemChave, "a chave guardada não vale mais neste servidor: cole a chave de novo")
			return false
		}
	}
	return true
}

// responderDitado escreve o ditado gravado, com o aviso do Polish se houver.
// O ditado que foi para uma nota (N8) leva também a nota como ficou e o
// destino: o desta gravação (destino), ou, no reenvio, o que o ditado gravado
// diz (cloudstore.DestinoGravado). A nota que não se lê (apagada no meio) fica
// de fora; o ditado já está gravado.
func (c *Cloud) responderDitado(w http.ResponseWriter, r *http.Request, status int, t cloudstore.Transcription, aviso string,
	destino *cloudstore.DestinoDoDitado) {
	d := ditadoDTO{itemDTO: toItemDTO(t), Aviso: aviso}
	if t.VirouNota != nil {
		var nota *cloudstore.Note
		if n, err := c.cfg.Store.GetNote(r.Context(), t.UserID, *t.VirouNota); err == nil {
			nota = &n
			dto := toNoteDTO(n)
			d.Nota = &dto
		}
		dest := cloudstore.DestinoGravado(t, nota)
		if destino != nil {
			dest = *destino
		}
		d.Destino = &destinoDTO{NotaID: dest.NotaID, Nova: dest.Nova, Linhas: dest.Linhas}
		if !dest.Nova && dest.Linhas > 0 {
			antes := dest.CorpoAntes
			d.Destino.CorpoAntes = &antes
		}
	}
	writeJSON(w, status, d)
}

// destinoDTO é para onde o ditado foi (N8): a nota, se ela nasceu do ditado,
// quantas linhas entraram e, no acrescentar, o corpo de antes (o Desfazer o
// manda de volta). No reenvio, as linhas e o corpo de antes não se sabem mais.
type destinoDTO struct {
	NotaID     string  `json:"nota_id"`
	Nova       bool    `json:"nova"`
	Linhas     int     `json:"linhas"`
	CorpoAntes *string `json:"corpo_antes,omitempty"`
}

// ParaNota é o valor do cabeçalho Ditado-Nota do "Ditar para nota": a nuvem
// escolhe a nota pela regra do título (N8). O outro valor é o id da nota aberta
// (o microfone de dentro dela).
const ParaNota = "para-nota"

// fraseDestino: o Ditado-Nota não é "para-nota" nem o id de uma nota.
const fraseDestino = `Ditado-Nota tem de ser "para-nota" ou o id da nota aberta`

// destinoNaNota é o que o Ditado-Nota pediu: nada (o ditado comum, que vai ao
// histórico), o "Ditar para nota" (paraNota) ou a nota aberta (nota).
type destinoNaNota struct {
	paraNota bool
	nota     *string
}

// destinoDoDitado lê o Ditado-Nota: ausente, um valor só com "para-nota", ou
// um UUID.
func destinoDoDitado(h http.Header) (destinoNaNota, bool) {
	v := h.Values("Ditado-Nota")
	switch {
	case len(v) == 0:
		return destinoNaNota{}, true
	case len(v) != 1:
		return destinoNaNota{}, false
	case v[0] == ParaNota:
		return destinoNaNota{paraNota: true}, true
	}
	id, ok := cloudstore.CanonicalUUID(v[0])
	if !ok {
		return destinoNaNota{}, false
	}
	return destinoNaNota{nota: &id}, true
}

// responderDesfecho escreve a recusa de um desfecho, com o Retry-After quando
// ela tem.
func responderDesfecho(w http.ResponseWriter, d desfecho) {
	if d.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(d.retryAfter))
	}
	writeError(w, d.status, d.code, d.frase)
}

// tipoDoAudio é o único tipo que a rota aceita (o webm/opus do MediaRecorder do
// Chrome do Android; ADR-021, decisão C).
const tipoDoAudio = "audio/webm"

// idDoDitado lê o Ditado-Id: um valor só, um UUID v7 (a versão 7 e a variante
// do RFC 9562), na forma canônica, em minúsculas.
func idDoDitado(h http.Header) (string, bool) {
	v := h.Values("Ditado-Id")
	if len(v) != 1 {
		return "", false
	}
	id, ok := cloudstore.CanonicalUUID(v[0])
	if !ok || id[14] != '7' || !bytes.ContainsRune([]byte("89ab"), rune(id[19])) {
		return "", false
	}
	return id, true
}

// medidasDoDitado lê o Ditado-Duracao-Ms e o Ditado-Fala-Ms: um valor só cada,
// milissegundos inteiros, sem sinal, e a fala até a duração.
func medidasDoDitado(h http.Header) (duracao, fala time.Duration, ok bool) {
	d, okD := milissegundos(h, "Ditado-Duracao-Ms")
	f, okF := milissegundos(h, "Ditado-Fala-Ms")
	if !okD || !okF || f > d {
		return 0, 0, false
	}
	return d, f, true
}

func milissegundos(h http.Header, nome string) (time.Duration, bool) {
	v := h.Values(nome)
	if len(v) != 1 || len(v[0]) == 0 || len(v[0]) > 9 {
		return 0, false
	}
	for i := 0; i < len(v[0]); i++ {
		if v[0][i] < '0' || v[0][i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(v[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(n) * time.Millisecond, true
}

// lerOAudio lê o corpo inteiro, até TetoDoCorpoDoDitado, numa variável só: com o
// Content-Length, num slice do tamanho certo (sem as cópias do crescimento de um
// buffer); sem ele, o http.MaxBytesReader corta o que passar do teto.
func lerOAudio(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	corpo := http.MaxBytesReader(w, r.Body, TetoDoCorpoDoDitado)
	if r.ContentLength > 0 {
		buf := make([]byte, r.ContentLength)
		if _, err := io.ReadFull(corpo, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	return io.ReadAll(corpo)
}

// cabecalhoEBML são os quatro primeiros bytes de todo arquivo EBML (o contêiner
// do webm e do Matroska).
var cabecalhoEBML = []byte{0x1A, 0x45, 0xDF, 0xA3}

// pareceWebm diz se os primeiros bytes são os de um webm: o cabeçalho EBML e,
// dentro dele, o elemento DocType (0x4282) com o valor "webm" (o Matroska diz
// "matroska"). Nunca o Content-Type, que é só o que o cliente diz. Não decodifica
// nada.
func pareceWebm(b []byte) bool {
	if !bytes.HasPrefix(b, cabecalhoEBML) {
		return false
	}
	cab := b[:min(len(b), 64)]
	i := bytes.Index(cab, []byte{0x42, 0x82})
	if i < 0 || i+3 > len(cab) {
		return false
	}
	tam := cab[i+2]
	if tam&0x80 == 0 {
		return false // só o tamanho de um byte (o que o Chrome escreve)
	}
	n := int(tam & 0x7f)
	if n < 4 || i+3+n > len(cab) {
		return false
	}
	return string(bytes.TrimRight(cab[i+3:i+3+n], "\x00")) == "webm"
}
