// A folha do aceite (folha.ts e FolhaDoAceite.tsx; ADR-021, decisão H) com um
// cliente de mentira: o texto que ela mostra (o que vai, para quem, onde o
// provedor processa, dado de terceiro, o áudio que não fica gravado), o "Aceito"
// que grava o aceite no servidor com a versão do aviso, o "Agora não" e o Esc
// que não gravam nada, a escolha do provedor, o Polish, os erros e o foco.

import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { VERSAO_DO_AVISO } from '../avisoDoDitado'
import type { ClienteNuvem } from '../cliente'
import type { Aceite } from './estado'
import { Folha, type PedidoDeAceite, type ResultadoDoAceite } from './folha'
import { FolhaDoAceite } from './FolhaDoAceite'

afterEach(cleanup)

type Aceitar = ClienteNuvem['aceitarAviso']

// O aceite que o servidor devolve ao gravar (a folha só espera que ele diga que
// gravou).
const gravado = (versao: number, transcricao: Aceite['transcricao'], polish: Aceite['polish']): Aceite => ({
  versao,
  transcricao,
  polish,
  aceito_em: '',
  vigente: true,
})

// montar: o cliente e o "recarregar" são dublês (o recarregar é o que o app faz
// com o AVISO_DESATUALIZADO).
function montar(aceitar?: Aceitar) {
  const aceitarAviso = vi.fn<Aceitar>(aceitar ?? (async (versao, transcricao, polish) => gravado(versao, transcricao, polish)))
  const recarregar = vi.fn()
  const folha = new Folha({ aceitarAviso }, recarregar)
  render(
    <>
      <button type="button">Ditar</button>
      <FolhaDoAceite folha={folha} />
    </>,
  )
  return { folha, aceitarAviso, recarregar }
}

// abrir pede a folha e devolve a promessa do resultado, dentro de um objeto:
// uma promessa devolta direto por uma função async seria esperada junto (e a
// folha, que só resolve quando alguém a fecha, travaria o teste).
async function abrir(folha: Folha, pedido: Partial<PedidoDeAceite> = {}) {
  let resultado!: Promise<ResultadoDoAceite | null>
  await act(async () => {
    resultado = folha.pedirAceite({ voz: 'openai', ...pedido })
  })
  return { resultado }
}

const folhaNaTela = () => screen.queryByRole('dialog')
const aceito = () => screen.getByRole('button', { name: 'Aceito' })
const agoraNao = () => screen.getByRole('button', { name: 'Agora não' })

describe('o que a folha diz', () => {
  it('é uma folha só, em destaque: um diálogo modal, com o foco no título', async () => {
    const { folha } = montar()
    await abrir(folha)
    const f = screen.getByRole('dialog')
    expect(f.getAttribute('aria-modal')).toBe('true')
    const titulo = within(f).getByRole('heading', { level: 2 })
    expect(titulo.textContent).toBe('Ao ditar pelo celular, a sua voz vai a um provedor que não diz processar no Brasil.')
    expect(f.getAttribute('aria-labelledby')).toBe(titulo.id)
    expect(document.activeElement).toBe(titulo)
    // Fora do Painel: a folha é filha do body.
    expect(f.closest('body')).toBe(document.body)
  })

  it('o que vai, para quem e por quanto tempo: a voz em memória, a chave cifrada, a dica e o Polish', async () => {
    const { folha } = montar()
    await abrir(folha)
    const t = screen.getByRole('dialog').textContent!
    // A voz: só em memória, não fica gravada, vai ao provedor escolhido.
    expect(t).toContain('A voz de cada ditado.')
    expect(t).toContain('só na memória')
    expect(t).toContain('vai ao provedor que você escolheu: OpenAI')
    expect(t).toContain('Não é gravada no seu aparelho nem no servidor.')
    // A chave: cifrada, mas legível para quem administra o servidor.
    expect(t).toContain('O servidor a guarda cifrada, mas quem administra o servidor consegue decifrá-la.')
    expect(t).toContain('Use uma chave só para o celular, com limite de gasto no provedor.')
    // A dica e o Polish: opcionais, começam desligados; a dica pode levar nome de cliente.
    expect(t).toContain('Se você ligar a dica:')
    expect(t).toContain('Eles podem ser nomes de clientes ou de outras pessoas.')
    expect(t).toContain('A dica começa desligada.')
    expect(t).toContain('Se você ligar o Polish:')
    expect(t).toContain('O Polish começa desligado.')
  })

  it('a voz e os termos podem levar dado de terceiro, como dado de cliente', async () => {
    const { folha } = montar()
    await abrir(folha)
    const t = screen.getByRole('dialog').textContent!
    expect(t).toContain('Dados de outras pessoas')
    expect(t).toContain('A voz e os termos podem ter dados de outras pessoas (clientes, quem está perto de você).')
    expect(t).toContain('O seu aceite vale por você, e não por elas.')
    expect(t).toContain('Se você dita dado de cliente, veja se isso cabe no que você deve a ele.')
  })

  it('é transferência internacional, e a voz não identifica ninguém', async () => {
    const { folha } = montar()
    await abrir(folha)
    const t = screen.getByRole('dialog').textContent!
    expect(t).toContain('Por isso, tratamos o envio como uma transferência internacional.')
    expect(t).toContain('A sua voz não é usada para identificar você.')
  })

  // O "Por isso," retoma o título: o parágrafo que vem logo depois dele começa por ele,
  // e não por "Leia antes de aceitar." (que o deixava solto, sem o que ele explica).
  it('o parágrafo depois do título começa pelo "Por isso," que retoma o título', async () => {
    const { folha } = montar()
    await abrir(folha)
    const titulo = within(screen.getByRole('dialog')).getByRole('heading', { level: 2 })
    expect(titulo.nextElementSibling?.textContent).toBe(
      'Por isso, tratamos o envio como uma transferência internacional. Leia antes de aceitar. Se você tocar em "Agora não", o microfone não abre, nada é enviado e o resto do app continua igual.',
    )
  })

  it('não funciona com a tela apagada nem com o app em segundo plano', async () => {
    const { folha } = montar()
    await abrir(folha)
    expect(screen.getByRole('dialog').textContent).toContain('Não funciona com a tela apagada nem com o app em segundo plano: se a tela apagar, o ditado para.')
  })

  it('onde o provedor processa: só o que a documentação dele diz', async () => {
    const { folha } = montar()
    await abrir(folha)
    const t = screen.getByRole('dialog').textContent!
    expect(t).toContain('Para onde, e por quanto tempo fica lá')
    expect(t).toContain('OpenAI. Ela diz que não usa o que você envia pela API para treinar os modelos dela')
    expect(t).toContain('Ela processa nos EUA ou na Europa se a sua conta fixou a residência de dados; se não, a documentação dela não diz em que país.')
    // O bloco da Groq não aparece para quem escolheu a OpenAI.
    expect(t).not.toContain('retenção zero')
  })

  it('como desfazer: Desligar o ditado no celular, e revogar a chave no provedor', async () => {
    const { folha } = montar()
    await abrir(folha)
    const t = screen.getByRole('dialog').textContent!
    expect(t).toContain('"Desligar o ditado no celular" tira o seu aceite e apaga as suas chaves do servidor.')
    expect(t).toContain('Revogue também a chave no provedor: uma cópia cifrada dela fica nos backups por cerca de 30 dias.')
  })

  it('os botões são "Aceito" e "Agora não"', async () => {
    const { folha } = montar()
    await abrir(folha)
    expect(within(screen.getByRole('dialog')).getAllByRole('button').map((b) => b.textContent)).toEqual(['Aceito', 'Agora não'])
    expect(aceito().className).toContain('primario')
  })
})

describe('Aceito', () => {
  it('grava o aceite no servidor com a versão do aviso e o provedor, fecha, e devolve o provedor', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledTimes(1)
    // A versão do aviso do app (a única, a VERSAO_DO_AVISO), a transcrição e,
    // sem Polish na folha, null.
    expect(aceitarAviso).toHaveBeenCalledWith(VERSAO_DO_AVISO, 'openai', null)
    expect(await resultado).toEqual({ voz: 'openai' })
    expect(folhaNaTela()).toBeNull()
  })

  it('o aceite é só o que a folha mostrou: a voz que ela mostra, e nada do que valia antes', async () => {
    // O servidor guarda dois lugares (a transcrição e o Polish) e cada aceite
    // novo substitui o anterior: trocar para a Groq aceita a Groq.
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha, { voz: 'groq' })
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledWith(VERSAO_DO_AVISO, 'groq', null)
    expect(await resultado).toEqual({ voz: 'groq' })
  })

  it('ligando o Polish: mostra o bloco do provedor dele, e o aceite leva os dois', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha, { voz: 'openai', polish: 'anthropic' })
    const t = screen.getByRole('dialog').textContent!
    expect(t).toContain('O Polish que você está ligando: o texto do ditado, para Anthropic')
    expect(t).toContain('Anthropic. Ela diz que pode guardar o texto por até 30 dias')
    // O da OpenAI continua lá (a voz).
    expect(t).toContain('OpenAI. Ela diz')
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledWith(VERSAO_DO_AVISO, 'openai', 'anthropic')
    expect(await resultado).toEqual({ voz: 'openai' })
  })

  it('o mesmo provedor da voz e do Polish tem um bloco só, e o aceite leva o provedor nos dois lugares', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha, { voz: 'openai', polish: 'openai' })
    expect(screen.getAllByText(/^OpenAI\.$/)).toHaveLength(1)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledWith(VERSAO_DO_AVISO, 'openai', 'openai')
    await resultado
  })

  it('enquanto grava: "Guardando…", e o Agora não, o Esc e um segundo Aceito não fazem nada', async () => {
    let terminar!: () => void
    const { folha, aceitarAviso } = montar(
      () =>
        new Promise((resolver) => {
          terminar = () => resolver(gravado(VERSAO_DO_AVISO, 'openai', null))
        }),
    )
    const { resultado } = await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(screen.getByRole('button', { name: 'Guardando…' }).getAttribute('aria-disabled')).toBe('true')
    expect(agoraNao().getAttribute('aria-disabled')).toBe('true')
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Guardando…' }))
      fireEvent.click(agoraNao())
      fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    })
    expect(folhaNaTela()).not.toBeNull()
    expect(aceitarAviso).toHaveBeenCalledTimes(1)
    await act(async () => terminar())
    expect(await resultado).toEqual({ voz: 'openai' })
    expect(folhaNaTela()).toBeNull()
  })
})

describe('Agora não', () => {
  it('não grava nada, fecha e devolve null', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha)
    await act(async () => {
      fireEvent.click(agoraNao())
    })
    expect(await resultado).toBeNull()
    expect(folhaNaTela()).toBeNull()
    expect(aceitarAviso).not.toHaveBeenCalled()
  })

  it('o Esc é o mesmo que Agora não', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha)
    await act(async () => {
      fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    })
    expect(await resultado).toBeNull()
    expect(folhaNaTela()).toBeNull()
    expect(aceitarAviso).not.toHaveBeenCalled()
  })

  it('o foco volta para quem estava com ele (o microfone)', async () => {
    const { folha } = montar()
    const microfone = screen.getByRole('button', { name: 'Ditar' })
    microfone.focus()
    const { resultado } = await abrir(folha)
    expect(document.activeElement).not.toBe(microfone)
    await act(async () => {
      fireEvent.click(agoraNao())
    })
    await resultado
    expect(document.activeElement).toBe(microfone)
  })
})

describe('a escolha do provedor da voz (o primeiro uso)', () => {
  it('só aparece quando o pedido a pede, com o provedor de partida marcado', async () => {
    const { folha } = montar()
    const { resultado } = await abrir(folha)
    expect(screen.queryByRole('radiogroup')).toBeNull()
    await act(async () => {
      fireEvent.click(agoraNao())
    })
    await resultado

    await abrir(folha, { escolherVoz: true, voz: 'groq' })
    const grupo = screen.getByRole('radiogroup', { name: 'Provedor da voz' })
    const opcoes = within(grupo).getAllByRole<HTMLInputElement>('radio')
    expect(opcoes.map((o) => [o.labels?.[0]?.textContent, o.checked])).toEqual([
      ['OpenAI', false],
      ['Groq', true],
    ])
  })

  it('trocar para a Groq muda o bloco do provedor e o que o Aceito grava', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha, { escolherVoz: true })
    expect(screen.getByRole('dialog').textContent).toContain('OpenAI. Ela diz')
    await act(async () => {
      fireEvent.click(screen.getByRole('radio', { name: 'Groq' }))
    })
    const t = screen.getByRole('dialog').textContent!
    expect(t).toContain('vai ao provedor que você escolheu: Groq')
    expect(t).toContain('Groq. Ela diz que, por padrão, não retém o que você envia')
    expect(t).toContain('a documentação dela não diz onde a transcrição roda')
    expect(t).not.toContain('OpenAI. Ela diz')
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledWith(VERSAO_DO_AVISO, 'groq', null)
    expect(await resultado).toEqual({ voz: 'groq' })
  })
})

describe('só ler o aviso', () => {
  it('quem já aceitou relê: só Fechar, e nada é gravado', async () => {
    const { folha, aceitarAviso } = montar()
    const { resultado } = await abrir(folha, { somenteLer: true })
    expect(within(screen.getByRole('dialog')).getAllByRole('button').map((b) => b.textContent)).toEqual(['Fechar'])
    expect(screen.getByRole('dialog').textContent).toContain('Você já aceitou este aviso')
    expect(screen.getByRole('dialog').textContent).not.toContain('Se você tocar em "Agora não"')
    // Sem o "Leia antes de aceitar." (já aceitou), o "Por isso," continua retomando o título.
    const titulo = within(screen.getByRole('dialog')).getByRole('heading', { level: 2 })
    expect(titulo.nextElementSibling?.textContent).toBe(
      'Você já aceitou este aviso; ele está aqui para você reler. Por isso, tratamos o envio como uma transferência internacional.',
    )
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    })
    expect(await resultado).toBeNull()
    expect(aceitarAviso).not.toHaveBeenCalled()
  })
})

describe('quando o aceite não chega ao servidor', () => {
  it('sem rede: a folha fica, com a frase, e dá para tentar de novo', async () => {
    let falhar = true
    const { folha, aceitarAviso } = montar(async (versao, transcricao, polish) => {
      if (falhar) throw new ForaDoAr()
      return gravado(versao, transcricao, polish)
    })
    const { resultado } = await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(screen.getByRole('alert').textContent).toBe('Sem conexão. Tente de novo.')
    expect(folhaNaTela()).not.toBeNull()
    falhar = false
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledTimes(2)
    expect(await resultado).toEqual({ voz: 'openai' })
  })

  it('a frase é nossa: a mensagem do servidor nunca vai à tela', async () => {
    const { folha } = montar(async () => {
      throw new ErroDaApi(500, 'chave sk-segredo-que-o-servidor-repetiu')
    })
    await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(screen.getByRole('alert').textContent).toBe('Não deu para guardar o aceite. Tente de novo.')
    expect(document.body.textContent).not.toContain('sk-segredo')
  })

  it('o servidor sem o ditado (404) e o limite (429)', async () => {
    let erro: ErroDaApi = new ErroDaApi(404, 'x')
    const { folha } = montar(async () => {
      throw erro
    })
    await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(screen.getByRole('alert').textContent).toBe('Este servidor não tem o ditado pelo celular.')
    erro = new ErroDaApi(429, 'x')
    erro.esperaSeg = 12
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(screen.getByRole('alert').textContent).toBe('Espere um pouco. Tente de novo em 12 s.')
  })

  it('a sessão que acabou: a folha fecha, devolve null e acende a faixa da sessão', async () => {
    const semSessao = vi.fn()
    const { folha } = montar(async () => {
      throw new SemSessao()
    })
    folha.ligar(semSessao)
    const { resultado } = await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(await resultado).toBeNull()
    expect(folhaNaTela()).toBeNull()
    expect(semSessao).toHaveBeenCalledTimes(1)
  })

  it('o aviso que o app mostrou não é o atual (AVISO_DESATUALIZADO, 409): a folha fecha sem aceite e o app recarrega', async () => {
    const velho = new ErroDaApi(409, 'o aviso que o app mostrou não é o atual')
    velho.codigo = 'AVISO_DESATUALIZADO'
    const { folha, aceitarAviso, recarregar } = montar(async () => {
      throw velho
    })
    const { resultado } = await abrir(folha)
    expect(recarregar).not.toHaveBeenCalled()
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(aceitarAviso).toHaveBeenCalledTimes(1)
    // Não é aceite, e não fica uma folha com o texto velho esperando outro toque.
    expect(await resultado).toBeNull()
    expect(folhaNaTela()).toBeNull()
    expect(recarregar).toHaveBeenCalledTimes(1)
  })

  it('o 409 que não diz AVISO_DESATUALIZADO não recarrega: é um erro como os outros', async () => {
    const { folha, recarregar } = montar(async () => {
      throw new ErroDaApi(409, 'x')
    })
    await abrir(folha)
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(screen.getByRole('alert').textContent).toBe('Não deu para guardar o aceite. Tente de novo.')
    expect(folhaNaTela()).not.toBeNull()
    expect(recarregar).not.toHaveBeenCalled()
  })
})

describe('uma folha por vez', () => {
  it('um segundo pedido, com a folha na tela, não abre outra e devolve null', async () => {
    const { folha } = montar()
    const { resultado: primeiro } = await abrir(folha)
    const { resultado: segundo } = await abrir(folha)
    expect(await segundo).toBeNull()
    expect(screen.getAllByRole('dialog')).toHaveLength(1)
    await act(async () => {
      fireEvent.click(agoraNao())
    })
    await primeiro
  })
})

describe('o teclado', () => {
  it('o Tab não sai da folha: do último botão volta ao primeiro, e o Shift+Tab do título vai ao último', async () => {
    const { folha } = montar()
    await abrir(folha, { escolherVoz: true })
    const f = screen.getByRole('dialog')
    const botoes = within(f).getAllByRole('button')
    const ultimo = botoes[botoes.length - 1]!
    const primeiroFocavel = within(f).getAllByRole('radio')[0]!
    ultimo.focus()
    fireEvent.keyDown(f, { key: 'Tab' })
    expect(document.activeElement).toBe(primeiroFocavel)
    // O foco nasce no título: o Shift+Tab dali não escapa para a página.
    within(f).getByRole('heading', { level: 2 }).focus()
    fireEvent.keyDown(f, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(ultimo)
  })
})
