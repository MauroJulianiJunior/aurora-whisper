import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '../fontes.css'
import '../index.css'
import AppNuvem from './AppNuvem'
import { registrarSW } from './registrarSW'

// A entrada do build da nuvem (vite build --mode nuvem; ADR-019, decisão 12).
// O vite.config.ts troca o script do index.html por este arquivo nesse modo:
// o build local nem enxerga a pasta nuvem.
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppNuvem />
  </StrictMode>,
)
registrarSW()
