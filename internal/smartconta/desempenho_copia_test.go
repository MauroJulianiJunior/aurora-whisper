package smartconta

import (
	"fmt"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O tempo de montar com a cópia no limite (ADR-023, "Riscos", 3): 2.000 termos e
// 10.000 variantes.
//
// O que a medição mostrou (05-nov-2026, i7-13650HX):
//
//	a cópia no limite, sozinha                              ~90 ms
//	com 500 pares que ela já tem (o caso de sempre)          pulados (JaNaCopia)
//	com 500 pares NOVOS (o pior caso)                        ~19 s
//
// O pior caso é o lote do Aprender conferindo cada par contra o dicionário
// inteiro (dictionary.aprenderEm normaliza cada variante a cada par: termos ×
// pares), o mesmo caminho do PC que recebe 500 pares. Na nuvem ele roda uma vez
// por versão, em segundo plano (o Cache segue a montagem depois do prazo do
// ditado), e o ditado do meio usa o Smart sem a cópia. O caso de sempre é o par
// que a cópia já tem: o PC fonte o recebeu pelo pull, e a montagem o pula.
//
// Tetos folgados, para pegar a volta de um problema; quem mede é o benchmark.
// Pulam sob -short e com o detector de corrida.

func copiaNoLimite() dictionary.Semente {
	s := dictionary.Semente{Usuario: dictionary.UsuarioDaCopia}
	porTermo := dictionary.MaxVariantesDaCopia / dictionary.MaxTermosDaCopia
	for i := 0; i < dictionary.MaxTermosDaCopia; i++ {
		e := dictionary.EntradaSemente{Canonica: fmt.Sprintf("Termo%dQZ", i), Prioridade: i % 120}
		for j := 0; j < porTermo; j++ {
			e.Alias = append(e.Alias, fmt.Sprintf("termozq%dv%d", i, j))
		}
		s.Entradas = append(s.Entradas, e)
	}
	return s
}

// paresDaCopia são n pares que a cópia já tem, do mais recente ao mais antigo.
func paresDaCopia(s dictionary.Semente, n int) []dictionary.ParDaNuvem {
	pares := make([]dictionary.ParDaNuvem, 0, n)
	for i := 0; i < n && i < len(s.Entradas); i++ {
		e := s.Entradas[i]
		pares = append(pares, dictionary.ParDaNuvem{Variante: e.Alias[0], Termo: e.Canonica})
	}
	return pares
}

func TestDesempenho_CopiaNoLimiteMonta(t *testing.T) {
	pularSeForMedirDemais(t)
	copia := copiaNoLimite()
	if err := dictionary.ValidarCopia(copia); err != nil {
		t.Fatalf("a cópia do teste passou do limite: %v", err)
	}

	inicio := time.Now()
	if _, err := MontarComCopia("conta-1", &copia, nil); err != nil {
		t.Fatal(err)
	}
	sozinha := time.Since(inicio)

	inicio = time.Now()
	conta, err := MontarComCopia("conta-1", &copia, paresDaCopia(copia, MaxPares))
	if err != nil {
		t.Fatal(err)
	}
	comPares := time.Since(inicio)

	t.Logf("cópia no limite: %v sozinha, %v com %d pares que ela já tem", sozinha, comPares, MaxPares)
	if r := conta.Resumo(); r.JaNaCopia != MaxPares || r.Aplicados != 0 {
		t.Errorf("resumo = %+v: os pares que a cópia tem não foram pulados", r)
	}
	if sozinha > 2*time.Second {
		t.Errorf("a cópia no limite levou %v para montar (teto 2 s)", sozinha)
	}
	if comPares > 2*time.Second {
		t.Errorf("a cópia no limite com %d pares que ela já tem levou %v (teto 2 s)", MaxPares, comPares)
	}
}

// BenchmarkMontarCopiaNoLimiteComParesNovos é o pior caso: a cópia no limite e
// 500 pares que ela não tem (go test -bench CopiaNoLimite -run ^$).
func BenchmarkMontarCopiaNoLimiteComParesNovos(b *testing.B) {
	copia := copiaNoLimite()
	pares := paresSinteticos(MaxPares)
	for i := 0; i < b.N; i++ {
		if _, err := MontarComCopia("conta-1", &copia, pares); err != nil {
			b.Fatal(err)
		}
	}
}
