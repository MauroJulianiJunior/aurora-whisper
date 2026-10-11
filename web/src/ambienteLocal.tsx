// O ambiente do Painel no modo local (o PC): a Local API do Agent, as
// Configurações do PC, a faixa que manda abrir pela bandeja, o Aprender
// (ADR-017) e, desde a F7c, a escolha do selo (o mesmo componente da nuvem;
// a escolha sobe pela fila do sync). Só o build local chega aqui (App.tsx).

import type { Ambiente } from './ambiente'
import { efeitoDeAprender, perguntaDeSugestao, resultadoDeAprender, resultadoDeDesfazer } from './aprendizado'
import type { Cliente } from './clienteLocal'
import { Configuracoes } from './Configuracoes'
import { FaixaLocal } from './FaixaLocal'

export function ambienteLocal(cliente: Cliente): Ambiente {
  return {
    cliente,
    // Um componente por ambiente, criado uma vez: o estado da tela (o que a
    // pessoa mudou e não salvou) fica entre um render e outro.
    Configuracoes: (props) => <Configuracoes cliente={cliente} {...props} />,
    Faixa: FaixaLocal,
    textos: {
      vazio: 'Segure a tecla de ditado e fale em qualquer aplicativo. O que você ditar aparece aqui.',
      outroLugar: 'em outra aba',
      semResposta: 'o Agent não respondeu',
    },
    resolverConflito: (id, versao, escolha) => cliente.resolverConflito(id, versao, escolha),
    aprendiz: {
      pergunta: perguntaDeSugestao,
      efeito: efeitoDeAprender,
      aprender: (id, sugestao) => cliente.aprender(id, sugestao),
      desfazer: (variante) => cliente.desfazerAprendizado(variante),
      resultadoDeAprender,
      resultadoDeDesfazer,
    },
  }
}
