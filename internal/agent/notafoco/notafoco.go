// Package notafoco guarda, só na memória do Agent, em que nota a pessoa está com
// o cursor (ADR-022, "Ditado feito dentro da nota").
//
// O painel avisa o Agent enquanto o título ou uma linha de uma nota tem o foco
// (e a janela do navegador também); o Agent consulta isto quando grava um ditado
// no histórico: se há uma nota em foco, o ditado nasce escondido do histórico
// (virou_nota), como se fosse um "Virar nota" automático. A inserção no campo
// não muda.
//
// # Contexto efêmero
//
// Saber em que nota a pessoa está é contexto de outra aplicação, no sentido do
// CLAUDE.md: vive só aqui, na memória, com validade curta (TTL). Nunca vai a
// disco, a log nem ao sync. Nada neste pacote chama o log, e nada devolve o
// valor para quem queira registrá-lo além de quem grava a linha do ditado.
//
// # Por que TTL
//
// O painel renova o aviso a cada ~20 s enquanto o foco fica. Se o painel morrer
// sem avisar (aba fechada à força, navegador travado), o aviso vence sozinho em
// TTL e os ditados voltam a aparecer no histórico.
package notafoco

import (
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TTL é a validade de um aviso sem renovação. O painel renova a cada ~20 s.
const TTL = 60 * time.Second

// Marcador é o aviso em memória: no máximo um (a pessoa só tem um cursor).
// Seguro para uso concorrente. O zero não serve; use Novo.
type Marcador struct {
	mu    sync.Mutex
	agora func() time.Time
	valor string // o que vai em virou_nota: o id da nota ou "nova:<token>"
	vence time.Time
	ativo bool
}

// Novo devolve um marcador vazio. agora nil usa time.Now.
func Novo(agora func() time.Time) *Marcador {
	if agora == nil {
		agora = time.Now
	}
	return &Marcador{agora: agora}
}

// DefinirNota avisa que o cursor está na nota existente id. Devolve false (e não
// guarda nada) se id não é um identificador aceitável.
func (m *Marcador) DefinirNota(id string) bool {
	if !storage.TokenDaNotaValido(id) {
		return false
	}
	m.definir(id)
	return true
}

// DefinirNova avisa que o cursor está no "Criar uma nota" de token token (a nota
// ainda não existe). Devolve false se o token não é aceitável.
func (m *Marcador) DefinirNova(token string) bool {
	if !storage.TokenDaNotaValido(token) {
		return false
	}
	m.definir(storage.PrefixoDaNotaNova + token)
	return true
}

func (m *Marcador) definir(valor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.valor = valor
	m.vence = m.agora().Add(TTL)
	m.ativo = true
}

// LimparNota tira o aviso se ele ainda é o da nota id (um aviso de outra nota,
// que chegou depois, fica). Devolve se limpou.
func (m *Marcador) LimparNota(id string) bool { return m.limparSe(id) }

// LimparNova tira o aviso se ele ainda é o do "Criar uma nota" de token token.
func (m *Marcador) LimparNova(token string) bool {
	return m.limparSe(storage.PrefixoDaNotaNova + token)
}

// Limpar tira o aviso, seja qual for.
func (m *Marcador) Limpar() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.valor, m.ativo = "", false
}

func (m *Marcador) limparSe(valor string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ativo || m.valor != valor {
		return false
	}
	m.valor, m.ativo = "", false
	return true
}

// ParaOHistorico é a decisão: o que gravar em virou_nota no ditado que acaba de
// ser feito. nil quando não há aviso ou ele venceu (o ditado comum). Um
// marcador nil também devolve nil (sem painel ligado, tudo como antes).
func (m *Marcador) ParaOHistorico() *string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ativo {
		return nil
	}
	if !m.agora().Before(m.vence) {
		m.valor, m.ativo = "", false
		return nil
	}
	v := m.valor
	return &v
}
