import { Fragment, type KeyboardEvent, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { PropsDosResultadosDaBusca } from '../ambiente'
import { type ClienteDoHistorico, type Ditado, problemaDe } from '../api'
import type { ClienteDeNotas, Nota } from '../notas/tipos'
import { destacar, quandoNaBusca, recortar, resumirNota, textoDoDitado } from './destaque'

// Quantos resultados cada grupo mostra antes do "Ver todas" (ADR-022, "Busca
// única": a busca é a "memória" do pedido, não a lista inteira).
const porGrupo = 5

type Cliente = Pick<ClienteDeNotas, 'listarNotas'> & Pick<ClienteDoHistorico, 'listar'>

// Achados é um grupo da busca: o que veio, se há mais do que veio (os ditados
// vêm em páginas) ou por que não veio.
interface Achados<T> {
  itens: T[]
  mais: boolean
  erro?: string
}

// Resultado guarda a busca que o produziu: o que está na tela nunca é o de outra
// busca (a resposta lenta de "fg" não aparece embaixo de "fgts").
interface Resultado {
  busca: string
  hoje: Date
  notas: Achados<Nota>
  ditados: Achados<Ditado>
}

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

// As fixadas primeiro; a ordem de cada grupo é a da API (a ordem manual das notas).
const ordenar = (notas: Nota[]) => [...notas.filter((n) => n.fixada), ...notas.filter((n) => !n.fixada)]

const plural = (n: number, um: string, varios: string) => `${n} ${n === 1 ? um : varios}`

// Marcado escreve o texto com o que casa com a busca em <mark>.
function Marcado({ texto, termo }: { texto: string; termo: string }) {
  return (
    <>
      {destacar(texto, termo).map((t, i) => (
        <Fragment key={i}>{t.marcado ? <mark>{t.texto}</mark> : t.texto}</Fragment>
      ))}
    </>
  )
}

// anunciar é a frase da região viva (WCAG 4.1.3): quem usa leitor de tela, com o
// foco no campo, precisa ouvir o que a busca achou.
function anunciar(r: Resultado): string {
  const { notas, ditados } = r
  const achados: string[] = []
  if (notas.itens.length > 0) achados.push(plural(notas.itens.length, 'nota', 'notas'))
  if (ditados.itens.length > 0) {
    const n = ditados.itens.length
    achados.push(`${n}${ditados.mais ? ' ou mais' : ''} ${n === 1 && !ditados.mais ? 'ditado' : 'ditados'}`)
  }
  const falhas = [notas.erro && 'Não consegui buscar nas notas.', ditados.erro && 'Não consegui buscar nos ditados.'].filter(Boolean)
  if (achados.length === 0 && falhas.length === 0) return `Nada encontrado para “${r.busca}”.`
  return [achados.length > 0 && `${achados.join(' e ')} para “${r.busca}”.`, ...falhas].filter(Boolean).join(' ')
}

// BuscaUnica são os resultados do campo do topo (ADR-022, "Busca única"): as
// notas e os ditados que casam com o texto, em dois grupos, sem diferenciar
// maiúscula nem acento. Clicar numa nota a abre sobre os resultados; num ditado,
// leva ao histórico com ele aberto; "Ver todas" leva à aba do grupo, com a mesma
// busca. O Painel decide o que cada clique faz (PropsDosResultadosDaBusca).
export function BuscaUnica({
  cliente,
  busca,
  recarga,
  aoAbrirNota,
  aoAbrirDitado,
  aoVerTodas,
  aoLimpar,
  aoVoltarAoCampo,
  aoPerderLigacao: aoPerderLigacaoDoPainel,
}: PropsDosResultadosDaBusca & { cliente: Cliente }) {
  // O Painel passa uma função nova a cada render: a daqui lê a do momento, para a
  // leitura não recomeçar a cada render dele.
  const ligacao = useRef(aoPerderLigacaoDoPainel)
  useEffect(() => {
    ligacao.current = aoPerderLigacaoDoPainel
  })
  const [resultado, setResultado] = useState<Resultado | null>(null)
  const [tentativa, setTentativa] = useState(0)
  const raiz = useRef<HTMLDivElement>(null)
  // voltarA: o resultado cuja janela está aberta; quando ela fecha e o foco se
  // perde, volta para ele.
  const voltarA = useRef<string | null>(null)

  useEffect(() => {
    const ctrl = new AbortController()
    // As duas leituras saem juntas, e cada uma pode falhar sozinha: o grupo que
    // deu certo aparece, e o outro diz o que houve.
    void Promise.allSettled([cliente.listarNotas({ vista: 'ativas', busca }, ctrl.signal), cliente.listar({ lixeira: false, busca }, ctrl.signal)]).then(
      ([n, d]) => {
        if (ctrl.signal.aborted) return
        const falha = (r: PromiseRejectedResult) => {
          if (problemaDe(r.reason)) ligacao.current(r.reason)
          return mensagemDe(r.reason)
        }
        setResultado((antes) => {
          // A falha de uma releitura da mesma busca não apaga o que já estava na tela.
          const igual = antes?.busca === busca ? antes : null
          return {
            busca,
            hoje: new Date(),
            notas: n.status === 'fulfilled' ? { itens: ordenar(n.value), mais: false } : (igual?.notas ?? { itens: [], mais: false, erro: falha(n) }),
            ditados:
              d.status === 'fulfilled'
                ? { itens: d.value.items, mais: Boolean(d.value.next_cursor) }
                : (igual?.ditados ?? { itens: [], mais: false, erro: falha(d) }),
          }
        })
      },
    )
    return () => ctrl.abort()
  }, [cliente, busca, recarga, tentativa])

  // A janela de uma nota fechou (o Painel mexeu na recarga): se o foco se perdeu,
  // volta para o resultado de onde ela veio.
  useLayoutEffect(() => {
    const chave = voltarA.current
    if (!chave) return
    const ativo = document.activeElement
    if (ativo && ativo !== document.body && ativo.isConnected) return
    voltarA.current = null
    Array.from(raiz.current?.querySelectorAll<HTMLElement>('[data-chave]') ?? [])
      .find((el) => el.dataset.chave === chave)
      ?.focus()
  }, [recarga])

  const visiveis = resultado?.busca === busca ? resultado : null

  // As setas andam pelos resultados e pelos "Ver todas"; para cima do primeiro,
  // volta ao campo. Tab continua passando por tudo. Esc acaba a busca.
  function aoTeclar(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      aoLimpar()
      return
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    const itens = Array.from(raiz.current?.querySelectorAll<HTMLElement>('[data-resultado]') ?? [])
    const i = itens.indexOf(e.target as HTMLElement)
    if (i === -1) return
    e.preventDefault()
    if (e.key === 'ArrowUp' && i === 0) return aoVoltarAoCampo()
    itens[i + (e.key === 'ArrowDown' ? 1 : -1)]?.focus()
  }

  function abrirNota(n: Nota) {
    voltarA.current = `n:${n.id}`
    aoAbrirNota(n.id)
  }

  let conteudo
  if (!visiveis) {
    conteudo = <p className="estado atrasado">Carregando…</p>
  } else {
    const { notas, ditados } = visiveis
    const vazio = !notas.erro && !ditados.erro && notas.itens.length === 0 && ditados.itens.length === 0
    conteudo = vazio ? (
      <div className="estado">
        <p>Nada encontrado para “{visiveis.busca}”.</p>
      </div>
    ) : (
      <>
        {(notas.erro || notas.itens.length > 0) && (
          <section className="grupo-busca" aria-labelledby="busca-notas">
            <h2 id="busca-notas" className="dia">
              Notas{!notas.erro && ` · ${notas.itens.length}`}
            </h2>
            {notas.erro ? (
              <ErroDoGrupo onde="nas notas" erro={notas.erro} aoTentar={() => setTentativa((t) => t + 1)} />
            ) : (
              <ul className="resultados">
                {notas.itens.slice(0, porGrupo).map((n) => {
                  const { titulo, linha } = resumirNota(n, visiveis.busca)
                  return (
                    <li key={n.id}>
                      <button type="button" className="resultado" data-resultado data-chave={`n:${n.id}`} aria-haspopup="dialog" onClick={() => abrirNota(n)}>
                        <span className="bolinha" data-cor={n.cor ?? undefined} aria-hidden="true" />
                        <span className="resultado-texto">
                          {titulo && (
                            <strong>
                              <Marcado texto={titulo} termo={visiveis.busca} />
                            </strong>
                          )}
                          {titulo && linha && ' · '}
                          {linha && <Marcado texto={linha} termo={visiveis.busca} />}
                          {!titulo && !linha && 'Nota vazia'}
                        </span>
                      </button>
                    </li>
                  )
                })}
                {notas.itens.length > porGrupo && (
                  <li>
                    <button type="button" className="ver-todas" data-resultado onClick={() => aoVerTodas('notas')}>
                      Ver todas as {notas.itens.length} notas
                    </button>
                  </li>
                )}
              </ul>
            )}
          </section>
        )}

        {(ditados.erro || ditados.itens.length > 0) && (
          <section className="grupo-busca" aria-labelledby="busca-ditados">
            <h2 id="busca-ditados" className="dia">
              Ditados{!ditados.erro && ` · ${ditados.itens.length}${ditados.mais ? '+' : ''}`}
            </h2>
            {ditados.erro ? (
              <ErroDoGrupo onde="nos ditados" erro={ditados.erro} aoTentar={() => setTentativa((t) => t + 1)} />
            ) : (
              <ul className="resultados">
                {ditados.itens.slice(0, porGrupo).map((d) => (
                  <li key={d.id}>
                    <button type="button" className="resultado" data-resultado data-chave={`d:${d.id}`} onClick={() => aoAbrirDitado(d)}>
                      <time className="hora" dateTime={d.created_at}>
                        {quandoNaBusca(d.created_at, visiveis.hoje)}
                      </time>
                      <span className="resultado-texto">
                        <Marcado texto={recortar(textoDoDitado(d, visiveis.busca), visiveis.busca)} termo={visiveis.busca} />
                      </span>
                    </button>
                  </li>
                ))}
                {(ditados.itens.length > porGrupo || ditados.mais) && (
                  <li>
                    <button type="button" className="ver-todas" data-resultado onClick={() => aoVerTodas('ditados')}>
                      Ver todos os {ditados.itens.length}
                      {ditados.mais ? '+' : ''} ditados
                    </button>
                  </li>
                )}
              </ul>
            )}
          </section>
        )}
      </>
    )
  }

  return (
    <div className="busca-unica" ref={raiz} onKeyDown={aoTeclar}>
      <p className="so-leitor anuncio" role="status">
        {visiveis ? anunciar(visiveis) : ''}
      </p>
      {conteudo}
    </div>
  )
}

function ErroDoGrupo({ onde, erro, aoTentar }: { onde: string; erro: string; aoTentar(): void }) {
  return (
    <div className="estado-grupo">
      <p>
        Não consegui buscar {onde}: {erro}
      </p>
      <button type="button" className="botao" onClick={aoTentar}>
        Tentar de novo
      </button>
    </div>
  )
}
