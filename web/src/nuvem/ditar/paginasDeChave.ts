// Onde se cria a chave de cada provedor do ditado pelo celular: os da página
// compartilhada (OpenAI e Anthropic) e a Groq, que só o app da nuvem conhece.
// Endereço conferido em 13-out-2026: a documentação da Groq manda criar a chave
// em /keys do console dela.

import { type PaginaDaChave, paginasDeChave } from '../../paginasDeChave'
import type { Provedor } from './aviso'

export const paginasDeChaveDoApp: Record<Provedor, PaginaDaChave> = {
  ...paginasDeChave,
  groq: { nome: 'Groq', url: 'https://console.groq.com/keys' },
}
