import { type ReactNode, useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Conflito, ErroDaApi, problemaDe } from '../api'
import { Aurora } from '../Aurora'
import { type AcoesDaNota, CartaoDaNota, LinhaDaNota, TabelaDeNotas } from './CartaoDaNota'
import { GradeDeNotas, ListaDeNotas } from './ColecaoDeNotas'
import { copiarParaWhatsApp } from './copiarParaWhatsApp'
import { CriarNota } from './CriarNota'
import { DialogoDaNota, type FimDaNota } from './DialogoDaNota'
import { EditarMarcadores } from './EditarMarcadores'
import { Icone } from './Icone'
import { acharPorNome, limparNome, ordenarPorNome } from './marcadores'
import { NotaNaLixeira } from './NotaNaLixeira'
import { aplicarOrdem, fraseNaPonta, frasePosicao, moverNaLista, vizinhasDe } from './ordem'
import { type FormaDeVer, FORMAS, gravarColunas, gravarForma, lerColunas, lerForma, limitar, limitesDeColunas } from './preferencias'
import {
  type ChegadaNaNota,
  type ClienteDeNotas,
  type CorDaNota,
  type EstadoDaRede,
  type FiltroDasNotas,
  type Marcador,
  type Nota,
  type NotasNoCelular,
  SoComRede,
  type VistaDasNotas,
} from './tipos'
import { type OrigemDoMovimento, type ConfigDaOrdem } from './useOrdemManual'
import { useTelaEstreita } from './useTelaEstreita'

// Tempo mínimo entre duas leituras causadas por voltar à aba: "focus" e
// "visibilitychange" chegam juntos (o mesmo cuidado do histórico).
const intervaloDaRecarga = 2000

interface Props {
  cliente: ClienteDeNotas
  // busca: o texto do campo de busca do Painel (já com a espera).
  busca: string
  aoPerderLigacao(e: unknown): void
  // avisar: a pílula do Painel; desfazer, quando existe, vira o botão e o Ctrl+Z;
  // abrir, o botão "Abrir" (o aviso do "Ditar para nota" no celular).
  avisar(texto: ReactNode, desfazer?: () => void, abrir?: () => void): void
  // abrirNota: o "Abrir" do aviso "Virou nota" (N3): a nota que a janela abre
  // assim que a aba aparece. vez sobe a cada pedido.
  abrirNota?: { id: string; vez: number } | null
  // oculta e aoTerminarNota: a busca única (N6) está à vista; ver PropsDasNotas.
  oculta?: boolean
  aoTerminarNota?(): void
  // mudancasDoSync: sobe quando o sync do PC trouxe algo da nuvem (a nota
  // feita no celular); a lista é lida de novo. A janela aberta não muda.
  mudancasDoSync?: number
  // celular: o que só o app do celular tem (N8): a rede e a fila das mudanças
  // feitas sem rede, o "Ditar para nota" embaixo e o microfone dentro da nota.
  // O PC não passa nada.
  celular?: NotasNoCelular
  // abrirConfiguracoes leva às Configurações do ditado (a chave que falta para
  // o "Ditar para nota" no celular).
  abrirConfiguracoes?(): void
}

// Sem o celular, a rede é sempre a de quem tem rede, e nada espera.
const redeDoPC: EstadoDaRede = { semRede: false, esperando: new Set() }
const semAssinar = () => () => {}
const lerRedeDoPC = () => redeDoPC

// tituloCurto é o título como o aviso do "Ditar para nota" o mostra: numa
// linha, cortado com "…" antes de 40 letras, para o "+N" nunca sumir (a pílula
// do PC faz o mesmo, internal/notas.TituloCurto).
function tituloCurto(titulo: string): string {
  const t = titulo.split(/\s+/).filter(Boolean).join(' ')
  const letras = Array.from(t)
  return letras.length > 40 ? `${letras.slice(0, 39).join('').trimEnd()}…` : t
}

// O que a tela mostra: a lista da consulta, e a lista das notas ativas inteira
// (os números dos marcadores) com os marcadores.
interface Resultado {
  // consulta: vista, marcador e busca (a recarga da mesma consulta não é
  // "carregando": a lista que está na tela fica até a nova chegar).
  consulta: string
  itens: Nota[]
  base: Nota[]
  marcadores: Marcador[]
  erro?: string
}

// Sem a primeira leitura, a lista de marcadores é esta (a mesma de sempre, para o que
// depende dela não recalcular).
const semMarcadores: Marcador[] = []

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

const nomesDasFormas: Record<FormaDeVer, string> = { grade: 'Quadradinhos', lista: 'Lista', detalhes: 'Detalhes' }

// ordenar põe as fixadas primeiro; a ordem de cada grupo é a da API (a ordem
// manual, ADR-022 N2b: a nota nova e a que se fixa entram no topo, editar não move).
const ordenar = (itens: Nota[]) => [...itens.filter((n) => n.fixada), ...itens.filter((n) => !n.fixada)]

function vizinhoDe(ordenados: Nota[], id: string): string | null {
  const i = ordenados.findIndex((n) => n.id === id)
  if (i < 0) return null
  return (ordenados[i + 1] ?? ordenados[i - 1])?.id ?? null
}

// Notas é a aba Notas do painel local (ADR-022, "A tela"): o campo de criar, a
// fileira de marcadores com as formas de ver, a grade (ou a lista, ou os
// detalhes), a nota aberta numa janela, o Arquivo e a Lixeira. Só o PC o tem
// (ambienteLocal.tsx): o painel da nuvem não importa este arquivo.
export function Notas({
  cliente,
  busca,
  aoPerderLigacao: aoPerderLigacaoDoPainel,
  avisar,
  abrirNota,
  oculta,
  aoTerminarNota,
  mudancasDoSync,
  celular,
  abrirConfiguracoes,
}: Props) {
  // O Painel passa uma função nova a cada render: a daqui é estável, e lê a
  // do momento (senão cada render do Painel leria a lista de novo).
  const ligacao = useRef(aoPerderLigacaoDoPainel)
  useEffect(() => {
    ligacao.current = aoPerderLigacaoDoPainel
  })
  const aoPerderLigacao = useCallback((e: unknown) => ligacao.current(e), [])
  const [vista, setVista] = useState<VistaDasNotas>('ativas')
  const [marcador, setMarcador] = useState<string | null>(null)
  const [resultado, setResultado] = useState<Resultado | null>(null)
  const [recarga, setRecarga] = useState(0)
  const [aberta, setAberta] = useState<Nota | null>(null)
  // editando: a janela "Editar marcadores" está aberta; o chip dela ganha o foco de volta.
  const [editando, setEditando] = useState(false)
  const chipEditar = useRef<HTMLButtonElement>(null)
  const eraEditando = useRef(false)
  const [forma, setForma] = useState<FormaDeVer>(lerForma)
  const estreita = useTelaEstreita()
  const [colunasNoPc, setColunasNoPc] = useState(() => lerColunas(false))
  const [colunasNoCelular, setColunasNoCelular] = useState(() => lerColunas(true))
  const colunas = estreita ? colunasNoCelular : colunasNoPc
  const limites = limitesDeColunas(estreita)
  const raiz = useRef<HTMLDivElement>(null)
  const ultimaCarga = useRef(0)
  // focarDepois: a nota que recebe o foco quando a janela fecha ou um cartão
  // sai da tela (null: não há nota, e o foco vai para o "Criar uma nota").
  const focarDepois = useRef<{ id: string | null } | null>(null)
  // A ordem manual: a frase que o leitor de tela ouve depois de mover, a fila dos
  // pedidos de mover (um de cada vez, na ordem em que a pessoa soltou) e a versão
  // mais nova que cada nota teve numa resposta de mover (mover sobe a versão, e a
  // próxima edição, feita com a nota do cartão, não pode usar a de antes).
  const [anuncioDaOrdem, setAnuncioDaOrdem] = useState('')
  const filaDaEdicao = useRef<Promise<unknown>>(Promise.resolve())
  const versoesMovidas = useRef(new Map<string, number>())

  const consulta = `${vista}\u0000${marcador ?? ''}\u0000${busca}`
  const recarregar = useCallback(() => setRecarga((n) => n + 1), [])
  // A rede e a fila do celular (N8, E4). A volta da rede, e cada mudança que
  // sobe, leem a lista de novo: o "esperando a rede" sai, e a mescla aparece.
  const rede = useSyncExternalStore(celular?.rede.assinar ?? semAssinar, celular?.rede.estado ?? lerRedeDoPC)
  const semRede = rede.semRede
  const nEsperando = rede.esperando.size
  // Os avisos da fila (o selo de uma nota que mudou nos dois lados, a recusa que
  // virou nota nova) vão para a pílula.
  useEffect(() => celular?.rede.aoAvisar?.((texto) => avisar(texto)), [celular, avisar])
  const redeVista = useRef({ semRede, nEsperando })
  useEffect(() => {
    const antes = redeVista.current
    redeVista.current = { semRede, nEsperando }
    if (antes.semRede !== semRede || antes.nEsperando !== nEsperando) recarregar()
  }, [semRede, nEsperando, recarregar])

  useEffect(() => {
    const ctrl = new AbortController()
    ultimaCarga.current = Date.now()
    const filtro: FiltroDasNotas = { vista }
    if (marcador) filtro.marcador = marcador
    if (busca) filtro.busca = busca
    const padrao = vista === 'ativas' && !marcador && !busca
    Promise.all([
      cliente.listarNotas(filtro, ctrl.signal),
      padrao ? null : cliente.listarNotas({ vista: 'ativas' }, ctrl.signal),
      cliente.listarMarcadores(ctrl.signal),
    ]).then(
      ([itens, base, marcadores]) => {
        setResultado({ consulta, itens, base: base ?? itens, marcadores })
        // O marcador que deixou de existir (apagado em outra aba) sai do filtro.
        setMarcador((m) => (m && !marcadores.some((x) => x.id === m) ? null : m))
      },
      (e: unknown) => {
        if (ctrl.signal.aborted) return
        if (problemaDe(e)) aoPerderLigacao(e)
        // Com a lista da mesma consulta na tela, a falha de uma recarga não a apaga.
        setResultado((r) => (r?.consulta === consulta ? r : { consulta, itens: [], base: [], marcadores: [], erro: mensagemDe(e) }))
      },
    )
    return () => ctrl.abort()
  }, [cliente, vista, marcador, busca, consulta, recarga, mudancasDoSync, aoPerderLigacao])

  // O "Abrir" do aviso "Virou nota": lê a nota e abre a janela dela. A lista
  // também é lida de novo (a nota nova não estava nela).
  useEffect(() => {
    if (!abrirNota) return
    let cancelado = false
    cliente.obterNota(abrirNota.id).then(
      (n) => {
        if (cancelado) return
        recarregar()
        if (n.deleted_at) avisar('Esta nota está na lixeira.')
        else setAberta(n)
      },
      (e: unknown) => {
        if (cancelado) return
        if (problemaDe(e)) aoPerderLigacao(e)
        else avisar('Não consegui abrir a nota.')
      },
    )
    return () => {
      cancelado = true
    }
  }, [abrirNota, cliente, recarregar, avisar, aoPerderLigacao])

  // Voltar para a aba lê de novo: a nota pode ter mudado em outra aba.
  useEffect(() => {
    function voltou() {
      if (document.visibilityState === 'hidden') return
      if (Date.now() - ultimaCarga.current < intervaloDaRecarga) return
      ultimaCarga.current = Date.now()
      recarregar()
    }
    window.addEventListener('focus', voltou)
    document.addEventListener('visibilitychange', voltou)
    return () => {
      window.removeEventListener('focus', voltou)
      document.removeEventListener('visibilitychange', voltou)
    }
  }, [recarregar])

  // Depois que a janela fecha, ou um cartão sai: o foco volta ao cartão
  // da nota (ou ao vizinho, ou ao "Criar uma nota"). Só se o foco estava
  // perdido; quem já está digitando em outro lugar fica.
  useEffect(() => {
    const alvo = focarDepois.current
    if (!alvo || aberta) return
    focarDepois.current = null
    const ativo = document.activeElement
    const livre = !ativo || ativo === document.body || !ativo.isConnected || Boolean(ativo.closest('.veu'))
    if (!livre && !raiz.current?.contains(ativo)) return
    const abrir = alvo.id ? Array.from(raiz.current?.querySelectorAll<HTMLElement>('[data-nota]') ?? []).find((el) => el.dataset.nota === alvo.id) : null
    const destino =
      abrir?.querySelector<HTMLElement>('[data-abrir]') ??
      (abrir?.hasAttribute('data-abrir') ? abrir : null) ??
      raiz.current?.querySelector<HTMLElement>('[data-abrir]') ??
      raiz.current?.querySelector<HTMLElement>('.criar-abrir')
    destino?.focus()
  })

  useEffect(() => {
    if (eraEditando.current && !editando) chipEditar.current?.focus()
    eraEditando.current = editando
  }, [editando])

  const falhar = useCallback(
    (e: unknown) => {
      if (e instanceof SoComRede) return avisar(e.message)
      if (problemaDe(e)) aoPerderLigacao(e)
      else avisar(`Não deu certo: ${mensagemDe(e)}`)
    },
    [aoPerderLigacao, avisar],
  )

  const mudarItens = (f: (itens: Nota[]) => Nota[]) => setResultado((r) => r && { ...r, itens: f(r.itens) })
  // trocar põe a nota nova na lista e na base (as contas dos marcadores saem dela).
  const trocar = (nova: Nota) => {
    const por = (itens: Nota[]) => itens.map((n) => (n.id === nova.id ? nova : n))
    setResultado((r) => r && { ...r, itens: por(r.itens), base: por(r.base) })
  }
  const mudarMarcadores = (f: (atuais: Marcador[]) => Marcador[]) => setResultado((r) => r && { ...r, marcadores: f(r.marcadores) })
  const tirar = (id: string) => mudarItens((itens) => itens.filter((n) => n.id !== id))

  const visiveis = resultado?.consulta === consulta ? resultado : null
  const ordenados = visiveis ? ordenar(visiveis.itens) : []
  const marcadores = resultado?.marcadores ?? semMarcadores
  const nomesDosMarcadores = new Map(marcadores.map((m) => [m.id, m.nome]))

  // O que se faz com uma nota que muda de lista (arquivar, desarquivar, ir para
  // a lixeira): sai da tela, o foco vai ao vizinho e a pílula oferece o Desfazer.
  function sairDaLista(id: string) {
    focarDepois.current = { id: vizinhoDe(ordenados, id) }
    tirar(id)
  }

  // editar muda a nota pelo PATCH com a versão que o cartão tem. O 409 quer
  // dizer que ela mudou em outra aba: a lista é lida de novo e a pessoa repete.
  async function editar(nota: Nota, mudanca: { cor?: CorDaNota | null; fixada?: boolean; arquivada?: boolean }): Promise<Nota | null> {
    // Na fila dos pedidos que mexem na nota: um mover ainda a caminho sobe a versão
    // dela, e o PATCH com a versão de antes seria um 409 à toa.
    const passo = async (): Promise<Nota | null> => {
      const versao = Math.max(nota.version, versoesMovidas.current.get(nota.id) ?? 0)
      try {
        const nova = await cliente.editarNota(nota.id, versao, mudanca)
        versoesMovidas.current.delete(nota.id)
        return nova
      } catch (e) {
        if (e instanceof Conflito) {
          avisar('Esta nota mudou em outra aba. A lista foi lida de novo; repita se ainda quiser.')
          recarregar()
          return null
        }
        falhar(e)
        return null
      }
    }
    const p = filaDaEdicao.current.then(passo, passo)
    filaDaEdicao.current = p
    return p
  }

  // mover é o soltar de uma nota em outro lugar do grupo dela (ou o Alt + seta):
  // a lista muda na hora, o pedido vai ao Agent, e, se ele recusa, a ordem de antes
  // volta e a pílula diz por quê. indice é a posição nova SEM contar a própria nota.
  function mover(id: string, indice: number, origem: OrigemDoMovimento) {
    const nota = ordenados.find((n) => n.id === id)
    if (!nota) return
    const grupo = ordenados.filter((n) => n.fixada === nota.fixada).map((n) => n.id)
    const nova = moverNaLista(grupo, id, indice)
    const posicao = nova.indexOf(id) + 1
    if (nova.every((x, i) => x === grupo[i])) {
      if (origem === 'teclado') setAnuncioDaOrdem(fraseNaPonta(posicao, grupo.length))
      return
    }
    const { antes, depois } = vizinhasDe(nova, id)
    mudarItens((itens) => aplicarOrdem(itens, nova))
    setAnuncioDaOrdem(frasePosicao(posicao, grupo.length))
    if (origem === 'teclado') focarDepois.current = { id }
    const passo = async () => {
      try {
        const salva = await cliente.moverNota(id, antes, depois)
        versoesMovidas.current.set(id, salva.version)
        trocar(salva)
      } catch (e) {
        // Volta a ordem de antes e lê a lista de novo: o que o Agent tem é o que vale.
        mudarItens((itens) => aplicarOrdem(itens, grupo))
        if (e instanceof Conflito) avisar('A lista das notas mudou em outra aba. Li de novo; repita se ainda quiser.')
        else falhar(e)
        recarregar()
      }
    }
    const p = filaDaEdicao.current.then(passo, passo)
    filaDaEdicao.current = p
  }

  async function arquivar(nota: Nota) {
    const volta = !nota.arquivada
    const nova = await editar(nota, { arquivada: !nota.arquivada })
    if (!nova) return
    sairDaLista(nota.id)
    avisar(volta ? 'Nota arquivada.' : 'Nota desarquivada.', () => void desfazerArquivo(nova, nota.arquivada))
    recarregar()
  }

  // desfazerArquivo devolve a nota ao estado de antes, na versão em que ela está agora.
  async function desfazerArquivo(nota: Nota, arquivada: boolean) {
    try {
      await cliente.editarNota(nota.id, nota.version, { arquivada })
    } catch (e) {
      if (e instanceof Conflito) avisar('Esta nota mudou em outra aba; não consegui desfazer.')
      else falhar(e)
    }
    recarregar()
  }

  async function paraALixeira(nota: Nota) {
    try {
      await cliente.moverNotaParaLixeira(nota.id)
    } catch (e) {
      return falhar(e)
    }
    sairDaLista(nota.id)
    avisar('Nota movida para a lixeira.', () => void desfazerLixeira(nota))
    recarregar()
  }

  async function desfazerLixeira(nota: Nota) {
    try {
      await cliente.restaurarNota(nota.id)
      avisar('Nota de volta.')
    } catch (e) {
      falhar(e)
    }
    recarregar()
  }

  async function restaurar(nota: Nota) {
    try {
      await cliente.restaurarNota(nota.id)
    } catch (e) {
      return falhar(e)
    }
    sairDaLista(nota.id)
    avisar('Nota restaurada.')
    recarregar()
  }

  async function excluirDeVez(nota: Nota): Promise<boolean> {
    try {
      await cliente.apagarNotaDeVez(nota.id)
    } catch (e) {
      falhar(e)
      return false
    }
    sairDaLista(nota.id)
    avisar('Nota excluída para sempre.')
    recarregar()
    return true
  }

  // copiar escreve na área de transferência dentro do clique de quem pediu.
  function copiar(nota: Nota) {
    const texto = [nota.titulo.trim(), nota.corpo].filter(Boolean).join('\n\n')
    if (texto === '') return
    const area = navigator.clipboard
    if (!area) return avisar('Não consegui copiar. Abra a nota e use Ctrl+C.')
    area.writeText(texto).then(
      () => avisar('Copiado.'),
      () => avisar('Não consegui copiar. Abra a nota e use Ctrl+C.'),
    )
  }

  // copiarWhatsApp copia a nota do cartão como texto de WhatsApp, no clique.
  function copiarWhatsApp(nota: Nota) {
    copiarParaWhatsApp(nota.titulo, nota.corpo, { avisar, estreita, aoSelecionar: 'Abra a nota e use Ctrl+C.' })
  }

  // criarMarcador cria o marcador que o menu do botão de marcador pediu. Devolve o
  // marcador, ou a frase de erro que o menu mostra.
  async function criarMarcador(nome: string): Promise<Marcador | string> {
    const limpo = limparNome(nome)
    const igual = acharPorNome(marcadores, limpo)
    if (igual) return `Já existe o marcador "${igual.nome}".`
    try {
      const novo = await cliente.criarMarcador(limpo)
      mudarMarcadores((l) => ordenarPorNome([...l.filter((m) => m.id !== novo.id), novo]))
      return novo
    } catch (e) {
      if (e instanceof ErroDaApi && e.status === 409 && e.codigo === 'NAME_IN_USE') return `Já existe o marcador "${limpo}".`
      if (problemaDe(e)) aoPerderLigacao(e)
      return `Não consegui criar o marcador: ${mensagemDe(e)}`
    }
  }

  // marcarNoCartao põe ou tira o marcador de uma nota sem abri-la. Uma de cada vez: a
  // nota que volta de um pedido já traz os marcadores do anterior.
  const filaDosMarcadores = useRef<Promise<unknown>>(Promise.resolve())
  function marcarNoCartao(nota: Nota, m: Marcador, marcar: boolean): Promise<void> {
    const passo = async () => {
      try {
        trocar(await (marcar ? cliente.marcarNota(nota.id, m.id) : cliente.desmarcarNota(nota.id, m.id)))
      } catch (e) {
        if (e instanceof Conflito) {
          avisar('Esta nota mudou em outra aba. A lista foi lida de novo; repita se ainda quiser.')
          recarregar()
        } else if (e instanceof ErroDaApi && e.status === 404) {
          avisar('Esse marcador não existe mais.')
          recarregar()
        } else falhar(e)
      }
    }
    const p = filaDosMarcadores.current.then(passo, passo)
    filaDosMarcadores.current = p
    return p
  }

  const acoes: AcoesDaNota = {
    marcar: (nota, m, marcar) => void marcarNoCartao(nota, m, marcar),
    criarEMarcar: async (nota, nome) => {
      const criado = await criarMarcador(nome)
      if (typeof criado === 'string') return criado
      await marcarNoCartao(nota, criado, true)
      return null
    },
    fixar: (nota) => {
      // Fixar e desafixar levam a nota para o topo do grupo de destino (o Agent
      // faz o mesmo): ela vai para o começo da lista, e a lista a põe no grupo.
      void editar(nota, { fixada: !nota.fixada }).then((nova) => {
        if (!nova) return
        trocar(nova)
        mudarItens((itens) => [nova, ...itens.filter((n) => n.id !== nova.id)])
      })
    },
    cor: (nota, cor) => {
      void editar(nota, { cor }).then((nova) => nova && trocar(nova))
    },
    arquivar: (nota) => void arquivar(nota),
    copiar,
    copiarWhatsApp,
    lixeira: (nota) => void paraALixeira(nota),
  }

  // chegouNaNota é o "Ditar para nota" do celular (N8): a nota entra na lista
  // (a nova no topo; a de destino, trocada), e o aviso diz para onde foi, com
  // Abrir e Desfazer.
  function chegouNaNota(c: ChegadaNaNota) {
    if (c.nova) mudarItens((itens) => (vista === 'ativas' && !marcador && !busca ? [c.nota, ...itens.filter((n) => n.id !== c.nota.id)] : itens))
    else trocar(c.nota)
    recarregar()
    const texto = c.nova ? (
      <strong>Nota nova</strong>
    ) : (
      <>
        <strong>{tituloCurto(c.nota.titulo)}</strong> <span className="mais-n">+{c.linhas}</span>
      </>
    )
    avisar(
      texto,
      () => {
        c.desfazer().then(
          (r) => {
            recarregar()
            avisar(r.nota_voltou ? 'Ditado de volta ao histórico.' : 'Ditado de volta ao histórico. A nota ficou, porque mudou depois do ditado.')
          },
          (e: unknown) => falhar(e),
        )
      },
      () => setAberta(c.nota),
    )
  }

  function terminou(fim: FimDaNota) {
    setAberta(null)
    aoTerminarNota?.()
    const { nota, tipo } = fim
    if (tipo === 'fechou') {
      focarDepois.current = { id: nota.id }
      trocar(nota)
    } else if (tipo === 'lixeira') {
      sairDaLista(nota.id)
      avisar('Nota movida para a lixeira.', () => void desfazerLixeira(nota))
    } else {
      sairDaLista(nota.id)
      avisar(tipo === 'arquivou' ? 'Nota arquivada.' : 'Nota desarquivada.', () => void desfazerArquivo(nota, tipo === 'desarquivou'))
    }
    recarregar()
  }

  function escolherForma(f: FormaDeVer) {
    setForma(f)
    gravarForma(f)
  }

  function mudarColunas(n: number) {
    const novo = limitar(n, limites)
    if (estreita) setColunasNoCelular(novo)
    else setColunasNoPc(novo)
    gravarColunas(estreita, novo)
  }

  function escolherVista(v: VistaDasNotas, m: string | null) {
    setVista(v)
    setMarcador(m)
  }

  const noArquivo = vista === 'arquivo'
  const base = resultado?.base ?? []
  // Reorganizar só vale na tela principal, sem filtro nem busca: lá a lista é a
  // ordem inteira das notas ativas, e o "entre estas duas" do Agent faz sentido.
  // Nas outras (marcador, busca, Arquivo) arrastar avisa por que não.
  const podeOrdenar = vista === 'ativas' && !marcador && !busca
  const ordem: ConfigDaOrdem = {
    habilitado: podeOrdenar,
    aoMover: mover,
    aoBloquear: () =>
      avisar(
        busca
          ? 'Para reorganizar, limpe a busca.'
          : marcador
            ? 'Para reorganizar, saia do filtro de marcador: volte para “Notas”.'
            : 'O Arquivo fica na ordem em que as notas foram arquivadas.',
      ),
  }
  const nomeDaVista = vista === 'lixeira' ? 'a lixeira das notas' : noArquivo ? 'o arquivo' : 'as notas'

  let conteudo: ReactNode
  if (!visiveis) {
    conteudo = <p className="estado atrasado">Carregando…</p>
  } else if (visiveis.erro) {
    conteudo = (
      <div className="estado">
        <p>
          Não consegui carregar {nomeDaVista}: {visiveis.erro}
        </p>
        <button type="button" className="botao" onClick={recarregar}>
          Tentar de novo
        </button>
      </div>
    )
  } else if (ordenados.length === 0) {
    conteudo = (
      <div className="estado">
        {busca ? (
          <p>Nada encontrado para “{busca}”.</p>
        ) : vista === 'lixeira' ? (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">A lixeira das notas está vazia.</p>
            <p>A nota apagada fica aqui por 30 dias.</p>
          </>
        ) : noArquivo ? (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">O arquivo está vazio.</p>
            <p>As notas que você arquiva saem da tela principal e ficam aqui.</p>
          </>
        ) : marcador ? (
          <p>Nenhuma nota com este marcador.</p>
        ) : (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">Nenhuma nota ainda.</p>
            <p>Escreva a primeira em “Criar uma nota…”.</p>
          </>
        )}
      </div>
    )
  } else if (vista === 'lixeira') {
    conteudo = (
      <ul className="itens">
        {ordenados.map((n) => (
          <NotaNaLixeira key={n.id} nota={n} aoRestaurar={() => void restaurar(n)} aoExcluirDeVez={() => excluirDeVez(n)} />
        ))}
      </ul>
    )
  } else {
    const fixadas = ordenados.filter((n) => n.fixada)
    const outras = ordenados.filter((n) => !n.fixada)
    // Abrir leva a versão mais nova que a nota teve num mover (a do cartão pode ser
    // a de antes, se a resposta ainda não chegou).
    const abrir = (n: Nota) => setAberta({ ...n, version: Math.max(n.version, versoesMovidas.current.get(n.id) ?? 0) })
    const desenhar = (itens: Nota[]) =>
      forma === 'lista' ? (
        <ListaDeNotas
          notas={itens}
          config={ordem}
          renderizar={(n, o) => (
            <LinhaDaNota key={n.id} nota={n} nomesDosMarcadores={nomesDosMarcadores} marcadores={marcadores} noArquivo={noArquivo} aoAbrir={abrir} acoes={acoes} ordem={o} />
          )}
        />
      ) : (
        <GradeDeNotas
          notas={itens}
          colunas={colunas}
          config={ordem}
          renderizar={(n, o) => (
            <CartaoDaNota key={n.id} nota={n} nomesDosMarcadores={nomesDosMarcadores} marcadores={marcadores} noArquivo={noArquivo} aoAbrir={abrir} acoes={acoes} ordem={o} />
          )}
        />
      )
    if (forma === 'detalhes') {
      conteudo = <TabelaDeNotas fixadas={fixadas} outras={outras} nomesDosMarcadores={nomesDosMarcadores} aoAbrir={abrir} config={ordem} />
    } else if (fixadas.length > 0) {
      conteudo = (
        <>
          <section aria-labelledby="notas-fixadas">
            <h2 id="notas-fixadas" className="secao-notas">
              Fixadas
            </h2>
            {desenhar(fixadas)}
          </section>
          {outras.length > 0 && (
            <section aria-labelledby="notas-outras">
              <h2 id="notas-outras" className="secao-notas">
                Outras
              </h2>
              {desenhar(outras)}
            </section>
          )}
        </>
      )
    } else {
      conteudo = desenhar(ordenados)
    }
  }

  const total = visiveis && !visiveis.erro ? ordenados.length : null
  const anuncio = total === null ? '' : total === 1 ? '1 nota.' : `${total} notas.`

  return (
    <div className={celular ? 'notas no-celular' : 'notas'} ref={raiz} data-oculta={oculta ? '' : undefined}>
      <p className="so-leitor anuncio" role="status">
        {anuncio}
      </p>
      <p className="so-leitor" role="status" aria-live="polite" data-ordem>
        {anuncioDaOrdem}
      </p>

      {celular && semRede && (
        <p className="faixa-rede" role="status">
          <span className="bolinha" aria-hidden="true" />
          Sem rede{nEsperando > 0 && ` · ${nEsperando} ${nEsperando === 1 ? 'mudança esperando' : 'mudanças esperando'}`}
        </p>
      )}

      <div className="barra-de-notas">
        <div className="filtros" role="group" aria-label="Marcadores">
          <button type="button" className="filtro" aria-pressed={vista === 'ativas' && marcador === null} onClick={() => escolherVista('ativas', null)}>
            Notas{' '}
            {resultado && <span className="filtro-n">{base.length}</span>}
          </button>
          {marcadores.map((m) => (
            <button key={m.id} type="button" className="filtro" aria-pressed={vista === 'ativas' && marcador === m.id} onClick={() => escolherVista('ativas', m.id)}>
              {m.nome}{' '}
              <span className="filtro-n">{base.filter((n) => n.marcadores.includes(m.id)).length}</span>
            </button>
          ))}
          <button type="button" className="filtro separa" aria-pressed={noArquivo} onClick={() => escolherVista('arquivo', null)}>
            Arquivo
          </button>
          <button type="button" className="filtro" aria-pressed={vista === 'lixeira'} onClick={() => escolherVista('lixeira', null)}>
            Lixeira
          </button>
          <button ref={chipEditar} type="button" className="filtro editar" aria-haspopup="dialog" onClick={() => setEditando(true)}>
            Editar marcadores
          </button>
        </div>

        {vista !== 'lixeira' && (
          <div className="formas">
            {forma === 'grade' && (
              <div className="colunas" role="group" aria-label="Colunas">
                <button type="button" className="icone-botao" aria-label="Menos colunas" title="Menos colunas" disabled={colunas <= limites.min} onClick={() => mudarColunas(colunas - 1)}>
                  −
                </button>
                <span className="colunas-n" aria-hidden="true">
                  {colunas}
                </span>
                <button type="button" className="icone-botao" aria-label="Mais colunas" title="Mais colunas" disabled={colunas >= limites.max} onClick={() => mudarColunas(colunas + 1)}>
                  +
                </button>
              </div>
            )}
            <div className="forma-de-ver" role="group" aria-label="Forma de ver">
              {FORMAS.map((f) => (
                <button key={f} type="button" className="icone-botao" aria-pressed={forma === f} aria-label={nomesDasFormas[f]} title={nomesDasFormas[f]} onClick={() => escolherForma(f)}>
                  <Icone nome={f} />
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {vista === 'ativas' && (
        <CriarNota
          cliente={cliente}
          desligado={semRede}
          celular={Boolean(celular)}
          aoCriar={(nota) => {
            mudarItens((itens) => (marcador || busca ? itens : [nota, ...itens]))
            if (marcador || busca) avisar('Nota criada.')
            recarregar()
          }}
          aoPerderLigacao={aoPerderLigacao}
        />
      )}

      {conteudo}

      {aberta && (
        <DialogoDaNota
          key={aberta.id}
          nota={aberta}
          cliente={cliente}
          marcadores={marcadores}
          notasParaSugerir={base}
          criarMarcador={criarMarcador}
          avisar={avisar}
          aoPerderLigacao={aoPerderLigacao}
          aoTerminar={terminou}
          celular={celular && { semRede, DitarNaNota: celular.DitarNaNota }}
        />
      )}

      {celular && !oculta && vista !== 'lixeira' && (
        <celular.DitarParaNota
          desligado={semRede}
          aoChegar={chegouNaNota}
          avisar={avisar}
          aoPerderLigacao={aoPerderLigacao}
          abrirConfiguracoes={abrirConfiguracoes}
        />
      )}

      {editando && (
        <EditarMarcadores
          cliente={cliente}
          marcadores={marcadores}
          notasAtivas={base}
          aoMudar={(f) => {
            mudarMarcadores(f)
            recarregar()
          }}
          aoFechar={() => setEditando(false)}
          aoPerderLigacao={aoPerderLigacao}
          avisar={avisar}
        />
      )}
    </div>
  )
}
