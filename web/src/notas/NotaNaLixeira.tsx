import { useEffect, useMemo, useRef, useState } from 'react'
import { formatarCurta } from '../datas'
import { inicioDoCorpo, lerCorpo, nomeDaNota } from './corpo'
import { contaDeImagens, imagensDaNota, nomeComImagens } from './imagens'
import type { Nota } from './tipos'

interface Props {
  nota: Nota
  aoRestaurar(): void
  // aoExcluirDeVez devolve se excluiu: a linha só sai da tela quando sim.
  aoExcluirDeVez(): Promise<boolean>
}

// NotaNaLixeira é a linha da Lixeira das notas, a irmã da LinhaDaLixeira do
// histórico: não abre nada, só Restaurar e "Excluir definitivamente", que
// pergunta na própria linha, sem janela: "Excluir para sempre?", com o foco no
// Cancelar, que é o lado seguro. Nota na lixeira não se edita (a API recusa o
// PATCH): para mexer nela, restaura.
export function NotaNaLixeira({ nota, aoRestaurar, aoExcluirDeVez }: Props) {
  const linhas = useMemo(() => lerCorpo(nota.corpo), [nota.corpo])
  // As imagens vão e voltam da lixeira com a nota (N10): a linha diz quantas.
  const nImagens = imagensDaNota(nota).length
  const nomeDoTexto = nomeDaNota(nota.titulo, linhas)
  const nome = nomeComImagens(nomeDoTexto, nImagens)
  // A nota só com imagem já diz no nome quantas tem.
  const comConta = nImagens > 0 && nome === nomeDoTexto
  const resumo = [nota.titulo.trim() ? inicioDoCorpo(linhas, 200) : '', comConta ? contaDeImagens(nImagens) : ''].filter(Boolean).join(' · ')
  const quando = nota.deleted_at ?? nota.updated_at
  const [confirmando, setConfirmando] = useState(false)
  const [excluindo, setExcluindo] = useState(false)
  const botaoExcluir = useRef<HTMLButtonElement>(null)
  const botaoCancelar = useRef<HTMLButtonElement>(null)
  const voltarFoco = useRef(false)
  const idTexto = `texto-${nota.id}`
  const idPergunta = `excluir-${nota.id}`

  useEffect(() => {
    if (confirmando) botaoCancelar.current?.focus()
    else if (voltarFoco.current) {
      voltarFoco.current = false
      botaoExcluir.current?.focus()
    }
  }, [confirmando])

  function cancelar() {
    voltarFoco.current = true
    setConfirmando(false)
  }

  async function excluir() {
    if (excluindo) return
    setExcluindo(true)
    if (!(await aoExcluirDeVez())) setExcluindo(false)
  }

  return (
    <li className="item nota-na-lixeira" data-nota={nota.id} data-cor={nota.cor ?? undefined}>
      <div className="linha estatica">
        <time className="hora" dateTime={quando}>
          {formatarCurta(quando)}
        </time>
        <span className="texto-e-meta" id={idTexto}>
          <span className="nota-linha-titulo">{nome}</span>
          {resumo && <span className="nota-linha-inicio">{resumo}</span>}
        </span>
        <span className="acoes-linha">
          <button type="button" className="botao" data-linha aria-describedby={idTexto} onClick={aoRestaurar}>
            Restaurar
          </button>
          {!confirmando && (
            <button ref={botaoExcluir} type="button" className="botao" aria-describedby={idTexto} onClick={() => setConfirmando(true)}>
              Excluir definitivamente
            </button>
          )}
        </span>
      </div>
      {confirmando && (
        <div
          className="confirmar-linha"
          role="group"
          aria-labelledby={idPergunta}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault()
              cancelar()
            }
          }}
        >
          <p id={idPergunta}>Excluir para sempre? Não dá para desfazer.</p>
          <button type="button" className="botao risco" aria-disabled={excluindo || undefined} aria-describedby={idTexto} onClick={() => void excluir()}>
            {excluindo ? 'Excluindo…' : 'Excluir'}
          </button>
          <button ref={botaoCancelar} type="button" className="botao" onClick={cancelar}>
            Cancelar
          </button>
        </div>
      )}
    </li>
  )
}
