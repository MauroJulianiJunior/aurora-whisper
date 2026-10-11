package audio

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"testing"
)

// syntheticSpeechF32 gera um sinal de fala sintética determinístico — soma
// de harmônicos com F0 variando devagar ao longo do tempo, sob um
// envelope de amplitude que imita sílabas — amostrado diretamente a
// sampleRate. É "verdade conhecida": nunca passou pela corrupção nem por
// nenhuma gravação real, então dá para comparar amostra a amostra com o
// que volta de RecoverBitPatternFloat32 depois de corromper e recuperar.
func syntheticSpeechF32(sampleRate int, seconds float64) []float32 {
	n := int(float64(sampleRate) * seconds)
	out := make([]float32, n)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		f0 := 100 + 30*math.Sin(2*math.Pi*0.3*t) // F0 variando entre ~70 e ~130Hz
		phase += 2 * math.Pi * f0 / float64(sampleRate)
		var s float64
		for h := 1; h <= 12; h++ {
			s += math.Sin(phase*float64(h)) / float64(h*h)
		}
		env := 0.5 * (1 - math.Cos(2*math.Pi*math.Mod(t, 0.6)/0.6)) // "sílabas" de 0.6s
		out[i] = float32(0.3 * s * env)
	}
	return out
}

// corruptForward aplica, amostra a amostra, EXATAMENTE a corrupção que o
// defeito (já corrigido) de layout da WAVEFORMATEXTENSIBLE introduziu —
// ver o comentário de pacote em recover.go: os 4 bytes de cada float32
// são reinterpretados como int32 com sinal e normalizados por 2^31, e o
// resultado então convertido para int16 pelo MESMO floatToInt16
// (resampler.go, não exportado, mas reaproveitado aqui por estar no mesmo
// pacote) que o resampler real usa.
func corruptForward(clean []float32) []int16 {
	out := make([]int16, len(clean))
	for i, f := range clean {
		bits := math.Float32bits(f)
		x := float32(int32(bits)) / 2147483648
		out[i] = floatToInt16(x)
	}
	return out
}

func toFloat64Slice(samples []float32) []float64 {
	out := make([]float64, len(samples))
	for i, v := range samples {
		out[i] = float64(v)
	}
	return out
}

// readInt16WAVForTest lê o corpo de amostras int16 de um WAV canônico de
// 44 bytes de cabeçalho — exatamente o que dataset.WriteWAV escreve, e é
// o único formato usado pelas gravações reais do dataset em
// testdata/asr-dataset/audio. Não é um parser de WAV genérico de
// propósito: qualquer arquivo com chunks extras já não seria uma
// gravação deste dataset, e um parser mais tolerante escondendo isso
// silenciosamente é pior do que este teste falhar alto.
func readInt16WAVForTest(t *testing.T, path string) []int16 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lendo %s: %v", path, err)
	}
	const headerSize = 44
	if len(raw) < headerSize {
		t.Fatalf("%s: arquivo menor que o cabeçalho WAV esperado (%d bytes)", path, headerSize)
	}
	body := raw[headerSize:]
	out := make([]int16, len(body)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(body[i*2 : i*2+2]))
	}
	return out
}

// TestRecoverBitPatternFloat32_RoundTripKnownTruth é o teste que importa:
// gera fala sintética conhecida, corrompe para a frente exatamente como o
// defeito real corrompia, recupera com RecoverBitPatternFloat32, e mede o
// ERRO contra o original — não só confere que a função rodou.
func TestRecoverBitPatternFloat32_RoundTripKnownTruth(t *testing.T) {
	const sampleRate = 16000
	clean := syntheticSpeechF32(sampleRate, 3.0)
	corrupted := corruptForward(clean)
	recovered := RecoverBitPatternFloat32(corrupted)

	if len(recovered) != len(clean) {
		t.Fatalf("len(recovered) = %d, want %d", len(recovered), len(clean))
	}

	var peakClean, peakRecovered float64
	var sumSqErr, sumSqClean float64
	var maxSampleRelErr float64
	for i := range clean {
		want := float64(clean[i])
		got := float64(recovered[i])
		errAbs := math.Abs(got - want)
		sumSqErr += errAbs * errAbs
		sumSqClean += want * want
		if a := math.Abs(want); a > peakClean {
			peakClean = a
		}
		if a := math.Abs(got); a > peakRecovered {
			peakRecovered = a
		}
		// Erro relativo por amostra só é um número estável longe de
		// zero — perto de zero, arredondamento de 16 bits domina e o
		// erro relativo dispara sem significar nada sobre a qualidade
		// da recuperação.
		if math.Abs(want) > 0.01 {
			if rel := errAbs / math.Abs(want); rel > maxSampleRelErr {
				maxSampleRelErr = rel
			}
		}
	}
	rmsClean := math.Sqrt(sumSqClean / float64(len(clean)))
	rmsErr := math.Sqrt(sumSqErr / float64(len(clean)))
	rmsRelErr := rmsErr / rmsClean
	peakRelErr := math.Abs(peakRecovered-peakClean) / peakClean

	t.Logf("pico: limpo=%.6f recuperado=%.6f (erro relativo %.4f%%)", peakClean, peakRecovered, 100*peakRelErr)
	t.Logf("RMS do erro / RMS do limpo = %.4f%%", 100*rmsRelErr)
	t.Logf("maior erro relativo por amostra (|amostra limpa|>0.01) = %.4f%%", 100*maxSampleRelErr)

	// O passo bits(f)->int32/2^31 é bijetivo; o único erro real vem de
	// quantizar para int16 e depois expandir de volta — que preserva
	// sinal+expoente+7 bits de mantissa (ver recover.go). Isso dá
	// precisão relativa bem melhor que 1% em teoria; o teto de 2% abaixo
	// é folga deliberada para não deixar o teste frágil a mudanças
	// mínimas no sinal sintético, mantendo ainda um limite que uma
	// recuperação quebrada (escala errada, sinal invertido) estouraria
	// com folga.
	const maxRelErr = 0.02
	if peakRelErr > maxRelErr {
		t.Fatalf("erro relativo do pico = %.4f%%, want <= %.0f%%", 100*peakRelErr, 100*maxRelErr)
	}
	if rmsRelErr > maxRelErr {
		t.Fatalf("erro RMS relativo = %.4f%%, want <= %.0f%%", 100*rmsRelErr, 100*maxRelErr)
	}

	profile := AnalyzeF0(toFloat64Slice(recovered), sampleRate)
	t.Logf("perfil do recuperado: vozeado=%.1f%% F0med=%.1fHz |dF0|med=%.2fHz",
		100*profile.VoicedFraction, profile.MedianF0Hz, profile.MedianAbsDeltaHz)
	if !profile.IsSpeech() {
		t.Fatalf("AnalyzeF0(recuperado).IsSpeech() = false, want true (perfil: %+v)", profile)
	}
}

// TestRecoverBitPatternFloat32_ShuffledControlFailsSpeechDetector é o
// controle decisivo: embaralhar as MESMAS amostras recuperadas preserva a
// distribuição exata de valores (mesmo pico, mesmo RMS) e destrói só a
// estrutura temporal. Se o detector continuasse marcando isso como fala,
// ele estaria medindo distribuição de valores, não pitch — e a prova de
// recuperação acima não provaria nada.
func TestRecoverBitPatternFloat32_ShuffledControlFailsSpeechDetector(t *testing.T) {
	const sampleRate = 16000
	clean := syntheticSpeechF32(sampleRate, 3.0)
	recovered := RecoverBitPatternFloat32(corruptForward(clean))

	recoveredProfile := AnalyzeF0(toFloat64Slice(recovered), sampleRate)
	if !recoveredProfile.IsSpeech() {
		t.Fatalf("pré-condição falhou: recuperado (não embaralhado) já não passa no detector (perfil: %+v)", recoveredProfile)
	}

	shuffled := make([]float32, len(recovered))
	copy(shuffled, recovered)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	shuffledProfile := AnalyzeF0(toFloat64Slice(shuffled), sampleRate)
	t.Logf("embaralhado: vozeado=%.1f%% (recuperado não embaralhado: %.1f%%)",
		100*shuffledProfile.VoicedFraction, 100*recoveredProfile.VoicedFraction)
	if shuffledProfile.IsSpeech() {
		t.Fatalf("AnalyzeF0(embaralhado).IsSpeech() = true, want false — embaralhar não deveria deixar passar (perfil: %+v)", shuffledProfile)
	}
	if shuffledProfile.VoicedFraction >= recoveredProfile.VoicedFraction {
		t.Fatalf("fração vozeada embaralhada (%.3f) não caiu abaixo da não embaralhada (%.3f)",
			shuffledProfile.VoicedFraction, recoveredProfile.VoicedFraction)
	}
}

// TestRecoverBitPatternFloat32_WhiteNoiseFailsSpeechDetector confere que
// ruído branco puro — sem nenhuma relação com a corrupção ou a
// recuperação — não engana o detector.
func TestRecoverBitPatternFloat32_WhiteNoiseFailsSpeechDetector(t *testing.T) {
	const sampleRate = 16000
	rng := rand.New(rand.NewSource(2))
	noise := make([]float64, int(sampleRate*3.0))
	for i := range noise {
		noise[i] = rng.NormFloat64() * 0.1
	}

	profile := AnalyzeF0(noise, sampleRate)
	t.Logf("ruído branco: vozeado=%.1f%%", 100*profile.VoicedFraction)
	if profile.IsSpeech() {
		t.Fatalf("AnalyzeF0(ruído branco).IsSpeech() = true, want false (perfil: %+v)", profile)
	}
}

// TestRecoverBitPatternFloat32_RealDatasetSamplesPassSpeechDetector roda
// a recuperação contra as gravações REAIS que o defeito corrompeu, e
// confere que o detector — o MESMO usado por cmd/asrfix — as reconhece
// como fala. Não prova precisão de amplitude (para isso existe o teste
// sintético acima, que tem verdade conhecida); prova que a recuperação
// funciona fora do laboratório.
//
// # Por que ele PULA em vez de falhar
//
// O áudio que ele lê não está no git: o .gitignore bloqueia *.wav, que é
// o invariante AC-028. Logo este teste só tem o que ler na máquina onde
// as gravações existem.
//
// A primeira versão não tratava isso e apontava para
// testdata/asr-dataset/audio. Duas consequências, as duas ruins:
//
//  1. no CI, onde não há áudio nenhum, ele falhava com "no such file" — e
//     deixou o CI vermelho por vários commits sem ninguém notar, porque a
//     suíte passava na máquina de quem escreveu;
//  2. quando as 46 gravações foram refeitas (já com a captura corrigida),
//     aquele diretório passou a conter áudio LIMPO. Aplicar a recuperação
//     a áudio limpo produz lixo, e o teste falhou de novo — desta vez
//     acusando corretamente que o arquivo não era o que ele esperava.
//
// A lição é a mesma das duas vezes: um teste que depende de dado não
// versionado precisa dizer de qual dado depende e pular quando ele não
// está lá. Pular em silêncio seria pior — por isso o t.Skip explica o
// motivo, e o caminho aponta para audio-corrompido/, onde os originais
// defeituosos foram guardados, e não para audio/, que hoje tem as
// gravações boas.
func TestRecoverBitPatternFloat32_RealDatasetSamplesPassSpeechDetector(t *testing.T) {
	const dir = "../../../testdata/asr-dataset/audio-corrompido/"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("pulando: %s não existe nesta máquina. "+
			"São as gravações originais, corrompidas pelo defeito de layout da WAVEFORMATEXTENSIBLE, "+
			"e elas ficam fora do git (.gitignore bloqueia *.wav, invariante AC-028). "+
			"Quem garante a recuperação em toda máquina é o teste sintético de ida e volta.", dir)
	}

	for _, id := range []string{"cont-01", "ingles-01", "media-02", "curta-02"} {
		id := id
		t.Run(id, func(t *testing.T) {
			path := dir + id + ".wav"
			if _, err := os.Stat(path); err != nil {
				t.Skipf("pulando: %s não existe", path)
			}
			raw := readInt16WAVForTest(t, path)
			recovered := RecoverBitPatternFloat32(raw)

			const sampleRate = 16000
			profile := AnalyzeF0(toFloat64Slice(recovered), sampleRate)
			t.Logf("%s recuperado: vozeado=%.1f%% F0med=%.1fHz |dF0|med=%.2fHz",
				id, 100*profile.VoicedFraction, profile.MedianF0Hz, profile.MedianAbsDeltaHz)
			if !profile.IsSpeech() {
				t.Fatalf("%s: AnalyzeF0(recuperado).IsSpeech() = false, want true (perfil: %+v)", id, profile)
			}
		})
	}
}
