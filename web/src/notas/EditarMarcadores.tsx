import { type KeyboardEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { Conflito, ErroDaApi, problemaDe } from '../api'
import { Icone } from './Icone'
import { acharPorNome, contagemDeNotas, limparNome, MAX_NOME_DO_MARCADOR, ordenarPorNome } from './marcadores'
import { prenderOFoco } from './prenderOFoco'
import type { ClienteDeNotas, Marcador, Nota } from './tipos'

interface Props {
  cliente: ClienteDeNotas
  // marcadores: os de agora, na ordem do nome; é o Notas quem os guarda, e
  // aoMudar os troca.
  marcadores: readonly Marcador[]
  // notasAtivas: as notas da tela principal, para contar as de cada marcador. As do
  // Arquivo a janela lê sozinha: apagar o marcador tira dele também.
  notasAtivas: readonly Nota[]
  aoMudar(f: (atuais: Marcador[]) => Marcador[]): void
  aoFechar(): void
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
}

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))
const jaExiste = (nome: string) => `Já existe o marcador "${nome}".`
const ehNomeEmUso = (e: unknown) => e instanceof ErroDaApi && e.status === 409 && e.codigo === 'NAME_IN_USE'

// "Ele sai das 2 notas; as notas ficam." — o que apagar faz, na conta de quem usa.
function oQueAcontece(n: number): string {
  if (n === 0) return 'Nenhuma nota usa este marcador.'
  if (n === 1) return 'Ele sai da nota; a nota fica.'
  return `Ele sai das ${n} notas; as notas ficam.`
}

// EditarMarcadores é a janela do chip "Editar marcadores" (ADR-022, "Marcadores,
// prévia da N4"): criar no campo de cima, renomear na própria linha e apagar com a
// pergunta também na linha, sem janela por cima de janela. Mesma moldura da nota
// aberta (colchetes verdes, foco preso, Esc fecha). Cada mudança vale na hora: o
// Fechar só fecha.
export function EditarMarcadores({ cliente, marcadores, notasAtivas, aoMudar, aoFechar, aoPerderLigacao, avisar }: Props) {
  const [texto, setTexto] = useState('')
  const [erroDoAgent, setErroDoAgent] = useState('')
  const [ocupado, setOcupado] = useState(false)
  const [renomeando, setRenomeando] = useState<{ id: string; texto: string; erro: string } | null>(null)
  const [apagando, setApagando] = useState<string | null>(null)
  const [arquivadas, setArquivadas] = useState<readonly Nota[]>([])
  const janela = useRef<HTMLDivElement>(null)
  const campoNovo = useRef<HTMLInputElement>(null)
  const campoRenomear = useRef<HTMLInputElement>(null)
  const botaoCancelar = useRef<HTMLButtonElement>(null)
  // voltarA: o botão que ganha o foco quando a linha em edição volta ao normal.
  const voltarA = useRef<{ id: string; botao: 'renomear' | 'apagar' } | null>(null)

  useEffect(() => {
    campoNovo.current?.focus()
  }, [])

  // As notas do Arquivo também têm marcador: entram na conta (melhor esforço; se a
  // leitura falhar, a conta fica só com as da tela principal).
  useEffect(() => {
    const ctrl = new AbortController()
    cliente.listarNotas({ vista: 'arquivo' }, ctrl.signal).then(setArquivadas, () => {})
    return () => ctrl.abort()
  }, [cliente])

  useEffect(() => {
    if (renomeando) {
      campoRenomear.current?.focus()
      campoRenomear.current?.select()
    }
  }, [renomeando?.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (apagando) botaoCancelar.current?.focus()
  }, [apagando])
  useEffect(() => {
    // Depois de cancelar ou salvar, o foco volta ao botão da linha.
    if (renomeando || apagando || !voltarA.current) return
    const { id, botao } = voltarA.current
    voltarA.current = null
    janela.current?.querySelector<HTMLElement>(`[data-marcador="${id}"] [data-${botao}]`)?.focus()
  })

  const contar = (id: string) => notasAtivas.filter((n) => n.marcadores.includes(id)).length + arquivadas.filter((n) => n.marcadores.includes(id)).length

  // falhou trata o que não é recusa de nome: a ligação caiu, ou o Agent disse não.
  function falhou(e: unknown): string {
    if (problemaDe(e)) {
      aoPerderLigacao(e)
      return 'Sem ligação com o Aurora Whisper.'
    }
    return `Não deu certo: ${mensagemDe(e)}`
  }

  // --- criar ---
  const nomeNovo = limparNome(texto)
  const repetido = acharPorNome(marcadores, nomeNovo)
  const erroNovo = repetido ? jaExiste(repetido.nome) : erroDoAgent

  async function criar() {
    if (nomeNovo === '' || repetido || ocupado) return
    setOcupado(true)
    try {
      const novo = await cliente.criarMarcador(nomeNovo)
      aoMudar((l) => ordenarPorNome([...l.filter((m) => m.id !== novo.id), novo]))
      setTexto('')
      setErroDoAgent('')
    } catch (e) {
      setErroDoAgent(ehNomeEmUso(e) ? jaExiste(nomeNovo) : falhou(e))
    } finally {
      setOcupado(false)
    }
  }

  // --- renomear ---
  function comecarARenomear(m: Marcador) {
    setApagando(null)
    setRenomeando({ id: m.id, texto: m.nome, erro: '' })
  }

  function cancelarRenomear() {
    if (renomeando) voltarA.current = { id: renomeando.id, botao: 'renomear' }
    setRenomeando(null)
  }

  async function salvarNome() {
    if (!renomeando || ocupado) return
    const m = marcadores.find((x) => x.id === renomeando.id)
    const nome = limparNome(renomeando.texto)
    if (!m || nome === '' || acharPorNome(marcadores, nome, m.id)) return
    if (nome === m.nome) return cancelarRenomear()
    setOcupado(true)
    try {
      const novo = await cliente.renomearMarcador(m.id, m.version, nome)
      aoMudar((l) => ordenarPorNome(l.map((x) => (x.id === novo.id ? novo : x))))
      cancelarRenomear()
    } catch (e) {
      if (ehNomeEmUso(e)) setRenomeando((r) => r && { ...r, erro: jaExiste(nome) })
      else if (e instanceof Conflito) {
        // A versão do marcador mudou em outra aba: lê de novo e deixa a pessoa repetir.
        avisar('Este marcador mudou em outra aba. A lista foi lida de novo; repita se ainda quiser.')
        cancelarRenomear()
        cliente.listarMarcadores().then((lista) => aoMudar(() => lista), () => {})
      } else setRenomeando((r) => r && { ...r, erro: falhou(e) })
    } finally {
      setOcupado(false)
    }
  }

  // --- apagar ---
  function comecarAApagar(m: Marcador) {
    setRenomeando(null)
    setApagando(m.id)
  }

  function cancelarApagar() {
    if (apagando) voltarA.current = { id: apagando, botao: 'apagar' }
    setApagando(null)
  }

  async function apagar(m: Marcador) {
    if (ocupado) return
    setOcupado(true)
    try {
      await cliente.apagarMarcador(m.id)
    } catch (e) {
      // 404: o marcador já tinha sido apagado em outra aba, e o resultado é o pedido.
      if (!(e instanceof ErroDaApi && e.status === 404)) {
        setOcupado(false)
        setApagando(null)
        avisar(falhou(e))
        return
      }
    }
    setOcupado(false)
    setApagando(null)
    aoMudar((l) => l.filter((x) => x.id !== m.id))
    avisar(`Marcador "${m.nome}" apagado.`)
    // A linha sumiu: o foco vai ao campo de criar.
    campoNovo.current?.focus()
  }

  function aoTeclar(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault()
      aoFechar()
      return
    }
    prenderOFoco(e, janela.current)
  }

  const nomeRenomeado = renomeando ? limparNome(renomeando.texto) : ''
  const repetidoNaLinha = renomeando ? acharPorNome(marcadores, nomeRenomeado, renomeando.id) : undefined
  const erroDaLinha = repetidoNaLinha ? jaExiste(repetidoNaLinha.nome) : (renomeando?.erro ?? '')

  return (
    <div
      className="veu"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) aoFechar()
      }}
    >
      <div ref={janela} className="dialogo marcadores-janela" role="dialog" aria-modal="true" aria-label="Editar marcadores" onKeyDown={aoTeclar}>
        <h3>Editar marcadores</h3>

        <div className="campo-novo">
          <span className="mais-m" aria-hidden="true">
            +
          </span>
          <input
            ref={campoNovo}
            type="text"
            aria-label="Criar marcador"
            placeholder="Criar marcador"
            autoComplete="off"
            maxLength={MAX_NOME_DO_MARCADOR}
            aria-invalid={erroNovo ? true : undefined}
            aria-describedby={erroNovo ? 'erro-novo-marcador' : undefined}
            value={texto}
            onChange={(e) => {
              setTexto(e.target.value)
              setErroDoAgent('')
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
                e.preventDefault()
                void criar()
              }
            }}
          />
          <button type="button" className="botao pequeno primario" disabled={nomeNovo === '' || Boolean(repetido) || ocupado} onClick={() => void criar()}>
            Criar
          </button>
        </div>
        {erroNovo && (
          <p id="erro-novo-marcador" className="erro-campo" role="alert">
            {erroNovo}
          </p>
        )}

        {marcadores.length === 0 ? (
          <p className="vazio-menu">Nenhum marcador ainda. Crie o primeiro no campo de cima.</p>
        ) : (
          <ul className="lista-marcadores">
            {marcadores.map((m) => {
              const n = contar(m.id)
              if (renomeando?.id === m.id) {
                return (
                  <li key={m.id} className="renomeando" data-marcador={m.id}>
                    <div className="campo-novo">
                      <input
                        ref={campoRenomear}
                        type="text"
                        aria-label={`Novo nome de ${m.nome}`}
                        autoComplete="off"
                        maxLength={MAX_NOME_DO_MARCADOR}
                        aria-invalid={erroDaLinha ? true : undefined}
                        value={renomeando.texto}
                        onChange={(e) => setRenomeando({ id: m.id, texto: e.target.value, erro: '' })}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
                            e.preventDefault()
                            void salvarNome()
                          } else if (e.key === 'Escape') {
                            // Esc desfaz só o renomear; a janela fica.
                            e.preventDefault()
                            cancelarRenomear()
                          }
                        }}
                      />
                      <button type="button" className="botao pequeno primario" disabled={nomeRenomeado === '' || Boolean(repetidoNaLinha) || ocupado} onClick={() => void salvarNome()}>
                        Salvar
                      </button>
                    </div>
                    {erroDaLinha && (
                      <p className="erro-campo" role="alert">
                        {erroDaLinha}
                      </p>
                    )}
                  </li>
                )
              }
              if (apagando === m.id) {
                return (
                  <li
                    key={m.id}
                    className="apagando"
                    data-marcador={m.id}
                    role="group"
                    aria-label={`Apagar o marcador ${m.nome}`}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') {
                        e.preventDefault()
                        cancelarApagar()
                      }
                    }}
                  >
                    <div className="pergunta">
                      Apagar "{m.nome}"? <span>{oQueAcontece(n)}</span>
                    </div>
                    <span className="empurra" />
                    <button ref={botaoCancelar} type="button" className="botao pequeno" onClick={cancelarApagar}>
                      Cancelar
                    </button>
                    <button type="button" className="botao pequeno risco" aria-disabled={ocupado || undefined} onClick={() => void apagar(m)}>
                      Apagar
                    </button>
                  </li>
                )
              }
              return (
                <li key={m.id} data-marcador={m.id}>
                  <span className="nome-m">{m.nome}</span>
                  <span className="qtd">{contagemDeNotas(n)}</span>
                  <button type="button" className="icone-botao" data-renomear aria-label={`Renomear ${m.nome}`} title="Renomear" onClick={() => comecarARenomear(m)}>
                    <Icone nome="lapis" />
                  </button>
                  <button type="button" className="icone-botao" data-apagar aria-label={`Apagar ${m.nome}`} title="Apagar" onClick={() => comecarAApagar(m)}>
                    <Icone nome="lixeira" />
                  </button>
                </li>
              )
            })}
          </ul>
        )}

        <div className="rodape-janela">
          <button type="button" className="botao leve" onClick={aoFechar}>
            Fechar
          </button>
        </div>
      </div>
    </div>
  )
}
