package winruntime

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
)

// Paridade da C5a (ADR-021): o prazo do Polish saiu daqui para
// internal/polish, onde o servidor o usa também, e o runtime o chama de lá
// (prazoDoTeto, em polish.go).
//
// Os testes do Polish do runtime (polish_test.go e polish_longo_test.go)
// seguem lendo o prazo pelos nomes que ele tinha neste pacote, SEM mudança:
// são a prova de que o Agent tem o mesmo prazo por tamanho de texto. Estes
// dois nomes existem só para isso, e só na compilação dos testes.
const pisoPrazoPolish = polish.PisoPrazo

func prazoPolish(texto string) time.Duration { return polish.Prazo(texto) }
