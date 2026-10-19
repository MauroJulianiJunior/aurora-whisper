package resolver

import "testing"

// A correção de grafia (emenda de 18-out-2026 da ADR-016): o Polish pode
// corrigir a palavra que NÃO EXISTE em português para uma de escrita
// parecida que existe; qualquer outra troca continua reprovada.
func TestGuardaPalavras_CorrecaoDeGrafia(t *testing.T) {
	casos := []struct {
		nome, entrada, saida string
		passa                bool
	}{
		// O que o diagnóstico do histórico achou, e a guarda agora aceita.
		{"acento que faltava", "o relatorio ficou pronto ontem", "O relatório ficou pronto ontem.", true},
		{"duas letras numa palavra longa", "a lei vejante determina o prazo", "A lei vigente determina o prazo.", true},
		{"uma letra", "a sasonalidade das vendas pesa no caixa", "A sazonalidade das vendas pesa no caixa.", true},
		{"junção com hífen", "eu mandei um e mail para o cliente", "Eu mandei um e-mail para o cliente.", true},
		{"separação", "derepente o cliente ligou de novo", "De repente o cliente ligou de novo.", true},
		{"acento numa negação", "eu nao vou mandar o boleto hoje", "Eu não vou mandar o boleto hoje.", true},
		{"correção e pontuação falada", "o relatorio vírgula o balancete e a folha", "O relatório, o balancete e a folha.", true},

		// A palavra da entrada existe: pode ser o que a pessoa disse.
		{"pagou -> pegou", "o cliente pagou a conta ontem", "O cliente pegou a conta ontem.", false},
		{"concordância", "os lançamentos compõe o saldo do mês", "Os lançamentos compõem o saldo do mês.", false},
		{"junção de duas que existem", "a logo marca da empresa mudou", "A logomarca da empresa mudou.", false},

		// A palavra da saída não serve.
		{"sigla vira outra sigla", "o irpf vence no fim do mês", "O IRPJ vence no fim do mês.", false},
		{"sigla minúscula vira outra", "o irpf vence no fim do mês", "O irpj vence no fim do mês.", false},
		{"termo que o VERO não tem", "os promptes ficaram longos demais", "Os prompts ficaram longos demais.", false},
		{"nome com acento", "a debora mandou o balancete", "A Débora mandou o balancete.", false},
		{"vira negação", "ele nunka atrasou o pagamento", "Ele nunca atrasou o pagamento.", false},
		{"primeira letra trocada", "o esocial travou de novo hoje", "O social travou de novo hoje.", false},
		{"longe demais", "a lei vejante determina o prazo", "A lei vingança determina o prazo.", false},

		// A palavra da entrada é nome ou sigla: maiúscula no meio da frase.
		{"maiúscula no meio da frase", "falei com o Relatorio ontem", "Falei com o relatório ontem.", false},

		// A correção não abre porta para o resto.
		{"correção com negação apagada", "não mandei o relatorio para ele", "Mandei o relatório para ele.", false},
		{"correção com palavra acrescentada", "o relatorio ficou pronto ontem", "O relatório já ficou pronto ontem.", false},
		{"correção com palavra comum apagada", "então o relatorio ficou pronto", "O relatório ficou pronto.", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := guardaPalavras(c.entrada, c.saida, PolishMaxEdicao)
			if (got == MotivoNenhum) != c.passa {
				t.Errorf("guardaPalavras(%q, %q) = %q, quero passa=%v", c.entrada, c.saida, got, c.passa)
			}
		})
	}
}

// Quando nem a correção salva a saída, o motivo é o da conta sem correção,
// o mesmo de antes da emenda: a frase acrescentada num ditado longo segue
// "trecho_novo", com a correção de grafia contada como palavra trocada.
func TestGuardaPalavras_CorrecaoNaoMudaOMotivo(t *testing.T) {
	entrada := "o relatorio do mês vai para o cliente amanhã cedo sem falta porque ele precisa " +
		"fechar o balanço da empresa antes da reunião com o banco na semana que vem e " +
		"a gente combinou de mandar tudo junto com as notas"
	saida := "O relatório do mês vai para o cliente amanhã cedo sem falta, porque ele precisa " +
		"fechar o balanço da empresa antes da reunião com o banco na semana que vem, e " +
		"a gente combinou de mandar tudo junto com as notas e os recibos."
	if got := guardaPalavras(entrada, saida, PolishMaxEdicao); got != MotivoTrechoNovo {
		t.Errorf("motivo = %q, quero %q", got, MotivoTrechoNovo)
	}
}

func TestGrafiaParecida(t *testing.T) {
	casos := []struct {
		de, para string
		quero    bool
	}{
		{"relatorio", "relatório", true},
		{"vejante", "vigente", true},
		{"scriptes", "scripts", true},
		{"ate", "até", true},  // até 3 letras, só o acento
		{"ate", "ato", false}, // até 3 letras, a letra não
		{"pagu", "pago", true},
		{"pagu", "pegou", false},
		{"esocial", "social", false}, // primeira letra
		{"vejante", "vingança", false},
		{"igual", "igual", false},
	}
	for _, c := range casos {
		if got := grafiaParecida(c.de, c.para); got != c.quero {
			t.Errorf("grafiaParecida(%q, %q) = %v, quero %v", c.de, c.para, got, c.quero)
		}
	}
}

func TestPelaPronuncia(t *testing.T) {
	casos := map[string]string{
		"vigente": "vijente", "Sazonalidade": "sasonalidade", "ação": "asao",
		"chave": "xave", "cessão": "sesao", "passo": "paso",
	}
	for de, quero := range casos {
		if got := pelaPronuncia(de); got != quero {
			t.Errorf("pelaPronuncia(%q) = %q, quero %q", de, got, quero)
		}
	}
}

func TestDistanciaDeEdicao(t *testing.T) {
	casos := []struct {
		a, b  string
		quero int
	}{
		{"", "", 0}, {"a", "", 1}, {"", "abc", 3}, {"vejante", "vigente", 3},
		{"relatorio", "relatorio", 0}, {"ação", "acao", 2},
	}
	for _, c := range casos {
		if got := distanciaDeEdicao(c.a, c.b); got != c.quero {
			t.Errorf("distanciaDeEdicao(%q, %q) = %d, quero %d", c.a, c.b, got, c.quero)
		}
	}
}
