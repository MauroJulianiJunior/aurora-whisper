import { useRef } from 'react'
import { BotaoComMenu } from './BotaoComMenu'
import { Icone } from './Icone'

interface Props {
  // celular: no app do celular o ícone abre o menu "Tirar foto" / "Escolher da
  // galeria" (N10b, cena 5 da prévia); no PC ele abre direto a escolha de arquivos
  // do Windows.
  celular: boolean
  // desabilitado: a nota na lixeira, e o celular sem rede (a imagem só se anexa com
  // rede, e o ícone fica apagado até ela voltar).
  desabilitado: boolean
  // rotulo e titulo são o nome e a dica do botão (a barra da nota e o "Criar uma nota…").
  rotulo: string
  titulo: string
  // rotuloDoCampo é o nome do campo escondido que escolhe os arquivos.
  rotuloDoCampo: string
  aoEscolher(arquivos: File[]): void
}

// BotaoDeImagem é o ícone de imagem da barra da nota e do "Criar uma nota…". Os
// campos de arquivo ficam FORA do menu: o menu se desmonta ao fechar, e o campo
// que sai da página antes da escolha perderia o que a pessoa escolheu.
export function BotaoDeImagem({ celular, desabilitado, rotulo, titulo, rotuloDoCampo, aoEscolher }: Props) {
  const galeria = useRef<HTMLInputElement>(null)
  const camera = useRef<HTMLInputElement>(null)
  const escolheu = (campo: HTMLInputElement) => {
    const arquivos = Array.from(campo.files ?? [])
    campo.value = ''
    if (arquivos.length > 0) aoEscolher(arquivos)
  }
  return (
    <>
      {celular ? (
        <BotaoComMenu icone="imagem" rotulo={rotulo} titulo={titulo} desabilitado={desabilitado} classe="de-imagem">
          {(fechar) => (
            <>
              <button
                type="button"
                onClick={() => {
                  fechar()
                  camera.current?.click()
                }}
              >
                <Icone nome="camera" />
                Tirar foto
              </button>
              <button
                type="button"
                onClick={() => {
                  fechar()
                  galeria.current?.click()
                }}
              >
                <Icone nome="imagem" />
                Escolher da galeria
              </button>
            </>
          )}
        </BotaoComMenu>
      ) : (
        <button type="button" className="icone-botao" aria-label={rotulo} title={titulo} disabled={desabilitado} onClick={() => galeria.current?.click()}>
          <Icone nome="imagem" />
        </button>
      )}
      {/* A escolha de arquivos do sistema, aberta pelo clique de quem usa. */}
      <input
        ref={galeria}
        type="file"
        accept="image/*"
        multiple
        hidden
        tabIndex={-1}
        aria-label={rotuloDoCampo}
        onChange={(e) => escolheu(e.currentTarget)}
      />
      {celular && (
        <input
          ref={camera}
          type="file"
          accept="image/*"
          capture="environment"
          hidden
          tabIndex={-1}
          aria-label="Foto tirada agora"
          onChange={(e) => escolheu(e.currentTarget)}
        />
      )}
    </>
  )
}
