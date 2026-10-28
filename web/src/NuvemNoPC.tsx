import { type KeyboardEvent, type ReactNode, useEffect, useEffectEvent, useRef, useState } from 'react'
import { ErroDaApi, problemaDe } from './api'
import type { AvisoDaNuvem, Cliente, EstadoNuvem, ResumoDoHistorico } from './clienteLocal'
import { formatarHora } from './datas'
import { freioDoAprendido, naoSubiram, naoSubiuFica, numero, vaoSubir } from './textosDaNuvem'

// A seção "Nuvem" das Configurações do PC (ADR-019, "Conectar o PC"; F6 e
// F7c). A nuvem começa desligada: nada vai para ela até a pessoa colar o
// endereço e clicar em Conectar à nuvem. (A frase da tela diz "nada vai para
// a nuvem", e não "nada sai deste PC": com o Polish ligado, o texto vai ao
// provedor de IA.) Aí o Agent abre a página de aprovação no navegador, e o
// painel mostra o código de conferência, o mesmo da página. A pessoa aprova
// lá, o navegador volta ao PC, e o painel, quando ela volta a esta aba,
// mostra "Conectado".
//
// Conectado (F7c), o histórico de antes da conexão sobe com um clique (D1):
// a seção diz quantos ditados vão, que a lixeira fica e que antes fica uma
// cópia do histórico no PC, e o "Enviar o histórico" faz a cópia e põe os
// ditados na fila. Enquanto o envio corre, a seção relê o Agent a cada 2 s
// (só com ela à vista; é leitura local, sem rede) e diz "Enviando o
// histórico: 120 de 800." Desconectado, com uma nuvem gravada, há o
// "Esquecer esta nuvem", para ligar o PC a outra nuvem ou conta, com a
// pergunta na própria linha.
//
// Fora isso, sem laço: o painel pergunta ao Agent quando a tela aparece e
// quando a aba volta a ficar à vista (o Agent fala com a nuvem no máximo a
// cada 30 s). Age na hora, como o Dicionário: fica fora do Salvar
// configurações.

interface Props {
  cliente: Cliente
  visivel: boolean
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
  // aoMudarEstado: o estado novo, a cada leitura (a Privacidade diz para
  // onde o texto vai quando o PC está conectado).
  aoMudarEstado?(e: EstadoNuvem): void
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

// O intervalo da releitura enquanto o envio do histórico corre.
const RELEITURA_DO_ENVIO_MS = 2000

// O que cada aviso do Agent diz na tela (08-ui-ux-spec.md, "Nuvem, no PC").
// risco: a frase vai numa nota (o que deu errado); senão, é informação.
const avisos: Record<AvisoDaNuvem, { texto: string; risco: boolean }> = {
  revogado: { texto: 'Este PC foi desconectado na nuvem.', risco: false },
  sem_resposta: { texto: 'Desconectado neste PC. A nuvem não respondeu: revogue-o em Aparelhos, no app.', risco: false },
  nao_abriu: { texto: 'O navegador não abriu. Use Abrir de novo.', risco: true },
  vencido: { texto: 'O pedido venceu. Clique em Conectar à nuvem de novo.', risco: true },
  outra_conta: { texto: 'Este PC já está ligado a outra conta.', risco: true },
  falhou: { texto: 'Não deu para conectar. Clique em Conectar à nuvem de novo.', risco: true },
  sem_rede: { texto: 'A nuvem não respondeu agora. O PC continua conectado.', risco: false },
}

// Só um endereço que o Agent montou e que é da nuvem vira link.
const enderecoDeAprovar = (u: string | undefined) => (u && /^(https:\/\/|http:\/\/localhost[:/])/.test(u) ? u : undefined)

type Acao = 'conectar' | 'cancelar' | 'desconectar' | 'enviar' | 'esquecer'

export function NuvemNoPC({ cliente, visivel, aoPerderLigacao, avisar, aoMudarEstado }: Props) {
  const [estado, setEstado] = useState<EstadoNuvem | null>(null)
  // indisponivel: o Agent não tem a nuvem (de antes da F6, ou sem o banco).
  const [indisponivel, setIndisponivel] = useState<string | null>(null)
  const [url, setUrl] = useState('')
  // mexeu: a pessoa já escreveu no campo; o endereço do Agent não passa por
  // cima do que ela digitou.
  const mexeu = useRef(false)
  const [indo, setIndo] = useState<Acao | null>(null)
  const [erro, setErro] = useState<{ texto: string; vez: number } | null>(null)
  // confirmando: a pergunta do "Esquecer esta nuvem" está aberta.
  const [confirmando, setConfirmando] = useState(false)
  const campo = useRef<HTMLInputElement>(null)
  const abrirDeNovo = useRef<HTMLAnchorElement>(null)
  const botaoEsquecer = useRef<HTMLButtonElement>(null)
  const cancelarEsquecer = useRef<HTMLButtonElement>(null)
  // focar: para onde o foco vai depois que o botão clicado sumir com o
  // estado (o render seguinte).
  const focar = useRef<'campo' | 'abrir' | 'esquecer' | 'cancelar' | null>(null)
  // enviando: a última leitura tinha o envio em curso; quando ele some, a
  // seção avisa que o histórico subiu.
  const enviando = useRef(false)
  const perderLigacao = useEffectEvent(aoPerderLigacao)

  function aplicar(e: EstadoNuvem) {
    const estavaEnviando = enviando.current
    enviando.current = e.estado === 'conectado' && Boolean(e.historico?.envio)
    if (estavaEnviando && !enviando.current && e.estado === 'conectado' && e.historico) {
      const k = e.historico.nao_subiram
      avisar(k > 0 ? `O histórico subiu para a nuvem. ${naoSubiram(k)}` : 'O histórico subiu para a nuvem.')
    }
    setEstado(e)
    setIndisponivel(null)
    if (e.estado !== 'desconectado' || !e.url) setConfirmando(false)
    if (!mexeu.current && e.url) setUrl(e.url)
    aoMudarEstado?.(e)
  }

  function falhou(e: unknown) {
    if (problemaDe(e)) aoPerderLigacao(e)
    else setErro((a) => ({ texto: mensagem(e), vez: (a?.vez ?? 0) + 1 }))
  }

  // conferir: ao aparecer e ao voltar à aba. Com o PC conectado, o Agent
  // pergunta à nuvem se o token ainda vale (o 401 vira "desconectado");
  // com o pedido em curso, só diz se a aprovação já voltou.
  const conferir = useEffectEvent(() => {
    cliente.conferirNuvem().then(aplicar, (e: unknown) => {
      if (problemaDe(e)) perderLigacao(e)
      // O painel novo com um Agent de antes da nuvem (recompilado com o
      // Agent aberto): a rota não existe, e o 404 dele não tem frase.
      else if (e instanceof ErroDaApi && e.status === 404) setIndisponivel('Este Agent ainda não tem a nuvem. Reinicie o Agent.')
      else setIndisponivel(mensagem(e))
    })
  })

  // reler: o estado do Agent, sem ir à nuvem (a releitura do envio).
  const reler = useEffectEvent(() => {
    cliente.nuvem().then(aplicar, (e: unknown) => {
      if (problemaDe(e)) perderLigacao(e)
    })
  })

  useEffect(() => {
    if (!visivel) return
    conferir()
    const aoVoltar = () => {
      if (document.visibilityState === 'visible') conferir()
    }
    document.addEventListener('visibilitychange', aoVoltar)
    return () => document.removeEventListener('visibilitychange', aoVoltar)
  }, [visivel])

  // Enquanto o envio do histórico corre, ou enquanto a primeira rodada com a
  // conexão não termina (a pergunta do envio espera por ela), e só com a
  // seção à vista: a releitura a cada 2 s (leitura local, sem rede).
  const h = estado?.estado === 'conectado' ? estado.historico : undefined
  const emEnvio = Boolean(h && (h.envio || (!h.aceito && haAEnviar(h) && !h.conferido_em)))
  useEffect(() => {
    if (!visivel || !emEnvio) return
    const id = setInterval(() => {
      if (document.visibilityState === 'visible') reler()
    }, RELEITURA_DO_ENVIO_MS)
    return () => clearInterval(id)
  }, [visivel, emEnvio])

  useEffect(() => {
    const alvo = focar.current
    if (alvo === null) return
    focar.current = null
    if (alvo === 'campo') campo.current?.focus()
    else if (alvo === 'abrir') abrirDeNovo.current?.focus()
    else if (alvo === 'esquecer') botaoEsquecer.current?.focus()
    else cancelarEsquecer.current?.focus()
  }, [estado, confirmando])

  const semEndereco = url.trim() === ''

  async function conectar() {
    if (indo || semEndereco) return
    setIndo('conectar')
    setErro(null)
    try {
      const e = await cliente.conectarNuvem(url.trim())
      focar.current = 'abrir'
      mexeu.current = false
      aplicar(e)
    } catch (e) {
      falhou(e)
    } finally {
      setIndo(null)
    }
  }

  async function desconectar(qual: 'cancelar' | 'desconectar') {
    if (indo) return
    setIndo(qual)
    setErro(null)
    try {
      const e = await cliente.desconectarNuvem()
      focar.current = 'campo'
      aplicar(e)
      if (qual === 'desconectar' && !e.aviso) avisar('Este PC foi desconectado da nuvem.')
    } catch (e) {
      falhou(e)
    } finally {
      setIndo(null)
    }
  }

  async function enviar() {
    if (indo) return
    setIndo('enviar')
    setErro(null)
    try {
      aplicar(await cliente.enviarHistorico())
    } catch (e) {
      falhou(e)
    } finally {
      setIndo(null)
    }
  }

  function perguntarEsquecer() {
    if (indo) return
    setErro(null)
    focar.current = 'cancelar'
    setConfirmando(true)
  }

  function naoEsquecer() {
    focar.current = 'esquecer'
    setConfirmando(false)
  }

  async function esquecer() {
    if (indo) return
    setIndo('esquecer')
    setErro(null)
    try {
      const e = await cliente.esquecerNuvem()
      mexeu.current = false
      setUrl('')
      focar.current = 'campo'
      setConfirmando(false)
      aplicar(e)
      avisar('Este PC esqueceu a nuvem.')
    } catch (e) {
      falhou(e)
    } finally {
      setIndo(null)
    }
  }

  function teclaNoCampo(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== 'Enter') return
    e.preventDefault()
    void conectar()
  }

  const aviso = estado?.aviso ? avisos[estado.aviso] : undefined
  const abrir = enderecoDeAprovar(estado?.abrir_url)
  const historico = estado?.historico
  // Um Agent de antes da F8 não manda o freio: vale 0.
  const freio = historico?.freio_trocas ?? 0

  return (
    <section className="secao" aria-labelledby="cfg-nuvem">
      <h2 id="cfg-nuvem" className="secao-titulo">
        Nuvem
      </h2>
      {estado === null && !indisponivel && <p className="ajuda">Carregando…</p>}
      {indisponivel && estado === null && (
        <p className="nota" role="alert">
          {indisponivel}
        </p>
      )}
      {/* O que aconteceu por último vem antes do resto: "Este PC foi
          desconectado na nuvem." e, embaixo, o campo de novo. */}
      {aviso &&
        (aviso.risco ? (
          <p className="nota" role="alert">
            {aviso.texto}
          </p>
        ) : (
          <p className="ajuda" role="status">
            {aviso.texto}
          </p>
        ))}

      {estado?.estado === 'desconectado' && (
        <>
          <p className="secao-intro">Desligada. Nada vai para a nuvem até você conectar.</p>
          <div className="ajuste">
            <label className="ajuste-nome" htmlFor="cfg-nuvem-url">
              Endereço da nuvem
            </label>
            <div className="com-botao">
              <input
                ref={campo}
                id="cfg-nuvem-url"
                className="entrada"
                type="url"
                inputMode="url"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                placeholder="https://…"
                aria-describedby="cfg-nuvem-nome"
                value={url}
                onChange={(e) => {
                  mexeu.current = true
                  setUrl(e.target.value)
                }}
                onKeyDown={teclaNoCampo}
              />
              <button type="button" className="botao" aria-disabled={semEndereco || indo !== null || undefined} onClick={() => void conectar()}>
                {indo === 'conectar' ? 'Conectando…' : 'Conectar à nuvem'}
              </button>
            </div>
            {/* O que o Conectar manda, antes do clique: o nome do computador (que
                costuma ter o nome da pessoa) vai à nuvem digitada, para a lista de
                aparelhos de lá, antes mesmo do login. */}
            <p className="ajuda" id="cfg-nuvem-nome">
              O nome deste PC vai para a nuvem que você conectar, para a lista de aparelhos.
            </p>
          </div>
          {estado.url &&
            (confirmando ? (
              <div
                className="ajuste confirmar"
                role="group"
                aria-labelledby="cfg-esquecer"
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.preventDefault()
                    naoEsquecer()
                  }
                }}
              >
                <p className="ajuda" id="cfg-esquecer">
                  <strong>Esquecer {estado.url}?</strong> Este PC deixa de saber dessa nuvem
                  {estado.email ? (
                    <>
                      {' '}
                      e da conta <span className="email">{estado.email}</span>
                    </>
                  ) : null}
                  . Os ditados continuam aqui; na nuvem, nada muda.
                </p>
                {/* Só as mudanças num ditado que a nuvem tem: o histórico e o ditado novo que não
                    subiram não são "mudanças feitas aqui" (reconferência da F7c). */}
                {historico && historico.mudancas > 0 && <p className="ajuda">{naoSubiuFica(historico.mudancas)}</p>}
                <div className="acoes">
                  <button type="button" className="botao risco" aria-disabled={indo !== null || undefined} onClick={() => void esquecer()}>
                    {indo === 'esquecer' ? 'Esquecendo…' : 'Esquecer'}
                  </button>
                  <button ref={cancelarEsquecer} type="button" className="botao" onClick={naoEsquecer}>
                    Cancelar
                  </button>
                </div>
              </div>
            ) : (
              <div className="ajuste esquecer">
                <div className="acoes">
                  <button ref={botaoEsquecer} type="button" className="botao" aria-disabled={indo !== null || undefined} onClick={perguntarEsquecer}>
                    Esquecer esta nuvem
                  </button>
                </div>
                <p className="ajuda">Para ligar este PC a outra nuvem ou conta.</p>
              </div>
            ))}
        </>
      )}

      {estado?.estado === 'conectando' && (
        <div className="ajuste">
          <p className="secao-intro" role="status">
            Aprove no navegador e confira o código <strong className="codigo">{estado.conferencia}</strong>.
          </p>
          <div className="acoes">
            {abrir && (
              <a ref={abrirDeNovo} className="botao" href={abrir} target="_blank" rel="noopener noreferrer">
                Abrir de novo
              </a>
            )}
            <button type="button" className="botao" aria-disabled={indo !== null || undefined} onClick={() => void desconectar('cancelar')}>
              {indo === 'cancelar' ? 'Cancelando…' : 'Cancelar'}
            </button>
          </div>
        </div>
      )}

      {estado?.estado === 'conectado' && (
        <div className="ajuste">
          <p className="secao-intro">
            Conectado como <strong className="email">{estado.email}</strong>.
          </p>
          <p className="ajuda">Na nuvem, este PC aparece como {estado.nome}.</p>
          {historico && <Envio historico={historico} enviandoAgora={indo === 'enviar'} bloqueado={indo !== null} aoEnviar={() => void enviar()} />}
          {/* O freio do aprendido (F8): o aviso que a ADR prometia, junto de "em dia", que
              sem ele pareceria dizer que nada falta subir. */}
          {freio > 0 && (
            <p className="ajuda envio" role="status">
              {freioDoAprendido(freio)}
            </p>
          )}
          <div className="acoes">
            <button type="button" className="botao" aria-disabled={indo !== null || undefined} onClick={() => void desconectar('desconectar')}>
              {indo === 'desconectar' ? 'Desconectando…' : 'Desconectar'}
            </button>
          </div>
          <p className="ajuda">Desconectar tira o acesso deste PC, aqui e na nuvem.</p>
        </div>
      )}

      {erro && (
        <p className="nota" role="alert" key={erro.vez}>
          {erro.texto}
        </p>
      )}
    </section>
  )
}

// haAEnviar: o clique em "Enviar o histórico" soltaria algo, ditados ou
// trocas aprendidas (a D3 da F8: nada aprendido sobe antes do clique, e a
// pergunta aparece também quando só há trocas). Um Agent de antes da F8 não
// manda as trocas: valem 0.
const haAEnviar = (h: ResumoDoHistorico) => h.previa > 0 || (h.previa_trocas ?? 0) > 0

// Envio é o histórico diante da nuvem, com o PC conectado: a pergunta do
// envio inicial (antes do clique), o progresso (durante) e a linha de "em
// dia" (depois).
function Envio({
  historico: h,
  enviandoAgora,
  bloqueado,
  aoEnviar,
}: {
  historico: ResumoDoHistorico
  enviandoAgora: boolean
  bloqueado: boolean
  aoEnviar(): void
}) {
  // A pergunta espera a primeira rodada com esta conexão (o Agent recusa o
  // envio antes dela): o pull já trouxe o que a nuvem tem, e a prévia é o
  // que ela não tem.
  if (!h.aceito && haAEnviar(h) && !h.conferido_em) {
    return (
      <p className="ajuda envio" role="status">
        {h.falhou ? 'A última tentativa não deu certo. O PC tenta de novo sozinho.' : 'Conferindo a nuvem…'}
      </p>
    )
  }
  if (!h.aceito && haAEnviar(h)) {
    // Só com trocas (previa 0) não há histórico a copiar nem lixeira a
    // lembrar, e a frase "Alguns destes ditados…" não teria de que falar.
    const comDitados = h.previa > 0
    return (
      <div className="envio" role="group" aria-labelledby="cfg-envio">
        {h.outra_nuvem && comDitados && <p className="ajuda">Alguns destes ditados podem já estar na nuvem que este PC esqueceu.</p>}
        <p className="ajuda" id="cfg-envio">
          {vaoSubir(h.previa, h.previa_trocas ?? 0)}
          {/* As notas sobem pelo mesmo clique (ADR-022, N7): dito aqui, onde a
              pessoa aceita. */}
          {' As suas notas vão junto.'}
          {comDitados && ' A lixeira não vai. Antes, uma cópia do histórico fica neste PC.'}
        </p>
        <div className="acoes">
          <button type="button" className="botao" aria-disabled={bloqueado || undefined} onClick={aoEnviar}>
            {enviandoAgora ? (comDitados ? 'Copiando o histórico…' : 'Enviando…') : 'Enviar o histórico'}
          </button>
        </div>
      </div>
    )
  }
  if (h.envio) {
    return (
      <p className="ajuda envio" role="status">
        Enviando o histórico: {numero(h.envio.feitos)} de {numero(h.envio.total)}.
      </p>
    )
  }
  const partes: string[] = []
  if (h.pendentes > 0) partes.push(`${numero(h.pendentes)} para enviar.`)
  if (h.falhou) partes.push('A última tentativa não deu certo. O PC tenta de novo sozinho.')
  else if (h.pendentes === 0 && h.conferido_em) partes.push(`Em dia com a nuvem. Conferido às ${formatarHora(h.conferido_em)}.`)
  // O que não subiu fica dito na linha, e não só no aviso passageiro do fim.
  if (h.nao_subiram > 0) partes.push(naoSubiram(h.nao_subiram))
  if (partes.length === 0) return null
  return (
    <p className="ajuda envio" role="status">
      {partes.join(' ')}
    </p>
  )
}
