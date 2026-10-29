import { type ReactNode, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Icone, type NomeDoIcone } from './Icone'

interface Props {
  icone: NomeDoIcone
  // rotulo: o nome do botão e do menu, para o leitor de tela.
  rotulo: string
  desabilitado?: boolean
  // title: a dica do botão; sem ela, é o rótulo.
  titulo?: string
  // classe: a mais do menu (Marcadores: "escolher", o menu com campo de busca).
  classe?: string
  // children recebe o fechar: o item que age fecha o menu.
  children(fechar: () => void): ReactNode
}

// BotaoComMenu é um botão de ícone que abre um menu embaixo dele (Formatar,
// Cor e Mais, na nota aberta e no cartão). É um botão de abrir e fechar com
// o menu logo depois dele no DOM, e não o menu de verdade do ARIA: os itens
// são botões comuns, e o Tab passa por eles. Esc fecha e devolve o foco ao botão
// (sem fechar a janela da nota, que também ouve o Esc); clicar fora ou sair
// dele com o Tab também fecha.
export function BotaoComMenu({ icone, rotulo, desabilitado, titulo, classe, children }: Props) {
  const [aberto, setAberto] = useState(false)
  const raiz = useRef<HTMLSpanElement>(null)
  const botao = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)

  // O menu tem de caber na tela, e isso só se sabe medindo (antes de pintar, sem
  // pulo). Na barra da nota aberta ele abre para cima: numa nota curta no alto da
  // tela (o celular), a parte de cima ficava fora dela, e o que passa da borda de
  // cima de uma área de rolagem não volta com a rolagem. Aí ele abre para baixo,
  // onde a janela da nota rola até ele. E o menu que passa da borda direita (o
  // Mais e a Cor da coluna da direita, no celular) vai para a esquerda o que
  // passou, sem sair pela borda esquerda.
  useLayoutEffect(() => {
    const m = menu.current
    if (!aberto || !m) return
    const r = m.getBoundingClientRect()
    if (r.height === 0) return
    if (r.top < 8) m.dataset.lado = 'baixo'
    const passou = r.right - ((document.documentElement.clientWidth || window.innerWidth) - 8)
    if (passou > 0) m.style.left = `${-Math.min(passou, Math.max(0, r.left - 8))}px`
  }, [aberto])

  useEffect(() => {
    if (!aberto) return
    function fora(e: MouseEvent) {
      if (!raiz.current?.contains(e.target as Node)) setAberto(false)
    }
    document.addEventListener('mousedown', fora)
    return () => document.removeEventListener('mousedown', fora)
  }, [aberto])

  return (
    <span
      className="menu-caixa"
      ref={raiz}
      onKeyDown={(e) => {
        if (e.key !== 'Escape' || !aberto) return
        e.preventDefault()
        e.stopPropagation()
        setAberto(false)
        botao.current?.focus()
      }}
      onBlur={(e) => {
        if (aberto && !e.currentTarget.contains(e.relatedTarget as Node | null)) setAberto(false)
      }}
    >
      <button
        ref={botao}
        type="button"
        className="icone-botao"
        aria-label={rotulo}
        title={titulo ?? rotulo}
        aria-expanded={aberto}
        disabled={desabilitado}
        onClick={() => setAberto((a) => !a)}
      >
        <Icone nome={icone} />
      </button>
      {aberto && (
        <div ref={menu} className={classe ? `menu ${classe}` : 'menu'} role="group" aria-label={rotulo}>
          {children(() => setAberto(false))}
        </div>
      )}
    </span>
  )
}
