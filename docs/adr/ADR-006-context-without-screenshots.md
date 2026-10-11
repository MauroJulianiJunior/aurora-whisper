# ADR-006 — Contexto via UI Automation sem screenshots

Status: Accepted

## Decisão
Usar Windows UI Automation e metadados do controle focado para contexto. Screenshot/OCR fica fora de escopo.

## Justificativa
Menor coleta, menor risco de privacidade e complexidade suficiente para o MVP.

## Consequências
Alguns apps terão contexto limitado; o sistema deve degradar graciosamente.
