package main

import (
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/painel"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/permissao"
)

var _ localapi.Microfone = microfoneDoAgent{}

// microfoneDoAgent é o localapi.Microfone do Agent: a leitura do registro
// (permissao) e a abertura da tela do Windows (painel). As duas são
// funções, para o teste trocar o registro e o ShellExecute por dublês.
type microfoneDoAgent struct {
	consultar func() permissao.Motivo
	abrir     func() error
}

func novoMicrofoneDoAgent() microfoneDoAgent {
	return microfoneDoAgent{consultar: permissao.Consultar, abrir: painel.AbrirConfiguracoesDoMicrofone}
}

// Estado lê o registro a cada chamada: a pessoa liga a chave e volta ao
// painel, e o aviso tem de sumir sozinho. "Não deu para ler" não é
// bloqueio, mas o motivo segue para o painel.
func (m microfoneDoAgent) Estado() (bool, string) {
	motivo := m.consultar()
	return motivo.Bloqueado(), string(motivo)
}

func (m microfoneDoAgent) AbrirConfiguracoes() error { return m.abrir() }
