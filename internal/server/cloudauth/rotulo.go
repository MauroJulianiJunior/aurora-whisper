package cloudauth

import "strings"

// RotuloPadrao é o rótulo de um navegador que o User-Agent não identifica.
const RotuloPadrao = "Navegador"

// Rotulo devolve um rótulo curto do aparelho para a lista de sessões (por
// exemplo, "Android · Chrome"), a partir do User-Agent. Nunca o User-Agent
// inteiro: só um sistema e um navegador de listas fechadas, então o rótulo
// cabe nos 64 caracteres do banco (web_sessions.label) e não guarda versão,
// modelo do aparelho nem nada que o identifique além disso.
func Rotulo(userAgent string) string {
	sistema := primeiro(userAgent, []par{
		{"Android", "Android"},
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"CrOS", "ChromeOS"},
		{"Windows", "Windows"},
		{"Macintosh", "Mac"},
		{"Linux", "Linux"},
	})
	// A ordem importa: o Edge e o Opera também dizem "Chrome", e o Chrome
	// também diz "Safari".
	navegador := primeiro(userAgent, []par{
		{"Edg", "Edge"},
		{"OPR/", "Opera"},
		{"SamsungBrowser", "Samsung Internet"},
		{"Firefox/", "Firefox"},
		{"FxiOS", "Firefox"},
		{"CriOS", "Chrome"},
		{"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
	})
	switch {
	case sistema != "" && navegador != "":
		return sistema + " · " + navegador
	case navegador != "":
		return navegador
	case sistema != "":
		return sistema
	}
	return RotuloPadrao
}

type par struct{ marca, nome string }

func primeiro(ua string, pares []par) string {
	for _, p := range pares {
		if strings.Contains(ua, p.marca) {
			return p.nome
		}
	}
	return ""
}
