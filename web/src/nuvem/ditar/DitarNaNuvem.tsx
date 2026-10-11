// O microfone do app da nuvem com a porta do aceite (ADR-021, decisão H): o
// gravador (Ditar.tsx) só abre o microfone depois de a porta dizer que o aviso
// foi aceito e que há chave, e a folha do aceite mora aqui, ao lado dele: é o
// único componente do ditado que o Painel monta sempre, e as Configurações
// pedem a mesma folha por ele (folha.ts).

import { type ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef } from 'react'
import { ErroDaApi } from '../../api'
import type { PropsDoDitar, SecaoDasConfiguracoes } from '../../ambiente'
import type { ClienteNuvem } from '../cliente'
import { Ditar } from './Ditar'
import { FolhaDoAceite } from './FolhaDoAceite'
import type { Folha } from './folha'
import type { Plataforma } from './gravador'
import type { Porta } from './porta'

interface Props extends PropsDoDitar {
  cliente: ClienteNuvem
  plataforma?: Plataforma
  folha: Folha
  porta: Porta
}

// As recusas do servidor que mostram que a porta sabia errado: a conferência
// seguinte vai à conta.
const recusasDaPorta = new Set(['SEM_ACEITE', 'SEM_CHAVE', 'PROVEDOR_RECUSOU_CHAVE'])

export function DitarNaNuvem({ cliente, plataforma, folha, porta, ...ganchosDoPainel }: Props) {
  // Os ganchos do Painel mudam a cada render dele; a porta e a folha falam com
  // a versão de agora.
  const ganchos = useRef(ganchosDoPainel)
  useLayoutEffect(() => {
    ganchos.current = ganchosDoPainel
  })

  // Um 401 no aceite fecha a folha e acende a faixa da sessão.
  useEffect(() => folha.ligar(() => ganchos.current.aoSemSessao()), [folha])

  const daPorta = useCallback(
    () => ({
      avisar: (texto: ReactNode) => ganchos.current.avisar(texto),
      abrirConfiguracoes: (secao: SecaoDasConfiguracoes) => ganchos.current.abrirConfiguracoes(secao),
      aoSemSessao: () => ganchos.current.aoSemSessao(),
    }),
    [],
  )
  const liberar = useCallback(() => porta.liberar(daPorta()), [porta, daPorta])
  const reabrirAceite = useCallback(() => porta.reabrirAceite(daPorta()), [porta, daPorta])

  // O cliente do gravador: o envio recusado por falta de aceite, de chave ou
  // pela chave que o provedor não aceita faz a porta conferir de novo.
  const doGravador = useMemo<ClienteNuvem>(
    () => ({
      ...cliente,
      ditar: async (pedido, sinal) => {
        try {
          return await cliente.ditar(pedido, sinal)
        } catch (e) {
          if (e instanceof ErroDaApi && e.codigo && recusasDaPorta.has(e.codigo)) porta.invalidar()
          throw e
        }
      },
    }),
    [cliente, porta],
  )

  return (
    <>
      <Ditar cliente={doGravador} plataforma={plataforma} liberar={liberar} reabrirAceite={reabrirAceite} {...ganchosDoPainel} />
      <FolhaDoAceite folha={folha} />
    </>
  )
}
