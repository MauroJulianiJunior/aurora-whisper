import { act, cleanup, render, screen } from '@testing-library/react'
import { createElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { VERSAO_DO_AVISO } from './avisoDoDitado'
import { Folha, type PedidoDeAceite } from './ditar/folha'
import { FolhaDoAceite } from './ditar/FolhaDoAceite'

afterEach(cleanup)

// A versão do aviso é um inteiro a partir de 1 (a coluna notice_version da
// dictation_consents tem CHECK >= 1). O servidor tem a dele, e o teste do Go
// (TestVersaoDoAviso_AppEServidorConferem) lê o arquivo e prende os dois.
describe('VERSAO_DO_AVISO', () => {
  it('é um inteiro positivo', () => {
    expect(Number.isInteger(VERSAO_DO_AVISO)).toBe(true)
    expect(VERSAO_DO_AVISO).toBeGreaterThanOrEqual(1)
  })
})

// A versão e o texto andam juntos (ADR-021, decisão H, escolha menor 21): quem
// aceitou o texto da versão N aceitou ESSE texto, e mudar uma palavra dele sem
// subir a versão é registrar, para quem já aceitou, um aceite de um texto que
// ela não leu. Por isso este teste guarda uma impressão do que a folha mostra
// (os blocos de cada provedor e o texto de FolhaDoAceite.tsx, nos pedidos que o
// app faz) para cada versão. Mudou o texto, ou a versão, sem a outra: ele quebra
// e diz o que fazer.
//
// Subir o aviso é: (1) mudar o texto, (2) subir VERSAO_DO_AVISO neste app
// (avisoDoDitado.ts) e cloudstore.VersaoDoAviso no Go (o teste do Go quebra se um
// ficar para trás), e (3) acrescentar abaixo a impressão da versão nova, sem
// apagar as antigas (são o registro do que cada versão disse).
//
// A impressão é um FNV-1a de 64 bits do texto: não é segurança, é só um jeito
// de comparar sem guardar o texto aqui.
//
// A impressão da versão 1 foi TROCADA (11-out-2026), e a versão continua 1: o
// título e o primeiro parágrafo foram reordenados (o "Por isso," ficava solto, sem
// o que ele retoma), sem mudar o sentido, e NINGUÉM aceitou a versão 1 ainda (o
// ditado no celular não foi ao ar: nenhum aceite foi gravado em servidor algum,
// ADR-021, C7). Depois que a primeira pessoa real aceitar, isto não vale mais: a
// regra de cima, subir a versão e acrescentar a impressão nova sem apagar a antiga,
// passa a valer para qualquer mudança de texto.
const impressoes: Record<number, string> = {
  1: '5fbf39d58a4bcffa',
}

function impressao(texto: string): string {
  const mascara = 0xffffffffffffffffn
  let h = 0xcbf29ce484222325n
  for (const byte of new TextEncoder().encode(texto)) h = ((h ^ BigInt(byte)) * 0x100000001b3n) & mascara
  return h.toString(16).padStart(16, '0')
}

// Os pedidos que o app faz à folha: a voz de cada provedor, com e sem o Polish
// (do mesmo provedor e de outro), com a escolha do provedor da voz, e só para
// reler. O texto de cada um é o que a pessoa lê.
const pedidos: PedidoDeAceite[] = [
  { voz: 'openai' },
  { voz: 'groq' },
  { voz: 'openai', polish: 'openai' },
  { voz: 'openai', polish: 'anthropic' },
  { voz: 'groq', polish: 'anthropic' },
  { voz: 'groq', polish: 'openai', escolherVoz: true },
  { voz: 'openai', somenteLer: true },
]

async function textoDoAviso(): Promise<string> {
  const partes: string[] = []
  for (const pedido of pedidos) {
    const folha = new Folha({ aceitarAviso: vi.fn() }, vi.fn())
    const { unmount } = render(createElement(FolhaDoAceite, { folha }))
    await act(async () => {
      void folha.pedirAceite(pedido)
    })
    partes.push(screen.getByRole('dialog').textContent!.replace(/\s+/g, ' ').trim())
    unmount()
  }
  return partes.join('\n')
}

describe('o texto do aviso e a versão dele', () => {
  it('o texto que a folha mostra é o da VERSAO_DO_AVISO (mudou um, mude o outro, nos dois lados)', async () => {
    const atual = impressao(await textoDoAviso())
    const registrada = impressoes[VERSAO_DO_AVISO]
    expect(
      registrada,
      `a VERSAO_DO_AVISO é ${VERSAO_DO_AVISO} e não tem impressão registrada: acrescente "${VERSAO_DO_AVISO}: '${atual}'" a impressoes (e suba cloudstore.VersaoDoAviso, no Go, junto)`,
    ).toBeDefined()
    expect(
      atual,
      `o texto do aviso mudou e a versão ${VERSAO_DO_AVISO} não: suba VERSAO_DO_AVISO (avisoDoDitado.ts) e cloudstore.VersaoDoAviso (Go) juntos, e registre a impressão '${atual}' para a versão nova`,
    ).toBe(registrada)
  })

  it('cada versão que já existiu tem a sua impressão, sem lacuna', () => {
    const versoes = Object.keys(impressoes).map(Number)
    expect(versoes).toEqual(Array.from({ length: versoes.length }, (_, i) => i + 1))
    expect(Math.max(...versoes)).toBe(VERSAO_DO_AVISO)
  })
})
