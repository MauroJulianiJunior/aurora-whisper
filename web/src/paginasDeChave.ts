// Onde a pessoa cria a chave de cada provedor. O Aurora Whisper não traz chave
// nenhuma: a de quem usa é dela, criada na conta dela e cobrada nela. Os
// endereços são os que a documentação oficial de cada provedor indica,
// conferidos em 13-out-2026; um provedor que mude de endereço muda aqui (e no
// teste, que prende o texto).
//
// Este arquivo é dos dois builds e só sabe dos provedores que os dois têm
// (OpenAI e Anthropic). A Groq é só do ditado do celular e mora em
// nuvem/ditar/paginasDeChave.ts: o painel do PC não a carrega (bundle.test.ts).
//
// - OpenAI: a página de chaves da plataforma (a documentação da OpenAI aponta
//   platform.openai.com/api-keys; ela só abre no navegador com a conta aberta).
// - Anthropic: o Console da Anthropic mudou de console.anthropic.com para
//   platform.claude.com; o endereço antigo redireciona para este.

export interface PaginaDaChave {
  // nome é o da conta onde o uso é cobrado.
  nome: string
  url: string
}

export const paginasDeChave = {
  openai: { nome: 'OpenAI', url: 'https://platform.openai.com/api-keys' },
  anthropic: { nome: 'Anthropic', url: 'https://platform.claude.com/settings/keys' },
} as const satisfies Record<string, PaginaDaChave>
