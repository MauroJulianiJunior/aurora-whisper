// A página de calibração do microfone (ADR-021, C6c): SÓ do `pnpm dev`
// (calibracao-dev.html), aberta no Chrome do Android pelo cabo USB (o
// encaminhamento de porta do docs/teste-manual-nuvem-f4.md, §9). Mede, no
// próprio aparelho, com as mesmas restrições do getUserMedia e a mesma medição
// do gravador (calibracao.ts), o piso e os picos em dBFS, a distribuição do
// RMS, a fala medida e os bytes por segundo do MediaRecorder.
//
// MOSTRA SÓ NÚMEROS. Nenhum arquivo, nenhum envio: o áudio é lido quadro a
// quadro como um nível, os pedaços do MediaRecorder viram só o tamanho, e tudo
// é jogado fora. A página não tem botão de salvar, baixar, copiar nem enviar:
// quem calibra tira uma captura da tela e passa os números.

import { type ReactNode, useEffect, useRef, useState } from 'react'
import { erroDoMicrofone } from './erros'
import type { Plataforma } from './gravador'
import { plataformaDoNavegador } from './navegador'
import {
  type AbrirCaptura,
  type Captura,
  type Fase,
  type InfoDaCaptura,
  Medicao,
  type Resumo,
  abrirCapturaDoNavegador,
  doApp,
  duracaoDaFase,
  formatarDB,
  formatarInteiro,
  formatarPct,
  formatarRMS,
  formatarSeg,
} from './calibracao'

const nomeDaFase: Record<Fase, string> = { silencio: 'Silêncio', fala: 'Fala' }

interface Resultado {
  resumo: Resumo
  info: InfoDaCaptura
}

interface Props {
  // abrir e relogio: o microfone e o relógio do navegador, ou os dublês dos
  // testes (nenhum teste abre o microfone de verdade).
  abrir?: AbrirCaptura
  relogio?: Pick<Plataforma, 'agora' | 'quadro' | 'depois'>
}

const simNao = (v: boolean | string | null) => (v === null ? 'não informado' : typeof v === 'string' ? v : v ? 'sim' : 'não')

export function PaginaDeCalibracao({ abrir = abrirCapturaDoNavegador, relogio }: Props) {
  const [navegador] = useState(() => relogio ?? plataformaDoNavegador())
  const [medindo, setMedindo] = useState<{ fase: Fase; restamMs: number; rms: number } | null>(null)
  const [resultados, setResultados] = useState<Partial<Record<Fase, Resultado>>>({})
  const [erro, setErro] = useState<string | null>(null)
  const sessao = useRef<{ parar(): void } | null>(null)

  // Sair da página com o microfone aberto o desliga.
  useEffect(() => () => sessao.current?.parar(), [])

  async function medir(fase: Fase) {
    if (medindo) return
    setErro(null)
    setMedindo({ fase, restamMs: duracaoDaFase[fase], rms: 0 })
    let captura: Captura
    try {
      captura = await abrir()
    } catch (e) {
      setMedindo(null)
      setErro(erroDoMicrofone(e).frase)
      return
    }

    const medicao = new Medicao()
    const inicio = navegador.agora()
    let ultimaNaTela = -Infinity
    let encerrado = false
    let cancelaQuadro: () => void = () => undefined
    let cancelaPrazo: () => void = () => undefined
    const soltarPedacos = captura.aoPedaco((bytes) => medicao.pedaco(bytes))

    const ler = () => {
      const t = navegador.agora()
      const rms = captura.nivel()
      medicao.amostrar(t, rms)
      // A tela se atualiza umas 5 vezes por segundo, não a cada quadro.
      if (t - ultimaNaTela >= 200) {
        ultimaNaTela = t
        setMedindo({ fase, restamMs: Math.max(0, duracaoDaFase[fase] - (t - inicio)), rms })
      }
      cancelaQuadro = navegador.quadro(ler)
    }

    const acabar = async () => {
      if (encerrado) return
      encerrado = true
      cancelaQuadro()
      cancelaPrazo()
      await captura.parar()
      soltarPedacos()
      setResultados((r) => ({ ...r, [fase]: { resumo: medicao.resumo(navegador.agora()), info: captura.info } }))
      setMedindo(null)
      sessao.current = null
    }

    cancelaQuadro = navegador.quadro(ler)
    cancelaPrazo = navegador.depois(duracaoDaFase[fase], () => void acabar())
    sessao.current = { parar: () => void acabar() }
  }

  const fases: Fase[] = ['silencio', 'fala']
  return (
    <main className="painel calibracao">
      <h1 className="secao-titulo">Calibração do microfone</h1>
      <p className="secao-intro">
        Só no <code>pnpm dev</code>. Mede o microfone deste aparelho como o gravador do ditado o mede, e mostra só números: nenhum áudio é gravado em arquivo nem sai
        daqui.
      </p>

      <section className="secao" aria-labelledby="calibracao-roteiro">
        <h2 id="calibracao-roteiro" className="secao-titulo">
          Roteiro
        </h2>
        <ol className="calibracao-roteiro">
          <li>Segure o celular como você dita. Toque em Medir o silêncio e fique quieto por {formatarSeg(duracaoDaFase.silencio)}, no lugar onde você costuma ditar.</li>
          <li>Toque em Medir a fala e leia um texto em voz normal por {formatarSeg(duracaoDaFase.fala)}, com pausas, como num ditado.</li>
          <li>Repita a fala mais baixo, ou mais longe, se quiser ver o limite. Cada medição refaz o resultado dela.</li>
          <li>Tire uma captura da tela e passe os números. A página não guarda nada.</li>
        </ol>
      </section>

      <section className="secao" aria-labelledby="calibracao-app">
        <h2 id="calibracao-app" className="secao-titulo">
          O que o app usa hoje
        </h2>
        <dl className="calibracao-app">
          <Linha nome="Limiar de fala (RMS)" valor={formatarRMS(doApp.limiarDeFala)} />
          <Linha nome="Fala mínima para enviar" valor={`${doApp.falaMinimaMs} ms, sem os últimos ${doApp.fimDescartadoMs} ms`} />
          <Linha nome="Onda (piso e teto)" valor={`${doApp.pisoDBFS} a ${doApp.tetoDBFS} dBFS, janela de ${formatarSeg(doApp.janelaDaOndaMs)}`} />
          <Linha nome="Gravação" valor={`${doApp.tipoDoAudio} a ${formatarInteiro(doApp.bitsPorSegundo / 1000)} kbit/s`} />
          <Linha nome="Teto de duração e de corpo" valor={`${formatarInteiro(doApp.tetoMs / 1000)} s, ${formatarInteiro(doApp.tetoDoCorpoBytes / 1024)} KiB`} />
          <Linha nome="Janela do analisador" valor={`${formatarInteiro(doApp.janelaDoAnalisador)} amostras`} />
        </dl>
      </section>

      <div className="acoes">
        {fases.map((fase) => (
          <button key={fase} type="button" className="botao primario" aria-disabled={medindo !== null || undefined} onClick={() => void medir(fase)}>
            {fase === 'silencio' ? 'Medir o silêncio' : 'Medir a fala'} ({formatarSeg(duracaoDaFase[fase])})
          </button>
        ))}
        <button type="button" className="botao" aria-disabled={medindo === null || undefined} onClick={() => sessao.current?.parar()}>
          Parar agora
        </button>
        <button type="button" className="botao" aria-disabled={medindo !== null || undefined} onClick={() => medindo === null && setResultados({})}>
          Zerar
        </button>
      </div>

      <p className="ajuda" role="status">
        {medindo
          ? `Medindo ${medindo.fase === 'silencio' ? 'o silêncio' : 'a fala'}: restam ${formatarSeg(medindo.restamMs)}. Nível agora: RMS ${formatarRMS(medindo.rms)}.`
          : ''}
      </p>
      {erro && (
        <p className="nota" role="alert">
          {erro}
        </p>
      )}

      {fases.map((fase) => {
        const r = resultados[fase]
        return r ? <Medido key={fase} fase={fase} resultado={r} /> : null
      })}
    </main>
  )
}

function Linha({ nome, valor }: { nome: string; valor: ReactNode }) {
  return (
    <div className="calibracao-linha">
      <dt>{nome}</dt>
      <dd>{valor}</dd>
    </div>
  )
}

function Medido({ fase, resultado }: { fase: Fase; resultado: Resultado }) {
  const { resumo: r, info } = resultado
  const id = `calibracao-${fase}`
  return (
    <section className="secao" aria-labelledby={id}>
      <h2 id={id} className="secao-titulo">
        {nomeDaFase[fase]}: {formatarSeg(r.duracaoMs)}, {formatarInteiro(r.quadros)} quadros
      </h2>

      <h3 className="calibracao-sub">Nível por percentil</h3>
      <table className="calibracao-tabela">
        <thead>
          <tr>
            <th scope="col">Percentil</th>
            <th scope="col">RMS</th>
            <th scope="col">dBFS</th>
          </tr>
        </thead>
        <tbody>
          {r.quantis.map((q) => (
            <tr key={q.nome}>
              <th scope="row">{q.nome}</th>
              <td>{formatarRMS(q.rms)}</td>
              <td>{formatarDB(q.dbfs)}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h3 className="calibracao-sub">Tempo acima de cada limiar de RMS</h3>
      <table className="calibracao-tabela">
        <thead>
          <tr>
            <th scope="col">Limiar</th>
            <th scope="col">Tempo</th>
            <th scope="col">Da janela</th>
          </tr>
        </thead>
        <tbody>
          {r.acimaDe.map((a) => (
            <tr key={a.limiar}>
              <th scope="row">
                {formatarRMS(a.limiar)}
                {a.doApp ? ' (o do app)' : ''}
              </th>
              <td>{formatarInteiro(a.ms)} ms</td>
              <td>{formatarPct(a.fracao)}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h3 className="calibracao-sub">Fala medida pelo app</h3>
      <p className="calibracao-fato">
        {formatarInteiro(r.falaMedidaMs)} ms de fala, com o limiar de {formatarRMS(doApp.limiarDeFala)} e sem os últimos {doApp.fimDescartadoMs} ms.{' '}
        {r.enviaria ? `Passa dos ${doApp.falaMinimaMs} ms: o app enviaria.` : `Não passa dos ${doApp.falaMinimaMs} ms: o app diria "Não ouvi nada." e não enviaria.`}
      </p>

      <h3 className="calibracao-sub">Distribuição do nível, em bandas de 6 dB</h3>
      <table className="calibracao-tabela">
        <thead>
          <tr>
            <th scope="col">dBFS</th>
            <th scope="col">Quadros</th>
            <th scope="col">Da janela</th>
          </tr>
        </thead>
        <tbody>
          {r.bandas.map((b) => (
            <tr key={b.ate}>
              <th scope="row">{b.de === -Infinity ? `abaixo de ${b.ate}` : `${b.de} a ${b.ate}`}</th>
              <td>{formatarInteiro(b.quadros)}</td>
              <td>{formatarPct(b.fracao)}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h3 className="calibracao-sub">MediaRecorder</h3>
      <dl className="calibracao-app">
        <Linha nome="Pedaços de 1 s (contados e jogados fora)" valor={formatarInteiro(r.bytes.pedacos)} />
        <Linha nome="Total" valor={`${formatarInteiro(r.bytes.total)} bytes`} />
        <Linha nome="Menor e maior pedaço" valor={`${formatarInteiro(r.bytes.menor)} e ${formatarInteiro(r.bytes.maior)} bytes`} />
        <Linha nome="Por segundo" valor={`${formatarInteiro(r.bytes.porSegundo)} bytes/s = ${formatarInteiro(r.bytes.kbitPorSegundo)} kbit/s`} />
        <Linha
          nome="Pedido ao navegador, e o que o MediaRecorder diz"
          valor={`${formatarInteiro(info.bitsPedidos / 1000)} kbit/s; ${info.bitsDoRecorder === null ? 'não informado' : `${formatarInteiro(info.bitsDoRecorder / 1000)} kbit/s`}`}
        />
        <Linha
          nome={`${formatarInteiro(doApp.tetoMs / 1000)} s no mesmo ritmo`}
          valor={`${formatarInteiro(r.bytes.projetado120 / 1024)} KiB (o servidor aceita até ${formatarInteiro(doApp.tetoDoCorpoBytes / 1024)} KiB)`}
        />
      </dl>

      <h3 className="calibracao-sub">Captura</h3>
      <dl className="calibracao-app">
        <Linha nome="Taxa do AudioContext" valor={`${formatarInteiro(info.taxaDoContexto)} Hz`} />
        <Linha nome="Taxa e canais da faixa" valor={`${info.taxaDaFaixa === null ? 'não informada' : `${formatarInteiro(info.taxaDaFaixa)} Hz`}; ${info.canais ?? 'não informado'}`} />
        <Linha nome="Cancelamento de eco" valor={simNao(info.eco)} />
        <Linha nome="Supressão de ruído" valor={simNao(info.ruido)} />
        <Linha nome="Ganho automático" valor={simNao(info.ganho)} />
        <Linha nome="Tipo gravado" valor={info.tipoDoRecorder || 'não informado'} />
      </dl>
    </section>
  )
}
