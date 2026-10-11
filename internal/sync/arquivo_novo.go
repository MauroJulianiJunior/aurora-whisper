package syncengine

import (
	"context"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O dicionario.json que não é mais o que o espelho conhece (a revisão da F8g,
// 08-out-2026): o Agent o recriou vazio na subida (garantirDicionario, no
// cmd/agent: o arquivo sumiu entre duas execuções), ou outro arquivo, de
// outro usuário, foi posto no lugar. Enquanto o Agent roda, os dois são o
// arquivo indisponível (ausente, de outro usuário) da varredura, que nunca
// vira delete. Na subida seguinte, porém, o arquivo novo é válido: sem este
// passo, a varredura o compararia com o espelho do arquivo antigo, e cada par
// dele viraria "saiu do arquivo" (o delete, que tira o par do celular e dos
// outros PCs, e da cópia na nuvem), abaixo do freio.
//
// # O que muda no espelho (DecidirArquivoNovo)
//
// Cada id que o arquivo antigo tinha (a projeção guardada, ou a intenção de
// um lote sobre ele) sai da projeção SEM virar mudança daqui: a versão local
// não sobe, e nenhum marcador nasce. O par que a nuvem tem vivo (a base, não
// desfeita) fica guardado como o par da nuvem, com a versão da base, "não
// aplicado neste PC" (arquivo_indisponivel): a varredura seguinte o aplica no
// arquivo novo, como aplica o par que o pull trouxe com o arquivo
// indisponível (PlanejarPendente). O texto é o par da nuvem da base, quando a
// linha o guarda (o de uma etiqueta), ou a projeção guardada, que é o texto da
// base (a base não guarda outra cópia). O que já esperava (o par da nuvem mais
// novo, o não aplicado) continua pendente, e vale do mesmo jeito. O par que a
// nuvem não tem (nunca subiu, ou ela o desfez) só sai da projeção: ele estava
// só no arquivo que sumiu.
//
// # O que fica de fora
//
// A mudança daqui que a nuvem não confirmou (a versão local acima da base) e o
// id com marcador (um envio que espera, ou em voo, cuja projeção é a do
// envio): a varredura os trata como sempre. A mudança daqui pode ser o
// Desfazer do painel que a varredura ainda não viu, e então o delete é de
// verdade; mas também pode ser um Aprender, ou a troca de termo, que ainda
// não subiu (o segurado antes do clique), e esse vira delete do par que a
// nuvem tem, porque o espelho não distingue os dois. É o risco que fica: o
// arquivo que some com uma mudança daqui por subir.

// DecidirArquivoNovo é o que o espelho faz com um id quando o
// dicionario.json passou a ser outro arquivo (o comentário acima). Pura; a
// ação vazia é "nada a fazer".
func DecidirArquivoNovo(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
	aplicando := e.Situacao == storage.EspelhoAplicando
	if !e.TemLinha || e.MudouAqui() || e.Marcador != nil || e.Projecao == nil && !aplicando {
		return storage.AcaoDoAprendido{}
	}
	a := storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}}
	if aplicando {
		// A intenção de um lote sobre o arquivo antigo sai; o par da nuvem
		// dela, com a versão, continua guardado.
		a.Situacao = &storage.NovaSituacao{}
	}
	g := e.Nuvem
	naoAplicado := &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado,
		Motivo: string(dictionary.MotivoArquivoIndisponivel)}
	switch {
	case g != nil && g.Versao != nil && (e.ServerVersion == nil || *e.ServerVersion < *g.Versao),
		e.Situacao == storage.EspelhoNaoAplicado:
		// Já pendente (um par da nuvem mais novo, ou o não aplicado): a
		// varredura o tenta no arquivo novo, como está.
	case e.ServerVersion == nil || e.BaseDesfeita:
		// A nuvem não tem o par vivo: ele estava só no arquivo que sumiu.
	case g != nil && g.Versao != nil && *g.Versao == *e.ServerVersion:
		// O par da nuvem da base já está guardado (o texto de uma etiqueta):
		// é ele que volta.
		a.Situacao = naoAplicado
	case e.Projecao != nil:
		// O texto da base é a projeção guardada.
		p, v := e.Projecao, *e.ServerVersion
		a.Nuvem = &storage.ParGuardado{Versao: &v, Par: dictionary.ParDaNuvem{
			Variante: p.Variante, Termo: p.Termo, Restrito: p.Restrito, Dominio: p.Dominio,
		}}
		a.Situacao = naoAplicado
	}
	return a
}

// SoltarOArquivoAntigo aplica DecidirArquivoNovo a todo o espelho, numa
// transação, e devolve quantos ids mudaram. É do Agent, na subida, antes de o
// laço rodar, quando o dicionario.json foi recriado ou é de outro usuário.
// Sem nada a mudar, não escreve. De novo sobre o mesmo espelho, não faz nada.
func SoltarOArquivoAntigo(ctx context.Context, fila *storage.Fila, agora time.Time) (int, error) {
	es, err := fila.Espelho(ctx)
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, e := range es {
		if soOQueMuda(e, DecidirArquivoNovo(e)) != (storage.AcaoDoAprendido{}) {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	err = fila.MudarAprendido(ctx, agora, ids, func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return soOQueMuda(e, DecidirArquivoNovo(e))
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
