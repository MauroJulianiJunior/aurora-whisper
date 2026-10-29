// Só para ver as telas do app da nuvem no `pnpm dev`, sem Docker, sem Google e
// sem o servidor de verdade (ADR-019, F8h): o app inteiro contra a nuvem de
// mentira dos testes (testes/servidorFalsoNuvem.ts), em memória. Não faz parte
// de nenhum build: nenhum index.html o importa (o `vite build` só lê o
// index.html), e o bundle.test.ts confere os dois.
//
//   pnpm -C web dev      e abrir      /telas-dev-nuvem.html?cenario=lista
//
// cenario: lista (padrão), vazio, teto (passou do teto do servidor), erro (a
// lista não carrega), longa (variante e domínio compridos) ou entrar (sem
// sessão: a tela Entrar, com a linha da nuvem particular).
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
//                    por colar;
//   ditar-sem-chave  as Configurações, ligado e sem nenhuma chave, com o Polish na
//                    Anthropic: os dois campos, cada um com a linha de onde criar a
//                    chave do provedor dele.
//
// As notas no celular (ADR-022, N8), com as notas de mentira (notas/notasFalsas.ts):
//   notas            a aba Notas, com uma nota curta no alto da coluna da esquerda
//                    (abrir uma nota e a Cor: a paleta tem de caber na tela). Desde a
//                    N10b, com imagens: uma, duas, cinco e uma nota só com imagem (os
//                    bytes são JPEGs que o canvas desenha na hora; o Imagem na barra da
//                    nota abre o menu da câmera e da galeria, que escolhem arquivos de
//                    verdade e sobem para a nuvem de mentira, em memória).
//   notas-sem-rede   a mesma aba aberta sem rede, com as notas guardadas no aparelho:
//                    a faixa "Sem rede", as imagens como "Imagem sem rede" e o ícone de
//                    imagem apagado (cena 6 da prévia da N10).
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
import { armazemNaMemoria } from './notas/armazem'
import { daNuvem, type ImagemDaNuvem } from './notas/formato'
import { imagemFalsa, juntar, notaDaNuvem, notasFalsasNaNuvem } from './notas/notasFalsas'

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
        : cenario === 'ditar-sem-chave'
          ? {
              preferencias: { polish: true, polish_provedor: 'anthropic', polish_modelo: 'claude-haiku-4-5' },
              aceite: aceiteDeHoje('openai', 'anthropic'),
              chaves: {},
            }
          : undefined

const srv = servidorFalsoNuvem(
  [
    itemNuvem({ id: 'd1', created_at: hora(7, 14), final_text: 'Manda o relatório amanhã cedo.' }),
    itemNuvem({ id: 'd2', created_at: hora(7, 9), final_text: 'A reunião com o contador ficou para quinta, às dez.' }),
    itemNuvem({ id: 'd3', created_at: hora(6, 17), final_text: 'Lembrar de pagar o DAS até o dia vinte.' }),
  ],
  {
    // O cenário "entrar" abre sem sessão: a tela Entrar.
    logado: cenario !== 'entrar',
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
const buscarDoHistorico: typeof srv.buscar = (entrada, init) =>
  cenario === 'enviando' && entrada === '/api/v1/dictations' ? new Promise(() => {}) : srv.buscar(entrada, init)
// Os JPEGs de mentira das notas com imagem: um degradê com o id, desenhado no canvas
// na hora (só aqui, no pnpm dev). Sem canvas (o jsdom), os bytes mínimos de um JPEG.
const PALETA_DAS_IMAGENS = ['#7fae93', '#d29a5c', '#6f9bc4', '#c27a96', '#9d92c4', '#b8b36a']
const jpegVazio = () => new Blob([new Uint8Array([0xff, 0xd8, 0xff, 0xd9])], { type: 'image/jpeg' })
function jpegDeMentira(im: ImagemDaNuvem): Promise<Blob> {
  try {
    const c = document.createElement('canvas')
    c.width = 480
    c.height = Math.max(1, Math.round((480 * im.altura) / im.largura))
    const g = c.getContext('2d')
    if (!g) return Promise.resolve(jpegVazio())
    const cor = PALETA_DAS_IMAGENS[im.id.split('').reduce((a, ch) => a + ch.charCodeAt(0), 0) % PALETA_DAS_IMAGENS.length]!
    const fundo = g.createLinearGradient(0, 0, c.width, c.height)
    fundo.addColorStop(0, cor)
    fundo.addColorStop(1, '#2b3a33')
    g.fillStyle = fundo
    g.fillRect(0, 0, c.width, c.height)
    // Duas formas e o id no meio: a imagem fica reconhecível em qualquer recorte (cover).
    g.fillStyle = 'rgba(255,255,255,0.22)'
    g.beginPath()
    g.arc(c.width * 0.3, c.height * 0.38, c.height * 0.22, 0, Math.PI * 2)
    g.fill()
    g.fillRect(c.width * 0.5, c.height * 0.55, c.width * 0.36, c.height * 0.2)
    g.fillStyle = 'rgba(255,255,255,0.92)'
    g.font = '600 30px sans-serif'
    g.textAlign = 'center'
    g.textBaseline = 'middle'
    g.fillText(im.id, c.width / 2, c.height / 2)
    return new Promise((ok) => c.toBlob((b) => ok(b ?? jpegVazio()), 'image/jpeg', 0.8))
  } catch {
    return Promise.resolve(jpegVazio())
  }
}

const notasDoCenario = [
  notaDaNuvem({ id: 'n1', titulo: 'recibos de outubro', posicao: 'U', imagens: [imagemFalsa({ id: 'recibo', largura: 800, altura: 600 })] }),
  notaDaNuvem({
    id: 'n2',
    titulo: 'Teste N8',
    corpo: 'primeira versão\nAdivinhado pelo celular',
    posicao: 'V',
    imagens: [imagemFalsa({ id: 'duas-a', largura: 800, altura: 600 }), imagemFalsa({ id: 'duas-b', largura: 600, altura: 800, created_at: '2026-10-27T10:01:00Z' })],
  }),
  notaDaNuvem({
    id: 'n3',
    titulo: 'Mercado N8',
    corpo: '- [ ] Arroz\n- [ ] Feijão\n- [ ] Leite\n- [ ] Pão',
    posicao: 'W',
    imagens: ['a', 'b', 'c', 'd', 'e'].map((x, i) =>
      imagemFalsa({ id: `cinco-${x}`, largura: i === 0 ? 1280 : 800, altura: i === 0 ? 720 : 800, created_at: `2026-10-27T10:0${i}:00Z` }),
    ),
  }),
  notaDaNuvem({ id: 'n4', titulo: 'mais um', corpo: '- [ ] Leite\n- [ ] Pão\n- [ ] Café', cor: 'verde', posicao: 'X' }),
  notaDaNuvem({ id: 'n5', posicao: 'Y', cor: 'azul', imagens: [imagemFalsa({ id: 'so-imagem', largura: 600, altura: 800 })] }),
]
const comNotas = cenario === 'notas' || cenario === 'notas-sem-rede'
const semRedeDeMentira = cenario === 'notas-sem-rede'
const notas = comNotas ? notasFalsasNaNuvem(notasDoCenario, { bytesDaImagem: jpegDeMentira }) : undefined
// Sem rede: nenhum pedido chega, e o app abre nas notas guardadas no aparelho.
const buscar = notas
  ? semRedeDeMentira
    ? () => Promise.reject(new TypeError('Failed to fetch'))
    : juntar(notas, buscarDoHistorico)
  : buscarDoHistorico
const armazem = armazemNaMemoria(semRedeDeMentira ? { notas: notasDoCenario.map(daNuvem) } : {})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppNuvem
      buscar={buscar}
      irPara={() => {}}
      plataformaDeDitar={microfone.plataforma}
      armazemDasNotas={notas ? () => armazem : undefined}
      contaGuardada={semRedeDeMentira ? async () => ({ id: 'u1', email: 'mauro@exemplo.test' }) : undefined}
    />
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
} else if (comNotas) {
  quandoHouver('Notas', (b) => b.click())
} else if (busca.get('config') !== '0' && cenario !== 'entrar') {
  // Abre as Configurações sozinha: o botão da barra é o único com esse nome.
  quandoHouver('Configurações', (b) => b.click())
}
