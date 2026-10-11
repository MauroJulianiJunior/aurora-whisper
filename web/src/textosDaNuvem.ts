// As frases da seção Nuvem do PC que dependem de uma contagem (08-ui-ux-spec.md,
// "Nuvem (no PC)"; ADR-019, F7c), com o singular e o ponto do milhar.

// numero escreve uma contagem como no resto do painel ("7.000").
export const numero = (n: number) => n.toLocaleString('pt-BR')

// As frases do envio (08-ui-ux-spec.md, "Nuvem (no PC)", F7c e F8h).
const ditados = (n: number) => `${numero(n)} ${n === 1 ? 'ditado' : 'ditados'} dos últimos 6 meses`
const trocas = (m: number) => `${numero(m)} ${m === 1 ? 'troca aprendida' : 'trocas aprendidas'}`

// vaoSubir é a pergunta do envio: n ditados e m trocas aprendidas (a D3 da F8).
// Sem troca, é a frase de sempre. Com as duas, "Vão subir 800 ditados dos
// últimos 6 meses e 3 trocas aprendidas."; só com troca (n = 0), "Vão subir 3
// trocas aprendidas." O verbo vai ao singular só quando é uma coisa só.
export function vaoSubir(n: number, m = 0): string {
  const partes = [n > 0 && ditados(n), m > 0 && trocas(m)].filter((p): p is string => p !== false)
  const verbo = n + m === 1 ? 'Vai' : 'Vão'
  return `${verbo} subir ${partes.join(' e ')}.`
}

// freioDoAprendido é o aviso do freio (ADR-019, F8, escolha menor 1): mais de
// 10 trocas saíram do dicionário de uma vez (à mão, ou um dicionario.json
// antigo posto no lugar), e o PC não manda nada do aprendido até elas
// voltarem. Sem o aviso, a pessoa não saberia que as trocas daqui pararam de
// subir.
export const freioDoAprendido = (n: number) =>
  n === 1
    ? '1 troca aprendida saiu do dicionário de uma vez. Por segurança, nada do aprendido sobe para a nuvem até ela voltar.'
    : `${numero(n)} trocas aprendidas saíram do dicionário de uma vez. Por segurança, nada do aprendido sobe para a nuvem até elas voltarem.`
export const naoSubiram = (n: number) =>
  n === 1 ? '1 ditado não subiu para a nuvem.' : `${numero(n)} ditados não subiram para a nuvem.`
// A pergunta do esquecer: as mudanças que não subiram ficam neste PC (de volta
// à mesma conta, a reconciliação as sobe; ADR-019, "Decisões da F7c").
export const naoSubiuFica = (n: number) =>
  n === 1
    ? '1 mudança feita aqui ainda não subiu. Ela fica neste PC.'
    : `${numero(n)} mudanças feitas aqui ainda não subiram. Elas ficam neste PC.`
