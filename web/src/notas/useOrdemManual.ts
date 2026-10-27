import { type CSSProperties, type KeyboardEvent, type PointerEvent as EventoDoPonteiro, type RefCallback, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { type Geometria, dispor, ehTeclaDeSeta, indiceDoTeclado, vagaMaisPerto, velocidadeDaRolagem } from './ordem'

// O gesto de reorganizar as notas (ADR-022, "Ordem manual (N2b)"): o hook de UMA
// seção (as fixadas, ou as outras), usado pela grade, pela lista e pelos
// detalhes. Ele faz três coisas:
//
//   1. põe os cartões na grade em ordem de leitura (ordem.ts, dispor): cada um
//      fica numa posição absoluta, medida pela altura que o navegador deu a ele;
//   2. o arrastar: ponteiro do mouse (depois de uns pixels de movimento, para o
//      clique continuar abrindo a nota), do toque (segurar uns 400 ms, para a
//      rolagem continuar rolando) ou da caneta (como o mouse). O cartão segue o
//      ponteiro, os outros abrem a vaga, e soltar chama aoMover;
//   3. o teclado: Alt + seta anda uma vaga (ver indiceDoTeclado).
//
// Só mexe em `transform`, que é o que o navegador anima sem refazer a página, e
// quem pediu menos movimento (prefers-reduced-motion) não vê a transição (CSS).

export type OrigemDoMovimento = 'ponteiro' | 'teclado'

// ConfigDaOrdem é o que a tela decide: se a seção pode ser reorganizada agora, o
// que fazer ao tentar quando não pode, e o que fazer ao soltar. indice é a posição
// nova na seção, contada SEM a própria nota (ver moverNaLista).
export interface ConfigDaOrdem {
  habilitado: boolean
  aoBloquear(): void
  aoMover(id: string, indice: number, origem: OrigemDoMovimento): void
}

interface Opcoes extends ConfigDaOrdem {
  // ids da seção, na ordem de leitura de agora.
  ids: readonly string[]
  colunas: number
  // vao entre os cartões, em px.
  vao: number
  // absoluto: a grade e a lista (cada item fica em posição absoluta). fluxo: a
  // tabela dos detalhes, que fica no fluxo e só desloca as linhas com transform.
  modo: 'absoluto' | 'fluxo'
}

// PropsDoItem vão no elemento de cada nota (o <article>, o <li> ou o <tr>).
export interface PropsDoItem {
  ref: RefCallback<HTMLElement>
  style?: CSSProperties
  onPointerDown(e: EventoDoPonteiro<HTMLElement>): void
  onKeyDown(e: KeyboardEvent<HTMLElement>): void
  'data-ordenavel'?: ''
  'data-arrastando'?: ''
}

// A vaga é onde o cartão cairia: o espaço aberto na grade.
export interface Vaga {
  x: number
  y: number
  largura: number
  altura: number
}

export interface OrdemManual {
  montarContainer: RefCallback<HTMLElement>
  estiloDoContainer: CSSProperties | undefined
  propsDoItem(id: string): PropsDoItem
  vaga: Vaga | null
  arrastando: string | null
  // pronto: a primeira disposição já foi pintada; daqui em diante os cartões
  // animam quando mudam de lugar.
  pronto: boolean
}

// Os tempos e distâncias do gesto.
export const ESPERA_DO_TOQUE_MS = 400
const LIMIAR_DO_MOUSE = 5
const LIMIAR_DO_TOQUE = 10
const ESCALA_DO_CARTAO = 1.03

// O que é clicável por dentro do cartão e não começa um arrasto: os botões das
// ferramentas e dos menus. O que abre a nota ([data-abrir]) é a superfície de
// agarrar.
const INTERATIVOS = 'button, a, input, textarea, select, summary, [role="menu"], [role="menuitem"], [role="dialog"]'

interface Arrasto {
  id: string
  // k é a posição (sem o próprio cartão) em que ele cairia.
  k: number
  // esquerda e topo: onde o cartão está, nas coordenadas do container.
  esquerda: number
  topo: number
  larguraDoCartao: number
  alturaDoCartao: number
  larguraDaColuna: number
}

// Gesto é o que se guarda entre os eventos de um mesmo toque do ponteiro.
interface Gesto {
  id: string
  pointerId: number
  tipo: string
  x0: number
  y0: number
  x: number
  y: number
  ativo: boolean
  bloqueado: boolean
  espera: ReturnType<typeof setTimeout> | null
  quadro: number | null
  desvioX: number
  desvioY: number
  larguraDoCartao: number
  larguraDoContainer: number
  indiceOriginal: number
  rolagem: HTMLElement | null
}

function acharRolagem(do_: HTMLElement | null): HTMLElement | null {
  for (let el = do_?.parentElement ?? null; el; el = el.parentElement) {
    const o = getComputedStyle(el).overflowY
    if ((o === 'auto' || o === 'scroll') && el.scrollHeight > el.clientHeight) return el
  }
  return null
}

function vibrar() {
  try {
    navigator.vibrate?.(10)
  } catch {
    // Sem vibração (a maioria dos computadores): só não vibra.
  }
}

export function useOrdemManual({ ids, colunas, vao, modo, habilitado, aoBloquear, aoMover }: Opcoes): OrdemManual {
  // Os elementos que o hook mede: os callbacks de ref os preenchem; só os eventos e
  // a medida os leem, nunca a pintura.
  const registro = useRef({
    elementos: new Map<string, HTMLElement>(),
    container: null as HTMLElement | null,
    observador: null as ResizeObserver | null,
  })
  const [alturas, setAlturas] = useState<Record<string, number>>({})
  const [arrasto, setArrasto] = useState<Arrasto | null>(null)
  const [pronto, setPronto] = useState(false)

  // O que os eventos (que vivem fora do ciclo do React) precisam ler e guardar: as
  // opções de agora, o gesto em curso, os ouvintes da janela. Só os callbacks e os
  // efeitos mexem aqui, nunca a pintura.
  const vivo = useRef({
    ao: { ids, colunas, vao, modo, habilitado, aoBloquear, aoMover, alturas },
    gesto: null as Gesto | null,
    ouvintes: [] as [string, EventListener][],
    arrasto: null as Arrasto | null,
    rolar: () => {},
  })
  useLayoutEffect(() => {
    vivo.current.ao = { ids, colunas, vao, modo, habilitado, aoBloquear, aoMover, alturas }
  })

  // medir lê a altura que o navegador deu a cada cartão. Só mexe no estado se
  // algo mudou (senão cada medida pediria outra pintura).
  const medir = useCallback(() => {
    const atuais = vivo.current.ao.alturas
    const novas: Record<string, number> = {}
    for (const id of vivo.current.ao.ids) {
      const el = registro.current.elementos.get(id)
      if (el) novas[id] = el.offsetHeight
    }
    const a = Object.keys(novas)
    const mudou = a.length !== Object.keys(atuais).length || a.some((id) => novas[id] !== atuais[id])
    if (mudou) {
      vivo.current.ao.alturas = novas
      setAlturas(novas)
    }
  }, [])
  useLayoutEffect(() => {
    medir()
  })

  // Os cartões mudam de altura sem o React saber: a janela que estreita ou alarga
  // (a largura de cada cartão sai da do container) e a fonte que chega depois. O
  // observador do container e a promessa das fontes pedem a medida de novo.
  useEffect(() => {
    void document.fonts?.ready.then(medir)
    if (typeof ResizeObserver === 'undefined') return
    const o = new ResizeObserver(() => medir())
    const r = registro.current
    r.observador = o
    if (r.container) o.observe(r.container)
    return () => {
      o.disconnect()
      r.observador = null
    }
  }, [medir])

  // As transições só ligam depois da primeira disposição: sem isso, os cartões
  // "voariam" do canto na primeira pintura.
  useEffect(() => {
    const t = requestAnimationFrame(() => setPronto(true))
    return () => cancelAnimationFrame(t)
  }, [])

  const refDoItem =
    (id: string): RefCallback<HTMLElement> =>
    (el) => {
      if (el) registro.current.elementos.set(id, el)
      else registro.current.elementos.delete(id)
    }
  const montarContainer = useCallback<RefCallback<HTMLElement>>((el) => {
    if (registro.current.container && registro.current.container !== el) registro.current.observador?.unobserve(registro.current.container)
    registro.current.container = el
    if (el) registro.current.observador?.observe(el)
  }, [])

  // ---- o gesto -----------------------------------------------------------

  const geometria = useCallback((g: Gesto): Geometria => {
    const { colunas: n, vao: v, modo: m } = vivo.current.ao
    if (m === 'fluxo') return { colunas: 1, larguraDaColuna: g.larguraDoContainer, vao: 0 }
    return { colunas: n, larguraDaColuna: Math.max(0, (g.larguraDoContainer - (n - 1) * v) / n), vao: v }
  }, [])

  // atualizar refaz a posição do cartão arrastado e a vaga para onde o ponteiro
  // (g.x, g.y, em coordenadas da janela) está agora.
  const atualizar = useCallback(() => {
    const g = vivo.current.gesto
    const caixa = registro.current.container
    if (!g || !g.ativo || !caixa) return
    const r = caixa.getBoundingClientRect()
    const ant = vivo.current.arrasto
    const { ids: todos, alturas: medidas, modo: m } = vivo.current.ao
    const h = medidas[g.id] ?? ant?.alturaDoCartao ?? 0
    const esquerda = g.x - r.left - g.desvioX
    const topo = g.y - r.top - g.desvioY
    const geo = geometria(g)
    const restantes = todos.filter((x) => x !== g.id).map((x) => medidas[x] ?? 0)
    // Na tabela só se anda na vertical: o centro fica na coluna única.
    const cx = m === 'fluxo' ? geo.larguraDaColuna / 2 : esquerda + g.larguraDoCartao / 2
    const k = vagaMaisPerto(restantes, h, geo, { x: cx, y: topo + (h > 0 ? h : 1) / 2 })
    const novo: Arrasto = {
      id: g.id,
      k,
      esquerda,
      topo,
      larguraDoCartao: g.larguraDoCartao,
      alturaDoCartao: h,
      larguraDaColuna: geo.larguraDaColuna,
    }
    vivo.current.arrasto = novo
    setArrasto(novo)
  }, [geometria])

  // Encerrar o gesto: tira os ouvintes, o temporizador e o quadro de rolagem.
  const terminar = useCallback(() => {
    const g = vivo.current.gesto
    if (g?.espera) clearTimeout(g.espera)
    if (g?.quadro != null) cancelAnimationFrame(g.quadro)
    vivo.current.gesto = null
    vivo.current.arrasto = null
    setArrasto(null)
    for (const [tipo, f] of vivo.current.ouvintes) window.removeEventListener(tipo, f, true)
    vivo.current.ouvintes = []
  }, [])

  // O clique que vem logo depois de soltar um arrasto não pode abrir a nota.
  const engolirOCliqueSeguinte = useCallback(() => {
    const engolir = (e: Event) => {
      e.stopPropagation()
      e.preventDefault()
    }
    window.addEventListener('click', engolir, { capture: true, once: true })
    // Se o clique não vier (soltou fora do cartão, ou o toque foi cancelado), o
    // ouvinte sai sozinho, para não engolir o próximo clique de verdade.
    setTimeout(() => window.removeEventListener('click', engolir, true), 0)
  }, [])

  const cancelar = useCallback(() => {
    const g = vivo.current.gesto
    if (g?.ativo) engolirOCliqueSeguinte()
    terminar()
  }, [engolirOCliqueSeguinte, terminar])

  const soltar = useCallback(() => {
    const g = vivo.current.gesto
    const a = vivo.current.arrasto
    if (g?.ativo && a) {
      engolirOCliqueSeguinte()
      const { id, indiceOriginal } = g
      const k = a.k
      terminar()
      // O cartão que sumiu no meio do arrasto (outra aba o apagou, a lista foi lida
      // de novo) não é movido: a tela já o largou (arrastoValido).
      if (k !== indiceOriginal && vivo.current.ao.ids.includes(id)) vivo.current.ao.aoMover(id, k, 'ponteiro')
      return
    }
    terminar()
  }, [engolirOCliqueSeguinte, terminar])

  // rolar anda a janela (ou a área rolável) enquanto o ponteiro está perto da
  // borda, e refaz a posição do cartão: a grade andou por baixo do ponteiro.
  const rolar = useCallback(() => {
    const g = vivo.current.gesto
    if (!g?.ativo) return
    const alvo = g.rolagem
    const topo = alvo ? alvo.getBoundingClientRect().top : 0
    const base = alvo ? alvo.getBoundingClientRect().bottom : window.innerHeight
    const v = velocidadeDaRolagem(g.y, Math.max(0, topo), Math.min(window.innerHeight, base))
    if (v !== 0) {
      if (alvo) alvo.scrollTop += v
      else window.scrollBy(0, v)
      atualizar()
    }
    g.quadro = requestAnimationFrame(() => vivo.current.rolar())
  }, [atualizar])
  useLayoutEffect(() => {
    vivo.current.rolar = rolar
  }, [rolar])

  const ativar = useCallback(() => {
    const g = vivo.current.gesto
    const caixa = registro.current.container
    const el = g && registro.current.elementos.get(g.id)
    if (!g || g.ativo || !caixa || !el) return
    if (g.espera) clearTimeout(g.espera)
    g.espera = null
    if (!vivo.current.ao.habilitado) return
    const rc = caixa.getBoundingClientRect()
    const re = el.getBoundingClientRect()
    g.ativo = true
    g.desvioX = g.x0 - re.left
    g.desvioY = g.y0 - re.top
    g.larguraDoCartao = re.width
    g.larguraDoContainer = rc.width
    g.indiceOriginal = vivo.current.ao.ids.indexOf(g.id)
    g.rolagem = acharRolagem(caixa)
    window.getSelection()?.removeAllRanges()
    if (g.tipo === 'touch') vibrar()
    atualizar()
    g.quadro = requestAnimationFrame(() => vivo.current.rolar())
  }, [atualizar])

  const aoMoverOPonteiro = useCallback(
    (e: Event) => {
      const g = vivo.current.gesto
      const p = e as PointerEvent
      if (!g || p.pointerId !== g.pointerId) return
      g.x = p.clientX
      g.y = p.clientY
      if (g.ativo) {
        atualizar()
        return
      }
      const andou = Math.hypot(g.x - g.x0, g.y - g.y0)
      if (g.tipo === 'touch') {
        // Mexeu antes de segurar o bastante: é a rolagem. Larga o gesto.
        if (andou > LIMIAR_DO_TOQUE) terminar()
        return
      }
      if (andou > LIMIAR_DO_MOUSE) {
        if (g.bloqueado) {
          vivo.current.ao.aoBloquear()
          terminar()
        } else {
          ativar()
        }
      }
    },
    [ativar, atualizar, terminar],
  )

  const aoLevantarOPonteiro = useCallback(
    (e: Event) => {
      const g = vivo.current.gesto
      if (!g || (e as PointerEvent).pointerId !== g.pointerId) return
      soltar()
    },
    [soltar],
  )

  const aoCancelarOPonteiro = useCallback(
    (e: Event) => {
      const g = vivo.current.gesto
      if (!g || (e as PointerEvent).pointerId !== g.pointerId) return
      cancelar()
    },
    [cancelar],
  )

  const aoApertarUmaTecla = useCallback(
    (e: Event) => {
      const t = e as globalThis.KeyboardEvent
      if (t.key === 'Escape' && vivo.current.gesto?.ativo) {
        t.preventDefault()
        t.stopPropagation()
        cancelar()
      }
    },
    [cancelar],
  )

  // O menu de contexto que o toque longo abre no celular atrapalha o arrasto.
  const semMenuDeContexto = useCallback((e: Event) => {
    if (vivo.current.gesto?.tipo === 'touch') e.preventDefault()
  }, [])

  const comecar = useCallback(
    (e: EventoDoPonteiro<HTMLElement>, id: string) => {
      if (vivo.current.gesto || !e.isPrimary) return
      if (e.pointerType === 'mouse' && e.button !== 0) return
      const alvo = e.target as HTMLElement
      const interativo = alvo.closest(INTERATIVOS)
      // Os botões do cartão são botões; o que abre a nota (data-abrir), mesmo sendo
      // um <button> na tabela, é a superfície de agarrar.
      if (interativo && !interativo.hasAttribute('data-abrir')) return
      const toque = e.pointerType === 'touch'
      const g: Gesto = {
        id,
        pointerId: e.pointerId,
        tipo: e.pointerType || 'mouse',
        x0: e.clientX,
        y0: e.clientY,
        x: e.clientX,
        y: e.clientY,
        ativo: false,
        bloqueado: !vivo.current.ao.habilitado,
        espera: null,
        quadro: null,
        desvioX: 0,
        desvioY: 0,
        larguraDoCartao: 0,
        larguraDoContainer: 0,
        indiceOriginal: -1,
        rolagem: null,
      }
      // Sem poder reorganizar, o toque não faz nada de especial (o toque longo é
      // do navegador); o mouse avisa por que não arrasta.
      if (g.bloqueado && toque) return
      vivo.current.gesto = g
      vivo.current.ouvintes = [
        ['pointermove', aoMoverOPonteiro],
        ['pointerup', aoLevantarOPonteiro],
        ['pointercancel', aoCancelarOPonteiro],
        ['keydown', aoApertarUmaTecla],
        ['contextmenu', semMenuDeContexto],
      ]
      for (const [tipo, f] of vivo.current.ouvintes) window.addEventListener(tipo, f, true)
      if (toque) g.espera = setTimeout(ativar, ESPERA_DO_TOQUE_MS)
    },
    [aoApertarUmaTecla, aoCancelarOPonteiro, aoLevantarOPonteiro, aoMoverOPonteiro, ativar, semMenuDeContexto],
  )

  // Durante o arrasto de toque a rolagem da página não pode andar junto: um
  // ouvinte de toque que não é passivo a impede. O ouvinte nasce com a tela (e
  // não com o gesto), porque o iOS só respeita o que já estava lá no começo do toque.
  useEffect(() => {
    const impedir = (e: TouchEvent) => {
      if (vivo.current.gesto?.ativo && e.cancelable) e.preventDefault()
    }
    document.addEventListener('touchmove', impedir, { passive: false })
    return () => document.removeEventListener('touchmove', impedir)
  }, [])

  // Sair da tela no meio do gesto solta tudo.
  useEffect(() => terminar, [terminar])

  const aoApertarNoItem = useCallback(
    (e: KeyboardEvent<HTMLElement>, id: string) => {
      if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || !ehTeclaDeSeta(e.key)) return
      // Só do cartão em si, nunca de um campo ou menu aberto dentro dele.
      const alvo = e.target as HTMLElement
      const interativo = alvo.closest(INTERATIVOS)
      if (interativo && !interativo.hasAttribute('data-abrir')) return
      const { ids: todos, colunas: n, habilitado: pode, modo: m } = vivo.current.ao
      const i = todos.indexOf(id)
      const novo = indiceDoTeclado(e.key, i, todos.length, m === 'fluxo' ? 1 : n)
      if (novo === null || i < 0) return
      // Alt + ← e Alt + → são "voltar" e "avançar" do navegador.
      e.preventDefault()
      if (!pode) {
        vivo.current.ao.aoBloquear()
        return
      }
      vivo.current.ao.aoMover(id, novo, 'teclado')
    },
    [],
  )

  // ---- o que a tela desenha ---------------------------------------------

  const medidas = ids.map((id) => alturas[id] ?? 0)
  const medido = ids.length > 0 && ids.every((id) => alturas[id] !== undefined)
  const arrastoValido = arrasto && ids.includes(arrasto.id) ? arrasto : null
  const restantes = arrastoValido ? ids.filter((id) => id !== arrastoValido.id) : ids
  const medidasRestantes = restantes.map((id) => alturas[id] ?? 0)
  // A disposição que se vê: sem arrasto, a dos cartões na ordem; com ele, a dos
  // outros com a vaga aberta em k.
  const vagaK = arrastoValido?.k ?? -1
  const alturaDoArrastado = arrastoValido ? (alturas[arrastoValido.id] ?? 0) : 0
  const comVaga = arrastoValido
    ? [...medidasRestantes.slice(0, vagaK), alturaDoArrastado, ...medidasRestantes.slice(vagaK)]
    : medidas
  const disposicao = dispor(comVaga, modo === 'fluxo' ? 1 : colunas, modo === 'fluxo' ? 0 : vao)
  const original = modo === 'fluxo' && arrastoValido ? dispor(medidas, 1, 0) : null

  const indiceNaDisposicao = (id: string) => {
    if (!arrastoValido) return ids.indexOf(id)
    const j = restantes.indexOf(id)
    return j < vagaK ? j : j + 1
  }

  function estiloDoItem(id: string): CSSProperties | undefined {
    if (arrastoValido && id === arrastoValido.id) {
      if (modo === 'fluxo') {
        const o = original!.y[ids.indexOf(id)] ?? 0
        return { transform: `translate3d(0, ${arrastoValido.topo - o}px, 0) scale(1.01)` }
      }
      return {
        width: `calc((100% - ${(colunas - 1) * vao}px) / ${colunas})`,
        transform: `translate3d(${arrastoValido.esquerda}px, ${arrastoValido.topo}px, 0) scale(${ESCALA_DO_CARTAO})`,
      }
    }
    const i = indiceNaDisposicao(id)
    if (modo === 'fluxo') {
      if (!original) return undefined
      const dy = (disposicao.y[i] ?? 0) - (original.y[ids.indexOf(id)] ?? 0)
      return dy === 0 ? undefined : { transform: `translate3d(0, ${dy}px, 0)` }
    }
    const c = disposicao.coluna[i] ?? 0
    const y = disposicao.y[i] ?? 0
    return {
      width: `calc((100% - ${(colunas - 1) * vao}px) / ${colunas})`,
      transform: `translate3d(calc(${c} * (100% + ${vao}px)), ${y}px, 0)`,
    }
  }

  const propsDoItem = (id: string): PropsDoItem => ({
    ref: refDoItem(id),
    style: estiloDoItem(id),
    onPointerDown: (e) => comecar(e, id),
    onKeyDown: (e) => aoApertarNoItem(e, id),
    ...(habilitado ? { 'data-ordenavel': '' as const } : {}),
    ...(arrastoValido?.id === id ? { 'data-arrastando': '' as const } : {}),
  })

  let vaga: Vaga | null = null
  if (arrastoValido && modo === 'absoluto') {
    vaga = {
      x: (disposicao.coluna[vagaK] ?? 0) * (arrastoValido.larguraDaColuna + vao),
      y: disposicao.y[vagaK] ?? 0,
      largura: arrastoValido.larguraDaColuna,
      altura: alturaDoArrastado,
    }
  }

  const estiloDoContainer: CSSProperties | undefined =
    modo === 'absoluto' && medido ? { height: disposicao.altura } : undefined

  return { montarContainer, estiloDoContainer, propsDoItem, vaga, arrastando: arrastoValido?.id ?? null, pronto }
}
