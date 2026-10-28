package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// O estado dos 6 meses para a tela e as cópias ao lado do banco (ADR-019,
// "Decisões da F10", F10d e F10e).
//
// # As cópias
//
// Há três, cada uma com a sua marca no nome: a do dono fixo (marcaDaCopia), a
// do envio à nuvem (marcaDaCopiaDoEnvio) e a dos 6 meses
// (marcaDaCopiaDosSeisMeses). Todas guardam o texto inteiro do histórico,
// inclusive o que os 6 meses apagam: enquanto elas existem, a regra não vale
// de verdade no disco. Por isso o Agent NUNCA as apaga sozinho (D4): só o
// botão "Apagar as cópias", depois da pergunta na tela.
//
// ApagarCopias apaga só o arquivo REGULAR cujo nome casa EXATAMENTE com
// <banco>.antes-(do-dono|da-nuvem|dos-6-meses)-AAAAMMDD-HHMMSS.db, na pasta
// do banco: nunca o historico.db, o -wal, o -shm, outro .db, um nome
// parecido, uma pasta nem um link. A cópia do envio à nuvem que ainda não
// terminou (a nuvem_copia sem o nuvem_envio_aceito_em) não sai: o
// EnviarHistorico não copia de novo quando a chave existe, então apagá-la
// deixaria o clique seguinte seguir sem a cópia que a F7c exige.

// ErrSemCaminhoDoBanco: a regra foi montada sem o arquivo do banco, e não há
// ao lado do que procurar cópias.
var ErrSemCaminhoDoBanco = errors.New("storage: sem o caminho do banco")

// ResultadoDasCopias é o que ApagarCopias fez. Só contagens.
type ResultadoDasCopias struct {
	// Apagadas: os arquivos que saíram agora.
	Apagadas int
	// Ficaram: as cópias que continuam ao lado do banco (a do envio que ainda
	// não terminou, ou a que o sistema não deixou apagar).
	Ficaram int
}

// EstadoDosSeisMeses é o que a tela do bloco Histórico mostra: a prévia, se a
// regra está ligada ou foi pausada pelo freio, e quantas cópias há. Só
// contagens e booleanos.
type EstadoDosSeisMeses struct {
	ContagemDosSeisMeses
	// SemDonoFixo: o banco não tem o dono fixo (a regra antiga do escolherDono):
	// o passo pula, e a contagem fica em zero.
	SemDonoFixo bool
	// Ligada: o OK está gravado (ChaveRetencaoAceitaEm).
	Ligada bool
	// Pausada: o freio pausou a regra (ChaveRetencaoPausadaEm); a tela volta a
	// perguntar. Nunca vem junto com Ligada.
	Pausada bool
	// Copias: quantos arquivos de cópia há ao lado do banco.
	Copias int
	// CopiasApagaveis: das Copias, as que o botão "Apagar as cópias" apaga
	// agora (todas, menos a do envio à nuvem que ainda não terminou). A tela
	// pergunta por estas, e sem nenhuma não oferece o botão.
	CopiasApagaveis int
	// CopiaDosSeisMeses: a cópia de um OK anterior (a registrada em
	// ChaveRetencaoCopia) está ao lado do banco, e o próximo OK não copia de
	// novo. O que nasceu depois dela não está nela: a tela diz isso, em vez de
	// prometer que "uma cópia fica" com tudo o que sai.
	CopiaDosSeisMeses bool
}

// Estado lê, com agora, o que a tela do bloco Histórico mostra. A contagem, o
// OK, a pausa e os registros das cópias vêm de uma transação de leitura (uma
// foto só); as cópias, da pasta do banco. Sem o dono fixo não é erro: a
// contagem é zero e a tela não tem o que perguntar (SemDonoFixo).
func (s *SeisMeses) Estado(ctx context.Context, agora time.Time) (EstadoDosSeisMeses, error) {
	var e EstadoDosSeisMeses
	var protegida func(string) bool
	var copiaRegistrada string
	var temCopiaRegistrada bool
	err := naTransacao(ctx, s.db, func(tx *sql.Tx) error {
		e = EstadoDosSeisMeses{}
		var err error
		e.ContagemDosSeisMeses, err = previaDosSeisMeses(ctx, tx, agora)
		if errors.Is(err, ErrSemDonoFixo) {
			e.SemDonoFixo = true
			err = nil
		}
		if err != nil {
			return err
		}
		if _, e.Ligada, err = lerChave(ctx, tx, ChaveRetencaoAceitaEm); err != nil {
			return err
		}
		if _, e.Pausada, err = lerChave(ctx, tx, ChaveRetencaoPausadaEm); err != nil {
			return err
		}
		if copiaRegistrada, temCopiaRegistrada, err = lerChave(ctx, tx, ChaveRetencaoCopia); err != nil {
			return err
		}
		protegida, err = copiaDoEnvioEmCurso(ctx, tx)
		return err
	})
	if err != nil {
		return EstadoDosSeisMeses{}, err
	}
	// Ligada e pausada não andam juntas: o Ligar tira a pausa, e a pausa tira
	// o OK. Se uma corrida deixasse as duas, vale o OK.
	e.Pausada = e.Pausada && !e.Ligada
	if s.caminhoBanco != "" {
		nomes, err := CopiasAoLado(s.caminhoBanco)
		if err != nil {
			return EstadoDosSeisMeses{}, err
		}
		e.Copias = len(nomes)
		for _, nome := range nomes {
			if !protegida(nome) {
				e.CopiasApagaveis++
			}
		}
		// A mesma conta do Ligar (copiaExiste): a chave e o arquivo.
		if temCopiaRegistrada {
			_, err := os.Stat(filepath.Join(filepath.Dir(s.caminhoBanco), filepath.Base(copiaRegistrada)))
			e.CopiaDosSeisMeses = err == nil
		}
	}
	return e, nil
}

// copiaDoEnvioEmCurso lê, na transação, qual cópia é a do envio à nuvem que
// ainda não terminou (a nuvem_copia sem o nuvem_envio_aceito_em) e devolve quem
// a reconhece pelo nome: essa não sai pelo botão. Com o nome registrado
// ilegível (vazio), todas as do envio (errar para o lado de guardar). Sem envio
// em curso, nenhuma.
func copiaDoEnvioEmCurso(ctx context.Context, tx *sql.Tx) (func(nome string) bool, error) {
	guardada, tem, err := lerChave(ctx, tx, ChaveNuvemCopia)
	if err != nil {
		return nil, err
	}
	_, aceito, err := lerChave(ctx, tx, ChaveNuvemEnvioAceitoEm)
	if err != nil {
		return nil, err
	}
	envioEmCurso := tem && !aceito
	return func(nome string) bool {
		if !envioEmCurso {
			return false
		}
		if guardada == "" {
			return strings.Contains(nome, marcaDaCopiaDoEnvio)
		}
		return nome == filepath.Base(guardada)
	}, nil
}

// nomeDeCopia é o nome de uma cópia do banco de nome base, inteiro e sem
// sobra: uma das marcas dadas (sem nenhuma, as três), a data e a hora, e o
// .db.
func nomeDeCopia(base string, marcas ...string) *regexp.Regexp {
	if len(marcas) == 0 {
		marcas = []string{marcaDaCopia, marcaDaCopiaDoEnvio, marcaDaCopiaDosSeisMeses}
	}
	alternativas := make([]string, len(marcas))
	for i, m := range marcas {
		alternativas[i] = regexp.QuoteMeta(m)
	}
	return regexp.MustCompile(`^` + regexp.QuoteMeta(base) + `(?:` + strings.Join(alternativas, `|`) + `)` + `\d{8}-\d{6}\.db$`)
}

// ehCopia é a regra única do que é uma cópia do banco: arquivo regular cujo
// nome casa inteiro com casa (nomeDeCopia). Um link ou uma pasta com o nome
// da cópia não é uma cópia. Vale para a tela e o botão (CopiasAoLado) e para o
// limite de MaxCopias (copiasAnteriores).
func ehCopia(e fs.DirEntry, casa *regexp.Regexp) bool {
	return e.Type().IsRegular() && casa.MatchString(e.Name())
}

// semCaminho tira o caminho de um erro do sistema de arquivos (ele leva o
// nome de usuário do Windows, que não vai para log nem para a tela) e deixa só
// o motivo. O erro do rename (os.LinkError) leva os dois caminhos.
func semCaminho(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

// CopiasAoLado lista os nomes (sem a pasta) das cópias do histórico ao lado
// do banco: só arquivo regular, e só o nome que casa inteiro com o padrão. Em
// ordem.
func CopiasAoLado(caminhoBanco string) ([]string, error) {
	if caminhoBanco == "" {
		return nil, ErrSemCaminhoDoBanco
	}
	entradas, err := os.ReadDir(filepath.Dir(caminhoBanco))
	if err != nil {
		return nil, fmt.Errorf("storage: lendo a pasta do banco: %w", semCaminho(err))
	}
	casa := nomeDeCopia(filepath.Base(caminhoBanco))
	var nomes []string
	for _, e := range entradas {
		// Um link ou uma pasta com o nome da cópia não é uma cópia: nunca é
		// apagado por aqui.
		if ehCopia(e, casa) {
			nomes = append(nomes, e.Name())
		}
	}
	sort.Strings(nomes)
	return nomes, nil
}

// ApagarCopias apaga as cópias do histórico ao lado do banco (CopiasAoLado),
// menos a do envio à nuvem que ainda não terminou. Quem chama já fez a
// pergunta na tela (D4); o Agent nunca a chama sozinho. Tira a
// retencao_6m_copia quando o arquivo registrado não existe mais (o próximo
// OK copia de novo).
//
// Roda numa transação, segurando a conexão do banco: o registro e o aceite
// do envio são lidos no mesmo instante em que os arquivos saem, e nenhuma
// escrita (a gravação da nuvem_copia do envio, por exemplo) passa no meio.
//
// Um arquivo que o sistema não deixa apagar não para os outros: o resultado
// conta o que saiu e o que ficou, e o erro (sem o caminho) diz o primeiro.
func (s *SeisMeses) ApagarCopias(ctx context.Context) (ResultadoDasCopias, error) {
	if s.caminhoBanco == "" {
		return ResultadoDasCopias{}, ErrSemCaminhoDoBanco
	}
	var r ResultadoDasCopias
	var falha error
	pasta := filepath.Dir(s.caminhoBanco)
	err := naTransacao(ctx, s.db, func(tx *sql.Tx) error {
		r, falha = ResultadoDasCopias{}, nil
		nomes, err := CopiasAoLado(s.caminhoBanco)
		if err != nil {
			return err
		}
		// A do envio que ainda não terminou: a registrada. Sem nome legível, as
		// do envio inteiras (errar para o lado de guardar).
		protegida, err := copiaDoEnvioEmCurso(ctx, tx)
		if err != nil {
			return err
		}
		for _, nome := range nomes {
			if protegida(nome) {
				r.Ficaram++
				continue
			}
			err := os.Remove(filepath.Join(pasta, nome))
			switch {
			case err == nil:
				r.Apagadas++
			case errors.Is(err, fs.ErrNotExist):
				// Sumiu entre a listagem e agora: nada a apagar nem a contar.
			default:
				r.Ficaram++
				if falha == nil {
					falha = fmt.Errorf("storage: apagando uma cópia do histórico: %w", semCaminho(err))
				}
			}
		}
		// A cópia dos 6 meses registrada que não existe mais: a chave sai.
		registrada, ok, err := lerChave(ctx, tx, ChaveRetencaoCopia)
		if err != nil || !ok {
			return err
		}
		if _, err := os.Stat(filepath.Join(pasta, filepath.Base(registrada))); errors.Is(err, fs.ErrNotExist) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome = ?`, ChaveRetencaoCopia); err != nil {
				return fmt.Errorf("storage: tirando o registro da cópia dos 6 meses: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	return r, falha
}
