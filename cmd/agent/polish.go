package main

import (
	"context"
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Este arquivo é a metade do Polish que só o binário do Agent monta
// (ADR-016, fatia S4): o que liga a chave do Gerenciador de Credenciais ao
// runtime (winruntime.AbrirPolidor). O resto do adaptador (o cliente do
// ditado, a política de esforço e o prazo) mora em internal/polish desde a
// C5a da ADR-021, onde o servidor o usa também; o Agent o chama de lá, sem
// mudar o comportamento.

// polishDoAgent monta, a cada ditado, o Polidor do provedor e do modelo
// que valiam no keydown.
//
// # A chave
//
// Lida do cofre A CADA DITADO, pelo provedor do keydown, dentro da própria
// chamada (ia.Polidor.Chave): "Remover chave" na tela vale no ditado
// seguinte, e nenhum campo daqui nem do ia.Polidor guarda a chave (ADR-016,
// decisão 2). Em produção o cofre é o Gerenciador de Credenciais, e a
// leitura passa pelo portão sysinput, que só abre no binário do Agent. Um
// erro de leitura, qualquer que seja, vira o motivo sem_chave (ia.Polidor),
// sem abrir a rede.
type polishDoAgent struct {
	smart   *resolver.SmartResolver
	cofre   chaves.Cofre
	cliente *ia.Cliente
	esforco *polish.PoliticaEsforco
}

func novoPolishDoAgent(smart *resolver.SmartResolver, cofre chaves.Cofre, cliente *ia.Cliente, log *slog.Logger) *polishDoAgent {
	return &polishDoAgent{smart: smart, cofre: cofre, cliente: cliente, esforco: polish.NovaPoliticaEsforco(log)}
}

// com devolve o mesmo adaptador sobre o Smart de um dicionário novo (o que
// o painel acabou de aprender, ver aprendizadoDoAgent). Cofre, cliente e a
// política de esforço são os mesmos: a memória dos modelos que recusaram o
// esforço não se perde ao trocar o dicionário.
func (a *polishDoAgent) com(smart *resolver.SmartResolver) *polishDoAgent {
	c := *a
	c.smart = smart
	return &c
}

// abrir implementa winruntime.AbrirPolidor.
//
// O dicionário é o mesmo do Smart, consultado localmente pelas guardas
// (resolver.PolishResolver.Reescrever); nunca vai ao provedor.
func (a *polishDoAgent) abrir(provedor, modelo string, contar func(entrada, saida int)) (winruntime.Polidor, string) {
	return polish.Montagem{
		Smart:    a.smart,
		Cliente:  a.cliente,
		Esforco:  a.esforco,
		Provedor: provedor,
		Modelo:   modelo,
		// O cofre é lido a cada chamada ao provedor, pelo provedor do
		// keydown. Sem provedor válido, Montagem nem chama esta função.
		Chave:  func(context.Context) (string, error) { return a.cofre.Ler(provedor) },
		Contar: contar,
	}.Abrir()
}

// ultimoPolishParaATela traduz o último Polish do runtime para a API local
// (polish_ultimo_motivo, fatia S5): o motivo, o provedor do keydown e a
// hora. Nenhum Polish ainda: false, e a tela não mostra nada. Só o enum
// atravessa — a frase é da API local, e texto ditado nunca passa aqui.
func ultimoPolishParaATela(ultimo func() winruntime.EstadoPolish) func() (localapi.DesfechoDoPolish, bool) {
	return func() (localapi.DesfechoDoPolish, bool) {
		e := ultimo()
		if e.Quando.IsZero() {
			return localapi.DesfechoDoPolish{}, false
		}
		return localapi.DesfechoDoPolish{Motivo: string(e.Motivo), Provedor: e.Provedor, Quando: e.Quando}, true
	}
}
