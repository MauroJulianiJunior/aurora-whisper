// As imagens das notas no app do celular (ADR-022, N10b; a prévia
// docs/notas-previa-n10.html, cenas 5 a 7) contra a nuvem de mentira
// (notas/notasFalsas.ts): o ícone de imagem da barra com o menu "Tirar foto" /
// "Escolher da galeria", a nota nova com imagem, os avisos, o ícone apagado e o
// "Imagem sem rede" quando a rede cai, e o que NUNCA acontece: os bytes da imagem
// no IndexedDB (armazemNaMemoria), no Cache API ou no service worker.
//
// O jsdom não tem canvas, createImageBitmap nem URL.createObjectURL: os três são
// dublês, e o que se prova é o que o app FAZ com eles (o tamanho pedido ao canvas, o
// JPEG, o pedido à nuvem). A redução em si é de notas/reduzirImagem.test.ts.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'
import { plataformaFalsa } from './ditar/plataformaFalsa'
import { armazemNaMemoria } from './notas/armazem'
import { daNuvem } from './notas/formato'
import { imagemFalsa, juntar, notaDaNuvem, notasFalsasNaNuvem } from './notas/notasFalsas'

vi.setConfig({ testTimeout: 20_000 })

let urls = 0
let desenhos: { largura: number; altura: number }[] = []
let abertas: string[] = []
let soltas: string[] = []

beforeEach(() => {
  urls = 0
  desenhos = []
  abertas = []
  soltas = []
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    writable: true,
    value: vi.fn((b: Blob) => {
      abertas.push(b.type)
      return `blob:aw-${++urls}`
    }),
  })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, writable: true, value: vi.fn((u: string) => void soltas.push(u)) })
  vi.stubGlobal(
    'createImageBitmap',
    vi.fn(async (b: Blob) => {
      if (b.type === 'image/heic') throw new DOMException('não abre', 'InvalidStateError')
      return { width: 4032, height: 3024, close: vi.fn() }
    }),
  )
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function (this: HTMLCanvasElement) {
    desenhos.push({ largura: this.width, altura: this.height })
    return { fillRect: vi.fn(), drawImage: vi.fn() } as unknown as CanvasRenderingContext2D
  } as never)
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb: BlobCallback, tipo?: string) {
    cb(new Blob([new Uint8Array(1234)], { type: tipo ?? 'image/png' }))
  })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const recibo = notaDaNuvem({ id: 'n1', titulo: 'Recibos', corpo: 'táxi', imagens: [imagemFalsa({ id: 'i1', largura: 800, altura: 600 })] })
const quadro = notaDaNuvem({
  id: 'n2',
  titulo: 'Quadro da reunião',
  corpo: 'os prazos',
  posicao: 'W',
  imagens: ['a', 'b', 'c', 'd', 'e'].map((x, i) => imagemFalsa({ id: `q${x}`, created_at: `2026-10-27T10:0${i}:00Z` })),
})
const lista = notaDaNuvem({ id: 'n3', titulo: 'Lista do sábado', corpo: '- [ ] leite\n- [ ] pão', posicao: 'X' })
const cheia = notaDaNuvem({ id: 'n4', titulo: 'Cheia', posicao: 'Y', imagens: Array.from({ length: 10 }, (_, i) => imagemFalsa({ id: `c${i}` })) })

function montar(iniciais = [recibo, quadro, lista, cheia]) {
  const notas = notasFalsasNaNuvem(iniciais)
  const srv = servidorFalsoNuvem([])
  const armazem = armazemNaMemoria()
  const p = plataformaFalsa()
  render(<AppNuvem buscar={juntar(notas, srv.buscar)} irPara={vi.fn()} plataformaDeDitar={p.plataforma} armazemDasNotas={() => armazem} contaGuardada={async () => null} />)
  return { notas, srv, armazem }
}

async function abrirNotas() {
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await screen.findByText('Lista do sábado')
}

const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent ?? ''
const dialogo = () => screen.getByRole('dialog', { name: /^Nota:/ })
const abrir = async (id: string) => {
  fireEvent.click(cartao(id).querySelector('[data-abrir]')!)
  return await screen.findByRole('dialog', { name: /^Nota:/ })
}
const jpg = (nome = 'IMG_2041.jpg', tipo = 'image/jpeg') => new File([new Uint8Array(64)], nome, { type: tipo })
const campoDaCamera = () => within(dialogo()).getByLabelText<HTMLInputElement>('Foto tirada agora')
const campoDaGaleria = () => within(dialogo()).getByLabelText<HTMLInputElement>('Escolher imagens para a nota')
const doCampo = (campo: HTMLInputElement, arquivos: File[]) => fireEvent.change(campo, { target: { files: arquivos } })

// cairARede é a rede que some: o navegador diz que caiu, e o pedido seguinte não chega.
async function cairARede(notas: ReturnType<typeof notasFalsasNaNuvem>) {
  notas.cair()
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
  await act(async () => {
    window.dispatchEvent(new Event('offline'))
  })
  await waitFor(() => expect(document.querySelector('.faixa-rede')?.textContent).toBe('Sem rede'))
}

async function voltarARede(notas: ReturnType<typeof notasFalsasNaNuvem>) {
  notas.voltar()
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
  await act(async () => {
    window.dispatchEvent(new Event('online'))
  })
}

describe('a grade, no cartão do celular', () => {
  it('as imagens aparecem no topo do cartão, lidas pela sessão (sem o token do PC), e a de "+N" mostra o resto', async () => {
    const m = montar()
    await abrirNotas()
    await waitFor(() => expect(cartao('n1').querySelector('.fotos.um img')?.getAttribute('src')).toMatch(/^blob:aw-/))
    const lida = m.notas.pedidos.find((p) => p.caminho === '/api/v1/notes/n1/images/i1')
    expect(lida?.metodo).toBe('GET')
    const grade = cartao('n2').querySelector('.fotos.tres')!
    expect(grade.querySelectorAll('.foto')).toHaveLength(3)
    expect(grade.querySelector('.faltam')?.textContent).toBe('+2')
    // Só as três que se veem são lidas.
    await waitFor(() => expect(m.notas.pedidos.filter((p) => p.caminho.startsWith('/api/v1/notes/n2/images/'))).toHaveLength(3))
    expect(cartao('n3').querySelector('.fotos')).toBeNull()
  })

  it('a nota só com imagem não é "Nota vazia"', async () => {
    montar([notaDaNuvem({ id: 'n9', imagens: [imagemFalsa({ id: 'z1' })] }), lista])
    await abrirNotas()
    expect(cartao('n9').classList.contains('so-imagem')).toBe(true)
    expect(cartao('n9').querySelector('[data-abrir]')!.getAttribute('aria-label')).toBe('Nota com imagem')
  })
})

describe('a barra da nota aberta no celular (cena 5)', () => {
  it('a ordem é a da prévia (Formatar, Cor, Marcadores, Imagem, Ditar aqui, Mais, Fechar) e o ícone abre o menu', async () => {
    montar()
    await abrirNotas()
    const d = await abrir('n3')
    const nomes = Array.from(d.querySelectorAll('.barra-nota > button, .barra-nota > .menu-caixa > button'), (b) => b.getAttribute('aria-label') ?? b.textContent)
    expect(nomes).toEqual(['Formatar', 'Cor', 'Marcadores', 'Imagem', 'Ditar aqui', 'Mais', 'Fechar'])
    // O Arquivar fica dentro do Mais, como na N8.
    expect(within(d).queryByRole('button', { name: 'Arquivar' })).toBeNull()

    fireEvent.click(within(d).getByRole('button', { name: 'Imagem' }))
    const menu = within(d).getByRole('group', { name: 'Imagem' })
    expect(within(menu).getByRole('button', { name: 'Tirar foto' })).toBeTruthy()
    expect(within(menu).getByRole('button', { name: 'Escolher da galeria' })).toBeTruthy()
  })

  it('"Tirar foto" abre a câmera (capture); "Escolher da galeria" abre a galeria, com várias', async () => {
    montar()
    await abrirNotas()
    await abrir('n3')
    expect(campoDaCamera().getAttribute('capture')).toBe('environment')
    expect(campoDaCamera().getAttribute('accept')).toBe('image/*')
    expect(campoDaCamera().multiple).toBe(false)
    expect(campoDaGaleria().hasAttribute('capture')).toBe(false)
    expect(campoDaGaleria().multiple).toBe(true)
    const cliques: string[] = []
    campoDaCamera().addEventListener('click', () => cliques.push('camera'))
    campoDaGaleria().addEventListener('click', () => cliques.push('galeria'))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Imagem' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Tirar foto' }))
    // O menu fecha, e o campo (que mora fora dele) continua na página para receber a foto.
    expect(within(dialogo()).queryByRole('group', { name: 'Imagem' })).toBeNull()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Imagem' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Escolher da galeria' }))
    expect(cliques).toEqual(['camera', 'galeria'])
  })
})

describe('anexar pelo celular', () => {
  it('a foto da câmera: reduz a 2.000 px, mostra "guardando…" e manda só o JPEG reduzido à /api/v1', async () => {
    const m = montar()
    await abrirNotas()
    await abrir('n3')
    let solta = () => {}
    const portao = new Promise<void>((ok) => (solta = ok))
    const buscar = m.notas.buscar
    vi.spyOn(m.notas, 'buscar').mockImplementation(async (e, init) => {
      if (String(e).endsWith('/images') && init?.method === 'POST') await portao
      return buscar(e, init)
    })
    doCampo(campoDaCamera(), [jpg()])
    expect(await within(dialogo()).findByText('guardando…')).toBeTruthy()
    expect(desenhos).toEqual([{ largura: 2000, altura: 1500 }])
    solta()
    await waitFor(() => expect(within(dialogo()).queryByText('guardando…')).toBeNull())
    const [p] = m.notas.anexos()
    expect(p?.caminho).toBe('/api/v1/notes/n3/images')
    expect(p?.tipo).toBe('image/jpeg')
    expect(p?.bytes).toBe(1234)
    // O original (e o nome dele) nunca sai do aparelho.
    expect(JSON.stringify(m.notas.pedidos)).not.toContain('IMG_2041')
    expect(within(dialogo()).getAllByRole('button', { name: /^Abrir a imagem \d de 1$/ })).toHaveLength(1)
    // A versão da nota não mudou: a janela segue salvando o texto sem 409.
    expect(m.notas.notas.get('n3')!.version).toBe(1)
  })

  it('a galeria com várias: sobem uma de cada vez, na ordem em que foram escolhidas', async () => {
    const m = montar()
    await abrirNotas()
    await abrir('n3')
    doCampo(campoDaGaleria(), [jpg('a.jpg'), jpg('b.jpg'), jpg('c.jpg')])
    await waitFor(() => expect(m.notas.anexos()).toHaveLength(3))
    await waitFor(() => expect(within(dialogo()).getAllByRole('button', { name: /^Abrir a imagem \d de 3$/ })).toHaveLength(3))
    expect(m.notas.notas.get('n3')!.imagens!.map((i) => i.id)).toEqual(['img-1', 'img-2', 'img-3'])
  })

  it('o ícone do "Criar uma nota…" cria a nota só com a imagem (with-image) e a abre; o menu tem câmera e galeria', async () => {
    const m = montar()
    await abrirNotas()
    fireEvent.click(screen.getByRole('button', { name: 'Nova nota com imagem' }))
    const menu = screen.getByRole('group', { name: 'Nova nota com imagem' })
    expect(within(menu).getByRole('button', { name: 'Tirar foto' })).toBeTruthy()
    fireEvent.click(within(menu).getByRole('button', { name: 'Escolher da galeria' }))
    doCampo(screen.getByLabelText<HTMLInputElement>('Escolher a imagem da nota nova'), [jpg()])
    await waitFor(() => expect(m.notas.anexos().map((p) => p.caminho)).toEqual(['/api/v1/notes/with-image']))
    const d = await screen.findByRole('dialog', { name: 'Nota: Nota com imagem' })
    expect(within(d).getAllByRole('button', { name: 'Abrir a imagem 1 de 1' })).toHaveLength(1)
    fireEvent.click(within(d).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    const nova = [...m.notas.notas.values()].find((n) => (n.imagens ?? []).length === 1 && n.titulo === '' && n.id.startsWith('nova-'))!
    expect(cartao(nova.id).classList.contains('so-imagem')).toBe(true)
  })

  it('a imagem grande, o Baixar e o tirar com Desfazer funcionam como no PC', async () => {
    const m = montar()
    await abrirNotas()
    const d = await abrir('n1')
    fireEvent.click(within(d).getByRole('button', { name: 'Abrir a imagem 1 de 1' }))
    const luz = screen.getByRole('dialog', { name: 'Imagem 1 de 1' })
    await waitFor(() => expect(within(luz).getByRole('link', { name: 'Baixar' }).getAttribute('href')).toMatch(/^blob:aw-/))
    expect(within(luz).getByRole('link', { name: 'Baixar' }).getAttribute('download')).toBe('nota-imagem-1.jpg')
    fireEvent.click(within(luz).getByRole('button', { name: 'Tirar da nota' }))
    expect(aviso()).toContain('Imagem tirada da nota.')
    fireEvent.click(within(document.querySelector<HTMLElement>('.aviso-regiao')!).getByRole('button', { name: 'Desfazer' }))
    expect(within(d).getByRole('button', { name: 'Abrir a imagem 1 de 1' })).toBeTruthy()
    await act(async () => {})
    expect(m.notas.pedidos.filter((p) => p.metodo === 'DELETE')).toHaveLength(0)

    // Sem o Desfazer, a imagem sai de vez quando a página fecha, com keepalive.
    fireEvent.click(within(d).getByRole('button', { name: 'Tirar a imagem 1 da nota' }))
    window.dispatchEvent(new Event('pagehide'))
    await waitFor(() => expect(m.notas.pedidos.filter((p) => p.metodo === 'DELETE' && p.caminho === '/api/v1/notes/n1/images/i1')).toHaveLength(1))
    expect(m.notas.notas.get('n1')!.imagens).toEqual([])
  })
})

describe('os avisos do celular (cena 7)', () => {
  it('o teto da conta mostra a frase da nuvem; a nota com 10; o arquivo que não abre; o de mais de 10 MB', async () => {
    const m = montar()
    await abrirNotas()
    await abrir('n3')
    m.notas.noProximoAnexo(m.notas.erro(409, 'IMAGES_FULL', 'As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras.'))
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(aviso()).toContain('As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras.'))
    expect(within(dialogo()).queryByText('guardando…')).toBeNull()

    const gigante = jpg('gigante.jpg')
    Object.defineProperty(gigante, 'size', { value: 10 * 1024 * 1024 + 1 })
    doCampo(campoDaGaleria(), [gigante])
    await waitFor(() => expect(aviso()).toContain('A imagem passou de 10 MB.'))
    doCampo(campoDaGaleria(), [jpg('foto.heic', 'image/heic')])
    await waitFor(() => expect(aviso()).toContain('Esse arquivo não é uma imagem que o navegador abra.'))
    expect(m.notas.anexos()).toHaveLength(1)

    // A nota que já tem 10: o celular nem pede.
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await abrir('n4')
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(aviso()).toContain('A nota já tem 10 imagens.'))
    expect(m.notas.anexos()).toHaveLength(1)
  })

  it('a recusa do servidor (400 e 413) vira a frase do aviso, e a nota na lixeira diz por quê', async () => {
    const m = montar()
    await abrirNotas()
    await abrir('n3')
    m.notas.noProximoAnexo(m.notas.erro(400, 'IMAGE_INVALID', 'não é um JPEG'), m.notas.erro(413, 'IMAGE_TOO_LARGE', 'passou de 4 MB'), m.notas.erro(409, 'TOO_MANY_IMAGES', 'a nota já tem 10'))
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(aviso()).toContain('Esse arquivo não é uma imagem que o navegador abra.'))
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(aviso()).toContain('A imagem ficou grande demais mesmo reduzida.'))
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(aviso()).toContain('A nota já tem 10 imagens.'))
  })

  it('a rede que cai antes de a imagem subir', async () => {
    const m = montar()
    await abrirNotas()
    await abrir('n3')
    const buscar = m.notas.buscar
    vi.spyOn(m.notas, 'buscar').mockImplementation(async (e, init) => {
      if (String(e).endsWith('/images') && init?.method === 'POST') throw new TypeError('Failed to fetch')
      return buscar(e, init)
    })
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(aviso()).toContain('A rede caiu antes de a imagem subir. Tente de novo.'))
    expect(within(dialogo()).queryByText('guardando…')).toBeNull()
  })
})

describe('sem rede (cena 6)', () => {
  it('o ícone de imagem fica apagado, na nota aberta e no "Criar uma nota…", até a rede voltar', async () => {
    const m = montar()
    await abrirNotas()
    const d = await abrir('n3')
    expect(within(d).getByRole<HTMLButtonElement>('button', { name: 'Imagem' }).disabled).toBe(false)
    await cairARede(m.notas)
    expect(within(d).getByRole<HTMLButtonElement>('button', { name: 'Imagem' }).disabled).toBe(true)
    fireEvent.click(within(d).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Nova nota com imagem' }).disabled).toBe(true)

    await voltarARede(m.notas)
    await waitFor(() => expect(document.querySelector('.faixa-rede')).toBeNull())
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Nova nota com imagem' }).disabled).toBe(false)
  })

  it('a imagem que não carrega vira "Imagem sem rede" (só a primeira diz), com a proporção; com a rede, carrega', async () => {
    const armazem = armazemNaMemoria({ notas: [daNuvem(recibo), daNuvem(quadro), daNuvem(lista)] })
    const notas = notasFalsasNaNuvem([recibo, quadro, lista])
    // O app abre sem rede: nenhum pedido chega (nem o /me), até a rede voltar.
    let comRede = false
    const nuvem = juntar(notas, servidorFalsoNuvem([]).buscar)
    const p = plataformaFalsa()
    render(
      <AppNuvem
        buscar={(e, i) => (comRede ? nuvem(e, i) : Promise.reject(new TypeError('Failed to fetch')))}
        irPara={vi.fn()}
        plataformaDeDitar={p.plataforma}
        armazemDasNotas={() => armazem}
        contaGuardada={async () => ({ id: 'u1', email: 'mauro@exemplo.test' })}
      />,
    )
    expect(await screen.findByText('Quadro da reunião')).toBeTruthy()
    await waitFor(() => expect(document.querySelector('.faixa-rede')?.textContent).toBe('Sem rede'))
    // O recibo: um quadrado, com o texto. O quadro: três, só o primeiro com o texto.
    expect(within(cartao('n1')).getByText('Imagem sem rede')).toBeTruthy()
    expect(cartao('n1').querySelector<HTMLElement>('.foto')!.style.aspectRatio).toMatch(/^1\.333/)
    expect(cartao('n2').querySelectorAll('.foto.sem-rede')).toHaveLength(3)
    expect(within(cartao('n2')).getAllByText('Imagem sem rede')).toHaveLength(1)
    expect(cartao('n1').querySelector('img')).toBeNull()
    // Nenhum "Imagem indisponível": a falta de rede não é erro da imagem.
    expect(document.body.textContent).not.toContain('Imagem indisponível')

    comRede = true
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
    await act(async () => {
      window.dispatchEvent(new Event('online'))
    })
    await waitFor(() => expect(cartao('n1').querySelector('.fotos.um img')?.getAttribute('src')).toMatch(/^blob:aw-/))
    expect(document.body.textContent).not.toContain('Imagem sem rede')
  })

  it('a imagem que já apareceu nesta visita continua aparecendo quando a rede cai; a outra vira "Imagem sem rede"', async () => {
    const m = montar()
    await abrirNotas()
    await waitFor(() => expect(cartao('n1').querySelector('.fotos.um img')).toBeTruthy())
    // O quadro estava longe da tela? No jsdom tudo é visível: as três do quadro também foram lidas.
    await waitFor(() => expect(cartao('n2').querySelectorAll('.foto img')).toHaveLength(3))
    await cairARede(m.notas)
    expect(cartao('n1').querySelector('.fotos.um img')?.getAttribute('src')).toMatch(/^blob:aw-/)
    expect(cartao('n2').querySelectorAll('.foto img')).toHaveLength(3)
    // Uma nota aberta agora, sem rede, com uma imagem que não chegou a ser lida, mostra a caixa.
    expect(document.body.textContent).not.toContain('Imagem sem rede')
  })

  it('a imagem que não foi lida e a rede que cai: a nota aberta mostra as caixas, e o Tirar avisa que precisa de rede', async () => {
    const m = montar()
    // A nota "cheia" fica fora das três primeiras leituras? Aqui as lidas são as do cartão; a cheia tem 10 e lê 3.
    await abrirNotas()
    await cairARede(m.notas)
    const d = await abrir('n4')
    const caixas = d.querySelectorAll('.foto.sem-rede, .foto img')
    expect(caixas.length).toBeGreaterThan(0)
    const tirar = within(d).queryAllByRole('button', { name: /^Tirar a imagem/ })[0]!
    fireEvent.click(tirar)
    expect(aviso()).toContain('Sem rede: a imagem só sai da nota quando a rede voltar.')
    expect(m.notas.pedidos.filter((p) => p.metodo === 'DELETE')).toHaveLength(0)
  })
})

describe('os bytes da imagem nunca ficam no aparelho', () => {
  it('só os metadados da imagem entram no banco das notas (armazemNaMemoria); nenhum byte, nenhuma URL', async () => {
    const m = montar()
    await abrirNotas()
    await waitFor(() => expect(cartao('n1').querySelector('.fotos.um img')).toBeTruthy())
    await abrir('n3')
    doCampo(campoDaGaleria(), [jpg()])
    await waitFor(() => expect(m.notas.anexos()).toHaveLength(1))
    await act(async () => {})
    const guardadas = m.armazem.foto().notas
    const n1 = guardadas.find((n) => n.id === 'n1')!
    // O que se guarda é o que a nuvem mandou: id, tamanhos e sha256.
    expect(n1.imagens).toEqual([expect.objectContaining({ id: 'i1', largura: 800, altura: 600, bytes: 400_000, sha256: 'a'.repeat(64) })])
    const n3 = guardadas.find((n) => n.id === 'n3')!
    expect(n3.imagens!.map((i) => i.id)).toEqual(['img-1'])
    const visitar = (v: unknown): void => {
      if (v instanceof Blob || v instanceof ArrayBuffer || ArrayBuffer.isView(v)) throw new Error('bytes no banco das notas')
      if (typeof v === 'string') expect(v.startsWith('blob:')).toBe(false)
      if (v && typeof v === 'object') for (const x of Object.values(v)) visitar(x)
    }
    visitar(m.armazem.foto())
    // A fila (o que se fez sem rede) também não leva imagem.
    expect(m.armazem.foto().fila).toEqual([])
  })

  it('a URL blob: só nasce de um JPEG, e o app a solta ao sair da aba', async () => {
    montar()
    await abrirNotas()
    await waitFor(() => expect(cartao('n1').querySelector('.fotos.um img')).toBeTruthy())
    expect(abertas.length).toBeGreaterThan(0)
    expect(new Set(abertas)).toEqual(new Set(['image/jpeg']))
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await waitFor(() => expect(soltas.length).toBeGreaterThan(0))
  })

  it('o cliente da imagem recusa o que não é JPEG (o áudio do ditado nunca vira URL blob:)', async () => {
    const { urlDaImagem } = await import('./notas/urlDaImagem')
    expect(() => urlDaImagem(new Blob([new Uint8Array(8)], { type: 'audio/webm' }))).toThrow()
    expect(() => urlDaImagem(new Blob([new Uint8Array(8)], { type: '' }))).toThrow()
    expect(urlDaImagem(new Blob([new Uint8Array(8)], { type: 'image/jpeg' }))).toMatch(/^blob:/)
  })

  it('a resposta da nuvem que não é um JPEG não vira imagem (Imagem indisponível)', async () => {
    const m = montar([recibo, lista])
    const buscar = m.notas.buscar
    vi.spyOn(m.notas, 'buscar').mockImplementation(async (e, init) => {
      if (String(e).includes('/images/')) return new Response('<html>', { status: 200, headers: { 'Content-Type': 'text/html' } })
      return buscar(e, init)
    })
    await abrirNotas()
    await waitFor(() => expect(within(cartao('n1')).getByText('Imagem indisponível')).toBeTruthy())
    expect(cartao('n1').querySelector('img')).toBeNull()
  })
})
