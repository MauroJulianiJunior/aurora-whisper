// A folha do aceite do ditado pelo celular (ADR-021, decisão H; texto da D0,
// versão do aviso em aviso.ts): uma folha só, em destaque, específica e
// separada das outras finalidades. Diz o que vai (a voz de cada ditado, a
// chave, a dica e o Polish se ligados), para quem, onde o provedor diz que
// processa e por quanto tempo guarda, e que a voz e a dica PODEM levar dado de
// terceiro, como dado de cliente. Os botões são "Aceito" e "Agora não".
//
// Sem o aceite, o microfone nem abre, a chave não é pedida, e o servidor
// recusa o ditado (SEM_ACEITE). Quem pede a folha é o gravador, antes do
// getUserMedia (porta.ts), e as Configurações, ao trocar de provedor. Qualquer
// mudança no texto daqui muda a versão do aviso (aviso.ts): quem aceitou a
// antiga aceita de novo.

import { type KeyboardEvent, type ReactNode, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { createPortal } from 'react-dom'
import { type Provedor, blocoDoProvedor, nomeDoProvedor, provedoresDeVoz } from './aviso'
import type { Folha, FolhaAberta } from './folha'

const focaveis = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex="0"]'

export function FolhaDoAceite({ folha }: { folha: Folha }) {
  const aberta = useSyncExternalStore(folha.assinar, folha.estado)
  // Fora do Painel: a folha é da tela inteira, e nenhum ancestral dele (o
  // cabeçalho fixo) pode prendê-la.
  return aberta ? createPortal(<Conteudo folha={folha} aberta={aberta} />, document.body) : null
}

function Conteudo({ folha, aberta }: { folha: Folha; aberta: FolhaAberta }) {
  const { pedido, voz, enviando, erro } = aberta
  const folhaRef = useRef<HTMLDivElement>(null)
  const titulo = useRef<HTMLHeadingElement>(null)
  // Quem tinha o foco quando a folha abriu (o microfone, o seletor de
  // provedor) o recebe de volta ao fechar.
  const [antes] = useState(() => (document.activeElement instanceof HTMLElement ? document.activeElement : null))

  useEffect(() => {
    titulo.current?.focus()
    return () => antes?.focus()
  }, [antes])

  function teclado(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      folha.recusar()
      return
    }
    if (e.key !== 'Tab') return
    // O Tab não sai da folha: do último volta ao primeiro, e o Shift+Tab do
    // primeiro (ou do título, onde o foco nasce) vai ao último.
    const lista = Array.from(folhaRef.current?.querySelectorAll<HTMLElement>(focaveis) ?? [])
    const primeiro = lista[0]
    const ultimo = lista[lista.length - 1]
    if (!primeiro || !ultimo) return
    const atual = document.activeElement
    if (e.shiftKey && (atual === primeiro || atual === titulo.current)) {
      e.preventDefault()
      ultimo.focus()
    } else if (!e.shiftKey && atual === ultimo) {
      e.preventDefault()
      primeiro.focus()
    }
  }

  const polish = pedido.polish
  // Os blocos "Para onde, e por quanto tempo fica lá": o provedor da voz e,
  // se for outro, o do Polish.
  const blocos: Provedor[] = [voz]
  if (polish && polish !== voz) blocos.push(polish)

  return (
    <>
      <div className="folha-fundo" aria-hidden="true" />
      <div ref={folhaRef} className="folha" role="dialog" aria-modal="true" aria-labelledby="folha-titulo" onKeyDown={teclado}>
        <div className="folha-corpo">
          <h2 id="folha-titulo" className="folha-titulo" ref={titulo} tabIndex={-1}>
            Ao ditar pelo celular, a sua voz vai a um provedor que não diz processar no Brasil.
          </h2>
          {/* O "Por isso," retoma o título: vem sempre logo depois dele (no modo de só ler, depois do que já foi aceito). */}
          {pedido.somenteLer ? (
            <p>Você já aceitou este aviso; ele está aqui para você reler. Por isso, tratamos o envio como uma transferência internacional.</p>
          ) : (
            <p>
              Por isso, tratamos o envio como uma transferência internacional. Leia antes de aceitar. Se você tocar em &quot;Agora não&quot;, o
              microfone não abre, nada é enviado e o resto do app continua igual.
            </p>
          )}

          {pedido.escolherVoz && (
            <div className="modos folha-escolha" role="radiogroup" aria-labelledby="folha-escolha-rotulo">
              <p id="folha-escolha-rotulo" className="ajuste-nome">
                Provedor da voz
              </p>
              {provedoresDeVoz.map((p) => (
                <div key={p} className="modo">
                  <input
                    type="radio"
                    id={`folha-voz-${p}`}
                    name="folha-voz"
                    checked={voz === p}
                    disabled={enviando}
                    onChange={() => folha.escolherVoz(p)}
                  />
                  <label htmlFor={`folha-voz-${p}`}>{nomeDoProvedor[p]}</label>
                </div>
              ))}
            </div>
          )}

          <h3 className="folha-sub">O que vai</h3>
          <ul className="fatos folha-lista">
            <li>
              <strong>A voz de cada ditado.</strong> Ela passa pelo servidor desta nuvem, só na memória, e vai ao provedor que você escolheu:{' '}
              <strong>{nomeDoProvedor[voz]}</strong>. Não é gravada no seu aparelho nem no servidor. O provedor pode guardá-la: veja abaixo.
            </li>
            <li>
              <strong>A sua chave de API.</strong> Você a cola uma vez. O servidor a guarda cifrada, mas <strong>quem administra o servidor consegue decifrá-la</strong>.
              Use uma chave só para o celular, com limite de gasto no provedor.
            </li>
            <li>
              <strong>Se você ligar a dica:</strong> os termos que você ensinou ao Aurora Whisper, junto com a voz, em todo ditado. Eles podem ser nomes de clientes
              ou de outras pessoas. A dica começa desligada.
            </li>
            <li>
              {polish ? (
                <>
                  <strong>O Polish que você está ligando:</strong> o texto do ditado, para <strong>{nomeDoProvedor[polish]}</strong>, com a sua chave.
                </>
              ) : (
                <>
                  <strong>Se você ligar o Polish:</strong> o texto do ditado, para a OpenAI ou a Anthropic (a que você escolher), com a sua chave. O Polish começa
                  desligado.
                </>
              )}
            </li>
          </ul>

          <h3 className="folha-sub">Para onde, e por quanto tempo fica lá</h3>
          {blocos.map((p) => (
            <Bloco key={p}>
              <strong>{nomeDoProvedor[p]}.</strong> {blocoDoProvedor[p]}
            </Bloco>
          ))}

          <h3 className="folha-sub">Dados de outras pessoas</h3>
          <p>
            A voz e os termos podem ter dados de outras pessoas (clientes, quem está perto de você). O seu aceite vale por você, e não por elas. Se você dita dado
            de cliente, veja se isso cabe no que você deve a ele.
          </p>

          <h3 className="folha-sub">O que isto não faz</h3>
          <p>
            Não funciona com a tela apagada nem com o app em segundo plano: se a tela apagar, o ditado para. A sua voz não é usada para identificar você.
          </p>

          <h3 className="folha-sub">Você pode desfazer</h3>
          <p>
            Em Configurações, &quot;Desligar o ditado no celular&quot; tira o seu aceite e apaga as suas chaves do servidor. Revogue também a chave no provedor: uma cópia
            cifrada dela fica nos backups por cerca de 30 dias.
          </p>
        </div>

        <div className="folha-rodape">
          {erro && (
            <p className="nota" role="alert">
              {erro}
            </p>
          )}
          <div className="acoes">
            {pedido.somenteLer ? (
              <button type="button" className="botao" onClick={() => folha.recusar()}>
                Fechar
              </button>
            ) : (
              <>
                <button type="button" className="botao primario" aria-disabled={enviando || undefined} onClick={() => void folha.aceitar()}>
                  {enviando ? 'Guardando…' : 'Aceito'}
                </button>
                <button type="button" className="botao" aria-disabled={enviando || undefined} onClick={() => folha.recusar()}>
                  Agora não
                </button>
              </>
            )}
          </div>
        </div>
      </div>
    </>
  )
}

function Bloco({ children }: { children: ReactNode }) {
  return <p className="folha-bloco">{children}</p>
}
