import { type KeyboardEvent, type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { type Aprendida, Conflito, type Ditado, type EscolhaDoSelo, ForaDoAr, type Problema, type Sugestao, problemaDe } from './api'
import type { Ambiente, Aprendiz } from './ambiente'
import { Aurora } from './Aurora'
import { Aviso, type AvisoDados } from './Aviso'
import { agruparPorDia, formatarCurta, formatarHora } from './datas'
import { type Rascunho, type Salvamento, naoTocado, novoRascunho, salvarSobre, textosNaoSalvos } from './edicao'
import { ItemDitado, TextoDaLinha } from './ItemDitado'
import {
  type Resultado,
  type Vista,
  anexarPagina,
  anunciar,
  aposFalha,
  campoDaOrdem,
  inserirEmOrdem,
  mesclarRecarga,
  mesmaConsulta,
  sumidosEmEdicao,
  vizinho,
} from './lista'
import { Marca } from './Marca'

const esperaDaBusca = 250

// Ao voltar para a aba, a lista recarrega — no máximo uma vez a cada 2 s:
// "focus" e "visibilitychange" costumam chegar juntos, e alternar de
// janela várias vezes não precisa virar uma rajada de pedidos.
const intervaloDaRecarga = 2000

interface Props {
  // ambiente: o que muda entre o PC e a nuvem (ambiente.ts). Estável: quem
  // monta o Painel cria o ambiente uma vez.
  ambiente: Ambiente
  // focarNoTitulo: o Painel entra no lugar de uma tela que tinha o foco (a
  // Entrar e a Sem conexão da nuvem, cujo botão some), e o foco vai para o
  // título, para o leitor de tela anunciar o histórico e o teclado não
  // recomeçar do <body>. Sem ele (o PC, e a nuvem que abre com a sessão
  // do cookie), o foco fica onde o navegador o pôs.
  focarNoTitulo?: boolean
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Atalhos da página não valem onde a pessoa digita: lá, Ctrl+Z desfaz a
// digitação e Ctrl+K é do navegador.
const dentroDeCampo = (alvo: EventTarget | null) => alvo instanceof HTMLElement && alvo.closest('input, textarea, select, [contenteditable]') !== null

const atalho = (e: globalThis.KeyboardEvent, tecla: string) =>
  (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === tecla && !dentroDeCampo(e.target)

// Alt+A é o Aprender do aviso da sugestão (ADR-017). Só o Alt: Ctrl+Alt é o
// AltGr dos teclados com ele (ABNT2 inclusive), e Ctrl+Enter, que salva no
// editor, repetiria o salvar como aprender. A tecla segurada (repeat) não
// conta: um segundo disparo antes da resposta não pode aprender de novo, e
// um Alt+A que fica apertado não pode desfazer o que aprendeu.
const atalhoDeAprender = (e: globalThis.KeyboardEvent) =>
  e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.repeat && e.key.toLowerCase() === 'a' && !dentroDeCampo(e.target)

function semChave<T>(r: Record<string, T>, id: string): Record<string, T> {
  if (!(id in r)) return r
  const resto = { ...r }
  delete resto[id]
  return resto
}

export function Painel({ ambiente, focarNoTitulo = false }: Props) {
  const { cliente, textos, Configuracoes, Faixa, ditar: Ditar } = ambiente
  const [vista, setVista] = useState<Vista>('historico')
  // configurando: a tela de Configurações está à vista, no lugar da lista.
  // A vista da lista não muda por baixo: voltar leva à mesma lista, com a
  // mesma busca. A tela, uma vez aberta, fica montada e só se esconde, e
  // o que a pessoa mudou e não salvou não se perde ao olhar o histórico.
  const [configurando, setConfigurando] = useState(false)
  const [abriuConfiguracoes, setAbriuConfiguracoes] = useState(false)
  const [texto, setTexto] = useState('')
  const [busca, setBusca] = useState('')
  const [recarga, setRecarga] = useState(0)
  const [resultado, setResultado] = useState<Resultado | null>(null)
  const [carregandoMais, setCarregandoMais] = useState(false)
  const [aberto, setAberto] = useState<string | null>(null)
  const [rascunhos, setRascunhos] = useState<Record<string, Rascunho>>({})
  const [aviso, setAviso] = useState<AvisoDados | null>(null)
  // aprendizado sobe a cada aprendizado ou desfazer feito pelo aviso: é o
  // que a lista "Aprendido no painel" das Configurações espera para ler de
  // novo.
  const [aprendizado, setAprendizado] = useState(0)
  // problema: a ligação com o Agent caiu no meio do uso. Não desmonta
  // nada — só acende a faixa do topo (revisão de 29-set-2026: o App
  // trocava a tela inteira e levava junto o texto que estava sendo
  // editado).
  const [problema, setProblema] = useState<Problema | null>(null)
  // ditadoPendente: há um ditado gravado e ainda não enviado, só na memória do
  // gravador (nuvem; ADR-021). Conta como texto não salvo: a sessão que acaba
  // não troca a tela inteira, e a faixa da sessão diz que o ditado espera.
  const [ditadoPendente, setDitadoPendente] = useState(false)
  const audioPendente = useRef(false)
  // chegou: o ditado que o celular acabou de gravar, em destaque no topo.
  const [chegou, setChegou] = useState<string | null>(null)
  const chegouAtual = useRef(chegou)
  const topo = useRef<HTMLElement>(null)
  const titulo = useRef<HTMLHeadingElement>(null)
  const campoBusca = useRef<HTMLInputElement>(null)
  const lista = useRef<HTMLDivElement>(null)
  // focarDepois: a linha que recebe o foco no próximo render, depois que
  // uma linha sai (lixeira, restaurar) ou entra (Desfazer, mais antigos).
  // id null: não há linha para onde ir, e o foco vai para a busca.
  const focarDepois = useRef<{ id: string | null } | null>(null)
  const proximoAviso = useRef(0)
  // aprendendo: um clique em Aprender está no ar. Um segundo (o Alt+A e o
  // botão, dois cliques seguidos) não pede de novo: a segunda troca da
  // mesma edição daria "já existe".
  const aprendendo = useRef(false)
  const ultimaCarga = useRef(0)
  // O que termina depois de um await decide pela lista de AGORA, não pela
  // do momento do clique.
  const resultadoAtual = useRef(resultado)
  const avisoAtual = useRef(aviso)
  const abertoAtual = useRef(aberto)
  const rascunhosAtuais = useRef(rascunhos)
  useLayoutEffect(() => {
    resultadoAtual.current = resultado
    avisoAtual.current = aviso
    abertoAtual.current = aberto
    rascunhosAtuais.current = rascunhos
    chegouAtual.current = chegou
  })

  const chave = `${vista}\u0000${busca}\u0000${recarga}`
  const carregando = resultado?.chave !== chave

  const mostrarAviso = useCallback((dados: Omit<AvisoDados, 'id'>) => {
    proximoAviso.current += 1
    setAviso({ id: proximoAviso.current, ...dados })
  }, [])
  const avisar = useCallback((texto: ReactNode, desfazer?: () => void) => mostrarAviso({ texto, desfazer }), [mostrarAviso])
  const sumirAviso = useCallback(() => setAviso(null), [])

  // acender: a ligação caiu. No PC, a faixa do topo avisa e nada sai da
  // tela; na nuvem, a sessão que acabou troca a tela inteira
  // (ambiente.aoSemSessao, "Sessão expirada") — menos com um texto não
  // salvo na tela, que só existe na memória desta página: aí a tela fica, e
  // a faixa leva a pessoa a entrar de novo sem perdê-lo (ADR-019, decisão do
  // Mauro de 05-out-2026: "Guarda até entrar de novo").
  const acender = useCallback(
    (p: Problema) => {
      // O ditado gravado e ainda não enviado conta como texto não salvo
      // (ADR-021): trocar a tela inteira o jogaria fora.
      const comTexto = textosNaoSalvos(rascunhosAtuais.current, resultadoAtual.current?.itens).length > 0 || audioPendente.current
      if (p === 'sem-sessao' && ambiente.aoSemSessao && !comTexto) ambiente.aoSemSessao()
      else setProblema(p)
    },
    [ambiente],
  )

  // entrouDeNovo: a mesma conta voltou sem a página sair do lugar (a faixa
  // da sessão, na nuvem). A faixa sai, a lista lê de novo (os editores e os
  // rascunhos ficam: mesclarRecarga), o aviso diz que dá para salvar, e o
  // foco, se estava na faixa, vai para o texto não salvo.
  const focarRascunho = useRef(false)
  const entrouDeNovo = useCallback(() => {
    focarRascunho.current = true
    setProblema(null)
    setRecarga((n) => n + 1)
    avisar('Você entrou de novo. Pode salvar.')
  }, [avisar])

  useEffect(() => {
    const t = setTimeout(() => setBusca(texto.trim()), esperaDaBusca)
    return () => clearTimeout(t)
  }, [texto])

  useEffect(() => {
    const ctrl = new AbortController()
    ultimaCarga.current = Date.now()
    const consulta = { chave, vista, busca }
    cliente.listar({ lixeira: vista === 'lixeira', busca }, ctrl.signal).then(
      (p) => {
        // hoje é o momento desta carga, não o da montagem: a aba fica
        // aberta por dias, e "Hoje" congelado passava a marcar os ditados
        // de ontem (revisão de 29-set-2026).
        const novo = { ...consulta, hoje: new Date(), itens: p.items, proximo: p.next_cursor }
        const carga = { ...novo, anuncio: anunciar(novo) }
        // Em edição: a linha aberta e as que têm rascunho (rascunho não
        // tocado some ao fechar a linha, então os que restam são texto
        // que a pessoa escreveu). Nada disso sai da tela numa recarga.
        const emEdicao = new Set(Object.keys(rascunhosAtuais.current))
        if (abertoAtual.current) emEdicao.add(abertoAtual.current)
        // O ditado que o celular acabou de gravar: uma carga que o servidor
        // montou antes de gravá-lo não o tira da tela.
        if (chegouAtual.current) emEdicao.add(chegouAtual.current)
        setResultado((anterior) => mesclarRecarga(anterior, carga, emEdicao))
        setProblema(null)
        // Quem ficou sem vir na recarga é perguntado um a um: se foi para
        // a lixeira em outra aba, o editor passa a dizer isso e a linha
        // ganha a etiqueta "na lixeira" (revisão de 29-set-2026).
        for (const sumido of sumidosEmEdicao(resultadoAtual.current, carga, emEdicao)) {
          cliente.obter(sumido.id).then(
            (atual) => setResultado((r) => r && { ...r, itens: r.itens.map((i) => (i.id === atual.id ? atual : i)) }),
            (e: unknown) => {
              // Outro erro (404 de um ditado apagado de vez): a linha
              // fica como estava, com o texto para copiar.
              const deLigacao = problemaDe(e)
              if (deLigacao) acender(deLigacao)
            },
          )
        }
      },
      (e: unknown) => {
        if (ctrl.signal.aborted) return
        const deLigacao = problemaDe(e)
        if (deLigacao) acender(deLigacao)
        const texto = e instanceof ForaDoAr ? textos.semResposta : mensagem(e)
        const depois = aposFalha(resultadoAtual.current, { ...consulta, hoje: new Date() }, texto, deLigacao !== null)
        setResultado(depois)
        if (!deLigacao && !depois.erro) avisar(`Não consegui atualizar a lista: ${mensagem(e)}`)
      },
    )
    return () => ctrl.abort()
  }, [cliente, vista, busca, chave, avisar, acender, textos])

  // Voltar para a aba recarrega a lista: quem dita com o painel aberto ao
  // lado precisa ver o ditado novo sem apertar F5 — que, por segurança,
  // derruba a sessão (revisão de 29-set-2026). A recarga não fecha editor
  // nem mexe em rascunho, e mantém as páginas já carregadas
  // (mesclarRecarga).
  useEffect(() => {
    function voltou() {
      if (document.visibilityState === 'hidden') return
      if (Date.now() - ultimaCarga.current < intervaloDaRecarga) return
      ultimaCarga.current = Date.now()
      setRecarga((n) => n + 1)
    }
    window.addEventListener('focus', voltou)
    document.addEventListener('visibilitychange', voltou)
    return () => {
      window.removeEventListener('focus', voltou)
      document.removeEventListener('visibilitychange', voltou)
    }
  }, [])

  // Os atalhos da página, num ouvinte só, que existe desde a montagem e lê
  // o aviso do momento: um ouvinte que nascesse junto com o aviso só
  // entraria depois do efeito, e um Ctrl+Z logo após a pílula aparecer se
  // perderia.
  //
  // Ctrl+K leva à busca. Era "/", uma tecla só, e um "28/09" ditado na
  // aba errada mandava o resto do texto para a busca (WCAG 2.1.4,
  // revisão de 29-set-2026).
  //
  // Ctrl+Z desfaz enquanto o aviso com "Desfazer" existe. Sem isso, quem
  // usa só o teclado precisava de dezenas de Tabs até a pílula, que some
  // em 8 s (revisão de 29-set-2026).
  //
  // Alt+A aprende enquanto o aviso com "Aprender" existe, pelo mesmo
  // motivo (ADR-017). Ele não tira o aviso do lugar: quem o troca pelo
  // resultado é o aprender, quando a resposta chega.
  useEffect(() => {
    function tecla(e: globalThis.KeyboardEvent) {
      // Nas Configurações não há busca: Ctrl+K fica com o navegador.
      if (atalho(e, 'k') && campoBusca.current) {
        e.preventDefault()
        campoBusca.current?.focus()
        return
      }
      const a = avisoAtual.current
      if (a?.aprender && atalhoDeAprender(e)) {
        e.preventDefault()
        a.aprender()
        return
      }
      if (!a?.desfazer || !atalho(e, 'z')) return
      e.preventDefault()
      // Um segundo Ctrl+Z antes do próximo render não desfaz de novo.
      avisoAtual.current = null
      a.desfazer()
      setAviso(null)
    }
    document.addEventListener('keydown', tecla)
    return () => document.removeEventListener('keydown', tecla)
  }, [])

  // A altura do cabeçalho fixo vira o scroll-padding-top da página: a
  // linha que recebe o foco nunca fica embaixo dele (WCAG 2.4.11). Medida,
  // e não fixa no CSS, porque a faixa de problema e as telas estreitas
  // mudam essa altura.
  useLayoutEffect(() => {
    const el = topo.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const raiz = document.documentElement
    const medir = () => raiz.style.setProperty('--altura-topo', `${el.offsetHeight}px`)
    const obs = new ResizeObserver(medir)
    obs.observe(el)
    medir()
    return () => {
      obs.disconnect()
      raiz.style.removeProperty('--altura-topo')
    }
  }, [])

  useEffect(() => {
    if (focarNoTitulo) titulo.current?.focus()
  }, [focarNoTitulo])

  // O destaque do ditado que acabou de chegar dura 10 s, ou até a pessoa abrir
  // uma linha ou trocar de vista. E a linha entra na tela: o cabeçalho fixo não
  // a cobre (scroll-padding-top).
  useEffect(() => {
    if (chegou === null) return
    const t = setTimeout(() => setChegou((c) => (c === chegou ? null : c)), 10_000)
    const linha = Array.from(lista.current?.querySelectorAll<HTMLElement>('[data-item]') ?? []).find((el) => el.dataset.item === chegou)
    linha?.scrollIntoView?.({ block: 'nearest' })
    return () => clearTimeout(t)
  }, [chegou])

  // O foco só muda de lugar se estava perdido (a linha que o tinha saiu
  // da página) ou numa linha ou botão da própria lista. Nunca é tirado de
  // um campo em que a pessoa digita — a busca, ou o editor de outra linha
  // que ela abriu enquanto a resposta não vinha (revisão de 29-set-2026:
  // o editor também fica dentro da lista, e contava como livre).
  useEffect(() => {
    const alvo = focarDepois.current
    if (!alvo) return
    focarDepois.current = null
    const ativo = document.activeElement
    const livre = !ativo || ativo === document.body || (Boolean(lista.current?.contains(ativo)) && !dentroDeCampo(ativo))
    if (!livre) return
    const linha = alvo.id === null ? null : linhaDe(lista.current, alvo.id)
    const destino = linha ?? campoBusca.current
    destino?.focus()
  })

  // Depois de entrar de novo: a faixa saiu, e o foco que estava nela caiu no
  // <body>. Vai para o editor aberto (é para salvar que a pessoa entrou) ou,
  // sem ele, para a primeira linha com texto não salvo. Quem já estava
  // noutro lugar (digitando no editor, por exemplo) fica onde está.
  useEffect(() => {
    if (!focarRascunho.current) return
    focarRascunho.current = false
    const ativo = document.activeElement
    if (ativo && ativo !== document.body && ativo.isConnected) return
    const rs = rascunhosAtuais.current
    const naoSalvo = (id: string) => !naoTocado(rs[id]!, resultadoAtual.current?.itens.find((i) => i.id === id))
    const id = abertoAtual.current ?? Object.keys(rs).find(naoSalvo)
    const item = Array.from(lista.current?.querySelectorAll<HTMLElement>('[data-item]') ?? []).find((el) => el.dataset.item === id)
    item?.querySelector<HTMLElement>('textarea, [data-linha]')?.focus()
  })

  function falhar(e: unknown) {
    const p = problemaDe(e)
    if (p) acender(p)
    else avisar(`Não deu certo: ${mensagem(e)}`)
  }

  const mudarItens = (f: (itens: Ditado[]) => Ditado[]) => setResultado((r) => r && { ...r, itens: f(r.itens) })
  const trocarItem = (novo: Ditado) => mudarItens((itens) => itens.map((i) => (i.id === novo.id ? novo : i)))
  const tirar = (id: string) => mudarItens((itens) => itens.filter((i) => i.id !== id))
  const itemDe = (id: string) => resultado?.itens.find((i) => i.id === id)

  // largar descarta o rascunho de uma linha que se fecha sem que a pessoa
  // tenha mexido nele — senão a linha virava "não salvo" só por ter sido
  // aberta, e reabria com o texto velho (revisão de 29-set-2026).
  function largar(id: string | null) {
    if (id === null) return
    const item = itemDe(id)
    setRascunhos((rs) => {
      const r = rs[id]
      return r && naoTocado(r, item) ? semChave(rs, id) : rs
    })
  }

  function trocarVista(v: Vista) {
    setConfigurando(false)
    if (v === vista) return
    largar(aberto)
    setVista(v)
    setTexto('')
    setBusca('')
    setAberto(null)
    setChegou(null)
  }

  // Ir às Configurações fecha o editor aberto, como trocar de vista: o
  // rascunho tocado fica, com a etiqueta "não salvo".
  function abrirConfiguracoes() {
    if (configurando) return
    largar(aberto)
    setAberto(null)
    setConfigurando(true)
    setAbriuConfiguracoes(true)
  }

  function abrir(item: Ditado) {
    if (aberto !== item.id) largar(aberto)
    setRascunhos((rs) => (item.id in rs ? rs : { ...rs, [item.id]: novoRascunho(item) }))
    setAberto(item.id)
    setChegou(null)
  }

  // fechar é Cancelar ou Esc: a pessoa desistiu da edição.
  function fechar(id: string) {
    setRascunhos((rs) => semChave(rs, id))
    setAberto((a) => (a === id ? null : a))
  }

  function mudar(item: Ditado, novoTexto: string) {
    setRascunhos((rs) => ({ ...rs, [item.id]: { ...(rs[item.id] ?? novoRascunho(item)), texto: novoTexto } }))
  }

  async function salvar(item: Ditado) {
    const r = rascunhos[item.id] ?? novoRascunho(item)
    const enviado = r.texto
    // A nota de "sem conexão" é do salvar anterior: este tenta de novo.
    if (r.semConexao) setRascunhos((rs) => (rs[item.id] ? { ...rs, [item.id]: { ...rs[item.id]!, semConexao: false } } : rs))
    let s: Salvamento
    try {
      s = await salvarSobre(cliente, item.id, r.base, r.textoBase, enviado)
    } catch (e) {
      // Na nuvem, a edição que não chegou não fica guardada em lugar
      // nenhum: o editor diz isso, e o texto continua na tela.
      if (e instanceof ForaDoAr && textos.edicaoSemConexao) {
        setRascunhos((rs) => ({ ...rs, [item.id]: { ...(rs[item.id] ?? r), semConexao: true } }))
      }
      return falhar(e)
    }
    if (s.tipo === 'conflito') {
      // O texto mudou em outra aba (ou o ditado foi para a lixeira lá). O
      // rascunho passa a se basear no atual — o próximo salvar é uma
      // escolha consciente de substituir — e guarda a nota, que aparece
      // de novo se a pessoa fechar e reabrir a linha.
      const atual = s.atual
      trocarItem(atual)
      setRascunhos((rs) => ({
        ...rs,
        [item.id]: { texto: rs[item.id]?.texto ?? enviado, base: atual.version, textoBase: atual.final_text, conflito: true },
      }))
      return
    }
    const salvo = s.item
    setRascunhos((rs) => {
      const agora = rs[item.id]
      if (agora && agora.texto !== enviado) {
        return { ...rs, [item.id]: { ...agora, base: salvo.version, textoBase: salvo.final_text, conflito: false, semConexao: false } }
      }
      return semChave(rs, item.id)
    })
    // A resposta pode chegar com outra linha já aberta: só fecha o editor
    // se ele ainda for o desta (revisão de 29-set-2026).
    setAberto((a) => (a === item.id ? null : a))
    if (salvo.deleted_at) {
      // Outra aba pôs o ditado na lixeira, e a API gravou mesmo assim.
      // Ele não é do histórico: sai daqui, e o aviso diz onde ele está.
      tirar(item.id)
      avisar(`Texto salvo, mas este ditado está na lixeira: foi movido ${textos.outroLugar}. Restaure pela Lixeira para vê-lo no histórico.`)
      return
    }
    trocarItem(salvo)
    const sugestoes = s.sugestoes
    // Sem aprendiz (a nuvem), nada é sugerido: aprender é só no PC.
    const aprendiz = ambiente.aprendiz
    if (sugestoes.length === 0 || !aprendiz) return avisar('Texto salvo.')
    // A edição é uma troca de termo que o dicionário pode aprender (ADR-017):
    // o mesmo aviso de "Texto salvo." pergunta, com o botão Aprender. Se
    // ele some sem o clique, nada foi aprendido.
    mostrarAviso({
      texto: aprendiz.pergunta(sugestoes),
      aprender: () => void aprender(aprendiz, salvo, sugestoes),
      efeito: aprendiz.efeito,
    })
  }

  // aprender é o clique em Aprender (ou o Alt+A): uma troca por pedido, na
  // ordem do texto, e cada uma diz por si se foi. Com duas trocas, uma pode
  // valer e a outra ser recusada ("SF já vira ECD"), e a pessoa precisa ver
  // as duas coisas. O aviso é trocado pelo resultado, com o Desfazer do que
  // valeu.
  async function aprender(aprendiz: Aprendiz, item: Ditado, sugestoes: Sugestao[]) {
    if (aprendendo.current) return
    aprendendo.current = true
    const feitas: Aprendida[] = []
    const recusas: string[] = []
    let caiu = false
    try {
      for (const s of sugestoes) {
        try {
          feitas.push(await aprendiz.aprender(item.id, s))
        } catch (e) {
          // Sessão perdida ou Agent fora do ar: as outras trocas também não
          // dariam certo, e a faixa do topo já diz o que fazer.
          const p = problemaDe(e)
          if (p) {
            acender(p)
            caiu = true
            break
          }
          recusas.push(mensagem(e))
        }
      }
    } finally {
      aprendendo.current = false
    }
    if (feitas.length > 0) setAprendizado((n) => n + 1)
    if (caiu) return
    // focoNoTexto: o foco de quem acabou de clicar em Aprender não vai para o
    // Desfazer da pílula nova (um Enter ou um Espaço a mais desfaria o que
    // acabou de ser aprendido): vai para o texto, e o Desfazer fica a um Tab.
    mostrarAviso({
      texto: aprendiz.resultadoDeAprender(feitas, recusas),
      desfazer: feitas.length > 0 ? () => void desfazerAprendido(aprendiz, feitas) : undefined,
      focoNoTexto: true,
    })
  }

  // desfazerAprendido tira o que o aviso acabou de aprender. O Desfazer
  // das Configurações é o de uma linha só (AprendidoNoPainel).
  async function desfazerAprendido(aprendiz: Aprendiz, feitas: Aprendida[]) {
    const desfeitas: Aprendida[] = []
    const recusas: string[] = []
    for (const a of feitas) {
      try {
        desfeitas.push(await aprendiz.desfazer(a.variante))
      } catch (e) {
        const p = problemaDe(e)
        if (p) {
          acender(p)
          break
        }
        recusas.push(mensagem(e))
      }
    }
    if (desfeitas.length > 0) setAprendizado((n) => n + 1)
    avisar(aprendiz.resultadoDeDesfazer(desfeitas, recusas))
  }

  // chegouUmDitado é o ditado que o celular acabou de gravar (ADR-021): entra
  // no topo do histórico, em destaque, com o Copiar que toda linha tem. Numa
  // busca, na Lixeira ou nas Configurações ele não entra na lista que está na
  // tela, e o aviso diz onde ele está. nota e aviso: o que o app fez sozinho e
  // a frase de o Polish não ter agido.
  function chegouUmDitado(d: Ditado, extra: { nota?: string; aviso?: string }) {
    const r = resultadoAtual.current
    const naLista = !configurando && r !== null && !r.erro && r.vista === 'historico' && r.busca === '' && vista === 'historico'
    if (naLista) {
      setResultado((x) =>
        x && !x.erro && x.vista === 'historico' && x.busca === '' ? { ...x, itens: inserirEmOrdem(x.itens, d, 'created_at', Boolean(x.proximo)) } : x,
      )
      setChegou(d.id)
    }
    avisar([naLista ? 'Ditado no topo do histórico.' : 'Ditado salvo no histórico.', extra.aviso, extra.nota].filter(Boolean).join(' '))
  }

  // copiar nunca grava texto vazio: a área de transferência pode ter o
  // texto que o Agent deixou lá quando a inserção falhou (revisão de
  // 29-set-2026).
  function copiar(textoACopiar: string) {
    if (textoACopiar.trim() === '') return
    const area = navigator.clipboard
    if (!area) return avisar('Não consegui copiar. Selecione o texto e use Ctrl+C.')
    area.writeText(textoACopiar).then(
      () => avisar('Copiado.'),
      () => avisar('Não consegui copiar. Selecione o texto e use Ctrl+C.'),
    )
  }

  async function moverParaLixeira(item: Ditado) {
    const buscaDeOrigem = resultadoAtual.current?.busca ?? ''
    try {
      await cliente.moverParaLixeira(item.id)
    } catch (e) {
      return falhar(e)
    }
    const atual = resultadoAtual.current
    focarDepois.current = { id: atual ? vizinho(atual.itens, item.id) : null }
    tirar(item.id)
    setRascunhos((rs) => semChave(rs, item.id))
    setAberto((a) => (a === item.id ? null : a))
    avisar('Movido para a lixeira.', () => desfazerLixeira(item, buscaDeOrigem))
  }

  // desfazerLixeira devolve o ditado ao lugar dele sem recarregar a lista:
  // recarregar trazia só a primeira página, e um ditado de uma página mais
  // antiga não voltava à tela (revisão de 29-set-2026).
  function desfazerLixeira(item: Ditado, buscaDeOrigem: string) {
    cliente.restaurar(item.id).then(() => {
      const r = resultadoAtual.current
      if (r?.vista === 'lixeira') {
        // A pessoa já foi para a Lixeira: lá, ele não está mais.
        focarDepois.current = { id: vizinho(r.itens, item.id) }
        tirar(item.id)
      } else if (r && (r.busca === '' || r.busca === buscaDeOrigem)) {
        // Numa busca diferente da de onde ele saiu, não dá para saber se
        // ele casa com ela: fica de fora até a próxima carga.
        setResultado((x) => (x && x.vista === 'historico' ? { ...x, itens: inserirEmOrdem(x.itens, item, 'created_at', Boolean(x.proximo)) } : x))
        focarDepois.current = { id: item.id }
      }
      avisar('Ditado de volta ao histórico.')
    }, falhar)
  }

  async function restaurar(item: Ditado) {
    try {
      await cliente.restaurar(item.id)
    } catch (e) {
      return falhar(e)
    }
    const atual = resultadoAtual.current
    focarDepois.current = { id: atual ? vizinho(atual.itens, item.id) : null }
    tirar(item.id)
    avisar('Restaurado para o histórico.')
  }

  // excluirDeVez é o "Excluir definitivamente" da Lixeira (só na nuvem). Não
  // tem Desfazer: a linha pediu a confirmação antes. Devolve se excluiu.
  async function excluirDeVez(item: Ditado): Promise<boolean> {
    if (!ambiente.excluirDeVez) return false
    try {
      await ambiente.excluirDeVez(item.id)
    } catch (e) {
      falhar(e)
      return false
    }
    const atual = resultadoAtual.current
    focarDepois.current = { id: atual ? vizinho(atual.itens, item.id) : null }
    tirar(item.id)
    avisar('Ditado excluído para sempre.')
    return true
  }

  // escolher é a escolha do selo. O 409 quer dizer que o ditado mudou
  // enquanto a pessoa olhava (outra aba, ou outro aparelho já escolheu): ele
  // é lido de novo, e o selo mostra o que vale agora.
  async function escolher(item: Ditado, escolha: EscolhaDoSelo) {
    if (!ambiente.resolverConflito) return
    let novo: Ditado
    try {
      novo = await ambiente.resolverConflito(item.id, item.version, escolha)
    } catch (e) {
      if (!(e instanceof Conflito)) return falhar(e)
      let atual: Ditado
      try {
        atual = await cliente.obter(item.id)
      } catch (e2) {
        return falhar(e2)
      }
      trocarItem(atual)
      return avisar(
        atual.versao_em_conflito
          ? `Este ditado mudou ${textos.outroLugar}. Confira as versões e escolha de novo.`
          : `A versão já foi escolhida ${textos.outroLugar}.`,
      )
    }
    trocarItem(novo)
    // O editor aberto mostra o texto que ficou, a não ser que a pessoa já
    // tenha escrito nele: aí o texto dela fica, sobre a versão nova.
    setRascunhos((rs) => {
      const r = rs[item.id]
      if (!r) return rs
      return { ...rs, [item.id]: naoTocado(r, item) ? novoRascunho(novo) : { ...r, base: novo.version, textoBase: novo.final_text } }
    })
    avisar('Ficou a versão escolhida.')
  }

  // carregarMais pede a página seguinte da consulta que está NA TELA —
  // busca e cursor do mesmo resultado. Com a busca do campo, que pode ser
  // de uma consulta que ainda não respondeu, o cursor de uma lista
  // trazia a página de outra (revisão de 29-set-2026).
  //
  // A página vale para a lista enquanto a CONSULTA for a mesma, não a
  // chave: o clique que devolve o foco à janela dispara uma recarga, que
  // troca a chave, e a página pedida no mesmo clique era jogada fora em
  // silêncio (revisão de 29-set-2026).
  async function carregarMais() {
    const atual = resultado
    if (!atual?.proximo || carregandoMais) return
    setCarregandoMais(true)
    const vale = (r: Resultado | null): r is Resultado => r !== null && !r.erro && mesmaConsulta(r, atual)
    try {
      const p = await cliente.listar({ lixeira: atual.vista === 'lixeira', busca: atual.busca, cursor: atual.proximo })
      const agora = resultadoAtual.current
      if (vale(agora)) {
        const ja = new Set(agora.itens.map((i) => i.id))
        // O foco vai para a primeira linha que chegou: o botão pode sumir
        // (última página), e o foco cairia no início da página.
        focarDepois.current = { id: p.items.find((i) => !ja.has(i.id))?.id ?? null }
      }
      setResultado((r) => {
        if (!vale(r)) return r
        const juntos = { ...r, itens: anexarPagina(r.itens, p.items), proximo: p.next_cursor }
        return { ...juntos, anuncio: anunciar(juntos) }
      })
    } catch (e) {
      falhar(e)
    } finally {
      setCarregandoMais(false)
    }
  }

  function tentarDeNovo() {
    setProblema(null)
    setRecarga((n) => n + 1)
    campoBusca.current?.focus()
  }

  // Setas para cima e para baixo andam entre as linhas, como numa lista
  // de verdade; Tab continua passando por tudo.
  function navegar(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    if (!(e.target instanceof HTMLElement) || !e.target.hasAttribute('data-linha')) return
    const linhas = Array.from(lista.current?.querySelectorAll<HTMLElement>('[data-linha]') ?? [])
    const alvo = linhas[linhas.indexOf(e.target) + (e.key === 'ArrowDown' ? 1 : -1)]
    if (alvo) {
      e.preventDefault()
      alvo.focus()
    }
  }

  // Com a ligação caída, a lista que está na tela fica, mesmo que a vista
  // pedida seja outra: é nela que estão os rascunhos.
  const visivel = resultado && (resultado.vista === vista || problema) ? resultado : null
  const naLixeira = vista === 'lixeira'
  // naoSalvos: o que a pessoa escreveu e ainda não salvou (a faixa da sessão
  // o oferece para copiar; as Configurações avisam antes de um Sair o jogar fora).
  const naoSalvos = textosNaoSalvos(rascunhos, resultado?.itens)

  // campo é a busca. Na nuvem, o gravador do ditado a recebe e a põe ao lado do
  // microfone.
  const campo = (
    <input
      ref={campoBusca}
      type="search"
      className="busca"
      placeholder={naLixeira ? 'Buscar na lixeira' : 'Buscar no histórico'}
      aria-label={naLixeira ? 'Buscar na lixeira' : 'Buscar no histórico'}
      aria-keyshortcuts="Control+K"
      value={texto}
      onChange={(e) => setTexto(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && texto) {
          e.preventDefault()
          setTexto('')
          setBusca('')
        }
      }}
    />
  )

  let conteudo
  if (!visivel) {
    conteudo = <p className="estado atrasado">Carregando…</p>
  } else if (visivel.erro) {
    conteudo = (
      <div className="estado">
        <p>
          Não consegui carregar {visivel.vista === 'lixeira' ? 'a lixeira' : 'o histórico'}: {visivel.erro}
        </p>
        {!problema && (
          <button type="button" className="botao" onClick={() => setRecarga((n) => n + 1)}>
            Tentar de novo
          </button>
        )}
      </div>
    )
  } else if (visivel.itens.length === 0) {
    // A mensagem é da busca que produziu ESTE resultado, não da que está
    // no campo e talvez ainda não tenha respondido (revisão de
    // 29-set-2026).
    // A Aurora aparece quando o painel fala de si — nada ditado ainda,
    // lixeira vazia —, não quando a busca só não achou. Embaixo dela vem
    // sempre um título: sem ele, na lixeira, o retrato dominava uma frase
    // cinza de cinco palavras.
    conteudo = (
      <div className="estado">
        {visivel.busca ? (
          <p>Nada encontrado para “{visivel.busca}”.</p>
        ) : visivel.vista === 'lixeira' ? (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">A lixeira está vazia.</p>
          </>
        ) : (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">Nenhum ditado ainda.</p>
            <p>{textos.vazio}</p>
          </>
        )}
      </div>
    )
  } else {
    // Na Lixeira, o dia e a hora são os da exclusão, na ordem em que a
    // API manda: o que acabou de ser apagado aparece em "Hoje", no topo
    // (revisão de 29-set-2026).
    const campo = campoDaOrdem(visivel.vista)
    const grupos = agruparPorDia(visivel.itens, visivel.hoje, campo)
    conteudo = (
      <div ref={lista} onKeyDown={navegar}>
        {grupos.map((g) => (
          <section key={g.chave} className="grupo" aria-labelledby={`dia-${g.chave}`}>
            <h2 id={`dia-${g.chave}`} className="dia">
              {g.rotulo}
            </h2>
            <ul className="itens">
              {g.itens.map((item) =>
                visivel.vista === 'lixeira' ? (
                  <LinhaDaLixeira
                    key={item.id}
                    item={item}
                    aoRestaurar={() => void restaurar(item)}
                    aoExcluirDeVez={ambiente.excluirDeVez && (() => excluirDeVez(item))}
                  />
                ) : (
                  <ItemDitado
                    key={item.id}
                    item={item}
                    aberto={aberto === item.id}
                    rascunho={rascunhos[item.id]}
                    aoAbrir={() => abrir(item)}
                    aoFechar={() => fechar(item.id)}
                    aoMudar={(t) => mudar(item, t)}
                    aoSalvar={() => salvar(item)}
                    aoCopiar={copiar}
                    aoMoverParaLixeira={() => void moverParaLixeira(item)}
                    outroLugar={textos.outroLugar}
                    edicaoSemConexao={textos.edicaoSemConexao}
                    aoEscolher={ambiente.resolverConflito && ((escolha) => escolher(item, escolha))}
                    novo={chegou === item.id}
                  />
                ),
              )}
            </ul>
          </section>
        ))}
        {visivel.proximo && (
          <button type="button" className="botao mais" disabled={carregandoMais} onClick={() => void carregarMais()}>
            {carregandoMais ? 'Carregando…' : 'Mostrar mais antigos'}
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="painel">
      <header className="topo" ref={topo}>
        {problema && (
          <Faixa
            problema={problema}
            aoTentarDeNovo={tentarDeNovo}
            textoNaoSalvo={problema === 'sem-sessao' ? naoSalvos.join('\n\n') : undefined}
            aoEntrarDeNovo={entrouDeNovo}
            ditadoNaoEnviado={problema === 'sem-sessao' && ditadoPendente}
          />
        )}
        <div className="barra">
          <span className="nome">
            <Marca />
            Aurora Whisper
          </span>
          <div className="vistas" role="group" aria-label="Mostrar">
            <button type="button" aria-pressed={!configurando && vista === 'historico'} onClick={() => trocarVista('historico')}>
              Histórico
            </button>
            <button type="button" aria-pressed={!configurando && naLixeira} onClick={() => trocarVista('lixeira')}>
              Lixeira
            </button>
            <button type="button" aria-pressed={configurando} onClick={abrirConfiguracoes}>
              Configurações
            </button>
          </div>
        </div>
        {Ditar ? (
          // Na nuvem, o microfone fica ao lado da busca, e gravando a troca
          // por uma faixa (ADR-021). Montado também nas Configurações: uma
          // gravação em curso, ou um áudio que espera, não se perde ao trocar
          // de tela.
          <Ditar
            busca={configurando ? null : campo}
            escondido={configurando}
            aoChegar={chegouUmDitado}
            aoAudioPendente={(pendente) => {
              audioPendente.current = pendente
              setDitadoPendente(pendente)
            }}
            aoSemSessao={() => acender('sem-sessao')}
            abrirConfiguracoes={abrirConfiguracoes}
            avisar={avisar}
          />
        ) : (
          !configurando && campo
        )}
      </header>

      <main className="conteudo" aria-busy={!configurando && carregando}>
        <h1 className="so-leitor" ref={titulo} tabIndex={focarNoTitulo ? -1 : undefined}>
          {configurando ? 'Configurações' : (visivel?.vista ?? vista) === 'lixeira' ? 'Lixeira' : 'Histórico'}
        </h1>
        {/* A região viva existe sempre e anuncia cada carga — quantos
            ditados, nada encontrado, erro. Quem usa leitor de tela, com o
            foco na busca, não ouvia se ela tinha achado algo (WCAG 4.1.3,
            revisão de 29-set-2026). */}
        <p className="so-leitor anuncio" role="status">
          {(!configurando && visivel?.anuncio) || ''}
        </p>
        {!configurando && conteudo}
        {abriuConfiguracoes && (
          <Configuracoes
            visivel={configurando}
            aoPerderLigacao={falhar}
            avisar={avisar}
            aprendizado={aprendizado}
            comTextoNaoSalvo={naoSalvos.length > 0}
            recarregarHistorico={() => setRecarga((n) => n + 1)}
          />
        )}
      </main>

      <Aviso aviso={aviso} aoSumir={sumirAviso} />
    </div>
  )
}

function linhaDe(lista: HTMLElement | null, id: string): HTMLElement | null {
  const item = Array.from(lista?.querySelectorAll<HTMLElement>('[data-item]') ?? []).find((el) => el.dataset.item === id)
  return item?.querySelector<HTMLElement>('[data-linha]') ?? null
}

// A linha da Lixeira não abre nada: só o Restaurar age. Por isso não tem
// o fundo de passar o ponteiro das linhas do histórico, e mostra o texto
// inteiro — não há editor para lê-lo (revisão de 29-set-2026).
//
// Na nuvem, ela tem também o "Excluir definitivamente" (ADR-019, F4), que
// pergunta na própria linha, sem janela: "Excluir para sempre?", com o foco
// no Cancelar, que é o lado seguro.
function LinhaDaLixeira({ item, aoRestaurar, aoExcluirDeVez }: { item: Ditado; aoRestaurar(): void; aoExcluirDeVez?(): Promise<boolean> }) {
  const quando = item.deleted_at ?? item.created_at
  const [confirmando, setConfirmando] = useState(false)
  const [excluindo, setExcluindo] = useState(false)
  const botaoExcluir = useRef<HTMLButtonElement>(null)
  const botaoCancelar = useRef<HTMLButtonElement>(null)
  const voltarFoco = useRef(false)
  const idTexto = `texto-${item.id}`
  const idPergunta = `excluir-${item.id}`

  useEffect(() => {
    if (confirmando) botaoCancelar.current?.focus()
    else if (voltarFoco.current) {
      voltarFoco.current = false
      botaoExcluir.current?.focus()
    }
  }, [confirmando])

  function cancelar() {
    voltarFoco.current = true
    setConfirmando(false)
  }

  async function excluir() {
    if (excluindo || !aoExcluirDeVez) return
    setExcluindo(true)
    // Excluiu: a linha sai, e o Painel leva o foco à vizinha. Não excluiu:
    // a pergunta fica, e o aviso diz o porquê.
    if (!(await aoExcluirDeVez())) setExcluindo(false)
  }

  return (
    <li className="item" data-item={item.id}>
      <div className="linha estatica">
        <time className="hora" dateTime={quando}>
          {formatarHora(quando)}
        </time>
        <span className="texto-e-meta">
          <TextoDaLinha item={item} id={idTexto} />
          <span className="desde">ditado em {formatarCurta(item.created_at)}</span>
        </span>
        <span className="acoes-linha">
          <button type="button" className="botao" data-linha aria-describedby={idTexto} onClick={aoRestaurar}>
            Restaurar
          </button>
          {aoExcluirDeVez && !confirmando && (
            <button ref={botaoExcluir} type="button" className="botao" aria-describedby={idTexto} onClick={() => setConfirmando(true)}>
              Excluir definitivamente
            </button>
          )}
        </span>
      </div>
      {confirmando && (
        <div
          className="confirmar-linha"
          role="group"
          aria-labelledby={idPergunta}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault()
              cancelar()
            }
          }}
        >
          <p id={idPergunta}>Excluir para sempre? Não dá para desfazer.</p>
          <button type="button" className="botao risco" aria-disabled={excluindo || undefined} aria-describedby={idTexto} onClick={() => void excluir()}>
            {excluindo ? 'Excluindo…' : 'Excluir'}
          </button>
          <button ref={botaoCancelar} type="button" className="botao" onClick={cancelar}>
            Cancelar
          </button>
        </div>
      )}
    </li>
  )
}
