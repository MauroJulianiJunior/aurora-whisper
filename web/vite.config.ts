/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { type Plugin, defineConfig } from 'vite'

// Dois builds do mesmo código (ADR-019, decisão 12):
//   - o local (vite build): o painel que o Agent serve no PC, em bin\painel,
//     com a entrada src/main.tsx;
//   - o da nuvem (vite build --mode nuvem): o app que o servidor da nuvem
//     serve (PAINEL_DIR), em dist-nuvem, com a entrada src/nuvem/main.tsx.
// A troca é de entrada, e não um "if" em tempo de execução: o código da
// nuvem (login, cliente da /api/v1) não entra no grafo de módulos do build
// local, e o do PC (bilhete, chave de API) não entra no da nuvem.
// bundle.test.ts constrói os dois e confere.

const modoNuvem = 'nuvem'

// O index.html é um só; no modo nuvem, o script dele aponta para a entrada da
// nuvem. 'pre': antes de o Vite ler o HTML atrás das entradas.
const entradaDaNuvem: Plugin = {
  name: 'aurora-entrada-da-nuvem',
  transformIndexHtml: {
    order: 'pre',
    handler(html) {
      const trocado = html.replace('src="/src/main.tsx"', 'src="/src/nuvem/main.tsx"')
      if (trocado === html) throw new Error('index.html: não achei a entrada /src/main.tsx para trocar pela da nuvem')
      return trocado
    },
  },
}

// O app instalável (ADR-019, F5), só no build da nuvem: o manifest, os ícones
// e o service worker vão para a raiz do build, e o index.html ganha o link do
// manifest e a cor da barra do sistema nos dois temas (o manifest aceita uma
// cor só; a meta da página vale quando ela abre). As fontes ficam em web/pwa;
// os ícones saem do `go run ./cmd/gerarico`.
const pastaRaiz = path.dirname(fileURLToPath(import.meta.url))
const pastaPWA = path.join(pastaRaiz, 'pwa')
const iconesDoApp = ['icone-192.png', 'icone-512.png', 'icone-maskable-512.png']

// cascaDoBuild: o que o service worker guarda no install. O / (o index.html: o
// servidor da nuvem responde 404 em /index.html), o favicon e todo /assets do
// build. Nada da raiz além disso: nem o sw.js, nem o manifest, nem os ícones.
function cascaDoBuild(arquivosDoBundle: string[]): string[] {
  const assets = arquivosDoBundle.filter((f) => f.startsWith('assets/')).map((f) => '/' + f)
  return ['/', '/favicon.svg', ...assets.sort()]
}

const appInstalavel: Plugin = {
  name: 'aurora-app-instalavel',
  transformIndexHtml() {
    return [
      { tag: 'link', attrs: { rel: 'manifest', href: '/manifest.webmanifest' }, injectTo: 'head' },
      { tag: 'meta', attrs: { name: 'theme-color', media: '(prefers-color-scheme: light)', content: '#f5f7f5' }, injectTo: 'head' },
      { tag: 'meta', attrs: { name: 'theme-color', media: '(prefers-color-scheme: dark)', content: '#070a09' }, injectTo: 'head' },
    ]
  },
  // 'post': depois de o Vite pôr o index.html no bundle.
  generateBundle: {
    order: 'post',
    handler(_, bundle) {
      const indice = bundle['index.html']
      if (!indice || indice.type !== 'asset') throw new Error('o build da nuvem não tem o index.html para a casca do service worker')

      // A versão é o hash dos nomes e do conteúdo de cada arquivo da casca:
      // qualquer mudança nela troca o sw.js, e o navegador instala a casca nova.
      const casca = cascaDoBuild(Object.keys(bundle))
      const hash = createHash('sha256')
      for (const url of casca) {
        let conteudo: string | Uint8Array
        if (url === '/') conteudo = indice.source
        else if (url === '/favicon.svg') conteudo = readFileSync(path.join(pastaRaiz, 'public', 'favicon.svg'))
        else {
          const item = bundle[url.slice(1)]
          if (!item) throw new Error(`a casca pede ${url}, que não está no build`)
          conteudo = item.type === 'asset' ? item.source : item.code
        }
        hash.update(url + '\0')
        hash.update(conteudo)
        hash.update('\0')
      }
      const versao = 'aurora-casca-' + hash.digest('hex').slice(0, 12)

      const fonte = readFileSync(path.join(pastaPWA, 'sw.js'), 'utf8')
      const marcadores: [string, string][] = [
        ["'__VERSAO__'", JSON.stringify(versao)],
        ["['__CASCA__']", JSON.stringify(casca)],
      ]
      let sw = fonte
      for (const [marcador, valor] of marcadores) {
        if (sw.split(marcador).length !== 2) throw new Error(`web/pwa/sw.js: o marcador ${marcador} tem de aparecer uma vez`)
        sw = sw.replace(marcador, () => valor)
      }
      this.emitFile({ type: 'asset', fileName: 'sw.js', source: sw })

      // O manifest passa por um JSON.parse: um manifest quebrado quebra o build.
      const manifest = readFileSync(path.join(pastaPWA, 'manifest.webmanifest'), 'utf8')
      JSON.parse(manifest)
      this.emitFile({ type: 'asset', fileName: 'manifest.webmanifest', source: manifest })
      for (const nome of iconesDoApp) {
        this.emitFile({ type: 'asset', fileName: nome, source: readFileSync(path.join(pastaPWA, nome)) })
      }
    },
  },
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const nuvem = mode === modoNuvem
  return {
    plugins: nuvem ? [react(), entradaDaNuvem, appInstalavel] : [react()],
    // marca.test.tsx confere as marcas do painel contra os SVG originais, em
    // docs/marca, fora da pasta web. Só essa pasta a mais: o resto do
    // repositório continua fora do alcance do servidor de desenvolvimento.
    server: { fs: { allow: ['.', '../docs/marca'] } },
    build: {
      // O Agent serve o painel da pasta "painel" ao lado do executável
      // (cmd/agent/main.go, subirPainel), e o executável fica em bin\. O
      // build da nuvem fica em web\dist-nuvem (fora do git); o servidor lê a
      // pasta de PAINEL_DIR.
      outDir: nuvem ? 'dist-nuvem' : '../bin/painel',
      emptyOutDir: true,
      // O servidor do painel manda CSP default-src 'self': nenhum script
      // embutido no HTML roda. O polyfill de modulepreload não faz falta nos
      // navegadores atuais e é o único candidato a script embutido.
      modulePreload: { polyfill: false },
      // Nada vira data: URI. A CSP da nuvem não tem img-src nem font-src: o
      // default-src 'self' recusa data:. Hoje nada passa do limite padrão
      // (4 KiB), e isto garante que continue assim.
      assetsInlineLimit: 0,
    },
    test: {
      environment: 'jsdom',
      // Os findBy esperam 5 s, e cada teste, 20 s: a máquina ocupada não pode
      // derrubar a suíte (testes/preparar.ts).
      setupFiles: ['./src/testes/preparar.ts'],
      testTimeout: 20_000,
      // O vitest troca todo CSS por texto vazio. O index.css passa pelo
      // Vite de verdade para que estilo.test.ts leia os tokens e confira o
      // contraste no próprio arquivo; fontes.css e os CSS dos pacotes
      // @fontsource, para que fontes.test.ts compare um com os outros.
      css: { include: [/index\.css/, /fontes\.css/, /@fontsource/] },
    },
  }
})
