package syncengine

import (
	"strings"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões do sync no PC (ADR-019, "Contrato de sync", "Conflito", "Eco e
// mescla", "Lápides, gone, epoch e recomeço"; "Decisões da F2" e da F7a/b).
// Puras: recebem o estado do ditado lido na transação da resposta e devolvem
// a ação (storage.AcaoLocal), que internal/storage executa ali mesmo. Nada
// aqui toca banco, rede, relógio ou log.

// O tipo de cada decisão, para as contagens do log (lista fechada).
const (
	// Do push.
	TipoAplicado     = "aplicado"
	TipoMesclado     = "mesclado"
	TipoConflito     = "conflito"
	TipoRecusado     = "recusado"
	TipoReenviar     = "reenviar"
	TipoApagado      = "apagado"
	TipoForaDaJanela = "fora_da_janela"
	TipoIgnorado     = "ignorado"
	// Do pull.
	TipoRecebido    = "recebido"
	TipoPulado      = "pulado"
	TipoEco         = "eco"
	TipoNaoAplicado = "nao_aplicado"
	// TipoReconciliado: o ditado existe aqui sem a versão da nuvem, e a nuvem
	// o tem diferente; ele não é sobrescrito, e sobe inteiro (F7c, D3).
	TipoReconciliado = "reconciliado"
)

// CodigoRespostaDesconhecida é o last_sync_error de um ditado cuja resposta
// tinha um status (ou motivo) que este PC não conhece.
const CodigoRespostaDesconhecida = "resposta_desconhecida"

// CodigoTextoGrande é o last_sync_error de um ditado que o PC nem mandou: o
// texto passa do teto da nuvem (storage.MaxTextoBytes).
const CodigoTextoGrande = "texto_grande"

// Decisao é a ação sobre um ditado e o tipo dela.
type Decisao struct {
	Acao storage.AcaoLocal
	Tipo string
}

// DecidirResultado decide o que o resultado r do push faz com o ditado e. O
// resultado só vale para o envio em curso: sem marcador, ou com o marcador de
// outro envio, ele é ignorado.
//
// "Mudou" é a version local diferente da que foi lida para montar o envio
// (alguém editou durante o voo): aí o marcador fica, com os campos, e o
// ditado vai de novo, com a base que o PC conhece.
func DecidirResultado(e storage.EstadoDoItem, r MutationResult) Decisao {
	m := e.Marcador
	if m == nil || !m.EmVoo() || !strings.EqualFold(m.MutationID, r.MutationID) {
		return Decisao{Tipo: TipoIgnorado}
	}
	if e.Linha == nil {
		// O ditado sumiu daqui durante o voo (expurgado): nada a gravar.
		return Decisao{Acao: storage.AcaoLocal{Marcador: storage.MarcadorSai}, Tipo: TipoIgnorado}
	}
	mudou := e.Linha.Version != m.VersaoEnviada
	semMudanca := storage.MarcadorSai
	if mudou {
		semMudanca = storage.MarcadorReenviar
	}

	status := r.Status
	duplicado := status == StatusDuplicate
	if duplicado {
		// O reenvio de um envio que a nuvem já tinha aplicado: vale a
		// primeira resposta (sem o state, que a nuvem não guarda).
		status = r.Original
	}
	switch status {
	case StatusApplied:
		a := storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: semMudanca}
		if r.Mesclado {
			// A versão de uma mescla não é guardada: o estado mesclado
			// chega no pull seguinte, onde a versão difere.
			return Decisao{Acao: a, Tipo: TipoMesclado}
		}
		v := r.EntityVersion
		a.ServerVersion = &v
		return Decisao{Acao: a, Tipo: TipoAplicado}

	case StatusConflict:
		if mudou {
			// Fica com a base velha: o próximo envio conflita de novo, e o
			// texto mais novo deste PC vai para o selo.
			return Decisao{Acao: storage.AcaoLocal{Marcador: storage.MarcadorReenviar}, Tipo: TipoConflito}
		}
		a := storage.AcaoLocal{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}
		if !duplicado && r.State != nil && itemValido(*r.State, e.ID) {
			// O estado atual: o texto que venceu, e o deste PC no selo.
			g := LinhaDoItem(*r.State)
			v := r.State.Version
			a.Gravar, a.ServerVersion = &g, &v
		}
		// Sem o state (o duplicate), a versão não é gravada: o estado chega
		// no pull, que não aconteceu entre o envio sem resposta e o reenvio.
		return Decisao{Acao: a, Tipo: TipoConflito}

	case StatusRejected:
		switch r.Code {
		case CodigoMutacaoReusada:
			// Outro aparelho já usou este id (um historico.db copiado):
			// outro id e de novo.
			return Decisao{Acao: storage.AcaoLocal{Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case CodigoBaseInvalida:
			// A nuvem não conhece a base: o ditado sobe inteiro.
			return Decisao{Acao: storage.AcaoLocal{ZerarServerVersion: true, Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		}
		code := codigoCurto(r.Code)
		return Decisao{
			Acao: storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: code, Marcador: semMudanca},
			Tipo: TipoRecusado,
		}

	case StatusGone:
		switch r.Motivo {
		case MotivoApagado:
			// Apagado de vez na nuvem: apagar vence.
			return Decisao{Acao: storage.AcaoLocal{ApagarLinha: true}, Tipo: TipoApagado}
		case MotivoForaDaJanela:
			// O PC mantém a cópia e para de sincronizá-la.
			return Decisao{
				Acao: storage.AcaoLocal{Situacao: storage.SituacaoForaDaJanela, Marcador: storage.MarcadorSai},
				Tipo: TipoForaDaJanela,
			}
		}
	}
	return Decisao{
		Acao: storage.AcaoLocal{Situacao: storage.SituacaoRecusado, Erro: CodigoRespostaDesconhecida, Marcador: semMudanca},
		Tipo: TipoRecusado,
	}
}

// DecidirMudanca decide o que uma mudança do pull faz com o ditado e.
//
//   - Com marcador pendente, o ditado é PULADO: o aplicador nunca sobrescreve
//     um item com pendência. O estado chega pela resposta do push (o conflict
//     traz o state) ou, quando a nuvem mescla, pelo pull seguinte.
//   - O eco é reconhecido pela VERSÃO (a entity_version é a server_version
//     que o PC guardou), nunca pelo device_id.
//   - Uma mudança que não vale aqui (recusa por regra) é "não aplicada", e o
//     cursor anda do mesmo jeito: só erro de E/S segura o cursor.
//   - Aplicar não ecoa (storage.AcaoLocal.Gravar nunca põe marcador) e sobe a
//     version local.
//   - A reconciliação (ADR-019, "Decisões da F7c", D3): um ditado que existe
//     aqui SEM a versão da nuvem (nunca confirmado por esta conexão: depois do
//     "Esquecer esta nuvem" e de voltar à mesma conta, ou depois do recomeço
//     de um epoch novo), sem marcador, que MUDOU AQUI (depois do esquecer;
//     no epoch novo, sempre: e.MudouAqui) e em que o lado deste PC pode
//     vencer (pcPodeVencer: outro texto, ou na lixeira só aqui), não é
//     sobrescrito: ganha um marcador de criar (sobe inteiro, com a base
//     vazia), e a nuvem decide pelas regras de sempre (apagar vence; texto
//     contra texto vira selo). O que não mudou aqui é o ditado velho: vale o
//     da nuvem, sem selo à toa. Igual, ou diferente só porque a NUVEM o pôs
//     na lixeira, é aplicado como qualquer mudança: apagar vence, e subir o
//     ditado fora da lixeira não mudaria nada lá (a nuvem responderia
//     mesclado, com versão nova, e o pull seguinte reconciliaria de novo,
//     sem fim).
func DecidirMudanca(e storage.EstadoDoItem, c Change) Decisao {
	if c.EntityType != storage.EntidadeDitado || e.DeOutroDono || !pareceUUID(e.ID) {
		return Decisao{Tipo: TipoNaoAplicado}
	}
	switch c.Operation {
	case OpTombstone:
		if e.Marcador != nil {
			// O push recebe o gone, e a linha sai por ele.
			return Decisao{Tipo: TipoPulado}
		}
		if e.Linha == nil {
			return Decisao{Tipo: TipoEco}
		}
		return Decisao{Acao: storage.AcaoLocal{ApagarLinha: true}, Tipo: TipoApagado}
	case OpUpsert:
		if c.Payload == nil || c.EntityVersion < 1 || !itemValido(*c.Payload, e.ID) {
			return Decisao{Tipo: TipoNaoAplicado}
		}
		if e.Marcador != nil {
			return Decisao{Tipo: TipoPulado}
		}
		if e.Linha != nil && e.Linha.ServerVersion != nil && *e.Linha.ServerVersion == c.EntityVersion {
			return Decisao{Tipo: TipoEco}
		}
		if e.Linha != nil && e.Linha.ServerVersion == nil && e.MudouAqui && pcPodeVencer(*e.Linha, *c.Payload) {
			return Decisao{Acao: storage.AcaoLocal{Marcador: storage.MarcadorCriar}, Tipo: TipoReconciliado}
		}
		g := LinhaDoItem(*c.Payload)
		v := c.EntityVersion
		a := storage.AcaoLocal{Gravar: &g, ServerVersion: &v}
		if e.Linha != nil && e.Linha.SyncState == storage.SituacaoRecusado {
			// A mudança recusada foi sobrescrita pelo estado da nuvem.
			a.Situacao = storage.SituacaoNormal
		}
		return Decisao{Acao: a, Tipo: TipoRecebido}
	}
	return Decisao{Tipo: TipoNaoAplicado}
}

// pcPodeVencer diz se subir o ditado daqui, inteiro e com a base vazia, pode
// mudar alguma coisa na nuvem: o texto final é outro (vira o texto da nuvem
// ou o selo), ou o ditado está na lixeira só aqui (apagar vence). Na lixeira
// só na NUVEM, com o mesmo texto, o lado daqui nunca vence (restaurar contra
// uma lixeira de outro aparelho não vale), e a reconciliação não convergiria.
func pcPodeVencer(l storage.Transcription, it Item) bool {
	return l.FinalText != it.FinalText || l.DeletedAt != nil && it.DeletedAt == nil
}

// itemValido é a recusa por regra do que vem da nuvem: o id é o da mudança
// (um UUID), a data existe, os textos cabem no teto e a versão é positiva.
func itemValido(it Item, id string) bool {
	if !pareceUUID(id) || !strings.EqualFold(it.ID, id) || it.Version < 1 || it.CreatedAt.IsZero() {
		return false
	}
	textos := []string{it.RawText, it.FinalText}
	if it.VersaoEmConflito != nil {
		textos = append(textos, it.VersaoEmConflito.FinalText)
	}
	for _, s := range textos {
		if len(s) > storage.MaxTextoBytes || !utf8.ValidString(s) {
			return false
		}
	}
	return true
}

// LinhaDoItem é o ditado da nuvem como linha local (o id, o dono e as
// versões são de quem grava). target_app nunca vem.
func LinhaDoItem(it Item) storage.Transcription {
	t := storage.Transcription{
		ID: strings.ToLower(it.ID), DeviceID: it.DeviceID, SourcePlatform: it.SourcePlatform,
		RawText: it.RawText, FinalText: it.FinalText, Mode: it.Mode, Language: it.Language,
		ASRProvider: it.ASRProvider, ASRModel: it.ASRModel, DurationMs: it.DurationMs,
		ProcessingMs: it.ProcessingMs, Confidence: it.Confidence, Status: it.Status,
		CreatedAt: it.CreatedAt.UTC(), UpdatedAt: it.UpdatedAt.UTC(),
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = t.CreatedAt
	}
	if it.DeletedAt != nil {
		d := it.DeletedAt.UTC()
		t.DeletedAt = &d
	}
	if s := it.VersaoEmConflito; s != nil {
		t.Conflito = &storage.Conflito{FinalText: s.FinalText, DeviceID: s.DeviceID, Em: s.ConflictAt.UTC()}
	}
	return t
}

// codigoCurto guarda um código de recusa da nuvem, que é de uma lista
// fechada; qualquer coisa fora do formato (grande, com caractere estranho)
// vira CodigoRespostaDesconhecida, para nada de fora ir parar no banco.
func codigoCurto(c string) string {
	if c == "" || len(c) > 40 {
		return CodigoRespostaDesconhecida
	}
	for i := 0; i < len(c); i++ {
		if !(c[i] >= 'a' && c[i] <= 'z' || c[i] == '_') {
			return CodigoRespostaDesconhecida
		}
	}
	return c
}

// pareceUUID: 8-4-4-4-12 em hexadecimal.
func pareceUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
