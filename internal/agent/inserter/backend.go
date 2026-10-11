package inserter

// Este arquivo não tem build tag: é só a forma das duas portas que
// strategy.go consome, sem nenhuma chamada de sistema — mesmo padrão de
// internal/agent/clipboard/backend.go.

// unicodeInjector injeta uma sequência de UTF-16 code units já
// codificada (incluindo os surrogate pairs de code points fora do BMP —
// essa codificação é feita por strategy.Insert, com utf16.Encode, não
// aqui) via SendInput com KEYEVENTF_UNICODE, um par down+up por unit, na
// ordem recebida. A implementação real é backend_windows.go.
type unicodeInjector interface {
	// InjectUnicode devolve quantos eventos foram efetivamente
	// injetados, junto com o erro.
	//
	// A contagem não é diagnóstico: ela decide se o fallback pode
	// rodar. Uma injeção PARCIAL já colocou caracteres na tela, e
	// inserir o texto de novo pelo clipboard os duplicaria — foi o que
	// Mauro viu em 27-set-2026 ("O vencimento é dia 12. O vencimento é
	// dia 12.").
	//
	// O contrato antigo devolvia só error, e quem chamava lia "falhou"
	// como "nada foi inserido". Isso é falso justamente no caso em que
	// o fallback é mais perigoso.
	InjectUnicode(units []uint16) (injetados int, err error)
}

// clipboardInserter cola text via clipboard: snapshot -> set -> Ctrl+V ->
// restore (ADR-011, decisões 3 e 4). Encapsula o ciclo de vida inteiro
// numa chamada só porque a corrida "restaurar cedo demais" (decisão 4)
// exige que quem dispara o Ctrl+V e quem decide restaurar sejam a mesma
// sequência, na ordem certa — não algo que strategy.go deveria
// orquestrar por fora.
//
// retained reporta se text ficou de propósito no clipboard porque a
// colagem falhou depois que o clipboard já continha o texto ditado
// (ADR-011, decisão 3: "não perder o texto") — só significativo quando
// err != nil. A implementação real é backend_windows.go.
type clipboardInserter interface {
	InsertViaClipboard(text string) (retained bool, err error)
}
