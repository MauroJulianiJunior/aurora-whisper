// O texto da busca única (ADR-022, "Busca única"): achar o trecho, marcá-lo e
// resumir a nota e o ditado em uma linha. Puro (sem React, sem rede), para a
// regra ficar testável sem tela.
//
// A comparação não diferencia maiúscula nem acento, como a do Agent
// (storage.dobrar), nos ditados e nas notas: "reuniao" acha "Reunião".

import type { Ditado } from '../api'
import { inicioDoCorpo, lerCorpo } from '../notas/corpo'
import type { Nota } from '../notas/tipos'

// dobrado é um texto sem maiúscula nem acento e, para cada letra dele, o lugar
// da letra no texto original: o destaque é marcado no original, e uma letra
// acentuada pode ter mais ou menos unidades que a dobrada (ã é uma só; no NFD,
// duas).
interface Dobrado {
  texto: string
  // origem[i]: o índice, no original, da letra que gerou texto[i]. Tem uma
  // posição a mais: o fim do original.
  origem: number[]
}

function dobrar(original: string): Dobrado {
  let texto = ''
  const origem: number[] = []
  for (let i = 0; i < original.length; ) {
    const letra = String.fromCodePoint(original.codePointAt(i)!)
    // O acento solto (\p{M}) some, de quem escreveu em NFD; o resto vai para minúscula.
    const d = letra.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
    for (let k = 0; k < d.length; k++) origem.push(i)
    texto += d
    i += letra.length
  }
  origem.push(original.length)
  return { texto, origem }
}

// semAcento põe o texto em minúscula e sem acento: a forma em que se compara.
export const semAcento = (s: string) => dobrar(s).texto

// contem diz se o texto tem o termo (sem diferenciar maiúscula nem acento). Termo
// vazio não casa com nada.
export function contem(texto: string, termo: string): boolean {
  const t = semAcento(termo.trim())
  return t !== '' && semAcento(texto).includes(t)
}

export interface Trecho {
  texto: string
  marcado: boolean
}

// destacar parte o texto nos pedaços que casam com o termo (marcado) e nos que
// não. Juntos, os pedaços são o texto original, letra por letra.
export function destacar(texto: string, termo: string): Trecho[] {
  const t = semAcento(termo.trim())
  if (t === '' || texto === '') return [{ texto, marcado: false }]
  const { texto: d, origem } = dobrar(texto)
  const partes: Trecho[] = []
  let desde = 0
  for (let i = d.indexOf(t); i !== -1; i = d.indexOf(t, i + t.length)) {
    const ini = origem[i]!
    const fim = origem[i + t.length]!
    if (ini > desde) partes.push({ texto: texto.slice(desde, ini), marcado: false })
    partes.push({ texto: texto.slice(ini, fim), marcado: true })
    desde = fim
  }
  if (desde < texto.length) partes.push({ texto: texto.slice(desde), marcado: false })
  return partes.length > 0 ? partes : [{ texto, marcado: false }]
}

// recortar põe o texto numa linha só e, se passa de `max` letras, o corta em
// volta da primeira vez que o termo aparece, com reticências nas pontas cortadas.
// Sem o termo, fica o começo.
export function recortar(texto: string, termo: string, max = 140): string {
  const linha = texto.replace(/\s+/g, ' ').trim()
  if (linha.length <= max) return linha
  const t = semAcento(termo.trim())
  const { texto: d, origem } = dobrar(linha)
  const achou = t === '' ? -1 : d.indexOf(t)
  // O termo aparece perto do começo da janela, com um pouco de contexto antes.
  let ini = achou === -1 ? 0 : Math.max(0, origem[achou]! - Math.floor(max / 4))
  let fim = Math.min(linha.length, ini + max)
  ini = Math.max(0, fim - max)
  // Não corta uma palavra ao meio quando há espaço por perto.
  if (ini > 0) {
    const espaco = linha.indexOf(' ', ini)
    if (espaco !== -1 && espaco - ini < 12) ini = espaco + 1
  }
  if (fim < linha.length) {
    const espaco = linha.lastIndexOf(' ', fim)
    if (espaco > ini && fim - espaco < 12) fim = espaco
  }
  return `${ini > 0 ? '…' : ''}${linha.slice(ini, fim).trim()}${fim < linha.length ? '…' : ''}`
}

export interface ResumoDaNota {
  // titulo: o título da nota ('' quando ela não tem).
  titulo: string
  // linha: a primeira linha do corpo que casa com o termo; se o termo só está no
  // título, o começo do corpo.
  linha: string
}

// resumirNota é o que a linha de resultado diz da nota: "Título · linha que casa".
// Os prefixos do corpo ("- ", "# ", "- [ ] ") não entram.
export function resumirNota(nota: Pick<Nota, 'titulo' | 'corpo'>, termo: string): ResumoDaNota {
  const titulo = nota.titulo.trim()
  const linhas = lerCorpo(nota.corpo)
  const achada = linhas.find((l) => contem(l.texto, termo))
  const linha = achada ? achada.texto : titulo ? inicioDoCorpo(linhas, 120) : ''
  return { titulo, linha: recortar(linha, termo, 120) }
}

// textoDoDitado é o texto do ditado que a linha de resultado mostra: o final, ou
// o bruto quando só ele casa (o Agent procura nos dois) ou quando não há final.
export function textoDoDitado(d: Pick<Ditado, 'final_text' | 'raw_text'>, termo: string): string {
  if (d.final_text.trim() !== '' && (contem(d.final_text, termo) || !contem(d.raw_text, termo))) return d.final_text
  return d.raw_text
}

const meses = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez']

const mesmoDia = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()

// quandoNaBusca é o "quando" curto da linha do ditado, na mono: a hora se foi
// hoje, "ontem", ou o dia e o mês ("22 out", com o ano se não é o deste).
export function quandoNaBusca(iso: string, hoje: Date): string {
  const d = new Date(iso)
  if (mesmoDia(d, hoje)) return d.toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' })
  const ontem = new Date(hoje)
  ontem.setDate(hoje.getDate() - 1)
  if (mesmoDia(d, ontem)) return 'ontem'
  const dia = `${d.getDate()} ${meses[d.getMonth()]}`
  return d.getFullYear() === hoje.getFullYear() ? dia : `${dia} ${String(d.getFullYear()).slice(2)}`
}
