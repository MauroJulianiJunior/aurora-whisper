// O estado do ditado pelo celular na conta (ADR-021, "O aceite e as
// preferências" e "As chaves"): as preferências, o aceite vigente e as chaves
// (só `configurada` e o `final`: a chave inteira nunca volta ao app). É o que
// GET /api/v1/dictation devolve. A forma exata do JSON é a que o servidor da
// C3b deve entregar; o app a lê com tolerância (doServidor, em cliente.ts) e
// estas funções puras decidem em cima dela.

import { type Provedor, type ProvedorDeVoz, type ProvedorDoPolish, todosOsProvedores, versaoDoAviso } from './aviso'

// Preferencias são as escolhas do ditado, por conta. Moram só na nuvem e nunca
// descem aos PCs (ADR-021, conflito 7).
export interface Preferencias {
  // provedor e modelo da transcrição. modelo "" é o padrão do provedor (as
  // escolhas menores 1 e 2): o servidor resolve.
  provedor: ProvedorDeVoz
  modelo: string
  // dica: os termos aprendidos vão ao provedor junto com a voz. Desligada.
  dica: boolean
  // polish: o texto passa pelo Polish. Desligado.
  polish: boolean
  polish_provedor: ProvedorDoPolish
  polish_modelo: string
}

export const preferenciasPadrao: Preferencias = {
  provedor: 'openai',
  modelo: '',
  dica: false,
  polish: false,
  polish_provedor: 'openai',
  polish_modelo: '',
}

// Aceite é o aceite vigente da conta: a versão do aviso que ela leu, os
// provedores que aceitou e quando.
export interface Aceite {
  versao: string
  provedores: Provedor[]
  aceito_em: string
}

// ChaveGuardada é o que o app sabe de uma chave: se existe e o fim dela (até 4
// caracteres). Nunca mais que isso.
export interface ChaveGuardada {
  configurada: boolean
  final: string
}

export interface EstadoDoDitado {
  preferencias: Preferencias
  aceite: Aceite | null
  chaves: Record<Provedor, ChaveGuardada>
}

// provedoresExigidos são os provedores que as preferências usam: o da voz e,
// com o Polish ligado, o do Polish. Cada um precisa estar no aceite.
export function provedoresExigidos(p: Preferencias): Provedor[] {
  const lista: Provedor[] = [p.provedor]
  if (p.polish && !lista.includes(p.polish_provedor)) lista.push(p.polish_provedor)
  return lista
}

// aceiteVigente é o aceite que vale com o aviso de agora. O aceite da versão
// antiga não vale: o texto mudou, e quem dita lê e aceita de novo.
export function aceiteVigente(e: Pick<EstadoDoDitado, 'aceite'>): Aceite | null {
  return e.aceite !== null && e.aceite.versao === versaoDoAviso ? e.aceite : null
}

// aceitoPara diz se o aceite vigente cobre todos os provedores pedidos.
export function aceitoPara(e: Pick<EstadoDoDitado, 'aceite'>, provedores: readonly Provedor[]): boolean {
  const aceite = aceiteVigente(e)
  return aceite !== null && provedores.every((p) => aceite.provedores.includes(p))
}

// Os modelos que a chave alcança, do jeito que o provedor os lista: o de voz
// é o que tem "transcribe" ou "whisper" no nome (ADR-021, "O motor"); o do
// Polish, o resto (o Polish usa os modelos de texto).
const ehModeloDeVoz = (m: string) => /transcribe|whisper/i.test(m)

export const modelosDeVoz = (lista: readonly string[]): string[] => lista.filter(ehModeloDeVoz)
export const modelosDoPolish = (lista: readonly string[]): string[] => lista.filter((m) => !ehModeloDeVoz(m))

export function chavesVazias(): Record<Provedor, ChaveGuardada> {
  const chaves = {} as Record<Provedor, ChaveGuardada>
  for (const p of todosOsProvedores) chaves[p] = { configurada: false, final: '' }
  return chaves
}
