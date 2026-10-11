package dictionary

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Este arquivo fecha o laço de aprendizado do lado da PERSISTÊNCIA: um
// alias confirmado pela pessoa precisa sobreviver ao restart, senão não
// é aprendizado, é esquecimento com passos extras.
//
// # Por que no JSON, e não em SQLite
//
// O 04-system-design.md aponta SQLite como persistência do Windows, e
// para histórico e escala é a escolha certa. Para o DICIONÁRIO, hoje,
// não é:
//
//   - são 4,4 KB e algumas dezenas de termos;
//   - a pessoa consegue ABRIR e auditar o que o produto aprendeu sobre
//     ela, o que num .db opaco seria impossível;
//   - o arquivo já está versionado, então cada aprendizado vira um diff
//     legível;
//   - não exige dependência de produção nova (CLAUDE.md, "não adicionar
//     dependência de produção sem justificar").
//
// Quando o dicionário crescer a ponto de a leitura linear incomodar — o
// mesmo ponto em que winruntime.limiarResolveLento começa a avisar — a
// troca para SQLite entra atrás do mesmo DictionaryRepository, que já é
// interface justamente para isso.
//
// # O painel também grava aqui
//
// Em 30-set-2026 o Mauro decidiu que o que o painel aprende (etapa 3) mora
// neste mesmo arquivo, com a origem "painel" (painel.go e arquivo.go). As
// tabelas local_dictionary_* e local_corrections do SQLite seguem vazias
// até a sincronização da nuvem (etapa 5). O dictlearn continua valendo para
// a curadoria pelo terminal, e os dois releem o arquivo antes de gravar.

// AcrescentarAlias adiciona alias à entrada de forma canônica canonica,
// devolvendo a semente atualizada.
//
// Não grava em disco: separar decidir de escrever é o que permite testar
// a regra sem tocar no sistema de arquivos, e o que garante que um erro
// de validação não deixe o arquivo meio escrito.
//
// A entrada é criada quando não existe: aprender "comite" -> "commit"
// precisa funcionar mesmo que "commit" nunca tenha sido cadastrado.
//
// restrito marca o alias como válido só dentro do domínio da entrada —
// ver DictionaryAlias.Restricted. Quem decide isso é quem chama, porque
// depende de o alias colidir ou não com palavra comum, e isso é
// julgamento humano.
func (s Semente) AcrescentarAlias(canonica, alias string, restrito bool) (Semente, error) {
	canonica = strings.TrimSpace(canonica)
	alias = strings.TrimSpace(alias)
	if canonica == "" || alias == "" {
		return s, fmt.Errorf("dictionary: forma canônica e alias não podem ser vazios")
	}
	// Alias igual à forma canônica é ruído: o match exato já resolve.
	if Normalize(alias) == Normalize(canonica) {
		return s, fmt.Errorf("dictionary: %q e %q são a mesma coisa depois de normalizar — não há o que aprender", alias, canonica)
	}

	copia := s
	copia.Entradas = append([]EntradaSemente(nil), s.Entradas...)

	for i, e := range copia.Entradas {
		if Normalize(e.Canonica) != Normalize(canonica) {
			continue
		}
		if jaTem(e.Alias, alias) || jaTem(e.AliasRestrito, alias) {
			return s, fmt.Errorf("dictionary: %q já é alias de %q", alias, e.Canonica)
		}
		if restrito {
			e.AliasRestrito = inserirOrdenado(e.AliasRestrito, alias)
		} else {
			e.Alias = inserirOrdenado(e.Alias, alias)
		}
		copia.Entradas[i] = e
		return copia, nil
	}

	// Entrada nova. Prioridade default no meio da faixa usada: alta o
	// bastante para o termo ser levado a sério, baixa o bastante para não
	// atropelar o que a pessoa priorizou à mão.
	nova := EntradaSemente{Canonica: canonica, Prioridade: PrioridadeTermoAprendido}
	if restrito {
		nova.AliasRestrito = []string{alias}
	} else {
		nova.Alias = []string{alias}
	}
	copia.Entradas = append(copia.Entradas, nova)
	return copia, nil
}

func jaTem(as []string, alias string) bool {
	for _, a := range as {
		if Normalize(a) == Normalize(alias) {
			return true
		}
	}
	return false
}

// inserirOrdenado mantém a lista em ordem alfabética para o diff do
// arquivo ficar estável: sem isso, cada aprendizado embaralharia a
// ordem e o `git diff` mostraria mudança onde não houve.
func inserirOrdenado(as []string, alias string) []string {
	out := append(append([]string(nil), as...), alias)
	sort.Strings(out)
	return out
}

// Gravar escreve a semente em caminho, de forma ATÔMICA: grava num
// arquivo temporário ao lado e só então renomeia por cima.
//
// Sem isso, uma queda de energia no meio da escrita deixaria o
// dicionário truncado — e um dicionário truncado é pior que nenhum,
// porque o Agent recusa subir com arquivo inválido (cmd/agent,
// abrirDicionario) e a pessoa perderia o ditado inteiro por causa de um
// alias.
func (s Semente) Gravar(caminho string) (err error) {
	dados, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("dictionary: serializando o dicionário: %w", err)
	}
	dados = append(dados, '\n')

	dir := filepath.Dir(caminho)
	tmp, err := os.CreateTemp(dir, ".dicionario-*.json")
	if err != nil {
		return fmt.Errorf("dictionary: criando temporário em %s: %w", dir, err)
	}
	nomeTmp := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(nomeTmp)
		}
	}()

	if _, err = tmp.Write(dados); err != nil {
		return fmt.Errorf("dictionary: escrevendo %s: %w", nomeTmp, err)
	}
	// Sync antes do rename: o rename ser atômico não ajuda se os bytes
	// ainda estiverem só no cache do sistema.
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("dictionary: sincronizando %s: %w", nomeTmp, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("dictionary: fechando %s: %w", nomeTmp, err)
	}
	if err = renomearComTentativas(nomeTmp, caminho); err != nil {
		return fmt.Errorf("dictionary: substituindo %s: %w", caminho, err)
	}
	return nil
}

// renomear e pausasEntreRenames são variáveis só para o teste trocá-las.
var (
	renomear           = os.Rename
	pausasEntreRenames = []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 160 * time.Millisecond}
)

// renomearComTentativas renomeia de para para, e tenta de novo, com pausas
// curtas (no total, menos de meio segundo), quando o destino está preso por
// outro processo.
//
// No Windows o rename por cima de um arquivo que alguém tem aberto falha com
// "Acesso negado" ou violação de compartilhamento, e quem o abre por um
// instante é o que menos se espera: o antivírus, o indexador de busca, o
// dictlearn lendo. A falha é segura (o arquivo fica como estava e o
// temporário é apagado), mas o clique do painel falharia por um tropeço de
// milissegundos. Só o que parece esse tropeço é repetido: qualquer outro
// erro (pasta apagada, disco cheio) volta na hora.
func renomearComTentativas(de, para string) error {
	err := renomear(de, para)
	for _, pausa := range pausasEntreRenames {
		if err == nil || !destinoPreso(err) {
			return err
		}
		time.Sleep(pausa)
		err = renomear(de, para)
	}
	return err
}

// destinoPreso diz se err é o de um arquivo aberto por outro processo:
// permissão negada, ou ERROR_SHARING_VIOLATION (32) do Windows.
func destinoPreso(err error) bool {
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == 32
}
