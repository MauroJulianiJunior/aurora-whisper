// A chave de um provedor nas Configurações do ditado pelo celular (ADR-021,
// decisão G e "As chaves"): colar, trocar e remover. O servidor a guarda
// cifrada e o app só vê o fim dela ("Chave terminada em WXYZ"). Ao trocar ou
// remover, a tela lembra de revogar a antiga no provedor: uma cópia cifrada
// dela fica nos backups.
//
// A chave nunca entra no estado do React: fica só no campo (type=password) até
// o "Guardar chave", vai no corpo do PUT e o campo esvazia. Nenhuma frase de
// erro daqui repete o que o servidor disse: o decodificador dele pode trazer um
// pedaço do que foi colado, e a tela escolhe a frase pelo código.

import { type KeyboardEvent, useEffect, useRef, useState } from 'react'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { OndeCriarAChave } from '../../OndeCriarAChave'
import type { ClienteNuvem } from '../cliente'
import { type Provedor, nomeDoProvedor } from './aviso'
import { fraseDaChave } from './erros'
import type { ChaveGuardada } from './estado'
import { paginasDeChaveDoApp } from './paginasDeChave'

interface Props {
  cliente: ClienteNuvem
  provedor: Provedor
  chave: ChaveGuardada
  // aoMudar: a chave foi guardada ou removida. Devolve quando a tela já leu o
  // estado novo da conta.
  aoMudar(): Promise<void>
  // aoSemAceite: o servidor recusou a chave por falta de aceite (o aceite acabou
  // em outro aparelho). Abre a folha; true se a pessoa aceitou.
  aoSemAceite(): Promise<boolean>
  aoPerderLigacao(e: unknown): void
  avisar(texto: string): void
}

const lembrete = 'Revogue também esta chave no provedor. Uma cópia cifrada dela fica nos backups por cerca de 30 dias.'

type Foco = 'campo' | 'trocar' | 'remover' | 'cancelar'

export function ChaveDoProvedor({ cliente, provedor, chave, aoMudar, aoSemAceite, aoPerderLigacao, avisar }: Props) {
  const nome = nomeDoProvedor[provedor]
  // ver: o estado da chave (e o campo, se não há chave); colar: trocar uma chave
  // que existe; remover: a pergunta do Remover.
  const [modo, setModo] = useState<'ver' | 'colar' | 'remover'>('ver')
  const [ocupado, setOcupado] = useState<'guardando' | 'removendo' | null>(null)
  const [erro, setErro] = useState<string | null>(null)
  const [digitada, setDigitada] = useState(false)
  const campo = useRef<HTMLInputElement>(null)
  const botaoTrocar = useRef<HTMLButtonElement>(null)
  const botaoRemover = useRef<HTMLButtonElement>(null)
  const botaoCancelar = useRef<HTMLButtonElement>(null)
  const focarDepois = useRef<Foco | null>(null)
  const id = `cfg-chave-${provedor}`

  // O foco vai para o controle que ocupa o lugar do que a pessoa acabou de
  // usar. Se ele ainda não existe (a tela espera o estado novo da conta), o
  // pedido fica para o próximo render.
  useEffect(() => {
    const alvo = focarDepois.current
    if (alvo === null) return
    const el = { campo, trocar: botaoTrocar, remover: botaoRemover, cancelar: botaoCancelar }[alvo].current
    if (!el) return
    focarDepois.current = null
    el.focus()
  }, [modo, chave.configurada])

  const colando = !chave.configurada || modo === 'colar'

  function abrirColar() {
    if (ocupado) return
    setErro(null)
    focarDepois.current = 'campo'
    setModo('colar')
  }

  function fecharColar() {
    if (ocupado) return
    if (campo.current) campo.current.value = ''
    setDigitada(false)
    setErro(null)
    focarDepois.current = 'trocar'
    setModo('ver')
  }

  function perguntarRemover() {
    if (ocupado) return
    setErro(null)
    focarDepois.current = 'cancelar'
    setModo('remover')
  }

  function cancelarRemover() {
    if (ocupado) return
    focarDepois.current = 'remover'
    setModo('ver')
  }

  async function guardar(depoisDoAceite = false): Promise<void> {
    const valor = campo.current?.value.trim() ?? ''
    if (valor === '' || (ocupado !== null && !depoisDoAceite)) return
    setOcupado('guardando')
    setErro(null)
    try {
      await cliente.guardarChave(provedor, valor)
    } catch (e) {
      if (e instanceof SemSessao || e instanceof ForaDoAr) {
        setOcupado(null)
        aoPerderLigacao(e)
        return
      }
      // O servidor diz que falta o aceite (acabou em outro aparelho): a folha
      // abre, e a chave, que continua no campo, segue se a pessoa aceitar.
      if (e instanceof ErroDaApi && e.codigo === 'SEM_ACEITE' && !depoisDoAceite) {
        let aceitou = false
        try {
          aceitou = await aoSemAceite()
        } catch (e2) {
          if (e2 instanceof SemSessao || e2 instanceof ForaDoAr) {
            setOcupado(null)
            aoPerderLigacao(e2)
            return
          }
        }
        if (aceitou) return guardar(true)
        setOcupado(null)
        return
      }
      setOcupado(null)
      setErro(fraseDaChave(e))
      return
    }
    // Guardada: o campo esvazia, e a chave não fica em lugar nenhum da tela.
    if (campo.current) campo.current.value = ''
    setDigitada(false)
    focarDepois.current = 'trocar'
    try {
      await aoMudar()
    } finally {
      setOcupado(null)
    }
    setModo('ver')
    avisar('Chave guardada.')
  }

  async function remover() {
    if (ocupado) return
    setOcupado('removendo')
    setErro(null)
    try {
      await cliente.removerChave(provedor)
    } catch (e) {
      setOcupado(null)
      if (e instanceof SemSessao || e instanceof ForaDoAr) aoPerderLigacao(e)
      else setErro('Não deu para remover a chave.')
      return
    }
    focarDepois.current = 'campo'
    try {
      await aoMudar()
    } finally {
      setOcupado(null)
    }
    setModo('ver')
    avisar('Chave removida.')
  }

  function teclaNoCampo(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      void guardar()
    } else if (e.key === 'Escape' && chave.configurada) {
      e.preventDefault()
      fecharColar()
    }
  }

  return (
    <div className="ajuste">
      <p className="ajuste-nome" id={`${id}-nome`}>
        Chave da {nome}
      </p>
      <p className="estado-chave" id={`${id}-estado`}>
        {chave.configurada ? (
          <>
            Chave terminada em <span className="final">{chave.final}</span>.
          </>
        ) : chave.colar_de_novo ? (
          // Havia uma chave e o servidor já não a decifra (a chave de cifra
          // trocou, ou o backup voltou sem ela): a pessoa cola de novo.
          'A chave guardada não vale mais neste servidor. Cole a chave de novo.'
        ) : (
          'Nenhuma chave guardada.'
        )}
      </p>

      {modo === 'remover' ? (
        <div
          className="confirmar"
          role="group"
          aria-labelledby={`${id}-remover`}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault()
              cancelarRemover()
            }
          }}
        >
          <p className="ajuste-nome" id={`${id}-remover`}>
            Remover a chave da {nome}
          </p>
          <p className="ajuda">{lembrete}</p>
          <div className="acoes">
            <button type="button" className="botao risco" aria-disabled={ocupado !== null || undefined} onClick={() => void remover()}>
              {ocupado === 'removendo' ? 'Removendo…' : 'Remover'}
            </button>
            <button ref={botaoCancelar} type="button" className="botao" aria-disabled={ocupado !== null || undefined} onClick={cancelarRemover}>
              Cancelar
            </button>
          </div>
        </div>
      ) : colando ? (
        <>
          {chave.configurada && <p className="ajuda">{lembrete}</p>}
          <div className="com-botao">
            <input
              ref={campo}
              className="entrada"
              type="password"
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              placeholder="Cole a chave aqui"
              aria-labelledby={`${id}-nome`}
              aria-describedby={`${id}-estado ${id}-ajuda`}
              onInput={(e) => setDigitada(e.currentTarget.value.trim() !== '')}
              onKeyDown={teclaNoCampo}
            />
            <button type="button" className="botao" aria-disabled={!digitada || ocupado !== null || undefined} onClick={() => void guardar()}>
              {ocupado === 'guardando' ? 'Guardando…' : 'Guardar chave'}
            </button>
            {chave.configurada && (
              <button type="button" className="botao" aria-disabled={ocupado !== null || undefined} onClick={fecharColar}>
                Cancelar
              </button>
            )}
          </div>
          <OndeCriarAChave pagina={paginasDeChaveDoApp[provedor]} paraQue="o celular" id={`${id}-ajuda`} />
        </>
      ) : (
        <div className="acoes">
          <button ref={botaoTrocar} type="button" className="botao" aria-label={`Trocar a chave da ${nome}`} onClick={abrirColar}>
            Trocar
          </button>
          <button ref={botaoRemover} type="button" className="botao" aria-label={`Remover a chave da ${nome}`} onClick={perguntarRemover}>
            Remover
          </button>
        </div>
      )}

      {erro && (
        <p className="nota" role="alert">
          {erro}
        </p>
      )}
    </div>
  )
}
