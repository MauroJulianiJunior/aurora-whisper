package dictionary

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Este arquivo cria o dicionario.json VAZIO que o Agent instalado precisa para
// o aprender do painel funcionar desde o primeiro dia (ADR-018, "O dicionário
// que não existe"; decisão do Mauro, 30-set-2026).
//
// O pacote do instalador não leva dicionário (o do repositório é pessoal), e o
// Arquivo (arquivo.go) nunca cria um: o usuário dono do dicionário vem do
// próprio arquivo, e ele recusa operar sobre o que não existe. Sem este passo,
// quem instala não aprenderia nada e não seria avisado.
//
// # Nunca sobrescreve
//
// Só cria se NADA existe no caminho. Um dicionário que já está lá, válido ou
// não, é de quem o pôs: o inválido o Agent recusa e o diz (abrirDicionarioEm),
// e quem o conserta é a pessoa. Uma pasta com o nome do arquivo também fica
// como está. A garantia não depende de olhar antes de criar (isso deixaria uma
// janela para outro processo, o dictlearn por exemplo): a criação em si
// recusa o caminho que já existe.
//
// # Atômico
//
// O arquivo nasce completo ou não nasce. Escreve num temporário ao lado e o
// LIGA ao nome final (os.Link, que falha se o destino existe), em vez de
// renomear por cima como Gravar faz: no Windows o rename substitui o que está
// lá, e aqui isso é exatamente o que não pode acontecer. Um dicionário
// truncado seria pior que nenhum, porque o Agent recusa subir com arquivo
// inválido.
//
// O link físico não existe em todo sistema de arquivos (FAT, exFAT, alguns
// compartilhamentos de rede). Quando falha por outro motivo que não "já existe",
// cai para criar o arquivo final de forma exclusiva (O_EXCL) e escrevê-lo de uma
// vez: o arquivo tem algumas dezenas de bytes, e se a escrita falhar ele é
// apagado, para não deixar um arquivo truncado.

// ligar e criarExclusivo são variáveis só para o teste trocá-las.
var (
	ligar = os.Link

	criarExclusivo = func(caminho string) (*os.File, error) {
		return os.OpenFile(caminho, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
)

// ErrUsuarioVazio: CriarSeFaltar sem o usuario do dicionário. O dicionário é
// pessoal e a busca filtra por ele (CarregarSemente recusa o arquivo sem).
var ErrUsuarioVazio = errors.New("dictionary: o dicionário novo precisa de um usuario")

// CriarSeFaltar cria em caminho um dicionário sem nenhum termo, do usuario
// dado, se e somente se nada existir ali. Devolve criou=true quando foi esta
// chamada que o criou; false, sem erro, quando já havia algo no caminho (que
// não é tocado).
//
// A pasta de caminho tem de existir. O formato é o de Semente.Gravar (JSON
// indentado em 2 espaços, terminado em nova linha), de modo que o primeiro
// Aprender e o Desfazer que o desfaz tratam o arquivo como qualquer outro, e o
// Desfazer devolve os mesmos bytes.
//
// Seguro de qualquer goroutine e de qualquer processo: dois chamadores ao mesmo
// tempo resultam em um criou=true e um arquivo íntegro.
func CriarSeFaltar(caminho, usuario string) (criou bool, err error) {
	usuario = strings.TrimSpace(usuario)
	if usuario == "" {
		return false, ErrUsuarioVazio
	}
	// Lstat, e não Stat: um link simbólico quebrado nesse nome também é "algo
	// que existe", e não se escreve por cima dele.
	if _, err := os.Lstat(caminho); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("dictionary: olhando o dicionário: %w", err)
	}

	// Entradas é uma lista vazia, não nil: nil sairia como "entradas": null.
	dados, err := json.MarshalIndent(Semente{Usuario: usuario, Entradas: []EntradaSemente{}}, "", "  ")
	if err != nil {
		return false, fmt.Errorf("dictionary: serializando o dicionário vazio: %w", err)
	}
	dados = append(dados, '\n')

	dir := filepath.Dir(caminho)
	tmp, err := os.CreateTemp(dir, ".dicionario-*.json")
	if err != nil {
		return false, fmt.Errorf("dictionary: criando temporário em %s: %w", dir, err)
	}
	nomeTmp := tmp.Name()
	// O temporário não fica para trás, deu certo ou não.
	defer os.Remove(nomeTmp)

	if _, err = tmp.Write(dados); err != nil {
		tmp.Close()
		return false, fmt.Errorf("dictionary: escrevendo %s: %w", nomeTmp, err)
	}
	// Sync antes de ligar: o link ser atômico não ajuda se os bytes ainda
	// estiverem só no cache do sistema.
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return false, fmt.Errorf("dictionary: sincronizando %s: %w", nomeTmp, err)
	}
	if err = tmp.Close(); err != nil {
		return false, fmt.Errorf("dictionary: fechando %s: %w", nomeTmp, err)
	}

	switch err = ligar(nomeTmp, caminho); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrExist):
		// Outro processo criou entre o Lstat e o link: o dele fica.
		return false, nil
	}
	// O sistema de arquivos não faz link físico. Cria o arquivo final de
	// forma exclusiva.
	return criarDeUmaVez(caminho, dados)
}

// criarDeUmaVez cria caminho com dados, recusando o que já existe. O ramo de
// CriarSeFaltar para o sistema de arquivos sem link físico. Se a escrita falha
// depois de o arquivo nascer, ele é apagado: um arquivo truncado travaria a
// subida do Agent.
func criarDeUmaVez(caminho string, dados []byte) (bool, error) {
	f, err := criarExclusivo(caminho)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dictionary: criando o dicionário: %w", err)
	}
	if _, err = f.Write(dados); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(caminho)
		return false, fmt.Errorf("dictionary: escrevendo o dicionário: %w", err)
	}
	return true, nil
}
