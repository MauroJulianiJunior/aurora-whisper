package nuvem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ListarModelos pergunta ao provedor, com a chave, quais modelos ela alcança
// e devolve só os de TRANSCRIÇÃO (EhModeloDeTranscricao), do mais novo para o
// mais antigo. Serve para duas coisas ao mesmo tempo: provar que a chave
// funciona e trazer a lista de onde a pessoa escolhe o modelo. Nenhum nome de
// modelo mora no código além dos padrões.
func (c *Cliente) ListarModelos(ctx context.Context, provedor, chave string) ([]string, error) {
	if !Valido(provedor) {
		return nil, fmt.Errorf("asr/nuvem: provedor desconhecido %q", provedor)
	}
	if err := chaveUsavel(provedor, chave); err != nil {
		return nil, err
	}
	timeout := c.TimeoutLista
	if timeout <= 0 {
		timeout = TimeoutListaPadrao
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base(provedor)+"/models", nil)
	if err != nil {
		return nil, novoErro(provedor, MotivoSemRede, fmt.Sprintf(
			"Não consegui falar com a %s.", Nome(provedor)))
	}
	req.Header.Set("Authorization", "Bearer "+chave)
	req.Header.Set("Accept", "application/json")

	resp, err := c.semRedirecionamento().Do(req)
	if err != nil {
		return nil, falhaDeRede(ctx, provedor)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, falhaHTTP(provedor, resp.StatusCode)
	}
	corpo, err := io.ReadAll(io.LimitReader(resp.Body, limiteResposta+1))
	if err != nil {
		return nil, falhaDeRede(ctx, provedor)
	}
	if len(corpo) > limiteResposta {
		return nil, respostaIlegivel(provedor)
	}
	return modelosDeTranscricao(provedor, corpo)
}

// modelosDeTranscricao lê {"data":[{"id","created"}]}, a forma das duas APIs.
// Nenhuma promete ordem: ordena pelo mais novo (created), e pelo nome no
// empate.
func modelosDeTranscricao(provedor string, corpo []byte) ([]string, error) {
	var r struct {
		Data *[]struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(corpo, &r); err != nil || r.Data == nil {
		return nil, respostaIlegivel(provedor)
	}
	dados := *r.Data
	sort.SliceStable(dados, func(i, j int) bool {
		if dados[i].Created != dados[j].Created {
			return dados[i].Created > dados[j].Created
		}
		return dados[i].ID < dados[j].ID
	})
	ids := make([]string, 0, len(dados))
	vistos := make(map[string]bool, len(dados))
	for _, m := range dados {
		if m.ID == "" || vistos[m.ID] || !EhModeloDeTranscricao(m.ID) {
			continue
		}
		vistos[m.ID] = true
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// EhModeloDeTranscricao diz se o id de um modelo da lista do provedor é de
// transcrição: o nome traz "transcribe" ou "whisper" (ADR-021, "O motor").
// Ficam de fora, por não servirem a quem dita em português:
//   - os de diarização ("...-diarize"), que separam quem falou e pedem outro
//     formato de resposta;
//   - os só de inglês (nome terminado em "-en" ou ".en").
func EhModeloDeTranscricao(id string) bool {
	m := strings.ToLower(id)
	if !strings.Contains(m, "transcribe") && !strings.Contains(m, "whisper") {
		return false
	}
	if strings.Contains(m, "diarize") || strings.HasSuffix(m, "-en") || strings.HasSuffix(m, ".en") {
		return false
	}
	return true
}
