// O link discreto para a política de privacidade da instância, só quando ela
// existe (politica.ts). Abre numa aba nova: a página é do servidor, fora do app.

import { caminhoDaPolitica, usePoliticaPublicada } from './politica'

export function LinkDaPolitica() {
  if (!usePoliticaPublicada()) return null
  return (
    <p className="ajuda">
      <a href={caminhoDaPolitica} target="_blank" rel="noopener">
        Política de privacidade
      </a>
    </p>
  )
}
