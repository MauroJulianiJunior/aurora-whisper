// As imagens das notas no painel do PC (ADR-022, N10; a prévia
// docs/notas-previa-n10.html, cenas 1 a 4 e 7) contra o Agent de mentira
// (testes/servidorFalso.ts): a grade no cartão e na nota, a nota só com imagem, o
// ícone da barra, o Ctrl+V e o arrastar (eventos sintéticos: nada toca a área de
// transferência nem o Windows), o "guardando…", a imagem grande, o tirar com
// Desfazer e os avisos.
//
// O jsdom não tem canvas, createImageBitmap nem URL.createObjectURL: os três são
// dublês, e o que se prova é o que o painel FAZ com eles (o tamanho pedido ao
// canvas, o JPEG, o pedido ao Agent). A redução em si é de notas/reduzirImagem.test.ts.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type OpcoesDoServidor, type Pedido, imagem, nota, servidorFalso } from './testes/servidorFalso'
import type { Nota } from './notas/tipos'

let urls = 0
let desenhos: { largura: number; altura: number }[] = []

beforeEach(() => {
  window.localStorage.clear()
  urls = 0
  desenhos = []
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, writable: true, value: vi.fn(() => `blob:aw-${++urls}`) })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, writable: true, value: vi.fn() })
  // O navegador abre o JPEG, o PNG e o print; o HEIC (e o que não é imagem) não.
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

const recibo = () => nota({ id: 'n1', titulo: 'Recibos', corpo: 'táxi', imagens: [imagem({ id: 'i1', largura: 800, altura: 600 })] })
const quadro = () =>
  nota({
    id: 'n2',
    titulo: 'Quadro da reunião',
    corpo: 'os prazos',
    imagens: ['a', 'b', 'c', 'd', 'e'].map((x, i) => imagem({ id: `q${x}`, created_at: new Date(2026, 9, 25, 9, i).toISOString() })),
  })
const print = () => nota({ id: 'n3', imagens: [imagem({ id: 'p1', largura: 1200, altura: 800 })] })
const texto = () => nota({ id: 'n4', titulo: 'Só texto', corpo: 'ligar às cinco' })

async function abrirNotas(notas: Nota[] = [recibo(), quadro(), print(), texto()], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([], { notas, ...opcoes })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/marcadores')).toBe(true))
  await act(async () => {})
  return srv
}

const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent ?? ''
const dialogo = () => screen.getByRole('dialog', { name: /^Nota:/ })
const anexos = (srv: { pedidos: Pedido[] }, id: string) => srv.pedidos.filter((p) => p.caminho === `/api/notas/${id}/imagens` && p.metodo === 'POST')
const tiradas = (srv: { pedidos: Pedido[] }, id: string, imagemId: string) =>
  srv.pedidos.filter((p) => p.caminho === `/api/notas/${id}/imagens/${imagemId}` && p.metodo === 'DELETE')
const jpg = (nome = 'recibo-taxi.jpg', tipo = 'image/jpeg') => new File([new Uint8Array(64)], nome, { type: tipo })

async function abrir(id: string) {
  fireEvent.click(cartao(id).querySelector('[data-abrir]')!)
  return dialogo()
}

function escolherNaBarra(arquivos: File[]) {
  const campo = within(dialogo()).getByLabelText<HTMLInputElement>('Escolher imagens para a nota')
  fireEvent.change(campo, { target: { files: arquivos } })
}

describe('a grade, no cartão', () => {
  it('uma imagem de ponta a ponta; cinco viram três quadrados com "+2"; a nota só com imagem é só ela', async () => {
    const srv = await abrirNotas()
    await waitFor(() => expect(cartao('n1').querySelector('.fotos.um img')?.getAttribute('src')).toMatch(/^blob:aw-/))
    // A imagem chega por fetch com o token: um <img> apontando para a API não o levaria.
    const lida = srv.pedidos.find((p) => p.caminho === '/api/notas/n1/imagens/i1')
    expect(lida?.metodo).toBe('GET')
    expect(lida?.auth).toBe('Bearer tok')
    // A primeira imagem do cartão tem a proporção dela (800 x 600).
    expect(cartao('n1').querySelector<HTMLElement>('.foto')!.style.aspectRatio).toMatch(/^1\.333/)

    const grade = cartao('n2').querySelector('.fotos.tres')!
    expect(grade.querySelectorAll('.foto')).toHaveLength(3)
    expect(grade.querySelector('.faltam')?.textContent).toBe('+2')
    // Só as três que se veem são lidas.
    await waitFor(() => expect(srv.pedidos.filter((p) => p.caminho.startsWith('/api/notas/n2/imagens/'))).toHaveLength(3))

    expect(cartao('n3').classList.contains('so-imagem')).toBe(true)
    expect(cartao('n3').querySelector('[data-abrir]')!.getAttribute('aria-label')).toBe('Nota com imagem')
    expect(within(cartao('n3')).queryByText('Nota vazia')).toBeNull()
    expect(cartao('n4').querySelector('.fotos')).toBeNull()
  })

  it('a forma "lista" diz quantas imagens a nota tem; a lixeira também', async () => {
    const lixo = nota({ id: 'n9', titulo: 'No lixo', corpo: 'x', deleted_at: new Date(2026, 9, 26).toISOString(), imagens: [imagem({ id: 'z1' }), imagem({ id: 'z2' })] })
    await abrirNotas([quadro(), lixo])
    fireEvent.click(screen.getByRole('button', { name: 'Lista' }))
    expect(within(cartao('n2')).getByText('5 imagens')).toBeTruthy()
    fireEvent.click(within(screen.getByRole('group', { name: 'Marcadores' })).getByRole('button', { name: 'Lixeira' }))
    expect(await screen.findByText(/2 imagens/)).toBeTruthy()
  })
})

describe('a nota aberta', () => {
  it('as imagens ficam em cima do título, cada uma abre grande, e a meta conta quantas são', async () => {
    await abrirNotas()
    const d = await abrir('n2')
    const botoes = within(d).getAllByRole('button', { name: /^Abrir a imagem \d de 5$/ })
    expect(botoes).toHaveLength(3)
    expect(d.querySelector('.fotos')!.compareDocumentPosition(within(d).getByRole('textbox', { name: 'Título' })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(d.querySelector('.meta')!.textContent).toContain('5 imagens')
    // O ícone de imagem da barra deixou de estar apagado.
    expect(within(d).getByRole('button', { name: 'Imagem' }).hasAttribute('disabled')).toBe(false)
  })

  it('a imagem grande: "3 de 5", as setas, Baixar a cópia reduzida e o Esc, que fecha só ela', async () => {
    await abrirNotas()
    const d = await abrir('n2')
    fireEvent.click(within(d).getByRole('button', { name: 'Abrir a imagem 3 de 5' }))
    const luz = screen.getByRole('dialog', { name: 'Imagem 3 de 5' })
    expect(luz.querySelector('.conta')!.textContent).toBe('3 de 5 · Quadro da reunião')
    expect(document.activeElement).toBe(within(luz).getByRole('button', { name: 'Fechar a imagem' }))
    await waitFor(() => expect(within(luz).getByRole('link', { name: 'Baixar' }).getAttribute('href')).toMatch(/^blob:aw-/))
    expect(within(luz).getByRole('link', { name: 'Baixar' }).getAttribute('download')).toBe('nota-imagem-3.jpg')

    fireEvent.click(within(luz).getByRole('button', { name: 'Próxima imagem' }))
    expect(screen.getByRole('dialog', { name: 'Imagem 4 de 5' })).toBeTruthy()
    fireEvent.keyDown(screen.getByRole('dialog', { name: 'Imagem 4 de 5' }), { key: 'ArrowRight' })
    fireEvent.keyDown(screen.getByRole('dialog', { name: 'Imagem 5 de 5' }), { key: 'ArrowRight' })
    expect(screen.getByRole('dialog', { name: 'Imagem 1 de 5' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Imagem anterior' }))
    expect(screen.getByRole('dialog', { name: 'Imagem 5 de 5' })).toBeTruthy()

    fireEvent.keyDown(screen.getByRole('dialog', { name: 'Imagem 5 de 5' }), { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: /^Imagem/ })).toBeNull()
    // A nota continua aberta: o Esc da imagem grande não é o da nota.
    expect(dialogo()).toBeTruthy()
  })
})

describe('anexar', () => {
  it('pelo ícone da barra: reduz a 2.000 px, mostra "guardando…" e manda só o JPEG reduzido, com o token', async () => {
    const srv = await abrirNotas()
    await abrir('n1')
    const soltar = srv.pausar((p) => p.caminho === '/api/notas/n1/imagens' && p.metodo === 'POST')
    escolherNaBarra([jpg()])
    expect(await within(dialogo()).findByText('guardando…')).toBeTruthy()
    expect(desenhos).toEqual([{ largura: 2000, altura: 1500 }])
    soltar()
    await waitFor(() => expect(within(dialogo()).queryByText('guardando…')).toBeNull())
    const [p] = anexos(srv, 'n1')
    expect(p?.tipo).toBe('image/jpeg')
    expect(p?.auth).toBe('Bearer tok')
    expect(p?.blob?.type).toBe('image/jpeg')
    expect(p?.blob?.size).toBe(1234)
    // O original (e o nome dele) nunca sai da página.
    expect(p?.blob).not.toBeInstanceOf(File)
    expect(JSON.stringify(srv.pedidos)).not.toContain('recibo-taxi')
    expect(within(dialogo()).getAllByRole('button', { name: /^Abrir a imagem \d de 2$/ })).toHaveLength(2)
    // A versão da nota não mudou: a janela segue salvando o texto sem 409.
    fireEvent.change(within(dialogo()).getByRole('textbox', { name: 'Título' }), { target: { value: 'Recibos de outubro' } })
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(srv.notas.find((n) => n.id === 'n1')!.titulo).toBe('Recibos de outubro')
    // O cartão já mostra as duas.
    expect(cartao('n1').querySelectorAll('.fotos.dois .foto')).toHaveLength(2)
  })

  it('o Ctrl+V de um print anexa; com texto junto, o colar é o de sempre', async () => {
    const srv = await abrirNotas()
    await abrir('n4')
    const titulo = within(dialogo()).getByRole('textbox', { name: 'Título' })
    const png = jpg('image.png', 'image/png')
    fireEvent.paste(titulo, { clipboardData: { getData: () => '', files: [png], items: [], types: ['Files'] } })
    await waitFor(() => expect(anexos(srv, 'n4')).toHaveLength(1))

    fireEvent.paste(titulo, { clipboardData: { getData: (t: string) => (t === 'text/plain' ? 'célula do Excel' : ''), files: [png], items: [], types: ['text/plain', 'Files'] } })
    await act(async () => {})
    expect(anexos(srv, 'n4')).toHaveLength(1)
  })

  it('arrastar um arquivo do Windows: "Solte para anexar à nota", e soltar anexa', async () => {
    const srv = await abrirNotas()
    const d = await abrir('n4')
    const arquivo = jpg()
    fireEvent.dragEnter(d, { dataTransfer: { types: ['Files'], files: [] } })
    expect(within(d).getByText('Solte para anexar à nota')).toBeTruthy()
    fireEvent.dragLeave(d, { dataTransfer: { types: ['Files'], files: [] } })
    expect(within(d).queryByText('Solte para anexar à nota')).toBeNull()

    fireEvent.dragEnter(d, { dataTransfer: { types: ['Files'], files: [] } })
    fireEvent.drop(d, { dataTransfer: { types: ['Files'], files: [arquivo] } })
    expect(within(d).queryByText('Solte para anexar à nota')).toBeNull()
    await waitFor(() => expect(anexos(srv, 'n4')).toHaveLength(1))

    // Um arrastar de texto (ou de um item da página) não é anexo.
    fireEvent.dragEnter(d, { dataTransfer: { types: ['text/plain'], files: [] } })
    expect(within(d).queryByText('Solte para anexar à nota')).toBeNull()
    // Soltar só um PDF diz por que não anexou.
    fireEvent.drop(d, { dataTransfer: { types: ['Files'], files: [new File(['%PDF'], 'contrato.pdf', { type: 'application/pdf' })] } })
    await waitFor(() => expect(aviso()).toContain('Esse arquivo não é uma imagem que o navegador abra.'))
    expect(anexos(srv, 'n4')).toHaveLength(1)
  })

  it('soltar um arquivo fora da nota aberta não faz o navegador trocar o painel pela imagem', async () => {
    const srv = await abrirNotas()
    // Sem nota aberta, na grade: o navegador não abre o arquivo (o evento sai
    // cancelado), e nada é anexado.
    expect(fireEvent.dragOver(cartao('n4'), { dataTransfer: { types: ['Files'], files: [] } })).toBe(false)
    expect(fireEvent.drop(cartao('n4'), { dataTransfer: { types: ['Files'], files: [jpg()] } })).toBe(false)
    // Um arrastar de texto segue o caminho de sempre.
    expect(fireEvent.drop(cartao('n4'), { dataTransfer: { types: ['text/plain'], files: [] } })).toBe(true)
    // Com a nota aberta, o soltar no véu também não sai do painel.
    const d = await abrir('n4')
    const veu = d.parentElement!
    expect(fireEvent.drop(veu, { dataTransfer: { types: ['Files'], files: [jpg()] } })).toBe(false)
    await act(async () => {})
    expect(anexos(srv, 'n4')).toHaveLength(0)
  })

  it('o ícone de imagem do "Criar uma nota…" cria a nota só com a imagem e a abre', async () => {
    const srv = await abrirNotas([texto()])
    const campo = screen.getByLabelText<HTMLInputElement>('Escolher a imagem da nota nova')
    expect(screen.getByRole('button', { name: 'Nova nota com imagem' }).hasAttribute('disabled')).toBe(false)
    fireEvent.change(campo, { target: { files: [jpg()] } })
    await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/notas/com-imagem' && p.metodo === 'POST' && p.tipo === 'image/jpeg')).toBe(true))
    const d = await screen.findByRole('dialog', { name: 'Nota: Nota com imagem' })
    expect(within(d).getAllByRole('button', { name: 'Abrir a imagem 1 de 1' })).toHaveLength(1)
    fireEvent.click(within(d).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    const nova = srv.notas.find((n) => n.id !== 'n4')!
    expect(cartao(nova.id).classList.contains('so-imagem')).toBe(true)
  })
})

describe('tirar da nota', () => {
  it('o × esconde na hora, e o Desfazer devolve sem pedir nada ao Agent', async () => {
    const srv = await abrirNotas()
    const d = await abrir('n1')
    fireEvent.click(within(d).getByRole('button', { name: 'Tirar a imagem 1 da nota' }))
    expect(within(d).queryByRole('button', { name: /^Abrir a imagem/ })).toBeNull()
    expect(aviso()).toContain('Imagem tirada da nota.')
    fireEvent.click(within(document.querySelector<HTMLElement>('.aviso-regiao')!).getByRole('button', { name: 'Desfazer' }))
    expect(within(d).getByRole('button', { name: 'Abrir a imagem 1 de 1' })).toBeTruthy()
    await act(async () => {})
    expect(tiradas(srv, 'n1', 'i1')).toHaveLength(0)
  })

  it('sem o Desfazer, a imagem sai de vez quando o aviso sai (outro aviso, ou a página fechando)', async () => {
    const srv = await abrirNotas()
    const d = await abrir('n2')
    fireEvent.click(within(d).getByRole('button', { name: 'Tirar a imagem 1 da nota' }))
    expect(tiradas(srv, 'n2', 'qa')).toHaveLength(0)
    // O segundo aviso toma o lugar do primeiro: o Desfazer da primeira já não dá.
    fireEvent.click(within(d).getByRole('button', { name: 'Tirar a imagem 1 da nota' }))
    await waitFor(() => expect(tiradas(srv, 'n2', 'qa')).toHaveLength(1))
    expect(tiradas(srv, 'n2', 'qb')).toHaveLength(0)
    expect(srv.notas.find((n) => n.id === 'n2')!.imagens!.map((i) => i.id)).toEqual(['qb', 'qc', 'qd', 'qe'])

    window.dispatchEvent(new Event('pagehide'))
    await waitFor(() => expect(tiradas(srv, 'n2', 'qb')).toHaveLength(1))
    expect(tiradas(srv, 'n2', 'qb')[0]!.keepalive).toBe(true)
    // O cartão também perdeu as duas.
    expect(cartao('n2').querySelector('.faltam')?.textContent).toBeUndefined()
  })

  it('"Tirar da nota" na imagem grande mostra a seguinte; a última fecha a imagem grande', async () => {
    await abrirNotas()
    const d = await abrir('n1')
    fireEvent.click(within(d).getByRole('button', { name: 'Abrir a imagem 1 de 1' }))
    fireEvent.click(within(screen.getByRole('dialog', { name: 'Imagem 1 de 1' })).getByRole('button', { name: 'Tirar da nota' }))
    expect(screen.queryByRole('dialog', { name: /^Imagem/ })).toBeNull()
    expect(aviso()).toContain('Imagem tirada da nota.')
  })
})

describe('os avisos', () => {
  it('a imagem de mais de 10 MB, a nota com 10, o arquivo que não abre e o teto', async () => {
    const cheia = nota({ id: 'n5', titulo: 'Cheia', imagens: Array.from({ length: 10 }, (_, i) => imagem({ id: `c${i}` })) })
    const srv = await abrirNotas([texto(), cheia], { tetoDasImagens: true })
    await abrir('n4')
    const gigante = jpg('gigante.jpg')
    Object.defineProperty(gigante, 'size', { value: 10 * 1024 * 1024 + 1 })
    escolherNaBarra([gigante])
    await waitFor(() => expect(aviso()).toContain('A imagem passou de 10 MB.'))

    escolherNaBarra([jpg('foto.heic', 'image/heic')])
    await waitFor(() => expect(aviso()).toContain('Esse arquivo não é uma imagem que o navegador abra.'))
    expect(anexos(srv, 'n4')).toHaveLength(0)

    // O teto vem do Agent, com o número na frase.
    escolherNaBarra([jpg()])
    await waitFor(() => expect(aviso()).toContain('As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras.'))
    expect(within(dialogo()).queryByText('guardando…')).toBeNull()

    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await abrir('n5')
    escolherNaBarra([jpg()])
    await waitFor(() => expect(aviso()).toContain('A nota já tem 10 imagens.'))
    expect(anexos(srv, 'n5')).toHaveLength(0)
  })

  it('a rede que cai antes de a imagem subir', async () => {
    const srv = await abrirNotas()
    await abrir('n4')
    srv.interceptar((p) => {
      if (p.caminho === '/api/notas/n4/imagens') throw new TypeError('Failed to fetch')
      return undefined
    })
    escolherNaBarra([jpg()])
    await waitFor(() => expect(aviso()).toContain('A rede caiu antes de a imagem subir. Tente de novo.'))
    expect(within(dialogo()).queryByText('guardando…')).toBeNull()
  })
})

describe('o que não olha a imagem', () => {
  it('copiar e copiar para WhatsApp levam só o texto', async () => {
    const writeText = vi.fn(async () => undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    await abrirNotas()
    await abrir('n1')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copiar texto' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('Recibos\n\ntáxi'))
    Reflect.deleteProperty(navigator, 'clipboard')
  })
})
