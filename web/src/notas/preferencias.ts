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
