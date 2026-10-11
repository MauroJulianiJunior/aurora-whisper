// Package local implementa asr.ASRProvider sobre um processo de ASR
// residente, conforme docs/adr/ADR-012-asr-local-como-processo-externo.md.
//
// # Por que residente, e não um processo por transcrição
//
// Não é preferência de desenho: é o que o benchmark do Sprint 3 mediu, nos
// 46 arquivos do dataset de voz, com a configuração escolhida
// (docs/benchmarks/asr-decisao-2026-09-18.md):
//
//	cold (processo novo a cada fala)   p50 976 ms
//	warm (processo residente)          p50 565 ms
//
// A meta do produto é p50 ≤ 500 ms ponta a ponta (02-prd.md §7). Iniciar um
// processo por fala gasta quase metade do orçamento antes de o modelo
// processar qualquer coisa.
//
// # O que fica residente, e o que isso custa
//
// O servidor segura o modelo em memória o tempo todo: 365 MB medidos com
// `small` em q4_0. Numa máquina que a pessoa usa para trabalhar, isso não é
// detalhe — é o preço da latência, e está registrado aqui para ser uma
// decisão consciente e não um efeito colateral.
//
// # Áudio nunca toca o disco
//
// O `AC-028` e o `09-security-privacy.md` proíbem persistir áudio. O
// servidor recebe o WAV montado em RAM, por multipart HTTP em loopback.
// Nenhum arquivo temporário existe em nenhum ponto do caminho — foi
// verificado no Sprint 3 que esse caminho funciona antes de ele virar
// dependência.
//
// # O que este pacote precisa resolver e não existia antes
//
// Processo residente cria dois problemas que processo de um tiro não tem:
//
//  1. o servidor pode morrer no meio da sessão. O provider reinicia com
//     backoff e o ditado falha com mensagem clara enquanto isso — nunca em
//     silêncio, nunca devolvendo texto vazio como se fosse transcrição.
//  2. o Agent pode ser morto sem encerrar nada. Sem cuidado explícito, o
//     servidor fica órfão segurando os 365 MB para sempre. Ver process.go.
//
// # Organização
//
// A decisão sobre formato, backoff, montagem de WAV e leitura da resposta é
// pura e fica FORA de build tag, testável em qualquer SO. Só o que fala com
// o Windows (criar processo, atrelar ao ciclo de vida do Agent) fica atrás
// de //go:build windows — a mesma disciplina que internal/agent/audio
// adotou depois que um defeito de layout passou dois sprints escondido
// justamente por estar atrás de uma build tag sem cobertura.
package local
