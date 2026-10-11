// A porta antes do microfone (ADR-021, decisão H e "O caminho de um ditado", 1):
// no toque no microfone, antes de o getUserMedia abrir e de qualquer pedido de
// chave, confere na conta se o aviso foi aceito para os provedores das
// preferências. Sem o aceite, mostra a folha; sem o "Aceito", o microfone nem
// abre, e nada é enviado ao servidor além da leitura do estado.
//
// Depois da folha, o gravador não começa sozinho: a pessoa acabou de ler um
// aviso longo, e um microfone que abre sem outro toque a pegaria de surpresa.
// Ela toca de novo. Com o aceite e a chave já na conta (o caso de todo ditado
// depois do primeiro), a porta passa direto, e a conferência não se repete
// nesta página: o ditado sem rede continua gravando (o áudio espera o "Tentar
// de novo"), e uma recusa do servidor no envio (SEM_ACEITE, SEM_CHAVE) faz a
// porta conferir de novo.

import type { ReactNode } from 'react'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import type { SecaoDasConfiguracoes } from '../../ambiente'
import type { ClienteNuvem } from '../cliente'
import { nomeDoProvedor } from './aviso'
import { type EstadoDoDitado, aceitoPara, provedoresExigidos } from './estado'
import type { Folha } from './folha'

// GanchosDaPorta é o que a porta pede ao Painel: o aviso da pílula, as
// Configurações na seção do ditado e a sessão que acabou.
export interface GanchosDaPorta {
  avisar(texto: ReactNode): void
  abrirConfiguracoes(secao: SecaoDasConfiguracoes): void
  aoSemSessao(): void
}

export interface Porta {
  // liberar: true se o microfone pode abrir agora.
  liberar(g: GanchosDaPorta): Promise<boolean>
  // reabrirAceite abre a folha de novo porque o servidor recusou o ditado por
  // falta de aceite (SEM_ACEITE). true se a pessoa aceitou.
  reabrirAceite(g: GanchosDaPorta): Promise<boolean>
  // invalidar: o que a porta sabia deixou de valer (uma preferência mudou, o
  // servidor recusou o ditado). A próxima conferência vai à conta.
  invalidar(): void
}

function falhar(e: unknown, g: GanchosDaPorta) {
  if (e instanceof SemSessao) g.aoSemSessao()
  else if (e instanceof ForaDoAr) g.avisar('Sem conexão.')
  else if (e instanceof ErroDaApi && e.status === 404) g.avisar('Este servidor não tem o ditado pelo celular.')
  else g.avisar('Não deu para abrir o ditado. Tente de novo.')
}

export function criarPorta(cliente: Pick<ClienteNuvem, 'estadoDoDitado' | 'salvarPreferencias'>, folha: Folha): Porta {
  let liberado = false

  const pedido = (estado: EstadoDoDitado, escolherVoz: boolean) => ({
    voz: estado.preferencias.provedor,
    polish: estado.preferencias.polish ? estado.preferencias.polish_provedor : undefined,
    escolherVoz,
  })

  return {
    invalidar() {
      liberado = false
    },

    async liberar(g) {
      if (liberado) return true
      let estado: EstadoDoDitado
      try {
        estado = await cliente.estadoDoDitado()
      } catch (e) {
        falhar(e, g)
        return false
      }

      let aceitou = false
      if (!aceitoPara(estado, provedoresExigidos(estado.preferencias))) {
        const r = await folha.pedirAceite(pedido(estado, true))
        if (!r) return false
        aceitou = true
        try {
          // O provedor que ela escolheu na folha passa a ser o das preferências
          // (o aceite já o inclui: o PUT só recusa um provedor fora dele).
          if (r.voz !== estado.preferencias.provedor) {
            await cliente.salvarPreferencias({ ...estado.preferencias, provedor: r.voz, modelo: '' })
          }
          estado = await cliente.estadoDoDitado()
        } catch (e) {
          falhar(e, g)
          return false
        }
      }

      // A chave só é pedida depois do aceite, e é colada nas Configurações, no
      // lugar de sempre: o app leva a pessoa até lá. A que o servidor guardava e
      // já não decifra (colar_de_novo) é colada de novo, no mesmo lugar.
      const semChave = provedoresExigidos(estado.preferencias).find((p) => !estado.chaves[p].configurada)
      if (semChave) {
        const nome = nomeDoProvedor[semChave]
        g.avisar(estado.chaves[semChave].colar_de_novo ? `Cole a chave da ${nome} de novo nas Configurações.` : `Falta a chave da ${nome}. Cole a chave nas Configurações.`)
        g.abrirConfiguracoes('ditar')
        return false
      }
      if (aceitou) {
        g.avisar('Aceito. Toque no microfone para ditar.')
        return false
      }
      liberado = true
      return true
    },

    async reabrirAceite(g) {
      liberado = false
      let estado: EstadoDoDitado
      try {
        estado = await cliente.estadoDoDitado()
      } catch (e) {
        falhar(e, g)
        return false
      }
      return (await folha.pedirAceite(pedido(estado, false))) !== null
    },
  }
}
