// Os dois microfones das notas no celular (ADR-022, N8; a prévia
// docs/notas-previa-n8.html, cenas 2 e 3):
//
//   - "Ditar para nota", o botão embaixo da aba Notas, ao alcance do polegar: a
//     regra do Ctrl+Win+Alt do PC, na nuvem (o título no começo leva o resto ao
//     fim daquela nota; sem título, nasce uma nota nova). O aviso diz o destino,
//     com Abrir e Desfazer.
//   - O microfone dentro da nota aberta, na barra de baixo: o texto entra onde
//     está o cursor, como segurar Ctrl+Win dentro da nota no PC.
//
// Nos dois, o ditado não vai ao histórico de aparelho nenhum: o cabeçalho
// Ditado-Nota faz a nuvem gravá-lo já ligado à nota. O gravador é o mesmo do
// ditado do histórico (ditar/gravador.ts): o áudio só na memória, o mesmo id
// em toda tentativa, a porta do aceite antes do microfone.

import { type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { ErroDaApi, SemSessao } from '../../api'
import type { PropsDoDitarNaNota, PropsDoDitarParaNota } from '../../notas/tipos'
import type { ClienteNuvem, DitadoGravado } from '../cliente'
import { IconeMicrofone, IconeParar, Onda, Tempo } from '../ditar/Ditar'
import { type Chegada, Gravador, type Plataforma } from '../ditar/gravador'
import { plataformaDoNavegador } from '../ditar/navegador'
import type { GanchosDaPorta, Porta } from '../ditar/porta'
import { daNuvem } from './formato'
import type { NotasNaNuvem } from './notasNaNuvem'

// As recusas do servidor que mostram que a porta sabia errado (DitarNaNuvem).
const recusasDaPorta = new Set(['SEM_ACEITE', 'SEM_CHAVE', 'PROVEDOR_RECUSOU_CHAVE'])

// O que os dois componentes recebem do ambiente da nuvem.
export interface DoAmbiente {
  cliente: ClienteNuvem
  notas: NotasNaNuvem
  porta: Porta
  plataforma?: Plataforma
}

interface Ganchos {
  avisar(texto: ReactNode): void
  aoPerderLigacao(e: unknown): void
  abrirConfiguracoes?(): void
}

// EnvioDaNota leva o ditado à nuvem com o destino (nota: "para-nota" ou o id da
// nota aberta) e guarda a última resposta inteira (a nota e o destino), que a
// chegada do gravador não traz.
class EnvioDaNota {
  private ultimo: DitadoGravado | null = null
  private d: DoAmbiente
  private nota: string
  constructor(d: DoAmbiente, nota: string) {
    this.d = d
    this.nota = nota
  }
  enviar = async (pedido: Parameters<ClienteNuvem['ditar']>[0], sinal: AbortSignal): Promise<DitadoGravado> => {
    try {
      const r = await this.d.cliente.ditar({ ...pedido, nota: this.nota }, sinal)
      this.ultimo = r
      return r
    } catch (e) {
      if (e instanceof ErroDaApi && e.codigo && recusasDaPorta.has(e.codigo)) this.d.porta.invalidar()
      throw e
    }
  }
  tirar(): DitadoGravado | null {
    const r = this.ultimo
    this.ultimo = null
    return r
  }
}

// useLiberar é a porta do aceite antes do microfone, com os ganchos de agora
// (os do Painel mudam a cada render).
function useLiberar(porta: Porta, ganchos: Ganchos): () => Promise<boolean> {
  const atuais = useRef(ganchos)
  useLayoutEffect(() => {
    atuais.current = ganchos
  })
  return useCallback(() => {
    const daPorta: GanchosDaPorta = {
      avisar: (t) => atuais.current.avisar(t),
      abrirConfiguracoes: () => atuais.current.abrirConfiguracoes?.(),
      aoSemSessao: () => atuais.current.aoPerderLigacao(new SemSessao()),
    }
    return porta.liberar(daPorta)
  }, [porta])
}

// useGravadorDaNota monta o gravador de um destino e liga as chegadas. O
// gravador é um por componente: o áudio que espera "Tentar de novo" fica entre um
// render e outro.
function useGravadorDaNota(
  d: DoAmbiente,
  nota: string,
  liberar: () => Promise<boolean>,
  ganchos: Ganchos,
  aoChegar: (c: Chegada, g: DitadoGravado | null) => void,
) {
  const atuais = useRef({ ganchos, aoChegar })
  useLayoutEffect(() => {
    atuais.current = { ganchos, aoChegar }
  })
  const [base] = useState(() => {
    const p = d.plataforma ?? plataformaDoNavegador()
    const envio = new EnvioDaNota(d, nota)
    const g = new Gravador({ plataforma: p, liberar, enviar: envio.enviar })
    return { g, envio, suporta: p.suporta() }
  })
  const { g, envio, suporta } = base
  const estado = useSyncExternalStore(g.assinar, g.estado)

  useEffect(() => {
    const soltar = [
      g.aoChegar((c) => atuais.current.aoChegar(c, envio.tirar())),
      g.aoSessaoPerdida(() => atuais.current.ganchos.aoPerderLigacao(new SemSessao())),
    ]
    return () => {
      for (const f of soltar) f()
      g.encerrar()
    }
  }, [g, envio])

  // A falha que não guarda áudio (sem fala, longo demais) é um aviso, e o botão
  // volta ao normal.
  useEffect(() => {
    if (estado.fase !== 'erro' || estado.erro.retem) return
    atuais.current.ganchos.avisar(estado.erro.frase)
    g.dispensar()
  }, [estado, g])

  return { g, suporta, estado }
}

// ErroRetido é a falha que guarda o áudio: a frase, Tentar de novo e Descartar.
function ErroRetido({ g, frase, configuracoes, abrirConfiguracoes }: { g: Gravador; frase: string; configuracoes: boolean; abrirConfiguracoes?(): void }) {
  return (
    <div className="nota ditar-erro" role="alert">
      <span className="ditar-frase">{frase}</span>
      <span className="ditar-acoes">
        {configuracoes && abrirConfiguracoes && (
          <button type="button" className="botao" onClick={abrirConfiguracoes}>
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
  )
}

// DitarParaNota é o botão "Ditar para nota" de baixo da aba Notas, e a faixa da
// gravação no lugar dele enquanto grava.
export function DitarParaNota(props: DoAmbiente & PropsDoDitarParaNota) {
  const liberar = useLiberar(props.porta, props)
  return <GravadorParaNota {...props} liberar={liberar} />
}

function GravadorParaNota({
  cliente,
  notas,
  porta,
  plataforma,
  desligado,
  aoChegar,
  avisar,
  aoPerderLigacao,
  abrirConfiguracoes,
  liberar,
}: DoAmbiente & PropsDoDitarParaNota & { liberar(): Promise<boolean> }) {
  const { g, suporta, estado } = useGravadorDaNota(
    { cliente, notas, porta, plataforma },
    'para-nota',
    liberar,
    { avisar, aoPerderLigacao, abrirConfiguracoes },
    (c, r) => {
      if (!r?.nota || !r.destino) {
        avisar('O ditado foi para uma nota. Abra a lista de novo para ver.')
        return
      }
      const nota = daNuvem(r.nota)
      notas.receber(nota)
      const corpoAntes = r.destino.corpo_antes
      aoChegar({
        nota,
        nova: r.destino.nova,
        linhas: r.destino.linhas,
        desfazer: () => cliente.desfazerDitadoNaNota(c.ditado.id, nota.version, corpoAntes),
      })
    },
  )
  if (!suporta) return null
  const fase = estado.fase
  const falado = fase === 'gravando' ? 'Gravando para nota' : fase === 'enviando' ? 'Transcrevendo' : fase === 'pedindo' ? 'Abrindo o microfone' : ''

  let miolo: ReactNode
  if (fase === 'gravando') {
    miolo = (
      <div
        className="gravando-nota"
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            g.cancelar()
          }
        }}
      >
        <span className="rotulo-g">Para nota</span>
        <Onda gravador={g} />
        <Tempo gravador={g} />
        <button type="button" className="cancelar" onClick={() => g.cancelar()}>
          Cancelar
        </button>
        <button type="button" className="parar" aria-label="Parar e enviar" title="Parar e enviar" onClick={() => void g.parar()}>
          <IconeParar />
        </button>
      </div>
    )
  } else if (fase === 'enviando') {
    miolo = (
      <div className="gravando-nota">
        <span className="rotulo-g">Transcrevendo…</span>
      </div>
    )
  } else if (estado.fase === 'erro' && estado.erro.retem) {
    miolo = <ErroRetido g={g} frase={estado.erro.frase} configuracoes={estado.erro.vai !== undefined} abrirConfiguracoes={abrirConfiguracoes} />
  } else {
    miolo = (
      <button
        type="button"
        className="ditar-nota"
        disabled={desligado}
        aria-disabled={fase === 'pedindo' || undefined}
        title={desligado ? 'Sem rede: ditar espera a rede.' : undefined}
        onClick={() => {
          if (fase !== 'pedindo') void g.iniciar()
        }}
      >
        <IconeMicrofone />
        Ditar para nota
      </button>
    )
  }
  return (
    <div className="ditar-para-nota" data-fase={fase}>
      {miolo}
      <p className="so-leitor" role="status">
        {falado}
      </p>
    </div>
  )
}

// DitarNaNota é o microfone da barra de baixo da nota aberta. O lugar do cursor
// é anotado quando a gravação começa (aoComecar), e o texto que chega entra ali
// (aoTexto).
export function DitarNaNota(props: DoAmbiente & PropsDoDitarNaNota) {
  const liberar = useLiberar(props.porta, props)
  return <GravadorNaNota {...props} liberar={liberar} />
}

function GravadorNaNota({
  cliente,
  notas,
  porta,
  plataforma,
  notaId,
  desligado,
  aoComecar,
  aoTexto,
  avisar,
  aoPerderLigacao,
  liberar,
}: DoAmbiente & PropsDoDitarNaNota & { liberar(): Promise<boolean> }) {
  const { g, suporta, estado } = useGravadorDaNota({ cliente, notas, porta, plataforma }, notaId, liberar, { avisar, aoPerderLigacao }, (c) =>
    aoTexto(c.ditado.final_text),
  )
  if (!suporta) return null
  const fase = estado.fase
  const falado = fase === 'gravando' ? 'Gravando na nota' : fase === 'enviando' ? 'Transcrevendo' : ''
  return (
    <>
      {fase === 'gravando' && (
        <span
          className="gravando-na-nota"
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault()
              e.stopPropagation()
              g.cancelar()
            }
          }}
        >
          <Onda gravador={g} />
          <Tempo gravador={g} />
          <button type="button" className="ditar-cancelar" onMouseDown={(e) => e.preventDefault()} onClick={() => g.cancelar()}>
            Cancelar
          </button>
        </span>
      )}
      {fase === 'enviando' && (
        <span className="gravando-na-nota" aria-hidden="true">
          Transcrevendo…
        </span>
      )}
      <button
        type="button"
        className={fase === 'gravando' ? 'icone-botao mic-nota gravando' : 'icone-botao mic-nota'}
        aria-label={fase === 'gravando' ? 'Parar e escrever na nota' : 'Ditar aqui'}
        title={desligado ? 'Sem rede: ditar espera a rede.' : fase === 'gravando' ? 'Parar e escrever na nota' : 'Ditar aqui'}
        disabled={desligado && fase !== 'gravando'}
        aria-disabled={fase === 'pedindo' || fase === 'enviando' || (estado.fase === 'erro' && estado.erro.retem) || undefined}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => {
          if (fase === 'gravando') void g.parar()
          else if (fase === 'parado' || fase === 'pronto') {
            aoComecar()
            void g.iniciar()
          }
        }}
      >
        {fase === 'gravando' ? <IconeParar /> : <IconeMicrofone />}
      </button>
      {estado.fase === 'erro' && estado.erro.retem && <ErroRetido g={g} frase={estado.erro.frase} configuracoes={false} />}
      <span className="so-leitor" role="status">
        {falado}
      </span>
    </>
  )
}
