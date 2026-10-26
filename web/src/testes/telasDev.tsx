// Só para ver as telas do painel do PC no `pnpm dev`, sem o Agent (ADR-019,
// F8h): o painel inteiro contra o Agent de mentira dos testes
// (testes/servidorFalso.ts), em memória. Não faz parte de nenhum build:
// nenhum index.html o importa (o `vite build` só lê o index.html).
//
//   pnpm -C web dev      e abrir      /telas-dev.html?cenario=estados
//
// cenario:
//   estados    (padrão) o Aprendido no painel com uma troca que vale e as três
//              linhas de estado do sync;
//   so-estados só linhas de estado (a seção aparece sem nenhum Desfazer);
//   pergunta   a pergunta do envio com 800 ditados e 3 trocas aprendidas;
//   so-trocas  a pergunta do envio sem ditado a enviar (N = 0), só trocas;
//   conectado  a Privacidade com o PC conectado (as trocas também vão);
//   freio      o aviso do freio: 12 trocas saíram do dicionário de uma vez;
//   historico  (F10) o bloco Histórico com a pergunta dos 6 meses: 1.240 ditados,
//              3 na lixeira, 2 com duas versões, 5 de outro dono, e 2 cópias do banco;
//   historico-simples  a pergunta com 12 ditados, sem as linhas de baixo e sem cópias;
//   historico-1        a pergunta no singular (1 ditado, 1 na lixeira, 1 cópia);
//   historico-ligado   os 6 meses ligados, com o Desligar (e 1 cópia ao lado do banco);
//   historico-pausado  a regra pausada pelo freio: a pergunta volta, com a frase;
//   historico-copias   só as 3 cópias do banco, com Apagar as cópias;
//   historico-envio    3 cópias, 1 delas a do envio à nuvem que não terminou (fica);
//   historico-so-envio só a cópia do envio que não terminou: sem o botão;
//   historico-de-novo  a pergunta com a cópia de um OK anterior ao lado do banco
//                      (o OK não copia de novo, e o que nasceu depois dela não está nela);
//   historico-velho    o Agent de antes da F10: a frase do Reinicie o Agent.
//   primeiro-uso       o primeiro uso: o histórico vazio, que diz que o ditado funciona
//                      sem chave e que a chave só serve ao Polish (abra com ?config=0);
//   chaves             a seção IA com a OpenAI escolhida e nenhuma chave guardada: a
//                      linha de onde criar a chave embaixo do campo;
//   chaves-anthropic   o mesmo, com a Anthropic.
//   notas              (ADR-022, N2) a aba Notas, que abre sozinha: oito notas, uma de cada cor
//                      (uma fixada, uma lista de marcar, uma que veio de um ditado, uma no
//                      Arquivo, uma na Lixeira) e três marcadores;
//   notas-vazio        a aba Notas sem nenhuma nota.
//   No historico, o clique em Apagar os mais antigos pergunta na linha, e o Apagar
//   liga a regra no Agent de mentira (a prévia some; a Privacidade ganha a linha).
// A tela de Configurações abre sozinha; ?config=0 deixa no Histórico.
// Claro e escuro seguem o sistema do navegador (no DevTools: Rendering,
// "prefers-color-scheme"); 320 e 375 px, no modo de dispositivo.

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from '../App'
import type { Aprendida } from '../api'
import type { EstadoNuvem, EstadoRetencao } from '../clienteLocal'
import '../fontes.css'
import '../index.css'
import type { Nota } from '../notas/tipos'
import { ditado, marcador, nota, servidorFalso } from './servidorFalso'

const busca = new URLSearchParams(window.location.search)
const cenario = busca.get('cenario') ?? 'estados'

const vale: Aprendida[] = [
  { termo: 'ECF', variante: 'sf' },
  { termo: 'Wispr Flow', variante: 'whisperflow' },
]
const estados: Aprendida[] = [
  { termo: 'DCTFWeb', variante: 'dctf web', situacao: 'nao_aplicado', motivo: 'dominio_ausente' },
  { termo: 'n8n', variante: 'n oito n', situacao: 'desfeito_em_outro_aparelho' },
  { termo: 'ECD', variante: 'sf', situacao: 'trocado_em_outro_aparelho' },
]

const conectado = (previa: number, previaTrocas: number, freioTrocas = 0): EstadoNuvem => ({
  estado: 'conectado',
  url: 'https://nuvem.exemplo.test',
  email: 'voce@exemplo.test',
  nome: 'DELL-G15',
  historico: {
    previa,
    previa_trocas: previaTrocas,
    ...(freioTrocas > 0 ? { freio_trocas: freioTrocas, aceito: true } : { aceito: false }),
    nao_subiram: 0,
    pendentes: 0,
    mudancas: 0,
    conferido_em: new Date(2026, 9, 7, 15, 0).toISOString(),
    falhou: false,
    outra_nuvem: false,
  },
})

// Os 6 meses no PC (F10d e F10e): o que o Agent de mentira diz no começo.
const retencaoBase: EstadoRetencao = { previa: 0, na_lixeira: 0, com_selo: 0, de_outro_dono: 0, ligada: false, pausada: false, copias: 0 }
const retencoes: Record<string, Partial<EstadoRetencao>> = {
  historico: { previa: 1240, na_lixeira: 3, com_selo: 2, de_outro_dono: 5, copias: 2 },
  'historico-simples': { previa: 12 },
  'historico-1': { previa: 1, na_lixeira: 1, com_selo: 1, de_outro_dono: 1, copias: 1 },
  'historico-ligado': { ligada: true, copias: 1 },
  'historico-pausado': { previa: 12, na_lixeira: 2, pausada: true },
  'historico-copias': { copias: 3 },
  'historico-envio': { copias: 3 },
  'historico-so-envio': { copias: 1 },
  'historico-de-novo': { previa: 12, copias: 1, copia_dos_6_meses: true },
}
const retencao = retencoes[cenario]

const aprendidas = cenario === 'estados' ? [...vale, ...estados] : cenario === 'so-estados' ? estados : []
const nuvem =
  cenario === 'pergunta' ? conectado(800, 3) : cenario === 'so-trocas' ? conectado(0, 3) : cenario === 'conectado' ? conectado(0, 0) : cenario === 'freio' ? conectado(0, 0, 12) : undefined

// O primeiro uso é o histórico vazio; a IA com um provedor escolhido e sem chave mostra a linha de onde criá-la.
const ditados = cenario === 'primeiro-uso' ? [] : [ditado({ id: 'd1', created_at: new Date(2026, 9, 7, 14).toISOString(), final_text: 'Manda o relatório amanhã cedo.' })]
const provedorDaIA = cenario === 'chaves' ? 'openai' : cenario === 'chaves-anthropic' ? 'anthropic' : ''

// As notas (ADR-022, N2): uma de cada cor, para ver os oito tons no tema do navegador.
const hora = (h: number, m = 0) => new Date(2026, 9, 25, h, m).toISOString()
const notasDeExemplo: Nota[] = [
  nota({ id: 'n1', titulo: 'Compras da semana', corpo: '- [ ] leite\n- [ ] café\n- [ ] ovos\n- [x] pão\n- [x] manteiga', cor: 'amarelo', fixada: true, marcadores: ['m1'], updated_at: hora(9) }),
  nota({ id: 'n2', titulo: 'Reunião com o contador', corpo: '# Pauta\n- conferir o extrato do Itaú\n- boleto do aluguel\n1. mandar o DAS\n2. pagar o ISS', cor: 'azul', marcadores: ['m2'], updated_at: hora(10), ditado_de_origem: 'd1' }),
  nota({ id: 'n3', titulo: 'Ideia de vídeo', corpo: 'Um Reels curto sobre o que muda na reforma para o MEI.', cor: 'roxo', marcadores: ['m3'], updated_at: hora(11) }),
  nota({ id: 'n4', corpo: 'Ligar para a Fernanda antes das cinco e confirmar a gravação de quinta.', cor: 'laranja', updated_at: hora(12) }),
  nota({ id: 'n5', titulo: 'Ponto de atenção', corpo: 'Rever o prazo da DCTFWeb.', cor: 'vermelho', updated_at: hora(8) }),
  nota({ id: 'n6', titulo: 'Para lembrar', corpo: '- trocar o filtro\n- regar as plantas', cor: 'verde', updated_at: hora(7) }),
  nota({ id: 'n7', titulo: 'Presente', corpo: 'Livro de receitas, a capa dura.', cor: 'rosa', updated_at: hora(6) }),
  nota({ id: 'n8', titulo: 'Sem cor', corpo: 'Uma nota comum.', cor: 'cinza', updated_at: hora(5) }),
  nota({ id: 'n9', titulo: 'Arquivada', corpo: 'Já resolvido.', arquivada: true, updated_at: hora(4) }),
  nota({ id: 'n10', titulo: 'Na lixeira', corpo: 'Apagada ontem.', deleted_at: hora(3), updated_at: hora(3) }),
]

const srv = servidorFalso(ditados, {
  ...(cenario === 'notas'
    ? { notas: notasDeExemplo, marcadores: [marcador({ id: 'm1', nome: 'Compras' }), marcador({ id: 'm2', nome: 'Escritório' }), marcador({ id: 'm3', nome: 'Ideias' })] }
    : {}),
  aprendidas,
  ...(provedorDaIA ? { config: { ai_provider: provedorDaIA } } : {}),
  ...(nuvem ? { nuvem } : {}),
  ...(retencao ? { retencao: { ...retencaoBase, ...retencao } } : {}),
  // A cópia do envio à nuvem que não terminou: o botão das cópias não a apaga.
  ...(cenario === 'historico-envio' || cenario === 'historico-so-envio' ? { copiasProtegidas: 1 } : {}),
})
// O Agent de antes da F10: as rotas dão 404.
if (cenario === 'historico-velho') srv.agentSemRetencao()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App sessao={Promise.resolve('tok')} buscar={srv.buscar} />
  </StrictMode>,
)

// Abre as Configurações sozinha (ou as Notas, nos cenários delas): o botão da
// barra é o único com esse nome.
if (busca.get('config') !== '0') {
  const aba = cenario.startsWith('notas') ? 'Notas' : 'Configurações'
  const id = setInterval(() => {
    const botao = Array.from(document.querySelectorAll<HTMLButtonElement>('.vistas button')).find((b) => b.textContent === aba)
    if (botao) {
      botao.click()
      clearInterval(id)
    }
  }, 50)
}
