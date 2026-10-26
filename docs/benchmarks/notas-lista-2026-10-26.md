# Benchmark da separação em itens das notas — 2026-10-26

- Comando: `-gastar -rodadas 3 -modelos openai:gpt-6-luna -notas-lista`
- Prompt: `notas-lista@1.0.0`, hash `7665f39e`
- Dataset: `testdata/polish-dataset/notas-lista.json`, 21 trechos (categoria `notas-lista`), sha256 `4f4f74dc3b0c2efafd7b0c02b04e2a14fa8e983cfb3b23fd8f670418741b4d34`
- Rodadas: 3; gasto da rodada: US$ 0,0054

O caminho medido é o da API local (`resolver.SepararItens`): a IA, as guardas e, quando a IA não vale, a regra. "Fora do trecho" é o item que não é pedaço do trecho e tem de ser **0**.

## Resumo

| Modelo | Medidas | IA valeu | Regra | Nada separou | Fallback | F1 dos itens | F1 só IA | Listas exatas | Fora do trecho | p50 | p95 | Acima do prazo | Custo |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `openai:gpt-6-luna` | 63 | 48 | 0 | 15 | 23,8% | 1,000 | 1,000 | 100,0% | 0 | 1,37 s | 2,48 s | 0 | US$ 0,0054 |

## Recusas e erros

"Um item" é o modelo dizendo que o trecho não é lista (esperado nos casos `um_item` e `injecao` de uma frase só).

| Modelo | Recusas da guarda | Um item | Erros do provedor ou da rede |
| --- | ---: | ---: | ---: |
| `openai:gpt-6-luna` | — | 15 | — |

## Listas exatas por tipo de trecho

| Modelo | virgula | sem_virgula | numero | unidade | sigla | um_item | injecao |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `openai:gpt-6-luna` | 9/9 | 15/15 | 9/9 | 3/3 | 9/9 | 12/12 | 6/6 |

## O que não saiu como o esperado

### `openai:gpt-6-luna`

Nada: toda lista saiu como a esperada.

