// Package inserter é o inserter em camadas do Sprint 2 (ADR-011, decisão
// 3): substitui a inserção mínima caractere-a-caractere do Sprint 1
// (que vivia em internal/agent/winruntime/insert.go) por uma estratégia
// de duas camadas com um terceiro desfecho explícito de "não perder o
// texto":
//
//  1. SendInput com KEYEVENTF_UNICODE, para texto curto.
//  2. Clipboard (snapshot -> set -> Ctrl+V -> restore), para texto longo
//     ou quando o caminho 1 falha.
//  3. Se os dois falharem: devolve um resultado que diz isso — nunca
//     silenciosamente descarta o texto (04-system-design.md, "Inserção",
//     item 3: "Se falhar, manter histórico e oferecer copiar").
//
// # Organização
//
//   - strategy.go: [Inserter], [Config] e [Result] — a escolha de
//     caminho, o limiar, e o que fazer quando um caminho falha. Pura, sem
//     build tag, testada contra dublês (strategy_test.go) cobrindo as
//     seis situações que importam: sucesso no caminho 1; falha no 1 e
//     sucesso no 2; falha nos dois; texto vazio; texto longo o bastante
//     para pular direto ao caminho 2; e um code point fora do BMP (emoji),
//     que em UTF-16 vira um par de surrogates — o caso clássico de
//     corrupção silenciosa quando alguém mede "tamanho do texto" contando
//     runes em vez de code units.
//   - backend.go: as duas interfaces que a estratégia consome
//     (unicodeInjector, clipboardInserter) — só a forma, sem chamada de
//     sistema.
//   - backend_windows.go / backend_other.go: as implementações reais
//     (Win32 SendInput e internal/agent/clipboard) e o stub fora do
//     Windows, no mesmo par que todo pacote deste repositório que toca
//     Win32 usa (ver internal/agent/win32/doc.go).
//
// # Texto com quebra de linha (ADR-016, decisão 11)
//
// Texto com "\n" ou "\r" vai SEMPRE pelo caminho 2, com as quebras
// normalizadas para "\r\n", qualquer que seja o tamanho — e o caminho 1 não
// é nem tentado, nem como queda: pelo SendInput, um "\n" pode virar Enter e
// enviar a mensagem num chat (WhatsApp Web) antes de o texto acabar. Se o
// clipboard falhar, vale a regra de falha dele (Result.ClipboardRetained), e
// o texto não é digitado. É lógica pura (temQuebraDeLinha,
// normalizarQuebras, Insert), testada contra dublês em
// strategy_quebra_test.go; que o aplicativo trate a colagem como quebra, e
// não como envio, só o roteiro manual confere.
//
// # Portão de segurança (ADR-011, decisão 6)
//
// Toda função em backend_windows.go que injeta entrada (SendInput, seja
// para o caminho unicode ou para o Ctrl+V do caminho clipboard) começa
// com sysinput.Guard(). internal/agent/clipboard já faz o mesmo antes de
// tocar no clipboard. Nenhuma chamada real acontece sob "go test" —
// mesmo exercitando o caminho de produção inteiro, o Guard recusa antes
// de qualquer SendInput ou escrita de clipboard. A lógica de decisão
// (strategy.go) é o que tem teste automatizado; a chamada de sistema em
// si é verificada no roteiro de teste manual (ver CLAUDE.md, "Testes que
// agem no sistema real").
//
// # [ASSUMPTION] — limiar entre os dois caminhos
//
// ADR-011 deixa em aberto, deliberadamente, o tamanho a partir do qual o
// clipboard vence o SendInput (é uma pergunta de medição, não de
// palpite). [DefaultUnicodeThreshold] é um número defensável — não
// medido em aplicativo real — contado em UTF-16 code units (não runes:
// um emoji fora do BMP custa duas unidades de SendInput, e é esse custo,
// não o número de "caracteres" que a pessoa percebe, que motiva o
// limiar). [Config.UnicodeThreshold] deixa isso configurável para quando
// a medição da matriz mínima (Bloco de Notas, textarea do Chrome,
// WhatsApp Web, VS Code, inputs web comuns — 13-implementation-plan.md,
// TASK-004) estiver disponível.
//
// # Concern registrado — o marcador de "isto fomos nós" não alcança o Ctrl+V
//
// internal/agent/hook usa um marcador em KEYBDINPUT.DwExtraInfo
// (hook.markerExtraInfo, não exportado) para reconhecer os taps que ele
// mesmo sintetiza e não reprocessá-los como se fossem digitação física
// (ADR-007, seção 3; ver o comentário histórico em
// internal/agent/winruntime/insert.go sobre por que isso nunca importou
// para o caminho unicode: SendInput com KEYEVENTF_UNICODE sempre reporta
// VK_PACKET ao hook, e vkToKey nunca mapeia VK_PACKET para nenhuma
// dictation.KeyCode — PassThrough garantido, marcador ou não).
//
// O Ctrl+V deste pacote é diferente: ele injeta VK_CONTROL e VK_V de
// verdade (não KEYEVENTF_UNICODE), e hook/keymap.go MAPEIA os dois —
// ControlLeft e KeyV são dictation.KeyCode válidos. Se a tecla principal
// configurada (dictation.Config.Key, default KeyF) alguma vez for
// configurada como a letra V, o Ctrl+V sintético deste pacote — disparado
// enquanto uma inserção está em andamento — chega ao hook como um
// keydown/keyup físico da própria tecla de ditado. Não é um laço infinito
// (o núcleo não está em StateIdle durante a inserção, então
// Machine.onKeyDown descarta o keydown por "já existe tentativa em
// curso" — ver dictation/machine.go), mas é uma corrida real que não foi
// eliminada, só considerada improvável com a tecla default.
//
// A correção completa exigiria expor um IsOurs equivalente ao de
// internal/agent/hook para marcas de VK real (hoje só cobre o caminho
// unicode), o que significa editar internal/agent/hook —
// fora do escopo exclusivo desta fatia (internal/agent/inserter/** e
// internal/agent/winruntime/**). DwExtraInfo é marcado aqui mesmo assim
// (insertMarker, backend_windows.go), só não é reconhecido do outro lado.
// Registrado como concern no relatório da fatia, não corrigido aqui.
package inserter
