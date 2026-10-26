import type { ReactNode } from 'react'

// Os ícones da aba Notas, os da prévia aprovada (docs/notas-previa-n2.html):
// traço de 1,7 numa grade de 24, na cor do texto (currentColor). São
// decorativos: o botão que os carrega diz o que faz (aria-label).
export type NomeDoIcone =
  | 'fixar'
  | 'cor'
  | 'arquivar'
  | 'desarquivar'
  | 'mais'
  | 'formatar'
  | 'imagem'
  | 'marcar'
  | 'marcador'
  | 'lapis'
  | 'copiar'
  | 'conversa'
  | 'lixeira'
  | 'desmarcar'
  | 'restaurar'
  | 'grade'
  | 'lista'
  | 'detalhes'
  | 'seta'

const traco = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.7, strokeLinejoin: 'round', strokeLinecap: 'round' } as const

const desenhos: Record<NomeDoIcone, ReactNode> = {
  fixar: <path d="M9 4h6l-1 6 3 3v2H7v-2l3-3-1-6Z M12 15v5" {...traco} />,
  cor: (
    <>
      <path
        d="M12 3.5a8.5 8.5 0 1 0 0 17c1.2 0 1.8-.8 1.8-1.7 0-1.4-1.3-1.6-1.3-2.8 0-1 .8-1.6 1.8-1.6h2.2a4 4 0 0 0 4-4C20.5 6.6 16.7 3.5 12 3.5Z"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.7}
      />
      <circle cx="7.8" cy="11" r="1.2" fill="currentColor" />
      <circle cx="10.5" cy="7.4" r="1.2" fill="currentColor" />
      <circle cx="15" cy="7.6" r="1.2" fill="currentColor" />
    </>
  ),
  arquivar: <path d="M4 5h16v4H4z M5.5 9v10h13V9 M10 13h4" {...traco} />,
  desarquivar: <path d="M4 5h16v4H4z M5.5 9v10h13V9 M12 17v-5 M9.5 14.2 12 11.7l2.5 2.5" {...traco} />,
  mais: (
    <>
      <circle cx="12" cy="5.5" r="1.6" fill="currentColor" />
      <circle cx="12" cy="12" r="1.6" fill="currentColor" />
      <circle cx="12" cy="18.5" r="1.6" fill="currentColor" />
    </>
  ),
  formatar: (
    <>
      <path d="M9 6h11 M9 12h11 M9 18h11" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" />
      <circle cx="4.5" cy="6" r="1.3" fill="currentColor" />
      <circle cx="4.5" cy="12" r="1.3" fill="currentColor" />
      <circle cx="4.5" cy="18" r="1.3" fill="currentColor" />
    </>
  ),
  imagem: (
    <>
      <rect x="3.5" y="4.5" width="17" height="15" rx="2" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <circle cx="9" cy="10" r="1.6" fill="currentColor" />
      <path d="m4 18 5-5 4 4 2.5-2.5L20 19" {...traco} />
    </>
  ),
  marcar: (
    <>
      <rect x="4" y="4" width="16" height="16" rx="3" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <path d="m8 12.3 2.7 2.7L16 9.6" {...traco} />
    </>
  ),
  marcador: (
    <>
      <path d="M4 12.6V5a1 1 0 0 1 1-1h7.6l7.7 7.7a1.5 1.5 0 0 1 0 2.1l-6.5 6.5a1.5 1.5 0 0 1-2.1 0L4 12.6Z" {...traco} />
      <circle cx="8.5" cy="8.5" r="1.3" fill="currentColor" />
    </>
  ),
  lapis: <path d="M4 20h4L19 9l-4-4L4 16v4Z M13.5 6.5l4 4" {...traco} />,
  copiar: (
    <>
      <rect x="8" y="8" width="12" height="12" rx="2" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" fill="none" stroke="currentColor" strokeWidth={1.7} />
    </>
  ),
  conversa: <path d="M4 5.5h16v10H12l-4.5 3.5v-3.5H4Z M8 9.5h8 M8 12.5h5" {...traco} />,
  lixeira: <path d="M4.5 7h15 M9.5 7V4.5h5V7 M6.5 7l1 12.5h9l1-12.5" {...traco} />,
  desmarcar: (
    <>
      <rect x="4" y="4" width="16" height="16" rx="3" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <path d="M8.5 12h7" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" />
    </>
  ),
  restaurar: <path d="M4.5 12a7.5 7.5 0 1 0 2.4-5.5 M4.5 4.5v4h4" {...traco} />,
  grade: (
    <>
      <rect x="4" y="4" width="7" height="9" rx="1.5" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <rect x="13" y="4" width="7" height="5" rx="1.5" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <rect x="13" y="11" width="7" height="9" rx="1.5" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <rect x="4" y="15" width="7" height="5" rx="1.5" fill="none" stroke="currentColor" strokeWidth={1.7} />
    </>
  ),
  lista: <path d="M4 7h16 M4 12h16 M4 17h16" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" />,
  detalhes: (
    <>
      <rect x="3.5" y="5" width="17" height="14" rx="2" fill="none" stroke="currentColor" strokeWidth={1.7} />
      <path d="M3.5 10h17 M9.5 10v9" fill="none" stroke="currentColor" strokeWidth={1.7} />
    </>
  ),
  seta: <path d="m7 10 5 5 5-5" {...traco} />,
}

export function Icone({ nome }: { nome: NomeDoIcone }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      {desenhos[nome]}
    </svg>
  )
}
