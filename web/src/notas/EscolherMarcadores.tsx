import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react'
import { acharPorNome, dobrar, limparNome, MAX_NOME_DO_MARCADOR } from './marcadores'
import type { Marcador } from './tipos'

interface Props {
  // marcadores: todos, na ordem do nome; marcados: os ids que a nota tem.
  marcadores: readonly Marcador[]
  marcados: readonly string[]
  // aoAlternar marca ou desmarca na hora (a caixinha). Quem chama grava e traz a nota
  // nova: o que vale como marcado é o que volta em `marcados`.
  aoAlternar(marcador: Marcador, marcar: boolean): void
  // aoCriar cria o marcador e o põe na nota. Devolve a frase de erro, ou null se deu certo.
  aoCriar(nome: string): Promise<string | null>
}

// EscolherMarcadores é o menu do botão de marcador da nota e do cartão (ADR-022,
// "O marcador"): um campo de busca em cima e, embaixo, uma caixinha por marcador
// (marca e desmarca na hora). Com um nome que ainda não existe, a última linha é
// "Criar marcador". É o padrão de caixa de combinação com lista de várias
// escolhas do ARIA: o foco fica no campo, e as setas movem a linha ativa
// (aria-activedescendant); Enter alterna a ativa, Esc fecha (quem fecha é o
// BotaoComMenu) e o clique do mouse não tira o foco do campo.
export function EscolherMarcadores({ marcadores, marcados, aoAlternar, aoCriar }: Props) {
  const [texto, setTexto] = useState('')
  const [ativo, setAtivo] = useState(0)
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  const campo = useRef<HTMLInputElement>(null)
  const prefixo = useId()

  useEffect(() => {
    campo.current?.focus()
  }, [])

  const chave = dobrar(limparNome(texto))
  const visiveis = chave === '' ? marcadores : marcadores.filter((m) => dobrar(m.nome).includes(chave))
  const nomeNovo = limparNome(texto)
  const podeCriar = nomeNovo !== '' && !acharPorNome(marcadores, nomeNovo)
  // As linhas, na ordem da tela: os marcadores que casam e, por último, o "Criar".
  const linhas: ({ tipo: 'marcador'; marcador: Marcador } | { tipo: 'criar'; nome: string })[] = [
    ...visiveis.map((marcador) => ({ tipo: 'marcador' as const, marcador })),
    ...(podeCriar ? [{ tipo: 'criar' as const, nome: nomeNovo }] : []),
  ]
  const indice = Math.min(ativo, Math.max(linhas.length - 1, 0))
  const idDa = (i: number) => `${prefixo}-${i}`

  async function usar(i: number) {
    const linha = linhas[i]
    if (!linha || ocupado) return
    if (linha.tipo === 'marcador') {
      aoAlternar(linha.marcador, !marcados.includes(linha.marcador.id))
      return
    }
    setOcupado(true)
    const falha = await aoCriar(linha.nome)
    setOcupado(false)
    if (falha) return setErro(falha)
    setErro('')
    setTexto('')
    setAtivo(0)
    campo.current?.focus()
  }

  function aoTeclar(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (linhas.length === 0) return
      setAtivo((indice + (e.key === 'ArrowDown' ? 1 : linhas.length - 1)) % linhas.length)
    } else if (e.key === 'Home' || e.key === 'End') {
      // Home e End também movem o cursor do campo: só valem na lista com Ctrl.
      if (!e.ctrlKey) return
      e.preventDefault()
      setAtivo(e.key === 'Home' ? 0 : Math.max(linhas.length - 1, 0))
    } else if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()
      void usar(indice)
    }
  }

  return (
    <>
      <div className="campo-novo">
        <input
          ref={campo}
          type="text"
          role="combobox"
          aria-label="Buscar ou criar marcador"
          aria-expanded="true"
          aria-controls={`${prefixo}-lista`}
          aria-activedescendant={linhas.length > 0 ? idDa(indice) : undefined}
          autoComplete="off"
          spellCheck={false}
          maxLength={MAX_NOME_DO_MARCADOR}
          placeholder="Buscar ou criar marcador"
          value={texto}
          onChange={(e) => {
            setTexto(e.target.value)
            setAtivo(0)
            setErro('')
          }}
          onKeyDown={aoTeclar}
        />
      </div>
      {erro && (
        <p className="erro-campo" role="alert">
          {erro}
        </p>
      )}
      <ul id={`${prefixo}-lista`} className="opcoes" role="listbox" aria-multiselectable="true" aria-label="Marcadores">
        {linhas.map((linha, i) => {
          const classe = i === indice ? 'ativo' : undefined
          // O mousedown não pode tirar o foco do campo: o BotaoComMenu fecha o
          // menu quando o foco sai dele.
          const naoTirarOFoco = (e: { preventDefault(): void }) => e.preventDefault()
          if (linha.tipo === 'criar') {
            return (
              <li key="criar" id={idDa(i)} role="option" className={`criar-m ${classe ?? ''}`.trim()} onMouseDown={naoTirarOFoco} onClick={() => void usar(i)}>
                <span className="mais-m" aria-hidden="true">
                  +
                </span>
                <span>
                  Criar marcador “<strong>{linha.nome}</strong>”
                </span>
              </li>
            )
          }
          const marcado = marcados.includes(linha.marcador.id)
          return (
            <li key={linha.marcador.id} id={idDa(i)} role="option" aria-selected={marcado} className={classe} onMouseDown={naoTirarOFoco} onClick={() => void usar(i)}>
              <span className={marcado ? 'caixa feita' : 'caixa'} aria-hidden="true" />
              {linha.marcador.nome}
            </li>
          )
        })}
      </ul>
      {linhas.length === 0 && (
        <p className="vazio-menu">{marcadores.length === 0 ? 'Nenhum marcador ainda. Digite um nome para criar.' : 'Nenhum marcador com esse nome.'}</p>
      )}
    </>
  )
}
