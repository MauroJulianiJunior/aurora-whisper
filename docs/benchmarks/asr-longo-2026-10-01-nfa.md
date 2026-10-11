# Benchmark de áudio longo — 2026-10-01 13:06

Gerado por `cmd/asrlongo` (incidente de 01-out-2026, `docs/defeito-ditado-longo.md`).

- Servidor: `thirdparty/whisper/Release/whisper-server.exe`
- Modelo: `models/quant/small-q4_0.bin`
- Threads: 12
- Vocabulário (campo prompt): 140 runas, do dicionário
- Dataset: hash `4c094a1c61f7cd8cfc0e2430cec7b9b3ca7686ea0f841e512175c6470a08c7d1`
- Áudios longos: 30s, 60s, 90s, 2 por duração, amostras do dataset em ordem com 500ms de silêncio entre elas
- Fala curta das variantes intercaladas: `geral-01`
- Comando: `asrlongo medir -manifest testdata/asr-dataset/manifest-normalizado.json -servidor thirdparty/whisper/Release/whisper-server.exe -modelo models/quant/small-q4_0.bin -dicionario testdata/asr-dataset/dicionario.json -variantes por-duracao,por-duracao-intercalada,nfa-por-duracao,nfa-por-duracao-intercalada -out docs/benchmarks/asr-longo-2026-10-01-nfa.md`

## Variantes do áudio longo

- `por-duracao`: audio_ctx por requisição (768 até 12 s, janela inteira acima), servidor só com áudio longo
- `por-duracao-intercalada`: o mesmo, com uma fala curta antes de cada áudio longo (como no uso)
- `nfa-por-duracao`: por duração, sem flash attention (-nfa), servidor só com áudio longo
- `nfa-por-duracao-intercalada`: por duração, sem flash attention (-nfa), intercalada

## Resumo do áudio longo

| variante | áudios | falhas | WER médio | WER com a guarda | laços | tempo médio | razão tempo/áudio (média / máx) | estouraria o prazo |
|---|---|---|---|---|---|---|---|---|
| `por-duracao` | 6 | 0 | 0.283 | 0.283 | 0 | 5.24 s | 0.09 / 0.12 | 0 |
| `por-duracao-intercalada` | 6 | 0 | 0.308 | 0.308 | 0 | 4.85 s | 0.08 / 0.09 | 0 |
| `nfa-por-duracao` | 6 | 0 | 0.282 | 0.282 | 0 | 6.83 s | 0.11 / 0.13 | 0 |
| `nfa-por-duracao-intercalada` | 6 | 0 | 0.282 | 0.282 | 0 | 6.74 s | 0.11 / 0.12 | 0 |

## Por áudio

| áudio | duração | variante | audio_ctx | tempo | razão | WER | WER com a guarda | laços (palavras tiradas) | runas | erro |
|---|---|---|---|---|---|---|---|---|---|---|
| longo-30s-1 | 32.1 s | `por-duracao` | 0 | 3.78 s | 0.12 | 0.514 | 0.514 | 0 (0) | 393 | |
| longo-30s-2 | 36.0 s | `por-duracao` | 0 | 3.51 s | 0.10 | 0.173 | 0.173 | 0 (0) | 425 | |
| longo-60s-1 | 63.9 s | `por-duracao` | 0 | 5.20 s | 0.08 | 0.229 | 0.229 | 0 (0) | 712 | |
| longo-60s-2 | 60.7 s | `por-duracao` | 0 | 4.89 s | 0.08 | 0.243 | 0.243 | 0 (0) | 587 | |
| longo-90s-1 | 94.3 s | `por-duracao` | 0 | 6.96 s | 0.07 | 0.301 | 0.301 | 0 (0) | 1190 | |
| longo-90s-2 | 93.8 s | `por-duracao` | 0 | 7.12 s | 0.08 | 0.239 | 0.239 | 0 (0) | 988 | |
| longo-30s-1 | 32.1 s | `por-duracao-intercalada` | 0 | 2.82 s | 0.09 | 0.557 | 0.557 | 0 (0) | 385 | |
| longo-30s-2 | 36.0 s | `por-duracao-intercalada` | 0 | 3.07 s | 0.09 | 0.227 | 0.227 | 0 (0) | 414 | |
| longo-60s-1 | 63.9 s | `por-duracao-intercalada` | 0 | 4.98 s | 0.08 | 0.267 | 0.267 | 0 (0) | 714 | |
| longo-60s-2 | 60.7 s | `por-duracao-intercalada` | 0 | 4.64 s | 0.08 | 0.252 | 0.252 | 0 (0) | 587 | |
| longo-90s-1 | 94.3 s | `por-duracao-intercalada` | 0 | 6.89 s | 0.07 | 0.306 | 0.306 | 0 (0) | 1182 | |
| longo-90s-2 | 93.8 s | `por-duracao-intercalada` | 0 | 6.68 s | 0.07 | 0.239 | 0.239 | 0 (0) | 962 | |
| longo-30s-1 | 32.1 s | `nfa-por-duracao` | 0 | 4.21 s | 0.13 | 0.514 | 0.514 | 0 (0) | 393 | |
| longo-30s-2 | 36.0 s | `nfa-por-duracao` | 0 | 4.31 s | 0.12 | 0.200 | 0.200 | 0 (0) | 415 | |
| longo-60s-1 | 63.9 s | `nfa-por-duracao` | 0 | 6.81 s | 0.11 | 0.206 | 0.206 | 0 (0) | 713 | |
| longo-60s-2 | 60.7 s | `nfa-por-duracao` | 0 | 6.83 s | 0.11 | 0.243 | 0.243 | 0 (0) | 589 | |
| longo-90s-1 | 94.3 s | `nfa-por-duracao` | 0 | 9.56 s | 0.10 | 0.286 | 0.286 | 0 (0) | 1184 | |
| longo-90s-2 | 93.8 s | `nfa-por-duracao` | 0 | 9.27 s | 0.10 | 0.244 | 0.244 | 0 (0) | 988 | |
| longo-30s-1 | 32.1 s | `nfa-por-duracao-intercalada` | 0 | 3.95 s | 0.12 | 0.514 | 0.514 | 0 (0) | 393 | |
| longo-30s-2 | 36.0 s | `nfa-por-duracao-intercalada` | 0 | 4.34 s | 0.12 | 0.200 | 0.200 | 0 (0) | 415 | |
| longo-60s-1 | 63.9 s | `nfa-por-duracao-intercalada` | 0 | 7.23 s | 0.11 | 0.206 | 0.206 | 0 (0) | 713 | |
| longo-60s-2 | 60.7 s | `nfa-por-duracao-intercalada` | 0 | 6.60 s | 0.11 | 0.243 | 0.243 | 0 (0) | 589 | |
| longo-90s-1 | 94.3 s | `nfa-por-duracao-intercalada` | 0 | 9.21 s | 0.10 | 0.286 | 0.286 | 0 (0) | 1184 | |
| longo-90s-2 | 93.8 s | `nfa-por-duracao-intercalada` | 0 | 9.10 s | 0.10 | 0.244 | 0.244 | 0 (0) | 988 | |

## A fala curta antes muda o texto do áudio longo?

**Com flash attention** (`por-duracao` contra `por-duracao-intercalada`): 0 de 6 áudios com o texto igual; 6 diferentes.

O texto do áudio longo depende da requisição anterior. Ver "Como ler".

**Sem flash attention (`-nfa`)** (`nfa-por-duracao` contra `nfa-por-duracao-intercalada`): 6 de 6 áudios com o texto igual; 0 diferentes.

Nenhuma diferença: nesta medição, a requisição anterior não mudou o texto.


## Fala curta: o 768 por requisição contra o -ac 750 medido

As 46 amostras do dataset (todas abaixo de 12 s).

- `ac750`: -ac 750 na linha de comando (medido em 18-set-2026)
- `por-duracao`: audio_ctx 768 por requisição (toda amostra tem menos de 12 s)
- `nfa-por-duracao`: audio_ctx 768 por requisição, sem flash attention (-nfa)

| variante | amostras | falhas | WER médio | tempo p50 | tempo p95 | tempo máx |
|---|---|---|---|---|---|---|
| `ac750` | 46 | 0 | 0.272 | 0.60 s | 0.79 s | 1.90 s |
| `por-duracao` | 46 | 0 | 0.267 | 0.59 s | 0.70 s | 3.18 s |
| `nfa-por-duracao` | 46 | 0 | 0.270 | 0.76 s | 0.96 s | 3.35 s |

## Como ler

- **WER** é contra a referência do dataset (as frases concatenadas). "Com a guarda" é depois de
  `internal/asr/repeticao` tirar os laços, como o Agent faz antes de resolver.
- **Laços** conta os áudios em que a guarda achou repetição. Zero é o esperado.
- **Tempo** é o que o provider mediu (`ASRResult.ProcessingTime`). **Estouraria o prazo** conta os
  áudios em que o tempo passou do prazo que o Agent dá (`dictation.PrazoDeTranscricao`).
- A comparação "fala curta antes" mostra se o K/V que sobra da requisição curta (768) muda o texto
  da longa (janela inteira, lida arredondada para 1536 com a flash attention). Texto diferente com o
  mesmo áudio e os mesmos parâmetros quer dizer que muda; aí a saída medida é desligar a flash
  attention (`-nfa`), que custou 22% em 18-set-2026.
- **Razão tempo/áudio máxima** é medida com a máquina livre. O fator 0,75 do prazo é a pior razão do
  incidente (0,51, ainda com o -ac 750 e com o Meet aberto) com folga de 1,5×. Com a janela inteira o
  encoder custa mais por janela; se a razão máxima aqui passar de 0,5, o fator fica curto para a CPU
  disputada: rever o fator, ou medir uma vez com carga.
- **`prompt-fixo`** contra `por-duracao-intercalada`: as duas só diferem com vocabulário (`-dicionario`).
  WER menor no `prompt-fixo` quer dizer que o vocabulário saindo do contexto depois da primeira janela
  de 30 s pesa no ditado longo.
