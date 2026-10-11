// Package nuvem transcreve voz com os provedores de nuvem que quem dita
// escolhe (OpenAI e Groq), com a chave que ELA colou, por trás do contrato
// asr.ASRProvider (ADR-021, decisão D). O domínio nunca conhece o provedor
// concreto (ADR-003, AC-029).
//
// As duas APIs têm a mesma forma — POST .../audio/transcriptions, multipart —
// e por isso moram num pacote só. O que muda de um modelo para outro (o campo
// do idioma, o formato da resposta) fica numa tabela pequena (perfilDoModelo),
// e nenhum nome de modelo é imposto: os padrões (ModeloPadrao) são constantes
// que a pessoa troca, e a lista de onde ela escolhe vem do provedor
// (Cliente.ListarModelos).
//
// # O áudio
//
// Só entra áudio COMPRIMIDO (asr.AudioBuffer.Comprimido, webm/opus): este
// pacote não decodifica nem codifica nada, e recusa PCM com
// asr.ErrPCMNaoSuportado. Os bytes recebidos vão ao provedor como estão, sem
// segunda cópia: o corpo do multipart é montado de PARTES PRONTAS (o cabeçalho
// das partes, os bytes recebidos e o fecho) juntas por um io.MultiReader, com
// o Content-Length calculado. Nunca chunked: não está provado que os dois
// provedores o aceitam. Nenhum arquivo é criado e nada é logado (este pacote
// nem importa os pacotes de log: um teste confere).
//
// # O que nunca sai daqui
//
// A chave só vai no cabeçalho Authorization. Não entra em mensagem de erro,
// nem em URL, nem em log. O corpo das respostas de erro do provedor também não
// passa adiante: a OpenAI, por exemplo, devolve no 401 um pedaço da chave
// recusada. Toda falha vira um Erro com um Motivo de uma lista fechada e uma
// mensagem fixa, escrita aqui, que não embrulha o erro de origem (um erro de
// rede carrega a URL). A única causa embrulhada é context.DeadlineExceeded.
//
// Redirecionamento não é seguido: o Authorization seria entregue a quem o
// provedor, ou quem estivesse no meio, apontasse. Um 3xx é uma resposta
// inesperada e pronto. E só HTTP/1.1, de propósito, pelo mesmo motivo do
// internal/ia: o HTTP/2 do net/http escreve cabeçalhos e o começo do corpo no
// log padrão sob GODEBUG=http2debug, e guarda o Authorization na tabela HPACK
// da conexão.
//
// # Sem fala
//
// Quando o modelo devolve segmentos com no_speech_prob e avg_logprob (o
// verbose_json do whisper), os que passam dos limiares (LimiteSemFala,
// LimiteLogprob) saem do texto. Texto vazio no fim é ErrSemFala. Esse filtro
// roda DEPOIS de o provedor cobrar: ele evita o ditado vazio, não o custo.
//
// Sem troca automática de provedor: se o escolhido falha, a chamada falha.
package nuvem
