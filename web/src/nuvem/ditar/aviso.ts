// O aviso do ditado pelo celular (ADR-021, decisão H e "O que a D0 reescreve"):
// o que cada provedor diz. O texto da folha do aceite está em
// FolhaDoAceite.tsx; o que muda de um provedor para outro está aqui, em dados.
//
// A VERSÃO do aviso não mora aqui: é a VERSAO_DO_AVISO de ../avisoDoDitado.ts,
// a única do app (presa à do servidor por um teste do Go). QUALQUER mudança no
// texto da folha ou nos blocos abaixo muda aquela versão, nos dois lados, e
// quem aceitou a anterior aceita de novo (decisão H, escolha menor 21); o
// avisoDoDitado.test.ts quebra se o texto mudar sem a versão.
//
// Tudo o que o aviso diz sobre um provedor é o que a documentação dele diz,
// lida em 10-out-2026 (ADR-021, "O motor"). O aviso não afirma o país onde o
// provedor processa quando a página dele não afirma, nem afirma que a conta de
// quem dita tem uma proteção que o servidor não consegue conferir.

// Os provedores que o ditado fala: a voz vai à OpenAI ou à Groq; o Polish, à
// OpenAI ou à Anthropic. A chave de cada um é uma só, qualquer que seja o uso.
export type Provedor = 'openai' | 'groq' | 'anthropic'
export type ProvedorDeVoz = 'openai' | 'groq'
export type ProvedorDoPolish = 'openai' | 'anthropic'

export const provedoresDeVoz: readonly ProvedorDeVoz[] = ['openai', 'groq']
export const provedoresDoPolish: readonly ProvedorDoPolish[] = ['openai', 'anthropic']
export const todosOsProvedores: readonly Provedor[] = ['openai', 'groq', 'anthropic']

export const nomeDoProvedor: Record<Provedor, string> = {
  openai: 'OpenAI',
  groq: 'Groq',
  anthropic: 'Anthropic',
}

// ehProvedor confere o que o servidor mandou: um nome que o app não conhece
// não vira escolha.
export function ehProvedor(v: unknown): v is Provedor {
  return v === 'openai' || v === 'groq' || v === 'anthropic'
}

// blocoDoProvedor é o parágrafo "Para onde, e por quanto tempo fica lá" de cada
// provedor. A OpenAI e a Groq valem para a voz; a Anthropic, para o Polish (a
// OpenAI vale para os dois).
export const blocoDoProvedor: Record<Provedor, string> = {
  openai:
    'Ela diz que não usa o que você envia pela API para treinar os modelos dela, a menos que você permita. ' +
    'Guarda registros de monitoramento de abuso, que podem conter a sua voz e a resposta, por até 30 dias ' +
    '(a tabela dela diz "nenhuma" para a transcrição de áudio e o texto geral diz até 30 dias, então contamos até 30; ' +
    'pode ser mais, se a lei exigir), a não ser que a sua conta tenha uma exceção que ela aprovou. ' +
    'Ela processa nos EUA ou na Europa se a sua conta fixou a residência de dados; se não, a documentação dela ' +
    'não diz em que país. O que está aqui é o que a OpenAI diz (lido em 10-out-2026). O Aurora Whisper não confere a sua conta.',
  groq:
    'Ela diz que, por padrão, não retém o que você envia; pode registrar a voz e o texto por até 30 dias, só para ' +
    'investigar falha ou abuso, a menos que a sua conta ligue a retenção zero. O que ela retém fica em servidores ' +
    'nos EUA; a documentação dela não diz onde a transcrição roda, e o contrato dela prevê tratamento nos EUA e em ' +
    'outros países. As páginas que lemos não falam em treinar com os seus dados. O que está aqui é o que a Groq ' +
    'diz (lido em 10-out-2026). O Aurora Whisper não confere a sua conta.',
  anthropic:
    'Ela diz que pode guardar o texto por até 30 dias e que não o usa para treinar modelos (visto em 29-set-2026, ' +
    'e não relido em 10-out-2026). O Aurora Whisper não afirma em que país ela processa e não confere a sua conta.',
}
