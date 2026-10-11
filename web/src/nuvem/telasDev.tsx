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
// Claro e escuro seguem o sistema do navegador (no DevTools: Rendering,
// "prefers-color-scheme"); 320 e 375 px, no modo de dispositivo.

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '../fontes.css'
import '../index.css'
import { aprendido, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
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

const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: hora(7, 14), final_text: 'Manda o relatório amanhã cedo.' })], {
  sessoes: [{ id: 's1', rotulo: 'Android · Chrome', criada_em: hora(1, 9), ultimo_uso_em: hora(7, 17), atual: true }],
  aparelhos: [{ id: 'a1', nome: 'DELL-G15', plataforma: 'windows', versao: '0.1.0', conectado_em: hora(1, 9), ultimo_uso_em: hora(7, 16) }],
  aprendidos: cenario === 'vazio' || cenario === 'erro' ? [] : cenario === 'longa' ? longas : trocas,
  tetoDosAprendidos: cenario === 'teto' ? 2 : 500,
})
if (cenario === 'erro') {
  srv.interceptar((p) => (p.caminho === '/api/v1/learned' ? new Response(JSON.stringify({ error: { message: 'falha interna' } }), { status: 500 }) : undefined))
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppNuvem buscar={srv.buscar} irPara={() => {}} />
  </StrictMode>,
)

// Abre as Configurações sozinha: o botão da barra é o único com esse nome.
if (busca.get('config') !== '0') {
  const id = setInterval(() => {
    const botao = Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Configurações')
    if (botao) {
      botao.click()
      clearInterval(id)
    }
  }, 50)
}
