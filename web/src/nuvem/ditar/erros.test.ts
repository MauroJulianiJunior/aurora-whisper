// O que cada falha do ditado vira na tela (ADR-021, "A tela"): a frase, e se o
// áudio fica na memória para "Tentar de novo". O app decide pelo error.code.

import { describe, expect, it } from 'vitest'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { type ErroDoDitado, MicrofoneIndisponivel, erroDoEnvio, erroDoMicrofone, erroSemFala, fraseDoAviso, textoDaEspera } from './erros'

function api(status: number, codigo?: string, esperaSeg?: number): ErroDaApi {
  const e = new ErroDaApi(status, 'a frase do servidor, que o app não usa')
  if (codigo) e.codigo = codigo
  if (esperaSeg !== undefined) e.esperaSeg = esperaSeg
  return e
}

describe('a falha do envio, pelo código', () => {
  const tabela: [string, unknown, Partial<ErroDoDitado>][] = [
    ['SEM_ACEITE', api(403, 'SEM_ACEITE'), { tipo: 'sem-aceite', retem: true, vai: 'configuracoes' }],
    ['SEM_CHAVE', api(403, 'SEM_CHAVE'), { tipo: 'sem-chave', frase: 'Falta a chave do provedor.', retem: true, vai: 'configuracoes' }],
    ['UM_POR_VEZ', api(429, 'UM_POR_VEZ', 7), { tipo: 'espere', frase: 'Espere um pouco. Tente de novo em 7 s.', retem: true }],
    ['LIMITE', api(429, 'LIMITE', 30), { tipo: 'espere', frase: 'Espere um pouco. Tente de novo em 30 s.', retem: true }],
    ['OCUPADO', api(503, 'OCUPADO', 2), { tipo: 'espere', frase: 'Espere um pouco. Tente de novo em 2 s.', retem: true }],
    ['PAYLOAD_TOO_LARGE', api(413, 'PAYLOAD_TOO_LARGE'), { tipo: 'longo', frase: 'Ficou longo demais.', retem: false }],
    ['FORMATO', api(415, 'FORMATO'), { tipo: 'formato', retem: false }],
    ['SEM_FALA', api(422, 'SEM_FALA'), { tipo: 'sem-fala', frase: 'Não ouvi nada.', retem: false }],
    ['PROVEDOR_RECUSOU_CHAVE', api(502, 'PROVEDOR_RECUSOU_CHAVE'), { tipo: 'provedor-chave', frase: 'O provedor recusou a chave.', retem: true, vai: 'configuracoes' }],
    ['PROVEDOR_LIMITE', api(502, 'PROVEDOR_LIMITE'), { tipo: 'provedor-fora', frase: 'O provedor não respondeu agora.', retem: true }],
    ['PROVEDOR_FORA', api(502, 'PROVEDOR_FORA'), { tipo: 'provedor-fora', frase: 'O provedor não respondeu agora.', retem: true }],
    ['APAGADO', api(410, 'APAGADO'), { tipo: 'apagado', frase: 'Esse ditado foi apagado.', retem: false }],
  ]

  it.each(tabela)('%s', (_, e, esperado) => {
    expect(erroDoEnvio(e)).toMatchObject(esperado)
  })

  it('o código vale mais que o status: o app não decide pela frase do servidor', () => {
    const e = api(500, 'APAGADO')
    expect(erroDoEnvio(e).tipo).toBe('apagado')
    expect(erroDoEnvio(e).frase).not.toContain('servidor')
  })
})

describe('a falha do envio que o servidor não explicou', () => {
  it('sem sessão: o áudio fica, e a frase manda entrar de novo', () => {
    expect(erroDoEnvio(new SemSessao())).toEqual({ tipo: 'sessao', frase: 'O ditado espera você entrar de novo.', retem: true })
  })

  it('sem rede: o áudio fica', () => {
    expect(erroDoEnvio(new ForaDoAr())).toEqual({ tipo: 'rede', frase: 'Sem conexão.', retem: true })
  })

  it('o prazo de 90 s (o AbortController): o áudio fica', () => {
    expect(erroDoEnvio(new DOMException('abortado', 'AbortError'))).toEqual({ tipo: 'prazo', frase: 'O envio demorou demais.', retem: true })
  })

  it('pelo status, quando não há código (um proxy)', () => {
    expect(erroDoEnvio(api(413))).toMatchObject({ tipo: 'longo', retem: false })
    expect(erroDoEnvio(api(404))).toMatchObject({ tipo: 'indisponivel', retem: false })
    expect(erroDoEnvio(api(429, undefined, 10))).toMatchObject({ tipo: 'espere', retem: true })
    expect(erroDoEnvio(api(503))).toMatchObject({ tipo: 'espere', frase: 'Espere um pouco.', retem: true })
    expect(erroDoEnvio(api(500))).toMatchObject({ tipo: 'outro', retem: true })
  })

  it('um código desconhecido cai pelo status, e a frase do servidor nunca vai à tela', () => {
    const e = erroDoEnvio(api(500, 'CODIGO_NOVO'))
    expect(e).toMatchObject({ tipo: 'outro', frase: 'Não deu para enviar o ditado.', retem: true })
  })

  it('o que não é da API (um bug) também guarda o áudio', () => {
    expect(erroDoEnvio(new TypeError('x'))).toMatchObject({ tipo: 'outro', retem: true })
  })
})

describe('textoDaEspera: o Retry-After', () => {
  it.each([
    [undefined, ''],
    [0, ''],
    [-3, ''],
    [12, ' Tente de novo em 12 s.'],
    [89, ' Tente de novo em 89 s.'],
    [90, ' Tente de novo em 2 min.'],
    [600, ' Tente de novo em 10 min.'],
  ])('%s', (seg, texto) => {
    expect(textoDaEspera(seg)).toBe(texto)
  })
})

describe('a falha do microfone', () => {
  const nomeado = (nome: string) => Object.assign(new Error('x'), { name: nome })

  it.each([
    ['NotAllowedError', 'permissao', 'Sem acesso ao microfone. Libere nas permissões do site.'],
    ['SecurityError', 'permissao', 'Sem acesso ao microfone. Libere nas permissões do site.'],
    ['NotFoundError', 'sem-microfone', 'Não achei um microfone.'],
    ['OverconstrainedError', 'sem-microfone', 'Não achei um microfone.'],
    ['NotReadableError', 'microfone-ocupado', 'Não consegui abrir o microfone. Outro app pode estar usando.'],
    ['AbortError', 'microfone-ocupado', 'Não consegui abrir o microfone. Outro app pode estar usando.'],
    ['QualquerOutra', 'gravacao', 'A gravação falhou. Tente de novo.'],
  ])('%s', (nome, tipo, frase) => {
    expect(erroDoMicrofone(nomeado(nome))).toEqual({ tipo, frase, retem: false })
  })

  it('o navegador sem o formato ou sem o suporte', () => {
    expect(erroDoMicrofone(new MicrofoneIndisponivel('sem-formato')).tipo).toBe('sem-formato')
    expect(erroDoMicrofone(new MicrofoneIndisponivel('sem-suporte')).tipo).toBe('sem-suporte')
  })

  it('o que não é Error também vira a falha da gravação', () => {
    expect(erroDoMicrofone('texto').tipo).toBe('gravacao')
    expect(erroDoMicrofone(undefined).tipo).toBe('gravacao')
  })
})

describe('erroSemFala', () => {
  it('a frase é uma só; a parada do app vem antes', () => {
    expect(erroSemFala().frase).toBe('Não ouvi nada.')
    expect(erroSemFala('Parei quando o app saiu da frente.').frase).toBe('Parei quando o app saiu da frente. Não ouvi nada.')
    expect(erroSemFala().retem).toBe(false)
  })
})

describe('fraseDoAviso: o motivo de o Polish não ter agido', () => {
  it('um motivo conhecido vira frase', () => {
    expect(fraseDoAviso('chave_recusada')).toBe('O Polish não agiu: o provedor recusou a chave.')
    expect(fraseDoAviso('sem_credito')).toBe('O Polish não agiu: falta crédito no provedor.')
    expect(fraseDoAviso('tempo_esgotado')).toBe('O Polish não agiu: o provedor demorou.')
  })

  it('um motivo que o app não conhece não vira frase solta, e nunca mostra o texto que veio', () => {
    expect(fraseDoAviso('texto livre qualquer')).toBe('O Polish não agiu.')
    expect(fraseDoAviso('')).toBe('O Polish não agiu.')
  })
})
