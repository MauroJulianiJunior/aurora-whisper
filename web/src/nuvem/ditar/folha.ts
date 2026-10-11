// A folha do aceite do ditado pelo celular (ADR-021, decisão H): o estado dela
// e o que ela faz com a nuvem. A tela está em FolhaDoAceite.tsx; este arquivo
// não conhece React, para o gravador (que pede o aceite antes do microfone) e
// as Configurações (que o pedem ao trocar de provedor) falarem com uma folha só.
//
// Quem pede recebe uma promessa: o provedor da voz que valeu, se a pessoa
// aceitou; null, se tocou em "Agora não", apertou o Esc ou só leu o aviso. O
// aceite fica gravado no servidor, por conta, com a versão do aviso e os dois
// provedores que a folha mostrou, o da transcrição e o do Polish, se houver
// (POST /api/v1/dictation/consent); o celular não guarda nada.
//
// O aceite do servidor tem esses dois lugares e cada aceite novo substitui o
// anterior: o que a pessoa aceita é o que a folha mostrou agora, e nada que
// valia antes é carregado para o aceite novo.

import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { VERSAO_DO_AVISO } from '../avisoDoDitado'
import type { ClienteNuvem } from '../cliente'
import type { ProvedorDeVoz, ProvedorDoPolish } from './aviso'
import { textoDaEspera } from './erros'

export interface PedidoDeAceite {
  // voz é o provedor da transcrição que vale se a pessoa aceitar. Com
  // escolherVoz, é o ponto de partida da escolha dela.
  voz: ProvedorDeVoz
  // polish é o provedor do Polish que está sendo ligado (ou que já está ligado,
  // quando a pessoa relê o aviso). Sem ele, o Polish não entra no pedido.
  polish?: ProvedorDoPolish
  // escolherVoz mostra a escolha do provedor da voz (o primeiro uso). Nas
  // Configurações, o provedor já foi escolhido ali.
  escolherVoz?: boolean
  // somenteLer reabre o aviso de quem já aceitou: só "Fechar", sem aceitar de novo.
  somenteLer?: boolean
}

export interface ResultadoDoAceite {
  voz: ProvedorDeVoz
}

// FolhaAberta é o que a tela desenha. Cada mudança é um objeto novo (o
// useSyncExternalStore compara por identidade).
export interface FolhaAberta {
  pedido: PedidoDeAceite
  voz: ProvedorDeVoz
  enviando: boolean
  erro: string | null
}

// fraseDoErro: nunca a mensagem do servidor, só uma frase fixa por situação.
function fraseDoErro(e: unknown): string {
  if (e instanceof ForaDoAr) return 'Sem conexão. Tente de novo.'
  if (e instanceof ErroDaApi) {
    if (e.status === 404) return 'Este servidor não tem o ditado pelo celular.'
    if (e.status === 429 || e.status === 503) return `Espere um pouco.${textoDaEspera(e.esperaSeg)}`
  }
  return 'Não deu para guardar o aceite. Tente de novo.'
}

export class Folha {
  private aberta: FolhaAberta | null = null
  private ouvintes = new Set<() => void>()
  private resolver: ((r: ResultadoDoAceite | null) => void) | null = null
  private vez = 0
  private semSessao: (() => void) | null = null
  private cliente: Pick<ClienteNuvem, 'aceitarAviso'>
  private recarregar: () => void

  // recarregar é o que o app faz quando o servidor diz que o aviso que ele
  // mostrou não é o atual (AVISO_DESATUALIZADO): ler a página de novo, para vir
  // o texto novo. Os testes põem um dublê.
  constructor(cliente: Pick<ClienteNuvem, 'aceitarAviso'>, recarregar: () => void = () => window.location.reload()) {
    this.cliente = cliente
    this.recarregar = recarregar
  }

  // estado e assinar são o contrato do useSyncExternalStore.
  estado = (): FolhaAberta | null => this.aberta

  assinar = (cb: () => void): (() => void) => {
    this.ouvintes.add(cb)
    return () => void this.ouvintes.delete(cb)
  }

  // ligar diz o que fazer quando o aceite leva 401: a folha fecha, e quem
  // ligou acende a faixa da sessão. Devolve quem desliga.
  ligar(semSessao: () => void): () => void {
    this.semSessao = semSessao
    return () => {
      if (this.semSessao === semSessao) this.semSessao = null
    }
  }

  // pedirAceite abre a folha. Uma só por vez: um segundo pedido, com a folha na
  // tela, não abre outra e volta null.
  pedirAceite(pedido: PedidoDeAceite): Promise<ResultadoDoAceite | null> {
    if (this.aberta) return Promise.resolve(null)
    this.vez++
    return new Promise((resolve) => {
      this.resolver = resolve
      this.mudar({ pedido, voz: pedido.voz, enviando: false, erro: null })
    })
  }

  // escolherVoz troca o provedor da voz na folha (o bloco do provedor muda
  // junto).
  escolherVoz(voz: ProvedorDeVoz): void {
    const a = this.aberta
    if (!a || a.enviando || !a.pedido.escolherVoz) return
    this.mudar({ ...a, voz, erro: null })
  }

  // aceitar é o "Aceito": grava o aceite no servidor, com a versão do aviso e
  // os provedores que a folha mostrou (o da voz e, se houver, o do Polish). A
  // folha só fecha com o servidor dizendo que gravou.
  async aceitar(): Promise<void> {
    const a = this.aberta
    if (!a || a.enviando || a.pedido.somenteLer) return
    const vez = this.vez
    this.mudar({ ...a, enviando: true, erro: null })
    try {
      await this.cliente.aceitarAviso(VERSAO_DO_AVISO, a.voz, a.pedido.polish ?? null)
    } catch (e) {
      if (vez !== this.vez || !this.aberta) return
      if (e instanceof SemSessao) {
        this.fechar(null)
        this.semSessao?.()
        return
      }
      if (e instanceof ErroDaApi && e.codigo === 'AVISO_DESATUALIZADO') {
        // O servidor tem outra versão do aviso: este app mostrou o texto velho,
        // e nada foi gravado. A folha fecha sem aceite e o app é lido de novo,
        // para a pessoa ler o texto novo no próximo toque.
        this.fechar(null)
        this.recarregar()
        return
      }
      this.mudar({ ...this.aberta, enviando: false, erro: fraseDoErro(e) })
      return
    }
    if (vez !== this.vez) return
    this.fechar({ voz: a.voz })
  }

  // recusar é o "Agora não", o Esc, e o "Fechar" de quem só lia. Com o aceite
  // a caminho, espera: o POST não se aborta.
  recusar(): void {
    const a = this.aberta
    if (!a || a.enviando) return
    this.fechar(null)
  }

  private fechar(r: ResultadoDoAceite | null) {
    const resolver = this.resolver
    this.resolver = null
    this.vez++
    this.mudar(null)
    resolver?.(r)
  }

  private mudar(a: FolhaAberta | null) {
    this.aberta = a
    for (const cb of [...this.ouvintes]) cb()
  }
}
