// O símbolo do sistema, o "pico estourado": os colchetes do design system
// emolduram a voz, e uma barra fura a moldura. O desenho é o de
// docs/marca/pico-estourado/mark-small.svg, a versão que lê em tamanho
// pequeno.
//
// O arquivo vem num ladrilho preto (#070A09), que é o favicon. No
// cabeçalho o ladrilho não entra: no escuro ele some no fundo, que é o
// mesmo preto, e no claro viraria a coisa mais pesada da barra. As cores
// seguem as variantes da própria marca — no escuro as de mark.svg (verde e
// off-white), no claro as de mark-light.svg (verde profundo e preto) —
// pelos tokens --marca-moldura e --marca-traco do index.css.
// marca.test.tsx confere o desenho e as cores contra os arquivos.
//
// 16 px, o tamanho da bandeja e da aba: a grade do desenho é de 32, e os
// traços de 4 unidades viram 2 px inteiros. A 24 px viravam 3 px com as
// bordas em meio pixel, e a marca saía borrada (revisão de 29-set-2026).
export function Marca({ tamanho = 16 }: { tamanho?: number }) {
  return (
    <svg className="marca" width={tamanho} height={tamanho} viewBox="0 0 32 32" aria-hidden="true" focusable="false">
      <path
        className="marca-moldura"
        d="M4 12V11A3 3 0 0 1 7 8H8M24 8H25A3 3 0 0 1 28 11V12M28 20V21A3 3 0 0 1 25 24H24M8 24H7A3 3 0 0 1 4 21V20"
        fill="none"
        strokeWidth="4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path className="marca-pico" d="M16 4V28" fill="none" strokeWidth="4" strokeLinecap="round" />
    </svg>
  )
}
