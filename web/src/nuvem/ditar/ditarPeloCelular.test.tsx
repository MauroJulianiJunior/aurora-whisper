// "Ditar pelo celular" nas Configurações do app da nuvem (ADR-021, C6b; texto da
// D0): o estado, o provedor, a chave (colar, trocar, remover, com o lembrete de
// revogar a antiga), o modelo, a dica e o Polish, contra a nuvem de mentira
// (testes/servidorFalsoNuvem.ts). O que se prova é o que a tela faz: quais
// pedidos saem, em que ordem, o que a pessoa vê, e que a chave inteira nunca
// aparece nem volta. O que a nuvem decide é dos testes Go (C3a e C3b).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type OpcoesDaNuvem, itemNuvem, servidorFalsoNuvem } from '../../testes/servidorFalsoNuvem'
import AppNuvem from '../AppNuvem'
import { versaoDoAviso } from './aviso'
import { plataformaFalsa } from './plataformaFalsa'

const espera = { timeout: 5000 }
vi.setConfig({ testTimeout: 20_000 })

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 10, 12, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

type Servidor = ReturnType<typeof servidorFalsoNuvem>
type Provedores = ('openai' | 'groq' | 'anthropic')[]

const aceiteDe = (provedores: Provedores, versao = versaoDoAviso) => ({ versao, provedores, aceito_em: '2026-10-09T12:00:00.000Z' })
const chaveDaOpenAI = 'sk-teste-de-mentira-WXYZ'

// pronto: a conta com o aceite da OpenAI e a chave dela.
const pronto = (extra: NonNullable<OpcoesDaNuvem['ditado']> = {}): OpcoesDaNuvem => ({
  ditado: { aceite: aceiteDe(['openai']), chaves: { openai: chaveDaOpenAI }, ...extra },
})

async function abrir(opcoes: OpcoesDaNuvem = {}) {
  const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 9, 14).toISOString(), final_text: 'Manda o relatório amanhã.' })], opcoes)
  const p = plataformaFalsa()
  render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} abrirJanela={vi.fn(() => true)} plataformaDeDitar={p.plataforma} />)
  await screen.findByRole('button', { name: /Manda o relatório amanhã/ }, espera)
  fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { level: 2, name: 'Ditar pelo celular' }, espera)
  return { srv, p }
}

const secao = () => screen.getByRole('region', { name: 'Ditar pelo celular' })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const folha = () => screen.findByRole('dialog', undefined, espera)
const aceito = () => screen.getByRole('button', { name: 'Aceito' })
const seletor = (nome: string) => within(secao()).getByLabelText<HTMLSelectElement>(nome)

// pedidos da conta do ditado (o que o teste conferiu), na ordem, sem as leituras.
const escritas = (srv: Servidor) =>
  srv.pedidos
    .filter((x) => /^\/api\/v1\/(dictation\/|ai-keys)/.test(x.caminho) && x.metodo !== 'GET')
    .map((x) => `${x.metodo} ${x.caminho}`)
const leituras = (srv: Servidor, caminho: string) => srv.pedidos.filter((x) => x.metodo === 'GET' && x.caminho === caminho).length
const doCaminho = (srv: Servidor, metodo: string, caminho: string) => srv.pedidos.filter((x) => x.metodo === metodo && x.caminho === caminho)

async function ligadaComChave(extra: NonNullable<OpcoesDaNuvem['ditado']> = {}, opcoes: OpcoesDaNuvem = {}) {
  const m = await abrir({ ...pronto(extra), ...opcoes })
  await within(secao()).findByText('Chave terminada em', { exact: false }, espera)
  return m
}

describe('o estado', () => {
  it('sem aceite: "Desligado.", com o Ligar; nada de provedor, chave, modelo, dica nem Polish', async () => {
    const { srv } = await abrir({ ditado: {} })
    await within(secao()).findByText('Desligado.', undefined, espera)
    expect(within(secao()).getByRole('button', { name: 'Ligar' })).toBeTruthy()
    expect(within(secao()).queryByLabelText('Provedor da voz')).toBeNull()
    expect(within(secao()).queryByText(/^Chave da/)).toBeNull()
    expect(within(secao()).queryByRole('checkbox')).toBeNull()
    expect(within(secao()).queryByRole('button', { name: 'Desligar o ditado no celular' })).toBeNull()
    // Só a leitura do estado: nenhum modelo, nenhuma chave.
    expect(leituras(srv, '/api/v1/dictation')).toBe(1)
    expect(escritas(srv)).toEqual([])
  })

  it('com aceite: "Ligado desde" a data do aceite, com Ler o aviso e Desligar', async () => {
    await ligadaComChave()
    expect(within(secao()).getByText('Ligado desde 09/10/2026.')).toBeTruthy()
    expect(within(secao()).getByRole('button', { name: 'Ler o aviso' })).toBeTruthy()
    expect(within(secao()).getByRole('button', { name: 'Desligar o ditado no celular' }).textContent).toBe('Desligar')
    expect(within(secao()).queryByRole('button', { name: 'Ligar' })).toBeNull()
  })

  it('o aviso mudou (o aceite é de uma versão velha): "Desligado" e o Ligar, com o motivo', async () => {
    await abrir({ ditado: { aceite: aceiteDe(['openai'], '2026-01-01'), chaves: { openai: chaveDaOpenAI } } })
    await within(secao()).findByText('Desligado. O aviso mudou: leia e aceite de novo para ligar.', undefined, espera)
    expect(within(secao()).getByRole('button', { name: 'Ligar' })).toBeTruthy()
    expect(within(secao()).queryByLabelText('Provedor da voz')).toBeNull()
  })

  it('diz de saída que não funciona com a tela apagada nem com o app em segundo plano', async () => {
    await abrir({ ditado: {} })
    expect(secao().textContent).toContain('O ditado não funciona com a tela apagada nem com o app em segundo plano.')
  })

  it('o servidor sem o ditado (404): a frase, e mais nada', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 9, 14).toISOString(), final_text: 'Um ditado.' })])
    srv.interceptar((x) => (x.caminho === '/api/v1/dictation' ? new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'rota não encontrada' } }), { status: 404 }) : undefined))
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} plataformaDeDitar={plataformaFalsa().plataforma} />)
    await screen.findByRole('button', { name: /Um ditado/ }, espera)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    expect((await within(secao()).findByRole('alert', undefined, espera)).textContent).toBe('Este servidor não tem o ditado pelo celular.')
    expect(within(secao()).queryByRole('button', { name: 'Ligar' })).toBeNull()
  })

  it('outro erro: uma frase nossa, nunca a do servidor', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 9, 14).toISOString(), final_text: 'Um ditado.' })])
    srv.interceptar((x) => (x.caminho === '/api/v1/dictation' ? new Response(JSON.stringify({ error: { message: 'pânico na linha 42 de cofre.go' } }), { status: 500 }) : undefined))
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} plataformaDeDitar={plataformaFalsa().plataforma} />)
    await screen.findByRole('button', { name: /Um ditado/ }, espera)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    const nota = await within(secao()).findByRole('alert', undefined, espera)
    expect(nota.textContent).toBe('Não deu para ver o ditado pelo celular. Tente de novo mais tarde.')
  })
})

describe('Ligar', () => {
  it('abre a folha com a escolha do provedor; Aceito grava o aceite e o provedor escolhido, e liga', async () => {
    const { srv } = await abrir({ ditado: {} })
    await act(async () => {
      fireEvent.click(await within(secao()).findByRole('button', { name: 'Ligar' }, espera))
    })
    await folha()
    await act(async () => {
      fireEvent.click(screen.getByRole('radio', { name: 'Groq' }))
    })
    await act(async () => {
      fireEvent.click(aceito())
    })
    await within(secao()).findByText('Ligado desde 10/10/2026.', undefined, espera)
    expect(escritas(srv)).toEqual(['POST /api/v1/dictation/consent', 'PUT /api/v1/dictation/settings'])
    expect(doCaminho(srv, 'POST', '/api/v1/dictation/consent')[0]!.corpo).toEqual({ versao: versaoDoAviso, provedores: ['groq'] })
    expect(doCaminho(srv, 'PUT', '/api/v1/dictation/settings')[0]!.corpo).toEqual({
      provedor: 'groq',
      modelo: '',
      dica: false,
      polish: false,
      polish_provedor: 'openai',
      polish_modelo: '',
    })
    expect(aviso().textContent).toBe('Ditar pelo celular ligado.')
    // Agora a Groq é o provedor, e falta a chave dela.
    expect(seletor('Provedor da voz').value).toBe('groq')
    expect(within(secao()).getByText('Chave da Groq')).toBeTruthy()
    expect(within(secao()).getByText('Nenhuma chave guardada.')).toBeTruthy()
  })

  it('com o OpenAI mesmo, só o aceite é gravado: o provedor já é o das preferências', async () => {
    const { srv } = await abrir({ ditado: {} })
    await act(async () => {
      fireEvent.click(await within(secao()).findByRole('button', { name: 'Ligar' }, espera))
    })
    await folha()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await within(secao()).findByText('Ligado desde 10/10/2026.', undefined, espera)
    expect(escritas(srv)).toEqual(['POST /api/v1/dictation/consent'])
  })

  it('"Agora não" não grava nada e a seção continua desligada', async () => {
    const { srv } = await abrir({ ditado: {} })
    await act(async () => {
      fireEvent.click(await within(secao()).findByRole('button', { name: 'Ligar' }, espera))
    })
    await folha()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(within(secao()).getByText('Desligado.')).toBeTruthy()
    expect(escritas(srv)).toEqual([])
    expect(srv.daConta.aceite).toBeNull()
  })
})

describe('o provedor da voz', () => {
  it('trocar para um provedor fora do aceite reabre a folha, e sem o Aceito nada muda', async () => {
    const { srv } = await ligadaComChave()
    fireEvent.change(seletor('Provedor da voz'), { target: { value: 'groq' } })
    const f = await folha()
    // O bloco é o do provedor novo, e a escolha não aparece: o provedor já foi escolhido ali.
    expect(f.textContent).toContain('vai ao provedor que você escolheu: Groq')
    expect(within(f).queryByRole('radiogroup')).toBeNull()
    expect(escritas(srv)).toEqual([])

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(seletor('Provedor da voz').value).toBe('openai')
    expect(escritas(srv)).toEqual([])
    expect(srv.daConta.preferencias.provedor).toBe('openai')
  })

  it('com o Aceito, o aceite novo mantém a OpenAI, e a troca vale; o modelo volta ao padrão', async () => {
    const { srv } = await ligadaComChave({ preferencias: { modelo: 'gpt-4o-transcribe' } })
    fireEvent.change(seletor('Provedor da voz'), { target: { value: 'groq' } })
    await folha()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await waitFor(() => expect(seletor('Provedor da voz').value).toBe('groq'))
    expect(escritas(srv)).toEqual(['POST /api/v1/dictation/consent', 'PUT /api/v1/dictation/settings'])
    expect(doCaminho(srv, 'POST', '/api/v1/dictation/consent')[0]!.corpo).toEqual({ versao: versaoDoAviso, provedores: ['openai', 'groq'] })
    expect(srv.daConta.preferencias).toMatchObject({ provedor: 'groq', modelo: '' })
    await within(secao()).findByText('Chave da Groq', undefined, espera)
  })

  it('um provedor que já está no aceite troca direto, sem folha', async () => {
    const { srv } = await ligadaComChave({ aceite: aceiteDe(['openai', 'groq']) })
    fireEvent.change(seletor('Provedor da voz'), { target: { value: 'groq' } })
    await waitFor(() => expect(seletor('Provedor da voz').value).toBe('groq'))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(escritas(srv)).toEqual(['PUT /api/v1/dictation/settings'])
  })

  it('o servidor que recusa por falta de aceite (SEM_ACEITE no PUT) abre a folha; aceitando, o PUT é refeito', async () => {
    const { srv } = await ligadaComChave({ aceite: aceiteDe(['openai', 'groq']) })
    // O aviso mudou em outro aparelho: a tela ainda acha que vale.
    srv.envelhecerAceitePorFora()
    fireEvent.change(seletor('Provedor da voz'), { target: { value: 'groq' } })
    await folha()
    expect(screen.queryByRole('radiogroup')).toBeNull()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await waitFor(() => expect(seletor('Provedor da voz').value).toBe('groq'))
    expect(escritas(srv)).toEqual(['PUT /api/v1/dictation/settings', 'POST /api/v1/dictation/consent', 'PUT /api/v1/dictation/settings'])
    expect(doCaminho(srv, 'PUT', '/api/v1/dictation/settings').map((x) => x.corpo?.provedor)).toEqual(['groq', 'groq'])
    // O aceite que vale agora é o de hoje, só da Groq (o velho não conta).
    expect(srv.daConta.aceite).toMatchObject({ versao: versaoDoAviso, provedores: ['groq'] })
  })

  it('SEM_ACEITE no PUT e "Agora não": nada é gravado, e a tela passa a dizer o que o servidor diz (o aviso mudou)', async () => {
    const { srv } = await ligadaComChave({ aceite: aceiteDe(['openai', 'groq']) })
    srv.envelhecerAceitePorFora()
    fireEvent.change(seletor('Provedor da voz'), { target: { value: 'groq' } })
    await folha()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    await within(secao()).findByText('Desligado. O aviso mudou: leia e aceite de novo para ligar.', undefined, espera)
    expect(escritas(srv)).toEqual(['PUT /api/v1/dictation/settings'])
    expect(srv.daConta.preferencias.provedor).toBe('openai')
    expect(within(secao()).queryByLabelText('Provedor da voz')).toBeNull()
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('o erro de salvar é uma frase nossa, nunca a do servidor', async () => {
    const { srv } = await ligadaComChave({ aceite: aceiteDe(['openai', 'groq']) })
    srv.interceptar((x) =>
      x.caminho === '/api/v1/dictation/settings' ? new Response(JSON.stringify({ error: { message: 'deadlock em dictation_settings' } }), { status: 500 }) : undefined,
    )
    fireEvent.change(seletor('Provedor da voz'), { target: { value: 'groq' } })
    const nota = await within(secao()).findByRole('alert', undefined, espera)
    expect(nota.textContent).toBe('Não deu para salvar. Tente de novo.')
    expect(secao().textContent).not.toContain('deadlock')
    expect(seletor('Provedor da voz').value).toBe('openai')
  })
})

describe('a chave', () => {
  const guardar = async (valor: string, nome = 'Chave da OpenAI') => {
    const campo = within(secao()).getByLabelText<HTMLInputElement>(nome)
    fireEvent.input(campo, { target: { value: valor } })
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Guardar chave' }))
    })
    return campo
  }

  it('sem chave: um campo de senha, a ajuda de usar uma chave só para o celular, e o Guardar só vale com algo colado', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    const campo = await within(secao()).findByLabelText<HTMLInputElement>('Chave da OpenAI', undefined, espera)
    expect(campo.type).toBe('password')
    expect(campo.autocomplete).toBe('off')
    expect(within(secao()).getByText('Nenhuma chave guardada.')).toBeTruthy()
    expect(within(secao()).getByText('Use uma chave só para o celular, com limite de gasto no provedor.')).toBeTruthy()
    const botao = within(secao()).getByRole('button', { name: 'Guardar chave' })
    expect(botao.getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(botao)
    expect(escritas(srv)).toEqual([])
  })

  it('guardar: a chave vai no corpo do PUT (nunca na URL), o campo some, e só o final de 4 caracteres fica na tela', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    await guardar('sk-teste-do-celular-ABCD')
    await within(secao()).findByText('Chave terminada em', { exact: false }, espera)

    const [pedido] = doCaminho(srv, 'PUT', '/api/v1/ai-keys/openai')
    expect(pedido!.corpo).toEqual({ chave: 'sk-teste-do-celular-ABCD' })
    expect(pedido!.busca.toString()).toBe('')
    // A chave não vai a nenhuma URL, nem de passagem.
    expect(srv.pedidos.some((x) => x.caminho.includes('sk-teste') || x.busca.toString().includes('sk-teste'))).toBe(false)
    expect(within(secao()).getByText('Chave terminada em', { exact: false }).textContent).toBe('Chave terminada em ABCD.')
    expect(within(secao()).queryByLabelText('Chave da OpenAI')).toBeNull()
    expect(document.body.innerHTML).not.toContain('sk-teste-do-celular')
    expect(document.body.textContent).not.toContain('sk-teste-do-celular')
    expect(aviso().textContent).toBe('Chave guardada.')
    // O foco vai para o Trocar, que ocupa o lugar do campo.
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Trocar a chave da OpenAI' })))
    // O servidor guardou a chave inteira, e só ele.
    expect(srv.daConta.chaves.openai).toBe('sk-teste-do-celular-ABCD')
  })

  it('o Enter no campo guarda', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    const campo = await within(secao()).findByLabelText<HTMLInputElement>('Chave da OpenAI', undefined, espera)
    fireEvent.input(campo, { target: { value: 'sk-teste-do-celular-ENTR' } })
    await act(async () => {
      fireEvent.keyDown(campo, { key: 'Enter' })
    })
    await within(secao()).findByText('Chave terminada em', { exact: false }, espera)
    expect(doCaminho(srv, 'PUT', '/api/v1/ai-keys/openai')).toHaveLength(1)
  })

  it('com a chave guardada: "Chave terminada em WXYZ", com Trocar e Remover, e nada da chave além do final', async () => {
    await ligadaComChave()
    expect(within(secao()).getByText('Chave terminada em', { exact: false }).textContent).toBe('Chave terminada em WXYZ.')
    expect(within(secao()).getByRole('button', { name: 'Trocar a chave da OpenAI' }).textContent).toBe('Trocar')
    expect(within(secao()).getByRole('button', { name: 'Remover a chave da OpenAI' }).textContent).toBe('Remover')
    expect(document.body.textContent).not.toContain('sk-teste-de-mentira')
    expect(document.body.innerHTML).not.toContain('sk-teste-de-mentira')
  })

  it('o final que o servidor mande a mais não passa de 4 caracteres', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 9, 14).toISOString(), final_text: 'Um ditado.' })], pronto())
    srv.interceptar((x) =>
      x.caminho === '/api/v1/dictation' && x.metodo === 'GET'
        ? new Response(
            JSON.stringify({
              preferencias: { provedor: 'openai', modelo: '', dica: false, polish: false, polish_provedor: 'openai', polish_modelo: '' },
              aceite: aceiteDe(['openai']),
              chaves: { openai: { configurada: true, final: 'sk-teste-inteira-1234567890' } },
            }),
          )
        : undefined,
    )
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} plataformaDeDitar={plataformaFalsa().plataforma} />)
    await screen.findByRole('button', { name: /Um ditado/ }, espera)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await within(secao()).findByText('Chave terminada em', { exact: false }, espera)
    expect(within(secao()).getByText('Chave terminada em', { exact: false }).textContent).toBe('Chave terminada em 7890.')
    expect(document.body.textContent).not.toContain('sk-teste-inteira')
  })

  it('Trocar: lembra de revogar a antiga no provedor, pede a nova, e Cancelar volta sem pedir nada', async () => {
    const { srv } = await ligadaComChave()
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Trocar a chave da OpenAI' }))
    })
    expect(within(secao()).getByText('Revogue também esta chave no provedor. Uma cópia cifrada dela fica nos backups por cerca de 30 dias.')).toBeTruthy()
    const campo = within(secao()).getByLabelText<HTMLInputElement>('Chave da OpenAI')
    expect(document.activeElement).toBe(campo)
    // A chave de antes continua valendo até a nova ser guardada.
    expect(within(secao()).getByText('Chave terminada em', { exact: false }).textContent).toBe('Chave terminada em WXYZ.')
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Cancelar' }))
    })
    expect(within(secao()).queryByLabelText('Chave da OpenAI')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Trocar a chave da OpenAI' })))
    expect(escritas(srv)).toEqual([])
  })

  it('Trocar e guardar a nova: o PUT, o final novo, e o servidor com a chave nova', async () => {
    const { srv } = await ligadaComChave()
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Trocar a chave da OpenAI' }))
    })
    await guardar('sk-teste-nova-9876')
    await waitFor(() => expect(within(secao()).getByText('Chave terminada em', { exact: false }).textContent).toBe('Chave terminada em 9876.'))
    expect(escritas(srv)).toEqual(['PUT /api/v1/ai-keys/openai'])
    expect(srv.daConta.chaves.openai).toBe('sk-teste-nova-9876')
    expect(document.body.textContent).not.toContain('sk-teste-nova')
  })

  it('Remover: pergunta com o lembrete de revogar, o foco no Cancelar, o Esc cancela e devolve o foco', async () => {
    const { srv } = await ligadaComChave()
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Remover a chave da OpenAI' }))
    })
    const grupo = within(secao()).getByRole('group', { name: 'Remover a chave da OpenAI' })
    expect(within(grupo).getByText('Revogue também esta chave no provedor. Uma cópia cifrada dela fica nos backups por cerca de 30 dias.')).toBeTruthy()
    const cancelar = within(grupo).getByRole('button', { name: 'Cancelar' })
    expect(document.activeElement).toBe(cancelar)
    fireEvent.keyDown(grupo, { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Remover a chave da OpenAI' })))
    expect(within(secao()).queryByRole('group', { name: 'Remover a chave da OpenAI' })).toBeNull()
    expect(escritas(srv)).toEqual([])
  })

  it('Remover mesmo: o DELETE, a chave some do servidor, o aviso, e o campo volta com o foco', async () => {
    const { srv } = await ligadaComChave()
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Remover a chave da OpenAI' }))
    })
    await act(async () => {
      fireEvent.click(within(within(secao()).getByRole('group', { name: 'Remover a chave da OpenAI' })).getByRole('button', { name: 'Remover' }))
    })
    await within(secao()).findByText('Nenhuma chave guardada.', undefined, espera)
    expect(escritas(srv)).toEqual(['DELETE /api/v1/ai-keys/openai'])
    expect(srv.daConta.chaves.openai).toBeUndefined()
    expect(aviso().textContent).toBe('Chave removida.')
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByLabelText('Chave da OpenAI')))
  })

  it('o provedor que recusa a chave: "O provedor recusou a chave.", e a chave continua no campo para conferir', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    const campo = await guardar('recusada-pelo-provedor')
    expect((await within(secao()).findByRole('alert', undefined, espera)).textContent).toBe('O provedor recusou a chave.')
    expect(campo.value).toBe('recusada-pelo-provedor')
    expect(srv.daConta.chaves.openai).toBeUndefined()
    // A chave não vai à tela, nem na frase de erro.
    expect(secao().textContent).not.toContain('recusada-pelo-provedor')
  })

  it('o erro do servidor nunca repete o que ele disse: a mensagem dele pode trazer um pedaço da chave', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    srv.interceptar((x) =>
      x.caminho === '/api/v1/ai-keys/openai' && x.metodo === 'PUT'
        ? new Response(JSON.stringify({ error: { code: 'VALIDATION', message: 'invalid character near sk-teste-pedaco-da-chave' } }), { status: 400 })
        : undefined,
    )
    await guardar('sk-teste-pedaco-da-chave-1')
    const nota = await within(secao()).findByRole('alert', undefined, espera)
    expect(nota.textContent).toBe('Essa chave não parece válida. Confira e cole de novo.')
    expect(document.body.textContent).not.toContain('pedaco-da-chave')
  })

  it('o limite e o provedor fora do ar têm a frase certa', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    srv.interceptar((x) =>
      x.caminho === '/api/v1/ai-keys/openai' && x.metodo === 'PUT'
        ? new Response(JSON.stringify({ error: { code: 'LIMITE', message: 'x' } }), { status: 429, headers: { 'Retry-After': '30' } })
        : undefined,
    )
    await guardar('sk-teste-limite-1234')
    expect((await within(secao()).findByRole('alert', undefined, espera)).textContent).toBe('Espere um pouco. Tente de novo em 30 s.')
    srv.interceptar((x) =>
      x.caminho === '/api/v1/ai-keys/openai' && x.metodo === 'PUT' ? new Response(JSON.stringify({ error: { code: 'PROVEDOR_FORA', message: 'x' } }), { status: 502 }) : undefined,
    )
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Guardar chave' }))
    })
    await waitFor(() => expect(within(secao()).getByRole('alert').textContent).toBe('O provedor não respondeu agora. Tente de novo.'))
  })

  it('SEM_ACEITE ao guardar (o aviso mudou em outro aparelho): a folha abre, e aceitando a chave é guardada', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    srv.envelhecerAceitePorFora()
    await guardar('sk-teste-depois-do-aceite-1')
    await folha()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await within(secao()).findByText('Chave terminada em', { exact: false }, espera)
    expect(escritas(srv)).toEqual(['PUT /api/v1/ai-keys/openai', 'POST /api/v1/dictation/consent', 'PUT /api/v1/ai-keys/openai'])
    expect(srv.daConta.chaves.openai).toBe('sk-teste-depois-do-aceite-1')
  })

  it('SEM_ACEITE ao guardar, a chave colada fica no campo enquanto a folha está aberta; com "Agora não" ela não é guardada', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    srv.envelhecerAceitePorFora()
    const campo = await guardar('sk-teste-sem-aceite-12')
    await folha()
    expect(campo.isConnected).toBe(true)
    expect(campo.value).toBe('sk-teste-sem-aceite-12')
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    // A tela diz o que o servidor diz: o aviso mudou. A chave não foi guardada.
    await within(secao()).findByText('Desligado. O aviso mudou: leia e aceite de novo para ligar.', undefined, espera)
    expect(srv.daConta.chaves.openai).toBeUndefined()
    expect(escritas(srv)).toEqual(['PUT /api/v1/ai-keys/openai'])
    expect(document.body.textContent).not.toContain('sk-teste-sem-aceite')
  })

  it('sem rede ao guardar: a faixa de "Sem conexão", e a chave continua no campo para tentar de novo', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    srv.derrubar(true)
    const campo = await guardar('sk-teste-sem-rede-1234')
    await screen.findByText('Sem conexão.', undefined, espera)
    expect(campo.value).toBe('sk-teste-sem-rede-1234')
    expect(srv.daConta.chaves.openai).toBeUndefined()
  })
})

describe('o modelo da voz', () => {
  it('sem chave não há lista: o campo fica fechado, com o porquê', async () => {
    const { srv } = await abrir(pronto({ chaves: {} }))
    await within(secao()).findByLabelText('Chave da OpenAI', undefined, espera)
    const modelo = seletor('Modelo da voz')
    expect(modelo.disabled).toBe(true)
    expect(within(secao()).getByText('Cole a chave para ver os modelos da sua conta.')).toBeTruthy()
    expect(leituras(srv, '/api/v1/ai-keys/openai/models')).toBe(0)
  })

  it('com a chave: o padrão do provedor e os modelos de voz da conta (os de texto ficam de fora)', async () => {
    const { srv } = await ligadaComChave()
    await waitFor(() =>
      expect(Array.from(seletor('Modelo da voz').options, (o) => o.textContent)).toEqual(['Padrão do provedor', 'gpt-4o-transcribe', 'gpt-4o-mini-transcribe', 'whisper-1']),
    )
    expect(seletor('Modelo da voz').value).toBe('')
    expect(leituras(srv, '/api/v1/ai-keys/openai/models')).toBe(1)
  })

  it('escolher o modelo salva na hora, sem pedir o aceite de novo', async () => {
    const { srv } = await ligadaComChave()
    await waitFor(() => expect(seletor('Modelo da voz').options.length).toBe(4))
    fireEvent.change(seletor('Modelo da voz'), { target: { value: 'gpt-4o-transcribe' } })
    await waitFor(() => expect(srv.daConta.preferencias.modelo).toBe('gpt-4o-transcribe'))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(escritas(srv)).toEqual(['PUT /api/v1/dictation/settings'])
    expect(seletor('Modelo da voz').value).toBe('gpt-4o-transcribe')
  })

  it('o modelo que a conta tem e a lista não mostra continua escolhível', async () => {
    await ligadaComChave({ preferencias: { modelo: 'gpt-transcribe' } })
    await waitFor(() => expect(seletor('Modelo da voz').options.length).toBe(5))
    expect(seletor('Modelo da voz').value).toBe('gpt-transcribe')
  })

  it('a Groq lista os whisper dela', async () => {
    await ligadaComChave({ aceite: aceiteDe(['groq']), preferencias: { provedor: 'groq' }, chaves: { groq: 'gsk-teste-de-mentira-GROQ' } })
    await waitFor(() => expect(Array.from(seletor('Modelo da voz').options, (o) => o.textContent)).toEqual(['Padrão do provedor', 'whisper-large-v3-turbo', 'whisper-large-v3']))
  })

  it('a lista que não veio: o padrão do provedor vale, e a tela diz', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 9, 14).toISOString(), final_text: 'Um ditado.' })], pronto())
    srv.interceptar((x) => (x.caminho === '/api/v1/ai-keys/openai/models' ? new Response(JSON.stringify({ error: { message: 'x' } }), { status: 502 }) : undefined))
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} plataformaDeDitar={plataformaFalsa().plataforma} />)
    await screen.findByRole('button', { name: /Um ditado/ }, espera)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await within(secao()).findByText('Não deu para ler os modelos. Vale o padrão do provedor.', undefined, espera)
    expect(Array.from(seletor('Modelo da voz').options, (o) => o.textContent)).toEqual(['Padrão do provedor'])
  })
})

describe('a dica', () => {
  it('começa desligada; ligar salva na hora, e a ajuda diz que os termos podem ser nomes de clientes', async () => {
    const { srv } = await ligadaComChave()
    const dica = within(secao()).getByRole<HTMLInputElement>('checkbox', { name: 'Mandar os termos aprendidos como dica' })
    expect(dica.checked).toBe(false)
    expect(secao().textContent).toContain('Eles vão ao provedor junto com a voz, em todo ditado, e podem ser nomes de clientes ou de outras pessoas.')
    fireEvent.click(dica)
    await waitFor(() => expect(srv.daConta.preferencias.dica).toBe(true))
    expect(doCaminho(srv, 'PUT', '/api/v1/dictation/settings')[0]!.corpo).toMatchObject({ dica: true, polish: false })
    await waitFor(() => expect(within(secao()).getByRole<HTMLInputElement>('checkbox', { name: 'Mandar os termos aprendidos como dica' }).checked).toBe(true))
    fireEvent.click(within(secao()).getByRole('checkbox', { name: 'Mandar os termos aprendidos como dica' }))
    await waitFor(() => expect(srv.daConta.preferencias.dica).toBe(false))
  })
})

describe('o Polish', () => {
  it('começa desligado, sem provedor nem modelo à vista; a ajuda diz para onde vai o texto', async () => {
    await ligadaComChave()
    expect(within(secao()).getByRole<HTMLInputElement>('checkbox', { name: 'Organizar o texto com o Polish' }).checked).toBe(false)
    expect(secao().textContent).toContain('O texto do ditado vai ao provedor do Polish.')
    expect(within(secao()).queryByLabelText('Provedor do Polish')).toBeNull()
    expect(within(secao()).queryByLabelText('Modelo do Polish')).toBeNull()
  })

  it('ligar com a OpenAI (já no aceite) salva na hora: a mesma chave serve, e os modelos são os de texto', async () => {
    const { srv } = await ligadaComChave()
    fireEvent.click(within(secao()).getByRole('checkbox', { name: 'Organizar o texto com o Polish' }))
    await within(secao()).findByLabelText('Provedor do Polish', undefined, espera)
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(srv.daConta.preferencias).toMatchObject({ polish: true, polish_provedor: 'openai' })
    expect(seletor('Provedor do Polish').value).toBe('openai')
    expect(Array.from(seletor('Provedor do Polish').options, (o) => o.textContent)).toEqual(['OpenAI', 'Anthropic'])
    expect(within(secao()).getByText('A chave da OpenAI, acima, também serve ao Polish.')).toBeTruthy()
    await waitFor(() => expect(Array.from(seletor('Modelo do Polish').options, (o) => o.textContent)).toEqual(['Padrão do provedor', 'gpt-4o', 'gpt-4o-mini']))
  })

  it('ligar com um provedor fora do aceite abre a folha com o bloco dele; sem o Aceito, continua desligado', async () => {
    const { srv } = await ligadaComChave({ preferencias: { polish_provedor: 'anthropic' } })
    fireEvent.click(within(secao()).getByRole('checkbox', { name: 'Organizar o texto com o Polish' }))
    const f = await folha()
    expect(f.textContent).toContain('O Polish que você está ligando: o texto do ditado, para Anthropic')
    expect(f.textContent).toContain('Anthropic. Ela diz que pode guardar o texto por até 30 dias')
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    expect(within(secao()).getByRole<HTMLInputElement>('checkbox', { name: 'Organizar o texto com o Polish' }).checked).toBe(false)
    expect(escritas(srv)).toEqual([])
    expect(srv.daConta.preferencias.polish).toBe(false)
  })

  it('trocar o provedor do Polish para a Anthropic reabre a folha; aceitando, pede a chave dela, e o aceite leva os dois', async () => {
    const { srv } = await ligadaComChave({ preferencias: { polish: true } })
    await within(secao()).findByLabelText('Provedor do Polish', undefined, espera)
    fireEvent.change(seletor('Provedor do Polish'), { target: { value: 'anthropic' } })
    await folha()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await within(secao()).findByText('Chave da Anthropic', undefined, espera)
    expect(doCaminho(srv, 'POST', '/api/v1/dictation/consent')[0]!.corpo).toEqual({ versao: versaoDoAviso, provedores: ['openai', 'anthropic'] })
    expect(srv.daConta.preferencias).toMatchObject({ polish: true, polish_provedor: 'anthropic', polish_modelo: '' })
    expect(within(secao()).getByText('Nenhuma chave guardada.')).toBeTruthy()

    // A chave da Anthropic é colada ali, com o mesmo cuidado.
    const campo = within(secao()).getByLabelText<HTMLInputElement>('Chave da Anthropic')
    fireEvent.input(campo, { target: { value: 'sk-ant-teste-do-polish-QRST' } })
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Guardar chave' }))
    })
    await waitFor(() => expect(srv.daConta.chaves.anthropic).toBe('sk-ant-teste-do-polish-QRST'))
    expect(doCaminho(srv, 'PUT', '/api/v1/ai-keys/anthropic')[0]!.corpo).toEqual({ chave: 'sk-ant-teste-do-polish-QRST' })
    expect(document.body.textContent).not.toContain('sk-ant-teste-do-polish')
    await waitFor(() => expect(within(secao()).getAllByText('Chave terminada em', { exact: false })).toHaveLength(2))
  })

  it('desligar o Polish salva na hora', async () => {
    const { srv } = await ligadaComChave({ preferencias: { polish: true } })
    await within(secao()).findByLabelText('Provedor do Polish', undefined, espera)
    fireEvent.click(within(secao()).getByRole('checkbox', { name: 'Organizar o texto com o Polish' }))
    await waitFor(() => expect(within(secao()).queryByLabelText('Provedor do Polish')).toBeNull())
    expect(srv.daConta.preferencias.polish).toBe(false)
  })
})

describe('Ler o aviso', () => {
  it('reabre a folha de quem já aceitou, só para ler: Fechar, sem gravar nada', async () => {
    const { srv } = await ligadaComChave()
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Ler o aviso' }))
    })
    const f = await folha()
    expect(within(f).getAllByRole('button').map((b) => b.textContent)).toEqual(['Fechar'])
    expect(f.textContent).toContain('Você já aceitou este aviso')
    await act(async () => {
      fireEvent.click(within(f).getByRole('button', { name: 'Fechar' }))
    })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(escritas(srv)).toEqual([])
  })
})

describe('Desligar', () => {
  async function perguntar() {
    const m = await ligadaComChave()
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Desligar o ditado no celular' }))
    })
    const grupo = within(secao()).getByRole('group', { name: 'Desligar o ditado no celular' })
    return { ...m, grupo }
  }

  it('pergunta ali mesmo: tira o aceite, apaga as chaves, não apaga os ditados, e manda revogar no provedor; o foco no Cancelar', async () => {
    const { srv, grupo } = await perguntar()
    expect(grupo.textContent).toContain('Isto tira o seu aceite e apaga as suas chaves do servidor. Não apaga os ditados já feitos. Revogue as chaves no provedor.')
    expect(document.activeElement).toBe(within(grupo).getByRole('button', { name: 'Cancelar' }))
    expect(escritas(srv)).toEqual([])
  })

  it('o Esc e o Cancelar fecham, devolvem o foco ao Desligar e não desligam nada', async () => {
    const { srv, grupo } = await perguntar()
    fireEvent.keyDown(grupo, { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Desligar o ditado no celular' })))
    expect(escritas(srv)).toEqual([])
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Desligar o ditado no celular' }))
    })
    await act(async () => {
      fireEvent.click(within(within(secao()).getByRole('group', { name: 'Desligar o ditado no celular' })).getByRole('button', { name: 'Cancelar' }))
    })
    expect(within(secao()).queryByRole('group', { name: 'Desligar o ditado no celular' })).toBeNull()
    expect(srv.daConta.aceite).not.toBeNull()
  })

  it('Desligar: revoga o aceite, apaga as chaves, e a seção volta a "Desligado."; o foco vai ao título', async () => {
    const { srv, grupo } = await perguntar()
    await act(async () => {
      fireEvent.click(within(grupo).getByRole('button', { name: 'Desligar' }))
    })
    await within(secao()).findByText('Desligado.', undefined, espera)
    expect(escritas(srv)).toEqual(['DELETE /api/v1/dictation/consent'])
    expect(srv.daConta.aceite).toBeNull()
    expect(srv.daConta.chaves).toEqual({})
    expect(aviso().textContent).toBe('Ditar pelo celular desligado.')
    expect(within(secao()).queryByLabelText('Provedor da voz')).toBeNull()
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Ditar pelo celular'))
  })

  it('depois de desligar, o microfone pede a folha de novo (a porta esqueceu que estava pronta)', async () => {
    const { p } = await ligadaComChave()
    // O gravador já passou pela porta nesta página.
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    const microfone = await screen.findByRole('button', { name: 'Ditar' }, espera)
    await act(async () => {
      fireEvent.click(microfone)
    })
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    })
    expect(p.estado.abriu).toBe(1)

    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await act(async () => {
      fireEvent.click(within(secao()).getByRole('button', { name: 'Desligar o ditado no celular' }))
    })
    await act(async () => {
      fireEvent.click(within(within(secao()).getByRole('group', { name: 'Desligar o ditado no celular' })).getByRole('button', { name: 'Desligar' }))
    })
    await within(secao()).findByText('Desligado.', undefined, espera)

    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await act(async () => {
      fireEvent.click(await screen.findByRole('button', { name: 'Ditar' }, espera))
    })
    await folha()
    expect(p.estado.abriu).toBe(1)
  })
})

describe('apagar meus dados da nuvem', () => {
  it('apaga também o aceite, as preferências e as chaves: a seção lê de novo e fica desligada', async () => {
    const { srv } = await ligadaComChave({ preferencias: { dica: true } })
    fireEvent.click(screen.getByRole('button', { name: 'Apagar meus dados da nuvem' }))
    fireEvent.change(screen.getByLabelText(/Para confirmar, digite/), { target: { value: 'apagar' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apagar tudo' }))
    await within(secao()).findByText('Desligado.', undefined, espera)
    expect(srv.daConta.aceite).toBeNull()
    expect(srv.daConta.chaves).toEqual({})
    expect(srv.daConta.preferencias.dica).toBe(false)
  })
})

describe('a Privacidade', () => {
  it('diz o que mudou com o ditado pelo celular: a chave do PC continua no PC, e a voz só fica em memória', async () => {
    await abrir({ ditado: {} })
    const priv = screen.getByRole('region', { name: 'Privacidade' })
    const t = priv.textContent!
    expect(t).toContain('Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os detalhes da transcrição, e as trocas aprendidas. O texto fica legível para quem administra o servidor.')
    expect(t).toContain('Ficam 6 meses (a lixeira, 30 dias). Uma cópia cifrada guarda tudo por cerca de 30 dias além disso.')
    expect(t).toContain('Do PC nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos PCs; a chave do PC continua no PC.')
    expect(t).toContain(
      'Ditar pelo celular é opcional. Se você ligar, a voz de cada ditado vai ao provedor que você escolher (OpenAI ou Groq), com a sua chave, que o servidor guarda cifrada. A voz não é gravada: fica na memória e é descartada ao fim.',
    )
    // O build da nuvem não conhece o Gerenciador de Credenciais do Windows.
    expect(document.body.textContent).not.toContain('Gerenciador de Credenciais')
  })
})
