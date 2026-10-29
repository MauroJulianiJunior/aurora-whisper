package syncengine

import (
	"context"
	"errors"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As imagens das notas na rodada (ADR-022, "Decisões da N10b"). Duas fases, fora
// de qualquer transação, que nunca seguram o resto do sync:
//
//   - EnviarImagens, depois do push (a nota criada no lote já tem a versão da
//     nuvem) e antes do pull: o PUT de cada imagem a criar cuja nota a nuvem já
//     tem, e o DELETE de cada imagem tirada da nota;
//   - BaixarImagens, depois do pull: o GET de cada imagem que veio da nuvem e
//     espera o arquivo.
//
// Uma imagem que não sobe ou não baixa espera a vez dela (EsperaDasImagens, por
// imagem) e não falha a rodada: um arquivo estragado, ou uma recusa da nuvem, não
// prende o push nem o pull de mais nada. Só o 401 (o token caiu) e o contexto
// cancelado voltam como erro; a rede que cai e o 429 só param a fase, e a rodada
// seguinte continua de onde parou.

// ImagensDoPC é o que o motor usa das imagens no disco: o *storage.
// SQLiteNotaRepository (a pasta das imagens e a trava dela).
type ImagensDoPC interface {
	// LerImagem lê os bytes da imagem, conferidos contra a linha.
	LerImagem(ctx context.Context, userID, notaID, id string) (storage.ImagemDaNota, []byte, error)
	// GuardarBaixada grava o arquivo da imagem que veio da nuvem e a põe
	// normal (os bytes conferidos contra a linha).
	GuardarBaixada(ctx context.Context, id string, jpeg []byte) error
	// TirarArquivosSemLinha apaga o arquivo de imagem que nenhuma linha cita.
	TirarArquivosSemLinha(ctx context.Context) (int, error)
}

// Os tetos de uma rodada das imagens: o resto fica para a próxima.
const (
	MaxImagensEnviadasPorRodada = 20
	MaxImagensBaixadasPorRodada = 20
)

// clienteDasImagens é o lado das imagens do cliente da rodada, ou nil (o
// SyncClient sem ele, ou o motor sem as ImagensDoPC).
func (m *Motor) clienteDasImagens() ClienteDeImagens {
	if m.Imagens == nil {
		return nil
	}
	c, _ := m.Cliente.(ClienteDeImagens)
	return c
}

// paraAFase diz se o erro de um pedido de imagem para a fase sem falhar a
// rodada (a rede, o prazo, o 429, o redirecionamento) e devolve o erro que a
// rodada leva (o 401 e o contexto cancelado).
func paraAFase(err error) (parar bool, daRodada error) {
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, ErrNaoAutorizado), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return true, err
	}
	return true, nil
}

// EnviarImagens sobe o que está na fila das imagens (Fila.PendentesDeImagens):
// o PUT de cada imagem a criar (na_nuvem gravado antes da rede) e o DELETE de
// cada lápide, até MaxImagensEnviadasPorRodada, e aplica cada resposta.
func (m *Motor) EnviarImagens(ctx context.Context, rel *Relatorio) error {
	cli := m.clienteDasImagens()
	if cli == nil {
		return nil
	}
	agora := m.agora()
	itens, err := m.Fila.PendentesDeImagens(ctx, MaxImagensEnviadasPorRodada, agora, EsperaDaImagem)
	if err != nil {
		return err
	}
	for i, it := range itens {
		var resp RespostaDaImagem
		if it.Lapide {
			resp, err = cli.TirarImagem(ctx, it.NotaID, it.ID)
		} else {
			_, jpeg, lerr := m.Imagens.LerImagem(ctx, m.Fila.Dono(), it.NotaID, it.ID)
			if lerr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// O arquivo sumiu ou não confere (a varredura da subida tira
				// a linha sem arquivo): espera, sem rede.
				rel.Imagens[TipoImagemSemArquivo]++
				if err := m.Fila.AplicarEnvioDaImagem(ctx, it, storage.AcaoDaImagemNoEnvio{Esperar: true,
					Erro: CodigoImagemEstragada}, m.agora()); err != nil {
					return err
				}
				continue
			}
			marcou, merr := m.Fila.MarcarEnvioDaImagem(ctx, it.ID)
			if merr != nil {
				return merr
			}
			if !marcou {
				// Tirada da nota depois de a fila ser lida: nada sobe.
				rel.Imagens[TipoImagemTiradaAntes]++
				continue
			}
			resp, err = cli.SubirImagem(ctx, it.NotaID, it.ID, jpeg, it.Linha.SHA256, it.Linha.CreatedAt)
		}
		if parar, daRodada := paraAFase(err); parar {
			rel.Imagens[TipoImagemAdiada] += len(itens) - i
			return daRodada
		}
		d := DecidirEnvioDaImagem(it, resp)
		rel.Imagens[d.Tipo]++
		if err := m.Fila.AplicarEnvioDaImagem(ctx, it, d.Acao, m.agora()); err != nil {
			return err
		}
	}
	return nil
}

// BaixarImagens baixa as imagens que vieram da nuvem e esperam o arquivo
// (Fila.ImagensABaixar), até MaxImagensBaixadasPorRodada: o GET, e o arquivo
// gravado e conferido (ImagensDoPC.GuardarBaixada) ou a decisão do que deu
// errado (DecidirBaixaDaImagem).
func (m *Motor) BaixarImagens(ctx context.Context, rel *Relatorio) error {
	cli := m.clienteDasImagens()
	if cli == nil {
		return nil
	}
	lista, err := m.Fila.ImagensABaixar(ctx, MaxImagensBaixadasPorRodada, m.agora(), EsperaDaImagem)
	if err != nil {
		return err
	}
	for i, im := range lista {
		jpeg, resp, err := cli.BaixarImagem(ctx, im.NotaID, im.ID)
		if parar, daRodada := paraAFase(err); parar {
			rel.Imagens[TipoImagemAdiada] += len(lista) - i
			return daRodada
		}
		if resp.Status == 200 && jpeg != nil {
			switch gerr := m.Imagens.GuardarBaixada(ctx, im.ID, jpeg); {
			case gerr == nil:
				rel.Imagens[TipoImagemBaixada]++
				continue
			case errors.Is(gerr, storage.ErrNotFound):
				// Tirada aqui no meio, ou já baixada: nada a fazer.
				continue
			case ctx.Err() != nil:
				return ctx.Err()
			case !errors.Is(gerr, storage.ErrImagemEstragada):
				// O arquivo não se gravou (o disco cheio, a pasta sem
				// permissão): a imagem espera a vez dela, e a rodada segue.
				rel.Imagens[TipoImagemEsperandoBaixa]++
				if err := m.Fila.AplicarBaixaDaImagem(ctx, im.ID, storage.AcaoDaImagemNaBaixa{Esperar: true,
					Erro: CodigoImagemNaoGravada}, m.agora()); err != nil {
					return err
				}
				continue
			}
		}
		d := DecidirBaixaDaImagem(resp)
		rel.Imagens[d.Tipo]++
		if err := m.Fila.AplicarBaixaDaImagem(ctx, im.ID, d.Acao, m.agora()); err != nil {
			return err
		}
	}
	return nil
}

// imagensDaPagina separa, de uma página do pull, as imagens: as mudanças
// note_image e as da lista imagens de cada nota (a que já veio como mudança
// não entra de novo). Devolve a lista para a Fila e as decisões por id.
func imagensDaPagina(changes []Change) ([]storage.ImagemNoPull, map[string]ImagemDoPull) {
	var lista []storage.ImagemNoPull
	porID := map[string]ImagemDoPull{}
	for _, c := range changes {
		if c.EntityType != storage.EntidadeImagemDaNota {
			continue
		}
		id := strings.ToLower(c.EntityID)
		if _, ja := porID[id]; ja {
			continue
		}
		switch {
		case c.Operation == OpTombstone:
			porID[id] = ImagemDoPull{Tombstone: true}
			lista = append(lista, storage.ImagemNoPull{ID: id, Tombstone: true})
		case c.PayloadImagem != nil:
			meta := *c.PayloadImagem
			porID[id] = ImagemDoPull{Meta: &meta}
			lista = append(lista, storage.ImagemNoPull{ID: id, NotaID: strings.ToLower(meta.NoteID)})
		default:
			porID[id] = ImagemDoPull{}
			lista = append(lista, storage.ImagemNoPull{ID: id})
		}
	}
	for _, c := range changes {
		if c.EntityType != storage.EntidadeNota || c.Operation != OpUpsert || c.PayloadNota == nil {
			continue
		}
		nota := strings.ToLower(c.EntityID)
		for _, im := range c.PayloadNota.Imagens {
			id := strings.ToLower(im.ID)
			if _, ja := porID[id]; ja || id == "" {
				continue
			}
			meta := im
			meta.NoteID = nota
			porID[id] = ImagemDoPull{Meta: &meta}
			lista = append(lista, storage.ImagemNoPull{ID: id, NotaID: nota})
		}
	}
	return lista, porID
}
