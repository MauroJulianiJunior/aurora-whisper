// O Painel no app da nuvem (ADR-019, F4): o selo de conflito e a escolha, o
// "Excluir definitivamente" da Lixeira e a edição que não chegou por falta
// de rede. Contra a nuvem de mentira (testes/servidorFalsoNuvem.ts).

import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

function montar() {
  const srv = servidorFalsoNuvem([
    itemNuvem({ id: 'd1', created_at: em(5, 14, 32), raw_text: 'manda o relatório amanhã', final_text: 'Manda o relatório amanhã cedo.' }),
    itemNuvem({ id: 'd2', created_at: em(5, 9, 5), final_text: 'A reunião é às dez.' }),
    itemNuvem({ id: 'l1', created_at: em(3, 10), final_text: 'Lembrete velho.', deleted_at: em(5, 11) }),
    itemNuvem({ id: 'l2', created_at: em(2, 10), final_text: 'Outro lembrete.', deleted_at: em(5, 10) }),
  ])
  srv.conflitoPorFora('d1', 'Manda o relatório amanhã à tarde.')
  render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
  return srv
}

const linha = (texto: RegExp) => screen.findByRole('button', { name: texto })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const patches = (srv: ReturnType<typeof servidorFalsoNuvem>) => srv.pedidos.filter((p) => p.metodo === 'PATCH').map((p) => p.corpo)

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('o selo de conflito', () => {
  it('a linha mostra "2 versões", discreto, e o leitor de tela ouve que dá para escolher', async () => {
    montar()
    const comSelo = await linha(/Manda o relatório amanhã cedo/)
    expect(comSelo.textContent).toContain('2 versões')
    expect(comSelo.textContent).toContain('2 versões, abra para escolher')
    const etiqueta = within(comSelo).getByText('2 versões')
    expect(etiqueta.className).toBe('etiqueta')
    expect((await linha(/A reunião/)).textContent).not.toContain('2 versões')
  })

  it('aberto, mostra as duas versões, com o que muda marcado', async () => {
    montar()
    fireEvent.click(await linha(/Manda o relatório amanhã cedo/))
    const selo = screen.getByRole('region', { name: 'Mudou em dois lugares. Ficou esta; a outra está guardada.' })
    const [ficou, outra] = Array.from(selo.querySelectorAll<HTMLElement>('.versao'))
    expect(within(ficou!).getByText('Ficou')).toBeTruthy()
    expect(ficou!.querySelector('.versao-texto')?.textContent).toBe('Manda o relatório amanhã cedo.')
    expect(Array.from(ficou!.querySelectorAll('.difere')).map((d) => d.textContent)).toEqual(['cedo.'])
    expect(outra!.querySelector('.rotulo')?.textContent).toMatch(/^A outra · \d\d\/\d\d/)
    expect(outra!.querySelector('.versao-texto')?.textContent).toBe('Manda o relatório amanhã à tarde.')
    expect(Array.from(outra!.querySelectorAll('.difere')).map((d) => d.textContent)).toEqual(['à tarde.'])
    // O selo vem antes do editor.
    expect(selo.compareDocumentPosition(screen.getByLabelText('Texto final')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    // A escolha não tem Desfazer, e os dois botões dizem isso antes do
    // clique: "guardada" só vale até a escolha.
    for (const nome of ['Ficar com esta', 'Ficar com a outra']) {
      const id = within(selo).getByRole('button', { name: nome }).getAttribute('aria-describedby')
      expect(document.getElementById(id ?? '')?.textContent).toBe('A que você não escolher é descartada.')
    }
  })

  it('Ficar com a outra: o PATCH leva resolver_conflito, e o texto da outra vai para o editor', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã cedo/))
    fireEvent.click(screen.getByRole('button', { name: 'Ficar com a outra' }))
    await waitFor(() => expect(aviso().textContent).toBe('Ficou a versão escolhida.'))
    expect(patches(srv)).toEqual([{ base_version: 2, resolver_conflito: 'trocar' }])
    expect(screen.queryByRole('region', { name: /Mudou em dois lugares/ })).toBeNull()
    const campo = screen.getByLabelText<HTMLTextAreaElement>('Texto final')
    expect(campo.value).toBe('Manda o relatório amanhã à tarde.')
    // O botão saiu com o selo: o foco vai para o texto que ficou. Vem de um
    // efeito, que roda depois de o selo sair da página: espera.
    await waitFor(() => expect(document.activeElement).toBe(campo))
    // E a linha, fechada, não tem mais o selo.
    fireEvent.keyDown(campo, { key: 'Escape' })
    expect((await linha(/Manda o relatório amanhã à tarde/)).textContent).not.toContain('2 versões')
  })

  it('Ficar com esta: o texto fica e o selo sai', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã cedo/))
    fireEvent.click(screen.getByRole('button', { name: 'Ficar com esta' }))
    await waitFor(() => expect(aviso().textContent).toBe('Ficou a versão escolhida.'))
    expect(patches(srv)).toEqual([{ base_version: 2, resolver_conflito: 'manter' }])
    expect(srv.itens.find((i) => i.id === 'd1')).toMatchObject({ final_text: 'Manda o relatório amanhã cedo.', versao_em_conflito: null })
  })

  it('um 409 lê o item de novo e diz o que houve', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã cedo/))
    // Outro aparelho escolheu enquanto isso.
    const d1 = srv.itens.find((i) => i.id === 'd1')!
    d1.versao_em_conflito = null
    d1.version += 1
    fireEvent.click(screen.getByRole('button', { name: 'Ficar com a outra' }))
    await waitFor(() => expect(aviso().textContent).toBe('A versão já foi escolhida em outro lugar.'))
    expect(srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/v1/transcriptions/d1')).toHaveLength(1)
    expect(screen.queryByRole('region', { name: /Mudou em dois lugares/ })).toBeNull()
  })

  it('na Lixeira não há selo: lá, a pessoa restaura antes', async () => {
    const srv = montar()
    srv.conflitoPorFora('l1', 'outra versão')
    await linha(/Manda o relatório/)
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByText('Lembrete velho.')
    expect(document.body.textContent).not.toContain('2 versões')
  })
})

describe('Excluir definitivamente', () => {
  async function naLixeira() {
    const srv = montar()
    await linha(/Manda o relatório/)
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByText('Lembrete velho.')
    return srv
  }
  const linhaDe = (texto: string) => screen.getByText(texto).closest<HTMLElement>('li')!

  it('pergunta na própria linha, com o foco no Cancelar; Cancelar não exclui', async () => {
    const srv = await naLixeira()
    fireEvent.click(within(linhaDe('Lembrete velho.')).getByRole('button', { name: 'Excluir definitivamente' }))
    const pergunta = screen.getByRole('group', { name: 'Excluir para sempre? Não dá para desfazer.' })
    expect(document.activeElement).toBe(within(pergunta).getByRole('button', { name: 'Cancelar' }))
    expect(within(pergunta).getByRole('button', { name: 'Excluir' }).className).toBe('botao risco')
    fireEvent.click(within(pergunta).getByRole('button', { name: 'Cancelar' }))
    await waitFor(() => expect(screen.queryByRole('group', { name: /Excluir para sempre/ })).toBeNull())
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Excluir definitivamente'))
    // Esc também cancela.
    fireEvent.click(within(linhaDe('Lembrete velho.')).getByRole('button', { name: 'Excluir definitivamente' }))
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('group', { name: /Excluir para sempre/ })).toBeNull())
    expect(srv.pedidos.some((p) => p.caminho.endsWith('/purge'))).toBe(false)
  })

  it('Excluir apaga de vez, tira a linha e leva o foco à vizinha', async () => {
    const srv = await naLixeira()
    fireEvent.click(within(linhaDe('Lembrete velho.')).getByRole('button', { name: 'Excluir definitivamente' }))
    fireEvent.click(screen.getByRole('button', { name: 'Excluir' }))
    await waitFor(() => expect(screen.queryByText('Lembrete velho.')).toBeNull())
    expect(srv.pedidos.filter((p) => p.caminho.endsWith('/purge')).map((p) => [p.metodo, p.caminho])).toEqual([['POST', '/api/v1/transcriptions/l1/purge']])
    expect(aviso().textContent).toBe('Ditado excluído para sempre.')
    expect(srv.itens.some((i) => i.id === 'l1')).toBe(false)
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Restaurar'))
    expect(document.activeElement?.closest('li')?.textContent).toContain('Outro lembrete.')
  })

  it('quando não dá (restaurado em outro lugar), a pergunta fica e o aviso diz o porquê', async () => {
    const srv = await naLixeira()
    fireEvent.click(within(linhaDe('Lembrete velho.')).getByRole('button', { name: 'Excluir definitivamente' }))
    srv.restaurarPorFora('l1')
    fireEvent.click(screen.getByRole('button', { name: 'Excluir' }))
    await waitFor(() => expect(aviso().textContent).toBe('Não deu certo: o ditado não está na lixeira'))
    expect(screen.getByRole('button', { name: 'Excluir' }).getAttribute('aria-disabled')).toBeNull()
  })
})

describe('editar sem conexão', () => {
  it('a edição não foi salva: o editor diz isso, e o texto fica', async () => {
    const srv = montar()
    fireEvent.click(await linha(/A reunião/))
    const campo = screen.getByLabelText<HTMLTextAreaElement>('Texto final')
    fireEvent.change(campo, { target: { value: 'A reunião é às onze.' } })
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(screen.getAllByRole('alert').map((a) => a.textContent)).toContain('Sem conexão: a edição não foi salva.'))
    expect(campo.value).toBe('A reunião é às onze.')
    expect(srv.itens.find((i) => i.id === 'd2')?.final_text).toBe('A reunião é às dez.')
    // De volta a rede, salvar de novo grava e a nota sai.
    srv.derrubar(false)
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await linha(/A reunião é às onze/)
    expect(screen.queryByText('Sem conexão: a edição não foi salva.')).toBeNull()
  })
})
