package main

import (
	"io"
	"os"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
)

// abrirLogDoServidor cria o whisper-server.log (sobrescrito a cada subida) e
// devolve o destino da saída do servidor e a função que o fecha. O destino é o
// arquivo com os caminhos de quem usa trocados por rótulos (<programa>,
// <dados>, <perfil>), linha a linha (pastas.AnonimizarLinhas).
//
// # Por que o servidor tem o próprio filtro
//
// O whisper-server escreve, na primeira linha, de que pasta carregou a
// biblioteca do processador (load_backend: loaded CPU backend from ...). É a
// pasta do executável dele, e instalada leva o nome de usuário do Windows
// (C:\Users\<nome>\AppData\Local\Programs\...). Passar o modelo pelo nome (F2
// da ADR-018) tira o caminho da linha do modelo, mas não desta, que é do
// servidor. O log é o que a pessoa cola num issue, como o agent.log, e passa
// pelo mesmo filtro (saidaDoLog).
//
// Quem chega ao filtro são pedaços de saída de outro processo, não linhas
// inteiras: por isso AnonimizarLinhas, e não Anonimizar.
//
// Se o arquivo não abre, devolve nil: o servidor cai no stderr do Agent, como
// antes (sem console, um handle inválido, mas o Agent sobe). A função que fecha
// entrega a última linha pendente, e só depois fecha o arquivo; vale chamá-la
// DEPOIS de o servidor sair, que é quando ele para de escrever.
func abrirLogDoServidor(caminho string, p pastas.Pastas, perfil string) (io.Writer, func()) {
	arquivo, err := os.Create(caminho)
	if err != nil {
		return nil, func() {}
	}
	saida := pastas.AnonimizarLinhas(arquivo, p, perfil)
	return saida, func() {
		saida.Close()
		arquivo.Close()
	}
}
