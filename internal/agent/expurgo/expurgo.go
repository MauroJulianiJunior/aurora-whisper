// Package expurgo apaga de vez o que já venceu no histórico local: a lixeira
// de 30 dias e, depois do OK de quem usa, os 6 meses.
//
// # Por que isto existe
//
// Mover para a lixeira (DELETE /api/history/{id}) grava PurgeAt = agora
// + 30 dias (docs/ai-spec/05-data-model.md, "Retenção"), mas até aqui
// nenhum código de produção chamava o PurgeExpired do repositório. A
// lixeira crescia para sempre — guardando texto ditado que a pessoa já
// tinha mandado embora.
//
// # Quando
//
// Uma vez na subida e depois a cada Intervalo. A spec pede purge diário
// (09-security-privacy.md, "Retenção"); de hora em hora, o item some no
// máximo uma hora depois do prazo em vez de até um dia depois.
//
// Nunca ANTES do prazo, e isso não depende do timer ser pontual: quem
// decide o que venceu é o repositório, comparando PurgeAt com o Now() lido
// na hora da rodada. Um timer atrasado — computador suspenso, por exemplo
// — só faz o item sumir mais tarde.
//
// A cadência vive aqui, sobre um clock.Clock injetado: nos testes o
// relógio é o clock.Fake, e trinta dias passam num Advance, sem sleep.
//
// # A rodada (ADR-019, "Decisões da F10")
//
// Cada rodada, com um agora só: (1) a lixeira (PurgeExpired), com a guarda
// da D3; (2) o passo dos 6 meses (SeisMeses.Passo), que só apaga com o OK
// gravado e pausa a regra quando o freio dispara; (3) a gravação da rodada
// (SeisMeses.GravarRodada), com ou sem o OK, menos na rodada que pausou e na
// em que o passo falhou: é com ela que o freio da rodada seguinte compara, e
// a gravação depois de uma pausa que voltou atrás apagaria o freio.
//
// # A lixeira que espera a nuvem
//
// Desde a F10 (D3), o repositório não apaga a lixeira vencida que a nuvem
// pode ter e ainda não soube (o PC desconectado, a mudança na fila, a anotada
// pelo "Esquecer esta nuvem"), no máximo até os 6 meses do ditado. Ela é
// contada em esperando_a_nuvem.
//
// # O mutex
//
// O Ciclo é o dono do mutex que a rodada, o Ligar e o Desligar dos 6 meses
// compartilham: um Desligar entre dois lotes do Ligar de outra aba seria
// sobrescrito pela gravação do OK, e uma rodada no meio do Ligar apagaria com
// outro agora. O ApagarCopias (o botão "Apagar as cópias", F10e) entra nele
// também: um Ligar pode estar fazendo a cópia dos 6 meses. O Estado (o que a
// tela do bloco Histórico lê) é só leitura e não entra.
//
// # As cópias do banco
//
// O ciclo nunca apaga as cópias ao lado do banco (as de antes do dono, da
// nuvem e dos 6 meses): elas guardam o texto que os 6 meses apagam, e a
// decisão de se desfazer delas é de quem usa (ADR-019, "Decisões da F10", D4).
// Só o ApagarCopias, chamado pela rota depois da pergunta na tela, as apaga.
//
// # O que vai para o log
//
// Só contagens. O expurgo nem chega a ver o texto: o contrato que ele usa
// devolve números. A linha "lixeira: expurgo" sai na subida (a prova de que
// o expurgo está ligado), quando algo foi apagado, ou quando o número dos que
// esperam a nuvem mudou desde a rodada anterior: o PC que segura uma lixeira
// por meses não escreve uma linha por hora. Os 6 meses: "historico: 6 meses
// apagados", "ligados", "desligados", "pausados" (WARN) e "pulados" (sem o
// dono fixo, na subida). Nunca id, texto nem caminho.
package expurgo

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Intervalo é o tempo entre duas rodadas depois da primeira.
const Intervalo = time.Hour

// Repositorio é o pedaço do histórico que o expurgo usa.
// storage.TranscriptionRepository o satisfaz.
type Repositorio interface {
	PurgeExpired(ctx context.Context, now time.Time) (storage.ResultadoDoExpurgo, error)
}

// SeisMeses é o pedaço dos 6 meses que o ciclo usa. *storage.SeisMeses o
// satisfaz.
type SeisMeses interface {
	Passo(ctx context.Context, agora time.Time) (storage.PassoDosSeisMeses, error)
	GravarRodada(ctx context.Context, agora time.Time) error
	Ligar(ctx context.Context, agora time.Time, previa int) (storage.ResultadoDoLigar, error)
	Desligar(ctx context.Context) error
	// Estado e ApagarCopias são o que a tela do bloco Histórico usa (F10d e
	// F10e), pela Local API.
	Estado(ctx context.Context, agora time.Time) (storage.EstadoDosSeisMeses, error)
	ApagarCopias(ctx context.Context) (storage.ResultadoDasCopias, error)
}

// ErrParado: o ciclo já parou (o Agent está fechando o banco).
var ErrParado = errors.New("expurgo: o ciclo já parou")

// ErrSemSeisMeses: o ciclo foi montado sem os 6 meses.
var ErrSemSeisMeses = errors.New("expurgo: o ciclo não tem os 6 meses")

// Ciclo é o expurgo em andamento. Pare com Parar antes de fechar o banco.
type Ciclo struct {
	relogio clock.Clock
	repo    Repositorio
	seis    SeisMeses
	log     *slog.Logger

	// mu serializa as rodadas, o Ligar, o Desligar e o Parar: Parar espera
	// a rodada em curso terminar, e quem fecha o banco logo depois não o
	// tira de baixo de um DELETE.
	mu     sync.Mutex
	parado bool
	// esperando é o EsperandoANuvem da última rodada que deu certo (-1 antes
	// da primeira): a linha do log sai quando ele muda.
	esperando int
}

// Iniciar roda a primeira rodada agora, antes de devolver, e agenda as
// seguintes a cada Intervalo. seis pode ser nil: só a lixeira, sem os 6
// meses e sem a gravação da rodada.
//
// Falha de uma rodada não para o ciclo: vai para o log, e a próxima
// tenta de novo.
func Iniciar(relogio clock.Clock, repo Repositorio, seis SeisMeses, log *slog.Logger) *Ciclo {
	c := &Ciclo{relogio: relogio, repo: repo, seis: seis, log: log, esperando: -1}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rodar(true)
	c.relogio.AfterFunc(Intervalo, c.disparar)
	return c
}

func (c *Ciclo) disparar() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parado {
		return
	}
	c.rodar(false)
	c.relogio.AfterFunc(Intervalo, c.disparar)
}

// rodar exige c.mu.
func (c *Ciclo) rodar(naSubida bool) {
	ctx := context.Background()
	agora := c.relogio.Now()
	c.expurgarALixeira(ctx, agora, naSubida)
	if c.seis == nil {
		return
	}
	if gravar := c.passoDosSeisMeses(ctx, agora, naSubida); !gravar {
		return
	}
	if err := c.seis.GravarRodada(ctx, agora); err != nil {
		c.log.Warn("historico: não gravei a rodada do expurgo; tento de novo no próximo ciclo", "err", err)
	}
}

// expurgarALixeira é o passo 1 da rodada. Exige c.mu.
func (c *Ciclo) expurgarALixeira(ctx context.Context, agora time.Time, naSubida bool) {
	r, err := c.repo.PurgeExpired(ctx, agora)
	if err != nil {
		c.log.Warn("lixeira: expurgo falhou; tento de novo no próximo ciclo", "err", err)
		return
	}
	mudou := r.EsperandoANuvem != c.esperando
	c.esperando = r.EsperandoANuvem
	if r.Apagados > 0 || naSubida || mudou {
		// Na subida sai mesmo com zero: é a prova, no log, de que o expurgo
		// está ligado. Nas rodadas seguintes, zero apagado com a mesma
		// espera seria uma linha por hora dizendo nada.
		c.log.Info("lixeira: expurgo", "apagados", r.Apagados, "esperando_a_nuvem", r.EsperandoANuvem,
			"proximo_em", Intervalo)
	}
}

// passoDosSeisMeses é o passo 2 da rodada; devolve se a rodada pode ser
// gravada: não pode quando o freio pausou a regra, nem quando o passo falhou.
// Exige c.mu.
//
// O passo que falha pode ser a transação da pausa que voltou atrás (o freio
// disparou, e o COMMIT falhou com o banco ocupado, por exemplo): o OK e a
// rodada velha continuam no banco. Gravar a rodada agora apagaria o freio, e a
// rodada seguinte apagaria sem pausar e sem a pergunta voltar (D2). Sem a
// gravação, a seguinte freia de novo e tenta pausar outra vez; na lixeira, o
// freio só segura mais. Se a falha for de outra parte do passo (um lote do
// apagar), a rodada seguinte compara com uma rodada uma hora mais velha, e nada
// muda.
func (c *Ciclo) passoDosSeisMeses(ctx context.Context, agora time.Time, naSubida bool) bool {
	p, err := c.seis.Passo(ctx, agora)
	switch {
	case err != nil:
		c.log.Warn("historico: o passo dos 6 meses falhou; tento de novo no próximo ciclo",
			"apagados", p.Apagados, "err", err)
		return false
	case p.SemDonoFixo:
		// A regra antiga do dono é provisória: apagar com ela poderia apagar
		// o que o painel nunca mostrou. Na subida, a contagem do que pulou.
		if naSubida {
			c.log.Info("historico: 6 meses pulados", "motivo", "sem_dono_fixo", "fora_da_janela", p.Pulados)
		}
	case p.Pausada:
		c.log.Warn("historico: 6 meses pausados; nada foi apagado, e a pergunta volta nas Configurações",
			"motivo", "o Agent ficou parado mais de 7 dias, ou o relógio do PC pulou",
			"dias_parado", p.DiasParado, "previa", p.Previa)
		return false
	case p.Apagados > 0:
		c.log.Info("historico: 6 meses apagados", "apagados", p.Apagados)
	}
	return true
}

// Ligar é o OK dos 6 meses (storage.SeisMeses.Ligar), com o agora do relógio
// do ciclo, dentro do mutex: nenhuma rodada, nenhum outro Ligar e nenhum
// Desligar passa no meio. Os erros do storage voltam como estão (quem chama
// escolhe a frase), menos o storage.ErrLigadoIncompleto: a regra ligou, e o
// resultado volta sem erro, com Incompleto.
func (c *Ciclo) Ligar(ctx context.Context, previa int) (storage.ResultadoDoLigar, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parado {
		return storage.ResultadoDoLigar{}, ErrParado
	}
	if c.seis == nil {
		return storage.ResultadoDoLigar{}, ErrSemSeisMeses
	}
	r, err := c.seis.Ligar(ctx, c.relogio.Now(), previa)
	if errors.Is(err, storage.ErrLigadoIncompleto) {
		// O OK foi gravado e parte saiu: a regra está ligada, e o resto sai na
		// rodada seguinte. Não é recusa, e quem chama recebe o resultado
		// (Incompleto) sem erro; o log diz a causa.
		c.log.Warn("historico: 6 meses ligados, mas nem todos saíram; o resto sai na próxima rodada",
			"previa", r.Previa, "apagados", r.Apagados, "copia", copiaDoLigar(r), "incompleto", true, "err", err)
		return r, nil
	}
	if err != nil {
		campos := []any{"motivo", motivoDoLigar(err), "apagados", r.Apagados}
		var ec *storage.ErroDaCopia
		if errors.As(err, &ec) {
			// O detalhe tem o nome do arquivo, nunca o caminho.
			campos = append(campos, "detalhe", ec.Detalhe)
		}
		c.log.Warn("historico: os 6 meses não ligaram", campos...)
		return r, err
	}
	if r.JaLigada {
		return r, nil
	}
	c.log.Info("historico: 6 meses ligados", "previa", r.Previa, "apagados", r.Apagados, "copia", copiaDoLigar(r))
	return r, nil
}

// copiaDoLigar diz no log se o Ligar fez a cópia ou se ela já existia.
func copiaDoLigar(r storage.ResultadoDoLigar) string {
	if r.CopiaJaExistia {
		return "ja_existia"
	}
	return "feita"
}

// Desligar volta ao de antes do OK (storage.SeisMeses.Desligar), dentro do
// mutex: espera um Ligar em curso terminar.
func (c *Ciclo) Desligar(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parado {
		return ErrParado
	}
	if c.seis == nil {
		return ErrSemSeisMeses
	}
	if err := c.seis.Desligar(ctx); err != nil {
		c.log.Warn("historico: os 6 meses não desligaram", "err", err)
		return err
	}
	c.log.Info("historico: 6 meses desligados")
	return nil
}

// Estado é o que a tela do bloco Histórico mostra (storage.SeisMeses.Estado),
// com o agora do relógio do ciclo: a contagem do que o OK apagaria, se a regra
// está ligada ou pausada, e quantas cópias há. É leitura: não entra no mutex,
// e o GET de uma aba não espera o Ligar de outra terminar.
func (c *Ciclo) Estado(ctx context.Context) (storage.EstadoDosSeisMeses, error) {
	if c.seis == nil {
		return storage.EstadoDosSeisMeses{}, ErrSemSeisMeses
	}
	return c.seis.Estado(ctx, c.relogio.Now())
}

// ApagarCopias é o botão "Apagar as cópias" (D4; storage.SeisMeses.ApagarCopias),
// dentro do mutex: nem uma rodada nem um Ligar passa no meio (o Ligar pode
// estar fazendo uma cópia). Quem chama já fez a pergunta na tela; o ciclo nunca
// o chama sozinho. O log só tem contagens.
func (c *Ciclo) ApagarCopias(ctx context.Context) (storage.ResultadoDasCopias, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parado {
		return storage.ResultadoDasCopias{}, ErrParado
	}
	if c.seis == nil {
		return storage.ResultadoDasCopias{}, ErrSemSeisMeses
	}
	r, err := c.seis.ApagarCopias(ctx)
	if err != nil {
		// O erro já vem sem o caminho (storage.semCaminho).
		c.log.Warn("historico: as cópias não saíram todas", "apagadas", r.Apagadas, "ficaram", r.Ficaram, "err", err)
		return r, err
	}
	c.log.Info("historico: cópias apagadas", "apagadas", r.Apagadas, "ficaram", r.Ficaram)
	return r, nil
}

// motivoDoLigar é o motivo de um Ligar recusado, para o log (lista fechada).
func motivoDoLigar(err error) string {
	var mudou *storage.ErrPreviaMudou
	switch {
	case errors.Is(err, storage.ErrSemDonoFixo):
		return "sem_dono_fixo"
	case errors.Is(err, storage.ErrNadaAApagar):
		return "nada_a_apagar"
	case errors.As(err, &mudou):
		return "previa_mudou"
	case errors.Is(err, storage.ErrCopiasDemais):
		return "copias_demais"
	case errors.Is(err, storage.ErrDiscoCheio):
		return "disco_cheio"
	case errors.Is(err, storage.ErrCopiaDosSeisMeses):
		return "copia_falhou"
	}
	return "outro"
}

// Parar cancela as próximas rodadas e espera a que estiver em curso (e um
// Ligar ou Desligar em curso). Chamar mais de uma vez não faz mal.
//
// O timer pendente não é cancelado, de propósito: se ele já disparou e
// está esperando c.mu, Stop não o alcança — quem segura é parado, e
// parado sozinho já basta. O disparo que sobrar sai sem fazer nada.
func (c *Ciclo) Parar() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parado = true
}
