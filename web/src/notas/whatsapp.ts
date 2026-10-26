// Copiar a nota como texto de WhatsApp (ADR-022, "Copiar como texto de
// WhatsApp"): o título e as seções em *negrito*, o marcador como •, a lista de
// marcar como ☐ e ✅, a numerada como está. Puro (sem React, sem rede nem
// área de transferência): quem copia é copiarParaWhatsApp.ts, no clique.

import { lerCorpo } from './corpo'

// emNegrito põe o texto entre asteriscos. O WhatsApp não tem escape: um * de
// dentro fecharia o negrito antes da hora ("*a*b*" vira "a" em negrito e "b*"
// solto). Por isso os asteriscos do título e das seções saem; os do corpo ficam,
// porque quem escreveu "*urgente*" numa nota já escreveu em WhatsApp.
// Devolve '' quando não sobra texto.
function emNegrito(texto: string): string {
  const limpo = texto.replace(/\*/g, '').replace(/\s+/g, ' ').trim()
  return limpo === '' ? '' : `*${limpo}*`
}

// paraWhatsApp converte o título e o corpo (o formato de corpo.ts) em texto de
// WhatsApp. Nota vazia dá ''.
//
// Linhas em branco: uma depois do título e uma antes de cada seção que não abre
// o texto; as do corpo se juntam numa só, e nenhuma fica no começo nem no fim.
export function paraWhatsApp(titulo: string, corpo: string): string {
  const saida: string[] = []
  // branco: falta uma linha em branco antes da próxima linha com texto.
  let branco = false
  const escrever = (linha: string) => {
    if (branco && saida.length > 0) saida.push('')
    branco = false
    saida.push(linha)
  }

  const t = emNegrito(titulo)
  if (t) {
    escrever(t)
    branco = true
  }

  let n = 0
  for (const l of lerCorpo(corpo)) {
    n = l.tipo === 'numerada' ? n + 1 : 0
    const texto = l.texto.trim()
    if (l.tipo === 'paragrafo') {
      if (texto === '') branco = true
      else escrever(l.texto.trimEnd())
      continue
    }
    // Item de lista ou seção sem texto não diz nada.
    if (texto === '') continue
    switch (l.tipo) {
      case 'titulo': {
        const s = emNegrito(texto)
        if (s === '') break
        branco = true
        escrever(s)
        break
      }
      case 'marcador':
        escrever(`• ${texto}`)
        break
      case 'numerada':
        escrever(`${n}. ${texto}`)
        break
      case 'tarefa':
        escrever(`${l.feito ? '✅' : '☐'} ${texto}`)
        break
    }
  }
  return saida.join('\n')
}
