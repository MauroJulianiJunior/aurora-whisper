// Package instancia garante que só um Agent rode por sessão de usuário.
//
// # Por que existe
//
// Em 27-set-2026 Mauro passou uma noite investigando texto duplicado. A
// causa era outro programa de ditado — o Wispr Flow — rodando no MESMO
// atalho: os dois gravavam, os dois transcreviam, os dois inseriam (ver
// internal/agent/conflito).
//
// Com o início automático (internal/agent/autostart), o próprio Aurora
// Whisper passa a poder causar isso sozinho: o Windows sobe um Agent, a
// pessoa não repara no ícone da bandeja, abre o executável de novo — e
// agora são dois hooks na mesma tecla. O sintoma seria idêntico ao da
// noite de 27-set, e desta vez a causa seria nossa.
//
// # Como
//
// Um mutex nomeado do Windows no namespace "Local\": um por sessão de
// usuário. Duas pessoas logadas na mesma máquina rodam um Agent cada; a
// mesma pessoa, um só.
//
// O mutex some quando o processo termina, de qualquer forma — inclusive
// morto pelo Gerenciador de Tarefas. Não há arquivo de trava para ficar
// órfão e impedir a próxima subida.
package instancia

import "errors"

// NomeDoAgent é o nome do mutex do Agent de verdade. Os testes usam
// outro nome, sempre — para nunca disputar com um Agent rodando na
// máquina de quem roda a suíte.
const NomeDoAgent = `Local\AuroraWhisperAgent`

// ErrJaRodando é devolvido quando outro processo já detém a instância.
var ErrJaRodando = errors.New("instancia: o Aurora Whisper já está rodando nesta sessão")

// ErrNaoSuportado é devolvido fora do Windows.
var ErrNaoSuportado = errors.New("instancia: só existe no Windows")
