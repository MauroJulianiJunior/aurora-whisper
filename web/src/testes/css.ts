// O index.css lido como texto, para os testes que conferem o que o jsdom
// não aplica: os tokens de cada tema, o bloco de uma regra, o valor de uma
// propriedade e o contraste do WCAG. estilo.test.ts, fontes.test.ts e
// marca.test.tsx usam.

import { expect } from 'vitest'
import bruto from '../index.css?raw'

export const css = bruto.replace(/\r\n/g, '\n')

export type Tema = Record<string, string>

// tokens pega as propriedades --nome: #rrggbb de um trecho.
export function tokens(trecho: string): Tema {
  return Object.fromEntries(Array.from(trecho.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\s*;/gi), (m) => [m[1]!, m[2]!.toLowerCase()]))
}

// bloco devolve o que está entre a chave que abre depois de `inicio` e a
// que fecha no mesmo nível.
export function bloco(inicio: string): string {
  const i = css.indexOf(inicio)
  expect(i, `falta "${inicio}" no index.css`).toBeGreaterThanOrEqual(0)
  const abre = css.indexOf('{', i)
  let nivel = 0
  for (let j = abre; j < css.length; j++) {
    if (css[j] === '{') nivel++
    else if (css[j] === '}' && --nivel === 0) return css.slice(abre + 1, j)
  }
  throw new Error(inicio)
}

// Claro é o :root; escuro é o :root com o que o bloco do tema escuro troca.
export const temaClaro = tokens(bloco(':root {'))
export const temaEscuro: Tema = { ...temaClaro, ...tokens(bloco('@media (prefers-color-scheme: dark)')) }
export const temas = { claro: temaClaro, escuro: temaEscuro }

// Regra é uma regra sem nada aninhado: o seletor (sem espaços nas pontas)
// e as declarações. As de dentro de um @media também entram.
export interface Regra {
  seletor: string
  corpo: string
}

export const regras: Regra[] = Array.from(css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g), (m) => ({
  seletor: m[1]!.trim(),
  corpo: m[2]!,
}))

// valor devolve o que `prop` vale no corpo de uma regra, sem o ponto e
// vírgula. Casa a propriedade inteira: "color" não pega "border-color".
export function valor(corpo: string, prop: string): string | undefined {
  return corpo.match(new RegExp(`(?:^|[;\\s])${prop}:\\s*([^;]+);`))?.[1]?.trim()
}

// Luminância relativa e razão de contraste do WCAG 2.2.
function luminancia(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

export function contraste(a: string, b: string): number {
  const [claro, escuro] = [luminancia(a), luminancia(b)].sort((x, y) => y - x) as [number, number]
  return (claro + 0.05) / (escuro + 0.05)
}
