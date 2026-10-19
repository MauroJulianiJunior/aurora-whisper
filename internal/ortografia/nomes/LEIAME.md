# Listas de nomes de pessoa

Servem a uma pergunta só, a da guarda de siglas e termos do Polish (ADR-016, emenda de
19-out-2026): a palavra que o Whisper escreveu com maiúscula no meio da frase é nome de pessoa? Se
for, ela nunca perde a maiúscula, mesmo que o VERO a tenha como palavra comum ("joão", "silva",
"rosa"). As duas listas estão em minúscula e sem acento.

## `prenomes.txt`

Os **8.225 prenomes** com frequência de **1.000 ou mais** no Censo 2010 do IBGE.

- **Fonte:** IBGE/Censo 2010, dados tratados por Álvaro Justen/[Brasil.IO](https://brasil.io/)
  (dataset `genero-nomes`, arquivo `nomes.csv.gz`, baixado de
  <https://data.brasil.io/dataset/genero-nomes/nomes.csv.gz> em 19-out-2026).
- **Licença:** Creative Commons Attribution-ShareAlike 4.0 (CC BY-SA 4.0),
  <https://creativecommons.org/licenses/by-sa/4.0/>. A lista é uma adaptação dos dados (só a coluna
  `first_name`, filtrada por `frequency_total >= 1000`, em minúscula, sem repetição, em ordem) e é
  distribuída sob a mesma licença. O resto do projeto continua sob a licença MIT.
- **Como foi gerada:** de `nomes.csv.gz`, as linhas com `frequency_total` de 1.000 ou mais, a
  coluna `first_name` em minúscula, sem repetição, em ordem alfabética, uma por linha.

## `lugares.txt`

Os **2.784 nomes de uma palavra só** entre os 5.571 municípios e os 27 estados do Brasil ("recife",
"fortaleza", "belem", "goias"), em minúscula e sem acento. O nome de várias palavras ("Campo
Grande", "São Paulo") fica de fora: cada palavra dele é palavra comum, e protegê-las uma a uma
travaria "grande", "campo" e "santo" em todo ditado.

- **Fonte:** API de localidades do IBGE (<https://servicodados.ibge.gov.br/api/v1/localidades/municipios>
  e `/estados`), consultada em 19-out-2026. Dado público do IBGE, citado aqui como fonte.
- **Como foi gerada:** os nomes dos municípios e dos estados, em minúscula e sem acento, só os que
  são uma palavra (com hífen, vale: "embu-guacu"), sem repetição, em ordem.

## `sobrenomes.txt`

Os sobrenomes mais comuns no Brasil, **escritos à mão** para este projeto (o Censo não publica
sobrenome): "silva", "santos", "oliveira", "souza" e afins. Licença MIT, como o projeto.

## Atualizar

Para mudar o limite de frequência, gerar de novo `prenomes.txt` do `nomes.csv.gz` com o critério
novo, registrar aqui e rodar `go test ./internal/ortografia ./internal/resolver`.
