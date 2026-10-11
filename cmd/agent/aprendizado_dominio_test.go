package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Terceira revisão da etapa 3 (30-set-2026): a conferência "a variante age?"
// tem um critério só para o domínio. Tudo em t.TempDir(), com o runtime falso.

// dicionarioComConcorrente: "Vite" (90) leva a busca por "Viti" sempre que
// está visível, e "Vitti" (60) é o termo que vai ganhar a variante. O que muda
// de caso para caso é o domínio de cada um e se Vitti é restrito.
func dicionarioComConcorrente(dominioDoVite, vitti string) string {
	return `{
  "usuario": "mauro",
  "dominios": {"dev": ["Code.exe"], "contabil": ["SistemaContabil.exe"]},
  "entradas": [
    {"canonica": "Vite", "prioridade": 90` + dominioDoVite + `},
    {"canonica": "Vitti", "prioridade": 60` + vitti + `}
  ]
}
`
}

const (
	naDev      = `, "dominio": "dev"`
	naContabil = `, "dominio": "contabil"`
	restritoEm = `, "restrito": true`
)

// TestAprendizado_Dominio_TermoNaoRestritoConfereNoDesconhecido: um termo com
// domínio que a pessoa NÃO restringiu aparece em qualquer domínio, inclusive no
// desconhecido, e é lá que a conferência roda. "Vite", de outro domínio,
// também aparece ali e leva a busca (prioridade maior): a variante nunca age
// quando o app em foco não é de domínio nenhum, e o painel não pode responder
// "aprendido". Conferir só no domínio do termo (dev), onde o "Vite" contábil
// não aparece, aceitaria uma variante que só age em parte dos ditados.
func TestAprendizado_Dominio_TermoNaoRestritoConfereNoDesconhecido(t *testing.T) {
	conteudo := dicionarioComConcorrente(naContabil, naDev)
	m := montarAprendizado(t, conteudo, "mauro")
	_, err := m.a.Aprender(context.Background(), "Vitti", "Viti")
	if !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Fatalf("erro = %v, quero ErrNaoAgiria", err)
	}
	if got := m.bytes(t); !bytes.Equal(got, []byte(conteudo)) {
		t.Error("a recusa gravou no arquivo")
	}
	if m.rt.quantos() != 0 {
		t.Errorf("o runtime recebeu %d dicionários por um pedido recusado", m.rt.quantos())
	}

	// O mesmo termo, sem domínio nenhum, é recusado pelo mesmo motivo: o
	// critério não depende de ele ter domínio.
	m = montarAprendizado(t, dicionarioComConcorrente(naContabil, ``), "mauro")
	if _, err := m.a.Aprender(context.Background(), "Vitti", "Viti"); !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Errorf("termo sem domínio: erro = %v, quero ErrNaoAgiria", err)
	}
}

// TestAprendizado_Dominio_TermoRestritoConfereNoDominioDele: o termo restrito
// só aparece no domínio dele, e é lá que a conferência roda. O "Vite" de outro
// domínio não aparece ali, então a variante age e é aceita; o "Vite" do mesmo
// domínio leva a busca, e a variante é recusada.
func TestAprendizado_Dominio_TermoRestritoConfereNoDominioDele(t *testing.T) {
	m := montarAprendizado(t, dicionarioComConcorrente(naContabil, naDev+restritoEm), "mauro")
	if _, err := m.a.Aprender(context.Background(), "Vitti", "Viti"); err != nil {
		t.Fatalf("concorrente de outro domínio: Aprender: %v", err)
	}
	if m.rt.quantos() != 1 {
		t.Errorf("o runtime recebeu %d dicionários", m.rt.quantos())
	}

	conteudo := dicionarioComConcorrente(naDev, naDev+restritoEm)
	m = montarAprendizado(t, conteudo, "mauro")
	_, err := m.a.Aprender(context.Background(), "Vitti", "Viti")
	if !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Fatalf("concorrente do mesmo domínio: erro = %v, quero ErrNaoAgiria", err)
	}
	if got := m.bytes(t); !bytes.Equal(got, []byte(conteudo)) {
		t.Error("a recusa gravou no arquivo")
	}
}

// TestAprendizado_Dominio_TermoComDominioConfereTambemNoDominioDele: o termo de
// domínio que a pessoa não restringiu vive em dois lugares, o desconhecido e o
// domínio dele, e a variante tem de agir nos dois. O "Vite" restrito ao dev só
// aparece no dev, então no desconhecido a conferência passa sozinha; é no dev,
// onde ele leva a busca por prioridade, que a variante de "Vitti" não age.
// Conferir só no desconhecido responderia "aprendido" para uma variante que não
// faz nada no domínio onde o termo vive.
func TestAprendizado_Dominio_TermoComDominioConfereTambemNoDominioDele(t *testing.T) {
	conteudo := dicionarioComConcorrente(naDev+restritoEm, naDev)
	m := montarAprendizado(t, conteudo, "mauro")
	_, err := m.a.Aprender(context.Background(), "Vitti", "Viti")
	if !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Fatalf("erro = %v, quero ErrNaoAgiria", err)
	}
	if got := m.bytes(t); !bytes.Equal(got, []byte(conteudo)) {
		t.Error("a recusa gravou no arquivo")
	}
	if m.rt.quantos() != 0 {
		t.Errorf("o runtime recebeu %d dicionários por um pedido recusado", m.rt.quantos())
	}

	// O mesmo "Vite" restrito, mas a outro domínio: não aparece no dev nem no
	// desconhecido, e a variante age nos dois lugares onde "Vitti" vive.
	m = montarAprendizado(t, dicionarioComConcorrente(naContabil+restritoEm, naDev), "mauro")
	if _, err := m.a.Aprender(context.Background(), "Vitti", "Viti"); err != nil {
		t.Fatalf("concorrente restrito a outro domínio: Aprender: %v", err)
	}
	if m.rt.quantos() != 1 {
		t.Errorf("o runtime recebeu %d dicionários", m.rt.quantos())
	}
}

// TestAprendizado_Dominio_RestritoSemDominioNaoAge: o Search só mostra a
// entrada restrita quando o domínio pedido bate com o dela, e sem domínio não
// há quem bata. A variante nunca age, e é recusada.
func TestAprendizado_Dominio_RestritoSemDominioNaoAge(t *testing.T) {
	const conteudo = `{
  "usuario": "mauro",
  "entradas": [
    {"canonica": "Zulu", "prioridade": 60, "restrito": true}
  ]
}
`
	m := montarAprendizado(t, conteudo, "mauro")
	_, err := m.a.Aprender(context.Background(), "Zulu", "variante de teste")
	if !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Fatalf("erro = %v, quero ErrNaoAgiria", err)
	}
	if m.rt.quantos() != 0 {
		t.Errorf("o runtime recebeu %d dicionários", m.rt.quantos())
	}
}
