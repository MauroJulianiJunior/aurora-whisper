import { Fragment, type KeyboardEvent, type ReactNode, useMemo } from 'react'
import { formatarCurta } from '../datas'
import { BotaoComMenu } from './BotaoComMenu'
import { type Linha, contarItens, inicioDoCorpo, lerCorpo, nomeDaNota } from './corpo'
import { Icone } from './Icone'
import { PaletaDeCores } from './PaletaDeCores'
import { type CorDaNota, NOMES_DAS_CORES, type Nota } from './tipos'

// O que se faz com uma nota sem abri-la (as ferramentas do cartão).
export interface AcoesDaNota {
  fixar(nota: Nota): void
  arquivar(nota: Nota): void
  cor(nota: Nota, cor: CorDaNota | null): void
  copiar(nota: Nota): void
  lixeira(nota: Nota): void
}

interface PropsComuns {
  nota: Nota
  // nomesDosMarcadores: id → nome.
  nomesDosMarcadores: ReadonlyMap<string, string>
  // noArquivo: a vista é o Arquivo (o botão é Desarquivar).
  noArquivo: boolean
  aoAbrir(nota: Nota): void
  acoes: AcoesDaNota
}

// Quantas linhas de texto cabem no cartão antes das reticências: o cartão é um
// resumo, e a nota inteira está a um clique.
const linhasNoCartao = 14

const marcada = (l: Linha) => l.tipo === 'tarefa' && l.feito

// abrirComTeclado: Enter ou Espaço no cartão (que é um botão por papel).
function abrirComTeclado(e: KeyboardEvent<HTMLElement>, abrir: () => void) {
  if (e.target !== e.currentTarget) return
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    abrir()
  }
}

// CorpoDoCartao desenha o corpo pelo prefixo de cada linha: marcadores,
// numeradas, caixas de marcar, títulos e parágrafos. Os itens marcados ficam
// de fora, resumidos em "+ N marcados".
function CorpoDoCartao({ linhas }: { linhas: Linha[] }) {
  const abertas = linhas.filter((l) => !marcada(l) && !(l.tipo === 'paragrafo' && l.texto.trim() === ''))
  const nMarcados = linhas.filter(marcada).length
  const mostradas = abertas.slice(0, linhasNoCartao)
  const cortou = abertas.length > mostradas.length

  // Linhas seguidas do mesmo tipo de lista formam uma lista só.
  const blocos: ReactNode[] = []
  for (let i = 0; i < mostradas.length; ) {
    const l = mostradas[i]!
    if (l.tipo === 'marcador' || l.tipo === 'numerada' || l.tipo === 'tarefa') {
      let j = i
      while (j < mostradas.length && mostradas[j]!.tipo === l.tipo) j++
      const itens = mostradas.slice(i, j).map((x) => (
        <li key={x.chave}>
          {x.tipo === 'tarefa' && <span className="caixa" aria-hidden="true" />}
          {x.texto}
        </li>
      ))
      if (l.tipo === 'numerada') blocos.push(<ol key={l.chave}>{itens}</ol>)
      else blocos.push(<ul key={l.chave} className={l.tipo === 'tarefa' ? 'marcar-lista' : undefined}>{itens}</ul>)
      i = j
    } else {
      blocos.push(
        l.tipo === 'titulo' ? (
          <h4 key={l.chave}>{l.texto}</h4>
        ) : (
          <p key={l.chave}>{l.texto}</p>
        ),
      )
      i++
    }
  }
  return (
    <>
      {blocos.map((b, k) => (
        <Fragment key={k}>{b}</Fragment>
      ))}
      {cortou && <p className="cortado">…</p>}
      {nMarcados > 0 && <div className="marcados">+ {nMarcados} {nMarcados === 1 ? 'marcado' : 'marcados'}</div>}
    </>
  )
}

// Etiquetas do pé do cartão: os marcadores da nota e, se veio de um ditado, a
// origem.
function PeDoCartao({ nota, nomesDosMarcadores }: Pick<PropsComuns, 'nota' | 'nomesDosMarcadores'>) {
  const nomes = nota.marcadores.map((id) => nomesDosMarcadores.get(id)).filter((n): n is string => Boolean(n))
  if (nomes.length === 0 && !nota.ditado_de_origem) return null
  return (
    <div className="pe">
      {nomes.map((n) => (
        <span key={n} className="etiqueta">
          {n}
        </span>
      ))}
      {nota.ditado_de_origem && <span className="origem">veio de um ditado</span>}
    </div>
  )
}

// Fixar (no canto) e as ferramentas do cartão: cor, arquivar e mais. Aparecem
// com o ponteiro ou o foco no cartão; no toque ficam sempre à vista.
function Ferramentas({ nota, noArquivo, acoes, nome }: Pick<PropsComuns, 'nota' | 'noArquivo' | 'acoes'> & { nome: string }) {
  return (
    <>
      <button
        type="button"
        className="icone-botao fixar"
        aria-pressed={nota.fixada}
        aria-label={`${nota.fixada ? 'Desafixar' : 'Fixar'} a nota “${nome}”`}
        title={nota.fixada ? 'Desafixar' : 'Fixar'}
        onClick={() => acoes.fixar(nota)}
      >
        <Icone nome="fixar" />
      </button>
      <div className="ferramentas">
        <BotaoComMenu icone="cor" rotulo={`Cor da nota “${nome}”`} titulo="Cor">
          {(fechar) => (
            <PaletaDeCores
              atual={nota.cor}
              aoEscolher={(c) => {
                acoes.cor(nota, c)
                fechar()
              }}
            />
          )}
        </BotaoComMenu>
        <button
          type="button"
          className="icone-botao"
          aria-label={`${noArquivo ? 'Desarquivar' : 'Arquivar'} a nota “${nome}”`}
          title={noArquivo ? 'Desarquivar' : 'Arquivar'}
          onClick={() => acoes.arquivar(nota)}
        >
          <Icone nome={noArquivo ? 'desarquivar' : 'arquivar'} />
        </button>
        <BotaoComMenu icone="mais" rotulo={`Mais opções da nota “${nome}”`} titulo="Mais">
          {(fechar) => (
            <>
              <button
                type="button"
                onClick={() => {
                  acoes.copiar(nota)
                  fechar()
                }}
              >
                <Icone nome="copiar" />
                Copiar texto
              </button>
              <hr />
              <button
                type="button"
                className="risco"
                onClick={() => {
                  fechar()
                  acoes.lixeira(nota)
                }}
              >
                <Icone nome="lixeira" />
                Mover para a lixeira
              </button>
            </>
          )}
        </BotaoComMenu>
      </div>
    </>
  )
}

// CartaoDaNota é um quadradinho da grade (ADR-022, "A tela"): título em Space
// Grotesk, o corpo, os marcadores, e a cor só como fundo suave.
export function CartaoDaNota(props: PropsComuns) {
  const { nota, aoAbrir } = props
  const linhas = useMemo(() => lerCorpo(nota.corpo), [nota.corpo])
  const nome = nomeDaNota(nota.titulo, linhas)
  const vazia = nota.titulo.trim() === '' && linhas.every((l) => l.texto.trim() === '')
  return (
    <article className="cartao" data-nota={nota.id} data-cor={nota.cor ?? undefined}>
      <div
        role="button"
        tabIndex={0}
        className="cartao-abrir"
        data-abrir
        aria-haspopup="dialog"
        aria-label={nome}
        onClick={() => aoAbrir(nota)}
        onKeyDown={(e) => abrirComTeclado(e, () => aoAbrir(nota))}
      >
        {nota.titulo.trim() !== '' && <h3>{nota.titulo}</h3>}
        {vazia ? <p className="vazio">Nota vazia</p> : <CorpoDoCartao linhas={linhas} />}
        <PeDoCartao nota={nota} nomesDosMarcadores={props.nomesDosMarcadores} />
      </div>
      <Ferramentas nota={nota} noArquivo={props.noArquivo} acoes={props.acoes} nome={nome} />
    </article>
  )
}

// LinhaDaNota é a forma "lista": uma nota por linha, o título e o começo do
// corpo, a cor numa bolinha.
export function LinhaDaNota(props: PropsComuns) {
  const { nota, aoAbrir } = props
  const linhas = useMemo(() => lerCorpo(nota.corpo), [nota.corpo])
  const nome = nomeDaNota(nota.titulo, linhas)
  const titulo = nota.titulo.trim()
  // Sem título, o nome já é o começo do corpo: o resumo não o repete.
  const inicio = titulo ? inicioDoCorpo(linhas) : ''
  return (
    <li className="nota-linha" data-nota={nota.id}>
      <div
        role="button"
        tabIndex={0}
        className="nota-linha-abrir"
        data-abrir
        aria-haspopup="dialog"
        aria-label={nome}
        onClick={() => aoAbrir(nota)}
        onKeyDown={(e) => abrirComTeclado(e, () => aoAbrir(nota))}
      >
        <span className="bolinha" data-cor={nota.cor ?? undefined} title={nota.cor ? NOMES_DAS_CORES[nota.cor] : undefined} aria-hidden="true" />
        <span className="nota-linha-titulo">{titulo || (linhas.some((l) => l.texto.trim() !== '') ? inicioDoCorpo(linhas, 120) : 'Nota vazia')}</span>
        {inicio && <span className="nota-linha-inicio">{inicio}</span>}
      </div>
      <Ferramentas nota={nota} noArquivo={props.noArquivo} acoes={props.acoes} nome={nome} />
    </li>
  )
}

// TabelaDeNotas é a forma "detalhes": título, marcadores, itens marcados
// ("2/6"), cor e quando foi editada.
export function TabelaDeNotas({
  notas,
  nomesDosMarcadores,
  aoAbrir,
}: {
  notas: Nota[]
  nomesDosMarcadores: ReadonlyMap<string, string>
  aoAbrir(nota: Nota): void
}) {
  return (
    <div className="detalhes-rolagem">
    <table className="detalhes">
      <thead>
        <tr>
          <th scope="col">Título</th>
          <th scope="col">Marcadores</th>
          <th scope="col">Itens marcados</th>
          <th scope="col">Cor</th>
          <th scope="col">Editada</th>
        </tr>
      </thead>
      <tbody>
        {notas.map((n) => {
          const linhas = lerCorpo(n.corpo)
          const nome = nomeDaNota(n.titulo, linhas)
          const { feitos, total } = contarItens(linhas)
          const nomes = n.marcadores.map((id) => nomesDosMarcadores.get(id)).filter((x): x is string => Boolean(x))
          return (
            <tr key={n.id} data-nota={n.id} onClick={() => aoAbrir(n)}>
              <td>
                <button
                  type="button"
                  className="detalhes-abrir"
                  data-abrir
                  aria-haspopup="dialog"
                  onClick={(e) => {
                    e.stopPropagation()
                    aoAbrir(n)
                  }}
                >
                  {nome}
                </button>
                {n.fixada && <span className="etiqueta">fixada</span>}
              </td>
              <td>
                {nomes.map((x) => (
                  <span key={x} className="etiqueta">
                    {x}
                  </span>
                ))}
              </td>
              <td className="numero">{total > 0 ? `${feitos}/${total}` : '—'}</td>
              <td>
                {n.cor ? (
                  <span className="cor-na-tabela">
                    <span className="bolinha" data-cor={n.cor} aria-hidden="true" />
                    {NOMES_DAS_CORES[n.cor]}
                  </span>
                ) : (
                  '—'
                )}
              </td>
              <td className="hora">
                <time dateTime={n.updated_at}>{formatarCurta(n.updated_at)}</time>
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
    </div>
  )
}
