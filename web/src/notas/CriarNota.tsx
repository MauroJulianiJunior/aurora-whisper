import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { problemaDe } from '../api'
import { CamposDaNota } from './CamposDaNota'
import { Icone } from './Icone'
import type { ClienteDeNotas, Nota } from './tipos'
import { useEditorDeLinhas } from './useEditorDeLinhas'

interface Props {
  cliente: ClienteDeNotas
  // aoCriar: a nota foi gravada (fechar com algo escrito).
  aoCriar(nota: Nota): void
  aoPerderLigacao(e: unknown): void
}

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

// CriarNota é o "Criar uma nota…" do alto da tela (ADR-022): uma barra
// recolhida que abre num título e no corpo por linhas. Fechar com algo
// escrito grava a nota; fechar vazio não grava nada.
export function CriarNota({ cliente, aoCriar, aoPerderLigacao }: Props) {
  // aberto: o corpo com que a caixa abriu ('' é a nota em branco; '- [ ] ' é a
  // "Nova lista de marcar"). A caixa nasce de novo a cada abertura.
  const [aberto, setAberto] = useState<string | null>(null)
  const barra = useRef<HTMLButtonElement>(null)
  const devolverFoco = useRef(false)

  // Fechou: o foco volta à barra (a caixa que tinha o foco saiu da tela).
  useEffect(() => {
    if (aberto !== null || !devolverFoco.current) return
    devolverFoco.current = false
    barra.current?.focus()
  })

  if (aberto !== null) {
    return (
      <CaixaAberta
        corpoInicial={aberto}
        cliente={cliente}
        aoPerderLigacao={aoPerderLigacao}
        aoFechar={(nota) => {
          devolverFoco.current = true
          setAberto(null)
          if (nota) aoCriar(nota)
        }}
      />
    )
  }

  return (
    <div className="criar">
      <button ref={barra} type="button" className="criar-abrir" onClick={() => setAberto('')}>
        Criar uma nota…
      </button>
      <button type="button" className="icone-botao" aria-label="Nova lista de marcar" title="Nova lista de marcar" onClick={() => setAberto('- [ ] ')}>
        <Icone nome="marcar" />
      </button>
      <button type="button" className="icone-botao" aria-label="Nova nota com imagem" title="Imagens chegam depois" disabled>
        <Icone nome="imagem" />
      </button>
    </div>
  )
}

function CaixaAberta({
  corpoInicial,
  cliente,
  aoFechar,
  aoPerderLigacao,
}: {
  corpoInicial: string
  cliente: ClienteDeNotas
  aoFechar(nota: Nota | null): void
  aoPerderLigacao(e: unknown): void
}) {
  const [titulo, setTitulo] = useState('')
  const [erro, setErro] = useState<string | null>(null)
  const editor = useEditorDeLinhas(corpoInicial)
  const { corpo } = editor
  const raiz = useRef<HTMLDivElement>(null)
  const agora = useRef({ titulo, corpo })
  const gravando = useRef(false)
  useLayoutEffect(() => {
    agora.current = { titulo, corpo }
  })

  async function fechar() {
    if (gravando.current) return
    const { titulo: t, corpo: c } = agora.current
    if (t.trim() === '' && c.trim() === '') return aoFechar(null)
    gravando.current = true
    try {
      const nota = await cliente.criarNota({ titulo: t.trim(), corpo: c })
      aoFechar(nota)
    } catch (e) {
      gravando.current = false
      if (problemaDe(e)) aoPerderLigacao(e)
      setErro(mensagemDe(e))
    }
  }

  // Clicar fora fecha, como no Keep: a nota escrita não se perde.
  const fecharRef = useRef(fechar)
  useLayoutEffect(() => {
    fecharRef.current = fechar
  })
  useEffect(() => {
    function fora(e: MouseEvent) {
      if (raiz.current && !raiz.current.contains(e.target as Node)) void fecharRef.current()
    }
    document.addEventListener('mousedown', fora)
    return () => document.removeEventListener('mousedown', fora)
  }, [])

  return (
    <div
      ref={raiz}
      className="criar aberto"
      onKeyDown={(e) => {
        if (e.defaultPrevented) return
        if (e.key === 'Escape' || (e.key === 'Enter' && (e.ctrlKey || e.metaKey))) {
          e.preventDefault()
          void fechar()
        }
      }}
    >
      <CamposDaNota
        titulo={titulo}
        aoMudarTitulo={setTitulo}
        editor={editor}
        focoInicial={corpoInicial === '' ? 'titulo' : 'corpo'}
        rotulo="Texto da nova nota"
      />
      {erro && (
        <p className="nota" role="alert">
          Não consegui salvar a nota: {erro}. O texto continua aqui; feche de novo para tentar.
        </p>
      )}
      <div className="barra-nota">
        <span className="empurra" />
        <button type="button" className="botao leve" onClick={() => void fechar()}>
          Fechar
        </button>
      </div>
    </div>
  )
}
