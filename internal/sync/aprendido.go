package syncengine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões do aprendido no PC (ADR-019, "Decisões da F8", F8f2): o que o
// PC faz com cada par do pull, com a resposta do push, e a mutação que sobe.
// Puras, como as do ditado (decisao.go): recebem o estado de um id lido pelo
// storage (storage.EstadoDoAprendido) e devolvem a ação
// (storage.AcaoDoAprendido), que o storage executa na transação de quem a
// recebe. Nada aqui toca banco, rede, arquivo, relógio ou log; quem lê o
// dicionario.json e chama o lote (dictionary.Arquivo.AplicarPares) é o
// Receber (F8f4), e quem compara o arquivo com o espelho no começo da rodada
// é a varredura (F8f3).
//
// # O pull, em três passos
//
// O par que chega da nuvem passa pelo arquivo, que não tem transação comum
// com o SQLite (a ordem está na ADR, "A ordem"). Por isso a decisão dele vem
// em três pedaços, um para cada passo:
//
//  1. PlanejarPar, sob a trava do dicionário, com o arquivo relido (o gancho
//     escolher do lote): o eco, o par mudado aqui que não subiu (pulado), o
//     arquivo indisponível (pendente) e a recusa por regra decidem sem o
//     lote; o resto vai a ele (PlanoDoPar.Lote);
//  2. IntencaoDoPar, depois do lote e antes da gravação (o gancho
//     antesDeGravar): a intenção (write-ahead) de cada id cujo arquivo muda;
//  3. DecidirPar, na transação da página (com os ditados e o cursor): o que
//     o espelho guarda de cada id, pelo plano e pelo que o lote fez.
//
// PlanejarPendente é o passo 1 para o par da nuvem que o espelho guardou e
// ainda não vale aqui (o não aplicado, o pulado, o do arquivo indisponível e
// o state de um conflict): a varredura o tenta de novo quando a causa some.
//
// # A base
//
// A base (a server_version que o espelho guarda) só anda quando o par foi
// conferido contra o arquivo: aplicado, convergente, desfeito, ausente, já
// vale aqui e não aplicado. O pulado, o pendente e o conflict guardam o par
// da nuvem com a versão, e a base fica: sem isso, o pull da mesma versão
// seria tomado por eco, e o par nunca passaria a valer aqui. O não aplicado
// anda a base porque a pessoa que aprende de novo neste PC, sabendo que a
// troca da nuvem não vale aqui, precisa vencer (D4: "para mudar, a pessoa
// aprende de novo"); com a base velha, o Aprender daqui conflitaria, o
// state voltaria, e a troca o tiraria do arquivo de novo, sem fim.
//
// # O que nunca acontece
//
//   - Nenhuma decisão do pull põe marcador: aplicar não ecoa, e a adaptação
//     deste PC (a grafia do termo daqui, o alias_restrito) nunca sobe.
//   - O par mudado aqui que não subiu nunca é sobrescrito (pulado).
//   - Nenhuma ação leva um par que o storage recusaria (o id que não é o da
//     variante, o que não passa no ConferirPar, o código de recusa fora do
//     formato): a recusa do executor desfaria a página inteira, e o cursor
//     nunca andaria.

// Os tipos das decisões do aprendido, para as contagens do log (lista
// fechada, ao lado das do ditado em decisao.go).
const (
	// TipoConvergente: o par já valia aqui; só a base anda.
	TipoConvergente = "convergente"
	// TipoTrocado: o par que valia aqui saiu, e o novo entrou (a troca).
	TipoTrocado = "trocado"
	// TipoDesfeito: o par desfeito na nuvem saiu daqui.
	TipoDesfeito = "desfeito"
	// TipoAusente: o par desfeito na nuvem não tinha o que tirar aqui.
	TipoAusente = "ausente"
	// TipoJaValeAqui: a variante está no termo, posta à mão; nada muda.
	TipoJaValeAqui = "ja_vale_aqui"
	// TipoPendente: o arquivo está indisponível; o par fica guardado, e a
	// varredura o aplica quando ele volta.
	TipoPendente = "pendente"
	// TipoNadaADesfazer: o delete de um par que a nuvem não tem; o desfazer
	// fica confirmado.
	TipoNadaADesfazer = "nada_a_desfazer"
)

// DecisaoDoAprendido é a ação sobre um id do aprendido e o tipo dela (Tipo*).
// O código de uma recusa vai na ação (Situacao.Motivo, em EspelhoRecusado).
type DecisaoDoAprendido struct {
	Acao storage.AcaoDoAprendido
	Tipo string
}

// ─── A mutação ───

// payloadAprendido é o upsert do aprendido, na lista fechada da nuvem: os
// quatro campos sempre presentes, e domain null quando não há (nunca
// ausente).
type payloadAprendido struct {
	Variant    string  `json:"variant"`
	Term       string  `json:"term"`
	Restricted bool    `json:"restricted"`
	Domain     *string `json:"domain"`
}

// MutacaoDoAprendido monta a mutação de um marcador learned (o estado vem de
// storage.Fila.PendentesDoAprendido), com o mutationID dado: o do envio em
// curso, num reenvio; um novo, senão.
//
// A operação sai do espelho: o par presente na projeção guardada é upsert,
// com o par como o arquivo o tem (a variante como está escrita, o termo
// daqui, a restrição efetiva e o nome do domínio); o ausente é delete, sem
// payload. A base é a server_version da base do espelho, ou 0 sem ela; nunca
// a versão de um par da nuvem guardado que ainda não vale aqui (com ela, uma
// mudança daqui venceria uma troca da nuvem que este PC não aplicou: vale o
// primeiro na nuvem, D4). O delete não olha a base, e leva a mesma.
//
// O reenvio do envio em voo repete a mesma operação e o mesmo payload: a
// projeção guardada não muda durante o voo (storage.ErrProjecaoEmVoo), e a
// base também não (o pull pula o id com marcador). O mesmo vale para o envio
// a repetir depois de uma reconexão (storage.Marcador.Repetir), com o
// mutationID novo do aparelho novo. Quem marca o envio
// (storage.Fila.MarcarEnvio) grava a versão local lida (Envio.Versao =
// VersaoLocal): é com ela que DecidirResultadoDoAprendido reconhece a
// mudança feita durante o voo.
func MutacaoDoAprendido(e storage.EstadoDoAprendido, mutationID string) (Mutation, error) {
	m := Mutation{
		MutationID: strings.ToLower(mutationID),
		EntityType: storage.EntidadeAprendido,
		EntityID:   strings.ToLower(e.ID),
	}
	if e.ServerVersion != nil {
		m.BaseVersion = *e.ServerVersion
	}
	p := e.Projecao
	if p == nil {
		m.Operation = OpDelete
		return m, nil
	}
	corpo := payloadAprendido{Variant: p.Variante, Term: p.Termo, Restricted: p.Restrito}
	if p.Dominio != "" {
		d := p.Dominio
		corpo.Domain = &d
	}
	raw, err := json.Marshal(corpo)
	if err != nil {
		return Mutation{}, fmt.Errorf("sync: montando o payload do aprendido: %w", err)
	}
	m.Operation, m.Payload = OpUpsert, raw
	return m, nil
}

// ─── A resposta do push ───

// DecidirResultadoDoAprendido decide o que o resultado r do push faz com o id
// do aprendido de e. Como no ditado, o resultado só vale para o envio em
// curso: sem marcador, com o marcador de outro envio, ou sem a linha do
// espelho, é ignorado.
//
// A operação enviada é a da projeção guardada (presente, upsert; ausente,
// delete), que não muda em voo. "Mudou" é a versão local diferente da
// enviada (o Aprender ou o Desfazer do painel, ou a varredura, durante o
// voo). Diferente do ditado, a mudança do voo NUNCA vai pelo marcador: o
// payload do aprendido sai da projeção guardada, que só a varredura atualiza
// (no começo da rodada), e o marcador reenviado levaria a projeção velha.
// Com mudança, o marcador sai, a base cobre só a versão enviada, e a versão
// local fica acima dela: a varredura seguinte põe o marcador de novo, com o
// que o arquivo tem, com outro id e a base que a resposta trouxe.
//
//   - applied: a base guarda a versão (o applied do aprendido nunca é
//     mesclado) e cobre a enviada; o marcador sai. A situação sai (o
//     "desfeito aqui", uma etiqueta velha, o não aplicado), e o par da nuvem
//     guardado que a versão nova cobre também: no delete, a linha fica só
//     com o id;
//   - conflict: a mudança daqui perdeu na nuvem (D4, vale o primeiro). A
//     base fica (o par da nuvem ainda não vale aqui) e cobre a enviada; o
//     marcador sai; o state é guardado como o par da nuvem, com a versão, e
//     passa a valer aqui pelas regras do PC que recebe (PlanejarPendente,
//     ou o pull da mesma versão), com a etiqueta: "desfeito em outro
//     aparelho" quando o state é o par desfeito, "trocado em outro aparelho"
//     quando é outro conteúdo; a troca enviada fica com a etiqueta, como o
//     par perdido (a linha de estado do painel). Com mudança durante o voo, nada de etiqueta
//     ainda: a mudança do voo sobe com a base velha e conflita de novo, e a
//     etiqueta vem dela. O duplicate não traz o state: vale o par da nuvem
//     já guardado com a mesma versão, se houver; senão, o state chega pelo
//     pull;
//   - rejected base_invalida (só no upsert): a base sai, e o par sobe de
//     novo com 0;
//   - rejected nada_a_desfazer (só no delete): a nuvem não tem o par, e o
//     desfazer fica confirmado; o marcador sai, a base sai, e a linha fica só
//     com o id;
//   - rejected mutacao_reusada: de novo, com outro id;
//   - os outros rejected (e o código fora do caso dele, como base_invalida no
//     delete): "recusado", com o código, e a projeção guardada é a recusada;
//     o marcador sai. A versão coberta não anda (a mudança recusada não
//     subiu, e continua mudança daqui: o pull pula o id, e o esquecer e o
//     epoch novo a reconhecem); a varredura não a manda de novo enquanto a
//     projeção for a recusada (fica "recusado" até mudar, até um epoch novo
//     ou até o esquecer);
//   - o status que o PC não conhece (o gone, que o aprendido não tem): como
//     uma recusa, com CodigoRespostaDesconhecida.
func DecidirResultadoDoAprendido(e storage.EstadoDoAprendido, r MutationResult) DecisaoDoAprendido {
	m := e.Marcador
	if m == nil || !m.EmVoo() || !strings.EqualFold(m.MutationID, r.MutationID) || !e.TemLinha {
		return DecisaoDoAprendido{Tipo: TipoIgnorado}
	}
	mudou := e.VersaoLocal != m.VersaoEnviada
	upsert := e.Presente()
	aplicando := e.Situacao == storage.EspelhoAplicando

	status := r.Status
	duplicado := status == StatusDuplicate
	if duplicado {
		// O reenvio de um envio que a nuvem já tinha decidido: vale a
		// primeira resposta (sem o state).
		status = r.Original
	}
	switch status {
	case StatusApplied:
		a := storage.AcaoDoAprendido{Cobre: cobrir(e, m.VersaoEnviada), Marcador: storage.MarcadorSai}
		if r.Mesclado || r.EntityVersion < 1 {
			// Fora do contrato (o aprendido nunca é mesclado, e a versão é
			// positiva): a versão não é guardada, e o estado chega no pull.
			return DecisaoDoAprendido{Acao: a, Tipo: TipoMesclado}
		}
		a.Base = &storage.BaseDoAprendido{ServerVersion: r.EntityVersion, Desfeita: !upsert}
		if !aplicando {
			a.Situacao = semSituacao(e)
			if g := e.Nuvem; g != nil && (g.Versao == nil || *g.Versao <= r.EntityVersion) {
				a.LimparNuvem = true
			}
		}
		return DecisaoDoAprendido{Acao: a, Tipo: TipoAplicado}

	case StatusConflict:
		a := storage.AcaoDoAprendido{Cobre: cobrir(e, m.VersaoEnviada), Marcador: storage.MarcadorSai}
		if aplicando {
			// Não acontece (a intenção só nasce sem marcador): a linha é da
			// intenção, e a varredura a reconcilia antes.
			return DecisaoDoAprendido{Acao: a, Tipo: TipoConflito}
		}
		var g *storage.ParGuardado
		if !duplicado {
			if par, ok := parQueChegou(e.ID, r.StateAprendido); ok && r.EntityVersion >= 1 {
				v := r.EntityVersion
				g = &storage.ParGuardado{Versao: &v, Par: par}
			}
		} else if n := e.Nuvem; n != nil && n.Versao != nil && *n.Versao == r.EntityVersion {
			g = n
		}
		if g == nil {
			return DecisaoDoAprendido{Acao: a, Tipo: TipoConflito}
		}
		if !mesmoGuardado(e.Nuvem, g) {
			a.Nuvem = g
		}
		if !mudou {
			a.Situacao = &storage.NovaSituacao{Situacao: etiquetaDoConflito(g.Par)}
			if p := e.Projecao; p != nil {
				// A troca que perdeu é a enviada (a projeção guardada não
				// muda em voo): é a linha de estado do painel, ao lado da
				// que vale aqui, que passa a ser a da nuvem. (O delete não
				// conflita: o desfazer sempre vence na nuvem.)
				a.Perdido = &storage.ParPerdido{Variante: p.Variante, Termo: p.Termo}
			}
		}
		return DecisaoDoAprendido{Acao: a, Tipo: TipoConflito}

	case StatusRejected:
		reenviar := storage.MarcadorReenviar
		if mudou {
			// A mudança do voo sobe pela varredura, com o que o arquivo tem.
			reenviar = storage.MarcadorSai
		}
		switch {
		case r.Code == CodigoMutacaoReusada:
			// Outro aparelho já usou este id (um historico.db copiado).
			return DecisaoDoAprendido{Acao: storage.AcaoDoAprendido{Marcador: reenviar}, Tipo: TipoReenviar}
		case r.Code == CodigoBaseInvalida && upsert && e.ServerVersion != nil:
			// A nuvem não conhece a base: sobe com 0. (Com a base já 0, o
			// base_invalida é fora do contrato, e cairia num laço: recusado.)
			return DecisaoDoAprendido{Acao: storage.AcaoDoAprendido{ZerarBase: true, Marcador: reenviar}, Tipo: TipoReenviar}
		case r.Code == CodigoNadaADesfazer && !upsert:
			a := storage.AcaoDoAprendido{ZerarBase: e.ServerVersion != nil, Cobre: cobrir(e, m.VersaoEnviada),
				Marcador: storage.MarcadorSai}
			if !aplicando {
				a.Situacao = semSituacao(e)
				a.LimparNuvem = e.Nuvem != nil
			}
			return DecisaoDoAprendido{Acao: a, Tipo: TipoNadaADesfazer}
		}
		return recusado(codigoCurto(r.Code))
	}
	return recusado(CodigoRespostaDesconhecida)
}

// recusado é a recusa: a situação com o código, e o marcador sai. A
// projeção guardada (a do envio) fica, e é a recusada.
func recusado(codigo string) DecisaoDoAprendido {
	return DecisaoDoAprendido{
		Acao: storage.AcaoDoAprendido{
			Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoRecusado, Motivo: codigo},
			Marcador: storage.MarcadorSai,
		},
		Tipo: TipoRecusado,
	}
}

// etiquetaDoConflito é a etiqueta do PC que perdeu na nuvem (D4): o par
// desfeito lá, ou outro conteúdo (a escolha menor 7).
func etiquetaDoConflito(p dictionary.ParDaNuvem) storage.SituacaoDoEspelho {
	if p.Desfeito {
		return storage.EspelhoDesfeitoEmOutro
	}
	return storage.EspelhoTrocadoEmOutro
}

// ─── O pull ───

// ArquivoDoPar é o dicionario.json diante de um id do aprendido, lido sob a
// trava do dicionário: a linha do id na projeção do arquivo
// (dictionary.Semente.ParesDoPainel; nil quando o id não está nela), ou o
// arquivo indisponível (dictionary.ArquivoIndisponivel: ausente, inválido,
// de outro usuário, preso na troca de nome). O indisponível é "sem
// informação", nunca "o par saiu".
type ArquivoDoPar struct {
	Indisponivel bool
	Projecao     *dictionary.ParDoPainel
}

// PlanoDoPar é o passo 1 do pull de um id (PlanejarPar, PlanejarPendente):
// o que se decide antes do lote, e o que os passos seguintes precisam saber.
type PlanoDoPar struct {
	// ID é o id do aprendido (o do espelho).
	ID string
	// Lote: o par vai ao lote (dictionary.Arquivo.AplicarPares). Senão,
	// Tipo é a decisão: TipoEco, TipoPulado, TipoPendente ou TipoNaoAplicado
	// (a recusa por regra: o par que chegou não vale como par).
	Lote bool
	Tipo string
	// Versao e Par são o par da nuvem (a entity_version e o payload).
	Versao int64
	Par    dictionary.ParDaNuvem
	// Atual é a projeção do id no arquivo antes do lote (nil fora dela).
	Atual *dictionary.ParDoPainel
	// Etiqueta é a etiqueta que o par leva quando passa a valer aqui: a de
	// um conflict que este PC perdeu (DecidirResultadoDoAprendido), cujo
	// state é este par. A intenção ("aplicando") toma o lugar dela na linha
	// até o arquivo ser gravado, e o plano a guarda até DecidirPar.
	Etiqueta storage.SituacaoDoEspelho
}

// PlanejarPar é o passo 1 de uma mudança do pull de um id do aprendido, com o
// estado do espelho e o arquivo lidos sob a trava do dicionário. A tabela da
// ADR ("O que o PC faz com cada par do pull"), na ordem, a primeira que casa
// decide:
//
//	o que chega                      neste PC                                         decisão
//	a versão é a da base             —                                                eco: nada
//	qualquer                         mudou aqui e não subiu (a projeção do arquivo    pulado: o par fica guardado,
//	                                 não é a guardada, ou a versão local passou da    e nem o arquivo nem a base
//	                                 base), ou tem marcador                           mudam
//	qualquer                         arquivo indisponível                             pendente aqui, e o cursor anda
//	o resto                          —                                                vai ao lote (DecidirPar)
//
// A recusa por regra (o tipo, a operação, o payload, o id que não é o da
// variante, o que o ConferirPar recusa, a versão menor que 1) vem antes, e
// a versão mais velha que a conhecida (a da base, ou a do par da nuvem
// guardado) conta como eco: o pull nunca volta um par a um estado velho.
//
// A linha "aplicando" (a intenção de um lote que não terminou) compara o
// arquivo com a projeção esperada quando o arquivo já é ela (o lote gravou):
// o par novo é decidido sobre o que o arquivo tem, e a intenção velha sai
// com a decisão. Diferente das duas, o par é pulado, e a linha não muda (a
// varredura a reconcilia).
func PlanejarPar(e storage.EstadoDoAprendido, c Change, arq ArquivoDoPar) PlanoDoPar {
	p := PlanoDoPar{ID: e.ID, Atual: arq.Projecao}
	if c.EntityType != storage.EntidadeAprendido || c.Operation != OpUpsert || c.EntityVersion < 1 {
		p.Tipo = TipoNaoAplicado
		return p
	}
	par, ok := parQueChegou(e.ID, c.PayloadAprendido)
	if !ok {
		p.Tipo = TipoNaoAplicado
		return p
	}
	p.Par, p.Versao = par, c.EntityVersion
	if sv := e.ServerVersion; sv != nil && p.Versao <= *sv {
		p.Tipo = TipoEco
		return p
	}
	if g := e.Nuvem; g != nil && g.Versao != nil && p.Versao < *g.Versao {
		p.Tipo = TipoEco
		return p
	}
	return planejar(e, p, arq)
}

// TemPendente diz se o espelho guarda um par da nuvem, com a versão, que
// ainda não vale aqui: o não aplicado, o do arquivo indisponível, o pulado
// mais novo que a base, e o state de um conflict ainda não aplicado. Não é
// pendente o par de uma etiqueta que já vale aqui (a base é a versão dele: o
// par guardado é só o texto da etiqueta), nem o da intenção ("aplicando"),
// que a varredura reconcilia antes.
func TemPendente(e storage.EstadoDoAprendido) bool {
	g := e.Nuvem
	if g == nil || g.Versao == nil || e.Situacao == storage.EspelhoAplicando {
		return false
	}
	if e.Situacao == storage.EspelhoNaoAplicado {
		return true
	}
	return e.ServerVersion == nil || *e.ServerVersion < *g.Versao
}

// PlanejarPendente é o passo 1 para o par da nuvem que o espelho guardou e
// ainda não vale aqui (TemPendente): a varredura o tenta de novo quando a
// causa some (o domínio criado aqui, o arquivo que voltou, a mudança daqui
// que voltou ao que era), e o state de um conflict passa a valer aqui por
// ele. É PlanejarPar sem o eco (o não aplicado já anda a base até a versão
// do par): o pulado e o arquivo indisponível valem do mesmo jeito. ok falso:
// nada pendente.
func PlanejarPendente(e storage.EstadoDoAprendido, arq ArquivoDoPar) (PlanoDoPar, bool) {
	if !TemPendente(e) {
		return PlanoDoPar{}, false
	}
	p := PlanoDoPar{ID: e.ID, Atual: arq.Projecao, Versao: *e.Nuvem.Versao, Par: e.Nuvem.Par}
	return planejar(e, p, arq), true
}

// planejar são as linhas do pulado, do arquivo indisponível e do lote.
func planejar(e storage.EstadoDoAprendido, p PlanoDoPar, arq ArquivoDoPar) PlanoDoPar {
	if e.Marcador != nil || e.MudouAqui() {
		p.Tipo = TipoPulado
		return p
	}
	if arq.Indisponivel {
		p.Tipo = TipoPendente
		return p
	}
	guardada := e.Projecao
	if e.Situacao == storage.EspelhoAplicando && mesmaProjecao(arq.Projecao, e.Esperada) {
		// O lote da intenção gravou o arquivo, e a página dele não terminou.
		guardada = e.Esperada
	}
	if !mesmaProjecao(arq.Projecao, guardada) {
		p.Tipo = TipoPulado
		return p
	}
	if (e.Situacao == storage.EspelhoDesfeitoEmOutro || e.Situacao == storage.EspelhoTrocadoEmOutro) &&
		e.Nuvem != nil && e.Nuvem.Versao != nil && *e.Nuvem.Versao == p.Versao {
		p.Etiqueta = e.Situacao
	}
	p.Lote = true
	return p
}

// IntencaoDoPar é o passo 2: a intenção (write-ahead) de um id que o lote
// muda no arquivo, gravada ANTES da gravação dele (o gancho antesDeGravar),
// fora da transação da página. A situação "aplicando", a projeção esperada
// (a do id depois do lote, já com a adaptação deste PC; nil é a intenção
// que tira o par) e o par da nuvem com a versão; o id entra no "veio da
// nuvem" quando o lote escreve o par. ok falso: o arquivo não muda para este
// id (convergente, já vale aqui, ausente, o não aplicado que não tirou nada),
// e não há intenção a gravar.
func IntencaoDoPar(p PlanoDoPar, r dictionary.ResultadoDoPar) (storage.AcaoDoAprendido, bool) {
	if !p.Lote || r.ID != p.ID || mesmaProjecao(p.Atual, r.Projecao) {
		return storage.AcaoDoAprendido{}, false
	}
	v := p.Versao
	a := storage.AcaoDoAprendido{
		Nuvem:    &storage.ParGuardado{Versao: &v, Par: p.Par},
		Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicando},
		Esperada: &storage.NovaProjecao{Par: r.Projecao},
	}
	if r.Situacao == dictionary.SituacaoAplicado {
		a.VeioDaNuvem = storage.VeioEntra
	}
	return a, true
}

// DecidirPar é o passo 3: o que o espelho guarda de um id do pull, na
// transação da página, pelo plano (passo 1) e pelo que o lote fez com o par
// (r; nil quando o par não foi ao lote). e é o estado lido NESTA transação.
//
//	o plano / o lote   o que o espelho grava
//	eco                nada
//	recusa por regra   nada
//	pulado             o par da nuvem, com a versão, como pendente; a base e a
//	                   projeção ficam. Uma etiqueta velha sai (o par mais novo
//	                   a superou); na linha "aplicando", só o par mais novo
//	                   que o da intenção é guardado (naIntencao)
//	pendente           o par da nuvem, com a versão, e "não aplicado"
//	                   (arquivo_indisponivel); a base fica. O state de um
//	                   conflict já guardado com a etiqueta fica como está; na
//	                   linha "aplicando", como no pulado
//	aplicado           a base (a versão), a projeção nova, "aplicado" (ou a
//	                   etiqueta do conflict que este PC perdeu), o id no
//	                   "veio da nuvem"; o par da nuvem sai (o texto é o do
//	                   arquivo), menos com a etiqueta, que o guarda
//	trocado            como o aplicado (o par de antes saiu, e o novo entrou)
//	convergente        a base; a situação sai ("aplicado" fica); o par da
//	                   nuvem sai. Nenhuma etiqueta: nada mudou aqui
//	desfeito           a base desfeita, a projeção ausente, sem etiqueta (o
//	                   desfazer que chega pelo pull não tem etiqueta), menos a
//	                   do conflict que este PC perdeu; o texto sai (fica o id)
//	ausente            como o desfeito: o desfazer remoto não tinha o que tirar
//	já vale aqui       a base, "já vale aqui"; o par da nuvem sai (a variante
//	                   posta à mão não é adotada, e o desfazer não a tira)
//	não aplicado       a base, a projeção (sem o par de antes, quando a troca o
//	                   tirou), "não aplicado" com o motivo, e o par da nuvem
//	                   com a versão (tenta de novo quando a causa some)
//
// A versão local que a base cobre não muda: o par só vai ao lote sem mudança
// daqui (a versão local é a coberta), e o Aprender ou o Desfazer do painel
// feito depois do lote (fora da trava) fica acima dela, e a varredura o vê.
// Nada aqui põe marcador: aplicar não ecoa.
func DecidirPar(e storage.EstadoDoAprendido, p PlanoDoPar, r *dictionary.ResultadoDoPar) DecisaoDoAprendido {
	if p.ID != e.ID {
		return DecisaoDoAprendido{Tipo: TipoIgnorado}
	}
	aplicando := e.Situacao == storage.EspelhoAplicando
	v := p.Versao
	guardado := &storage.ParGuardado{Versao: &v, Par: p.Par}
	tipo := p.Tipo
	if p.Lote && r == nil {
		// O lote não rodou para este par: é o arquivo que não deu.
		tipo = TipoPendente
	}

	switch {
	case p.Lote && r != nil:
	case tipo == TipoPulado:
		if aplicando {
			return naIntencao(e, guardado, TipoPulado)
		}
		if mesmoGuardado(e.Nuvem, guardado) {
			return DecisaoDoAprendido{Tipo: TipoPulado}
		}
		a := storage.AcaoDoAprendido{Nuvem: guardado}
		if etiqueta(e.Situacao) {
			a.Situacao = &storage.NovaSituacao{}
		}
		return DecisaoDoAprendido{Acao: a, Tipo: TipoPulado}
	case tipo == TipoPendente:
		if aplicando {
			return naIntencao(e, guardado, TipoPendente)
		}
		if etiqueta(e.Situacao) && mesmoGuardado(e.Nuvem, guardado) {
			// O state de um conflict já está guardado com a etiqueta, que não
			// se perde pelo arquivo indisponível (PlanejarPendente o tenta de
			// novo, com ela).
			return DecisaoDoAprendido{Tipo: TipoPendente}
		}
		a := storage.AcaoDoAprendido{Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado,
			Motivo: string(dictionary.MotivoArquivoIndisponivel)}}
		if !mesmoGuardado(e.Nuvem, guardado) {
			a.Nuvem = guardado
		}
		return DecisaoDoAprendido{Acao: a, Tipo: TipoPendente}
	case tipo == TipoEco:
		return DecisaoDoAprendido{Tipo: TipoEco}
	default:
		return DecisaoDoAprendido{Tipo: TipoNaoAplicado}
	}

	if r.ID != p.ID {
		return DecisaoDoAprendido{Tipo: TipoIgnorado}
	}
	a := storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: p.Versao, Desfeita: p.Par.Desfeito}}
	if e.Marcador == nil || !e.Marcador.Congelado() {
		// (Em voo, ou a repetir, a projeção é a do envio. Não acontece: o
		// plano pula o id com marcador.)
		a.Projecao = &storage.NovaProjecao{Par: r.Projecao}
	}
	// limpar tira o par da nuvem guardado (o texto); guardar o deixa como o
	// desta versão (o da etiqueta, ou o pendente do não aplicado).
	limpar := func() { a.LimparNuvem = e.Nuvem != nil }
	guardar := func() {
		if !mesmoGuardado(e.Nuvem, guardado) {
			a.Nuvem = guardado
		}
	}
	comEtiqueta := func(sem storage.SituacaoDoEspelho) {
		if p.Etiqueta != storage.EspelhoNenhuma {
			a.Situacao = &storage.NovaSituacao{Situacao: p.Etiqueta}
			guardar()
			return
		}
		a.Situacao = &storage.NovaSituacao{Situacao: sem}
		limpar()
	}

	switch r.Situacao {
	case dictionary.SituacaoAplicado:
		comEtiqueta(storage.EspelhoAplicado)
		a.VeioDaNuvem = storage.VeioEntra
		if p.Atual != nil {
			return DecisaoDoAprendido{Acao: a, Tipo: TipoTrocado}
		}
		return DecisaoDoAprendido{Acao: a, Tipo: TipoAplicado}
	case dictionary.SituacaoConvergente:
		s := storage.EspelhoNenhuma
		if e.Situacao == storage.EspelhoAplicado || aplicando {
			s = storage.EspelhoAplicado
		}
		a.Situacao = &storage.NovaSituacao{Situacao: s}
		limpar()
		return DecisaoDoAprendido{Acao: a, Tipo: TipoConvergente}
	case dictionary.SituacaoDesfeito:
		comEtiqueta(storage.EspelhoNenhuma)
		return DecisaoDoAprendido{Acao: a, Tipo: TipoDesfeito}
	case dictionary.SituacaoAusente:
		comEtiqueta(storage.EspelhoNenhuma)
		return DecisaoDoAprendido{Acao: a, Tipo: TipoAusente}
	case dictionary.SituacaoJaValeAqui:
		a.Situacao = &storage.NovaSituacao{Situacao: storage.EspelhoJaValeAqui}
		limpar()
		return DecisaoDoAprendido{Acao: a, Tipo: TipoJaValeAqui}
	case dictionary.SituacaoNaoAplicado:
		a.Situacao = &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado, Motivo: string(motivoValido(r.Motivo))}
		guardar()
		return DecisaoDoAprendido{Acao: a, Tipo: TipoNaoAplicado}
	}
	// Uma situação que este PC não conhece (defeito do lote): nada é gravado,
	// e a linha "aplicando", se houver, fica para a varredura reconciliar.
	return DecisaoDoAprendido{Tipo: TipoIgnorado}
}

// naIntencao é o pulado e o pendente na linha "aplicando" (a intenção de um
// lote que não terminou): a linha fica para a varredura reconciliar pelo
// arquivo (a situação e a projeção esperada não mudam), mas o par que chega,
// se é mais novo que o da intenção, fica guardado no lugar dele. É ele que a
// varredura deixa pendente quando confirma a intenção ou a devolve, e o
// aplica depois. Sem isso, o cursor andava sem guardar nada, e a varredura
// aplicava o par velho da intenção: o PC voltava a um estado velho da nuvem,
// e nunca se corrigia (o par novo já tinha passado do cursor).
func naIntencao(e storage.EstadoDoAprendido, g *storage.ParGuardado, tipo string) DecisaoDoAprendido {
	if n := e.Nuvem; n != nil && n.Versao != nil && *g.Versao <= *n.Versao {
		return DecisaoDoAprendido{Tipo: tipo}
	}
	return DecisaoDoAprendido{Acao: storage.AcaoDoAprendido{Nuvem: g}, Tipo: tipo}
}

// ─── Auxiliares ───

// parQueChegou é o par que a nuvem mandou (o payload do pull, ou o state do
// conflict), conferido como o storage o confere antes de guardar: o id do
// fio é o do espelho, o id do espelho é o da variante (dictionary.IDDoPar), e
// o par passa no dictionary.ConferirPar. ok falso: o par não vale como par, e
// nada dele é guardado.
func parQueChegou(id string, a *Aprendido) (dictionary.ParDaNuvem, bool) {
	if a == nil || !pareceUUID(id) || !strings.EqualFold(a.ID, id) || dictionary.IDDoPar(a.Variant) != id {
		return dictionary.ParDaNuvem{}, false
	}
	if dictionary.ConferirPar(a.Variant, a.Term, a.Restricted, a.Domain) != nil {
		return dictionary.ParDaNuvem{}, false
	}
	par := dictionary.ParDaNuvem{Variante: a.Variant, Termo: a.Term, Restrito: a.Restricted, Desfeito: a.UndoneAt != nil}
	if a.Domain != nil {
		par.Dominio = *a.Domain
	}
	return par, true
}

// cobrir é a versão local que a base passa a cobrir (Cobre): v, quando anda
// (acima da coberta) e cabe (até a local); nil, senão, e nada é gravado.
func cobrir(e storage.EstadoDoAprendido, v int64) *int64 {
	if v <= e.VersaoBase || v > e.VersaoLocal {
		return nil
	}
	return &v
}

// semSituacao tira a situação da linha; nil quando ela já não tem.
func semSituacao(e storage.EstadoDoAprendido) *storage.NovaSituacao {
	if e.Situacao == storage.EspelhoNenhuma {
		return nil
	}
	return &storage.NovaSituacao{}
}

// etiqueta diz se a situação é uma das etiquetas da D4.
func etiqueta(s storage.SituacaoDoEspelho) bool {
	return s == storage.EspelhoDesfeitoEmOutro || s == storage.EspelhoTrocadoEmOutro
}

// mesmoGuardado diz se o par da nuvem guardado já é g (a mesma versão e o
// mesmo par): gravá-lo de novo não mudaria nada.
func mesmoGuardado(atual, g *storage.ParGuardado) bool {
	if atual == nil || g == nil {
		return atual == g
	}
	if (atual.Versao == nil) != (g.Versao == nil) || atual.Versao != nil && *atual.Versao != *g.Versao {
		return false
	}
	return atual.Par == g.Par
}

// mesmaProjecao compara duas projeções (nil é a ausente), com a forma local
// (alias ou alias_restrito) inclusive.
func mesmaProjecao(a, b *dictionary.ParDoPainel) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// motivoValido é m, quando é da lista fechada; senão (defeito do lote),
// MotivoNaoAgiria, para o executor não recusar a página inteira.
func motivoValido(m dictionary.MotivoDoPar) dictionary.MotivoDoPar {
	for _, v := range dictionary.MotivosDoPar() {
		if m == v {
			return m
		}
	}
	return dictionary.MotivoNaoAgiria
}
