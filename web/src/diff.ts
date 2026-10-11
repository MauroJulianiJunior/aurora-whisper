// Diferença palavra a palavra entre o bruto do ASR e o texto final.
//
// É o que mostra, num ditado, exatamente o que o resolver e o dicionário
// trocaram — e, enquanto a pessoa edita, o que ELA está trocando. Fica
// aqui, em função pura, porque é a peça do painel com lógica de verdade.

export type Trecho = { tipo: 'igual' | 'saiu' | 'entrou'; texto: string }

// Acima disto a tabela da LCS pesaria demais (n·m células); o ditado
// típico tem dezenas de palavras, então isto é só um teto de segurança.
const maxCelulas = 250_000

function palavras(texto: string): string[] {
  return texto.split(/\s+/).filter((p) => p !== '')
}

export function compararPalavras(antes: string, depois: string): Trecho[] {
  const a = palavras(antes)
  const b = palavras(depois)
  const trechos: Trecho[] = []
  const juntar = (tipo: Trecho['tipo'], palavra: string) => {
    const ultimo = trechos[trechos.length - 1]
    if (ultimo && ultimo.tipo === tipo) ultimo.texto += ` ${palavra}`
    else trechos.push({ tipo, texto: palavra })
  }

  if ((a.length + 1) * (b.length + 1) > maxCelulas) {
    if (a.join(' ') === b.join(' ')) return a.length ? [{ tipo: 'igual', texto: a.join(' ') }] : []
    if (a.length) trechos.push({ tipo: 'saiu', texto: a.join(' ') })
    if (b.length) trechos.push({ tipo: 'entrou', texto: b.join(' ') })
    return trechos
  }

  // lcs[i*(m+1)+j] = tamanho da maior subsequência comum de a[i:] e b[j:].
  const m = b.length
  const lcs = new Uint32Array((a.length + 1) * (m + 1))
  const em = (i: number, j: number) => lcs[i * (m + 1) + j] ?? 0
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i * (m + 1) + j] = a[i] === b[j] ? em(i + 1, j + 1) + 1 : Math.max(em(i + 1, j), em(i, j + 1))
    }
  }

  let i = 0
  let j = 0
  while (i < a.length && j < m) {
    const pa = a[i] as string
    const pb = b[j] as string
    if (pa === pb) {
      juntar('igual', pa)
      i++
      j++
    } else if (em(i + 1, j) >= em(i, j + 1)) {
      // Empate sai primeiro: numa troca, o que saiu vem antes do que
      // entrou ("Tiago" riscado, depois "Thiago").
      juntar('saiu', pa)
      i++
    } else {
      juntar('entrou', pb)
      j++
    }
  }
  for (; i < a.length; i++) juntar('saiu', a[i] as string)
  for (; j < m; j++) juntar('entrou', b[j] as string)
  return trechos
}
