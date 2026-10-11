//go:build windows

package inserter

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/clipboard"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// insertMarker é gravado em todo INPUT.Ki.DwExtraInfo que este arquivo
// emite — mesma técnica de internal/agent/hook/marker.go (KEYBDINPUT.
// DwExtraInfo é copiado sem alteração até KBDLLHOOKSTRUCT.DwExtraInfo,
// documentado pelo próprio Win32), com um valor PRÓPRIO deste pacote,
// distinto de hook.markerExtraInfo (não exportado, pacote fora do escopo
// desta fatia). Só serve para diagnóstico (aparece em qualquer dump
// externo de KBDLLHOOKSTRUCT.DwExtraInfo) — hook.IsOurs não o reconhece.
// Ver doc.go, "Concern registrado — o marcador de 'isto fomos nós' não
// alcança o Ctrl+V", para o porquê disso importar (ou não) em cada
// caminho.
const insertMarker uintptr = 0x4155524F52412D32 // "AURORA-2" em ASCII, truncado a 8 bytes

// vkControl e vkV são os VK_* que o caminho clipboard injeta para
// disparar Ctrl+V. Não vêm de internal/agent/win32 (que só expõe as
// constantes de KEYBDINPUT.DwFlags usadas pelo caminho unicode, não
// tabela de VK_* — ver seu types.go) nem de internal/agent/hook/keymap.go
// (não exportadas, pacote fora do escopo desta fatia); são os valores
// publicados pela documentação do Win32 (winuser.h), mesma fonte que
// keymap.go usa.
const (
	vkControl uint16 = 0x11 // VK_CONTROL
	vkV       uint16 = 0x56 // 'V' (VK_* de letra == código ASCII)
)

// winUnicodeBackend é a implementação real de unicodeInjector, via
// win32.SendInput com KEYEVENTF_UNICODE — mesmo mecanismo da inserção
// mínima do Sprint 1 (antigo internal/agent/winruntime/insert.go), agora
// atrás de sysinput.Guard() (ADR-011, decisão 6, acrescentada depois do
// incidente do Sprint 2 — ver internal/agent/sysinput/doc.go).
type winUnicodeBackend struct{}

func newUnicodeBackend() unicodeInjector { return winUnicodeBackend{} }

func (winUnicodeBackend) InjectUnicode(units []uint16) (int, error) {
	if err := sysinput.Guard(); err != nil {
		return 0, err
	}

	inputs := make([]win32.INPUT, 0, len(units)*2)
	for _, u := range units {
		down := win32.INPUT{
			Type: win32.InputKeyboard,
			Ki: win32.KEYBDINPUT{
				WScan:       u,
				DwFlags:     win32.KeyEventFUnicode,
				DwExtraInfo: insertMarker,
			},
		}
		up := down
		up.Ki.DwFlags |= win32.KeyEventFKeyUp
		inputs = append(inputs, down, up)
	}

	n, err := win32.SendInput(inputs)
	if n != uint32(len(inputs)) {
		if err == nil {
			err = fmt.Errorf("sendinput unicode incompleto: %d/%d eventos injetados", n, len(inputs))
		}
		// Devolve n: os eventos já injetados estão na fila do sistema e
		// viraram caracteres na tela. Quem chama precisa saber disso
		// para NÃO tentar o clipboard por cima.
		return int(n), err
	}
	return len(inputs), nil
}

// winClipboardBackend é a implementação real de clipboardInserter:
// snapshot -> set (via internal/agent/clipboard, que já passa por
// sysinput.Guard() internamente) -> Ctrl+V (via SendInput, guardado
// aqui) -> restore.
type winClipboardBackend struct {
	preserver *clipboard.Preserver
	log       *slog.Logger
}

func newClipboardBackend(logger *slog.Logger) clipboardInserter {
	return &winClipboardBackend{
		preserver: clipboard.New(clipboard.WithLogger(logger)),
		log:       logger,
	}
}

func (b *winClipboardBackend) InsertViaClipboard(text string) (retained bool, err error) {
	snap, err := b.preserver.Preserve(text)
	if err != nil {
		// Preserve não chegou a escrever nada no clipboard (ver seu
		// próprio doc: só devolve snapshot válido depois de escrever com
		// sucesso) — nada ficou retido.
		return false, fmt.Errorf("clipboard preserve: %w", err)
	}

	if pasteErr := sendCtrlV(); pasteErr != nil {
		// O clipboard já contém o texto ditado (Preserve acima já
		// escreveu). ADR-011, decisão 3: não perder o texto — de
		// propósito, NÃO restauramos aqui. O texto fica disponível para
		// o usuário colar manualmente em outro lugar
		// (04-system-design.md, "Inserção", item 3).
		return true, fmt.Errorf("clipboard paste (ctrl+v): %w", pasteErr)
	}

	if restoreErr := b.preserver.Restore(snap); restoreErr != nil {
		// O paste já aconteceu — a inserção em si teve sucesso. Falha ao
		// restaurar não desfaz isso. ErrRestoreSkippedExternalChange é o
		// caso esperado por ADR-011 decisão 4 (algo mais alterou o
		// clipboard nesse meio tempo); qualquer erro aqui é só logado,
		// nunca propagado como falha de inserção.
		if !errors.Is(restoreErr, clipboard.ErrRestoreSkippedExternalChange) {
			b.log.Warn("inserter: restore do clipboard após colar falhou", "err", restoreErr)
		}
	}
	return false, nil
}

// sendCtrlV injeta Ctrl+V via VK_CONTROL + VK_V reais (não
// KEYEVENTF_UNICODE — Ctrl+V é um atalho, não um caractere). Atrás de
// sysinput.Guard(), mesma disciplina de winUnicodeBackend.InjectUnicode.
func sendCtrlV() error {
	if err := sysinput.Guard(); err != nil {
		return err
	}

	key := func(vk uint16, up bool) win32.INPUT {
		flags := uint32(0)
		if up {
			flags = win32.KeyEventFKeyUp
		}
		return win32.INPUT{
			Type: win32.InputKeyboard,
			Ki: win32.KEYBDINPUT{
				WVk:         vk,
				DwFlags:     flags,
				DwExtraInfo: insertMarker,
			},
		}
	}

	inputs := []win32.INPUT{
		key(vkControl, false),
		key(vkV, false),
		key(vkV, true),
		key(vkControl, true),
	}

	n, err := win32.SendInput(inputs)
	if n != uint32(len(inputs)) {
		if err == nil {
			err = fmt.Errorf("sendinput ctrl+v incompleto: %d/%d eventos injetados", n, len(inputs))
		}
		return err
	}
	return nil
}
