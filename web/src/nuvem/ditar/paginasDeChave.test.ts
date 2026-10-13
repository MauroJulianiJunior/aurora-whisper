// Onde se cria a chave de cada provedor do ditado pelo celular. Os endereços são os
// que a documentação oficial de cada provedor indica (conferidos em 13-out-2026) e
// ficam presos aqui: se um mudar, o teste quebra e a mudança é uma decisão.

import { describe, expect, it } from 'vitest'
import { paginasDeChave } from '../../paginasDeChave'
import { type Provedor, nomeDoProvedor, todosOsProvedores } from './aviso'
import { paginasDeChaveDoApp } from './paginasDeChave'

describe('paginasDeChaveDoApp', () => {
  it('OpenAI, Groq e Anthropic: o link da página de chaves de cada um', () => {
    expect(paginasDeChaveDoApp.openai.url).toBe('https://platform.openai.com/api-keys')
    expect(paginasDeChaveDoApp.groq.url).toBe('https://console.groq.com/keys')
    expect(paginasDeChaveDoApp.anthropic.url).toBe('https://platform.claude.com/settings/keys')
  })

  it('tem todos os provedores do aviso, cada um com o nome que o aviso usa', () => {
    expect(Object.keys(paginasDeChaveDoApp).sort()).toEqual([...todosOsProvedores].sort())
    for (const p of todosOsProvedores as readonly Provedor[]) expect(paginasDeChaveDoApp[p].nome).toBe(nomeDoProvedor[p])
  })

  it('os dois provedores do PC são os mesmos objetos da página compartilhada: um endereço só', () => {
    expect(paginasDeChaveDoApp.openai).toBe(paginasDeChave.openai)
    expect(paginasDeChaveDoApp.anthropic).toBe(paginasDeChave.anthropic)
  })

  it('todos em https, sem parâmetro, sem credencial no endereço', () => {
    for (const p of Object.values(paginasDeChaveDoApp)) {
      const u = new URL(p.url)
      expect(u.protocol).toBe('https:')
      expect(u.search).toBe('')
      expect(u.hash).toBe('')
      expect(u.username + u.password).toBe('')
    }
  })
})
