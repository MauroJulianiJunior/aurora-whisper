package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// montarSync monta o laço de sync do histórico (ADR-019, F7b) sobre o mesmo
// banco do histórico e a conexão da F6. Só existe com o histórico aberto.
//
// O ditado nunca espera o laço: a gravação põe o marcador na fila, na mesma
// transação, e só ACORDA o laço (Avisar, sem bloquear); a rede é dele, numa
// goroutine à parte, fora de qualquer transação. Desconectado, nada roda e
// nenhum pedido sai. O token é lido do Gerenciador de Credenciais pelo
// nuvem.Servico, atrás do portão sysinput (que recusa sob go test).
//
// O aprendido do painel (F8g): o laço recebe o aplicador do dicionario.json
// (a varredura no começo da rodada, o envio dos marcadores learned e o
// Receber do pull, sob a trava do painel), e o Aprender e o Desfazer do painel
// sobem a versão local do id no espelho no ato e acordam o laço
// (aprendiz.Aplicador.LigarAoSync): a troca sobe em segundos.
//
// Sem dicionário na subida (ap sem arquivo: a criação do vazio falhou), o
// aplicador vai ao laço do mesmo jeito, e é o arquivo indisponível de toda
// rodada (dictionary.ErrSemArquivo): a varredura não decide nada e o M fica
// desconhecido, então o aceite do "Enviar o histórico" não é gravado sozinho
// (as trocas de um dicionário reposto depois subiriam sem a pergunta, D3); e
// o par que o pull trouxer fica guardado como "não aplicado neste PC"
// (arquivo_indisponivel), para valer quando o dicionário voltar, em vez de
// passar do cursor. (Até a revisão da F8g, o aprendido ficava de fora, com o
// Dicionario nil: o desvio 37, revisto.)
func montarSync(hist *storage.SQLiteTranscriptionRepository, dono string, n *nuvem.Servico, ap *aprendizadoDoAgent,
	imagens syncengine.ImagensDoPC, log *slog.Logger) *syncengine.Laco {
	cfg := configDoSync(hist.Fila(dono), n, ap, log)
	// As imagens das notas (ADR-022, N10b): o repositório das notas, com a
	// pasta das imagens. nil as deixa de fora do sync.
	cfg.Imagens = imagens
	return ligarSync(cfg, hist, n, ap)
}

// imagensDoSync é o repositório das notas como as imagens do sync o usam
// (N10b), ou nil sem ele (um ponteiro nil não vira uma interface não nil).
func imagensDoSync(n *storage.SQLiteNotaRepository) syncengine.ImagensDoPC {
	if n == nil {
		return nil
	}
	return n
}

// ligarSync monta o laço com cfg e o liga ao resto do Agent: o ditado que
// entra na fila e a conexão nova o acordam, o aviso do freio do aprendido vai
// ao painel (o freio_trocas do GET /api/nuvem: o da última varredura, só em
// memória) e o gancho do painel sobe a versão local no espelho. Separada do
// montarSync para o teste rodar uma rodada pelo laço ligado assim, com a
// conexão e o cliente da nuvem trocados pelos dublês.
func ligarSync(cfg syncengine.ConfigDoLaco, hist *storage.SQLiteTranscriptionRepository, n *nuvem.Servico, ap *aprendizadoDoAgent) *syncengine.Laco {
	laco := syncengine.NovoLaco(cfg)
	hist.AoEnfileirar(laco.Avisar)
	n.AoConectar(laco.Avisar)
	n.UsarFreio(laco.Freio)
	if ap != nil {
		ap.LigarAoSync(cfg.Fila, laco.Avisar)
	}
	return laco
}

// configDoSync é a configuração do laço que o montarSync monta: a fila do
// dono, a conexão da F6 e, com o aprendizado do Agent, o aplicador do
// dicionario.json (com arquivo ou sem; dicionarioDoSync). Sem o aprendizado
// (nil: só nos testes da F7), o aprendido fica de fora.
func configDoSync(fila *storage.Fila, n *nuvem.Servico, ap *aprendizadoDoAgent, log *slog.Logger) syncengine.ConfigDoLaco {
	cfg := syncengine.ConfigDoLaco{
		Fila:    fila,
		Conexao: conexaoDaNuvem{n},
		NovoID:  storage.NewID,
		Log:     log,
	}
	if ap != nil {
		cfg.Dicionario = ap.dicionarioDoSync()
	}
	return cfg
}

// dicionarioDoSync é o dicionario.json como o laço o usa: o aplicador, ou,
// quando a subida não conseguiu conferir o arquivo (semConferir), o arquivo
// indisponível de toda rodada (nada vira delete, o M fica desconhecido, e o
// par do pull fica pendente).
func (a *aprendizadoDoAgent) dicionarioDoSync() syncengine.DicionarioDoAprendido {
	if a.semConferir {
		return dicionarioIndisponivel{}
	}
	return a.Aplicador
}

// dicionarioIndisponivel é o dicionario.json que o laço não lê nesta execução:
// toda leitura é dictionary.ErrSemArquivo.
type dicionarioIndisponivel struct{}

func (dicionarioIndisponivel) AplicarPares(func(dictionary.Semente) []dictionary.ParDaNuvem,
	func(dictionary.Lote) error) (dictionary.Semente, dictionary.Lote, error) {
	return dictionary.Semente{}, dictionary.Lote{}, dictionary.ErrSemArquivo
}

// prepararOAprendido confere, na subida e antes do laço, se o dicionario.json
// é o arquivo que o espelho do sync conhece (ADR-019, F8; a revisão da F8g).
// Não é quando o Agent acabou de criá-lo vazio (criou: o arquivo sumiu entre
// duas execuções) ou quando o usuario dele não é o gravado na subida anterior
// (outro arquivo posto no lugar). Enquanto o Agent roda, os dois são o arquivo
// indisponível da varredura (ausente, de outro usuário), que nunca vira
// delete; sem esta conferência, um reinício bastaria para o arquivo novo,
// válido, valer como "a pessoa desfez tudo", e os pares do espelho sairiam do
// celular e dos outros PCs.
//
// Com outro arquivo, o espelho do antigo é solto (syncengine.
// SoltarOArquivoAntigo): nenhum par vira delete, e os que a nuvem tem voltam
// ao arquivo novo pela varredura. A ordem garante o refazer: o valor vazio em
// storage.ChaveNuvemDicionario antes, o usuario depois. Sem dicionário
// (usuario vazio), nada a conferir: o aplicador já é o arquivo indisponível.
//
// Devolve falso quando não deu para conferir: o laço desta execução vê o
// arquivo como indisponível (aprendizadoDoAgent.semConferir). O log só tem
// contagens e códigos.
func prepararOAprendido(ctx context.Context, fila *storage.Fila, inst *storage.Instalacao, usuario string, criou bool, agora time.Time, log *slog.Logger) bool {
	if usuario == "" {
		return true
	}
	anterior, tinha, err := inst.Ler(ctx, storage.ChaveNuvemDicionario)
	if err != nil {
		log.Warn("sync: não consegui conferir o dicionário do aprendido; o laço o deixa de lado nesta execução", "etapa", "ler")
		return false
	}
	if !criou && (!tinha || anterior == usuario) {
		if !tinha {
			// A primeira subida com a conferência: o arquivo é o que o espelho
			// conhece, e só é anotado. Sem a anotação, a próxima tenta de novo.
			if err := inst.Gravar(ctx, map[string]string{storage.ChaveNuvemDicionario: usuario}); err != nil {
				log.Warn("sync: não consegui anotar o dicionário do aprendido", "etapa", "anotar")
			}
		}
		return true
	}
	if err := inst.Gravar(ctx, map[string]string{storage.ChaveNuvemDicionario: ""}); err != nil {
		log.Warn("sync: não consegui conferir o dicionário do aprendido; o laço o deixa de lado nesta execução", "etapa", "marcar")
		return false
	}
	n, err := syncengine.SoltarOArquivoAntigo(ctx, fila, agora)
	if err != nil {
		log.Warn("sync: não consegui soltar o espelho do dicionário antigo; o laço o deixa de lado nesta execução", "etapa", "soltar")
		return false
	}
	if err := inst.Gravar(ctx, map[string]string{storage.ChaveNuvemDicionario: usuario}); err != nil {
		// O espelho já foi solto: a próxima subida refaz, sem nada a mudar.
		log.Warn("sync: não consegui anotar o dicionário do aprendido", "etapa", "anotar")
	}
	log.Info("sync: o dicionário é outro arquivo; o aprendido do anterior não vira desfazer, e o da nuvem volta pela varredura",
		"criado_agora", criou, "pares", n)
	return true
}

// ligarAprendidoAoPainel põe na configuração do painel o aprendizado do Agent
// (o Aprender, o Desfazer, a lista e o "Adicionar termo") e as linhas de
// estado do aprendido (F8g): o espelho do sync, no mesmo banco e do mesmo
// dono do laço. Sem o espelho, o GET /api/aprendizado volta a ser só a lista
// do arquivo, em silêncio; por isso a montagem sai do run(), para o teste.
func ligarAprendidoAoPainel(p localapi.Preferencias, hist *storage.SQLiteTranscriptionRepository, dono string, ap *aprendizadoDoAgent) localapi.Preferencias {
	if ap != nil {
		p.Aprendizado, p.Termos = ap, ap
	}
	p.Espelho = hist.Fila(dono)
	return p
}

// ligarMudancasAoPainel põe na configuração do painel o contador de mudanças
// do laço (ADR-021, S): a GET /api/nuvem/mudancas, que o painel do PC lê a cada
// poucos segundos enquanto está à vista. A leitura avisa o laço de que o painel
// está aberto, e é por isso que ele busca a nuvem a cada 10 s enquanto ela se
// repete. Sai do run() para o teste.
func ligarMudancasAoPainel(p localapi.Preferencias, laco *syncengine.Laco) localapi.Preferencias {
	p.Mudancas = laco
	return p
}

// iniciarSync põe o laço para rodar e devolve como pará-lo (o cancelamento e
// a espera da rodada em curso). Um pânico no laço é registrado e o para, sem
// derrubar o Agent: o ditado segue local.
func iniciarSync(laco *syncengine.Laco, log *slog.Logger) (parar func()) {
	ctx, cancel := context.WithCancel(context.Background())
	feito := make(chan struct{})
	go func() {
		defer close(feito)
		defer func() {
			if r := recover(); r != nil {
				log.Error("sync: o laço parou por um defeito; o ditado segue local", "tipo", fmt.Sprintf("%T", r))
			}
		}()
		laco.Rodar(ctx)
	}()
	return func() { cancel(); <-feito }
}

// conexaoDaNuvem adapta o nuvem.Servico ao que o laço usa.
type conexaoDaNuvem struct{ s *nuvem.Servico }

func (c conexaoDaNuvem) Credencial(ctx context.Context) (syncengine.Credencial, error) {
	cr, err := c.s.Credencial(ctx)
	if err != nil {
		return syncengine.Credencial{}, err
	}
	return syncengine.Credencial{
		Configurada: cr.Configurada, Conectado: cr.Conectado, URL: cr.URL, Token: cr.Token,
		Aparelho: cr.Aparelho, PareadoEm: cr.PareadoEm,
	}, nil
}

func (c conexaoDaNuvem) TokenRecusado(ctx context.Context, token string) error {
	return c.s.TokenRecusado(ctx, token)
}
