package main

import (
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
)

// Paridade da C5a (ADR-021): a política de esforço saiu daqui para
// internal/polish, onde o servidor a usa também, e o adaptador do Agent
// (polish.go) a chama de lá.
//
// Os testes do Polish do Agent (polish_test.go) seguem chamando a política
// pelos nomes que ela tinha neste pacote, SEM mudança: são a prova de que o
// Agent se comporta como antes. Estes nomes existem só para isso, e só na
// compilação dos testes. O embutido é a política de verdade, a mesma que o
// polishDoAgent guarda.
type politicaEsforco struct{ *polish.PoliticaEsforco }

func novaPoliticaEsforco(log *slog.Logger) politicaEsforco {
	return politicaEsforco{polish.NovaPoliticaEsforco(log)}
}

func (p politicaEsforco) atual(provedor, modelo string) string {
	return p.Atual(provedor, modelo)
}

func (p politicaEsforco) recusou(provedor, modelo, mandado string) {
	p.Recusou(provedor, modelo, mandado)
}
