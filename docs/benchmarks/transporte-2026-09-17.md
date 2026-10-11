# Custo de transporte entre processos — medição de 17-set-2026

Primeira medição do Sprint 3, e a única que **não baixa modelo nenhum**.

O `ADR-012` manda medir o custo ponta a ponta de falar com um ASR que roda como processo externo,
e reportar cold e warm separados. Se o custo de transporte não for isolado **antes**, todo número
de warm do benchmark de ASR sai com o transporte embutido e não separável — e a decisão do Sprint 4
sobre qual protocolo usar fica sem base.

Medido com um processo de eco que não faz nada além de devolver o que recebeu. Nenhum modelo, nenhum
áudio real: só bytes do tamanho que o áudio teria.

---

## A conclusão

**O custo de transporte é ruído diante da meta de `p50 ≤ 500 ms`.**

O pior número medido — HTTP em cold, 16,6 ms de p50 — é **3,3% do orçamento**. Em warm, o piso está
uma a duas ordens de grandeza abaixo da meta.

**Nenhum dos três transportes é descartável por custo.** A escolha de protocolo do Sprint 4 pode ser
feita por outros critérios — simplicidade de integração com o runtime escolhido, robustez a crash,
se dá para fazer streaming — sem que o transporte seja o fator decisivo.

---

## A tabela

Repetições: cold 20, warm 200, controle 2000. Zero falhas nas 21 combinações — **todo eco bateu
byte a byte**.

| Transporte | Tamanho | Modo | p50 | p95 | máx |
|---|---|---|---|---|---|
| pipe (stdin/stdout) | 1 s | cold | 13,0 ms | 14,0 ms | 15,6 ms |
| TCP loopback cru | 1 s | cold | 15,2 ms | 16,2 ms | 18,9 ms |
| HTTP POST multipart | 1 s | cold | 16,3 ms | 17,8 ms | 19,6 ms |
| pipe | 10 s | cold | 13,7 ms | 14,4 ms | 14,7 ms |
| TCP loopback cru | 10 s | cold | 15,9 ms | 17,0 ms | 17,9 ms |
| HTTP POST multipart | 10 s | cold | 16,6 ms | 20,7 ms | 31,7 ms |
| pipe | 10 s | warm | — | 607 µs | 8,0 ms |
| TCP loopback cru | 10 s | warm | — | 672 µs | 11,2 ms |
| HTTP POST multipart | 10 s | warm | 1,07 ms | 1,69 ms | 23,3 ms |
| **in-process (controle)** | 10 s | — | — | 541 µs | 10,5 ms |

O tamanho do payload quase não muda nada: 320 KB custam praticamente o mesmo que 32 KB. O custo é
de **ida e volta**, não de volume.

O controle in-process — cópia pura em memória, sem I/O nenhum — mede quase o mesmo que o warm de
pipe e TCP. Isso diz que o overhead do próprio harness já domina a medição de warm, e que o número
reportado é o piso do sistema, não o custo do transporte.

---

## O limite que restringe toda medição futura deste projeto

**`time.Now()` nesta máquina tem granularidade de ~504 µs.**

Medido de forma independente, em 300.000 chamadas seguidas: **99,999% devolveram o mesmo
timestamp**. O menor delta não-zero observado foi 504,1 µs. Uma espera ocupada de 100 µs mede
999,5 µs de p50 — o relógio não enxerga nada mais curto que um tique.

Consequências, e elas valem além deste benchmark:

- **Os números de cold (13 a 20 ms) são confiáveis**, muito acima do tique.
- **Os p50 de warm marcados "—" não são "zero": são "abaixo da resolução".** Não dá para comparar
  warm entre pipe, TCP e HTTP com precisão fina nesta máquina.
- A meta de `p50 ≤ 500 ms` do produto tem folga de três ordens de grandeza sobre o tique, então
  **o benchmark de ASR não é afetado**.
- Qualquer medição futura abaixo de 1 ms precisa de `QueryPerformanceCounter` por syscall, não de
  `time.Now()`.

Isso não é defeito do código de medição: é o relógio disponível ao processo neste ambiente.

---

## Um defeito real encontrado no caminho

O primeiro teste do transporte HTTP falhou por truncamento: 32.044 bytes enviados, 3.696 recebidos,
com `http: invalid Read on closed Body`.

Não era do benchmark. É o comportamento padrão do `net/http` para HTTP/1.1: um handler que escreve
na resposta **antes** de terminar de ler o corpo da requisição não é full-duplex, e o servidor fecha
o resto do corpo na primeira escrita.

Vale registrar porque **é exatamente o erro que um servidor de ASR local pode cometer**, e o
sintoma é áudio truncado em silêncio — transcrição pela metade, sem erro nenhum. Se um candidato
apresentar transcrição curta demais, este é o primeiro lugar a olhar.

O teste pegou porque compara **byte a byte**, não só o tempo. Um transporte que corrompe e é rápido
não serve de nada, e um teste que só cronometra não vê isso.

---

## O que NÃO foi medido

- **Named pipe do Windows.** Ficou de fora para manter o benchmark testável no Linux. A omissão é
  declarada na saída do próprio comando, não só aqui — quem olhar só o terminal também vê.
- **WebSocket.** Exigiria dependência nova, e nenhum dos candidatos da lista curta usa.
- **O custo com carga real de CPU.** Estas medições rodaram com a máquina ociosa. Um ASR consumindo
  todos os núcleos muda o agendamento, e os picos de 8 a 23 ms vistos no máximo de warm são
  justamente disso.
