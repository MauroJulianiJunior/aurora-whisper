// O que as notas guardam no celular (ADR-022, N8; E4, "Marcar sem rede"): a
// última lista que a nuvem deu, os marcadores e a fila das mudanças feitas sem
// rede. É o que deixa a lista abrir no mercado sem sinal e o marcar de lá
// sobreviver a fechar o app e a desligar o celular.
//
// Um banco IndexedDB por conta ("aurora-notas-<id da conta>"), apagado no Sair e
// no "Apagar meus dados" (ConfiguracoesNuvem.tsx). Só notas e marcadores: o
// ditado e o áudio nunca vêm para cá (o gravador os segura só na memória;
// bundle.test.ts confere que só este arquivo fala com o IndexedDB). E um banco à
// parte, "aurora-notas", com a última conta que abriu o app, para ele abrir sem
// rede na conta certa.
//
// Sem IndexedDB (uma janela anônima que o recusa), tudo vive só na memória da
// página: o app funciona com rede, e sem rede não tem o que mostrar.

import type { Marcador, Nota } from '../../notas/tipos'
import type { Eu } from '../cliente'

// MudancaEsperando é uma nota mudada sem rede, que ainda não subiu: uma por nota.
// A base é a versão (e o conteúdo dela) que a nuvem confirmou e de onde a
// mudança partiu; titulo e corpo são o que está no celular agora. Subir é o
// PATCH com a base e o corpo_base: a nuvem junta (a mescla da lista de marcar da
// N7) ou guarda no selo, e nada se perde calado.
export interface MudancaEsperando {
  notaId: string
  baseVersion: number
  tituloBase: string
  corpoBase: string
  titulo: string
  corpo: string
  // enviado: um envio saiu e a resposta não chegou (a rede caiu no meio). Antes
  // do próximo, a nota é lida de novo: se ela já tem o que foi enviado, a base
  // passa a ser ela.
  enviado?: { titulo: string; corpo: string }
  // criadaEm: quando a primeira mudança desta nota entrou na fila (a ordem de
  // subir).
  criadaEm: string
}

export interface Guardado {
  notas: Nota[]
  marcadores: Marcador[]
  fila: MudancaEsperando[]
}

export interface Armazem {
  carregar(): Promise<Guardado>
  gravarNota(n: Nota): Promise<void>
  apagarNota(id: string): Promise<void>
  gravarMarcadores(m: Marcador[]): Promise<void>
  gravarNaFila(m: MudancaEsperando): Promise<void>
  tirarDaFila(notaId: string): Promise<void>
  // esquecer apaga tudo o que esta conta guardou no aparelho.
  esquecer(): Promise<void>
}

// O que vai para o banco: a nota sem o que só vale na tela (o "esperando a rede"
// e a gravação de uma resposta).
function paraGuardar(n: Nota): Nota {
  const g = { ...n }
  delete g.esperando
  delete g.gravacao
  return g
}

// armazemNaMemoria guarda só na memória da página: os testes, e o navegador sem
// IndexedDB.
export function armazemNaMemoria(inicial: Partial<Guardado> = {}): Armazem & { foto(): Guardado } {
  const notas = new Map((inicial.notas ?? []).map((n) => [n.id, paraGuardar(n)]))
  let marcadores = [...(inicial.marcadores ?? [])]
  const fila = new Map((inicial.fila ?? []).map((m) => [m.notaId, { ...m }]))
  const foto = (): Guardado => ({ notas: [...notas.values()].map((n) => ({ ...n })), marcadores: [...marcadores], fila: [...fila.values()].map((m) => ({ ...m })) })
  return {
    foto,
    carregar: async () => foto(),
    gravarNota: async (n) => void notas.set(n.id, paraGuardar(n)),
    apagarNota: async (id) => void notas.delete(id),
    gravarMarcadores: async (m) => {
      marcadores = [...m]
    },
    gravarNaFila: async (m) => void fila.set(m.notaId, { ...m }),
    tirarDaFila: async (id) => void fila.delete(id),
    esquecer: async () => {
      notas.clear()
      marcadores = []
      fila.clear()
    },
  }
}

// --- O IndexedDB ---

const VERSAO_DO_BANCO = 1
const NOTAS = 'notas'
const MARCADORES = 'marcadores'
const FILA = 'fila'

const nomeDoBanco = (conta: string) => `aurora-notas-${conta}`

function pedidoEmPromessa<T>(p: IDBRequest<T>): Promise<T> {
  return new Promise((ok, falha) => {
    p.onsuccess = () => ok(p.result)
    p.onerror = () => falha(p.error)
  })
}

function transacaoEmPromessa(tx: IDBTransaction): Promise<void> {
  return new Promise((ok, falha) => {
    tx.oncomplete = () => ok()
    tx.onerror = () => falha(tx.error)
    tx.onabort = () => falha(tx.error)
  })
}

function abrir(fabrica: IDBFactory, nome: string, criar: (db: IDBDatabase) => void): Promise<IDBDatabase> {
  return new Promise((ok, falha) => {
    const p = fabrica.open(nome, VERSAO_DO_BANCO)
    p.onupgradeneeded = () => criar(p.result)
    p.onsuccess = () => ok(p.result)
    p.onerror = () => falha(p.error)
    p.onblocked = () => falha(new Error('o banco das notas está aberto em outra aba com outra versão'))
  })
}

// fabricaDoNavegador é o IndexedDB da página, ou null quando não há.
function fabricaDoNavegador(): IDBFactory | null {
  try {
    return typeof indexedDB === 'undefined' ? null : indexedDB
  } catch {
    return null
  }
}

// armazemNoNavegador é o banco da conta no IndexedDB. Se ele não abre, cai para
// a memória da página (o app funciona; só não guarda para depois).
export function armazemNoNavegador(conta: string, fabrica: IDBFactory | null = fabricaDoNavegador()): Armazem {
  if (!fabrica) return armazemNaMemoria()
  const nome = nomeDoBanco(conta)
  let banco: Promise<IDBDatabase> | null = null
  const db = () => {
    banco ??= abrir(fabrica, nome, (d) => {
      if (!d.objectStoreNames.contains(NOTAS)) d.createObjectStore(NOTAS, { keyPath: 'id' })
      if (!d.objectStoreNames.contains(MARCADORES)) d.createObjectStore(MARCADORES, { keyPath: 'id' })
      if (!d.objectStoreNames.contains(FILA)) d.createObjectStore(FILA, { keyPath: 'notaId' })
    })
    return banco
  }
  const escrever = async (lojas: string[], f: (tx: IDBTransaction) => void) => {
    const d = await db()
    const tx = d.transaction(lojas, 'readwrite')
    f(tx)
    await transacaoEmPromessa(tx)
  }
  const memoria = armazemNaMemoria()
  let semBanco = false
  // Cada operação tenta o banco; se ele não abre, passa a usar a memória.
  const ou = async <T,>(f: () => Promise<T>, g: () => Promise<T>): Promise<T> => {
    if (semBanco) return g()
    try {
      return await f()
    } catch {
      semBanco = true
      return g()
    }
  }
  return {
    carregar: () =>
      ou(
        async () => {
          const d = await db()
          const tx = d.transaction([NOTAS, MARCADORES, FILA], 'readonly')
          const [notas, marcadores, fila] = await Promise.all([
            pedidoEmPromessa(tx.objectStore(NOTAS).getAll() as IDBRequest<Nota[]>),
            pedidoEmPromessa(tx.objectStore(MARCADORES).getAll() as IDBRequest<Marcador[]>),
            pedidoEmPromessa(tx.objectStore(FILA).getAll() as IDBRequest<MudancaEsperando[]>),
          ])
          return { notas, marcadores, fila }
        },
        () => memoria.carregar(),
      ),
    gravarNota: (n) =>
      ou(
        () => escrever([NOTAS], (tx) => tx.objectStore(NOTAS).put(paraGuardar(n))),
        () => memoria.gravarNota(n),
      ),
    apagarNota: (id) =>
      ou(
        () => escrever([NOTAS], (tx) => tx.objectStore(NOTAS).delete(id)),
        () => memoria.apagarNota(id),
      ),
    gravarMarcadores: (ms) =>
      ou(
        () =>
          escrever([MARCADORES], (tx) => {
            const loja = tx.objectStore(MARCADORES)
            loja.clear()
            for (const m of ms) loja.put(m)
          }),
        () => memoria.gravarMarcadores(ms),
      ),
    gravarNaFila: (m) =>
      ou(
        () => escrever([FILA], (tx) => tx.objectStore(FILA).put(m)),
        () => memoria.gravarNaFila(m),
      ),
    tirarDaFila: (id) =>
      ou(
        () => escrever([FILA], (tx) => tx.objectStore(FILA).delete(id)),
        () => memoria.tirarDaFila(id),
      ),
    esquecer: async () => {
      await memoria.esquecer()
      try {
        if (banco) (await banco).close()
      } catch {
        // o banco nem abriu
      }
      banco = null
      try {
        await pedidoEmPromessa(fabrica.deleteDatabase(nome))
      } catch {
        // sem o banco, não há o que apagar
      }
    },
  }
}

// --- A última conta, para abrir sem rede ---

const BANCO_DA_CONTA = 'aurora-notas'
const CONTA = 'conta'
const ULTIMA = 'ultima'

function bancoDaConta(fabrica: IDBFactory) {
  return abrir(fabrica, BANCO_DA_CONTA, (d) => {
    if (!d.objectStoreNames.contains(CONTA)) d.createObjectStore(CONTA)
  })
}

// guardarConta anota a conta que abriu o app (só o id, o e-mail e o nome): é o
// que o app usa para abrir sem rede (AppNuvem.tsx). Melhor esforço: sem
// IndexedDB, o app só não abre sem rede.
export async function guardarConta(eu: Eu, fabrica: IDBFactory | null = fabricaDoNavegador()): Promise<void> {
  if (!fabrica) return
  try {
    const d = await bancoDaConta(fabrica)
    const tx = d.transaction(CONTA, 'readwrite')
    tx.objectStore(CONTA).put({ id: eu.id, email: eu.email, display_name: eu.display_name }, ULTIMA)
    await transacaoEmPromessa(tx)
    d.close()
  } catch {
    // melhor esforço
  }
}

// lerConta é a última conta que abriu o app, ou null.
export async function lerConta(fabrica: IDBFactory | null = fabricaDoNavegador()): Promise<Eu | null> {
  if (!fabrica) return null
  try {
    const d = await bancoDaConta(fabrica)
    const tx = d.transaction(CONTA, 'readonly')
    const eu = await pedidoEmPromessa(tx.objectStore(CONTA).get(ULTIMA) as IDBRequest<Eu | undefined>)
    d.close()
    return eu && typeof eu.id === 'string' && typeof eu.email === 'string' ? eu : null
  } catch {
    return null
  }
}

// esquecerConta tira a última conta (o Sair e o "Apagar meus dados").
export async function esquecerConta(fabrica: IDBFactory | null = fabricaDoNavegador()): Promise<void> {
  if (!fabrica) return
  try {
    const d = await bancoDaConta(fabrica)
    const tx = d.transaction(CONTA, 'readwrite')
    tx.objectStore(CONTA).delete(ULTIMA)
    await transacaoEmPromessa(tx)
    d.close()
  } catch {
    // melhor esforço
  }
}
