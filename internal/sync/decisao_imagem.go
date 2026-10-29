package syncengine

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões das imagens das notas (ADR-022, "Decisões da N10b"), puras: o que
// fazer com uma imagem que o pull trouxe, com a resposta de um PUT ou de um
// DELETE, e com a de um GET. O motor as aplica (motor_imagens.go), e os testes
// as conferem em tabela, sem banco nem rede.

// Os tipos das decisões das imagens, para o relatório da rodada (Relatorio.
// Imagens): só contagens.
const (
	// O pull.
	TipoImagemRecebida    = "recebida"     // veio da nuvem; espera o download
	TipoImagemApagada     = "apagada"      // a lápide da nuvem tirou a imagem daqui
	TipoImagemEco         = "eco"          // a imagem daqui, que a nuvem confirma
	TipoImagemConflito    = "conflito"     // o mesmo id com outros bytes: fica a daqui
	TipoImagemPulada      = "pulada"       // tirada aqui; o DELETE vai subir
	TipoImagemNaoAplicada = "nao_aplicada" // a nota não está aqui, ou os metadados não valem

	// O envio.
	TipoImagemSubiu       = "subiu"        // o PUT valeu (ou a nuvem já a tinha)
	TipoImagemTirada      = "tirada"       // o DELETE valeu
	TipoImagemEsperando   = "esperando"    // uma recusa que pode passar: tenta depois
	TipoImagemRecusada    = "recusada"     // a nuvem recusou de vez: fica só neste PC
	TipoImagemSumiu       = "sumiu"        // a nuvem tirou a imagem (ou apagou a nota)
	TipoImagemSemArquivo  = "sem_arquivo"  // o arquivo daqui sumiu ou não confere
	TipoImagemAdiada      = "adiada"       // a rede caiu (ou a nuvem pediu espera): a fase parou
	TipoImagemTiradaAntes = "tirada_antes" // tirada da nota antes da vez dela: nada sobe

	// O download.
	TipoImagemBaixada        = "baixada"         // o arquivo chegou, conferido
	TipoImagemSumiuNaNuvem   = "sumiu_na_nuvem"  // a nuvem não tem mais: a linha sai
	TipoImagemEsperandoBaixa = "esperando_baixa" // não baixou: tenta depois
	TipoImagemEstragada      = "estragada"       // os bytes não conferem: tenta depois
)

// Os códigos de espera e de recusa que o PC grava na linha (last_sync_error),
// quando a nuvem não mandou um código.
const (
	CodigoImagemStatus    = "status"
	CodigoImagemEstragada = "imagem_estragada"
	// CodigoImagemNaoGravada: o arquivo baixado não se gravou aqui.
	CodigoImagemNaoGravada = "imagem_nao_gravada"
)

// EsperaDasImagens é a espera de uma imagem que não subiu por um motivo que
// pode passar, ou que não baixou: a BackoffPolicy do sync com outra escala (de 1
// min a 6 h), porque cada tentativa pode custar até 4 MB de rede. Contada a
// partir de tentou_em, por imagem.
var EsperaDasImagens = BackoffPolicy{Base: time.Minute, Max: 6 * time.Hour}

// EsperaDaImagem é a espera depois de tentativas falhas de uma imagem: a de
// EsperaDasImagens, sem o jitter (a vez de cada imagem é lida de novo a cada
// rodada, e tem de dar a mesma resposta de uma leitura para a outra).
func EsperaDaImagem(tentativas int) time.Duration {
	d := EsperaDasImagens.Base
	for i := 1; i < tentativas && d < EsperaDasImagens.Max; i++ {
		d *= 2
	}
	return min(d, EsperaDasImagens.Max)
}

// ImagemDoPull é o que o pull trouxe de uma imagem: a lápide, ou os metadados
// (da mudança note_image, ou da lista imagens da nota).
type ImagemDoPull struct {
	Tombstone bool
	Meta      *ImagemDaNuvem
}

// DecisaoDaImagem é o tipo (para o relatório) e a ação.
type DecisaoDaImagem struct {
	Tipo string
	Acao storage.AcaoDaImagem
}

var hexDoSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// metaValida confere os metadados que vieram da nuvem: os ids, os tamanhos e o
// SHA-256 (o Conferir do download confere o resto, nos bytes).
func metaValida(m ImagemDaNuvem, id string) bool {
	return pareceUUID(id) && strings.EqualFold(m.ID, id) && pareceUUID(m.NoteID) && m.Largura > 0 && m.Altura > 0 &&
		m.Bytes > 0 && hexDoSHA.MatchString(strings.ToLower(m.SHA256)) && !m.CreatedAt.IsZero()
}

// DecidirMudancaDaImagem decide o que fazer com o que o pull trouxe de uma
// imagem:
//   - a lápide: a imagem daqui sai, com o marcador (a de criar já não tem para
//     onde ir; a lápide daqui terminou na nuvem);
//   - os metadados, com a imagem tirada aqui (a lápide local na fila): nada (o
//     DELETE vai subir);
//   - a imagem daqui com o mesmo SHA-256: a nuvem a tem (na_nuvem), e o marcador
//     de criar sai (o PUT cuja resposta se perdeu valeu); com outro SHA-256, fica
//     a daqui (não acontece com um UUID v7);
//   - a imagem que não está aqui, de uma nota que está: nasce "aguardando", sem
//     o arquivo, até o download. A nota que não está aqui (ainda não chegou, ou
//     foi apagada aqui) deixa a imagem de fora: ela chega com a nota (a lista
//     imagens).
func DecidirMudancaDaImagem(e storage.EstadoDaImagem, c ImagemDoPull) DecisaoDaImagem {
	if !pareceUUID(e.ID) {
		return DecisaoDaImagem{Tipo: TipoImagemNaoAplicada}
	}
	if c.Tombstone {
		if e.Linha == nil {
			return DecisaoDaImagem{Tipo: TipoImagemEco, Acao: storage.AcaoDaImagem{TirarMarcador: e.Marcador != nil}}
		}
		return DecisaoDaImagem{Tipo: TipoImagemApagada, Acao: storage.AcaoDaImagem{ApagarLinha: true, TirarMarcador: true}}
	}
	if c.Meta == nil || !metaValida(*c.Meta, e.ID) {
		return DecisaoDaImagem{Tipo: TipoImagemNaoAplicada}
	}
	if e.Marcador != nil && e.Marcador.Campos&storage.CampoApagado != 0 {
		return DecisaoDaImagem{Tipo: TipoImagemPulada}
	}
	if e.Linha != nil {
		if !strings.EqualFold(e.Linha.SHA256, c.Meta.SHA256) {
			return DecisaoDaImagem{Tipo: TipoImagemConflito}
		}
		return DecisaoDaImagem{Tipo: TipoImagemEco, Acao: storage.AcaoDaImagem{NaNuvem: true, TirarCriar: e.Marcador != nil}}
	}
	if !e.NotaAqui {
		return DecisaoDaImagem{Tipo: TipoImagemNaoAplicada}
	}
	return DecisaoDaImagem{Tipo: TipoImagemRecebida, Acao: storage.AcaoDaImagem{Inserir: &storage.ImagemDaNota{
		ID: strings.ToLower(e.ID), NotaID: strings.ToLower(c.Meta.NoteID), Largura: c.Meta.Largura, Altura: c.Meta.Altura,
		Bytes: c.Meta.Bytes, SHA256: strings.ToLower(c.Meta.SHA256), CreatedAt: c.Meta.CreatedAt.UTC(),
	}}}
}

// DecisaoDoEnvioDaImagem é o tipo e a ação de uma resposta do PUT ou do DELETE.
type DecisaoDoEnvioDaImagem struct {
	Tipo string
	Acao storage.AcaoDaImagemNoEnvio
}

// Os códigos de erro das rotas das imagens da nuvem (imagens_routes.go).
const (
	codigoImageInvalid     = "IMAGE_INVALID"
	codigoImageTooLarge    = "IMAGE_TOO_LARGE"
	codigoImageShaMismatch = "IMAGE_SHA_MISMATCH"
	codigoImageConflict    = "IMAGE_CONFLICT"
)

// codigoDaResposta é o código da resposta para a linha: o da nuvem, ou o status.
func codigoDaResposta(r RespostaDaImagem) string {
	if r.Codigo != "" {
		return r.Codigo
	}
	return CodigoImagemStatus
}

// DecidirEnvioDaImagem decide o que fazer com a resposta do envio de it (o PUT
// da imagem a criar, ou o DELETE da lápide):
//   - o PUT 200 ou 201: subiu (o marcador sai, as tentativas zeram);
//   - o PUT 410 (a imagem tirada na nuvem, ou a nota apagada de vez): o
//     marcador sai; a lápide, ou a da nota, chega pelo pull e tira a daqui;
//   - o PUT que a nuvem recusa pelos bytes (IMAGE_INVALID, IMAGE_TOO_LARGE,
//     IMAGE_SHA_MISMATCH) ou pelo id (IMAGE_CONFLICT): recusada de vez, fica só
//     neste PC (nada some calado: a imagem continua na nota daqui);
//   - qualquer outra (a nota com 10 na nuvem, o teto, a nota da lixeira na
//     nuvem, a nota que a nuvem não tem, um 5xx): espera e tenta de novo
//     (EsperaDasImagens);
//   - o DELETE 204, 404 ou 410: a imagem não está na nuvem, o marcador sai; outro
//     (um 5xx): espera a rodada seguinte, com o marcador.
func DecidirEnvioDaImagem(it storage.ItemDeImagemNaFila, r RespostaDaImagem) DecisaoDoEnvioDaImagem {
	if it.Lapide {
		switch r.Status {
		case http.StatusNoContent, http.StatusOK, http.StatusNotFound, http.StatusGone:
			return DecisaoDoEnvioDaImagem{Tipo: TipoImagemTirada, Acao: storage.AcaoDaImagemNoEnvio{TirarMarcador: true}}
		}
		return DecisaoDoEnvioDaImagem{Tipo: TipoImagemEsperando}
	}
	switch {
	case r.Status == http.StatusOK || r.Status == http.StatusCreated:
		return DecisaoDoEnvioDaImagem{Tipo: TipoImagemSubiu, Acao: storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Subiu: true}}
	case r.Status == http.StatusGone:
		return DecisaoDoEnvioDaImagem{Tipo: TipoImagemSumiu, Acao: storage.AcaoDaImagemNoEnvio{TirarMarcador: true}}
	case r.Codigo == codigoImageInvalid || r.Codigo == codigoImageTooLarge || r.Codigo == codigoImageShaMismatch ||
		r.Codigo == codigoImageConflict || r.Status == http.StatusUnsupportedMediaType || r.Status == http.StatusRequestEntityTooLarge:
		return DecisaoDoEnvioDaImagem{Tipo: TipoImagemRecusada,
			Acao: storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Recusar: true, Erro: codigoDaResposta(r)}}
	}
	return DecisaoDoEnvioDaImagem{Tipo: TipoImagemEsperando, Acao: storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: codigoDaResposta(r)}}
}

// DecisaoDaBaixaDaImagem é o tipo e a ação de uma resposta do GET que não
// gravou o arquivo (o 200 com os bytes vai para GuardarBaixada).
type DecisaoDaBaixaDaImagem struct {
	Tipo string
	Acao storage.AcaoDaImagemNaBaixa
}

// DecidirBaixaDaImagem decide o que fazer com a resposta do GET de uma imagem
// que espera o arquivo, quando ele não foi gravado: o 404 e o 410 (a nuvem não
// tem mais: a imagem tirada, ou a nota apagada de vez) tiram a linha que espera;
// o resto (um 5xx, o 200 sem os bytes) espera e tenta de novo.
func DecidirBaixaDaImagem(r RespostaDaImagem) DecisaoDaBaixaDaImagem {
	switch r.Status {
	case http.StatusNotFound, http.StatusGone:
		return DecisaoDaBaixaDaImagem{Tipo: TipoImagemSumiuNaNuvem, Acao: storage.AcaoDaImagemNaBaixa{Sumiu: true}}
	case http.StatusOK:
		return DecisaoDaBaixaDaImagem{Tipo: TipoImagemEstragada,
			Acao: storage.AcaoDaImagemNaBaixa{Esperar: true, Erro: CodigoImagemEstragada}}
	}
	return DecisaoDaBaixaDaImagem{Tipo: TipoImagemEsperandoBaixa,
		Acao: storage.AcaoDaImagemNaBaixa{Esperar: true, Erro: codigoDaResposta(r)}}
}
