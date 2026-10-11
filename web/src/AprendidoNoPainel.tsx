import { type ReactNode, useEffect, useEffectEvent, useRef, useState } from 'react'
import { type Aprendida, problemaDe } from './api'
import type { Cliente } from './clienteLocal'
import { linhaDoAprendido, resultadoDeDesfazer } from './aprendizado'
import { Termo } from './Termo'

interface Props {
  cliente: Cliente
  // visivel: a tela das Configurações fica montada depois de aberta, e só
  // se esconde. A lista é lida ao aparecer, para mostrar o que o aviso do
  // histórico aprendeu ou desfez enquanto ela estava escondida.
  visivel: boolean
  // versao sobe a cada aprendizado ou desfazer feito pelo aviso do
  // histórico (Painel): a lista lê de novo.
  versao: number
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
  // aoEsvaziar: a última linha saiu, a seção some com ela, e o foco
  // precisa ir para algum lugar da página.
  aoEsvaziar(): void
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

// A troca como a linha a mostra: sf → ECF, e o leitor de tela ouve “sf vira
// ECF”. A seta é só para os olhos.
function Troca({ a }: { a: Aprendida }) {
  return (
    <>
      <Termo>{a.variante}</Termo>
      <span aria-hidden="true"> →</span>
      <span className="so-leitor"> vira</span> <Termo>{a.termo}</Termo>
    </>
  )
}

// A seção "Aprendido no painel" (ADR-017): o que a pessoa confirmou ao
// corrigir um ditado, uma linha por par, cada uma com o seu Desfazer. Sem
// nada aprendido a seção nem aparece (o menor que funciona), e uma lista que
// não carregou também não: ela só confirma o que o Agent disse.
//
// Com a nuvem (ADR-019, F8), o Agent manda também as linhas de estado (06,
// GET /api/aprendizado): a troca que chegou de outro PC e não vale neste, e a
// deste PC que perdeu na nuvem. Elas vêm com uma etiqueta cinza e sem
// Desfazer (não estão no dicionário daqui: para mudar, é aprender de novo), e
// a seção aparece também quando só há linhas assim. Um Agent de antes da F8
// não manda `situacao`, e nada muda.
//
// É a rede de segurança do aprendizado: o aviso do histórico some, e uma
// variante que causa correção falsa tem de ter onde ser desfeita.
export function AprendidoNoPainel({ cliente, visivel, versao, aoPerderLigacao, avisar, aoEsvaziar }: Props) {
  const [itens, setItens] = useState<Aprendida[]>([])
  const [erro, setErro] = useState<string | null>(null)
  // desfazendo: a variante que está no ar. Um clique por vez.
  const [desfazendo, setDesfazendo] = useState<string | null>(null)
  const lista = useRef<HTMLUListElement>(null)
  const titulo = useRef<HTMLHeadingElement>(null)
  // focarDepois: para onde o foco vai no próximo render, depois que uma linha
  // sai: o índice do botão (os botões são só os das linhas que valem, e as
  // linhas de estado não têm), ou o título da seção, quando só restam linhas
  // de estado.
  const focarDepois = useRef<number | 'titulo' | null>(null)
  // O pai passa uma função nova a cada render: dentro do efeito, ela não
  // pode ser dependência, ou a lista seria lida de novo a cada render do
  // Painel.
  const perderLigacao = useEffectEvent(aoPerderLigacao)

  // Só lê com a tela à vista: uma tela escondida não precisa da lista, e
  // ao aparecer lê de novo. A resposta de uma leitura que já não vale (a
  // tela escondeu, a versão subiu) é descartada.
  useEffect(() => {
    if (!visivel) return
    let vivo = true
    cliente.aprendidas().then(
      (a) => {
        if (vivo) setItens(a)
      },
      // Lista que não carregou: a seção fica como estava. A ligação perdida
      // é da faixa do Painel.
      (e: unknown) => {
        if (vivo && problemaDe(e)) perderLigacao(e)
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, visivel, versao])

  // As linhas à vista, com o que cada uma é. A de situação desconhecida não
  // entra: não se sabe o que dizer dela, e ela não tem Desfazer a oferecer.
  const linhas = itens.flatMap((a) => {
    const l = linhaDoAprendido(a)
    return l.tipo === 'desconhecida' ? [] : [{ a, l }]
  })
  const valem = linhas.filter((x) => x.l.tipo === 'vale').map((x) => x.a)

  async function desfazer(a: Aprendida) {
    if (desfazendo !== null) return
    setDesfazendo(a.variante)
    setErro(null)
    try {
      const feita = await cliente.desfazerAprendizado(a.variante)
      // Só a linha que vale: a linha de estado de uma troca trocada em outro
      // aparelho tem a mesma variante e fica.
      const mesma = (x: Aprendida) => !x.situacao && x.variante === a.variante
      const i = valem.findIndex(mesma)
      const restantes = itens.filter((x) => !mesma(x))
      setItens(restantes)
      avisar(resultadoDeDesfazer([feita]))
      // O foco vai para o botão que ocupa o lugar do que saiu, ou para o
      // anterior se era o último. Sem botão nenhum: a seção continua, se
      // restam linhas de estado (o foco vai para o título dela), ou some, e o
      // pai decide. Depois do render: a linha que saiu ainda está no DOM agora.
      if (valem.length > 1) focarDepois.current = Math.min(i, valem.length - 2)
      else if (restantes.some((x) => linhaDoAprendido(x).tipo === 'estado')) focarDepois.current = 'titulo'
      else aoEsvaziar()
    } catch (e) {
      if (problemaDe(e)) aoPerderLigacao(e)
      else setErro(`Não deu para desfazer: ${mensagem(e)}`)
    } finally {
      setDesfazendo(null)
    }
  }

  useEffect(() => {
    const alvo = focarDepois.current
    if (alvo === null) return
    focarDepois.current = null
    if (alvo === 'titulo') titulo.current?.focus()
    else lista.current?.querySelectorAll<HTMLElement>('button')[alvo]?.focus()
  }, [itens])

  if (linhas.length === 0) return null
  return (
    <section className="secao" aria-labelledby="cfg-aprendido">
      <h2 id="cfg-aprendido" className="secao-titulo" ref={titulo} tabIndex={-1}>
        Aprendido no painel
      </h2>
      <p className="secao-intro">Trocas que você confirmou ao corrigir um ditado. Valem para todo ditado, em qualquer aplicativo.</p>
      <ul className="aprendidas" ref={lista}>
        {linhas.map(({ a, l }) =>
          l.tipo === 'estado' ? (
            <li key={`${a.situacao}:${a.variante}:${a.termo}`}>
              <span className="troca">
                <Troca a={a} /> <span className="etiqueta">{l.etiqueta}</span>
                {l.motivo && (
                  <>
                    {' '}
                    <span className="motivo">{l.motivo}</span>
                  </>
                )}
              </span>
            </li>
          ) : (
            <li key={a.variante}>
              <span className="troca">
                <Troca a={a} />
              </span>
              <button
                type="button"
                className="botao"
                aria-disabled={desfazendo !== null || undefined}
                aria-label={`Desfazer: ${a.variante} vira ${a.termo}`}
                onClick={() => void desfazer(a)}
              >
                {desfazendo === a.variante ? 'Desfazendo…' : 'Desfazer'}
              </button>
            </li>
          ),
        )}
      </ul>
      {erro && (
        <p className="nota" role="alert">
          {erro}
        </p>
      )}
    </section>
  )
}
