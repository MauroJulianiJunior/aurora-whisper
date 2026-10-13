package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Provider implementa asr.ASRProvider sobre um servidor de ASR residente.
type Provider struct {
	cfg   Config
	model string

	// novoProcesso é injetável para teste: a suíte troca por um dublê e
	// exercita subida, morte e reinício sem binário nenhum.
	novoProcesso func(Config) serverProcess
	httpClient   *http.Client

	mu   sync.Mutex // serializa: um servidor, uma requisição por vez
	proc serverProcess
	// end é onde o servidor de pé atende, com o caminho secreto da subida
	// dele (process.go). Zero sem servidor.
	end endereco
}

// New cria o provider. Nada é iniciado aqui — o servidor sobe em Warmup,
// que é onde o contrato de asr.ASRProvider diz que o custo de inicialização
// deve ser pago.
func New(cfg Config, modelo string) *Provider {
	return &Provider{
		cfg:          cfg,
		model:        modelo,
		novoProcesso: newServerProcess,
		httpClient:   &http.Client{},
	}
}

func (p *Provider) ID() string { return "whisper-local" }

// Capabilities descreve o que este provider entrega DE FATO.
//
// Confidence é false porque o whisper-server não devolve confidence no
// corpo de /inference. O contrato de asr.ASRCapabilities é explícito: com
// Confidence false, ASRResult.Confidence vem sempre ausente — nunca um
// valor fabricado. Um número inventado aqui envenenaria a policy do Engine,
// que usa confidence para decidir fallback.
func (p *Provider) Capabilities() asr.ASRCapabilities {
	return asr.ASRCapabilities{
		Streaming:  false,
		Confidence: false,
		Timestamps: false,
		NBest:      false,
		// O whisper-server aceita o campo "prompt" (o "initial prompt" do
		// whisper.cpp) e o usa para enviesar o decodificador — ver pedir().
		Vocabulary: true,
		// Formatos fica vazio de propósito: só PCM. Ver Transcribe.
	}
}

// Warmup sobe o servidor e espera ele aceitar requisição.
func (p *Provider) Warmup(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.garantirDePe(ctx)
}

// garantirDePe sobe o servidor se ele não estiver de pé. Chamador tem de
// segurar p.mu.
func (p *Provider) garantirDePe(ctx context.Context) error {
	if p.proc != nil && p.proc.Alive() {
		return nil
	}
	if p.proc != nil {
		_ = p.proc.Stop() // morto ou travado: não deixa resto para trás
		p.proc = nil
		p.end = endereco{}
	}

	var ultimo error
	for tentativa := 0; tentativa < maxReinicios; tentativa++ {
		if tentativa > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoffPara(tentativa - 1)):
			}
		}
		proc := p.novoProcesso(p.cfg)
		end, err := proc.Start(ctx)
		if err != nil {
			ultimo = err
			_ = proc.Stop()
			continue
		}
		p.proc, p.end = proc, end
		return nil
	}
	return fmt.Errorf("%w: %d tentativas falharam: %v", ErrServerDead, maxReinicios, ultimo)
}

// Transcribe manda o áudio ao servidor residente e devolve a transcrição.
//
// NUNCA chama audio.Release: o dono do buffer é quem libera, e apenas uma
// vez, depois de todos os providers (asr.AudioBuffer, TEST-010).
//
// Só entende PCM: o whisper.cpp local recebe um WAV, e este provider não
// decodifica webm nem opus (ADR-021, decisão C). Áudio comprimido é recusado
// com asr.ErrComprimidoNaoSuportado ANTES de qualquer outra coisa, inclusive
// de subir o servidor: quem manda comprimido para o PC errou de provider, e
// gastar a subida do modelo para dizer isso seria o pior jeito de descobrir.
func (p *Provider) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	if audio.Comprimido.Presente() {
		return asr.ASRResult{}, asr.ErrComprimidoNaoSuportado
	}
	wav, err := encodeWAV(audio.PCM, audio.SampleRateHz)
	if err != nil {
		return asr.ASRResult{}, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.garantirDePe(ctx); err != nil {
		return asr.ASRResult{}, err
	}

	// A janela do encoder e os ajustes do decodificador dependem da
	// duração do áudio (JanelaPorDuracao).
	campos := camposDeDecodificacao(p.cfg, duracaoDoAudio(audio.PCM, audio.SampleRateHz))

	inicio := time.Now()
	texto, err := p.pedir(ctx, wav, opts.Vocabulary, campos)
	if err != nil {
		// O servidor pode ter morrido entre o Alive() e esta chamada. Uma
		// segunda tentativa, depois de recolocá-lo de pé, distingue "morreu"
		// de "não sabe transcrever" — e é a diferença entre um ditado que
		// se recupera sozinho e um que a pessoa perde.
		if p.proc == nil || !p.proc.Alive() {
			if reErr := p.garantirDePe(ctx); reErr != nil {
				return asr.ASRResult{}, reErr
			}
			texto, err = p.pedir(ctx, wav, opts.Vocabulary, campos)
		}
		if err != nil {
			return asr.ASRResult{}, err
		}
	}

	return asr.ASRResult{
		Text:           texto,
		Language:       opts.Language,
		Model:          p.model,
		ProcessingTime: time.Since(inicio),
		// Confidence fica ausente de propósito: ver Capabilities.
	}, nil
}

// pedir faz o POST multipart com o WAV em memória. Chamador segura p.mu.
//
// vocabulario, quando não vazio, vai no campo "prompt" que o
// whisper-server expõe (o "initial prompt" do whisper.cpp): enviesa o
// decodificador para esses termos. Ver asr.ASROptions.Vocabulary para a
// medição que justifica, e o custo de latência que ela cobra.
//
// campos são os de camposDeDecodificacao (audio_ctx e os ajustes do
// decodificador), escritos depois do vocabulário, na ordem dada.
func (p *Provider) pedir(ctx context.Context, wav []byte, vocabulario string, campos []campoForm) (string, error) {
	var corpo bytes.Buffer
	w := multipart.NewWriter(&corpo)
	parte, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("asr/local: montando requisição: %w", err)
	}
	if _, err := parte.Write(wav); err != nil {
		return "", fmt.Errorf("asr/local: escrevendo áudio na requisição: %w", err)
	}
	// O campo vai DEPOIS do arquivo de propósito: o whisper-server lê a
	// requisição inteira antes de transcrever, então a ordem não muda o
	// comportamento dele — mas escrever o arquivo primeiro mantém o
	// caminho sem vocabulário byte a byte igual ao que já estava em
	// produção e foi medido.
	if vocabulario != "" {
		if err := w.WriteField("prompt", vocabulario); err != nil {
			return "", fmt.Errorf("asr/local: escrevendo vocabulário na requisição: %w", err)
		}
	}
	for _, c := range campos {
		if err := w.WriteField(c.nome, c.valor); err != nil {
			return "", fmt.Errorf("asr/local: escrevendo %s na requisição: %w", c.nome, err)
		}
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("asr/local: fechando requisição: %w", err)
	}

	// A URL leva o caminho secreto do servidor (process.go). Nenhum erro
	// daqui a carrega: o do net/http sai sem o *url.Error (semURL), e o corpo
	// de uma resposta de erro sai sem o caminho (semCaminho).
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.end.url("/inference"), &corpo)
	if err != nil {
		return "", fmt.Errorf("asr/local: criando requisição: %w", semURL(err))
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("asr/local: falando com o servidor em %s (POST /inference): %w", p.end.base, semURL(err))
	}
	defer resp.Body.Close()

	// Ler o corpo INTEIRO e propagar erro de leitura. Um corpo truncado
	// devolvido como sucesso viraria transcrição pela metade sem nenhum
	// sinal — exatamente o defeito que a medição de transporte do Sprint 3
	// encontrou do lado servidor (docs/benchmarks/transporte-2026-09-17.md).
	dados, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("asr/local: lendo resposta: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Sem o caminho ANTES de cortar: cortado no meio, um pedaço dele
		// escaparia da troca.
		return "", fmt.Errorf("asr/local: servidor respondeu %s: %s", resp.Status, primeirosBytes(p.end.semCaminho(string(dados))))
	}
	return parseResposta(dados)
}

func primeirosBytes(s string) string {
	const max = 200
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// semURL tira o *url.Error que o net/http põe em volta dos erros dele: ele
// traz a URL inteira, e a URL leva o caminho secreto do servidor. Fica a
// causa (a conexão recusada ou cortada, o prazo, o cancelamento), que é o que
// interessa a quem lê o log, e errors.Is continua achando context.Canceled e
// context.DeadlineExceeded nela.
func semURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Close encerra o servidor residente. O Agent chama no encerramento; sem
// isso o processo continua de pé segurando a memória do modelo.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.proc == nil {
		return nil
	}
	err := p.proc.Stop()
	p.proc, p.end = nil, endereco{}
	return err
}
