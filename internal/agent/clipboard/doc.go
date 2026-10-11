// Package clipboard preserva e restaura o clipboard do sistema ao redor de
// uma escrita temporária (ADR-011 decisão 4, AC-009): antes de colar o
// texto ditado, tira um snapshot do que já estava lá; depois, devolve esse
// conteúdo.
//
// # O problema central: restaurar cedo demais
//
// A sequência é snapshot -> escreve -> (algo cola) -> restaura, e ela tem
// uma corrida real: se a restauração acontecer antes de o aplicativo alvo
// ter lido o clipboard, o Ctrl+V vai colar o conteúdo ANTIGO — a diferença
// entre colar o ditado e colar o que a pessoa tinha copiado antes.
//
// O Win32 não tem um jeito síncrono de saber "o clipboard acabou de ser
// lido". Foi pesquisado deliberadamente antes de escrever este pacote:
//
//   - GetClipboardSequenceNumber incrementa quando o conteúdo MUDA
//     (SetClipboardData, EmptyClipboard) — nunca quando alguém só lê.
//     Colar (Ctrl+V) não altera a sequência.
//   - AddClipboardFormatListener manda WM_CLIPBOARDUPDATE pelo mesmo
//     motivo: mudança, não leitura.
//
// Ou seja: nenhum dos dois mecanismos prova que o paste já aconteceu. O
// que a sequência PROVA é outra coisa, também real: se ela mudou desde a
// nossa própria escrita, alguma coisa (o usuário, outro programa) já
// alterou o clipboard de novo — e restaurar por cima disso destruiria essa
// mudança mais nova. Restore usa a sequência só para essa checagem
// defensiva (shouldRestore, em policy.go), não para decidir "já foi lido".
//
// Para a pergunta "quanto esperar antes de restaurar", a resposta adotada
// aqui é um atraso fixo e configurável (Config.RestoreDelay) — não porque
// seja elegante, mas porque é a estratégia honesta quando não existe sinal
// melhor. O valor default está marcado [ASSUMPTION] no próprio código e
// PRECISA ser medido com uma pessoa colando de verdade na matriz de
// aplicativos da spec (Bloco de Notas, textarea do Chrome, WhatsApp Web,
// VS Code, inputs web comuns) — ver o roteiro de teste manual.
//
// # O que é preservado, e o que não é
//
// Só texto Unicode (CF_UNICODETEXT) é capturado e devolvido. Isto cobre o
// caso comum (copiar texto de qualquer app moderno) e, como efeito
// colateral gratuito, também os formatos de texto legado: o Windows
// sintetiza CF_TEXT/CF_OEMTEXT a partir de CF_UNICODETEXT sozinho quando
// um app pede um desses formatos e só o Unicode foi fornecido — não é
// preciso escrever os três.
//
// NÃO é preservado: imagem (CF_BITMAP/CF_DIB), arquivos (CF_HDROP), RTF,
// HTML, ou qualquer outro formato não-texto. Se o clipboard original só
// tinha um desses, Restore não tem como trazê-lo de volta — o snapshot
// nunca o capturou — e o clipboard fica vazio depois do Restore, não com o
// conteúdo original. Preserve nunca finge que capturou algo que não
// capturou: quando não há CF_UNICODETEXT disponível, o snapshot registra
// "sem texto" explicitamente, e Restore, nesse caso, apenas esvazia o
// clipboard de volta.
//
// # O portão
//
// Toda escrita no clipboard — e toda LEITURA também, porque ler expõe
// conteúdo do usuário — passa por sysinput.Guard() antes de tocar o
// sistema real. Guard recusa sempre sob "go test" (ver
// internal/agent/sysinput), então a suíte deste pacote nunca abre o
// clipboard de verdade: Preserve e Restore, os dois pontos de entrada
// exportados, são os únicos lugares que chamam o backend Win32 real, e
// ambos chamam Guard primeiro. A lógica de decisão (retry, snapshot,
// restore) é testada contra um backend dublê que nunca encosta no
// clipboard do sistema, e por isso não precisa — e não deve — chamar
// Guard: quem chama Guard é só o caminho até o backend real.
//
// # Privacidade
//
// O conteúdo do clipboard nunca é escrito em log, nem em debug
// (09-security-privacy.md proíbe "clipboard" explicitamente). Todo log
// deste pacote registra contagem de runes e metadados (havia texto
// anterior? qual o número de sequência?), nunca o texto em si.
//
// # Armadilhas tratadas
//
//   - O clipboard é um recurso GLOBAL: OpenClipboard falha quando outro
//     processo o mantém aberto. openWithRetry tenta de novo com backoff
//     curto e desiste de forma limpa — nunca laço infinito.
//   - Abrir e não fechar trava o clipboard do sistema inteiro para todos
//     os apps. Todo caminho de saída — inclusive erro — fecha via defer,
//     assim que open() tiver sucesso.
//   - Conteúdo grande, binário (via ausência de CF_UNICODETEXT) e
//     clipboard vazio são tratados como casos normais, não exceções.
package clipboard
