import { type RefObject, useEffect } from 'react'
import type { AlvoDoFoco, ClienteDeNotas } from './tipos'

// A renovação do aviso: o Agent o esquece em 60 s sem notícia (notafoco.TTL).
export const RENOVA_O_FOCO_EM = 20_000

// ehCampoDeTexto: o título, uma linha do corpo ou qualquer campo editável. A
// caixa de seleção da lista de marcar não conta: ditar nela não escreve nada.
function ehCampoDeTexto(e: EventTarget | Element | null): e is HTMLElement {
  if (e instanceof HTMLTextAreaElement) return true
  if (e instanceof HTMLInputElement) return ['text', 'search', ''].includes(e.type)
  return e instanceof HTMLElement && e.isContentEditable
}

// useFocoDaNota avisa o Agent de que o cursor está dentro de uma nota (ADR-022,
// "Ditado feito dentro da nota"): enquanto o título ou uma linha do corpo (dentro
// de raiz) tem o foco E a janela do navegador também, manda PUT /api/notas/foco e
// o renova a cada 20 s; ao perder o foco, perder a janela (blur da janela, aba
// escondida, pagehide) ou desmontar (fechar, Esc, Ctrl+Enter, clicar fora), manda
// o DELETE. Com o aviso ativo, o ditado feito agora nasce fora do histórico.
//
// O alvo é a nota que existe (nota) ou o token da caixa "Criar uma nota" (token);
// sem nenhum dos dois, o gancho não faz nada. O aviso é melhor esforço: uma falha
// é ignorada, porque o Agent o deixa vencer sozinho e o pior caso é o ditado
// aparecer no histórico, que é o comportamento de sempre. As chamadas vão numa
// fila só, para um DELETE nunca passar na frente do PUT que ele desfaz.
export function useFocoDaNota(
  raiz: RefObject<HTMLElement | null>,
  cliente: ClienteDeNotas | undefined,
  nota: string | undefined,
  token: string | undefined,
) {
  useEffect(() => {
    const el = raiz.current
    const alvo: AlvoDoFoco | null = nota ? { nota } : token ? { token } : null
    if (!el || !cliente || !alvo) return

    let avisado = false
    let renovacao: ReturnType<typeof setInterval> | undefined
    let fila: Promise<unknown> = Promise.resolve()
    const enfileirar = (op: () => Promise<void>) => {
      fila = fila.then(op).catch(() => undefined)
    }
    const avisar = () => enfileirar(() => cliente.definirFoco(alvo))

    const ativar = () => {
      if (avisado) return
      avisado = true
      avisar()
      renovacao = setInterval(avisar, RENOVA_O_FOCO_EM)
    }
    const desativar = () => {
      if (!avisado) return
      avisado = false
      clearInterval(renovacao)
      enfileirar(() => cliente.limparFoco(alvo))
    }
    // reavaliar olha o estado de agora: um campo de texto da nota com o foco, na
    // janela que tem o foco.
    const reavaliar = () => {
      const a = document.activeElement
      if (a && el.contains(a) && ehCampoDeTexto(a) && document.hasFocus()) ativar()
      else desativar()
    }

    const entrou = (e: FocusEvent) => {
      if (ehCampoDeTexto(e.target) && document.hasFocus()) ativar()
    }
    const saiu = (e: FocusEvent) => {
      // Do título para o corpo (ou de linha para linha) o foco passa direto: o
      // aviso não pisca.
      const para = e.relatedTarget
      if (para instanceof Element && el.contains(para) && ehCampoDeTexto(para)) return
      // Sem destino (a linha que tinha o foco saiu da tela, como no Enter que
      // divide a linha): espera o foco assentar antes de decidir.
      if (para === null) setTimeout(reavaliar, 0)
      else desativar()
    }
    const escondida = () => (document.visibilityState === 'hidden' ? desativar() : reavaliar())

    el.addEventListener('focusin', entrou)
    el.addEventListener('focusout', saiu)
    window.addEventListener('blur', desativar)
    window.addEventListener('focus', reavaliar)
    window.addEventListener('pagehide', desativar)
    document.addEventListener('visibilitychange', escondida)
    // Quem abre a nota já põe o cursor no corpo (CamposDaNota): o foco veio
    // antes destes ouvintes.
    reavaliar()

    return () => {
      el.removeEventListener('focusin', entrou)
      el.removeEventListener('focusout', saiu)
      window.removeEventListener('blur', desativar)
      window.removeEventListener('focus', reavaliar)
      window.removeEventListener('pagehide', desativar)
      document.removeEventListener('visibilitychange', escondida)
      desativar()
    }
  }, [raiz, cliente, nota, token])
}
