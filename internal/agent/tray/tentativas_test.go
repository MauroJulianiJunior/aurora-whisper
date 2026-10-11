package tray

import "testing"

func TestDepoisDaTentativa(t *testing.T) {
	casos := []struct {
		tentativas int
		conseguiu  bool
		jaSubiu    bool
		quer       Passo
	}{
		{1, true, false, PassoPronto},
		// A primeira falha NÃO é o fim: é o Explorer ocupado no logon.
		{1, false, false, PassoTentarDeNovo},
		{MaxTentativasDoIcone - 1, false, false, PassoTentarDeNovo},
		{MaxTentativasDoIcone, false, false, PassoDesistir},
		// Conseguir na última ainda vale.
		{MaxTentativasDoIcone, true, false, PassoPronto},
	}
	for _, c := range casos {
		if got := DepoisDaTentativa(c.tentativas, c.conseguiu, c.jaSubiu); got != c.quer {
			t.Errorf("DepoisDaTentativa(%d, %v, %v) = %d, quer %d", c.tentativas, c.conseguiu, c.jaSubiu, got, c.quer)
		}
	}
}

// TestDepoisDaTentativa_DepoisDaSubidaNuncaDesiste trava o defeito da
// revisão de 29-set-2026: o Explorer reiniciou, o NIM_ADD do
// TaskbarCreated falhou, e o ícone sumia pelo resto da sessão. Depois da
// subida não há a quem devolver erro — desistir só deixaria o Agent
// ditando sem ícone.
func TestDepoisDaTentativa_DepoisDaSubidaNuncaDesiste(t *testing.T) {
	for _, n := range []int{1, MaxTentativasDoIcone, MaxTentativasDoIcone + 1, 10 * MaxTentativasDoIcone} {
		if got := DepoisDaTentativa(n, false, true); got != PassoTentarDeNovo {
			t.Errorf("depois da subida, tentativa %d falhou: passo %d, quer PassoTentarDeNovo", n, got)
		}
	}
	if got := DepoisDaTentativa(MaxTentativasDoIcone+1, true, true); got != PassoPronto {
		t.Errorf("depois da subida, conseguir = passo %d, quer PassoPronto", got)
	}
}

// TestTentativas_JanelaDeLogonRazoavel: o total de espera precisa cobrir
// um Explorer ocupado no logon (o SendMessageTimeout dele é de segundos),
// sem deixar quem abriu o Agent esperando uma eternidade por um erro.
func TestTentativas_JanelaDeLogonRazoavel(t *testing.T) {
	total := IntervaloEntreTentativas * (MaxTentativasDoIcone - 1)
	if total.Seconds() < 5 || total.Seconds() > 30 {
		t.Fatalf("tentativas cobrem %v; o razoável fica entre 5 s e 30 s", total)
	}
}
