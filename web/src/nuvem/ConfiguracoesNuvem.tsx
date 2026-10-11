// As Configurações no app da nuvem (ADR-019, F4; 08-ui-ux-spec.md,
// "Configurações do celular"): a conta e o Sair, os aparelhos (os PCs
// conectados, com o Revogar; F6), o aprendido (as trocas dos PCs, com o
// Desfazer; F8), as sessões abertas (o celular perdido), o que vai e o que
// nunca vai para a nuvem, e o "apagar
// meus dados da nuvem", com a confirmação digitada. Nada do PC: chave de
// API, microfone, atalho, início automático e modo de texto ficam em cada PC
// (decisão 8).

import { type KeyboardEvent, useEffect, useEffectEvent, useRef, useState } from 'react'
import { ErroDaApi, SemSessao, problemaDe } from '../api'
import type { PropsDasConfiguracoes } from '../ambiente'
import { formatarCurta } from '../datas'
import { AprendidoNaNuvem } from './AprendidoNaNuvem'
import { type Aparelho, type ClienteNuvem, type Eu, type Sessao, confirmacaoDeApagar } from './cliente'

interface Props extends PropsDasConfiguracoes {
  cliente: ClienteNuvem
  eu: Eu
  // aoSair: a sessão foi encerrada aqui; o app volta para Entrar.
  aoSair(): void
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function ConfiguracoesNuvem({ cliente, eu, aoSair, visivel, aoPerderLigacao, avisar, recarregarHistorico, comTextoNaoSalvo }: Props) {
  const perderLigacao = useEffectEvent(aoPerderLigacao)
  const titulo = useRef<HTMLHeadingElement>(null)

  // ─── Conta ───
  const [saindo, setSaindo] = useState(false)
  const [erroSair, setErroSair] = useState<string | null>(null)
  // confirmandoSair: Sair com texto não salvo na tela, que o Sair jogaria
  // fora, pergunta antes, ali mesmo, com o foco no lado seguro (Cancelar).
  const [confirmandoSair, setConfirmandoSair] = useState(false)
  const botaoSair = useRef<HTMLButtonElement>(null)
  const botaoCancelarSair = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (confirmandoSair) botaoCancelarSair.current?.focus()
  }, [confirmandoSair])

  function pedirSair() {
    if (saindo || confirmandoSair) return
    if (comTextoNaoSalvo) setConfirmandoSair(true)
    else void sair()
  }

  function cancelarSair() {
    setConfirmandoSair(false)
    botaoSair.current?.focus()
  }

  async function sair() {
    if (saindo) return
    setSaindo(true)
    setErroSair(null)
    try {
      await cliente.sair()
    } catch (e) {
      // Sem sessão é o mesmo que ter saído.
      if (!(e instanceof SemSessao)) {
        setSaindo(false)
        if (problemaDe(e)) aoPerderLigacao(e)
        else setErroSair(`Não deu para sair: ${mensagem(e)}`)
        return
      }
    }
    aoSair()
  }

  // ─── Sessões abertas ───
  const [sessoes, setSessoes] = useState<Sessao[] | null>(null)
  const [erroSessoes, setErroSessoes] = useState<string | null>(null)
  const [encerrando, setEncerrando] = useState<string | null>(null)
  // leitura sobe para ler as listas de novo (depois do "apagar meus dados",
  // que apaga também os aparelhos).
  const [leitura, setLeitura] = useState(0)
  const listaSessoes = useRef<HTMLUListElement>(null)
  const focarSessao = useRef<number | null>(null)

  // ─── Aparelhos (F6) ───
  const [aparelhos, setAparelhos] = useState<Aparelho[] | null>(null)
  const [erroAparelhos, setErroAparelhos] = useState<string | null>(null)
  // confirmandoRevogar: o id do aparelho cuja linha pergunta antes de
  // revogar; revogando: o id do que está indo.
  const [confirmandoRevogar, setConfirmandoRevogar] = useState<string | null>(null)
  const [revogando, setRevogando] = useState<string | null>(null)
  const listaAparelhos = useRef<HTMLUListElement>(null)
  const tituloAparelhos = useRef<HTMLHeadingElement>(null)
  // focarAparelho: para onde o foco vai depois do render — o Cancelar da
  // pergunta que abriu (o lado seguro), o Revogar que pediu (no Cancelar),
  // ou o Revogar que ficou no lugar do que saiu.
  const focarAparelho = useRef<{ tipo: 'pergunta' } | { tipo: 'botao'; id: string } | { tipo: 'lugar'; indice: number } | null>(null)

  // Os aparelhos são lidos quando a tela aparece, como as sessões: um PC
  // pode ter conectado (ou sido revogado em outro aparelho) enquanto ela
  // estava escondida.
  useEffect(() => {
    if (!visivel) return
    let vivo = true
    cliente.aparelhos().then(
      (a) => {
        if (!vivo) return
        setAparelhos(a)
        setErroAparelhos(null)
      },
      (e: unknown) => {
        if (!vivo) return
        if (problemaDe(e)) perderLigacao(e)
        else setErroAparelhos(`Não deu para ver os aparelhos: ${mensagem(e)}`)
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, visivel, leitura])

  useEffect(() => {
    const alvo = focarAparelho.current
    if (alvo === null) return
    focarAparelho.current = null
    const lista = listaAparelhos.current
    if (alvo.tipo === 'pergunta') {
      lista?.querySelector<HTMLElement>('.confirmar .acoes > button:last-child')?.focus()
      return
    }
    if (alvo.tipo === 'botao') {
      lista?.querySelector<HTMLElement>(`[data-aparelho="${alvo.id}"] > button`)?.focus()
      return
    }
    const botoes = lista?.querySelectorAll<HTMLElement>('li > button')
    const b = botoes?.[Math.min(alvo.indice, botoes.length - 1)]
    if (b) b.focus()
    else tituloAparelhos.current?.focus()
  }, [aparelhos, confirmandoRevogar])

  function perguntarRevogar(a: Aparelho) {
    if (revogando !== null) return
    focarAparelho.current = { tipo: 'pergunta' }
    setConfirmandoRevogar(a.id)
    setErroAparelhos(null)
  }

  function cancelarRevogar() {
    // Com o pedido no ar, desistir não o aborta: a pergunta sumiria e o PC
    // sairia da lista mesmo assim. O Cancelar espera o pedido terminar.
    if (revogando !== null) return
    if (confirmandoRevogar !== null) focarAparelho.current = { tipo: 'botao', id: confirmandoRevogar }
    setConfirmandoRevogar(null)
  }

  async function revogar(a: Aparelho) {
    if (revogando !== null || !aparelhos) return
    setRevogando(a.id)
    setErroAparelhos(null)
    try {
      await cliente.revogarAparelho(a.id)
    } catch (e) {
      // Já revogado (em outra aba, ou pelo próprio PC ao desconectar): some
      // da lista do mesmo jeito.
      if (!(e instanceof ErroDaApi && e.status === 404)) {
        setRevogando(null)
        if (problemaDe(e)) aoPerderLigacao(e)
        else setErroAparelhos(`Não deu para revogar: ${mensagem(e)}`)
        return
      }
    }
    setRevogando(null)
    setConfirmandoRevogar(null)
    focarAparelho.current = { tipo: 'lugar', indice: aparelhos.findIndex((x) => x.id === a.id) }
    setAparelhos(aparelhos.filter((x) => x.id !== a.id))
    avisar('PC revogado.')
  }

  // A lista é lida quando a tela aparece: outra aba, ou outro aparelho, pode
  // ter aberto ou encerrado sessões enquanto ela estava escondida.
  useEffect(() => {
    if (!visivel) return
    let vivo = true
    cliente.sessoes().then(
      (s) => {
        if (!vivo) return
        setSessoes(s)
        setErroSessoes(null)
      },
      (e: unknown) => {
        if (!vivo) return
        if (problemaDe(e)) perderLigacao(e)
        else setErroSessoes(`Não deu para ver as sessões: ${mensagem(e)}`)
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, visivel, leitura])

  useEffect(() => {
    const i = focarSessao.current
    if (i === null) return
    focarSessao.current = null
    const botoes = listaSessoes.current?.querySelectorAll<HTMLElement>('button')
    const alvo = botoes?.[Math.min(i, botoes.length - 1)]
    if (alvo) alvo.focus()
    else titulo.current?.focus()
  }, [sessoes])

  async function encerrar(s: Sessao) {
    if (encerrando !== null || !sessoes) return
    setEncerrando(s.id)
    setErroSessoes(null)
    try {
      await cliente.encerrarSessao(s.id)
    } catch (e) {
      setEncerrando(null)
      if (problemaDe(e)) aoPerderLigacao(e)
      else setErroSessoes(`Não deu para encerrar: ${mensagem(e)}`)
      return
    }
    setEncerrando(null)
    // O foco vai para o Encerrar que ocupou o lugar, ou o de cima; sem
    // nenhum, para o título da seção.
    const outras = sessoes.filter((x) => !x.atual)
    focarSessao.current = outras.findIndex((x) => x.id === s.id)
    setSessoes(sessoes.filter((x) => x.id !== s.id))
    avisar('Sessão encerrada.')
  }

  // ─── Apagar meus dados da nuvem ───
  const [apagando, setApagando] = useState<'fechado' | 'aberto' | 'indo'>('fechado')
  const [digitado, setDigitado] = useState('')
  const [erroApagar, setErroApagar] = useState<string | null>(null)
  const botaoApagar = useRef<HTMLButtonElement>(null)
  const campoApagar = useRef<HTMLInputElement>(null)
  const confirmado = digitado.trim().toLowerCase() === confirmacaoDeApagar

  // Abrir leva o foco ao campo; fechar (Cancelar, Esc, ou depois de apagar)
  // o devolve ao botão, que volta a existir só depois do render.
  const focarBotaoApagar = useRef(false)
  useEffect(() => {
    if (apagando === 'aberto') campoApagar.current?.focus()
    if (apagando === 'fechado' && focarBotaoApagar.current) {
      focarBotaoApagar.current = false
      botaoApagar.current?.focus()
    }
  }, [apagando])

  function fecharApagar() {
    focarBotaoApagar.current = true
    setApagando('fechado')
    setDigitado('')
    setErroApagar(null)
  }

  async function apagar() {
    if (!confirmado || apagando === 'indo') return
    setApagando('indo')
    setErroApagar(null)
    try {
      await cliente.apagarMeusDados()
    } catch (e) {
      setApagando('aberto')
      if (problemaDe(e)) aoPerderLigacao(e)
      else setErroApagar(`Não deu para apagar: ${mensagem(e)}`)
      return
    }
    fecharApagar()
    avisar('Seus dados foram apagados da nuvem.')
    // O histórico ficou vazio, e as outras sessões foram encerradas.
    recarregarHistorico()
    setLeitura((n) => n + 1)
  }

  function teclaNoApagar(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      fecharApagar()
    } else if (e.key === 'Enter') {
      e.preventDefault()
      void apagar()
    }
  }

  const outras = sessoes?.filter((s) => !s.atual) ?? []

  return (
    <div className="config" hidden={!visivel}>
      <section className="secao" aria-labelledby="cfg-conta">
        <h2 id="cfg-conta" className="secao-titulo">
          Conta
        </h2>
        <div className="ajuste conta">
          <p className="secao-intro">
            Entrou como <strong className="email">{eu.email}</strong>.
          </p>
          <button ref={botaoSair} type="button" className="botao" aria-disabled={saindo || undefined} onClick={pedirSair}>
            {saindo && !confirmandoSair ? 'Saindo…' : 'Sair'}
          </button>
        </div>
        {confirmandoSair && (
          <div className="ajuste">
            <div
              className="confirmar"
              role="group"
              aria-labelledby="cfg-sair-titulo"
              onKeyDown={(e) => {
                if (e.key === 'Escape') {
                  e.preventDefault()
                  cancelarSair()
                }
              }}
            >
              <p className="ajuste-nome" id="cfg-sair-titulo">
                Sair com texto não salvo
              </p>
              <p className="ajuda">Há texto que você escreveu e não salvou. Se você sair, ele se perde.</p>
              <div className="acoes">
                <button type="button" className="botao risco" aria-disabled={saindo || undefined} onClick={() => void sair()}>
                  {saindo ? 'Saindo…' : 'Sair e descartar o texto'}
                </button>
                <button ref={botaoCancelarSair} type="button" className="botao" onClick={cancelarSair}>
                  Cancelar
                </button>
              </div>
            </div>
          </div>
        )}
        {erroSair && (
          <p className="nota" role="alert">
            {erroSair}
          </p>
        )}
      </section>

      <section className="secao" aria-labelledby="cfg-aparelhos">
        <h2 id="cfg-aparelhos" className="secao-titulo" ref={tituloAparelhos} tabIndex={-1}>
          Aparelhos
        </h2>
        {aparelhos === null && !erroAparelhos && <p className="ajuda">Carregando…</p>}
        {aparelhos && aparelhos.length > 0 && (
          <ul className="sessoes aparelhos" ref={listaAparelhos}>
            {aparelhos.map((a) => {
              const uso = formatarCurta(a.ultimo_uso_em)
              const plataforma = a.plataforma === 'windows' ? 'Windows' : a.plataforma
              return (
                <li key={a.id} data-aparelho={a.id}>
                  <span className="sessao">
                    <span className="sessao-nome">{a.nome}</span>
                    <span className="sessao-uso">
                      <span className="so-leitor">, </span>
                      {plataforma}
                      {a.versao && ` · versão ${a.versao}`} · último uso <time dateTime={a.ultimo_uso_em}>{uso}</time>
                    </span>
                  </span>
                  {confirmandoRevogar === a.id ? (
                    <div
                      className="confirmar"
                      role="group"
                      aria-labelledby={`cfg-revogar-${a.id}`}
                      onKeyDown={(e) => {
                        if (e.key === 'Escape') {
                          e.preventDefault()
                          cancelarRevogar()
                        }
                      }}
                    >
                      <p className="ajuda" id={`cfg-revogar-${a.id}`}>
                        Revogar {a.nome}? O PC para de sincronizar e continua funcionando nele.
                      </p>
                      <div className="acoes">
                        <button type="button" className="botao risco" aria-disabled={revogando !== null || undefined} onClick={() => void revogar(a)}>
                          {revogando === a.id ? 'Revogando…' : 'Revogar'}
                        </button>
                        <button type="button" className="botao" aria-disabled={revogando !== null || undefined} onClick={cancelarRevogar}>
                          Cancelar
                        </button>
                      </div>
                    </div>
                  ) : (
                    <button
                      type="button"
                      className="botao"
                      aria-disabled={revogando !== null || undefined}
                      aria-label={`Revogar ${a.nome}, último uso ${uso}`}
                      onClick={() => perguntarRevogar(a)}
                    >
                      Revogar
                    </button>
                  )}
                </li>
              )
            })}
          </ul>
        )}
        {aparelhos && aparelhos.length === 0 && <p className="ajuda">Nenhum PC conectado.</p>}
        {erroAparelhos && (
          <p className="nota" role="alert">
            {erroAparelhos}
          </p>
        )}
      </section>

      {/* O que os PCs aprenderam, com Desfazer (F8; 08-ui-ux-spec.md,
          "Configurações do celular"). */}
      <AprendidoNaNuvem cliente={cliente} visivel={visivel} leitura={leitura} aoPerderLigacao={aoPerderLigacao} avisar={avisar} />

      <section className="secao" aria-labelledby="cfg-sessoes">
        <h2 id="cfg-sessoes" className="secao-titulo" ref={titulo} tabIndex={-1}>
          Sessões abertas
        </h2>
        <p className="secao-intro">Perdeu o celular? Encerre a sessão dele aqui.</p>
        {sessoes === null && !erroSessoes && <p className="ajuda">Carregando…</p>}
        {sessoes && (
          <ul className="sessoes" ref={listaSessoes}>
            {sessoes.map((s) => (
              <li key={s.id}>
                <span className="sessao">
                  <span className="sessao-nome">{s.rotulo || 'Navegador'}</span>
                  <span className="sessao-uso">
                    <span className="so-leitor">, </span>último uso <time dateTime={s.ultimo_uso_em}>{formatarCurta(s.ultimo_uso_em)}</time>
                  </span>
                </span>
                {s.atual ? (
                  <span className="etiqueta">este aparelho</span>
                ) : (
                  <button
                    type="button"
                    className="botao"
                    aria-disabled={encerrando !== null || undefined}
                    aria-label={`Encerrar a sessão ${s.rotulo || 'Navegador'}, último uso ${formatarCurta(s.ultimo_uso_em)}`}
                    onClick={() => void encerrar(s)}
                  >
                    {encerrando === s.id ? 'Encerrando…' : 'Encerrar'}
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        {sessoes && outras.length === 0 && <p className="ajuda">Nenhuma outra sessão aberta.</p>}
        {erroSessoes && (
          <p className="nota" role="alert">
            {erroSessoes}
          </p>
        )}
      </section>

      <section className="secao" aria-labelledby="cfg-privacidade">
        <h2 id="cfg-privacidade" className="secao-titulo">
          Privacidade
        </h2>
        <ul className="fatos">
          <li>Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os detalhes da transcrição, e as trocas aprendidas.</li>
          <li>Nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos PCs.</li>
        </ul>

        <div className="ajuste apagar">
          {apagando === 'fechado' ? (
            <button ref={botaoApagar} type="button" className="botao risco" onClick={() => setApagando('aberto')}>
              Apagar meus dados da nuvem
            </button>
          ) : (
            <div className="confirmar" role="group" aria-labelledby="cfg-apagar-titulo">
              <p className="ajuste-nome" id="cfg-apagar-titulo">
                Apagar meus dados da nuvem
              </p>
              <p className="ajuda" id="cfg-apagar-efeito">
                Apaga tudo desta conta na nuvem e desconecta os PCs. Os PCs ficam com a cópia deles.
              </p>
              <label className="ajuste-nome rotulo-campo" htmlFor="cfg-apagar">
                Para confirmar, digite <span className="termo">apagar</span>
              </label>
              <input
                ref={campoApagar}
                id="cfg-apagar"
                className="entrada"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                value={digitado}
                aria-describedby="cfg-apagar-efeito"
                onChange={(e) => setDigitado(e.target.value)}
                onKeyDown={teclaNoApagar}
              />
              <div className="acoes">
                <button type="button" className="botao risco" aria-disabled={!confirmado || apagando === 'indo' || undefined} onClick={() => void apagar()}>
                  {apagando === 'indo' ? 'Apagando…' : 'Apagar tudo'}
                </button>
                <button type="button" className="botao" onClick={fecharApagar}>
                  Cancelar
                </button>
              </div>
              {erroApagar && (
                <p className="nota" role="alert">
                  {erroApagar}
                </p>
              )}
            </div>
          )}
        </div>
      </section>
    </div>
  )
}
