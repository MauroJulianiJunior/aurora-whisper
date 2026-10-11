import { describe, expect, it } from 'vitest'
import type { Editaveis, ResultadoTeste } from './api'
import { bloqueioDoPolish, comSugestao, editaveis, iguais, mesclar, paraEnviar, pedemReinicio, sugeridoNaConta, teclas } from './config'
import { configuracoesPadrao } from './testes/servidorFalso'

const base: Editaveis = editaveis(configuracoesPadrao())

describe('editaveis', () => {
  it('leva só o que o PUT aceita: nem a tecla, nem a versão, nem o que o Agent informa', () => {
    const c = configuracoesPadrao({ autostart_indisponivel: 'motivo', reinicio_pendente: ['beep_enabled'], polish_disponivel: true, version: 7 })
    expect(Object.keys(editaveis(c)).sort()).toEqual(['ai_model', 'ai_provider', 'autostart', 'beep_enabled', 'hold_threshold_ms', 'microphone_always_ready', 'text_mode'])
  })
})

describe('iguais', () => {
  it.each([
    ['hold_threshold_ms', 750],
    ['beep_enabled', false],
    ['microphone_always_ready', true],
    ['text_mode', 'literal'],
    ['ai_provider', 'openai'],
    ['ai_model', 'um-modelo'],
    ['autostart', true],
  ] as const)('vê a mudança em %s', (campo, valor) => {
    expect(iguais(base, { ...base })).toBe(true)
    expect(iguais(base, { ...base, [campo]: valor })).toBe(false)
  })
})

describe('mesclar', () => {
  it('o que eu mudei fica como deixei; o que não mudei vem do atual', () => {
    const meu = { ...base, hold_threshold_ms: 900 }
    const atual = { ...base, beep_enabled: false, ai_model: 'da-outra-aba' }
    expect(mesclar(base, meu, atual)).toEqual({ ...base, hold_threshold_ms: 900, beep_enabled: false, ai_model: 'da-outra-aba' })
  })

  it('o microfone sempre pronto anda como o bip: o que eu não mexi vem do atual, o que mexi fica', () => {
    expect(mesclar(base, { ...base, hold_threshold_ms: 900 }, { ...base, microphone_always_ready: true }).microphone_always_ready).toBe(true)
    expect(mesclar(base, { ...base, microphone_always_ready: true }, { ...base, beep_enabled: false }).microphone_always_ready).toBe(true)
    expect(mesclar({ ...base, microphone_always_ready: true }, base, { ...base, microphone_always_ready: true }).microphone_always_ready).toBe(false)
  })

  it('o mesmo campo mudado dos dois lados: vale o meu', () => {
    expect(mesclar(base, { ...base, text_mode: 'literal' }, { ...base, text_mode: 'polish' }).text_mode).toBe('literal')
  })

  it('campo que eu mudei e voltei ao que era: vem do atual', () => {
    expect(mesclar(base, { ...base }, { ...base, autostart: true }).autostart).toBe(true)
  })

  it('provedor e modelo andam juntos: mexi só no modelo, e o provedor que a outra aba trocou não fica com ele', () => {
    const b = { ...base, ai_provider: 'openai' as const, ai_model: 'modelo-antigo' }
    const meu = { ...b, ai_model: 'modelo-da-openai' }
    const atual = { ...b, ai_provider: 'anthropic' as const, ai_model: '' }
    expect(mesclar(b, meu, atual)).toMatchObject({ ai_provider: 'openai', ai_model: 'modelo-da-openai' })
  })

  it('provedor e modelo andam juntos: mexi só no provedor, e o modelo que a outra aba escreveu não vem junto', () => {
    const meu = { ...base, ai_provider: 'anthropic' as const }
    const atual = { ...base, ai_model: 'modelo-da-openai' }
    expect(mesclar(base, meu, atual)).toMatchObject({ ai_provider: 'anthropic', ai_model: '' })
  })

  it('sem mexer em provedor nem modelo, o par vem inteiro do atual', () => {
    const atual = { ...base, ai_provider: 'anthropic' as const, ai_model: 'da-outra-aba' }
    expect(mesclar(base, { ...base, beep_enabled: false }, atual)).toMatchObject({ ai_provider: 'anthropic', ai_model: 'da-outra-aba', beep_enabled: false })
  })
})

describe('paraEnviar', () => {
  // O autostart está fora da version: reenviar o que a tela leu desfazia
  // o que a bandeja ou o Gerenciador de Tarefas mudaram depois.
  it('o autostart que a pessoa não mexeu fica fora do PUT; o resto vai', () => {
    const rascunho = { ...base, beep_enabled: false }
    expect(paraEnviar(base, rascunho)).toEqual({
      hold_threshold_ms: 700,
      beep_enabled: false,
      microphone_always_ready: false,
      text_mode: 'smart',
      ai_provider: '',
      ai_model: '',
    })
    expect('autostart' in paraEnviar({ ...base, autostart: true }, { ...base, autostart: true })).toBe(false)
  })

  it('o microfone sempre pronto vai com o rascunho, ligado ou desligado', () => {
    expect(paraEnviar(base, { ...base, microphone_always_ready: true }).microphone_always_ready).toBe(true)
    expect(paraEnviar({ ...base, microphone_always_ready: true }, base).microphone_always_ready).toBe(false)
  })

  it('o autostart que a pessoa mudou vai, ligando ou desligando', () => {
    expect(paraEnviar(base, { ...base, autostart: true })).toEqual({ ...base, autostart: true })
    expect(paraEnviar({ ...base, autostart: true }, base)).toEqual({ ...base, autostart: false })
  })

  it('não mexe no rascunho', () => {
    const rascunho = { ...base }
    paraEnviar(base, rascunho)
    expect(rascunho).toEqual(base)
  })
})

describe('pedemReinicio', () => {
  it('só o que este salvamento mudou conta', () => {
    const enviado = { ...base, beep_enabled: false }
    expect(pedemReinicio(base, enviado, ['hold_threshold_ms'])).toBe(false)
    expect(pedemReinicio(base, enviado, ['beep_enabled'])).toBe(true)
    expect(pedemReinicio(base, { ...enviado, hold_threshold_ms: 900 }, ['hold_threshold_ms'])).toBe(true)
    expect(pedemReinicio(base, { ...base }, ['hold_threshold_ms', 'beep_enabled'])).toBe(false)
    expect(pedemReinicio(base, { ...base, microphone_always_ready: true }, ['microphone_always_ready'])).toBe(true)
  })
})

describe('teclas', () => {
  it('o atalho do contrato vira as teclas da tela', () => {
    expect(teclas('ctrl+win')).toEqual(['Ctrl', 'Win'])
    expect(teclas('Ctrl + Alt + f9')).toEqual(['Ctrl', 'Alt', 'F9'])
  })
})

describe('bloqueioDoPolish', () => {
  const chave = { provedor: 'openai' as const, configurada: true, final: 'a1B2' }

  it('sem polish_disponivel do Agent, nunca libera — nem com provedor e chave', () => {
    expect(bloqueioDoPolish({}, 'openai', chave)).toContain('próxima versão')
    expect(bloqueioDoPolish({ polish_disponivel: false }, 'openai', chave)).toContain('próxima versão')
  })

  it('com o Polish no Agent: pede o provedor, depois a chave dele', () => {
    expect(bloqueioDoPolish({ polish_disponivel: true }, '', undefined)).toContain('selecione o provedor')
    expect(bloqueioDoPolish({ polish_disponivel: true }, 'anthropic', undefined)).toContain('chave de API da Anthropic')
    expect(bloqueioDoPolish({ polish_disponivel: true }, 'openai', { ...chave, configurada: false })).toContain('chave de API da OpenAI')
    expect(bloqueioDoPolish({ polish_disponivel: true }, 'openai', chave)).toBeNull()
  })
})

describe('sugeridoNaConta', () => {
  const conta = ['gpt-5-nano', 'gpt-6-luna']

  it('é o sugerido pelo Agent, se a conta o tem', () => {
    expect(sugeridoNaConta({ ok: true, modelos: conta, modelo_sugerido: 'gpt-6-luna' })).toBe('gpt-6-luna')
  })

  it.each<[string, ResultadoTeste | null | undefined]>([
    ['sem teste', null],
    ['sem teste ainda (undefined)', undefined],
    ['teste que falhou', { ok: false, erro: 'A OpenAI recusou a chave.' }],
    ['sem sugestão (vazio)', { ok: true, modelos: conta, modelo_sugerido: '' }],
    ['sem o campo (Agent de antes)', { ok: true, modelos: conta }],
    ['sugerido que a conta não tem', { ok: true, modelos: conta, modelo_sugerido: 'gpt-9-inexistente' }],
    ['conta sem nenhum modelo', { ok: true, modelos: [], modelo_sugerido: 'gpt-6-luna' }],
    ['nome quase igual não vale', { ok: true, modelos: conta, modelo_sugerido: 'GPT-6-LUNA' }],
  ])('é vazio: %s', (_, r) => {
    expect(sugeridoNaConta(r)).toBe('')
  })
})

describe('comSugestao', () => {
  const rascunho: Editaveis = { ...base, ai_provider: 'openai', ai_model: '' }

  it('sem modelo escolhido, o sugerido entra — e só o modelo muda', () => {
    expect(comSugestao(rascunho, 'openai', 'gpt-6-luna')).toEqual({ ...rascunho, ai_model: 'gpt-6-luna' })
  })

  it('não mexe no rascunho que recebeu', () => {
    const antes = { ...rascunho }
    comSugestao(rascunho, 'openai', 'gpt-6-luna')
    expect(rascunho).toEqual(antes)
  })

  it('modelo só de espaços não é escolha', () => {
    expect(comSugestao({ ...rascunho, ai_model: '   ' }, 'openai', 'gpt-6-luna').ai_model).toBe('gpt-6-luna')
  })

  it.each([
    ['já há um modelo escolhido', { ...rascunho, ai_model: 'gpt-5-nano' }, 'openai', 'gpt-6-luna'],
    ['o rascunho é de outro provedor', { ...rascunho, ai_provider: 'anthropic' as const }, 'openai', 'gpt-6-luna'],
    ['o rascunho ficou sem provedor', { ...rascunho, ai_provider: '' as const }, 'openai', 'gpt-6-luna'],
    ['nada sugerido', rascunho, 'openai', ''],
  ])('devolve o mesmo rascunho: %s', (_, r, provedor, sugerido) => {
    expect(comSugestao(r, provedor as 'openai', sugerido)).toBe(r)
  })
})
