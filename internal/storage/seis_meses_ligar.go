package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// O Ligar, o Desligar, a cópia e o freio dos 6 meses (ADR-019, "Decisões da
// F10", F10c). Quem chama cada um é o ciclo do expurgo (internal/agent/expurgo),
// dentro do mutex dele: o Ligar e o Desligar de duas abas, e a rodada de hora
// em hora, nunca se cruzam.
//
// # As chaves (a tabela instalacao, sem migration)
//
// Não começam por "nuvem_": o Esquecer não as apaga, e o acessor da nuvem
// (Instalacao) não as alcança.
//   - ChaveRetencaoAceitaEm: o OK, o portão (seis_meses_sqlite.go);
//   - ChaveRetencaoRodadaEm: a última rodada do ciclo, o freio
//     (lixeira_nuvem.go);
//   - ChaveRetencaoCopia: a cópia feita no OK (o nome do arquivo, sem a
//     pasta);
//   - ChaveRetencaoPausadaEm: quando o freio pausou a regra (a tela diz que
//     foi pausada).

const (
	// ChaveRetencaoCopia: o nome (sem a pasta) da cópia de antes dos 6 meses.
	// Uma só: os OKs seguintes não copiam enquanto ela (a chave e o arquivo)
	// existir.
	ChaveRetencaoCopia = "retencao_6m_copia"
	// ChaveRetencaoPausadaEm: quando o freio pausou a regra. O Ligar e o
	// Desligar a tiram.
	ChaveRetencaoPausadaEm = "retencao_6m_pausada_em"
)

// marcaDaCopiaDosSeisMeses separa, no nome da cópia do Ligar, o nome do banco
// da hora.
const marcaDaCopiaDosSeisMeses = ".antes-dos-6-meses-"

// CaminhoDaCopiaDosSeisMeses é onde o Ligar copia o banco antes de apagar: ao
// lado dele, com a hora local no nome, por exemplo
// historico.db.antes-dos-6-meses-20261008-153000.db.
func CaminhoDaCopiaDosSeisMeses(caminhoBanco string, agora time.Time) string {
	return caminhoBanco + marcaDaCopiaDosSeisMeses + agora.Format("20060102-150405") + ".db"
}

// ErrNadaAApagar: o Ligar recontou e não há nada fora da janela. Nada foi
// feito: nem cópia, nem OK (a regra continua desligada, e a pergunta volta no
// dia em que houver o que apagar).
var ErrNadaAApagar = errors.New("storage: não há nada a apagar agora")

// ErrPreviaMudou: o Ligar recontou e o número não é o que a tela mostrou.
// Nada foi feito; a tela troca a frase pelo número novo (Previa) antes de
// apagar.
type ErrPreviaMudou struct{ Previa int }

func (e *ErrPreviaMudou) Error() string {
	return fmt.Sprintf("storage: a contagem dos 6 meses mudou para %d", e.Previa)
}

// ErrLigadoIncompleto: o OK foi gravado e o primeiro lote saiu, mas um lote
// seguinte falhou (o pedido cancelado, o banco ocupado). Não é uma recusa: a
// regra está LIGADA, o ResultadoDoLigar vem com Incompleto e os Apagados até
// ali, e o resto sai na rodada seguinte do ciclo (em até 1 hora). O erro
// embrulha a causa.
var ErrLigadoIncompleto = errors.New("storage: os 6 meses ligaram, mas nem tudo saiu")

// ResultadoDoLigar é o que o Ligar fez. Só contagens e o nome da cópia.
type ResultadoDoLigar struct {
	// JaLigada: a regra já estava ligada; nada foi feito.
	JaLigada bool
	// Previa: a recontagem, com o mesmo agora do apagar.
	Previa int
	// Apagados: os ditados apagados agora. Igual a Previa, menos quando
	// Incompleto.
	Apagados int
	// Incompleto: o OK foi gravado, mas um lote seguinte falhou
	// (ErrLigadoIncompleto); Apagados é o que saiu até ali.
	Incompleto bool
	// Copia: o nome (sem a pasta) da cópia feita agora; vazio quando a cópia
	// registrada já existia (CopiaJaExistia).
	Copia          string
	CopiaJaExistia bool
}

// PassoDosSeisMeses é o que uma rodada do ciclo fez com os 6 meses. Só
// contagens.
type PassoDosSeisMeses struct {
	// SemDonoFixo: o banco não tem o dono fixo, e o passo pulou. Pulados é
	// quantas linhas (de qualquer dono) estão fora da janela.
	SemDonoFixo bool
	Pulados     int
	// Ligada: o OK estava gravado no começo da rodada.
	Ligada bool
	// Apagados: os ditados apagados nesta rodada.
	Apagados int
	// Pausada: o freio disparou nesta rodada (DiasParado desde a última), e a
	// regra foi pausada: nada apagado, o OK tirado. Previa é a contagem do
	// que teria saído.
	Pausada    bool
	DiasParado int
	Previa     int
}

// ganchoDoPrimeiroLote, se não for nil, roda na transação do OK do Ligar,
// depois de gravar as chaves e antes do primeiro lote; um erro dele desfaz a
// transação. Só os testes o ligam.
var ganchoDoPrimeiroLote func() error

// ganchoDepoisDoOK, se não for nil, roda logo depois de a transação do OK (e
// do primeiro lote) ser confirmada, antes dos lotes seguintes. Só os testes o
// ligam (para cancelar o pedido nesse ponto).
var ganchoDepoisDoOK func()

// Ligar é o OK dos 6 meses (ADR-019, "Decisões da F10", "O Ligar"), com o
// agora fixo e o previa que a tela mostrou, nesta ordem:
//  1. sem o dono fixo, ErrSemDonoFixo, antes de tudo; já ligada, não faz nada
//     (JaLigada);
//  2. reconta: com zero, ErrNadaAApagar; diferente do previa, *ErrPreviaMudou
//     com o número novo. Nos dois, nada é feito;
//  3. a cópia do banco, conferida tabela por tabela, se a registrada não
//     existir (a chave e o arquivo), e o registro dela logo depois. A cópia
//     que falha (*ErroDaCopia, com ErrCopiaDosSeisMeses) deixa tudo como
//     estava: nada apagado, nenhuma chave gravada;
//  4. numa transação só: grava o OK e a rodada, tira a pausa e apaga o
//     primeiro lote pelo mesmo portão (apagarLoteComPortao, que lê o OK
//     recém-gravado). Se o primeiro lote falha, o rollback leva o OK junto;
//  5. os lotes seguintes, cada um na sua transação, com o mesmo agora. Um
//     lote seguinte que falha não desfaz o OK nem o que já saiu:
//     ErrLigadoIncompleto, com o resultado marcado Incompleto (a regra está
//     ligada, e o resto sai na rodada seguinte).
func (s *SeisMeses) Ligar(ctx context.Context, agora time.Time, previa int) (ResultadoDoLigar, error) {
	if _, err := lerDonoFixo(ctx, s.db); err != nil {
		return ResultadoDoLigar{}, err
	}
	if _, ligada, err := lerChave(ctx, s.db, ChaveRetencaoAceitaEm); err != nil {
		return ResultadoDoLigar{}, err
	} else if ligada {
		return ResultadoDoLigar{JaLigada: true}, nil
	}
	c, err := s.PreviaDosSeisMeses(ctx, agora)
	if err != nil {
		return ResultadoDoLigar{}, err
	}
	if c.Total == 0 {
		return ResultadoDoLigar{}, ErrNadaAApagar
	}
	if c.Total != previa {
		return ResultadoDoLigar{}, &ErrPreviaMudou{Previa: c.Total}
	}
	r := ResultadoDoLigar{Previa: c.Total}

	existe, err := s.copiaExiste(ctx)
	if err != nil {
		return ResultadoDoLigar{}, err
	}
	if existe {
		r.CopiaJaExistia = true
	} else {
		if s.caminhoBanco == "" {
			return ResultadoDoLigar{}, &ErroDaCopia{Motivo: ErrCopiaDosSeisMeses, Detalhe: "sem o caminho do banco", base: ErrCopiaDosSeisMeses}
		}
		destino := CaminhoDaCopiaDosSeisMeses(s.caminhoBanco, agora)
		if err := copiarSegurando(ctx, s.db, destino, marcaDaCopiaDosSeisMeses, ErrCopiaDosSeisMeses); err != nil {
			return ResultadoDoLigar{}, err
		}
		r.Copia = filepath.Base(destino)
		// Registrada logo depois de conferida: o clique de novo, depois de
		// uma queda, não copia outra vez.
		if err := gravarChave(ctx, s.db, ChaveRetencaoCopia, r.Copia); err != nil {
			return ResultadoDoLigar{}, err
		}
	}

	var apagados, lidos int
	var ultimo cursorDosSeisMeses
	err = naTransacao(ctx, s.db, func(tx *sql.Tx) error {
		for nome, valor := range map[string]string{ChaveRetencaoAceitaEm: emTexto(agora), ChaveRetencaoRodadaEm: emTexto(agora)} {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
				nome, valor); err != nil {
				return fmt.Errorf("storage: gravando o OK dos 6 meses: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome = ?`, ChaveRetencaoPausadaEm); err != nil {
			return fmt.Errorf("storage: tirando a pausa dos 6 meses: %w", err)
		}
		if ganchoDoPrimeiroLote != nil {
			if err := ganchoDoPrimeiroLote(); err != nil {
				return err
			}
		}
		var err error
		apagados, lidos, ultimo, err = apagarLoteComPortao(ctx, tx, agora, cursorDosSeisMeses{})
		return err
	})
	if err != nil {
		return ResultadoDoLigar{}, err
	}
	if ganchoDepoisDoOK != nil {
		ganchoDepoisDoOK()
	}
	r.Apagados = apagados
	if lidos >= LoteDosSeisMeses {
		r.Apagados, err = s.apagarDesde(ctx, agora, ultimo, apagados)
		if err != nil {
			// O OK já está gravado e parte já saiu: a regra está ligada, e a
			// rodada seguinte do ciclo continua. Não é recusa.
			r.Incompleto = true
			return r, fmt.Errorf("%w: %w", ErrLigadoIncompleto, err)
		}
	}
	return r, nil
}

// copiaExiste diz se a cópia registrada (ChaveRetencaoCopia) ainda está ao
// lado do banco. Apagada (pelo botão, ou à mão), o próximo OK copia de novo.
func (s *SeisMeses) copiaExiste(ctx context.Context) (bool, error) {
	nome, ok, err := lerChave(ctx, s.db, ChaveRetencaoCopia)
	if err != nil || !ok || s.caminhoBanco == "" {
		return false, err
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(s.caminhoBanco), filepath.Base(nome)))
	return err == nil, nil
}

// Desligar volta ao de antes do OK: tira o OK, a rodada e a pausa, numa
// transação. Fica a cópia (ChaveRetencaoCopia). O que já saiu não volta.
func (s *SeisMeses) Desligar(ctx context.Context) error {
	return naTransacao(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome IN (?, ?, ?)`,
			ChaveRetencaoAceitaEm, ChaveRetencaoRodadaEm, ChaveRetencaoPausadaEm); err != nil {
			return fmt.Errorf("storage: desligando os 6 meses: %w", err)
		}
		return nil
	})
}

// GravarRodada grava agora como a última rodada do ciclo (o freio compara a
// próxima com ela). O ciclo a chama ao fim de toda rodada, com ou sem o OK,
// menos a rodada que pausou e a em que o Passo falhou.
func (s *SeisMeses) GravarRodada(ctx context.Context, agora time.Time) error {
	return gravarChave(ctx, s.db, ChaveRetencaoRodadaEm, emTexto(agora))
}

// Passo é os 6 meses numa rodada do ciclo, com agora:
//   - sem o dono fixo: pula (SemDonoFixo), e conta o que pulou;
//   - sem o OK: nada;
//   - com o OK e o freio disparado (a última rodada há mais de 7 dias: o
//     Agent parado, ou o relógio que pulou): pausa, na mesma transação que
//     leu o freio: tira o OK e a rodada, grava a pausa, e nada é apagado;
//   - com o OK: apaga o que passou da janela (ApagarForaDaJanela: cada lote lê
//     o OK de novo).
func (s *SeisMeses) Passo(ctx context.Context, agora time.Time) (PassoDosSeisMeses, error) {
	agora = agora.UTC()
	var r PassoDosSeisMeses
	err := naTransacao(ctx, s.db, func(tx *sql.Tx) error {
		r = PassoDosSeisMeses{}
		if _, err := lerDonoFixo(ctx, tx); errors.Is(err, ErrSemDonoFixo) {
			r.SemDonoFixo = true
			r.Pulados, err = contarForaDaJanelaDeTodos(ctx, tx, agora)
			return err
		} else if err != nil {
			return err
		}
		_, ligada, err := lerChave(ctx, tx, ChaveRetencaoAceitaEm)
		if err != nil || !ligada {
			return err
		}
		r.Ligada = true
		v, temRodada, err := lerChave(ctx, tx, ChaveRetencaoRodadaEm)
		if err != nil {
			return err
		}
		freio := false
		if temRodada {
			em, err := deTempo(v)
			if err != nil {
				// Ilegível: freia, como na lixeira. A pausa tira o valor.
				freio = true
			} else if retention.FreioDaRodada(&em, agora) {
				freio = true
				r.DiasParado = retention.DiasParado(&em, agora)
			}
		}
		if !freio {
			return nil
		}
		r.Pausada = true
		if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome IN (?, ?)`,
			ChaveRetencaoAceitaEm, ChaveRetencaoRodadaEm); err != nil {
			return fmt.Errorf("storage: pausando os 6 meses: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
			ChaveRetencaoPausadaEm, emTexto(agora)); err != nil {
			return fmt.Errorf("storage: pausando os 6 meses: %w", err)
		}
		c, err := previaDosSeisMeses(ctx, tx, agora)
		r.Previa = c.Total
		return err
	})
	if err != nil || !r.Ligada || r.Pausada {
		return r, err
	}
	r.Apagados, err = s.ApagarForaDaJanela(ctx, agora)
	return r, err
}

// contarForaDaJanelaDeTodos conta as linhas de qualquer dono fora da janela:
// o que o passo pulou sem o dono fixo.
func contarForaDaJanelaDeTodos(ctx context.Context, tx *sql.Tx, agora time.Time) (int, error) {
	n := 0
	var depois cursorDosSeisMeses
	for {
		// Todo user_id é diferente do vazio (a coluna é NOT NULL, e nada
		// grava vazio): deOutroDono com o dono vazio lê todas.
		vencidos, lidos, ultimo, err := foraDaJanela(ctx, tx, "", true, agora, depois)
		if err != nil {
			return 0, err
		}
		n += len(vencidos)
		if lidos < LoteDosSeisMeses {
			return n, nil
		}
		depois = ultimo
	}
}
