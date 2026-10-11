// Sessão do painel: o bilhete que o Agent põe na URL vira o token da
// Local API (internal/agent/painel/bilhete.go).
//
// O bilhete vale UMA troca. Por isso esta troca acontece uma vez por
// carga de página, fora do React: o StrictMode roda cada efeito duas
// vezes em desenvolvimento, e a segunda troca gastaria um bilhete que já
// não existe.

import { type Buscador, ErroDaApi, SemSessao, buscadorPadrao, pedir } from './api'

// lerBilhete tira o bilhete da URL e o apaga dela na hora, antes de
// qualquer requisição: o que sobra no histórico do navegador é a URL sem
// o bilhete.
export function lerBilhete(local: Pick<Location, 'search' | 'pathname' | 'hash'>, historico: Pick<History, 'replaceState' | 'state'>): string | null {
  const params = new URLSearchParams(local.search)
  const bilhete = params.get('t')
  if (bilhete === null) return null
  params.delete('t')
  const resto = params.toString()
  historico.replaceState(historico.state, '', local.pathname + (resto ? `?${resto}` : '') + local.hash)
  return bilhete
}

export async function trocarBilhete(bilhete: string, buscar: Buscador = buscadorPadrao): Promise<string> {
  let resposta: Response
  try {
    resposta = await pedir(buscar, '/api/sessao', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bilhete }),
    })
  } catch (e) {
    // 400 (corpo estranho) e 401 (bilhete gasto ou vencido) dizem a
    // mesma coisa para quem está na frente da tela: abra pela bandeja.
    if (e instanceof ErroDaApi && e.status === 400) throw new SemSessao()
    throw e
  }
  const { token } = (await resposta.json()) as { token?: string }
  if (!token) throw new SemSessao()
  return token
}

export function iniciarSessao(buscar: Buscador = buscadorPadrao): Promise<string> {
  const bilhete = lerBilhete(window.location, window.history)
  if (bilhete === null) return Promise.reject(new SemSessao())
  return trocarBilhete(bilhete, buscar)
}
