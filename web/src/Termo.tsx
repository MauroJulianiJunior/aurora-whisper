// Um termo do dicionário, na IBM Plex Mono (a paleta do projeto: a mono é
// dos dados técnicos). Herda a cor de onde está; o verde do que foi
// confirmado vem da classe de fora (.confirmado .termo).
export function Termo({ children }: { children: string }) {
  return <span className="termo">{children}</span>
}
