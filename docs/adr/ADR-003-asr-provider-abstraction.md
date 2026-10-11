# ADR-003 — ASR por provider substituível

Status: Accepted

## Decisão
Não fixar Whisper, Parakeet ou outro modelo como dependência estrutural. Usar `ASRProvider`.

## Justificativa
O vencedor depende de benchmark PT-BR, inglês, latência, RAM e precisão.

## Consequências
Provider local só vira default após eval. Cloud/VPS usam o mesmo contrato lógico.
