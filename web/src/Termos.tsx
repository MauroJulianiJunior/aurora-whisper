import { type FormEvent, type ReactNode, useEffect, useEffectEvent, useRef, useState } from 'react'
import { type Dicas, ErroDaApi, type ListaDeTermos, type MudancaDeTermos, type TermoAdicionado, problemaDe } from './api'
import type { Cliente } from './clienteLocal'
import { Termo } from './Termo'

interface Props {
  cliente: Cliente
  // visivel: a tela das Configurações fica montada depois de aberta, e só
  // se esconde. A lista é lida ao aparecer.
  visivel: boolean
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Um termo entre aspas tipográficas, na mono: “RAG”.
const citado = (t: string) => (
  <>
    “<Termo>{t}</Termo>”
  </>
)

// grafiasDe separa o que a pessoa digitou em "Como sai errado": por vírgula,
// sem os pedaços vazios ("rague, , hage").
const grafiasDe = (texto: string) =>
  texto
    .split(',')
    .map((g) => g.trim())
    .filter(Boolean)

// A conta das dicas, para a ajuda da caixa. Uma dica nova precisa de ao
// menos um caractere, mais o ", " que a separa das outras.
function sobraNasDicas(d: Dicas): { sobram: number; cabe: boolean } {
  const sobram = Math.max(0, d.orcamento - d.usadas)
  return { sobram, cabe: sobram - (d.usadas > 0 ? 2 : 0) >= 1 }
}

// Sem espaço, os dois caminhos: desmarcar a caixa deste termo (o mais curto)
// ou desligar a dica de outro. Com espaço, sobram sempre 3 ou mais (a dica
// nova precisa do ", "), então não há singular.
function ajudaDaDica(d: Dicas): string {
  const { sobram, cabe } = sobraNasDicas(d)
  if (!cabe) return 'Não sobra espaço: desmarque aqui ou desligue a dica de outro termo.'
  return `Sobram ${sobram} de ${d.orcamento} caracteres para dicas.`
}

// lista junta com vírgula e "e" no fim, na mono: DCTFWeb, eSocial e DAS.
function emLista(termos: string[]): ReactNode {
  return termos.map((t, i) => (
    <span key={t}>
      {i > 0 && (i === termos.length - 1 ? ' e ' : ', ')}
      <Termo>{t}</Termo>
    </span>
  ))
}

type Acao = 'adicionar' | 'ligar' | 'desligar' | 'remover'

// O que a pílula diz depois de uma mudança que o Agent gravou. doDicionario:
// o termo já era do dicionário, e o Remover tirou só as grafias dela.
function resultado(m: MudancaDeTermos, acao: Acao, doDicionario = false): ReactNode {
  const quando = m.vale_ao_reiniciar ? ' Vale depois de reiniciar o Agent.' : acao === 'adicionar' ? ' Vale no próximo ditado.' : ''
  const sairam = m.sairam_das_dicas
  return (
    <>
      {acao === 'adicionar' && (
        <>
          {citado(m.termo)} {m.ja_existia ? 'atualizado' : 'adicionado'}.
        </>
      )}
      {acao === 'remover' && (doDicionario ? <>Grafias de {citado(m.termo)} removidas.</> : <>{citado(m.termo)} removido.</>)}
      {(acao === 'ligar' || acao === 'desligar') && (
        <>
          Dica {acao === 'ligar' ? 'ligada' : 'desligada'} para {citado(m.termo)}.
        </>
      )}
      {quando}
      {m.dica_ignorada && <> A dica não muda por aqui: {citado(m.termo)} já era do dicionário.</>}
      {sairam.length > 0 && (
        <>
          {' '}
          {sairam.length === 1 ? 'Saiu' : 'Saíram'} das dicas: {emLista(sairam)}.
        </>
      )}
    </>
  )
}

// A seção "Dicionário" das Configurações (ADR-020): a pessoa põe no
// dicionário a palavra que o reconhecimento de voz escreve errado, as grafias
// erradas que ele costuma escrever, e diz se a palavra vira dica para ele.
// Age na hora, como o Desfazer do "Aprendido no painel": fica fora do Salvar
// configurações.
//
// O menor que funciona: um formulário de uma linha (que quebra no celular) e,
// quando há algo, a lista "Adicionados por você", uma linha por termo, com a
// caixa da dica e o Remover.
export function Termos({ cliente, visivel, aoPerderLigacao, avisar }: Props) {
  const [lista, setLista] = useState<ListaDeTermos | null>(null)
  // indisponivel: a lista não carregou por um motivo do Agent (o dicionário
  // não abre, não existe...). A frase dele fica no lugar do formulário.
  const [indisponivel, setIndisponivel] = useState<string | null>(null)
  const [palavra, setPalavra] = useState('')
  const [grafias, setGrafias] = useState('')
  // A dica vem marcada (decisão 1 da ADR-020): o caso RAG funcionou porque
  // estava no prompt.
  const [dica, setDica] = useState(true)
  const [adicionando, setAdicionando] = useState(false)
  const [erroDoForm, setErroDoForm] = useState<string | null>(null)
  const [erroDaLista, setErroDaLista] = useState<string | null>(null)
  // mexendo: o termo cuja dica ou remoção está no ar. Um pedido por vez.
  const [mexendo, setMexendo] = useState<{ termo: string; acao: 'dica' | 'remover' } | null>(null)
  const campoPalavra = useRef<HTMLInputElement>(null)
  const listaRef = useRef<HTMLUListElement>(null)
  const focarDepois = useRef<number | null>(null)
  const perderLigacao = useEffectEvent(aoPerderLigacao)

  useEffect(() => {
    if (!visivel) return
    let vivo = true
    cliente.termos().then(
      (l) => {
        if (!vivo) return
        setLista(l)
        setIndisponivel(null)
      },
      (e: unknown) => {
        if (!vivo) return
        if (problemaDe(e)) perderLigacao(e)
        // O painel novo com um Agent de antes do "Adicionar termo" (recompilado
        // com o Agent aberto): a rota não existe, e o 404 dele não tem frase.
        else if (e instanceof ErroDaApi && e.status === 404) setIndisponivel('Este Agent ainda não tem o Adicionar termo. Reinicie o Agent.')
        else setIndisponivel(mensagem(e))
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, visivel])

  useEffect(() => {
    const i = focarDepois.current
    if (i === null) return
    focarDepois.current = null
    const botoes = listaRef.current?.querySelectorAll<HTMLElement>('button.remover')
    if (botoes && botoes.length > 0) botoes[Math.min(i, botoes.length - 1)]?.focus()
    else campoPalavra.current?.focus()
  }, [lista])

  async function adicionar(e: FormEvent) {
    e.preventDefault()
    if (adicionando) return
    if (palavra.trim() === '') {
      setErroDoForm('Não deu para adicionar: faltou a palavra certa.')
      campoPalavra.current?.focus()
      return
    }
    setAdicionando(true)
    setErroDoForm(null)
    try {
      const m = await cliente.adicionarTermo({ termo: palavra.trim(), variantes: grafiasDe(grafias), dica })
      setLista(m)
      setPalavra('')
      setGrafias('')
      // O formulário volta ao começo, com a dica marcada (decisão 1).
      setDica(true)
      avisar(resultado(m, 'adicionar'))
      campoPalavra.current?.focus()
    } catch (err) {
      if (problemaDe(err)) aoPerderLigacao(err)
      else setErroDoForm(`Não deu para adicionar: ${mensagem(err)}`)
    } finally {
      setAdicionando(false)
    }
  }

  async function mudarDica(t: TermoAdicionado, ligar: boolean) {
    if (mexendo !== null) return
    setMexendo({ termo: t.termo, acao: 'dica' })
    setErroDaLista(null)
    try {
      const m = await cliente.dicaDoTermo(t.termo, ligar)
      setLista(m)
      avisar(resultado(m, ligar ? 'ligar' : 'desligar'))
    } catch (err) {
      if (problemaDe(err)) aoPerderLigacao(err)
      else setErroDaLista(`Não deu para mudar a dica: ${mensagem(err)}`)
    } finally {
      setMexendo(null)
    }
  }

  async function remover(t: TermoAdicionado, i: number) {
    if (mexendo !== null) return
    setMexendo({ termo: t.termo, acao: 'remover' })
    setErroDaLista(null)
    try {
      const m = await cliente.removerTermo(t.termo)
      // O foco vai para o Remover da linha que ocupou o lugar, ou para a
      // palavra certa quando a lista some. Depois do render.
      focarDepois.current = i
      setLista(m)
      avisar(resultado(m, 'remover', t.do_dicionario))
    } catch (err) {
      if (problemaDe(err)) aoPerderLigacao(err)
      else setErroDaLista(`Não deu para remover: ${mensagem(err)}`)
    } finally {
      setMexendo(null)
    }
  }

  const itens = lista?.items ?? []
  return (
    <section className="secao" aria-labelledby="cfg-dicionario">
      <h2 id="cfg-dicionario" className="secao-titulo">
        Dicionário
      </h2>
      <p className="secao-intro">Palavras que o reconhecimento de voz escreve errado. Valem já no próximo ditado.</p>

      {indisponivel !== null ? (
        <p className="ajuda">{indisponivel}</p>
      ) : (
        <form className="termo-form" onSubmit={(e) => void adicionar(e)} noValidate>
          <div className="termo-campo">
            <label className="ajuste-nome" htmlFor="cfg-termo">
              Palavra certa
            </label>
            <input
              ref={campoPalavra}
              id="cfg-termo"
              className="entrada"
              type="text"
              autoComplete="off"
              spellCheck={false}
              maxLength={40}
              placeholder="RAG"
              value={palavra}
              onChange={(e) => setPalavra(e.target.value)}
            />
          </div>
          <div className="termo-campo">
            <label className="ajuste-nome" htmlFor="cfg-grafias">
              Como sai errado <span className="opcional">(opcional)</span>
            </label>
            <input
              id="cfg-grafias"
              className="entrada"
              type="text"
              autoComplete="off"
              spellCheck={false}
              placeholder="rague, hage"
              aria-describedby="cfg-grafias-ajuda"
              value={grafias}
              onChange={(e) => setGrafias(e.target.value)}
            />
            <p className="ajuda" id="cfg-grafias-ajuda">
              Separe por vírgula.
            </p>
          </div>
          <div className="termo-rodape">
            <div>
              <label className="marcar">
                <input
                  type="checkbox"
                  checked={dica}
                  aria-describedby={lista ? 'cfg-dica-ajuda' : undefined}
                  onChange={(e) => setDica(e.target.checked)}
                />
                Dica para o reconhecimento de voz
              </label>
              {lista && (
                <p className="ajuda" id="cfg-dica-ajuda">
                  {ajudaDaDica(lista.dicas)}
                </p>
              )}
            </div>
            <button type="submit" className="botao" aria-disabled={adicionando || undefined}>
              {adicionando ? 'Adicionando…' : 'Adicionar'}
            </button>
          </div>
          {erroDoForm && (
            <p className="nota" role="alert">
              {erroDoForm}
            </p>
          )}
        </form>
      )}

      {itens.length > 0 && (
        <>
          <h3 className="secao-sub">Adicionados por você</h3>
          <ul className="aprendidas termos" ref={listaRef}>
            {itens.map((t, i) => (
              <li key={t.termo}>
                <span className="troca">
                  <Termo>{t.termo}</Termo>
                  {t.variantes.length > 0 && (
                    <>
                      <span aria-hidden="true"> ←</span>
                      <span className="so-leitor">, sai errado como</span> <Termo>{t.variantes.join(', ')}</Termo>
                    </>
                  )}
                  {t.do_dicionario && (
                    <>
                      {' '}
                      <span className="etiqueta">do dicionário</span>
                    </>
                  )}
                </span>
                {!t.do_dicionario && (
                  <label className="marcar dica">
                    <input
                      type="checkbox"
                      checked={t.dica}
                      aria-label={`Dica para o reconhecimento de voz: ${t.termo}`}
                      aria-disabled={mexendo !== null || undefined}
                      onChange={(e) => void mudarDica(t, e.target.checked)}
                    />
                    <span aria-hidden="true">dica</span>
                  </label>
                )}
                <button
                  type="button"
                  className="botao remover"
                  aria-disabled={mexendo !== null || undefined}
                  aria-label={t.do_dicionario ? `Remover as grafias de ${t.termo}` : `Remover ${t.termo}`}
                  onClick={() => void remover(t, i)}
                >
                  {mexendo?.termo === t.termo && mexendo.acao === 'remover' ? 'Removendo…' : 'Remover'}
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {erroDaLista && (
        <p className="nota" role="alert">
          {erroDaLista}
        </p>
      )}
    </section>
  )
}
