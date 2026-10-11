// Datas do histórico, no fuso e no idioma de quem usa.

import type { Ditado } from './api'

const hora = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit' })
const diaDaSemana = new Intl.DateTimeFormat('pt-BR', { weekday: 'long', day: 'numeric', month: 'long' })
const diaComAno = new Intl.DateTimeFormat('pt-BR', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
const completa = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'full', timeStyle: 'short' })
const curta = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
const soData = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: '2-digit', year: 'numeric' })

function chaveDoDia(d: Date): string {
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

function maiuscula(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function rotuloDoDia(d: Date, hoje: Date): string {
  if (chaveDoDia(d) === chaveDoDia(hoje)) return 'Hoje'
  const ontem = new Date(hoje)
  ontem.setDate(hoje.getDate() - 1)
  if (chaveDoDia(d) === chaveDoDia(ontem)) return 'Ontem'
  const formato = d.getFullYear() === hoje.getFullYear() ? diaDaSemana : diaComAno
  return maiuscula(formato.format(d))
}

export interface Grupo {
  chave: string
  rotulo: string
  itens: Ditado[]
}

// agruparPorDia mantém a ordem que a API mandou (mais novo primeiro) e
// só abre um grupo novo quando o dia muda.
export function agruparPorDia(itens: Ditado[], hoje: Date, campo: 'created_at' | 'deleted_at' = 'created_at'): Grupo[] {
  const grupos: Grupo[] = []
  for (const item of itens) {
    const d = new Date(item[campo] ?? item.created_at)
    const chave = chaveDoDia(d)
    const ultimo = grupos[grupos.length - 1]
    if (ultimo && ultimo.chave === chave) ultimo.itens.push(item)
    else grupos.push({ chave, rotulo: rotuloDoDia(d, hoje), itens: [item] })
  }
  return grupos
}

export const formatarHora = (iso: string) => hora.format(new Date(iso))
export const formatarCompleta = (iso: string) => maiuscula(completa.format(new Date(iso)))
export const formatarCurta = (iso: string) => curta.format(new Date(iso))
// formatarData: só o dia, com o ano ("10/10/2026").
export const formatarData = (iso: string) => soData.format(new Date(iso))

export function formatarSegundos(ms: number): string {
  return `${(ms / 1000).toLocaleString('pt-BR', { maximumFractionDigits: 1 })} s`
}
