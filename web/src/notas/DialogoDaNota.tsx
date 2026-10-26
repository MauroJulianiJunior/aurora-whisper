import { type KeyboardEvent, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Conflito, problemaDe } from '../api'
import { formatarHora } from '../datas'
import { BotaoComMenu } from './BotaoComMenu'
import { CamposDaNota } from './CamposDaNota'
import { type TipoDaLinha, lerCorpo, nomeDaNota } from './corpo'
import { Icone } from './Icone'
import { PaletaDeCores } from './PaletaDeCores'
import type { ClienteDeNotas, CorDaNota, MudancaDaNota, Nota } from './tipos'
import { useEditorDeLinhas } from './useEditorDeLinhas'

// A espera do salvar automático depois da última tecla.
const esperaDoSalvar = 800

// FimDaNota é como a janela termina. O Notas decide o resto (a lista, o aviso, o
// Desfazer, o foco).
export type FimDaNota = { tipo: 'fechou' | 'arquivou' | 'desarquivou' | 'lixeira'; nota: Nota }

interface Props {
  nota: Nota
  cliente: ClienteDeNotas
  // nomesDosMarcadores: id → nome, para as etiquetas da nota.
  nomesDosMarcadores: ReadonlyMap<string, string>
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
export function DialogoDaNota({ nota, cliente, nomesDosMarcadores, avisar, aoPerderLigacao, aoTerminar }: Props) {
  const [titulo, setTitulo] = useState(nota.titulo)
  const [cor, setCor] = useState<CorDaNota | null>(nota.cor)
  const [fixada, setFixada] = useState(nota.fixada)
  const [arquivada, setArquivada] = useState(nota.arquivada)
  const [ultima, setUltima] = useState(nota)
  const [estado, setEstado] = useState<Estado>({ tipo: 'ok' })
  const editor = useEditorDeLinhas(nota.corpo)
  const { corpo, marcados } = editor
  const dialogo = useRef<HTMLDivElement>(null)
  // O nome da janela é o da nota quando ela abriu: não muda a cada tecla.
  const [nome] = useState(() => nomeDaNota(nota.titulo, lerCorpo(nota.corpo)))
  // Nota nova, sem nada escrito, começa no título; a que já tem texto, no fim dele.
  const [focoInicial] = useState<'titulo' | 'corpo'>(nota.titulo === '' && nota.corpo === '' ? 'titulo' : 'corpo')

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
    if (e.key !== 'Tab' || !dialogo.current) return
    const focaveis = Array.from(dialogo.current.querySelectorAll<HTMLElement>('button, input, textarea, [tabindex]')).filter(
      (el) => !el.hasAttribute('disabled') && el.getAttribute('tabindex') !== '-1',
    )
    const primeiro = focaveis[0]
    const ultimo = focaveis[focaveis.length - 1]
    if (!primeiro || !ultimo) return
    const ativo = document.activeElement
    if (e.shiftKey && (ativo === primeiro || !dialogo.current.contains(ativo))) {
      e.preventDefault()
      ultimo.focus()
    } else if (!e.shiftKey && (ativo === ultimo || !dialogo.current.contains(ativo))) {
      e.preventDefault()
      primeiro.focus()
    }
  }

  const etiquetas = ultima.marcadores.map((id) => nomesDosMarcadores.get(id)).filter((n): n is string => Boolean(n))
  const quando = formatarHora(ultima.updated_at)

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

        <CamposDaNota titulo={titulo} aoMudarTitulo={setTitulo} editor={editor} focoInicial={focoInicial} rotulo="Texto da nota" />

        {etiquetas.length > 0 && (
          <div className="pe">
            {etiquetas.map((e) => (
              <span key={e} className="etiqueta">
                {e}
              </span>
            ))}
          </div>
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
                        editor.formatar(tipo)
                        fechar()
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
