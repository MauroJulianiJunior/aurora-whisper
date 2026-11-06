package syncengine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O dicionário na rodada (ADR-023, "F. O PC: a opção, o envio e o aviso"): uma
// fase depois do pull e do download das imagens, só com a opção "Levar o
// dicionário para o celular" ligada. Ela monta a cópia do dicionario.json de
// agora, e, se o SHA-256 não é o último que a nuvem aceitou, manda o PUT ...
// manter; a resposta vira uma ação em DecidirEnvioDoDicionario (pura).
//
// Fora de qualquer transação, e nunca segura o resto do sync: só o 401 (o token
// caiu) e o contexto cancelado voltam como erro; a rede que cai, o 429 e o
// redirecionamento só param a fase, e a rodada seguinte tenta de novo (a mesma
// regra das imagens, paraAFase).
//
// A memória da fase (CopiaDoDicionario) mora no laço, e não no motor de cada
// rodada: o último SHA-256 aceito e o 404 valem até o Agent reiniciar.

// FonteDaCopia é o dicionario.json deste PC como a cópia o lê: o dicionário de
// agora, sem tocar no arquivo. Em produção, o *aprendiz.Aplicador
// (Dicionario(ctx)). dictionary.ErrSemArquivo é o dicionário que o Agent não
// conseguiu abrir.
type FonteDaCopia interface {
	Dicionario(ctx context.Context) (dictionary.Semente, error)
}

// OpcaoDaCopia é a opção "Levar o dicionário para o celular" e o que a fase
// conta ao painel. Em produção, o nuvem.Servico (pela cmd/agent).
type OpcaoDaCopia interface {
	// Ligada diz se a opção está ligada neste PC.
	Ligada(ctx context.Context) (bool, error)
	// Desligar grava a opção desligada e guarda o aviso para o painel (a nuvem
	// passou a fonte para outro PC, ou apagou a cópia).
	Desligar(ctx context.Context, aviso string) error
	// Avisar guarda o aviso para o painel, sem mexer na opção.
	Avisar(aviso string)
	// Enviou conta que a nuvem aceitou a cópia (termos e hora); o aviso de antes
	// sai. Só memória.
	Enviou(termos int, quando time.Time)
}

// CopiaDoDicionario é a fase do dicionário e a memória dela: o último SHA-256
// que a nuvem aceitou, por pareamento, a cópia que a nuvem recusou e o 404.
// Seguro para uso concorrente (o laço roda uma rodada por vez, mas o painel lê
// em outra goroutine).
type CopiaDoDicionario struct {
	Fonte FonteDaCopia
	Opcao OpcaoDaCopia

	mu sync.Mutex
	// pareamento é de quem são shaAceito e shaRecusado: outro pareamento (a
	// reconexão, outra nuvem) começa do zero, e o primeiro envio é um 200 sem
	// gravar.
	pareamento string
	shaAceito  string
	// shaRecusado é a cópia que a nuvem recusou (422): só tenta de novo quando o
	// SHA-256 mudar.
	shaRecusado string
	// semRota: o 404; não tenta mais até reiniciar.
	semRota bool
	// foraDoLimite: o último aviso foi o de uma cópia fora dos limites; quando
	// ela voltar a caber, o aviso sai (mesmo que o SHA-256 seja o já aceito).
	foraDoLimite bool
}

// esquecer zera o que foi aceito e recusado. Com c.mu.
func (c *CopiaDoDicionario) esquecer() { c.shaAceito, c.shaRecusado = "", "" }

// clienteDoDicionario é o lado do dicionário do cliente da rodada, ou nil (o
// SyncClient sem ele deixa o dicionário de fora, como a nuvem sem a rota).
func (m *Motor) clienteDoDicionario() ClienteDoDicionario {
	if m.Copia == nil || m.Copia.Fonte == nil || m.Copia.Opcao == nil {
		return nil
	}
	c, _ := m.Cliente.(ClienteDoDicionario)
	return c
}

// EnviarDicionario é a fase do dicionário da rodada. Veja o comentário do
// arquivo.
func (m *Motor) EnviarDicionario(ctx context.Context, rel *Relatorio) error {
	cli := m.clienteDoDicionario()
	if cli == nil {
		return nil
	}
	c := m.Copia
	ligada, err := c.Opcao.Ligada(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if !ligada {
		// Desligada (ou desligada de volta): o que se lembrava já não vale.
		c.esquecer()
		c.mu.Unlock()
		return nil
	}
	if c.semRota {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	semente, err := c.Fonte.Dicionario(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Sem o arquivo (ou ilegível): nada a mandar agora.
		rel.Dicionario[TipoDicionarioSemArquivo]++
		return nil
	}
	copia, sha, err := semente.BytesDaCopia()
	if err != nil {
		var ec *dictionary.ErrCopia
		if errors.As(err, &ec) {
			// Fora dos limites: não sobe, e o painel diz o motivo. Sem rede, e a
			// rodada seguinte confere de novo (o arquivo pode ter mudado).
			c.mu.Lock()
			c.foraDoLimite = true
			c.mu.Unlock()
			c.Opcao.Avisar(FraseDoMotivoDaCopia(string(ec.Motivo)))
			rel.Dicionario[TipoDicionarioForaDoLimite]++
			return nil
		}
		rel.Dicionario[TipoDicionarioSemArquivo]++
		return nil
	}

	chave := m.Aparelho + "|" + m.PareadoEm
	c.mu.Lock()
	limparAviso := c.foraDoLimite
	c.foraDoLimite = false
	if c.pareamento != chave {
		c.pareamento = chave
		c.esquecer()
	}
	if limparAviso {
		defer c.Opcao.Avisar("")
	}
	switch sha {
	case c.shaAceito:
		c.mu.Unlock()
		rel.Dicionario[TipoDicionarioIgual]++
		return nil
	case c.shaRecusado:
		c.mu.Unlock()
		rel.Dicionario[TipoDicionarioEsperando]++
		return nil
	}
	c.mu.Unlock()

	resp, err := cli.SubirDicionario(ctx, copia, sha, EnvioManter)
	if parar, daRodada := paraAFase(err); parar {
		rel.Dicionario[TipoDicionarioAdiado]++
		return daRodada
	}
	d := DecidirEnvioDoDicionario(resp)
	rel.Dicionario[d.Tipo]++

	c.mu.Lock()
	switch {
	case d.Aceitar:
		c.shaAceito, c.shaRecusado = sha, ""
	case d.SoQuandoMudar:
		c.shaRecusado = sha
	case d.NaoTentarMais:
		c.semRota = true
	case d.Desligar:
		c.esquecer()
	}
	c.mu.Unlock()

	switch {
	case d.Aceitar:
		c.Opcao.Enviou(len(semente.Entradas), m.agora())
	case d.Desligar:
		if err := c.Opcao.Desligar(ctx, d.Aviso); err != nil {
			return err
		}
	case d.Aviso != "":
		c.Opcao.Avisar(d.Aviso)
	}
	return nil
}
