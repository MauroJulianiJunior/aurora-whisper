package tray

import "strings"

// Tema é a cor da barra de tarefas, que decide a variante do ícone: o
// ícone não tem fundo próprio (ver PixelsDoIcone), então a barra dele
// precisa contrastar com a barra de tarefas.
type Tema int

const (
	// TemaEscuro é o padrão do Windows para a barra de tarefas, e o que
	// vale quando não se sabe.
	TemaEscuro Tema = iota
	TemaClaro
)

// Onde o Windows diz de que cor é a barra de tarefas: o valor DWORD
// SystemUsesLightTheme, 1 para clara. É o "modo do Windows" das
// Configurações; o "modo dos aplicativos" é outro valor
// (AppsUseLightTheme), e não interessa à bandeja.
const (
	chaveDoTema = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	valorDoTema = "SystemUsesLightTheme"

	// areaDoTema é o texto que vem no lParam do WM_SETTINGCHANGE quando o
	// modo claro/escuro muda.
	areaDoTema = "ImmersiveColorSet"
)

// TemaDaBarra decide a variante a partir da leitura do registro.
//
// Qualquer falha de leitura — o valor ausente, que é o caso do Windows 10
// antes da 1903, sem barra clara; um tipo inesperado; a chave ilegível —
// fica com o escuro, o padrão do Windows. Errar para o escuro numa barra
// clara deixa a barra do ícone clara sobre claro, mas os colchetes
// continuam lá; não há leitura melhor para oferecer.
func TemaDaBarra(valor uint64, err error) Tema {
	if err == nil && valor != 0 {
		return TemaClaro
	}
	return TemaEscuro
}

// mudouOTema reporta se um WM_SETTINGCHANGE é a troca de modo claro/escuro.
// O Windows manda WM_SETTINGCHANGE a toda janela de nível superior por
// muita coisa — idioma, política, variáveis de ambiente —, e só esta
// interessa ao ícone.
func mudouOTema(area string) bool {
	return strings.EqualFold(area, areaDoTema)
}
