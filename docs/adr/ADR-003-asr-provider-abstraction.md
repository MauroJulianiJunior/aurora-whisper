# ADR-003 — ASR por provider substituível

Status: Accepted

## Decisão
Não fixar Whisper, Parakeet ou outro modelo como dependência estrutural. Usar `ASRProvider`.

## Justificativa
O vencedor depende de benchmark PT-BR, inglês, latência, RAM e precisão.

## Consequências
Provider local só vira default após eval. Cloud/VPS usam o mesmo contrato lógico.

## Nota de 10-out-2026 (ADR-021, ditar no celular)
O `AudioBuffer` deixa de aceitar só PCM. Ele ganha um campo opcional `Comprimido` (os bytes e o tipo,
por exemplo `audio/webm`), e cada buffer leva PCM **ou** o comprimido, nunca os dois. O
`ASRCapabilities` ganha `Formatos`, a lista do que o provider aceita comprimido.

- **O provider local** não declara formato nenhum e recusa o comprimido com um erro próprio. O Agent não
  muda.
- **Os providers da nuvem** (OpenAI e Groq, escolhidos pela pessoa; ADR-021) usam este mesmo contrato.
  O servidor repassa o áudio como chegou: não decodifica opus e não transcreve.
- **Por quê:** a alternativa era uma segunda interface de ASR só para a nuvem, e ela faria dois
  contratos no lugar de um, o que esta ADR existe para evitar.

O contrato detalhado fica em `18-tool-contract.md`, que a ADR-021 também emenda.
