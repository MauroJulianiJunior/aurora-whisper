// A entrada da página de calibração do microfone (ADR-021, C6c). SÓ do `pnpm dev`:
// nenhum index.html a importa, e o bundle.test.ts prova que ela não está em
// nenhum dos dois builds.
//
//   pnpm -C web dev      e abrir      /calibracao-dev.html
//
// No Chrome do Android, pelo cabo USB (a porta encaminhada, como em
// docs/teste-manual-nuvem-f4.md, §9, mas a do Vite, 5173, no lugar da 8080): a
// página fica em http://localhost, onde o navegador deixa abrir o microfone. O
// roteiro está na própria página.

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '../../fontes.css'
import '../../index.css'
import { PaginaDeCalibracao } from './PaginaDeCalibracao'
import './calibracao.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PaginaDeCalibracao />
  </StrictMode>,
)
