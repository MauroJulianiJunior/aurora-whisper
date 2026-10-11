// Package polish é o adaptador do Polish que o PC e o servidor usam igual
// (ADR-021, subfatia C5a): a ponte entre o motor de guardas
// (internal/resolver), o cliente dos provedores (internal/ia) e quem
// dita. Saiu de cmd/agent e de internal/agent/winruntime sem mudar o
// comportamento do Agent, para o servidor chamar o MESMO código no ditado
// do celular.
//
// # O que mora aqui
//
//   - Prazo e PisoPrazo: o teto da chamada ao provedor, de 3,5 s a 8 s
//     conforme o tamanho do texto (ADR-016, emenda de 01-out-2026).
//   - PoliticaEsforco: o esforço de raciocínio que a OpenAI recebe, sem nome
//     de modelo no código (ADR-016, decisão 9).
//   - Montagem.Abrir: monta o resolver.PolishResolver de UM ditado, com o
//     cliente que liga a política à resposta do provedor.
//
// # O que não mora aqui
//
// A chave, o Smart e o relógio são de quem chama. A chave entra por uma
// função chamada a cada ditado (no PC, a leitura do Gerenciador de
// Credenciais; no servidor, a decifragem da chave da conta): nenhum campo
// deste pacote a guarda, e ela só chega ao cabeçalho do pedido. O relógio
// do ditado (o watchdog do núcleo, a goroutine da chamada, o ctx com
// prazo) é do runtime do Agent (internal/agent/winruntime) ou da rota do
// servidor; este pacote só diz quanto o prazo vale.
//
// # Regras do pacote
//
// Compila em qualquer sistema: nenhum arquivo tem build tag, e nada daqui
// toca Windows, disco ou sistema de entrada. O único pacote do Agent que
// ele importa é internal/agent/dictation, o núcleo puro do ditado (só
// errors, fmt, time e utf8), que o próprio internal/resolver já carrega,
// por internal/context. Os testes de limites_test.go travam isso.
//
// Texto ditado, chave e contexto nunca vão a log nem a mensagem de erro
// daqui: a única linha de log (a da política de esforço) leva só o
// provedor, o modelo e os rótulos do esforço.
package polish
