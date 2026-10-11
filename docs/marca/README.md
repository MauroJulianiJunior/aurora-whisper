# Marca do Aurora Whisper

Escolhida em 29-set-2026, entre seis direções desenhadas sobre a paleta do projeto (verde
`#00E676`, preto `#070A09`, off-white `#F5F7F5`, colchetes de canto arredondados). Sem vermelho: a
paleta reserva o vermelho para risco.

## Licença de cada pasta

| Pasta | Licença |
|---|---|
| `pico-estourado/` | MIT, como o resto do repositório (`LICENSE`). |
| `retrato/` | MIT, como o resto do repositório (`LICENSE`). O autor liberou a ilustração da Aurora em 30-set-2026: ela deixou de ser de direitos reservados. |

O mesmo vale para a cópia do desenho que o painel embute (`web/src/Aurora.tsx`). Quem copiar,
modificar ou distribuir qualquer uma das duas pastas mantém o aviso de copyright da MIT.

| Pasta | Papel | Onde entra |
|---|---|---|
| `pico-estourado/` | **Símbolo do sistema.** Os colchetes da paleta emolduram a onda de voz, e uma barra fura a moldura por cima e por baixo: uma voz alta demais para caber no limite. | Ícone da bandeja, ícone do executável e do Setup, favicon, marca no cabeçalho do painel. É a versão que lê em 16 px. |
| `retrato/` | **A Aurora.** Ilustração plana da persona: franja reta, coque bagunçado, delineado, argola e fones de ouvido no pescoço. Inspirada na persona dela — roqueira, independente. | Dentro do painel, grande, onde ele "fala" com quem usa: estados vazios, "abra pela bandeja", Agent fora do ar. Precisa de uns 80 px ou mais. |

Arquivos de cada pasta:

- `mark.svg` — para fundo escuro (`#070A09`), viewBox 64×64.
- `mark-light.svg` — para fundo claro (`#F5F7F5`).
- `mark-small.svg` — versão para 16–32 px, viewBox 32×32.

A ilustração é estilizada e representa a persona Aurora.

## Onde entrou no painel (`web/`)

As marcas vão como SVG inline (componentes React), sem `<img>`: o painel não carrega nada de
fora. São decorativas — o texto ao lado diz tudo —, por isso `aria-hidden` e sem `<title>`.
`web/src/marca.test.tsx` confere, traço a traço, que o desenho e as cores de cada tema são os
destes arquivos; quem mudar um arquivo daqui muda a cópia no painel junto.

| Lugar | Arquivo | Tamanho | Cores |
|---|---|---|---|
| Ícone do executável e do Setup (`cmd/agent/icone_windows_amd64.syso` e `installer/aurora-whisper.ico`) | `pico-estourado/mark-small.svg`, **com** o ladrilho: o desenho da bandeja (`tray.PixelsDoIcone`, tema escuro) sobre o ladrilho preto arredondado (`rx` 7 de 32). Gerados em Go por `cmd/gerarico`, sem imagem de origem: para regenerar, `go run ./cmd/gerarico` da raiz do repositório | 16, 24, 32, 48, 64 e 256 px | as do arquivo: o ladrilho garante contraste no Explorador, no Menu Iniciar e em "Aplicativos", sobre fundo claro e escuro |
| Aba do navegador (`web/public/favicon.svg`) | `pico-estourado/mark-small.svg`, igual, com o ladrilho preto | 16–32 px | as do arquivo: o ladrilho garante contraste em aba clara e escura |
| Cabeçalho do painel, ao lado do nome (`web/src/Marca.tsx`) | `pico-estourado/mark-small.svg`, **sem** o ladrilho | 16 px, o da bandeja e da aba: na grade de 32 do desenho, o traço de 4 unidades vira 2 px inteiros (a 24 px virava 3 px em meio pixel, borrado) | escuro: as de `mark.svg` (verde e off-white); claro: as de `mark-light.svg` (verde vivo e preto). No escuro o ladrilho sumiria no fundo, que é o mesmo preto; no claro seria a coisa mais pesada da barra. |
| Histórico vazio, lixeira vazia e a tela "Abra o painel pela bandeja" (`web/src/Aurora.tsx`, `Aurora`) | `retrato/mark.svg` e `retrato/mark-light.svg` | 96 px (os testes seguram de 80 a 120), sempre sobre um título | escuro: `mark.svg`; claro: `mark-light.svg` |

A Aurora não aparece em "Nada encontrado": ali é a busca respondendo, não o painel falando de si.

Decidido pelo Mauro em 29-set-2026:

- **Um verde só nos colchetes das marcas:** o verde vivo `#00E676`, nos dois temas, como nos
  arquivos claros da paleta do projeto. O
  `pico-estourado/mark-light.svg` trocou o verde profundo por ele. São logo, não controle: o
  traço preto ao lado carrega a forma. O foco e o anel da aba ativa continuam em verde profundo
  no tema claro, porque ali o contraste é funcional.
- **A Aurora pequena saiu da faixa** de Agent fora do ar e de sessão perdida: chamava mais
  atenção que a mensagem, num alerta que a paleta pinta de preto e vermelho. O arquivo
  `retrato/mark-small.svg` continua aqui, sem uso no painel.
