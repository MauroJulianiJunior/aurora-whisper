package painel

// ConfiguracoesDoMicrofone é a tela de privacidade do microfone do Windows
// (Configurações > Privacidade e segurança > Microfone), onde fica a chave
// que a pessoa precisa ligar quando o Windows bloqueia o ditado.
const ConfiguracoesDoMicrofone = "ms-settings:privacy-microphone"

// AbrirConfiguracoesDoMicrofone abre a tela do Windows em que a pessoa liga
// a permissão do microfone. O app não liga a permissão sozinho — nem
// deveria: só mostra a tela certa.
//
// Passa pelo mesmo portão de AbrirNoNavegador (internal/agent/sysinput):
// abrir uma janela na tela de quem usa é agir no sistema real, e sob go
// test isso nunca acontece. O alvo é fixo, sem bilhete nem dado de ninguém.
func AbrirConfiguracoesDoMicrofone() error {
	return AbrirNoNavegador(ConfiguracoesDoMicrofone)
}
