package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// UpdateInfo é a resposta de GET /updates/windows/latest
// (docs/ai-spec/06-api-contract.md, "Updates"). Contém só dados públicos do
// release (versão, URLs, hash) — nunca uma credencial de provider
// (AC-040).
type UpdateInfo struct {
	Available       bool
	Version         string
	ReleaseNotesURL string
	DownloadURL     string
	SHA256          string
	Signature       string
}

// UpdateProvider resolve "há uma versão mais nova?" a partir da versão
// atual do cliente (docs/ai-spec/18-tool-contract.md, "UpdateProvider").
//
//	type UpdateProvider interface {
//	    Latest(ctx context.Context, currentVersion, arch string) (UpdateInfo, error)
//	}
//
// A implementação real (HTTPUpdateProvider) fala apenas com o endpoint
// cloud — nunca diretamente com GitHub ou outro provider; é o backend quem
// abstrai se o release é público ou privado (06-api-contract.md, "Isso
// abstrai GitHub público/privado").
type UpdateProvider interface {
	Latest(ctx context.Context, currentVersion, arch string) (UpdateInfo, error)
}

// HTTPUpdateProvider implementa UpdateProvider contra
// GET /updates/windows/latest?current=...&arch=.... Não carrega nenhuma
// credencial de provider (AC-040): a requisição não leva nenhum header de
// autenticação de terceiro, só os parâmetros públicos current/arch.
type HTTPUpdateProvider struct {
	// BaseURL é a raiz da API cloud, ex.:
	// "https://exemplo.com/api/v1". Não é segredo — é
	// configuração local de endpoint, análoga à de qualquer outro cliente
	// deste repositório.
	BaseURL string
	// Client é o http.Client usado na requisição. Se nil, usa
	// http.DefaultClient; o chamador controla timeout via ctx.
	Client *http.Client
}

func (p HTTPUpdateProvider) httpClient() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

// updateInfoDTO espelha exatamente o JSON de
// docs/ai-spec/06-api-contract.md, "Updates".
type updateInfoDTO struct {
	UpdateAvailable bool   `json:"update_available"`
	Version         string `json:"version"`
	ReleaseNotesURL string `json:"release_notes_url"`
	DownloadURL     string `json:"download_url"`
	SHA256          string `json:"sha256"`
	Signature       string `json:"signature"`
}

// Latest implementa UpdateProvider. Qualquer resposta que não seja um 200
// com JSON válido vira erro — "falha limpa, sem instalar", nunca um
// UpdateInfo parcialmente preenchido tratado como válido.
func (p HTTPUpdateProvider) Latest(ctx context.Context, currentVersion, arch string) (UpdateInfo, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return UpdateInfo{}, fmt.Errorf("update: HTTPUpdateProvider.BaseURL vazio")
	}

	u, err := url.Parse(strings.TrimRight(p.BaseURL, "/") + "/updates/windows/latest")
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("update: BaseURL inválida: %w", err)
	}
	q := u.Query()
	q.Set("current", currentVersion)
	q.Set("arch", arch)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("update: falha ao montar requisição: %w", err)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("update: falha ao consultar endpoint de update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("update: endpoint de update respondeu status %d", resp.StatusCode)
	}

	var dto updateInfoDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		return UpdateInfo{}, fmt.Errorf("update: resposta JSON inválida: %w", err)
	}

	return UpdateInfo{
		Available:       dto.UpdateAvailable,
		Version:         dto.Version,
		ReleaseNotesURL: dto.ReleaseNotesURL,
		DownloadURL:     dto.DownloadURL,
		SHA256:          dto.SHA256,
		Signature:       dto.Signature,
	}, nil
}
