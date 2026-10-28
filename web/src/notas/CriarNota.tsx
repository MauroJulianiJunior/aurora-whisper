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
  // desligado: o celular sem rede (N8, E4); celular: o app do celular.
  desligado?: boolean
  celular?: boolean
  // aoEscolherImagens: o ícone de imagem (N10), a nota nova com a imagem. Só
  // quem tem as imagens (o PC) o passa; sem ele, o ícone não aparece.
  aoEscolherImagens?(arquivos: File[]): void
}

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

// novoToken identifica uma abertura da caixa: um UUID (só letras, dígitos e
// hífen, o que o Agent aceita). O painel não tem Math.random determinístico a
// preservar: o token só precisa ser único entre as aberturas.
function novoToken(): string {
  const c = globalThis.crypto
  if (typeof c?.randomUUID === 'function') return c.randomUUID()
  return `${Date.now().toString(36)}-${Math.floor(Math.random() * 2 ** 32).toString(36)}`
}

// CriarNota é o "Criar uma nota…" do alto da tela (ADR-022): uma barra
// recolhida que abre num título e no corpo por linhas. Fechar com algo
// escrito grava a nota; fechar vazio não grava nada.
export function CriarNota({ cliente, aoCriar, aoPerderLigacao, desligado = false, aoEscolherImagens }: Props) {
  // aberto: o corpo com que a caixa abriu ('' é a nota em branco; '- [ ] ' é a
  // "Nova lista de marcar"). A caixa nasce de novo a cada abertura.
  const [aberto, setAberto] = useState<string | null>(null)
  const barra = useRef<HTMLButtonElement>(null)
  const escolher = useRef<HTMLInputElement>(null)
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

  // Sem rede no celular (N8, E4), criar nota espera a rede: a barra fica
  // apagada. No celular não há o botão da imagem (a prévia da N8; a imagem no
  // celular é a segunda metade da N10).
  return (
    <div className={desligado ? 'criar desligado' : 'criar'}>
      <button ref={barra} type="button" className="criar-abrir" disabled={desligado} onClick={() => setAberto('')}>
        Criar uma nota…
      </button>
      <button
        type="button"
        className="icone-botao"
        aria-label="Nova lista de marcar"
        title="Nova lista de marcar"
        disabled={desligado}
        onClick={() => setAberto('- [ ] ')}
      >
        <Icone nome="marcar" />
      </button>
      {aoEscolherImagens && (
        <>
          <button
            type="button"
            className="icone-botao"
            aria-label="Nova nota com imagem"
            title="Nova nota com imagem"
            disabled={desligado}
            onClick={() => escolher.current?.click()}
          >
            <Icone nome="imagem" />
          </button>
          {/* A escolha de arquivos do Windows, aberta pelo clique de quem usa. */}
          <input
            ref={escolher}
            type="file"
            accept="image/*"
            multiple
            hidden
            tabIndex={-1}
            aria-label="Escolher a imagem da nota nova"
            onChange={(e) => {
              const arquivos = Array.from(e.currentTarget.files ?? [])
              e.currentTarget.value = ''
              if (arquivos.length > 0) aoEscolherImagens(arquivos)
            }}
          />
        </>
      )}
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
  // O token da caixa (ADR-022, "Ditado feito dentro da nota"): a nota ainda não
  // tem id, e o Agent esconde do histórico o que for ditado aqui sob este token
  // até a nota nascer (criarNota o leva) ou a caixa fechar vazia (descartar).
  const [token] = useState(novoToken)
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
    if (t.trim() === '' && c.trim() === '') {
      // Nada a gravar: o que foi ditado aqui (e depois apagado) volta ao
      // histórico, para nada se perder calado. Melhor esforço: o Agent também
      // solta o que sobrar depois de 24 h.
      cliente.limparFoco({ token }, true).catch(() => undefined)
      return aoFechar(null)
    }
    gravando.current = true
    try {
      const nota = await cliente.criarNota({ titulo: t.trim(), corpo: c, token })
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
        rotulo="Texto da nova nota"
        foco={{ cliente, token }}
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
