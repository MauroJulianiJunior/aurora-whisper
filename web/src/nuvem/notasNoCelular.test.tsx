// As notas no app do celular (ADR-022, N8; a prévia docs/notas-previa-n8.html):
// a aba Notas é a mesma do PC; nela o microfone do lado da busca sai e o "Ditar
// para nota" fica embaixo; no Histórico, o microfone fica. Sem rede, a lista
// abre do aparelho (mesmo com o app aberto do zero), a faixa conta o que espera,
// e a nota aberta deixa só marcar, desmarcar e acrescentar item.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { notaDaNuvem, notasFalsasNaNuvem, juntar } from './notas/notasFalsas'
import { servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'
import { plataformaFalsa } from './ditar/plataformaFalsa'
import { armazemNaMemoria } from './notas/armazem'
import { daNuvem } from './notas/formato'

afterEach(cleanup)
vi.setConfig({ testTimeout: 20_000 })

const lista = notaDaNuvem({ id: 'n1', titulo: 'Lista do sábado', corpo: '- [ ] leite\n- [ ] pão', cor: 'amarelo' })
const ideia = notaDaNuvem({ id: 'n2', titulo: 'Ideia de vídeo', corpo: 'Um Reels curto sobre o MEI.', posicao: 'W' })

function montar() {
  const notas = notasFalsasNaNuvem([lista, ideia])
  const srv = servidorFalsoNuvem([])
  const armazem = armazemNaMemoria()
  const p = plataformaFalsa()
  render(<AppNuvem buscar={juntar(notas, srv.buscar)} irPara={vi.fn()} plataformaDeDitar={p.plataforma} armazemDasNotas={() => armazem} contaGuardada={async () => null} />)
  return { notas, srv, armazem }
}

const abaNotas = () => screen.findByRole('button', { name: 'Notas' })
const abrir = (id: string) => fireEvent.click(document.querySelector(`[data-nota="${id}"] [data-abrir], [data-nota="${id}"][data-abrir]`)!)

describe('a aba Notas no celular', () => {
  it('existe, com os cartões da nuvem; nela o microfone do lado da busca sai e o "Ditar para nota" fica embaixo', async () => {
    montar()
    // No Histórico, o microfone fica ao lado da busca.
    expect(await screen.findByRole('button', { name: 'Ditar' })).toBeTruthy()
    fireEvent.click(await abaNotas())
    expect(await screen.findByText('Lista do sábado')).toBeTruthy()
    expect(screen.getByText('Ideia de vídeo')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Ditar' })).toBeNull()
    expect(screen.getByRole('searchbox', { name: 'Buscar nas notas' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Ditar para nota' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Criar uma nota…' })).toBeTruthy()
    // De volta ao Histórico, o microfone volta.
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    expect(await screen.findByRole('button', { name: 'Ditar' })).toBeTruthy()
  })

  it('editar pela janela vai à /api/v1/notes com a base, o conteúdo inteiro e o corpo da base', async () => {
    const m = montar()
    fireEvent.click(await abaNotas())
    await screen.findByText('Lista do sábado')
    abrir('n1')
    const dialogo = await screen.findByRole('dialog')
    fireEvent.click(within(dialogo).getByRole('checkbox', { name: 'leite' }))
    fireEvent.click(within(dialogo).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(m.notas.patches()).toHaveLength(1))
    expect(m.notas.patches()[0]).toEqual({
      corpo: '- [ ] pão\n- [x] leite',
      base_version: 1,
      titulo: 'Lista do sábado',
      corpo_base: '- [ ] leite\n- [ ] pão',
    })
  })
})

describe('sem rede (E4)', () => {
  it('o app aberto sem rede abre nas notas guardadas, com a faixa', async () => {
    const armazem = armazemNaMemoria({ notas: [daNuvem(lista), daNuvem(ideia)] })
    const semRede = async () => {
      throw new TypeError('Failed to fetch')
    }
    render(
      <AppNuvem
        buscar={semRede}
        irPara={vi.fn()}
        plataformaDeDitar={plataformaFalsa().plataforma}
        armazemDasNotas={() => armazem}
        contaGuardada={async () => ({ id: 'u1', email: 'mauro@exemplo.test' })}
      />,
    )
    expect(await screen.findByText('Lista do sábado')).toBeTruthy()
    await waitFor(() => expect(document.querySelector('.faixa-rede')?.textContent).toBe('Sem rede'))
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Ditar para nota' }).disabled).toBe(true)
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Criar uma nota…' }).disabled).toBe(true)
  })

  it('a nota aberta sem rede: só marcar, desmarcar e acrescentar item; o marcar fica esperando a rede', async () => {
    const m = montar()
    fireEvent.click(await abaNotas())
    expect(await screen.findByText('Lista do sábado')).toBeTruthy()
    m.notas.cair()
    // O navegador diz que a rede caiu (o mercado sem sinal).
    const onLine = vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
    await act(async () => {
      window.dispatchEvent(new Event('offline'))
    })
    await waitFor(() => expect(document.querySelector('.faixa-rede')?.textContent).toBe('Sem rede'))
    abrir('n1')
    const dialogo = await screen.findByRole('dialog')
    await waitFor(() => expect(within(dialogo).getByText(/Sem rede: dá para marcar, desmarcar e acrescentar item/)).toBeTruthy())
    expect(within(dialogo).getByRole<HTMLInputElement>('textbox', { name: 'Título' }).readOnly).toBe(true)
    expect(within(dialogo).getByRole<HTMLButtonElement>('button', { name: 'Formatar' }).disabled).toBe(true)
    expect(within(dialogo).getByRole<HTMLButtonElement>('button', { name: 'Cor' }).disabled).toBe(true)
    expect(within(dialogo).getByRole<HTMLButtonElement>('button', { name: 'Ditar aqui' }).disabled).toBe(true)
    const linhas = within(dialogo).getAllByRole<HTMLTextAreaElement>('textbox', { name: 'Item' })
    expect(linhas.every((l) => l.readOnly)).toBe(true)
    // O "+ Item" abre um item que se escreve.
    fireEvent.click(within(dialogo).getByRole('button', { name: /Item/ }))
    const novo = within(dialogo).getAllByRole<HTMLTextAreaElement>('textbox', { name: 'Item' }).find((l) => !l.readOnly)
    expect(novo).toBeTruthy()
    fireEvent.change(novo!, { target: { value: 'manteiga' } })
    fireEvent.click(within(dialogo).getByRole('checkbox', { name: 'leite' }))
    fireEvent.click(within(dialogo).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(document.querySelector('.faixa-rede')?.textContent).toBe('Sem rede · 1 mudança esperando'))
    expect(m.armazem.foto().fila).toEqual([expect.objectContaining({ notaId: 'n1', corpo: '- [ ] pão\n- [ ] manteiga\n- [x] leite' })])
    expect(await screen.findByText('esperando a rede')).toBeTruthy()
    expect(m.notas.patches()).toEqual([])
    onLine.mockRestore()
  })
})
