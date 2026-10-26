import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react'
import { Conflito, problemaDe } from '../api'
import { Aurora } from '../Aurora'
import { type AcoesDaNota, CartaoDaNota, LinhaDaNota, TabelaDeNotas } from './CartaoDaNota'
import { CriarNota } from './CriarNota'
import { DialogoDaNota, type FimDaNota } from './DialogoDaNota'
import { Icone } from './Icone'
import { NotaNaLixeira } from './NotaNaLixeira'
import { type FormaDeVer, FORMAS, gravarColunas, gravarForma, lerColunas, lerForma, limitar, limitesDeColunas } from './preferencias'
import type { ClienteDeNotas, CorDaNota, FiltroDasNotas, Marcador, Nota, VistaDasNotas } from './tipos'
import { useTelaEstreita } from './useTelaEstreita'

// Tempo mínimo entre duas leituras causadas por voltar à aba: "focus" e
// "visibilitychange" chegam juntos (o mesmo cuidado do histórico).
const intervaloDaRecarga = 2000

interface Props {
  cliente: ClienteDeNotas
  // busca: o texto do campo de busca do Painel (já com a espera).
  busca: string
  aoPerderLigacao(e: unknown): void
  // avisar: a pílula do Painel; desfazer, quando existe, vira o botão e o Ctrl+Z.
  avisar(texto: ReactNode, desfazer?: () => void): void
}

// O que a tela mostra: a lista da consulta, e a lista das notas ativas inteira
// (os números dos marcadores) com os marcadores.
interface Resultado {
  // consulta: vista, marcador e busca (a recarga da mesma consulta não é
  // "carregando": a lista que está na tela fica até a nova chegar).
  consulta: string
  itens: Nota[]
  base: Nota[]
  marcadores: Marcador[]
  erro?: string
}

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

const nomesDasFormas: Record<FormaDeVer, string> = { grade: 'Quadradinhos', lista: 'Lista', detalhes: 'Detalhes' }

// ordenar põe as fixadas primeiro; a ordem de cada grupo é a da API (a última
// edição primeiro).
const ordenar = (itens: Nota[]) => [...itens.filter((n) => n.fixada), ...itens.filter((n) => !n.fixada)]

function vizinhoDe(ordenados: Nota[], id: string): string | null {
  const i = ordenados.findIndex((n) => n.id === id)
  if (i < 0) return null
  return (ordenados[i + 1] ?? ordenados[i - 1])?.id ?? null
}

// Notas é a aba Notas do painel local (ADR-022, "A tela"): o campo de criar, a
// fileira de marcadores com as formas de ver, a grade (ou a lista, ou os
// detalhes), a nota aberta numa janela, o Arquivo e a Lixeira. Só o PC o tem
// (ambienteLocal.tsx): o painel da nuvem não importa este arquivo.
export function Notas({ cliente, busca, aoPerderLigacao: aoPerderLigacaoDoPainel, avisar }: Props) {
  // O Painel passa uma função nova a cada render: a daqui é estável, e lê a
  // do momento (senão cada render do Painel leria a lista de novo).
  const ligacao = useRef(aoPerderLigacaoDoPainel)
  useEffect(() => {
    ligacao.current = aoPerderLigacaoDoPainel
  })
  const aoPerderLigacao = useCallback((e: unknown) => ligacao.current(e), [])
  const [vista, setVista] = useState<VistaDasNotas>('ativas')
  const [marcador, setMarcador] = useState<string | null>(null)
  const [resultado, setResultado] = useState<Resultado | null>(null)
  const [recarga, setRecarga] = useState(0)
  const [aberta, setAberta] = useState<Nota | null>(null)
  const [forma, setForma] = useState<FormaDeVer>(lerForma)
  const estreita = useTelaEstreita()
  const [colunasNoPc, setColunasNoPc] = useState(() => lerColunas(false))
  const [colunasNoCelular, setColunasNoCelular] = useState(() => lerColunas(true))
  const colunas = estreita ? colunasNoCelular : colunasNoPc
  const limites = limitesDeColunas(estreita)
  const raiz = useRef<HTMLDivElement>(null)
  const ultimaCarga = useRef(0)
  // focarDepois: a nota que recebe o foco quando a janela fecha ou um cartão
  // sai da tela (null: não há nota, e o foco vai para o "Criar uma nota").
  const focarDepois = useRef<{ id: string | null } | null>(null)

  const consulta = `${vista}\u0000${marcador ?? ''}\u0000${busca}`
  const recarregar = useCallback(() => setRecarga((n) => n + 1), [])

  useEffect(() => {
    const ctrl = new AbortController()
    ultimaCarga.current = Date.now()
    const filtro: FiltroDasNotas = { vista }
    if (marcador) filtro.marcador = marcador
    if (busca) filtro.busca = busca
    const padrao = vista === 'ativas' && !marcador && !busca
    Promise.all([
      cliente.listarNotas(filtro, ctrl.signal),
      padrao ? null : cliente.listarNotas({ vista: 'ativas' }, ctrl.signal),
      cliente.listarMarcadores(ctrl.signal),
    ]).then(
      ([itens, base, marcadores]) => {
        setResultado({ consulta, itens, base: base ?? itens, marcadores })
        // O marcador que deixou de existir (apagado em outra aba) sai do filtro.
        setMarcador((m) => (m && !marcadores.some((x) => x.id === m) ? null : m))
      },
      (e: unknown) => {
        if (ctrl.signal.aborted) return
        if (problemaDe(e)) aoPerderLigacao(e)
        // Com a lista da mesma consulta na tela, a falha de uma recarga não a apaga.
        setResultado((r) => (r?.consulta === consulta ? r : { consulta, itens: [], base: [], marcadores: [], erro: mensagemDe(e) }))
      },
    )
    return () => ctrl.abort()
  }, [cliente, vista, marcador, busca, consulta, recarga, aoPerderLigacao])

  // Voltar para a aba lê de novo: a nota pode ter mudado em outra aba.
  useEffect(() => {
    function voltou() {
      if (document.visibilityState === 'hidden') return
      if (Date.now() - ultimaCarga.current < intervaloDaRecarga) return
      ultimaCarga.current = Date.now()
      recarregar()
    }
    window.addEventListener('focus', voltou)
    document.addEventListener('visibilitychange', voltou)
    return () => {
      window.removeEventListener('focus', voltou)
      document.removeEventListener('visibilitychange', voltou)
    }
  }, [recarregar])

  // Depois que a janela fecha, ou um cartão sai: o foco volta ao cartão
  // da nota (ou ao vizinho, ou ao "Criar uma nota"). Só se o foco estava
  // perdido; quem já está digitando em outro lugar fica.
  useEffect(() => {
    const alvo = focarDepois.current
    if (!alvo || aberta) return
    focarDepois.current = null
    const ativo = document.activeElement
    const livre = !ativo || ativo === document.body || !ativo.isConnected || Boolean(ativo.closest('.veu'))
    if (!livre && !raiz.current?.contains(ativo)) return
    const abrir = alvo.id ? Array.from(raiz.current?.querySelectorAll<HTMLElement>('[data-nota]') ?? []).find((el) => el.dataset.nota === alvo.id) : null
    const destino =
      abrir?.querySelector<HTMLElement>('[data-abrir]') ??
      (abrir?.hasAttribute('data-abrir') ? abrir : null) ??
      raiz.current?.querySelector<HTMLElement>('[data-abrir]') ??
      raiz.current?.querySelector<HTMLElement>('.criar-abrir')
    destino?.focus()
  })

  const falhar = useCallback(
    (e: unknown) => {
      if (problemaDe(e)) aoPerderLigacao(e)
      else avisar(`Não deu certo: ${mensagemDe(e)}`)
    },
    [aoPerderLigacao, avisar],
  )

  const mudarItens = (f: (itens: Nota[]) => Nota[]) => setResultado((r) => r && { ...r, itens: f(r.itens) })
  const trocar = (nova: Nota) => mudarItens((itens) => itens.map((n) => (n.id === nova.id ? nova : n)))
  const tirar = (id: string) => mudarItens((itens) => itens.filter((n) => n.id !== id))

  const visiveis = resultado?.consulta === consulta ? resultado : null
  const ordenados = visiveis ? ordenar(visiveis.itens) : []
  const nomesDosMarcadores = new Map((resultado?.marcadores ?? []).map((m) => [m.id, m.nome]))

  // O que se faz com uma nota que muda de lista (arquivar, desarquivar, ir para
  // a lixeira): sai da tela, o foco vai ao vizinho e a pílula oferece o Desfazer.
  function sairDaLista(id: string) {
    focarDepois.current = { id: vizinhoDe(ordenados, id) }
    tirar(id)
  }

  // editar muda a nota pelo PATCH com a versão que o cartão tem. O 409 quer
  // dizer que ela mudou em outra aba: a lista é lida de novo e a pessoa repete.
  async function editar(nota: Nota, mudanca: { cor?: CorDaNota | null; fixada?: boolean; arquivada?: boolean }): Promise<Nota | null> {
    try {
      return await cliente.editarNota(nota.id, nota.version, mudanca)
    } catch (e) {
      if (e instanceof Conflito) {
        avisar('Esta nota mudou em outra aba. A lista foi lida de novo; repita se ainda quiser.')
        recarregar()
        return null
      }
      falhar(e)
      return null
    }
  }

  async function arquivar(nota: Nota) {
    const volta = !nota.arquivada
    const nova = await editar(nota, { arquivada: !nota.arquivada })
    if (!nova) return
    sairDaLista(nota.id)
    avisar(volta ? 'Nota arquivada.' : 'Nota desarquivada.', () => void desfazerArquivo(nova, nota.arquivada))
    recarregar()
  }

  // desfazerArquivo devolve a nota ao estado de antes, na versão em que ela está agora.
  async function desfazerArquivo(nota: Nota, arquivada: boolean) {
    try {
      await cliente.editarNota(nota.id, nota.version, { arquivada })
    } catch (e) {
      if (e instanceof Conflito) avisar('Esta nota mudou em outra aba; não consegui desfazer.')
      else falhar(e)
    }
    recarregar()
  }

  async function paraALixeira(nota: Nota) {
    try {
      await cliente.moverNotaParaLixeira(nota.id)
    } catch (e) {
      return falhar(e)
    }
    sairDaLista(nota.id)
    avisar('Nota movida para a lixeira.', () => void desfazerLixeira(nota))
    recarregar()
  }

  async function desfazerLixeira(nota: Nota) {
    try {
      await cliente.restaurarNota(nota.id)
      avisar('Nota de volta.')
    } catch (e) {
      falhar(e)
    }
    recarregar()
  }

  async function restaurar(nota: Nota) {
    try {
      await cliente.restaurarNota(nota.id)
    } catch (e) {
      return falhar(e)
    }
    sairDaLista(nota.id)
    avisar('Nota restaurada.')
    recarregar()
  }

  async function excluirDeVez(nota: Nota): Promise<boolean> {
    try {
      await cliente.apagarNotaDeVez(nota.id)
    } catch (e) {
      falhar(e)
      return false
    }
    sairDaLista(nota.id)
    avisar('Nota excluída para sempre.')
    recarregar()
    return true
  }

  // copiar escreve na área de transferência dentro do clique de quem pediu.
  function copiar(nota: Nota) {
    const texto = [nota.titulo.trim(), nota.corpo].filter(Boolean).join('\n\n')
    if (texto === '') return
    const area = navigator.clipboard
    if (!area) return avisar('Não consegui copiar. Abra a nota e use Ctrl+C.')
    area.writeText(texto).then(
      () => avisar('Copiado.'),
      () => avisar('Não consegui copiar. Abra a nota e use Ctrl+C.'),
    )
  }

  const acoes: AcoesDaNota = {
    fixar: (nota) => {
      void editar(nota, { fixada: !nota.fixada }).then((nova) => nova && trocar(nova))
    },
    cor: (nota, cor) => {
      void editar(nota, { cor }).then((nova) => nova && trocar(nova))
    },
    arquivar: (nota) => void arquivar(nota),
    copiar,
    lixeira: (nota) => void paraALixeira(nota),
  }

  function terminou(fim: FimDaNota) {
    setAberta(null)
    const { nota, tipo } = fim
    if (tipo === 'fechou') {
      focarDepois.current = { id: nota.id }
      trocar(nota)
    } else if (tipo === 'lixeira') {
      sairDaLista(nota.id)
      avisar('Nota movida para a lixeira.', () => void desfazerLixeira(nota))
    } else {
      sairDaLista(nota.id)
      avisar(tipo === 'arquivou' ? 'Nota arquivada.' : 'Nota desarquivada.', () => void desfazerArquivo(nota, tipo === 'desarquivou'))
    }
    recarregar()
  }

  function escolherForma(f: FormaDeVer) {
    setForma(f)
    gravarForma(f)
  }

  function mudarColunas(n: number) {
    const novo = limitar(n, limites)
    if (estreita) setColunasNoCelular(novo)
    else setColunasNoPc(novo)
    gravarColunas(estreita, novo)
  }

  function escolherVista(v: VistaDasNotas, m: string | null) {
    setVista(v)
    setMarcador(m)
  }

  const noArquivo = vista === 'arquivo'
  const base = resultado?.base ?? []
  const nomeDaVista = vista === 'lixeira' ? 'a lixeira das notas' : noArquivo ? 'o arquivo' : 'as notas'

  let conteudo: ReactNode
  if (!visiveis) {
    conteudo = <p className="estado atrasado">Carregando…</p>
  } else if (visiveis.erro) {
    conteudo = (
      <div className="estado">
        <p>
          Não consegui carregar {nomeDaVista}: {visiveis.erro}
        </p>
        <button type="button" className="botao" onClick={recarregar}>
          Tentar de novo
        </button>
      </div>
    )
  } else if (ordenados.length === 0) {
    conteudo = (
      <div className="estado">
        {busca ? (
          <p>Nada encontrado para “{busca}”.</p>
        ) : vista === 'lixeira' ? (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">A lixeira das notas está vazia.</p>
            <p>A nota apagada fica aqui por 30 dias.</p>
          </>
        ) : noArquivo ? (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">O arquivo está vazio.</p>
            <p>As notas que você arquiva saem da tela principal e ficam aqui.</p>
          </>
        ) : marcador ? (
          <p>Nenhuma nota com este marcador.</p>
        ) : (
          <>
            <Aurora tamanho={96} />
            <p className="estado-titulo">Nenhuma nota ainda.</p>
            <p>Escreva a primeira em “Criar uma nota…”.</p>
          </>
        )}
      </div>
    )
  } else if (vista === 'lixeira') {
    conteudo = (
      <ul className="itens">
        {ordenados.map((n) => (
          <NotaNaLixeira key={n.id} nota={n} aoRestaurar={() => void restaurar(n)} aoExcluirDeVez={() => excluirDeVez(n)} />
        ))}
      </ul>
    )
  } else {
    const fixadas = ordenados.filter((n) => n.fixada)
    const outras = ordenados.filter((n) => !n.fixada)
    const abrir = (n: Nota) => setAberta(n)
    const desenhar = (itens: Nota[]) =>
      forma === 'detalhes' ? null : forma === 'lista' ? (
        <ul className="notas-lista">
          {itens.map((n) => (
            <LinhaDaNota key={n.id} nota={n} nomesDosMarcadores={nomesDosMarcadores} noArquivo={noArquivo} aoAbrir={abrir} acoes={acoes} />
          ))}
        </ul>
      ) : (
        <div className="grade" data-colunas={colunas}>
          {itens.map((n) => (
            <CartaoDaNota key={n.id} nota={n} nomesDosMarcadores={nomesDosMarcadores} noArquivo={noArquivo} aoAbrir={abrir} acoes={acoes} />
          ))}
        </div>
      )
    if (forma === 'detalhes') {
      conteudo = <TabelaDeNotas notas={ordenados} nomesDosMarcadores={nomesDosMarcadores} aoAbrir={abrir} />
    } else if (fixadas.length > 0) {
      conteudo = (
        <>
          <section aria-labelledby="notas-fixadas">
            <h2 id="notas-fixadas" className="secao-notas">
              Fixadas
            </h2>
            {desenhar(fixadas)}
          </section>
          {outras.length > 0 && (
            <section aria-labelledby="notas-outras">
              <h2 id="notas-outras" className="secao-notas">
                Outras
              </h2>
              {desenhar(outras)}
            </section>
          )}
        </>
      )
    } else {
      conteudo = desenhar(ordenados)
    }
  }

  const total = visiveis && !visiveis.erro ? ordenados.length : null
  const anuncio = total === null ? '' : total === 1 ? '1 nota.' : `${total} notas.`

  return (
    <div className="notas" ref={raiz}>
      <p className="so-leitor anuncio" role="status">
        {anuncio}
      </p>

      {vista === 'ativas' && (
        <CriarNota
          cliente={cliente}
          aoCriar={(nota) => {
            mudarItens((itens) => (marcador || busca ? itens : [nota, ...itens]))
            if (marcador || busca) avisar('Nota criada.')
            recarregar()
          }}
          aoPerderLigacao={aoPerderLigacao}
        />
      )}

      <div className="barra-de-notas">
        <div className="filtros" role="group" aria-label="Marcadores">
          <button type="button" className="filtro" aria-pressed={vista === 'ativas' && marcador === null} onClick={() => escolherVista('ativas', null)}>
            Notas{' '}
            {resultado && <span className="filtro-n">{base.length}</span>}
          </button>
          {(resultado?.marcadores ?? []).map((m) => (
            <button key={m.id} type="button" className="filtro" aria-pressed={vista === 'ativas' && marcador === m.id} onClick={() => escolherVista('ativas', m.id)}>
              {m.nome}{' '}
              <span className="filtro-n">{base.filter((n) => n.marcadores.includes(m.id)).length}</span>
            </button>
          ))}
          <button type="button" className="filtro separa" aria-pressed={noArquivo} onClick={() => escolherVista('arquivo', null)}>
            Arquivo
          </button>
          <button type="button" className="filtro" aria-pressed={vista === 'lixeira'} onClick={() => escolherVista('lixeira', null)}>
            Lixeira
          </button>
        </div>

        {vista !== 'lixeira' && (
          <div className="formas">
            {forma === 'grade' && (
              <div className="colunas" role="group" aria-label="Colunas">
                <button type="button" className="icone-botao" aria-label="Menos colunas" title="Menos colunas" disabled={colunas <= limites.min} onClick={() => mudarColunas(colunas - 1)}>
                  −
                </button>
                <span className="colunas-n" aria-hidden="true">
                  {colunas}
                </span>
                <button type="button" className="icone-botao" aria-label="Mais colunas" title="Mais colunas" disabled={colunas >= limites.max} onClick={() => mudarColunas(colunas + 1)}>
                  +
                </button>
              </div>
            )}
            <div className="forma-de-ver" role="group" aria-label="Forma de ver">
              {FORMAS.map((f) => (
                <button key={f} type="button" className="icone-botao" aria-pressed={forma === f} aria-label={nomesDasFormas[f]} title={nomesDasFormas[f]} onClick={() => escolherForma(f)}>
                  <Icone nome={f} />
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {conteudo}

      {aberta && (
        <DialogoDaNota
          key={aberta.id}
          nota={aberta}
          cliente={cliente}
          nomesDosMarcadores={nomesDosMarcadores}
          avisar={avisar}
          aoPerderLigacao={aoPerderLigacao}
          aoTerminar={terminou}
        />
      )}
    </div>
  )
}
