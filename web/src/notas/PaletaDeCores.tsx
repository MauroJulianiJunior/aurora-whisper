import { CORES, type CorDaNota, NOMES_DAS_CORES } from './tipos'

interface Props {
  atual: CorDaNota | null
  aoEscolher(cor: CorDaNota | null): void
}

// PaletaDeCores é o menu "Cor": sem cor e as oito do ADR-022 (E5), cada uma
// um círculo no tom suave do tema (o mesmo do fundo do cartão).
export function PaletaDeCores({ atual, aoEscolher }: Props) {
  return (
    <>
      <div className="titulo-menu">COR</div>
      <div className="cores">
        <button type="button" className="cor nenhuma" aria-label="Sem cor" title="Sem cor" aria-pressed={atual === null} onClick={() => aoEscolher(null)} />
        {CORES.map((c) => (
          <button
            key={c}
            type="button"
            className="cor"
            data-cor={c}
            aria-label={NOMES_DAS_CORES[c]}
            title={NOMES_DAS_CORES[c]}
            aria-pressed={atual === c}
            onClick={() => aoEscolher(c)}
          />
        ))}
      </div>
    </>
  )
}
