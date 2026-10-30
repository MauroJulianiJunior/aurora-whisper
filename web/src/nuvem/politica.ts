// A política de privacidade desta instância (docs/deploy-privacidade.md). Quem
// sobe a nuvem a publica em GET /privacidade, preenchida com os dados dele; uma
// instância sem ela responde 404 e o app não mostra link nenhum. O app descobre
// com um HEAD na própria origem (sem corpo, sem cookie, sem cache): 200 é "tem".
// Qualquer outra coisa, ou a rede caída, é "não tem": o link só aparece com
// certeza, e nunca fica um link que leva a um 404.

import { useEffect, useState } from 'react'

export const caminhoDaPolitica = '/privacidade'

export async function politicaPublicada(sinal?: AbortSignal): Promise<boolean> {
  try {
    const r = await fetch(caminhoDaPolitica, { method: 'HEAD', cache: 'no-store', credentials: 'omit', signal: sinal })
    return r.status === 200
  } catch {
    return false
  }
}

// usePoliticaPublicada: false até a nuvem confirmar. Nada pula de lugar: o link
// é uma linha pequena que aparece no fim da seção.
export function usePoliticaPublicada(): boolean {
  const [tem, setTem] = useState(false)
  useEffect(() => {
    const parar = new AbortController()
    void politicaPublicada(parar.signal).then((v) => {
      if (!parar.signal.aborted) setTem(v)
    })
    return () => parar.abort()
  }, [])
  return tem
}
