// O que o CSS promete, conferido no próprio index.css: contraste calculado
// a partir dos tokens dos dois temas (System, escuro; Clear, claro) e do
// que cada regra declara, a paleta do design system valor por valor, o
// papel de cada cor, e as regras que o jsdom não aplica (foco sob o
// cabeçalho, Temas de Contraste, sublinhado do que entrou, linha da
// Lixeira).

import { describe, expect, it } from 'vitest'
import { linhaDoAprendido } from './aprendizado'
import { bloco, contraste, css, regras, temaClaro, temaEscuro, temas, valor } from './testes/css'

// Os oito tons das notas (storage.CoresDaNota), em cada tema.
const notaCores = ['amarelo', 'laranja', 'vermelho', 'rosa', 'roxo', 'azul', 'verde', 'cinza'].map((c) => `n-${c}`)

// Cada par [frente, fundos...] é texto que precisa de 4,5:1 (WCAG 1.4.3),
// ou um contorno que precisa de 3:1 (WCAG 1.4.11).
const texto: [string, ...string[]][] = [
  ['texto', 'fundo', 'superficie', 'realce'],
  // --texto-2 e --texto-3 são texto de 11 a 15 px, e a linha sob o
  // ponteiro ganha --realce por trás.
  ['texto-2', 'fundo', 'superficie', 'realce'],
  ['texto-3', 'fundo', 'superficie', 'realce'],
  // A etiqueta "falhou", na linha (fundo, realce) e no editor (superfície);
  // o botão de risco (nuvem), também sob o ponteiro (risco-tinta).
  ['risco-texto', 'fundo', 'superficie', 'realce', 'risco-tinta'],
  // As notas de erro e conflito: texto comum sobre o fundo de risco.
  ['texto', 'risco-tinta'],
  // Botão principal e vista escolhida: texto preto no verde, também no
  // verde do hover.
  ['tinta-texto', 'tinta', 'tinta-forte'],
  // O aviso e a faixa são escuros nos dois temas.
  ['pilula-texto', 'pilula', 'risco-fundo'],
  ['pilula-texto-2', 'pilula'],
  ['acento', 'pilula', 'risco-fundo'],
  // O que entrou no diff, e o texto selecionado.
  ['texto', 'entrou', 'selecao'],
  // O ditado que acabou de chegar (ADR-021): a linha inteira, com a hora e a
  // etiqueta "novo" em cinza, sobre o fundo do que entrou.
  ['texto-2', 'entrou'],
  ['texto-3', 'entrou'],
  // As notas coloridas (ADR-022): o título, o corpo, as etiquetas e a hora do
  // cartão e da nota aberta, a linha riscada dos marcados e o texto dos botões
  // da barra, tudo sobre os oito tons de cada tema.
  ['texto', ...notaCores],
  ['texto-2', ...notaCores],
  ['texto-3', ...notaCores],
]

const contorno: [string, ...string[]][] = [
  ['contorno', 'fundo', 'superficie'],
  // O foco encosta no fundo da página, no do cartão e no da linha, e na nota
  // colorida que está aberta.
  ['foco', 'fundo', 'superficie', 'realce', ...notaCores],
  // A borda da caixa de marcar (--texto-2) sobre os oito tons.
  ['texto-2', ...notaCores],
]

describe('contraste', () => {
  describe.each(Object.entries(temas))('tema %s', (_, t) => {
    it.each(texto)('texto --%s: 4,5:1', (frente, ...fundos) => {
      for (const fundo of fundos) {
        expect(t[frente], `falta --${frente}`).toBeDefined()
        expect(t[fundo], `falta --${fundo}`).toBeDefined()
        expect(contraste(t[frente]!, t[fundo]!), `--${frente} sobre --${fundo}`).toBeGreaterThanOrEqual(4.5)
      }
    })

    it.each(contorno)('contorno --%s: 3:1', (frente, ...fundos) => {
      for (const fundo of fundos) {
        expect(contraste(t[frente]!, t[fundo]!), `--${frente} sobre --${fundo}`).toBeGreaterThanOrEqual(3)
      }
    })
  })

  it('busca, campo de edição e campos das Configurações usam o contorno', () => {
    expect(bloco('\n.busca {')).toContain('border: 1px solid var(--contorno)')
    expect(bloco('\n.campo {')).toContain('border: 1px solid var(--contorno)')
    expect(bloco('\n.entrada {')).toContain('border: 1px solid var(--contorno)')
  })

  // Faixa, caixas e opções das Configurações são os controles nativos,
  // pintados com accent-color. O verde vivo teria 1,6:1 sobre o claro: o
  // estado marcado sumiria. O foco tem 3:1 nos dois temas (tabela acima).
  it('os controles nativos das Configurações usam o verde do foco, não o vivo', () => {
    const pintados = regras.filter((r) => /accent-color:/.test(r.corpo))
    expect(pintados.map((r) => r.seletor).sort()).toEqual(['.marcar input,\n.modo input', '.regua input'])
    for (const r of pintados) expect(valor(r.corpo, 'accent-color')).toBe('var(--foco)')
  })

  // Caixa e opção desligadas (autostart indisponível, Polish bloqueado)
  // continuam legíveis: o texto delas vai para --texto-2, que a tabela
  // acima segura em 4,5:1 — não para uma opacidade qualquer.
  it('o rótulo de caixa e de opção desligadas usa --texto-2', () => {
    expect(valor(bloco('\n.marcar:has(input:disabled) {'), 'color')).toBe('var(--texto-2)')
    expect(valor(bloco('\n.modo input:disabled + label {'), 'color')).toBe('var(--texto-2)')
  })

  // O verde vivo some sobre o claro (1,6:1). A vista escolhida precisa se
  // separar do fundo por outra coisa: o anel do foco, 3:1 nos dois temas.
  it('a vista escolhida tem o anel do foco, que se separa do fundo', () => {
    expect(bloco("\n.vistas button[aria-pressed='true'] {")).toContain('border-color: var(--foco)')
    expect(contraste(temaClaro.tinta!, temaClaro.fundo!)).toBeLessThan(3)
  })

  it('o foco é visível em todo controle', () => {
    expect(bloco('\n:focus-visible {')).toContain('outline: 2px solid var(--foco)')
  })
})

// As tabelas acima conferem pares de tokens; aqui se confere que as regras
// usam esses pares. Toda regra que pinta o próprio fundo e a própria cor é
// conferida com o que ela declara, nos dois temas. Sem isto, trocar o fundo
// da faixa por --fundo deixava, no claro, texto off-white sobre off-white
// (1:1), e a suíte continuava verde (revisão de 29-set-2026).
describe('cada regra que pinta fundo e texto tem contraste com o que declara', () => {
  const token = (v?: string) => v?.match(/^var\(--([\w-]+)\)$/)?.[1]
  const pintam = regras.flatMap((r) => {
    const fundo = token(valor(r.corpo, 'background'))
    const cor = token(valor(r.corpo, 'color'))
    return fundo && cor ? [[r.seletor, cor, fundo] as const] : []
  })

  // As que carregam texto de verdade não podem sair da lista sem ninguém
  // ver: uma faixa com color: inherit ficaria com o texto do tema sobre o
  // escuro da faixa.
  it('a faixa, o aviso, as notas, o botão principal e a vista escolhida estão entre elas', () => {
    expect(pintam.map(([s]) => s)).toEqual(
      expect.arrayContaining(['body', '.faixa', '.aviso', '.nota', '.botao.primario', ".vistas button[aria-pressed='true']"]),
    )
  })

  describe.each(Object.entries(temas))('tema %s', (_, t) => {
    it.each(pintam)('%s: --%s sobre --%s, 4,5:1', (_, cor, fundo) => {
      expect(contraste(t[cor]!, t[fundo]!)).toBeGreaterThanOrEqual(4.5)
    })
  })
})

// A paleta é a do projeto (os tokens do index.css), valor por valor. O
// contraste sozinho não segura isso: trocar o verde do sistema por
// um verde qualquer passava em todos os mínimos (revisão de 29-set-2026).
// Os tons que não existem no sistema — os cinzas de texto do claro, o
// vermelho escurecido para texto no claro — ficam de fora: saíram do
// contraste, e a tabela de contraste é que os segura.
describe('a paleta do design system', () => {
  const ds = {
    verde: '#00e676',
    preto: '#070a09',
    offWhite: '#f5f7f5',
    verdeProfundo: '#0f3d2e',
    cinza: '#9aa4a0',
    vermelho: '#ff3b3b',
    emergencia: '#120606',
    superficie: '#0c1210',
    elevada: '#111916',
    borda: '#24322d',
    textoSuave: '#aeb9b4',
    clearSuperficie: '#ffffff',
    clearBorda: '#dde4e0',
    clearTexto: '#101512',
  }
  // Iguais nos dois temas: a ação, o aviso e a faixa.
  const ambos = {
    tinta: ds.verde,
    'tinta-texto': ds.preto,
    moldura: ds.verde,
    acento: ds.verde,
    pilula: ds.elevada,
    'pilula-borda': ds.borda,
    'pilula-texto': ds.offWhite,
    'pilula-texto-2': ds.textoSuave,
    'risco-fundo': ds.emergencia,
  }
  const esperado = {
    escuro: {
      ...ambos,
      fundo: ds.preto,
      superficie: ds.superficie,
      realce: ds.elevada,
      texto: ds.offWhite,
      'texto-2': ds.textoSuave,
      'texto-3': ds.cinza,
      linha: ds.borda,
      foco: ds.verde,
      entrou: ds.verdeProfundo,
      'risco-texto': ds.vermelho,
      'marca-traco': ds.offWhite,
      'marca-moldura': ds.verde,
    },
    claro: {
      ...ambos,
      fundo: ds.offWhite,
      superficie: ds.clearSuperficie,
      texto: ds.clearTexto,
      linha: ds.clearBorda,
      foco: ds.verdeProfundo,
      'marca-traco': ds.preto,
      'marca-moldura': ds.verde,
    },
  }

  it.each(Object.entries(esperado))('tema %s', (nome, tokens) => {
    expect(temas[nome as keyof typeof temas]).toMatchObject(tokens)
  })

  // A borda de risco é o vermelho do sistema (#FF3B3B), translúcido.
  it('a borda de risco é o vermelho do sistema', () => {
    expect(valor(bloco(':root {'), '--risco-borda')).toBe('rgb(255 59 59 / 0.55)')
  })
})

describe('o papel de cada cor do design system', () => {
  // As linhas de código, sem os comentários — o cabeçalho do index.css
  // cita as cores ao explicar o mundo.
  const codigo = css
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .map((l) => l.toLowerCase())
  const com = (cores: string[]) => codigo.filter((l) => cores.some((c) => l.includes(c)))

  // Vermelho é risco, erro ou revisão humana — e aparece sempre ao lado do
  // texto que diz o que houve. A cor crua só existe nos tokens de risco;
  // os tokens, só nas regras que carregam esse texto.
  it('vermelho: a cor crua só nos tokens de risco', () => {
    const linhas = com(['#ff3b3b', '#c62828', '#fdeeee', '#120606', '255 59 59'])
    expect(linhas.length).toBeGreaterThan(0)
    for (const l of linhas) expect(l, 'vermelho fora de um token --risco').toMatch(/^\s*--risco[\w-]*:/)
  })

  // O botão de risco (ADR-019, F4) é o de ação que não se desfaz: Excluir
  // definitivamente e Apagar meus dados da nuvem, sempre com a palavra.
  it('vermelho: só na etiqueta "falhou", nas notas de erro e conflito, na faixa de Agent fora do ar e no botão de risco', () => {
    const usam = regras.filter((r) => /var\(--risco/.test(r.corpo)).map((r) => r.seletor)
    expect(usam.sort()).toEqual(
      // '.menu .risco' é o "Mover para a lixeira" dos menus das notas.
      // '.erro-campo' é a frase de nome repetido nos campos de marcador (texto no --superficie).
      ['.botao.risco', ".botao.risco:hover:not(:disabled, [aria-disabled='true'])", '.erro-campo', '.etiqueta.risco', '.faixa', '.menu .risco', '.nota'].sort(),
    )
  })

  // O limite de baixo: nos três lugares, o vermelho está onde se vê — no
  // texto e na borda da etiqueta, na borda da faixa e da nota. Citar um
  // token --risco não basta: um fundo quase preto também cita (revisão de
  // 29-set-2026).
  it('vermelho: visível na etiqueta "falhou", na faixa e nas notas', () => {
    const etiqueta = bloco('\n.etiqueta.risco {')
    expect(valor(etiqueta, 'color')).toBe('var(--risco-texto)')
    expect(valor(etiqueta, 'border-color')).toBe('var(--risco-borda)')

    const faixa = bloco('\n.faixa {')
    expect(valor(faixa, 'border')).toBe('1px solid var(--risco-borda)')
    expect(valor(faixa, 'background')).toBe('var(--risco-fundo)')
    expect(valor(faixa, 'color')).toBe('var(--pilula-texto)')

    const nota = bloco('\n.nota {')
    expect(valor(nota, 'border')).toBe('1px solid var(--risco-borda)')
    expect(valor(nota, 'background')).toBe('var(--risco-tinta)')
    expect(valor(nota, 'color')).toBe('var(--texto)')

    const risco = bloco('\n.botao.risco {')
    expect(valor(risco, 'border-color')).toBe('var(--risco-borda)')
    expect(valor(risco, 'color')).toBe('var(--risco-texto)')
  })

  // Verde é ação e estado válido, nunca enfeite. Uma regra nova que queira
  // verde muda esta lista de propósito. O "Aprendido:" do aviso (ADR-017) é
  // o estado confirmado: só os termos dele ficam verdes, e a sugestão, que
  // ainda não é nada, fica no off-white da pílula.
  it('verde: só ação, vista escolhida, foco, as marcas e o que foi confirmado', () => {
    for (const l of com(['#00e676', '#3ded8f'])) expect(l, 'verde fora de um token').toMatch(/^\s*--[\w-]+:/)

    const usam = regras.filter((r) => /var\(--(tinta|tinta-forte|acento|moldura|marca-moldura)\)/.test(r.corpo)).map((r) => r.seletor)
    expect(usam.sort()).toEqual(
      [
        ".vistas button[aria-pressed='true']",
        '.aurora-moldura',
        '.aviso-acao',
        '.aviso-acao:focus-visible',
        '.aviso-acao:hover',
        '.aviso-texto:focus-visible',
        '.botao.primario',
        ".botao.primario:hover:not(:disabled, [aria-disabled='true'])",
        // A nota aberta: os colchetes do editor e a caixa de marcar marcada.
        '.caixa.feita',
        '.confirmado .termo',
        // O "Ditar para nota" do celular (N8): o botão de ação e o parar da gravação.
        '.ditar-nota',
        '.gravando-nota .parar',
        '.dialogo::before,\n.dialogo::after',
        '.editor::before,\n.editor::after',
        '.marca-moldura',
      ].sort(),
    )
    // O foco é verde no escuro; no claro, o verde profundo.
    expect(temaEscuro.foco).toBe('#00e676')
    expect(temaClaro.foco).toBe('#0f3d2e')
  })

  // O sistema pede off-white no lugar de branco puro nas superfícies
  // extensas: no claro, branco só em --superficie (o cartão do editor
  // aberto e o campo de busca).
  it('off-white no lugar de branco', () => {
    expect(com(['#fff'])).toEqual(['  --superficie: #ffffff;'])
  })

  it('nada de gradiente, brilho ou neon', () => {
    expect(codigo.join('\n')).not.toMatch(/gradient\(|text-shadow|drop-shadow|filter:/)
  })

  // O que a F4 acrescentou (as telas da nuvem, o selo, o excluir de vez) não
  // tem cor solta: só os tokens de cada tema.
  it('o CSS do app da nuvem só usa tokens', () => {
    const inicio = css.indexOf('/* ─── App da nuvem')
    const fim = css.indexOf('@keyframes abrir')
    expect(inicio).toBeGreaterThan(0)
    expect(fim).toBeGreaterThan(inicio)
    const secao = css.slice(inicio, fim).replace(/\/\*[\s\S]*?\*\//g, '')
    expect(secao).not.toMatch(/#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(/i)
    for (const [, cor] of secao.matchAll(/(?:^|[\s;{])(?:color|background|border-color|border|outline|text-decoration):\s*([^;]+);/g)) {
      expect(cor, 'cor fora dos tokens').toMatch(/^(var\(--[\w-]+\)|\d+px solid var\(--[\w-]+\)|underline \d+px var\(--[\w-]+\)|transparent|inherit|none|0)$/)
    }
  })
})

describe('tipografia do design system', () => {
  it('as três famílias, com os fallbacks do sistema', () => {
    const raiz = bloco(':root {')
    expect(valor(raiz, '--fonte')).toBe("Inter, Aptos, 'Segoe UI', Arial, sans-serif")
    expect(valor(raiz, '--fonte-titulo')).toBe("'Space Grotesk', 'Aptos Display', 'Segoe UI', Arial, sans-serif")
    expect(valor(raiz, '--fonte-mono')).toBe("'IBM Plex Mono', 'Cascadia Mono', Consolas, monospace")
    expect(valor(bloco('\nbody {'), 'font-family')).toBe('var(--fonte)')
  })

  it.each(['.hora', '.dia', '.desde', '.etiqueta', '.meta', '.atalhos', '.aviso-atalho', '.vazio', 'kbd', '.valor', '.final', '.termo', '.sessao-uso'])(
    'IBM Plex Mono em hora, data e estado: %s',
    (seletor) => {
      expect(valor(bloco(`\n${seletor} {`), 'font-family')).toBe('var(--fonte-mono)')
    },
  )

  it.each(['.nome', '.estado-titulo', '.bloqueio h1', '.secao-titulo', '.tela h1'])('Space Grotesk no nome e nos títulos: %s', (seletor) => {
    expect(valor(bloco(`\n${seletor} {`), 'font-family')).toBe('var(--fonte-titulo)')
  })
})

describe('regras que o jsdom não aplica', () => {
  it('o foco não fica sob o cabeçalho fixo', () => {
    expect(bloco('\nhtml {')).toMatch(/scroll-padding-top:\s*calc\(var\(--altura-topo/)
  })

  it('o que entrou no diff é sublinhado, não só cor', () => {
    expect(bloco('\n.diff ins {')).toMatch(/text-decoration:\s*underline/)
  })

  it('"saiu:" e "entrou:" vêm do CSS', () => {
    expect(bloco('\n.diff del::before {')).toContain("content: 'saiu: '")
    expect(bloco('\n.diff ins::before {')).toContain("content: 'entrou: '")
  })

  it('Temas de Contraste: a vista escolhida e o foco dos campos continuam visíveis', () => {
    const forcado = bloco('@media (forced-colors: active)')
    expect(forcado).toMatch(/\.vistas button\[aria-pressed='true'\]\s*\{[^}]*background:\s*Highlight/)
    expect(forcado).toMatch(/\.entrada:focus-visible,\s*\.busca:focus-visible,\s*\.campo:focus-visible\s*\{[^}]*outline:\s*2px solid Highlight/)
  })

  // O navegador dá ao select a fonte e a cor dele (Arial, branco ou preto
  // puro): sem o reset, o Provedor ficava fora do design system.
  it('todo controle herda a fonte e a cor da página, o select inclusive', () => {
    const reset = regras.find((r) => valor(r.corpo, 'font') === 'inherit')
    expect(reset?.seletor.split(',').map((s) => s.trim()).sort()).toEqual(['button', 'input', 'select', 'textarea'])
    expect(valor(reset!.corpo, 'color')).toBe('inherit')
  })

  it('o foco dos campos das Configurações é o mesmo da busca', () => {
    expect(css).toMatch(/\n\.entrada:focus-visible,\s*\.busca:focus-visible,\s*\.campo:focus-visible\s*\{[^}]*border-color:\s*var\(--foco\)/)
  })

  // Na mesma linha dos botões, a barra de ações não cabia no cartão, e
  // "Mover para a lixeira" quebrava sozinho (revisão de 29-set-2026).
  it('o lembrete dos atalhos vai numa linha só dele, embaixo dos botões', () => {
    const atalhos = bloco('\n.atalhos {')
    expect(valor(atalhos, 'order')).toBe('1')
    expect(valor(atalhos, 'flex-basis')).toBe('100%')
  })

  // O 08 promete 44 px a todo alvo com o ponteiro de toque (WCAG 2.5.5).
  // As abas tinham 38 (revisão da F4); os botões da faixa, 30 (a faixa da
  // sessão que acaba com um texto não salvo).
  it('no toque, os botões, a busca, as abas, o copiar e os botões da faixa têm 44 px', () => {
    const toque = bloco('@media (pointer: coarse)')
    for (const seletor of ['.botao', '.busca', '.vistas button', '.copiar-linha', '.faixa .aviso-acao', '.ditar-botao', '.ditar-faixa']) {
      const corpo = toque.match(new RegExp(`(?:^|\\n)\\s*${seletor.replace(/\./g, '\\.')}\\s*\\{([^}]*)\\}`))?.[1]
      expect(corpo, seletor).toBeDefined()
      expect(valor(corpo!, 'height'), seletor).toBe('44px')
    }
  })

  // A faixa da sessão, com a frase e o botão em duas linhas, deixava uns 90 px
  // do editor à vista num celular de 375 x 440 (o teclado aberto): com pouca
  // altura, ela fica numa linha só, e a frase sai da vista, não do leitor de
  // tela.
  it('com pouca altura, a faixa da sessão fica numa linha, e a frase só para o leitor de tela', () => {
    const media = bloco('@media (max-height: 560px)')
    const regra = (seletor: string) => {
      expect(media, seletor).toContain(seletor)
      const r = regras.find((x) => x.seletor === seletor)
      expect(r, seletor).toBeDefined()
      return r!.corpo
    }
    expect(valor(regra('.faixa-compactavel'), 'flex-wrap')).toBe('nowrap')
    expect(valor(regra('.faixa-compactavel .faixa-acoes'), 'flex-basis')).toBe('auto')
    const frase = regra('.faixa-compactavel .faixa-frase')
    // Escondida como o .so-leitor: display: none a tiraria do leitor de tela.
    expect(valor(frase, 'position')).toBe('absolute')
    expect(valor(frase, 'clip-path')).toBe('inset(50%)')
    expect(frase).not.toMatch(/display:\s*none|visibility:\s*hidden/)
  })

  // F8h: as linhas de estado do "Aprendido no painel" levam uma .etiqueta
  // depois da troca. Ela é nowrap (nunca quebra no meio), e por isso cada uma
  // tem de caber SOZINHA numa linha do celular mais estreito (320 px, com o
  // recuo lateral do .painel): quando a troca ocupa o resto, a etiqueta desce
  // inteira para a linha de baixo. A conta é a do CSS: texto em mono (0,6 em
  // por caractere, o avanço da IBM Plex Mono), o recuo e a borda dela.
  it.each([
    ['nao_aplicado', 'não aplicado neste PC'],
    ['desfeito_em_outro_aparelho', 'desfeito em outro aparelho'],
    ['trocado_em_outro_aparelho', 'trocado em outro aparelho'],
  ])('a etiqueta da linha de estado "%s" é nowrap e cabe em 320 px', (situacao, texto) => {
    const l = linhaDoAprendido({ termo: 'ECF', variante: 'sf', situacao })
    expect(l).toMatchObject({ tipo: 'estado', etiqueta: texto })
    const etiqueta = bloco('\n.etiqueta {')
    expect(valor(etiqueta, 'white-space')).toBe('nowrap')
    expect(valor(etiqueta, 'font-family')).toBe('var(--fonte-mono)')
    const fonte = parseFloat(valor(etiqueta, 'font-size')!)
    const lateral = parseFloat(valor(etiqueta, 'padding')!.split(/\s+/)[1]!)
    const largura = texto.length * 0.6 * fonte + 2 * lateral + 2 + 4 /* o margin-left dela dentro da troca */
    const recuo = Number(css.match(/@media \(max-width: 520px\) \{\s*\.painel \{\s*padding: 0 (\d+)px/)?.[1])
    expect(recuo, 'o recuo do .painel no celular').toBeGreaterThan(0)
    expect(largura).toBeLessThanOrEqual(320 - 2 * recuo)
    // A troca não a espreme: a etiqueta é inline, e quem quebra é a troca.
    expect(valor(bloco('\n.aprendidas .troca .etiqueta {'), 'margin-left')).toBe('4px')
  })

  it('o motivo do "não aplicado" é uma frase pequena e discreta embaixo da troca', () => {
    const motivo = bloco('\n.aprendidas .motivo {')
    expect(valor(motivo, 'display')).toBe('block')
    expect(valor(motivo, 'color')).toBe('var(--texto-3)')
    expect(parseFloat(valor(motivo, 'font-size')!)).toBeLessThan(13)
  })

  // O Desfazer do celular pergunta na própria linha, como o Revogar: a pergunta
  // desce para uma linha inteira, e a variante (texto ditado) quebra em vez de
  // empurrar o botão para fora da tela.
  it('o aprendido do celular: a pergunta ocupa a linha, e a variante quebra em qualquer ponto', () => {
    const regra = (seletor: string) => {
      const r = regras.find((x) => x.seletor.split(',').map((s) => s.trim()).includes(seletor))
      expect(r, seletor).toBeDefined()
      return r!.corpo
    }
    expect(valor(regra('.aprendidos li'), 'flex-wrap')).toBe('wrap')
    expect(valor(regra('.aprendidos .confirmar'), 'flex-basis')).toBe('100%')
    // O texto encolhe (base de 8 rem) antes de o Desfazer descer para outra linha.
    expect(valor(regra('.aprendidos .sessao'), 'flex')).toBe('1 1 8rem')
    expect(valor(regra('.aprendidos .sessao-nome'), 'overflow-wrap')).toBe('anywhere')
    expect(valor(regra('.aprendidos .sessao-uso'), 'overflow-wrap')).toBe('anywhere')
  })

  // O ditado no celular (ADR-021, C6a): o microfone ao lado da busca, e a faixa
  // da onda que a troca enquanto grava.
  describe('o microfone e a faixa da onda', () => {
    const regra = (seletor: string) => {
      const r = regras.find((x) => x.seletor === seletor)
      expect(r, seletor).toBeDefined()
      return r!.corpo
    }

    // Tons de cinza, como a pílula do overlay do PC: o verde é da ação e o
    // vermelho, do erro; gravar não é nenhum dos dois (e o teste do papel de
    // cada cor trava que estas regras não entrem nas listas deles).
    it('a onda é cinza: as barras usam --texto-2, 2 px de largura e 1 px entre elas, como o overlay do PC', () => {
      const barra = regra('.onda-barra')
      expect(valor(barra, 'background')).toBe('var(--texto-2)')
      expect(valor(barra, 'width')).toBe('2px')
      expect(valor(regra('.onda'), 'gap')).toBe('1px')
      // Em repouso: 12% da altura e 45% do brilho (o JS sobe os dois com a voz).
      expect(valor(barra, 'transform')).toBe('scaleY(0.12)')
      expect(valor(barra, 'opacity')).toBe('0.45')
    })

    it('a faixa tem o contorno e a altura da busca; o botão, a altura dela', () => {
      const faixa = regra('.ditar-faixa')
      const busca = bloco('\n.busca {')
      expect(valor(faixa, 'border')).toBe(valor(busca, 'border'))
      expect(valor(faixa, 'height')).toBe(valor(busca, 'height'))
      expect(valor(faixa, 'border-radius')).toBe(valor(busca, 'border-radius'))
      expect(valor(regra('.ditar-botao'), 'height')).toBe(valor(faixa, 'height'))
      expect(valor(regra('.ditar-botao'), 'width')).toBe(valor(faixa, 'height'))
    })

    it('no toque, o Cancelar preenche o miolo da faixa (44 px menos as duas bordas)', () => {
      const toque = bloco('@media (pointer: coarse)')
      const cancelar = toque.match(/\.ditar-cancelar\s*\{([^}]*)\}/)?.[1]
      expect(valor(cancelar!, 'height')).toBe('42px')
    })

    it('em 320 px a onda perde as 5 barras mais antigas, e não as pontas: 28 barras (83 px) cabem', () => {
      const media = bloco('@media (max-width: 340px)')
      expect(media).toMatch(/\.onda-barra:nth-child\(-n \+ 5\)\s*\{\s*display:\s*none/)
      // 33 barras de 2 px com 1 px entre elas são 98 px; 28 são 83.
      const largura = (n: number) => n * 2 + (n - 1)
      expect(largura(33)).toBe(98)
      expect(largura(28)).toBe(83)
    })

    it('o "Transcrevendo" pulsa, e quem pede menos movimento não o vê pulsar', () => {
      expect(valor(regra('.ditar-enviando'), 'animation')).toMatch(/^pulsar /)
      const reduzido = bloco('@media (prefers-reduced-motion: reduce)')
      expect(reduzido).toMatch(/\.ditar-enviando\s*\{[^}]*animation:\s*none/)
    })

    it('o ocupado continua legível: o microfone sem resposta usa --texto-3, que passa em 4,5:1', () => {
      expect(valor(regra(".ditar-botao[aria-disabled='true']"), 'color')).toBe('var(--texto-3)')
      for (const t of Object.values(temas)) expect(contraste(t['texto-3']!, t['superficie']!)).toBeGreaterThanOrEqual(4.5)
    })

    it('Temas de Contraste: as barras da onda (só fundo) continuam visíveis', () => {
      const forcado = bloco('@media (forced-colors: active)')
      expect(forcado).toMatch(/\.onda-barra\s*\{[^}]*forced-color-adjust:\s*none[^}]*background:\s*CanvasText/)
    })

    it('o ditado que chegou tem o fundo do que entrou', () => {
      expect(valor(regra('.item.chegou > .linha'), 'background')).toBe('var(--entrou)')
    })

    it('a falha que guarda o áudio é a nota de erro de sempre (o vermelho não ganha regra nova)', () => {
      const usam = regras.filter((r) => /var\(--risco/.test(r.corpo)).map((r) => r.seletor)
      expect(usam.some((s) => s.includes('ditar'))).toBe(false)
    })
  })

  it('linha da Lixeira: sem fundo de passar o ponteiro, e com o texto inteiro', () => {
    expect(css).not.toMatch(/\.linha\.estatica:hover/)
    expect(bloco('\n.linha.estatica .texto {')).toMatch(/line-clamp:\s*none/)
  })

  // A busca única (ADR-022, N6): o trecho que casa vai em <mark> sobre o fundo de
  // seleção (o contraste do texto sobre --selecao está na lista de cima), a aba
  // Notas por trás dela só mostra a janela da nota, e no toque cada resultado tem
  // 44 px.
  it('a busca única: o <mark> usa o fundo de seleção, a aba por trás só mostra a janela, e no toque o resultado tem 44 px', () => {
    expect(valor(bloco('\n.resultado mark {'), 'background')).toBe('var(--selecao)')
    expect(valor(bloco('\n.notas[data-oculta] > :not(.veu) {'), 'display')).toBe('none')
    const toque = Array.from(css.matchAll(/@media \(pointer: coarse\)/g)).map((m) => bloco(css.slice(m.index)))
    const corpo = toque.map((b) => b.match(/\.resultado,\s*\.ver-todas\s*\{([^}]*)\}/)?.[1]).find(Boolean)
    expect(corpo, 'falta .resultado, .ver-todas no toque').toBeDefined()
    expect(valor(corpo!, 'min-height')).toBe('44px')
  })

  // A confirmação do bloco Histórico (F10) fica dentro do ajuste, depois do texto
  // dele: sem a regra, a caixa com borda cola na última linha (gap 0). O
  // historicoNoPC.test.tsx confere que ela fica onde a regra a alcança.
  it('a confirmação depois do texto de um ajuste tem o respiro da linha de botões que ela substitui', () => {
    const r = regras.find((x) => x.seletor === '.ajuste > * + .confirmar')
    expect(r, 'falta a regra da confirmação depois do texto do ajuste').toBeDefined()
    expect(valor(r!.corpo, 'margin-top')).toBe(valor(bloco('\n.acoes {'), 'margin-top'))
  })
})
