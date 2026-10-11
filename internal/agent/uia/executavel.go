package uia

// Este arquivo não tem build tag: é a regra de QUANDO o nome do executável
// em foco pode ir para o diagnóstico, e de que forma — só o nome-base.

import "strings"

// nomeBase devolve só o nome do arquivo de caminho: tudo depois da última
// barra (as duas, "\" e "/") ou dois-pontos de unidade. É feito à mão, e
// não com filepath.Base, porque filepath.Base no Linux não trata "\" como
// separador — e esta regra é testada no runner Linux também.
//
// "C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe"
// vira "brave.exe".
func nomeBase(caminho string) string {
	if i := strings.LastIndexAny(caminho, `\/:`); i >= 0 {
		return caminho[i+1:]
	}
	return caminho
}

// executavelDoAviso é o que vai para DiagnosticoFoco.Executavel: o
// nome-base do executável em foco, SÓ numa inspeção em dúvida — as que
// saem em WARN (protegido sem ser campo de senha lido). Em qualquer outra,
// "".
//
// # A exceção à regra de não registrar contexto de janela
//
// Autorizada pelo Mauro em 29-set-2026 (decisão 5; registrada em
// docs/ai-spec/09-security-privacy.md, na ADR-011 e no CLAUDE.md): sem
// isto, o log de um travamento nunca diz QUAL aplicativo travou. Só o
// nome-base do executável (ex.: brave.exe) — nunca título de janela,
// classe, caminho completo ou texto —, e só nas linhas WARN do rastreador,
// nunca em Info/Debug. Esta função garante a primeira metade (só em
// dúvida, só o nome-base); registroDoFoco, em cmd/agent, a segunda (só no
// nível WARN).
//
// De onde vem o nome: do processo do elemento, quando a UIA o devolveu
// (timeout); senão, do processo da janela em primeiro plano, lido depois
// de uma inspeção sem elemento (lerPrimeiroPlano). nomeBase é aplicado
// aqui de novo mesmo que quem leu já o tenha feito: um caminho completo
// não atravessa esta fronteira nem por uma mudança distraída do outro
// lado.
func executavelDoAviso(s Snapshot) string {
	if !s.Protected() || s.Outcome == InspectOK {
		return ""
	}
	nome := s.ProcessName
	if nome == "" {
		nome = s.ExecutavelDoPrimeiroPlano
	}
	return nomeBase(nome)
}
