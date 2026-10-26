import { describe, expect, it } from 'vitest'
import { paraWhatsApp } from './whatsapp'

// Cada caso: nome, título, corpo (como a nota o guarda) e o texto que vai ao WhatsApp.
const casos: [string, string, string, string][] = [
  ['título e parágrafo', 'Recado', 'Ligar para a Fernanda.', '*Recado*\n\nLigar para a Fernanda.'],
  ['sem título, o corpo começa direto', '', 'Ligar para a Fernanda.', 'Ligar para a Fernanda.'],
  ['título de espaços não conta', '   ', 'Texto', 'Texto'],
  ['só o título, sem linha em branco sobrando', 'Recado', '', '*Recado*'],
  ['nota vazia', '', '', ''],
  ['só espaços e linhas vazias', ' ', '\n\n', ''],
  ['marcador vira •', '', '- leite\n- café', '• leite\n• café'],
  ['numerada recomeça em cada sequência', '', '1. um\n2. dois\ntexto\n1. outro\n2. mais', '1. um\n2. dois\ntexto\n1. outro\n2. mais'],
  ['numerada renumera quando a gravada estava fora de ordem', '', '3. um\n9. dois', '1. um\n2. dois'],
  ['lista de marcar: ☐ aberto e ✅ marcado', 'Compras', '- [ ] leite\n- [ ] café\n- [x] pão', '*Compras*\n\n☐ leite\n☐ café\n✅ pão'],
  ['só itens marcados', '', '- [x] pão\n- [X] manteiga', '✅ pão\n✅ manteiga'],
  [
    'seção vira negrito e tem linha em branco antes',
    'Reunião',
    'abertura\n# Pauta\n- DAS\n- ISS\n# Dúvidas\nprazo?',
    '*Reunião*\n\nabertura\n\n*Pauta*\n• DAS\n• ISS\n\n*Dúvidas*\nprazo?',
  ],
  ['seção logo depois do título não ganha linha dupla', 'Reunião', '# Pauta\n- DAS', '*Reunião*\n\n*Pauta*\n• DAS'],
  ['seção na primeira linha de nota sem título', '', '# Pauta\n- DAS', '*Pauta*\n• DAS'],
  ['linhas em branco do corpo se juntam numa só', '', 'um\n\n\n\ndois', 'um\n\ndois'],
  ['linha em branco no começo e no fim saem', '', '\n\num\n\n', 'um'],
  ['item e seção vazios saem', '', '- \n- [ ] \n# \n1. \nfim', 'fim'],
  ['asterisco no título sai (fecharia o negrito)', 'a*b*c', 'texto', '*abc*\n\ntexto'],
  ['título só de asteriscos some', '***', 'texto', 'texto'],
  ['asterisco na seção sai', '', '# *Urgente* hoje\nx', '*Urgente hoje*\nx'],
  ['asterisco no corpo fica: já é WhatsApp', '', 'é *urgente*\n- item *forte*\n- [ ] 2 * 3', 'é *urgente*\n• item *forte*\n☐ 2 * 3'],
  ['quebra de linha do Windows', '', 'um\r\n- dois\r\n- [x] tres', 'um\n• dois\n✅ tres'],
  ['espaços sobrando no item e no fim do parágrafo', '', '-   leite  \nrecado   ', '• leite\nrecado'],
  [
    'tudo junto',
    'Sábado',
    '# Mercado\n- [ ] leite\n- [x] pão\n\n# Casa\n1. varrer\n2. regar\nobs *importante*',
    '*Sábado*\n\n*Mercado*\n☐ leite\n✅ pão\n\n*Casa*\n1. varrer\n2. regar\nobs *importante*',
  ],
]

describe('paraWhatsApp', () => {
  it.each(casos)('%s', (_, titulo, corpo, esperado) => {
    expect(paraWhatsApp(titulo, corpo)).toBe(esperado)
  })

  it('não termina nem começa com quebra de linha', () => {
    for (const [, titulo, corpo] of casos) {
      const t = paraWhatsApp(titulo, corpo)
      expect(t).toBe(t.trim())
    }
  })
})
