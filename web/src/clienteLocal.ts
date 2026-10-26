// O cliente da Local API do Agent (docs/ai-spec/06-api-contract.md, "API
// local"): o histórico, as Configurações, a chave de IA, o aprendizado, os
// termos, a nuvem e os 6 meses do histórico no PC. Só o build local o importa: o da nuvem tem o dele
// (nuvem/cliente.ts), e bundle.test.ts confere que nada daqui vai para lá.
//
// O token vem da troca do bilhete (sessao.ts) e vive só aqui, na memória
// da aba — nunca em localStorage, cookie ou URL (09-security-privacy.md).

import {
  type Aprendida,
  type Buscador,
  type ClienteDoHistorico,
  type Configuracoes,
  type Ditado,
  type Editaveis,
  type EscolhaDoSelo,
  type EstadoChave,
  type ListaDeTermos,
  type MudancaDeTermos,
  type Pagina,
  type PedidoDeTermo,
  type Provedor,
  type ResultadoTeste,
  type Sugestao,
  Conflito,
  ErroDaApi,
  buscadorPadrao,
  pedir,
} from './api'
import type { ClienteDeNotas, DesfeitoOVirarNota, Marcador, Nota } from './notas/tipos'

// EstadoNuvem é o GET /api/nuvem (ADR-019, F6): se este PC está ligado à
// nuvem. Nunca traz o token do aparelho, que fica no Gerenciador de
// Credenciais do Windows.
export interface EstadoNuvem {
  estado: 'desconectado' | 'conectando' | 'conectado'
  // url: a nuvem desta instalação (ou do pedido em curso), para o campo.
  url: string
  // Conectado: a conta e o nome com que o PC aparece na nuvem. Desconectado
  // com a nuvem gravada, a conta continua (o Esquecer diz qual é).
  email?: string
  nome?: string
  // Conectando: o código de conferência e a página de aprovação.
  conferencia?: string
  abrir_url?: string
  // aviso: o que aconteceu por último (internal/agent/nuvem, Aviso*).
  aviso?: AvisoDaNuvem
  // historico: o lado do histórico (F7c), com a nuvem gravada: a prévia do
  // envio inicial, o progresso, o que não subiu e a última rodada.
  historico?: ResumoDoHistorico
}

// ResumoDoHistorico é o que a seção Nuvem mostra do histórico (ADR-019,
// "Decisões da F7c"). Só contagens e a hora da última rodada.
export interface ResumoDoHistorico {
  // previa: quantos ditados o "Enviar o histórico" mandaria agora.
  previa: number
  // previa_trocas: quantas trocas aprendidas o mesmo clique soltaria (o M de
  // "Vão subir N ditados e M trocas aprendidas"; ADR-019, F8, D3). Ausente num
  // Agent de antes da F8: vale 0, e a pergunta é a de sempre. A pergunta
  // aparece também com previa 0 e previa_trocas maior que 0.
  previa_trocas?: number
  // freio_trocas: quantas trocas aprendidas saíram do dicionário de uma vez,
  // na última varredura, e seguraram o aprendido: com mais de 10 saindo à mão,
  // nada do aprendido sobe até elas voltarem (o freio de ADR-019, F8, escolha
  // menor 1). Ausente, ou 0, é sem freio (também num Agent de antes da F8).
  freio_trocas?: number
  // aceito: a pessoa já mandou enviar (ou não havia nada a enviar).
  aceito: boolean
  // envio: o envio inicial em curso; ausente quando não há.
  envio?: { feitos: number; total: number }
  // nao_subiram: ditados deste PC que a nuvem recusou ou pôs fora da janela.
  nao_subiram: number
  // pendentes: o que ainda não subiu.
  pendentes: number
  // mudancas: dos pendentes, as mudanças feitas aqui num ditado que a nuvem
  // tem (a edição, a lixeira, o restaurar, a escolha do selo e a
  // reconciliação) e, desde a F8, a troca aprendida desfeita ou trocada aqui
  // que a nuvem ainda não confirmou; fora o ditado novo e o histórico que
  // ainda não subiram: o número da pergunta do esquecer.
  mudancas: number
  // conferido_em: a última rodada que deu certo (RFC 3339).
  conferido_em?: string
  // falhou: a última rodada não deu certo; o PC tenta de novo sozinho.
  falhou: boolean
  // outra_nuvem: este PC esqueceu uma nuvem que já tinha ditados dele.
  outra_nuvem: boolean
}

export type AvisoDaNuvem = 'revogado' | 'sem_resposta' | 'nao_abriu' | 'vencido' | 'outra_conta' | 'falhou' | 'sem_rede'

// EstadoRetencao é o GET /api/retencao (ADR-019, F10; 06, "Retenção no PC"): os
// 6 meses do histórico no PC e as cópias ao lado do banco. Só números e
// booleanos: nunca texto, id ou caminho.
export interface EstadoRetencao {
  // previa: quantos ditados o OK apagaria agora (os fora da janela de 6 meses,
  // ativos ou na lixeira).
  previa: number
  // na_lixeira e com_selo: subconjuntos da previa (o selo, "2 versões", sai
  // sem a escolha de quem usa).
  na_lixeira: number
  com_selo: number
  // de_outro_dono: os fora da janela de outro dono, que ficam e não entram na
  // previa.
  de_outro_dono: number
  // ligada: o OK está gravado, e o que completa 6 meses sai sozinho.
  ligada: boolean
  // pausada: o freio pausou a regra (o Agent ficou mais de 7 dias parado, ou o
  // relógio do PC pulou); a pergunta volta.
  pausada: boolean
  // copias: quantos arquivos de cópia do histórico há ao lado do banco.
  copias: number
  // copias_apagaveis: das copias, as que o botão apaga agora (todas menos a do
  // envio do histórico à nuvem que ainda não terminou). Ausente num Agent
  // anterior a ela: valem todas.
  copias_apagaveis?: number
  // copia_dos_6_meses: a cópia de um OK anterior está ao lado do banco; o
  // próximo OK não copia de novo, e o que nasceu depois dela não está nela.
  copia_dos_6_meses?: boolean
}

// RetencaoLigada é a resposta do OK: o estado de agora e quantos ditados saíram.
export interface RetencaoLigada extends EstadoRetencao {
  apagados: number
  // incompleto: o OK foi gravado e parte saiu, mas um lote seguinte caiu. A
  // regra está ligada; o que falta (previa) sai na próxima rodada, em até 1
  // hora. Ausente num Agent de antes desta resposta.
  incompleto?: boolean
}

// CopiasApagadas é a resposta do botão das cópias: o estado de agora, quantas
// saíram e quantas ficaram (a do envio do histórico à nuvem que ainda não
// terminou fica).
export interface CopiasApagadas extends EstadoRetencao {
  apagadas: number
  ficaram: number
}

// Cliente é o cliente local inteiro: o histórico, as notas (ADR-022) e o que só
// o PC tem.
export interface Cliente extends ClienteDoHistorico, ClienteDeNotas {
  configuracoes(): Promise<Configuracoes>
  // campos: só os que vão no PUT; o que fica de fora, o Agent mantém.
  salvarConfiguracoes(versao: number, campos: Partial<Editaveis>): Promise<Configuracoes>
  chave(provedor: Provedor): Promise<EstadoChave>
  // guardarChave é o único caminho por onde a chave sai da página.
  guardarChave(provedor: Provedor, chave: string): Promise<void>
  removerChave(provedor: Provedor): Promise<void>
  testarConexao(provedor: Provedor): Promise<ResultadoTeste>
  // aprender é o clique em Aprender: o Agent confere de novo a sugestão
  // contra o texto bruto daquele ditado e só então grava. Um 409 vem como
  // ErroDaApi, com a frase para a tela.
  aprender(id: string, sugestao: Pick<Sugestao, 'de' | 'para'>): Promise<Aprendida>
  // desfazerAprendizado tira uma variante que o painel aprendeu.
  desfazerAprendizado(variante: string): Promise<Aprendida>
  // aprendidas é a lista "Aprendido no painel".
  aprendidas(): Promise<Aprendida[]>
  // termos é a lista "Adicionados por você" e o orçamento das dicas
  // (ADR-020). O texto vai sempre no corpo, nunca na URL.
  termos(): Promise<ListaDeTermos>
  adicionarTermo(pedido: PedidoDeTermo): Promise<MudancaDeTermos>
  dicaDoTermo(termo: string, dica: boolean): Promise<MudancaDeTermos>
  removerTermo(termo: string): Promise<MudancaDeTermos>
  // A nuvem (ADR-019, F6). conferirNuvem pergunta à nuvem se o token ainda
  // vale (o Agent pergunta no máximo a cada 30 s) e devolve o estado;
  // desconectarNuvem também é o Cancelar do pedido em curso. Os erros vêm
  // como ErroDaApi, com a frase do Agent.
  nuvem(): Promise<EstadoNuvem>
  conectarNuvem(url: string): Promise<EstadoNuvem>
  conferirNuvem(): Promise<EstadoNuvem>
  desconectarNuvem(): Promise<EstadoNuvem>
  // O envio inicial e o esquecer (F7c): enviarHistorico faz a cópia e põe
  // o histórico na fila (a cópia que falha volta como ErroDaApi, com a
  // frase); esquecerNuvem só vale desconectado.
  enviarHistorico(): Promise<EstadoNuvem>
  esquecerNuvem(): Promise<EstadoNuvem>
  // Os 6 meses no PC e as cópias do banco (ADR-019, F10d e F10e). Os erros vêm
  // como ErroDaApi, com a frase do Agent; um Agent de antes da F10 responde
  // 404. ligarRetencao manda a contagem que a tela mostrou: se a recontagem do
  // Agent difere, o 409 vem com o número novo em ErroDaApi.previa, e nada foi
  // apagado. apagarCopias só vale depois da pergunta na tela.
  retencao(): Promise<EstadoRetencao>
  ligarRetencao(previa: number): Promise<RetencaoLigada>
  desligarRetencao(): Promise<EstadoRetencao>
  apagarCopias(): Promise<CopiasApagadas>
  // resolverConflito é a escolha do selo (F7c), o mesmo PATCH da nuvem: o
  // 409 vem como Conflito, e o painel relê o ditado.
  resolverConflito(id: string, versao: number, escolha: EscolhaDoSelo): Promise<Ditado>
  // virarNota é o "Virar nota" do histórico (ADR-022, N3; POST
  // /api/history/{id}/virar-nota): cria a nota com o texto do ditado e esconde o
  // ditado do histórico, sem lixeira. Os 409 vêm como ErroDaApi, com o codigo
  // IN_TRASH (o ditado está na lixeira) ou ALREADY_NOTE (já virou nota).
  virarNota(ditadoId: string): Promise<Nota>
  // desfazerVirarNota é o Desfazer do aviso (POST /api/notas/{id}/desfazer-virar):
  // o ditado volta ao histórico, e a nota sai se ninguém a editou.
  desfazerVirarNota(notaId: string): Promise<DesfeitoOVirarNota>
  // mudancas é o contador de mudanças do sync (ADR-021, S; GET
  // /api/nuvem/mudancas): quantas rodadas gravaram no histórico algo que veio
  // da nuvem, desde que o Agent subiu. Só um número. A leitura é também o
  // aviso ao Agent de que o painel está aberto (ele busca a nuvem a cada 10
  // s); um Agent de antes da S responde 404.
  mudancas(sinal?: AbortSignal): Promise<number>
  // microfone diz se o Windows bloqueia o microfone do Agent (GET
  // /api/microfone) e por quê. Lê o registro a cada pedido: o aviso some sozinho
  // quando a pessoa liga a chave e volta à aba. Um Agent de antes do aviso
  // responde 404.
  microfone(sinal?: AbortSignal): Promise<EstadoDoMicrofone>
  // abrirConfiguracoesDoMicrofone pede ao Agent que abra a tela de privacidade
  // do microfone do Windows (POST /api/microfone/configuracoes). Quem liga a
  // chave é a pessoa; o app não a liga sozinho.
  abrirConfiguracoesDoMicrofone(): Promise<void>
}

// MotivoDoBloqueio é por que o Windows bloqueia o microfone: a política da
// empresa, o aparelho, a conta, os programas da área de trabalho ou só este
// app. Vazio é liberado; "desconhecido" é "o Agent não conseguiu ler", que não
// é bloqueio. Uma palavra que o painel não conheça (um Agent mais novo) cai no
// texto do aparelho.
export type MotivoDoBloqueio = 'empresa' | 'aparelho' | 'conta' | 'programas' | 'este_app' | 'desconhecido' | ''

// EstadoDoMicrofone é o GET /api/microfone: só o booleano e a palavra do motivo.
export interface EstadoDoMicrofone {
  bloqueado: boolean
  motivo: MotivoDoBloqueio
}

// Um slice nil do Go vira null no JSON: a lista vazia do contrato pode
// chegar assim.
const comListas = (c: Configuracoes): Configuracoes => ({ ...c, reinicio_pendente: c.reinicio_pendente ?? [] })

// Um slice nil do Go vira null: as listas de /api/termos chegam sempre
// como lista.
function daLista(l: ListaDeTermos): ListaDeTermos {
  return {
    items: (l.items ?? []).map((t) => ({ ...t, variantes: t.variantes ?? [] })),
    dicas: l.dicas,
  }
}

function daMudanca(m: MudancaDeTermos): MudancaDeTermos {
  return { ...m, ...daLista(m), sairam_das_dicas: m.sairam_das_dicas ?? [] }
}

// O resultado do teste do jeito que a tela usa: modelos sempre lista (o
// Go que filtra e não acha nada manda null, e um .length em null
// derrubava o painel inteiro), modelo_sugerido sempre texto ("" quando o
// Agent não sugere, ou é de antes da sugestão) e erro sempre com texto.
function resultadoDoTeste(r: { ok?: unknown; modelos?: string[] | null; modelo_sugerido?: unknown; erro?: string }): ResultadoTeste {
  if (r.ok === true) return { ok: true, modelos: r.modelos ?? [], modelo_sugerido: typeof r.modelo_sugerido === 'string' ? r.modelo_sugerido : '' }
  return { ok: false, erro: r.erro || 'O teste não deu certo, e o Aurora Whisper não disse por quê.' }
}

export function criarCliente(token: string, buscar: Buscador = buscadorPadrao): Cliente {
  const autorizado = (init: RequestInit = {}): RequestInit => ({
    ...init,
    headers: { ...(init.headers as Record<string, string> | undefined), Authorization: `Bearer ${token}` },
  })
  const doItem = (id: string) => `/api/history/${encodeURIComponent(id)}`
  const daNota = (id: string) => `/api/notas/${encodeURIComponent(id)}`
  const doMarcador = (id: string) => `/api/marcadores/${encodeURIComponent(id)}`
  const comJson = (method: string, corpo: unknown): RequestInit =>
    autorizado({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(corpo) })
  const daChave = (provedor: Provedor) => `/api/ia/chave?${new URLSearchParams({ provedor })}`

  return {
    async listar({ lixeira, busca, cursor }, sinal) {
      const q = new URLSearchParams()
      if (lixeira) q.set('trash', 'true')
      if (busca) q.set('q', busca)
      if (cursor) q.set('cursor', cursor)
      const r = await pedir(buscar, `/api/history?${q}`, autorizado({ signal: sinal }))
      return (await r.json()) as Pagina
    },
    async obter(id) {
      const r = await pedir(buscar, doItem(id), autorizado())
      return (await r.json()) as Ditado
    },
    async editar(id, versao, texto) {
      const r = await pedir(
        buscar,
        doItem(id),
        autorizado({
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ base_version: versao, final_text: texto }),
        }),
      )
      // O que o Agent acrescenta ao item é só o campo sugestoes: o resto é o
      // ditado, e é ele que a lista guarda. Um Agent anterior à etapa 3, ou
      // um slice nil do Go, não manda a lista.
      const { sugestoes, ...ditado } = (await r.json()) as Ditado & { sugestoes?: Sugestao[] | null }
      return { ditado, sugestoes: sugestoes ?? [] }
    },
    async moverParaLixeira(id) {
      await pedir(buscar, doItem(id), autorizado({ method: 'DELETE' }))
    },
    async virarNota(ditadoId) {
      // conflitoEhVersao false: o 409 aqui não é versão, é "na lixeira" ou "já virou nota".
      const r = await pedir(buscar, `${doItem(ditadoId)}/virar-nota`, autorizado({ method: 'POST' }), false)
      return (await r.json()) as Nota
    },
    async desfazerVirarNota(notaId) {
      const r = await pedir(buscar, `${daNota(notaId)}/desfazer-virar`, autorizado({ method: 'POST' }), false)
      return (await r.json()) as DesfeitoOVirarNota
    },
    async restaurar(id) {
      await pedir(buscar, `${doItem(id)}/restore`, autorizado({ method: 'POST' }))
    },
    // As notas (ADR-022, N1; 06, "Notas e marcadores"). O texto da nota vai
    // sempre no corpo do pedido; a busca é o parâmetro q, como no histórico.
    async listarNotas({ vista, marcador, busca }, sinal) {
      const q = new URLSearchParams({ vista })
      if (marcador) q.set('marcador', marcador)
      if (busca) q.set('q', busca)
      const r = await pedir(buscar, `/api/notas?${q}`, autorizado({ signal: sinal }))
      const { items } = (await r.json()) as { items?: Nota[] | null }
      return items ?? []
    },
    async obterNota(id) {
      const r = await pedir(buscar, daNota(id), autorizado())
      return (await r.json()) as Nota
    },
    async criarNota(pedido) {
      const r = await pedir(buscar, '/api/notas', comJson('POST', pedido), false)
      return (await r.json()) as Nota
    },
    async editarNota(id, versao, mudanca) {
      // O 409 é a versão velha, ou a nota que foi para a lixeira em outra aba:
      // nos dois casos o painel lê a nota de novo (Conflito).
      const r = await pedir(buscar, daNota(id), comJson('PATCH', { ...mudanca, base_version: versao }))
      return (await r.json()) as Nota
    },
    async moverNotaParaLixeira(id) {
      await pedir(buscar, daNota(id), autorizado({ method: 'DELETE' }), false)
    },
    async restaurarNota(id) {
      const r = await pedir(buscar, `${daNota(id)}/restaurar`, autorizado({ method: 'POST' }), false)
      return (await r.json()) as Nota
    },
    async apagarNotaDeVez(id) {
      await pedir(buscar, `${daNota(id)}/definitivo`, autorizado({ method: 'DELETE' }), false)
    },
    // O aviso de nota em foco (ADR-022, "Ditado feito dentro da nota"). Melhor
    // esforço: quem chama ignora a falha, e o Agent deixa o aviso vencer em 60 s.
    async definirFoco(alvo) {
      await pedir(buscar, '/api/notas/foco', comJson('PUT', alvo), false)
    },
    async limparFoco(alvo, descartar) {
      const corpo = alvo ? { ...alvo, ...(descartar ? { descartar: true } : {}) } : {}
      // keepalive: o pedido sobrevive à aba que está fechando.
      await pedir(buscar, '/api/notas/foco', { ...comJson('DELETE', corpo), keepalive: true }, false)
    },
    // A separação em itens (N5). A resposta vem normalizada: um "como" que o
    // painel não conhece vale "nenhum", e "nenhum" nunca traz itens.
    async separarItens(trecho) {
      const r = await pedir(buscar, '/api/notas/separar-itens', comJson('POST', { trecho }), false)
      const { itens, como } = (await r.json()) as { itens?: unknown; como?: unknown }
      const lista = Array.isArray(itens) ? itens.filter((i): i is string => typeof i === 'string') : []
      if ((como !== 'ia' && como !== 'regra') || lista.length < 2) return { itens: [], como: 'nenhum' }
      return { itens: lista, como }
    },
    async listarMarcadores(sinal) {
      const r = await pedir(buscar, '/api/marcadores', autorizado({ signal: sinal }), false)
      const { items } = (await r.json()) as { items?: Marcador[] | null }
      return items ?? []
    },
    async criarMarcador(nome) {
      // conflitoEhVersao false: o 409 daqui é o nome já usado (NAME_IN_USE).
      const r = await pedir(buscar, '/api/marcadores', comJson('POST', { nome }), false)
      return (await r.json()) as Marcador
    },
    async renomearMarcador(id, versao, nome) {
      // Dois 409: o nome já usado (NAME_IN_USE, que a tela mostra) e a versão
      // velha (CONFLICT, que vira Conflito).
      try {
        const r = await pedir(buscar, doMarcador(id), comJson('PATCH', { nome, base_version: versao }), false)
        return (await r.json()) as Marcador
      } catch (e) {
        if (e instanceof ErroDaApi && e.status === 409 && e.codigo !== 'NAME_IN_USE') throw new Conflito()
        throw e
      }
    },
    async apagarMarcador(id) {
      await pedir(buscar, doMarcador(id), autorizado({ method: 'DELETE' }), false)
    },
    async marcarNota(notaId, marcadorId) {
      // O 409 é a nota que foi para a lixeira em outra aba (Conflito).
      const r = await pedir(buscar, `${daNota(notaId)}/marcadores/${encodeURIComponent(marcadorId)}`, autorizado({ method: 'PUT' }))
      return (await r.json()) as Nota
    },
    async desmarcarNota(notaId, marcadorId) {
      const r = await pedir(buscar, `${daNota(notaId)}/marcadores/${encodeURIComponent(marcadorId)}`, autorizado({ method: 'DELETE' }))
      return (await r.json()) as Nota
    },
    async configuracoes() {
      const r = await pedir(buscar, '/api/settings', autorizado())
      return comListas((await r.json()) as Configuracoes)
    },
    async salvarConfiguracoes(versao, campos) {
      const r = await pedir(buscar, '/api/settings', comJson('PUT', { ...campos, base_version: versao }))
      return comListas((await r.json()) as Configuracoes)
    },
    async chave(provedor) {
      const r = await pedir(buscar, daChave(provedor), autorizado())
      return (await r.json()) as EstadoChave
    },
    async guardarChave(provedor, chave) {
      await pedir(buscar, '/api/ia/chave', comJson('PUT', { provedor, chave }))
    },
    async removerChave(provedor) {
      await pedir(buscar, daChave(provedor), autorizado({ method: 'DELETE' }))
    },
    async testarConexao(provedor) {
      const r = await pedir(buscar, '/api/ia/testar', comJson('POST', { provedor }))
      return resultadoDoTeste((await r.json()) as Parameters<typeof resultadoDoTeste>[0])
    },
    async aprender(id, { de, para }) {
      const r = await pedir(buscar, '/api/aprendizado', comJson('POST', { id, de, para }), false)
      return (await r.json()) as Aprendida
    },
    async desfazerAprendizado(variante) {
      // POST com o texto no corpo: a variante é texto ditado e não vai para
      // a URL.
      const r = await pedir(buscar, '/api/aprendizado/desfazer', comJson('POST', { variante }), false)
      return (await r.json()) as Aprendida
    },
    async aprendidas() {
      const r = await pedir(buscar, '/api/aprendizado', autorizado(), false)
      const { items } = (await r.json()) as { items?: Aprendida[] | null }
      return items ?? []
    },
    async termos() {
      const r = await pedir(buscar, '/api/termos', autorizado(), false)
      return daLista((await r.json()) as ListaDeTermos)
    },
    async adicionarTermo(pedido) {
      const r = await pedir(buscar, '/api/termos', comJson('POST', pedido), false)
      return daMudanca((await r.json()) as MudancaDeTermos)
    },
    async dicaDoTermo(termo, dica) {
      const r = await pedir(buscar, '/api/termos/dica', comJson('POST', { termo, dica }), false)
      return daMudanca((await r.json()) as MudancaDeTermos)
    },
    async removerTermo(termo) {
      const r = await pedir(buscar, '/api/termos/remover', comJson('POST', { termo }), false)
      return daMudanca((await r.json()) as MudancaDeTermos)
    },
    async nuvem() {
      const r = await pedir(buscar, '/api/nuvem', autorizado(), false)
      return (await r.json()) as EstadoNuvem
    },
    async conectarNuvem(url) {
      const r = await pedir(buscar, '/api/nuvem/conectar', comJson('POST', { url }), false)
      return (await r.json()) as EstadoNuvem
    },
    async conferirNuvem() {
      const r = await pedir(buscar, '/api/nuvem/conferir', autorizado({ method: 'POST' }), false)
      return (await r.json()) as EstadoNuvem
    },
    async desconectarNuvem() {
      const r = await pedir(buscar, '/api/nuvem/desconectar', autorizado({ method: 'POST' }), false)
      return (await r.json()) as EstadoNuvem
    },
    async enviarHistorico() {
      const r = await pedir(buscar, '/api/nuvem/enviar', autorizado({ method: 'POST' }), false)
      return (await r.json()) as EstadoNuvem
    },
    async esquecerNuvem() {
      const r = await pedir(buscar, '/api/nuvem/esquecer', autorizado({ method: 'POST' }), false)
      return (await r.json()) as EstadoNuvem
    },
    async retencao() {
      const r = await pedir(buscar, '/api/retencao', autorizado(), false)
      return (await r.json()) as EstadoRetencao
    },
    async ligarRetencao(previa) {
      const r = await pedir(buscar, '/api/retencao/ligar', comJson('POST', { previa }), false)
      return (await r.json()) as RetencaoLigada
    },
    async desligarRetencao() {
      const r = await pedir(buscar, '/api/retencao/desligar', autorizado({ method: 'POST' }), false)
      return (await r.json()) as EstadoRetencao
    },
    async apagarCopias() {
      const r = await pedir(buscar, '/api/retencao/copias/apagar', autorizado({ method: 'POST' }), false)
      return (await r.json()) as CopiasApagadas
    },
    async resolverConflito(id, versao, escolha) {
      const r = await pedir(buscar, doItem(id), comJson('PATCH', { base_version: versao, resolver_conflito: escolha }))
      return (await r.json()) as Ditado
    },
    async mudancas(sinal) {
      const r = await pedir(buscar, '/api/nuvem/mudancas', autorizado({ signal: sinal }), false)
      const { contador } = (await r.json()) as { contador?: unknown }
      // Um corpo que não é o contador não é uma mudança: a leitura falha, e a
      // seguinte tenta de novo.
      if (typeof contador !== 'number' || !Number.isFinite(contador)) throw new Error('o Agent não mandou o contador de mudanças')
      return contador
    },
    async microfone(sinal) {
      const r = await pedir(buscar, '/api/microfone', autorizado({ signal: sinal }), false)
      const { bloqueado, motivo } = (await r.json()) as { bloqueado?: unknown; motivo?: unknown }
      // Só um bloqueio que o Agent afirma conta: um corpo estranho não acende
      // aviso nenhum.
      return { bloqueado: bloqueado === true, motivo: typeof motivo === 'string' ? (motivo as MotivoDoBloqueio) : '' }
    },
    async abrirConfiguracoesDoMicrofone() {
      await pedir(buscar, '/api/microfone/configuracoes', autorizado({ method: 'POST' }), false)
    },
  }
}
