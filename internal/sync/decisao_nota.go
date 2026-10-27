package syncengine

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As decisões do sync das notas e dos marcadores de nota no PC (ADR-022,
// "Sync", N7), no molde das do ditado (decisao.go): puras, recebem o estado
// lido na transação da resposta (storage.EstadoDaNota,
// storage.EstadoDoMarcadorDeNota) e devolvem a ação, que internal/storage
// executa ali mesmo. Nada aqui toca banco, rede, relógio ou log.
//
// O que muda em relação ao ditado:
//
//   - A lápide sobe: o marcador com storage.CampoApagado é a operação delete
//     (apagar de vez, o expurgo da lixeira, o marcador apagado). A resposta,
//     qualquer que seja, tira o marcador: a linha já saiu daqui (no marcador,
//     a linha da lixeira sai junto). Só a recusa que é de uma nuvem que não
//     sabe de notas (TipoAdiado) deixa o marcador para depois.
//   - O corpo que a nuvem confirmou (storage.Nota.CorpoNuvem, a base da
//     mescla da lista de marcar) anda com a versão: o applied sem mudança no
//     voo grava o corpo atual; o mesclado e o applied com mudança no voo o
//     esquecem (o pull traz o de verdade).
//   - Não há janela de 6 meses: nenhum gone fora_da_janela é esperado.
//   - A posição, a cor, o fixar, o arquivar e os marcadores de uma nota não
//     têm selo: o último que escreve vence na nuvem (decisão da N7). Só o
//     título e o corpo têm selo.

// TipoAdiado: a nuvem recusou a nota ou o marcador por não conhecer o tipo
// (tipo_desconhecido, operacao_invalida: uma nuvem de antes da N7, ou que
// voltou para antes dela). O marcador fica, sem o envio em curso, e o push das
// notas para nesta rodada (Motor.Enviar): a capacidade da nuvem é conferida de
// novo no pull.
const TipoAdiado = "adiado"

// Os códigos de recusa que dizem "esta nuvem não sabe de notas".
const (
	CodigoTipoDesconhecido = "tipo_desconhecido"
	CodigoOperacaoInvalida = "operacao_invalida"
)

// DecisaoDaNota é a ação sobre uma nota e o tipo dela.
type DecisaoDaNota struct {
	Acao storage.AcaoDaNota
	Tipo string
}

// DecisaoDoMarcadorDeNota é a ação sobre um marcador de nota e o tipo dela.
type DecisaoDoMarcadorDeNota struct {
	Acao storage.AcaoDoMarcadorDeNota
	Tipo string
}

// mesmaMutacao diz se o resultado é do envio em curso do marcador.
func mesmaMutacao(m *storage.Marcador, r MutationResult) bool {
	return m != nil && m.EmVoo() && strings.EqualFold(m.MutationID, r.MutationID)
}

// statusDe é o status do resultado, com o duplicate trocado pelo da primeira
// vez.
func statusDe(r MutationResult) (status string, duplicado bool) {
	if r.Status == StatusDuplicate {
		return r.Original, true
	}
	return r.Status, false
}

// ehNuvemSemNotas diz se a recusa é de uma nuvem que não sabe de notas.
func ehNuvemSemNotas(r MutationResult) bool {
	return r.Code == CodigoTipoDesconhecido || r.Code == CodigoOperacaoInvalida
}

// DecidirResultadoDaNota decide o que o resultado r do push faz com a nota e.
func DecidirResultadoDaNota(e storage.EstadoDaNota, r MutationResult) DecisaoDaNota {
	m := e.Marcador
	if !mesmaMutacao(m, r) {
		return DecisaoDaNota{Tipo: TipoIgnorado}
	}
	status, duplicado := statusDe(r)
	if m.Campos&storage.CampoApagado != 0 {
		// A lápide: a nota já saiu daqui. Applied (apagada lá), gone (já
		// estava apagada) e qualquer recusa terminam o marcador; só a nuvem
		// sem notas o deixa para depois, e o id reusado vai com outro id.
		switch {
		case status == StatusRejected && r.Code == CodigoMutacaoReusada:
			return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case status == StatusRejected && ehNuvemSemNotas(r):
			return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoAdiado}
		}
		a := storage.AcaoDaNota{Marcador: storage.MarcadorSai, ApagarLinha: e.Linha != nil}
		switch status {
		case StatusApplied:
			return DecisaoDaNota{Acao: a, Tipo: TipoAplicado}
		case StatusGone:
			return DecisaoDaNota{Acao: a, Tipo: TipoApagado}
		}
		return DecisaoDaNota{Acao: a, Tipo: TipoRecusado}
	}
	if e.Linha == nil {
		// A nota sumiu daqui sem a lápide (não acontece: todo apagar de vez
		// põe o CampoApagado). Nada a gravar.
		return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorSai}, Tipo: TipoIgnorado}
	}
	mudou := e.Linha.Version != m.VersaoEnviada
	semMudanca := storage.MarcadorSai
	if mudou {
		semMudanca = storage.MarcadorReenviar
	}
	switch status {
	case StatusApplied:
		a := storage.AcaoDaNota{Situacao: storage.SituacaoNormal, Marcador: semMudanca}
		if r.Mesclado {
			// A versão (e o corpo) de uma mescla não são guardados: o estado
			// mesclado chega no pull seguinte.
			a.EsquecerCorpoNuvem = true
			return DecisaoDaNota{Acao: a, Tipo: TipoMesclado}
		}
		v := r.EntityVersion
		a.ServerVersion = &v
		if mudou {
			// O corpo que subiu não é mais o da linha.
			a.EsquecerCorpoNuvem = true
		} else {
			a.CorpoNuvemAtual = true
		}
		return DecisaoDaNota{Acao: a, Tipo: TipoAplicado}

	case StatusConflict:
		if mudou {
			// Como no ditado: fica a base velha, e o próximo envio conflita
			// de novo, com o texto mais novo daqui no selo.
			return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoConflito}
		}
		a := storage.AcaoDaNota{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}
		if !duplicado && r.StateNota != nil && notaValida(*r.StateNota, e.ID) {
			g := NotaDaNuvem(*r.StateNota)
			v := r.StateNota.Version
			a.Gravar, a.ServerVersion = &g, &v
		} else {
			a.EsquecerCorpoNuvem = true
		}
		return DecisaoDaNota{Acao: a, Tipo: TipoConflito}

	case StatusRejected:
		switch {
		case r.Code == CodigoMutacaoReusada:
			return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case r.Code == CodigoBaseInvalida:
			// A nuvem não conhece a base (a nota não está lá): sobe inteira.
			return DecisaoDaNota{Acao: storage.AcaoDaNota{ZerarServerVersion: true, EsquecerCorpoNuvem: true,
				Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case ehNuvemSemNotas(r):
			return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoAdiado}
		}
		return DecisaoDaNota{
			Acao: storage.AcaoDaNota{Situacao: storage.SituacaoRecusado, Erro: codigoCurto(r.Code), Marcador: semMudanca},
			Tipo: TipoRecusado,
		}

	case StatusGone:
		if r.Motivo == MotivoApagado {
			// Apagada de vez em outro aparelho: apagar vence.
			return DecisaoDaNota{Acao: storage.AcaoDaNota{ApagarLinha: true}, Tipo: TipoApagado}
		}
	}
	return DecisaoDaNota{
		Acao: storage.AcaoDaNota{Situacao: storage.SituacaoRecusado, Erro: CodigoRespostaDesconhecida, Marcador: semMudanca},
		Tipo: TipoRecusado,
	}
}

// DecidirMudancaDaNota decide o que uma mudança do pull faz com a nota e, com
// as regras do ditado (DecidirMudanca): a nota com marcador pendente é pulada;
// o eco é reconhecido pela versão; a mudança que não vale aqui é "não
// aplicada" e o cursor anda; aplicar não ecoa.
//
// A reconciliação é mais simples que a do ditado: a nota que existe aqui sem a
// versão da nuvem e sem marcador (depois do "Esquecer esta nuvem", ou do
// recomeço de um epoch novo) sobe inteira, com a base vazia, se for diferente
// da da nuvem (a nuvem decide: o conteúdo diferente vira selo, nada se perde);
// igual, só adota a versão.
func DecidirMudancaDaNota(e storage.EstadoDaNota, c Change) DecisaoDaNota {
	if c.EntityType != storage.EntidadeNota || !pareceUUID(e.ID) {
		return DecisaoDaNota{Tipo: TipoNaoAplicado}
	}
	switch c.Operation {
	case OpTombstone:
		if e.Marcador != nil && e.Marcador.Campos&storage.CampoApagado == 0 {
			// O push recebe o gone, e a linha sai por ele.
			return DecisaoDaNota{Tipo: TipoPulado}
		}
		if e.Linha == nil {
			// Nada aqui (a lápide daqui, inclusive, que já apagou a linha):
			// o marcador da lápide, se houver, termina pela resposta do push.
			return DecisaoDaNota{Tipo: TipoEco}
		}
		return DecisaoDaNota{Acao: storage.AcaoDaNota{ApagarLinha: true}, Tipo: TipoApagado}
	case OpUpsert:
		if c.PayloadNota == nil || c.EntityVersion < 1 || !notaValida(*c.PayloadNota, e.ID) {
			return DecisaoDaNota{Tipo: TipoNaoAplicado}
		}
		if e.Marcador != nil {
			return DecisaoDaNota{Tipo: TipoPulado}
		}
		if e.Linha != nil && e.Linha.ServerVersion != nil && *e.Linha.ServerVersion == c.EntityVersion {
			return DecisaoDaNota{Tipo: TipoEco}
		}
		if e.Linha != nil && e.Linha.ServerVersion == nil && notaDiferente(*e.Linha, *c.PayloadNota) {
			return DecisaoDaNota{Acao: storage.AcaoDaNota{Marcador: storage.MarcadorCriar}, Tipo: TipoReconciliado}
		}
		g := NotaDaNuvem(*c.PayloadNota)
		v := c.EntityVersion
		a := storage.AcaoDaNota{Gravar: &g, ServerVersion: &v}
		if e.Linha != nil && e.Linha.SyncState == storage.SituacaoRecusado {
			a.Situacao = storage.SituacaoNormal
		}
		return DecisaoDaNota{Acao: a, Tipo: TipoRecebido}
	}
	return DecisaoDaNota{Tipo: TipoNaoAplicado}
}

// notaDiferente diz se a nota daqui difere da da nuvem em algo que a pessoa vê
// (o conteúdo, a cor, o fixar, o arquivar, os marcadores, a lixeira). A
// posição fica de fora: duas listas reordenadas não são uma mudança a guardar.
func notaDiferente(l storage.Nota, n Nota) bool {
	cor := func(c *string) string {
		if c == nil {
			return ""
		}
		return *c
	}
	return l.Titulo != n.Titulo || l.Corpo != n.Corpo || cor(l.Cor) != cor(n.Cor) ||
		l.Fixada != n.Fixada || l.Arquivada != n.Arquivada || (l.DeletedAt != nil) != (n.DeletedAt != nil) ||
		!mesmosIDs(l.Marcadores, n.Marcadores)
}

// mesmosIDs compara dois conjuntos de ids, sem caixa nem ordem.
func mesmosIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := idsDaNuvem(a), idsDaNuvem(b)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// textoDaNotaValido: cabe no teto e é UTF-8 sem o caractere nulo (o que o
// storage recusaria e desfaria a página inteira).
func textoDaNotaValido(s string, teto int) bool {
	return len(s) <= teto && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// notaValida é a recusa por regra da nota que vem da nuvem: o id é o da
// mudança, a versão é positiva, a data existe, os textos cabem nos tetos do PC,
// a cor é uma das oito, a posição é uma chave válida e os marcadores são ids.
func notaValida(n Nota, id string) bool {
	if !pareceUUID(id) || !strings.EqualFold(n.ID, id) || n.Version < 1 || n.CreatedAt.IsZero() {
		return false
	}
	if !textoDaNotaValido(n.Titulo, storage.MaxTituloDaNotaBytes) || !textoDaNotaValido(n.Corpo, storage.MaxCorpoDaNotaBytes) {
		return false
	}
	if s := n.VersaoEmConflito; s != nil &&
		(!textoDaNotaValido(s.Titulo, storage.MaxTituloDaNotaBytes) || !textoDaNotaValido(s.Corpo, storage.MaxCorpoDaNotaBytes)) {
		return false
	}
	if n.Cor != nil && !storage.CorValida(*n.Cor) {
		return false
	}
	if !storage.PosicaoValida(n.Posicao) {
		return false
	}
	if n.DitadoDeOrigem != nil && !pareceUUID(*n.DitadoDeOrigem) {
		return false
	}
	for _, m := range n.Marcadores {
		if !pareceUUID(m) {
			return false
		}
	}
	return true
}

// NotaDaNuvem é a nota da nuvem como linha local (o dono, as versões locais e
// a situação são de quem grava).
func NotaDaNuvem(n Nota) storage.Nota {
	out := storage.Nota{
		ID: strings.ToLower(n.ID), Titulo: n.Titulo, Corpo: n.Corpo, Cor: n.Cor, Fixada: n.Fixada,
		Arquivada: n.Arquivada, Posicao: n.Posicao, Marcadores: idsDaNuvem(n.Marcadores),
		CreatedAt: n.CreatedAt.UTC(), UpdatedAt: n.UpdatedAt.UTC(),
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = out.CreatedAt
	}
	if n.DitadoDeOrigem != nil {
		d := strings.ToLower(*n.DitadoDeOrigem)
		out.DitadoDeOrigem = &d
	}
	if n.DeletedAt != nil {
		d := n.DeletedAt.UTC()
		out.DeletedAt = &d
	}
	if s := n.VersaoEmConflito; s != nil {
		out.Conflito = &storage.ConflitoDaNota{Titulo: s.Titulo, Corpo: s.Corpo, DeviceID: s.DeviceID, Em: s.ConflictAt.UTC()}
	}
	return out
}

// DecidirResultadoDoMarcadorDeNota decide o que o resultado r do push faz com
// o marcador de nota e. O marcador não tem selo: o nome do último que escreve
// vence na nuvem.
func DecidirResultadoDoMarcadorDeNota(e storage.EstadoDoMarcadorDeNota, r MutationResult) DecisaoDoMarcadorDeNota {
	m := e.Marcador
	if !mesmaMutacao(m, r) {
		return DecisaoDoMarcadorDeNota{Tipo: TipoIgnorado}
	}
	status, duplicado := statusDe(r)
	if m.Campos&storage.CampoApagado != 0 {
		switch {
		case status == StatusRejected && r.Code == CodigoMutacaoReusada:
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case status == StatusRejected && ehNuvemSemNotas(r):
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoAdiado}
		}
		// A linha da lixeira dos marcadores (o soft delete daqui) sai junto:
		// a nuvem já não tem o marcador.
		a := storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorSai, ApagarLinha: e.Linha != nil}
		switch status {
		case StatusApplied:
			return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoAplicado}
		case StatusGone:
			return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoApagado}
		}
		return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoRecusado}
	}
	if e.Linha == nil {
		return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorSai}, Tipo: TipoIgnorado}
	}
	mudou := e.Linha.Version != m.VersaoEnviada
	semMudanca := storage.MarcadorSai
	if mudou {
		semMudanca = storage.MarcadorReenviar
	}
	switch status {
	case StatusApplied:
		a := storage.AcaoDoMarcadorDeNota{Situacao: storage.SituacaoNormal, Marcador: semMudanca}
		if r.Mesclado {
			return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoMesclado}
		}
		v := r.EntityVersion
		a.ServerVersion = &v
		return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoAplicado}
	case StatusConflict:
		// A nuvem não dá conflito de marcador (o nome do último vence); se der,
		// vale o estado dela, como num pull.
		if mudou {
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoConflito}
		}
		a := storage.AcaoDoMarcadorDeNota{Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}
		if !duplicado && r.StateMarcador != nil && marcadorValido(*r.StateMarcador, e.ID) {
			g := MarcadorDeNotaDaNuvem(*r.StateMarcador)
			v := r.StateMarcador.Version
			a.Gravar, a.ServerVersion = &g, &v
		}
		return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoConflito}
	case StatusRejected:
		switch {
		case r.Code == CodigoMutacaoReusada:
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case r.Code == CodigoBaseInvalida:
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{ZerarServerVersion: true,
				Marcador: storage.MarcadorReenviar}, Tipo: TipoReenviar}
		case ehNuvemSemNotas(r):
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorReenviar}, Tipo: TipoAdiado}
		}
		return DecisaoDoMarcadorDeNota{
			Acao: storage.AcaoDoMarcadorDeNota{Situacao: storage.SituacaoRecusado, Erro: codigoCurto(r.Code), Marcador: semMudanca},
			Tipo: TipoRecusado,
		}
	case StatusGone:
		if r.Motivo == MotivoApagado {
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{ApagarLinha: true}, Tipo: TipoApagado}
		}
	}
	return DecisaoDoMarcadorDeNota{
		Acao: storage.AcaoDoMarcadorDeNota{Situacao: storage.SituacaoRecusado, Erro: CodigoRespostaDesconhecida, Marcador: semMudanca},
		Tipo: TipoRecusado,
	}
}

// DecidirMudancaDoMarcadorDeNota decide o que uma mudança do pull faz com o
// marcador de nota e. O "marcador que espera" (a linha que uma nota da nuvem
// citou antes de o marcador chegar, storage.SituacaoAguardando) não tem versão
// da nuvem nem marcador: o estado que chega a preenche.
func DecidirMudancaDoMarcadorDeNota(e storage.EstadoDoMarcadorDeNota, c Change) DecisaoDoMarcadorDeNota {
	if c.EntityType != storage.EntidadeMarcadorDeNota || !pareceUUID(e.ID) {
		return DecisaoDoMarcadorDeNota{Tipo: TipoNaoAplicado}
	}
	switch c.Operation {
	case OpTombstone:
		if e.Marcador != nil && e.Marcador.Campos&storage.CampoApagado == 0 {
			return DecisaoDoMarcadorDeNota{Tipo: TipoPulado}
		}
		if e.Linha == nil {
			return DecisaoDoMarcadorDeNota{Tipo: TipoEco}
		}
		return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{ApagarLinha: true}, Tipo: TipoApagado}
	case OpUpsert:
		if c.PayloadMarcador == nil || c.EntityVersion < 1 || !marcadorValido(*c.PayloadMarcador, e.ID) {
			return DecisaoDoMarcadorDeNota{Tipo: TipoNaoAplicado}
		}
		if e.Marcador != nil {
			return DecisaoDoMarcadorDeNota{Tipo: TipoPulado}
		}
		if e.Linha != nil && e.Linha.ServerVersion != nil && *e.Linha.ServerVersion == c.EntityVersion {
			return DecisaoDoMarcadorDeNota{Tipo: TipoEco}
		}
		if e.Linha != nil && e.Linha.ServerVersion == nil && e.Linha.SyncState != storage.SituacaoAguardando &&
			e.Linha.DeletedAt == nil && e.Linha.Nome != c.PayloadMarcador.Nome {
			// O marcador daqui, sem a versão da nuvem, com outro nome: sobe
			// inteiro (o nome daqui vence, como qualquer escrita).
			return DecisaoDoMarcadorDeNota{Acao: storage.AcaoDoMarcadorDeNota{Marcador: storage.MarcadorCriar}, Tipo: TipoReconciliado}
		}
		g := MarcadorDeNotaDaNuvem(*c.PayloadMarcador)
		v := c.EntityVersion
		a := storage.AcaoDoMarcadorDeNota{Gravar: &g, ServerVersion: &v}
		if e.Linha != nil && e.Linha.SyncState == storage.SituacaoRecusado {
			a.Situacao = storage.SituacaoNormal
		}
		return DecisaoDoMarcadorDeNota{Acao: a, Tipo: TipoRecebido}
	}
	return DecisaoDoMarcadorDeNota{Tipo: TipoNaoAplicado}
}

// marcadorValido é a recusa por regra do marcador que vem da nuvem: o id, a
// versão, a data e o nome (de 1 a storage.MaxNomeDoMarcador caracteres, sem
// controle).
func marcadorValido(l MarcadorDaNuvem, id string) bool {
	if !pareceUUID(id) || !strings.EqualFold(l.ID, id) || l.Version < 1 || l.CreatedAt.IsZero() {
		return false
	}
	n := utf8.RuneCountInString(l.Nome)
	if strings.TrimSpace(l.Nome) == "" || n > storage.MaxNomeDoMarcador || !utf8.ValidString(l.Nome) {
		return false
	}
	for _, r := range l.Nome {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// MarcadorDeNotaDaNuvem é o marcador da nuvem como linha local.
func MarcadorDeNotaDaNuvem(l MarcadorDaNuvem) storage.MarcadorDeNota {
	out := storage.MarcadorDeNota{
		ID: strings.ToLower(l.ID), Nome: l.Nome, CreatedAt: l.CreatedAt.UTC(), UpdatedAt: l.UpdatedAt.UTC(),
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = out.CreatedAt
	}
	return out
}
