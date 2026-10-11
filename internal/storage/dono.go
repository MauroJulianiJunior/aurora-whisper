package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// # O dono fixo do banco local
//
// Decisão do Mauro, 29-set-2026: "dono local fixo, migrando tudo pra esse
// dono e fazer a cópia antes de migrar".
//
// Até ali o dono dos ditados era o campo "usuario" do dicionario.json, ou
// "local" sem dicionário. Com o projeto aberto (MIT), quem instalasse sem
// o dicionário do Mauro teria um dono que muda assim que cria o dele — e
// trocar o "usuario" do dicionário escondia do painel todo o histórico
// anterior, porque toda listagem é escopada pelo dono.
//
// Agora o dono é gerado UMA vez por instalação (NewID, o mesmo UUID v7 do
// resto) e gravado no próprio banco, na tabela instalacao. O dicionário
// volta a ser só dicionário.

// Chaves da tabela instalacao (migrations/0002_instalacao.sql).
const (
	chaveDono      = "dono_local"
	chaveMigradoEm = "dono_local_migrado_em"
	chaveCopia     = "dono_local_copia"
)

// ErrSemDonoFixo: o banco ainda não tem dono fixo. É o banco de antes
// desta versão (sem a tabela instalacao), ou um em que a migração ainda
// não conseguiu rodar — a cópia falhou, por exemplo. Quem lê cai na regra
// antiga (o usuario do dicionário, ou "local").
//
// GarantirDonoFixo também embrulha nele toda falha de DEPOIS de confirmar
// que o banco não tinha dono: a cópia, a contagem, a transação desfeita.
// Assim quem chama sabe que o banco continua sem dono e que nada mudou —
// e a regra antiga ainda é a que mostra o histórico. Um erro de
// GarantirDonoFixo SEM ErrSemDonoFixo é outro caso: o banco pode ter dono,
// e a regra antiga gravaria ditados que o painel nunca mais mostraria.
var ErrSemDonoFixo = errors.New("storage: o banco ainda não tem dono fixo")

// ErrCopiaFalhou: a cópia de segurança de antes da migração não foi feita,
// ou foi feita e não confere. Nenhuma linha foi migrada.
var ErrCopiaFalhou = errors.New("storage: a cópia do banco antes da migração falhou")

// MaxCopias é quantas cópias de antes da migração podem ficar ao lado do
// banco. Cada tentativa que falha DEPOIS do VACUUM INTO deixa uma cópia
// completa — com todo o texto ditado —, e uma falha que se repete a cada
// subida as acumularia sem limite. Com MaxCopias já lá, nenhuma nova é
// feita, e nada migra até alguém ler o motivo no log e apagá-las.
const MaxCopias = 3

// marcaDaCopia separa, no nome da cópia, o nome do banco da hora.
const marcaDaCopia = ".antes-do-dono-"

// DonoFixo é o resultado de GarantirDonoFixo.
type DonoFixo struct {
	// Dono é o user_id de todas as linhas do banco local.
	Dono string
	// Migrou diz se ESTA chamada gerou o dono e migrou as linhas. False
	// quando o dono já estava gravado: aí nada foi copiado nem mudado.
	Migrou bool
	// Copia é o caminho da cópia feita antes de migrar. Vazio quando não
	// houve migração, ou quando o banco não tinha linha nenhuma com
	// user_id — ver GarantirDonoFixo.
	Copia string
	// Linhas é quantas linhas foram migradas em cada tabela com user_id.
	Linhas map[string]int64
}

// CaminhoDaCopia é onde GarantirDonoFixo deve copiar o banco antes de
// migrar: ao lado dele, com a hora no nome, por exemplo
// historico.db.antes-do-dono-20260929-101500.db.
//
// A hora é a local: o nome é para uma pessoa achar o arquivo, não para
// ordenar instantes.
func CaminhoDaCopia(caminhoBanco string, agora time.Time) string {
	return caminhoBanco + marcaDaCopia + agora.Format("20060102-150405") + ".db"
}

// copiasAnteriores conta as cópias da mesma marca que já estão ao lado de
// destino (marcaDaCopia, a do dono fixo, marcaDaCopiaDoEnvio, a do envio
// inicial, ou marcaDaCopiaDosSeisMeses), pela MESMA regra da tela e do botão
// "Apagar as cópias" (ehCopia: arquivo regular, com o nome do banco, a marca, a
// data, a hora e o .db, inteiro e sem sobra). Uma regra mais larga contaria no
// limite o que a tela não mostra e o botão não apaga, e o "já há 3 cópias,
// apague as que não quiser" ficaria sem saída. Toda cópia que o Agent faz tem
// o nome exato, então nenhuma escapa do limite. Um destino fora do padrão não
// tem com o que se comparar: zero. Cada marca conta só as dela.
func copiasAnteriores(destino, marca string) (int, error) {
	nome := filepath.Base(destino)
	i := strings.LastIndex(nome, marca)
	if i < 0 {
		return 0, nil
	}
	casa := nomeDeCopia(nome[:i], marca)
	entradas, err := os.ReadDir(filepath.Dir(destino))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entradas {
		if ehCopia(e, casa) {
			n++
		}
	}
	return n, nil
}

// LerDonoFixo devolve o dono fixo gravado no banco, ou ErrSemDonoFixo se
// ainda não há um. Só lê: serve a um banco aberto em modo somente leitura
// (o cmd/dictlearn) e a um banco anterior à migration 0002, sem a tabela.
func LerDonoFixo(ctx context.Context, db *sql.DB) (string, error) {
	return lerDonoFixo(ctx, db)
}

// lerDonoFixo é LerDonoFixo por qualquer consulta: o *sql.DB ou a transação
// que vai usar o dono (os 6 meses, F10).
func lerDonoFixo(ctx context.Context, db consulta) (string, error) {
	var tabelas int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'instalacao'`).Scan(&tabelas); err != nil {
		return "", fmt.Errorf("storage: procurando a tabela instalacao: %w", err)
	}
	if tabelas == 0 {
		return "", ErrSemDonoFixo
	}
	var dono string
	err := db.QueryRowContext(ctx, `SELECT valor FROM instalacao WHERE nome = ?`, chaveDono).Scan(&dono)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSemDonoFixo
	}
	if err != nil {
		return "", fmt.Errorf("storage: lendo o dono fixo: %w", err)
	}
	if strings.TrimSpace(dono) == "" {
		// Nada grava vazio; se aparecer, é um banco mexido à mão. Usar o
		// vazio travaria o painel (toda listagem recusa dono vazio).
		return "", errors.New("storage: o dono fixo gravado está vazio")
	}
	return dono, nil
}

// GarantirDonoFixo devolve o dono fixo do banco, criando-o — e migrando
// para ele todas as linhas antigas — na primeira vez.
//
// db já deve ter passado pelo Migrator (a tabela instalacao existe).
//
// # A ordem, e por que ela é essa
//
//  1. Dono já gravado: devolve e para. Sem cópia, sem UPDATE. É o caso de
//     toda subida depois da primeira. Uma linha gravada DEPOIS com outro
//     user_id — por um Agent anterior ao dono fixo rodando sobre o mesmo
//     banco — não é migrada: fica fora do painel. LinhasDeOutroDono é
//     quem as acha, para o aviso.
//  2. Cópia completa do banco em caminhoCopia, com VACUUM INTO — que lê
//     pela conexão, então pega também o que ainda está no -wal, e produz
//     um retrato consistente. Copiar o arquivo .db com o sistema de
//     arquivos perderia o que não passou por checkpoint. A cópia é
//     conferida: tem de existir, abrir, e ter as mesmas linhas, tabela
//     por tabela. Qualquer falha aqui devolve ErrCopiaFalhou e NADA é
//     migrado. Com MaxCopias cópias já ao lado do banco, nem tenta.
//  3. Numa transação só: gera o dono, grava, faz UPDATE de user_id para
//     ele em toda tabela que tenha a coluna — descobertas no próprio
//     schema, não numa lista que envelheceria —, e grava quando migrou e
//     onde ficou a cópia. Tudo ou nada.
//
// Todo user_id antigo vai para o dono: "mauro", "local", "" ou qualquer
// outro. Uma instalação é de uma pessoa só.
//
// Banco sem nenhuma linha com user_id (a instalação nova de quem baixou
// o projeto): o dono é gerado sem cópia, porque não há o que proteger — e
// uma cópia de banco vazio ao lado de cada instalação nova seria só
// entulho. A transação confere isso: se alguma linha for migrada sem
// cópia, ela desfaz tudo.
//
// Se a transação falhar, a cópia fica onde foi feita (o erro diz onde), e
// a próxima chamada tenta de novo, com uma cópia nova — até MaxCopias.
//
// Toda falha depois do passo 1 vem embrulhada em ErrSemDonoFixo: o banco
// continua sem dono, e nada mudou nele. Falha no passo 1 que não seja a
// falta do dono (dono gravado vazio, erro de leitura) NÃO vem: ali o
// banco pode já ter dono.
func GarantirDonoFixo(ctx context.Context, db *sql.DB, caminhoCopia string) (DonoFixo, error) {
	dono, err := LerDonoFixo(ctx, db)
	if err == nil {
		return DonoFixo{Dono: dono}, nil
	}
	if !errors.Is(err, ErrSemDonoFixo) {
		return DonoFixo{}, err
	}
	r, err := criarDonoFixo(ctx, db, caminhoCopia)
	if err != nil {
		return DonoFixo{}, fmt.Errorf("%w: %w", ErrSemDonoFixo, err)
	}
	return r, nil
}

// criarDonoFixo são os passos 2 e 3 de GarantirDonoFixo, num banco que
// não tem dono.
func criarDonoFixo(ctx context.Context, db *sql.DB, caminhoCopia string) (DonoFixo, error) {
	todas, err := tabelasDoBanco(ctx, db)
	if err != nil {
		return DonoFixo{}, err
	}
	antes, err := contarLinhas(ctx, db, todas)
	if err != nil {
		return DonoFixo{}, err
	}
	comDono, err := tabelasComDono(ctx, db, todas)
	if err != nil {
		return DonoFixo{}, err
	}
	var aMigrar int64
	for _, t := range comDono {
		aMigrar += antes[t]
	}

	copia := ""
	if aMigrar > 0 {
		if err := copiarBanco(ctx, db, caminhoCopia, marcaDaCopia, antes); err != nil {
			return DonoFixo{}, fmt.Errorf("%w: %v", ErrCopiaFalhou, err)
		}
		copia = caminhoCopia
	}

	dono, linhas, err := migrarParaDonoNovo(ctx, db, comDono, copia)
	if err != nil {
		if copia != "" {
			return DonoFixo{}, fmt.Errorf("storage: migrando para o dono fixo (nada mudou; a cópia ficou em %s): %w", copia, err)
		}
		return DonoFixo{}, fmt.Errorf("storage: gravando o dono fixo (nada mudou): %w", err)
	}
	return DonoFixo{Dono: dono, Migrou: true, Copia: copia, Linhas: linhas}, nil
}

// LinhasDeOutroDono conta, em cada tabela com user_id, as linhas que NÃO
// são de dono. Só entram as tabelas com alguma; mapa vazio é banco limpo.
//
// Depois da migração nada as grava — a não ser um Agent anterior ao dono
// fixo rodando sobre o mesmo banco, que grava com o usuario do dicionário.
// Essas linhas ficam fora do painel, e GarantirDonoFixo não as migra: o
// dono já está gravado, e migrar de novo seria decisão do Mauro, com cópia
// antes. O que cabe aqui é o aviso — só contagens, nunca texto.
func LinhasDeOutroDono(ctx context.Context, db *sql.DB, dono string) (map[string]int64, error) {
	todas, err := tabelasDoBanco(ctx, db)
	if err != nil {
		return nil, err
	}
	comDono, err := tabelasComDono(ctx, db, todas)
	if err != nil {
		return nil, err
	}
	fora := map[string]int64{}
	for _, t := range comDono {
		var n int64
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM `+identificador(t)+` WHERE user_id IS NOT ?`, dono).Scan(&n); err != nil {
			return nil, fmt.Errorf("storage: contando as linhas de outro dono em %s: %w", t, err)
		}
		if n > 0 {
			fora[t] = n
		}
	}
	return fora, nil
}

// migrarParaDonoNovo é o passo 3 de GarantirDonoFixo: a transação.
func migrarParaDonoNovo(ctx context.Context, db *sql.DB, comDono []string, copia string) (string, map[string]int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op depois do Commit.

	dono := NewID()
	// INSERT, e não INSERT OR REPLACE: um dono já gravado nunca é trocado.
	// Se outro processo gravou um entre a leitura e aqui, a chave primária
	// recusa, e a transação inteira volta.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, chaveDono, dono); err != nil {
		return "", nil, fmt.Errorf("gravar o dono: %w", err)
	}

	linhas := make(map[string]int64, len(comDono))
	for _, t := range comDono {
		res, err := tx.ExecContext(ctx, `UPDATE `+identificador(t)+` SET user_id = ?`, dono)
		if err != nil {
			return "", nil, fmt.Errorf("migrar %s: %w", t, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", nil, fmt.Errorf("contar as linhas migradas de %s: %w", t, err)
		}
		if n > 0 && copia == "" {
			// A contagem de antes dizia zero, e a cópia foi dispensada por
			// isso. Linha que apareceu depois não é migrada sem cópia.
			return "", nil, fmt.Errorf("%s ganhou %d linha(s) depois da contagem, e o banco não foi copiado", t, n)
		}
		linhas[t] = n
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO instalacao (nome, valor) VALUES (?, ?), (?, ?)`,
		chaveMigradoEm, emTexto(time.Now()), chaveCopia, copia); err != nil {
		return "", nil, fmt.Errorf("registrar a migração: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("commit: %w", err)
	}
	return dono, linhas, nil
}

// errCopiasDemais: já há MaxCopias cópias da mesma marca ao lado do banco, e
// nenhuma nova é feita.
var errCopiasDemais = errors.New("cópias demais ao lado do banco")

// executor cobre *sql.DB e *sql.Conn: o VACUUM INTO roda por quem o chama (o
// envio inicial o roda pela conexão que segura, ver copiarPelaConexao).
type executor interface {
	consulta
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// copiarBanco é o passo 2 de GarantirDonoFixo: VACUUM INTO, e a conferência.
// antes é a contagem de linhas de todas as tabelas do banco de origem; marca
// separa, no nome, as cópias de cada motivo (MaxCopias vale por marca).
func copiarBanco(ctx context.Context, db executor, destino, marca string, antes map[string]int64) error {
	if err := vacuumInto(ctx, db, destino, marca); err != nil {
		return err
	}
	return conferirCopia(ctx, destino, antes)
}

// vacuumInto faz a cópia completa do banco em destino, que não pode existir,
// e recusa quando já há MaxCopias cópias da mesma marca ao lado.
func vacuumInto(ctx context.Context, db executor, destino, marca string) error {
	// VACUUM INTO já recusa um destino existente; conferir antes dá uma
	// mensagem que diz o motivo. Uma cópia antiga nunca é sobrescrita.
	if _, err := os.Stat(destino); err == nil {
		return fmt.Errorf("%s já existe, e não é sobrescrito", destino)
	}
	n, err := copiasAnteriores(destino, marca)
	if err != nil {
		return fmt.Errorf("procurando as cópias anteriores ao lado de %s: %w", destino, err)
	}
	if n >= MaxCopias {
		return fmt.Errorf("já há %d cópia(s) de tentativas anteriores ao lado de %s, e nenhuma nova é feita: "+
			"o motivo das falhas está nos avisos anteriores do log; confira e apague as cópias que não quiser: %w",
			n, destino, errCopiasDemais)
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, destino); err != nil {
		return fmt.Errorf("VACUUM INTO %s: %w", destino, err)
	}
	return nil
}

// conferirCopia confere a cópia em destino contra antes, a contagem de linhas
// de todas as tabelas do banco de origem, tirada sem nenhuma escrita entre
// ela e o VACUUM INTO.
func conferirCopia(ctx context.Context, destino string, antes map[string]int64) error {
	// A conferência abre a cópia como qualquer um abriria: outro *sql.DB,
	// somente leitura. Contar pela conexão do banco original não provaria
	// que o arquivo copiado abre.
	if _, err := os.Stat(destino); err != nil {
		return fmt.Errorf("a cópia não apareceu em %s: %w", destino, err)
	}
	c, err := AbrirSomenteLeitura(destino)
	if err != nil {
		return err
	}
	defer c.Close()
	tabelas, err := tabelasDoBanco(ctx, c)
	if err != nil {
		return fmt.Errorf("abrindo a cópia %s: %w", destino, err)
	}
	depois, err := contarLinhas(ctx, c, tabelas)
	if err != nil {
		return fmt.Errorf("contando a cópia %s: %w", destino, err)
	}
	if len(depois) != len(antes) {
		return fmt.Errorf("a cópia %s tem %d tabela(s), e o banco %d", destino, len(depois), len(antes))
	}
	for t, n := range antes {
		m, ok := depois[t]
		if !ok || m != n {
			return fmt.Errorf("a cópia %s tem %d linha(s) em %s, e o banco %d", destino, m, t, n)
		}
	}
	return nil
}

// AbrirSomenteLeitura abre um banco SQLite existente sem poder escrever
// nele. O driver ("sqlite", modernc.org/sqlite) é registrado pelo binário
// que chama — este pacote não o importa (ADR-008).
func AbrirSomenteLeitura(caminho string) (*sql.DB, error) {
	// No URI, '%', '?' e '#' têm significado; um caminho que os tenha
	// abriria outro arquivo, ou nenhum.
	esc := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(caminho))
	db, err := sql.Open("sqlite", "file:"+esc+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("abrindo %s: %w", caminho, err)
	}
	return db, nil
}

// consulta cobre *sql.DB e *sql.Tx.
type consulta interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// tabelasDoBanco lista as tabelas de dados, em ordem de nome — as internas
// do SQLite (sqlite_*) ficam de fora.
func tabelasDoBanco(ctx context.Context, q consulta) ([]string, error) {
	linhas, err := q.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("storage: listando as tabelas: %w", err)
	}
	defer linhas.Close()
	var nomes []string
	for linhas.Next() {
		var n string
		if err := linhas.Scan(&n); err != nil {
			return nil, fmt.Errorf("storage: listando as tabelas: %w", err)
		}
		nomes = append(nomes, n)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: listando as tabelas: %w", err)
	}
	return nomes, nil
}

// tabelasComDono filtra, de tabelas, as que têm a coluna user_id.
func tabelasComDono(ctx context.Context, q consulta, tabelas []string) ([]string, error) {
	var com []string
	for _, t := range tabelas {
		var n int
		if err := q.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM pragma_table_info(?) WHERE name = 'user_id'`, t).Scan(&n); err != nil {
			return nil, fmt.Errorf("storage: lendo as colunas de %s: %w", t, err)
		}
		if n > 0 {
			com = append(com, t)
		}
	}
	sort.Strings(com)
	return com, nil
}

// contarLinhas conta as linhas de cada tabela.
func contarLinhas(ctx context.Context, q consulta, tabelas []string) (map[string]int64, error) {
	n := make(map[string]int64, len(tabelas))
	for _, t := range tabelas {
		var c int64
		if err := q.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+identificador(t)).Scan(&c); err != nil {
			return nil, fmt.Errorf("storage: contando %s: %w", t, err)
		}
		n[t] = c
	}
	return n, nil
}

// identificador põe um nome de tabela entre aspas para ir no SQL. Os nomes
// vêm do sqlite_master, nunca de fora, mas as aspas dobradas custam nada.
func identificador(nome string) string {
	return `"` + strings.ReplaceAll(nome, `"`, `""`) + `"`
}
