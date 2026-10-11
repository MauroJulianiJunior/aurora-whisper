package main

import (
	"log/slog"
	"os"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/painel"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// versaoDeDesenvolvimento é o valor de version num build sem -X: o de
// desenvolvimento (go build ./cmd/agent, o bin\). O build de release
// (scripts/montar-instalador.ps1) injeta a versão de verdade, e aí o Agent
// deixa de ser "de desenvolvimento" para a nuvem. TestVersaoDeDesenvolvimento
// prende os dois valores juntos: trocar o literal de version sem trocar este
// faria o bin\ passar por release (e conectar fora do modo portátil).
const versaoDeDesenvolvimento = "0.1.0-sprint2"

// politicaDaNuvem é o que o build e o modo deixam conectar (ADR-019, D7): o
// build de desenvolvimento só conecta no modo portátil (o bin\, com a conta de
// teste), para nunca ligar os dados reais à nuvem por engano; o de release, só
// com https.
func politicaDaNuvem(versao string, modo pastas.Modo) nuvem.Politica {
	return nuvem.Politica{
		Dev:      versao == versaoDeDesenvolvimento,
		Portatil: modo == pastas.ModoPortatil || modo == pastas.ModoPortatilLegado,
	}
}

// historicoDaNuvem é o lado do histórico da nuvem no Agent (ADR-019, F7c): a
// fila do dono e o arquivo do historico.db, ao lado do qual fica a cópia do
// envio inicial.
func historicoDaNuvem(hist *storage.SQLiteTranscriptionRepository, dono, caminhoBanco string) nuvem.Historico {
	return storage.HistoricoDaNuvem{Fila: hist.Fila(dono), CaminhoDoBanco: caminhoBanco}
}

// montarNuvem monta o "Conectar à nuvem" do Agent (ADR-019, F6) sobre a
// tabela instalacao do historico.db. As duas ações no sistema real passam
// pelo portão sysinput, que recusa sob go test: o navegador
// (painel.AbrirNoNavegador) e o Gerenciador de Credenciais
// (chaves.NovoGerenciador, no alvo do modo: AuroraWhisper/nuvem no instalado,
// AuroraWhisper/nuvem-portatil no bin\). nuvem_windows_test.go prova que,
// montado assim e sob go test, nada abre e nada é gravado.
//
// A porta do retorno é a do painel, que só se sabe depois do Listen: quem
// sobe o painel chama UsarPorta.
//
// hist é o lado do histórico (F7c: o resumo, o "Enviar o histórico", com a
// cópia ao lado do historico.db, e o "Esquecer esta nuvem"); nil deixa as
// duas ações em 503. Ele não passa pelo portão (é o banco), mas as duas
// ações passam pelo Gerenciador antes de tocar no banco (o envio confere que
// o PC está conectado; o esquecer apaga o token), e o portão as para sob go
// test.
func montarNuvem(inst nuvem.Instalacao, hist nuvem.Historico, modo pastas.Modo, log *slog.Logger) *nuvem.Servico {
	pol := politicaDaNuvem(version, modo)
	nome, err := os.Hostname()
	if err != nil {
		// Sem o nome, a nuvem mostra "PC" (nuvem.NomeDoPC).
		nome = ""
	}
	return nuvem.Novo(nuvem.Config{
		Cofre:       chaves.NovoGerenciador(),
		NomeNoCofre: nuvem.NomeNoCofre(pol.Portatil),
		Abrir:       painel.AbrirNoNavegador,
		Instalacao:  inst,
		Politica:    pol,
		Versao:      version,
		NomeDoPC:    nome,
		NovoID:      storage.NewID,
		Historico:   hist,
		Log:         log,
	})
}
