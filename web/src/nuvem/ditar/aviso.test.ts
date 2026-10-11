// O aviso do ditado pelo celular (aviso.ts, estado.ts): a versão dele presa à do
// servidor de mentira, o que cada provedor diz sem afirmar país que a página
// dele não afirma, e as regras puras do aceite (quem está dentro, quem precisa
// da folha, quais modelos a tela mostra).

import { describe, expect, it } from 'vitest'
import { versaoDoAvisoNoServidorFalso } from '../../testes/servidorFalsoNuvem'
import { blocoDoProvedor, ehProvedor, nomeDoProvedor, versaoDoAviso } from './aviso'
import { type EstadoDoDitado, aceiteVigente, aceitoPara, chavesVazias, modelosDeVoz, modelosDoPolish, preferenciasPadrao, provedoresExigidos } from './estado'

describe('a versão do aviso', () => {
  it('é uma data (AAAA-MM-DD), e é a mesma do servidor de mentira', () => {
    expect(versaoDoAviso).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    // Quem prende o app ao servidor de verdade é o teste da C3b; aqui, ao dublê.
    // A versão é PROVISÓRIA até a constante do servidor existir (aviso.ts).
    expect(versaoDoAvisoNoServidorFalso).toBe(versaoDoAviso)
  })
})

describe('o que cada provedor diz', () => {
  it('a OpenAI: o país só com a residência de dados fixada, e a retenção de até 30 dias', () => {
    const t = blocoDoProvedor.openai
    expect(t).toContain('EUA ou na Europa se a sua conta fixou a residência de dados')
    expect(t).toContain('não diz em que país')
    expect(t).toContain('até 30 dias')
    expect(t).toContain('não usa o que você envia pela API para treinar')
    expect(t).toContain('lido em 10-out-2026')
    expect(t).toContain('não confere a sua conta')
  })

  it('a Groq: o que ela guarda fica nos EUA, e a documentação não diz onde a transcrição roda', () => {
    const t = blocoDoProvedor.groq
    expect(t).toContain('não diz onde a transcrição roda')
    expect(t).toContain('até 30 dias')
    expect(t).toContain('retenção zero')
    expect(t).toContain('não falam em treinar')
    expect(t).toContain('não confere a sua conta')
  })

  it('a Anthropic (Polish): guarda até 30 dias, sem treinar, e o aviso não afirma país nenhum', () => {
    const t = blocoDoProvedor.anthropic
    expect(t).toContain('até 30 dias')
    expect(t).toContain('não o usa para treinar')
    expect(t).toContain('não afirma em que país')
  })

  it('nenhum bloco afirma que o provedor processa no Brasil', () => {
    for (const t of Object.values(blocoDoProvedor)) expect(t).not.toMatch(/processa no Brasil/i)
  })

  it('os nomes, e só os três provedores são reconhecidos', () => {
    expect(nomeDoProvedor).toEqual({ openai: 'OpenAI', groq: 'Groq', anthropic: 'Anthropic' })
    expect(['openai', 'groq', 'anthropic'].every(ehProvedor)).toBe(true)
    for (const v of ['gemini', '', null, undefined, 3, 'OPENAI']) expect(ehProvedor(v)).toBe(false)
  })
})

function estado(parcial: Partial<EstadoDoDitado> = {}): EstadoDoDitado {
  return { preferencias: preferenciasPadrao, aceite: null, chaves: chavesVazias(), ...parcial }
}

describe('o aceite vigente', () => {
  const aceite = (versao: string, provedores: ('openai' | 'groq' | 'anthropic')[]) => ({ versao, provedores, aceito_em: '2026-10-10T12:00:00.000Z' })

  it('só vale com a versão do aviso de agora', () => {
    expect(aceiteVigente(estado({ aceite: aceite(versaoDoAviso, ['openai']) }))).not.toBeNull()
    expect(aceiteVigente(estado({ aceite: aceite('2026-01-01', ['openai']) }))).toBeNull()
    expect(aceiteVigente(estado())).toBeNull()
  })

  it('cobre só os provedores que a pessoa aceitou', () => {
    const e = estado({ aceite: aceite(versaoDoAviso, ['openai']) })
    expect(aceitoPara(e, ['openai'])).toBe(true)
    expect(aceitoPara(e, ['groq'])).toBe(false)
    expect(aceitoPara(e, ['openai', 'anthropic'])).toBe(false)
    expect(aceitoPara(estado({ aceite: aceite('2026-01-01', ['openai']) }), ['openai'])).toBe(false)
  })

  it('os provedores que as preferências usam: o da voz e, com o Polish, o dele', () => {
    expect(provedoresExigidos(preferenciasPadrao)).toEqual(['openai'])
    expect(provedoresExigidos({ ...preferenciasPadrao, provedor: 'groq' })).toEqual(['groq'])
    expect(provedoresExigidos({ ...preferenciasPadrao, polish: true })).toEqual(['openai'])
    expect(provedoresExigidos({ ...preferenciasPadrao, polish: true, polish_provedor: 'anthropic' })).toEqual(['openai', 'anthropic'])
    expect(provedoresExigidos({ ...preferenciasPadrao, provedor: 'groq', polish: true, polish_provedor: 'openai' })).toEqual(['groq', 'openai'])
    // O Polish desligado não exige o provedor dele.
    expect(provedoresExigidos({ ...preferenciasPadrao, polish: false, polish_provedor: 'anthropic' })).toEqual(['openai'])
  })
})

describe('os modelos que a chave alcança', () => {
  const lista = ['gpt-4o-transcribe', 'gpt-4o-mini-transcribe', 'whisper-1', 'gpt-4o', 'gpt-4o-mini', 'whisper-large-v3-turbo', 'llama-3.3-70b-versatile']

  it('a voz mostra os de transcrição e whisper', () => {
    expect(modelosDeVoz(lista)).toEqual(['gpt-4o-transcribe', 'gpt-4o-mini-transcribe', 'whisper-1', 'whisper-large-v3-turbo'])
  })

  it('o Polish mostra os outros', () => {
    expect(modelosDoPolish(lista)).toEqual(['gpt-4o', 'gpt-4o-mini', 'llama-3.3-70b-versatile'])
  })
})
