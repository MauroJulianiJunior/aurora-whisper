package resolver

import (
	"context"
	"os"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// BenchmarkSmartResolve_DicionarioReal mede o custo do resolver com o
// dicionário de referência (43 termos) numa frase típica de ditado.
//
// O número decide uma escolha de arquitetura no Agent: se o resolve for
// barato, ele roda no laço de eventos como qualquer outro efeito; se for
// caro, precisa ir para outra goroutine como a transcrição foi (ADR-007,
// e o T20 do roteiro manual — o hook de teclado tem orçamento de ~300 ms
// por callback e o Windows o remove em silêncio se estourar).
//
// Pula quando o dicionário de referência não está presente: ele é dado
// de bancada, não dependência do pacote.
func BenchmarkSmartResolve_DicionarioReal(b *testing.B) {
	f, err := os.Open("../../testdata/asr-dataset/dicionario.json")
	if err != nil {
		b.Skip("dicionário de referência ausente:", err)
	}
	defer f.Close()

	s, err := dictionary.CarregarSemente(f)
	if err != nil {
		b.Fatalf("CarregarSemente: %v", err)
	}
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		b.Fatalf("Popular: %v", err)
	}
	r := NewSmartResolver(repo)

	// Frase longa de propósito: o custo cresce com o número de tokens,
	// porque cada um tenta frases de até 4 palavras contra o dicionário.
	const texto = "precisa transmitir a DCTF Web da competência de agosto antes do dia quinze " +
		"e depois subir a migration no Supabaese e conferir se o Postgre aceitou o commit"

	req := ResolveRequest{UserID: s.Usuario, ASRResult: asr.ASRResult{Text: texto, Language: "pt"}}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Resolve(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}
