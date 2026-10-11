// A página de aprovação de um PC (ADR-019, "Conectar o PC"; F6): o Agent abre
// /device/authorize?pedido=… no navegador, a pessoa (com a sessão) vê o nome
// do PC, o código de conferência, o mesmo do painel do PC, e a conta a que o PC
// vai ficar ligado (quem está logado na conta errada vê antes), e aprova. Aprovar
// é um POST por fetch, que devolve o endereço do retorno ao 127.0.0.1 do PC, e
// a página vai para lá com location.replace (o Voltar não volta à aprovação).
// O code vai ao PC, e só o Agent que começou o pedido, com o verificador do
// PKCE, o troca pelo token: um link de aprovação mandado por outra pessoa não
// dá nada a ela. O código curto é só conferência.
//
// Por que fetch e não formulário: o servidor manda form-action 'self' na CSP,
// e o Chromium aplica a diretiva ao redirecionamento de um formulário; com o
// fetch, a CSP fica como está, e o erro aparece aqui, na própria tela.

import { useEffect, useEffectEvent, useState } from 'react'
import { ErroDaApi, ForaDoAr, SemSessao } from '../api'
import { type ClienteNuvem, type PedidoDePareamento, pareceIdDePedido } from './cliente'
import { Abrindo, Tela } from './Telas'

interface Props {
  cliente: ClienteNuvem
  // pedido: o ?pedido= do endereço.
  pedido: string
  // conta: o e-mail da conta que aprova. A página o mostra antes do Aprovar:
  // o PC fica ligado a ela, e o painel do PC recusa trocar de conta depois
  // (a D3 da ADR-019).
  conta: string
  // aoSemSessao: a sessão acabou no meio; o app volta a Entrar, com a volta
  // para esta página.
  aoSemSessao(): void
  // trocarPara: vai ao retorno sem deixar a aprovação no histórico (os
  // testes trocam).
  trocarPara(url: string): void
}

type Estado =
  | { tipo: 'carregando' }
  | { tipo: 'pronto'; p: PedidoDePareamento }
  | { tipo: 'vencido' }
  | { tipo: 'nao-abriu' }
  | { tipo: 'recusado' }

const nomeDaPlataforma = (p: string) => (p === 'windows' ? 'Windows' : p)

export function AprovarPC({ cliente, pedido, conta, aoSemSessao, trocarPara }: Props) {
  const [estado, setEstado] = useState<Estado>(() => (pareceIdDePedido(pedido) ? { tipo: 'carregando' } : { tipo: 'vencido' }))
  const [aprovando, setAprovando] = useState(false)
  const [erro, setErro] = useState<{ texto: string; vez: number } | null>(null)
  const semSessao = useEffectEvent(aoSemSessao)

  useEffect(() => {
    if (!pareceIdDePedido(pedido)) return
    let vivo = true
    cliente.pedidoDePareamento(pedido).then(
      (p) => vivo && setEstado({ tipo: 'pronto', p }),
      (e: unknown) => {
        if (!vivo) return
        if (e instanceof SemSessao) semSessao()
        else if (e instanceof ErroDaApi && e.status === 404) setEstado({ tipo: 'vencido' })
        else setEstado({ tipo: 'nao-abriu' })
      },
    )
    return () => {
      vivo = false
    }
  }, [cliente, pedido])

  async function aprovar() {
    if (aprovando) return
    setAprovando(true)
    setErro(null)
    let retorno: string
    try {
      retorno = await cliente.aprovarPedido(pedido)
    } catch (e) {
      setAprovando(false)
      if (e instanceof SemSessao) return aoSemSessao()
      if (e instanceof ErroDaApi && e.status === 404) return setEstado({ tipo: 'vencido' })
      const texto = e instanceof ForaDoAr ? 'Sem conexão. Tente de novo.' : 'Não deu para aprovar. Tente de novo.'
      setErro((a) => ({ texto, vez: (a?.vez ?? 0) + 1 }))
      return
    }
    // A página vai embora; o botão fica em "Aprovando…" até lá.
    trocarPara(retorno)
  }

  switch (estado.tipo) {
    case 'carregando':
      return <Abrindo />
    case 'vencido':
      return <Tela titulo="Este pedido venceu ou já foi usado." linha="Volte ao PC e clique em Conectar à nuvem de novo." />
    case 'nao-abriu':
      return <Tela titulo="Não deu para abrir." linha="Recarregue a página daqui a pouco." />
    case 'recusado':
      return <Tela titulo="Nada foi conectado." linha="Pode fechar esta aba." />
    case 'pronto': {
      const { p } = estado
      return (
        <Tela titulo="Conectar este PC?" linha={`${p.nome} · ${nomeDaPlataforma(p.plataforma)}`}>
          <div className="aprovar">
            <p className="aprovar-codigo">
              Código <strong className="codigo">{p.conferencia}</strong>
            </p>
            <p className="ajuda">Confira se é o mesmo do painel do PC.</p>
            <p className="aprovar-conta">
              Este PC vai ficar ligado à conta <strong>{conta}</strong>.
            </p>
            <p className="aprovar-aviso">Só aprove se você clicou em Conectar neste PC agora.</p>
          </div>
          <div className="tela-acoes">
            <button type="button" className="botao primario" aria-disabled={aprovando || undefined} onClick={() => void aprovar()}>
              {aprovando ? 'Aprovando…' : 'Aprovar'}
            </button>
            <button type="button" className="botao" aria-disabled={aprovando || undefined} onClick={() => !aprovando && setEstado({ tipo: 'recusado' })}>
              Agora não
            </button>
            {erro && (
              <p className="nota" role="alert" key={erro.vez}>
                {erro.texto}
              </p>
            )}
          </div>
        </Tela>
      )
    }
  }
}
