import { type KeyboardEvent, type ReactNode, useEffect, useEffectEvent, useRef, useState } from 'react'
import { ErroDaApi, problemaDe } from './api'
import type { Cliente, EstadoRetencao } from './clienteLocal'
import {
  agentSemSeisMeses,
  apagaram,
  apagaramParte,
  comDuasVersoes,
  contagemMudou,
  copiaDoEnvioFica,
  copiasApagadas,
  deOutroDono,
  desligarAjuda,
  desligouAviso,
  haCopias,
  ligadaTexto,
  ligouSemTerminar,
  naLixeira,
  perguntaDosSeisMeses,
  regraPausada,
  restoDeApagar,
  restoDeApagarCopias,
  tituloDeApagar,
  tituloDeApagarCopias,
} from './textosDoHistorico'

// O bloco "Histórico" das Configurações do PC (ADR-019, "Decisões da F10";
// 08-ui-ux-spec.md, "Histórico (os 6 meses no PC)"). Fica entre a Nuvem e a
// Privacidade, age na hora (fora do Salvar configurações) e SÓ APARECE quando há
// o que decidir: ditados de mais de 6 meses, a regra ligada, ou cópias do banco
// ao lado dele. Nada fora das Configurações: sem faixa, sem janela, sem aviso na
// bandeja.
//
// A regra apaga ditado de vez, então nada acontece sem o clique:
//   - a pergunta (D1): "N ditados têm mais de 6 meses…" e o botão; o clique abre
//     a confirmação na própria linha (o padrão do "Esquecer esta nuvem", sem
//     janela), com o foco no Cancelar; o Esc cancela;
//   - o Agent recontou antes de apagar: se o número não é o que a pessoa viu, ele
//     recusa com o número novo, a frase troca, e a pessoa confirma de novo;
//   - ligada (D2), o que completa 6 meses sai sozinho em até 1 hora; Desligar
//     volta ao de antes, sem pergunta (não apaga nada);
//   - pausada pelo freio (o Agent ficou mais de 7 dias parado, ou o relógio do PC
//     pulou): a pergunta volta, com uma linha que diz isso;
//   - as cópias do banco (D4): quantas há e "Apagar as cópias", com a pergunta na
//     linha. O Agent nunca as apaga sozinho.
//
// Sem laço: o painel pergunta ao Agent quando a tela aparece e quando a aba volta
// a ficar à vista (leitura local, sem rede).

interface Props {
  cliente: Cliente
  visivel: boolean
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
  // aoMudarEstado: o estado novo, a cada leitura (a Privacidade diz que o que
  // completa 6 meses é apagado deste computador quando a regra está ligada).
  aoMudarEstado?(e: EstadoRetencao): void
  // recarregarHistorico: ditados saíram do PC; a lista do Histórico lê de novo.
  recarregarHistorico(): void
  // aoEsvaziar: o bloco sumiu depois de uma ação (não há mais o que decidir); o
  // foco, que estava num botão dele, vai para outro lugar.
  aoEsvaziar?(): void
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

// haOQueDecidir: o bloco aparece. Com nada a apagar, regra desligada e sem
// cópias, não há o que perguntar.
const haOQueDecidir = (e: EstadoRetencao) => e.previa > 0 || e.ligada || e.copias > 0

// apagaveis: as cópias que o botão apaga agora (a do envio do histórico à nuvem
// que não terminou fica). Um Agent que não as conta: todas.
const apagaveis = (e: EstadoRetencao) => e.copias_apagaveis ?? e.copias

type Acao = 'ligar' | 'desligar' | 'copias'
type Foco = 'apagar' | 'cancelar' | 'desligar' | 'copias' | 'cancelar-copias' | 'principal'

export function HistoricoNoPC({ cliente, visivel, aoPerderLigacao, avisar, aoMudarEstado, recarregarHistorico, aoEsvaziar }: Props) {
  const [estado, setEstado] = useState<EstadoRetencao | null>(null)
  // indisponivel: o Agent não tem os 6 meses (de antes da F10), ou não está
  // pronto para eles.
  const [indisponivel, setIndisponivel] = useState<string | null>(null)
  const [indo, setIndo] = useState<Acao | null>(null)
  const [confirmando, setConfirmando] = useState<'ligar' | 'copias' | null>(null)
  const [erro, setErro] = useState<{ texto: string; vez: number } | null>(null)
  // geracao: a resposta de uma leitura que ficou para trás (a da volta à aba que
  // chegou depois do OK) não pode desfazer o que a ação acabou de mostrar.
  const geracao = useRef(0)
  const botaoApagar = useRef<HTMLButtonElement>(null)
  const cancelar = useRef<HTMLButtonElement>(null)
  const botaoDesligar = useRef<HTMLButtonElement>(null)
  const botaoCopias = useRef<HTMLButtonElement>(null)
  const cancelarCopias = useRef<HTMLButtonElement>(null)
  // focar: para onde o foco vai depois que o botão clicado sumir com o estado
  // (o render seguinte).
  const focar = useRef<Foco | null>(null)

  function aplicar(e: EstadoRetencao) {
    setEstado(e)
    setIndisponivel(null)
    aoMudarEstado?.(e)
  }

  function falhou(e: unknown) {
    if (problemaDe(e)) aoPerderLigacao(e)
    else setErro((a) => ({ texto: mensagem(e), vez: (a?.vez ?? 0) + 1 }))
  }

  // ler: ao aparecer e ao voltar à aba. Uma resposta que chega depois de outra
  // mais nova (de uma leitura ou de uma ação) é descartada.
  function ler() {
    const minha = ++geracao.current
    cliente.retencao().then(
      (e) => {
        if (minha === geracao.current) aplicar(e)
      },
      (e: unknown) => {
        if (minha !== geracao.current) return
        if (problemaDe(e)) aoPerderLigacao(e)
        // O painel novo com um Agent de antes da F10 (recompilado com o Agent
        // aberto): a rota não existe, e o 404 dele não tem frase.
        else if (e instanceof ErroDaApi && e.status === 404) setIndisponivel(agentSemSeisMeses)
        else setIndisponivel(mensagem(e))
      },
    )
  }
  // lerNoEfeito: o que o efeito chama (a leitura de ler, que um efeito pode chamar
  // sem entrar nas dependências).
  const lerNoEfeito = useEffectEvent(ler)

  useEffect(() => {
    if (!visivel) return
    lerNoEfeito()
    const aoVoltar = () => {
      if (document.visibilityState === 'visible') lerNoEfeito()
    }
    document.addEventListener('visibilitychange', aoVoltar)
    return () => document.removeEventListener('visibilitychange', aoVoltar)
  }, [visivel])

  const mostrar = indisponivel !== null || (estado !== null && haOQueDecidir(estado))

  // semAlvo: o botão que devia receber o foco não existe mais (o bloco ficou sem
  // botão: só a cópia do envio que não terminou, por exemplo); o foco vai para
  // fora, como quando o bloco some.
  const semAlvo = useEffectEvent(() => aoEsvaziar?.())

  useEffect(() => {
    const alvo = focar.current
    if (alvo === null) return
    focar.current = null
    const el =
      alvo === 'principal'
        ? (botaoApagar.current ?? botaoDesligar.current ?? botaoCopias.current)
        : alvo === 'apagar'
          ? botaoApagar.current
          : alvo === 'cancelar'
            ? cancelar.current
            : alvo === 'desligar'
              ? botaoDesligar.current
              : alvo === 'copias'
                ? botaoCopias.current
                : cancelarCopias.current
    if (el) el.focus()
    else semAlvo()
  }, [estado, confirmando])

  // focoDepois leva o foco para alvo no render seguinte; se o bloco some com o
  // estado novo (não há mais o que decidir), o foco, que estava num botão dele,
  // vai para fora (o pai decide).
  function focoDepois(novo: EstadoRetencao, alvo: Foco) {
    if (haOQueDecidir(novo)) focar.current = alvo
    else aoEsvaziar?.()
  }

  // depoisDeFalhar relê o estado (o Agent pode ter mudado: o OK gravado e o
  // resto sem terminar, ou o último ditado que saiu) e mostra a frase onde a
  // pessoa a vê: na nota do bloco, ou, se o bloco sumiu com a releitura ("Não
  // há nada a apagar agora."), no aviso. Depois do OK (acao 'ligar'), a
  // releitura com a regra ligada quer dizer que o OK foi gravado antes da falha
  // (e parte pode ter saído): não é falha, e "tente de novo" não tem botão.
  async function depoisDeFalhar(texto: string, alvo: Foco, acao: Acao) {
    const nota = () => setErro((a) => ({ texto, vez: (a?.vez ?? 0) + 1 }))
    try {
      const minha = ++geracao.current
      const novo = await cliente.retencao()
      if (minha === geracao.current) aplicar(novo)
      if (acao === 'ligar' && novo.ligada) {
        focoDepois(novo, 'desligar')
        avisar(ligouSemTerminar)
        recarregarHistorico()
        return
      }
      focoDepois(novo, alvo)
      if (haOQueDecidir(novo)) nota()
      else avisar(texto)
    } catch (e) {
      falhou(e)
      nota()
    }
  }

  function perguntar(qual: 'ligar' | 'copias') {
    if (indo) return
    setErro(null)
    focar.current = qual === 'ligar' ? 'cancelar' : 'cancelar-copias'
    setConfirmando(qual)
  }

  function naoApagar() {
    // Com o OK em andamento não há mais o que cancelar.
    if (indo) return
    focar.current = confirmando === 'copias' ? 'copias' : 'apagar'
    setConfirmando(null)
  }

  async function ligar() {
    if (indo || !estado) return
    setIndo('ligar')
    setErro(null)
    try {
      // A contagem que a pessoa viu: o Agent recontou, e só apaga se for esta.
      const r = await cliente.ligarRetencao(estado.previa)
      geracao.current++
      focoDepois(r, 'desligar')
      setConfirmando(null)
      aplicar(r)
      if (r.incompleto) {
        // Ligou, mas um lote seguinte caiu: o que falta (a prévia de agora) sai
        // sozinho na próxima rodada. Não é falha: nada a tentar de novo.
        avisar(apagaramParte(r.apagados, r.previa))
        recarregarHistorico()
      } else if (r.apagados > 0) {
        avisar(apagaram(r.apagados))
        recarregarHistorico()
      }
    } catch (e) {
      setConfirmando(null)
      if (problemaDe(e)) aoPerderLigacao(e)
      else if (e instanceof ErroDaApi && e.status === 409 && e.previa !== undefined) {
        // A contagem mudou: a frase troca para o número novo, nada foi apagado, e
        // a pessoa confirma de novo.
        const nova = e.previa
        geracao.current++
        focar.current = 'apagar'
        setEstado((a) => (a ? { ...a, previa: nova } : a))
        avisar(contagemMudou)
        ler()
      } else await depoisDeFalhar(mensagem(e), 'apagar', 'ligar')
    } finally {
      setIndo(null)
    }
  }

  async function desligar() {
    if (indo) return
    setIndo('desligar')
    setErro(null)
    try {
      const e = await cliente.desligarRetencao()
      geracao.current++
      focoDepois(e, 'principal')
      aplicar(e)
      avisar(desligouAviso)
    } catch (e) {
      falhou(e)
    } finally {
      setIndo(null)
    }
  }

  async function apagarCopias() {
    if (indo) return
    setIndo('copias')
    setErro(null)
    try {
      const r = await cliente.apagarCopias()
      geracao.current++
      focoDepois(r, 'principal')
      setConfirmando(null)
      aplicar(r)
      avisar(copiasApagadas(r.apagadas, r.ficaram))
    } catch (e) {
      setConfirmando(null)
      if (problemaDe(e)) aoPerderLigacao(e)
      else await depoisDeFalhar(mensagem(e), 'copias', 'copias')
    } finally {
      setIndo(null)
    }
  }

  function escCancela(e: KeyboardEvent<HTMLElement>) {
    if (e.key !== 'Escape') return
    e.preventDefault()
    naoApagar()
  }

  if (!mostrar) return null
  const e = estado
  const ocupado = indo !== null || undefined

  return (
    <section className="secao" aria-labelledby="cfg-historico">
      <h2 id="cfg-historico" className="secao-titulo">
        Histórico
      </h2>
      {indisponivel && (
        <p className="nota" role="alert">
          {indisponivel}
        </p>
      )}

      {/* A pausa vem antes de tudo e vale sempre que houver: sem nada a apagar, o
          bloco aparece só pelas cópias, e sem esta linha nada diria que a regra
          desligou. */}
      {e?.pausada && !e.ligada && <p className="secao-intro">{regraPausada(e.previa)}</p>}

      {e && !e.ligada && e.previa > 0 && (
        <div className="ajuste">
          <p className="secao-intro" id="cfg-hist-pergunta">
            {perguntaDosSeisMeses(e.previa, e.copia_dos_6_meses === true)}
          </p>
          {e.na_lixeira > 0 && <p className="ajuda">{naLixeira(e.na_lixeira, e.previa)}</p>}
          {e.com_selo > 0 && <p className="ajuda">{comDuasVersoes(e.com_selo, e.previa)}</p>}
          {e.de_outro_dono > 0 && <p className="ajuda">{deOutroDono(e.de_outro_dono)}</p>}
          {confirmando === 'ligar' ? (
            <div className="confirmar" role="group" aria-labelledby="cfg-hist-confirma" onKeyDown={escCancela}>
              <p className="ajuda" id="cfg-hist-confirma">
                <strong>{tituloDeApagar(e.previa)}</strong> {restoDeApagar}
              </p>
              <div className="acoes">
                <button type="button" className="botao risco" aria-disabled={ocupado} onClick={() => void ligar()}>
                  {indo === 'ligar' ? (e.copia_dos_6_meses ? 'Apagando…' : 'Copiando e apagando…') : 'Apagar'}
                </button>
                <button ref={cancelar} type="button" className="botao" aria-disabled={indo === 'ligar' || undefined} onClick={naoApagar}>
                  Cancelar
                </button>
              </div>
            </div>
          ) : (
            <div className="acoes">
              <button ref={botaoApagar} type="button" className="botao" aria-disabled={ocupado} onClick={() => perguntar('ligar')}>
                Apagar os mais antigos
              </button>
            </div>
          )}
        </div>
      )}

      {e?.ligada && (
        <div className="ajuste">
          <p className="secao-intro">{ligadaTexto}</p>
          <div className="acoes">
            <button ref={botaoDesligar} type="button" className="botao" aria-disabled={ocupado} onClick={() => void desligar()}>
              {indo === 'desligar' ? 'Desligando…' : 'Desligar'}
            </button>
          </div>
          <p className="ajuda">{desligarAjuda}</p>
        </div>
      )}

      {e && e.copias > 0 && (
        <div className="ajuste">
          <p className="ajuda">{haCopias(e.copias)}</p>
          {apagaveis(e) < e.copias && <p className="ajuda">{copiaDoEnvioFica(e.copias, e.copias - apagaveis(e))}</p>}
          {apagaveis(e) === 0 ? null : confirmando === 'copias' ? (
            <div className="confirmar" role="group" aria-labelledby="cfg-hist-copias-confirma" onKeyDown={escCancela}>
              <p className="ajuda" id="cfg-hist-copias-confirma">
                <strong>{tituloDeApagarCopias(apagaveis(e))}</strong> {restoDeApagarCopias(apagaveis(e))}
              </p>
              <div className="acoes">
                <button type="button" className="botao risco" aria-disabled={ocupado} onClick={() => void apagarCopias()}>
                  {indo === 'copias' ? 'Apagando…' : 'Apagar'}
                </button>
                <button ref={cancelarCopias} type="button" className="botao" aria-disabled={indo === 'copias' || undefined} onClick={naoApagar}>
                  Cancelar
                </button>
              </div>
            </div>
          ) : (
            <div className="acoes">
              <button ref={botaoCopias} type="button" className="botao" aria-disabled={ocupado} onClick={() => perguntar('copias')}>
                Apagar as cópias
              </button>
            </div>
          )}
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
