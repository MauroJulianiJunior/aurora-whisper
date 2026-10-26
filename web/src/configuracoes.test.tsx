// A tela de Configurações contra o Agent de mentira, que segue o contrato
// comum de 29-set-2026 (GET/PUT /api/settings, /api/ia/chave,
// /api/ia/testar).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

type Servidor = ReturnType<typeof servidorFalso>

const umDitado = () => [ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda para o Thiago.' })]

function montar(opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso(umDitado(), opcoes)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

async function abrir(opcoes: OpcoesDoServidor = {}) {
  const srv = montar(opcoes)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Ditado' })
  return srv
}

const botao = (nome: string | RegExp) => screen.getByRole<HTMLButtonElement>('button', { name: nome })
const salvar = () => botao('Salvar configurações')
// Os botões desta tela se desligam por aria-disabled: um disabled perde o
// foco no clique. Um disabled de verdade devolve 'disabled', que não é
// nem true nem false, e o expect falha.
const desligado = (b: HTMLButtonElement) => (b.disabled ? 'disabled' : b.getAttribute('aria-disabled') === 'true')
const segurar = () => screen.getByLabelText<HTMLInputElement>('Segurar por')
const bip = () => screen.getByLabelText<HTMLInputElement>('Bip ao começar a gravar')
const microfone = () => screen.getByLabelText<HTMLInputElement>('Microfone sempre pronto')
const modo = (nome: string | RegExp) => screen.getByRole<HTMLInputElement>('radio', { name: nome })
const provedor = () => screen.getByLabelText<HTMLSelectElement>('Provedor')
const modelo = () => screen.getByLabelText<HTMLInputElement>('Modelo')
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const descricao = (el: HTMLElement) =>
  (el.getAttribute('aria-describedby') ?? '')
    .split(' ')
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? `(falta #${id})`)
    .join(' | ')
const puts = (srv: Servidor) => srv.pedidos.filter((p) => p.metodo === 'PUT' && p.caminho === '/api/settings')
const leituras = (srv: Servidor) => srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/settings')
const opcoesDeModelo = () => Array.from(document.querySelectorAll<HTMLOptionElement>('#cfg-modelos option'), (o) => o.value)
// A região viva embaixo de Testar e Remover: o resultado do teste, ou a
// pergunta do Remover.
const estadoDoTeste = () => document.querySelector<HTMLElement>('.acoes + [role="status"]')!
const anuncioDaLista = () => document.querySelector<HTMLElement>('.anuncio')!
const faixa = () => document.querySelector<HTMLElement>('.faixa')
const alertas = () => screen.queryAllByRole('alert').map((a) => a.textContent)

function escolherProvedor(valor: string) {
  fireEvent.change(provedor(), { target: { value: valor } })
}

function digitarChave(nome: string, chave: string) {
  fireEvent.input(screen.getByLabelText(`Chave de API da ${nome}`), { target: { value: chave } })
}

describe('a vista Configurações', () => {
  it('é a terceira do seletor: fica pressionada, esconde a busca e dá nome à página', async () => {
    montar()
    const config = await screen.findByRole('button', { name: 'Configurações' })
    expect(config.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(config)
    await screen.findByRole('heading', { name: 'Ditado' })

    expect(config.getAttribute('aria-pressed')).toBe('true')
    expect(botao('Histórico').getAttribute('aria-pressed')).toBe('false')
    expect(screen.queryByRole('searchbox')).toBeNull()
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Configurações')
    expect(screen.queryByRole('button', { name: /Manda para o Thiago/ })).toBeNull()
  })

  it('voltar ao histórico e de novo às Configurações não perde o que não foi salvo', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    fireEvent.click(botao('Histórico'))
    expect(await screen.findByRole('button', { name: /Manda para o Thiago/ })).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Ditado' })).toBeNull()

    fireEvent.click(botao('Configurações'))
    expect(segurar().value).toBe('900')
    expect(desligado(salvar())).toBe(false)
    expect(leituras(srv)).toHaveLength(1)
  })

  it('Ctrl+K fica com o navegador: não há busca nesta vista', async () => {
    await abrir()
    expect(fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })).toBe(true)
  })

  it('o anúncio da lista fica calado nesta vista', async () => {
    montar()
    await waitFor(() => expect(anuncioDaLista().textContent).not.toBe(''))
    fireEvent.click(botao('Configurações'))
    await screen.findByRole('heading', { name: 'Ditado' })
    expect(anuncioDaLista().textContent).toBe('')
  })

  it('ir às Configurações fecha o editor aberto; o que foi escrito fica como "não salvo"', async () => {
    montar()
    fireEvent.click(await screen.findByRole('button', { name: /Manda para o Thiago/ }))
    fireEvent.change(screen.getByLabelText('Texto final'), { target: { value: 'Manda para o Thiago amanhã.' } })
    fireEvent.click(botao('Configurações'))
    await screen.findByRole('heading', { name: 'Ditado' })

    fireEvent.click(botao('Histórico'))
    expect(await screen.findByRole('button', { name: /Manda para o Thiago\. não salvo/ })).toBeTruthy()
    expect(screen.queryByLabelText('Texto final')).toBeNull()
  })

  it('erro ao carregar: é anunciado, e depois de tentar de novo o foco vai para a primeira seção', async () => {
    const srv = montar()
    srv.interceptar((p) => (p.caminho === '/api/settings' ? new Response('{}', { status: 500 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    // A região da lista está calada nesta vista: o erro precisa de uma
    // região viva sua.
    expect((await screen.findByRole('alert')).textContent).toBe('Não consegui carregar as configurações: erro 500')

    srv.interceptar(null)
    botao('Tentar de novo').focus()
    fireEvent.click(botao('Tentar de novo'))
    const titulo = await screen.findByRole('heading', { name: 'Ditado' })
    await waitFor(() => expect(document.activeElement).toBe(titulo))
  })

  it('tentar de novo e falhar de novo: o botão fica com o foco, e o erro é anunciado de novo', async () => {
    const srv = montar()
    srv.interceptar((p) => (p.caminho === '/api/settings' ? new Response('{}', { status: 500 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    const primeiro = await screen.findByRole('alert')

    const tentar = botao('Tentar de novo')
    tentar.focus()
    fireEvent.click(tentar)
    expect(tentar.textContent).toBe('Tentando…')
    await waitFor(() => expect(screen.getByRole('alert')).not.toBe(primeiro))
    expect(screen.getByRole('alert').textContent).toBe('Não consegui carregar as configurações: erro 500')
    expect(document.activeElement).toBe(tentar)
    expect(tentar.textContent).toBe('Tentar de novo')
    expect(leituras(srv)).toHaveLength(2)
  })

  it('erro ao carregar e volta ao histórico: o erro das Configurações some junto', async () => {
    const srv = montar()
    srv.interceptar((p) => (p.caminho === '/api/settings' ? new Response('{}', { status: 500 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('alert')

    fireEvent.click(botao('Histórico'))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Tentar de novo' })).toBeNull()
  })
})

describe('Ditado', () => {
  it('mostra o que o Agent devolveu; a tecla é texto, não controle', async () => {
    await abrir({ config: { hold_threshold_ms: 850, beep_enabled: false } })
    const tecla = screen.getByText('Tecla de ditado').closest('.ajuste')!
    expect(Array.from(tecla.querySelectorAll('kbd'), (k) => k.textContent)).toEqual(['Ctrl', 'Win'])
    expect(tecla.querySelector('input, select, button, textarea')).toBeNull()
    expect(tecla.textContent).toContain('não dá para trocar')

    expect(segurar().value).toBe('850')
    expect(screen.getByText('850 ms').tagName).toBe('OUTPUT')
    expect(bip().checked).toBe(false)
    expect(modo(/^Smart/).checked).toBe(true)
    expect(desligado(salvar())).toBe(true)
  })

  it('segurar por: de 300 a 2000 ms, de 50 em 50, com o valor dito em milissegundos', async () => {
    await abrir()
    const r = segurar()
    expect([r.type, r.min, r.max, r.step]).toEqual(['range', '300', '2000', '50'])
    // Com Ctrl+Win, o Agent nunca engole tecla nenhuma
    // (NewDeciderModificadores): a ajuda não pode prometer "funciona como
    // a tecla normal" só para o toque curto. E o limiar não é quando o
    // microfone começa a captar — o Agent marca o áudio já no aperto e
    // liga o microfone ali (audio.Session.MarkStart, ADR-015; pre-roll 0 no
    // padrão, 300 ms só no "sempre pronto"): a ajuda não diz "até começar a
    // gravar" nem "não grava nada".
    expect(descricao(r)).toBe(
      'Quanto tempo segurar as teclas para o ditado valer: é quando o aviso aparece e o bip toca, se estiver ligado. Soltar antes disso descarta o ditado.',
    )
    expect(descricao(r)).not.toMatch(/começar a gravar|não grava/)
    fireEvent.change(r, { target: { value: '1250' } })
    expect(r.getAttribute('aria-valuetext')).toBe('1250 milissegundos')
    expect(screen.getByText('1250 ms').tagName).toBe('OUTPUT')
  })
})

// O "microfone sempre pronto" (ADR-015) anda como o bip: entra no
// rascunho, no "não salvo", no PUT e na mesclagem do 409. O padrão é
// desligado: o microfone só abre durante o ditado.
describe('Ditado: microfone sempre pronto', () => {
  const ajuda =
    'Ligado, o microfone fica aberto o tempo todo e dá para falar já ao apertar as teclas. Desligado, ele só abre durante o ditado: fale depois do bip.'

  it('começa desligado, na seção Ditado, sem nada por salvar', async () => {
    await abrir()
    expect(microfone().type).toBe('checkbox')
    expect(microfone().checked).toBe(false)
    expect(microfone().closest('section')?.getAttribute('aria-labelledby')).toBe(bip().closest('section')?.getAttribute('aria-labelledby'))
    expect(desligado(salvar())).toBe(true)
    expect(screen.queryByText('não salvo')).toBeNull()
  })

  it('mostra o que o Agent devolveu quando já está ligado', async () => {
    await abrir({ config: { microphone_always_ready: true } })
    expect(microfone().checked).toBe(true)
    expect(desligado(salvar())).toBe(true)
  })

  it('a ajuda está ligada ao controle (aria-describedby) e não diz mais do que isso', async () => {
    await abrir()
    expect(descricao(microfone())).toBe(ajuda)
    expect(descricao(bip())).not.toContain('microfone')
  })

  it('ligar marca "não salvo" e vai no PUT como true; desligar de novo desfaz o não salvo', async () => {
    const srv = await abrir()
    fireEvent.click(microfone())
    expect(microfone().checked).toBe(true)
    expect(screen.getByText('não salvo').className).toBe('etiqueta forte')
    expect(desligado(salvar())).toBe(false)

    fireEvent.click(microfone())
    expect(screen.queryByText('não salvo')).toBeNull()
    fireEvent.click(microfone())

    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)).toHaveLength(1)
    expect(puts(srv)[0]?.corpo).toMatchObject({ microphone_always_ready: true, base_version: 1 })
    expect(srv.config.microphone_always_ready).toBe(true)
    expect(microfone().checked).toBe(true)
    expect(screen.queryByText('não salvo')).toBeNull()
  })

  it('desligar um que estava ligado vai como false', async () => {
    const srv = await abrir({ config: { microphone_always_ready: true } })
    fireEvent.click(microfone())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[0]?.corpo).toMatchObject({ microphone_always_ready: false })
    expect(srv.config.microphone_always_ready).toBe(false)
  })

  // A tela manda o rascunho inteiro (só o autostart sai quando ninguém
  // mexeu), como faz com o bip: mexer em outro campo leva o valor lido do
  // microfone, sem trocá-lo. E um PUT sem o campo, como o de outro cliente,
  // também o mantém — aqui e no Go.
  it('mexer em outro campo não muda o microfone; e um PUT sem o campo o mantém', async () => {
    const srv = await abrir({ config: { microphone_always_ready: true } })
    fireEvent.change(segurar(), { target: { value: '900' } })
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[0]?.corpo).toMatchObject({ hold_threshold_ms: 900, beep_enabled: false, microphone_always_ready: true })
    expect(srv.config).toMatchObject({ hold_threshold_ms: 900, microphone_always_ready: true })
    expect(microfone().checked).toBe(true)

    const r = await srv.buscar('/api/settings', {
      method: 'PUT',
      headers: { Authorization: 'Bearer tok' },
      body: JSON.stringify({ base_version: srv.config.version, hold_threshold_ms: 1000 }),
    })
    expect(r.status).toBe(200)
    expect(srv.config).toMatchObject({ hold_threshold_ms: 1000, microphone_always_ready: true })
  })

  it('o que só vale no próximo início diz isso, junto do controle', async () => {
    await abrir({ config: { reinicio_pendente: ['microphone_always_ready'] } })
    expect(descricao(microfone())).toBe(`${ajuda} | O valor salvo vale a partir do próximo início do Aurora Whisper.`)
    expect(descricao(bip())).not.toContain('próximo início')
  })

  it('409: o que a pessoa mudou fica; o resto, inclusive o microfone da outra aba, vem do atual', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.configPorFora({ microphone_always_ready: true })

    fireEvent.click(salvar())
    expect((await screen.findByRole('alert')).textContent).toContain('salvas em outra aba')
    expect(segurar().value).toBe('900')
    expect(microfone().checked).toBe(true)

    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[1]?.corpo).toMatchObject({ hold_threshold_ms: 900, microphone_always_ready: true, base_version: 2 })
    expect(srv.config).toMatchObject({ hold_threshold_ms: 900, microphone_always_ready: true })
  })

  it('409: se a pessoa mexeu no microfone, vale o dela sobre o da outra aba', async () => {
    const srv = await abrir({ config: { microphone_always_ready: true } })
    fireEvent.click(microfone())
    srv.configPorFora({ beep_enabled: false })

    fireEvent.click(salvar())
    await screen.findByText(/salvas em outra aba/)
    expect(microfone().checked).toBe(false)
    expect(bip().checked).toBe(false)

    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config).toMatchObject({ beep_enabled: false, microphone_always_ready: false })
  })

  it('o que a pessoa mexe no microfone enquanto o salvar vai e volta fica na tela, como não salvo', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    const soltar = srv.pausar((p) => p.metodo === 'PUT')
    fireEvent.click(salvar())
    await waitFor(() => expect(puts(srv)).toHaveLength(1))
    fireEvent.click(microfone())
    soltar()

    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config.microphone_always_ready).toBe(false)
    expect(microfone().checked).toBe(true)
    expect(screen.getByText('não salvo')).toBeTruthy()
  })
})

describe('salvar', () => {
  it('manda só os campos editáveis — o autostart só se mexido —, sobre a versão lida, e avisa', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    fireEvent.click(bip())
    fireEvent.click(modo('Literal'))
    expect(screen.getByText('não salvo').className).toBe('etiqueta forte')

    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv).map((p) => p.corpo)).toEqual([
      {
        hold_threshold_ms: 900,
        beep_enabled: false,
        microphone_always_ready: false,
        note_trigger_enabled: true,
        text_mode: 'literal',
        ai_provider: '',
        ai_model: '',
        base_version: 1,
      },
    ])
    expect(srv.config.version).toBe(2)
    expect(desligado(salvar())).toBe(true)
    expect(screen.queryByText('não salvo')).toBeNull()
  })

  it('o que só vale no próximo início diz isso, junto do controle', async () => {
    const srv = await abrir({ exigemReinicio: ['hold_threshold_ms'] })
    expect(descricao(segurar())).not.toContain('próximo início')

    fireEvent.change(segurar(), { target: { value: '1000' } })
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toContain('vale a partir do próximo início do Aurora Whisper'))
    expect(srv.config.reinicio_pendente).toEqual(['hold_threshold_ms'])
    expect(descricao(segurar())).toContain('O valor salvo vale a partir do próximo início do Aurora Whisper.')
    expect(descricao(bip())).not.toContain('próximo início')
  })

  it('o que a pessoa mexe enquanto o salvar vai e volta fica na tela, como não salvo', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    const soltar = srv.pausar((p) => p.metodo === 'PUT')
    fireEvent.click(salvar())
    await waitFor(() => expect(puts(srv)).toHaveLength(1))
    fireEvent.click(bip())
    soltar()

    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config).toMatchObject({ hold_threshold_ms: 900, beep_enabled: true })
    expect(segurar().value).toBe('900')
    expect(bip().checked).toBe(false)
    expect(desligado(salvar())).toBe(false)
    expect(screen.getByText('não salvo')).toBeTruthy()
  })

  it('pendente de um salvamento anterior não vira aviso de reinício no próximo', async () => {
    const srv = await abrir({ config: { reinicio_pendente: ['hold_threshold_ms'] }, exigemReinicio: ['hold_threshold_ms'] })
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config.reinicio_pendente).toEqual(['hold_threshold_ms'])
    // O aviso junto do controle continua: o valor dele ainda espera.
    expect(descricao(segurar())).toContain('próximo início')
  })

  // Um slice nil do Go vira null no JSON.
  it('reinicio_pendente null é o mesmo que nenhum', async () => {
    const srv = montar()
    srv.interceptar((p) =>
      p.caminho === '/api/settings' && p.metodo === 'GET' ? new Response(JSON.stringify({ ...srv.config, reinicio_pendente: null }), { status: 200 }) : undefined,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Ditado' })
    expect(descricao(segurar())).not.toContain('próximo início')
  })

  it('reinicio_pendente que já vem no GET aparece ao abrir', async () => {
    await abrir({ config: { reinicio_pendente: ['beep_enabled', 'autostart'] } })
    expect(descricao(bip())).toContain('próximo início')
    expect(descricao(screen.getByLabelText('Iniciar com o Windows'))).toContain('próximo início')
    expect(descricao(segurar())).not.toContain('próximo início')
  })

  it('pendente do modo de texto e do modelo: ligado ao controle de cada um', async () => {
    await abrir({ config: { ai_provider: 'openai', reinicio_pendente: ['text_mode', 'ai_model'] } })
    for (const r of [modo('Literal'), modo(/^Smart/)]) expect(descricao(r)).toContain('O valor salvo vale a partir do próximo início do Aurora Whisper.')
    expect(descricao(modelo())).toContain('O valor salvo vale a partir do próximo início do Aurora Whisper.')
    expect(descricao(provedor())).not.toContain('próximo início')
    expect(descricao(bip())).not.toContain('próximo início')
  })

  // Primeiro uso: provedor escolhido, chave colada, e só o botão
  // principal apertado. A chave tem o botão dela e não vai neste Salvar.
  it('chave colada e só o Salvar configurações: o aviso diz que a chave não foi guardada', async () => {
    const srv = await abrir()
    escolherProvedor('openai')
    await screen.findByText('Nenhuma chave guardada.')
    digitarChave('OpenAI', 'sk-teste-esquecida-Ee44')
    fireEvent.click(salvar())
    await waitFor(() =>
      expect(aviso().textContent).toBe('Configurações salvas. A chave de API colada ainda não foi guardada: use Salvar chave.'),
    )
    expect(srv.chaves.openai).toBeUndefined()
    expect(JSON.stringify(puts(srv))).not.toContain('Ee44')
  })

  it('409: recarrega, mantém o que a pessoa mudou, traz o resto da outra aba e avisa', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.configPorFora({ beep_enabled: false, hold_threshold_ms: 1500 })

    fireEvent.click(salvar())
    expect((await screen.findByRole('alert')).textContent).toContain('salvas em outra aba')
    expect(segurar().value).toBe('900')
    expect(bip().checked).toBe(false)
    expect(srv.config.hold_threshold_ms).toBe(1500)

    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv).map((p) => p.corpo?.base_version)).toEqual([1, 2])
    expect(srv.config).toMatchObject({ hold_threshold_ms: 900, beep_enabled: false })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('409 em que a outra aba trocou o provedor: a tela pergunta a chave do provedor novo', async () => {
    const srv = await abrir({ chaves: { anthropic: 'sk-ant-teste-Ab12' } })
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.configPorFora({ ai_provider: 'anthropic', ai_model: 'modelo-da-outra-aba' })

    fireEvent.click(salvar())
    await screen.findByText(/salvas em outra aba/)
    expect(provedor().value).toBe('anthropic')
    expect(modelo().value).toBe('modelo-da-outra-aba')
    expect(await screen.findByText('••••Ab12')).toBeTruthy()
    expect(desligado(botao('Testar conexão'))).toBe(false)
  })

  it('409: a chave do provedor à vista é perguntada de novo — outra aba pode ter guardado uma', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.chaves.openai = 'sk-outra-aba-Zz11'
    srv.configPorFora({ beep_enabled: false })

    fireEvent.click(salvar())
    await screen.findByText(/salvas em outra aba/)
    expect(await screen.findByText('••••Zz11')).toBeTruthy()
  })

  it('409 em que a outra aba gravou o mesmo que a pessoa mudou: a nota não manda salvar de novo', async () => {
    const srv = await abrir()
    fireEvent.click(bip())
    srv.configPorFora({ beep_enabled: false })

    fireEvent.click(salvar())
    const nota = await screen.findByText(/salvas em outra aba/)
    expect(nota.textContent).toContain('não há nada diferente para salvar')
    expect(nota.textContent).not.toContain('Salve de novo')
    expect(desligado(salvar())).toBe(true)
  })

  it('Agent fora do ar ao recarregar depois do 409: a faixa do topo aparece, não uma nota local', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.interceptar((p) => {
      if (p.metodo !== 'PUT') return undefined
      srv.derrubar(true)
      return new Response('{}', { status: 409 })
    })
    fireEvent.click(salvar())
    await waitFor(() => expect(faixa()?.textContent).toContain('não está respondendo'))
    expect(alertas().some((a) => a?.includes('Não deu para salvar'))).toBe(false)
    expect(segurar().value).toBe('900')
  })

  it('400: a mensagem do Agent aparece e é anunciada; mexer de novo a tira', async () => {
    const srv = await abrir()
    srv.interceptar((p) =>
      p.metodo === 'PUT' ? new Response(JSON.stringify({ error: { code: 'BAD_REQUEST', message: 'valor fora da faixa' } }), { status: 400 }) : undefined,
    )
    fireEvent.change(segurar(), { target: { value: '900' } })
    fireEvent.click(salvar())
    expect((await screen.findByRole('alert')).textContent).toBe('Não deu para salvar: valor fora da faixa')
    expect(segurar().value).toBe('900')

    fireEvent.change(segurar(), { target: { value: '950' } })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('Agent fora do ar ao salvar: a faixa do topo aparece e o que foi mudado fica', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.derrubar(true)
    fireEvent.click(salvar())
    expect((await screen.findByRole('alert')).textContent).toContain('não está respondendo')
    expect(segurar().value).toBe('900')
  })
})

describe('Texto', () => {
  it('os três modos, cada um com a frase do que faz, num grupo com nome', async () => {
    await abrir()
    const grupo = screen.getByRole('radiogroup', { name: 'Texto' })
    expect(within(grupo).getAllByRole('radio')).toHaveLength(3)
    // O vocabulário do dicionário vai para o reconhecimento em todo modo
    // (ASROptions.Vocabulary): o Literal tira as trocas, não o dicionário.
    expect(descricao(modo('Literal'))).toContain('sem as trocas do dicionário')
    expect(descricao(modo('Literal'))).not.toMatch(/sem dicionário/i)
    expect(descricao(modo(/^Smart/))).toContain('dicionário')
    expect(descricao(modo('Polish'))).toContain('Envia o texto ao provedor de IA')
  })

  // O Polish preserva e organiza (ADR-016): a frase diz o que ele faz, para
  // onde o texto vai e o que acontece quando a IA falha — e não é mais a
  // frase neutra de antes do Polish existir.
  it('o Polish diz que organiza sem trocar as palavras, para onde o texto vai e que, se falhar, entra o Smart', async () => {
    await abrir()
    const frase = descricao(modo('Polish'))
    expect(frase).toContain('Organiza o texto com IA: pontuação, parágrafos e listas, sem trocar as suas palavras.')
    expect(frase).toContain('Envia o texto ao provedor de IA escolhido abaixo.')
    expect(frase).toContain('Se algo falhar, entra o texto do Smart.')
    expect(frase).not.toContain('para organizá-lo')
  })

  it('sem o Polish no Agent: não dá para escolher, nem com provedor e chave, e diz que chega na próxima versão', async () => {
    await abrir({ config: { ai_provider: 'openai', polish_disponivel: false }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    expect(modo('Polish').disabled).toBe(true)
    expect(descricao(modo('Polish'))).toContain('Chega na próxima versão do Aurora Whisper.')
    expect(modo('Literal').disabled).toBe(false)
    expect(screen.getByText(/Por enquanto, nada usa a IA/)).toBeTruthy()
  })

  it('Agent de antes da etapa 2, que nem manda polish_disponivel: o mesmo, o Polish fica bloqueado', async () => {
    const srv = servidorFalso(umDitado(), { config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    delete srv.config.polish_disponivel
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByText('••••a1B2')
    expect(modo('Polish').disabled).toBe(true)
    expect(descricao(modo('Polish'))).toContain('Chega na próxima versão')
  })

  // O Agent com a etapa 2 manda polish_disponivel: true. Com o provedor e a
  // chave dele guardada, o Polish deixa de estar bloqueado.
  it('com o Polish no Agent (o padrão do dublê) e a chave do provedor guardada: o Polish já vem liberado', async () => {
    await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    expect(modo('Polish').disabled).toBe(false)
    expect(descricao(modo('Polish'))).not.toContain('Chega na próxima versão')
    expect(descricao(modo('Polish'))).not.toContain('guarde a chave')
    expect(screen.getByText('Só o modo Polish usa a IA, e ele é opcional: o ditado funciona sem chave.')).toBeTruthy()
    expect(screen.queryByText(/Por enquanto, nada usa a IA/)).toBeNull()
  })

  it('o Polish grava e volta escolhido: o Agent guarda o modo com o provedor e o modelo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai', ai_model: 'gpt-6-luna' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    fireEvent.click(modo('Polish'))
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config).toMatchObject({ text_mode: 'polish', ai_provider: 'openai', ai_model: 'gpt-6-luna' })
    expect(modo('Polish').checked).toBe(true)
  })

  it('com o Polish no Agent: libera só com o provedor escolhido e a chave dele guardada', async () => {
    const srv = await abrir({ config: { polish_disponivel: true } })
    expect(modo('Polish').disabled).toBe(true)
    expect(descricao(modo('Polish'))).toContain('selecione o provedor')

    escolherProvedor('openai')
    await screen.findByText('Nenhuma chave guardada.')
    expect(modo('Polish').disabled).toBe(true)
    expect(descricao(modo('Polish'))).toContain('guarde a chave de API da OpenAI')

    digitarChave('OpenAI', 'sk-teste-polish-Zz99')
    fireEvent.click(botao('Salvar chave'))
    await screen.findByText('••••Zz99')
    expect(modo('Polish').disabled).toBe(false)

    fireEvent.click(modo('Polish'))
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config).toMatchObject({ text_mode: 'polish', ai_provider: 'openai' })
  })
})

describe('IA: a chave', () => {
  it('sem provedor, não há campo de chave nem de modelo', async () => {
    await abrir()
    expect(provedor().value).toBe('')
    expect(screen.queryByLabelText(/Chave de API/)).toBeNull()
    expect(screen.queryByLabelText('Modelo')).toBeNull()
  })

  it('guardar: a chave sai da página só no PUT, o campo esvazia, e ela não aparece em lugar nenhum', async () => {
    const espioes = (['log', 'info', 'warn', 'error', 'debug'] as const).map((m) => vi.spyOn(console, m))
    const segredo = 'sk-proj-segredo-de-teste-7Kq9a1B2'
    const srv = await abrir()
    escolherProvedor('openai')
    await screen.findByText('Nenhuma chave guardada.')
    const campo = screen.getByLabelText<HTMLInputElement>('Chave de API da OpenAI')
    expect(campo.type).toBe('password')
    expect(campo.getAttribute('autocomplete')).toBe('off')
    expect(desligado(botao('Salvar chave'))).toBe(true)

    digitarChave('OpenAI', `  ${segredo}\n`)
    fireEvent.click(botao('Salvar chave'))
    await screen.findByText('••••a1B2')
    await waitFor(() => expect(aviso().textContent).toBe('Chave da OpenAI guardada.'))

    expect(srv.chaves.openai).toBe(segredo)
    // Quem chega ao campo pelo teclado ouve o que está guardado.
    expect(descricao(screen.getByLabelText('Chave de API da OpenAI'))).toContain('Guardada: ••••a1B2')
    expect(screen.getByLabelText<HTMLInputElement>('Chave de API da OpenAI').value).toBe('')
    expect(desligado(botao('Salvar chave'))).toBe(true)
    const levaram = srv.pedidos.filter((p: Pedido) => `${p.caminho}?${p.busca} ${JSON.stringify(p.corpo ?? {})} ${p.auth}`.includes(segredo))
    expect(levaram.map((p) => [p.metodo, p.caminho, p.busca.toString(), p.corpo])).toEqual([['PUT', '/api/ia/chave', '', { provedor: 'openai', chave: segredo }]])
    expect(document.documentElement.outerHTML).not.toContain(segredo)
    expect(document.documentElement.outerHTML).not.toContain('7Kq9')
    for (const e of espioes) for (const chamada of e.mock.calls) expect(JSON.stringify(chamada)).not.toContain(segredo)
  })

  it('Enter no campo da chave também salva', async () => {
    const srv = await abrir({ config: { ai_provider: 'anthropic' } })
    await screen.findByText('Nenhuma chave guardada.')
    digitarChave('Anthropic', 'sk-ant-teste-Qw12')
    // false: o Enter foi tomado (preventDefault) e não segue adiante.
    expect(fireEvent.keyDown(screen.getByLabelText('Chave de API da Anthropic'), { key: 'Enter' })).toBe(false)
    await screen.findByText('••••Qw12')
    expect(srv.chaves.anthropic).toBe('sk-ant-teste-Qw12')
  })

  it('falha ao guardar: a mensagem é anunciada e o que foi colado fica no campo para corrigir', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    srv.interceptar((p) =>
      p.caminho === '/api/ia/chave' && p.metodo === 'PUT'
        ? new Response(JSON.stringify({ error: { code: 'BAD_REQUEST', message: 'a chave não tem o formato da OpenAI' } }), { status: 400 })
        : undefined,
    )
    digitarChave('OpenAI', 'chave-errada')
    fireEvent.click(botao('Salvar chave'))
    expect((await screen.findByRole('alert')).textContent).toBe('Não deu para guardar a chave: a chave não tem o formato da OpenAI')
    expect(screen.getByLabelText<HTMLInputElement>('Chave de API da OpenAI').value).toBe('chave-errada')
    expect(srv.chaves.openai).toBeUndefined()
  })

  it('remover: o primeiro clique só pergunta, e sair do botão desiste', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-9Z9Z' } })
    await screen.findByText('••••9Z9Z')
    fireEvent.click(botao('Remover chave'))
    expect(botao('Remover mesmo?')).toBeTruthy()
    expect(estadoDoTeste().textContent).toBe('Sem uma cópia da chave, será preciso gerar outra no site da OpenAI. Para remover, aperte de novo.')
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE')).toEqual([])

    fireEvent.blur(botao('Remover mesmo?'))
    expect(botao('Remover chave')).toBeTruthy()
    expect(estadoDoTeste().textContent).toBe('')
    fireEvent.click(botao('Remover chave'))
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE')).toEqual([])
    expect(srv.chaves.openai).toBe('sk-teste-9Z9Z')
  })

  it('remover: confirmado, pede ao Agent e passa a dizer que não há chave', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-9Z9Z' } })
    await screen.findByText('••••9Z9Z')
    fireEvent.click(botao('Remover chave'))
    fireEvent.click(botao('Remover mesmo?'))
    await screen.findByText('Nenhuma chave guardada.')
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE').map((p) => `${p.caminho}?${p.busca}`)).toEqual(['/api/ia/chave?provedor=openai'])
    expect(srv.chaves.openai).toBeUndefined()
    expect(desligado(botao('Remover chave'))).toBe(true)
    expect(desligado(botao('Testar conexão'))).toBe(true)
    expect(aviso().textContent).toBe('Chave da OpenAI removida.')
  })

  it('falha ao ver se há chave: diz que não sabe, anuncia o porquê e deixa conferir de novo', async () => {
    const srv = montar({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    srv.interceptar((p) => (p.caminho === '/api/ia/chave' ? new Response('{}', { status: 500 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    expect((await screen.findByRole('alert')).textContent).toBe('Não consegui ver se há uma chave guardada: erro 500')
    expect(screen.getByText('Não sei se há uma chave guardada.')).toBeTruthy()
    expect(screen.queryByText(/Conferindo/)).toBeNull()

    srv.interceptar(null)
    botao('Conferir de novo').focus()
    fireEvent.click(botao('Conferir de novo'))
    // O botão some no clique; o foco não cai no <body>.
    expect(document.activeElement).toBe(screen.getByLabelText('Chave de API da OpenAI'))
    expect(await screen.findByText('••••a1B2')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Conferir de novo' })).toBeNull()
  })

  it('trocar de provedor desliga o Salvar chave e tira o erro da chave do outro', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    srv.interceptar((p) =>
      p.caminho === '/api/ia/chave' && p.metodo === 'PUT' ? new Response(JSON.stringify({ error: { message: 'formato errado' } }), { status: 400 }) : undefined,
    )
    digitarChave('OpenAI', 'chave-errada')
    fireEvent.click(botao('Salvar chave'))
    await screen.findByRole('alert')

    escolherProvedor('anthropic')
    await screen.findByText('Nenhuma chave guardada.')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByLabelText<HTMLInputElement>('Chave de API da Anthropic').value).toBe('')
    expect(desligado(botao('Salvar chave'))).toBe(true)
  })

  it('Agent fora do ar ao guardar a chave: a faixa do topo aparece, e o que foi colado fica no campo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    digitarChave('OpenAI', 'sk-teste-Qq77')
    srv.derrubar(true)
    fireEvent.click(botao('Salvar chave'))
    await waitFor(() => expect(faixa()?.textContent).toContain('não está respondendo'))
    expect(alertas().some((a) => a?.includes('Não deu para guardar'))).toBe(false)
    expect(screen.getByLabelText<HTMLInputElement>('Chave de API da OpenAI').value).toBe('sk-teste-Qq77')
  })

  it('trocar de provedor: o modelo limpa (e volta com o salvo), o campo da chave esvazia, e o estado é o da chave do novo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai', ai_model: 'modelo-salvo' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    digitarChave('OpenAI', 'digitada-e-esquecida')

    escolherProvedor('anthropic')
    await screen.findByText('Nenhuma chave guardada.')
    expect(modelo().value).toBe('')
    expect(screen.getByLabelText<HTMLInputElement>('Chave de API da Anthropic').value).toBe('')
    expect(srv.pedidos.some((p) => p.caminho === '/api/ia/chave' && p.busca.get('provedor') === 'anthropic')).toBe(true)

    escolherProvedor('openai')
    expect(await screen.findByText('••••a1B2')).toBeTruthy()
    expect(modelo().value).toBe('modelo-salvo')
    expect(screen.getByLabelText<HTMLInputElement>('Chave de API da OpenAI').value).toBe('')
  })

  it('escolher "Nenhum" limpa o modelo, e o PUT leva os dois vazios', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai', ai_model: 'modelo-salvo' } })
    escolherProvedor('')
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[0]?.corpo).toMatchObject({ ai_provider: '', ai_model: '' })
  })

  it('trocar de provedor desfaz o "Remover mesmo?": o Remover do outro pergunta de novo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2', anthropic: 'sk-ant-teste-Bb22' } })
    await screen.findByText('••••a1B2')
    fireEvent.click(botao('Remover chave'))
    expect(botao('Remover mesmo?')).toBeTruthy()

    // O select troca sem tirar o foco do botão: quem desfaz é a troca.
    escolherProvedor('anthropic')
    await screen.findByText('••••Bb22')
    expect(estadoDoTeste().textContent).toBe('')
    fireEvent.click(botao('Remover chave'))
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE')).toEqual([])
    expect(srv.chaves.anthropic).toBe('sk-ant-teste-Bb22')
  })

  // A pergunta feita ao carregar ainda no ar quando a chave é guardada: a
  // resposta dela é de antes da chave. Antes, nenhuma pergunta nova saía,
  // e a velha ficava valendo.
  it('pergunta da chave ainda no ar ao guardar: sai uma pergunta nova, e ela vale', async () => {
    const srv = montar({ config: { ai_provider: 'openai' } })
    let primeira = true
    const soltar = srv.pausar((p) => {
      if (p.caminho !== '/api/ia/chave' || p.metodo !== 'GET' || !primeira) return false
      primeira = false
      return true
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByText('Conferindo se há uma chave guardada…')

    digitarChave('OpenAI', 'sk-teste-corrida-Rr55')
    fireEvent.click(botao('Salvar chave'))
    expect(await screen.findByText('••••Rr55')).toBeTruthy()

    expect(desligado(botao('Testar conexão'))).toBe(false)
    soltar()
  })

  // Remover só fica ligado com a chave conhecida — sem pergunta no ar da
  // geração atual. Uma pergunta velha no ar só sobra de antes de um
  // Salvar chave; guardar e remover em seguida, e a velha chega depois.
  it('resposta velha chegando depois de guardar e remover: vale o remover', async () => {
    const srv = montar({ config: { ai_provider: 'openai' } })
    let primeira = true
    const soltar = srv.pausar((p) => {
      if (p.caminho !== '/api/ia/chave' || p.metodo !== 'GET' || !primeira) return false
      primeira = false
      return true
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByText('Conferindo se há uma chave guardada…')
    digitarChave('OpenAI', 'sk-teste-corrida-Rr55')
    fireEvent.click(botao('Salvar chave'))
    await screen.findByText('••••Rr55')
    fireEvent.click(botao('Remover chave'))
    fireEvent.click(botao('Remover mesmo?'))
    await screen.findByText('Nenhuma chave guardada.')

    // O dublê monta a resposta quando solta o pedido: para a velha dizer
    // algo diferente do que a tela sabe, o servidor passa a ter uma chave.
    srv.chaves.openai = 'sk-teste-velha-Vv00'
    soltar()
    await act(() => new Promise((r) => setTimeout(r, 50)))
    expect(screen.getByText('Nenhuma chave guardada.')).toBeTruthy()
    expect(screen.queryByText('••••Vv00')).toBeNull()
  })
})

describe('IA: testar a conexão e escolher o modelo', () => {
  it('conexão boa: diz quantos modelos vieram, eles viram a lista do campo, e o modelo escolhido é salvo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    expect(opcoesDeModelo()).toEqual([])
    srv.testeDevolve({ ok: true, modelos: ['modelo-a', 'modelo-b', 'modelo-c'] })

    fireEvent.click(botao('Testar conexão'))
    // O teste lista modelos: prova que a chave foi aceita, não que ela
    // gera texto. A frase não diz mais que isso.
    expect(await screen.findByText('A OpenAI aceitou a chave e listou 3 modelos.')).toBeTruthy()
    expect(screen.getByText(/aceitou a chave/).getAttribute('role')).toBe('status')
    expect(estadoDoTeste().textContent).not.toMatch(/funciona/)
    expect(srv.pedidos.find((p) => p.caminho === '/api/ia/testar')?.corpo).toEqual({ provedor: 'openai' })
    expect(opcoesDeModelo()).toEqual(['modelo-a', 'modelo-b', 'modelo-c'])
    expect(modelo().getAttribute('list')).toBe('cfg-modelos')

    fireEvent.change(modelo(), { target: { value: 'modelo-b' } })
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config.ai_model).toBe('modelo-b'))
  })

  it('campo livre: dá para escrever um modelo sem testar', async () => {
    const srv = await abrir({ config: { ai_provider: 'anthropic' } })
    fireEvent.change(modelo(), { target: { value: 'nome-escrito-a-mao' } })
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config).toMatchObject({ ai_provider: 'anthropic', ai_model: 'nome-escrito-a-mao' }))
  })

  it('conexão ruim: a mensagem do Agent é anunciada, e nenhum modelo aparece', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: false, erro: 'A OpenAI recusou a chave. Confira se ela foi copiada inteira.' })

    fireEvent.click(botao('Testar conexão'))
    expect((await screen.findByRole('alert')).textContent).toBe('A OpenAI recusou a chave. Confira se ela foi copiada inteira.')
    expect(opcoesDeModelo()).toEqual([])
    expect(screen.queryByText(/aceitou a chave/)).toBeNull()
  })

  it('o resultado do teste é do provedor testado: trocar de provedor não mostra os modelos do outro', async () => {
    await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)

    escolherProvedor('anthropic')
    await screen.findByText('Nenhuma chave guardada.')
    expect(opcoesDeModelo()).toEqual([])
    expect(screen.queryByText(/aceitou a chave/)).toBeNull()
  })

  it('guardar outra chave ou remover a chave apaga o resultado do teste, que era da chave de antes', async () => {
    await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)

    digitarChave('OpenAI', 'sk-teste-nova-Cc33')
    fireEvent.click(botao('Salvar chave'))
    await screen.findByText('••••Cc33')
    expect(estadoDoTeste().textContent).toBe('')
    expect(opcoesDeModelo()).toEqual([])

    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)
    fireEvent.click(botao('Remover chave'))
    fireEvent.click(botao('Remover mesmo?'))
    await screen.findByText('Nenhuma chave guardada.')
    expect(estadoDoTeste().textContent).toBe('')
    expect(opcoesDeModelo()).toEqual([])
  })

  // Um slice nil do Go vira null no JSON: o painel inteiro caía no .length.
  it('modelos null: a chave foi aceita, sem modelo nenhum, e o painel continua de pé', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.interceptar((p) => (p.caminho === '/api/ia/testar' ? new Response(JSON.stringify({ ok: true, modelos: null }), { status: 200 }) : undefined))
    fireEvent.click(botao('Testar conexão'))
    expect(await screen.findByText('A OpenAI aceitou a chave, mas não listou nenhum modelo. Escreva o nome do modelo abaixo.')).toBeTruthy()
    expect(opcoesDeModelo()).toEqual([])
    expect(screen.getByRole('heading', { name: 'Ditado' })).toBeTruthy()
  })

  it('um modelo só: no singular', async () => {
    const srv = await abrir({ config: { ai_provider: 'anthropic' }, chaves: { anthropic: 'sk-ant-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: true, modelos: ['modelo-unico'] })
    fireEvent.click(botao('Testar conexão'))
    expect(await screen.findByText('A Anthropic aceitou a chave e listou 1 modelo.')).toBeTruthy()
  })

  it('testar de novo: o resultado anterior sai assim que o teste novo começa', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: false, erro: 'A OpenAI não respondeu a tempo.' })
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText('A OpenAI não respondeu a tempo.')

    const soltar = srv.pausar((p) => p.caminho === '/api/ia/testar')
    srv.testeDevolve({ ok: true, modelos: ['modelo-a'] })
    fireEvent.click(botao('Testar conexão'))
    await waitFor(() => expect(botao('Testando…')).toBeTruthy())
    expect(screen.queryByText('A OpenAI não respondeu a tempo.')).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    soltar()
    expect(await screen.findByText('A OpenAI aceitou a chave e listou 1 modelo.')).toBeTruthy()
  })

  it('ok:false sem mensagem: o alerta diz que falhou, não fica vazio', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.interceptar((p) => (p.caminho === '/api/ia/testar' ? new Response(JSON.stringify({ ok: false }), { status: 200 }) : undefined))
    fireEvent.click(botao('Testar conexão'))
    expect((await screen.findByRole('alert')).textContent).toBe('O teste não deu certo, e o Aurora Whisper não disse por quê.')
  })

  it('Agent fora do ar ao testar: a faixa do topo aparece, não um resultado de teste', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.derrubar(true)
    fireEvent.click(botao('Testar conexão'))
    await waitFor(() => expect(faixa()?.textContent).toContain('não está respondendo'))
    expect(alertas().some((a) => a?.includes('Não deu para testar'))).toBe(false)
    expect(desligado(botao('Testar conexão'))).toBe(false)
  })

  it('sem chave guardada, testar e remover ficam desligados', async () => {
    await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    expect(desligado(botao('Testar conexão'))).toBe(true)
    expect(desligado(botao('Remover chave'))).toBe(true)
  })
})

// polish_ultimo_motivo (ADR-016, decisão 14): por que o último Polish não
// agiu, numa frase pronta do Agent — só o motivo, nunca o texto ditado.
describe('IA: por que o último Polish não agiu', () => {
  const frase = 'Sem crédito na OpenAI: o último ditado saiu sem o Polish.'
  const secaoIA = () => screen.getByRole('region', { name: 'IA' })
  const linhaDoMotivo = () => document.getElementById('cfg-polish-motivo')!

  it('com motivo: uma linha na seção IA, como status, sem alarme', async () => {
    await abrir({ config: { polish_ultimo_motivo: frase } })
    const linha = within(secaoIA()).getByText(frase)
    expect(linha.getAttribute('role')).toBe('status')
    // Informação, não risco: a classe discreta da ajuda, nunca a da nota
    // (a de risco) nem um alerta.
    expect(linha.className).toBe('ajuda')
    expect(secaoIA().querySelector('.nota')).toBeNull()
    expect(alertas()).toEqual([])
  })

  it('a frase aparece do jeito que o Agent a mandou', async () => {
    const outra = 'A OpenAI demorou mais de 3,5 s: o último ditado saiu sem o Polish.'
    await abrir({ config: { polish_ultimo_motivo: outra } })
    expect(linhaDoMotivo().textContent).toBe(outra)
  })

  it('sem motivo ("" ou ausente): a região existe, vazia — e nada mais aparece', async () => {
    await abrir({ config: { polish_ultimo_motivo: '' } })
    expect(linhaDoMotivo().getAttribute('role')).toBe('status')
    expect(linhaDoMotivo().textContent).toBe('')
    cleanup()

    const srv = servidorFalso(umDitado())
    delete srv.config.polish_ultimo_motivo
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Ditado' })
    expect(linhaDoMotivo().textContent).toBe('')
    expect(alertas()).toEqual([])
  })

  it('a linha é da seção IA, não do texto nem da privacidade', async () => {
    await abrir({ config: { polish_ultimo_motivo: frase } })
    expect(within(secaoIA()).queryByText(frase)).not.toBeNull()
    expect(within(screen.getByRole('region', { name: 'Texto' })).queryByText(frase)).toBeNull()
    expect(within(screen.getByRole('region', { name: 'Privacidade' })).queryByText(frase)).toBeNull()
  })

  it('ao voltar à vista sem nada por salvar, a tela lê de novo e mostra o motivo novo', async () => {
    const srv = await abrir()
    expect(linhaDoMotivo().textContent).toBe('')
    fireEvent.click(botao('Histórico'))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    srv.configPorFora({ polish_ultimo_motivo: frase })

    fireEvent.click(botao('Configurações'))
    await waitFor(() => expect(linhaDoMotivo().textContent).toBe(frase))
  })

  it('e o motivo some quando o Polish volta a agir', async () => {
    const srv = await abrir({ config: { polish_ultimo_motivo: frase } })
    fireEvent.click(botao('Histórico'))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    srv.configPorFora({ polish_ultimo_motivo: '' })

    fireEvent.click(botao('Configurações'))
    await waitFor(() => expect(linhaDoMotivo().textContent).toBe(''))
  })

  it('a resposta do salvar traz o motivo do Agent', async () => {
    const srv = await abrir({ config: { polish_ultimo_motivo: '' } })
    srv.config.polish_ultimo_motivo = frase
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(linhaDoMotivo().textContent).toBe(frase)
  })

  it('não é o texto ditado: a tela mostra só o que o Agent mandou no campo, e nada do histórico', async () => {
    await abrir({ config: { polish_ultimo_motivo: frase } })
    // O ditado do histórico ("Manda para o Thiago.") não entra na linha.
    expect(linhaDoMotivo().textContent).not.toContain('Thiago')
  })
})

// O modelo sugerido (ADR-016, decisão 10): o Agent manda modelo_sugerido em
// POST /api/ia/testar; a tela o marca na lista da conta e, sem modelo
// escolhido, o põe no rascunho. Só sugestão: salvar é da pessoa, e nada a
// exige.
describe('IA: o modelo sugerido', () => {
  const daConta = ['gpt-5-nano', 'gpt-6-luna', 'gpt-4o-mini']
  const rotulosDaLista = () => Array.from(document.querySelectorAll<HTMLOptionElement>('#cfg-modelos option'), (o) => o.getAttribute('label'))

  async function testarComSugestao(config: OpcoesDoServidor['config'] = {}, teste: Parameters<Servidor['testeDevolve']>[0] = { ok: true, modelos: daConta, modelo_sugerido: 'gpt-6-luna' }) {
    const srv = await abrir({ config: { ai_provider: 'openai', ...config }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve(teste)
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)
    return srv
  }

  it('o sugerido vem marcado na lista da conta, e só ele', async () => {
    await testarComSugestao()
    expect(opcoesDeModelo()).toEqual(daConta)
    expect(rotulosDaLista()).toEqual([null, 'gpt-6-luna (sugerido)', null])
  })

  it('a ajuda do campo diz qual é o sugerido, e está ligada a ele', async () => {
    await testarComSugestao()
    expect(descricao(modelo())).toContain('Sugerido: gpt-6-luna, o que se saiu melhor nos testes do Aurora Whisper.')
  })

  it('sem modelo escolhido: o sugerido vem pré-selecionado no rascunho — e só nele', async () => {
    const srv = await testarComSugestao()
    expect(modelo().value).toBe('gpt-6-luna')
    // Nada foi salvo: a tela mostra o "não salvo", o Agent não recebeu PUT.
    expect(screen.getByText('não salvo')).toBeTruthy()
    expect(desligado(salvar())).toBe(false)
    expect(puts(srv)).toHaveLength(0)
    expect(srv.config.ai_model).toBe('')
  })

  it('salvar continua explícito: só o Salvar configurações grava o sugerido', async () => {
    const srv = await testarComSugestao()
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config.ai_model).toBe('gpt-6-luna'))
    expect(puts(srv)).toHaveLength(1)
    expect(puts(srv)[0]?.corpo).toMatchObject({ ai_provider: 'openai', ai_model: 'gpt-6-luna' })
    expect(screen.queryByText('não salvo')).toBeNull()
  })

  it('nunca é exigido: dá para escolher outro modelo da conta, ou escrever outro nome, e salvar', async () => {
    const srv = await testarComSugestao()
    fireEvent.change(modelo(), { target: { value: 'gpt-5-nano' } })
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config.ai_model).toBe('gpt-5-nano'))
    fireEvent.change(modelo(), { target: { value: 'um-modelo-que-so-eu-tenho' } })
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config.ai_model).toBe('um-modelo-que-so-eu-tenho'))
  })

  it('com um modelo já salvo, o sugerido não o troca — só aparece marcado', async () => {
    const srv = await testarComSugestao({ ai_model: 'gpt-5-nano' })
    expect(modelo().value).toBe('gpt-5-nano')
    expect(rotulosDaLista()).toEqual([null, 'gpt-6-luna (sugerido)', null])
    expect(desligado(salvar())).toBe(true)
    expect(puts(srv)).toHaveLength(0)
  })

  it('com um modelo escrito no rascunho antes de testar, o sugerido não o troca', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    fireEvent.change(modelo(), { target: { value: 'meu-modelo' } })
    srv.testeDevolve({ ok: true, modelos: daConta, modelo_sugerido: 'gpt-6-luna' })
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)
    expect(modelo().value).toBe('meu-modelo')
  })

  it('a pessoa escreveu um modelo enquanto o teste ia e voltava: o dela fica', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: true, modelos: daConta, modelo_sugerido: 'gpt-6-luna' })
    const soltar = srv.pausar((p) => p.caminho === '/api/ia/testar')
    fireEvent.click(botao('Testar conexão'))
    await waitFor(() => expect(botao('Testando…')).toBeTruthy())
    fireEvent.change(modelo(), { target: { value: 'meu-modelo' } })
    soltar()
    await screen.findByText(/aceitou a chave/)
    expect(modelo().value).toBe('meu-modelo')
  })

  it('a pessoa trocou de provedor enquanto o teste ia e voltava: o rascunho do outro não é tocado', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2', anthropic: 'sk-ant-teste-c3D4' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: true, modelos: daConta, modelo_sugerido: 'gpt-6-luna' })
    const soltar = srv.pausar((p) => p.caminho === '/api/ia/testar')
    fireEvent.click(botao('Testar conexão'))
    await waitFor(() => expect(botao('Testando…')).toBeTruthy())
    escolherProvedor('anthropic')
    await screen.findByText('••••c3D4')
    soltar()
    await waitFor(() => expect(srv.pedidos.filter((p) => p.caminho === '/api/ia/testar')).toHaveLength(1))
    // Dá tempo de a resposta chegar e ser (indevidamente) aplicada.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 30))
    })
    expect(provedor().value).toBe('anthropic')
    expect(modelo().value).toBe('')
    // E ao voltar para a OpenAI, o rascunho também não trouxe o sugerido.
    escolherProvedor('openai')
    expect(modelo().value).toBe('')
  })

  it('o sugerido não está na conta: nada é marcado, nada é pré-selecionado', async () => {
    await testarComSugestao({}, { ok: true, modelos: ['gpt-5-nano', 'gpt-4o-mini'], modelo_sugerido: 'gpt-6-luna' })
    expect(modelo().value).toBe('')
    expect(rotulosDaLista()).toEqual([null, null])
    expect(descricao(modelo())).not.toContain('Sugerido')
    expect(desligado(salvar())).toBe(true)
  })

  it('provedor sem sugestão (modelo_sugerido vazio): nada é marcado nem pré-selecionado', async () => {
    const srv = await abrir({ config: { ai_provider: 'anthropic' }, chaves: { anthropic: 'sk-ant-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: true, modelos: ['claude-haiku-4-5', 'claude-sonnet-5-5'], modelo_sugerido: '' })
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)
    expect(modelo().value).toBe('')
    expect(rotulosDaLista()).toEqual([null, null])
    expect(desligado(salvar())).toBe(true)
  })

  it('Agent sem modelo_sugerido na resposta (de antes da sugestão): nada acontece, e a tela segue de pé', async () => {
    await testarComSugestao({}, { ok: true, modelos: daConta })
    expect(modelo().value).toBe('')
    expect(rotulosDaLista()).toEqual([null, null, null])
    expect(screen.getByRole('heading', { name: 'Ditado' })).toBeTruthy()
  })

  it('a resposta do Agent sem o campo e com null: o mesmo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.interceptar((p) => (p.caminho === '/api/ia/testar' ? new Response(JSON.stringify({ ok: true, modelos: daConta, modelo_sugerido: null }), { status: 200 }) : undefined))
    fireEvent.click(botao('Testar conexão'))
    await screen.findByText(/aceitou a chave/)
    expect(modelo().value).toBe('')
  })

  it('esvaziar o campo depois não traz o sugerido de volta sozinho; testar de novo, com o campo vazio, sim', async () => {
    await testarComSugestao()
    expect(modelo().value).toBe('gpt-6-luna')
    fireEvent.change(modelo(), { target: { value: '' } })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 30))
    })
    expect(modelo().value).toBe('')

    fireEvent.click(botao('Testar conexão'))
    await waitFor(() => expect(modelo().value).toBe('gpt-6-luna'))
  })

  it('conexão ruim: sem lista, sem sugestão', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    srv.testeDevolve({ ok: false, erro: 'A OpenAI recusou a chave.' })
    fireEvent.click(botao('Testar conexão'))
    await screen.findByRole('alert')
    expect(modelo().value).toBe('')
    expect(descricao(modelo())).not.toContain('Sugerido')
  })
})

describe('Sistema e Privacidade', () => {
  it('iniciar com o Windows: liga e vai no PUT', async () => {
    const srv = await abrir()
    fireEvent.click(screen.getByLabelText('Iniciar com o Windows'))
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config.autostart).toBe(true))
    expect(puts(srv)[0]?.corpo).toMatchObject({ autostart: true })
  })

  it('iniciar com o Windows: desliga e vai no PUT', async () => {
    const srv = await abrir({ config: { autostart: true } })
    fireEvent.click(screen.getByLabelText('Iniciar com o Windows'))
    fireEvent.click(salvar())
    await waitFor(() => expect(srv.config.autostart).toBe(false))
    expect(puts(srv)[0]?.corpo).toMatchObject({ autostart: false })
  })

  // O registro muda por fora (bandeja, Gerenciador de Tarefas) sem subir
  // a version: o 409 não pega. Reenviar o que a tela leu ao abrir
  // religava o início que a pessoa desligou lá.
  it('desligado por fora depois de abrir: salvar outro ajuste não o religa', async () => {
    const srv = await abrir({ config: { autostart: true } })
    srv.autostartPorFora(false)
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[0]?.corpo).not.toHaveProperty('autostart')
    expect(srv.config.autostart).toBe(false)
    // E a caixa passa a mostrar o que é verdade.
    expect(screen.getByLabelText<HTMLInputElement>('Iniciar com o Windows').checked).toBe(false)
  })

  it('indisponível e mudado por fora: salvar outro ajuste não esbarra num controle que ninguém tocou', async () => {
    const srv = await abrir({ config: { autostart: true, autostart_indisponivel: 'não consegui ler' } })
    srv.autostartPorFora(false)
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(srv.config).toMatchObject({ beep_enabled: false, autostart: false })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('ao voltar às Configurações sem nada por salvar, a tela lê de novo', async () => {
    const srv = await abrir({ config: { autostart: true } })
    fireEvent.click(botao('Histórico'))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    srv.autostartPorFora(false)

    fireEvent.click(botao('Configurações'))
    await waitFor(() => expect(screen.getByLabelText<HTMLInputElement>('Iniciar com o Windows').checked).toBe(false))
    expect(leituras(srv)).toHaveLength(2)
    expect(desligado(salvar())).toBe(true)
  })

  it('ao voltar à aba sem nada por salvar, a tela lê de novo; com algo por salvar, não', async () => {
    const srv = await abrir()
    srv.autostartPorFora(true)
    fireEvent.focus(window)
    await waitFor(() => expect(screen.getByLabelText<HTMLInputElement>('Iniciar com o Windows').checked).toBe(true))
    expect(leituras(srv)).toHaveLength(2)

    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.autostartPorFora(false)
    fireEvent.focus(window)
    await act(() => new Promise((r) => setTimeout(r, 20)))
    expect(leituras(srv)).toHaveLength(2)
    expect(segurar().value).toBe('900')
  })

  it('fora desta vista, voltar à aba não lê as Configurações', async () => {
    const srv = await abrir()
    fireEvent.click(botao('Histórico'))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    fireEvent.focus(window)
    await act(() => new Promise((r) => setTimeout(r, 20)))
    expect(leituras(srv)).toHaveLength(1)
  })

  it('a leitura de volta chega depois de a pessoa mexer: o que ela mexeu fica', async () => {
    const srv = await abrir()
    const soltar = srv.pausar((p) => p.caminho === '/api/settings' && p.metodo === 'GET')
    fireEvent.focus(window)
    await waitFor(() => expect(leituras(srv)).toHaveLength(2))
    fireEvent.change(segurar(), { target: { value: '900' } })
    srv.autostartPorFora(true)
    soltar()
    await act(() => new Promise((r) => setTimeout(r, 20)))
    expect(segurar().value).toBe('900')
    expect(screen.getByLabelText<HTMLInputElement>('Iniciar com o Windows').checked).toBe(false)
    expect(desligado(salvar())).toBe(false)
  })

  it('iniciar com o Windows indisponível: desligado, com o motivo', async () => {
    await abrir({ config: { autostart_indisponivel: 'esta versão roda no console, sem instalador' } })
    const caixa = screen.getByLabelText<HTMLInputElement>('Iniciar com o Windows')
    expect(caixa.disabled).toBe(true)
    expect(descricao(caixa)).toBe('Não dá para mudar nesta instalação: esta versão roda no console, sem instalador')
  })

  it('privacidade: o áudio, as outras janelas, a chave e o texto do Polish', async () => {
    await abrir()
    const secao = screen.getByRole('region', { name: 'Privacidade' })
    const fatos = within(secao).getAllByRole('listitem').map((li) => li.textContent)
    expect(fatos).toHaveLength(4)
    expect(fatos[0]).toContain('áudio não é salvo')
    expect(fatos[1]).toContain('outras janelas não é guardado')
    expect(fatos[2]).toContain('Gerenciador de Credenciais do Windows')
    // A frase aprovada (ADR-016; 08-ui-ux-spec.md e 09-security-privacy.md),
    // palavra por palavra.
    expect(fatos[3]).toBe(
      'No modo Polish, o texto ditado (nunca o áudio nem o que está nas outras janelas) vai ao provedor de IA escolhido, com a sua chave; OpenAI e Anthropic podem guardá-lo por até 30 dias e dizem não usá-lo para treinar modelos.',
    )
  })

  it('a frase do Polish está na Privacidade também sem o Polish liberado no Agent: é aviso, não recurso', async () => {
    await abrir({ config: { polish_disponivel: false } })
    const secao = screen.getByRole('region', { name: 'Privacidade' })
    expect(within(secao).getByText(/No modo Polish, o texto ditado/)).toBeTruthy()
  })
})

// Revisão de 29-set-2026: com disabled, o botão apertado pelo teclado
// perdia o foco para o <body> no meio do pedido — e Salvar configurações,
// Salvar chave e Remover continuavam desligados depois, então o foco não
// voltava. Com aria-disabled o foco fica, e o clique que ainda chega é
// recusado pela própria função.
describe('os botões guardam o foco', () => {
  const pedidosA = (srv: Servidor, metodo: string, caminho: string) => srv.pedidos.filter((p) => p.metodo === metodo && p.caminho === caminho)

  it('Salvar configurações: o foco fica durante e depois, e o segundo clique não salva de novo', async () => {
    const srv = await abrir()
    fireEvent.change(segurar(), { target: { value: '900' } })
    const b = salvar()
    b.focus()
    const soltar = srv.pausar((p) => p.metodo === 'PUT')
    fireEvent.click(b)
    await waitFor(() => expect(puts(srv)).toHaveLength(1))
    expect(b.textContent).toBe('Salvando…')
    expect(desligado(b)).toBe(true)
    expect(document.activeElement).toBe(b)
    fireEvent.click(b)
    soltar()

    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(document.activeElement).toBe(b)
    expect(desligado(b)).toBe(true)
    fireEvent.click(b)
    await act(async () => {})
    expect(puts(srv)).toHaveLength(1)
  })

  it('Salvar chave: o foco fica, e nem o segundo clique nem o Enter guardam de novo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    digitarChave('OpenAI', 'sk-teste-Ff42')
    const b = botao('Salvar chave')
    b.focus()
    const soltar = srv.pausar((p) => p.caminho === '/api/ia/chave' && p.metodo === 'PUT')
    fireEvent.click(b)
    await waitFor(() => expect(pedidosA(srv, 'PUT', '/api/ia/chave')).toHaveLength(1))
    expect(desligado(b)).toBe(true)
    expect(document.activeElement).toBe(b)
    fireEvent.click(b)
    fireEvent.keyDown(screen.getByLabelText('Chave de API da OpenAI'), { key: 'Enter' })
    soltar()

    await screen.findByText('••••Ff42')
    expect(document.activeElement).toBe(b)
    expect(desligado(b)).toBe(true)
    expect(pedidosA(srv, 'PUT', '/api/ia/chave')).toHaveLength(1)
  })

  it('Remover: o foco fica no botão, e sem chave o clique não pergunta de novo', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-9Z9Z' } })
    await screen.findByText('••••9Z9Z')
    const b = botao('Remover chave')
    b.focus()
    fireEvent.click(b)
    const soltar = srv.pausar((p) => p.metodo === 'DELETE')
    fireEvent.click(b)
    await waitFor(() => expect(pedidosA(srv, 'DELETE', '/api/ia/chave')).toHaveLength(1))
    expect(b.textContent).toBe('Removendo…')
    expect(desligado(b)).toBe(true)
    expect(document.activeElement).toBe(b)
    soltar()

    await screen.findByText('Nenhuma chave guardada.')
    expect(document.activeElement).toBe(b)
    expect(desligado(b)).toBe(true)
    fireEvent.click(b)
    expect(b.textContent).toBe('Remover chave')
    expect(estadoDoTeste().textContent).toBe('')
    expect(pedidosA(srv, 'DELETE', '/api/ia/chave')).toHaveLength(1)
  })

  it('Testar conexão: o foco fica, e nem ele nem o Remover valem enquanto o teste está no ar', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    const testar = botao('Testar conexão')
    const remover = botao('Remover chave')
    testar.focus()
    const soltar = srv.pausar((p) => p.caminho === '/api/ia/testar')
    fireEvent.click(testar)
    await waitFor(() => expect(pedidosA(srv, 'POST', '/api/ia/testar')).toHaveLength(1))
    expect(desligado(testar)).toBe(true)
    expect(desligado(remover)).toBe(true)
    expect(document.activeElement).toBe(testar)
    fireEvent.click(testar)
    fireEvent.click(remover)
    expect(remover.textContent).toBe('Remover chave')
    soltar()

    expect(await screen.findByText(/A OpenAI aceitou a chave/)).toBeTruthy()
    expect(document.activeElement).toBe(testar)
    expect(desligado(testar)).toBe(false)
    expect(pedidosA(srv, 'POST', '/api/ia/testar')).toHaveLength(1)
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE')).toEqual([])
  })

  it('sem chave guardada, clicar em Testar ou Remover não faz nada', async () => {
    const srv = await abrir({ config: { ai_provider: 'openai' } })
    await screen.findByText('Nenhuma chave guardada.')
    fireEvent.click(botao('Testar conexão'))
    fireEvent.click(botao('Remover chave'))
    await act(async () => {})
    expect(botao('Remover chave')).toBeTruthy()
    expect(estadoDoTeste().textContent).toBe('')
    expect(srv.pedidos.filter((p) => p.caminho === '/api/ia/testar' || p.metodo === 'DELETE')).toEqual([])
  })
})

// O "ditar para nota" (ADR-022, N9): as teclas são só leitura, como as do
// ditado; o que se muda é ligar e desligar.
describe('o ditar para nota', () => {
  const nota = () => screen.getByLabelText<HTMLInputElement>('Ditar para nota')

  it('mostra as teclas do gatilho e nasce ligado', async () => {
    await abrir()
    expect(nota().checked).toBe(true)
    const teclas = Array.from(document.querySelectorAll('#cfg-nota-teclas kbd'), (k) => k.textContent)
    expect(teclas).toEqual(['Ctrl', 'Win', 'Alt'])
    expect(descricao(nota())).toContain('não para o campo')
    expect(descricao(nota())).toContain('Ctrl')
  })

  it('desligar vai no PUT como false, e ligar de novo como true', async () => {
    const srv = await abrir()
    fireEvent.click(nota())
    expect(screen.getByText('não salvo').className).toBe('etiqueta forte')
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[0]?.corpo).toMatchObject({ note_trigger_enabled: false, base_version: 1 })
    expect(srv.config.note_trigger_enabled).toBe(false)
    expect(nota().checked).toBe(false)

    fireEvent.click(nota())
    fireEvent.click(salvar())
    await waitFor(() => expect(puts(srv)).toHaveLength(2))
    expect(puts(srv)[1]?.corpo).toMatchObject({ note_trigger_enabled: true })
  })

  it('mexer em outro campo não muda o ditar para nota', async () => {
    const srv = await abrir({ config: { note_trigger_enabled: false } })
    expect(nota().checked).toBe(false)
    fireEvent.click(bip())
    fireEvent.click(salvar())
    await waitFor(() => expect(aviso().textContent).toBe('Configurações salvas.'))
    expect(puts(srv)[0]?.corpo).toMatchObject({ beep_enabled: false, note_trigger_enabled: false })
  })

  it('um Agent sem o gatilho (sem note_hotkey) não mostra a opção', async () => {
    await abrir({ config: { note_hotkey: undefined } })
    expect(screen.queryByLabelText('Ditar para nota')).toBeNull()
  })
})
