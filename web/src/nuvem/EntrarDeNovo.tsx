// Entrar de novo sem perder o texto (ADR-019, "Decisões da F4"; decisão do
// Mauro, 05-out-2026: "Guarda até entrar de novo"). A sessão acabou com um
// texto não salvo na tela: o Painel fica, com o editor e o texto, e a faixa
// do topo leva a pessoa a entrar de novo numa janela à parte (entrar.ts diz
// por quê). O texto fica só na memória da página, nunca no aparelho.
//
// Aqui estão as duas pontas: a faixa, na aba do texto, e a janela de entrar,
// que é o próprio app aberto em ?janela=entrar (vai ao Google) e
// ?janela=entrou (avisa e se fecha).

import { useCallback, useEffect, useRef, useState } from 'react'
import { type Buscador, SemSessao } from '../api'
import { type ClienteNuvem, type Eu, criarClienteNuvem } from './cliente'
import {
  type ModoDaJanela,
  type Reentrada,
  enderecoDaJanela,
  fraseDoLogin,
  nomeDoCanal,
  useLoginDeTeste,
  useVoltaDoCache,
  voltaDaJanela,
} from './entrar'
import { Tela } from './Telas'

interface PropsDaFaixaDaSessao {
  reentrada: Reentrada
  // textoNaoSalvo: o que o Copiar leva (todos os rascunhos não salvos).
  textoNaoSalvo: string
  // aoEntrarDeNovo: a mesma conta voltou; o Painel tira a faixa.
  aoEntrarDeNovo(): void
}

// FaixaDaSessao: "Sua sessão terminou. Seu texto continua aqui; entre de novo
// para salvar.", com Entrar de novo (a janela à parte) e, no servidor de
// teste, a conta de teste (um fetch, sem sair da página). Enquanto ela está
// na tela, o portão fica fechado: o Painel não fala com a nuvem. Ela sai
// quando o GET /api/v1/me diz que a mesma conta voltou: na volta à aba (o
// foco, a aba à vista de novo), no aviso da janela, ou depois da conta de
// teste.
//
// A janela bloqueada: a faixa diz isso, oferece Copiar o texto (pela área de
// transferência, no clique da pessoa) e entrar nesta aba, que leva a página
// ao Google e perde o texto.
//
// A rede de segurança: com texto na faixa, sair da página (recarregar, o
// puxar para atualizar do Android, fechar a aba, ou um navegador que abre a
// janela de entrar na mesma aba) faz o navegador perguntar antes
// (beforeunload; o diálogo é dele, sem texto nosso). O Entrar nesta aba é a
// exceção: a pessoa escolheu perder o texto, e a faixa disse antes.
export function FaixaDaSessao({ reentrada, textoNaoSalvo, aoEntrarDeNovo }: PropsDaFaixaDaSessao) {
  const { cliente, eu, portao, host, abrirJanela, irPara } = reentrada
  const teste = useLoginDeTeste(cliente, host, true)
  const [bloqueada, setBloqueada] = useState(false)
  const [outraConta, setOutraConta] = useState(false)
  const [copia, setCopia] = useState<'feita' | 'falhou' | null>(null)
  const [indo, setIndo] = useState<'teste' | 'aba' | null>(null)
  const [erro, setErro] = useState<string | null>(null)
  const botaoCopiar = useRef<HTMLButtonElement>(null)
  // deixarSair: a pessoa escolheu Entrar nesta aba; o beforeunload não
  // pergunta (e não deixaria o botão preso em "Abrindo o Google…" se ela
  // cancelasse o diálogo).
  const deixarSair = useRef(false)
  // temTexto: há o que perder. A faixa continua mesmo se a pessoa apagou o
  // texto ou o devolveu ao que estava salvo, e aí não há por que perguntar.
  const temTexto = textoNaoSalvo.trim() !== ''
  // ultima: só a resposta do último /me vale (o foco e a aba à vista chegam
  // juntos, e a conta de teste confere logo depois de entrar).
  const ultima = useRef(0)

  useEffect(() => {
    portao.fechar()
    return () => portao.abrir()
  }, [portao])

  useEffect(() => {
    if (!temTexto) return
    const perguntar = (e: BeforeUnloadEvent) => {
      if (deixarSair.current) return
      e.preventDefault()
      // Chrome antigo só pergunta com o returnValue.
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', perguntar)
    return () => window.removeEventListener('beforeunload', perguntar)
  }, [temTexto])

  // conferir: a mesma conta abre o portão e tira a faixa; outra conta fica
  // na faixa, com o portão fechado; sem sessão, fica como está (a janela
  // pode ter sido fechada sem entrar); sem rede, também, e a próxima volta à
  // aba confere de novo.
  const conferir = useCallback(async () => {
    const vez = ++ultima.current
    let agora: Eu
    try {
      agora = await cliente.eu()
    } catch (e) {
      if (vez === ultima.current && e instanceof SemSessao) setOutraConta(false)
      return
    }
    if (vez !== ultima.current) return
    if (agora.id !== eu.id) {
      setOutraConta(true)
      return
    }
    portao.abrir()
    aoEntrarDeNovo()
  }, [cliente, eu, portao, aoEntrarDeNovo])

  useEffect(() => {
    const voltou = () => {
      if (document.visibilityState !== 'hidden') void conferir()
    }
    window.addEventListener('focus', voltou)
    document.addEventListener('visibilitychange', voltou)
    const canal = typeof BroadcastChannel === 'function' ? new BroadcastChannel(nomeDoCanal) : null
    if (canal) canal.onmessage = () => void conferir()
    return () => {
      window.removeEventListener('focus', voltou)
      document.removeEventListener('visibilitychange', voltou)
      canal?.close()
    }
  }, [conferir])

  // Bloqueada, o Entrar de novo sai com o foco: o foco vai para o Copiar,
  // que é o passo seguinte.
  useEffect(() => {
    if (bloqueada) botaoCopiar.current?.focus()
  }, [bloqueada])

  // Voltar ao cache de ida e volta (o Voltar do Google): o botão e a pergunta
  // do beforeunload voltam ao que eram.
  useVoltaDoCache(
    useCallback(() => {
      deixarSair.current = false
      setIndo(null)
    }, []),
  )

  function entrarDeNovo() {
    setErro(null)
    if (!abrirJanela(enderecoDaJanela)) setBloqueada(true)
  }

  async function entrarComTeste(conta: string) {
    if (indo) return
    setIndo('teste')
    setErro(null)
    try {
      await cliente.entrarComContaDeTeste(conta)
    } catch (e) {
      setIndo(null)
      setErro(fraseDoLogin(e))
      return
    }
    await conferir()
    setIndo(null)
  }

  // entrarNestaAba: a página vai ao Google e volta ao começo do app; o texto
  // que não foi copiado se perde (a linha da faixa disse antes).
  async function entrarNestaAba() {
    if (indo) return
    setIndo('aba')
    setErro(null)
    try {
      const url = await cliente.inicioDoGoogle()
      deixarSair.current = true
      irPara(url)
    } catch (e) {
      setIndo(null)
      setErro(fraseDoLogin(e))
    }
  }

  function copiar() {
    const area = navigator.clipboard
    if (!area || textoNaoSalvo.trim() === '') {
      setCopia('falhou')
      return
    }
    area.writeText(textoNaoSalvo).then(
      () => setCopia('feita'),
      () => setCopia('falhou'),
    )
  }

  let titulo = 'Sua sessão terminou.'
  let linha = 'Seu texto continua aqui; entre de novo para salvar.'
  if (bloqueada) {
    titulo = 'Seu navegador bloqueou a janela.'
    linha =
      copia === 'feita'
        ? 'Texto copiado. Agora pode entrar de novo nesta aba.'
        : copia === 'falhou'
          ? 'Não consegui copiar. Selecione o texto e copie antes de entrar de novo nesta aba.'
          : 'Copie o texto antes de entrar de novo nesta aba.'
  } else if (outraConta) {
    titulo = 'Você entrou em outra conta.'
    linha = `Este texto é de ${eu.email}: entre com ela para salvar.`
  }
  if (erro) linha = erro

  // compactavel: a frase de sempre ("Seu texto continua aqui; ..."). Com pouca
  // altura (o teclado aberto, o celular deitado), o CSS a esconde da vista,
  // e a faixa fica numa linha só, o título e o botão: ela é fixa no topo, e
  // o que a pessoa quer é continuar escrevendo o texto. O leitor de tela
  // continua ouvindo a frase inteira. As outras (janela bloqueada, outra
  // conta, erro) dizem o que fazer, e ficam.
  const compactavel = !bloqueada && !outraConta && !erro

  // Os botões esperam a resposta sobre o login de teste, como na tela Entrar.
  const comGoogle = teste !== undefined && teste?.google !== false
  return (
    <div className={compactavel ? 'faixa faixa-compactavel' : 'faixa'}>
      <p role="alert">
        <strong>{titulo}</strong> <span className="faixa-frase">{linha}</span>
      </p>
      <span className="faixa-acoes">
        {bloqueada ? (
          <>
            <button ref={botaoCopiar} type="button" className="aviso-acao" onClick={copiar}>
              Copiar o texto
            </button>
            <button type="button" className="aviso-acao" aria-disabled={indo !== null || undefined} onClick={() => void entrarNestaAba()}>
              {indo === 'aba' ? 'Abrindo o Google…' : 'Entrar nesta aba'}
            </button>
          </>
        ) : (
          comGoogle && (
            <button type="button" className="aviso-acao" onClick={entrarDeNovo}>
              Entrar de novo
            </button>
          )
        )}
        {teste && (
          <button type="button" className="aviso-acao" aria-disabled={indo !== null || undefined} onClick={() => void entrarComTeste(teste.conta)}>
            {indo === 'teste' ? 'Entrando…' : 'Entrar com a conta de teste'}
          </button>
        )}
      </span>
    </div>
  )
}

interface PropsDaJanela {
  modo: ModoDaJanela
  buscar: Buscador
  // trocarPara: ir ao Google sem deixar esta página no histórico da janela
  // (o Voltar do Google não volta a pedir outro login).
  trocarPara(url: string): void
  // avisar e fechar: a volta do Google (entrar.ts).
  avisar(): void
  fechar(): void
}

// JanelaDeEntrar: o app aberto na janela à parte. Não mostra nada da conta e
// não carrega o histórico.
export function JanelaDeEntrar({ modo, buscar, trocarPara, avisar, fechar }: PropsDaJanela) {
  const [cliente] = useState<ClienteNuvem>(() => criarClienteNuvem(buscar))
  if (modo === 'entrou') return <Entrou avisar={avisar} fechar={fechar} />
  return <IndoAoGoogle cliente={cliente} trocarPara={trocarPara} />
}

// Entrou: o Google devolveu a janela com a sessão nova. Avisa as outras abas e
// se fecha; se o navegador não deixar fechar (o window.close() é ignorado, ou
// a página abriu na mesma aba), a tela diz o que fazer e deixa um caminho de
// volta ao app: o link, que não custa nada quando a janela se fecha sozinha.
function Entrou({ avisar, fechar }: Pick<PropsDaJanela, 'avisar' | 'fechar'>) {
  useEffect(() => {
    avisar()
    fechar()
  }, [avisar, fechar])
  return (
    <Tela titulo="Você entrou de novo." linha="Pode fechar esta janela e voltar ao seu texto.">
      <div className="tela-acoes">
        <a className="botao" href="/">
          Abrir o histórico
        </a>
      </div>
    </Tela>
  )
}

// IndoAoGoogle: pede o login com a volta para esta janela e segue para o
// Google. Um pedido por tentativa (o StrictMode do desenvolvimento roda o
// efeito duas vezes, e dois __Host-pre trocados derrubariam o login).
function IndoAoGoogle({ cliente, trocarPara }: { cliente: ClienteNuvem; trocarPara(url: string): void }) {
  const [vez, setVez] = useState(0)
  const [erro, setErro] = useState<string | null>(null)
  const pedida = useRef(-1)
  useEffect(() => {
    if (pedida.current === vez) return
    pedida.current = vez
    cliente.inicioDoGoogle(voltaDaJanela).then(trocarPara, (e: unknown) => setErro(fraseDoLogin(e)))
  }, [cliente, trocarPara, vez])

  return (
    <Tela titulo="Entrar de novo" linha={erro ? 'Seu texto continua na outra janela.' : 'Abrindo o Google…'}>
      {erro && (
        <div className="tela-acoes">
          <button
            type="button"
            className="botao primario"
            onClick={() => {
              setErro(null)
              setVez((v) => v + 1)
            }}
          >
            Tentar de novo
          </button>
          <p className="nota" role="alert">
            {erro}
          </p>
        </div>
      )}
    </Tela>
  )
}
