// A Aurora ilustrada (docs/marca/retrato/), nos momentos em que o painel
// "fala" com quem usa: histórico vazio, lixeira vazia, a tela de abrir pela
// bandeja e a faixa de Agent fora do ar. É decorativa — o texto ao lado diz
// tudo —, por isso aria-hidden e sem <title>.
//
// Os desenhos são os dos arquivos, copiados para cá como SVG inline (o
// painel não carrega nada de fora). marca.test.tsx confere, traço a traço,
// que continuam iguais aos de docs/marca/retrato/.
//
// Licença: o desenho segue a MIT do projeto, como o resto do repositório
// (LICENSE, THIRD_PARTY_NOTICES.md). O autor o liberou em 30-set-2026; antes
// era de direitos reservados.

// Aurora é a ilustração grande (mark.svg e mark-light.svg: o mesmo desenho,
// só muda a cor). O traço é --marca-traco — off-white no escuro, preto no
// claro — e os colchetes são --moldura, o verde do sistema nos dois.
export function Aurora({ tamanho }: { tamanho: number }) {
  return (
    <svg className="aurora" width={tamanho} height={tamanho} viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      <path
        className="aurora-moldura"
        d="M3.75 23.5L3.75 17.5C3.75 14.74 5.99 12.5 8.75 12.5L14 12.5M50 12.5L55.25 12.5C58.01 12.5 60.25 14.74 60.25 17.5L60.25 23.5M3.75 44.5L3.75 50.5C3.75 53.26 5.99 55.5 8.75 55.5L14 55.5M50 55.5L55.25 55.5C58.01 55.5 60.25 53.26 60.25 50.5L60.25 44.5"
        fill="none"
        strokeWidth="3.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <g fill="currentColor">
        <path
          fillRule="evenodd"
          d="M19.24 45.49C16.79 43.57 15.8 40.47 15.83 37.44C15.81 34.82 14.95 32.71 14.73 30.12C13.51 21.81 19.08 14.68 27.5 12.63C32 11.58 37.45 11.61 40.69 13.43C43.07 14.75 44.85 17.76 45.51 21.51C45.9 23.88 45.75 25.91 45.11 27.3C44.55 28.47 43.66 29.39 42.49 29.73L41.9 29.81L41.43 26.04L37.66 26.5L37.13 24.65L36.37 26.66L27.93 27.69L27.12 24.37L26.15 27.91L19.99 28.67C20.48 31.83 20.75 35.63 20.54 38.88C20.36 41.52 20.01 43.58 19.24 45.49ZM18.62 24C19.82 19.02 24.04 15.68 29.53 14.4C24.76 16.6 21.13 19.86 18.62 24Z"
        />
        <path
          fillRule="evenodd"
          d="M31.11 10.88C29.69 9.14 29.92 6.09 32.35 4.58C33.28 3.96 34.36 3.73 35.28 3.82C35.73 2.56 37.37 1.95 38.74 2.49C40.33 3.1 41.03 4.73 40.6 6.09C41.8 7.65 41.91 10.26 40.52 12.04C38.19 11.12 34.72 10.74 31.11 10.88ZM32.44 8.6C32.35 6.2 34.36 4.54 36.87 4.43C34.89 5.58 33.54 6.86 32.44 8.6Z"
        />
        <path d="M29.1 32.29C27.79 31.44 24.81 31.8 23.54 32.97L21.53 31.3C22.88 30.83 24.23 30.36 25.82 30.17C27.21 30 28.49 30.55 29.1 32.29ZM27.92 32.08C28.04 33.04 27.36 33.91 26.4 34.03C25.43 34.15 24.56 33.47 24.45 32.5C24.33 31.54 25.01 30.67 25.97 30.55C26.93 30.44 27.8 31.12 27.92 32.08ZM33.47 31.75C34.54 30.61 37.52 30.24 39.03 31.07L40.58 28.96C39.16 28.83 37.73 28.71 36.14 28.9C34.75 29.07 33.65 29.91 33.47 31.75ZM38.04 30.83C37.92 29.87 37.06 29.19 36.09 29.31C35.13 29.43 34.45 30.3 34.57 31.26C34.69 32.22 35.56 32.9 36.52 32.79C37.48 32.67 38.16 31.8 38.04 30.83ZM28.87 41.88C30.11 40.52 31.52 40.45 32.37 40.85C33.2 40.24 34.84 39.64 36.18 39.88C35.6 42.57 31.52 43.78 28.87 41.88Z" />
        <g fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round">
          <path d="M20.65 34.83C21.46 40.58 25.44 45.13 30.53 47.22C32.23 47.92 34.22 47.68 35.7 46.59C39.36 43.62 41.53 39.93 41.69 35.48" strokeWidth="2.4" />
          <path d="M42.01 29.89C44.12 28.22 46.34 29.06 46.36 31.68C46.4 33.69 45.08 35.26 42.07 35.33" strokeWidth="2.2" />
          <path d="M32.39 33.59C32.31 35.42 31.04 36.58 31.54 37.33C32.02 37.97 33 37.75 33.74 37.26" strokeWidth="1.9" />
          <path d="M27.9 46.5L27.8 51.2M37.3 45.7L37.5 51.2" strokeWidth="2.4" />
          <path d="M21.2 54.2C21.4 59.2 43.8 59.2 44 54.2" strokeWidth="2.8" />
          <circle cx="45.25" cy="37.46" r="1.3" strokeWidth="1.5" />
        </g>
        <path
          fillRule="evenodd"
          d="M25.7 50.6C25.7 53.09 23.69 55.1 21.2 55.1C18.71 55.1 16.7 53.09 16.7 50.6C16.7 48.11 18.71 46.1 21.2 46.1C23.69 46.1 25.7 48.11 25.7 50.6ZM24.15 50.6C24.15 48.97 22.83 47.65 21.2 47.65C19.57 47.65 18.25 48.97 18.25 50.6C18.25 52.23 19.57 53.55 21.2 53.55C22.83 53.55 24.15 52.23 24.15 50.6ZM23 50.6C23 51.59 22.19 52.4 21.2 52.4C20.21 52.4 19.4 51.59 19.4 50.6C19.4 49.61 20.21 48.8 21.2 48.8C22.19 48.8 23 49.61 23 50.6ZM48.5 50.6C48.5 53.09 46.49 55.1 44 55.1C41.51 55.1 39.5 53.09 39.5 50.6C39.5 48.11 41.51 46.1 44 46.1C46.49 46.1 48.5 48.11 48.5 50.6ZM46.95 50.6C46.95 48.97 45.63 47.65 44 47.65C42.37 47.65 41.05 48.97 41.05 50.6C41.05 52.23 42.37 53.55 44 53.55C45.63 53.55 46.95 52.23 46.95 50.6ZM45.8 50.6C45.8 51.59 44.99 52.4 44 52.4C43.01 52.4 42.2 51.59 42.2 50.6C42.2 49.61 43.01 48.8 44 48.8C44.99 48.8 45.8 49.61 45.8 50.6Z"
        />
      </g>
    </svg>
  )
}
