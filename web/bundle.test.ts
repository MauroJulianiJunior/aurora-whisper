// @vitest-environment node

// A prova da F4 e da F5 (ADR-019, "Fatias"): o bundle local não tem login nem
// service worker, e o da nuvem não tem nada do PC; o da nuvem é instalável (o
// manifest, os ícones e o sw.js). Constrói os dois de verdade (o mesmo
// vite.config.ts de pnpm build e pnpm build:nuvem), cada um numa pasta
// temporária — nunca em bin\ —, e lê o JS, o CSS e o HTML que saíram.
//
// O que cada lista procura é texto que só existe num dos lados: as rotas do
// Agent (/api/sessao, /api/ia/...), o Bearer do token, as rotas da nuvem
// (/auth/start, /api/v1), as telas de entrar. Um "precisa ter" de cada lado
// prova que o teste leu o bundle certo.
//
// O sw.js emitido roda aqui mesmo, num node:vm, com o self, o caches e o fetch
// de mentira: nenhum pedido sai da máquina.

import { createHash } from 'node:crypto'
import { mkdtemp, readFile, readdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import vm from 'node:vm'
import { build } from 'vite'
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest'

const raiz = path.dirname(fileURLToPath(import.meta.url))

interface Bundle {
  dir: string
  html: string
  js: string
  css: string
  arquivos: string[]
}

const pastas: string[] = []

async function construir(mode: 'production' | 'nuvem'): Promise<Bundle> {
  const outDir = await mkdtemp(path.join(tmpdir(), `aw-bundle-${mode}-`))
  pastas.push(outDir)
  await build({
    root: raiz,
    configFile: path.join(raiz, 'vite.config.ts'),
    mode,
    logLevel: 'silent',
    build: { outDir, emptyOutDir: true },
  })
  const arquivos = (await readdir(outDir, { recursive: true })).map((f) => f.split(path.sep).join('/'))
  // O JS e o CSS do app são os de assets/; o sw.js da raiz é lido à parte.
  const ler = async (filtro: (f: string) => boolean) =>
    (await Promise.all(arquivos.filter(filtro).map((f) => readFile(path.join(outDir, f), 'utf8')))).join('\n')
  return {
    dir: outDir,
    html: await ler((f) => f === 'index.html'),
    js: await ler((f) => f.startsWith('assets/') && f.endsWith('.js')),
    css: await ler((f) => f.startsWith('assets/') && f.endsWith('.css')),
    arquivos,
  }
}

let local: Bundle
let nuvem: Bundle
const nodeEnv = process.env.NODE_ENV

beforeAll(async () => {
  // O vitest roda com NODE_ENV=test; o build de verdade é o de produção.
  process.env.NODE_ENV = 'production'
  try {
    local = await construir('production')
    nuvem = await construir('nuvem')
  } finally {
    process.env.NODE_ENV = nodeEnv
  }
}, 120_000)

afterAll(async () => {
  await Promise.all(pastas.map((p) => rm(p, { recursive: true, force: true })))
})

// Os arquivos que só o build da nuvem tem, na raiz (F5).
const daRaizDaNuvem = ['icone-192.png', 'icone-512.png', 'icone-maskable-512.png', 'manifest.webmanifest', 'sw.js']

describe('o bundle local (bin\\painel)', () => {
  it('é o do PC: fala com a Local API pelo bilhete e pelo token', () => {
    expect(local.js).toContain('/api/sessao')
    expect(local.js).toContain('/api/history')
    expect(local.js).toContain('Bearer')
    // F6: o Conectar à nuvem do PC passa pelo Agent, nunca direto à nuvem.
    expect(local.js).toContain('/api/nuvem/conectar')
    // F7c: o envio do histórico e o esquecer também, e o selo com a escolha.
    expect(local.js).toContain('/api/nuvem/enviar')
    expect(local.js).toContain('/api/nuvem/esquecer')
    expect(local.js).toContain('Esquecer esta nuvem')
    // ADR-021, S: o contador de mudanças do sync, que faz o painel reler a lista
    // sozinho. É só do PC: o '/api/nuvem' da lista do bundle da nuvem o cobre.
    expect(local.js).toContain('/api/nuvem/mudancas')
    expect(local.js).toContain('resolver_conflito')
    // F8h: o aprendido do PC passa pelo Agent (/api/aprendizado), com as linhas de
    // estado do sync; a pergunta do envio conta as trocas aprendidas.
    expect(local.js).toContain('/api/aprendizado')
    expect(local.js).toContain('não aplicado neste PC')
    expect(local.js).toContain('desfeito em outro aparelho')
    expect(local.js).toContain('trocado em outro aparelho')
    expect(local.js).toContain('trocas aprendidas')
    // A frase da pergunta do envio é montada em pedaços (`${verbo} subir …`), então
    // "Vão subir" não existe em bundle nenhum; estes dois pedaços existem aqui, e é
    // por eles que o bundle da nuvem prova que a pergunta não entrou nele.
    expect(local.js).toContain('dos últimos 6 meses')
    expect(local.js).toContain('A lixeira não vai')
    // F10d e F10e: os 6 meses do histórico no PC e as cópias do banco passam pelo
    // Agent (/api/retencao), no bloco Histórico das Configurações.
    expect(local.js).toContain('/api/retencao')
    expect(local.js).toContain('Apagar os mais antigos')
    expect(local.js).toContain('Apagar as cópias')
    expect(local.js).toContain('Os 6 meses estão ligados')
  })

  it.each([
    '/auth/',
    '/api/v1',
    '/api/v1/learned',
    '/device',
    'serviceWorker',
    'sw.js',
    'Entrar com o Google',
    'Sessões abertas',
    'Apagar meus dados da nuvem',
    'Conectar este PC?',
    // F8h: o Desfazer do celular (a /api/v1/learned, a pergunta na linha) é só do app.
    'Desfeito em todos os aparelhos',
    'Nada aprendido ainda',
    // C6a (ADR-021): o gravador do ditado e o envio são só do app. O painel do
    // PC nunca abre o microfone do navegador: o Agent captura o áudio, e o
    // áudio do PC não sai dele.
    'getUserMedia',
    'MediaRecorder',
    'AudioContext',
    'createAnalyser',
    'wakeLock',
    '/api/v1/dictations',
    'Ditado-Id',
    'Ditado-Duracao-Ms',
    'Ditado-Fala-Ms',
    'audio/webm',
    'Transcrevendo',
    // C6b (ADR-021): o aceite, as chaves e as preferências do ditado pelo celular
    // são só do app: o painel do PC não tem a folha, nem as rotas da conta, nem
    // a chave que o servidor guarda cifrada (a do PC fica no PC).
    '/api/v1/dictation',
    '/api/v1/ai-keys',
    'Ditar pelo celular',
    // O título da folha do aceite (o 'Ditar pelo celular' acima é o da seção das Configurações).
    'Ao ditar pelo celular, a sua voz vai a um provedor',
    'transferência internacional',
    'Chave terminada em',
    'Revogue também esta chave',
    'Organizar o texto com o Polish',
  ])('não tem %s', (texto) => {
    expect(local.js).not.toContain(texto)
  })

  it('não é instalável: sem manifest, sem a cor da barra, sem sw.js e sem os ícones do app', () => {
    expect(local.html).not.toContain('manifest')
    expect(local.html).not.toContain('theme-color')
    for (const f of daRaizDaNuvem) expect(local.arquivos).not.toContain(f)
  })
})

describe('o bundle da nuvem (dist-nuvem)', () => {
  it('é o da nuvem: entra pelo /auth e fala com a /api/v1', () => {
    expect(nuvem.js).toContain('/auth/start')
    expect(nuvem.js).toContain('/api/v1/transcriptions')
    expect(nuvem.js).toContain('Entrar com o Google')
    // F6: a aprovação de um PC e os Aparelhos.
    expect(nuvem.js).toContain('/api/v1/device-auth/requests/')
    expect(nuvem.js).toContain('/api/v1/devices')
    expect(nuvem.js).toContain('Conectar este PC?')
    // F8h: o Aprendido do celular lista e desfaz pela /api/v1/learned.
    expect(nuvem.js).toContain('/api/v1/learned')
    expect(nuvem.js).toContain('/api/v1/learned/undo')
    expect(nuvem.js).toContain('Desfeito em todos os aparelhos.')
  })

  // C6a (ADR-021): o gravador e o envio do ditado. O microfone, o MediaRecorder
  // em webm/opus, o AnalyserNode da onda e da fala, o Wake Lock, e a rota nova
  // com os três cabeçalhos.
  it('tem o gravador do ditado e o envio', () => {
    for (const texto of [
      'getUserMedia',
      'MediaRecorder',
      'audio/webm;codecs=opus',
      'createAnalyser',
      'wakeLock',
      '/api/v1/dictations',
      'Ditado-Id',
      'Ditado-Duracao-Ms',
      'Ditado-Fala-Ms',
      'Transcrevendo',
      'Tentar de novo',
    ]) {
      expect(nuvem.js, texto).toContain(texto)
    }
  })

  // C6b (ADR-021): o aceite antes do microfone, as chaves do provedor (guardadas
  // cifradas no servidor) e as preferências do ditado. As rotas da conta, o
  // texto do aviso (o que sai, para quem, o dado de terceiro, o áudio que não
  // fica gravado) e a Privacidade com "a chave do PC continua no PC".
  it('tem o aceite, as chaves e as preferências do ditado pelo celular', () => {
    for (const texto of [
      '/api/v1/dictation',
      '/api/v1/dictation/settings',
      '/api/v1/dictation/consent',
      '/api/v1/ai-keys',
      'Ao ditar pelo celular, a sua voz vai a um provedor que não diz processar no Brasil.',
      'Não é gravada no seu aparelho nem no servidor.',
      'quem administra o servidor consegue decifrá-la',
      'A voz e os termos podem ter dados de outras pessoas',
      'Se você dita dado de cliente',
      'Chave terminada em',
      'Revogue também esta chave no provedor. Uma cópia cifrada dela fica nos backups por cerca de 30 dias.',
      'Mandar os termos aprendidos como dica',
      'Organizar o texto com o Polish',
      'a chave do PC continua no PC',
      'A voz não é gravada: fica na memória e é descartada ao fim.',
    ]) {
      expect(nuvem.js, texto).toContain(texto)
    }
  })

  // O build da nuvem não conhece o Windows: o aviso e a Privacidade do app dizem
  // que a chave do PC continua no PC sem citar onde ela mora.
  it('a chave do PC é citada sem o Gerenciador de Credenciais', () => {
    expect(nuvem.js).toContain('a chave do PC continua no PC')
    expect(nuvem.js).not.toContain('Gerenciador de Credenciais')
    expect(nuvem.js).not.toContain('Credential Manager')
  })

  // O áudio vive só numa variável da página: nada em IndexedDB, Cache ou
  // localStorage (o it.each de baixo trava as três frases do armazenamento do
  // navegador), nem salvo em arquivo, nem tocado de volta. O analisador não é
  // ligado a nenhuma saída, e o AudioWorklet fica de fora: a CSP default-src
  // 'self' não muda.
  it.each(['caches.', 'createObjectURL', 'showSaveFilePicker', 'FileReader', 'new Audio(', 'audioWorklet', 'AudioWorklet', 'navigator.storage'])(
    'o gravador não guarda nem toca o áudio: sem %s',
    (texto) => {
      expect(nuvem.js).not.toContain(texto)
    },
  )

  // Nada do PC: o bilhete, o token, a Local API, as Configurações do PC. E
  // nada guardado no aparelho.
  it.each([
    '/api/sessao',
    '/api/history',
    '/api/settings',
    '/api/ia/',
    '/api/aprendizado',
    '/api/termos',
    // F6: o lado do PC do pareamento (o Conectar, o estado, o token). F7c:
    // o envio do histórico e o esquecer são só do PC.
    '/api/nuvem',
    'Endereço da nuvem',
    'Esquecer esta nuvem',
    'Enviar o histórico',
    // F8h: as linhas de estado do sync e a pergunta do envio são do PC.
    '/api/aprendizado/desfazer',
    'Aprendido no painel',
    'não aplicado neste PC',
    'desfeito em outro aparelho',
    'trocado em outro aparelho',
    'dos últimos 6 meses',
    'A lixeira não vai',
    // F10d e F10e: as rotas que apagam ditado e cópia do PC, e o bloco Histórico,
    // são só do PC (o servidor da nuvem não as tem). O '/api/nuvem' acima não as
    // cobre.
    '/api/retencao',
    'Apagar os mais antigos',
    'Apagar as cópias',
    'Os 6 meses estão ligados',
    'Bearer',
    'Authorization',
    'Gerenciador de Credenciais',
    'localStorage',
    'sessionStorage',
    'indexedDB',
  ])('não tem %s', (texto) => {
    expect(nuvem.js).not.toContain(texto)
  })

  it('a busca vai no corpo de um POST, e a lista sem busca por GET', () => {
    expect(nuvem.js).toContain('/api/v1/transcriptions/search')
  })

  it('registra o service worker (F5)', () => {
    expect(nuvem.js).toContain('serviceWorker')
    expect(nuvem.js).toContain('/sw.js')
  })

  it('o index.html aponta o manifest e dá a cor da barra nos dois temas', () => {
    expect(nuvem.html).toContain('<link rel="manifest" href="/manifest.webmanifest">')
    expect(nuvem.html).toContain('<meta name="theme-color" media="(prefers-color-scheme: light)" content="#f5f7f5">')
    expect(nuvem.html).toContain('<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#070a09">')
  })
})

describe.each([
  ['local', () => local],
  ['nuvem', () => nuvem],
])('o HTML e o CSS do bundle %s cabem na CSP (default-src self)', (_, bundle) => {
  it('nenhum script embutido: todo <script> tem src', () => {
    const scripts = bundle().html.match(/<script\b[^>]*>/gi) ?? []
    expect(scripts.length).toBeGreaterThan(0)
    for (const s of scripts) expect(s).toMatch(/\ssrc="\/assets\/[^"]+\.js"/)
  })

  it('nenhum estilo embutido no HTML', () => {
    expect(bundle().html).not.toMatch(/<style\b/i)
    expect(bundle().html).not.toMatch(/\sstyle=/i)
  })

  it('nada carregado de fora nem como data:', () => {
    const { html, css } = bundle()
    expect(html).not.toMatch(/(src|href)="(https?:)?\/\//i)
    expect(css).not.toMatch(/url\((["']?)(https?:|\/\/|data:)/i)
  })
})

describe('os arquivos de cada build', () => {
  const daRaiz = (b: Bundle) => b.arquivos.filter((f) => f !== 'assets' && !f.startsWith('assets/')).sort()

  it('o local: index.html, assets/ e os da raiz de public/', () => {
    expect(daRaiz(local)).toEqual(['favicon.svg', 'index.html', 'licencas-fontes.txt'])
  })

  it('o da nuvem: os mesmos, mais o manifest, os ícones e o sw.js', () => {
    expect(daRaiz(nuvem)).toEqual(['favicon.svg', 'index.html', 'licencas-fontes.txt', ...daRaizDaNuvem].sort())
  })
})

// C6c (ADR-021): a página de calibração do microfone é só do `pnpm dev`
// (calibracao-dev.html, como a telas-dev-nuvem.html). Ela abre o microfone e
// mostra números; nada dela, nem o texto, nem o CSS, nem o HTML, vai para o
// painel do PC ou para o app da nuvem.
describe('a página de calibração do microfone (C6c)', () => {
  const daCalibracao = [
    'Calibração do microfone',
    'Medir o silêncio',
    'Medir a fala',
    'O que o app usa hoje',
    'Nível por percentil',
    'Tempo acima de cada limiar de RMS',
    'Fala medida pelo app',
    'Distribuição do nível, em bandas de 6 dB',
    'contados e jogados fora',
    'Parar agora',
  ]

  describe.each([
    ['local', () => local],
    ['nuvem', () => nuvem],
  ])('o bundle %s', (_, bundle) => {
    it.each(daCalibracao)('não tem %s', (texto) => {
      expect(bundle().js).not.toContain(texto)
    })

    it('não tem a página, o HTML nem o CSS dela', () => {
      expect(bundle().arquivos.filter((f) => /calibr/i.test(f))).toEqual([])
      expect(bundle().html).not.toMatch(/calibr/i)
      expect(bundle().css).not.toContain('calibracao')
    })
  })

  it('o vite.config.ts e o index.html não a conhecem: a entrada do build é uma só', async () => {
    expect(await readFile(path.join(raiz, 'vite.config.ts'), 'utf8')).not.toMatch(/calibr/i)
    expect(await readFile(path.join(raiz, 'index.html'), 'utf8')).not.toMatch(/calibr/i)
  })

  it('a página existe para o dev: um HTML próprio, com a entrada dela', async () => {
    const html = await readFile(path.join(raiz, 'calibracao-dev.html'), 'utf8')
    expect(html).toContain('<script type="module" src="/src/nuvem/ditar/calibracaoDev.tsx"></script>')
    expect(html).toContain('(só no pnpm dev)')
  })

  it('nenhum código do app importa a calibração: só os arquivos dela se conhecem', async () => {
    const arquivos = (await readdir(path.join(raiz, 'src'), { recursive: true })).map((f) => f.split(path.sep).join('/')).filter((f) => /\.(ts|tsx)$/.test(f))
    const dela = /(^|\/)(calibracao|PaginaDeCalibracao|calibracaoDev)(\.test)?\.tsx?$/
    expect(arquivos.filter((f) => dela.test(f)).sort()).toEqual([
      'nuvem/ditar/PaginaDeCalibracao.test.tsx',
      'nuvem/ditar/PaginaDeCalibracao.tsx',
      'nuvem/ditar/calibracao.test.ts',
      'nuvem/ditar/calibracao.ts',
      'nuvem/ditar/calibracaoDev.tsx',
    ])
    // Nenhuma forma de importar: `from '...'`, `import '...'` (sem efeito, o Rollup
    // a descarta do bundle e o teste de cima não a veria) e `import('...')`.
    for (const f of arquivos.filter((x) => !dela.test(x))) {
      const texto = await readFile(path.join(raiz, 'src', f), 'utf8')
      expect(texto, f).not.toMatch(/['"][^'"\n]*\/(calibracao|PaginaDeCalibracao|calibracaoDev)(\.tsx?|\.css)?(\?[^'"\n]*)?['"]/)
    }
  })
})

describe('o manifest e os ícones (F5)', () => {
  const lerManifest = async () => JSON.parse(await readFile(path.join(nuvem.dir, 'manifest.webmanifest'), 'utf8')) as Record<string, unknown>

  it('tem os campos do app instalável, e o id fixo (mudar o id é instalar outro app)', async () => {
    expect(await lerManifest()).toEqual({
      id: '/',
      name: 'Aurora Whisper',
      short_name: 'Whisper',
      description: 'Os seus ditados, no celular.',
      lang: 'pt-BR',
      dir: 'ltr',
      start_url: '/',
      scope: '/',
      display: 'standalone',
      background_color: '#070a09',
      theme_color: '#070a09',
      icons: [
        { src: '/icone-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
        { src: '/icone-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
        { src: '/icone-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
      ],
    })
  })

  it('cada ícone do manifest é um PNG do tamanho que ele diz (o cabeçalho IHDR)', async () => {
    const { icons } = (await lerManifest()) as { icons: { src: string; sizes: string }[] }
    for (const icone of icons) {
      const b = await readFile(path.join(nuvem.dir, icone.src.slice(1)))
      expect(b.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])), icone.src).toBe(true)
      expect(b.subarray(12, 16).toString('latin1'), icone.src).toBe('IHDR')
      expect(`${b.readUInt32BE(16)}x${b.readUInt32BE(20)}`, icone.src).toBe(icone.sizes)
    }
  })

  it('os ícones são os de web/pwa, sem mudança no caminho', async () => {
    for (const nome of ['icone-192.png', 'icone-512.png', 'icone-maskable-512.png']) {
      const doBuild = await readFile(path.join(nuvem.dir, nome))
      const daFonte = await readFile(path.join(raiz, 'pwa', nome))
      expect(doBuild.equals(daFonte), nome).toBe(true)
    }
  })
})

// --- o service worker emitido ------------------------------------------------

const origem = 'http://localhost:8080'

interface Pedido {
  url: string
  method: string
  mode: string
  cache: string
}

function pedido(endereco: string, init: { method?: string; mode?: string } = {}): Pedido {
  return { url: new URL(endereco, origem).href, method: init.method ?? 'GET', mode: init.mode ?? 'cors', cache: 'default' }
}

// O Request do escopo do service worker resolve o endereço relativo ao sw.js.
class RequestDoSW implements Pedido {
  url: string
  method = 'GET'
  mode = 'cors'
  cache: string
  constructor(endereco: string, init: { cache?: string } = {}) {
    this.url = new URL(endereco, origem).href
    this.cache = init.cache ?? 'default'
  }
}

type Rede = (p: Pedido) => Promise<Response>

class CacheDeMentira {
  itens = new Map<string, Response>()
  pedidosDoAddAll: Pedido[] = []
  gravacoesForaDoInstall = 0
  rede: Rede
  constructor(rede: Rede) {
    this.rede = rede
  }
  async addAll(pedidos: Pedido[]) {
    for (const p of pedidos) {
      this.pedidosDoAddAll.push(p)
      const r = await this.rede(p)
      if (!r.ok) throw new TypeError(`addAll: ${p.url} respondeu ${r.status}`)
      this.itens.set(p.url, r)
    }
  }
  async match(p: string | Pedido) {
    const r = this.itens.get(new URL(typeof p === 'string' ? p : p.url, origem).href)
    return r?.clone()
  }
  // O sw.js não grava nada fora do install: qualquer put ou add conta.
  async put(p: string | Pedido, r: Response) {
    this.gravacoesForaDoInstall++
    this.itens.set(new URL(typeof p === 'string' ? p : p.url, origem).href, r)
  }
  async add(p: string | Pedido) {
    this.gravacoesForaDoInstall++
    await this.addAll([typeof p === 'string' ? pedido(p) : p])
  }
}

class CachesDeMentira {
  mapa = new Map<string, CacheDeMentira>()
  rede: Rede
  constructor(rede: Rede) {
    this.rede = rede
  }
  async open(nome: string) {
    let c = this.mapa.get(nome)
    if (!c) {
      c = new CacheDeMentira(this.rede)
      this.mapa.set(nome, c)
    }
    return c
  }
  async keys() {
    return [...this.mapa.keys()]
  }
  async delete(nome: string) {
    return this.mapa.delete(nome)
  }
}

interface Evento {
  waitUntil(p: Promise<unknown>): void
}

// O sw.js num node:vm: o self, o caches e o fetch são de mentira, e a rede liga
// e desliga. Nada sai da máquina. A resposta diz de onde veio: "rede /x" é a
// do fetch do sw.js, e "casca /x", a que o install guardou.
function carregarSW(codigo: string) {
  const ouvintes = new Map<string, (e: unknown) => void>()
  const estado = { online: true, naRede: [] as Pedido[], ordem: [] as string[] }
  const servidor = (origemDaResposta: string): Rede => async (p) => {
    estado.naRede.push(p)
    if (!estado.online) throw new TypeError('Failed to fetch')
    const u = new URL(p.url)
    return new Response(`${origemDaResposta} ${u.pathname}${u.search}`, { status: 200 })
  }
  const rede = servidor('rede')
  const doInstall = servidor('casca')
  const caches = new CachesDeMentira(async (p) => {
    const r = await doInstall(p)
    estado.ordem.push(`guardou ${new URL(p.url).pathname}`)
    return r
  })
  const self = {
    location: new URL('/sw.js', origem),
    addEventListener: (tipo: string, f: (e: unknown) => void) => ouvintes.set(tipo, f),
    skipWaiting: async () => {
      estado.ordem.push('skipWaiting')
    },
  }
  const contexto = vm.createContext({ self, caches, fetch: rede, Request: RequestDoSW, URL })
  vm.runInContext(codigo, contexto, { filename: 'sw.js' })

  const estender = async (tipo: string) => {
    const esperas: Promise<unknown>[] = []
    const evento: Evento = { waitUntil: (p) => void esperas.push(Promise.resolve(p)) }
    ouvintes.get(tipo)?.(evento)
    await Promise.all(esperas)
  }
  // buscar devolve a resposta do respondWith, ou undefined se o sw.js não
  // respondeu (o pedido vai à rede como se ele não existisse).
  const buscar = (p: Pedido): Promise<Response> | undefined => {
    let resposta: Promise<Response> | undefined
    ouvintes.get('fetch')?.({
      request: p,
      respondWith: (r: Promise<Response> | Response) => {
        resposta = Promise.resolve(r)
      },
      waitUntil: () => {},
    })
    return resposta
  }
  const ler = <T>(nome: string): T => JSON.parse(JSON.stringify(vm.runInContext(nome, contexto))) as T
  return { estado, caches, instalar: () => estender('install'), ativar: () => estender('activate'), buscar, ler }
}

describe('o service worker emitido (sw.js)', () => {
  let codigo: string
  let casca: string[]
  let versao: string

  beforeAll(async () => {
    codigo = await readFile(path.join(nuvem.dir, 'sw.js'), 'utf8')
    const sw = carregarSW(codigo)
    casca = sw.ler<string[]>('CASCA')
    versao = sw.ler<string>('VERSAO')
  })

  it('a CASCA é o /, o favicon e todo /assets do build: sem /index.html, sem marcador sobrando', () => {
    const assets = nuvem.arquivos.filter((f) => f.startsWith('assets/')).map((f) => '/' + f)
    expect(assets.length).toBeGreaterThan(0)
    expect(casca).toEqual(['/', '/favicon.svg', ...assets.sort()])
    expect(casca).not.toContain('/index.html')
    expect(codigo).not.toContain('__CASCA__')
    expect(codigo).not.toContain('__VERSAO__')
  })

  it('a VERSAO é o hash dos nomes e do conteúdo da casca', async () => {
    const hash = createHash('sha256')
    for (const url of casca) {
      const arquivo = url === '/' ? 'index.html' : url.slice(1)
      hash.update(url + '\0')
      hash.update(await readFile(path.join(nuvem.dir, arquivo)))
      hash.update('\0')
    }
    expect(versao).toBe('aurora-casca-' + hash.digest('hex').slice(0, 12))
  })

  it('não grava nada fora do install, e não usa navigation preload', () => {
    expect(codigo).not.toMatch(/\.put\(/)
    expect(codigo).not.toMatch(/\.add\(/)
    expect(codigo).not.toContain('navigationPreload')
    expect(codigo).not.toContain('preloadResponse')
  })

  describe('rodando', () => {
    let sw: ReturnType<typeof carregarSW>
    beforeEach(async () => {
      sw = carregarSW(codigo)
      await sw.instalar()
      await sw.ativar()
    })
    const guardado = async () => [...(await sw.caches.open(versao)).itens.keys()].map((u) => new URL(u).pathname)

    it('o install guarda exatamente a CASCA, do servidor (reload), e termina com o skipWaiting', async () => {
      const cache = await sw.caches.open(versao)
      expect(cache.pedidosDoAddAll.map((p) => new URL(p.url).pathname)).toEqual(casca)
      expect(cache.pedidosDoAddAll.every((p) => p.cache === 'reload')).toBe(true)
      expect(await guardado()).toEqual(casca)
      expect(sw.estado.ordem.at(-1)).toBe('skipWaiting')
      expect(sw.estado.ordem.filter((o) => o === 'skipWaiting')).toHaveLength(1)
    })

    it('o activate apaga a casca velha e fica com a atual; cache de outro nome fica', async () => {
      await sw.caches.open('aurora-casca-000000000000')
      await sw.caches.open('outro-cache')
      await sw.ativar()
      expect((await sw.caches.keys()).sort()).toEqual([versao, 'outro-cache'].sort())
    })

    it.each(['/', '/?janela=entrou', '/?x=1'])('a navegação para %s vem da rede com rede, e da casca sem rede', async (endereco) => {
      const nav = pedido(endereco, { mode: 'navigate' })
      sw.estado.naRede.length = 0
      const comRede = sw.buscar(nav)
      expect(comRede).toBeDefined()
      expect(await (await comRede!).text()).toBe(`rede ${endereco}`)
      expect(sw.estado.naRede).toHaveLength(1)

      sw.estado.online = false
      const semRede = sw.buscar(nav)
      expect(await (await semRede!).text()).toBe('casca /')
    })

    it('o favicon vem da rede com rede, e da casca sem rede', async () => {
      expect(await (await sw.buscar(pedido('/favicon.svg', { mode: 'no-cors' }))!).text()).toBe('rede /favicon.svg')
      sw.estado.online = false
      expect(await (await sw.buscar(pedido('/favicon.svg', { mode: 'no-cors' }))!).text()).toBe('casca /favicon.svg')
    })

    it('sem rede e fora da casca, a navegação falha como sem o service worker', async () => {
      sw.estado.online = false
      await (await sw.caches.open(versao)).itens.delete(new URL('/', origem).href)
      await expect(sw.buscar(pedido('/', { mode: 'navigate' }))!).rejects.toThrow('Failed to fetch')
    })

    it('um asset da casca sai dela sem ir à rede; um que não está vai à rede e não é guardado', async () => {
      const asset = casca.find((u) => u.startsWith('/assets/'))!
      sw.estado.naRede.length = 0
      expect(await (await sw.buscar(pedido(asset))!).text()).toBe(`casca ${asset}`)
      expect(sw.estado.naRede).toEqual([])

      const novo = pedido('/assets/index-novo123.js')
      expect(await (await sw.buscar(novo)!).text()).toBe('rede /assets/index-novo123.js')
      expect(sw.estado.naRede).toEqual([novo])
      expect(await guardado()).toEqual(casca)
    })

    // A lista branca: estes pedidos vão à rede sem o service worker no meio.
    it.each<[string, Pedido]>([
      ['GET /api/v1/me', pedido('/api/v1/me')],
      ['GET /api/v1/me, navegando', pedido('/api/v1/me', { mode: 'navigate' })],
      ['GET /api/v1/transcriptions', pedido('/api/v1/transcriptions?trash=false')],
      ['POST /api/v1/transcriptions/search', pedido('/api/v1/transcriptions/search', { method: 'POST' })],
      // ADR-021: o áudio do ditado vai direto à rede, e o service worker nem o vê.
      ['POST /api/v1/dictations', pedido('/api/v1/dictations', { method: 'POST' })],
      ['POST /api/v1/device-auth/start', pedido('/api/v1/device-auth/start', { method: 'POST' })],
      ['POST /auth/start', pedido('/auth/start', { method: 'POST' })],
      ['GET /auth/google/callback, navegando', pedido('/auth/google/callback?code=c&state=s', { mode: 'navigate' })],
      ['GET /device/authorize, navegando', pedido('/device/authorize?pedido=p', { mode: 'navigate' })],
      ['POST /device/authorize', pedido('/device/authorize', { method: 'POST', mode: 'navigate' })],
      ['POST /', pedido('/', { method: 'POST', mode: 'navigate' })],
      ['GET / sem navegar', pedido('/')],
      ['GET /index.html, navegando', pedido('/index.html', { mode: 'navigate' })],
      ['GET /historico, navegando', pedido('/historico', { mode: 'navigate' })],
      ['GET /sw.js', pedido('/sw.js')],
      ['GET /manifest.webmanifest', pedido('/manifest.webmanifest')],
      ['GET /icone-192.png', pedido('/icone-192.png', { mode: 'no-cors' })],
      ['GET /licencas-fontes.txt', pedido('/licencas-fontes.txt')],
      ['o Google, navegando', pedido('https://accounts.google.com/o/oauth2/v2/auth?client_id=x', { mode: 'navigate' })],
      ['/assets de outra origem', pedido('https://exemplo.test/assets/index.js')],
    ])('não responde: %s', async (_, p) => {
      expect(sw.buscar(p)).toBeUndefined()
      sw.estado.online = false
      expect(sw.buscar(p)).toBeUndefined()
    })

    it('no fim de um uso inteiro, o cache só tem a CASCA: nada do que passou pela rede foi guardado', async () => {
      for (const p of [
        pedido('/', { mode: 'navigate' }),
        pedido('/?janela=entrar', { mode: 'navigate' }),
        pedido('/favicon.svg'),
        pedido('/assets/outro-abc.css'),
        ...casca.filter((u) => u.startsWith('/assets/')).map((u) => pedido(u)),
      ]) {
        const r = sw.buscar(p)
        if (r) await (await r).text()
      }
      sw.estado.online = false
      await (await sw.buscar(pedido('/', { mode: 'navigate' }))!).text()
      const caches = await sw.caches.keys()
      expect(caches).toEqual([versao])
      const cache = await sw.caches.open(versao)
      expect(cache.gravacoesForaDoInstall).toBe(0)
      expect(await guardado()).toEqual(casca)
    })
  })
})
