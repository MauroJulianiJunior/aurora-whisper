package syncengine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O log do laço com o aprendido (F8g): só contagens, e o "não aplicado neste
// PC" por motivo (a lista fechada do dictionary), no pull e na tentativa de
// novo dos pendentes. Nunca a variante, o termo ou o domínio.
func TestLog_NaoAplicadosPorMotivoSemTexto(t *testing.T) {
	ctx := context.Background()
	p, n := novoPCQueRecebe(t)
	n.mudancas = []Change{
		naSeq(1, doPull(1, fioDoPar("efede", "EFD", false, "fiscal", false))),
		naSeq(2, doPull(1, fioDoPar("dirfi", "DIRF", true, "contabil", false))),
		naSeq(3, doPull(1, fioDoPar("lalurr", "LALUR", false, "", false))),
	}
	rel, err := p.puxar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	quer := map[string]int{string(dictionary.MotivoDominioAusente): 1, string(dictionary.MotivoPalavraAusenteComDominio): 1}
	if fmt.Sprint(rel.NaoAplicados) != fmt.Sprint(quer) {
		t.Errorf("os não aplicados por motivo: %v, quero %v", rel.NaoAplicados, quer)
	}
	campos := fmt.Sprint(camposDoLog(rel, 0))
	if !strings.Contains(campos, "aprendido_nao_aplicados_por_motivo dominio_ausente:1,palavra_ausente_com_dominio:1") ||
		!strings.Contains(campos, "aprendido_aplicados 1") || !strings.Contains(campos, "aprendido_nao_aplicados 2") {
		t.Errorf("o log: %s", campos)
	}
	for _, proibido := range []string{"efede", "EFD", "fiscal", "dirfi", "DIRF", "lalurr", "LALUR", "contabil"} {
		if strings.Contains(campos, proibido) {
			t.Errorf("o log traz %q: %s", proibido, campos)
		}
	}

	// A tentativa de novo de um pendente que continua não valendo não conta
	// (não muda o espelho); a que muda (outro motivo) conta.
	r := p.varrer()
	if len(r.NaoAplicados) != 0 {
		t.Errorf("a tentativa de novo sem mudança contou: %v", r.NaoAplicados)
	}
	p.aMao(func(s *dictionary.Semente) {
		s.Dominios["fiscal"] = []string{"Fiscal.exe"}
	})
	r = p.varrer()
	if r.NaoAplicados[string(dictionary.MotivoPalavraAusenteComDominio)] != 1 {
		t.Errorf("a tentativa de novo com outro motivo: %v", r.NaoAplicados)
	}
}
