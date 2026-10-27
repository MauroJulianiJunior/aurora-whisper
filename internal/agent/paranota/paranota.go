// Package paranota é o destino do gatilho "ditar para nota" no PC (ADR-022,
// "Acrescentar por voz sem abrir a nota", fase N9): a qual nota o ditado vai,
// como o texto entra nela e o que a pílula diz.
//
// A regra é pura e mora em internal/notas desde a N8 (o botão "Ditar para nota"
// do celular usa a mesma, na nuvem): Escolher, Acrescentar e o texto da
// pílula. Aqui fica Gravar, o único passo com banco, que fala com o
// repositório das notas por uma interface estreita: nada de teclado, clipboard
// nem janela.
//
// # Privacidade
//
// O texto ditado e o título da nota são conteúdo da pessoa: nada deste pacote
// os escreve em log. Quem chama loga só números (quantas linhas, nota nova ou
// não).
package paranota

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/notas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TextoNotaNova: nenhum título casou, e nasceu uma nota com o ditado. É o
// Label de overlay.FaseNotaNova; a falha ("Não salvou na nota") é só do overlay
// (overlay.FaseNotaNaoSalvou).
const TextoNotaNova = notas.TextoNotaNova

// TextoDaPilula é o que a pílula mostra depois de acrescentar n linhas à nota
// de título titulo: "Lista de compras +3" (notas.TextoDaPilula).
func TextoDaPilula(titulo string, n int) string { return notas.TextoDaPilula(titulo, n) }

// Escolher acha a nota de destino do ditado entre as notas (notas.Escolher,
// sobre o id, o título e as datas de cada uma).
func Escolher(candidatas []storage.Nota, ditado string) (indice int, resto string) {
	c := make([]notas.Candidata, len(candidatas))
	for i, n := range candidatas {
		c[i] = notas.Candidata{ID: n.ID, Titulo: n.Titulo, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
	}
	return notas.Escolher(c, ditado)
}

// Acrescentar põe o resto do ditado no corpo (notas.Acrescentar).
func Acrescentar(corpo, resto string) (novo string, n int) { return notas.Acrescentar(corpo, resto) }

// ---------------------------------------------------------------------
// Gravar
// ---------------------------------------------------------------------

// Repositorio é o que Gravar precisa do repositório das notas
// (storage.NotaRepository o cumpre).
type Repositorio interface {
	Listar(ctx context.Context, f storage.FiltroDeNotas) ([]storage.Nota, error)
	Atualizar(ctx context.Context, userID, id string, baseVersion int64, m storage.MudancaDaNota) (storage.Nota, error)
	Criar(ctx context.Context, n storage.NovaNota) (storage.Nota, error)
}

// Resultado é para onde o ditado foi.
type Resultado struct {
	// NotaID é a nota que recebeu o texto (a nova, ou a de destino).
	NotaID string
	// Nova: nenhum título casou, e o ditado virou uma nota nova.
	Nova bool
	// Titulo é o da nota de destino ("" na nova). Vai para a pílula, nunca
	// para o log.
	Titulo string
	// Linhas é quantas linhas entraram (0 na nova: ela inteira é o ditado).
	Linhas int
}

// Pilula é o texto da pílula para o resultado.
func (r Resultado) Pilula() string {
	if r.Nova {
		return TextoNotaNova
	}
	return TextoDaPilula(r.Titulo, r.Linhas)
}

// tentativas é quantas vezes Gravar tenta de novo quando a nota de destino
// mudou entre a leitura e a gravação (o painel salvou no meio).
const tentativas = 3

// ErrNadaParaGravar: o ditado está vazio.
var ErrNadaParaGravar = errors.New("paranota: ditado vazio")

// Gravar leva o ditado à nota: acha o destino pelo título entre as notas
// ATIVAS (nem arquivadas nem na lixeira; ver Escolher) e acrescenta o resto,
// ou cria uma nota nova com o ditado inteiro, sem título, com DitadoDeOrigem =
// ditadoID (como o "Virar nota": a nota diz "veio de um ditado").
//
// A gravação na nota de destino é com a concorrência otimista do repositório:
// se o painel salvou a nota entre a leitura e a gravação, Gravar lê de novo e
// tenta outra vez (até tentativas). Erro de qualquer tipo volta para quem
// chama, que deixa o ditado no histórico.
//
// Nada vai para a fila do sync (as notas ainda não sincronizam, N7).
func Gravar(ctx context.Context, repo Repositorio, userID, ditadoID, texto string) (Resultado, error) {
	if strings.TrimSpace(texto) == "" {
		return Resultado{}, ErrNadaParaGravar
	}
	var ultimo error
	for range tentativas {
		ativas, err := repo.Listar(ctx, storage.FiltroDeNotas{UserID: userID, Vista: storage.VistaAtivas})
		if err != nil {
			return Resultado{}, fmt.Errorf("paranota: lendo as notas: %w", err)
		}
		i, resto := Escolher(ativas, texto)
		if i < 0 {
			n, err := repo.Criar(ctx, storage.NovaNota{UserID: userID, Corpo: texto, DitadoDeOrigem: ditadoID})
			if err != nil {
				return Resultado{}, fmt.Errorf("paranota: criando a nota: %w", err)
			}
			return Resultado{NotaID: n.ID, Nova: true}, nil
		}
		alvo := ativas[i]
		corpo, linhas := Acrescentar(alvo.Corpo, resto)
		if linhas == 0 {
			// Escolher só devolve destino com resto; resto sem nada que vire
			// linha não acontece, mas se acontecer não é sucesso calado.
			return Resultado{}, fmt.Errorf("paranota: nada a acrescentar")
		}
		_, err = repo.Atualizar(ctx, userID, alvo.ID, alvo.Version, storage.MudancaDaNota{Corpo: &corpo})
		if errors.Is(err, storage.ErrVersionConflict) {
			ultimo = err
			continue
		}
		if err != nil {
			return Resultado{}, fmt.Errorf("paranota: gravando na nota: %w", err)
		}
		return Resultado{NotaID: alvo.ID, Titulo: alvo.Titulo, Linhas: linhas}, nil
	}
	return Resultado{}, fmt.Errorf("paranota: a nota mudou %d vezes no meio: %w", tentativas, ultimo)
}
