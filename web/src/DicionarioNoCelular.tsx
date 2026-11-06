import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { ErroDaApi, problemaDe } from './api'
import type { Cliente, EstadoDoDicionario } from './clienteLocal'
import { formatarHora } from './datas'

// "Levar o dicionário para o celular" (ADR-023, F): uma opção da seção Nuvem,
// só com o PC conectado (quem a monta é o NuvemNoPC, no bloco "conectado").
// Age na hora, como o resto da seção, e fica fora do Salvar configurações:
// ligar monta a cópia do dicionário e a manda à nuvem; desligar apaga a cópia
// de lá. Com a caixa desabilitada durante o pedido, a opção só muda quando o
// Agent responde, e uma falha deixa tudo como estava, com a frase dele.
//
// O estado vem do GET /api/nuvem/dicionario, que pergunta à nuvem quem é a
// fonte da cópia; por isso é lido quando a seção aparece e quando a aba volta,
// e não a cada poucos segundos.

interface Props {
  cliente: Cliente
  visivel: boolean
  aoPerderLigacao(e: unknown): void
  // aoGravar: a opção foi gravada (o POST deu certo). Ela mora nas
  // configurações do PC e sobe a version delas: a tela das Configurações
  // adota a version nova, para o próximo Salvar não bater num 409.
  aoGravar?(): void
}

const mensagem = (e: unknown) => (e instanceof Error ? e.message : String(e))

const termos = (n: number) => `${n.toLocaleString('pt-BR')} ${n === 1 ? 'termo' : 'termos'}`

const AJUDA = 'O celular passa a usar os termos deste PC. Editar continua aqui.'

// A linha de ajuda da opção, pelo estado: ligada, diz quantos termos e quando
// o PC os mandou; desligada com outro PC como fonte, diz de quem é o dicionário
// que o celular usa hoje. Sem o que a frase precisa (um Agent que não mandou o
// número, ou o nome do outro PC), volta à ajuda de sempre.
function ajudaDe(e: EstadoDoDicionario): string {
  if (e.ligado) {
    if (e.termos === undefined) return AJUDA
    return e.enviado_em ? `${termos(e.termos)}, enviados às ${formatarHora(e.enviado_em)}.` : `${termos(e.termos)}.`
  }
  if (e.fonte === 'outro' && e.aparelho) return `Hoje o celular usa o dicionário de ${e.aparelho}. Ligar aqui passa a usar o deste PC.`
  return AJUDA
}

export function DicionarioNoCelular({ cliente, visivel, aoPerderLigacao, aoGravar }: Props) {
  const [estado, setEstado] = useState<EstadoDoDicionario | null>(null)
  // semRota: o Agent não tem as rotas (de antes da ADR-023): a opção não existe.
  const [semRota, setSemRota] = useState(false)
  const [indo, setIndo] = useState(false)
  const [erro, setErro] = useState<{ texto: string; vez: number } | null>(null)
  const caixa = useRef<HTMLInputElement>(null)
  // devolverFoco: o foco estava na caixa quando o pedido saiu.
  const devolverFoco = useRef(false)
  // geracao sobe a cada clique: uma leitura que saiu antes dele e chega depois
  // é do estado de antes, e não entra por cima da resposta do POST.
  const geracao = useRef(0)
  const perderLigacao = useEffectEvent(aoPerderLigacao)

  const ler = useEffectEvent(() => {
    const g = geracao.current
    cliente.dicionarioDaNuvem().then(
      (e) => {
        if (geracao.current === g) {
          setEstado(e)
          setSemRota(false)
        }
      },
      (e: unknown) => {
        if (geracao.current !== g) return
        if (problemaDe(e)) perderLigacao(e)
        else if (e instanceof ErroDaApi && e.status === 404) setSemRota(true)
        else setErro((a) => ({ texto: mensagem(e), vez: (a?.vez ?? 0) + 1 }))
      },
    )
  })

  useEffect(() => {
    if (!visivel) return
    ler()
    const aoVoltar = () => {
      if (document.visibilityState === 'visible') ler()
    }
    document.addEventListener('visibilitychange', aoVoltar)
    return () => document.removeEventListener('visibilitychange', aoVoltar)
  }, [visivel])

  async function alternar(ligar: boolean) {
    if (indo || !estado) return
    geracao.current += 1
    devolverFoco.current = document.activeElement === caixa.current
    setIndo(true)
    setErro(null)
    try {
      setEstado(await cliente.levarDicionario(ligar))
      aoGravar?.()
    } catch (e) {
      if (problemaDe(e)) aoPerderLigacao(e)
      else setErro((a) => ({ texto: mensagem(e), vez: (a?.vez ?? 0) + 1 }))
    } finally {
      setIndo(false)
    }
  }

  // A caixa desabilitada tira o foco do teclado dela: ao terminar o pedido,
  // o foco volta, se estava nela e não foi para outro lugar.
  useEffect(() => {
    if (indo || !devolverFoco.current) return
    devolverFoco.current = false
    if (document.activeElement === document.body || document.activeElement === caixa.current) caixa.current?.focus()
  }, [indo])

  if (semRota) return null
  // Sem o estado ainda: a opção não aparece. Uma falha da primeira leitura fica dita.
  if (!estado) {
    return erro ? (
      <p className="nota" role="alert" key={erro.vez}>
        {erro.texto}
      </p>
    ) : null
  }

  return (
    <div className="ajuste">
      <label className="marcar">
        <input
          ref={caixa}
          type="checkbox"
          checked={estado.ligado}
          disabled={indo}
          aria-describedby="cfg-dicionario-ajuda"
          onChange={(e) => void alternar(e.target.checked)}
        />
        Levar o dicionário para o celular
      </label>
      <p className="ajuda" id="cfg-dicionario-ajuda">
        {ajudaDe(estado)}
      </p>
      {estado.aviso && (
        <p className="ajuda" role="status">
          {estado.aviso}
        </p>
      )}
      {erro && (
        <p className="nota" role="alert" key={erro.vez}>
          {erro.texto}
        </p>
      )}
    </div>
  )
}
