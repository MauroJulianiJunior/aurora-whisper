package storage

import (
	"context"
	"time"
)

// Settings espelha a tabela local_settings (docs/ai-spec/05-data-model.md,
// "local_settings"): as preferências locais do Windows Agent, algumas
// sincronizáveis com o Postgres (UserSettings.settings), outras
// exclusivamente locais (ex.: LocalPort).
//
// Nenhum campo aqui guarda segredo. A chave de API do provedor de IA
// mora no Gerenciador de Credenciais do Windows (internal/agent/chaves),
// nunca no banco — AIProvider e AIModel são só a ESCOLHA de quem usa.
type Settings struct {
	Hotkey               string
	HoldThresholdMs      int64
	BeepEnabled          bool
	OverlayPosition      string
	MicrophoneDeviceID   *string
	Autostart            bool
	ASRPolicy            string
	LocalModelID         *string
	LocalContextEnabled  bool
	ProtectedFieldPolicy string
	LocalPort            int64
	// TextMode é a política de texto: "literal", "smart" ou "polish"
	// (16-ai-behavior-spec.md). Padrão "smart" (REQ-022).
	TextMode string
	// AIProvider é o provedor de IA escolhido: "", "openai" ou
	// "anthropic". Vazio = nenhum.
	AIProvider string
	// AIModel é o modelo escolhido nesse provedor, como o provedor o
	// nomeia. Texto livre: a lista vem do próprio provedor, nunca do
	// código.
	AIModel string
	// MicrophoneAlwaysReady liga o "microfone sempre pronto" (ADR-015):
	// aberto o tempo todo, com 300 ms de captura antes das teclas.
	// Padrão false: o microfone só abre durante o ditado.
	MicrophoneAlwaysReady bool
	// NoteTriggerEnabled liga o gatilho "ditar para nota" (ADR-022, N9:
	// Ctrl+Win+Alt manda o ditado para uma nota). Padrão true (a coluna nasce
	// com 1); desligado, Ctrl+Win+Alt é um ditado comum.
	NoteTriggerEnabled bool
	UpdatedAt          time.Time
	Version               int64
}

// SettingsRepository é o contrato de persistência das preferências
// locais. Há sempre no máximo uma Settings por instalação do Agent (a
// tabela local_settings é de linha única).
type SettingsRepository interface {
	// Get devolve as preferências atuais. Devolve ErrNotFound se ainda
	// não houver nenhuma configuração salva (Agent recém-instalado,
	// antes do primeiro Put).
	Get(ctx context.Context) (Settings, error)

	// Put grava as preferências, substituindo o que houver, desde que
	// baseVersion seja a versão atual — 0 quando nada foi salvo ainda.
	// Senão devolve ErrVersionConflict e não grava nada: outra aba (ou
	// outro pedido) salvou antes, e sobrescrever seria perder a mudança
	// dela sem ninguém ver.
	//
	// Version e UpdatedAt de s são ignorados: Version sobe de 1 a cada
	// Put (1 na primeira gravação) e UpdatedAt vira o instante da
	// chamada.
	Put(ctx context.Context, s Settings, baseVersion int64) (Settings, error)
}
