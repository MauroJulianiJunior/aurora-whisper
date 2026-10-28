// As frases do bloco "Histórico" das Configurações do PC que dependem de uma
// contagem (08-ui-ux-spec.md, "Histórico (os 6 meses no PC)"; ADR-019, F10d e
// F10e), com o singular e o ponto do milhar. Os textos são proposta, a
// confirmar pelo Mauro na entrega.

import { numero } from './textosDaNuvem'

const ditados = (n: number) => `${numero(n)} ${n === 1 ? 'ditado' : 'ditados'}`

// perguntaDosSeisMeses é a pergunta da D1 (a regra desligada, com ditados de
// mais de 6 meses): o que acontece ao confirmar, e a cópia. A cópia dos 6 meses
// é uma só (ADR-019, F10, escolha menor 5): com a de um OK anterior ao lado do
// banco (copiaDeAntes), o OK não copia de novo, e o que nasceu depois dela não
// está nela; a frase diz isso, em vez de prometer uma cópia com tudo o que sai.
export function perguntaDosSeisMeses(n: number, copiaDeAntes = false): string {
  const copia = copiaDeAntes
    ? 'Não se faz outra cópia: a que já está neste PC é de antes, e o que nasceu depois dela não está nela.'
    : 'Antes, uma cópia do histórico fica neste PC.'
  return n === 1
    ? `1 ditado tem mais de 6 meses. Ao confirmar, ele sai deste PC de vez, e daqui em diante o que fizer 6 meses sai sozinho. ${copia}`
    : `${numero(n)} ditados têm mais de 6 meses. Ao confirmar, eles saem deste PC de vez, e daqui em diante o que fizer 6 meses sai sozinho. ${copia}`
}

// As linhas discretas embaixo da pergunta: só aparecem quando maiores que 0. n é
// o total da pergunta: com um ditado só, "1 deles" não faz sentido, e a linha
// fala dele.
export const naLixeira = (m: number, n: number) =>
  n === 1 ? 'Ele está na lixeira.' : m === 1 ? '1 deles está na lixeira.' : `${numero(m)} deles estão na lixeira.`
export const comDuasVersoes = (k: number, n: number) =>
  n === 1 ? 'Ele tem duas versões, e as duas saem.' : k === 1 ? '1 tem duas versões, e as duas saem.' : `${numero(k)} têm duas versões, e as duas saem.`
export const deOutroDono = (j: number) => (j === 1 ? '1 é de outro dono e fica.' : `${numero(j)} são de outro dono e ficam.`)
// viraramNota: os que viraram nota não aparecem no histórico, e a pergunta os
// conta (ADR-022; o dossiê de LGPD, decisão 11): a linha diz que a nota fica.
export const viraramNota = (v: number, n: number) =>
  n === 1
    ? 'Ele virou nota: sai o ditado, e a nota fica.'
    : v === 1
      ? '1 deles virou nota: sai o ditado, e a nota fica.'
      : `${numero(v)} deles viraram nota: saem os ditados, e as notas ficam.`

// regraPausada vem antes da pergunta (ou sozinha, sem nada a apagar: aí o bloco
// só aparece pelas cópias, e a frase diz quando a pergunta volta).
export const regraPausada = (previa: number) =>
  previa > 0
    ? 'A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o relógio do PC mudou. Nada foi apagado.'
    : 'A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o relógio do PC mudou. Nada foi apagado. A pergunta volta quando houver o que apagar.'

// A pergunta na própria linha (o padrão do "Esquecer esta nuvem"): o título em
// negrito e o resto da frase. A cópia fica na pergunta, logo acima: aqui, "só a
// cópia guarda" seria falso para o que nasceu depois da cópia de um OK anterior.
export const tituloDeApagar = (n: number) => `Apagar ${ditados(n)}?`
export const restoDeApagar = 'Não dá para desfazer.'

// Pronto: quantos saíram. A cópia não entra aqui: ela já está na linha das
// cópias do bloco (e, com a de um OK anterior, nem é nova).
export const apagaram = (n: number) => (n === 1 ? '1 ditado saiu deste PC.' : `${numero(n)} ditados saíram deste PC.`)

// apagaramParte: o OK ligou a regra e apagou n, mas um lote seguinte caiu
// (incompleto): os resto que faltam saem sozinhos na próxima rodada.
export function apagaramParte(n: number, resto: number): string {
  const saiu = n === 0 ? 'Os 6 meses estão ligados.' : n === 1 ? '1 ditado saiu deste PC.' : `${numero(n)} ditados saíram deste PC.`
  if (resto === 0) return saiu
  return `${saiu} ${resto === 1 ? 'O outro sai' : `Os outros ${numero(resto)} saem`} na próxima rodada, em até 1 hora.`
}

// ligouSemTerminar: a resposta do OK falhou, mas a releitura mostra a regra
// ligada (o OK foi gravado antes da falha, e parte pode ter saído).
export const ligouSemTerminar = 'Os 6 meses estão ligados. O que ainda não saiu sai na próxima rodada, em até 1 hora.'

export const contagemMudou = 'A contagem mudou.'

export const ligadaTexto = 'Os 6 meses estão ligados: o que fizer 6 meses sai sozinho deste PC, em até 1 hora.'
export const desligarAjuda = 'Desligar volta ao de antes: o PC guarda tudo. O que já saiu não volta.'
export const desligouAviso = 'Os 6 meses foram desligados. O PC guarda tudo de novo.'

// As cópias ao lado do banco (F10e).
export const haCopias = (c: number) =>
  c === 1 ? 'Há 1 cópia do histórico ao lado do banco, com o texto de antes.' : `Há ${numero(c)} cópias do histórico ao lado do banco, com o texto de antes.`

// copiaDoEnvioFica: das c cópias, f são do envio do histórico à nuvem que ainda
// não terminou, e o botão não as apaga.
export function copiaDoEnvioFica(c: number, f: number): string {
  if (f >= c) return c === 1 ? 'Ela é a do envio do histórico à nuvem, que fica até o envio terminar.' : 'Todas são do envio do histórico à nuvem e ficam até o envio terminar.'
  return f === 1 ? '1 delas é a do envio do histórico à nuvem, que fica até o envio terminar.' : `${numero(f)} delas são do envio do histórico à nuvem e ficam até o envio terminar.`
}

// tituloDeApagarCopias e restoDeApagarCopias contam só as que o botão apaga.
export const tituloDeApagarCopias = (c: number) => (c === 1 ? 'Apagar a cópia?' : `Apagar as ${numero(c)} cópias?`)
export const restoDeApagarCopias = (c: number) =>
  `${c === 1 ? 'Ela guarda' : 'Elas guardam'} o texto que os 6 meses apagam. Não dá para desfazer.`

// copiasApagadas é o aviso depois do botão: as que saíram e, se houver, as que
// ficaram (a do envio do histórico à nuvem que ainda não terminou).
export function copiasApagadas(apagadas: number, ficaram: number): string {
  const saiu =
    apagadas === 0
      ? ficaram === 0
        ? 'Não havia cópias para apagar.'
        : 'Nenhuma cópia saiu.'
      : apagadas === 1
        ? '1 cópia apagada.'
        : `${numero(apagadas)} cópias apagadas.`
  if (ficaram === 0) return saiu
  return `${saiu} ${ficaram === 1 ? '1 fica' : `${numero(ficaram)} ficam`}: o envio do histórico à nuvem ainda não terminou.`
}

// O Agent de antes da F10 não tem as rotas (404, sem frase).
export const agentSemSeisMeses = 'Este Agent ainda não tem os 6 meses. Reinicie o Agent.'
