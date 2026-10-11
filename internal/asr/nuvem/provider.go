package nuvem

import (
	"context"
	"fmt"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Provider implementa asr.ASRProvider sobre um provedor de transcrição da
// nuvem, com a chave de quem dita. O valor zero não serve: Provedor é
// obrigatório. Os outros campos têm padrão.
type Provider struct {
	// Cliente fala com a API. Nil usa o valor zero (as APIs oficiais).
	Cliente *Cliente
	// Provedor é OpenAI ou Groq.
	Provedor string
	// Modelo é o modelo de transcrição. Vazio usa ModeloPadrao(Provedor).
	Modelo string
	// Idioma é o idioma pedido quando as ASROptions não trazem outro. Vazio
	// usa IdiomaPadrao.
	Idioma string
	// Chave entrega a chave do provedor, e é chamada a CADA transcrição,
	// como o ia.Polidor.Chave: nenhum campo guarda a chave, e removê-la vale
	// na chamada seguinte. Um erro dela nunca é repassado (poderia trazer
	// segredo): vira MotivoSemChave.
	Chave func(ctx context.Context) (string, error)
}

var _ asr.ASRProvider = (*Provider)(nil)

// ID é o nome do provedor ("openai", "groq"): é o que o histórico guarda em
// asr_provider. Nunca inclui segredos.
func (p *Provider) ID() string { return p.Provedor }

func (p *Provider) modelo() string {
	if p.Modelo != "" {
		return p.Modelo
	}
	return ModeloPadrao(p.Provedor)
}

// Capabilities descreve o que este provider entrega DE FATO.
//
// Confidence é false: o provedor não devolve confiança do texto (o
// no_speech_prob e o avg_logprob medem outra coisa e servem ao filtro de
// silêncio), e o contrato proíbe fabricar uma. Vocabulary é true: a dica vai
// no campo prompt. Timestamps vale só nos modelos que devolvem segmentos
// (o whisper). Formatos lista o áudio comprimido que o provedor aceita.
func (p *Provider) Capabilities() asr.ASRCapabilities {
	return asr.ASRCapabilities{
		Timestamps:   perfilDoModelo(p.modelo()).formato == "verbose_json",
		Vocabulary:   true,
		Acceleration: asr.AccelerationNone,
		Formatos:     Formatos(p.Provedor),
	}
}

// Warmup não faz nada: não há o que preparar, e uma chamada de teste seria
// cobrada.
func (p *Provider) Warmup(ctx context.Context) error { return ctx.Err() }

// Transcribe manda o áudio comprimido ao provedor. O PCM é recusado
// (asr.ErrPCMNaoSuportado): este pacote não codifica áudio.
//
// opts.Vocabulary vai como a dica (prompt) do provedor. opts.Language,
// quando traz um código (pt-BR), vira o idioma ISO 639-1 do pedido (pt).
//
// NUNCA chama audio.Release: o dono do buffer é quem libera (asr.AudioBuffer).
func (p *Provider) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	if err := audio.Validar(); err != nil {
		return asr.ASRResult{}, err
	}
	if !audio.Comprimido.Presente() {
		return asr.ASRResult{}, asr.ErrPCMNaoSuportado
	}
	if !Valido(p.Provedor) {
		return asr.ASRResult{}, fmt.Errorf("asr/nuvem: provedor desconhecido %q", p.Provedor)
	}
	chave, err := p.chave(ctx)
	if err != nil {
		return asr.ASRResult{}, err
	}

	idioma := idiomaISO(opts.Language)
	if idioma == "" {
		idioma = idiomaISO(p.Idioma)
	}
	cli := p.Cliente
	if cli == nil {
		cli = &Cliente{}
	}
	res, err := cli.Transcrever(ctx, Chamada{
		Provedor: p.Provedor,
		Modelo:   p.modelo(),
		Idioma:   idioma,
		Dica:     opts.Vocabulary,
	}, chave, audio.Comprimido)
	if err != nil {
		return asr.ASRResult{}, err
	}
	if opts.Language != "" {
		res.Language = opts.Language
	}
	return res, nil
}

// chave lê a chave a cada chamada. Nada do erro de Chave passa adiante.
func (p *Provider) chave(ctx context.Context) (string, error) {
	if p.Chave == nil {
		return "", novoErro(p.Provedor, MotivoSemChave, "Falta a chave da "+Nome(p.Provedor)+".")
	}
	chave, err := p.Chave(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", novoErro(p.Provedor, MotivoSemChave, "Não consegui ler a chave da "+Nome(p.Provedor)+".")
	}
	return chave, nil
}

// idiomaISO tira o código ISO 639-1 de um idioma BCP-47: "pt-BR" vira "pt".
// O que não parece um código de idioma (de 2 a 3 letras) vira vazio, para o
// pedido cair no padrão em vez de levar lixo ao provedor.
func idiomaISO(s string) string {
	if i := strings.IndexAny(s, "-_"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 || len(s) > 3 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return ""
		}
	}
	return s
}
