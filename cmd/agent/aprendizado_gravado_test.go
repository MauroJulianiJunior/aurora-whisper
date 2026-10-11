package main

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// TestAprendizado_GravouMasNaoTrocouDevolveOQueFicouGravado: se a montagem do
// dicionário em memória falha DEPOIS de gravar (na prática não acontece, a
// validação roda a mesma montagem antes), o arquivo já mudou. O erro é
// ErrGravadoSemTroca e vem com o que ficou gravado: a tela diz que aprendeu e
// que só vale depois de reiniciar, e não "não deu para aprender", porque a
// lista do painel já mostra o par. Tudo em t.TempDir(), com o runtime falso.
func TestAprendizado_GravouMasNaoTrocouDevolveOQueFicouGravado(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	m.a.montar = func(dictionary.Semente) (*resolver.SmartResolver, error) {
		return nil, errors.New("segredo: termo do dicionário que não pode vazar")
	}

	feito, err := m.a.Aprender(context.Background(), "ECF", "esse efe")
	if !errors.Is(err, dictionary.ErrGravadoSemTroca) {
		t.Fatalf("erro = %v, quero ErrGravadoSemTroca", err)
	}
	if want := (dictionary.Aprendida{Termo: "ECF", Variante: "esse efe"}); feito != want {
		t.Errorf("Aprendida = %+v, quero %+v", feito, want)
	}
	if strings.Contains(err.Error(), "segredo") {
		t.Errorf("o erro carrega a causa: %q", err)
	}
	if !bytes.Contains(m.bytes(t), []byte("esse efe")) {
		t.Error("o arquivo não tem o que o erro diz que foi gravado")
	}
	lista, err := m.a.Aprendidas(context.Background())
	if err != nil || len(lista) != 1 || lista[0].Variante != "esse efe" {
		t.Errorf("Aprendidas = %v, %v; quero a variante que foi gravada", lista, err)
	}
	if m.rt.quantos() != 0 {
		t.Errorf("o runtime recebeu %d dicionários de uma montagem que falhou", m.rt.quantos())
	}
	// Desfazer passa pelo mesmo caminho: o arquivo volta, a troca em memória
	// não vai, e o erro é o mesmo, com o que saiu.
	feito, err = m.a.Desfazer(context.Background(), "esse efe")
	if !errors.Is(err, dictionary.ErrGravadoSemTroca) {
		t.Fatalf("Desfazer: erro = %v, quero ErrGravadoSemTroca", err)
	}
	if want := (dictionary.Aprendida{Termo: "ECF", Variante: "esse efe"}); feito != want {
		t.Errorf("Desfazer: Aprendida = %+v, quero %+v", feito, want)
	}
	if got := m.bytes(t); string(got) != dicionarioDoAprendizado {
		t.Error("Desfazer não devolveu o arquivo ao que era")
	}

	// O log avisa das duas, e cada linha é só o aviso e a ação: nem a variante,
	// nem o termo (o dono), nem a causa entram nela (ADR-017, "O log").
	log := m.log.String()
	for _, proibido := range []string{"esse efe", "ECF", "segredo", "dicionário que não pode vazar"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
	linhas := strings.Split(strings.TrimSuffix(log, "\n"), "\n")
	if len(linhas) != 2 {
		t.Fatalf("o log tem %d linhas, quero 2:\n%s", len(linhas), log)
	}
	for i, acao := range []string{"aprender", "desfazer"} {
		forma := regexp.MustCompile(`^time=\S+ level=WARN msg="aprendizado: gravado, mas o dicionário em memória não foi trocado" acao=` + acao + `$`)
		if !forma.MatchString(linhas[i]) {
			t.Errorf("linha %d do log = %q, quero só o aviso e acao=%s", i+1, linhas[i], acao)
		}
	}
}
