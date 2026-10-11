// Preparação dos testes do web (vite.config.ts, test.setupFiles): os findBy e os
// waitFor esperam até 5 s, e não 1 s. Com a máquina ocupada (os outros testes
// do repositório, e os de outros agentes, rodam junto), a primeira
// renderização de um arquivo, que transforma e carrega o app inteiro, passava
// do 1 s padrão, e a suíte falhava por lentidão, e não por defeito. Um teste
// que espera uma coisa que não vem continua falhando, só 4 s depois.

import { configure } from '@testing-library/react'

configure({ asyncUtilTimeout: 5000 })
