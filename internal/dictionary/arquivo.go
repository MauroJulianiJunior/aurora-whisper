package dictionary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
)

// Arquivo é o dicionario.json como o painel o usa: cada operação RELÊ o
// arquivo, aplica a mudança (painel.go) e o grava de volta, de forma
// atômica (Semente.Gravar: temporário ao lado e rename).
//
// # Por que relê a cada vez
//
// O Agent só tem a cópia do dicionário que carregou na subida, e o dictlearn
// (outro processo) pode ter gravado alias desde então. Gravar a partir da
// cópia da subida apagaria o que o dictlearn gravou no meio. Com a leitura
// fresca, cada um acrescenta ao que o outro deixou.
//
// Sobra uma janela pequena, de um processo ler enquanto o outro renomeia: é
// do tamanho de uma escrita em disco. O dictlearn também relê o arquivo
// logo antes de gravar, DEPOIS da pergunta "Confirma?" (que espera uma
// pessoa e pode levar minutos): sem isso a janela seria o tempo da resposta,
// e um clique do painel nesse meio tempo sumiria do disco. Um arquivo de
// trava exigiria os dois cooperarem; não vale, por ora.
//
// # dictlearn e dicionário do repositório
//
// O dictlearn só cai neste mesmo arquivo se for chamado com
// -dict bin/dicionario.json. O padrão dele é o dicionário versionado do
// repositório (testdata/asr-dataset/dicionario.json), que o asrstage copia
// para bin/ UMA vez (e não mais, se bin/dicionario.json já existe): o que o
// dictlearn grava no padrão não chega ao Agent sozinho. O asrstage avisa
// quando os dois divergem.
//
// # O arquivo que não é nosso
//
// Arquivo ausente, inválido (JSON quebrado, campo desconhecido, sem usuario,
// ou com qualquer conteúdo depois do objeto) ou de outro usuário NUNCA é tocado: a operação falha antes de qualquer
// escrita. O Agent recusa subir com arquivo inválido pelo mesmo motivo.
//
// # Formato
//
// Gravar escreve o JSON indentado em 2 espaços, terminado em nova linha, com
// os campos na ordem da struct. Num arquivo que já está assim (o que o
// dictlearn deixa, e o que o repositório versiona), aprender e desfazer
// devolvem os mesmos bytes de antes. Um arquivo formatado à mão de outro
// jeito é reformatado na primeira gravação, como o dictlearn já faz: o
// conteúdo não muda, o espaçamento sim.
//
// # Os erros não trazem o conteúdo do dicionário
//
// Nenhum texto de erro daqui traz termo ou variante: a recusa de regra tem
// mensagem fixa, e o que falha na validação vem com a mensagem fixa
// ErrNaoCarrega (a causa fica em Unwrap, para errors.Is, e não no texto). A
// causa estrutural de um JSON inválido (posição, tipo, no máximo um caractere
// de sintaxe) segue no texto de ErrArquivoInvalido. Uma exceção por desenho:
// os erros *ErroVarianteDeOutroTermo e *ErroTermoEVarianteDeOutro levam o
// Dono (um termo do dicionário) em campo, para a tela explicar; quem os
// trata não o loga.
//
// Seguro de qualquer goroutine: as operações que gravam se serializam.
type Arquivo struct {
	// Caminho é o dicionario.json.
	Caminho string
	// Usuario, se não vazio, é o dono que o Agent carregou na subida. Um
	// arquivo de outro usuário (alguém o trocou com o Agent rodando) é
	// recusado: o resolver busca pelo usuário da subida, e um dicionário de
	// outro dono deixaria de funcionar sem aviso.
	Usuario string
	// Validar, se não nil, vê a semente resultante ANTES de ela ser gravada.
	// O Agent o usa para provar que o dicionário novo carrega no repositório
	// em memória: o que não carrega nunca chega ao disco.
	Validar func(Semente) error
	// ValidarAprendida, se não nil, vê a variante que Aprender acabou de
	// acrescentar, junto com a semente resultante, ANTES de gravar. O Agent o
	// usa para provar que a variante AGE: que o Smart, diante dela sozinha,
	// escreve o termo. ErrNaoAgiria recusa sem gravar; qualquer outro erro é
	// tratado como Validar (ErrNaoCarrega). Não vale para Desfazer.
	ValidarAprendida func(Semente, Aprendida) error
	// ConferirMudanca, se não nil, vê a semente de antes e a de depois de uma
	// operação do "Adicionar termo" (AdicionarTermo, DicaDoTermo,
	// RemoverTermo), com as variantes que ela acrescentou, ANTES de gravar
	// (ADR-020). O Agent o usa para provar que cada variante nova age
	// (*ErroNaoAgiria) e que nenhuma variante que agia antes deixa de agir
	// (*ErroQuebraria): os dois recusam sem gravar e chegam a quem chama como
	// estão. Qualquer outro erro é tratado como Validar (ErrNaoCarrega).
	ConferirMudanca func(antes, depois Semente, novas []Aprendida) error
	// PalavraComum, se não nil, diz se um texto é comum demais para virar
	// termo (grafia falso) ou variante (grafia verdadeiro) pelo "Adicionar
	// termo": devolve o motivo, ou "" (ver Semente.AdicionarTermo). O Agent
	// passa learning.Arriscada para o termo e learning.GrafiaArriscada, mais
	// estrita, para a grafia.
	PalavraComum func(texto string, grafia bool) string
	// ValidarPar, se não nil, vê cada par que AplicarPares (lote.go) vai
	// gravar, com a semente acumulada do lote, ANTES de gravar: o par recebido
	// da nuvem tem de AGIR aqui, em cada domínio onde ele age neste PC (a
	// restrição efetiva está no ParDoPainel). ErrNaoAgiria deixa o par "não
	// aplicado"; qualquer outro erro é tratado como Validar (ErrNaoCarrega) e
	// nada é gravado.
	ValidarPar func(Semente, ParDoPainel) error

	mu sync.Mutex
}

// Erros do arquivo. Os de quem chama (vazio, já existe, de outro termo...)
// estão em painel.go.
var (
	// ErrSemArquivo: não há dicionario.json. O Arquivo não cria um: o usuário
	// dono do dicionário vem dele, e um arquivo inventado não teria. Quem o
	// cria é o Agent, na subida, só se faltar (CriarSeFaltar, ADR-018).
	ErrSemArquivo = errors.New("dictionary: dicionário não encontrado")
	// ErrArquivoInvalido: o arquivo existe e não carrega. Ele não é tocado.
	ErrArquivoInvalido = errors.New("dictionary: dicionário inválido")
	// ErrUsuarioDiferente: o usuário do arquivo não é o que o Agent carregou.
	ErrUsuarioDiferente = errors.New("dictionary: o usuário do dicionário mudou; reinicie o Agent")
	// ErrNaoCarrega: Validar recusou a semente resultante, e nada foi gravado.
	// A causa está em Unwrap, e não no texto: ela pode citar termo e alias.
	ErrNaoCarrega = errors.New("dictionary: o dicionário resultante não carrega, nada foi gravado")
)

// erroNaoCarrega é o erro de Validar com mensagem fixa e a causa em Unwrap.
type erroNaoCarrega struct{ causa error }

func (e *erroNaoCarrega) Error() string      { return ErrNaoCarrega.Error() }
func (e *erroNaoCarrega) Unwrap() error      { return e.causa }
func (e *erroNaoCarrega) Is(alvo error) bool { return alvo == ErrNaoCarrega }

// Aprender acrescenta variante ao termo e grava. Devolve a semente gravada,
// para quem chama montar o dicionário em memória a partir do que está no
// disco.
func (a *Arquivo) Aprender(termo, variante string) (Semente, Aprendida, error) {
	return a.alterar(func(s Semente) (Semente, Aprendida, error) {
		return s.AprenderVariante(termo, variante)
	}, a.ValidarAprendida)
}

// Desfazer tira a variante que o painel aprendeu e grava.
func (a *Arquivo) Desfazer(variante string) (Semente, Aprendida, error) {
	return a.alterar(func(s Semente) (Semente, Aprendida, error) {
		return s.DesfazerVariante(variante)
	}, nil)
}

// Aprendidas lista o que o painel aprendeu, segundo o arquivo de agora. Só
// lê, mas espera uma gravação em curso: no Windows, renomear por cima de um
// arquivo que alguém está lendo falha.
func (a *Arquivo) Aprendidas() ([]Aprendida, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.ler()
	if err != nil {
		return nil, err
	}
	return s.VariantesDoPainel(), nil
}

func (a *Arquivo) alterar(op func(Semente) (Semente, Aprendida, error), conferir func(Semente, Aprendida) error) (Semente, Aprendida, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	atual, err := a.ler()
	if err != nil {
		return Semente{}, Aprendida{}, err
	}
	novo, feito, err := op(atual)
	if err != nil {
		return Semente{}, Aprendida{}, err
	}
	if a.Validar != nil {
		if err := a.Validar(novo); err != nil {
			return Semente{}, Aprendida{}, &erroNaoCarrega{causa: err}
		}
	}
	if conferir != nil {
		if err := conferir(novo, feito); err != nil {
			if errors.Is(err, ErrNaoAgiria) {
				return Semente{}, Aprendida{}, ErrNaoAgiria
			}
			return Semente{}, Aprendida{}, &erroNaoCarrega{causa: err}
		}
	}
	if err := novo.Gravar(a.Caminho); err != nil {
		return Semente{}, Aprendida{}, err
	}
	return novo, feito, nil
}

// ler carrega o arquivo de agora, sem tocá-lo.
func (a *Arquivo) ler() (Semente, error) {
	dados, err := os.ReadFile(a.Caminho)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Semente{}, ErrSemArquivo
		}
		return Semente{}, fmt.Errorf("dictionary: lendo o dicionário: %w", err)
	}
	s, err := CarregarSemente(bytes.NewReader(dados))
	if err != nil {
		return Semente{}, fmt.Errorf("%w: %v", ErrArquivoInvalido, err)
	}
	// CarregarSemente lê só o primeiro valor JSON. Gravar escreve o arquivo
	// inteiro por cima, e o que viesse depois do objeto (um segundo objeto,
	// marcas de conflito do git) sumiria em silêncio: arquivo que não é todo
	// nosso não é tocado.
	if temConteudoDepoisDoObjeto(dados) {
		return Semente{}, fmt.Errorf("%w: há conteúdo depois do objeto JSON", ErrArquivoInvalido)
	}
	if a.Usuario != "" && s.Usuario != a.Usuario {
		return Semente{}, ErrUsuarioDiferente
	}
	return s, nil
}

// temConteudoDepoisDoObjeto diz se, depois do primeiro valor JSON, há algo
// além de espaço em branco. dados já foi lido com sucesso por
// CarregarSemente.
func temConteudoDepoisDoObjeto(dados []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(dados))
	var primeiro, resto json.RawMessage
	if err := dec.Decode(&primeiro); err != nil {
		return false
	}
	return dec.Decode(&resto) != io.EOF
}
