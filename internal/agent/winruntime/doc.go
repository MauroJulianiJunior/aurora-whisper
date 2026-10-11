// Package winruntime é o runtime de produção do Windows Agent (ADR-010,
// "Escopo do Sprint 1"; ADR-011, Sprint 2): liga o núcleo puro do ditado
// (internal/agent/dictation, ADR-007) aos colaboradores Win32 reais —
// hook, áudio, overlay, rastreador de foco (internal/agent/uia), o
// inserter em camadas (internal/agent/inserter) e beep — exatamente como
// dictation/fakes.Runtime liga o mesmo núcleo aos dublês da Fase 1.
// Mesma forma, colaborador diferente de cada lado.
//
// # Organização
//
//   - runtime.go define Runtime e o laço Run: o único consumidor
//     serializado de dictation.Machine.Handle (ADR-010, "Modelo de
//     threads"). Sem build tag: hook.Hook, audio.Session, overlay.Overlay,
//     uia.Tracker e inserter.Inserter já resolvem sozinhos a diferença
//     Windows/outro SO através dos próprios pares
//     *_windows.go/*_other.go — este pacote não precisa repetir essa
//     divisão.
//   - insert.go aplica InsertEffect delegando a internal/agent/inserter
//     (ADR-011, decisão 3: SendInput unicode para texto curto; clipboard
//     e Ctrl+V para texto longo ou quando o SendInput falha). Substituiu,
//     no Sprint 2, a inserção mínima caractere-a-caractere do Sprint 1.
//   - beep_windows.go / beep_other.go tocam o beep de confirmação
//     (AC-006) via kernel32!Beep — não tem wrapper em
//     internal/agent/win32 (que só cobre user32/gdi32) nem em
//     golang.org/x/sys/windows, então é resolvido aqui do mesmo jeito que
//     internal/agent/hook resolve kernel32!GetTickCount (ADR-010, D3).
//   - microfone.go segura o bipe e a pílula "Gravando" até o microfone,
//     que no padrão só liga no keydown, entregar áudio, e escreve a linha "audio:
//     janela do microfone" de cada tentativa com os alertas (ADR-015).
//   - history.go registra RecordHistoryEffect em log estruturado (só o
//     tamanho do texto) e, quando UsarHistorico foi chamado, persiste a
//     transcrição: bruto, final, modelo, duração do áudio e tempo do
//     ASR — nunca o app em foco, que é contexto (09-security-privacy.md).
//     O modo gravado é o que aconteceu na tentativa: "polish" só com a
//     reescrita aceita.
//   - polish.go liga o Polish (ADR-016, S4): o Smart continua no laço, e só
//     a chamada ao provedor roda numa goroutine própria, com prazo de 3,5 s
//     (até 8 s no texto longo, prazoPolish) contado do começo da resolução;
//     o resultado volta por rt.polidos, com uma linha de log sem texto, e o
//     último motivo de fallback fica em memória para a tela (UltimoPolish).
//
// # Transcribe/Resolve são stub
//
// ASR real depende de um benchmark ainda não executado (ADR-010,
// "Escopo do Sprint 1"; "Não entra: ASR real"). TranscribeEffect e
// ResolveEffect são atendidos aqui por um texto de placeholder
// explicitamente marcado como tal — nunca silenciosamente parecido com
// uma transcrição real.
//
// # FocusTarget vem do rastreador de foco (Sprint 2, ADR-011)
//
// handleRaw, em runtime.go, lê rt.focus.Current() — o cache O(1) mantido
// por um internal/agent/uia.Tracker real, montado e iniciado por
// cmd/agent junto com hook/áudio/overlay — em vez do
// dictation.FocusTarget{} zero fixo do Sprint 1. Isso torna StateBlocked
// (AC-036) alcançável em produção pela primeira vez: um alvo com foco
// protegido (campo de senha) agora de fato bloqueia o ditado, não só em
// teste — ver runtime_test.go, que prova essa ligação sem precisar
// instalar hook nenhum de Windows.
package winruntime
