// O microfone do celular (ADR-021, "A tela"): um botão pequeno ao lado da busca.
// Gravando, a linha da busca vira uma faixa com a onda, o tempo e o Cancelar, e
// o microfone vira o botão de parar. "Gravando" fica só para o leitor de tela,
// sem rótulo visível: forma e movimento no lugar de texto (o menor que
// funciona; a referência do Mauro é o Wispr Flow). Depois de parar, a faixa
// diz "Transcrevendo" e o botão fica ocupado.
//
// Quem grava e envia é o Gravador (gravador.ts); aqui só se desenha o estado
// dele. A falha que guarda o áudio (sem conexão, o provedor que não respondeu)
// fica numa nota embaixo da linha, com "Tentar de novo" e "Descartar"; a que
// não guarda (sem fala, longo demais) vira um aviso da pílula, e some.

import { type ReactNode, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import type { PropsDoDitar } from '../../ambiente'
import type { ClienteNuvem } from '../cliente'
import { Gravador, type Plataforma } from './gravador'
import { barrasDaOnda, formatarTempo } from './medidas'
import { plataformaDoNavegador } from './navegador'

interface Props extends PropsDoDitar {
  cliente: ClienteNuvem
  // plataforma: o navegador de verdade, ou o dublê dos testes e das telas de
  // desenvolvimento.
  plataforma?: Plataforma
  // liberar: a porta antes do microfone (a folha do aceite, na C6b).
  liberar?(): Promise<boolean>
}

export function Ditar({ cliente, plataforma, liberar, busca, escondido, aoChegar, aoAudioPendente, aoSemSessao, abrirConfiguracoes, avisar }: Props) {
  const [base] = useState(() => {
    const p = plataforma ?? plataformaDoNavegador()
    return { g: new Gravador({ plataforma: p, enviar: (pedido, sinal) => cliente.ditar(pedido, sinal), liberar }), suporta: p.suporta() }
  })
  const { g, suporta } = base
  const estado = useSyncExternalStore(g.assinar, g.estado)

  // Os ganchos do Painel mudam a cada render dele; o gravador fala com a versão
  // de agora.
  const ganchos = useRef({ aoChegar, aoAudioPendente, aoSemSessao, abrirConfiguracoes, avisar })
  useLayoutEffect(() => {
    ganchos.current = { aoChegar, aoAudioPendente, aoSemSessao, abrirConfiguracoes, avisar }
  })

  useEffect(() => {
    const soltar = [
      g.aoChegar((c) => ganchos.current.aoChegar(c.ditado, { nota: c.nota, aviso: c.aviso })),
      g.aoAudio((tem) => ganchos.current.aoAudioPendente(tem)),
      g.aoSessaoPerdida(() => ganchos.current.aoSemSessao()),
    ]
    return () => {
      for (const f of soltar) f()
      // A tela saiu de cena (a sessão acabou sem áudio a perder, o Sair): o
      // microfone se desliga e o áudio se solta.
      g.encerrar()
    }
  }, [g])

  // A falha que não guarda áudio é um aviso da pílula, e a linha volta ao normal.
  useEffect(() => {
    if (estado.fase !== 'erro' || estado.erro.retem) return
    ganchos.current.avisar(estado.erro.frase)
    g.dispensar()
  }, [estado, g])

  // Sem getUserMedia, MediaRecorder ou AudioContext, o microfone nem aparece.
  if (!suporta) return <>{busca}</>

  const fase = estado.fase
  const gravando = fase === 'gravando'
  const enviando = fase === 'enviando'
  const retido = estado.fase === 'erro' && estado.erro.retem
  const ocupado = fase === 'pedindo' || enviando || retido
  // Nas Configurações a linha some, a não ser que haja uma gravação em curso
  // ou um áudio esperando: o botão de parar e a nota não podem sumir.
  const aVista = !escondido || gravando || ocupado

  let rotulo = 'Ditar'
  if (gravando) rotulo = 'Parar e enviar'
  else if (fase === 'pedindo') rotulo = 'Abrindo o microfone'
  else if (enviando) rotulo = 'Transcrevendo'
  const falado = gravando || fase === 'pedindo' || enviando ? (gravando ? 'Gravando' : rotulo) : ''

  let miolo: ReactNode = busca
  if (gravando) {
    miolo = (
      <div
        className="ditar-faixa"
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            g.cancelar()
          }
        }}
      >
        <Onda gravador={g} />
        <Tempo gravador={g} />
        <button type="button" className="ditar-cancelar" onClick={() => g.cancelar()}>
          Cancelar
        </button>
      </div>
    )
  } else if (enviando) {
    miolo = (
      <div className="ditar-faixa">
        <span className="ditar-enviando" aria-hidden="true">
          Transcrevendo…
        </span>
      </div>
    )
  }

  return (
    <div className="ditar" data-fase={fase}>
      {aVista && (
        <div className="ditar-linha">
          {miolo}
          <button
            type="button"
            className={gravando ? 'ditar-botao gravando' : 'ditar-botao'}
            aria-label={rotulo}
            title={rotulo}
            aria-disabled={ocupado || undefined}
            onClick={() => {
              if (gravando) void g.parar()
              else if (!ocupado) void g.iniciar()
            }}
          >
            {gravando ? <IconeParar /> : <IconeMicrofone />}
          </button>
        </div>
      )}
      {gravando && estado.telaPodeApagar && <p className="ditar-aviso">A tela pode apagar e parar o ditado.</p>}
      {estado.fase === 'erro' && estado.erro.retem && (
        <div className="nota ditar-erro" role="alert">
          <span className="ditar-frase">{estado.erro.frase}</span>
          <span className="ditar-acoes">
            {estado.erro.vai === 'configuracoes' && (
              <button type="button" className="botao" onClick={() => ganchos.current.abrirConfiguracoes()}>
                Abrir as Configurações
              </button>
            )}
            <button type="button" className="botao" onClick={() => void g.tentarDeNovo()}>
              Tentar de novo
            </button>
            <button type="button" className="botao" onClick={() => g.descartar()}>
              Descartar
            </button>
          </span>
        </div>
      )}
      {/* A região viva existe sempre: um leitor de tela só anuncia o que ENTRA
          numa região que já estava na página. */}
      <p className="so-leitor" role="status">
        {falado}
      </p>
    </div>
  )
}

// Onda desenha as 33 barras do overlay do PC: espelhadas, em cinza, o brilho
// acompanhando a altura (ADR-014). Escreve direto no estilo das barras a cada
// quadro, e não passa pelo React: 60 renders por segundo para mexer em 33
// <span> não valem.
function Onda({ gravador }: { gravador: Gravador }) {
  const barras = useRef<(HTMLSpanElement | null)[]>([])
  useEffect(
    () =>
      gravador.aoQuadro(({ barras: alturas }) => {
        alturas.forEach((a, i) => {
          const el = barras.current[i]
          if (!el) return
          el.style.transform = `scaleY(${(0.12 + 0.88 * a).toFixed(3)})`
          el.style.opacity = (0.45 + 0.55 * a).toFixed(3)
        })
      }),
    [gravador],
  )
  const itens: ReactNode[] = []
  for (let i = 0; i < barrasDaOnda; i++) {
    itens.push(
      <span
        key={i}
        className="onda-barra"
        ref={(el) => {
          barras.current[i] = el
        }}
      />,
    )
  }
  return (
    <span className="onda" aria-hidden="true">
      {itens}
    </span>
  )
}

// Tempo é o "m:ss" ao lado da onda. Só vira estado quando o texto muda: o
// quadro chega 60 vezes por segundo, e o React ignora o mesmo texto.
function Tempo({ gravador }: { gravador: Gravador }) {
  const [texto, setTexto] = useState('0:00')
  useEffect(() => gravador.aoQuadro((q) => setTexto(formatarTempo(q.decorrido))), [gravador])
  return (
    <span className="ditar-tempo" aria-hidden="true">
      {texto}
    </span>
  )
}

function IconeMicrofone() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="6.25" y="1.75" width="5.5" height="9" rx="2.75" />
      <path d="M3.5 8.5a5.5 5.5 0 0 0 11 0M9 14v2.25" />
    </svg>
  )
}

function IconeParar() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true" focusable="false" fill="currentColor">
      <rect x="4.5" y="4.5" width="9" height="9" rx="2" />
    </svg>
  )
}
