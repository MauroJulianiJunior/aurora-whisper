// Quanto o aviso espera antes de sumir sozinho, sem ponteiro nem foco em
// cima dele. Quem tem uma ação espera mais, e o que só informa, menos.
//
// O aviso com "Aprender" espera bem mais (revisão de 30-set-2026, ADR-017):
// ele fica no fim da página, e quem usa só o teclado chega nele por Alt+A
// ou por dezenas de Tabs; quem usa leitor de tela o ouve e depois decide.
// Sumir em 8 s tiraria a sugestão de quem ainda está chegando. Nenhum
// tempo vale para quem tem o foco ou o ponteiro em cima (WCAG 2.2.1): a
// pílula espera enquanto um dos dois estiver nela.
export const tempoDoAviso = { simples: 4000, comDesfazer: 8000, comSugestao: 30000 } as const

export function esperaDoAviso(a: { desfazer?: unknown; aprender?: unknown }): number {
  if (a.aprender) return tempoDoAviso.comSugestao
  return a.desfazer ? tempoDoAviso.comDesfazer : tempoDoAviso.simples
}
