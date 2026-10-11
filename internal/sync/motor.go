package syncengine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Motor é UMA rodada do sync de um PC conectado (ADR-019, F7a/F7b): o push
// da fila, em lotes, até ela esvaziar, e depois o pull, página por página,
// até não ter mais. O laço (Laco) o monta a cada rodada com a credencial do
// momento; os testes o rodam passo a passo.
//
// As regras de ordem:
//   - a rede nunca fica dentro de uma transação: os ids do envio são gravados
//     antes (MarcarEnvio), e a resposta é aplicada depois (Fila.Aplicar);
//   - um push sem resposta encerra a rodada SEM pull: o pull poderia passar
//     pela versão mesclada (ou pelo selo) enquanto o ditado ainda tem
//     marcador e pulá-la, e o duplicate do reenvio não traz o estado
//     (ADR-019, "Decisões da F2", para a F7b);
//   - o primeiro passo de toda rodada é o reenvio do que está em voo (a fila
//     os devolve primeiro), com os mesmos mutation_id.
type Motor struct {
	Fila    *storage.Fila
	Cliente SyncClient
	// Aparelho e PareadoEm são os do pareamento (a tabela instalacao): o
	// cursor é deles, e outro valor é reconexão (o cursor, o epoch e as
	// server_version continuam, passados ao pareamento novo; só o epoch novo,
	// ou a primeira rodada da instalação, recomeça).
	Aparelho  string
	PareadoEm string
	// NovoID gera o mutation_id (UUID v7).
	NovoID func() string
	// Agora é o relógio; nil é time.Now.
	Agora func() time.Time
	// AoLote é chamado depois de cada lote do push aplicado (o laço reconta
	// o progresso do envio inicial para a bandeja). nil não faz nada. Roda
	// na goroutine da rodada e não pode bloquear.
	AoLote func()
	// Dicionario é o dicionario.json deste PC, para o aprendido (ADR-019,
	// "Decisões da F8"): a varredura no começo da rodada (Varrer, F8f3), o
	// envio dos marcadores learned junto com os ditados, e o aplicador do
	// pull (Receber, F8f4). nil deixa o aprendido de fora, como na F7: nada é
	// varrido, enviado nem aplicado, o M é 0, e a mudança learned do pull é
	// "não aplicada" (o cursor anda).
	Dicionario DicionarioDoAprendido
}

// Os tetos de uma rodada: o resto fica para a próxima.
const (
	MaxLotesPorRodada   = 50
	MaxPaginasPorRodada = 100
)

// Relatorio é o que uma rodada fez, só em contagens (o log não leva texto,
// id, token nem URL).
type Relatorio struct {
	Enviados  int
	Recebidos int
	Paginas   int
	// Recomecos conta os recomeços (o epoch novo, ou a primeira rodada da
	// instalação); Releituras, os 410 do mesmo epoch (o cursor volta a 0);
	// Reconexoes, o cursor passado a um pareamento novo.
	Recomecos  int
	Releituras int
	Reconexoes int
	// Completados conta os ditados que a rodada pôs na fila para completar o
	// envio inicial (Fila.CompletarEnvio: o ditado novo feito desconectado).
	Completados int
	// Push e Pull contam as decisões por tipo (Tipo*); o Push conta também
	// as do aprendido (DecidirResultadoDoAprendido). O Pull é só do ditado
	// (e, sem o Dicionario, a mudança learned como "não aplicada", como na
	// F7); com o Dicionario, as decisões do pull do aprendido (DecidirPar:
	// aplicado, trocado, convergente, desfeito, pulado, pendente, não
	// aplicado, eco...) vão em PullDoAprendido.
	Push            map[string]int
	Pull            map[string]int
	PullDoAprendido map[string]int
	// NaoAplicados conta, por motivo (dictionary.MotivoDoPar, lista
	// fechada), os pares da nuvem que ficaram "não aplicado neste PC" nesta
	// rodada: no pull (DecidirPar, inclusive o arquivo indisponível) e na
	// tentativa de novo dos pendentes que mudou o espelho (a varredura).
	NaoAplicados map[string]int
	// Recusados conta as recusas por código (lista fechada).
	Recusados map[string]int
	// Varredura conta o que a varredura do aprendido gravou (TipoVarredura*),
	// e Reaplicados, os pendentes que ela tentou de novo e mudaram (os tipos
	// de DecidirPar). O que não mudou nada não conta.
	Varredura   map[string]int
	Reaplicados map[string]int
	// Trocas é o M da varredura (as trocas aprendidas que o clique em
	// "Enviar o histórico" solta); Freio, os pares que sairiam do arquivo
	// quando o freio segurou a varredura (0 sem ele); FreioConferido, a
	// varredura que comparou o arquivo nesta rodada (só então o Freio diz
	// algo, inclusive o 0 do freio solto); SemDicionario, o dicionario.json
	// indisponível na varredura.
	Trocas         int
	Freio          int
	FreioConferido bool
	SemDicionario  bool
}

func novoRelatorio() Relatorio {
	return Relatorio{Push: map[string]int{}, Pull: map[string]int{}, PullDoAprendido: map[string]int{},
		NaoAplicados: map[string]int{}, Recusados: map[string]int{}, Varredura: map[string]int{}, Reaplicados: map[string]int{}}
}

// Teve diz se a rodada fez alguma coisa (a varredura que só contou o M, sem
// gravar, não conta: o PC parado não escreve no banco a cada busca).
func (r Relatorio) Teve() bool {
	return r.Enviados > 0 || r.Recebidos > 0 || r.Recomecos > 0 || r.Releituras > 0 || r.Reconexoes > 0 ||
		r.Completados > 0 || soma(r.Varredura) > 0 || soma(r.Reaplicados) > 0
}

func soma(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// contarVarredura soma ao relatório o que a varredura fez.
func (r *Relatorio) contarVarredura(v ResultadoDaVarredura) {
	juntar(r.Varredura, v.Tipos)
	juntar(r.Reaplicados, v.Reaplicados)
	juntar(r.NaoAplicados, v.NaoAplicados)
	r.Trocas, r.Freio, r.FreioConferido, r.SemDicionario = v.Trocas, v.Freio, v.Conferiu, v.SemArquivo
}

// contarInicio soma ao relatório o que Preparar fez.
func (r *Relatorio) contarInicio(i storage.Inicio) {
	switch i {
	case storage.InicioRecomeco:
		r.Recomecos++
	case storage.InicioReconexao:
		r.Reconexoes++
	}
}

// RecusadosEmTexto é "codigo:n,codigo:n", em ordem, para o log.
func (r Relatorio) RecusadosEmTexto() string { return emTexto(r.Recusados) }

// NaoAplicadosEmTexto é "motivo:n,motivo:n", em ordem, para o log.
func (r Relatorio) NaoAplicadosEmTexto() string { return emTexto(r.NaoAplicados) }

// emTexto é "codigo:n,codigo:n", em ordem.
func emTexto(m map[string]int) string {
	var cs []string
	for c := range m {
		cs = append(cs, c)
	}
	sort.Strings(cs)
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%d", c, m[c])
	}
	return b.String()
}

func (m *Motor) agora() time.Time {
	if m.Agora != nil {
		return m.Agora()
	}
	return time.Now()
}

// Rodada é o push e o pull (e, se o pull reconciliou algum ditado, mais uma
// volta, para ele subir já). Antes, o cursor deste pareamento (Fila.Preparar:
// um pareamento novo da mesma instalação continua do cursor, com as versões
// que o PC guardou), a varredura do aprendido (Varrer, com o Dicionario: as
// mudanças daqui viram marcadores, e o M é contado) e a completação do envio
// inicial (Fila.CompletarEnvio: depois do "Enviar o histórico", o que ficou
// de fora entra na fila; sem nada a enviar, nem ditado nem troca, o aceite é
// gravado). Depois de uma falha que pede para ler de novo, a rodada
// roda outra vez, no máximo uma vez para cada uma (a segunda seguida é
// defeito da nuvem e volta como erro, e o laço espera longo):
//   - o 410 (o PC perdeu lápides) no mesmo epoch: só o cursor volta a 0
//     (Fila.ZerarCursor); as versões continuam valendo, e nada é reenviado;
//   - o epoch novo (um backup restaurado): o recomeço (Fila.Recomecar), com a
//     base vazia.
func (m *Motor) Rodada(ctx context.Context) (Relatorio, error) {
	rel := novoRelatorio()
	est, inicio, err := m.Fila.Preparar(ctx, m.Aparelho, m.PareadoEm, m.agora())
	if err != nil {
		return rel, err
	}
	rel.contarInicio(inicio)
	// As trocas aprendidas a subir (o M da pergunta, ADR-019, "Decisões da
	// F8", D3) vêm da varredura, que vem antes do CompletarEnvio.
	trocas := 0
	if m.Dicionario != nil {
		v, err := m.Varrer(ctx, est.Epoch)
		rel.contarVarredura(v)
		switch {
		case errors.Is(err, storage.ErrPareamentoMudou):
			// O PC desconectou, reconectou ou esqueceu a nuvem no meio: a
			// rodada seguinte lê a credencial de agora.
			return rel, nil
		case errors.Is(err, storage.ErrEpochDiferente):
			// Os pendentes esperam: o push ou o pull dizem o epoch, e o
			// recomeço vem deles. A tabela da varredura valeu, e o M também.
		case err != nil:
			return rel, err
		}
		trocas = v.Trocas
		if v.SemArquivo && trocas == 0 {
			// Sem o arquivo, o M não é conhecido: o aceite não é gravado
			// sozinho (as trocas que estiverem lá subiriam sem pergunta).
			trocas = 1
		}
	}
	if rel.Completados, err = m.Fila.CompletarEnvio(ctx, m.agora(), trocas); err != nil {
		return rel, err
	}
	releu, recomecou, reconciliou := false, false, false
	for {
		antes := rel.Pull[TipoReconciliado]
		err := m.Enviar(ctx, &rel)
		if err == nil {
			err = m.Receber(ctx, &rel)
		}
		switch {
		case err == nil && rel.Pull[TipoReconciliado] > antes && !reconciliou:
			// O pull pôs na fila ditados a reconciliar (D3): eles sobem já,
			// numa volta a mais, e não na rodada seguinte.
			reconciliou = true
		case errors.Is(err, ErrRecomeco) && !releu:
			if err := m.Fila.ZerarCursor(ctx, m.Aparelho); err != nil {
				return rel, err
			}
			releu = true
			rel.Releituras++
		case errors.Is(err, storage.ErrPareamentoMudou):
			// O PC desconectou, reconectou ou esqueceu a nuvem durante a
			// rodada: nada foi aplicado, e a rodada seguinte lê a credencial
			// de agora. Não recomeça (o aparelho desta rodada é o velho).
			return rel, nil
		case errors.Is(err, storage.ErrEpochDiferente) && !recomecou:
			if err := m.Fila.Recomecar(ctx, m.Aparelho, m.PareadoEm, m.agora()); err != nil {
				return rel, err
			}
			recomecou = true
			rel.Recomecos++
		default:
			return rel, err
		}
	}
}

// Enviar sobe a fila em lotes até ela esvaziar (ou até MaxLotesPorRodada).
// Um push com erro para tudo: o lote fica em voo, e nada de pull.
//
// Com o Dicionario, os marcadores learned (Fila.PendentesDoAprendido: os em
// voo primeiro; antes do aceite, só o delete) vão no começo de cada lote,
// antes dos ditados, com a mutação montada do espelho (MutacaoDoAprendido) e
// a resposta decidida por DecidirResultadoDoAprendido, na mesma transação dos
// ditados do lote.
func (m *Motor) Enviar(ctx context.Context, rel *Relatorio) error {
	for lote := 0; lote < MaxLotesPorRodada; lote++ {
		var aprendidos []storage.EstadoDoAprendido
		if m.Dicionario != nil {
			var err error
			if aprendidos, err = m.Fila.PendentesDoAprendido(ctx, MaxMutacoesPorLote); err != nil {
				return err
			}
		}
		itens, err := m.Fila.Pendentes(ctx, MaxMutacoesPorLote-len(aprendidos))
		if err != nil {
			return err
		}
		if len(itens) == 0 && len(aprendidos) == 0 {
			return nil
		}
		// O texto acima do teto da nuvem (uma linha de antes do teto no
		// painel) nem sai: a nuvem o recusaria (campo_invalido), e um texto
		// enorme passaria do corpo do push e travaria a fila inteira.
		var grandes []string
		cabem := itens[:0]
		for _, it := range itens {
			if len(it.Linha.RawText) > storage.MaxTextoBytes || len(it.Linha.FinalText) > storage.MaxTextoBytes {
				grandes = append(grandes, it.Marcador.EntityID)
				continue
			}
			cabem = append(cabem, it)
		}
		if len(grandes) > 0 {
			if err := m.Fila.Recusar(ctx, grandes, CodigoTextoGrande); err != nil {
				return err
			}
			rel.Push[TipoRecusado] += len(grandes)
			rel.Recusados[CodigoTextoGrande] += len(grandes)
		}
		itens = cabem
		if len(itens) == 0 && len(aprendidos) == 0 {
			continue
		}
		muts := make([]Mutation, 0, len(aprendidos)+len(itens))
		for _, e := range aprendidos {
			id := e.Marcador.MutationID
			if id == "" {
				id = m.NovoID()
			}
			mut, err := MutacaoDoAprendido(e, id)
			if err != nil {
				return err
			}
			muts = append(muts, mut)
		}
		for _, it := range itens {
			id := it.Marcador.MutationID
			if id == "" {
				id = m.NovoID()
			}
			mut, err := MutacaoDe(it, id)
			if err != nil {
				return err
			}
			muts = append(muts, mut)
		}
		n := CortarLote(muts)
		na := min(n, len(aprendidos))
		muts, aprendidos, itens = muts[:n], aprendidos[:na], itens[:n-na]

		// Antes da rede, numa transação curta: o id e a versão lida de cada
		// envio que ainda não estava em voo (a do aprendido é a versão local
		// do espelho; o envio a repetir depois de uma reconexão fica com a
		// versão enviada de antes, storage.Fila.MarcarEnvio).
		var envios []storage.Envio
		for i, e := range aprendidos {
			if !e.Marcador.EmVoo() {
				envios = append(envios, storage.Envio{
					EntityType: storage.EntidadeAprendido, EntityID: e.ID,
					MutationID: muts[i].MutationID, Versao: e.VersaoLocal,
				})
			}
		}
		for i, it := range itens {
			if !it.Marcador.EmVoo() {
				envios = append(envios, storage.Envio{
					EntityType: it.Marcador.EntityType, EntityID: it.Marcador.EntityID,
					MutationID: muts[na+i].MutationID, Versao: it.Linha.Version,
				})
			}
		}
		if err := m.Fila.MarcarEnvio(ctx, envios); err != nil {
			return err
		}

		res, err := m.Cliente.Push(ctx, muts)
		if err != nil {
			return err
		}

		porID := make(map[string]MutationResult, n-na)
		ids := make([]string, 0, n-na)
		doAprendido := make(map[string]MutationResult, na)
		idsDoAprendido := make([]string, 0, na)
		for i, r := range res.Results {
			if i < na {
				id := aprendidos[i].ID
				doAprendido[id] = r
				idsDoAprendido = append(idsDoAprendido, id)
				continue
			}
			id := itens[i-na].Marcador.EntityID
			porID[id] = r
			ids = append(ids, id)
		}
		parcial := novoRelatorio()
		err = m.Fila.AplicarComAprendido(ctx, m.Aparelho, res.Epoch, ids, nil, func(e storage.EstadoDoItem) storage.AcaoLocal {
			r := porID[e.ID]
			d := DecidirResultado(e, r)
			parcial.Push[d.Tipo]++
			if d.Tipo == TipoRecusado {
				parcial.Recusados[d.Acao.Erro]++
			}
			return d.Acao
		}, storage.RespostaDoAprendido{
			IDs: idsDoAprendido, Agora: m.agora(),
			Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
				d := DecidirResultadoDoAprendido(e, doAprendido[e.ID])
				parcial.Push[d.Tipo]++
				if d.Tipo == TipoRecusado && d.Acao.Situacao != nil {
					parcial.Recusados[d.Acao.Situacao.Motivo]++
				}
				return d.Acao
			},
		})
		if err != nil {
			return err
		}
		rel.Enviados += n
		juntar(rel.Push, parcial.Push)
		juntar(rel.Recusados, parcial.Recusados)
		if m.AoLote != nil {
			m.AoLote()
		}
	}
	return nil
}

// Receber puxa as páginas depois do cursor até não ter mais (ou até
// MaxPaginasPorRodada). Cada página é aplicada numa transação, com o cursor
// novo junto: um erro de E/S desfaz a página e o cursor não anda; a recusa
// por regra não segura nada.
//
// Com o Dicionario (o aplicador, F8f4), as mudanças learned de cada página
// passam antes pelo dicionario.json (receberAprendido, em receber.go: a
// leitura curta do epoch e do pareamento, o arquivo relido sob a trava, a
// intenção antes da gravação), e o espelho delas entra na transação da
// página, com os ditados e o cursor. O epoch ou o pareamento mudados antes do
// arquivo voltam como erro, sem nada aplicado; o arquivo que não deu deixa os
// pares pendentes, e o cursor anda. Sem o Dicionario, tudo é como na F7.
func (m *Motor) Receber(ctx context.Context, rel *Relatorio) error {
	est, inicio, err := m.Fila.Preparar(ctx, m.Aparelho, m.PareadoEm, m.agora())
	if err != nil {
		return err
	}
	rel.contarInicio(inicio)
	cursor := est.Cursor
	for pagina := 0; pagina < MaxPaginasPorRodada; pagina++ {
		p, err := m.Cliente.Pull(ctx, cursor, LimiteDoPull)
		if err != nil {
			return err
		}
		if p.HasMore && p.LastSeq <= cursor {
			// Um cursor que não anda prenderia o laço: é falha.
			return contrato(CodigoCursor)
		}
		porID := make(map[string]Change, len(p.Changes))
		ids := make([]string, 0, len(p.Changes))
		var doAprendido []Change
		for _, c := range p.Changes {
			if m.Dicionario != nil && c.EntityType == storage.EntidadeAprendido {
				doAprendido = append(doAprendido, c)
				continue
			}
			id := strings.ToLower(c.EntityID)
			porID[id] = c
			ids = append(ids, id)
		}
		novo := p.LastSeq
		parcial := novoRelatorio()
		var aprendido storage.RespostaDoAprendido
		if len(doAprendido) > 0 {
			if aprendido, err = m.receberAprendido(ctx, p.Epoch, doAprendido, &parcial); err != nil {
				return err
			}
		}
		err = m.Fila.AplicarComAprendido(ctx, m.Aparelho, p.Epoch, ids, &novo, func(e storage.EstadoDoItem) storage.AcaoLocal {
			d := DecidirMudanca(e, porID[e.ID])
			parcial.Pull[d.Tipo]++
			return d.Acao
		}, aprendido)
		if err != nil {
			return err
		}
		cursor = novo
		rel.Paginas++
		rel.Recebidos += len(p.Changes)
		juntar(rel.Pull, parcial.Pull)
		juntar(rel.PullDoAprendido, parcial.PullDoAprendido)
		juntar(rel.NaoAplicados, parcial.NaoAplicados)
		if !p.HasMore {
			return nil
		}
	}
	return nil
}

func juntar(dst, src map[string]int) {
	for k, v := range src {
		dst[k] += v
	}
}
