import type { KeyboardEvent } from 'react'

// prenderOFoco é o Tab de uma janela modal (a nota aberta, o Editar marcadores): o
// foco não sai dela. Tab no último campo volta ao primeiro, e Shift+Tab no
// primeiro vai ao último; com o foco fora da janela, o Tab o traz de volta.
// Devolve true quando tratou a tecla.
export function prenderOFoco(e: KeyboardEvent, janela: HTMLElement | null): boolean {
  if (e.key !== 'Tab' || !janela) return false
  const focaveis = Array.from(janela.querySelectorAll<HTMLElement>('button, input, textarea, [tabindex]')).filter(
    (el) => !el.hasAttribute('disabled') && el.getAttribute('tabindex') !== '-1',
  )
  const primeiro = focaveis[0]
  const ultimo = focaveis[focaveis.length - 1]
  if (!primeiro || !ultimo) return false
  const ativo = document.activeElement
  if (e.shiftKey && (ativo === primeiro || !janela.contains(ativo))) {
    e.preventDefault()
    ultimo.focus()
    return true
  }
  if (!e.shiftKey && (ativo === ultimo || !janela.contains(ativo))) {
    e.preventDefault()
    primeiro.focus()
    return true
  }
  return false
}
