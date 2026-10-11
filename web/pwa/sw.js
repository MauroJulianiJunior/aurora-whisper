// O service worker do app da nuvem (ADR-019, F5). Só existe no build da nuvem:
// o vite.config.ts troca os dois marcadores abaixo e o emite em /sw.js (o build
// quebra se faltar um). O painel do PC nunca o registra, e o Agent responde 404
// em /sw.js.
//
// Ele só guarda a CASCA do app (o index.html, o favicon e os /assets do build),
// gravada uma vez, no install. Não há put em lugar nenhum: nada que chega da
// rede depois entra no cache, então nenhum texto ditado tem como parar aqui (o
// celular não guarda ditado no aparelho).
//
// O que ele responde é uma lista branca; o resto ele nem toca, e o pedido vai à
// rede como se ele não existisse:
//   - a navegação GET para / (com ou sem busca): a rede primeiro; o / guardado só
//     entra se a rede falhar. Sem rede, o app sobe, o GET /api/v1/me falha, e a
//     tela é "Sem conexão.";
//   - o GET /favicon.svg: a rede primeiro, o guardado sem rede;
//   - o GET /assets/*: o guardado, senão a rede (o nome tem o hash e nunca muda).
// Nunca toca: método que não é GET, /api (inclusive /api/v1/device-auth), /auth
// (a volta do Google é de uso único), /device, /sw.js, o manifest, os ícones e
// outra origem. Uma rota nova fica de fora sozinha.
//
// Sem navigation preload, de propósito: ele vale para TODA navegação do escopo,
// inclusive as que este arquivo não responde, e aí o navegador faria o pedido
// duas vezes (o do preload e o de sempre). A volta do Google
// (/auth/google/callback) é de uso único: a segunda vez falharia.
//
// A versão: VERSAO muda quando muda qualquer arquivo da CASCA (o hash dos nomes
// e do conteúdo). O navegador confere o /sw.js a cada navegação (no-cache no
// servidor, updateViaCache 'none' no registro); um sw.js novo se instala em
// segundo plano, assume com o skipWaiting, e o activate apaga as cascas velhas.

'use strict'

const VERSAO = '__VERSAO__'
const CASCA = ['__CASCA__']
const PREFIXO = 'aurora-casca-'

self.addEventListener('install', (evento) => {
  evento.waitUntil(
    caches
      .open(VERSAO)
      // reload: a casca vem do servidor, nunca do cache HTTP do navegador.
      .then((cache) => cache.addAll(CASCA.map((url) => new Request(url, { cache: 'reload' }))))
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (evento) => {
  evento.waitUntil(
    caches
      .keys()
      .then((nomes) => Promise.all(nomes.filter((nome) => nome.startsWith(PREFIXO) && nome !== VERSAO).map((nome) => caches.delete(nome)))),
  )
})

self.addEventListener('fetch', (evento) => {
  const pedido = evento.request
  if (pedido.method !== 'GET') return
  const url = new URL(pedido.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname === '/' && pedido.mode === 'navigate') {
    evento.respondWith(redePrimeiro(pedido, '/'))
  } else if (url.pathname === '/favicon.svg') {
    evento.respondWith(redePrimeiro(pedido, '/favicon.svg'))
  } else if (url.pathname.startsWith('/assets/')) {
    evento.respondWith(guardadoPrimeiro(pedido))
  }
})

// redePrimeiro: a resposta da rede, qualquer que seja; o guardado em chave só
// quando a rede falha (o fetch rejeita). Nada é guardado.
function redePrimeiro(pedido, chave) {
  return fetch(pedido).catch((erro) =>
    caches
      .open(VERSAO)
      .then((cache) => cache.match(chave))
      .then((guardado) => guardado || Promise.reject(erro)),
  )
}

// guardadoPrimeiro: o arquivo da casca, senão a rede. Nada é guardado.
function guardadoPrimeiro(pedido) {
  return caches
    .open(VERSAO)
    .then((cache) => cache.match(pedido))
    .then((guardado) => guardado || fetch(pedido))
}
