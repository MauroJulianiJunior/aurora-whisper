import { Fragment, type KeyboardEvent, type ReactNode, useEffect, useEffectEvent, useRef, useState } from 'react'
import { type Configuracoes as Config, Conflito, type Editaveis, type EstadoChave, type ModoTexto, type Provedor, type ResultadoTeste, problemaDe } from './api'
import type { Cliente } from './clienteLocal'
import {
  type Campo,
  bloqueioDoPolish,
  comSugestao,
  editaveis,
  iguais,
  mesclar,
  nomeDoProvedor,
  paraEnviar,
  pedemReinicio,
  segurarMax,
  segurarMin,
  segurarPasso,
  sugeridoNaConta,
  teclas,
} from './config'
import { AprendidoNoPainel } from './AprendidoNoPainel'
import { HistoricoNoPC } from './HistoricoNoPC'
import { NuvemNoPC } from './NuvemNoPC'
import { OndeCriarAChave } from './OndeCriarAChave'
import { paginasDeChave } from './paginasDeChave'
import { Termos } from './Termos'

interface Props {
  cliente: Cliente
  // visivel: a tela fica montada depois de aberta pela primeira vez, e
  // só se esconde — trocar de vista não leva o que a pessoa mudou e ainda
  // não salvou.
  visivel: boolean
  // aoPerderLigacao: sessão perdida ou Agent fora do ar. O Painel acende
  // a faixa do topo; esta tela fica como está.
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
  // aprendizado: sobe a cada aprendizado ou desfazer feito pelo aviso do
  // histórico. A seção "Aprendido no painel" lê a lista de novo.
  aprendizado: number
  // recarregarHistorico: a lista do Histórico lê de novo (os 6 meses apagaram
  // ditados, ADR-019, F10d).
  recarregarHistorico(): void
}

// erro.tentativa conta as cargas que falharam: a cada uma, o alerta nasce
// de novo e é anunciado de novo, mesmo que a mensagem seja a mesma.
// pronta.conflito: false, ou a nota do último 409 (ver
// recarregarNoConflito).
type Carga =
  | { tipo: 'carregando' }
  | { tipo: 'erro'; mensagem: string; tentativa: number }
  | { tipo: 'pronta'; salvo: Config; rascunho: Editaveis; conflito: false | 'mudou' | 'igual' }

// O estado da chave de um provedor: o que o Agent disse, ou 'falhou' — a
// pergunta não teve resposta. Ausente: ainda não perguntado.
type Chave = EstadoChave | 'falhou'

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

function semProvedor(c: Partial<Record<Provedor, Chave>>, p: Provedor): Partial<Record<Provedor, Chave>> {
  const resto = { ...c }
  delete resto[p]
  return resto
}

// O teste é um GET /v1/models: prova que o provedor aceitou a chave e
// listou modelos, não que ela gera texto — uma conta sem crédito também
// passa. A frase diz só o que foi medido (revisão de 29-set-2026).
function textoDoTeste(nome: string, modelos: string[]): string {
  if (modelos.length === 0) return `A ${nome} aceitou a chave, mas não listou nenhum modelo. Escreva o nome do modelo abaixo.`
  return `A ${nome} aceitou a chave e listou ${modelos.length} ${modelos.length === 1 ? 'modelo' : 'modelos'}.`
}

// Cada modo com uma frase do que ele faz de verdade
// (docs/ai-spec/16-ai-behavior-spec.md; internal/resolver).
//
// Literal: o Agent desliga a resolução depois do ASR, mas o vocabulário
// do dicionário continua indo para o reconhecimento (ASROptions.Vocabulary,
// em todos os modos). "Sem dicionário" prometia mais do que acontece; o
// que some são as trocas (revisão de 29-set-2026).
const modos: { valor: ModoTexto; nome: string; descricao: string }[] = [
  {
    valor: 'literal',
    nome: 'Literal',
    descricao: 'O texto entra como o reconhecimento de voz entregou, com a pontuação dele: sem as trocas do dicionário e sem correções.',
  },
  {
    valor: 'smart',
    nome: 'Smart',
    descricao: 'Aplica o seu dicionário e as correções que você fala (“não, quer dizer…”), sem reescrever a frase.',
  },
  {
    valor: 'polish',
    nome: 'Polish',
    // O Polish preserva e organiza (ADR-016): as mesmas palavras, com
    // pontuação, parágrafos e listas. Diz também para onde o texto vai e o
    // que acontece quando a IA falha, que é o que muda o que a pessoa
    // espera; o motivo de cada falha aparece na seção IA.
    descricao:
      'Organiza o texto com IA: pontuação, parágrafos e listas, sem trocar as suas palavras. Envia o texto ao provedor de IA escolhido abaixo. Se algo falhar, entra o texto do Smart.',
  },
]

const pendenteTexto = 'O valor salvo vale a partir do próximo início do Aurora Whisper.'

// ids junta os ids de aria-describedby que existem.
const ids = (...lista: (string | false)[]) => lista.filter(Boolean).join(' ') || undefined

export function Configuracoes({ cliente, visivel, aoPerderLigacao, avisar, aprendizado, recarregarHistorico }: Props) {
  const [carga, setCarga] = useState<Carga>({ tipo: 'carregando' })
  // nuvemConectada: o endereço da nuvem com o PC conectado (a Privacidade
  // diz para onde o texto vai); null desconectado.
  const [nuvemConectada, setNuvemConectada] = useState<string | null>(null)
  // seisMesesLigados: os 6 meses estão ligados no PC (a Privacidade diz que o
  // que completa 6 meses é apagado deste computador).
  const [seisMesesLigados, setSeisMesesLigados] = useState(false)
  const [salvando, setSalvando] = useState(false)
  const [erroSalvar, setErroSalvar] = useState<string | null>(null)
  // chaves: o estado da chave de cada provedor, como o Agent disse. Nunca
  // a chave.
  const [chaves, setChaves] = useState<Partial<Record<Provedor, Chave>>>({})
  // geracao: por provedor, quantas vezes esta tela guardou a chave. A
  // resposta de um GET /api/ia/chave perguntado numa geração anterior
  // chegou tarde e é descartada: sem isso, a pergunta feita ao carregar,
  // respondida depois do Salvar chave, deixava a tela em "Nenhuma chave
  // guardada." com a chave já guardada, e ninguém perguntava de novo
  // (revisão de 29-set-2026). noAr: a geração da pergunta no caminho, por
  // provedor — o efeito não pergunta duas vezes a mesma coisa.
  //
  // Remover não passa a geração: o botão só fica ligado com a chave
  // conhecida, e ela só é conhecida sem pergunta da geração atual no ar.
  // Uma pergunta velha que chegue depois já é de uma geração anterior.
  const geracao = useRef<Partial<Record<Provedor, number>>>({})
  const noAr = useRef<Partial<Record<Provedor, number>>>({})
  // A chave digitada não entra no estado do React: fica só no campo, que
  // é lido no Salvar chave e esvaziado em seguida. Aqui, só se há algo
  // digitado — para o botão.
  const campoChave = useRef<HTMLInputElement>(null)
  const [digitada, setDigitada] = useState(false)
  const [mexendoNaChave, setMexendoNaChave] = useState<'guardando' | 'removendo' | null>(null)
  const [erroChave, setErroChave] = useState<string | null>(null)
  // confirmarRemocao: o primeiro clique em Remover chave só pergunta. A
  // OpenAI e a Anthropic mostram a chave uma vez só: sem uma cópia, é
  // gerar outra.
  const [confirmarRemocao, setConfirmarRemocao] = useState(false)
  const [testando, setTestando] = useState(false)
  const [teste, setTeste] = useState<{ provedor: Provedor; resultado: ResultadoTeste } | null>(null)
  const [tentando, setTentando] = useState(false)
  // focarAoCarregar: o Tentar de novo some quando a carga dá certo, e o
  // foco iria para o <body>. Vai para o título da primeira seção.
  const focarAoCarregar = useRef(false)
  const tituloDitado = useRef<HTMLHeadingElement>(null)
  // O botão Salvar configurações: para onde o foco vai quando a última
  // linha de "Aprendido no painel" sai e a seção some com ela.
  const botaoSalvar = useRef<HTMLButtonElement>(null)

  // falhou: a ligação caiu (faixa do Painel) ou foi outro erro, que a
  // própria tela mostra onde aconteceu.
  function falhou(e: unknown, mostrar: (texto: string) => void) {
    if (problemaDe(e)) aoPerderLigacao(e)
    else mostrar(mensagem(e))
  }

  function carregou(c: Config) {
    setCarga({ tipo: 'pronta', salvo: c, rascunho: editaveis(c), conflito: false })
  }

  function naoCarregou(e: unknown) {
    if (problemaDe(e)) aoPerderLigacao(e)
    setCarga((c) => ({ tipo: 'erro', mensagem: mensagem(e), tentativa: c.tipo === 'erro' ? c.tentativa + 1 : 0 }))
  }

  const aoCarregar = useEffectEvent(carregou)
  const aoNaoCarregar = useEffectEvent(naoCarregou)
  useEffect(() => {
    let vivo = true
    cliente.configuracoes().then(
      (c) => vivo && aoCarregar(c),
      (e: unknown) => vivo && aoNaoCarregar(e),
    )
    return () => {
      vivo = false
    }
  }, [cliente])

  // O estado da chave do provedor à vista é perguntado sempre que não se
  // sabe: ao carregar, ao trocar de provedor, depois de guardar uma chave
  // e depois de um 409 (outra aba pode ter trocado o provedor, ou a
  // chave). Um lugar só, para nenhum caminho ficar em "Conferindo…" sem
  // pedido nenhum no ar (revisão de 29-set-2026).
  const provedorVisivel = carga.tipo === 'pronta' ? carga.rascunho.ai_provider : ''
  const perguntarChave = useEffectEvent((p: Provedor) => {
    const g = geracao.current[p] ?? 0
    const valendo = () => (geracao.current[p] ?? 0) === g
    noAr.current[p] = g
    cliente
      .chave(p)
      .then(
        (estado) => {
          if (valendo()) setChaves((c) => ({ ...c, [p]: estado }))
        },
        (e: unknown) => {
          if (!valendo()) return
          setChaves((c) => ({ ...c, [p]: 'falhou' }))
          falhou(e, (m) => setErroChave(`Não consegui ver se há uma chave guardada: ${m}`))
        },
      )
      .finally(() => {
        if (noAr.current[p] === g) delete noAr.current[p]
      })
  })
  // Depende de `chaves` inteiro, não só do estado à vista: guardar a
  // chave com a primeira pergunta ainda no ar não muda o estado à vista
  // (continua desconhecido), e a pergunta da geração nova precisa sair.
  useEffect(() => {
    const p = provedorVisivel
    if (p && chaves[p] === undefined && noAr.current[p] !== (geracao.current[p] ?? 0)) perguntarChave(p)
  }, [provedorVisivel, chaves])

  // reler: ao voltar a esta vista, ou a esta aba, sem nada por salvar, a
  // tela lê de novo. O autostart muda por fora (bandeja, Gerenciador de
  // Tarefas) sem subir a version: sem reler, a caixa mostrava o que já
  // não é verdade. Com algo por salvar, não relê — o 409 é que avisa do
  // que outra aba gravou. A resposta só entra se, ao chegar, ainda não há
  // nada por salvar e ela não é mais velha que o que a tela tem.
  const reler = useEffectEvent(() => {
    if (carga.tipo !== 'pronta' || salvando || !iguais(carga.rascunho, editaveis(carga.salvo))) return
    cliente.configuracoes().then(
      (atual) =>
        setCarga((c) =>
          c.tipo === 'pronta' && iguais(c.rascunho, editaveis(c.salvo)) && atual.version >= c.salvo.version
            ? { ...c, salvo: atual, rascunho: editaveis(atual) }
            : c,
        ),
      // Outro erro fica calado: a tela continua com o que tinha, e o
      // próximo Salvar diz o que houver.
      (e: unknown) => {
        if (problemaDe(e)) aoPerderLigacao(e)
      },
    )
  })
  useEffect(() => {
    if (!visivel) return
    reler()
    const aoFocar = () => reler()
    window.addEventListener('focus', aoFocar)
    return () => window.removeEventListener('focus', aoFocar)
  }, [visivel])

  useEffect(() => {
    if (carga.tipo !== 'pronta' || !focarAoCarregar.current) return
    focarAoCarregar.current = false
    tituloDitado.current?.focus()
  }, [carga.tipo])

  // O botão fica no lugar enquanto tenta: se ele sumisse, o foco cairia
  // no <body>.
  function tentarDeNovo() {
    if (tentando) return
    setTentando(true)
    focarAoCarregar.current = true
    cliente
      .configuracoes()
      .then(carregou, naoCarregou)
      .finally(() => setTentando(false))
  }

  if (carga.tipo !== 'pronta') {
    return (
      <div className="config" hidden={!visivel}>
        {carga.tipo === 'carregando' ? (
          <p className="estado atrasado">Carregando…</p>
        ) : (
          <div className="estado">
            {/* role=alert: a região "anuncio" do Painel fica calada nesta
                vista, e o erro precisa ser ouvido. */}
            <p role="alert" key={carga.tentativa}>
              Não consegui carregar as configurações: {carga.mensagem}
            </p>
            <button type="button" className="botao" aria-disabled={tentando || undefined} onClick={tentarDeNovo}>
              {tentando ? 'Tentando…' : 'Tentar de novo'}
            </button>
          </div>
        )}
      </div>
    )
  }

  const { salvo, rascunho, conflito } = carga
  const alterado = !iguais(rascunho, editaveis(salvo))
  const provedor = rascunho.ai_provider
  const chaveAqui = provedor ? chaves[provedor] : undefined
  const chave = chaveAqui === 'falhou' ? undefined : chaveAqui
  const bloqueio = bloqueioDoPolish(salvo, provedor, chave)
  const testeDaqui = provedor !== '' && teste?.provedor === provedor ? teste.resultado : null
  const modelos = testeDaqui?.ok ? testeDaqui.modelos : []
  // O modelo que o Agent sugere, se a conta o tem (senão, ""): marcado na
  // lista e dito na ajuda. Só sugestão.
  const sugerido = sugeridoNaConta(testeDaqui)
  const pendente = (campo: Campo) => salvo.reinicio_pendente.includes(campo)
  const idPendente = (campo: Campo) => pendente(campo) && `cfg-pendente-${campo}`

  function mudar(parcial: Partial<Editaveis>) {
    setErroSalvar(null)
    setCarga((c) => (c.tipo === 'pronta' ? { ...c, rascunho: { ...c.rascunho, ...parcial } } : c))
  }

  // Trocar de provedor limpa o modelo (o nome de um não vale no outro) —
  // e volta o salvo, se a pessoa voltar ao provedor salvo. O que foi
  // digitado no campo da chave era para o outro provedor: o campo remonta
  // vazio (key).
  function escolherProvedor(p: '' | Provedor) {
    mudar({ ai_provider: p, ai_model: p === salvo.ai_provider ? salvo.ai_model : '' })
    setDigitada(false)
    setErroChave(null)
    setConfirmarRemocao(false)
  }

  // conferirChave pergunta de novo, depois de uma pergunta que falhou. O
  // botão some no clique: o foco vai para o campo da chave, que o estado
  // descreve, em vez de cair no <body>.
  function conferirChave() {
    if (!provedor) return
    setErroChave(null)
    setChaves((c) => semProvedor(c, provedor))
    campoChave.current?.focus()
  }

  async function salvar() {
    if (!alterado || salvando) return
    const enviado = rascunho
    const antes = editaveis(salvo)
    setSalvando(true)
    setErroSalvar(null)
    try {
      const novo = await cliente.salvarConfiguracoes(salvo.version, paraEnviar(antes, enviado))
      // O que a pessoa mexeu enquanto o pedido ia e voltava fica.
      setCarga((c) => (c.tipo === 'pronta' ? { tipo: 'pronta', salvo: novo, rascunho: mesclar(enviado, c.rascunho, editaveis(novo)), conflito: false } : c))
      // "Parte delas" só se algo que ESTE salvamento mudou ficou para o
      // próximo início — não um pendente de antes.
      const salvas = pedemReinicio(antes, enviado, novo.reinicio_pendente)
        ? 'Configurações salvas. Parte delas vale a partir do próximo início do Aurora Whisper.'
        : 'Configurações salvas.'
      // A chave não vai neste Salvar: tem o botão dela. Quem colou a chave
      // e apertou só este precisa saber que ela não foi guardada.
      avisar(campoChave.current?.value.trim() ? `${salvas} A chave de API colada ainda não foi guardada: use Salvar chave.` : salvas)
    } catch (e) {
      if (e instanceof Conflito) await recarregarNoConflito()
      else falhou(e, setErroSalvar)
    } finally {
      setSalvando(false)
    }
  }

  // No 409, como no editor de ditados: a tela passa a se basear no que a
  // outra aba gravou, o que a pessoa mudou continua como ela deixou, e a
  // nota diz o que houve. O próximo Salvar é escolha consciente.
  async function recarregarNoConflito() {
    let atual: Config
    try {
      atual = await cliente.configuracoes()
    } catch (e) {
      return falhou(e, setErroSalvar)
    }
    // A nota é decidida aqui, uma vez: se ela mudasse a cada mexida, o
    // alerta seria anunciado de novo a cada uma. 'igual': o que a pessoa
    // mudou é o que a outra aba gravou, e o Salvar fica desligado — a nota
    // não pode mandar salvar de novo (revisão de 29-set-2026).
    setCarga((c) => {
      if (c.tipo !== 'pronta') return c
      const rascunho = mesclar(editaveis(c.salvo), c.rascunho, editaveis(atual))
      return { tipo: 'pronta', salvo: atual, rascunho, conflito: iguais(rascunho, editaveis(atual)) ? 'igual' : 'mudou' }
    })
    // A outra aba pode ter trocado o provedor, ou guardado e removido
    // chaves: o que se sabia delas é esquecido, e o efeito pergunta de
    // novo a do provedor à vista.
    setChaves({})
  }

  async function guardarChave() {
    const campo = campoChave.current
    if (!provedor || !campo || mexendoNaChave) return
    const nova = campo.value.trim()
    if (!nova) return
    setMexendoNaChave('guardando')
    setErroChave(null)
    try {
      await cliente.guardarChave(provedor, nova)
    } catch (e) {
      return falhou(e, (m) => setErroChave(`Não deu para guardar a chave: ${m}`))
    } finally {
      setMexendoNaChave(null)
    }
    // A chave sai da página aqui: o campo esvazia, e nada mais a guarda.
    campo.value = ''
    setDigitada(false)
    setTeste(null)
    avisar(`Chave da ${nomeDoProvedor[provedor]} guardada.`)
    // Geração nova: uma pergunta ainda no ar é de antes da chave.
    geracao.current[provedor] = (geracao.current[provedor] ?? 0) + 1
    setChaves((c) => semProvedor(c, provedor))
  }

  async function removerChave() {
    if (!provedor || !chave?.configurada || testando || mexendoNaChave) return
    if (!confirmarRemocao) return setConfirmarRemocao(true)
    setConfirmarRemocao(false)
    setMexendoNaChave('removendo')
    setErroChave(null)
    try {
      await cliente.removerChave(provedor)
    } catch (e) {
      return falhou(e, (m) => setErroChave(`Não deu para remover a chave: ${m}`))
    } finally {
      setMexendoNaChave(null)
    }
    setChaves((c) => ({ ...c, [provedor]: { provedor, configurada: false, final: '' } }))
    setTeste(null)
    avisar(`Chave da ${nomeDoProvedor[provedor]} removida.`)
  }

  async function testar() {
    if (!provedor || !chave?.configurada || testando || mexendoNaChave) return
    setTestando(true)
    setTeste(null)
    try {
      const resultado = await cliente.testarConexao(provedor)
      setTeste({ provedor, resultado })
      // Nenhum modelo escolhido e o sugerido está na conta: ele entra no
      // rascunho, e só nele. Salvar continua sendo da pessoa. A conferência
      // é sobre o rascunho de AGORA (a pessoa pode ter escrito um modelo ou
      // trocado de provedor enquanto o teste ia e voltava).
      const sugeridoAqui = sugeridoNaConta(resultado)
      if (sugeridoAqui) setCarga((c) => (c.tipo === 'pronta' ? { ...c, rascunho: comSugestao(c.rascunho, provedor, sugeridoAqui) } : c))
    } catch (e) {
      falhou(e, (m) => setTeste({ provedor, resultado: { ok: false, erro: `Não deu para testar: ${m}` } }))
    } finally {
      setTestando(false)
    }
  }

  function teclaNaChave(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== 'Enter') return
    e.preventDefault()
    void guardarChave()
  }

  const modoBloqueado = (m: ModoTexto) => m === 'polish' && bloqueio !== null
  const configurada = Boolean(chave?.configurada)
  // aria-disabled, e não disabled: um botão disabled perde o foco no
  // clique e o teclado cai no <body> (revisão de 29-set-2026). O clique
  // continua chegando, e cada função confere a mesma condição.
  const semGuardar = !digitada || mexendoNaChave !== null
  const semUsarChave = !configurada || testando || mexendoNaChave !== null

  return (
    <div className="config" hidden={!visivel}>
      <section className="secao" aria-labelledby="cfg-ditado">
        <h2 id="cfg-ditado" className="secao-titulo" ref={tituloDitado} tabIndex={-1}>
          Ditado
        </h2>

        <div className="ajuste">
          <p className="ajuste-nome">Tecla de ditado</p>
          <p className="teclas">
            {teclas(salvo.hotkey).map((t, i) => (
              <Fragment key={t}>
                {i > 0 && ' + '}
                <kbd>{t}</kbd>
              </Fragment>
            ))}
          </p>
          <p className="ajuda">Segure enquanto fala e solte para o texto entrar. Por enquanto, não dá para trocar.</p>
        </div>

        {/* O segundo gatilho (ADR-022, N9): as teclas são só leitura, como as
            do ditado; o que se muda é ligar e desligar. Um Agent sem o
            gatilho não manda note_hotkey, e a tela não o mostra. */}
        {salvo.note_hotkey && (
          <div className="ajuste">
            <label className="marcar">
              <input
                type="checkbox"
                checked={rascunho.note_trigger_enabled}
                aria-describedby={ids('cfg-nota-teclas', 'cfg-nota-ajuda', idPendente('note_trigger_enabled'))}
                onChange={(e) => mudar({ note_trigger_enabled: e.target.checked })}
              />
              Ditar para nota
            </label>
            <p className="teclas" id="cfg-nota-teclas">
              {teclas(salvo.note_hotkey).map((t, i) => (
                <Fragment key={t}>
                  {i > 0 && ' + '}
                  <kbd>{t}</kbd>
                </Fragment>
              ))}
            </p>
            <p className="ajuda" id="cfg-nota-ajuda">
              O texto vai para uma nota, não para o campo. Comece pelo nome da nota (“Lista de compras: leite e pão”) para acrescentar nela; sem um
              nome que case, nasce uma nota nova. Desligado, estas teclas ditam no campo, como as do ditado.
            </p>
            <Pendente campo="note_trigger_enabled" ativo={pendente('note_trigger_enabled')} />
          </div>
        )}

        <div className="ajuste">
          <label className="ajuste-nome" htmlFor="cfg-segurar">
            Segurar por
          </label>
          <div className="regua">
            <input
              id="cfg-segurar"
              type="range"
              min={segurarMin}
              max={segurarMax}
              step={segurarPasso}
              value={rascunho.hold_threshold_ms}
              aria-valuetext={`${rascunho.hold_threshold_ms} milissegundos`}
              aria-describedby={ids('cfg-segurar-ajuda', idPendente('hold_threshold_ms'))}
              onChange={(e) => mudar({ hold_threshold_ms: Number(e.target.value) })}
            />
            <output className="valor" htmlFor="cfg-segurar">
              {rascunho.hold_threshold_ms} ms
            </output>
          </div>
          {/* O limiar decide se o ditado vale; não é quando o microfone
              começa a captar (o Agent marca o áudio já no aperto, e é ali
              que liga o microfone — ADR-015). No modo padrão, sob demanda,
              não há trecho de antes das teclas (pre-roll 0); os 300 ms de
              antes existem só no "sempre pronto". A ajuda diz o que a
              pessoa percebe: o aviso e o bip (revisão de 29-set-2026). */}
          <p className="ajuda" id="cfg-segurar-ajuda">
            Quanto tempo segurar as teclas para o ditado valer: é quando o aviso aparece e o bip toca, se estiver ligado. Soltar antes disso
            descarta o ditado.
          </p>
          <Pendente campo="hold_threshold_ms" ativo={pendente('hold_threshold_ms')} />
        </div>

        <div className="ajuste">
          <label className="marcar">
            <input
              type="checkbox"
              checked={rascunho.beep_enabled}
              aria-describedby={ids(idPendente('beep_enabled'))}
              onChange={(e) => mudar({ beep_enabled: e.target.checked })}
            />
            Bip ao começar a gravar
          </label>
          <Pendente campo="beep_enabled" ativo={pendente('beep_enabled')} />
        </div>

        <div className="ajuste">
          <label className="marcar">
            <input
              type="checkbox"
              checked={rascunho.microphone_always_ready}
              aria-describedby={ids('cfg-microfone-ajuda', idPendente('microphone_always_ready'))}
              onChange={(e) => mudar({ microphone_always_ready: e.target.checked })}
            />
            Microfone sempre pronto
          </label>
          <p className="ajuda" id="cfg-microfone-ajuda">
            Ligado, o microfone fica aberto o tempo todo e dá para falar já ao apertar as teclas. Desligado, ele só abre durante o ditado: fale
            depois do bip.
          </p>
          <Pendente campo="microphone_always_ready" ativo={pendente('microphone_always_ready')} />
        </div>
      </section>

      <section className="secao" aria-labelledby="cfg-texto">
        <h2 id="cfg-texto" className="secao-titulo">
          Texto
        </h2>
        <div className="modos" role="radiogroup" aria-labelledby="cfg-texto">
          {modos.map((m) => {
            const bloqueado = modoBloqueado(m.valor)
            return (
              <div key={m.valor} className="modo">
                <input
                  id={`cfg-modo-${m.valor}`}
                  type="radio"
                  name="cfg-modo"
                  value={m.valor}
                  checked={rascunho.text_mode === m.valor}
                  disabled={bloqueado}
                  aria-describedby={ids(`cfg-modo-${m.valor}-ajuda`, bloqueado && 'cfg-modo-bloqueio', idPendente('text_mode'))}
                  onChange={() => mudar({ text_mode: m.valor })}
                />
                <label htmlFor={`cfg-modo-${m.valor}`}>
                  {m.nome}
                  {m.valor === 'smart' && (
                    <>
                      {' '}
                      <span className="etiqueta">padrão</span>
                    </>
                  )}
                </label>
                <p className="ajuda" id={`cfg-modo-${m.valor}-ajuda`}>
                  {m.descricao}
                </p>
                {bloqueado && (
                  <p className="pendente" id="cfg-modo-bloqueio">
                    {bloqueio}
                  </p>
                )}
              </div>
            )
          })}
        </div>
        <Pendente campo="text_mode" ativo={pendente('text_mode')} />
      </section>

      <section className="secao" aria-labelledby="cfg-ia">
        <h2 id="cfg-ia" className="secao-titulo">
          IA
        </h2>
        <p className="secao-intro">
          {salvo.polish_disponivel
            ? 'Só o modo Polish usa a IA, e ele é opcional: o ditado funciona sem chave.'
            : 'Por enquanto, nada usa a IA: ela serve ao modo Polish, que chega na próxima versão. Dá para deixar tudo pronto.'}
        </p>
        {/* Por que o último Polish não agiu — só o motivo, nunca o texto. É
            informação, não alarme: sem cor de risco, sem role=alert. A
            região existe sempre, para um leitor de tela anunciar o motivo
            que entrar (um leitor só anuncia o que muda numa região que já
            estava na página). */}
        <p className="ajuda" role="status" id="cfg-polish-motivo">
          {salvo.polish_ultimo_motivo ?? ''}
        </p>

        <div className="ajuste">
          <label className="ajuste-nome" htmlFor="cfg-provedor">
            Provedor
          </label>
          <select
            id="cfg-provedor"
            className="entrada"
            value={provedor}
            aria-describedby={ids(idPendente('ai_provider'))}
            onChange={(e) => escolherProvedor(e.target.value as '' | Provedor)}
          >
            <option value="">Nenhum</option>
            <option value="openai">{nomeDoProvedor.openai}</option>
            <option value="anthropic">{nomeDoProvedor.anthropic}</option>
          </select>
          <Pendente campo="ai_provider" ativo={pendente('ai_provider')} />
        </div>

        {provedor && (
          <>
            <div className="ajuste">
              <label className="ajuste-nome" htmlFor="cfg-chave">
                Chave de API da {nomeDoProvedor[provedor]}
              </label>
              <p className="estado-chave" id="cfg-chave-estado">
                {chaveAqui === 'falhou' ? (
                  'Não sei se há uma chave guardada.'
                ) : !chave ? (
                  'Conferindo se há uma chave guardada…'
                ) : chave.configurada ? (
                  <>
                    Guardada: <span className="final">••••{chave.final}</span>
                  </>
                ) : (
                  'Nenhuma chave guardada.'
                )}
              </p>
              <div className="com-botao">
                <input
                  key={provedor}
                  ref={campoChave}
                  id="cfg-chave"
                  className="entrada"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={configurada ? 'Cole uma chave nova' : 'Cole a chave aqui'}
                  aria-describedby="cfg-chave-estado cfg-chave-onde cfg-chave-ajuda"
                  onInput={(e) => setDigitada(e.currentTarget.value.trim() !== '')}
                  onKeyDown={teclaNaChave}
                />
                <button type="button" className="botao" aria-disabled={semGuardar || undefined} onClick={() => void guardarChave()}>
                  {mexendoNaChave === 'guardando' ? 'Salvando…' : 'Salvar chave'}
                </button>
              </div>
              <OndeCriarAChave pagina={paginasDeChave[provedor]} paraQue="o Aurora Whisper" id="cfg-chave-onde" />
              <p className="ajuda" id="cfg-chave-ajuda">
                Ao salvar, a chave vai direto para o Gerenciador de Credenciais do Windows e não volta para esta página.
              </p>
              {erroChave && (
                <p className="nota" role="alert">
                  {erroChave}
                </p>
              )}
              <div className="acoes">
                {chaveAqui === 'falhou' && (
                  <button type="button" className="botao" onClick={conferirChave}>
                    Conferir de novo
                  </button>
                )}
                <button type="button" className="botao" aria-disabled={semUsarChave || undefined} onClick={() => void testar()}>
                  {testando ? 'Testando…' : 'Testar conexão'}
                </button>
                <button
                  type="button"
                  className="botao"
                  aria-disabled={semUsarChave || undefined}
                  onClick={() => void removerChave()}
                  onBlur={() => setConfirmarRemocao(false)}
                >
                  {mexendoNaChave === 'removendo' ? 'Removendo…' : confirmarRemocao ? 'Remover mesmo?' : 'Remover chave'}
                </button>
              </div>
              {/* A região existe sempre: um leitor de tela só anuncia o
                  que entra numa região que já estava na página. */}
              <p className="ajuda" role="status">
                {confirmarRemocao
                  ? `Sem uma cópia da chave, será preciso gerar outra no site da ${nomeDoProvedor[provedor]}. Para remover, aperte de novo.`
                  : testeDaqui?.ok
                    ? textoDoTeste(nomeDoProvedor[provedor], testeDaqui.modelos)
                    : ''}
              </p>
              {testeDaqui && !testeDaqui.ok && (
                <p className="nota" role="alert">
                  {testeDaqui.erro}
                </p>
              )}
            </div>

            <div className="ajuste">
              <label className="ajuste-nome" htmlFor="cfg-modelo">
                Modelo
              </label>
              <input
                id="cfg-modelo"
                className="entrada"
                list="cfg-modelos"
                autoComplete="off"
                spellCheck={false}
                value={rascunho.ai_model}
                aria-describedby={ids('cfg-modelo-ajuda', idPendente('ai_model'))}
                onChange={(e) => mudar({ ai_model: e.target.value })}
              />
              <datalist id="cfg-modelos">
                {/* label: o sugerido leva "(sugerido)". Os navegadores
                    divergem sobre o label de uma opção de datalist (uns
                    mostram o valor e o label, outros só o label): por isso
                    ele repete o nome, e serve aos dois. */}
                {modelos.map((m) => (
                  <option key={m} value={m} label={m === sugerido ? `${m} (sugerido)` : undefined} />
                ))}
              </datalist>
              <p className="ajuda" id="cfg-modelo-ajuda">
                {modelos.length > 0
                  ? 'Os modelos da sua conta aparecem ao digitar no campo. Também dá para escrever outro nome.'
                  : 'Teste a conexão para ver os modelos da sua conta, ou escreva o nome do modelo.'}
                {sugerido && ` Sugerido: ${sugerido}, o que se saiu melhor nos testes do Aurora Whisper.`}
              </p>
              <Pendente campo="ai_model" ativo={pendente('ai_model')} />
            </div>
          </>
        )}
      </section>

      <section className="secao" aria-labelledby="cfg-sistema">
        <h2 id="cfg-sistema" className="secao-titulo">
          Sistema
        </h2>
        <div className="ajuste">
          <label className="marcar">
            <input
              type="checkbox"
              checked={rascunho.autostart}
              disabled={salvo.autostart_indisponivel !== ''}
              aria-describedby={ids('cfg-autostart-ajuda', idPendente('autostart'))}
              onChange={(e) => mudar({ autostart: e.target.checked })}
            />
            Iniciar com o Windows
          </label>
          <p className="ajuda" id="cfg-autostart-ajuda">
            {salvo.autostart_indisponivel
              ? `Não dá para mudar nesta instalação: ${salvo.autostart_indisponivel}`
              : 'O Aurora Whisper abre sozinho, na bandeja, quando você entra no Windows.'}
          </p>
          <Pendente campo="autostart" ativo={pendente('autostart')} />
        </div>
      </section>

      <div className="salvar">
        {conflito && (
          <p className="nota" role="alert">
            {conflito === 'mudou'
              ? 'As configurações foram salvas em outra aba enquanto você mexia. O que você mudou continua aqui; o resto já é o que a outra aba salvou. Salve de novo para gravar.'
              : 'As configurações foram salvas em outra aba enquanto você mexia. A tela mostra o que ficou salvo, e não há nada diferente para salvar.'}
          </p>
        )}
        {erroSalvar && (
          <p className="nota" role="alert">
            Não deu para salvar: {erroSalvar}
          </p>
        )}
        <button
          ref={botaoSalvar}
          type="button"
          className="botao primario"
          aria-disabled={!alterado || salvando || undefined}
          onClick={() => void salvar()}
        >
          {salvando ? 'Salvando…' : 'Salvar configurações'}
        </button>
        {alterado && <span className="etiqueta forte">não salvo</span>}
      </div>

      {/* O Dicionário (ADR-020) e o Aprendido no painel ficam fora do
          formulário das configurações: os dois agem na hora, e o Salvar de
          cima não tem nada a ver com eles. O Aprendido só aparece com algo
          aprendido. */}
      <Termos cliente={cliente} visivel={visivel} aoPerderLigacao={aoPerderLigacao} avisar={avisar} />

      <AprendidoNoPainel
        cliente={cliente}
        visivel={visivel}
        versao={aprendizado}
        aoPerderLigacao={aoPerderLigacao}
        avisar={avisar}
        aoEsvaziar={() => botaoSalvar.current?.focus()}
      />

      {/* A nuvem (ADR-019, F6): age na hora, como o Dicionário, fora do
          Salvar configurações. */}
      <NuvemNoPC
        cliente={cliente}
        visivel={visivel}
        aoPerderLigacao={aoPerderLigacao}
        avisar={avisar}
        aoMudarEstado={(e) => setNuvemConectada(e.estado === 'conectado' && e.url ? e.url : null)}
      />

      {/* O Histórico (ADR-019, F10): os 6 meses no PC e as cópias do banco. Age na
          hora, fora do Salvar configurações, e só aparece quando há o que decidir. */}
      <HistoricoNoPC
        cliente={cliente}
        visivel={visivel}
        aoPerderLigacao={aoPerderLigacao}
        avisar={avisar}
        aoMudarEstado={(e) => setSeisMesesLigados(e.ligada)}
        recarregarHistorico={recarregarHistorico}
        aoEsvaziar={() => botaoSalvar.current?.focus()}
      />

      <section className="secao" aria-labelledby="cfg-privacidade">
        <h2 id="cfg-privacidade" className="secao-titulo">
          Privacidade
        </h2>
        <ul className="fatos">
          <li>O áudio não é salvo em lugar nenhum: nem neste computador, nem na nuvem.</li>
          <li>O texto das outras janelas não é guardado — nem no histórico, nem nos registros do programa.</li>
          <li>A chave de API fica no Gerenciador de Credenciais do Windows. Esta página nunca a recebe de volta: mostra só os quatro últimos caracteres.</li>
          {/* A frase aprovada do Polish (ADR-016; 08-ui-ux-spec.md e
              09-security-privacy.md): a única vez em que o texto ditado sai
              deste computador. */}
          <li>
            No modo Polish, o texto ditado (nunca o áudio nem o que está nas outras janelas) vai ao provedor de IA escolhido, com a sua chave; OpenAI e
            Anthropic podem guardá-lo por até 30 dias e dizem não usá-lo para treinar modelos.
          </li>
          {/* A separação em itens com IA (ADR-022, N5): é aviso, como a frase
              do Polish, e por isso aparece também com o Polish desligado. */}
          <li>Com o Polish ligado, separar em itens uma linha de uma nota (pelo Formatar) manda essa linha ao mesmo provedor.</li>
          {/* A frase da nuvem (ADR-019; pendente desde a F6, entra com o
              envio, F7c): só com o PC conectado. Desde a F8, as trocas
              aprendidas vão junto (06 e 09: a variante, o termo, a restrição e
              o domínio da troca do Aprender). */}
          {/* Os 6 meses ligados (F10): a regra que apaga o antigo deste PC. */}
          {seisMesesLigados && <li>O que completa 6 meses é apagado deste computador.</li>}
          {nuvemConectada && (
            <li>
              Com a nuvem conectada, o texto dos seus ditados, as suas notas e as trocas que você aprendeu vão para a sua nuvem, em{' '}
              <span className="email">{nuvemConectada}</span>. O áudio e o que está nas outras janelas nunca vão.
            </li>
          )}
        </ul>
      </section>
    </div>
  )
}

function Pendente({ campo, ativo }: { campo: Campo; ativo: boolean }) {
  if (!ativo) return null
  return (
    <p className="pendente" id={`cfg-pendente-${campo}`}>
      {pendenteTexto}
    </p>
  )
}
