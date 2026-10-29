import { type KeyboardEvent, type ReactNode, type RefObject, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useImagensNaTela } from './contextoDasImagens'
import { Icone } from './Icone'
import type { Guardando } from './imagensNaTela'
import { prenderOFoco } from './prenderOFoco'
import type { ImagemDaNota, Nota } from './tipos'

// As imagens das notas na tela (ADR-022, N10; a prévia docs/notas-previa-n10.html,
// cenas 1 a 4): a grade no topo do cartão e da nota (uma de ponta a ponta; duas
// lado a lado; três ou mais, a primeira larga e duas embaixo, com "+N" no último
// quadrado) e a imagem grande, escura nos dois temas.
//
// A loja (ImagensNaTela) vem pelo contexto (contextoDasImagens.ts): sem ela (o
// app do celular, nesta fase), nada de imagem aparece.

// proporcao é a da primeira imagem do cartão, contida entre um retrato 3:4 e uma
// paisagem 2:1: um print de celular não estica o cartão até o chão, e um
// panorama não vira uma faixa. As outras são quadradas (a prévia).
function proporcao(largura: number, altura: number): string {
  const r = Math.min(2, Math.max(0.75, largura / altura))
  return String(Math.round(r * 1000) / 1000)
}

type Item = { tipo: 'imagem'; imagem: ImagemDaNota } | { tipo: 'guardando'; g: Guardando }

interface PropsDaFoto {
  notaId: string
  item: Item
  // larga: o primeiro quadrado do cartão, com a proporção da imagem.
  larga: boolean
  // primeira: o primeiro quadrado da grade (o único que diz "Imagem sem rede").
  primeira: boolean
  noCartao: boolean
  children?: ReactNode
}

// A folga com que a imagem é lida antes de entrar na tela: a rolagem não espera,
// e a nota lá embaixo da grade não ocupa memória à toa.
const folgaDaLeitura = '600px'

// useVisivel diz se o quadrado já esteve perto da tela. Sem o IntersectionObserver
// (o jsdom), vale sempre.
function useVisivel<T extends Element>(): [RefObject<T | null>, boolean] {
  const ref = useRef<T>(null)
  const [visivel, setVisivel] = useState(() => typeof IntersectionObserver === 'undefined')
  useEffect(() => {
    const el = ref.current
    if (visivel || !el) return
    const obs = new IntersectionObserver(
      (es) => {
        if (es.some((e) => e.isIntersecting)) {
          setVisivel(true)
          obs.disconnect()
        }
      },
      { rootMargin: folgaDaLeitura },
    )
    obs.observe(el)
    return () => obs.disconnect()
  }, [visivel])
  return [ref, visivel]
}

// QuadradoVazio é o lugar de uma imagem que não está à vista, sem erro nenhum: a que
// ainda está a caminho do PC (N10b) e a que não carrega sem rede (o celular, cena 6
// da prévia). Ocupa o espaço da imagem, com a mesma proporção, e diz o porquê no
// primeiro quadrado da grade (os outros só levam o ícone).
function QuadradoVazio({ texto, dizer }: { texto: string; dizer: boolean }) {
  return (
    <span className="foto-vazia">
      <Icone nome="imagem" />
      {dizer && <span>{texto}</span>}
    </span>
  )
}

// Foto é um quadrado da grade: a imagem (lida quando chega perto da tela, se
// ainda não foi), o "guardando…" de quem está subindo, o lugar vazio da que está a
// caminho ou sem rede, ou o aviso de que não deu para abrir.
function Foto({ notaId, item, larga, noCartao, primeira, children }: PropsDaFoto) {
  const loja = useImagensNaTela()
  const [ref, visivel] = useVisivel<HTMLDivElement>()
  const id = item.tipo === 'imagem' ? item.imagem.id : null
  const aCaminho = item.tipo === 'imagem' && item.imagem.estado === 'a_caminho'
  const semRede = id ? (loja?.semRede(id) ?? false) : false
  // As dependências levam o a_caminho e o semRede: a imagem que chega (a lista
  // nova sem o estado) e a rede que volta pedem a leitura de novo.
  useEffect(() => {
    if (id && visivel && !aCaminho) loja?.pedir(notaId, id)
  }, [loja, notaId, id, visivel, aCaminho, semRede])
  const [w, h] = item.tipo === 'imagem' ? [item.imagem.largura, item.imagem.altura] : [item.g.largura, item.g.altura]
  const estilo = noCartao && larga ? { aspectRatio: proporcao(w, h) } : undefined
  if (item.tipo === 'guardando') {
    return (
      <div ref={ref} className="foto guardando" style={estilo}>
        <img src={item.g.url} alt="" draggable={false} />
        <span className="estado">guardando…</span>
      </div>
    )
  }
  const url = id ? loja?.url(id) : undefined
  const falhou = id ? loja?.falhou(id) : false
  // A que já foi lida nesta visita continua aparecendo (url), com ou sem rede.
  if (!url && aCaminho) {
    return (
      <div ref={ref} className="foto vazia a-caminho" style={estilo}>
        <QuadradoVazio texto="Imagem a caminho" dizer={primeira} />
        {children}
      </div>
    )
  }
  if (!url && semRede) {
    return (
      <div ref={ref} className="foto vazia sem-rede" style={estilo}>
        <QuadradoVazio texto="Imagem sem rede" dizer={primeira} />
        {children}
      </div>
    )
  }
  return (
    <div ref={ref} className={falhou ? 'foto sem-imagem' : 'foto'} style={estilo}>
      {url ? <img src={url} alt="" draggable={false} /> : falhou ? <span className="foto-aviso">Imagem indisponível</span> : null}
      {children}
    </div>
  )
}

interface PropsDaGrade {
  nota: Pick<Nota, 'id' | 'imagens'>
  // aoAbrir e aoTirar: só na nota aberta. aoAbrir recebe o índice da imagem; o
  // "+N" abre a terceira, e as setas da imagem grande levam às outras.
  aoAbrir?(indice: number): void
  aoTirar?(imagem: ImagemDaNota): void
}

// GradeDeImagens é a grade do topo do cartão (sem aoAbrir: o cartão inteiro é o
// botão que abre a nota) e da nota aberta (cada imagem abre grande, e o × a tira).
export function GradeDeImagens({ nota, aoAbrir, aoTirar }: PropsDaGrade) {
  const loja = useImagensNaTela()
  if (!loja) return null
  const imagens = loja.imagensDe(nota)
  const itens: Item[] = [
    ...imagens.map((imagem): Item => ({ tipo: 'imagem', imagem })),
    ...loja.guardandoDe(nota.id).map((g): Item => ({ tipo: 'guardando', g })),
  ]
  const n = itens.length
  if (n === 0) return null
  const mostrados = itens.slice(0, 3)
  const faltam = n - mostrados.length
  const forma = n === 1 ? 'fotos um' : n === 2 ? 'fotos dois' : 'fotos tres'
  const noCartao = !aoAbrir
  return (
    <div className={forma} aria-hidden={noCartao || undefined}>
      {mostrados.map((item, i) => {
        const larga = i === 0 && n !== 2
        const chave = item.tipo === 'imagem' ? item.imagem.id : item.g.chave
        const ultimoComMais = i === mostrados.length - 1 && faltam > 0
        if (noCartao || item.tipo === 'guardando') {
          return (
            <Foto key={chave} notaId={nota.id} item={item} larga={larga} primeira={i === 0} noCartao={noCartao}>
              {ultimoComMais && <span className="faltam">+{faltam}</span>}
            </Foto>
          )
        }
        const posicao = imagens.indexOf(item.imagem)
        return (
          <Foto key={chave} notaId={nota.id} item={item} larga={larga} primeira={i === 0} noCartao={false}>
            <button
              type="button"
              className="foto-abrir"
              aria-label={`Abrir a imagem ${posicao + 1} de ${imagens.length}`}
              onClick={() => aoAbrir?.(posicao)}
            >
              {ultimoComMais && (
                <span className="faltam" aria-hidden="true">
                  +{faltam}
                </span>
              )}
            </button>
            {aoTirar && (
              <button
                type="button"
                className="tirar"
                aria-label={`Tirar a imagem ${posicao + 1} da nota`}
                title="Tirar da nota"
                onClick={() => aoTirar(item.imagem)}
              >
                <Icone nome="fechar" />
              </button>
            )}
          </Foto>
        )
      })}
    </div>
  )
}

interface PropsDaImagemGrande {
  notaId: string
  // imagens: as da nota agora (sem as tiradas); indice, a que está à vista.
  imagens: readonly ImagemDaNota[]
  indice: number
  // titulo: o nome da nota, no "3 de 5 · Quadro da reunião".
  titulo: string
  aoMudar(indice: number): void
  aoFechar(): void
  aoTirar(imagem: ImagemDaNota): void
}

// ImagemGrande é a imagem aberta sobre a tela escura (a cena 4 da prévia): "3 de
// 5", as setas para as outras da nota, Baixar (a cópia reduzida, a única que
// existe, por um <a download> no clique da pessoa), Tirar da nota e fechar (Esc).
// Escura nos dois temas, como todo visor de foto.
export function ImagemGrande({ notaId, imagens, indice, titulo, aoMudar, aoFechar, aoTirar }: PropsDaImagemGrande) {
  const loja = useImagensNaTela()
  const caixa = useRef<HTMLDivElement>(null)
  const fechar = useRef<HTMLButtonElement>(null)
  const n = imagens.length
  const imagem = imagens[Math.min(indice, n - 1)]
  const id = imagem?.id
  const aCaminho = imagem?.estado === 'a_caminho'
  const semRede = id ? (loja?.semRede(id) ?? false) : false
  useEffect(() => {
    if (id && !aCaminho) loja?.pedir(notaId, id)
  }, [loja, notaId, id, aCaminho, semRede])
  // O foco entra no fechar ao abrir; quem abriu recebe de volta ao fechar (a nota
  // aberta cuida disso).
  useLayoutEffect(() => {
    fechar.current?.focus()
  }, [])
  if (!imagem) return null
  const url = loja?.url(imagem.id)
  const atual = Math.min(indice, n - 1) + 1
  const ir = (passo: number) => aoMudar((Math.min(indice, n - 1) + passo + n) % n)

  function aoTeclar(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      aoFechar()
      return
    }
    if (n > 1 && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
      e.preventDefault()
      ir(e.key === 'ArrowLeft' ? -1 : 1)
      return
    }
    prenderOFoco(e, caixa.current)
  }

  return (
    <div ref={caixa} className="luz" role="dialog" aria-modal="true" aria-label={`Imagem ${atual} de ${n}`} onKeyDown={aoTeclar}>
      <div className="topo-luz">
        <span className="conta">
          {atual} de {n}
          {titulo && ` · ${titulo}`}
        </span>
        {url ? (
          <a className="botao-luz" href={url} download={`nota-imagem-${atual}.jpg`} title="Baixar">
            <Icone nome="baixar" />
            <span className="rotulo-luz">Baixar</span>
          </a>
        ) : (
          <button type="button" className="botao-luz" disabled title="Baixar">
            <Icone nome="baixar" />
            <span className="rotulo-luz">Baixar</span>
          </button>
        )}
        <button type="button" className="botao-luz" title="Tirar da nota" onClick={() => aoTirar(imagem)}>
          <Icone nome="lixeira" />
          <span className="rotulo-luz">Tirar da nota</span>
        </button>
        <button ref={fechar} type="button" className="botao-luz" aria-label="Fechar a imagem" title="Fechar (Esc)" onClick={aoFechar}>
          <Icone nome="fechar" />
        </button>
      </div>
      <div className="imagem-grande">
        {url ? (
          <img src={url} alt={`Imagem ${atual} de ${n} da nota`} draggable={false} />
        ) : aCaminho ? (
          <span className="foto-aviso">Imagem a caminho</span>
        ) : semRede ? (
          <span className="foto-aviso">Imagem sem rede</span>
        ) : loja?.falhou(imagem.id) ? (
          <span className="foto-aviso">Imagem indisponível</span>
        ) : null}
      </div>
      {n > 1 && (
        <>
          <button type="button" className="seta-luz esq" aria-label="Imagem anterior" onClick={() => ir(-1)}>
            ‹
          </button>
          <button type="button" className="seta-luz dir" aria-label="Próxima imagem" onClick={() => ir(1)}>
            ›
          </button>
        </>
      )}
    </div>
  )
}
