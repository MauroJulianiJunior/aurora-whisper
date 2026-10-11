// Package painel serve o painel local do Aurora Whisper: a página React,
// e o caminho pelo qual ela recebe o token da Local API.
//
// # O problema que este pacote resolve
//
// A Local API exige "Authorization: Bearer <token>", e a spec proíbe o
// token de existir em qualquer lugar além da memória
// (09-security-privacy.md: "token aleatório em memória", "React mantém
// token em memória"). Não pode ir para arquivo, não pode ir para log, não
// pode ficar numa rota aberta — uma rota que devolvesse o token a quem
// pedisse o tornaria inútil.
//
// # Bilhete de uso único
//
// Quando a pessoa clica "Abrir Aurora Whisper" na bandeja, o Agent emite
// um BILHETE aleatório, de uso único, que expira em um minuto, e abre o
// navegador em http://127.0.0.1:PORTA/?t=BILHETE. A página troca o bilhete
// pelo token (POST /api/sessao) e apaga o bilhete da própria URL.
//
// O bilhete aparece na URL, e portanto pode acabar no histórico do
// navegador ou na linha de comando dele. Por isso ele morre na primeira
// troca e em um minuto de qualquer forma: o que sobra no histórico é um
// bilhete que não abre nada.
//
// O custo, assumido: recarregar a página perde o token, porque ele só
// existe na memória da aba. Abrir de novo pela bandeja emite outro
// bilhete. É a consequência direta da regra da spec, e é o comportamento
// certo — um token que sobrevivesse ao recarregar estaria guardado em
// algum lugar.
package painel

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	// ValidadeDoBilhete é quanto tempo um bilhete vale sem ser trocado.
	// Um minuto é folga de sobra para o navegador abrir; mais que isso só
	// aumentaria a janela em que um bilhete vazado ainda serve.
	ValidadeDoBilhete = time.Minute

	// maxPendentes limita os bilhetes vivos ao mesmo tempo. Cada clique
	// em "Abrir" emite um; sem limite, clicar muitas vezes cresceria o
	// mapa sem fim.
	maxPendentes = 8

	bytesDoBilhete = 32
)

// Bilhetes guarda os bilhetes emitidos e ainda não trocados.
type Bilhetes struct {
	mu        sync.Mutex
	pendentes map[string]pendente
	seq       uint64
	agora     func() time.Time
}

// pendente é um bilhete vivo. seq é a ordem de emissão, e é ela — não o
// horário — que decide qual é o mais antigo.
//
// A primeira versão comparava os horários de expiração, e o teste que a
// cobria falhava de vez em quando: dois bilhetes emitidos em sequência
// rápida caem no MESMO tique do relógio, e aí o "mais antigo" dependia
// da ordem de iteração do mapa, que o Go embaralha de propósito. Horário
// não é ordem.
type pendente struct {
	expira time.Time
	seq    uint64
}

// NovosBilhetes cria o registro de bilhetes com o relógio real.
func NovosBilhetes() *Bilhetes {
	return &Bilhetes{pendentes: make(map[string]pendente), agora: time.Now}
}

// Emitir cria um bilhete novo.
func (b *Bilhetes) Emitir() (string, error) {
	buf := make([]byte, bytesDoBilhete)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("painel: gerando bilhete: %w", err)
	}
	bilhete := hex.EncodeToString(buf)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.podar()
	if len(b.pendentes) >= maxPendentes {
		// Descarta o emitido primeiro: é o que a pessoa menos
		// provavelmente ainda está esperando abrir.
		var maisAntigo string
		var menorSeq uint64
		for k, v := range b.pendentes {
			if maisAntigo == "" || v.seq < menorSeq {
				maisAntigo, menorSeq = k, v.seq
			}
		}
		delete(b.pendentes, maisAntigo)
	}
	b.seq++
	b.pendentes[bilhete] = pendente{expira: b.agora().Add(ValidadeDoBilhete), seq: b.seq}
	return bilhete, nil
}

// Trocar consome o bilhete. Verdadeiro só se ele foi emitido, não expirou
// e nunca foi trocado — e depois disto ele não vale mais nada.
//
// A comparação é em tempo constante contra cada bilhete pendente, em vez
// de uma busca no mapa pela chave: o tempo de uma busca em mapa depende
// do conteúdo, e isso deixaria medir, requisição a requisição, quanto de
// um bilhete está certo. São no máximo maxPendentes comparações.
func (b *Bilhetes) Trocar(bilhete string) bool {
	if len(bilhete) != 2*bytesDoBilhete {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.podar()

	var achado string
	for k := range b.pendentes {
		if subtle.ConstantTimeCompare([]byte(k), []byte(bilhete)) == 1 {
			achado = k
		}
	}
	if achado == "" {
		return false
	}
	delete(b.pendentes, achado)
	return true
}

// podar remove os expirados. Chamado com mu travado.
func (b *Bilhetes) podar() {
	agora := b.agora()
	for k, p := range b.pendentes {
		if !agora.Before(p.expira) {
			delete(b.pendentes, k)
		}
	}
}
