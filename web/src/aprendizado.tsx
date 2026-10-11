// O texto do aprendizado pelo painel (ADR-017): o que o aviso diz ao
// sugerir, ao confirmar e ao recusar, e a frase do "aparece em N
// ditados". Só apresentação e contas de texto: quem chama a API é o Painel.
//
// Os termos vão na IBM Plex Mono (a paleta do projeto: a mono é dos dados
// técnicos), e entre aspas tipográficas, como o resto do painel. Nada
// aqui tem cor própria: o verde do "Aprendido" é da classe, e só cobre o
// que foi confirmado.

import type { ReactNode } from 'react'
import type { Aprendida, Sugestao } from './api'
import { Termo } from './Termo'

// Um termo entre aspas tipográficas: “SF”.
const citado = (t: string) => (
  <>
    “<Termo>{t}</Termo>”
  </>
)

// lista junta com vírgula e "e" no fim: A, B e C.
function lista(partes: ReactNode[]): ReactNode {
  return partes.map((p, i) => (
    <span key={i}>
      {i > 0 && (i === partes.length - 1 ? ' e ' : ', ')}
      {p}
    </span>
  ))
}

const pluralDeDitados = (n: number) => `${n} ${n === 1 ? 'ditado' : 'ditados'}`

// ocorrencias diz, discretamente, quantos ditados do histórico têm o
// "de": é o que deixa a pessoa medir o alcance da regra antes de aprendê-la.
// Só entra quem aparece em algum (0 é "em nenhum" ou "não deu para contar").
// Sem nenhum, "" — e o aviso não fala disso.
export function ocorrencias(sugestoes: Sugestao[]): ReactNode {
  const com = sugestoes.filter((s) => s.ocorrencias > 0)
  if (com.length === 0) return ''
  if (com.length === 1) {
    const s = com[0]!
    return (
      <>
        {citado(s.de)} aparece em {pluralDeDitados(s.ocorrencias)}.
      </>
    )
  }
  // Com duas, "“SF” aparece em 3 ditados, “RJ” em 1." — o verbo uma vez só.
  return (
    <>
      {citado(com[0]!.de)} aparece em {pluralDeDitados(com[0]!.ocorrencias)}, {citado(com[1]!.de)} em {com[1]!.ocorrencias}.
    </>
  )
}

// A troca como a pílula a mostra: “sf” → “ECF”. A seta é só para os olhos
// (aria-hidden); o leitor de tela ouve “sf vira ECF”, como na lista das
// Configurações (AprendidoNoPainel), e não "seta para a direita".
const troca = (de: string, para: string) => (
  <>
    {citado(de)} <span aria-hidden="true">→</span>
    <span className="so-leitor"> vira</span> {citado(para)}
  </>
)

// A pergunta que a pílula faz depois de "Texto salvo.".
export function perguntaDeSugestao(sugestoes: Sugestao[]): ReactNode {
  const nota = ocorrencias(sugestoes)
  return (
    <>
      Texto salvo. Aprender {lista(sugestoes.map((s) => troca(s.de, s.para)))}?
      {nota && (
        <>
          {' '}
          <span className="aviso-nota">{nota}</span>
        </>
      )}
    </>
  )
}

// O que Aprender faz, para quem pergunta (title do botão).
export const efeitoDeAprender = 'A troca vale para todo ditado daqui para frente, em qualquer aplicativo. Dá para desfazer.'

// Quando vale o que foi gravado. O Agent troca o dicionário em memória na
// hora, e vale a partir do próximo ditado; se a gravação deu certo e a troca
// em memória não (vale_ao_reiniciar), a mudança é real e só falta reiniciar o
// Agent. Nesse caso a pílula diz que aprendeu (ou desfez) e o que falta: a
// lista das Configurações já mostra o par, e "Não deu para aprender" seria
// mentira.
//
// A ressalva é de cada troca, e não do clique: duas trocas do mesmo clique
// podem divergir (a troca em memória falha para uma e não para a outra), e
// dizer "só depois de reiniciar" das duas seria falso para a que já vale. Com
// as duas em grupos, cada um leva o seu fim; com um grupo só, a frase é a de
// sempre.
const aoReiniciar = ', mas só depois de reiniciar o Agent'

function porQuandoVale(l: Aprendida[], frase: (a: Aprendida) => ReactNode, fimDoQueVale: string): ReactNode {
  const grupos = [
    { trocas: l.filter((a) => !a.vale_ao_reiniciar), fim: fimDoQueVale },
    { trocas: l.filter((a) => a.vale_ao_reiniciar), fim: aoReiniciar },
  ].filter((g) => g.trocas.length > 0)
  return (
    <>
      {grupos.map((g, i) => (
        <span key={i}>
          {i > 0 && '; '}
          {lista(g.trocas.map(frase))}
          {g.fim}
        </span>
      ))}
      .
    </>
  )
}

// O que a pílula diz depois do clique. `feitas` é o que foi gravado;
// `recusas`, a frase que o Agent deu para cada troca que não foi.
//
// Nenhuma gravada: só o motivo. Algumas gravadas e outras não: as duas
// coisas, para a pessoa saber o que ficou valendo.
export function resultadoDeAprender(feitas: Aprendida[], recusas: string[]): ReactNode {
  const motivo = recusas.join(' ')
  if (feitas.length === 0) return `Não deu para aprender. ${motivo}`.trim()
  return (
    <>
      <span className="confirmado">
        Aprendido: {porQuandoVale(feitas, (a) => <>{citado(a.variante)} vira {citado(a.termo)}</>, ' no próximo ditado')}
      </span>
      {motivo && ` ${motivo}`}
    </>
  )
}

// As linhas de estado do "Aprendido no painel" (ADR-019, F8; 06, GET
// /api/aprendizado): a etiqueta de cada `situacao` que o Agent manda, e a
// frase curta de cada `motivo` do "não aplicado". Mapas, e não objetos: o que o
// Agent manda é só chave, e uma chave como "constructor" não pode achar nada.
// Uma situação que o painel não conhece não vira linha (o Agent é mais novo
// que a página); um motivo que ele não conhece fica sem a frase, nunca com o
// código cru.
const etiquetas = new Map([
  ['nao_aplicado', 'não aplicado neste PC'],
  ['desfeito_em_outro_aparelho', 'desfeito em outro aparelho'],
  ['trocado_em_outro_aparelho', 'trocado em outro aparelho'],
])

const motivos = new Map([
  ['dominio_ausente', 'O domínio da troca não existe aqui.'],
  ['restricao_nao_cabe', 'A troca valeria em mais lugares do que no PC onde foi aprendida.'],
  ['palavra_ausente_com_dominio', 'A palavra não existe aqui, e a troca é presa a um domínio.'],
  ['variante_de_outro_termo', 'Aqui essa grafia já pertence a outra palavra.'],
  ['termo_desligado', 'A palavra está desligada.'],
  ['nao_agiria', 'O dicionário não aplicaria a troca.'],
  ['palavra_comum', 'A grafia é comum demais para virar troca.'],
  ['arquivo_indisponivel', 'O dicionário está indisponível.'],
])

// LinhaDoAprendido é o que uma troca da lista vira na tela: a que vale aqui (a
// de sempre, com o Desfazer), a linha de estado (com a etiqueta e, no "não
// aplicado", a frase do motivo; nunca com o Desfazer) ou nada, quando a
// situação é uma que o painel não conhece.
export type LinhaDoAprendido = { tipo: 'vale' } | { tipo: 'estado'; etiqueta: string; motivo?: string } | { tipo: 'desconhecida' }

export function linhaDoAprendido(a: Aprendida): LinhaDoAprendido {
  if (!a.situacao) return { tipo: 'vale' }
  const etiqueta = etiquetas.get(a.situacao)
  if (etiqueta === undefined) return { tipo: 'desconhecida' }
  const motivo = a.situacao === 'nao_aplicado' && a.motivo ? motivos.get(a.motivo) : undefined
  return motivo === undefined ? { tipo: 'estado', etiqueta } : { tipo: 'estado', etiqueta, motivo }
}

// O que a pílula diz depois de desfazer (e na lista das Configurações).
// Mesma regra do resultado de aprender: o que saiu, e a frase do que não.
export function resultadoDeDesfazer(desfeitas: Aprendida[], recusas: string[] = []): ReactNode {
  const motivo = recusas.join(' ')
  if (desfeitas.length === 0) return `Não deu para desfazer. ${motivo}`.trim()
  return (
    <>
      Desfeito: {porQuandoVale(desfeitas, (a) => <>{citado(a.variante)} não vira mais {citado(a.termo)}</>, '')}
      {motivo && ` ${motivo}`}
    </>
  )
}
