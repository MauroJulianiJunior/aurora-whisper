import { useEffect, useRef, useState } from 'react'
import type { PropsDoAvisoDoMicrofone } from './ambiente'
import type { Cliente, EstadoDoMicrofone, MotivoDoBloqueio } from './clienteLocal'

// O aviso do topo quando o Windows bloqueia o microfone do Agent. Quem instala
// o app com a chave de privacidade desligada não vê nada: o ditado não ouve, e
// a pílula só diz que não deu. O app não liga a permissão sozinho; este aviso
// diz o que fazer e o botão abre a tela certa do Windows. Só o PC o tem
// (ambienteLocal.tsx): a nuvem não lê o registro de ninguém.
//
// Consulta ao montar e de novo quando a aba volta a ter foco ou fica visível, e
// é aí que o aviso some sozinho: a pessoa liga a chave, volta para esta aba, e
// a resposta vem liberada. A leitura que falha não diz nada (o Agent
// reiniciando, um Agent de antes do aviso): a faixa do topo é quem avisa que a
// ligação caiu.

interface Props extends PropsDoAvisoDoMicrofone {
  cliente: Pick<Cliente, 'microfone' | 'abrirConfiguracoesDoMicrofone'>
}

// O caminho à mão, para quando o botão não consegue abrir a tela.
const caminhoManual = 'Abra o menu Iniciar > Configurações > Privacidade e segurança > Microfone.'

// textoDoMotivo diz o que fazer, no vocabulário da tela do Windows. Aparelho e
// conta têm a mesma chave (a do topo daquela tela); uma palavra que este painel
// não conhece (um Agent mais novo) cai nela também.
function textoDoMotivo(motivo: MotivoDoBloqueio) {
  switch (motivo) {
    case 'empresa':
      return 'Quem administra este computador bloqueou o microfone. Só essa pessoa consegue liberar.'
    case 'programas':
      return (
        <>
          Sem ele, o ditado não ouve nada. Clique no botão, ligue a chave <strong>Permitir que aplicativos da área de trabalho acessem o microfone</strong> e
          volte para esta aba.
        </>
      )
    case 'este_app':
      return 'Sem ele, o ditado não ouve nada. Clique no botão, procure o Aurora Whisper na lista, ligue a chave dele e volte para esta aba.'
    default:
      return (
        <>
          Sem ele, o ditado não ouve nada. Clique no botão, ligue a chave <strong>Acesso ao microfone</strong> e volte para esta aba.
        </>
      )
  }
}

export function AvisoDoMicrofone({ cliente, avisar }: Props) {
  const [estado, setEstado] = useState<EstadoDoMicrofone | null>(null)
  // estavaBloqueado: o último que o Agent disse era bloqueio; é o que faz a
  // próxima resposta liberada dizer "Microfone liberado".
  const estavaBloqueado = useRef(false)

  useEffect(() => {
    const ctrl = new AbortController()
    // noAr é desta montagem: a que o cleanup aborta não segura a seguinte.
    let noAr = false
    function consultar() {
      // "focus" e "visibilitychange" costumam chegar juntos: uma leitura só.
      if (noAr) return
      noAr = true
      cliente
        .microfone(ctrl.signal)
        .then(
          (e) => {
            if (ctrl.signal.aborted) return
            if (e.bloqueado) {
              estavaBloqueado.current = true
              setEstado(e)
              return
            }
            if (estavaBloqueado.current) {
              estavaBloqueado.current = false
              avisar('Microfone liberado. Pode ditar.')
            }
            setEstado(null)
          },
          () => {
            // Sem resposta: o aviso fica como estava.
          },
        )
        .finally(() => {
          noAr = false
        })
    }
    function voltou() {
      if (document.visibilityState !== 'hidden') consultar()
    }
    consultar()
    window.addEventListener('focus', voltou)
    document.addEventListener('visibilitychange', voltou)
    return () => {
      ctrl.abort()
      window.removeEventListener('focus', voltou)
      document.removeEventListener('visibilitychange', voltou)
    }
  }, [cliente, avisar])

  if (!estado?.bloqueado) return null
  // Só quem administra o computador libera: sem botão, que não leva a nada.
  const daEmpresa = estado.motivo === 'empresa'

  function abrir() {
    cliente.abrirConfiguracoesDoMicrofone().catch(() => avisar(`Não consegui abrir as configurações. ${caminhoManual}`))
  }

  return (
    <div className="faixa" role="status">
      <p>
        <strong>O Windows está bloqueando o microfone.</strong> {textoDoMotivo(estado.motivo)}
      </p>
      {!daEmpresa && (
        <button type="button" className="aviso-acao" onClick={abrir}>
          Abrir as configurações do microfone
        </button>
      )}
    </div>
  )
}
