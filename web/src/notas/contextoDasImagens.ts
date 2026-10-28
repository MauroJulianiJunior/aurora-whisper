import { createContext, useContext, useSyncExternalStore } from 'react'
import type { ImagensNaTela } from './imagensNaTela'

// A loja das imagens das notas (ImagensNaTela, ADR-022 N10) chega aos cartões, à
// nota aberta e à imagem grande por este contexto. Sem ela (o app do celular,
// nesta fase), nenhuma imagem aparece.
export const ContextoDasImagens = createContext<ImagensNaTela | null>(null)

const semAssinar = () => () => {}
const zero = () => 0

// useImagensNaTela devolve a loja e redesenha quem a usa quando ela muda.
export function useImagensNaTela(): ImagensNaTela | null {
  const loja = useContext(ContextoDasImagens)
  useSyncExternalStore(loja?.assinar ?? semAssinar, loja?.versao ?? zero)
  return loja
}
