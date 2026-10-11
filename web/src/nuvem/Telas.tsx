// As telas inteiras do app da nuvem (ADR-019, F4; 08-ui-ux-spec.md, "Celular
// (PWA)"): Abrindo, Entrar, Sessão expirada, Sem conexão e Não abriu, e a
// faixa do topo quando a rede cai no meio do uso. Pequenas e discretas: a Aurora, um
// título, uma linha e um botão. Cada tela leva o foco para o título quando
// aparece, para o leitor de tela anunciar a troca. A sessão que acaba com um
// texto não salvo na tela não troca a tela: EntrarDeNovo.tsx.

import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react'
import type { PropsDaFaixa } from '../ambiente'
import { Aurora } from '../Aurora'
import type { LoginDeTeste } from './cliente'
import { fraseDoLogin, useVoltaDoCache } from './entrar'

export function Tela({ titulo, linha, children }: { titulo: string; linha: string; children?: ReactNode }) {
  const h1 = useRef<HTMLHeadingElement>(null)
  useEffect(() => {
    h1.current?.focus()
  }, [titulo])
  return (
    <main className="tela">
      <Aurora tamanho={96} />
      <h1 ref={h1} tabIndex={-1}>
        {titulo}
      </h1>
      <p>{linha}</p>
      {children}
    </main>
  )
}

// esperaDoAbrindo: quanto a abertura espera o GET /api/v1/me antes de dizer
// "Abrindo…". Numa rede boa, a resposta chega antes, e nada pisca.
const esperaDoAbrindo = 300

// Abrindo: a abertura esperando a nuvem. Depois de esperaDoAbrindo, a Aurora
// e "Abrindo…", que o leitor de tela ouve (role="status": a região existe
// desde o começo, vazia, para o anúncio sair). Sem h1 e sem foco: a tela que
// vier depois leva o foco ao título dela.
export function Abrindo() {
  const [visivel, setVisivel] = useState(false)
  useEffect(() => {
    const t = setTimeout(() => setVisivel(true), esperaDoAbrindo)
    return () => clearTimeout(t)
  }, [])
  return (
    <main className="tela abrindo">
      {visivel && <Aurora tamanho={96} />}
      <p role="status">{visivel ? 'Abrindo…' : ''}</p>
    </main>
  )
}

interface PropsEntrar {
  // expirada: a sessão acabou no meio do uso (qualquer 401); senão, é a
  // primeira entrada.
  expirada: boolean
  // aprovar: a página é a aprovação de um PC (F6); entrar volta a ela.
  aprovar?: boolean
  // teste: o login de teste do servidor (só no servidor de teste, no PC).
  // undefined: ainda perguntando; null: não há.
  teste: LoginDeTeste | null | undefined
  entrarComGoogle(): Promise<void>
  entrarComContaDeTeste(conta: string): Promise<void>
}

export function Entrar({ expirada, aprovar = false, teste, entrarComGoogle, entrarComContaDeTeste }: PropsEntrar) {
  const [indo, setIndo] = useState<'google' | 'teste' | null>(null)
  const [erro, setErro] = useState<{ texto: string; vez: number } | null>(null)

  // A volta do Google pelo Voltar do navegador, com a página do cache: o
  // botão volta a valer.
  useVoltaDoCache(useCallback(() => setIndo(null), []))

  async function ir(qual: 'google' | 'teste', f: () => Promise<void>) {
    if (indo) return
    setIndo(qual)
    setErro(null)
    try {
      await f()
      // O Google: a página está indo embora, e o botão fica em "Abrindo o
      // Google…" até lá. A conta de teste: o app troca a tela.
    } catch (e) {
      setIndo(null)
      setErro((a) => ({ texto: fraseDoLogin(e), vez: (a?.vez ?? 0) + 1 }))
    }
  }

  // Os botões esperam a resposta do servidor sobre o login de teste (um
  // pedido curto): o do Google sumiria num servidor de teste sem ele.
  const comGoogle = teste !== undefined && teste?.google !== false
  return (
    <Tela
      titulo={expirada ? 'Sua sessão terminou.' : 'Aurora Whisper'}
      linha={
        aprovar ? 'Entre para aprovar a conexão do PC.' : expirada ? 'Entre de novo para ver seu histórico.' : 'Entre para ver o que você ditou.'
      }
    >
      <div className="tela-acoes">
        {comGoogle && (
          <button type="button" className="botao primario" aria-disabled={indo !== null || undefined} onClick={() => void ir('google', entrarComGoogle)}>
            {indo === 'google' ? 'Abrindo o Google…' : 'Entrar com o Google'}
          </button>
        )}
        {teste && (
          <>
            <button
              type="button"
              className={comGoogle ? 'botao' : 'botao primario'}
              aria-disabled={indo !== null || undefined}
              aria-describedby="entrar-teste-ajuda"
              onClick={() => void ir('teste', () => entrarComContaDeTeste(teste.conta))}
            >
              {indo === 'teste' ? 'Entrando…' : 'Entrar com a conta de teste'}
            </button>
            <p className="ajuda" id="entrar-teste-ajuda">
              Só neste PC, sem Google.
            </p>
          </>
        )}
        {erro && (
          <p className="nota" role="alert" key={erro.vez}>
            {erro.texto}
          </p>
        )}
      </div>
    </Tela>
  )
}

interface PropsTentar {
  tentando: boolean
  aoTentar(): void
}

// SemConexao: a página abriu, mas a nuvem não respondeu. Desde a F5, abre
// também sem rede nenhuma: o service worker (web/pwa/sw.js) sobe a casca
// guardada, e o GET /api/v1/me, que ele não toca, falha.
export function SemConexao({ tentando, aoTentar }: PropsTentar) {
  return (
    <Tela titulo="Sem conexão." linha="O que você ditou continua no PC.">
      <div className="tela-acoes">
        <BotaoTentar tentando={tentando} aoTentar={aoTentar} />
      </div>
    </Tela>
  )
}

// NaoAbriu: a nuvem respondeu com um erro que não é de sessão nem de rede.
export function NaoAbriu({ tentando, aoTentar }: PropsTentar) {
  return (
    <Tela titulo="Não deu para abrir." linha="Tente de novo daqui a pouco.">
      <div className="tela-acoes">
        <BotaoTentar tentando={tentando} aoTentar={aoTentar} />
      </div>
    </Tela>
  )
}

// O botão fica no lugar enquanto tenta: se ele sumisse, o foco cairia no
// <body>.
function BotaoTentar({ tentando, aoTentar }: PropsTentar) {
  return (
    <button type="button" className="botao primario" aria-disabled={tentando || undefined} onClick={() => !tentando && aoTentar()}>
      {tentando ? 'Tentando…' : 'Tentar de novo'}
    </button>
  )
}

// FaixaNuvem: a rede caiu no meio do uso. A lista e os editores ficam na tela,
// como no PC. A sessão que acaba tem a faixa dela (EntrarDeNovo.tsx), e só
// com um texto não salvo na tela; sem ele, o app troca a tela inteira por
// "Sua sessão terminou." (ambienteNuvem.tsx escolhe).
export function FaixaNuvem({ aoTentarDeNovo }: PropsDaFaixa) {
  return (
    <div className="faixa" role="alert">
      <p>
        <strong>Sem conexão.</strong> O que você ditou continua no PC.
      </p>
      <button type="button" className="aviso-acao" onClick={aoTentarDeNovo}>
        Tentar de novo
      </button>
    </div>
  )
}
