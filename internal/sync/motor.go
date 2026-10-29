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
	// Imagens são as imagens das notas no disco deste PC (N10b;
	// motor_imagens.go): o PUT, o GET e o arquivo que baixou. nil deixa as
	// imagens de fora: nada sobe nem baixa, e a mudança note_image do pull é
	// "não aplicada" (o cursor anda).
	Imagens ImagensDoPC
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
	// NotasCompletadas conta as notas e os marcadores de nota que a rodada pôs
	// na fila no envio inicial das notas (Fila.CompletarNotas, N7).
	NotasCompletadas int
	// ImagensCompletadas conta as imagens das notas que a rodada pôs na fila
	// no envio inicial delas (Fila.CompletarImagens, N10b).
	ImagensCompletadas int
	// Imagens conta, por tipo (TipoImagem*), o que a rodada fez com as imagens
	// das notas: o pull, o envio e o download.
	Imagens map[string]int
	// CapacidadeNova: a rodada viu pela primeira vez que a nuvem sincroniza
	// notas (N7), e o cursor voltou a 0 para ler tudo de novo uma vez.
	CapacidadeNova bool
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
		NaoAplicados: map[string]int{}, Recusados: map[string]int{}, Varredura: map[string]int{}, Reaplicados: map[string]int{},
		Imagens: map[string]int{}}
}

// Teve diz se a rodada fez alguma coisa (a varredura que só contou o M, sem
// gravar, não conta: o PC parado não escreve no banco a cada busca).
func (r Relatorio) Teve() bool {
	return r.Enviados > 0 || r.Recebidos > 0 || r.Recomecos > 0 || r.Releituras > 0 || r.Reconexoes > 0 ||
		r.Completados > 0 || r.NotasCompletadas > 0 || r.ImagensCompletadas > 0 || r.CapacidadeNova ||
		soma(r.Varredura) > 0 || soma(r.Reaplicados) > 0 || soma(r.Imagens) > 0
}

// AplicouDaNuvem conta o que a rodada gravou no histórico deste PC e veio da
// nuvem, ou seja, o que a lista do painel pode mostrar diferente depois dela
// (ADR-021, S; é o que faz subir o contador de Laco.LerMudancas):
//   - o pull que trouxe um ditado novo ou mudado (a edição, a lixeira, o
//     restaurar feitos no celular ou em outro PC) ou uma lápide que apagou a
//     linha daqui;
//   - o push que a nuvem respondeu com a lápide (apagar vence) ou com o estado
//     que venceu um conflito (o texto da nuvem no lugar do daqui, com o selo).
//
// É uma conta por cima: o conflito em que o ditado mudou durante o voo e nada
// foi gravado também entra, porque reler a lista a mais custa pouco, e deixar
// de reler não. Só contagens: nunca texto, id ou versão.
func (r Relatorio) AplicouDaNuvem() int {
	return r.Pull[TipoRecebido] + r.Pull[TipoApagado] + r.Push[TipoApagado] + r.Push[TipoConflito] +
		// As imagens das notas (N10b): a que chegou (a caminho), a que baixou
		// (o arquivo está aqui) e a que saiu: o painel relê a nota.
		r.Imagens[TipoImagemRecebida] + r.Imagens[TipoImagemApagada] + r.Imagens[TipoImagemBaixada] +
		r.Imagens[TipoImagemSumiuNaNuvem]
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
	// O envio inicial das notas e dos marcadores (N7): depois do aceite e com
	// a nuvem que sabe de notas, o que ainda não subiu entra na fila.
	if rel.NotasCompletadas, err = m.Fila.CompletarNotas(ctx, m.agora()); err != nil {
		return rel, err
	}
	// E as imagens das notas (N10b), sob o mesmo aceite e com a capacidade
	// delas: as anexadas antes desta versão, ou desconectado.
	if m.Imagens != nil {
		if rel.ImagensCompletadas, err = m.Fila.CompletarImagens(ctx, m.agora()); err != nil {
			return rel, err
		}
	}
	releu, recomecou, reconciliou, capacidade := false, false, false, false
	for {
		antes := rel.Pull[TipoReconciliado]
		err := m.Enviar(ctx, &rel)
		if err == nil {
			// As imagens sobem depois do push (a nota criada no lote já tem a
			// versão da nuvem) e antes do pull.
			err = m.EnviarImagens(ctx, &rel)
		}
		if err == nil {
			err = m.Receber(ctx, &rel)
		}
		switch {
		case err == nil && rel.Pull[TipoReconciliado] > antes && !reconciliou:
			// O pull pôs na fila ditados a reconciliar (D3): eles sobem já,
			// numa volta a mais, e não na rodada seguinte.
			reconciliou = true
		case errors.Is(err, errCapacidadeNova) && !capacidade:
			// A nuvem passou a sincronizar notas (N7): o cursor volta a 0 e a
			// rodada lê tudo de novo uma vez, porque um binário de antes da N7
			// pode ter passado pelas notas e pelo virou_nota sem os gravar. As
			// versões continuam valendo (o ditado volta como eco), e o envio
			// inicial das notas entra antes da volta seguinte.
			if err := m.Fila.ZerarCursor(ctx, m.Aparelho); err != nil {
				return rel, err
			}
			n, err := m.Fila.CompletarNotas(ctx, m.agora())
			if err != nil {
				return rel, err
			}
			rel.NotasCompletadas += n
			if m.Imagens != nil {
				if n, err = m.Fila.CompletarImagens(ctx, m.agora()); err != nil {
					return rel, err
				}
				rel.ImagensCompletadas += n
			}
			capacidade = true
			rel.CapacidadeNova = true
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
			if err == nil {
				// Depois do pull, as imagens que vieram da nuvem baixam
				// (N10b). Uma que não baixa espera a vez dela, sem falhar a
				// rodada.
				err = m.BaixarImagens(ctx, &rel)
			}
			return rel, err
		}
	}
}

// errCapacidadeNova: o pull viu pela primeira vez que a nuvem sincroniza notas
// (Fila.VerCapacidade). Nada da página foi aplicado; a rodada zera o cursor e
// lê de novo (Rodada).
var errCapacidadeNova = errors.New("sync: a nuvem passou a sincronizar notas")

// Enviar sobe a fila em lotes até ela esvaziar (ou até MaxLotesPorRodada).
// Um push com erro para tudo: o lote fica em voo, e nada de pull.
//
// Com o Dicionario, os marcadores learned (Fila.PendentesDoAprendido: os em
// voo primeiro; antes do aceite, só o delete) vão no começo de cada lote,
// antes dos ditados, com a mutação montada do espelho (MutacaoDoAprendido) e
// a resposta decidida por DecidirResultadoDoAprendido, na mesma transação dos
// ditados do lote.
//
// Com a nuvem que sabe de notas (N7), os marcadores de nota e as notas vêm
// logo depois do aprendido e antes dos ditados (MutacaoDoMarcadorDeNota,
// MutacaoDaNota), com as respostas decididas por DecidirResultadoDoMarcadorDeNota
// e DecidirResultadoDaNota, na mesma transação (storage.Fila.AplicarTudo).
func (m *Motor) Enviar(ctx context.Context, rel *Relatorio) error {
	// A nuvem que sabe de notas (N7): sem ela, nem nota nem marcador de nota
	// sobem (a fila nem os devolve), e o virou_nota do ditado fica de fora.
	comNotas, err := m.Fila.TemCapacidadeDeNotas(ctx)
	if err != nil {
		return err
	}
	for lote := 0; lote < MaxLotesPorRodada; lote++ {
		var aprendidos []storage.EstadoDoAprendido
		if m.Dicionario != nil {
			var err error
			if aprendidos, err = m.Fila.PendentesDoAprendido(ctx, MaxMutacoesPorLote); err != nil {
				return err
			}
		}
		// Os marcadores de nota antes das notas (a nota que cita um marcador
		// novo chega à nuvem depois dele), e os dois antes dos ditados.
		var marcadores []storage.ItemDeMarcadorNaFila
		var notas []storage.ItemDeNotaNaFila
		if comNotas {
			var err error
			if marcadores, err = m.Fila.PendentesDeMarcadoresDeNota(ctx, MaxMutacoesPorLote-len(aprendidos)); err != nil {
				return err
			}
			if notas, err = m.Fila.PendentesDeNotas(ctx, MaxMutacoesPorLote-len(aprendidos)-len(marcadores)); err != nil {
				return err
			}
		}
		itens, err := m.Fila.Pendentes(ctx, MaxMutacoesPorLote-len(aprendidos)-len(marcadores)-len(notas))
		if err != nil {
			return err
		}
		if len(itens) == 0 && len(aprendidos) == 0 && len(marcadores) == 0 && len(notas) == 0 {
			return nil
		}
		// O texto acima do teto da nuvem (uma linha de antes do teto no
		// painel) nem sai: a nuvem o recusaria (campo_invalido), e um texto
		// enorme passaria do corpo do push e travaria a fila inteira. (A nota
		// não passa do teto: o storage recusa antes de gravar.)
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
		if len(itens) == 0 && len(aprendidos) == 0 && len(marcadores) == 0 && len(notas) == 0 {
			continue
		}
		muts := make([]Mutation, 0, len(aprendidos)+len(marcadores)+len(notas)+len(itens))
		idDoEnvio := func(emVoo string) string {
			if emVoo != "" {
				return emVoo
			}
			return m.NovoID()
		}
		for _, e := range aprendidos {
			mut, err := MutacaoDoAprendido(e, idDoEnvio(e.Marcador.MutationID))
			if err != nil {
				return err
			}
			muts = append(muts, mut)
		}
		for _, it := range marcadores {
			mut, err := MutacaoDoMarcadorDeNota(it, idDoEnvio(it.Marcador.MutationID))
			if err != nil {
				return err
			}
			muts = append(muts, mut)
		}
		for _, it := range notas {
			mut, err := MutacaoDaNota(it, idDoEnvio(it.Marcador.MutationID))
			if err != nil {
				return err
			}
			muts = append(muts, mut)
		}
		for _, it := range itens {
			mut, err := MutacaoDoDitado(it, idDoEnvio(it.Marcador.MutationID), comNotas)
			if err != nil {
				return err
			}
			muts = append(muts, mut)
		}
		// O corte vale na ordem do lote: o aprendido, os marcadores de nota,
		// as notas, e os ditados no fim.
		n := CortarLote(muts)
		na := min(n, len(aprendidos))
		nm := min(n-na, len(marcadores))
		nn := min(n-na-nm, len(notas))
		base := na + nm + nn
		muts, aprendidos, marcadores, notas, itens = muts[:n], aprendidos[:na], marcadores[:nm], notas[:nn], itens[:n-base]

		// Antes da rede, numa transação curta: o id e a versão lida de cada
		// envio que ainda não estava em voo (a do aprendido é a versão local
		// do espelho; o envio a repetir depois de uma reconexão fica com a
		// versão enviada de antes, storage.Fila.MarcarEnvio). A lápide de nota
		// não tem linha: a versão é 0.
		var envios []storage.Envio
		for i, e := range aprendidos {
			if !e.Marcador.EmVoo() {
				envios = append(envios, storage.Envio{
					EntityType: storage.EntidadeAprendido, EntityID: e.ID,
					MutationID: muts[i].MutationID, Versao: e.VersaoLocal,
				})
			}
		}
		for i, it := range marcadores {
			if !it.Marcador.EmVoo() {
				var v int64
				if it.Linha != nil {
					v = it.Linha.Version
				}
				envios = append(envios, storage.Envio{
					EntityType: storage.EntidadeMarcadorDeNota, EntityID: it.Marcador.EntityID,
					MutationID: muts[na+i].MutationID, Versao: v,
				})
			}
		}
		for i, it := range notas {
			if !it.Marcador.EmVoo() {
				var v int64
				if it.Linha != nil {
					v = it.Linha.Version
				}
				envios = append(envios, storage.Envio{
					EntityType: storage.EntidadeNota, EntityID: it.Marcador.EntityID,
					MutationID: muts[na+nm+i].MutationID, Versao: v,
				})
			}
		}
		for i, it := range itens {
			if !it.Marcador.EmVoo() {
				envios = append(envios, storage.Envio{
					EntityType: it.Marcador.EntityType, EntityID: it.Marcador.EntityID,
					MutationID: muts[base+i].MutationID, Versao: it.Linha.Version,
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

		porID := make(map[string]MutationResult, len(itens))
		ids := make([]string, 0, len(itens))
		doAprendido := make(map[string]MutationResult, na)
		idsDoAprendido := make([]string, 0, na)
		doMarcador := make(map[string]MutationResult, nm)
		idsDosMarcadores := make([]string, 0, nm)
		daNota := make(map[string]MutationResult, nn)
		idsDasNotas := make([]string, 0, nn)
		for i, r := range res.Results {
			switch {
			case i < na:
				id := aprendidos[i].ID
				doAprendido[id] = r
				idsDoAprendido = append(idsDoAprendido, id)
			case i < na+nm:
				id := marcadores[i-na].Marcador.EntityID
				doMarcador[id] = r
				idsDosMarcadores = append(idsDosMarcadores, id)
			case i < base:
				id := notas[i-na-nm].Marcador.EntityID
				daNota[id] = r
				idsDasNotas = append(idsDasNotas, id)
			default:
				id := itens[i-base].Marcador.EntityID
				porID[id] = r
				ids = append(ids, id)
			}
		}
		parcial := novoRelatorio()
		err = m.Fila.AplicarTudo(ctx, m.Aparelho, res.Epoch, ids, nil, func(e storage.EstadoDoItem) storage.AcaoLocal {
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
		}, storage.RespostaDasNotas{
			Marcadores: idsDosMarcadores,
			DecidirMarcador: func(e storage.EstadoDoMarcadorDeNota) storage.AcaoDoMarcadorDeNota {
				d := DecidirResultadoDoMarcadorDeNota(e, doMarcador[e.ID])
				parcial.Push[d.Tipo]++
				if d.Tipo == TipoRecusado {
					parcial.Recusados[codigoDaRecusa(d.Acao.Erro)]++
				}
				return d.Acao
			},
			Notas: idsDasNotas,
			DecidirNota: func(e storage.EstadoDaNota) storage.AcaoDaNota {
				d := DecidirResultadoDaNota(e, daNota[e.ID])
				parcial.Push[d.Tipo]++
				if d.Tipo == TipoRecusado {
					parcial.Recusados[codigoDaRecusa(d.Acao.Erro)]++
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
		if parcial.Push[TipoAdiado] > 0 {
			// A nuvem recusou o tipo (uma nuvem de antes da N7): as notas e os
			// marcadores ficam na fila, e o resto desta rodada sobe sem eles.
			// Sem isso, o mesmo lote voltaria até MaxLotesPorRodada.
			comNotas = false
		}
	}
	return nil
}

// codigoDaRecusa é o código para a contagem das recusas: a lápide recusada
// não grava código (a linha já saiu), e conta como resposta desconhecida.
func codigoDaRecusa(c string) string {
	if c == "" {
		return CodigoRespostaDesconhecida
	}
	return c
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
		if pagina == 0 {
			// A capacidade da nuvem (N7), conferida na primeira página de cada
			// pull: a nuvem que passou a saber de notas faz a rodada ler tudo
			// de novo (errCapacidadeNova), antes de aplicar esta página; a que
			// deixou de saber (voltou a uma versão antiga) para o envio delas.
			primeira, err := m.Fila.VerCapacidade(ctx, p.TemNotas(), m.agora())
			if err != nil {
				return err
			}
			// A das imagens das notas (N10b), do mesmo jeito: na primeira
			// vez, a rodada lê tudo de novo, e as notas voltam com a lista
			// imagens.
			primeiraDasImagens := false
			if m.Imagens != nil {
				if primeiraDasImagens, err = m.Fila.VerCapacidadeDasImagens(ctx, p.TemImagens(), m.agora()); err != nil {
					return err
				}
			}
			if primeira || primeiraDasImagens {
				return errCapacidadeNova
			}
		}
		var imagens []storage.ImagemNoPull
		var daImagem map[string]ImagemDoPull
		if m.Imagens != nil && p.TemImagens() {
			imagens, daImagem = imagensDaPagina(p.Changes)
		}
		parcialDoPull := map[string]int{}
		porID := make(map[string]Change, len(p.Changes))
		ids := make([]string, 0, len(p.Changes))
		var doAprendido []Change
		daNota := map[string]Change{}
		doMarcador := map[string]Change{}
		var idsDasNotas, idsDosMarcadores []string
		for _, c := range p.Changes {
			if m.Dicionario != nil && c.EntityType == storage.EntidadeAprendido {
				doAprendido = append(doAprendido, c)
				continue
			}
			id := strings.ToLower(c.EntityID)
			switch c.EntityType {
			case storage.EntidadeNota:
				daNota[id] = c
				idsDasNotas = append(idsDasNotas, id)
				continue
			case storage.EntidadeMarcadorDeNota:
				doMarcador[id] = c
				idsDosMarcadores = append(idsDosMarcadores, id)
				continue
			case storage.EntidadeImagemDaNota:
				if daImagem == nil {
					// Sem as imagens (o motor sem elas, ou a nuvem sem a
					// capacidade): não aplicada, e o cursor anda.
					parcialDoPull[TipoImagemNaoAplicada]++
				}
				continue
			}
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
		// Os marcadores de nota são aplicados antes das notas da mesma página
		// (storage.Fila.AplicarTudo): a nota que cita um marcador da página o
		// acha pronto, e o que ainda não chegou vira um "marcador que espera".
		err = m.Fila.AplicarTudo(ctx, m.Aparelho, p.Epoch, ids, &novo, func(e storage.EstadoDoItem) storage.AcaoLocal {
			d := DecidirMudanca(e, porID[e.ID])
			parcial.Pull[d.Tipo]++
			return d.Acao
		}, aprendido, storage.RespostaDasNotas{
			Marcadores: idsDosMarcadores,
			DecidirMarcador: func(e storage.EstadoDoMarcadorDeNota) storage.AcaoDoMarcadorDeNota {
				d := DecidirMudancaDoMarcadorDeNota(e, doMarcador[e.ID])
				parcial.Pull[d.Tipo]++
				return d.Acao
			},
			Notas: idsDasNotas,
			DecidirNota: func(e storage.EstadoDaNota) storage.AcaoDaNota {
				d := DecidirMudancaDaNota(e, daNota[e.ID])
				parcial.Pull[d.Tipo]++
				return d.Acao
			},
			Imagens: imagens,
			DecidirImagem: func(e storage.EstadoDaImagem) storage.AcaoDaImagem {
				d := DecidirMudancaDaImagem(e, daImagem[e.ID])
				parcial.Imagens[d.Tipo]++
				return d.Acao
			},
		})
		if err != nil {
			return err
		}
		juntar(parcial.Imagens, parcialDoPull)
		if m.Imagens != nil && (parcial.Imagens[TipoImagemApagada] > 0 || parcial.Pull[TipoApagado] > 0) {
			// A lápide da imagem (ou a da nota, que leva as imagens dela)
			// tirou linhas: o arquivo sai já, e não só na rodada do expurgo.
			// Melhor esforço: o que ficar, a varredura tira depois.
			_, _ = m.Imagens.TirarArquivosSemLinha(ctx)
		}
		cursor = novo
		rel.Paginas++
		rel.Recebidos += len(p.Changes)
		juntar(rel.Pull, parcial.Pull)
		juntar(rel.PullDoAprendido, parcial.PullDoAprendido)
		juntar(rel.NaoAplicados, parcial.NaoAplicados)
		juntar(rel.Imagens, parcial.Imagens)
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
