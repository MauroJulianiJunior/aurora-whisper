package tray

import "unicode/utf16"

// Acoes é o que a bandeja faz quando a pessoa escolhe um item. A bandeja
// não sabe pausar o hook, nem gravar no registro, nem encerrar o Agent:
// ela chama o que cmd/agent ligou aqui. Assim o pacote não depende de
// hook, autostart nem do ciclo de vida do processo.
//
// Todas são chamadas na thread da bandeja.
type Acoes struct {
	// Estado é lido a cada abertura do menu — ver o comentário de Estado.
	Estado func() Estado
	// Pausar recebe o NOVO estado da pausa.
	Pausar func(bool)
	// IniciarComWindows recebe o NOVO estado do início automático.
	IniciarComWindows func(bool) error
	// Sair encerra o Agent.
	Sair func()
	// Abrir abre o painel no navegador. Opcional: nil é o mesmo que o
	// painel não ter subido, e Estado.PainelDisponivel deve dizer isso.
	Abrir func() error
	// AoFalhar recebe o erro do que não deu certo depois da subida, quando
	// não há mais Iniciar para devolvê-lo: uma ação do menu (gravar no
	// registro, abrir o painel) ou o ícone recusado depois de o Explorer
	// reiniciar. Nil descarta.
	AoFalhar func(error)
	// AoVoltar é chamada quando o ícone volta à bandeja depois de uma
	// recusa que AoFalhar já registrou. Sem ela, a última linha da bandeja
	// no log seguia dizendo "tentando de novo" com o ícone já de volta, e
	// quem investigasse não sabia se ele voltou nem quando (revisão de
	// 29-set-2026). Nil descarta.
	AoVoltar func()
}

// dicaUTF16 converte a dica para o campo szTip de NOTIFYICONDATAW: UTF-16,
// terminada em zero, com no máximo maxDica unidades contando o zero.
//
// Corta em fronteira de CARACTERE, não de unidade: um caractere fora do
// plano básico ocupa duas unidades UTF-16, e cortar entre elas deixaria
// um surrogate solto — que o Windows desenha como um quadrado.
func dicaUTF16(s string) [maxDica]uint16 {
	var out [maxDica]uint16
	n := 0
	for _, r := range s {
		u := utf16.Encode([]rune{r})
		if n+len(u) > maxDica-1 {
			break
		}
		copy(out[n:], u)
		n += len(u)
	}
	return out
}
