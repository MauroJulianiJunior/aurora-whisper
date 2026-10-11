// As fontes do design system vêm embutidas no build (fontes.css): o
// servidor do painel manda CSP default-src 'self', e fonte de fora não
// carrega. Aqui se confere que só vai o que o painel usa — os pesos
// combinados, em latin e latin-ext, só woff2 — e que cada arquivo e cada
// unicode-range são os do próprio pacote @fontsource.

import { describe, expect, it } from 'vitest'
import bruto from './fontes.css?raw'
import principal from './main.tsx?raw'
import { regras, valor } from './testes/css'

const fontes = bruto.replace(/\r\n/g, '\n')

// Os pesos combinados com o Mauro (29-set-2026).
const pesos: Record<string, number[]> = {
  'Space Grotesk': [600, 700],
  Inter: [400, 500, 600],
  'IBM Plex Mono': [400, 500],
}
const subconjuntos = ['latin', 'latin-ext']

interface Face {
  familia: string
  peso: number
  estilo?: string
  exibicao?: string
  src: string
  arquivo: string
  pacote: string
  intervalo: string
}

// faces lê os @font-face de um CSS. O src de fontes.css aponta para o
// pacote ('@fontsource/<pacote>/files/<arquivo>'); o dos CSS do pacote,
// para './files/<arquivo>'.
function faces(texto: string, pacote = ''): Face[] {
  return Array.from(texto.matchAll(/@font-face\s*\{([^}]*)\}/g), ([, corpo]) => {
    const src = valor(corpo!, 'src') ?? ''
    const url = src.match(/url\('?([^')]+)'?\)/)?.[1] ?? ''
    return {
      familia: (valor(corpo!, 'font-family') ?? '').replace(/'/g, ''),
      peso: Number(valor(corpo!, 'font-weight')),
      estilo: valor(corpo!, 'font-style'),
      exibicao: valor(corpo!, 'font-display'),
      src,
      arquivo: url.split('/').pop() ?? '',
      pacote: url.match(/^@fontsource\/([\w-]+)\//)?.[1] ?? pacote,
      intervalo: (valor(corpo!, 'unicode-range') ?? '').replace(/\s+/g, ''),
    }
  })
}

const nossas = faces(fontes)

// Os CSS por peso dos pacotes (400.css e afins), que declaram cada
// subconjunto com o seu unicode-range.
const doPacote = Object.entries(
  import.meta.glob<string>('/node_modules/@fontsource/{inter,space-grotesk,ibm-plex-mono}/{400,500,600,700}.css', {
    query: '?raw',
    import: 'default',
    eager: true,
  }),
).flatMap(([caminho, texto]) => faces(texto.replace(/\r\n/g, '\n'), caminho.split('/')[3]))

describe('fontes embutidas', () => {
  it('o painel importa fontes.css', () => {
    expect(principal).toMatch(/import '\.\/fontes\.css'/)
  })

  it('só os pesos combinados, cada um em latin e latin-ext', () => {
    const esperado = Object.entries(pesos).flatMap(([familia, ps]) => ps.flatMap((p) => subconjuntos.map((s) => `${familia} ${p} ${s}`)))
    const temos = nossas.map((f) => `${f.familia} ${f.peso} ${f.arquivo.match(/-(latin(?:-ext)?)-\d+-normal/)?.[1]}`)
    expect(temos.sort()).toEqual(esperado.sort())
  })

  it('só woff2, normal, com font-display: swap', () => {
    for (const f of nossas) {
      expect(f.src, f.arquivo).toMatch(/^url\('@fontsource\/[\w-]+\/files\/[\w-]+\.woff2'\) format\('woff2'\)$/)
      expect(f.estilo, f.arquivo).toBe('normal')
      expect(f.exibicao, f.arquivo).toBe('swap')
    }
  })

  // Sem o unicode-range, o @font-face do latin-ext cobria o do latin, e o
  // navegador baixava o latin-ext para todo texto.
  it('cada arquivo existe no pacote, com o mesmo unicode-range que o pacote declara', () => {
    expect(doPacote.length).toBeGreaterThan(0)
    for (const f of nossas) {
      const par = doPacote.find((p) => p.pacote === f.pacote && p.arquivo === f.arquivo)
      expect(par, `${f.arquivo} não está no CSS do pacote @fontsource/${f.pacote}`).toBeDefined()
      expect(f.familia).toBe(par!.familia)
      expect(f.peso).toBe(par!.peso)
      expect(f.intervalo, f.arquivo).toBe(par!.intervalo)
    }
  })

  it('o português inteiro cabe no latin', () => {
    const latin = nossas.find((f) => f.arquivo.includes('-latin-4'))!.intervalo.split(',')
    const cobre = (c: number) =>
      latin.some((faixa) => {
        const [de, ate = de] = faixa.replace('U+', '').split('-').map((h) => parseInt(h, 16))
        return c >= de! && c <= ate!
      })
    for (const ch of 'ÀÁÂÃÇÉÊÍÓÔÕÚàáâãçéêíóôõúü“”‘’—–…·ºª€') {
      expect(cobre(ch.codePointAt(0)!), ch).toBe(true)
    }
  })

  it('nada vem de fora', () => {
    expect(fontes).not.toMatch(/https?:|@import/)
  })
})

// Cada regra do index.css que escolhe família ou peso pede um peso que
// está embutido. Sem isto, um 700 da Inter viraria negrito sintético.
describe('o index.css só pede o que está embutido', () => {
  const familiaDe = (v?: string) => (v === 'var(--fonte-titulo)' ? 'Space Grotesk' : v === 'var(--fonte-mono)' ? 'IBM Plex Mono' : 'Inter')
  const pedidas = regras
    .filter((r) => /font-(family|weight):/.test(r.corpo))
    .map((r) => ({ seletor: r.seletor, familia: familiaDe(valor(r.corpo, 'font-family')), peso: Number(valor(r.corpo, 'font-weight') ?? 400) }))

  it.each(pedidas.map((p) => [p.seletor, p]))('%s', (_, p) => {
    expect(pesos[p.familia], p.familia).toContain(p.peso)
  })

  // O navegador põe <strong> e <b> em negrito (bolder, 700) por conta
  // própria, sem regra nenhuma no index.css, e a lista acima não vê. A
  // Inter 700 não está no build: sem a regra strong, o negrito da faixa e
  // da tela de bloqueio ficava por conta do navegador — o 600 no lugar, ou
  // negrito sintético (revisão de 29-set-2026: apagar a regra não quebrava
  // teste nenhum).
  it('<strong> pede um peso embutido, e <b> não é usado', () => {
    const componentes = Object.values(
      import.meta.glob<string>(['./**/*.tsx', '!./**/*.test.tsx'], { query: '?raw', import: 'default', eager: true }),
    ).join('\n')
    expect(componentes).toMatch(/<strong>/)
    expect(componentes).not.toMatch(/<b[\s>]/)
    const strong = regras.find((r) => r.seletor === 'strong')
    expect(strong, 'falta a regra strong no index.css').toBeDefined()
    expect(pesos.Inter).toContain(Number(valor(strong!.corpo, 'font-weight')))
  })
})
