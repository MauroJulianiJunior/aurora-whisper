// O Painel do PC com a busca do topo só no histórico: sem ambiente.buscaUnica
// (ADR-022, N6), é a busca de antes dela, a que o painel da nuvem e a Lixeira
// ainda têm. Os testes da lista (a mensagem de vazio de cada consulta, o
// "Mostrar mais antigos" com busca, a região viva) a usam para provar a lista
// sem passar pelos resultados dos dois grupos.

import { render } from '@testing-library/react'
import { ambienteLocal } from '../ambienteLocal'
import type { Buscador } from '../api'
import { criarCliente } from '../clienteLocal'
import { Painel } from '../Painel'

export function renderizarSoHistorico(buscar: Buscador) {
  const ambiente = { ...ambienteLocal(criarCliente('tok', buscar)), buscaUnica: undefined }
  return render(<Painel ambiente={ambiente} />)
}
