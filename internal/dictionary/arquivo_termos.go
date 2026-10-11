package dictionary

import "errors"

// O "Adicionar termo" sobre o arquivo (ADR-020): as mesmas regras de
// Aprender (relê o arquivo, valida, grava de forma atômica, e nunca toca o
// arquivo que não é nosso), com a conferência ConferirMudanca no lugar de
// ValidarAprendida.

// ListarTermos lista o que a pessoa adicionou, segundo o arquivo de agora.
// Como Aprendidas, só lê, e espera uma gravação em curso.
func (a *Arquivo) ListarTermos() (ListaDeTermos, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.ler()
	if err != nil {
		return ListaDeTermos{}, err
	}
	return s.TermosAdicionados(), nil
}

// AdicionarTermo grava o termo do pedido. Devolve a semente gravada, para quem
// chama montar o dicionário em memória a partir do que está no disco.
func (a *Arquivo) AdicionarTermo(p PedidoTermo) (Semente, MudancaDeTermos, error) {
	return a.alterarTermos(func(s Semente) (Semente, MudancaDeTermos, error) {
		return s.AdicionarTermo(p, a.PalavraComum)
	})
}

// DicaDoTermo liga ou desliga a dica de um termo que a pessoa adicionou.
func (a *Arquivo) DicaDoTermo(termo string, ligar bool) (Semente, MudancaDeTermos, error) {
	return a.alterarTermos(func(s Semente) (Semente, MudancaDeTermos, error) {
		return s.DicaDoTermo(termo, ligar)
	})
}

// RemoverTermo tira o que a pessoa adicionou no termo.
func (a *Arquivo) RemoverTermo(termo string) (Semente, MudancaDeTermos, error) {
	return a.alterarTermos(func(s Semente) (Semente, MudancaDeTermos, error) {
		return s.RemoverTermo(termo)
	})
}

func (a *Arquivo) alterarTermos(op func(Semente) (Semente, MudancaDeTermos, error)) (Semente, MudancaDeTermos, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	antes, err := a.ler()
	if err != nil {
		return Semente{}, MudancaDeTermos{}, err
	}
	depois, m, err := op(antes)
	if err != nil {
		return Semente{}, MudancaDeTermos{}, err
	}
	if a.Validar != nil {
		if err := a.Validar(depois); err != nil {
			return Semente{}, MudancaDeTermos{}, &erroNaoCarrega{causa: err}
		}
	}
	if a.ConferirMudanca != nil {
		if err := a.ConferirMudanca(antes, depois, m.Novas); err != nil {
			var naoAgiria *ErroNaoAgiria
			var quebraria *ErroQuebraria
			switch {
			case errors.As(err, &naoAgiria):
				return Semente{}, MudancaDeTermos{}, naoAgiria
			case errors.As(err, &quebraria):
				return Semente{}, MudancaDeTermos{}, quebraria
			}
			return Semente{}, MudancaDeTermos{}, &erroNaoCarrega{causa: err}
		}
	}
	if err := depois.Gravar(a.Caminho); err != nil {
		return Semente{}, MudancaDeTermos{}, err
	}
	return depois, m, nil
}
