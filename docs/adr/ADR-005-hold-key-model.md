# ADR-005 — Tecla simples com tap/hold

Status: Accepted

## Decisão
Uma tecla simples configurável atua normalmente em tap curto e como push-to-talk em hold.

Default inicial:
- tecla F;
- hold 700 ms;
- áudio bufferizado desde keydown.

## Risco
Interferência em digitação/atalhos.

## Mitigação
Bypass com modifiers, tecla configurável, testes por app, watchdog.
