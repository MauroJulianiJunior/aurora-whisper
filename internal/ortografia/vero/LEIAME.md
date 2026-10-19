# VERO — o dicionário pt-BR do LibreOffice

Os arquivos desta pasta são do **VERO**, o verificador ortográfico do português do Brasil do
LibreOffice (Raimundo Santos Moura e colaboradores), **sem nenhuma alteração**:

| Arquivo | O que é | sha256 do original (antes do gzip) |
|---|---|---|
| `pt_BR.aff.gz` | regras de prefixo e sufixo (Hunspell) | `21d8ad2a769a60e17e2b5ea4ef11d4d593a58b9e2a82d642ef82d6a4c5523865` |
| `pt_BR.dic.gz` | 312.368 raízes | `a38bfb26b68ece2834e79fe83e48d5792652970ace12db89d1b9674bf9933183` |
| `README_pt_BR.txt` | o LEIA-ME do VERO, com autoria e licença | — |
| `MPL-2.0.txt` | o texto da Mozilla Public License 2.0 | — |

Origem: <https://github.com/LibreOffice/dictionaries/tree/master/pt_BR>, commit
`67032ee957631908098a72edf48dc81cac27e369` (baixado em 18-out-2026). O `.aff` e o `.dic` foram só
comprimidos com `gzip -9n`; o teste `TestArquivosSaoOsDoVero` confere que o conteúdo descomprimido
tem o sha256 acima.

## Licença

O VERO é distribuído sob a **LGPLv3** e a **Mozilla Public License 2.0**, à escolha de quem
redistribui (`README_pt_BR.txt`). O Aurora Whisper usa a **MPL 2.0** (`MPL-2.0.txt`): os arquivos
ficam aqui, na forma original, com este aviso; o resto do projeto continua sob a licença MIT. Ver
`THIRD_PARTY_NOTICES.md`, na raiz.

## Para que serve

`internal/ortografia` responde se uma palavra existe em português, para a correção de grafia da
guarda de palavras do Polish (ADR-016, emenda de 18-out-2026).

## Atualizar

Baixar o `pt_BR.aff` e o `pt_BR.dic` da origem, comprimir com `gzip -9n`, trocar os hashes daqui e
do teste, e rodar `go test ./internal/ortografia ./internal/resolver`.
