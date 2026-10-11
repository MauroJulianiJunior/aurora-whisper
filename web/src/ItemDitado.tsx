import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react'
import type { Ditado, EscolhaDoSelo } from './api'
import { formatarCompleta, formatarCurta, formatarHora, formatarSegundos } from './datas'
import { compararPalavras } from './diff'
import { type Rascunho, novoRascunho, semTextoFinal, textoInicial } from './edicao'

interface Props {
  item: Ditado
  aberto: boolean
  // rascunho: a edição em andamento desta linha, se houver — sobrevive a
  // abrir outra linha, e carrega a versão em que começou (edicao.ts).
  rascunho?: Rascunho
  aoAbrir(): void
  aoFechar(): void
  aoMudar(texto: string): void
  aoSalvar(): Promise<void>
  aoCopiar(texto: string): void
  aoMoverParaLixeira(): void
  // outroLugar: de onde veio a mudança que esta tela não fez ("em outra
  // aba", no PC; Ambiente.textos).
  outroLugar: string
  // edicaoSemConexao: a nota do editor quando o salvar não chegou a lugar
  // nenhum (Ambiente.textos; só a nuvem tem).
  edicaoSemConexao?: string
  // aoEscolher: a escolha do selo. Sem ele, o selo aparece sem os botões.
  aoEscolher?(escolha: EscolhaDoSelo): Promise<void>
  // novo: o ditado que o celular acabou de gravar (ADR-021). Fica em destaque
  // no topo, com a etiqueta "novo" (o fundo sozinho não diz nada a quem não vê
  // a cor).
  novo?: boolean
}

// TextoDaLinha é o texto que a linha mostra. Ditado que falhou antes de
// o texto final existir mostra o bruto do ASR, discreto, em vez de "sem
// texto": é o texto que existe, e é o que a pessoa foi buscar (revisão
// de 29-set-2026). O "bruto do ASR:" para leitor de tela vem do CSS, para
// não entrar num texto selecionado e copiado.
export function TextoDaLinha({ item, id }: { item: Ditado; id?: string }) {
  if (item.final_text) {
    return (
      <span className="texto" id={id}>
        {item.final_text}
      </span>
    )
  }
  if (semTextoFinal(item)) {
    return (
      <span className="texto do-asr" id={id}>
        {item.raw_text}
      </span>
    )
  }
  return (
    <span className="texto" id={id}>
      <span className="vazio">sem texto</span>
    </span>
  )
}

// peloApp diz se o ditado nasceu na nuvem, pelo app do celular (ADR-021):
// source_platform "web", ou "android" no app nativo. O do PC é "windows".
function peloApp(item: Ditado): boolean {
  return item.source_platform === 'web' || item.source_platform === 'android'
}

export function ItemDitado(props: Props) {
  const { item, aberto, rascunho } = props
  const botao = useRef<HTMLButtonElement>(null)
  const estavaAberto = useRef(aberto)

  // Ao fechar o editor, o foco volta para a linha — quem usa o teclado
  // continua de onde estava, em vez de recomeçar do topo da página. Só se
  // o foco tiver se perdido: se ele foi para o editor de OUTRA linha, que
  // acabou de abrir, fica lá.
  useEffect(() => {
    const perdido = document.activeElement === null || document.activeElement === document.body
    if (estavaAberto.current && !aberto && perdido) botao.current?.focus()
    estavaAberto.current = aberto
  }, [aberto])

  if (aberto) return <Editor {...props} rascunho={rascunho ?? novoRascunho(item)} />

  // "editado" saiu (revisão de 29-set-2026): vinha de version > 1, e a
  // versão sobe também ao ir para a lixeira e voltar — a etiqueta
  // afirmava uma edição que não houve.
  const pendente = rascunho !== undefined && rascunho.texto !== rascunho.textoBase
  const falhou = item.status === 'error'
  const naLixeira = Boolean(item.deleted_at)
  // O selo (ADR-019, "Conflito"): o ditado mudou em dois lugares, e a versão
  // que perdeu está guardada nele. Discreto, sem o vermelho de erro: nada se
  // perdeu. Escolher é com a linha aberta.
  const selo = Boolean(item.versao_em_conflito)
  // Copiar direto da linha, sem abrir o editor (pedido de Mauro, 29-set-
  // 2026): um clique leva o texto que a linha MOSTRA — o final, ou o bruto
  // quando o ditado falhou antes de o final existir. Linha sem texto não
  // ganha o botão: copiar vazio apagaria o que está na área de
  // transferência.
  const paraCopiar = textoInicial(item)
  const copiavel = paraCopiar.trim() !== ''

  const novo = props.novo === true
  return (
    <li className={[copiavel ? 'item com-copia' : 'item', novo ? 'chegou' : ''].filter(Boolean).join(' ')} data-item={item.id}>
      <button ref={botao} type="button" className="linha" data-linha aria-expanded="false" onClick={props.aoAbrir}>
        <time className="hora" dateTime={item.created_at}>
          {formatarHora(item.created_at)}
        </time>{' '}
        {/* Os espaços entre as partes são para o nome acessível do botão:
            sem eles, o leitor de tela junta "14:32" e a primeira palavra. */}
        <TextoDaLinha item={item} />{' '}
        {(novo || pendente || falhou || naLixeira || selo) && (
          <span className="etiquetas">
            {novo && <span className="etiqueta">novo</span>}{' '}
            {pendente && <span className="etiqueta forte">não salvo</span>}{' '}
            {falhou && <span className="etiqueta risco">falhou</span>}{' '}
            {naLixeira && <span className="etiqueta">na lixeira</span>}{' '}
            {selo && (
              <span className="etiqueta">
                2 versões<span className="so-leitor">, abra para escolher</span>
              </span>
            )}
          </span>
        )}
      </button>
      {copiavel && (
        // Irmão da linha, não filho: botão dentro de botão não existe em
        // HTML, e o clique aqui não pode abrir o editor.
        <button
          type="button"
          className="copiar-linha"
          title="Copiar"
          aria-label={`Copiar o ditado das ${formatarHora(item.created_at)}`}
          onClick={() => props.aoCopiar(paraCopiar)}
        >
          <IconeCopiar />
        </button>
      )}
    </li>
  )
}

function IconeCopiar() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="5.75" y="5.75" width="8" height="8" rx="1.5" />
      <path d="M10.25 5.75V3.75a1.5 1.5 0 0 0-1.5-1.5h-5a1.5 1.5 0 0 0-1.5 1.5v5a1.5 1.5 0 0 0 1.5 1.5h2" />
    </svg>
  )
}

function Editor({
  item,
  rascunho,
  aoFechar,
  aoMudar,
  aoSalvar,
  aoCopiar,
  aoMoverParaLixeira,
  outroLugar,
  edicaoSemConexao,
  aoEscolher,
}: Props & { rascunho: Rascunho }) {
  const campo = useRef<HTMLTextAreaElement>(null)
  const [salvando, setSalvando] = useState(false)

  // Escolhida a versão, o selo sai da tela com o botão que tinha o foco: o
  // foco vai para o texto, que é o que ficou.
  const tinhaSelo = useRef(Boolean(item.versao_em_conflito))
  useEffect(() => {
    const tem = Boolean(item.versao_em_conflito)
    const perdido = document.activeElement === null || document.activeElement === document.body
    if (tinhaSelo.current && !tem && perdido) campo.current?.focus()
    tinhaSelo.current = tem
  }, [item.versao_em_conflito])
  const texto = rascunho.texto
  // mudou compara com o texto em que a edição se baseia, não com o da
  // lista: depois de um conflito, a base é a versão da outra aba.
  const mudou = texto !== rascunho.textoBase
  // Na lixeira (movido por outra aba), salvar gravaria num ditado que
  // não está mais no histórico. Fica o texto, para copiar.
  const naLixeira = Boolean(item.deleted_at)
  const trechos = useMemo(() => compararPalavras(item.raw_text, texto), [item.raw_text, texto])
  // "igual" só quando é igual de verdade: o diff é por palavras e não vê
  // uma quebra de linha acrescentada (revisão de 29-set-2026).
  const identico = item.raw_text === texto
  const vazio = texto.trim() === ''

  // Com o selo, o foco vai para o texto sem rolar até ele: a página rola até
  // o começo do cartão, para o selo (em cima do texto) não ficar escondido
  // sob o cabeçalho, no celular. scrollIntoView respeita o scroll-padding.
  const cartao = useRef<HTMLDivElement>(null)
  const seloAoAbrir = useRef(Boolean(item.versao_em_conflito))
  useEffect(() => {
    const el = campo.current
    if (!el) return
    el.focus({ preventScroll: seloAoAbrir.current })
    el.setSelectionRange(el.value.length, el.value.length)
    if (seloAoAbrir.current) cartao.current?.scrollIntoView?.({ block: 'start' })
  }, [])

  async function salvar() {
    if (!mudou || salvando || naLixeira) return
    setSalvando(true)
    try {
      await aoSalvar()
    } finally {
      setSalvando(false)
    }
  }

  function teclas(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      void salvar()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      aoFechar()
    }
  }

  const meta = [
    formatarCompleta(item.created_at),
    // A origem só é dita quando não é o comum: o ditado que o celular criou na
    // nuvem diz "pelo app" (ADR-021); o do PC não diz nada.
    peloApp(item) && 'pelo app',
    item.asr_model ? `${item.asr_provider} · ${item.asr_model}` : item.asr_provider,
    item.duration_ms != null && `${formatarSegundos(item.duration_ms)} de áudio`,
    item.processing_ms != null && `transcrito em ${formatarSegundos(item.processing_ms)}`,
    item.target_app,
  ].filter(Boolean)

  const idCampo = `final-${item.id}`
  const idBruto = `bruto-${item.id}`

  return (
    <li className="item aberto" data-item={item.id}>
      <div className="editor" ref={cartao}>
        <time className="hora" dateTime={item.created_at}>
          {formatarHora(item.created_at)}
        </time>
        <div className="corpo">
          {item.versao_em_conflito && <Selo item={item} aoEscolher={aoEscolher} />}
          <label className="rotulo" htmlFor={idCampo}>
            Texto final
          </label>
          <textarea
            ref={campo}
            id={idCampo}
            className="campo"
            value={texto}
            rows={2}
            lang="pt-BR"
            aria-describedby={idBruto}
            onChange={(e) => aoMudar(e.target.value)}
            onKeyDown={teclas}
          />

          <div className="bruto" id={idBruto}>
            <span className="rotulo">Bruto do ASR{identico && <span className="igual"> — igual ao texto final</span>}</span>
            {/* "saiu:" e "entrou:" para o leitor de tela vêm do CSS
                (::before): num span, entravam no texto que a pessoa
                seleciona e copia (revisão de 29-set-2026). */}
            <p className="diff">
              {trechos.map((t, i) => (
                <span key={i}>
                  {i > 0 && ' '}
                  {t.tipo === 'igual' && t.texto}
                  {t.tipo === 'saiu' && <del>{t.texto}</del>}
                  {t.tipo === 'entrou' && <ins>{t.texto}</ins>}
                </span>
              ))}
            </p>
          </div>

          {semTextoFinal(item) ? (
            <p className="nota">
              Este ditado falhou antes de o texto final existir. O campo começa com o texto do ASR: revise e salve, ou copie para colar onde
              precisar.
            </p>
          ) : (
            item.status === 'error' && (
              <p className="nota">Este ditado terminou com erro: o texto pode não ter entrado no campo. Copie para colar onde precisar.</p>
            )
          )}
          {naLixeira ? (
            <p className="nota" role="alert">
              Este ditado foi movido para a lixeira {outroLugar}. O texto acima continua aqui para você copiar; para salvá-lo, restaure o ditado pela
              Lixeira.
            </p>
          ) : (
            rascunho.conflito && (
              <p className="nota" role="alert">
                Este ditado foi alterado {outroLugar} enquanto você editava. O texto acima continua sendo o seu; salve de novo para substituir a versão
                atual.
              </p>
            )
          )}
          {rascunho.semConexao && edicaoSemConexao && (
            <p className="nota" role="alert">
              {edicaoSemConexao}
            </p>
          )}

          <p className="meta">{meta.join(' · ')}</p>

          <div className="acoes">
            <button type="button" className="botao primario" disabled={!mudou || salvando || naLixeira} onClick={() => void salvar()}>
              {salvando ? 'Salvando…' : 'Salvar'}
            </button>
            <button type="button" className="botao" onClick={aoFechar}>
              Cancelar
            </button>
            <span className="atalhos" aria-hidden="true">
              Ctrl+Enter salva · Esc cancela
            </span>
            <span className="empurra" />
            <button type="button" className="botao" disabled={vazio} onClick={() => aoCopiar(texto)}>
              Copiar
            </button>
            <button type="button" className="botao" onClick={aoMoverParaLixeira}>
              Mover para a lixeira
            </button>
          </div>
        </div>
      </div>
    </li>
  )
}

// Selo é o "2 versões" aberto: as duas, com as palavras que mudam marcadas, e
// a escolha (ADR-019, "Conflito"; 08, "O selo de conflito"). A que ficou é o
// texto do ditado; a outra, a que perdeu. O nome do aparelho chega na F6.
function Selo({ item, aoEscolher }: { item: Ditado; aoEscolher?(escolha: EscolhaDoSelo): Promise<void> }) {
  const outra = item.versao_em_conflito!
  const [escolhendo, setEscolhendo] = useState(false)
  // saiu: só na outra; entrou: só na que ficou (compararPalavras(a, b)).
  const trechos = useMemo(() => compararPalavras(outra.final_text, item.final_text), [outra.final_text, item.final_text])
  const idTitulo = `selo-${item.id}`
  const idAjuda = `selo-ajuda-${item.id}`

  async function escolher(escolha: EscolhaDoSelo) {
    if (escolhendo || !aoEscolher) return
    setEscolhendo(true)
    try {
      await aoEscolher(escolha)
    } finally {
      setEscolhendo(false)
    }
  }

  return (
    <section className="selo" aria-labelledby={idTitulo}>
      <p className="selo-titulo" id={idTitulo}>
        Mudou em dois lugares. Ficou esta; a outra está guardada.
      </p>
      <div className="versoes">
        <div className="versao">
          <p className="rotulo">Ficou</p>
          <p className="versao-texto">
            <Trechos trechos={trechos} sem="saiu" />
          </p>
        </div>
        <div className="versao">
          <p className="rotulo">
            A outra · <time dateTime={outra.conflict_at}>{formatarCurta(outra.conflict_at)}</time>
          </p>
          <p className="versao-texto">
            <Trechos trechos={trechos} sem="entrou" />
          </p>
        </div>
      </div>
      {/* A escolha não tem Desfazer: a que perde deixa de existir (um selo
          só por item, ADR-019). "Guardada" vale até a escolha, e a linha de
          baixo diz isso antes do clique. */}
      {aoEscolher && (
        <>
          <div className="acoes">
            <button
              type="button"
              className="botao"
              aria-disabled={escolhendo || undefined}
              aria-describedby={idAjuda}
              onClick={() => void escolher('manter')}
            >
              Ficar com esta
            </button>
            <button
              type="button"
              className="botao"
              aria-disabled={escolhendo || undefined}
              aria-describedby={idAjuda}
              onClick={() => void escolher('trocar')}
            >
              Ficar com a outra
            </button>
          </div>
          <p className="ajuda" id={idAjuda}>
            A que você não escolher é descartada.
          </p>
        </>
      )}
    </section>
  )
}

// Trechos escreve um dos dois lados da comparação: as palavras iguais e,
// marcadas, as que só este lado tem.
function Trechos({ trechos, sem }: { trechos: ReturnType<typeof compararPalavras>; sem: 'saiu' | 'entrou' }) {
  return trechos
    .filter((t) => t.tipo !== sem)
    .map((t, i) => (
      <span key={i}>
        {i > 0 && ' '}
        {t.tipo === 'igual' ? t.texto : <span className="difere">{t.texto}</span>}
      </span>
    ))
}
