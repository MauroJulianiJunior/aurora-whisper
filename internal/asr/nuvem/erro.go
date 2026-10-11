package nuvem

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Motivo classifica uma falha do provedor, para quem precisa decidir por ela.
// É uma lista fechada; os valores iguais aos do internal/ia dizem a mesma
// coisa (quem traduz para o código da rota é o servidor).
type Motivo string

const (
	// MotivoSemChave: não há chave para usar (nenhuma foi guardada, a função
	// que a entrega falhou ou devolveu vazio). Nada foi enviado.
	MotivoSemChave Motivo = "sem_chave"
	// MotivoChaveRecusada: o provedor respondeu 401, ou a chave tem caracteres
	// que uma chave não tem (nesse caso nada foi enviado).
	MotivoChaveRecusada Motivo = "chave_recusada"
	// MotivoSemPermissao: 403. A chave vale, mas não alcança este recurso.
	MotivoSemPermissao Motivo = "sem_permissao"
	// MotivoLimite: 429. Limite de uso, de pedidos ou falta de crédito.
	MotivoLimite Motivo = "limite"
	// MotivoArquivoGrande: o áudio passa do teto do provedor (413, ou os
	// TetoArquivo bytes, conferidos antes de enviar).
	MotivoArquivoGrande Motivo = "arquivo_grande"
	// MotivoIndisponivel: 5xx.
	MotivoIndisponivel Motivo = "indisponivel"
	// MotivoTempoEsgotado: o prazo do contexto estourou.
	MotivoTempoEsgotado Motivo = "tempo_esgotado"
	// MotivoSemRede: não deu para falar com o provedor.
	MotivoSemRede Motivo = "sem_rede"
	// MotivoRespostaInesperada: qualquer outra resposta, inclusive um
	// redirecionamento (que nunca é seguido) e um corpo que não se lê.
	MotivoRespostaInesperada Motivo = "resposta_inesperada"
)

// Erro é uma falha do provedor já traduzida para quem usa. Não embrulha o
// erro de origem de propósito: um erro de rede carrega a URL e o que mais a
// biblioteca achar útil, e o corpo de uma resposta de erro pode trazer um
// pedaço da chave. A única causa embrulhada é context.DeadlineExceeded, para
// errors.Is funcionar com o prazo.
type Erro struct {
	Motivo   Motivo
	Provedor string
	Mensagem string

	causa error
}

func (e *Erro) Error() string { return e.Mensagem }

// Unwrap devolve só context.DeadlineExceeded (no tempo esgotado); nos outros
// motivos, nil.
func (e *Erro) Unwrap() error { return e.causa }

// ErrSemFala é o erro de uma chamada que o provedor atendeu (e pode ter
// cobrado) mas devolveu texto vazio, ou só segmentos de silêncio. Não é falha
// do provedor: o servidor responde SEM_FALA e nenhum ditado é criado.
var ErrSemFala = errors.New("asr/nuvem: o provedor não devolveu texto")

func novoErro(provedor string, m Motivo, mensagem string) *Erro {
	return &Erro{Motivo: m, Provedor: provedor, Mensagem: mensagem}
}

// falhaDeRede traduz um erro do transporte (ou da leitura do corpo). O erro
// original é descartado. Cancelamento pelo chamador não é falha do provedor:
// volta como context.Canceled puro.
func falhaDeRede(ctx context.Context, provedor string) error {
	nome := Nome(provedor)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		e := novoErro(provedor, MotivoTempoEsgotado, fmt.Sprintf(
			"A %s não respondeu a tempo. Confira a conexão com a internet e tente de novo.", nome))
		e.causa = context.DeadlineExceeded
		return e
	case errors.Is(ctx.Err(), context.Canceled):
		return context.Canceled
	}
	return novoErro(provedor, MotivoSemRede, fmt.Sprintf(
		"Não consegui falar com a %s. Confira a conexão com a internet.", nome))
}

// falhaHTTP traduz o status de uma resposta que não é 200. Só o status conta:
// o corpo nem é lido.
func falhaHTTP(provedor string, status int) error {
	nome := Nome(provedor)
	switch {
	case status == http.StatusUnauthorized:
		return novoErro(provedor, MotivoChaveRecusada, fmt.Sprintf(
			"A %s recusou a chave. Confira se ela foi copiada inteira e se ainda está ativa.", nome))
	case status == http.StatusForbidden:
		return novoErro(provedor, MotivoSemPermissao, fmt.Sprintf(
			"A %s aceitou a chave, mas negou o acesso. Confira as permissões da chave.", nome))
	case status == http.StatusRequestEntityTooLarge:
		return novoErro(provedor, MotivoArquivoGrande, fmt.Sprintf(
			"A %s recusou o áudio por ser grande demais.", nome))
	case status == http.StatusTooManyRequests:
		return novoErro(provedor, MotivoLimite, fmt.Sprintf(
			"A %s recusou por limite de uso ou falta de crédito. Confira o saldo da conta e tente de novo em alguns minutos.", nome))
	case status >= 500:
		return novoErro(provedor, MotivoIndisponivel, fmt.Sprintf(
			"A %s está com problema agora (HTTP %d). Tente de novo mais tarde.", nome, status))
	}
	return novoErro(provedor, MotivoRespostaInesperada, fmt.Sprintf(
		"A %s respondeu de um jeito inesperado (HTTP %d).", nome, status))
}

func respostaIlegivel(provedor string) error {
	return novoErro(provedor, MotivoRespostaInesperada, fmt.Sprintf(
		"A %s respondeu de um jeito inesperado: a resposta não pôde ser lida.", Nome(provedor)))
}
