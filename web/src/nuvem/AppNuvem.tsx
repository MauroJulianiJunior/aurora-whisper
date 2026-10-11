// O app da nuvem (ADR-019, F4): o mesmo Painel do PC, aberto pelo navegador
// (e, desde a F5, instalável no celular), falando com a /api/v1 da própria origem
// pelo cookie da sessão. Só o build da nuvem chega aqui (nuvem/main.tsx).
//
// A abertura pergunta quem está entrando (GET /api/v1/me), com "Abrindo…"
// se a resposta demorar:
//   - 200: o Painel;
//   - 401: Entrar;
//   - sem rede: Sem conexão;
//   - outro erro: Não abriu.
// No meio do uso, um 401 troca a tela inteira por "Sua sessão terminou" — a
// não ser que haja um texto não salvo na tela: aí o Painel fica, com a faixa
// da sessão, e a pessoa entra de novo numa janela à parte, sem perder o
// texto (EntrarDeNovo.tsx). A rede que cai acende a faixa do topo, com a
// lista no lugar.
//
// A mesma página é também a janela à parte de entrar (?janela=entrar e
// ?janela=entrou; entrar.ts): aí ela não abre o histórico. E é a página de
// aprovação de um PC (/device/authorize?pedido=…; F6, AprovarPC.tsx): com a
// sessão, a aprovação no lugar do Painel; sem ela, Entrar, que volta ao
// mesmo endereço depois do login.

import { useCallback, useEffect, useState } from 'react'
import { type Buscador, ForaDoAr, SemSessao, buscadorPadrao } from '../api'
import type { Ambiente } from '../ambiente'
import { Painel } from '../Painel'
import { ambienteNuvem } from './ambienteNuvem'
import { AprovarPC } from './AprovarPC'
import { type ClienteNuvem, type Eu, criarClienteNuvem, criarPortao, pareceIdDePedido } from './cliente'
import type { Plataforma } from './ditar/gravador'
import { abrirJanelaPadrao, avisarQueEntrou, modoDaJanela, useLoginDeTeste } from './entrar'
import { JanelaDeEntrar } from './EntrarDeNovo'
import { Abrindo, Entrar, NaoAbriu, SemConexao } from './Telas'

type Tela =
  | { tipo: 'abrindo' }
  | { tipo: 'entrar'; expirada: boolean }
  | { tipo: 'sem-conexao' }
  | { tipo: 'nao-abriu' }
  // focar: o Painel entra no lugar de uma tela cujo botão tinha o foco.
  | { tipo: 'pronto'; ambiente: Ambiente; focar: boolean }
  // aprovar: a página de aprovação de um PC, com a sessão e o e-mail da conta
  // que aprova (o PC fica ligado a ela).
  | { tipo: 'aprovar'; email: string }

interface Props {
  buscar?: Buscador
  // irPara navega para o login do Google (os testes trocam).
  irPara?(url: string): void
  // host: o nome do servidor na barra de endereço (os testes trocam).
  host?: string
  // busca: o ?... do endereço, que diz se a página é a janela de entrar
  // (os testes trocam).
  busca?: string
  // caminho: o caminho do endereço; /device/authorize é a aprovação de um
  // PC (os testes trocam).
  caminho?: string
  // abrirJanela: abre a janela à parte de entrar; false quando o navegador
  // a bloqueou (os testes trocam).
  abrirJanela?(url: string): boolean
  // A janela de entrar: trocarPara vai ao Google sem deixar a página no
  // histórico; avisarQueEntrou e fecharJanela são a volta (os testes trocam).
  // A aprovação de um PC usa o mesmo trocarPara para ir ao retorno do PC.
  trocarPara?(url: string): void
  avisarQueEntrou?(): void
  fecharJanela?(): void
  // plataformaDeDitar: o microfone e o resto do navegador do gravador do
  // ditado (ADR-021). Os testes e as telas de desenvolvimento trocam; sem ela,
  // é o navegador de verdade.
  plataformaDeDitar?: Plataforma
  // recarregar lê o app de novo: é o que a folha do aceite faz quando o servidor
  // diz que o aviso que ela mostrou não é o atual (AVISO_DESATUALIZADO). Os
  // testes trocam.
  recarregar?(): void
}

const irParaPadrao = (url: string) => window.location.assign(url)
const trocarParaPadrao = (url: string) => window.location.replace(url)
const fecharJanelaPadrao = () => window.close()

// telaDoErro: para onde a abertura vai quando o /me não deu 200.
function telaDoErro(e: unknown): Tela {
  if (e instanceof SemSessao) return { tipo: 'entrar', expirada: false }
  if (e instanceof ForaDoAr) return { tipo: 'sem-conexao' }
  return { tipo: 'nao-abriu' }
}

// O caminho da página de aprovação de um PC (internal/server, CaminhoDeAprovar).
const caminhoDeAprovar = '/device/authorize'

export default function AppNuvem({
  busca = window.location.search,
  caminho = window.location.pathname,
  trocarPara = trocarParaPadrao,
  avisarQueEntrou: avisar = avisarQueEntrou,
  fecharJanela = fecharJanelaPadrao,
  ...props
}: Props) {
  const modo = modoDaJanela(busca)
  if (modo) return <JanelaDeEntrar modo={modo} buscar={props.buscar ?? buscadorPadrao} trocarPara={trocarPara} avisar={avisar} fechar={fecharJanela} />
  // A aprovação de um PC: o pedido vem do endereço.
  const pedido = caminho === caminhoDeAprovar ? (new URLSearchParams(busca).get('pedido') ?? '') : null
  return <AppDaConta {...props} pedido={pedido} trocarPara={trocarPara} />
}

function AppDaConta({
  buscar = buscadorPadrao,
  irPara = irParaPadrao,
  host = window.location.hostname,
  abrirJanela = abrirJanelaPadrao,
  plataformaDeDitar,
  recarregar,
  pedido,
  trocarPara,
}: Pick<Props, 'buscar' | 'irPara' | 'host' | 'abrirJanela' | 'plataformaDeDitar' | 'recarregar'> & { pedido: string | null; trocarPara(url: string): void }) {
  const [cliente] = useState<ClienteNuvem>(() => criarClienteNuvem(buscar))
  const [tela, setTela] = useState<Tela>({ tipo: 'abrindo' })
  const [tentando, setTentando] = useState(false)
  // O login de teste é perguntado uma vez, quando Entrar aparece. Um 404
  // (todo servidor de verdade) é null, e nenhum botão a mais aparece.
  const teste = useLoginDeTeste(cliente, host, tela.tipo === 'entrar')

  // pronto monta o ambiente uma vez por entrada: o Painel o recebe estável.
  // O cliente do Painel passa pelo portão da entrada, que a faixa da sessão
  // fecha enquanto espera a pessoa entrar de novo (cliente.ts, Portao); o
  // dela não passa.
  const pronto = useCallback(
    (eu: Eu, focar: boolean): Tela => {
      if (pedido !== null) return { tipo: 'aprovar', email: eu.email }
      const portao = criarPortao()
      return {
        tipo: 'pronto',
        ambiente: ambienteNuvem(criarClienteNuvem(portao.passagem(buscar)), eu, {
          aoSemSessao: () => setTela({ tipo: 'entrar', expirada: true }),
          aoSair: () => setTela({ tipo: 'entrar', expirada: false }),
          reentrada: { cliente, eu, portao, host, abrirJanela, irPara },
          plataformaDeDitar,
          recarregar,
        }),
        focar,
      }
    },
    [cliente, buscar, host, abrirJanela, irPara, pedido, plataformaDeDitar, recarregar],
  )

  useEffect(() => {
    let vivo = true
    cliente.eu().then(
      (eu) => vivo && setTela(pronto(eu, false)),
      (e: unknown) => vivo && setTela(telaDoErro(e)),
    )
    return () => {
      vivo = false
    }
  }, [cliente, pronto])

  function tentarDeNovo() {
    if (tentando) return
    setTentando(true)
    cliente
      .eu()
      .then(
        (eu) => setTela(pronto(eu, true)),
        (e: unknown) => setTela(telaDoErro(e)),
      )
      .finally(() => setTentando(false))
  }

  // A volta do login: a página de aprovação, quando é ela (um caminho
  // desta origem, que o servidor confere com o ValidarVolta).
  const volta = pedido !== null && pareceIdDePedido(pedido) ? `${caminhoDeAprovar}?pedido=${pedido}` : undefined

  async function entrarComGoogle() {
    irPara(await cliente.inicioDoGoogle(volta))
  }

  async function entrarComContaDeTeste(conta: string) {
    await cliente.entrarComContaDeTeste(conta)
    setTela(pronto(await cliente.eu(), true))
  }

  switch (tela.tipo) {
    case 'abrindo':
      return <Abrindo />
    case 'pronto':
      return <Painel ambiente={tela.ambiente} focarNoTitulo={tela.focar} />
    case 'aprovar':
      return <AprovarPC cliente={cliente} pedido={pedido ?? ''} conta={tela.email} aoSemSessao={() => setTela({ tipo: 'entrar', expirada: true })} trocarPara={trocarPara} />
    case 'entrar':
      return (
        <Entrar
          key={tela.expirada ? 'expirada' : 'entrar'}
          expirada={tela.expirada}
          aprovar={pedido !== null}
          teste={teste}
          entrarComGoogle={entrarComGoogle}
          entrarComContaDeTeste={entrarComContaDeTeste}
        />
      )
    case 'sem-conexao':
      return <SemConexao tentando={tentando} aoTentar={tentarDeNovo} />
    case 'nao-abriu':
      return <NaoAbriu tentando={tentando} aoTentar={tentarDeNovo} />
  }
}
