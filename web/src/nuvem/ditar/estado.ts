// O estado do ditado pelo celular na conta (ADR-021, "O aceite e as
// preferências" e "As chaves"): as preferências, o aceite e as chaves (só
// `configurada`, o `final` e o `colar_de_novo`: a chave inteira nunca volta ao
// app). É o que GET /api/v1/dictation devolve, na forma que o servidor fixou na
// C3b (06-api-contract.md, "O aceite e as preferências do ditado no celular").
// O cliente (cliente.ts) lê o JSON do servidor para estes tipos, com tolerância,
// e estas funções puras decidem em cima deles.

import { type Provedor, type ProvedorDeVoz, type ProvedorDoPolish, todosOsProvedores } from './aviso'

// Preferencias são as escolhas do ditado, por conta. Moram só na nuvem e nunca
// descem aos PCs (ADR-021, conflito 7). A forma é a da tela; o servidor as
// guarda em {transcricao, dica, polish} e o cliente traduz nos dois sentidos.
export interface Preferencias {
  // provedor e modelo da transcrição. No PUT, modelo "" é o padrão do provedor
  // (escolhas menores 1 e 2): o servidor o resolve e devolve o modelo de
  // verdade, então o que se lê nunca é "".
  provedor: ProvedorDeVoz
  modelo: string
  // dica: os termos aprendidos vão ao provedor junto com a voz. Desligada.
  dica: boolean
  // polish: o texto passa pelo Polish. Desligado. O servidor só liga o Polish
  // com um modelo (não há modelo padrão do Polish): polish_modelo "" só vale
  // com o Polish desligado.
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

// Aceite é o aceite em aberto da conta: a versão do aviso que ela leu, o
// provedor da transcrição e o do Polish que aceitou (null: o aviso que ela viu
// não incluía o Polish), e quando. Cada aceite novo substitui o anterior.
export interface Aceite {
  versao: number
  transcricao: ProvedorDeVoz
  polish: ProvedorDoPolish | null
  aceito_em: string
  // vigente: é o servidor quem diz. O aceite de uma versão velha do aviso
  // continua gravado, mas não vale: a pessoa lê o texto novo e aceita de novo.
  vigente: boolean
}

// ChaveGuardada é o que o app sabe de uma chave: se existe e vale, o fim dela
// (até 4 caracteres) e se ela precisa ser colada de novo (havia uma, e o
// servidor já não a decifra: a chave de cifra trocou, ou o backup voltou sem
// ela). Nunca mais que isso.
export interface ChaveGuardada {
  configurada: boolean
  final: string
  colar_de_novo: boolean
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

// aceiteVigente é o aceite que vale com o aviso de agora, segundo o servidor.
// O aceite da versão antiga não vale: o texto mudou, e quem dita lê e aceita de
// novo.
export function aceiteVigente(e: Pick<EstadoDoDitado, 'aceite'>): Aceite | null {
  return e.aceite !== null && e.aceite.vigente ? e.aceite : null
}

// aceitoPara diz se o aceite vigente cobre todos os provedores pedidos. O
// aceite cobre o CONJUNTO dos dois que aceitou (a transcrição e o Polish), seja
// qual for o uso: é a conferência do servidor (cloudstore.Aceite.Inclui).
export function aceitoPara(e: Pick<EstadoDoDitado, 'aceite'>, provedores: readonly Provedor[]): boolean {
  const aceite = aceiteVigente(e)
  return aceite !== null && provedores.every((p) => p === aceite.transcricao || p === aceite.polish)
}

// Os modelos que a chave alcança, do jeito que o provedor os lista: o de voz
// é o que tem "transcribe" ou "whisper" no nome (ADR-021, "O motor"); o do
// Polish, o resto (o Polish usa os modelos de texto).
const ehModeloDeVoz = (m: string) => /transcribe|whisper/i.test(m)

export const modelosDeVoz = (lista: readonly string[]): string[] => lista.filter(ehModeloDeVoz)
export const modelosDoPolish = (lista: readonly string[]): string[] => lista.filter((m) => !ehModeloDeVoz(m))

export function chavesVazias(): Record<Provedor, ChaveGuardada> {
  const chaves = {} as Record<Provedor, ChaveGuardada>
  for (const p of todosOsProvedores) chaves[p] = { configurada: false, final: '', colar_de_novo: false }
  return chaves
}
