// O que cada falha do ditado vira na tela (ADR-021, "A tela"): a frase, e se
// o áudio fica na memória para "Tentar de novo". O app decide pelo error.code
// do servidor, nunca pela frase dele. Uma falha que o servidor não explicou
// (um proxy, a rede) cai pelo status.

import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'

export type TipoDeErro =
  // Antes de gravar: nada ficou na memória.
  | 'permissao'
  | 'sem-microfone'
  | 'microfone-ocupado'
  | 'sem-suporte'
  | 'sem-formato'
  | 'gravacao'
  // A fala medida no app, ou o provedor não devolveu texto.
  | 'sem-fala'
  // O servidor recusou e o reenvio não adianta: o áudio sai da memória.
  | 'longo'
  | 'apagado'
  | 'formato'
  | 'indisponivel'
  // O áudio fica na memória, com "Tentar de novo" (o mesmo id) e "Descartar".
  | 'sessao'
  | 'rede'
  | 'prazo'
  | 'espere'
  | 'sem-aceite'
  | 'sem-chave'
  | 'provedor-chave'
  | 'provedor-fora'
  | 'outro'

export interface ErroDoDitado {
  tipo: TipoDeErro
  frase: string
  // retem: o áudio fica na memória até "Tentar de novo" ou "Descartar".
  retem: boolean
  // vai: onde a frase se resolve. 'aceite' é a folha do aviso (SEM_ACEITE: o
  // aceite acabou, ou o aviso mudou); 'configuracoes', a seção do ditado nas
  // Configurações (a chave do provedor).
  vai?: 'configuracoes' | 'aceite'
}

// MicrofoneIndisponivel é o que a plataforma levanta quando o navegador não
// grava neste formato: o getUserMedia deu certo, mas o webm/opus não existe.
export class MicrofoneIndisponivel extends Error {
  motivo: 'sem-formato' | 'sem-suporte'
  constructor(motivo: 'sem-formato' | 'sem-suporte') {
    super(motivo)
    this.name = 'MicrofoneIndisponivel'
    this.motivo = motivo
  }
}

const frasesDoMicrofone: Record<string, ErroDoDitado> = {
  permissao: { tipo: 'permissao', frase: 'Sem acesso ao microfone. Libere nas permissões do site.', retem: false },
  'sem-microfone': { tipo: 'sem-microfone', frase: 'Não achei um microfone.', retem: false },
  'microfone-ocupado': { tipo: 'microfone-ocupado', frase: 'Não consegui abrir o microfone. Outro app pode estar usando.', retem: false },
  'sem-suporte': { tipo: 'sem-suporte', frase: 'Este navegador não grava áudio.', retem: false },
  'sem-formato': { tipo: 'sem-formato', frase: 'Este navegador não grava no formato certo.', retem: false },
  gravacao: { tipo: 'gravacao', frase: 'A gravação falhou. Tente de novo.', retem: false },
}

// erroDoMicrofone traduz o que o getUserMedia e a gravação levantam.
export function erroDoMicrofone(e: unknown): ErroDoDitado {
  if (e instanceof MicrofoneIndisponivel) return frasesDoMicrofone[e.motivo]!
  const nome = e instanceof Error ? e.name : ''
  if (nome === 'NotAllowedError' || nome === 'SecurityError') return frasesDoMicrofone.permissao!
  if (nome === 'NotFoundError' || nome === 'OverconstrainedError') return frasesDoMicrofone['sem-microfone']!
  if (nome === 'NotReadableError' || nome === 'AbortError') return frasesDoMicrofone['microfone-ocupado']!
  return frasesDoMicrofone.gravacao!
}

// erroSemFala: menos fala que o mínimo, medida no app (nada foi enviado), ou o
// provedor que não devolveu texto (SEM_FALA do servidor). nota é o que o app
// fez sozinho antes ("Parei quando o app saiu da frente.").
export function erroSemFala(nota?: string): ErroDoDitado {
  return { tipo: 'sem-fala', frase: nota ? `${nota} Não ouvi nada.` : 'Não ouvi nada.', retem: false }
}

// textoDaEspera: "Tente de novo em 12 s." (o Retry-After do 429 e do 503).
export function textoDaEspera(seg?: number): string {
  if (seg === undefined || seg <= 0) return ''
  if (seg < 90) return ` Tente de novo em ${seg} s.`
  return ` Tente de novo em ${Math.ceil(seg / 60)} min.`
}

// erroDoEnvio traduz a falha do POST /api/v1/dictations.
export function erroDoEnvio(e: unknown): ErroDoDitado {
  if (e instanceof SemSessao) return { tipo: 'sessao', frase: 'O ditado espera você entrar de novo.', retem: true }
  if (e instanceof ForaDoAr) return { tipo: 'rede', frase: 'Sem conexão.', retem: true }
  if (e instanceof DOMException && e.name === 'AbortError') return { tipo: 'prazo', frase: 'O envio demorou demais.', retem: true }
  if (!(e instanceof ErroDaApi)) return { tipo: 'outro', frase: 'Não deu para enviar o ditado.', retem: true }
  switch (e.codigo) {
    case 'SEM_ACEITE':
      return { tipo: 'sem-aceite', frase: 'Falta aceitar o aviso do ditado pelo celular.', retem: true, vai: 'aceite' }
    case 'SEM_CHAVE':
      return { tipo: 'sem-chave', frase: 'Falta a chave do provedor.', retem: true, vai: 'configuracoes' }
    case 'UM_POR_VEZ':
    case 'LIMITE':
    case 'OCUPADO':
      return { tipo: 'espere', frase: `Espere um pouco.${textoDaEspera(e.esperaSeg)}`, retem: true }
    case 'PAYLOAD_TOO_LARGE':
      return { tipo: 'longo', frase: 'Ficou longo demais.', retem: false }
    case 'FORMATO':
      return { tipo: 'formato', frase: 'Não deu para ler o áudio.', retem: false }
    case 'SEM_FALA':
      return erroSemFala()
    case 'PROVEDOR_RECUSOU_CHAVE':
      return { tipo: 'provedor-chave', frase: 'O provedor recusou a chave.', retem: true, vai: 'configuracoes' }
    case 'PROVEDOR_LIMITE':
    case 'PROVEDOR_FORA':
      return { tipo: 'provedor-fora', frase: 'O provedor não respondeu agora.', retem: true }
    case 'APAGADO':
      return { tipo: 'apagado', frase: 'Esse ditado foi apagado.', retem: false }
  }
  // Sem código que o app conheça: pelo status.
  if (e.status === 413) return { tipo: 'longo', frase: 'Ficou longo demais.', retem: false }
  if (e.status === 404) return { tipo: 'indisponivel', frase: 'Este servidor não tem o ditado pelo celular.', retem: false }
  if (e.status === 429 || e.status === 503) return { tipo: 'espere', frase: `Espere um pouco.${textoDaEspera(e.esperaSeg)}`, retem: true }
  return { tipo: 'outro', frase: 'Não deu para enviar o ditado.', retem: true }
}

// fraseDaChave traduz a falha de guardar a chave de um provedor (PUT
// /api/v1/ai-keys/{provedor}, ADR-021). Pelo código, e nunca pela mensagem do
// servidor: o decodificador dele pode trazer um pedaço do que foi colado.
export function fraseDaChave(e: unknown): string {
  if (e instanceof ErroDaApi) {
    switch (e.codigo) {
      case 'PROVEDOR_RECUSOU_CHAVE':
        return 'O provedor recusou a chave.'
      case 'PROVEDOR_LIMITE':
      case 'PROVEDOR_FORA':
        return 'O provedor não respondeu agora. Tente de novo.'
      case 'LIMITE':
        return `Espere um pouco.${textoDaEspera(e.esperaSeg)}`
    }
    // 400 (VALIDATION: vazia, longa, com caractere estranho), 413 (o corpo acima
    // de 16 KiB: colou muito mais que uma chave) e 422 (o provedor a recusou).
    if (e.status === 400 || e.status === 413 || e.status === 422) return 'Essa chave não parece válida. Confira e cole de novo.'
    if (e.status === 404) return 'Este servidor não tem o ditado pelo celular.'
    if (e.status === 429 || e.status === 503) return `Espere um pouco.${textoDaEspera(e.esperaSeg)}`
  }
  return 'Não deu para guardar a chave.'
}

// fraseDoAviso: o motivo de o Polish ter caído para o texto do Smart. O servidor
// manda um resolver.MotivoFallback (06, "POST /dictations": o campo `aviso`),
// da lista fechada do Polish, e nunca texto. O motivo que o app não conhece não
// vira frase solta: fica só "O Polish não agiu." Um teste
// (contratoDoDitado.test.ts) lê a lista do 06 e exige uma frase para cada motivo
// dela: um motivo novo no servidor quebra a suíte aqui, e não a tela de alguém.
const motivosDoPolish: Record<string, string> = {
  // Da conta ou do provedor: a pessoa pode agir.
  sem_chave: 'falta a chave',
  chave_recusada: 'o provedor recusou a chave',
  sem_credito: 'falta crédito no provedor',
  limite: 'o provedor atingiu o limite',
  modelo_inexistente: 'o modelo escolhido não existe mais',
  modelo_incompativel: 'o modelo escolhido não aceita este pedido',
  // Do provedor ou da rede: costuma passar sozinho.
  prazo: 'o provedor demorou',
  transporte: 'não consegui falar com o provedor',
  sobrecarga: 'o provedor está sobrecarregado',
  pedido_recusado: 'o provedor rejeitou o pedido',
  recusa: 'o modelo recusou responder',
  truncada: 'a resposta veio cortada',
  resposta_invalida: 'o provedor respondeu de um jeito inesperado',
  // As guardas: a IA respondeu, mas o texto que voltou não era o que a pessoa
  // disse, só com sinais e quebras. O provedor não tem culpa, e o nome dele só
  // confundiria.
  json: 'a resposta veio fora do formato',
  formato: 'a resposta trouxe algo além do texto',
  numeros: 'a resposta mudou os números',
  siglas_termos: 'a resposta mudou siglas, termos ou nomes',
  palavras: 'a resposta mudou o texto',
  trecho_novo: 'a resposta acrescentou palavras',
  caixa: 'a resposta pôs palavras em maiúsculas',
  dicionario: 'não consegui conferir os termos aprendidos',
  // Motivos do ia e do asr/nuvem (ia.Motivo). O servidor não os manda no aviso,
  // mas o texto antigo da ADR-021 os citava: ficam por tolerância.
  sem_permissao: 'a chave não tem permissão',
  tempo_esgotado: 'o provedor demorou',
  sem_rede: 'não consegui falar com o provedor',
  indisponivel: 'o provedor está fora do ar',
}

export function fraseDoAviso(motivo: string): string {
  const porque = motivosDoPolish[motivo]
  return porque ? `O Polish não agiu: ${porque}.` : 'O Polish não agiu.'
}
