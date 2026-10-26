// O corpo da nota (ADR-022, "A nota"): texto puro, uma linha por bloco, com um
// prefixo que diz o tipo.
//
//   # Título de seção     título
//   - item                marcador
//   1. item               lista numerada
//   - [ ] item            lista de marcar (- [x] quando marcado)
//   qualquer outra        parágrafo
//
// Este módulo é puro (sem React, sem rede): lê e escreve o corpo e traz as
// operações do editor por linhas (dividir no Enter, juntar no Backspace,
// colar texto de várias linhas, trocar o tipo), para a regra ficar testável
// sem tela. Cada Linha tem uma chave que não muda enquanto ela existe: o editor
// a usa para devolver o foco à linha certa depois de uma operação.

export type TipoDaLinha = 'paragrafo' | 'titulo' | 'marcador' | 'numerada' | 'tarefa'

export interface Linha {
  // chave: identidade da linha na tela. Não vai para o corpo.
  chave: number
  tipo: TipoDaLinha
  texto: string
  // feito: só vale na tarefa.
  feito: boolean
}

// Foco é onde o cursor fica depois de uma operação: a linha (pela chave) e a
// posição no texto dela.
export interface Foco {
  chave: number
  pos: number
}

export interface Edicao {
  linhas: Linha[]
  foco: Foco
}

let ultimaChave = 0

export function criarLinha(tipo: TipoDaLinha = 'paragrafo', texto = '', feito = false): Linha {
  ultimaChave += 1
  return { chave: ultimaChave, tipo, texto, feito: tipo === 'tarefa' && feito }
}

export const ehLista = (t: TipoDaLinha) => t === 'marcador' || t === 'numerada' || t === 'tarefa'

const reTarefa = /^- \[( |x|X)\](?: (.*))?$/
const reMarcador = /^- (.*)$/
const reNumerada = /^\d+\. (.*)$/
const reTitulo = /^# (.*)$/
// O que se digita no começo de um parágrafo e vira o tipo: "- ", "1. ", "# ",
// "[] " (e "[ ] ", "[x] ") para a lista de marcar.
const reAtalhoDeTarefa = /^\[( |x|X)?\] (.*)$/

function lerLinha(bruta: string, atalhos: boolean): Linha {
  let m = reTarefa.exec(bruta)
  if (m) return criarLinha('tarefa', m[2] ?? '', m[1] !== ' ')
  if (atalhos && (m = reAtalhoDeTarefa.exec(bruta))) return criarLinha('tarefa', m[2] ?? '', m[1] === 'x' || m[1] === 'X')
  if ((m = reMarcador.exec(bruta))) return criarLinha('marcador', m[1] ?? '')
  if ((m = reNumerada.exec(bruta))) return criarLinha('numerada', m[1] ?? '')
  if ((m = reTitulo.exec(bruta))) return criarLinha('titulo', m[1] ?? '')
  return criarLinha('paragrafo', bruta)
}

const quebras = /\r\n?|\n/

// lerCorpo separa o corpo em linhas (\n, \r\n ou \r) e lê o prefixo de cada
// uma. Corpo vazio não tem linha nenhuma.
export function lerCorpo(corpo: string): Linha[] {
  if (corpo === '') return []
  return corpo.split(quebras).map((l) => lerLinha(l, false))
}

interface OpcoesDeEscrita {
  // limpar tira o que não diz nada: item de lista vazio e título vazio, e os
  // parágrafos vazios do fim. É o corpo que vai para o Agent; o editor guarda
  // as linhas vazias que a pessoa acabou de criar (o Enter de uma lista nova),
  // mas elas não viram texto gravado.
  limpar?: boolean
}

// escreverCorpo é o inverso de lerCorpo. A numeração é refeita: cada sequência
// de linhas numeradas conta de 1.
export function escreverCorpo(linhas: Linha[], { limpar = false }: OpcoesDeEscrita = {}): string {
  let fonte = linhas
  if (limpar) {
    fonte = linhas.filter((l) => l.tipo === 'paragrafo' || l.texto !== '')
    while (fonte.length > 0 && fonte[fonte.length - 1]!.tipo === 'paragrafo' && fonte[fonte.length - 1]!.texto === '') fonte = fonte.slice(0, -1)
  }
  let n = 0
  return fonte
    .map((l) => {
      n = l.tipo === 'numerada' ? n + 1 : 0
      switch (l.tipo) {
        case 'titulo':
          return `# ${l.texto}`
        case 'marcador':
          return `- ${l.texto}`
        case 'numerada':
          return `${n}. ${l.texto}`
        case 'tarefa':
          return `- [${l.feito ? 'x' : ' '}] ${l.texto}`
        default:
          return l.texto
      }
    })
    .join('\n')
}

// numeroDaLinha é a posição da linha dentro da sequência de numeradas que a
// contém (1, 2, 3...), ou 0 se ela não é numerada.
export function numeroDaLinha(linhas: Linha[], i: number): number {
  if (linhas[i]?.tipo !== 'numerada') return 0
  let n = 1
  for (let j = i - 1; j >= 0 && linhas[j]!.tipo === 'numerada'; j--) n++
  return n
}

// ─── A lista de marcar ────────────────────────────────────────────────

const estaMarcada = (l: Linha) => l.tipo === 'tarefa' && l.feito

// normalizarMarcados leva os itens marcados para o fim, na ordem em que
// estavam: a ordem gravada no corpo é a da tela (ADR-022, "Lista de marcar").
export function normalizarMarcados(linhas: Linha[]): Linha[] {
  return [...linhas.filter((l) => !estaMarcada(l)), ...linhas.filter(estaMarcada)]
}

// marcarLinha marca ou desmarca a tarefa. A que acaba de ser marcada vai para o
// fim de todas (os marcados ficam na ordem em que foram marcados); a
// desmarcada volta para o fim dos abertos.
export function marcarLinha(linhas: Linha[], chave: number, feito: boolean): Linha[] {
  const alvo = linhas.find((l) => l.chave === chave)
  if (!alvo || alvo.tipo !== 'tarefa' || alvo.feito === feito) return linhas
  const mudada = { ...alvo, feito }
  if (!feito) return normalizarMarcados(linhas.map((l) => (l.chave === chave ? mudada : l)))
  return normalizarMarcados([...linhas.filter((l) => l.chave !== chave), mudada])
}

export function desmarcarTudo(linhas: Linha[]): Linha[] {
  return linhas.map((l) => (estaMarcada(l) ? { ...l, feito: false } : l))
}

export function apagarMarcados(linhas: Linha[]): Linha[] {
  return linhas.filter((l) => !estaMarcada(l))
}

export interface ContagemDeItens {
  feitos: number
  total: number
}

export function contarItens(linhas: Linha[]): ContagemDeItens {
  const tarefas = linhas.filter((l) => l.tipo === 'tarefa')
  return { feitos: tarefas.filter((l) => l.feito).length, total: tarefas.length }
}

// ─── Resumos para o cartão, a lista e os detalhes ─────────────────────

// inicioDoCorpo é o começo do corpo em uma linha só: o texto de cada linha
// (os itens marcados ficam de fora), separados por " · ".
export function inicioDoCorpo(linhas: Linha[], max = 160): string {
  const partes = linhas.filter((l) => l.texto.trim() !== '' && !estaMarcada(l)).map((l) => l.texto.trim())
  const junto = partes.join(' · ')
  return junto.length > max ? `${junto.slice(0, max - 1).trimEnd()}…` : junto
}

// nomeDaNota é como a nota se chama para quem não a vê: o título, ou a
// primeira linha do corpo, ou "Nota vazia".
export function nomeDaNota(titulo: string, linhas: Linha[]): string {
  const t = titulo.trim()
  if (t) return t
  const primeira = linhas.find((l) => l.texto.trim() !== '')?.texto.trim()
  if (!primeira) return 'Nota vazia'
  return primeira.length > 60 ? `${primeira.slice(0, 59).trimEnd()}…` : primeira
}

// ─── O editor por linhas ──────────────────────────────────────────────

// dividir é o Enter: o texto depois do cursor desce para uma linha nova do
// mesmo tipo (a tarefa nova nasce desmarcada; depois de um título, vem um
// parágrafo). Enter numa linha de lista vazia sai da lista: ela vira parágrafo,
// como no Keep.
export function dividir(linhas: Linha[], i: number, pos: number): Edicao {
  const a = linhas[i]!
  if (a.texto === '' && a.tipo !== 'paragrafo') {
    const virou: Linha = { ...a, tipo: 'paragrafo', feito: false }
    return { linhas: linhas.map((l, k) => (k === i ? virou : l)), foco: { chave: a.chave, pos: 0 } }
  }
  const antes = a.texto.slice(0, pos)
  const nova = criarLinha(ehLista(a.tipo) ? a.tipo : 'paragrafo', a.texto.slice(pos))
  const todas = [...linhas.slice(0, i), { ...a, texto: antes }, nova, ...linhas.slice(i + 1)]
  return { linhas: normalizarMarcados(todas), foco: { chave: nova.chave, pos: 0 } }
}

// voltar é o Backspace com o cursor no começo da linha. Uma linha com tipo
// perde o tipo (e fica o texto); um parágrafo se junta ao anterior. null: não
// há o que fazer (primeira linha, já parágrafo).
export function voltar(linhas: Linha[], i: number): Edicao | null {
  const a = linhas[i]!
  if (a.tipo !== 'paragrafo') {
    const virou: Linha = { ...a, tipo: 'paragrafo', feito: false }
    return { linhas: normalizarMarcados(linhas.map((l, k) => (k === i ? virou : l))), foco: { chave: a.chave, pos: 0 } }
  }
  if (i === 0) return null
  const anterior = linhas[i - 1]!
  const junto: Linha = { ...anterior, texto: anterior.texto + a.texto }
  return {
    linhas: [...linhas.slice(0, i - 1), junto, ...linhas.slice(i + 1)],
    foco: { chave: anterior.chave, pos: anterior.texto.length },
  }
}

// puxar é o Delete com o cursor no fim da linha: o texto da linha de baixo
// sobe para esta. null na última linha.
export function puxar(linhas: Linha[], i: number): Edicao | null {
  if (i >= linhas.length - 1) return null
  const a = linhas[i]!
  const proxima = linhas[i + 1]!
  const junto: Linha = { ...a, texto: a.texto + proxima.texto }
  return { linhas: [...linhas.slice(0, i), junto, ...linhas.slice(i + 2)], foco: { chave: a.chave, pos: a.texto.length } }
}

// colar insere um texto, de uma ou de várias linhas, no lugar da seleção
// [ini, fim) da linha i. Cada linha colada lê o seu prefixo ("- ", "1. ",
// "- [ ] ", "# "): é assim que o ditado do Agent, que insere o texto do Polish
// por colagem ou por teclas, vira uma lista de verdade. A linha colada sem
// prefixo, dentro de uma lista, continua a lista; a vazia fica parágrafo. O
// que estava depois do cursor vai para o fim da última linha colada, e o
// cursor fica entre as duas partes.
export function colar(linhas: Linha[], i: number, ini: number, fim: number, texto: string): Edicao {
  const a = linhas[i]!
  const partes = texto.split(quebras)
  const antes = a.texto.slice(0, ini)
  const depois = a.texto.slice(fim)
  const tipoDaContinuacao: TipoDaLinha = ehLista(a.tipo) ? a.tipo : 'paragrafo'

  const novas: Linha[] = partes.map((parte, k) => {
    const lida = lerLinha(parte, false)
    if (k === 0) {
      // A primeira parte só vira outro tipo se estiver no começo da linha.
      if (antes === '' && lida.tipo !== 'paragrafo') return { ...a, tipo: lida.tipo, texto: lida.texto, feito: lida.feito }
      return { ...a, texto: antes + parte }
    }
    if (lida.tipo === 'paragrafo' && parte.trim() !== '') return criarLinha(tipoDaContinuacao, parte)
    return lida
  })
  const ultima = novas[novas.length - 1]!
  const pos = ultima.texto.length
  novas[novas.length - 1] = { ...ultima, texto: ultima.texto + depois }
  return { linhas: normalizarMarcados([...linhas.slice(0, i), ...novas, ...linhas.slice(i + 1)]), foco: { chave: ultima.chave, pos } }
}

// prefixoDigitado diz se o texto que a pessoa acabou de digitar numa linha de
// parágrafo começa com um prefixo ("- ", "1. ", "# ", "[] "): o tipo que a
// linha passa a ter e o texto que sobra. null: não é caso. Só vale para o que
// foi acrescentado (o texto cresceu): apagar até sobrar "- " não converte.
export function prefixoDigitado(tipoAtual: TipoDaLinha, antigo: string, novo: string): { tipo: TipoDaLinha; texto: string; feito: boolean } | null {
  if (tipoAtual !== 'paragrafo' || novo.length <= antigo.length) return null
  const lida = lerLinha(novo, true)
  return lida.tipo === 'paragrafo' ? null : { tipo: lida.tipo, texto: lida.texto, feito: lida.feito }
}

// converter troca o tipo das linhas dadas (pela chave). Só o prefixo muda: o
// texto fica. Uma tarefa que deixa de ser tarefa perde a marca; quem vira
// tarefa nasce desmarcada.
export function converter(linhas: Linha[], chaves: ReadonlySet<number>, tipo: TipoDaLinha): Linha[] {
  return normalizarMarcados(linhas.map((l) => (chaves.has(l.chave) ? { ...l, tipo, feito: false } : l)))
}

// inicialDoItem põe a primeira letra do item em maiúscula. Não mexe quando a
// primeira palavra é sigla (tem maiúscula depois da primeira letra: "eSocial",
// "iPhone"), para não virar "ESocial". É a mesma regra do Agent
// (inicialDoItem em internal/resolver/notas_lista.go).
function inicialDoItem(p: string): string {
  // A primeira palavra, sem a pontuação das bordas (como no Agent: letra ou dígito).
  const palavra = (p.split(/\s+/)[0] ?? '').replace(/^[^\p{L}\p{Nd}]+|[^\p{L}\p{Nd}]+$/gu, '')
  if (/\p{Lu}/u.test([...palavra].slice(1).join(''))) return p
  return p.charAt(0).toLocaleUpperCase('pt-BR') + p.slice(1)
}

// itensDaEnumeracao lê uma enumeração ditada ("Arroz, feijão, batata e
// queijo.") e devolve os itens, sem a vírgula, o "e" final e o ponto, cada um
// com a primeira letra maiúscula (menos a sigla: inicialDoItem). É a conversão
// por regra da ADR-022, a mesma do Agent (ItensPorRegra; os dois rodam os casos
// de testdata/notas-lista/regra.json)
// ("Formatar: converter trecho"): quebra na vírgula (nunca dentro de número,
// "1,5 kg"), no ponto e vírgula e no último " e ". null quando o texto não é
// uma enumeração: sem vírgula, com menos de `minimo` itens, com item de mais
// de cinco palavras ou que começa por conjunção ("mas a padaria estava
// fechada": uma frase com vírgulas não vira lista), ou com pergunta e
// dois-pontos (que a pessoa usou para outra coisa).
const conjuncao = /^(mas|porém|porque|pois|que|quando|se|então|depois|embora|enquanto)\b/i

export function itensDaEnumeracao(texto: string, minimo: number): string[] | null {
  const t = texto.trim().replace(/[.!]+$/, '')
  if (!/[,;]/.test(t) || /[?:]/.test(t)) return null
  const partes = t.split(/(?<!\d),|,(?!\d)|;/).map((p) => p.trim())
  const ultima = /^(.*\S)\s+e\s+(\S.*)$/.exec(partes[partes.length - 1] ?? '')
  if (ultima) partes.splice(partes.length - 1, 1, ultima[1]!, ultima[2]!)
  if (partes.length < minimo || partes.some((p) => p === '' || p.split(/\s+/).length > 5 || conjuncao.test(p))) return null
  return partes.map(inicialDoItem)
}

// separarEmItens troca a linha da chave pelos itens da enumeração dela, no
// tipo dado (ou no da própria linha, se ela já é lista). O primeiro item fica
// com a chave da linha; o foco vai para o fim do último. null: a linha não é
// uma enumeração (itensDaEnumeracao).
export function separarEmItens(linhas: Linha[], chave: number, minimo: number, tipo?: TipoDaLinha): Edicao | null {
  const a = linhas.find((l) => l.chave === chave)
  if (!a) return null
  const itens = itensDaEnumeracao(a.texto, minimo)
  if (!itens) return null
  return trocarPorItens(linhas, chave, itens, tipo)
}

// trocarPorItens troca a linha da chave pelos itens dados, no tipo dado (ou no
// da própria linha, se ela já é lista; senão, lista de marcar). É o fim das duas
// conversões: a da regra (separarEmItens) e a do Agent, com IA (N5). null: a
// linha não existe, ou não há itens.
export function trocarPorItens(linhas: Linha[], chave: number, itens: readonly string[], tipo?: TipoDaLinha): Edicao | null {
  const i = linhas.findIndex((l) => l.chave === chave)
  const a = linhas[i]
  if (!a || itens.length === 0) return null
  const t: TipoDaLinha = tipo ?? (ehLista(a.tipo) ? a.tipo : 'tarefa')
  const novas = itens.map((texto, k) => (k === 0 ? { ...a, tipo: t, texto, feito: false } : criarLinha(t, texto)))
  const ultima = novas[novas.length - 1]!
  return {
    linhas: normalizarMarcados([...linhas.slice(0, i), ...novas, ...linhas.slice(i + 1)]),
    foco: { chave: ultima.chave, pos: ultima.texto.length },
  }
}

// garantirUma devolve as linhas, ou uma linha de parágrafo vazia se não há
// nenhuma: o editor sempre tem onde digitar.
export function garantirUma(linhas: Linha[]): Linha[] {
  return linhas.length > 0 ? linhas : [criarLinha()]
}
