// Os tipos das notas (ADR-022; docs/ai-spec/06-api-contract.md, "Notas e
// marcadores") e o contrato do cliente. Só tipos e constantes: nada aqui fala
// com a rede, para o app do celular (N8) poder trazer o próprio cliente da
// nuvem e usar as mesmas telas.

// As oito cores (storage.CoresDaNota). A cor é um nome, não um hex: cada tema
// tem o seu tom (index.css, --n-*).
export const CORES = ['amarelo', 'laranja', 'vermelho', 'rosa', 'roxo', 'azul', 'verde', 'cinza'] as const
export type CorDaNota = (typeof CORES)[number]

export interface Nota {
  id: string
  titulo: string
  corpo: string
  // cor: sempre presente no contrato; null é sem cor.
  cor: CorDaNota | null
  fixada: boolean
  arquivada: boolean
  // marcadores: ids, na ordem do nome do marcador.
  marcadores: string[]
  ditado_de_origem?: string
  created_at: string
  updated_at: string
  deleted_at?: string
  version: number
}

export interface Marcador {
  id: string
  nome: string
  created_at: string
  updated_at: string
  version: number
}

// VistaDasNotas é o parâmetro ?vista= da lista: a tela principal, o Arquivo e a
// lixeira das notas.
export type VistaDasNotas = 'ativas' | 'arquivo' | 'lixeira'

export interface FiltroDasNotas {
  vista: VistaDasNotas
  // marcador: o id; só as notas que o têm.
  marcador?: string
  busca?: string
}

export interface NovaNota {
  titulo?: string
  corpo?: string
  cor?: CorDaNota | null
  fixada?: boolean
  arquivada?: boolean
  // token: o da caixa "Criar uma nota" (AlvoDoFoco): os ditados feitos dentro
  // dela passam a ser os da nota que nasce.
  token?: string
}

// AlvoDoFoco é onde está o cursor (ADR-022, "Ditado feito dentro da nota"): uma
// nota que já existe, ou a caixa "Criar uma nota", que ainda não tem id e se
// identifica por um token que o painel inventa a cada abertura.
export type AlvoDoFoco = { nota: string } | { token: string }

// MudancaDaNota é o PATCH: só vai o que mudou. cor null tira a cor.
export interface MudancaDaNota {
  titulo?: string
  corpo?: string
  cor?: CorDaNota | null
  fixada?: boolean
  arquivada?: boolean
}

// DesfeitoOVirarNota é a resposta do Desfazer do "Virar nota" (N3): o ditado que
// voltou ao histórico e se a nota saiu (estava intacta) ou ficou (foi editada).
export interface DesfeitoOVirarNota {
  ditado_id: string
  nota_apagada: boolean
}

// ClienteDeNotas é o que a aba Notas usa. O cliente local (clienteLocal.ts) o
// implementa sobre a Local API; os erros vêm como os do api.ts (Conflito para
// a versão desatualizada e para a nota que foi para a lixeira em outra aba).
export interface ClienteDeNotas {
  listarNotas(filtro: FiltroDasNotas, sinal?: AbortSignal): Promise<Nota[]>
  obterNota(id: string): Promise<Nota>
  criarNota(pedido: NovaNota): Promise<Nota>
  editarNota(id: string, versao: number, mudanca: MudancaDaNota): Promise<Nota>
  moverNotaParaLixeira(id: string): Promise<void>
  restaurarNota(id: string): Promise<Nota>
  apagarNotaDeVez(id: string): Promise<void>
  listarMarcadores(sinal?: AbortSignal): Promise<Marcador[]>
  // Os marcadores (N4). criarMarcador recusa o nome já usado (sem diferenciar
  // maiúscula nem acento) com ErroDaApi de codigo NAME_IN_USE; renomearMarcador
  // pede a versão do marcador e recusa do mesmo jeito (Conflito: versão velha);
  // apagarMarcador tira o marcador das notas e não apaga nota.
  criarMarcador(nome: string): Promise<Marcador>
  renomearMarcador(id: string, versao: number, nome: string): Promise<Marcador>
  apagarMarcador(id: string): Promise<void>
  // marcarNota e desmarcarNota põem e tiram um marcador de uma nota (idempotentes) e
  // devolvem a nota como ficou: a versão dela sobe, e é a nova que vale (Conflito:
  // a nota está na lixeira).
  marcarNota(notaId: string, marcadorId: string): Promise<Nota>
  desmarcarNota(notaId: string, marcadorId: string): Promise<Nota>
  // definirFoco avisa o Agent de que o cursor está neste alvo (PUT
  // /api/notas/foco); o aviso vence sozinho em 60 s, então se renova. limparFoco
  // o tira (DELETE): com alvo, só se o aviso ainda for aquele; descartar (só com
  // token) devolve ao histórico os ditados feitos na caixa que não virou nota.
  definirFoco(alvo: AlvoDoFoco): Promise<void>
  limparFoco(alvo?: AlvoDoFoco, descartar?: boolean): Promise<void>
}

// NOMES_DAS_CORES é o rótulo de cada cor, para o menu e para o leitor de tela.
export const NOMES_DAS_CORES: Record<CorDaNota, string> = {
  amarelo: 'Amarelo',
  laranja: 'Laranja',
  vermelho: 'Vermelho',
  rosa: 'Rosa',
  roxo: 'Roxo',
  azul: 'Azul',
  verde: 'Verde',
  cinza: 'Cinza',
}
