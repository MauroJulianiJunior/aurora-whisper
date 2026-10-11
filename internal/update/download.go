package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// Erros de rejeição de download — THR-010 (update malicioso). Um download
// rejeitado por qualquer um destes nunca fica no disco marcado como
// válido: o arquivo temporário é removido antes de o erro retornar.
var (
	ErrEmptyHash         = errors.New("update: sha256 vazio na resposta do servidor")
	ErrInvalidHashFormat = errors.New("update: sha256 mal formado (precisa ser 64 dígitos hexadecimais)")
	ErrHashMismatch      = errors.New("update: sha256 do arquivo baixado não confere com o anunciado")
	ErrSizeMismatch      = errors.New("update: tamanho do arquivo baixado não confere com o anunciado")
)

// VerifiedUpdate é um pacote de update já baixado e com hash conferido —
// StateVerified. Ainda NÃO foi instalado: instalação exige confirmação
// explícita do usuário (AC-039, ver install.go).
type VerifiedUpdate struct {
	Version  string
	FilePath string
	SHA256   string // sempre em minúsculas.
}

// Downloader baixa e verifica o pacote de update anunciado por um
// UpdateInfo.
type Downloader struct {
	// Client é o http.Client usado no download. Se nil, usa
	// http.DefaultClient.
	Client *http.Client
}

func (d Downloader) httpClient() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

// normalizeHash valida que h é um SHA-256 em hex (64 dígitos) e o devolve
// em minúsculas, para comparação case-insensitive ("hex em
// maiúsculas/minúsculas" é um caso de teste explícito). Hash vazio ou mal
// formado é rejeitado ANTES de qualquer requisição de rede — evita gastar
// banda/tempo baixando um arquivo que já não poderia ser aceito.
func normalizeHash(h string) (string, error) {
	if h == "" {
		return "", ErrEmptyHash
	}
	if len(h) != sha256.Size*2 {
		return "", ErrInvalidHashFormat
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", ErrInvalidHashFormat
	}
	lower := make([]byte, len(h))
	for i := 0; i < len(h); i++ {
		c := h[i]
		if c >= 'A' && c <= 'F' {
			c += 'a' - 'A'
		}
		lower[i] = c
	}
	return string(lower), nil
}

// checkDownloadSize compara o Content-Length declarado pelo servidor
// (-1 se desconhecido/chunked, e nesse caso não há o que checar) com o
// número de bytes efetivamente gravados. É defesa em profundidade: na
// prática o cliente HTTP da stdlib já corta a leitura em Content-Length
// bytes e erra em EOF prematuro, então este caminho normalmente nem é
// alcançado com written != declared — mas mantém a checagem explícita
// para não depender apenas desse comportamento implícito do transport.
func checkDownloadSize(declaredContentLength, written int64) error {
	if declaredContentLength < 0 {
		return nil
	}
	if written != declaredContentLength {
		return fmt.Errorf("%w: anunciado %d bytes, recebido %d", ErrSizeMismatch, declaredContentLength, written)
	}
	return nil
}

// DownloadAndVerify baixa info.DownloadURL para um arquivo temporário
// dentro de destDir, calcula o SHA-256 do conteúdo recebido e compara com
// info.SHA256. Qualquer divergência — hash vazio, mal formado, diferente
// do calculado, ou tamanho do corpo diferente do Content-Length anunciado
// — apaga o arquivo temporário antes de devolver erro: o arquivo rejeitado
// nunca fica acessível como se fosse válido (THR-010).
func (d Downloader) DownloadAndVerify(ctx context.Context, info UpdateInfo, destDir string) (VerifiedUpdate, error) {
	wantHash, err := normalizeHash(info.SHA256)
	if err != nil {
		return VerifiedUpdate{}, err
	}

	u, err := url.Parse(info.DownloadURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return VerifiedUpdate{}, fmt.Errorf("update: download_url inválida %q", info.DownloadURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return VerifiedUpdate{}, fmt.Errorf("update: download_url com esquema não suportado %q", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return VerifiedUpdate{}, fmt.Errorf("update: falha ao montar requisição de download: %w", err)
	}
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return VerifiedUpdate{}, fmt.Errorf("update: falha ao baixar update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return VerifiedUpdate{}, fmt.Errorf("update: download respondeu status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(destDir, "aurora-update-*.tmp")
	if err != nil {
		return VerifiedUpdate{}, fmt.Errorf("update: falha ao criar arquivo temporário: %w", err)
	}
	tmpPath := tmp.Name()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), resp.Body)
	closeErr := tmp.Close()

	if copyErr != nil {
		os.Remove(tmpPath)
		return VerifiedUpdate{}, fmt.Errorf("update: falha ao gravar download: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return VerifiedUpdate{}, fmt.Errorf("update: falha ao finalizar arquivo temporário: %w", closeErr)
	}

	if err := checkDownloadSize(resp.ContentLength, written); err != nil {
		os.Remove(tmpPath)
		return VerifiedUpdate{}, err
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if gotHash != wantHash {
		os.Remove(tmpPath)
		return VerifiedUpdate{}, fmt.Errorf("%w: esperado %s, calculado %s", ErrHashMismatch, wantHash, gotHash)
	}

	return VerifiedUpdate{Version: info.Version, FilePath: tmpPath, SHA256: gotHash}, nil
}
