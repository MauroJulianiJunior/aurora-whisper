// Package autostart liga e desliga o início do Agent junto com o Windows
// (AC-037, REQ-039: "Autostart liga/desliga").
//
// # Onde fica
//
// HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run, valor
// "AuroraWhisper". HKCU, e não HKLM: vale só para quem liga, não exige
// administrador, e não mexe na máquina de mais ninguém (TASK-021: "HKCU
// autostart toggle").
//
// # Quem liga
//
// A pessoa, pelo menu da bandeja. Nunca o instalador por conta própria,
// nunca a primeira execução: gravar no registro é mudar configuração do
// sistema de quem usa, e isso é decisão dela.
//
// A escrita passa pelo portão de internal/agent/sysinput, como toda ação
// que atua no sistema real — e o portão recusa incondicionalmente sob go
// test. Nenhum teste deste pacote grava no registro da máquina.
//
// Este arquivo não tem build tag: a lógica de decisão (qual valor gravar,
// se o valor existente aponta para este executável) roda e é testada no
// runner Linux do CI. O acesso ao registro fica em autostart_windows.go.
package autostart

import (
	"errors"
	"strings"
)

const (
	// ChaveRun é a chave de início automático por usuário.
	ChaveRun = `Software\Microsoft\Windows\CurrentVersion\Run`
	// NomeDoValor identifica a entrada do Aurora Whisper dentro dela.
	NomeDoValor = "AuroraWhisper"

	// ChaveAprovados é onde o PRÓPRIO Windows guarda o liga/desliga da
	// aba Inicializar do Gerenciador de Tarefas e de Configurações >
	// Aplicativos > Inicialização. Desligar ali NÃO apaga o valor em
	// ChaveRun: grava uma marca aqui, com o mesmo NomeDoValor. Ler só
	// ChaveRun mostrava o item marcado na bandeja com o início desligado
	// pelo Windows (revisão de 28-set-2026).
	ChaveAprovados = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// DesativadoPeloWindows interpreta o valor de ChaveAprovados.
//
// O formato não é documentado pela Microsoft. O que se observou nesta
// máquina (reg query, só leitura, 28-set-2026): 12 bytes, o primeiro 02
// nos itens ativos e 03 nos desativados, com a hora da desativação nos 8
// últimos. Há relatos de 06/07 com o mesmo sentido; o bit mais baixo é o
// que separa os dois nos quatro casos.
//
// Valor ausente é ativo: é o estado de toda entrada que ninguém desligou.
func DesativadoPeloWindows(dados []byte) bool {
	return len(dados) > 0 && dados[0]&1 == 1
}

// ErrNaoSuportado é devolvido fora do Windows.
var ErrNaoSuportado = errors.New("autostart: só existe no Windows")

// ComandoPara é o valor gravado para iniciar exe: o caminho entre aspas.
//
// As aspas não são enfeite. O Windows interpreta o valor como linha de
// comando, e sem aspas um caminho com espaço — "C:\Program Files\..." —
// seria partido no primeiro espaço, e o Windows tentaria executar
// "C:\Program".
func ComandoPara(exe string) string {
	return `"` + exe + `"`
}

// Aponta reporta se o valor gravado no registro inicia exe.
//
// É o que decide se o item "Iniciar com o Windows" aparece marcado. Não
// basta o valor EXISTIR: se o executável foi movido de pasta, a entrada
// antiga aponta para um caminho que não existe mais, e mostrá-la como
// ligada seria afirmar um início automático que não vai acontecer.
//
// Compara sem caixa e sem aspas, e com as duas barras como equivalentes:
// é assim que o Windows trata caminho.
func Aponta(gravado, exe string) bool {
	return normalizarCaminho(gravado) != "" && normalizarCaminho(gravado) == normalizarCaminho(exe)
}

func normalizarCaminho(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	s = strings.ReplaceAll(s, "/", `\`)
	return strings.ToLower(s)
}
