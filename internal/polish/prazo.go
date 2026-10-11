package polish

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// PisoPrazo é o prazo do Polish para o texto curto: 3,5 s.
//
// O benchmark de 30-set-2026 (docs/benchmarks/polish-2026-09-30.md) mediu
// p95 de 2,35 s no gpt-6-luna (conexão reaproveitada; a fria, 2,49 s) e
// 1,6% das chamadas acima de 3,5 s: o número fica (ADR-016, decisão 4).
//
// No Agent o prazo é contado do COMEÇO da resolução (antes do Smart), e a
// folga até o watchdog do núcleo vem de internal/agent/winruntime, que
// também trava essa conta (TestPrazoPolish_CabeNoWatchdogDoNucleo). No
// servidor, quem chama decide de onde conta.
const PisoPrazo = 3500 * time.Millisecond

// Prazo é o teto do Polish para o texto que entra na resolução: PisoPrazo
// mais dictation.AcrescimoPeloTexto, de 3,5 s (até 250 caracteres) a 8 s
// (1000 caracteres ou mais), com 6 ms por caractere no meio (decisão 5 de
// docs/defeito-ditado-longo.md; ADR-016, emenda de 01-out-2026; ADR-021,
// decisão N).
//
// # Por que cresce
//
// A latência da chamada é uma base (TLS, fila, primeiro token) mais o
// tempo de gerar a saída, e a saída cresce com o texto. No log de
// 01-out-2026: 359 caracteres levaram 3143 ms com 96 tokens de saída
// (aceito, a 357 ms do teto); 907, 3032 ms com 195 (recusado pela guarda,
// não pelo prazo); 802 levaram mais que os 3468 ms que sobravam e caíram
// no Smart. Com o prazo fixo, o ditado longo no modo com IA saía sempre
// como Smart. Agora: 359 caracteres têm 4154 ms; 802, 6812 ms; 907, 7442
// ms. A conta do acréscimo está em dictation.AcrescimoPorCaractere.
//
// O acréscimo é o MESMO do watchdog do núcleo (dictation.PrazoDeResolucao
// = 5 s + acréscimo), calculado sobre o MESMO texto: a folga de 1,5 s do
// Agent vale em qualquer tamanho. Por isso a conta não é repetida aqui: é
// a de dictation, chamada.
//
// Conta caracteres (runas), não bytes. Texto vazio dá o piso.
func Prazo(texto string) time.Duration {
	return PisoPrazo + dictation.AcrescimoPeloTexto(texto)
}
