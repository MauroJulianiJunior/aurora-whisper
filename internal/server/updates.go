package server

import "net/http"

// UpdateInfo é a resposta (estática, configurada no boot) de GET
// /updates/windows/latest (docs/ai-spec/06-api-contract.md, "Updates";
// AC-039). A checagem real de versão/assinatura contra um release
// publicado é trabalho de TASK-010/S10 (packaging); aqui só validamos o
// contrato HTTP com um provedor injetável.
type UpdateInfo struct {
	UpdateAvailable bool
	Version         string
	ReleaseNotesURL string
	DownloadURL     string
	SHA256          string
	Signature       string
}

// UpdateProvider resolve a resposta de "há atualização?" a partir da
// versão corrente do cliente. Nesta fase, StaticUpdateProvider cobre o
// contrato; um provedor real (consultando GitHub público/privado,
// conforme docs/ai-spec/06-api-contract.md: "Isso abstrai GitHub
// público/privado") entra depois sem mudar a rota.
type UpdateProvider interface {
	Latest(current, arch string) UpdateInfo
}

// StaticUpdateProvider sempre devolve o mesmo UpdateInfo, ignorando
// current/arch. Usado nesta fase e em testes.
type StaticUpdateProvider struct {
	Info UpdateInfo
}

func (p StaticUpdateProvider) Latest(_, _ string) UpdateInfo { return p.Info }

type updateInfoDTO struct {
	UpdateAvailable bool   `json:"update_available"`
	Version         string `json:"version,omitempty"`
	ReleaseNotesURL string `json:"release_notes_url,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	Signature       string `json:"signature,omitempty"`
}

// handleUpdatesLatest implementa GET /updates/windows/latest. Não exige
// sessão — um instalador checando atualização ainda não tem, necessariamente,
// uma sessão válida.
func handleUpdatesLatest(provider UpdateProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		info := provider.Latest(q.Get("current"), q.Get("arch"))
		writeJSON(w, http.StatusOK, updateInfoDTO{
			UpdateAvailable: info.UpdateAvailable, Version: info.Version,
			ReleaseNotesURL: info.ReleaseNotesURL, DownloadURL: info.DownloadURL,
			SHA256: info.SHA256, Signature: info.Signature,
		})
	}
}
