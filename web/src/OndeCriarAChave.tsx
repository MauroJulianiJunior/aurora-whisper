// "Onde consigo a chave": a linha que fica embaixo de cada campo de chave, no
// painel do PC e no app da nuvem. Diz onde criar a chave (o link oficial do
// provedor, escrito por extenso: quem lê vê para onde vai), que a chave é da
// própria pessoa e o uso é cobrado na conta dela, e a recomendação de uma chave
// só para isto, com limite de gasto. Uma frase só, sem tutorial.
//
// "Só para isto" é dito por quem chama (paraQue), porque muda de um lado para o
// outro, e cada build leva só a sua palavra: no PC, "o Aurora Whisper" (a chave
// fica no Gerenciador de Credenciais, e só o Agent a usa); no app da nuvem, "o
// celular" (a chave fica cifrada no servidor, onde quem o administra consegue
// decifrá-la: não deve ser a que o PC usa; REQ-053, ADR-021).
//
// O link abre em outra aba, sem passar a página de origem (noopener) nem o
// endereço dela (noreferrer): o painel do PC mora em 127.0.0.1 com uma porta que
// muda a cada início, e o do app, na nuvem da pessoa. O "(abre em outra aba)" é
// só para o leitor de tela; o espaço antes dele fica dentro do trecho escondido,
// para o sublinhado do link não passar do endereço e encostar no ponto.

import type { PaginaDaChave } from './paginasDeChave'

interface Props {
  pagina: PaginaDaChave
  // paraQue completa "use uma só para …": "o Aurora Whisper" no PC, "o celular"
  // no app da nuvem.
  paraQue: string
  // id liga a linha ao campo da chave (aria-describedby).
  id?: string
}

export function OndeCriarAChave({ pagina, paraQue, id }: Props) {
  return (
    <p className="ajuda" id={id}>
      Crie a chave em{' '}
      <a href={pagina.url} target="_blank" rel="noopener noreferrer">
        {pagina.url.replace(/^https:\/\//, '')}
        <span className="so-leitor"> (abre em outra aba)</span>
      </a>
      . Ela é sua e o uso é cobrado na sua conta da {pagina.nome}: use uma só para {paraQue}, com limite de gasto.
    </p>
  )
}
