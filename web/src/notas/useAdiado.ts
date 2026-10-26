import { useEffect, useState } from 'react'

// useAdiado devolve o valor só depois de `espera` ms sem mudar. O primeiro vale
// na hora: quem abre a nota não espera para ver a sugestão.
export function useAdiado<T>(valor: T, espera: number): T {
  const [adiado, setAdiado] = useState(valor)
  useEffect(() => {
    const t = setTimeout(() => setAdiado(valor), espera)
    return () => clearTimeout(t)
  }, [valor, espera])
  return adiado
}
