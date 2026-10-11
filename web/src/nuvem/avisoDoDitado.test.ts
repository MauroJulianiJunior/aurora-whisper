import { describe, expect, it } from 'vitest'
import { VERSAO_DO_AVISO } from './avisoDoDitado'

// A versão do aviso é um inteiro a partir de 1 (a coluna notice_version da
// dictation_consents tem CHECK >= 1). O servidor tem a dele, e o teste do Go
// (TestVersaoDoAviso_AppEServidorConferem) lê o arquivo e prende os dois.
describe('VERSAO_DO_AVISO', () => {
  it('é um inteiro positivo', () => {
    expect(Number.isInteger(VERSAO_DO_AVISO)).toBe(true)
    expect(VERSAO_DO_AVISO).toBeGreaterThanOrEqual(1)
  })
})
