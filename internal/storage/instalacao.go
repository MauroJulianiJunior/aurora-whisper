package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Instalacao lê e grava as chaves da nuvem na tabela instalacao
// (migrations/0002_instalacao.sql): a identidade desta instalação diante da
// nuvem (ADR-019, "Dono local e conta"; fatia F6). O install_id que o PC manda
// no pareamento, a URL da nuvem, a conta (o id do usuário na nuvem), o e-mail,
// o aparelho e quando conectou.
//
// O token do aparelho NUNCA vem para cá: ele mora no Gerenciador de
// Credenciais do Windows (internal/agent/chaves), como a chave de API. O
// schema_segredo_test confere os nomes das colunas; o teste do pacote nuvem
// confere que o valor do token não está em linha nenhuma.
//
// Só as chaves da nuvem passam por aqui (ChaveDaNuvem): o dono fixo e o
// registro da migração dele (dono.go) não se mexem por este caminho.
type Instalacao struct{ db *sql.DB }

// NovaInstalacao monta o acesso sobre db, que já deve ter passado pelo
// Migrator.
func NovaInstalacao(db *sql.DB) *Instalacao {
	if db == nil {
		panic("storage: db nil")
	}
	return &Instalacao{db: db}
}

// Instalacao devolve o acesso às chaves da nuvem no mesmo banco das
// preferências (o historico.db).
func (r *SQLiteSettingsRepository) Instalacao() *Instalacao { return NovaInstalacao(r.db) }

// ErrChaveDaInstalacao: o nome não é de uma chave da nuvem.
var ErrChaveDaInstalacao = errors.New("storage: só as chaves da nuvem (install_id e nuvem_*) mudam por aqui")

// ChaveDaNuvem diz se nome é uma chave que o pareamento pode ler e gravar: o
// install_id e as que começam com "nuvem_". Nunca as do dono fixo.
func ChaveDaNuvem(nome string) bool {
	return nome == "install_id" || (strings.HasPrefix(nome, "nuvem_") && len(nome) > len("nuvem_") && len(nome) <= 64)
}

// Ler devolve o valor de uma chave; ok=false se ela não existe.
func (i *Instalacao) Ler(ctx context.Context, nome string) (string, bool, error) {
	if !ChaveDaNuvem(nome) {
		return "", false, ErrChaveDaInstalacao
	}
	var v string
	err := i.db.QueryRowContext(ctx, `SELECT valor FROM instalacao WHERE nome = ?`, nome).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("storage: lendo a instalação: %w", err)
	}
	return v, true, nil
}

// Gravar grava as chaves numa transação só (todas ou nenhuma), substituindo
// o valor de cada uma.
func (i *Instalacao) Gravar(ctx context.Context, valores map[string]string) error {
	for nome := range valores {
		if !ChaveDaNuvem(nome) {
			return ErrChaveDaInstalacao
		}
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: abrindo a transação da instalação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for nome, valor := range valores {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
			nome, valor); err != nil {
			return fmt.Errorf("storage: gravando a instalação: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: confirmando a instalação: %w", err)
	}
	return nil
}

// Apagar tira as chaves (as que não existem, não importa), numa transação
// só.
func (i *Instalacao) Apagar(ctx context.Context, nomes ...string) error {
	for _, nome := range nomes {
		if !ChaveDaNuvem(nome) {
			return ErrChaveDaInstalacao
		}
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: abrindo a transação da instalação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, nome := range nomes {
		if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome = ?`, nome); err != nil {
			return fmt.Errorf("storage: apagando da instalação: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: confirmando a instalação: %w", err)
	}
	return nil
}
