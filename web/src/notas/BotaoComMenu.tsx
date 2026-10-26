import { type ReactNode, useEffect, useRef, useState } from 'react'
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
        <div className={classe ? `menu ${classe}` : 'menu'} role="group" aria-label={rotulo}>
          {children(() => setAberto(false))}
        </div>
      )}
    </span>
  )
}
