// "Ditar pelo celular" nas Configurações do app da nuvem (ADR-021, "A tela" e
// "O aceite e as preferências"; texto da D0): o estado (Desligado, ou Ligado
// desde a data do aceite), o provedor da voz, a chave, o modelo, a dica e o
// Polish. Tudo age na hora, sem "Salvar": cada mudança é um PUT das
// preferências na conta, e nada disso desce aos PCs.
//
// O aceite manda: sem ele, só o "Ligar". Trocar de provedor, ou ligar o Polish
// com um provedor fora do aceite, reabre a folha (o "Agora não" deixa tudo como
// estava), e o servidor que recusar o PUT por falta de aceite (SEM_ACEITE: o
// aceite acabou em outro aparelho) também abre a folha. A dica e o Polish
// começam desligados.
//
// O Polish só liga na conta com um modelo (o servidor não tem modelo padrão do
// Polish): marcar a caixa mostra o provedor, a chave e o modelo, e a conta passa
// a tê-lo ligado quando o modelo é escolhido. Até lá a caixa fica marcada só
// aqui (rascunhoDoPolish) e a conta continua com o Polish desligado.

import { type ReactNode, useEffect, useEffectEvent, useRef, useState } from 'react'
import { ErroDaApi, problemaDe } from '../../api'
import type { SecaoDasConfiguracoes } from '../../ambiente'
import { formatarData } from '../../datas'
import type { ClienteNuvem } from '../cliente'
import { type Provedor, type ProvedorDeVoz, type ProvedorDoPolish, nomeDoProvedor, provedoresDeVoz, provedoresDoPolish } from './aviso'
import { ChaveDoProvedor } from './ChaveDoProvedor'
import { textoDaEspera } from './erros'
import { type EstadoDoDitado, type Preferencias, aceiteVigente, aceitoPara, modelosDeVoz, modelosDoPolish, provedoresExigidos } from './estado'
import type { Folha } from './folha'

interface Props {
  cliente: ClienteNuvem
  folha: Folha
  visivel: boolean
  // leitura sobe quando a conta mudou por fora (o "apagar meus dados"): a seção
  // lê de novo.
  leitura: number
  // secao: o pedido do Painel de levar esta seção à vista (a chave que falta).
  secao?: { nome: SecaoDasConfiguracoes; vez: number } | null
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
  // aoMudar: o ditado pelo celular da conta mudou aqui; o gravador, que
  // guardava que estava tudo pronto, confere de novo.
  aoMudar(): void
}

// iguais: as preferências da tela são as que a conta já tem (nada a gravar).
const iguais = (a: Preferencias, b: Preferencias) =>
  a.provedor === b.provedor &&
  a.modelo === b.modelo &&
  a.dica === b.dica &&
  a.polish === b.polish &&
  a.polish_provedor === b.polish_provedor &&
  a.polish_modelo === b.polish_modelo

// fraseDeLer: a leitura do estado falhou por outra razão que a ligação.
function fraseDeLer(e: unknown): string {
  if (e instanceof ErroDaApi && e.status === 404) return 'Este servidor não tem o ditado pelo celular.'
  return 'Não deu para ver o ditado pelo celular. Tente de novo mais tarde.'
}

// fraseDeSalvar: nunca a mensagem do servidor.
function fraseDeSalvar(e: unknown): string {
  if (e instanceof ErroDaApi) {
    if (e.status === 404) return 'Este servidor não tem o ditado pelo celular.'
    if (e.status === 429 || e.status === 503) return `Espere um pouco.${textoDaEspera(e.esperaSeg)}`
  }
  return 'Não deu para salvar. Tente de novo.'
}

export function DitarPeloCelular({ cliente, folha, visivel, leitura, secao, aoPerderLigacao, avisar, aoMudar }: Props) {
  const perderLigacao = useEffectEvent(aoPerderLigacao)
  const [estado, setEstado] = useState<EstadoDoDitado | null>(null)
  const [erroDeLer, setErroDeLer] = useState<string | null>(null)
  // aplicando: um PUT das preferências, ou a folha que ele abriu, está no ar.
  const [aplicando, setAplicando] = useState(false)
  const [erroDeSalvar, setErroDeSalvar] = useState<string | null>(null)
  // modelos: os que cada chave alcança. Ausente, ainda não lido; null, não deu
  // para ler.
  const [modelos, setModelos] = useState<Partial<Record<Provedor, string[] | null>>>({})
  const lendoModelos = useRef(new Set<Provedor>())
  const titulo = useRef<HTMLHeadingElement>(null)
  const atendida = useRef(0)
  // O Polish marcado e ainda sem modelo: vale só para a leitura da conta em que
  // foi marcado (se a conta mudou por fora, o rascunho se perde).
  const [rascunhoDoPolish, setRascunhoDoPolish] = useState<number | null>(null)
  const polishAberto = rascunhoDoPolish === leitura

  // ─── Desligar ───
  const [desligando, setDesligando] = useState<'fechado' | 'aberto' | 'indo'>('fechado')
  const [erroDeDesligar, setErroDeDesligar] = useState<string | null>(null)
  const botaoDesligar = useRef<HTMLButtonElement>(null)
  const botaoCancelarDesligar = useRef<HTMLButtonElement>(null)
  const focarDesligar = useRef(false)

  // O estado da conta é lido quando a tela aparece: outro aparelho pode ter
  // mudado o aceite enquanto ela estava escondida.
  useEffect(() => {
    if (!visivel) return
    let vivo = true
    cliente.estadoDoDitado().then(
      (e) => {
        if (!vivo) return
        setEstado(e)
        setErroDeLer(null)
      },
      (e: unknown) => {
        if (!vivo) return
        if (problemaDe(e)) perderLigacao(e)
        else setErroDeLer(fraseDeLer(e))
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, visivel, leitura])

  // Quem pediu esta seção (o gravador sem chave) a vê com o foco no título,
  // uma vez por pedido.
  useEffect(() => {
    if (!visivel || !secao || secao.nome !== 'ditar' || secao.vez <= atendida.current) return
    atendida.current = secao.vez
    titulo.current?.focus()
  }, [visivel, secao])

  // Os modelos de cada chave, lidos quando a chave existe e ainda não foram lidos.
  useEffect(() => {
    if (!estado) return
    const usados: Provedor[] = [estado.preferencias.provedor]
    if (estado.preferencias.polish || polishAberto) usados.push(estado.preferencias.polish_provedor)
    for (const p of usados) {
      if (!estado.chaves[p].configurada || p in modelos || lendoModelos.current.has(p)) continue
      lendoModelos.current.add(p)
      cliente.modelosDaChave(p).then(
        (lista) => {
          lendoModelos.current.delete(p)
          setModelos((m) => ({ ...m, [p]: lista }))
        },
        (e: unknown) => {
          lendoModelos.current.delete(p)
          if (problemaDe(e)) perderLigacao(e)
          else setModelos((m) => ({ ...m, [p]: null }))
        },
      )
    }
  }, [cliente, estado, modelos, polishAberto])

  useEffect(() => {
    if (desligando === 'aberto') botaoCancelarDesligar.current?.focus()
    if (desligando === 'fechado' && focarDesligar.current) {
      focarDesligar.current = false
      botaoDesligar.current?.focus()
    }
  }, [desligando])

  // ler relê o estado da conta depois de uma mudança daqui.
  async function ler(): Promise<EstadoDoDitado | null> {
    try {
      const e = await cliente.estadoDoDitado()
      setEstado(e)
      setErroDeLer(null)
      return e
    } catch (e) {
      if (problemaDe(e)) aoPerderLigacao(e)
      else setErroDeLer(fraseDeLer(e))
      return null
    }
  }

  // mudou: algo da conta mudou aqui. As chaves podem ser outras, e os modelos
  // que se leram eram da chave de antes.
  async function mudou() {
    setModelos({})
    await ler()
    aoMudar()
  }

  function tratar(e: unknown) {
    if (problemaDe(e)) aoPerderLigacao(e)
    else setErroDeSalvar(fraseDeSalvar(e))
  }

  // querido são as preferências que a pessoa quer: as da conta, com o Polish
  // marcado aqui mesmo que a conta ainda o tenha desligado (falta o modelo).
  const querido = (prefs: Preferencias): Preferencias => ({ ...prefs, polish: prefs.polish || polishAberto })

  // pedidoDeAceite monta o pedido da folha a partir das preferências que
  // valerão: o provedor da voz e, com o Polish, o dele. O aceite que a folha
  // grava é só o que ela mostra.
  function pedidoDeAceite(prefs: Preferencias, extra: { escolherVoz?: boolean; somenteLer?: boolean } = {}) {
    return {
      voz: prefs.provedor,
      polish: prefs.polish ? prefs.polish_provedor : undefined,
      ...extra,
    }
  }

  // ligar é o "Ligar": a folha, com a escolha do provedor da voz. O aceite
  // gravado, o provedor escolhido vira o das preferências.
  async function ligar() {
    if (!estado || aplicando) return
    setAplicando(true)
    setErroDeSalvar(null)
    try {
      const r = await folha.pedirAceite(pedidoDeAceite(querido(estado.preferencias), { escolherVoz: true }))
      if (!r) return
      if (r.voz !== estado.preferencias.provedor) {
        await cliente.salvarPreferencias({ ...estado.preferencias, provedor: r.voz, modelo: '' })
      }
      await mudou()
      avisar('Ditar pelo celular ligado.')
    } catch (e) {
      tratar(e)
    } finally {
      setAplicando(false)
    }
  }

  async function lerOAviso() {
    if (!estado || aplicando) return
    setAplicando(true)
    try {
      await folha.pedirAceite(pedidoDeAceite(querido(estado.preferencias), { somenteLer: true }))
    } finally {
      setAplicando(false)
    }
  }

  // aplicar muda as preferências na conta. O provedor que não está no aceite
  // vigente abre a folha antes; sem o "Aceito", nada muda. O servidor que
  // recusar por falta de aceite (o aceite acabou em outro aparelho) também
  // abre a folha, e o PUT é refeito se a pessoa aceitar.
  async function aplicar(mudanca: Partial<Preferencias>) {
    if (!estado || aplicando) return
    // quer é o que a pessoa pediu; novas, o que a conta passa a guardar: o
    // servidor só liga o Polish com um modelo, então o Polish marcado sem modelo
    // fica desligado lá (e marcado aqui, no rascunho) até o modelo ser escolhido.
    const quer: Preferencias = { ...querido(estado.preferencias), ...mudanca }
    const novas: Preferencias = quer.polish && quer.polish_modelo === '' ? { ...quer, polish: false } : quer
    const rascunho = quer.polish && !novas.polish
    setAplicando(true)
    setErroDeSalvar(null)
    try {
      if (!aceitoPara(estado, provedoresExigidos(quer))) {
        if (!(await folha.pedirAceite(pedidoDeAceite(quer)))) return
      }
      if (iguais(novas, estado.preferencias)) {
        // Nada muda na conta (marcar o Polish sem modelo, ou desmarcar o
        // rascunho): só a tela.
        setRascunhoDoPolish(rascunho ? leitura : null)
        return
      }
      try {
        await cliente.salvarPreferencias(novas)
      } catch (e) {
        if (!(e instanceof ErroDaApi && e.codigo === 'SEM_ACEITE')) throw e
        // A tela achava que valia e o servidor não: relê a conta, e a folha
        // abre com o aceite que vale de verdade. Sem o "Aceito", a tela passa
        // a dizer o que o servidor diz.
        const atual = await cliente.estadoDoDitado()
        if (!(await folha.pedirAceite(pedidoDeAceite(quer)))) {
          setEstado(atual)
          return
        }
        await cliente.salvarPreferencias(novas)
      }
      setRascunhoDoPolish(rascunho ? leitura : null)
      await mudou()
    } catch (e) {
      tratar(e)
    } finally {
      setAplicando(false)
    }
  }

  // aoSemAceite: a chave foi recusada por falta de aceite; relê a conta e abre
  // a folha para as preferências de agora. A tela fica como estava enquanto a
  // folha está aberta (a chave colada continua no campo, para valer se a
  // pessoa aceitar); sem o "Aceito", ela passa a dizer o que o servidor diz.
  async function aoSemAceite(): Promise<boolean> {
    const atual = await cliente.estadoDoDitado()
    const r = await folha.pedirAceite(pedidoDeAceite(querido(atual.preferencias)))
    if (r) await mudou()
    else setEstado(atual)
    return r !== null
  }

  async function desligar() {
    if (desligando === 'indo') return
    setDesligando('indo')
    setErroDeDesligar(null)
    try {
      await cliente.desligarDitado()
    } catch (e) {
      setDesligando('aberto')
      if (problemaDe(e)) aoPerderLigacao(e)
      else setErroDeDesligar('Não deu para desligar. Tente de novo.')
      return
    }
    setDesligando('fechado')
    setRascunhoDoPolish(null)
    await mudou()
    avisar('Ditar pelo celular desligado.')
    // O Desligar saiu de cena com o aceite: o foco vai para o título da seção.
    titulo.current?.focus()
  }

  const prefs = estado?.preferencias
  const aceite = estado ? aceiteVigente(estado) : null
  const ligado = aceite !== null
  // O Polish que a pessoa vê marcado: o da conta, ou o rascunho sem modelo.
  const polishMarcado = (prefs?.polish ?? false) || polishAberto

  return (
    <section className="secao" aria-labelledby="cfg-ditar">
      <h2 id="cfg-ditar" className="secao-titulo" ref={titulo} tabIndex={-1}>
        Ditar pelo celular
      </h2>
      <p className="secao-intro">
        Fale no celular e o texto entra no histórico. A voz vai ao provedor que você escolher, com a sua chave. O ditado não funciona com a tela apagada nem com o app em
        segundo plano.
      </p>

      {estado === null && !erroDeLer && <p className="ajuda">Carregando…</p>}
      {erroDeLer && (
        <p className="nota" role="alert">
          {erroDeLer}
        </p>
      )}

      {estado && prefs && (
        <>
          <div className="ajuste conta">
            <p className="secao-intro" id="cfg-ditar-estado">
              {aceite
                ? `Ligado${aceite.aceito_em ? ` desde ${formatarData(aceite.aceito_em)}` : ''}.`
                : estado.aceite !== null
                  ? 'Desligado. O aviso mudou: leia e aceite de novo para ligar.'
                  : 'Desligado.'}
            </p>
            <span className="acoes">
              {ligado ? (
                <>
                  <button type="button" className="botao" aria-disabled={aplicando || undefined} onClick={() => void lerOAviso()}>
                    Ler o aviso
                  </button>
                  <button
                    ref={botaoDesligar}
                    type="button"
                    className="botao"
                    aria-label="Desligar o ditado no celular"
                    aria-disabled={aplicando || undefined}
                    onClick={() => !aplicando && setDesligando('aberto')}
                  >
                    Desligar
                  </button>
                </>
              ) : (
                <button type="button" className="botao primario" aria-disabled={aplicando || undefined} onClick={() => void ligar()}>
                  Ligar
                </button>
              )}
            </span>
          </div>

          {desligando !== 'fechado' && (
            <div className="ajuste">
              <div
                className="confirmar"
                role="group"
                aria-labelledby="cfg-ditar-desligar"
                onKeyDown={(e) => {
                  if (e.key === 'Escape' && desligando === 'aberto') {
                    e.preventDefault()
                    focarDesligar.current = true
                    setDesligando('fechado')
                  }
                }}
              >
                <p className="ajuste-nome" id="cfg-ditar-desligar">
                  Desligar o ditado no celular
                </p>
                <p className="ajuda">
                  Isto tira o seu aceite e apaga as suas chaves do servidor. Não apaga os ditados já feitos. Revogue as chaves no provedor.
                </p>
                <div className="acoes">
                  <button type="button" className="botao risco" aria-disabled={desligando === 'indo' || undefined} onClick={() => void desligar()}>
                    {desligando === 'indo' ? 'Desligando…' : 'Desligar'}
                  </button>
                  <button
                    ref={botaoCancelarDesligar}
                    type="button"
                    className="botao"
                    aria-disabled={desligando === 'indo' || undefined}
                    onClick={() => {
                      if (desligando === 'indo') return
                      focarDesligar.current = true
                      setDesligando('fechado')
                    }}
                  >
                    Cancelar
                  </button>
                </div>
                {erroDeDesligar && (
                  <p className="nota" role="alert">
                    {erroDeDesligar}
                  </p>
                )}
              </div>
            </div>
          )}

          {ligado && (
            <>
              <div className="ajuste">
                <label className="ajuste-nome" htmlFor="cfg-ditar-provedor">
                  Provedor da voz
                </label>
                <select
                  id="cfg-ditar-provedor"
                  className="entrada"
                  value={prefs.provedor}
                  disabled={aplicando}
                  onChange={(e) => void aplicar({ provedor: e.target.value as ProvedorDeVoz, modelo: '' })}
                >
                  {provedoresDeVoz.map((p) => (
                    <option key={p} value={p}>
                      {nomeDoProvedor[p]}
                    </option>
                  ))}
                </select>
              </div>

              <ChaveDoProvedor
                key={prefs.provedor}
                cliente={cliente}
                provedor={prefs.provedor}
                chave={estado.chaves[prefs.provedor]}
                aoMudar={mudou}
                aoSemAceite={aoSemAceite}
                aoPerderLigacao={aoPerderLigacao}
                avisar={avisar}
              />

              <Modelo
                id="cfg-ditar-modelo"
                rotulo="Modelo da voz"
                valor={prefs.modelo}
                lista={modelos[prefs.provedor]}
                temChave={estado.chaves[prefs.provedor].configurada}
                filtro={modelosDeVoz}
                desabilitado={aplicando}
                aoEscolher={(modelo) => void aplicar({ modelo })}
              />

              <div className="ajuste">
                <label className="marcar">
                  <input type="checkbox" checked={prefs.dica} disabled={aplicando} onChange={(e) => void aplicar({ dica: e.target.checked })} />
                  Mandar os termos aprendidos como dica
                </label>
                <p className="ajuda">Eles vão ao provedor junto com a voz, em todo ditado, e podem ser nomes de clientes ou de outras pessoas.</p>
              </div>

              <div className="ajuste">
                <label className="marcar">
                  <input type="checkbox" checked={polishMarcado} disabled={aplicando} onChange={(e) => void aplicar({ polish: e.target.checked })} />
                  Organizar o texto com o Polish
                </label>
                <p className="ajuda">O texto do ditado vai ao provedor do Polish.</p>
              </div>

              {polishMarcado && (
                <>
                  <div className="ajuste">
                    <label className="ajuste-nome" htmlFor="cfg-ditar-polish-provedor">
                      Provedor do Polish
                    </label>
                    <select
                      id="cfg-ditar-polish-provedor"
                      className="entrada"
                      value={prefs.polish_provedor}
                      disabled={aplicando}
                      onChange={(e) => void aplicar({ polish_provedor: e.target.value as ProvedorDoPolish, polish_modelo: '' })}
                    >
                      {provedoresDoPolish.map((p) => (
                        <option key={p} value={p}>
                          {nomeDoProvedor[p]}
                        </option>
                      ))}
                    </select>
                  </div>

                  {prefs.polish_provedor === prefs.provedor ? (
                    <div className="ajuste">
                      <p className="ajuda">A chave da {nomeDoProvedor[prefs.provedor]}, acima, também serve ao Polish.</p>
                    </div>
                  ) : (
                    <ChaveDoProvedor
                      key={prefs.polish_provedor}
                      cliente={cliente}
                      provedor={prefs.polish_provedor}
                      chave={estado.chaves[prefs.polish_provedor]}
                      aoMudar={mudou}
                      aoSemAceite={aoSemAceite}
                      aoPerderLigacao={aoPerderLigacao}
                      avisar={avisar}
                    />
                  )}

                  <Modelo
                    id="cfg-ditar-polish-modelo"
                    rotulo="Modelo do Polish"
                    valor={prefs.polish_modelo}
                    lista={modelos[prefs.polish_provedor]}
                    temChave={estado.chaves[prefs.polish_provedor].configurada}
                    filtro={modelosDoPolish}
                    semPadrao
                    desabilitado={aplicando}
                    aoEscolher={(polish_modelo) => void aplicar({ polish_modelo })}
                  />
                </>
              )}

              {erroDeSalvar && (
                <p className="nota" role="alert">
                  {erroDeSalvar}
                </p>
              )}
            </>
          )}
        </>
      )}
    </section>
  )
}

// Modelo é a lista dos modelos que a chave alcança, com "Padrão do provedor"
// no começo. O modelo que a conta tem e a lista não mostra (o provedor mudou a
// lista) continua escolhível, para a tela não mentir sobre o que vale.
//
// O servidor guarda sempre o modelo de verdade: "Padrão do provedor" é o pedido
// ao servidor de escolher o padrão (o "" do PUT), e o que volta é o nome dele.
// semPadrao é o modelo do Polish, que não tem padrão: enquanto não há modelo, a
// lista diz "Escolha um modelo" (que não se escolhe de volta), e a ajuda diz que
// falta escolher.
function Modelo({
  id,
  rotulo,
  valor,
  lista,
  temChave,
  filtro,
  semPadrao = false,
  desabilitado,
  aoEscolher,
}: {
  id: string
  rotulo: string
  valor: string
  lista: string[] | null | undefined
  temChave: boolean
  filtro(lista: readonly string[]): string[]
  semPadrao?: boolean
  desabilitado: boolean
  aoEscolher(modelo: string): void
}) {
  const modelos = lista ? filtro(lista) : []
  const valePadrao = semPadrao ? '' : ' Vale o padrão do provedor.'
  let ajuda = ''
  if (!temChave) ajuda = 'Cole a chave para ver os modelos da sua conta.'
  else if (lista === undefined) ajuda = 'Lendo os modelos da sua conta…'
  else if (lista === null) ajuda = `Não deu para ler os modelos.${semPadrao ? ' Tente de novo mais tarde.' : valePadrao}`
  else if (modelos.length === 0) ajuda = `A sua conta não mostrou nenhum modelo para este uso.${valePadrao}`
  else if (semPadrao && valor === '') ajuda = 'Escolha o modelo para o Polish agir nos seus ditados.'
  return (
    <div className="ajuste">
      <label className="ajuste-nome" htmlFor={id}>
        {rotulo}
      </label>
      <select id={id} className="entrada" value={valor} disabled={desabilitado || !temChave} aria-describedby={`${id}-ajuda`} onChange={(e) => aoEscolher(e.target.value)}>
        {semPadrao ? (
          valor === '' && (
            <option value="" disabled>
              Escolha um modelo
            </option>
          )
        ) : (
          <option value="">Padrão do provedor</option>
        )}
        {modelos.map((m) => (
          <option key={m} value={m}>
            {m}
          </option>
        ))}
        {valor !== '' && !modelos.includes(valor) && <option value={valor}>{valor}</option>}
      </select>
      <p className="ajuda" id={`${id}-ajuda`}>
        {ajuda}
      </p>
    </div>
  )
}
