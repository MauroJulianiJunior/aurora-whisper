import type { PropsDaFaixa } from './ambiente'

// A faixa do topo no modo local: a ligação com o Agent caiu. O resto da tela
// continua no lugar, com os editores abertos, para a pessoa copiar o que
// escreveu. Só texto: a Aurora pequena que ia aqui chamava mais atenção que a
// mensagem, num alerta que o design system pinta de preto e vermelho — saiu
// por decisão do Mauro (29-set-2026).
export function FaixaLocal({ problema, aoTentarDeNovo }: PropsDaFaixa) {
  if (problema === 'sem-sessao') {
    return (
      <div className="faixa" role="alert">
        <p>
          <strong>Esta aba perdeu o acesso ao Aurora Whisper.</strong> Abra o painel de novo pela bandeja, em <strong>Abrir Aurora Whisper</strong>. O
          que você escreveu continua aqui para copiar.
        </p>
      </div>
    )
  }
  // Fechar e reabrir o Agent não traz esta aba de volta: cada início usa
  // outra porta e outra chave. "Tentar de novo" fica para a falha
  // passageira (revisão de 29-set-2026: a tela mandava tentar de novo
  // justamente no caso em que isso nunca funciona).
  return (
    <div className="faixa" role="alert">
      <p>
        <strong>O Aurora Whisper não está respondendo.</strong> Se ele foi fechado ou reiniciado, esta aba não volta a funcionar — a cada início ele
        muda de endereço e de chave. Abra o painel de novo pela bandeja, em <strong>Abrir Aurora Whisper</strong>. O que você escreveu continua aqui
        para copiar.
      </p>
      <button type="button" className="aviso-acao" onClick={aoTentarDeNovo}>
        Tentar de novo
      </button>
    </div>
  )
}
