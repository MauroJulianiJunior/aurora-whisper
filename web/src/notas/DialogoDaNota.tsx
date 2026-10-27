import { type KeyboardEvent, type ReactNode, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Conflito, ErroDaApi, problemaDe } from '../api'
import { formatarCurta, formatarHora } from '../datas'
import { BotaoComMenu } from './BotaoComMenu'
import { CamposDaNota } from './CamposDaNota'
import { type TipoDaLinha, escreverCorpo, garantirUma, lerCorpo, nomeDaNota, normalizarMarcados } from './corpo'
import { copiarParaWhatsApp } from './copiarParaWhatsApp'
import { EscolherMarcadores } from './EscolherMarcadores'
import { Icone } from './Icone'
import { PaletaDeCores } from './PaletaDeCores'
import { dispensarSugestao, sugestaoDispensada } from './preferencias'
import { prenderOFoco } from './prenderOFoco'
import { sugerirMarcador } from './sugestao'
import type { ClienteDeNotas, CorDaNota, EscolhaDoConflito, Marcador, MudancaDaNota, Nota } from './tipos'
import { useAdiado } from './useAdiado'
import { useEditorDeLinhas } from './useEditorDeLinhas'
import { useTelaEstreita } from './useTelaEstreita'

// A espera do salvar automático depois da última tecla.
const esperaDoSalvar = 800
// A espera da sugestão de marcador depois da última tecla: o texto da nota tem de
// assentar antes de a regra olhar para ele.
const esperaDaSugestao = 600

// FimDaNota é como a janela termina. O Notas decide o resto (a lista, o aviso, o
// Desfazer, o foco).
export type FimDaNota = { tipo: 'fechou' | 'arquivou' | 'desarquivou' | 'lixeira'; nota: Nota }

interface Props {
  nota: Nota
  cliente: ClienteDeNotas
  // marcadores: todos, na ordem do nome (as etiquetas da nota e o menu do botão).
  marcadores: readonly Marcador[]
  // notasParaSugerir: as notas da tela principal, de onde sai a sugestão de marcador.
  notasParaSugerir: readonly Nota[]
  // criarMarcador cria um marcador novo (o Notas o guarda). Devolve o marcador, ou a
  // frase de erro para o menu.
  criarMarcador(nome: string): Promise<Marcador | string>
  avisar(texto: ReactNode): void
  // aoPerderLigacao: a falha foi da ligação (sem sessão, Agent fora do ar); o
  // Painel acende a faixa do topo.
  aoPerderLigacao(e: unknown): void
  aoTerminar(fim: FimDaNota): void
}

// O que a janela edita, e o que ela já salvou.
interface Campos {
  titulo: string
  corpo: string
  cor: CorDaNota | null
  fixada: boolean
  arquivada: boolean
}

type Resultado = 'ok' | 'conflito' | 'erro'
type Estado = { tipo: 'ok' } | { tipo: 'salvando' } | { tipo: 'erro'; mensagem: string } | { tipo: 'conflito' } | { tipo: 'lixeira' }

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Os tipos de lista do Formatar separam a linha de texto corrido em itens
// (N5): o Agent responde, e a linha vira um item por linha.
const separaEmItens = (t: TipoDaLinha) => t === 'marcador' || t === 'tarefa' || t === 'numerada'

const tiposDoFormatar: [TipoDaLinha, string][] = [
  ['marcador', 'Lista'],
  ['tarefa', 'Lista de marcar'],
  ['numerada', 'Numerada'],
  ['titulo', 'Título'],
  ['paragrafo', 'Parágrafo'],
]

// DialogoDaNota é a nota aberta (ADR-022, "A tela"): uma janela sobre a grade,
// com os colchetes verdes do editor do histórico. Salva sozinha, um instante
// depois da última tecla e ao fechar; cada gravação leva a versão em que a
// edição começou (base_version), e um 409 não perde o texto: ele continua na
// tela, com a nota vermelha do editor do histórico, até a pessoa salvar por
// cima.
export function DialogoDaNota({ nota, cliente, marcadores, notasParaSugerir, criarMarcador, avisar, aoPerderLigacao, aoTerminar }: Props) {
  const [titulo, setTitulo] = useState(nota.titulo)
  const [cor, setCor] = useState<CorDaNota | null>(nota.cor)
  const [fixada, setFixada] = useState(nota.fixada)
  const [arquivada, setArquivada] = useState(nota.arquivada)
  const [ultima, setUltima] = useState(nota)
  const [estado, setEstado] = useState<Estado>({ tipo: 'ok' })
  // escolhendo: a escolha do selo está no Agent (os dois botões esperam).
  const [escolhendo, setEscolhendo] = useState(false)
  const idDoSelo = useId()
  const editor = useEditorDeLinhas(nota.corpo)
  const { corpo, marcados } = editor
  const dialogo = useRef<HTMLDivElement>(null)
  const estreita = useTelaEstreita()
  // O nome da janela é o da nota quando ela abriu: não muda a cada tecla.
  const [nome] = useState(() => nomeDaNota(nota.titulo, lerCorpo(nota.corpo)))

  // Os campos de agora, lidos pela fila de gravação (que roda depois de
  // renders) sem ficar com o valor velho de um fechamento.
  const agora = useRef<Campos>({ titulo, corpo, cor, fixada, arquivada })
  useLayoutEffect(() => {
    agora.current = { titulo, corpo, cor, fixada, arquivada }
  })
  // salvo: o que o Agent tem, na versão em que a próxima gravação se baseia.
  const salvo = useRef<Campos & { versao: number }>({
    titulo: nota.titulo,
    corpo: nota.corpo,
    cor: nota.cor,
    fixada: nota.fixada,
    arquivada: nota.arquivada,
    versao: nota.version,
  })
  const ultimaRef = useRef(nota)
  // fila: as gravações uma depois da outra; cada uma manda o que mudou desde a
  // anterior, e a versão que a anterior devolveu.
  const fila = useRef<Promise<unknown>>(Promise.resolve())
  // parado: depois de um conflito, nada grava sozinho (nem o salvar
  // automático): só o Salvar por cima, ou o fechar.
  const parado = useRef<'conflito' | 'lixeira' | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const terminou = useRef(false)

  // O que difere do que o Agent tem.
  function mudancaPendente(): MudancaDaNota {
    const a = agora.current
    const s = salvo.current
    const m: MudancaDaNota = {}
    if (a.titulo !== s.titulo) m.titulo = a.titulo
    if (a.corpo !== s.corpo) m.corpo = a.corpo
    if (a.cor !== s.cor) m.cor = a.cor
    if (a.fixada !== s.fixada) m.fixada = a.fixada
    if (a.arquivada !== s.arquivada) m.arquivada = a.arquivada
    return m
  }

  function salvar(): Promise<Resultado> {
    const passo = async (): Promise<Resultado> => {
      if (parado.current === 'lixeira') return 'conflito'
      const mudanca = mudancaPendente()
      if (Object.keys(mudanca).length === 0) return 'ok'
      const enviado = { ...agora.current }
      setEstado({ tipo: 'salvando' })
      try {
        const nova = await cliente.editarNota(nota.id, salvo.current.versao, mudanca)
        // O salvo é o que foi enviado, e não o que voltou: se o Agent devolvesse
        // o texto de outro jeito, a janela ficaria "suja" para sempre.
        salvo.current = { ...enviado, versao: nova.version }
        ultimaRef.current = nova
        setUltima(nova)
        setEstado({ tipo: 'ok' })
        return 'ok'
      } catch (e) {
        if (e instanceof Conflito) return await aposConflito()
        if (problemaDe(e)) aoPerderLigacao(e)
        setEstado({ tipo: 'erro', mensagem: mensagemDe(e) })
        return 'erro'
      }
    }
    const p = fila.current.then(passo, passo)
    fila.current = p
    return p
  }

  // aposConflito lê a nota de novo: a versão que o Agent tem agora passa a ser
  // a base, e o texto da janela continua sendo o da pessoa.
  async function aposConflito(): Promise<Resultado> {
    let atual: Nota
    try {
      atual = await cliente.obterNota(nota.id)
    } catch (e) {
      if (problemaDe(e)) aoPerderLigacao(e)
      setEstado({ tipo: 'erro', mensagem: mensagemDe(e) })
      return 'erro'
    }
    if (atual.deleted_at) {
      parado.current = 'lixeira'
      setEstado({ tipo: 'lixeira' })
      return 'conflito'
    }
    salvo.current = { ...salvo.current, versao: atual.version }
    ultimaRef.current = atual
    setUltima(atual)
    parado.current = 'conflito'
    setEstado({ tipo: 'conflito' })
    return 'conflito'
  }

  // escolherSelo é a escolha entre as duas versões da nota (ADR-022, N7). O que a
  // pessoa digitou e ainda não foi gravado vai antes, e a escolha parte da versão
  // em que o Agent está, na mesma fila das outras gravações. Com "usar a outra", o
  // título e o corpo da janela passam a ser os do selo; a versão que o Agent
  // devolve é a nova base. 409 (versão velha, ou a nota já não tem selo): lê a
  // nota de novo, como em toda gravação.
  async function escolherSelo(escolha: EscolhaDoConflito) {
    if (escolhendo || !ultimaRef.current.conflito || parado.current) return
    setEscolhendo(true)
    clearTimeout(timer.current)
    try {
      if ((await salvar()) !== 'ok') return
      const passo = async () => {
        if (parado.current) return
        try {
          const nova = await cliente.resolverConflitoDaNota(nota.id, salvo.current.versao, escolha)
          // O corpo da janela é o das linhas normalizadas: é o que o editor vai
          // escrever, e o "salvo" tem de ser igual a ele para a janela não ficar "suja".
          const linhas = garantirUma(normalizarMarcados(lerCorpo(nova.corpo)))
          const corpoNovo = escreverCorpo(linhas, { limpar: true })
          agora.current = { ...agora.current, titulo: nova.titulo, corpo: corpoNovo }
          salvo.current = { ...salvo.current, titulo: nova.titulo, corpo: corpoNovo, versao: nova.version }
          if (escolha === 'trocar') {
            setTitulo(nova.titulo)
            editor.aplicar(linhas)
          }
          ultimaRef.current = nova
          setUltima(nova)
          // O botão que tinha o foco sai da tela com o selo: o foco vai ao título.
          dialogo.current?.querySelector<HTMLElement>('input')?.focus()
          avisar(escolha === 'trocar' ? 'Ficou a outra versão.' : 'Ficou esta versão.')
        } catch (e) {
          if (e instanceof Conflito) {
            await aposConflito()
            return
          }
          if (problemaDe(e)) aoPerderLigacao(e)
          setEstado({ tipo: 'erro', mensagem: mensagemDe(e) })
        }
      }
      const p = fila.current.then(passo, passo)
      fila.current = p
      await p
    } finally {
      setEscolhendo(false)
    }
  }

  // mudarMarcador põe ou tira um marcador na nota, na mesma fila das gravações: a
  // versão da nota sobe a cada mudança de marcador, e uma gravação do texto que
  // saísse ao mesmo tempo levaria a versão velha e daria 409. Devolve se deu certo.
  function mudarMarcador(marcadorId: string, marcar: boolean): Promise<boolean> {
    const passo = async (): Promise<boolean> => {
      if (parado.current === 'lixeira') return false
      try {
        const nova = await (marcar ? cliente.marcarNota(nota.id, marcadorId) : cliente.desmarcarNota(nota.id, marcadorId))
        // A gravação seguinte parte da versão nova, a não ser que outra aba também tenha
        // mexido na nota (a versão pulou): aí ela dá 409, e o conflito é tratado como sempre.
        if (nova.version === salvo.current.versao + 1) salvo.current = { ...salvo.current, versao: nova.version }
        // O que a janela guarda como "a nota de agora" muda só nos marcadores; o texto
        // pendente (ainda não gravado) continua o da pessoa.
        ultimaRef.current = { ...ultimaRef.current, marcadores: nova.marcadores, version: nova.version, updated_at: nova.updated_at }
        setUltima(ultimaRef.current)
        return true
      } catch (e) {
        if (e instanceof Conflito) {
          await aposConflito()
        } else if (problemaDe(e)) {
          aoPerderLigacao(e)
        } else if (e instanceof ErroDaApi && e.status === 404) {
          avisar('Esse marcador não existe mais. Abra "Editar marcadores" para ver a lista.')
        } else {
          avisar(`Não consegui mudar o marcador: ${mensagemDe(e)}`)
        }
        return false
      }
    }
    const p = fila.current.then(passo, passo)
    fila.current = p
    return p
  }

  // alternarMarcador é a caixinha do menu; criarEMarcar, o "Criar marcador" dele.
  function alternarMarcador(m: Marcador, marcar: boolean) {
    void mudarMarcador(m.id, marcar)
  }

  async function criarEMarcar(nomeNovo: string): Promise<string | null> {
    const criado = await criarMarcador(nomeNovo)
    if (typeof criado === 'string') return criado
    await mudarMarcador(criado.id, true)
    return null
  }

  // Tirar pelo × da etiqueta: depois que a etiqueta sai da tela, o foco vai ao × vizinho,
  // ou ao botão de marcador se não sobrou etiqueta.
  const focarDepoisDeTirar = useRef<number | null>(null)
  async function tirarMarcador(id: string, posicao: number) {
    // O pedido é de uma vez: a ref vale antes da nota nova chegar, e o efeito abaixo a lê.
    focarDepoisDeTirar.current = posicao
    if (!(await mudarMarcador(id, false))) focarDepoisDeTirar.current = null
  }
  useEffect(() => {
    const posicao = focarDepoisDeTirar.current
    if (posicao === null) return
    focarDepoisDeTirar.current = null
    const restantes = dialogo.current?.querySelectorAll<HTMLElement>('.pe .tiravel button')
    ;(restantes?.item(Math.min(posicao, restantes.length - 1)) ?? focarBotaoDeMarcador())?.focus()
  }, [ultima])

  function focarBotaoDeMarcador(): HTMLElement | null {
    return dialogo.current?.querySelector<HTMLElement>('button[aria-label="Marcadores"]') ?? null
  }

  // A sugestão de marcador (N4): só em nota sem marcador, e que a pessoa não
  // dispensou. O texto olhado é o de um instante atrás (a regra não roda a cada tecla).
  const tituloAdiado = useAdiado(titulo, esperaDaSugestao)
  const corpoAdiado = useAdiado(corpo, esperaDaSugestao)
  const [dispensada, setDispensada] = useState(() => sugestaoDispensada(nota.id))
  const semMarcador = ultima.marcadores.length === 0
  const sugestao = useMemo(
    () =>
      dispensada || !semMarcador
        ? null
        : sugerirMarcador({ id: nota.id, titulo: tituloAdiado, corpo: corpoAdiado, marcadores: [] }, notasParaSugerir, marcadores),
    [dispensada, semMarcador, nota.id, tituloAdiado, corpoAdiado, notasParaSugerir, marcadores],
  )

  async function aceitarSugestao(id: string) {
    await mudarMarcador(id, true)
    focarBotaoDeMarcador()?.focus()
  }

  function recusarSugestao() {
    dispensarSugestao(nota.id)
    setDispensada(true)
    focarBotaoDeMarcador()?.focus()
  }

  // O salvar automático: depois de uma mudança, um instante e grava. Cor,
  // fixar e arquivar vão na hora; o texto espera a pessoa parar de digitar.
  useEffect(() => {
    if (parado.current) return
    const m = mudancaPendente()
    if (Object.keys(m).length === 0) return
    const soNaoTexto = m.titulo === undefined && m.corpo === undefined
    timer.current = setTimeout(() => void salvar(), soNaoTexto ? 0 : esperaDoSalvar)
    return () => clearTimeout(timer.current)
    // salvar e mudancaPendente leem refs: o que dispara é o que a pessoa mudou.
  }, [titulo, corpo, cor, fixada, arquivada]) // eslint-disable-line react-hooks/exhaustive-deps

  // Um estado de conflito liberado pela escolha (Salvar por cima, ou o segundo
  // Fechar) grava sobre a versão atual.
  function liberarEGravar(): Promise<Resultado> {
    if (parado.current === 'conflito') parado.current = null
    return salvar()
  }

  function terminar(fim: FimDaNota) {
    if (terminou.current) return
    terminou.current = true
    aoTerminar(fim)
  }

  async function fechar() {
    clearTimeout(timer.current)
    if (parado.current === 'lixeira') return terminar({ tipo: 'fechou', nota: ultimaRef.current })
    const r = await liberarEGravar()
    if (r === 'ok') terminar({ tipo: 'fechou', nota: ultimaRef.current })
    // conflito ou erro: a janela fica aberta, com a nota que diz o que houve.
  }

  async function alternarArquivo() {
    clearTimeout(timer.current)
    const novo = !agora.current.arquivada
    agora.current = { ...agora.current, arquivada: novo }
    setArquivada(novo)
    const r = await liberarEGravar()
    if (r === 'ok') terminar({ tipo: novo ? 'arquivou' : 'desarquivou', nota: ultimaRef.current })
  }

  async function paraALixeira() {
    clearTimeout(timer.current)
    // O que foi escrito vai junto: a nota que sai da lixeira é a que a pessoa viu.
    if (parado.current !== 'lixeira') {
      const r = await liberarEGravar()
      if (r !== 'ok') return
    }
    try {
      await cliente.moverNotaParaLixeira(nota.id)
    } catch (e) {
      if (problemaDe(e)) aoPerderLigacao(e)
      setEstado({ tipo: 'erro', mensagem: mensagemDe(e) })
      return
    }
    terminar({ tipo: 'lixeira', nota: ultimaRef.current })
  }

  function mudarCor(nova: CorDaNota | null) {
    agora.current = { ...agora.current, cor: nova }
    setCor(nova)
  }

  function alternarFixada() {
    const nova = !agora.current.fixada
    agora.current = { ...agora.current, fixada: nova }
    setFixada(nova)
  }

  // copiar escreve na área de transferência dentro do clique de quem pediu, e
  // nunca fora dele: a janela não copia nada sozinha.
  function copiar() {
    const texto = [agora.current.titulo.trim(), agora.current.corpo].filter(Boolean).join('\n\n')
    if (texto === '') return
    const area = navigator.clipboard
    if (!area) return avisar('Não consegui copiar. Selecione o texto e use Ctrl+C.')
    area.writeText(texto).then(
      () => avisar('Copiado.'),
      () => avisar('Não consegui copiar. Selecione o texto e use Ctrl+C.'),
    )
  }

  // copiarWhatsApp entrega o que está na janela agora (mesmo o que ainda não foi
  // salvo) como texto de WhatsApp, no clique de quem pediu.
  function copiarWhatsApp() {
    copiarParaWhatsApp(agora.current.titulo, agora.current.corpo, {
      avisar,
      estreita,
      aoSelecionar: 'Selecione o texto e use Ctrl+C.',
    })
  }

  // O foco não sai da janela: Tab no último campo volta ao primeiro, e Shift+Tab
  // no primeiro vai ao último.
  function aoTeclar(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault()
      void fechar()
      return
    }
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && !e.defaultPrevented) {
      e.preventDefault()
      void fechar()
      return
    }
    prenderOFoco(e, dialogo.current)
  }

  const nomesDosMarcadores = new Map(marcadores.map((m) => [m.id, m.nome]))
  const etiquetas = ultima.marcadores.flatMap((id) => {
    const nome = nomesDosMarcadores.get(id)
    return nome ? [{ id, nome }] : []
  })
  const quando = formatarHora(ultima.updated_at)

  // formatarComItens é o Formatar de lista: numa linha de texto corrido, o Agent
  // separa os itens (editor.formatarComItens). O aviso só aparece quando algo não
  // saiu como pedido; a separação feita se mostra embaixo dos itens, com o
  // Desfazer. O texto nunca some: no erro, a linha fica como estava.
  async function formatarComItens(tipo: TipoDaLinha) {
    const d = await editor.formatarComItens(tipo, (trecho) => cliente.separarItens(trecho))
    if (d.tipo === 'sem-itens') avisar('Não achei itens para separar: a linha virou um item só.')
    else if (d.tipo === 'mudou') avisar('A linha mudou enquanto eu separava os itens; nada foi trocado.')
    else if (d.tipo === 'erro') {
      if (problemaDe(d.erro)) aoPerderLigacao(d.erro)
      avisar(`Não consegui separar em itens: ${mensagemDe(d.erro).replace(/\.$/, '')}. O texto ficou como estava.`)
    }
  }

  return (
    <div
      className="veu"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) void fechar()
      }}
    >
      <div
        ref={dialogo}
        className="dialogo"
        role="dialog"
        aria-modal="true"
        aria-label={`Nota: ${nome}`}
        data-cor={cor ?? undefined}
        onKeyDown={aoTeclar}
      >
        <button
          type="button"
          className="icone-botao fixar-no-dialogo"
          aria-pressed={fixada}
          aria-label={fixada ? 'Desafixar' : 'Fixar'}
          title={fixada ? 'Desafixar' : 'Fixar'}
          onClick={alternarFixada}
        >
          <Icone nome="fixar" />
        </button>

        <CamposDaNota
          titulo={titulo}
          aoMudarTitulo={setTitulo}
          editor={editor}
          rotulo="Texto da nota"
          foco={{ cliente, nota: nota.id }}
        />

        {etiquetas.length > 0 && (
          <div className="pe">
            {etiquetas.map((e, i) => (
              <span key={e.id} className="etiqueta tiravel">
                {e.nome}
                <button type="button" aria-label={`Tirar o marcador ${e.nome}`} title="Tirar o marcador" onClick={() => void tirarMarcador(e.id, i)}>
                  ×
                </button>
              </span>
            ))}
          </div>
        )}

        {sugestao && (
          <div className="sugestao" role="group" aria-label="Sugestão de marcador">
            <span>
              Marcar como <strong>{sugestao.nome}</strong>?
            </span>
            <button type="button" className="botao pequeno" onClick={() => void aceitarSugestao(sugestao.id)}>
              Marcar
            </button>
            <button type="button" className="botao pequeno leve" onClick={recusarSugestao}>
              Agora não
            </button>
          </div>
        )}

        {ultima.conflito && (
          <section className="selo" aria-labelledby={idDoSelo}>
            <p className="selo-titulo" id={idDoSelo}>
              Mudou em dois aparelhos. A outra versão está guardada até você escolher.
            </p>
            <div className="versao">
              <p className="rotulo">
                A outra · <time dateTime={ultima.conflito.conflict_at}>{formatarCurta(ultima.conflito.conflict_at)}</time>
              </p>
              {ultima.conflito.titulo.trim() !== '' && <p className="versao-texto versao-titulo">{ultima.conflito.titulo}</p>}
              <p className="versao-texto versao-corpo" role="region" aria-label="Texto da outra versão" tabIndex={0}>
                {ultima.conflito.corpo}
              </p>
            </div>
            <div className="acoes">
              <button
                type="button"
                className="botao"
                aria-disabled={escolhendo || estado.tipo === 'conflito' || estado.tipo === 'lixeira' || undefined}
                aria-describedby={`${idDoSelo}-ajuda`}
                onClick={() => void escolherSelo('manter')}
              >
                Manter esta
              </button>
              <button
                type="button"
                className="botao"
                aria-disabled={escolhendo || estado.tipo === 'conflito' || estado.tipo === 'lixeira' || undefined}
                aria-describedby={`${idDoSelo}-ajuda`}
                onClick={() => void escolherSelo('trocar')}
              >
                Usar a outra
              </button>
            </div>
            <p className="ajuda" id={`${idDoSelo}-ajuda`}>
              A que você não escolher é descartada.
            </p>
          </section>
        )}

        {estado.tipo === 'erro' && (
          <p className="nota" role="alert">
            Não consegui salvar a nota: {estado.mensagem}. O texto continua aqui; feche a nota para tentar de novo.
          </p>
        )}
        {estado.tipo === 'conflito' && (
          <div className="nota" role="alert">
            <p>
              Esta nota foi alterada em outra aba enquanto você editava. O texto aqui continua sendo o seu; salve de novo para substituir a versão
              atual.
            </p>
            <button type="button" className="botao" onClick={() => void liberarEGravar()}>
              Salvar por cima
            </button>
          </div>
        )}
        {estado.tipo === 'lixeira' && (
          <p className="nota" role="alert">
            Esta nota foi movida para a lixeira em outra aba. O texto acima continua aqui para você copiar; para salvá-lo, restaure a nota pela Lixeira
            das notas.
          </p>
        )}

        <p className="meta">
          editada às {quando}
          {ultima.ditado_de_origem && ' · veio de um ditado'}
          {estado.tipo === 'salvando' && ' · salvando…'}
        </p>

        <div className="barra-nota">
          <BotaoComMenu icone="formatar" rotulo="Formatar">
            {(fechar) => (
              <>
                <div className="titulo-menu">TRANSFORMAR EM</div>
                <div className="formatos">
                  {tiposDoFormatar.map(([tipo, rotulo]) => (
                    <button
                      key={tipo}
                      type="button"
                      onClick={() => {
                        fechar()
                        if (separaEmItens(tipo)) void formatarComItens(tipo)
                        else editor.formatar(tipo)
                      }}
                    >
                      {rotulo}
                    </button>
                  ))}
                </div>
                {editor.selecao.size > 1 && <div className="titulo-menu">{editor.selecao.size} linhas escolhidas</div>}
              </>
            )}
          </BotaoComMenu>

          <BotaoComMenu icone="cor" rotulo="Cor">
            {(fechar) => (
              <PaletaDeCores
                atual={cor}
                aoEscolher={(c) => {
                  mudarCor(c)
                  fechar()
                }}
              />
            )}
          </BotaoComMenu>

          <BotaoComMenu icone="marcador" rotulo="Marcadores" classe="escolher">
            {() => <EscolherMarcadores marcadores={marcadores} marcados={ultima.marcadores} aoAlternar={alternarMarcador} aoCriar={criarEMarcar} />}
          </BotaoComMenu>

          <button type="button" className="icone-botao" aria-label="Imagem" title="Imagens chegam depois" disabled>
            <Icone nome="imagem" />
          </button>

          <button
            type="button"
            className="icone-botao"
            aria-label={arquivada ? 'Desarquivar' : 'Arquivar'}
            title={arquivada ? 'Desarquivar' : 'Arquivar'}
            onClick={() => void alternarArquivo()}
          >
            <Icone nome={arquivada ? 'desarquivar' : 'arquivar'} />
          </button>

          <BotaoComMenu icone="mais" rotulo="Mais">
            {(fechar) => (
              <>
                <button
                  type="button"
                  onClick={() => {
                    copiar()
                    fechar()
                  }}
                >
                  <Icone nome="copiar" />
                  Copiar texto
                </button>
                <button
                  type="button"
                  onClick={() => {
                    copiarWhatsApp()
                    fechar()
                  }}
                >
                  <Icone nome="conversa" />
                  Copiar para WhatsApp
                </button>
                <hr />
                <button
                  type="button"
                  disabled={marcados === 0}
                  onClick={() => {
                    editor.desmarcarTudo()
                    fechar()
                  }}
                >
                  <Icone nome="desmarcar" />
                  Desmarcar tudo
                </button>
                <button
                  type="button"
                  disabled={marcados === 0}
                  onClick={() => {
                    editor.apagarMarcados()
                    fechar()
                  }}
                >
                  <Icone nome="lixeira" />
                  Apagar marcados
                </button>
                <hr />
                <button
                  type="button"
                  className="risco"
                  onClick={() => {
                    fechar()
                    void paraALixeira()
                  }}
                >
                  <Icone nome="lixeira" />
                  Mover para a lixeira
                </button>
              </>
            )}
          </BotaoComMenu>

          <span className="empurra" />
          <button type="button" className="botao leve" onClick={() => void fechar()}>
            Fechar
          </button>
        </div>
      </div>
    </div>
  )
}
