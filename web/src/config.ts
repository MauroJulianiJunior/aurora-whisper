// A tela de Configurações como dado: o que é editável, o que mudou, como
// juntar a escolha de quem edita com o que outra aba gravou, e quando o
// Polish pode ser escolhido. Funções puras, testadas em config.test.ts.

import type { Configuracoes, Editaveis, EstadoChave, Provedor, ResultadoTeste } from './api'

// A faixa e o passo de "Segurar por" (AC-044; internal/agent/dictation,
// MinHoldThreshold e MaxHoldThreshold).
export const segurarMin = 300
export const segurarMax = 2000
export const segurarPasso = 50

export const camposEditaveis = [
  'hold_threshold_ms',
  'beep_enabled',
  'microphone_always_ready',
  'text_mode',
  'ai_provider',
  'ai_model',
  'autostart',
] as const satisfies readonly (keyof Editaveis)[]

export type Campo = (typeof camposEditaveis)[number]

export const nomeDoProvedor: Record<Provedor, string> = { openai: 'OpenAI', anthropic: 'Anthropic' }

// editaveis tira do GET só o que o PUT leva. hotkey fica de fora: nesta
// etapa ela é só leitura.
export function editaveis(c: Configuracoes): Editaveis {
  return {
    hold_threshold_ms: c.hold_threshold_ms,
    beep_enabled: c.beep_enabled,
    microphone_always_ready: c.microphone_always_ready,
    text_mode: c.text_mode,
    ai_provider: c.ai_provider,
    ai_model: c.ai_model,
    autostart: c.autostart,
  }
}

export function iguais(a: Editaveis, b: Editaveis): boolean {
  return camposEditaveis.every((k) => a[k] === b[k])
}

// paraEnviar é o corpo do PUT: os editáveis, menos o autostart quando a
// pessoa não mexeu nele. O autostart mora no registro, fora da version:
// a bandeja e o Gerenciador de Tarefas o mudam sem subir a versão, e o
// 409 não pega. Reenviar o valor lido ao abrir a tela religava, sem
// ninguém pedir, o início que foi desligado lá — e, com
// autostart_indisponivel, dava 400 num controle que a pessoa nem tocou
// (revisão de 29-set-2026). Campo ausente, o Agent mantém o que está.
export function paraEnviar(salvo: Editaveis, rascunho: Editaveis): Partial<Editaveis> {
  const corpo: Partial<Editaveis> = { ...rascunho }
  if (rascunho.autostart === salvo.autostart) delete corpo.autostart
  return corpo
}

// mesclar junta três versões campo a campo: o que quem edita mudou em
// relação à base fica como ele deixou; o resto vem do atual. Serve para
// o 409 (a base é o que a tela carregou, o atual é o que outra aba
// gravou) e para a resposta do salvar (a base é o que foi enviado; o que
// a pessoa mexeu enquanto o pedido ia e voltava não se perde).
//
// Provedor e modelo andam juntos: o nome de um modelo só vale no provedor
// dele. Mexeu em um dos dois, vale o par de quem edita — sem isso, um
// modelo da OpenAI escolhido aqui acabava junto do provedor Anthropic que
// a outra aba gravou (revisão de 29-set-2026).
export function mesclar(base: Editaveis, meu: Editaveis, atual: Editaveis): Editaveis {
  const junto = { ...atual }
  for (const k of camposEditaveis) {
    if (meu[k] !== base[k]) Object.assign(junto, { [k]: meu[k] })
  }
  if (meu.ai_provider !== base.ai_provider || meu.ai_model !== base.ai_model) {
    junto.ai_provider = meu.ai_provider
    junto.ai_model = meu.ai_model
  }
  return junto
}

// pedemReinicio diz se algum campo que ESTE salvamento mudou (de `antes`
// para `enviado`) só vale no próximo início. Um campo que já estava
// pendente de um salvamento anterior, e não foi mexido agora, não conta.
export function pedemReinicio(antes: Editaveis, enviado: Editaveis, pendentes: readonly string[]): boolean {
  return camposEditaveis.some((k) => enviado[k] !== antes[k] && pendentes.includes(k))
}

// teclas separa o atalho do contrato ("ctrl+win") nas teclas que a tela
// mostra ("Ctrl", "Win").
export function teclas(hotkey: string): string[] {
  const nomes: Record<string, string> = { ctrl: 'Ctrl', win: 'Win', alt: 'Alt', shift: 'Shift' }
  return hotkey
    .split('+')
    .map((t) => t.trim())
    .filter(Boolean)
    .map((t) => nomes[t.toLowerCase()] ?? t.charAt(0).toUpperCase() + t.slice(1))
}

// bloqueioDoPolish diz por que o Polish não pode ser escolhido agora, ou
// null quando pode. O Agent é que diz se o Polish existe
// (polish_disponivel); sem isso, o painel não oferece o que não faz.
// Existindo, ele pede um provedor e a chave guardada DESSE provedor.
export function bloqueioDoPolish(c: Pick<Configuracoes, 'polish_disponivel'>, provedor: Editaveis['ai_provider'], chave: EstadoChave | undefined): string | null {
  if (c.polish_disponivel !== true) return 'Chega na próxima versão do Aurora Whisper.'
  if (!provedor) return 'Para escolher, selecione o provedor em IA, abaixo, e guarde a chave de API dele.'
  if (!chave?.configurada) return `Para escolher, guarde a chave de API da ${nomeDoProvedor[provedor]} em IA, abaixo.`
  return null
}

// sugeridoNaConta é o modelo que o Agent SUGERE para o Polish, se ele consta
// na lista que a conta devolveu; senão, "". O código sugere, nunca exige
// (ADR-016, decisão 10): os modelos baratos saem do ar, e a sugestão pode
// não existir na conta de quem usa.
export function sugeridoNaConta(r: ResultadoTeste | null | undefined): string {
  if (!r?.ok) return ''
  const sugerido = r.modelo_sugerido ?? ''
  return sugerido !== '' && r.modelos.includes(sugerido) ? sugerido : ''
}

// comSugestao põe o modelo sugerido no rascunho, e só nele — salvar
// continua explícito —, quando nenhum modelo foi escolhido ainda para este
// provedor. Quem já escolheu (ou digitou) um modelo fica com o dele, e o
// rascunho de outro provedor, que a pessoa passou a ver enquanto o teste
// ia e voltava, não é tocado.
export function comSugestao(rascunho: Editaveis, provedor: Provedor, sugerido: string): Editaveis {
  if (sugerido === '' || rascunho.ai_provider !== provedor || rascunho.ai_model.trim() !== '') return rascunho
  return { ...rascunho, ai_model: sugerido }
}
