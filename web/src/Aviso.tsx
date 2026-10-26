import { type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { esperaDoAviso } from './tempoDoAviso'

export interface AvisoDados {
  id: number
  // texto: o texto do aviso. Pode levar elementos (os termos do dicionário
  // vão na IBM Plex Mono); o leitor de tela lê tudo como frase.
  texto: ReactNode
  // desfazer: a pílula oferece "Desfazer", e Ctrl+Z fora de campo de
  // texto faz o mesmo enquanto ela existe (Painel.tsx).
  desfazer?(): void
  // abrir: a pílula oferece "Abrir" (o aviso "Virou nota" leva à nota). Vem antes
  // do Desfazer, sem atalho de teclado, e sumir é com o clique.
  abrir?(): void
  // aprender: a pílula oferece "Aprender" (ADR-017: a sugestão de trocar um
  // termo), e Alt+A fora de campo de texto faz o mesmo enquanto ela
  // existe (Painel.tsx). O clique é o único caminho para o dicionário: o
  // aviso que some sem ele não deixa nada aprendido.
  aprender?(): void
  // efeito: o que a ação faz, dito a quem pergunta (title e descrição do
  // botão). Vai com o aprender: "vale para todo ditado daqui para frente".
  efeito?: string
  // focoNoTexto: se o foco estava na pílula que saiu, esta o recebe NO TEXTO,
  // e não na ação (Desfazer). É o resultado de um clique: quem acabou de clicar
  // em Aprender (com o mouse, ou com Enter ou Espaço no botão) não pode ter,
  // no mesmo lugar, o Desfazer sob um Enter ou um Espaço a mais, que desfaria
  // o que acabou de ser aprendido. O texto guarda o lugar do teclado (Tab leva
  // ao Desfazer) e o leitor de tela lê o resultado, sem oferecer nenhuma ação.
  focoNoTexto?: boolean
}

interface Props {
  aviso: AvisoDados | null
  aoSumir(): void
}

// Aviso é a mesma pílula escura do overlay, embaixo da tela, nos dois
// temas — é por ela que o Aurora Whisper sempre fala com quem usa.
//
// A região viva existe o tempo todo; só o conteúdo muda. Um leitor de
// tela só anuncia o que ENTRA numa região que já estava na página.
export function Aviso({ aviso, aoSumir }: Props) {
  // Aviso novo remonta a pílula (key): se o foco estava na que saiu — no
  // "Desfazer", por exemplo —, ele passa para o botão da nova, em vez de
  // cair no início da página (revisão de 29-set-2026).
  const focoNaQueSaiu = useRef(false)
  const saiu = useCallback((comFoco: boolean) => {
    focoNaQueSaiu.current = comFoco
  }, [])
  const herdarFoco = useCallback(() => {
    const herdou = focoNaQueSaiu.current
    focoNaQueSaiu.current = false
    return herdou
  }, [])
  // A herança vale só para a troca no mesmo commit. Este efeito roda
  // depois do da pílula nova (o pai depois dos filhos): se nenhuma pílula
  // entrou para herdar — o aviso só sumiu —, a marca não pode ficar para
  // um aviso que chegue minutos depois e roube o foco de onde a pessoa
  // estiver.
  useLayoutEffect(() => {
    focoNaQueSaiu.current = false
  }, [aviso])

  return (
    <div className="aviso-regiao" role="status" aria-live="polite">
      {aviso && <Pilula key={aviso.id} aviso={aviso} aoSumir={aoSumir} saiu={saiu} herdarFoco={herdarFoco} />}
    </div>
  )
}

interface PropsPilula {
  aviso: AvisoDados
  aoSumir(): void
  saiu(comFoco: boolean): void
  herdarFoco(): boolean
}

function Pilula({ aviso, aoSumir, saiu, herdarFoco }: PropsPilula) {
  const caixa = useRef<HTMLDivElement>(null)
  const texto = useRef<HTMLSpanElement>(null)
  const botao = useRef<HTMLButtonElement>(null)
  const { focoNoTexto } = aviso
  // Com o ponteiro OU o foco em cima, o aviso espera: ninguém perde o
  // "Desfazer" no meio do gesto. São dois estados, não um: com um só, o
  // ponteiro passando por cima e saindo soltava a pausa com o foco ainda
  // no botão (revisão de 29-set-2026).
  const [sobPonteiro, setSobPonteiro] = useState(false)
  const [comFoco, setComFoco] = useState(false)
  const parado = sobPonteiro || comFoco

  useEffect(() => {
    if (parado) return
    const t = setTimeout(aoSumir, esperaDoAviso(aviso))
    return () => clearTimeout(t)
  }, [parado, aviso, aoSumir])

  useLayoutEffect(() => {
    // O foco herdado vai para a ação, salvo no resultado de um clique
    // (focoNoTexto): ali vai para o texto, e a ação fica a um Tab de distância.
    if (herdarFoco()) (focoNoTexto ? texto.current : botao.current)?.focus()
    const el = caixa.current
    // Roda antes de a pílula sair do DOM: ainda dá para ver se o foco
    // estava nela.
    return () => saiu(Boolean(el?.contains(document.activeElement)))
  }, [herdarFoco, saiu, focoNoTexto])

  return (
    <div
      ref={caixa}
      className="aviso"
      onMouseEnter={() => setSobPonteiro(true)}
      onMouseLeave={() => setSobPonteiro(false)}
      onFocus={() => setComFoco(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setComFoco(false)
      }}
    >
      {/* tabIndex -1: o texto recebe foco por código (focoNoTexto), mas não é
          uma parada do Tab. Sem focoNoTexto não é focável. */}
      <span ref={texto} className="aviso-texto" tabIndex={focoNoTexto ? -1 : undefined}>
        {aviso.texto}
      </span>
      {aviso.aprender && (
        <>
          <kbd className="aviso-atalho">Alt+A</kbd>
          {/* Não chama aoSumir: quem troca a pílula pelo resultado (Aprendido,
              ou o motivo de não ter aprendido) é o Painel, e a nova herda o
              foco. Sumir aqui deixaria o teclado sem lugar entre o clique e
              a resposta. */}
          <button
            ref={botao}
            type="button"
            className="aviso-acao"
            aria-keyshortcuts="Alt+A"
            title={aviso.efeito}
            onClick={() => aviso.aprender?.()}
          >
            Aprender
          </button>
        </>
      )}
      {aviso.abrir && (
        <button
          type="button"
          className="aviso-acao"
          onClick={() => {
            aviso.abrir?.()
            aoSumir()
          }}
        >
          Abrir
        </button>
      )}
      {aviso.desfazer && (
        <>
          <kbd className="aviso-atalho">Ctrl+Z</kbd>
          <button
            ref={botao}
            type="button"
            className="aviso-acao"
            aria-keyshortcuts="Control+Z"
            onClick={() => {
              aviso.desfazer?.()
              aoSumir()
            }}
          >
            Desfazer
          </button>
        </>
      )}
    </div>
  )
}
