import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './fontes.css'
import './index.css'
import App from './App.tsx'
import { iniciarSessao } from './sessao.ts'

// A troca do bilhete começa aqui, uma vez por carga de página — ver
// sessao.ts. O catch vazio só marca a promessa como tratada até o App
// ouvir o resultado; o App recebe a falha do mesmo jeito.
const sessao = iniciarSessao()
sessao.catch(() => {})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App sessao={sessao} />
  </StrictMode>,
)
