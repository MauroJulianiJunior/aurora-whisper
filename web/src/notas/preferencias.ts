// O que a aba Notas lembra do aparelho: a forma de ver (quadradinhos, lista ou
// detalhes) e quantas colunas a grade tem (ADR-022, "Decisões do Mauro sobre
// a prévia da tela"). Fica no localStorage, por aparelho, e só isso: é conforto,
// não dado. Todo acesso é num try/catch (janela privada, dados do site
// bloqueados, armazenamento cheio): sem ele, o painel mostra o padrão e
// funciona do mesmo jeito.

export type FormaDeVer = 'grade' | 'lista' | 'detalhes'
export const FORMAS: readonly FormaDeVer[] = ['grade', 'lista', 'detalhes']

// Os limites das colunas: 4 por padrão no PC (de 1 a 6); 2 no celular (de 1 a 3).
export interface LimitesDeColunas {
  min: number
  max: number
  padrao: number
}
export const COLUNAS_NO_PC: LimitesDeColunas = { min: 1, max: 6, padrao: 4 }
export const COLUNAS_NO_CELULAR: LimitesDeColunas = { min: 1, max: 3, padrao: 2 }

export const limitesDeColunas = (estreita: boolean) => (estreita ? COLUNAS_NO_CELULAR : COLUNAS_NO_PC)

const chaveDaForma = 'aurora.notas.forma'
const chaveDasColunas = (estreita: boolean) => `aurora.notas.colunas.${estreita ? 'celular' : 'pc'}`

function ler(chave: string): string | null {
  try {
    return window.localStorage.getItem(chave)
  } catch {
    return null
  }
}

function gravar(chave: string, valor: string): void {
  try {
    window.localStorage.setItem(chave, valor)
  } catch {
    // Sem armazenamento, a escolha vale só até a página fechar.
  }
}

export function lerForma(): FormaDeVer {
  const v = ler(chaveDaForma)
  return FORMAS.find((f) => f === v) ?? 'grade'
}

export function gravarForma(f: FormaDeVer): void {
  gravar(chaveDaForma, f)
}

// limitar leva o número para dentro dos limites (um valor velho, de uma tela
// maior, não passa do máximo desta).
export function limitar(n: number, { min, max }: LimitesDeColunas): number {
  return Math.min(max, Math.max(min, Math.round(n)))
}

export function lerColunas(estreita: boolean): number {
  const limites = limitesDeColunas(estreita)
  const bruto = ler(chaveDasColunas(estreita))
  const n = bruto !== null && /^\d{1,2}$/.test(bruto) ? Number(bruto) : NaN
  return Number.isFinite(n) ? limitar(n, limites) : limites.padrao
}

export function gravarColunas(estreita: boolean, n: number): void {
  gravar(chaveDasColunas(estreita), String(limitar(n, limitesDeColunas(estreita))))
}

// As notas em que a pessoa disse "Agora não" à sugestão de marcador (N4): a
// faixa não volta para elas. Guarda os ids, os mais recentes no fim, até
// maxDispensadas (a lista não cresce para sempre). Sem armazenamento, a faixa
// só deixa de aparecer enquanto a nota estiver aberta.
const chaveDasDispensadas = 'aurora.notas.sugestao.dispensadas'
const maxDispensadas = 300

function lerDispensadas(): string[] {
  try {
    const v: unknown = JSON.parse(ler(chaveDasDispensadas) ?? '[]')
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

export function sugestaoDispensada(notaId: string): boolean {
  return lerDispensadas().includes(notaId)
}

export function dispensarSugestao(notaId: string): void {
  const ids = lerDispensadas().filter((id) => id !== notaId)
  ids.push(notaId)
  gravar(chaveDasDispensadas, JSON.stringify(ids.slice(-maxDispensadas)))
}
