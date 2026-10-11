package syncengine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O Receber do aprendido (ADR-019, "Decisões da F8", F8f4): o par que chega
// pelo pull passa pelo dicionario.json, que não tem transação comum com o
// SQLite. Com o aplicador (Motor.Dicionario, a mesma interface da
// varredura), cada página do pull separa as mudanças learned das do ditado:
// as learned passam pelo arquivo aqui, e o espelho delas entra na transação
// da página, com os ditados e o cursor. Sem o aplicador (nil), nada daqui
// roda: a mudança learned segue como na F7 (DecidirMudanca a conta como "não
// aplicada", e o cursor anda).
//
// # A ordem ("A ordem", na ADR)
//
//  1. A leitura curta do epoch e do pareamento (conferirAntesDoArquivo, com
//     as regras do Fila.Aplicar) vem ANTES de o arquivo ser tocado. Mudou (um
//     backup restaurado, o esquecer ou a reconexão no meio da rodada): o
//     arquivo não é tocado, nada da página é aplicado, e o Receber devolve o
//     erro (a rodada recomeça, ou para). Ela roda sob a trava do dicionário,
//     depois de o arquivo ser relido e os pares escolhidos, e só quando algum
//     vai ao lote: é o mais perto possível da gravação (como na varredura).
//  2. Sob a trava do dicionário (DicionarioDoAprendido.AplicarPares, a mesma
//     do painel), com o arquivo relido e o espelho lido ali: PlanejarPar, par
//     a par. O par mudado aqui que não subiu (a projeção do arquivo não é a
//     guardada, a versão local passou da base, ou há marcador) é PULADO: nem
//     o arquivo nem a base mudam, e o par da nuvem fica guardado como
//     pendente. O aplicador nunca sobrescreve uma mudança daqui; um Aprender
//     ou um Desfazer do painel entre o pull e o arquivo cai aqui.
//  3. A intenção antes do arquivo (IntencaoDoPar, write-ahead, o gancho
//     antesDeGravar): o espelho grava "aplicando", com a projeção esperada (já
//     com a adaptação deste PC), e o id no "veio da nuvem"; só depois o
//     arquivo é gravado.
//  4. Fora da trava, numa transação (Fila.AplicarComAprendido): o espelho de
//     cada id (DecidirPar), os ditados da página e o cursor, com a conferência
//     do epoch e do pareamento de sempre. Se ela falha (o epoch mudou entre o
//     arquivo e ela, ou uma queda), a linha fica "aplicando", e a varredura
//     seguinte a reconcilia pelo arquivo: confirma, sem envio. O pull da
//     mesma versão (o cursor não andou) converge.
//
// # O arquivo que não deu
//
// O arquivo que o lote não alcança (ausente, inválido, de outro usuário,
// preso na troca de nome, ou outro erro de leitura) e o lote que não termina
// (o dicionário resultante não carrega, ou a gravação falha) NUNCA seguram os
// ditados nem o cursor: os pares ficam pendentes aqui (DecidirPar, "não
// aplicado" com MotivoArquivoIndisponivel, e o par da nuvem guardado), e a
// varredura os aplica quando o arquivo volta. Só o erro do SQLite segura a
// página (a intenção que não foi gravada; a transação da página).

// recepcao é o estado do aprendido de uma página do pull, entre os ganchos
// do lote.
type recepcao struct {
	m     *Motor
	ctx   context.Context
	epoch string
	agora time.Time

	// ids e mudancas: uma mudança por id (a última da página), na ordem em que
	// o id aparece.
	ids      []string
	mudancas map[string]Change
	// planos é o passo 1 de cada id (PlanejarPar).
	planos map[string]PlanoDoPar
	// noLote são os ids que foram ao lote, na ordem dos pares.
	noLote []string

	leu   bool
	falha error
	// intencaoFalhou: o erro do lote veio da gravação da intenção (o SQLite),
	// e não do arquivo.
	intencaoFalhou bool
}

// receberAprendido passa as mudanças learned de uma página pelo aplicador
// (os passos 1 a 3 de "A ordem") e devolve a parte do aprendido da transação
// da página (o passo 4), que conta as decisões em parcial.PullDoAprendido.
// Devolve storage.ErrEpochDiferente ou storage.ErrPareamentoMudou quando a
// leitura curta de antes do arquivo os viu (o arquivo não foi tocado), e o
// erro do SQLite da intenção; o arquivo que não deu não é erro.
func (m *Motor) receberAprendido(ctx context.Context, epoch string, cs []Change, parcial *Relatorio) (storage.RespostaDoAprendido, error) {
	r := &recepcao{m: m, ctx: ctx, epoch: epoch, agora: m.agora(),
		mudancas: make(map[string]Change, len(cs)), planos: make(map[string]PlanoDoPar, len(cs))}
	for _, c := range cs {
		id := strings.ToLower(c.EntityID)
		if _, ok := r.mudancas[id]; !ok {
			r.ids = append(r.ids, id)
		}
		r.mudancas[id] = c
	}

	_, lote, err := m.Dicionario.AplicarPares(r.escolher, r.antesDeGravar)
	resultados := make(map[string]*dictionary.ResultadoDoPar, len(r.noLote))
	switch {
	case r.falha != nil:
		return storage.RespostaDoAprendido{}, r.falha
	case err != nil && r.intencaoFalhou:
		return storage.RespostaDoAprendido{}, err
	case err != nil && !r.leu:
		// O arquivo não foi lido: cada par é planejado sem ele (o eco e o
		// pulado valem do mesmo jeito; o resto fica pendente aqui).
		if err := r.planejarSemArquivo(); err != nil {
			return storage.RespostaDoAprendido{}, err
		}
	case err != nil:
		// O lote não terminou: os pares que iam a ele ficam pendentes
		// (DecidirPar sem resultado), e a linha "aplicando" que a intenção
		// deixou a varredura reconcilia pelo arquivo.
	case len(lote.Resultados) == len(r.noLote):
		for i, id := range r.noLote {
			res := lote.Resultados[i]
			resultados[id] = &res
		}
	}
	return storage.RespostaDoAprendido{
		IDs: r.ids, Agora: r.agora,
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			d := DecidirPar(e, r.planos[e.ID], resultados[e.ID])
			parcial.PullDoAprendido[d.Tipo]++
			if m, ok := motivoDoNaoAplicado(d.Acao); ok {
				parcial.NaoAplicados[m]++
			}
			return d.Acao
		},
	}, nil
}

// escolher são os passos 2 e 1, sob a trava do dicionário, com o arquivo
// relido: o plano de cada id, e a leitura curta do epoch e do pareamento
// antes de devolver os pares do lote.
func (r *recepcao) escolher(s dictionary.Semente) []dictionary.ParDaNuvem {
	r.leu = true
	estados, err := r.estados()
	if err != nil {
		r.falha = err
		return nil
	}
	noArquivo := map[string]*dictionary.ParDoPainel{}
	for _, p := range s.ParesDoPainel() {
		p := p
		noArquivo[p.ID] = &p
	}
	var pares []dictionary.ParDaNuvem
	for _, id := range r.ids {
		plano := PlanejarPar(estados(id), r.mudancas[id], ArquivoDoPar{Projecao: noArquivo[id]})
		r.planos[id] = plano
		if plano.Lote {
			r.noLote = append(r.noLote, id)
			pares = append(pares, plano.Par)
		}
	}
	if len(pares) == 0 {
		return nil
	}
	if err := r.m.conferirAntesDoArquivo(r.ctx, r.epoch); err != nil {
		r.falha, r.noLote = err, nil
		return nil
	}
	return pares
}

// antesDeGravar é o passo 3: a intenção de cada id cujo arquivo muda, antes
// da gravação, fora da transação da página.
func (r *recepcao) antesDeGravar(l dictionary.Lote) error {
	acoes := map[string]storage.AcaoDoAprendido{}
	var ids []string
	for i, id := range r.noLote {
		if i >= len(l.Resultados) {
			break
		}
		if a, ok := IntencaoDoPar(r.planos[id], l.Resultados[i]); ok {
			acoes[id] = a
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	err := r.m.Fila.MudarAprendido(r.ctx, r.agora, ids, func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return acoes[e.ID]
	})
	if err != nil {
		r.intencaoFalhou = true
	}
	return err
}

// planejarSemArquivo é o passo 2 quando o arquivo não foi lido: cada id
// planejado com o arquivo indisponível (pendente aqui), com o espelho lido
// fora da trava (o arquivo não vai ser tocado).
func (r *recepcao) planejarSemArquivo() error {
	estados, err := r.estados()
	if err != nil {
		return err
	}
	for _, id := range r.ids {
		r.planos[id] = PlanejarPar(estados(id), r.mudancas[id], ArquivoDoPar{Indisponivel: true})
	}
	return nil
}

// estados lê o espelho e devolve o estado de um id (o zero, sem linha).
func (r *recepcao) estados() (func(id string) storage.EstadoDoAprendido, error) {
	es, err := r.m.Fila.Espelho(r.ctx)
	if err != nil {
		return nil, err
	}
	porID := make(map[string]storage.EstadoDoAprendido, len(es))
	for _, e := range es {
		porID[e.ID] = e
	}
	return func(id string) storage.EstadoDoAprendido {
		if e, ok := porID[id]; ok {
			return e
		}
		return storage.EstadoDoAprendido{ID: id}
	}, nil
}

// motivoDoNaoAplicado é o motivo da ação que deixa o par "não aplicado neste
// PC" (um dictionary.MotivoDoPar), para as contagens do log.
func motivoDoNaoAplicado(a storage.AcaoDoAprendido) (string, bool) {
	if s := a.Situacao; s != nil && s.Situacao == storage.EspelhoNaoAplicado {
		return s.Motivo, true
	}
	return "", false
}

// conferirAntesDoArquivo é a leitura curta de antes de o aplicador tocar o
// dicionario.json ("A ordem", passo 1), com as regras do Fila.Aplicar: sem o
// estado deste pareamento, storage.ErrPareamentoMudou; com outro epoch,
// storage.ErrEpochDiferente. O epoch ainda não visto (nulo: a primeira página
// de um pareamento, ou a de depois de um recomeço, sem push antes) é adotado,
// como a transação da página o adotaria: sem isso, o primeiro pull de um PC
// recém-conectado nunca aplicaria o aprendido. O caso comum (o mesmo epoch) é
// só leitura (Fila.ConferirPareamento).
func (m *Motor) conferirAntesDoArquivo(ctx context.Context, epoch string) error {
	err := m.Fila.ConferirPareamento(ctx, m.Aparelho, epoch)
	if !errors.Is(err, storage.ErrEpochDiferente) {
		return err
	}
	return m.Fila.Aplicar(ctx, m.Aparelho, epoch, nil, nil, nil)
}
