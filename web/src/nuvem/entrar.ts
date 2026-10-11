// O que as telas de entrar dividem (ADR-019, F4): a frase do erro, o login de
// teste, e a janela à parte que entra de novo sem a página sair do lugar.
//
// A janela à parte (decisão do Mauro, 05-out-2026: "Guarda até entrar de
// novo"): a sessão acabou com um texto não salvo na tela, e entrar pelo
// Google navegaria a página inteira, levando o texto, que só existe na
// memória dela. Então o login vai numa janela à parte, aberta no clique, no
// endereço do próprio app (?janela=entrar), que pede o POST /auth/start e
// segue para o Google; o Google devolve a janela ao app (?janela=entrou),
// que avisa as outras abas e se fecha. A aba do texto confere quem entrou
// (GET /api/v1/me) com o aviso, ou quando volta a ficar à vista.
//
// O aviso vai por BroadcastChannel, não pelo window.opener: o servidor manda
// Cross-Origin-Opener-Policy: same-origin (seguranca.go), e a janela que
// passa pelo Google perde o opener de vez. O canal é da mesma origem, só na
// memória (não guarda nada no aparelho), e o que chega por ele é só um
// "confira": quem decide é o /me.

import { useEffect, useState } from 'react'
import { ErroDaApi } from '../api'
import type { ClienteNuvem, Eu, LoginDeTeste, Portao } from './cliente'

// Reentrada: o que a faixa da sessão usa para a pessoa entrar de novo sem a
// página sair do lugar. O AppNuvem monta uma por entrada (um Painel).
export interface Reentrada {
  // cliente: o da nuvem SEM o portão: o /me, o login de teste e o começo do
  // login do Google passam.
  cliente: ClienteNuvem
  // eu: a conta do Painel. Só ela volta ao texto.
  eu: Eu
  portao: Portao
  // host: o nome do servidor na barra de endereço (o login de teste).
  host: string
  abrirJanela(url: string): boolean
  // irPara: entrar nesta aba, quando a janela foi bloqueada.
  irPara(url: string): void
}

// enderecoDaJanela: onde a janela à parte abre; voltaDaJanela: a volta que
// ela pede ao /auth/start (um caminho da origem, ValidarVolta no servidor).
export const enderecoDaJanela = '/?janela=entrar'
export const voltaDaJanela = '/?janela=entrou'

export type ModoDaJanela = 'entrar' | 'entrou'

// modoDaJanela: a página é a janela de entrar (o ?janela= do endereço), ou
// o app de sempre (null).
export function modoDaJanela(busca: string): ModoDaJanela | null {
  const modo = new URLSearchParams(busca).get('janela')
  return modo === 'entrar' || modo === 'entrou' ? modo : null
}

const nomeDaJanela = 'aurora-whisper-entrar'

// abrirJanelaPadrao abre a janela à parte. Precisa vir do clique (o
// navegador só deixa abrir assim). false: o navegador a bloqueou. Sem
// noopener: com ele, o window.open sempre devolve null, e não daria para
// saber do bloqueio (a janela é do próprio app). No Android, abre numa aba.
export function abrirJanelaPadrao(url: string): boolean {
  const janela = window.open(url, nomeDaJanela, 'popup,width=480,height=640')
  return janela !== null && !janela.closed
}

export const nomeDoCanal = 'aurora-whisper-sessao'

// avisarQueEntrou: a janela à parte diz às outras abas da origem que a
// sessão voltou. Sem BroadcastChannel no navegador, fica a outra metade: a
// aba confere quando volta a ficar à vista.
export function avisarQueEntrou() {
  if (typeof BroadcastChannel !== 'function') return
  const canal = new BroadcastChannel(nomeDoCanal)
  canal.postMessage('entrou')
  canal.close()
}

// fraseDoLogin: a frase de quando o login não começou. O 429 tem a sua; o
// resto (rede, servidor, recusa) é a mesma, sem detalhe.
export function fraseDoLogin(e: unknown): string {
  if (e instanceof ErroDaApi && e.status === 429) return 'Muitas tentativas. Espere um minuto e tente de novo.'
  return 'Não deu para entrar. Tente de novo.'
}

// useVoltaDoCache: voltar do Google pelo Voltar do navegador (a conta errada
// no seletor, por exemplo) pode trazer a página do cache de ida e volta
// (bfcache), com o botão ainda em "Abrindo o Google…" e o clique ignorado. O
// pageshow com persisted é essa volta: soltar devolve o botão.
export function useVoltaDoCache(soltar: () => void) {
  useEffect(() => {
    const voltou = (e: PageTransitionEvent) => {
      if (e.persisted) soltar()
    }
    window.addEventListener('pageshow', voltou)
    return () => window.removeEventListener('pageshow', voltou)
  }, [soltar])
}

// servidorDoPC: o login de teste só existe num servidor em localhost (o
// NewCloud com a tag logindeteste recusa outro PUBLIC_BASE_URL). Fora dele,
// a tela nem pergunta: o botão do Google aparece de cara, sem um pedido a
// mais nem o 404 dele no console.
const servidorDoPC = (host: string) => host === 'localhost' || host === '127.0.0.1'

// useLoginDeTeste: o login de teste do servidor, perguntado uma vez, quando
// perguntar fica verdadeiro. undefined: ainda perguntando; null: não há (um
// 404, que é todo servidor de verdade, ou fora de localhost, sem perguntar).
export function useLoginDeTeste(cliente: ClienteNuvem, host: string, perguntar: boolean): LoginDeTeste | null | undefined {
  const [teste, setTeste] = useState<LoginDeTeste | null | undefined>(() => (servidorDoPC(host) ? undefined : null))
  const falta = perguntar && teste === undefined
  useEffect(() => {
    if (!falta) return
    let vivo = true
    cliente.loginDeTeste().then(
      (t) => vivo && setTeste(t),
      () => vivo && setTeste(null),
    )
    return () => {
      vivo = false
    }
  }, [cliente, falta])
  return teste
}
