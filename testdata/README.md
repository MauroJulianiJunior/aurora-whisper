# Testdata

Fixtures, datasets e dados de teste para Aurora Whisper.

## Propósito

Este diretório contém:

- Fixtures de teste (JSON, SQL seeds, stubs)
- Dados de exemplo para desenvolvimento
- Configurações de teste
- Referências a datasets externos (com documentação de como obtê-los)

## Dataset de voz (Benchmark)

**Importante:** os arquivos de áudio do dataset de benchmark **não são versionados no git**.

O invariante AC-028 (privacidade) proíbe versionar dados de áudio pessoais. Em seu lugar:

- O dataset é obtido via script de download (Sprint 3+)
- Integridade é validada via hash SHA256
- Hash e metadados são incluídos no relatório de benchmark
- Consulte `docs/ai-spec/19-eval-plan.md` para detalhes completos

## Dataset de frases do Polish

`testdata/polish-dataset/frases.json` tem 88 frases **sintéticas** (nenhum dado real: nomes
genéricos, valores e datas inventados) para o benchmark do Polish (`cmd/polishbench`). Cada uma
traz `entrada` (o que o Smart entrega ao Polish), `esperado` (o Polish ideal, sob a ADR-016) e os
`termos` do dicionário presentes, **na grafia do dicionário** (o Smart os entrega assim; a
exceção é o "das" preposição da armadilha, e um teste garante). As 46 `reference` do manifest do ASR entram como a categoria
`ja_organizada`, em tempo de execução. O `esperado` de cada frase espera a revisão do Mauro. O
texto é versionado (não é áudio); o teste `cmd/polishbench/dataset_carga_test.go` procura CPF,
CNPJ, e-mail, telefone, endereço da web e chave.

## O historico.db do bin\ (F7c)

`testdata/historico-bin-0005/dados.sql` são os dados **inventados** (nenhum ditado de verdade) de
um `historico.db` como o do `bin\` do Mauro está antes da F7c: o esquema das migrations 0001 a 0005
(o teste as aplica a partir de `internal/storage/migrations`) e 221 ditados de todo tipo (lixeira,
mais de 6 meses, com acento e emoji, que falharam, sem metadados, um acima do teto da nuvem, um com
a data no futuro e três de outro dono), as preferências, a instalação revogada da F6, uma entrada
do dicionário antigo e uma correção. As datas são relativas ao momento do teste (`strftime` com
`'now'`). É a prova de que o envio inicial e o "Esquecer esta nuvem" não perdem nem mudam nada
(`internal/server/integration/sync_historico_de_verdade_e2e_test.go`, ADR-019, "Decisões da F7c").

## Estrutura esperada

```
testdata/
├── fixtures/           JSON, YAML, dados estruturados
├── sql/               Seeds de banco de dados
├── audio/             ⚠️ Diretório local, NUNCA commitado
│   ├── benchmark/     Dataset de áudio (obtido via script)
│   └── .gitignore     audio/ está bloqueado em .gitignore
└── README.md
```

## Referência

- `docs/ai-spec/19-eval-plan.md` — plano de avaliação (dataset, benchmark)
- `.gitignore` — `audio/` bloqueado para privacidade
