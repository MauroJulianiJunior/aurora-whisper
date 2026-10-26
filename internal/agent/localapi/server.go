package localapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// InicioAutomatico é o "Iniciar com o Windows". A fonte da verdade é o
// registro (internal/agent/autostart), não o banco: o Agent lê a cada
// pedido, com a MESMA regra da bandeja — inclusive a de que a build de
// console não mexe nele.
type InicioAutomatico interface {
	// Estado diz se está ligado e, quando não dá para mudar, por quê
	// ("" = pode mudar).
	Estado() (ligado bool, indisponivel string)
	// Definir liga ou desliga.
	Definir(ligar bool) error
}

// ListadorDeModelos pergunta ao provedor de IA quais modelos a chave
// alcança — em produção, *ia.Cliente.
type ListadorDeModelos interface {
	ListarModelos(ctx context.Context, provedor, chave string) ([]string, error)
}

// Preferencias é o que /api/settings, /api/ia, /api/aprendizado, /api/termos, /api/nuvem, /api/retencao e /api/microfone precisam
// além dos repositórios.
type Preferencias struct {
	// Padrao é o que GET /api/settings devolve, com version 0, antes do
	// primeiro PUT. O PUT com base_version 0 grava sobre ele.
	Padrao storage.Settings
	// Chaves guarda a chave de API de cada provedor. Obrigatório.
	Chaves chaves.Cofre
	// Inicio é o "Iniciar com o Windows". Obrigatório.
	Inicio InicioAutomatico
	// Modelos testa a chave e traz a lista de modelos. Obrigatório.
	Modelos ListadorDeModelos
	// Aplicar entrega as preferências recém-gravadas ao Agent em
	// execução e devolve os campos (nomes do JSON) que só valem no
	// próximo início. Quem sabe o que dá para aplicar com o Agent rodando
	// é o Agent, não esta API. Nil: nada a aplicar, nada pendente.
	Aplicar func(storage.Settings) (reinicioPendente []string)
	// UltimoPolish devolve o desfecho do último ditado que pediu o Polish
	// (ok=false: ainda não houve nenhum). É o ponto de injeção de
	// polish_ultimo_motivo em GET /api/settings: quem monta o Agent
	// guarda, em MEMÓRIA, o motivo de fallback de cada tentativa (o
	// resolver.MotivoFallback como texto, "" quando a reescrita foi aceita)
	// junto com o provedor e o instante, e devolve aqui. Só o motivo, nunca
	// texto ditado — e um motivo que a tabela de frases não conheça vira
	// uma frase genérica. Chamada a cada GET e PUT /api/settings, pelas
	// goroutines de HTTP: tem de ser segura para uso concorrente e não
	// pode bloquear. Nil: o Agent não informa, e o campo sai "".
	UltimoPolish func() (DesfechoDoPolish, bool)
	// Aprendizado é o dicionário em que o painel ensina (etapa 3, ADR-017):
	// a sugestão ao salvar uma edição, e /api/aprendizado. Nil: o Agent não
	// montou o aprendizado — salvar não sugere nada e as rotas de
	// aprendizado respondem 503 (a lista, vazia).
	Aprendizado Aprendizado
	// Termos é o "Adicionar termo" das Configurações (ADR-020): /api/termos.
	// Uma interface à parte de Aprendizado, para os dublês de hoje não
	// precisarem dela. Nil: as rotas respondem 503. Em produção é o mesmo
	// aprendizadoDoAgent, com a mesma trava.
	Termos dictionary.Termos
	// Espelho é o espelho do aprendido do sync (ADR-019, "Decisões da F8",
	// F8g; em produção, a storage.Fila do dono): as linhas de estado de GET
	// /api/aprendizado ("não aplicado neste PC", "desfeito em outro
	// aparelho", "trocado em outro aparelho"). Nil: a lista é só a do
	// arquivo, como antes da F8.
	Espelho EspelhoDoAprendido
	// Nuvem é o "Conectar à nuvem" das Configurações (ADR-019, F6):
	// /api/nuvem*. Em produção, o *nuvem.Servico. Nil: as rotas respondem
	// 503.
	Nuvem Nuvem
	// Mudancas é o contador de mudanças do sync (ADR-021, S): GET
	// /api/nuvem/mudancas. Em produção, o *syncengine.Laco. Nil: a rota
	// responde 503.
	Mudancas MudancasDoSync
	// Retencao é o bloco Histórico das Configurações (ADR-019, F10d e F10e):
	// /api/retencao*, os 6 meses no PC e as cópias do banco. Em produção, o
	// *expurgo.Ciclo. Nil: as rotas respondem 503.
	Retencao Retencao
	// Notas são as notas e os marcadores (ADR-022, N1): /api/notas* e
	// /api/marcadores*. Em produção, o *storage.SQLiteNotaRepository. Nil: as
	// rotas respondem 503.
	Notas storage.NotaRepository
	// Microfone é o aviso de microfone bloqueado pelo Windows e o botão que
	// abre a tela certa: /api/microfone*. Nil: as rotas respondem 503.
	Microfone Microfone
	// Log recebe as falhas do Gerenciador de Credenciais e do registro —
	// só o erro, nunca a chave — e as CONTAGENS do aprendizado (quantas
	// sugestões, por que uma edição não sugeriu), nunca o texto. Nil
	// descarta.
	Log *slog.Logger
}

// Server implementa a API HTTP local do dashboard (histórico,
// preferências e a chave do provedor de IA). Não é seguro para reuso
// entre usuários distintos: cada instância está presa a um único userID
// (ver doc.go, "user_id local").
type Server struct {
	userID         string
	transcriptions storage.TranscriptionRepository
	settings       storage.SettingsRepository
	pref           Preferencias
	log            *slog.Logger

	// reinicioPendente é o que o último Aplicar devolveu. Vive só na
	// memória, de propósito: é "o que ESTE processo não conseguiu
	// aplicar", e no próximo início tudo o que está gravado já vale.
	mu               sync.Mutex
	reinicioPendente []string

	// putMu serializa os PUT /api/settings. O início automático muda o
	// registro ANTES da gravação otimista, e o Aplicar entrega ao runtime
	// DEPOIS dela: sem a fila, dois PUTs com a mesma base_version podiam
	// mexer os dois no registro (e o perdedor, com 409, deixava o dele
	// valendo), e dois PUTs seguidos podiam chegar ao runtime fora de
	// ordem.
	putMu sync.Mutex

	// sugeridas lembra, em memória, o que o motor sugeriu a cada edição
	// (aprendizado.go): o clique em Aprender só vale para um par que saiu
	// de lá, na versão do ditado em que saiu.
	sugeridas lembradas
}

// New constrói o Server. userID é o usuário local do Agent (ver doc.go).
// Nada obrigatório pode ser nil — chamar com nil entraria em panic nas
// primeiras requisições, então New já valida isso cedo.
func New(userID string, transcriptions storage.TranscriptionRepository, settings storage.SettingsRepository, pref Preferencias) *Server {
	switch {
	case userID == "":
		panic("localapi: userID vazio")
	case transcriptions == nil:
		panic("localapi: transcriptions repository nil")
	case settings == nil:
		panic("localapi: settings repository nil")
	case pref.Chaves == nil:
		panic("localapi: cofre de chaves nil")
	case pref.Inicio == nil:
		panic("localapi: início automático nil")
	case pref.Modelos == nil:
		panic("localapi: listador de modelos nil")
	}
	log := pref.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{userID: userID, transcriptions: transcriptions, settings: settings, pref: pref, log: log}
}

// Handler monta as rotas da Local API. O chamador é responsável por
// compor o resultado com a cadeia de segurança de internal/agent/guard
// (guard.Protect) antes de servir — este handler, sozinho, não valida
// Host, Origin nem token (ver doc.go).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/history", s.handleListHistory)
	mux.HandleFunc("GET /api/history/{id}", s.handleGetHistory)
	mux.HandleFunc("PATCH /api/history/{id}", s.handlePatchHistory)
	mux.HandleFunc("DELETE /api/history/{id}", s.handleDeleteHistory)
	mux.HandleFunc("POST /api/history/{id}/restore", s.handleRestoreHistory)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	mux.HandleFunc("GET /api/ia/chave", s.handleGetChave)
	mux.HandleFunc("PUT /api/ia/chave", s.handlePutChave)
	mux.HandleFunc("DELETE /api/ia/chave", s.handleDeleteChave)
	mux.HandleFunc("POST /api/ia/testar", s.handleTestarIA)

	mux.HandleFunc("GET /api/aprendizado", s.handleGetAprendizado)
	mux.HandleFunc("POST /api/aprendizado", s.handleAprender)
	mux.HandleFunc("POST /api/aprendizado/desfazer", s.handleDesfazer)

	mux.HandleFunc("GET /api/termos", s.handleGetTermos)
	mux.HandleFunc("POST /api/termos", s.handleAdicionarTermo)
	mux.HandleFunc("POST /api/termos/dica", s.handleDicaDoTermo)
	mux.HandleFunc("POST /api/termos/remover", s.handleRemoverTermo)

	mux.HandleFunc("GET /api/nuvem", s.handleGetNuvem)
	mux.HandleFunc("POST /api/nuvem/conectar", s.handleConectarNuvem)
	mux.HandleFunc("POST /api/nuvem/conferir", s.handleConferirNuvem)
	mux.HandleFunc("POST /api/nuvem/desconectar", s.handleDesconectarNuvem)
	mux.HandleFunc("POST /api/nuvem/enviar", s.handleEnviarHistorico)
	mux.HandleFunc("POST /api/nuvem/esquecer", s.handleEsquecerNuvem)
	mux.HandleFunc("GET /api/nuvem/mudancas", s.handleMudancasNuvem)

	mux.HandleFunc("GET /api/retencao", s.handleGetRetencao)
	mux.HandleFunc("POST /api/retencao/ligar", s.handleLigarRetencao)
	mux.HandleFunc("POST /api/retencao/desligar", s.handleDesligarRetencao)
	mux.HandleFunc("POST /api/retencao/copias/apagar", s.handleApagarCopias)

	mux.HandleFunc("GET /api/notas", s.handleListNotas)
	mux.HandleFunc("POST /api/notas", s.handleCriarNota)
	mux.HandleFunc("GET /api/notas/{id}", s.handleGetNota)
	mux.HandleFunc("PATCH /api/notas/{id}", s.handlePatchNota)
	mux.HandleFunc("DELETE /api/notas/{id}", s.handleDeleteNota)
	mux.HandleFunc("POST /api/notas/{id}/restaurar", s.handleRestaurarNota)
	mux.HandleFunc("DELETE /api/notas/{id}/definitivo", s.handleApagarNotaDeVez)
	mux.HandleFunc("PUT /api/notas/{id}/marcadores/{marcador}", s.handleMarcarNota)
	mux.HandleFunc("DELETE /api/notas/{id}/marcadores/{marcador}", s.handleDesmarcarNota)

	mux.HandleFunc("POST /api/history/{id}/virar-nota", s.handleVirarNota)
	mux.HandleFunc("POST /api/notas/{id}/desfazer-virar", s.handleDesfazerVirarNota)

	mux.HandleFunc("GET /api/marcadores", s.handleListMarcadores)
	mux.HandleFunc("POST /api/marcadores", s.handleCriarMarcador)
	mux.HandleFunc("PATCH /api/marcadores/{id}", s.handlePatchMarcador)
	mux.HandleFunc("DELETE /api/marcadores/{id}", s.handleDeleteMarcador)

	mux.HandleFunc("GET /api/microfone", s.handleGetMicrofone)
	mux.HandleFunc("POST /api/microfone/configuracoes", s.handleAbrirConfiguracoesDoMicrofone)

	return mux
}
