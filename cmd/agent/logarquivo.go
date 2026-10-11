package main

import (
	"io"
	"log"
	"os"
	"runtime/debug"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
)

// limiteDoLog é o tamanho a partir do qual o agent.log é girado na
// subida. Com o anterior guardado como agent.log.1, o disco ocupado fica
// em no máximo o dobro disto.
const limiteDoLog = 5 << 20 // 5 MB

// configurarLog manda o log para o agent.log, na pasta de dados (ao lado do
// executável só no modo portátil: ver internal/agent/pastas).
//
// # Por que um arquivo
//
// Desde 28-set-2026 o Agent roda como aplicativo de bandeja, sem console
// — e sem console, stderr é um handle inválido. Todo o diagnóstico que
// resolveu os defeitos deste projeto (o texto duplicado, o falso "campo
// protegido", o corte do vocabulário) veio de ler o log. Sem arquivo,
// ele iria para lugar nenhum.
//
// Com console — rodado de um terminal, no desenvolvimento —, vai para os
// dois: o arquivo e a tela.
//
// # O que NÃO vai para o log
//
// Continua valendo o que 09-security-privacy.md proíbe: nem áudio, nem
// texto ditado, nem contexto de janela. O arquivo só muda o destino do
// que já era escrito, e nada do que era escrito continha isso.
//
// Nem o caminho completo de uma pasta (ADR-018): ele leva o nome de
// usuário do Windows, e o log é o que a pessoa cola num issue. As linhas
// que o Agent escreve dizem só o modo (dados=localappdata); e o que sobrar
// de caminho numa mensagem de erro do sistema sai trocado por um rótulo
// (saidaDoLog). O pânico do runtime (SetCrashOutput, abaixo) é escrito
// direto no arquivo, sem passar por ali.
//
// Devolve a função que fecha o arquivo. Falhar em abrir não impede o
// Agent de subir: o log continua indo para onde ia antes.
func configurarLog(p pastas.Pastas, perfil string) func() {
	caminho := caminhosDe(p).LogDoAgent
	girarSeGrande(caminho, limiteDoLog)

	f, err := os.OpenFile(caminho, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}
	}
	// Pânico e "fatal error" do runtime não passam pelo pacote log: o Go
	// os escreve direto no stderr, que sem console não existe. Sem isto, um
	// pânico em qualquer goroutine — o WndProc da bandeja, o callback do
	// hook — fazia o Agent sumir sem uma linha no arquivo.
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	log.SetOutput(saidaDoLog(f, p, perfil, temConsole()))
	return func() { f.Close() }
}

// saidaDoLog é o destino do log: o arquivo, com os caminhos de quem usa
// trocados por rótulos (pastas.Anonimizar) e, com console, também a tela —
// essa crua, porque é o terminal de quem está desenvolvendo.
func saidaDoLog(arquivo io.Writer, p pastas.Pastas, perfil string, console bool) io.Writer {
	w := pastas.Anonimizar(arquivo, p, perfil)
	if console {
		w = io.MultiWriter(w, os.Stderr)
	}
	return w
}

// girarSeGrande move caminho para caminho+".1" quando passa de limite.
// Só na subida: girar com o arquivo aberto exigiria coordenar com quem
// escreve, e um Agent que roda o dia inteiro não gera 5 MB num dia.
//
// E só DEPOIS da instância única (main): um segundo Agent aberto por
// engano girava o log do primeiro, apagando o agent.log.1 dele, antes de
// descobrir que ia sair.
func girarSeGrande(caminho string, limite int64) {
	info, err := os.Stat(caminho)
	if err != nil || info.Size() < limite {
		return
	}
	_ = os.Remove(caminho + ".1")
	_ = os.Rename(caminho, caminho+".1")
}
