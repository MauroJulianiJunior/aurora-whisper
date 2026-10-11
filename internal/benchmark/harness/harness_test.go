package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/candidate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// writeWAV escreve um WAV PCM16 mono mínimo (silêncio) e devolve o
// caminho — mesmo formato usado pelos testes de internal/benchmark/dataset.
func writeWAV(t *testing.T, dir, name string, sampleRateHz, numSamples int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("criando WAV: %v", err)
	}
	defer f.Close()

	const bitsPerSample = 16
	blockAlign := bitsPerSample / 8
	byteRate := sampleRateHz * blockAlign
	dataSize := numSamples * blockAlign

	writeLE32 := func(v uint32) { writeBytes(t, f, le32(v)) }
	writeLE16 := func(v uint16) { writeBytes(t, f, le16(v)) }

	f.WriteString("RIFF")
	writeLE32(uint32(36 + dataSize))
	f.WriteString("WAVE")
	f.WriteString("fmt ")
	writeLE32(16)
	writeLE16(1)
	writeLE16(1)
	writeLE32(uint32(sampleRateHz))
	writeLE32(uint32(byteRate))
	writeLE16(uint16(blockAlign))
	writeLE16(bitsPerSample)
	f.WriteString("data")
	writeLE32(uint32(dataSize))
	if dataSize > 0 {
		if _, err := f.Write(make([]byte, dataSize)); err != nil {
			t.Fatalf("escrevendo PCM: %v", err)
		}
	}
	return path
}

func le32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}
func le16(v uint16) []byte {
	return []byte{byte(v), byte(v >> 8)}
}
func writeBytes(t *testing.T, f *os.File, b []byte) {
	t.Helper()
	if _, err := f.Write(b); err != nil {
		t.Fatalf("escrevendo WAV: %v", err)
	}
}

func writeFixture(t *testing.T, audioPath, text string) {
	t.Helper()
	if err := os.WriteFile(audioPath+".transcript.txt", []byte(text), 0o644); err != nil {
		t.Fatalf("escrevendo fixture de transcrição: %v", err)
	}
}

func TestRun_ColdAndWarm_MockProvider_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	a1 := writeWAV(t, dir, "a1.wav", 16000, 16000) // 1s
	writeFixture(t, a1, "o Fisco cobrou o DCTFWeb")

	a2 := writeWAV(t, dir, "a2.wav", 16000, 16000)
	// Transcrição INTENCIONALMENTE diferente da referência, para provar
	// que WER/CER > 0 aparecem no relatório quando o candidato erra.
	writeFixture(t, a2, "completamente diferente")

	samples := []dataset.Sample{
		{ID: "s1", AudioPath: a1, Reference: "o Fisco cobrou o DCTFWeb", Category: dataset.CategoryContabilidade, Entities: []string{"Fisco", "DCTFWeb"}},
		{ID: "s2", AudioPath: a2, Reference: "referencia original aqui", Category: dataset.CategoryPTBRGeral},
	}

	t.Setenv(helperEnvVar, "helper")
	cfg := helperSelfConfig(t, "mock-candidate", true)

	result, err := Run(context.Background(), samples, []candidate.Config{cfg})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DatasetHash == "" {
		t.Fatal("DatasetHash vazio")
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(Candidates) = %d, want 1", len(result.Candidates))
	}

	cr := result.Candidates[0]

	// Cold: 2 amostras tentadas, 0 falhas, latência medida.
	if cr.Cold.Attempted != 2 || cr.Cold.Failed != 0 {
		t.Fatalf("Cold = %+v, want Attempted=2 Failed=0", cr.Cold)
	}
	if cr.Cold.Latency.N != 2 {
		t.Fatalf("Cold.Latency.N = %d, want 2", cr.Cold.Latency.N)
	}
	// A amostra s1 (transcrição == referência) deve ter WER/CER == 0; a
	// s2 (transcrição diferente) deve ter WER/CER > 0. A média agregada
	// entre as duas, portanto, deve ficar estritamente entre 0 e o valor
	// da pior amostra.
	if cr.Cold.WER.Mean <= 0 {
		t.Fatalf("Cold.WER.Mean = %v, want > 0 (s2 erra a transcrição)", cr.Cold.WER.Mean)
	}
	// EntityAccuracy: s1 tem 2 entidades esperadas e a transcrição bate
	// exatamente -> 2/2. s2 não declara entidades -> 0/0 (não soma nada).
	if cr.Cold.EntityAccuracy.Total != 2 || cr.Cold.EntityAccuracy.Correct != 2 {
		t.Fatalf("Cold.EntityAccuracy = %+v, want 2/2", cr.Cold.EntityAccuracy)
	}

	// Warm: mesmo candidato, protocolo diferente — Supported deve ser
	// true (WarmArgs configurado) e as métricas devem existir de forma
	// independente das de Cold (nunca somadas).
	if !cr.Warm.Supported {
		t.Fatal("Warm.Supported = false, want true (WarmArgs configurado)")
	}
	if cr.Warm.Attempted != 2 || cr.Warm.Failed != 0 {
		t.Fatalf("Warm = %+v, want Attempted=2 Failed=0", cr.Warm)
	}
	if cr.Warm.Latency.N != 2 {
		t.Fatalf("Warm.Latency.N = %d, want 2", cr.Warm.Latency.N)
	}
}

func TestRun_WarmNotSupported_ReportsUnmeasuredNotFailure(t *testing.T) {
	dir := t.TempDir()
	a1 := writeWAV(t, dir, "a1.wav", 16000, 16000)
	writeFixture(t, a1, "oi")
	samples := []dataset.Sample{
		{ID: "s1", AudioPath: a1, Reference: "oi", Category: dataset.CategoryIngles},
	}

	t.Setenv(helperEnvVar, "helper")
	cfg := helperSelfConfig(t, "sem-warm", false) // WarmArgs vazio

	result, err := Run(context.Background(), samples, []candidate.Config{cfg})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	warm := result.Candidates[0].Warm
	if warm.Supported {
		t.Fatal("Warm.Supported = true, want false (candidato sem WarmArgs)")
	}
	if warm.Attempted != 0 || warm.Failed != 0 {
		t.Fatalf("Warm = %+v, want zero-value (não medido, não é falha)", warm)
	}
}

func TestRun_FailingCandidate_CountsAsFailedNotCrash(t *testing.T) {
	dir := t.TempDir()
	a1 := writeWAV(t, dir, "a1.wav", 16000, 16000)
	writeFixture(t, a1, "oi")
	samples := []dataset.Sample{
		{ID: "s1", AudioPath: a1, Reference: "oi", Category: dataset.CategoryIngles},
	}

	t.Setenv(helperEnvVar, "fail")
	cfg := helperSelfConfig(t, "candidato-que-falha", false)

	result, err := Run(context.Background(), samples, []candidate.Config{cfg})
	if err != nil {
		t.Fatalf("Run: %v (Run não deve propagar falha de UM candidato)", err)
	}
	cold := result.Candidates[0].Cold
	if cold.Attempted != 1 || cold.Failed != 1 {
		t.Fatalf("Cold = %+v, want Attempted=1 Failed=1", cold)
	}
	if cold.Latency.N != 0 {
		t.Fatalf("Cold.Latency.N = %d, want 0 (amostra falha não entra na estatística)", cold.Latency.N)
	}
}

// TestRun_EmptyReferenceSample_NeverImprovesAggregateWER prova a correção
// do bug "falha de métrica vira nota perfeita": uma amostra com
// referência vazia (metrics.WER/CER devolvem ErrEmptyReference) NUNCA
// pode entrar na média agregada como um 0.0 disfarçado de transcrição
// impecável. O teste compara dois cenários com a MESMA amostra ruim (s2,
// que erra a transcrição de verdade) — um com uma segunda amostra de
// referência vazia no dataset, outro sem ela — e exige que o WER médio
// relatado seja EXATAMENTE IGUAL nos dois casos: a amostra quebrada não
// pode mexer no número de jeito nenhum, nem para cima nem (o bug real)
// para baixo.
func TestRun_EmptyReferenceSample_NeverImprovesAggregateWER(t *testing.T) {
	dir := t.TempDir()

	// s2: transcrição bem diferente da referência -> WER real, alto,
	// verificável de forma independente via metrics.WER.
	a2 := writeWAV(t, dir, "a2.wav", 16000, 16000)
	writeFixture(t, a2, "banana banana banana banana")
	s2 := dataset.Sample{ID: "s2", AudioPath: a2, Reference: "referencia curta aqui", Category: dataset.CategoryPTBRGeral}

	t.Setenv(helperEnvVar, "helper")

	// Cenário A: só a amostra ruim.
	cfgA := helperSelfConfig(t, "candidato-a", false)
	resultA, err := Run(context.Background(), []dataset.Sample{s2}, []candidate.Config{cfgA})
	if err != nil {
		t.Fatalf("Run (A): %v", err)
	}
	coldA := resultA.Candidates[0].Cold

	// Cenário B: a mesma amostra ruim MAIS uma amostra de referência vazia
	// (só espaços — normaliza para zero tokens, metrics.WER/CER falham).
	a1 := writeWAV(t, dir, "a1.wav", 16000, 16000)
	writeFixture(t, a1, "qualquer coisa aqui")
	s1 := dataset.Sample{ID: "s1", AudioPath: a1, Reference: "   ", Category: dataset.CategoryPTBRGeral}

	cfgB := helperSelfConfig(t, "candidato-b", false)
	resultB, err := Run(context.Background(), []dataset.Sample{s1, s2}, []candidate.Config{cfgB})
	if err != nil {
		t.Fatalf("Run (B): %v", err)
	}
	coldB := resultB.Candidates[0].Cold

	if coldB.Attempted != 2 || coldB.Failed != 0 {
		t.Fatalf("Cold (B) = %+v, want Attempted=2 Failed=0 (falha de MÉTRICA não é falha de AMOSTRA)", coldB)
	}
	if coldB.WERFailed != 1 {
		t.Fatalf("Cold.WERFailed (B) = %d, want 1", coldB.WERFailed)
	}
	if coldB.CERFailed != 1 {
		t.Fatalf("Cold.CERFailed (B) = %d, want 1", coldB.CERFailed)
	}
	if coldB.WER.N != 1 {
		t.Fatalf("Cold.WER.N (B) = %d, want 1 (a amostra de referência vazia fica de fora da fatia agregada)", coldB.WER.N)
	}

	// O ponto central do teste: o WER médio de A (só a amostra ruim) e de
	// B (amostra ruim + amostra quebrada) precisa ser o MESMO número.
	// Antes da correção, a amostra quebrada entrava como WER=0.0 e puxava
	// a média de B para BAIXO de A — uma amostra pior no dataset produzindo
	// um relatório "melhor".
	if coldA.WER.N != 1 {
		t.Fatalf("Cold.WER.N (A) = %d, want 1", coldA.WER.N)
	}
	if coldA.WER.Mean != coldB.WER.Mean {
		t.Fatalf("Cold.WER.Mean A=%v B=%v, want iguais (amostra de referência vazia não pode mudar a média agregada)",
			coldA.WER.Mean, coldB.WER.Mean)
	}

	// E o número em si precisa ser o WER de verdade da amostra ruim, não
	// diluído por nenhum 0.0.
	wantWER, werr := metrics.WER(s2.Reference, "banana banana banana banana")
	if werr != nil {
		t.Fatalf("metrics.WER (esperado): %v", werr)
	}
	if wantWER <= 0 {
		t.Fatalf("wantWER = %v, want > 0 (fixture precisa errar a transcrição de verdade)", wantWER)
	}
	if coldB.WER.Mean != wantWER {
		t.Fatalf("Cold.WER.Mean (B) = %v, want %v (WER real da única amostra válida)", coldB.WER.Mean, wantWER)
	}
}

func TestRun_HangingCandidate_TimesOutInsteadOfHangingTheHarness(t *testing.T) {
	dir := t.TempDir()
	a1 := writeWAV(t, dir, "a1.wav", 16000, 16000)
	writeFixture(t, a1, "oi")
	samples := []dataset.Sample{
		{ID: "s1", AudioPath: a1, Reference: "oi", Category: dataset.CategoryIngles},
	}

	t.Setenv(helperEnvVar, "hang")
	cfg := helperSelfConfig(t, "candidato-travado", false)
	cfg.Timeout = 150 * time.Millisecond

	start := time.Now()
	result, err := Run(context.Background(), samples, []candidate.Config{cfg})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run demorou %v com um candidato travado (timeout configurado %v)", elapsed, cfg.Timeout)
	}
	cold := result.Candidates[0].Cold
	if cold.TimedOut != 1 {
		t.Fatalf("Cold.TimedOut = %d, want 1", cold.TimedOut)
	}
	if cold.Failed != 1 {
		t.Fatalf("Cold.Failed = %d, want 1", cold.Failed)
	}
}

// TestBuildOutcome_TranscricaoVaziaEhFalhaNaoNotaZero trava a distinção que
// separa "candidato ruim" de "medição inválida".
//
// Sem a guarda, um candidato que não produz nada — processo mal
// configurado, servidor HTTP que fechou o corpo cedo, binário que leu EOF
// em vez do áudio — entra no relatório com WER 1,0 e Entity 0/N, e lê como
// "modelo péssimo". As duas leituras pedem ações OPOSTAS: uma elimina o
// candidato do benchmark, a outra manda consertar a configuração e medir
// de novo.
//
// O caso é concreto, não hipotético: cmd/whisperwarm não tem modo de um
// tiro, e se alguém o declarasse em coldArgs, RunCold não escreveria nada
// em stdin, ele leria EOF na hora e devolveria stdout vazio.
//
// O dataset não tem amostra muda — as 46 foram verificadas uma a uma, com
// fala em todas — então vazio aqui só pode ser defeito.
func TestBuildOutcome_TranscricaoVaziaEhFalhaNaoNotaZero(t *testing.T) {
	s := dataset.Sample{
		ID:        "amostra",
		Reference: "precisa transmitir a DCTFWeb antes do dia quinze",
		Entities:  []string{"DCTFWeb"},
		Category:  dataset.CategoryContabilidade,
	}

	for _, texto := range []string{"", "   ", "\t\n  "} {
		res := candidate.Result{Text: texto, WallTime: 250 * time.Millisecond}
		o := buildOutcome(s, res, nil, 250*time.Millisecond)

		if o.Err == nil {
			t.Fatalf("buildOutcome(texto=%q).Err = nil, want %v.\n"+
				"Transcrição vazia entrou como resultado: WER=%v Entity=%d/%d — "+
				"o relatório diria 'modelo péssimo' onde a verdade é 'medição inválida'.",
				texto, ErrEmptyTranscription, o.WER, o.EntityResult.Correct, o.EntityResult.Total)
		}
		if !errors.Is(o.Err, ErrEmptyTranscription) {
			t.Errorf("buildOutcome(texto=%q).Err = %v, want %v", texto, o.Err, ErrEmptyTranscription)
		}
		// Uma falha não pode carregar latência: ela entraria na agregação
		// como se fosse uma transcrição rápida.
		if o.Latency != 0 {
			t.Errorf("buildOutcome(texto=%q).Latency = %v, want 0 — falha não tem latência a agregar", texto, o.Latency)
		}
	}
}

// TestBuildOutcome_TextoComConteudoNaoEhFalha é o controle do teste acima:
// sem ele, a guarda poderia recusar tudo e os dois passariam.
func TestBuildOutcome_TextoComConteudoNaoEhFalha(t *testing.T) {
	s := dataset.Sample{ID: "amostra", Reference: "já mandei o arquivo", Category: dataset.CategoryPTBRGeral}
	res := candidate.Result{Text: "já mandei o arquivo", WallTime: 250 * time.Millisecond}

	o := buildOutcome(s, res, nil, 250*time.Millisecond)
	if o.Err != nil {
		t.Fatalf("buildOutcome(texto válido).Err = %v, want nil", o.Err)
	}
	if o.Latency != 250*time.Millisecond {
		t.Errorf("Latency = %v, want 250ms", o.Latency)
	}
}
