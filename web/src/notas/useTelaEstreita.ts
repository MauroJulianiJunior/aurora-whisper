import { useSyncExternalStore } from 'react'

// A tela estreita (o celular): até 640 px. A grade tem menos colunas nela.
const consulta = '(max-width: 640px)'

function assinar(aviso: () => void) {
  if (typeof window.matchMedia !== 'function') return () => undefined
  const m = window.matchMedia(consulta)
  m.addEventListener('change', aviso)
  return () => m.removeEventListener('change', aviso)
}

// Sem matchMedia (o jsdom dos testes), a tela é larga.
const ler = () => typeof window.matchMedia === 'function' && window.matchMedia(consulta).matches

// useTelaEstreita diz se a janela é estreita agora, e acompanha quando muda.
export function useTelaEstreita(): boolean {
  return useSyncExternalStore(assinar, ler, () => false)
}
