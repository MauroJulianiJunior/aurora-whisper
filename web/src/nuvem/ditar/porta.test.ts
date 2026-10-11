// A porta antes do microfone (porta.ts; ADR-021, decisão H): confere a conta
// antes de o getUserMedia abrir. Sem o aceite dos provedores das preferências,
// pede a folha, e sem o "Aceito" o microfone não abre; sem a chave, leva às
// Configurações; com tudo pronto, passa uma vez e lembra. Cliente e folha são
// dublês: aqui se prova a decisão, e a tela está em aceite.test.tsx.

import { describe, expect, it, vi } from 'vitest'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { VERSAO_DO_AVISO } from '../avisoDoDitado'
import { type EstadoDoDitado, chavesVazias, preferenciasPadrao } from './estado'
import type { Folha, PedidoDeAceite } from './folha'
import { type GanchosDaPorta, criarPorta } from './porta'

const pronto = (): EstadoDoDitado => ({
  preferencias: { ...preferenciasPadrao },
  aceite: { versao: VERSAO_DO_AVISO, transcricao: 'openai', polish: null, aceito_em: '2026-10-10T12:00:00.000Z', vigente: true },
  chaves: { ...chavesVazias(), openai: { configurada: true, final: 'WXYZ', colar_de_novo: false } },
})

const semAceite = (): EstadoDoDitado => ({ ...pronto(), aceite: null, chaves: chavesVazias() })

function montar(estados: (EstadoDoDitado | Error)[], aceitar: (p: PedidoDeAceite) => { voz: 'openai' | 'groq' } | null = () => ({ voz: 'openai' })) {
  const fila = [...estados]
  let ultimo = estados[estados.length - 1]!
  const estadoDoDitado = vi.fn(async () => {
    const e = fila.length > 0 ? fila.shift()! : ultimo
    ultimo = e
    if (e instanceof Error) throw e
    return e
  })
  const salvarPreferencias = vi.fn(async (p: EstadoDoDitado['preferencias']) => p)
  const pedirAceite = vi.fn(async (p: PedidoDeAceite) => aceitar(p))
  const porta = criarPorta({ estadoDoDitado, salvarPreferencias }, { pedirAceite } as unknown as Folha)
  const ganchos = { avisar: vi.fn(), abrirConfiguracoes: vi.fn(), aoSemSessao: vi.fn() } satisfies GanchosDaPorta
  return { porta, ganchos, estadoDoDitado, salvarPreferencias, pedirAceite }
}

describe('com o aceite e a chave na conta', () => {
  it('passa, sem folha, sem aviso, sem Configurações', async () => {
    const m = montar([pronto()])
    expect(await m.porta.liberar(m.ganchos)).toBe(true)
    expect(m.pedirAceite).not.toHaveBeenCalled()
    expect(m.ganchos.avisar).not.toHaveBeenCalled()
    expect(m.ganchos.abrirConfiguracoes).not.toHaveBeenCalled()
  })

  it('lembra: a segunda vez não vai à conta (o ditado sem rede continua gravando)', async () => {
    const m = montar([pronto()])
    await m.porta.liberar(m.ganchos)
    await m.porta.liberar(m.ganchos)
    expect(m.estadoDoDitado).toHaveBeenCalledTimes(1)
  })

  it('invalidar faz a próxima conferência ir à conta de novo', async () => {
    const m = montar([pronto()])
    await m.porta.liberar(m.ganchos)
    m.porta.invalidar()
    await m.porta.liberar(m.ganchos)
    expect(m.estadoDoDitado).toHaveBeenCalledTimes(2)
  })

  it('o Polish ligado exige também o aceite e a chave do provedor dele', async () => {
    const e = pronto()
    e.preferencias = { ...e.preferencias, polish: true, polish_provedor: 'anthropic' }
    const m = montar([e], () => null)
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.pedirAceite).toHaveBeenCalledTimes(1)
    expect(m.pedirAceite.mock.calls[0]![0]).toEqual({ voz: 'openai', polish: 'anthropic', escolherVoz: true })
  })

  it('o aceite que cobre os dois (a voz e o Polish) não pede folha', async () => {
    const e = pronto()
    e.preferencias = { ...e.preferencias, polish: true, polish_provedor: 'anthropic', polish_modelo: 'claude-haiku-4-5' }
    e.aceite = { ...e.aceite!, polish: 'anthropic' }
    e.chaves = { ...e.chaves, anthropic: { configurada: true, final: 'ANTH', colar_de_novo: false } }
    const m = montar([e])
    expect(await m.porta.liberar(m.ganchos)).toBe(true)
    expect(m.pedirAceite).not.toHaveBeenCalled()
  })
})

describe('sem o aceite', () => {
  it('pede a folha, com a escolha do provedor da voz, e sem o Aceito não libera nem salva nada', async () => {
    const m = montar([semAceite()], () => null)
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.pedirAceite).toHaveBeenCalledTimes(1)
    expect(m.pedirAceite.mock.calls[0]![0]).toEqual({ voz: 'openai', polish: undefined, escolherVoz: true })
    expect(m.salvarPreferencias).not.toHaveBeenCalled()
    // Nenhum aviso e nenhuma ida às Configurações: a pessoa disse "Agora não".
    expect(m.ganchos.avisar).not.toHaveBeenCalled()
    expect(m.ganchos.abrirConfiguracoes).not.toHaveBeenCalled()
  })

  it('o aceite de um aviso velho (o servidor diz vigente: false) não vale: a folha abre, e o microfone não', async () => {
    const e = pronto()
    e.aceite = { ...e.aceite!, vigente: false }
    const m = montar([e], () => null)
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.pedirAceite).toHaveBeenCalledTimes(1)
  })

  it('com o Aceito e a chave na conta: avisa e NÃO abre o microfone (a pessoa toca de novo)', async () => {
    const aceito = { ...pronto() }
    const m = montar([{ ...semAceite(), chaves: pronto().chaves }, aceito])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Aceito. Toque no microfone para ditar.')
    expect(m.ganchos.abrirConfiguracoes).not.toHaveBeenCalled()
    // O toque seguinte passa.
    expect(await m.porta.liberar(m.ganchos)).toBe(true)
  })

  it('com o Aceito e sem a chave: avisa e leva às Configurações, na seção do ditado', async () => {
    const m = montar([semAceite(), { ...pronto(), chaves: chavesVazias() }])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Falta a chave da OpenAI. Cole a chave nas Configurações.')
    expect(m.ganchos.abrirConfiguracoes).toHaveBeenCalledWith('ditar')
  })

  it('o provedor escolhido na folha vira o das preferências, com o modelo no padrão', async () => {
    const aceito: EstadoDoDitado = { ...pronto(), aceite: { versao: VERSAO_DO_AVISO, transcricao: 'groq', polish: null, aceito_em: '', vigente: true } }
    aceito.preferencias = { ...aceito.preferencias, provedor: 'groq' }
    const m = montar([semAceite(), aceito], () => ({ voz: 'groq' }))
    await m.porta.liberar(m.ganchos)
    expect(m.salvarPreferencias).toHaveBeenCalledWith({ ...preferenciasPadrao, provedor: 'groq', modelo: '' })
    // Sem a chave da Groq, o aviso nomeia a Groq.
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Falta a chave da Groq. Cole a chave nas Configurações.')
  })

  it('se o provedor escolhido é o das preferências, não salva nada', async () => {
    const m = montar([semAceite(), pronto()])
    await m.porta.liberar(m.ganchos)
    expect(m.salvarPreferencias).not.toHaveBeenCalled()
  })
})

describe('sem a chave, com o aceite', () => {
  it('não pede a folha: leva às Configurações', async () => {
    const e = { ...pronto(), chaves: chavesVazias() }
    const m = montar([e])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.pedirAceite).not.toHaveBeenCalled()
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Falta a chave da OpenAI. Cole a chave nas Configurações.')
    expect(m.ganchos.abrirConfiguracoes).toHaveBeenCalledWith('ditar')
  })

  it('a chave que o servidor já não decifra (colar_de_novo) pede para colar de novo, no mesmo lugar', async () => {
    const e = { ...pronto(), chaves: { ...chavesVazias(), openai: { configurada: false, final: '', colar_de_novo: true } } }
    const m = montar([e])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.pedirAceite).not.toHaveBeenCalled()
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Cole a chave da OpenAI de novo nas Configurações.')
    expect(m.ganchos.abrirConfiguracoes).toHaveBeenCalledWith('ditar')
  })

  it('e não lembra: a chave colada depois vale no toque seguinte', async () => {
    const m = montar([{ ...pronto(), chaves: chavesVazias() }, pronto()])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(await m.porta.liberar(m.ganchos)).toBe(true)
  })
})

describe('quando a conta não responde', () => {
  it('sem rede: avisa, e o microfone não abre (sem saber do aceite, não abre)', async () => {
    const m = montar([new ForaDoAr()])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Sem conexão.')
    expect(m.pedirAceite).not.toHaveBeenCalled()
  })

  it('a sessão acabou: acende a faixa da sessão', async () => {
    const m = montar([new SemSessao()])
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.ganchos.aoSemSessao).toHaveBeenCalledTimes(1)
    expect(m.ganchos.avisar).not.toHaveBeenCalled()
  })

  it('o servidor sem o ditado (404) e qualquer outro erro, com frase nossa', async () => {
    const a = montar([new ErroDaApi(404, 'rota não encontrada')])
    expect(await a.porta.liberar(a.ganchos)).toBe(false)
    expect(a.ganchos.avisar).toHaveBeenCalledWith('Este servidor não tem o ditado pelo celular.')

    const b = montar([new ErroDaApi(500, 'detalhe interno do servidor')])
    expect(await b.porta.liberar(b.ganchos)).toBe(false)
    expect(b.ganchos.avisar).toHaveBeenCalledWith('Não deu para abrir o ditado. Tente de novo.')
  })

  it('a falha ao salvar o provedor escolhido também não abre', async () => {
    const m = montar([semAceite()], () => ({ voz: 'groq' }))
    m.salvarPreferencias.mockRejectedValueOnce(new ForaDoAr())
    expect(await m.porta.liberar(m.ganchos)).toBe(false)
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Sem conexão.')
  })
})

describe('reabrir o aceite (o servidor recusou o ditado por falta dele)', () => {
  it('abre a folha para as preferências de agora, sem a escolha do provedor, e diz se aceitou', async () => {
    const m = montar([pronto()])
    expect(await m.porta.reabrirAceite(m.ganchos)).toBe(true)
    expect(m.pedirAceite.mock.calls[0]![0]).toEqual({ voz: 'openai', polish: undefined, escolherVoz: false })

    const nao = montar([pronto()], () => null)
    expect(await nao.porta.reabrirAceite(nao.ganchos)).toBe(false)
  })

  it('esquece o que sabia: a conferência seguinte vai à conta', async () => {
    const m = montar([pronto()])
    await m.porta.liberar(m.ganchos)
    await m.porta.reabrirAceite(m.ganchos)
    await m.porta.liberar(m.ganchos)
    expect(m.estadoDoDitado).toHaveBeenCalledTimes(3)
  })

  it('sem rede, não abre a folha', async () => {
    const m = montar([new ForaDoAr()])
    expect(await m.porta.reabrirAceite(m.ganchos)).toBe(false)
    expect(m.pedirAceite).not.toHaveBeenCalled()
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Sem conexão.')
  })
})
