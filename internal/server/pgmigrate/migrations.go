// Package pgmigrate lê, confere e aplica as migrations PostgreSQL do backend
// cloud (migrations/*.sql, embutidas por migrations.FS).
//
// Ele existe para o comando "aurora-whisper-server migrate" (ADR-019, decisão
// 13): as migrations rodam por um comando próprio, nunca na subida do
// servidor (05-data-model.md, "Integridade"). A subida do servidor não
// importa este pacote.
//
// # Duas metades
//
//   - Esta, PURA, sem banco: Load lê os arquivos e valida nome, ordem, vazio e
//     controle de transação; Plan decide, a partir do que o banco registrou,
//     quais migrations faltam e se alguma já aplicada foi alterada, sumiu ou
//     chegou fora de ordem. Roda em qualquer máquina, sem Postgres, e é onde
//     moram as regras que importam.
//   - run.go, que fala com o Postgres por pgx: tranca com pg_advisory_lock,
//     aplica cada arquivo numa transação própria e registra a versão e o
//     checksum em schema_migrations.
//
// # Regras (iguais às do migrations/README.md)
//
//   - Nome: NNNN_<nome>.sql, com NNNN de quatro dígitos a partir de 0001,
//     sem buraco nem repetição. Arquivo que não termina em .sql é ignorado.
//   - Cada arquivo é uma transação. O arquivo não pode conter BEGIN, COMMIT
//     nem ROLLBACK: o aplicador já abre e fecha a transação, e um COMMIT de
//     dentro quebraria o "tudo ou nada" do arquivo e o registro da versão.
//   - O checksum é o SHA-256 do arquivo com os fins de linha normalizados
//     para LF (um checkout com CRLF não muda o checksum). Migration já
//     aplicada que muda de checksum é RECUSADA: depois da primeira aplicação
//     real, migration não se edita (migrations/README.md). A ADR-019
//     (decisão 13) autorizou corrigir a 0001-0009 no lugar, na F1, porque
//     nenhuma delas rodou em lugar nenhum. Estender isso a quem rodou só em
//     banco de teste descartável, até o deploy (F11), é proposta que o Mauro
//     ainda confirma (migrations/README.md); o checksum é o que impede de
//     fazê-lo num banco que já aplicou o arquivo.
package pgmigrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Os erros de Load e Plan. Cada um vem embrulhado com o arquivo e o motivo,
// e nenhum traz o conteúdo do SQL: use errors.Is para distingui-los.
var (
	// ErrBadFileName: um arquivo .sql que não é NNNN_<nome>.sql.
	ErrBadFileName = errors.New("pgmigrate: nome de migration inválido")
	// ErrDuplicateVersion: dois arquivos com o mesmo NNNN.
	ErrDuplicateVersion = errors.New("pgmigrate: duas migrations com a mesma versão")
	// ErrGap: falta um número na sequência 0001, 0002, ...
	ErrGap = errors.New("pgmigrate: falta uma migration na sequência")
	// ErrEmpty: arquivo sem nenhum comando.
	ErrEmpty = errors.New("pgmigrate: migration vazia")
	// ErrTransactionControl: o arquivo tem BEGIN, COMMIT ou ROLLBACK.
	ErrTransactionControl = errors.New("pgmigrate: a migration não pode controlar a transação")

	// ErrChecksum: uma migration já aplicada foi alterada depois.
	ErrChecksum = errors.New("pgmigrate: migration alterada depois de aplicada")
	// ErrMissingFile: o banco registra uma migration que este binário não tem.
	ErrMissingFile = errors.New("pgmigrate: migration aplicada no banco não existe neste binário")
	// ErrNameChanged: a mesma versão, com outro nome.
	ErrNameChanged = errors.New("pgmigrate: o nome da migration mudou depois de aplicada")
	// ErrOutOfOrder: uma migration pendente é mais antiga que outra já aplicada.
	ErrOutOfOrder = errors.New("pgmigrate: migration pendente com número menor que uma já aplicada")
)

// Migration é um arquivo de migration lido e validado.
type Migration struct {
	Version  int    // 1 para 0001_users_table.sql
	Name     string // "users_table"
	File     string // "0001_users_table.sql"
	SQL      string // o conteúdo, com fim de linha LF
	Checksum string // SHA-256 do SQL, em hexadecimal
}

// Applied é o que o banco registrou em schema_migrations para uma versão.
type Applied struct {
	Version  int
	Name     string
	Checksum string
}

var fileNamePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.sql$`)

// Load lê as migrations da raiz de fsys (os arquivos *.sql; o resto é
// ignorado), valida cada uma e as devolve em ordem de versão.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("pgmigrate: ler o diretório de migrations: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := entry.Name()
		m := fileNamePattern.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("%w: %q (esperado NNNN_nome_em_minusculas.sql)", ErrBadFileName, name)
		}
		version, err := strconv.Atoi(m[1])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("%w: %q (a numeração começa em 0001)", ErrBadFileName, name)
		}
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("pgmigrate: ler %s: %w", name, err)
		}
		sql := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
		if err := lint(name, sql); err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(sql))
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     m[2],
			File:     name,
			SQL:      sql,
			Checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		if migrations[i].Version != migrations[j].Version {
			return migrations[i].Version < migrations[j].Version
		}
		return migrations[i].File < migrations[j].File
	})
	for i, m := range migrations {
		if i > 0 && m.Version == migrations[i-1].Version {
			return nil, fmt.Errorf("%w: %s e %s", ErrDuplicateVersion, migrations[i-1].File, m.File)
		}
		if m.Version != i+1 {
			return nil, fmt.Errorf("%w: esperava a versão %04d, achei %s", ErrGap, i+1, m.File)
		}
	}
	return migrations, nil
}

// END não entra na lista de propósito: é fim de bloco de PL/pgSQL e do
// "BEGIN ATOMIC" de uma função SQL, e o corte em ";" o deixa no começo de um
// "comando".
var txControl = regexp.MustCompile(`(?i)^(BEGIN|COMMIT|ROLLBACK|START\s+TRANSACTION)\b`)

// codeOnly devolve o SQL só com o que é código: tira os comentários de linha
// ("--") e de bloco ("/* */", que o Postgres deixa aninhar) e esvazia o miolo
// dos literais de texto (entre aspas simples, com a aspa dobrada dentro e, em
// E'...', a barra invertida),
// dos identificadores entre aspas ("...") e dos corpos entre delimitadores de
// dólar ($$ ... $$ e $tag$ ... $tag$). Este último é onde mora o corpo de uma
// função PL/pgSQL, com o DECLARE, o BEGIN, o END e os ";" dele, e nada disso é
// um comando do arquivo. O "$" no meio de um identificador (a$b$c) e o
// parâmetro ($1) não abrem corpo nenhum.
func codeOnly(sql string) string {
	var out strings.Builder
	n := len(sql)
	for i := 0; i < n; {
		c := sql[i]
		switch {
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
		case c == '/' && i+1 < n && sql[i+1] == '*':
			depth := 1
			for i += 2; i < n && depth > 0; {
				switch {
				case sql[i] == '/' && i+1 < n && sql[i+1] == '*':
					depth++
					i += 2
				case sql[i] == '*' && i+1 < n && sql[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
			out.WriteByte(' ')
		case c == '\'':
			// E'...' (com E ou e sozinho na frente) aceita \' dentro.
			escapes := i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e') && (i == 1 || !isIdentByte(sql[i-2]))
			i = skipQuoted(sql, i, '\'', escapes)
			out.WriteString("''")
		case c == '"':
			i = skipQuoted(sql, i, '"', false)
			out.WriteString(`""`)
		case c == '$' && (i == 0 || !isIdentByte(sql[i-1])):
			tag, ok := dollarTag(sql, i)
			if !ok {
				out.WriteByte(c)
				i++
				continue
			}
			if end := strings.Index(sql[i+len(tag):], tag); end < 0 {
				i = n // sem fechar: o Postgres recusa o arquivo, e o resto é corpo
			} else {
				i += len(tag) + end + len(tag)
			}
			out.WriteString("$$")
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// skipQuoted devolve o índice logo depois do fecha-aspas do literal que abre em
// sql[i] (aspas simples ou duplas). A aspa dobrada é uma aspa dentro do
// literal; com escapes, a barra invertida protege o caractere seguinte.
func skipQuoted(sql string, i int, quote byte, escapes bool) int {
	n := len(sql)
	for j := i + 1; j < n; {
		switch {
		case escapes && sql[j] == '\\':
			j += 2
		case sql[j] == quote && j+1 < n && sql[j+1] == quote:
			j += 2
		case sql[j] == quote:
			return j + 1
		default:
			j++
		}
	}
	return n
}

// dollarTag lê o delimitador de dólar que abre em sql[i] ($$ ou $tag$, com a
// tag começando por letra ou "_") e o devolve inteiro, com os dois "$".
func dollarTag(sql string, i int) (string, bool) {
	j := i + 1
	if j < len(sql) && (isLetterByte(sql[j]) || sql[j] == '_') {
		for j++; j < len(sql) && sql[j] != '$' && isIdentByte(sql[j]); j++ {
		}
	}
	if j < len(sql) && sql[j] == '$' {
		return sql[i : j+1], true
	}
	return "", false
}

func isLetterByte(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80 }

// isIdentByte: o que pode fazer parte de um identificador do Postgres (o "$"
// também pode, fora do começo).
func isIdentByte(b byte) bool {
	return isLetterByte(b) || b >= '0' && b <= '9' || b == '_' || b == '$'
}

// lint confere o que dá para conferir sem um banco: que o arquivo tem algum
// comando e que não mexe na transação. Olha só o código (codeOnly): um BEGIN
// dentro de um corpo de função, de um literal ou de um comentário não conta, e
// um COMMIT depois de um literal com "--" dentro conta.
func lint(file, sql string) error {
	code := codeOnly(sql)
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("%w: %s", ErrEmpty, file)
	}
	for _, stmt := range strings.Split(code, ";") {
		if m := txControl.FindString(strings.TrimSpace(stmt)); m != "" {
			return fmt.Errorf("%w: %s tem %s (o aplicador já abre e fecha a transação do arquivo)", ErrTransactionControl, file, strings.ToUpper(m))
		}
	}
	return nil
}

// Plan decide o que falta aplicar. all são as migrations deste binário (a
// saída de Load) e applied é o que o banco registrou. Devolve as pendentes,
// em ordem, ou um erro quando o banco e os arquivos não combinam: uma
// migration aplicada que foi alterada, que sumiu, que mudou de nome, ou uma
// pendente com número menor que outra já aplicada (um binário velho contra um
// banco novo, ou um arquivo que apareceu no meio da história).
func Plan(all []Migration, applied []Applied) ([]Migration, error) {
	byVersion := make(map[int]Migration, len(all))
	for _, m := range all {
		byVersion[m.Version] = m
	}

	sorted := append([]Applied(nil), applied...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	done := make(map[int]bool, len(sorted))
	maxApplied := 0
	for _, a := range sorted {
		m, ok := byVersion[a.Version]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: %04d_%s (este binário é mais antigo que o banco?)", ErrMissingFile, a.Version, a.Name)
		case m.Name != a.Name:
			return nil, fmt.Errorf("%w: a versão %04d foi aplicada como %q e o arquivo agora se chama %q", ErrNameChanged, a.Version, a.Name, m.Name)
		case m.Checksum != a.Checksum:
			return nil, fmt.Errorf("%w: %s (checksum no banco %s, no arquivo %s)", ErrChecksum, m.File, short(a.Checksum), short(m.Checksum))
		}
		done[a.Version] = true
		if a.Version > maxApplied {
			maxApplied = a.Version
		}
	}

	var pending []Migration
	for _, m := range all {
		if done[m.Version] {
			continue
		}
		if m.Version < maxApplied {
			return nil, fmt.Errorf("%w: %s está pendente, mas o banco já aplicou a versão %04d", ErrOutOfOrder, m.File, maxApplied)
		}
		pending = append(pending, m)
	}
	return pending, nil
}

// short abrevia um checksum para a mensagem de erro.
func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}
