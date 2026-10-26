// O ambiente do Painel no modo local (o PC): a Local API do Agent, as
// Configurações do PC, a faixa que manda abrir pela bandeja, o Aprender
// (ADR-017) e, desde a F7c, a escolha do selo (o mesmo componente da nuvem;
// a escolha sobe pela fila do sync). Só o build local chega aqui (App.tsx).

import type { Ambiente } from './ambiente'
import { efeitoDeAprender, perguntaDeSugestao, resultadoDeAprender, resultadoDeDesfazer } from './aprendizado'
import type { Cliente } from './clienteLocal'
import { Configuracoes } from './Configuracoes'
import { AvisoDoMicrofone } from './AvisoDoMicrofone'
import { FaixaLocal } from './FaixaLocal'
import { Notas } from './notas/Notas'

export function ambienteLocal(cliente: Cliente): Ambiente {
  return {
    cliente,
    // Criado uma vez, como a tela de Configurações: o estado do aviso (o último
    // que o Agent disse) fica entre um render e outro do Painel.
    AvisoDoMicrofone: (props) => <AvisoDoMicrofone cliente={cliente} {...props} />,
    // Um componente por ambiente, criado uma vez: o estado da tela (o que a
    // pessoa mudou e não salvou) fica entre um render e outro.
    Configuracoes: (props) => <Configuracoes cliente={cliente} {...props} />,
    Faixa: FaixaLocal,
    // As notas moram no Agent (ADR-022): a aba só existe no PC.
    Notas: (props) => <Notas cliente={cliente} {...props} />,
    textos: {
      // O primeiro uso: o ditado funciona sem chave nenhuma, e a chave (a da IA,
      // nas Configurações) só serve ao Polish, que é opcional. A tecla é a de
      // fábrica, que por enquanto não se troca (Configurações > Ditado).
      vazio: 'Segure Ctrl+Win, fale e solte, em qualquer aplicativo. Funciona sem chave: ela só serve ao Polish, que é opcional.',
      outroLugar: 'em outra aba',
      semResposta: 'o Agent não respondeu',
    },
    resolverConflito: (id, versao, escolha) => cliente.resolverConflito(id, versao, escolha),
    // O ditado do celular e as edições feitas lá chegam sozinhos (ADR-021, S).
    mudancasDoSync: (sinal) => cliente.mudancas(sinal),
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
