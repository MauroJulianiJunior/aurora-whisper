import { useEffect, useState } from 'react'
import { type Buscador, buscadorPadrao } from './api'
import type { Ambiente } from './ambiente'
import { ambienteLocal } from './ambienteLocal'
import { Aurora } from './Aurora'
import { criarCliente } from './clienteLocal'
import { Painel } from './Painel'

// O App do modo local (o painel que o Agent serve no PC). O da nuvem é
// nuvem/AppNuvem.tsx, em outro build (ADR-019, decisão 12).

// A tela cheia de bloqueio é só para a sessão que nunca existiu: a troca
// do bilhete falhou. Sessão perdida ou Agent fora do ar NO MEIO do uso
// ficam com o Painel, que mostra uma faixa e mantém na tela o que a pessoa
// escreveu (revisão de 29-set-2026: trocar a tela inteira desmontava o
// Painel e levava junto os rascunhos).
type Estado = { tipo: 'iniciando' } | { tipo: 'pronto'; ambiente: Ambiente } | { tipo: 'sem-sessao' }

interface Props {
  // sessao é a troca do bilhete, começada uma vez, fora do React (main.tsx).
  sessao: Promise<string>
  buscar?: Buscador
}

export default function App({ sessao, buscar = buscadorPadrao }: Props) {
  const [estado, setEstado] = useState<Estado>({ tipo: 'iniciando' })

  useEffect(() => {
    let vivo = true
    sessao.then(
      (token) => vivo && setEstado({ tipo: 'pronto', ambiente: ambienteLocal(criarCliente(token, buscar)) }),
      // Qualquer falha na troca termina no mesmo lugar: o bilhete já saiu
      // da URL e não vale de novo, então só a bandeja abre outra sessão.
      () => vivo && setEstado({ tipo: 'sem-sessao' }),
    )
    return () => {
      vivo = false
    }
  }, [sessao, buscar])

  switch (estado.tipo) {
    case 'iniciando':
      return null
    case 'pronto':
      return <Painel ambiente={estado.ambiente} />
    case 'sem-sessao':
      // Quem fala aqui é a Aurora: é a única tela em que o painel não tem
      // nada de si para mostrar, só o caminho de volta.
      return (
        <main className="bloqueio">
          <Aurora tamanho={96} />
          <h1>Abra o painel pela bandeja</h1>
          <p>
            Por segurança, o acesso ao histórico vale só para a aba que o Aurora Whisper abriu, e termina quando a página é recarregada ou
            quando o programa reinicia.
          </p>
          <p>
            Clique no ícone do Aurora Whisper na bandeja do Windows e escolha <strong>Abrir Aurora Whisper</strong>.
          </p>
        </main>
      )
  }
}
