// A seção "Aprendido" das Configurações do celular (ADR-019, F8h; 08-ui-ux-spec.md,
// "Configurações do celular"): as trocas que os PCs aprenderam, uma por linha,
// com o Desfazer. Entre os Aparelhos e as Sessões abertas.
//
// O celular não aprende (aprender é só no PC, ADR-017): não há botão de
// criar, e por isso o Desfazer pergunta na própria linha, como o Revogar dos
// Aparelhos — dali não dá para aprender de novo. Desfeito, a troca deixa de
// valer em todos os PCs (chega a eles pelo pull). O texto da troca nunca vai
// na URL: o id vai no corpo do POST.

import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { ErroDaApi, problemaDe } from '../api'
import type { PropsDasConfiguracoes } from '../ambiente'
import { Termo } from '../Termo'
import type { Aprendido, Aprendidos, ClienteNuvem } from './cliente'

interface Props extends Pick<PropsDasConfiguracoes, 'visivel' | 'aoPerderLigacao' | 'avisar'> {
  cliente: ClienteNuvem
  // leitura sobe quando a tela pai quer as listas de novo (depois do "apagar
  // meus dados", que apaga o aprendido também).
  leitura: number
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

// A frase do 404 do servidor para a troca que não existe (ou é de outra conta):
// internal/server/store_routes.go, e o `06`. Só o 404 com ela vale como "já
// desfeita". Qualquer outro 404 (o painel estático publicado antes do binário
// do servidor, um proxy que não conhece a rota) não desfez nada, e dizer que
// a troca saiu de todos os aparelhos seria mentira.
// A pergunta da linha, que se vê e que entra no nome do grupo.
const PERGUNTA = 'Desfazer? A troca deixa de valer em todos os PCs.'

const FRASE_TROCA_NAO_ENCONTRADA = 'troca aprendida não encontrada'
const jaNaoEstaAi = (e: unknown) => e instanceof ErroDaApi && e.status === 404 && e.message === FRASE_TROCA_NAO_ENCONTRADA

// Para onde o foco vai depois do render: o Cancelar da pergunta que abriu (o
// lado seguro), o Desfazer que pediu (de volta, no Cancelar), ou o Desfazer
// que ficou no lugar do que saiu.
type Foco = { tipo: 'pergunta' } | { tipo: 'botao'; id: string } | { tipo: 'lugar'; indice: number }

export function AprendidoNaNuvem({ cliente, visivel, leitura, aoPerderLigacao, avisar }: Props) {
  const perderLigacao = useEffectEvent(aoPerderLigacao)
  const [lista, setLista] = useState<Aprendidos | null>(null)
  const [erro, setErro] = useState<string | null>(null)
  // confirmando: o id da linha cuja pergunta está aberta; desfazendo: o do
  // que está indo. Um por vez.
  const [confirmando, setConfirmando] = useState<string | null>(null)
  const [desfazendo, setDesfazendo] = useState<string | null>(null)
  const ul = useRef<HTMLUListElement>(null)
  const titulo = useRef<HTMLHeadingElement>(null)
  const focar = useRef<Foco | null>(null)

  // A lista é lida quando a tela aparece: outro aparelho pode ter aprendido
  // ou desfeito enquanto ela estava escondida.
  useEffect(() => {
    if (!visivel) return
    let vivo = true
    cliente.aprendidos().then(
      (l) => {
        if (!vivo) return
        setLista(l)
        setErro(null)
      },
      (e: unknown) => {
        if (!vivo) return
        if (problemaDe(e)) perderLigacao(e)
        else setErro(`Não deu para ver o aprendido: ${mensagem(e)}`)
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, visivel, leitura])

  useEffect(() => {
    const alvo = focar.current
    if (alvo === null) return
    focar.current = null
    if (alvo.tipo === 'pergunta') {
      ul.current?.querySelector<HTMLElement>('.confirmar .acoes > button:last-child')?.focus()
      return
    }
    if (alvo.tipo === 'botao') {
      ul.current?.querySelector<HTMLElement>(`[data-aprendido="${alvo.id}"] > button`)?.focus()
      return
    }
    const botoes = ul.current?.querySelectorAll<HTMLElement>('li > button')
    const b = botoes?.[Math.min(alvo.indice, botoes.length - 1)]
    if (b) b.focus()
    else titulo.current?.focus()
  }, [lista, confirmando])

  function perguntar(a: Aprendido) {
    if (desfazendo !== null) return
    focar.current = { tipo: 'pergunta' }
    setConfirmando(a.id)
    setErro(null)
  }

  function cancelar() {
    // Com o POST no ar, desistir não o aborta: a pergunta sumiria, a troca
    // sairia da lista mesmo assim e a pílula diria o que a pessoa acabou de
    // pedir para não acontecer. O Cancelar espera o pedido terminar.
    if (desfazendo !== null) return
    if (confirmando !== null) focar.current = { tipo: 'botao', id: confirmando }
    setConfirmando(null)
  }

  async function desfazer(a: Aprendido) {
    if (desfazendo !== null || !lista) return
    setDesfazendo(a.id)
    setErro(null)
    try {
      await cliente.desfazerAprendido(a.id)
    } catch (e) {
      // Já desfeita em outro lugar (ou de outra conta): a troca já não está
      // aí, e some da lista do mesmo jeito.
      if (!jaNaoEstaAi(e)) {
        setDesfazendo(null)
        if (problemaDe(e)) aoPerderLigacao(e)
        else setErro(`Não deu para desfazer: ${mensagem(e)}`)
        return
      }
    }
    setDesfazendo(null)
    setConfirmando(null)
    focar.current = { tipo: 'lugar', indice: lista.items.findIndex((x) => x.id === a.id) }
    setLista({ ...lista, items: lista.items.filter((x) => x.id !== a.id) })
    avisar('Desfeito em todos os aparelhos.')
  }

  return (
    <section className="secao" aria-labelledby="cfg-aprendido">
      <h2 id="cfg-aprendido" className="secao-titulo" ref={titulo} tabIndex={-1}>
        Aprendido
      </h2>
      <p className="secao-intro">Trocas que os PCs aprenderam ao corrigir ditados.</p>
      {lista === null && !erro && <p className="ajuda">Carregando…</p>}
      {lista && lista.items.length > 0 && (
        <ul className="sessoes aprendidos" ref={ul}>
          {lista.items.map((a) => (
            <li key={a.id} data-aprendido={a.id}>
              <span className="sessao">
                <span className="sessao-nome">
                  <Termo>{a.variante}</Termo>
                  <span aria-hidden="true"> →</span>
                  <span className="so-leitor"> vira</span> <Termo>{a.termo}</Termo>
                </span>
                {a.dominio && (
                  <span className="sessao-uso">
                    <span className="so-leitor">, </span>
                    {a.restrito ? 'só no domínio' : 'domínio'} {a.dominio}
                  </span>
                )}
              </span>
              {/* O nome do grupo da pergunta diz qual troca é ("sf vira ECF. Desfazer? A troca deixa
                  de valer em todos os PCs."): o foco vai para o Cancelar, e o botão da linha, que
                  tinha o nome da troca, sai de cena. É o "Revogar {nome}?" dos Aparelhos, sem mudar
                  o texto que se vê. Um aria-label montado dos dados, e não um aria-labelledby no
                  .sessao-nome: o nome calculado do que está embaixo depende de como cada leitor de
                  tela junta o texto dos spans. */}
              {confirmando === a.id ? (
                <div
                  className="confirmar"
                  role="group"
                  aria-label={`${a.variante} vira ${a.termo}. ${PERGUNTA}`}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') {
                      e.preventDefault()
                      cancelar()
                    }
                  }}
                >
                  <p className="ajuda">{PERGUNTA}</p>
                  <div className="acoes">
                    <button type="button" className="botao risco" aria-disabled={desfazendo !== null || undefined} onClick={() => void desfazer(a)}>
                      {desfazendo === a.id ? 'Desfazendo…' : 'Desfazer'}
                    </button>
                    <button type="button" className="botao" aria-disabled={desfazendo !== null || undefined} onClick={cancelar}>
                      Cancelar
                    </button>
                  </div>
                </div>
              ) : (
                <button
                  type="button"
                  className="botao"
                  aria-disabled={desfazendo !== null || undefined}
                  aria-label={`Desfazer: ${a.variante} vira ${a.termo}`}
                  onClick={() => perguntar(a)}
                >
                  Desfazer
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {lista && lista.items.length === 0 && <p className="ajuda">Nada aprendido ainda.</p>}
      {/* O teto do servidor: sem alarde, uma linha. */}
      {lista && lista.has_more && <p className="ajuda">Só as mais recentes aparecem.</p>}
      {erro && (
        <p className="nota" role="alert">
          {erro}
        </p>
      )}
    </section>
  )
}
