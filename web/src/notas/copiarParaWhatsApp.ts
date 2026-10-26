import type { ReactNode } from 'react'
import { paraWhatsApp } from './whatsapp'

interface Opcoes {
  // avisar: a pílula de baixo da tela.
  avisar(texto: ReactNode): void
  // estreita: a tela é a do celular (useTelaEstreita). Lá, com o compartilhar
  // do sistema, a pessoa escolhe o WhatsApp na folha do Android.
  estreita: boolean
  // aoSelecionar: o que fazer se a cópia não deu ("Abra a nota e use Ctrl+C.").
  aoSelecionar: string
}

const semCopia = (aoSelecionar: string) => `Não consegui copiar. ${aoSelecionar}`

// escrever grava na área de transferência. É chamada no clique, antes de
// qualquer await: o navegador só deixa gravar enquanto o gesto de quem clicou
// vale.
function escrever(texto: string, { avisar, aoSelecionar }: Opcoes) {
  const area = navigator.clipboard
  if (!area) return avisar(semCopia(aoSelecionar))
  area.writeText(texto).then(
    () => avisar('Copiado para o WhatsApp.'),
    () => avisar(semCopia(aoSelecionar)),
  )
}

// copiarParaWhatsApp converte a nota em texto de WhatsApp e a entrega, sempre
// dentro do clique de quem pediu (nunca pelo Agent: CLAUDE.md, "Testes que agem
// no sistema real"). No celular, com navigator.share, abre a folha de
// compartilhar; se ela falhar, ou fora do celular, vai para a área de
// transferência. Fechar a folha sem escolher não é falha: nada a dizer.
export function copiarParaWhatsApp(titulo: string, corpo: string, opcoes: Opcoes) {
  const texto = paraWhatsApp(titulo, corpo)
  if (texto === '') return opcoes.avisar('A nota está vazia: não há o que copiar.')
  if (opcoes.estreita && typeof navigator.share === 'function') {
    navigator.share({ text: texto }).then(undefined, (e: unknown) => {
      if (e instanceof DOMException && e.name === 'AbortError') return
      escrever(texto, opcoes)
    })
    return
  }
  escrever(texto, opcoes)
}
