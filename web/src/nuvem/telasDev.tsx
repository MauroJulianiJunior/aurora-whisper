// Só para ver as telas do app da nuvem no `pnpm dev`, sem Docker, sem Google e
// sem o servidor de verdade (ADR-019, F8h): o app inteiro contra a nuvem de
// mentira dos testes (testes/servidorFalsoNuvem.ts), em memória. Não faz parte
// de nenhum build: nenhum index.html o importa (o `vite build` só lê o
// index.html), e o bundle.test.ts confere os dois.
//
//   pnpm -C web dev      e abrir      /telas-dev-nuvem.html?cenario=lista
//
// cenario: lista (padrão), vazio, teto (passou do teto do servidor), erro (a
// lista não carrega) ou longa (variante e domínio compridos).
// A tela de Configurações abre sozinha; ?config=0 deixa no Histórico.
//
// O ditado do celular (ADR-021, C6a), no Histórico, com um microfone de mentira
// (testes/plataformaFalsa.ts) que fala sozinho, num relógio parado: cada
// cenário toca no microfone, deixa 6,6 s de fala sintética passarem e fica
// onde a tela interessa.
//   gravando   a faixa da onda, o tempo e o Cancelar, no meio da fala;
//   enviando   parou, e a faixa diz "Transcrevendo" (o servidor não responde);
//   erro-rede  sem conexão: a nota, com Tentar de novo e Descartar;
//   sessao     a sessão acabou com o ditado na memória: a faixa e a nota;
//   chegou     o ditado novo no topo, em destaque, com a etiqueta e o Copiar.
//
// O aceite e as Configurações do ditado (ADR-021, C6b), numa conta sem aceite
// nem chave (a nuvem de mentira, em memória; nada sai da máquina):
//   aceite           o toque no microfone abre a folha do aceite (a escolha do
//                    provedor da voz, o aviso, "Aceito" e "Agora não");
//   ditar-desligado  as Configurações, com "Ditar pelo celular" desligado;
//   ditar-ligado     as Configurações, ligado, com a chave da OpenAI guardada;
//   ditar-polish     as Configurações, com o Polish na Anthropic e a chave dela
//                    por colar.
//
// Claro e escuro seguem o sistema do navegador (no DevTools: Rendering,
// "prefers-color-scheme"); 320 e 375 px, no modo de dispositivo.

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '../fontes.css'
import '../index.css'
import { VERSAO_DO_AVISO } from './avisoDoDitado'
import { plataformaFalsa } from './ditar/plataformaFalsa'
import { type OpcoesDaNuvem, aprendido, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

const busca = new URLSearchParams(window.location.search)
const cenario = busca.get('cenario') ?? 'lista'
const hora = (dia: number, h: number) => new Date(2026, 9, dia, h).toISOString()

const trocas = [
  aprendido({ variante: 'n oito n', termo: 'n8n' }),
  aprendido({ variante: 'dctf web', termo: 'DCTFWeb', dominio: 'contabilidade' }),
  aprendido({ variante: 'e c f', termo: 'ECF', dominio: 'contabilidade', restrito: true }),
  aprendido({ variante: 'whisper flow', termo: 'Wispr Flow' }),
]
const longas = [
  aprendido({ variante: 'um dois tres quatro cinco', termo: 'Pneumoultramicroscopicossilicovulcanoconiotico', dominio: 'administracao-publica-municipal' }),
  aprendido({ variante: 'supercalifragilisticexpialidocious', termo: 'SF', dominio: 'dominio-com-um-nome-muito-comprido-mesmo', restrito: true }),
]

// O ditado pelo celular da conta, nos cenários do aceite e das Configurações;
// nos outros, a conta já está pronta (o microfone abre direto).
const aceiteDeHoje = (transcricao: 'openai' | 'groq', polish: 'openai' | 'anthropic' | null = null) => ({
  versao: VERSAO_DO_AVISO,
  transcricao,
  polish,
  aceito_em: hora(9, 12),
})
const ditadoDoCenario: OpcoesDaNuvem['ditado'] =
  cenario === 'aceite' || cenario === 'ditar-desligado'
    ? {}
    : cenario === 'ditar-ligado'
      ? { aceite: aceiteDeHoje('openai'), chaves: { openai: 'sk-teste-de-mentira-WXYZ' } }
      : cenario === 'ditar-polish'
        ? {
            preferencias: { polish: true, polish_provedor: 'anthropic', polish_modelo: 'claude-haiku-4-5' },
            aceite: aceiteDeHoje('openai', 'anthropic'),
            chaves: { openai: 'sk-teste-de-mentira-WXYZ' },
          }
        : undefined

const srv = servidorFalsoNuvem(
  [
    itemNuvem({ id: 'd1', created_at: hora(7, 14), final_text: 'Manda o relatório amanhã cedo.' }),
    itemNuvem({ id: 'd2', created_at: hora(7, 9), final_text: 'A reunião com o contador ficou para quinta, às dez.' }),
    itemNuvem({ id: 'd3', created_at: hora(6, 17), final_text: 'Lembrar de pagar o DAS até o dia vinte.' }),
  ],
  {
    sessoes: [{ id: 's1', rotulo: 'Android · Chrome', criada_em: hora(1, 9), ultimo_uso_em: hora(7, 17), atual: true }],
    aparelhos: [{ id: 'a1', nome: 'DELL-G15', plataforma: 'windows', versao: '0.1.0', conectado_em: hora(1, 9), ultimo_uso_em: hora(7, 16) }],
    aprendidos: cenario === 'vazio' || cenario === 'erro' ? [] : cenario === 'longa' ? longas : trocas,
    tetoDosAprendidos: cenario === 'teto' ? 2 : 500,
    ditado: ditadoDoCenario,
  },
)
if (cenario === 'erro') {
  srv.interceptar((p) => (p.caminho === '/api/v1/learned' ? new Response(JSON.stringify({ error: { message: 'falha interna' } }), { status: 500 }) : undefined))
}

const ditar = ['gravando', 'enviando', 'erro-rede', 'sessao', 'chegou'].includes(cenario)
const microfone = plataformaFalsa()
// No "enviando", o servidor nunca responde ao ditado.
const buscar: typeof srv.buscar = (entrada, init) =>
  cenario === 'enviando' && entrada === '/api/v1/dictations' ? new Promise(() => {}) : srv.buscar(entrada, init)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppNuvem buscar={buscar} irPara={() => {}} plataformaDeDitar={microfone.plataforma} />
  </StrictMode>,
)

const botaoDe = (nome: string) => Array.from(document.querySelectorAll('button')).find((b) => b.getAttribute('aria-label') === nome || b.textContent === nome)

// Espera o botão aparecer, e então segue.
function quandoHouver(nome: string, seguir: (b: HTMLElement) => void) {
  const id = setInterval(() => {
    const botao = botaoDe(nome)
    if (botao) {
      clearInterval(id)
      seguir(botao)
    }
  }, 50)
}

if (ditar) {
  // A fala sintética: sílabas de ~230 ms com pausas, de volume que sobe e desce
  // (RMS de 0,002 a 0,2: da pausa à sílaba forte, como a fala medida no PC).
  const fala = (t: number) => 0.002 + 0.2 * Math.abs(Math.sin(t / 73)) ** 2.6 * (0.35 + 0.65 * Math.abs(Math.sin(t / 590))) * (Math.sin(t / 1900) > -0.7 ? 1 : 0.03)
  quandoHouver('Ditar', (mic) => {
    srv.aoDitar({ texto: 'Manda o relatório de outubro para a Fernanda até amanhã cedo.' })
    mic.click()
    // O microfone abre em um tick; a fala corre num relógio parado.
    setTimeout(() => {
      microfone.rodar(6600, fala)
      if (cenario === 'gravando') return
      if (cenario === 'erro-rede') srv.derrubar(true)
      if (cenario === 'sessao') srv.expirar()
      botaoDe('Parar e enviar')?.click()
    }, 120)
  })
} else if (cenario === 'aceite') {
  // O primeiro toque no microfone de uma conta sem aceite: a folha, e o
  // microfone fechado.
  quandoHouver('Ditar', (mic) => mic.click())
} else if (busca.get('config') !== '0') {
  // Abre as Configurações sozinha: o botão da barra é o único com esse nome.
  quandoHouver('Configurações', (b) => b.click())
}
