import { type ClipboardEvent, type KeyboardEvent, useEffect, useRef } from 'react'
import { colar } from './corpo'
import { EditorDeLinhas } from './EditorDeLinhas'
import type { ClienteDeNotas } from './tipos'
import type { EditorDeLinhasEstado } from './useEditorDeLinhas'
import { useFocoDaNota } from './useFocoDaNota'

interface Props {
  titulo: string
  aoMudarTitulo(titulo: string): void
  editor: EditorDeLinhasEstado
  // rotulo: o nome do grupo do corpo, para o leitor de tela.
  rotulo: string
  // foco: de que nota (ou de que caixa "Criar uma nota", pelo token) estes campos
  // são, para avisar o Agent enquanto o cursor está neles (useFocoDaNota). Sem
  // ele, nada é avisado.
  foco?: { cliente: ClienteDeNotas; nota?: string; token?: string }
}

const temQuebra = /[\r\n]/
const quebras = /\r\n?|\n/

// CamposDaNota é o miolo da nota, na janela dela e no "Criar uma nota": o título e o
// corpo por linhas. O título é um campo de uma linha só: o texto de várias
// linhas colado ali (um ditado que caiu no campo errado) não perde nada, a
// primeira linha fica no título e o resto vai para o começo do corpo.
export function CamposDaNota({ titulo, aoMudarTitulo, editor, rotulo, foco }: Props) {
  const raiz = useRef<HTMLDivElement>(null)
  useFocoDaNota(raiz, foco?.cliente, foco?.nota, foco?.token)
  const campoTitulo = useRef<HTMLInputElement>(null)

  // O cursor começa no corpo, também na nota nova, como no Keep (decisão do
  // Mauro, 25-out-2026: o primeiro ditado ia para o título, que é opcional).
  useEffect(() => {
    // O fim da última linha ainda aberta: o item marcado fica embaixo, mas quem
    // abre a lista quer continuar nela (um ditado ali cairia num item já feito).
    const abertos = raiz.current?.querySelectorAll('.linha-editada:not(.feita) textarea')
    const campos = abertos?.length ? abertos : raiz.current?.querySelectorAll('textarea')
    const ultimo = campos?.item(campos.length - 1) as HTMLTextAreaElement | undefined
    if (!ultimo) return
    ultimo.focus()
    ultimo.setSelectionRange(ultimo.value.length, ultimo.value.length)
  }, [])

  function focarCorpo() {
    const campo = raiz.current?.querySelector('textarea')
    campo?.focus()
    campo?.setSelectionRange(0, 0)
  }

  function aoColar(e: ClipboardEvent<HTMLInputElement>) {
    const texto = e.clipboardData.getData('text')
    if (!temQuebra.test(texto)) return
    e.preventDefault()
    const campo = e.currentTarget
    const [primeira, ...resto] = texto.split(quebras)
    aoMudarTitulo(titulo.slice(0, campo.selectionStart ?? titulo.length) + (primeira ?? '') + titulo.slice(campo.selectionEnd ?? titulo.length))
    // O resto entra antes do que já estava no corpo: sobra uma linha em branco
    // só se a primeira linha do corpo tinha texto.
    const primeiraDoCorpo = editor.linhas[0]!
    const depois = resto.join('\n')
    const r = colar(editor.linhas, 0, 0, 0, primeiraDoCorpo.texto === '' && primeiraDoCorpo.tipo === 'paragrafo' ? depois : `${depois}\n`)
    editor.aplicar(r.linhas)
  }

  function aoTeclar(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== 'Enter' || e.nativeEvent.isComposing) return
    e.preventDefault()
    focarCorpo()
  }

  return (
    <div className="campos-da-nota" ref={raiz}>
      <input
        ref={campoTitulo}
        type="text"
        className="titulo-campo"
        placeholder="Título"
        aria-label="Título"
        lang="pt-BR"
        maxLength={300}
        value={titulo}
        onChange={(e) => aoMudarTitulo(e.target.value)}
        onPaste={aoColar}
        onKeyDown={aoTeclar}
      />
      <EditorDeLinhas editor={editor} rotulo={rotulo} />
    </div>
  )
}
