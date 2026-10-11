// Package model localiza e verifica o modelo de ASR que o Agent usa
// (TASK-007, "model manager; checksum; metadata").
//
// # Embarcado ao lado do executável, não dentro dele
//
// Decisão de Mauro em 18-set-2026: o instalador embarca o `q4_0` de 139 MB
// já pronto, em vez de baixar o f16 de 465 MB e quantizar na instalação.
// Mais simples, instalador menor, e não depende de rede na primeira vez que
// a pessoa abre o programa.
//
// "Embarcar" aqui é **entregar o arquivo ao lado do executável**, e não
// `go:embed` dentro do binário. A diferença importa: embutir 139 MB no
// .exe faria cada build carregar isso, e o runtime manteria uma cópia do
// modelo em RAM ANTES de o whisper-server abrir a dele — memória dobrada
// para nada, num produto cujo alvo de working set é ~1 GB.
//
// # Por que falhar alto é o ponto deste pacote
//
// Um modelo faltando ou corrompido não pode virar transcrição ruim. Este
// projeto já pagou esse preço duas vezes: um defeito de layout gravou
// padrão de bits no lugar de áudio por dois sprints, e a "correção" que
// aceitou PCM de 32 bits transformou uma recusa barulhenta em corrupção
// muda. Nos dois casos o sintoma foi qualidade ruim, e a causa era outra.
//
// Por isso aqui não há fallback silencioso, nem "tenta assim mesmo": se o
// arquivo não estiver lá, ou o conteúdo não bater com o checksum esperado,
// o Agent recusa começar e diz exatamente o que está errado.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Erros que o Agent distingue para dar mensagem útil a quem usa.
var (
	// ErrAusente: o arquivo não está no lugar esperado. Instalação
	// incompleta, ou alguém apagou.
	ErrAusente = errors.New("asr/model: arquivo do modelo não encontrado")

	// ErrCorrompido: o arquivo existe mas o conteúdo não é o esperado.
	// Download interrompido, disco com problema, ou modelo trocado por
	// outro sem atualizar a expectativa.
	ErrCorrompido = errors.New("asr/model: checksum do modelo não confere")
)

// Descriptor é a identidade do modelo, e existe para além da verificação:
// o AC-043 exige que o histórico mostre provider e modelo de cada ditado.
// Sem um descritor explícito, essa informação viraria uma string solta
// montada em algum lugar do caminho.
type Descriptor struct {
	// Nome identifica o modelo no histórico e no relatório (ex.:
	// "whisper-small-q4_0").
	Nome string
	// Arquivo é o nome do arquivo esperado ao lado do executável.
	Arquivo string
	// SHA256 é o checksum esperado, em hexadecimal minúsculo.
	SHA256 string
	// Bytes é o tamanho esperado. Conferido antes do checksum porque ler
	// 139 MB para descobrir que o arquivo tem metade disso é desperdício,
	// e porque tamanho errado dá uma mensagem mais direta.
	Bytes int64
}

// SmallQ4_0 é o modelo escolhido no Sprint 3
// (docs/benchmarks/asr-decisao-2026-09-18.md).
//
// O checksum não é decoração: este arquivo NÃO existe pronto para baixar em
// lugar nenhum. Foi gerado com whisper-quantize.exe a partir do
// ggml-small.bin, e quantizar de novo noutra máquina pode não produzir
// bytes idênticos. O checksum fixa exatamente o artefato que foi medido —
// sem ele, "small q4_0" seria um nome, não uma identidade.
var SmallQ4_0 = Descriptor{
	Nome:    "whisper-small-q4_0",
	Arquivo: "ggml-small-q4_0.bin",
	SHA256:  "f6099c17a4305c6d173ef90ab6f15150dcbb85332b7831313bf36a378d7c5a85",
	Bytes:   145458015,
}

// Resolve devolve o caminho do modelo dentro de dir e confirma que ele
// serve, verificando tamanho e checksum.
//
// Devolve erro embrulhando ErrAusente ou ErrCorrompido para quem chama
// poder distinguir "instale de novo" de "o arquivo chegou quebrado".
func Resolve(dir string, d Descriptor) (string, error) {
	caminho := filepath.Join(dir, d.Arquivo)

	info, err := os.Stat(caminho)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrAusente, caminho)
		}
		return "", fmt.Errorf("asr/model: lendo %s: %w", caminho, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %s é um diretório", ErrAusente, caminho)
	}
	if d.Bytes > 0 && info.Size() != d.Bytes {
		return "", fmt.Errorf("%w: %s tem %d bytes, esperado %d (download incompleto?)",
			ErrCorrompido, caminho, info.Size(), d.Bytes)
	}

	soma, err := somaSHA256(caminho)
	if err != nil {
		return "", err
	}
	if soma != d.SHA256 {
		return "", fmt.Errorf("%w: %s tem sha256 %s, esperado %s",
			ErrCorrompido, caminho, soma, d.SHA256)
	}
	return caminho, nil
}

// somaSHA256 lê o arquivo em blocos. 139 MB não cabem confortavelmente em
// memória só para calcular um hash, e io.Copy já faz o streaming.
func somaSHA256(caminho string) (string, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return "", fmt.Errorf("asr/model: abrindo %s: %w", caminho, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("asr/model: lendo %s: %w", caminho, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
