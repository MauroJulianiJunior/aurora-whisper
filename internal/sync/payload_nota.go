package syncengine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O payload do push das notas e dos marcadores de nota (ADR-022, "Sync", N7;
// 06-api-contract.md, "Sync"), no molde do ditado (payload.go):
//
//   - criar (server_version nulo, base 0): a nota inteira, na lista fechada da
//     nuvem. O selo, a situação do sync e o corpo_nuvem nunca vão;
//   - mudar (base = server_version): só as chaves dos bits do marcador. O
//     conteúdo é o título e o corpo juntos, e leva o corpo_base (o corpo da
//     versão que a nuvem confirmou por último, storage.Nota.CorpoNuvem) quando
//     o PC o conhece: é com ele que a nuvem faz a mescla da lista de marcar;
//   - apagar de vez (storage.CampoApagado): a operação delete, sem payload e
//     com a base 0 (a lápide não olha a versão).
//
// O texto é lido da linha agora, nunca da fila.

// payloadCriarNota é a nota inteira.
type payloadCriarNota struct {
	Titulo         string   `json:"titulo"`
	Corpo          string   `json:"corpo"`
	Cor            *string  `json:"cor"`
	Fixada         bool     `json:"fixada"`
	Arquivada      bool     `json:"arquivada"`
	Posicao        string   `json:"posicao"`
	Marcadores     []string `json:"marcadores"`
	DitadoDeOrigem *string  `json:"ditado_de_origem,omitempty"`
	CreatedAt      string   `json:"created_at"`
	DeletedAt      *string  `json:"deleted_at,omitempty"`
}

// idsDaNuvem põe os ids em minúsculas (como a nuvem os devolve), e nunca nil.
func idsDaNuvem(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strings.ToLower(id))
	}
	return out
}

// MutacaoDaNota monta a mutação de um marcador de nota, com o mutationID
// dado.
func MutacaoDaNota(it storage.ItemDeNotaNaFila, mutationID string) (Mutation, error) {
	m := Mutation{
		MutationID: strings.ToLower(mutationID),
		EntityType: storage.EntidadeNota,
		EntityID:   strings.ToLower(it.Marcador.EntityID),
		Operation:  OpUpsert,
	}
	if it.Marcador.Campos&storage.CampoApagado != 0 || it.Linha == nil {
		// A lápide: apagar vence, e não olha a base.
		m.Operation = OpDelete
		return m, nil
	}
	n := it.Linha
	var corpo any
	if n.ServerVersion == nil {
		c := payloadCriarNota{
			Titulo: n.Titulo, Corpo: n.Corpo, Cor: n.Cor, Fixada: n.Fixada, Arquivada: n.Arquivada,
			Posicao: n.Posicao, Marcadores: idsDaNuvem(n.Marcadores), CreatedAt: dataDaNuvem(n.CreatedAt),
		}
		if n.DitadoDeOrigem != nil && pareceUUID(*n.DitadoDeOrigem) {
			d := strings.ToLower(*n.DitadoDeOrigem)
			c.DitadoDeOrigem = &d
		}
		if n.DeletedAt != nil {
			d := dataDaNuvem(*n.DeletedAt)
			c.DeletedAt = &d
		}
		corpo = c
	} else {
		m.BaseVersion = *n.ServerVersion
		campos := it.Marcador.Campos
		escolha := it.Marcador.SeloEscolha
		if campos == 0 && escolha == "" {
			// Como no ditado: um marcador sem campo numa nota que a nuvem já
			// tem. O conteúdo e a lixeira com a base certa não mudam nada que
			// a nuvem não tenha.
			campos = storage.CampoNotaConteudo | storage.CampoNotaLixeira
		}
		p := map[string]any{}
		if escolha != "" {
			p["resolver_conflito"] = escolha
			p["conflict_at"] = dataDaNuvem(it.Marcador.SeloEm)
		}
		if campos&storage.CampoNotaConteudo != 0 {
			p["titulo"] = n.Titulo
			p["corpo"] = n.Corpo
			if n.CorpoNuvem != nil {
				p["corpo_base"] = *n.CorpoNuvem
			}
		}
		if campos&storage.CampoNotaLixeira != 0 {
			if n.DeletedAt != nil {
				p["deleted_at"] = dataDaNuvem(*n.DeletedAt)
			} else {
				p["deleted_at"] = nil
			}
		}
		if campos&storage.CampoNotaCor != 0 {
			p["cor"] = n.Cor
		}
		if campos&storage.CampoNotaFixada != 0 {
			p["fixada"] = n.Fixada
		}
		if campos&storage.CampoNotaArquivada != 0 {
			p["arquivada"] = n.Arquivada
		}
		if campos&storage.CampoNotaMarcadores != 0 {
			p["marcadores"] = idsDaNuvem(n.Marcadores)
		}
		if campos&storage.CampoNotaPosicao != 0 {
			p["posicao"] = n.Posicao
		}
		corpo = p
	}
	raw, err := json.Marshal(corpo)
	if err != nil {
		return Mutation{}, fmt.Errorf("sync: montando o payload da nota: %w", err)
	}
	m.Payload = raw
	return m, nil
}

// MutacaoDoMarcadorDeNota monta a mutação de um marcador (rótulo) de nota.
func MutacaoDoMarcadorDeNota(it storage.ItemDeMarcadorNaFila, mutationID string) (Mutation, error) {
	m := Mutation{
		MutationID: strings.ToLower(mutationID),
		EntityType: storage.EntidadeMarcadorDeNota,
		EntityID:   strings.ToLower(it.Marcador.EntityID),
		Operation:  OpUpsert,
	}
	if it.Marcador.Campos&storage.CampoApagado != 0 || it.Linha == nil {
		m.Operation = OpDelete
		return m, nil
	}
	l := it.Linha
	var corpo any
	if l.ServerVersion == nil {
		corpo = map[string]any{"nome": l.Nome, "created_at": dataDaNuvem(l.CreatedAt)}
	} else {
		m.BaseVersion = *l.ServerVersion
		// O nome é o único campo do marcador: um marcador sem bit numa linha
		// que a nuvem tem leva o nome do mesmo jeito.
		corpo = map[string]any{"nome": l.Nome}
	}
	raw, err := json.Marshal(corpo)
	if err != nil {
		return Mutation{}, fmt.Errorf("sync: montando o payload do marcador: %w", err)
	}
	m.Payload = raw
	return m, nil
}
