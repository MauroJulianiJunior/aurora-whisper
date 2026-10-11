// O que os dois modos do painel dividem (ADR-019, decisão 12: o mesmo
// código, dois builds): os tipos do contrato (docs/ai-spec/06-api-contract.md),
// os erros, o pedir e o ClienteDoHistorico. Os clientes ficam fora daqui: o
// local em clienteLocal.ts (a Local API do Agent, com o token do bilhete) e o
// da nuvem em nuvem/cliente.ts (a /api/v1, pelo cookie da sessão).

export interface Ditado {
  id: string
  raw_text: string
  final_text: string
  mode: string
  asr_provider: string
  asr_model?: string
  duration_ms?: number
  processing_ms?: number
  target_app?: string
  status: string
  created_at: string
  updated_at: string
  deleted_at?: string
  version: number
  // versao_em_conflito é o selo (ADR-019, "Conflito"): o texto que perdeu
  // quando o mesmo ditado mudou em dois lugares. A nuvem e o PC (desde a
  // F7c) o mandam no mesmo formato.
  versao_em_conflito?: VersaoEmConflito
}

export interface VersaoEmConflito {
  final_text: string
  // device_id: o aparelho cuja versão perdeu (null: o celular).
  device_id: string | null
  conflict_at: string
}

// EscolhaDoSelo: "manter" fica com o texto atual; "trocar" fica com o do
// selo. As duas limpam o selo (ADR-019, "Decisões da F4").
export type EscolhaDoSelo = 'manter' | 'trocar'

export interface Pagina {
  items: Ditado[]
  next_cursor?: string
}

// Sugestao é uma troca de termo que a edição sugere aprender (ADR-017): onde
// o ASR escreveu `de`, o texto final passou a ter `para`. Só sugestão: nada
// vai ao dicionário sem o clique em Aprender.
export interface Sugestao {
  // de é a forma que o ASR escreveu (lida do texto bruto); é a que vira
  // variante.
  de: string
  // para é a forma final: a canônica do dicionário se o termo já existe.
  para: string
  // termo_existente: o termo do dicionário que já é o `para`, ou "" quando
  // aprender CRIA o termo.
  termo_existente: string
  // ocorrencias: em quantos dos últimos ditados o `de` aparece no bruto. 0
  // é "em nenhum" ou "não deu para contar": a tela só fala quando é maior.
  ocorrencias: number
}

// Aprendida é uma variante que o painel ensinou: `variante` vira `termo`.
export interface Aprendida {
  termo: string
  variante: string
  // vale_ao_reiniciar: o Agent gravou a mudança, mas não conseguiu trocar o
  // dicionário em memória: ela só vale depois de reiniciar o Agent. Ausente
  // no caso comum (vale a partir do próximo ditado).
  vale_ao_reiniciar?: boolean
  // situacao: só nas linhas de estado do sync (ADR-019, F8): "nao_aplicado"
  // (a troca chegou da nuvem e não vale neste PC; não está no dicionário daqui
  // e não tem Desfazer), "desfeito_em_outro_aparelho" e
  // "trocado_em_outro_aparelho" (uma troca deste PC que perdeu na nuvem; a
  // linha some quando a pessoa aprende de novo, ou em 30 dias). Ausente na
  // troca que vale aqui, e num Agent de antes da F8: a linha é a de sempre,
  // com o Desfazer. Um valor que o painel não conhece não vira linha.
  situacao?: string
  // motivo: só em "nao_aplicado", de uma lista fechada do Agent
  // (dominio_ausente, restricao_nao_cabe, ...; textos em aprendizado.tsx).
  motivo?: string
}

// TermoAdicionado é uma linha de "Adicionados por você" (ADR-020): a palavra
// certa e as grafias erradas que a pessoa pôs nela.
export interface TermoAdicionado {
  termo: string
  // variantes: só as que a pessoa digitou, na ordem do dicionário.
  variantes: string[]
  // dica: a palavra vai no prompt do reconhecimento de voz.
  dica: boolean
  // do_dicionario: o termo já era do dicionário; a pessoa só pôs grafias, e
  // a dica dele não muda pelo painel.
  do_dicionario: boolean
}

// Dicas é o orçamento do prompt do reconhecimento de voz, contado só sobre
// as dicas da pessoa, em caracteres.
export interface Dicas {
  usadas: number
  orcamento: number
}

export interface ListaDeTermos {
  items: TermoAdicionado[]
  dicas: Dicas
}

// MudancaDeTermos é a resposta de adicionar, ligar ou desligar a dica e
// remover: a lista inteira e o que a mudança fez.
export interface MudancaDeTermos extends ListaDeTermos {
  // termo: a forma como ficou no dicionário.
  termo: string
  ja_existia: boolean
  // dica_ignorada: no Adicionar, o pedido trazia a dica e o termo já era do
  // dicionário: as grafias entraram, a dica dele não mudou. Ausente no caso
  // comum.
  dica_ignorada?: boolean
  // sairam_das_dicas: termos que saíram do prompt com a mudança.
  sairam_das_dicas: string[]
  // vale_ao_reiniciar: gravado, mas só vale depois de reiniciar o Agent.
  vale_ao_reiniciar?: boolean
}

export interface PedidoDeTermo {
  termo: string
  variantes: string[]
  dica: boolean
}

// Gravado é a resposta do PATCH: o ditado como ficou e as sugestões de
// aprendizado, de 0 a 2 (vazia quando não há o que sugerir, ou num Agent
// anterior à etapa 3).
export interface Gravado {
  ditado: Ditado
  sugestoes: Sugestao[]
}

export type ModoTexto = 'literal' | 'smart' | 'polish'
export type Provedor = 'openai' | 'anthropic'

// Configuracoes é o GET /api/settings do contrato comum de 29-set-2026.
export interface Configuracoes {
  // hotkey é só leitura nesta etapa: o painel mostra, não muda.
  hotkey: string
  hold_threshold_ms: number
  beep_enabled: boolean
  // microphone_always_ready (ADR-015): ligado, o microfone fica aberto o
  // tempo todo; desligado (padrão), só abre durante o ditado.
  microphone_always_ready: boolean
  text_mode: ModoTexto
  ai_provider: '' | Provedor
  ai_model: string
  // autostart vem do registro do Windows, que é a fonte da verdade.
  autostart: boolean
  // autostart_indisponivel: o motivo de não dar para mudar ("" = dá).
  autostart_indisponivel: string
  // reinicio_pendente: campos gravados que só valem no próximo início
  // do Agent.
  reinicio_pendente: string[]
  // polish_disponivel não está no contrato comum: enquanto o Agent não
  // mandar true, o Polish não existe para o painel — ausente é false. O
  // Agent com a etapa 2 (ADR-016) manda true.
  polish_disponivel?: boolean
  // polish_ultimo_motivo: por que o último Polish não agiu, numa frase
  // pronta para a tela ("Sem crédito na OpenAI: …"). "" ou ausente: o
  // último agiu, ainda não houve nenhum, ou o motivo já é antigo. Só o
  // motivo — nunca o texto ditado. Vive em memória no Agent.
  polish_ultimo_motivo?: string
  version: number
}

export type Editaveis = Pick<
  Configuracoes,
  'hold_threshold_ms' | 'beep_enabled' | 'microphone_always_ready' | 'text_mode' | 'ai_provider' | 'ai_model' | 'autostart'
>

// EstadoChave nunca traz a chave: só se há uma guardada e os 4 últimos
// caracteres dela.
export interface EstadoChave {
  provedor: Provedor
  configurada: boolean
  final: string
}

// modelo_sugerido: o modelo que o Agent SUGERE para o Polish nesse provedor
// (ADR-016, decisão 10; o vencedor do benchmark). "" ou ausente: nenhum. É
// só sugestão — a tela o marca e, se ele consta em `modelos`, o põe no
// rascunho quando nenhum modelo foi escolhido; nunca o exige.
export type ResultadoTeste = { ok: true; modelos: string[]; modelo_sugerido?: string } | { ok: false; erro: string }

// SemSessao: o 401. No modo local, o Agent não reconhece o token (ou o
// bilhete): acontece ao recarregar a página, porque o token só existe na
// memória da aba. Na nuvem, a sessão do cookie venceu ou foi encerrada.
export class SemSessao extends Error {
  constructor() {
    super('sem sessão')
    this.name = 'SemSessao'
  }
}

// ForaDoAr: o fetch nem chegou a uma resposta. No modo local, o Agent foi
// fechado — ou reiniciou: a cada início ele escuta numa porta nova, com um
// token novo, e esta aba continua apontando para a porta velha (revisão de
// 29-set-2026: reiniciar dá ForaDoAr, não 401). Na nuvem, é a rede (ou o
// servidor fora do ar): "Sem conexão". A mensagem é a do modo local; a tela
// usa a do ambiente (Ambiente.textos.semResposta).
export class ForaDoAr extends Error {
  constructor() {
    super('o Agent não respondeu')
    this.name = 'ForaDoAr'
  }
}

// Conflito: o PATCH de um ditado ou o PUT das configurações levou uma
// base_version velha (409). Nas rotas de aprendizado o 409 é outra coisa
// (variante que já é de outro termo, sugestão vencida...) e chega como
// ErroDaApi, com a frase pronta para a tela.
export class Conflito extends Error {
  constructor() {
    super('versão desatualizada')
    this.name = 'Conflito'
  }
}

export class ErroDaApi extends Error {
  status: number
  // previa: só no 409 do OK dos 6 meses (POST /api/retencao/ligar) quando a
  // contagem mudou: o número novo, ao lado do envelope de erro (06, "Retenção
  // no PC"). Ausente em todo outro erro.
  previa?: number
  // codigo: o error.code do envelope, em maiúsculas (ADR-021, "A rota": o
  // app decide pelo código, nunca pela frase). Ausente quando o corpo não é
  // o envelope (um proxy, um erro do navegador).
  codigo?: string
  // esperaSeg: o Retry-After da resposta (429 e 503), em segundos. Ausente
  // quando o servidor não mandou, ou mandou uma data.
  esperaSeg?: number
  constructor(status: number, mensagem: string, previa?: number) {
    super(mensagem)
    this.name = 'ErroDaApi'
    this.status = status
    if (previa !== undefined) this.previa = previa
  }
}

// Problema: a falha não é da ação, é da ligação (com o Agent, ou com a
// nuvem) — nenhuma outra ação vai dar certo até isso mudar.
export type Problema = 'sem-sessao' | 'fora-do-ar'

export function problemaDe(e: unknown): Problema | null {
  if (e instanceof SemSessao) return 'sem-sessao'
  if (e instanceof ForaDoAr) return 'fora-do-ar'
  return null
}

export type Buscador = (entrada: string, init?: RequestInit) => Promise<Response>

export const buscadorPadrao: Buscador = (entrada, init) => fetch(entrada, init)

// pedir faz a requisição e traduz as falhas para os erros acima. A
// mensagem de ErroDaApi é a do próprio contrato ({error:{message}}),
// quando existe. conflitoEhVersao: o 409 é a base_version velha (Conflito);
// false, ele é um erro comum, com a mensagem do Agent.
export async function pedir(buscar: Buscador, caminho: string, init: RequestInit = {}, conflitoEhVersao = true): Promise<Response> {
  let resposta: Response
  try {
    resposta = await buscar(caminho, { ...init, cache: 'no-store' })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    throw new ForaDoAr()
  }
  if (resposta.ok) return resposta
  if (resposta.status === 401) throw new SemSessao()
  if (resposta.status === 409 && conflitoEhVersao) throw new Conflito()
  let mensagem = `erro ${resposta.status}`
  let previa: number | undefined
  let codigo: string | undefined
  try {
    const corpo = (await resposta.json()) as { error?: { message?: string; code?: unknown }; previa?: unknown }
    if (corpo.error?.message) mensagem = corpo.error.message
    if (typeof corpo.error?.code === 'string' && corpo.error.code !== '') codigo = corpo.error.code
    if (typeof corpo.previa === 'number') previa = corpo.previa
  } catch {
    // corpo que não é JSON: fica o status
  }
  const erro = new ErroDaApi(resposta.status, mensagem, previa)
  if (codigo) erro.codigo = codigo
  // Retry-After em segundos inteiros; uma data (a outra forma do cabeçalho) não conta.
  const espera = resposta.headers?.get('Retry-After') ?? ''
  if (/^\d{1,5}$/.test(espera)) erro.esperaSeg = Number(espera)
  throw erro
}

// ClienteDoHistorico é o que o Painel usa do histórico, nos dois modos: o
// cliente local (clienteLocal.ts, a Local API do Agent) e o da nuvem
// (nuvem/cliente.ts, a /api/v1 pelo cookie da sessão).
export interface ClienteDoHistorico {
  listar(filtro: { lixeira: boolean; busca: string; cursor?: string }, sinal?: AbortSignal): Promise<Pagina>
  obter(id: string): Promise<Ditado>
  // editar devolve o ditado como ficou e as sugestões de aprendizado (a
  // nuvem nunca sugere: aprender é só no PC, ADR-017).
  editar(id: string, versao: number, texto: string): Promise<Gravado>
  moverParaLixeira(id: string): Promise<void>
  restaurar(id: string): Promise<void>
}

