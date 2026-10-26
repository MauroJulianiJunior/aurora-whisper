import { type ChangeEvent, type ClipboardEvent, Fragment, type KeyboardEvent, type MouseEvent, useLayoutEffect, useRef } from 'react'
import { type Linha, colar, dividir, marcarLinha, normalizarMarcados, numeroDaLinha, prefixoDigitado, puxar, voltar } from './corpo'
import { Icone } from './Icone'
import type { EditorDeLinhasEstado, PedidoDeFoco } from './useEditorDeLinhas'

// O editor por linhas (ADR-022, "A nota"): cada linha do corpo é o seu próprio
// campo, com o tipo dela (parágrafo, título, marcador, numerada, tarefa). O
// texto sempre sai como o corpo de uma linha por bloco (corpo.ts).
//
// O Agent insere o ditado no campo em foco de duas maneiras (ADR-011): colando
// o texto, ou digitando as teclas, e o "\n" chega como Enter. As duas viram
// linhas aqui: o Enter parte a linha; a colagem de várias linhas, e o campo que
// recebe um valor com quebra de linha de uma vez (o que a automação do
// Windows faz às vezes), são cortados em linhas e cada uma lê o seu prefixo.

const nomeDoCampo = (l: Linha) => (l.tipo === 'titulo' ? 'Título de seção' : l.tipo === 'paragrafo' ? 'Texto' : 'Item')
const temQuebra = /[\r\n]/

const marcada = (l: Linha) => l.tipo === 'tarefa' && l.feito

interface Props {
  editor: EditorDeLinhasEstado
  // rotulo: o nome do grupo, para o leitor de tela.
  rotulo: string
}

export function EditorDeLinhas({ editor, rotulo }: Props) {
  const { linhas, recolhido, consumirPedido, aplicar, atual, setAtual, selecao, escolher, lerAncora, fixarAncora } = editor
  const raiz = useRef<HTMLDivElement>(null)

  // Cumpre o pedido de foco da operação que acabou de acontecer: a linha pode
  // ter mudado de lugar (marcar), nascido (Enter) ou se juntado a outra.
  useLayoutEffect(() => {
    const p = consumirPedido()
    if (!p) return
    const linha = raiz.current?.querySelector(`[data-chave="${p.chave}"]`)
    if (!linha) return
    if (p.alvo === 'caixa') {
      linha.querySelector<HTMLElement>('[role="checkbox"]')?.focus()
      return
    }
    const campo = linha.querySelector('textarea')
    if (!campo) return
    campo.focus()
    campo.setSelectionRange(p.pos, p.pos)
  })

  const inicioDosMarcados = linhas.findIndex(marcada)
  const nMarcados = inicioDosMarcados < 0 ? 0 : linhas.length - inicioDosMarcados
  // Recolhidos, os marcados não estão na tela: o teclado também não passa por eles.
  const visiveis = recolhido && inicioDosMarcados >= 0 ? inicioDosMarcados : linhas.length

  function irPara(i: number, fim: boolean) {
    const campo = raiz.current?.querySelector(`[data-chave="${linhas[i]!.chave}"] textarea`) as HTMLTextAreaElement | null
    if (!campo) return
    campo.focus()
    const pos = fim ? campo.value.length : 0
    campo.setSelectionRange(pos, pos)
  }

  // estender escolhe as linhas entre a âncora e a linha i (Shift+clique, ou
  // Shift+seta no limite da linha): é a "seleção de várias linhas" do Formatar,
  // já que o navegador não seleciona texto entre campos diferentes.
  function estender(i: number) {
    const a = linhas.findIndex((l) => l.chave === (lerAncora() ?? atual))
    if (a < 0) return
    fixarAncora(linhas[a]!.chave)
    const [de, ate] = a < i ? [a, i] : [i, a]
    escolher(new Set(linhas.slice(de, ate + 1).map((l) => l.chave)))
  }

  function aoMudar(e: ChangeEvent<HTMLTextAreaElement>, i: number) {
    const valor = e.target.value
    const l = linhas[i]!
    // Um valor com quebra de linha que chegou de uma vez (a automação do
    // Windows, um arrastar-e-soltar): é o mesmo que colar no lugar da linha.
    if (temQuebra.test(valor)) {
      const r = colar(linhas, i, 0, l.texto.length, valor)
      return aplicar(r.linhas, r.foco)
    }
    // "- ", "1. " e "[] " no começo de um parágrafo viram o tipo.
    const virou = prefixoDigitado(l.tipo, l.texto, valor)
    if (virou) {
      return aplicar(
        normalizarMarcados(linhas.map((x) => (x.chave === l.chave ? { ...x, ...virou } : x))),
        { chave: l.chave, pos: virou.texto.length },
      )
    }
    aplicar(linhas.map((x) => (x.chave === l.chave ? { ...x, texto: valor } : x)))
  }

  function aoColar(e: ClipboardEvent<HTMLTextAreaElement>, i: number) {
    const texto = e.clipboardData.getData('text')
    // Uma linha só entra como o navegador faria (e o aoMudar lê o prefixo).
    if (!temQuebra.test(texto)) return
    e.preventDefault()
    const { selectionStart: ini, selectionEnd: fim } = e.currentTarget
    const r = colar(linhas, i, ini, fim, texto)
    aplicar(r.linhas, r.foco)
  }

  function aoTeclar(e: KeyboardEvent<HTMLTextAreaElement>, i: number) {
    if (e.nativeEvent.isComposing) return
    const l = linhas[i]!
    const { selectionStart: ini, selectionEnd: fim } = e.currentTarget
    const semMod = !e.ctrlKey && !e.metaKey && !e.altKey

    if (e.key === 'Enter' && semMod) {
      e.preventDefault()
      // Com texto escolhido, o Enter o apaga antes de partir a linha.
      const base = fim > ini ? linhas.map((x) => (x.chave === l.chave ? { ...x, texto: l.texto.slice(0, ini) + l.texto.slice(fim) } : x)) : linhas
      const r = dividir(base, i, ini)
      return aplicar(r.linhas, r.foco)
    }
    if (e.key === 'Backspace' && semMod && ini === 0 && fim === 0) {
      const r = voltar(linhas, i)
      if (!r) return
      e.preventDefault()
      return aplicar(r.linhas, r.foco)
    }
    if (e.key === 'Delete' && semMod && ini === l.texto.length && fim === ini) {
      const r = puxar(linhas, i)
      if (!r || i + 1 >= visiveis) return
      e.preventDefault()
      return aplicar(r.linhas, r.foco)
    }
    if (e.key === 'ArrowUp' && !e.ctrlKey && !e.metaKey && !e.altKey && ini === 0 && fim === 0 && i > 0) {
      e.preventDefault()
      if (e.shiftKey) estender(i - 1)
      else irPara(i - 1, true)
    } else if (e.key === 'ArrowDown' && !e.ctrlKey && !e.metaKey && !e.altKey && ini === l.texto.length && fim === ini && i + 1 < visiveis) {
      e.preventDefault()
      if (e.shiftKey) estender(i + 1)
      else irPara(i + 1, false)
    }
  }

  function aoPressionar(e: MouseEvent<HTMLDivElement>, i: number) {
    if (!e.shiftKey || atual === null) return
    // Shift+clique em outra linha escolhe o intervalo, em vez de mover o cursor.
    e.preventDefault()
    estender(i)
  }

  function alternar(l: Linha, i: number) {
    const feito = !l.feito
    const novas = marcarLinha(linhas, l.chave, feito)
    let foco: PedidoDeFoco | undefined = { chave: l.chave, pos: 0, alvo: 'caixa' }
    if (feito && recolhido) {
      // A linha marcada some da tela: o foco vai para a que ficou no lugar dela.
      const abertas = novas.filter((x) => !marcada(x))
      const vizinha = abertas[Math.min(i, abertas.length - 1)]
      foco = vizinha && { chave: vizinha.chave, pos: vizinha.texto.length }
    }
    aplicar(novas, foco)
  }

  const vazio = linhas.length === 1 && linhas[0]!.tipo === 'paragrafo' && linhas[0]!.texto === ''

  return (
    <div className="editor-de-linhas" role="group" aria-label={rotulo} ref={raiz}>
      {linhas.map((l, i) => (
        <Fragment key={l.chave}>
          {i === inicioDosMarcados && (
            <button type="button" className="recolher" aria-expanded={!recolhido} onClick={editor.alternarRecolhido}>
              {nMarcados} {nMarcados === 1 ? 'marcado' : 'marcados'}
              <span className={recolhido ? 'seta recolhida' : 'seta'}>
                <Icone nome="seta" />
              </span>
            </button>
          )}
          {i < visiveis && (
            <div
              className={['linha-editada', `l-${l.tipo}`, l.feito ? 'feita' : '', selecao.has(l.chave) ? 'escolhida' : ''].filter(Boolean).join(' ')}
              data-chave={l.chave}
              onMouseDown={(e) => aoPressionar(e, i)}
            >
              {l.tipo === 'tarefa' && (
                <button
                  type="button"
                  role="checkbox"
                  className={l.feito ? 'caixa feita' : 'caixa'}
                  aria-checked={l.feito}
                  aria-label={l.texto.trim() || 'Item vazio'}
                  onClick={() => alternar(l, i)}
                />
              )}
              {l.tipo === 'marcador' && (
                <span className="prefixo" aria-hidden="true">
                  •
                </span>
              )}
              {l.tipo === 'numerada' && (
                <span className="prefixo numero" aria-hidden="true">
                  {numeroDaLinha(linhas, i)}.
                </span>
              )}
              <textarea
                className="campo-da-linha"
                rows={1}
                lang="pt-BR"
                value={l.texto}
                aria-label={nomeDoCampo(l)}
                placeholder={vazio ? 'Escreva a nota' : undefined}
                onFocus={() => {
                  setAtual(l.chave)
                  fixarAncora(l.chave)
                  if (selecao.size > 0) escolher(new Set())
                }}
                onChange={(e) => aoMudar(e, i)}
                onPaste={(e) => aoColar(e, i)}
                onKeyDown={(e) => aoTeclar(e, i)}
              />
            </div>
          )}
        </Fragment>
      ))}
    </div>
  )
}
