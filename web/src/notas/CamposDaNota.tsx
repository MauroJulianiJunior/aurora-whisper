import { type ClipboardEvent, type KeyboardEvent, useEffect, useRef } from 'react'
import { colar } from './corpo'
import { EditorDeLinhas } from './EditorDeLinhas'
import type { EditorDeLinhasEstado } from './useEditorDeLinhas'

interface Props {
  titulo: string
  aoMudarTitulo(titulo: string): void
  editor: EditorDeLinhasEstado
  // focoInicial: onde o cursor começa: no título (nota nova) ou no fim do
  // texto (nota que já tem o que ler).
  focoInicial: 'titulo' | 'corpo'
  // rotulo: o nome do grupo do corpo, para o leitor de tela.
  rotulo: string
}

const temQuebra = /[\r\n]/
const quebras = /\r\n?|\n/

// CamposDaNota é o miolo da nota, na janela dela e no "Criar uma nota": o título e o
// corpo por linhas. O título é um campo de uma linha só: o texto de várias
// linhas colado ali (um ditado que caiu no campo errado) não perde nada, a
// primeira linha fica no título e o resto vai para o começo do corpo.
export function CamposDaNota({ titulo, aoMudarTitulo, editor, focoInicial, rotulo }: Props) {
  const raiz = useRef<HTMLDivElement>(null)
  const campoTitulo = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (focoInicial === 'titulo') return campoTitulo.current?.focus()
    // O fim da última linha ainda aberta: o item marcado fica embaixo, mas quem
    // abre a lista quer continuar nela (um ditado ali cairia num item já feito).
    const abertos = raiz.current?.querySelectorAll('.linha-editada:not(.feita) textarea')
    const campos = abertos?.length ? abertos : raiz.current?.querySelectorAll('textarea')
    const ultimo = campos?.item(campos.length - 1) as HTMLTextAreaElement | undefined
    if (!ultimo) return
    ultimo.focus()
    ultimo.setSelectionRange(ultimo.value.length, ultimo.value.length)
  }, [focoInicial])

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
