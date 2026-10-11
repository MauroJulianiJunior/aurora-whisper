package cloudstore

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"time"
)

// As preferências e o aceite do ditado no celular (ADR-021, decisões H e N e
// C3b): duas tabelas por conta, dictation_settings (a 0018) e
// dictation_consents (a 0019). Nenhuma sincroniza com os PCs, nenhuma passa
// pelo ponto único de gravação do histórico (não há seq nem evento), e
// nenhuma guarda áudio, texto ditado nem chave: a chave da IA mora na
// ai_provider_keys (chaves_ia.go), cifrada.
//
// O "apagar meus dados" apaga as três tabelas do ditado no celular
// (ApagarDadosDaConta), e revogar o aceite (RevogarAceite) apaga as chaves.

// VersaoDoAviso é a versão do texto do aviso do ditado no celular que este
// servidor aceita (decisão H): o aceite só vale com ela, e mudar o texto do
// aviso é subir este número, o que pede um aceite novo a todas as contas.
//
// A MESMA versão mora no app, em web/src/nuvem/avisoDoDitado.ts
// (VERSAO_DO_AVISO): o app manda a versão do texto que mostrou, e o servidor
// recusa o aceite de outra (AVISO_DESATUALIZADO). Um teste
// (TestVersaoDoAviso_AppEServidorConferem) lê o arquivo do app e prende os
// dois números: subir um sem o outro quebra a suíte, e não o primeiro aceite
// de alguém.
const VersaoDoAviso = 1

// Quem transcreve e quem faz o Polish no celular. A transcrição é OpenAI ou
// Groq; o Polish, OpenAI ou Anthropic (decisões D e N). São as listas dos
// CHECKs da 0018 e da 0019, e um teste do pgstore as confere contra o SQL.
var (
	ProvedoresDeTranscricao = []string{ProvedorOpenAI, ProvedorGroq}
	ProvedoresDePolish      = []string{ProvedorOpenAI, ProvedorAnthropic}
)

// ProvedorDeTranscricaoValido diz se p transcreve no celular (exatamente, em
// minúsculas).
func ProvedorDeTranscricaoValido(p string) bool { return slices.Contains(ProvedoresDeTranscricao, p) }

// ProvedorDePolishValido diz se p faz o Polish no celular.
func ProvedorDePolishValido(p string) bool { return slices.Contains(ProvedoresDePolish, p) }

// Os modelos padrão da transcrição, para quem ainda não escolheu (escolhas
// menores 1 e 2). O da OpenAI é o mais barato: a ADR pede o gpt-transcribe se
// a chamada de teste em pt-BR da C7 for boa, e esta constante é o único lugar
// a mudar quando ela for. A lista de modelos vem do provedor, e a pessoa troca.
const (
	ModeloPadraoOpenAI = "gpt-4o-mini-transcribe"
	ModeloPadraoGroq   = "whisper-large-v3-turbo"
)

// ModeloPadraoDaTranscricao é o modelo padrão do provedor, ou "" se ele não
// transcreve.
func ModeloPadraoDaTranscricao(provedor string) string {
	switch provedor {
	case ProvedorOpenAI:
		return ModeloPadraoOpenAI
	case ProvedorGroq:
		return ModeloPadraoGroq
	}
	return ""
}

// TamanhoMaximoDoModelo é o teto do nome de um modelo (o CHECK da 0018).
const TamanhoMaximoDoModelo = 100

// reModelo é a forma de um nome de modelo: as letras, os dígitos e
// . _ : / @ -, de 1 a 100. É a expressão do CHECK da 0018 (asr_model e
// polish_model), e a mesma classe vale nas duas linguagens (RE2 e ARE).
var reModelo = regexp.MustCompile(`^[A-Za-z0-9._:/@-]{1,100}$`)

// ModeloValido diz se m tem a forma de um nome de modelo.
func ModeloValido(m string) bool { return reModelo.MatchString(m) }

// ProvedorEModelo é uma escolha: quem faz e com que modelo.
type ProvedorEModelo struct {
	Provedor string
	Modelo   string
}

// PreferenciasDoDitado são as preferências do ditado no celular de uma conta
// (a dictation_settings). Moram só na nuvem e nunca descem aos PCs.
type PreferenciasDoDitado struct {
	// Transcricao é quem transcreve e com que modelo (OpenAI ou Groq).
	Transcricao ProvedorEModelo
	// Dica manda os termos aprendidos da conta ao provedor como dica (decisão
	// E). Começa desligada até a C8 medir.
	Dica bool
	// PolishLigado liga o Polish no celular (decisão N). Começa desligado.
	PolishLigado bool
	// Polish é quem faz o Polish e com que modelo (OpenAI ou Anthropic). O
	// modelo pode ficar vazio enquanto o Polish está desligado.
	Polish ProvedorEModelo
	// CriadaEm e AtualizadaEm são a primeira e a última gravação; zero nos
	// padrões (a conta nunca gravou).
	CriadaEm     time.Time
	AtualizadaEm time.Time
}

// PreferenciasPadrao são as preferências de uma conta que ainda não escolheu:
// a OpenAI e o modelo padrão dela, sem dica, sem Polish (a OpenAI como
// provedor dele, sem modelo escolhido).
func PreferenciasPadrao() PreferenciasDoDitado {
	return PreferenciasDoDitado{
		Transcricao: ProvedorEModelo{Provedor: ProvedorOpenAI, Modelo: ModeloPadraoOpenAI},
		Polish:      ProvedorEModelo{Provedor: ProvedorOpenAI},
	}
}

// Os erros das preferências e do aceite. Nenhum carrega o valor recusado.
var (
	// ErrProvedorDeTranscricao: o provedor da transcrição não é openai nem groq.
	ErrProvedorDeTranscricao = errors.New("cloudstore: provedor de transcrição fora da lista")
	// ErrProvedorDePolish: o provedor do Polish não é openai nem anthropic.
	ErrProvedorDePolish = errors.New("cloudstore: provedor do Polish fora da lista")
	// ErrModeloInvalido: o nome do modelo não tem a forma de um nome de modelo.
	ErrModeloInvalido = errors.New("cloudstore: o nome do modelo não tem a forma esperada")
	// ErrPolishSemModelo: o Polish está ligado e não tem modelo.
	ErrPolishSemModelo = errors.New("cloudstore: o Polish ligado precisa de um modelo")
	// ErrAceiteInvalido: o aceite não tem versão, ou o provedor está fora da lista.
	ErrAceiteInvalido = errors.New("cloudstore: o aceite não tem a forma esperada")
	// ErrSemAceite: a conta não tem aceite em aberto.
	ErrSemAceite = errors.New("cloudstore: a conta não tem aceite do ditado no celular")
)

// ConferirPreferencias confere as preferências antes do banco, com os mesmos
// CHECKs da 0018: o provedor da transcrição e o do Polish da lista de cada um,
// o modelo da transcrição com a forma de um nome de modelo, o do Polish vazio
// ou com ela, e o Polish ligado com modelo. Nenhuma preferência que passe aqui
// alcança um CHECK da 0018.
func ConferirPreferencias(p PreferenciasDoDitado) error {
	switch {
	case !ProvedorDeTranscricaoValido(p.Transcricao.Provedor):
		return ErrProvedorDeTranscricao
	case !ModeloValido(p.Transcricao.Modelo):
		return ErrModeloInvalido
	case !ProvedorDePolishValido(p.Polish.Provedor):
		return ErrProvedorDePolish
	case p.Polish.Modelo != "" && !ModeloValido(p.Polish.Modelo):
		return ErrModeloInvalido
	case p.PolishLigado && p.Polish.Modelo == "":
		return ErrPolishSemModelo
	}
	return nil
}

// MotivoDoFimDoAceite é por que um aceite terminou (end_reason da 0019).
const (
	// FimRevogado: a pessoa desligou o ditado no celular.
	FimRevogado = "revogado"
	// FimSubstituido: ela aceitou de novo, e o aceite novo tomou o lugar.
	FimSubstituido = "substituido"
)

// Aceite é uma linha da dictation_consents: o que a pessoa aceitou, quando, e
// se terminou. Cada aceite é uma linha, e nenhuma some quando termina.
type Aceite struct {
	ID string
	// Versao é a versão do texto do aviso que a pessoa viu.
	Versao int
	// Transcricao é o provedor da transcrição aceito; Polish, o do Polish
	// ("" quando o aviso que ela viu não incluía o Polish).
	Transcricao string
	Polish      string
	AceitoEm    time.Time
	// EncerradoEm é o fim do aceite (zero: em aberto) e Motivo, por quê
	// (FimRevogado ou FimSubstituido; vazio enquanto está em aberto).
	EncerradoEm time.Time
	Motivo      string
}

// Aberto diz se o aceite ainda não terminou.
func (a Aceite) Aberto() bool { return a.EncerradoEm.IsZero() }

// Vigente diz se o aceite vale agora: em aberto e do texto atual do aviso. Um
// aceite de uma versão velha continua gravado (é a prova do que a pessoa
// aceitou), mas não vale: o servidor recusa o provedor até ela aceitar o aviso
// novo.
func (a Aceite) Vigente() bool { return a.Aberto() && a.Versao == VersaoDoAviso }

// Inclui diz se o aceite cobre o provedor: o da transcrição ou o do Polish. É
// só a conferência do conjunto; quem decide se o aceite vale é o Vigente.
func (a Aceite) Inclui(provedor string) bool {
	return provedor != "" && (a.Transcricao == provedor || a.Polish == provedor)
}

// Cobre diz se o aceite deixa usar o provedor agora: vigente (em aberto e do
// texto atual do aviso) e com o provedor no conjunto. É a conferência de todo
// uso do provedor com a chave da conta: a do AceiteDoDitado do servidor e a da
// gravação da chave sob a trava da conta (GravarChaveDeIAConferindo), que têm
// de ser a mesma.
func (a Aceite) Cobre(provedor string) bool { return a.Vigente() && a.Inclui(provedor) }

// mesmoAceite diz se n é o que o aceite em aberto já registra (a versão e os
// dois provedores): aceitar de novo o mesmo não grava uma linha nova.
func (a Aceite) mesmoAceite(n NovoAceite) bool {
	return a.Versao == n.Versao && a.Transcricao == n.Transcricao && a.Polish == n.Polish
}

// NovoAceite é o que a pessoa aceitou, para GravarAceite.
type NovoAceite struct {
	Versao      int
	Transcricao string
	// Polish é o provedor do Polish que o aviso mostrou, ou "" se não mostrou.
	Polish string
}

// ConferirAceite confere o aceite antes do banco, com os CHECKs da 0019: a
// versão a partir de 1, o provedor da transcrição da lista dele e o do Polish
// vazio ou da lista dele.
func ConferirAceite(n NovoAceite) error {
	if n.Versao < 1 || !ProvedorDeTranscricaoValido(n.Transcricao) || (n.Polish != "" && !ProvedorDePolishValido(n.Polish)) {
		return ErrAceiteInvalido
	}
	return nil
}

// Revogacao é o que RevogarAceite fez.
type Revogacao struct {
	// Revogou diz se havia um aceite em aberto e ele terminou.
	Revogou bool
	// ChavesApagadas é quantas chaves da IA da conta saíram junto.
	ChavesApagadas int
}

// DitadoNoCelular guarda as preferências e o aceite do ditado no celular. O
// MemoryStore e o Store do Postgres o implementam, e a mesma suíte
// (internal/server/chavestest) roda contra os dois.
type DitadoNoCelular interface {
	// LerPreferenciasDoDitado devolve as preferências da conta, ou os padrões
	// (PreferenciasPadrao, com as datas zero) se ela nunca gravou. Um usuário
	// que não existe também recebe os padrões.
	LerPreferenciasDoDitado(ctx context.Context, userID string) (PreferenciasDoDitado, error)
	// GravarPreferenciasDoDitado grava as preferências inteiras por cima das
	// que houver, com now como AtualizadaEm (e como CriadaEm, se é a primeira).
	// Elas passam por ConferirPreferencias antes. O usuário que não existe é
	// ErrUnknownUser. Não confere o aceite: quem decide é a rota.
	GravarPreferenciasDoDitado(ctx context.Context, userID string, p PreferenciasDoDitado, now time.Time) (PreferenciasDoDitado, error)
	// LerAceiteAberto devolve o aceite da conta que ainda não terminou, de
	// qualquer versão do aviso (quem confere a versão é o Vigente), ou
	// ErrSemAceite.
	LerAceiteAberto(ctx context.Context, userID string) (Aceite, error)
	// GravarAceite grava o aceite com now como AceitoEm. O aceite em aberto
	// que houver termina como FimSubstituido na mesma transação, e a conta
	// nunca tem dois em aberto. Se o aceite em aberto já é este (a mesma versão
	// e os mesmos provedores), nada muda e ele é devolvido: aceitar duas vezes
	// o mesmo não cria uma linha, e a data é a da primeira vez. O aceite passa
	// por ConferirAceite antes. O usuário que não existe é ErrUnknownUser.
	GravarAceite(ctx context.Context, userID string, n NovoAceite, now time.Time) (Aceite, error)
	// RevogarAceite termina o aceite em aberto (FimRevogado, com now) e apaga
	// TODAS as chaves da IA da conta, numa transação só: não fica o aceite
	// revogado com a chave guardada, nem a chave apagada com o aceite em pé.
	// Sem aceite em aberto, ainda apaga as chaves (Revogou false). O usuário
	// que não existe não tem nada a revogar.
	RevogarAceite(ctx context.Context, userID string, now time.Time) (Revogacao, error)
	// ListarAceites devolve todos os aceites da conta, do mais velho ao mais
	// novo, os que terminaram inclusive: é a prova do que ela aceitou e
	// quando. Nunca nil.
	ListarAceites(ctx context.Context, userID string) ([]Aceite, error)
}

var _ DitadoNoCelular = (*MemoryStore)(nil)

func (s *MemoryStore) LerPreferenciasDoDitado(_ context.Context, userID string) (PreferenciasDoDitado, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok && u.preferenciasDoDitado != nil {
		return *u.preferenciasDoDitado, nil
	}
	return PreferenciasPadrao(), nil
}

func (s *MemoryStore) GravarPreferenciasDoDitado(_ context.Context, userID string, p PreferenciasDoDitado, now time.Time) (PreferenciasDoDitado, error) {
	if err := ConferirPreferencias(p); err != nil {
		return PreferenciasDoDitado{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return PreferenciasDoDitado{}, ErrUnknownUser
	}
	now = now.UTC()
	p.CriadaEm, p.AtualizadaEm = now, now
	if antes := u.preferenciasDoDitado; antes != nil {
		// A primeira gravação fica como a data de criação, e a troca nunca
		// fica antes dela (o GREATEST e o CHECK da 0018), mesmo com o relógio
		// voltando.
		p.CriadaEm = antes.CriadaEm
		if p.AtualizadaEm.Before(p.CriadaEm) {
			p.AtualizadaEm = p.CriadaEm
		}
	}
	u.preferenciasDoDitado = &p
	return p, nil
}

func (s *MemoryStore) LerAceiteAberto(_ context.Context, userID string) (Aceite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		if n := len(u.aceitesDoDitado); n > 0 && u.aceitesDoDitado[n-1].Aberto() {
			return u.aceitesDoDitado[n-1], nil
		}
	}
	return Aceite{}, ErrSemAceite
}

func (s *MemoryStore) GravarAceite(_ context.Context, userID string, n NovoAceite, now time.Time) (Aceite, error) {
	if err := ConferirAceite(n); err != nil {
		return Aceite{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return Aceite{}, ErrUnknownUser
	}
	now = now.UTC()
	if k := len(u.aceitesDoDitado); k > 0 && u.aceitesDoDitado[k-1].Aberto() {
		aberto := &u.aceitesDoDitado[k-1]
		if aberto.mesmoAceite(n) {
			return *aberto, nil
		}
		// O fim nunca fica antes do começo (o GREATEST e o CHECK da 0019),
		// mesmo com o relógio voltando.
		aberto.EncerradoEm, aberto.Motivo = now, FimSubstituido
		if aberto.EncerradoEm.Before(aberto.AceitoEm) {
			aberto.EncerradoEm = aberto.AceitoEm
		}
	}
	a := Aceite{ID: NewUUIDv7(), Versao: n.Versao, Transcricao: n.Transcricao, Polish: n.Polish, AceitoEm: now}
	u.aceitesDoDitado = append(u.aceitesDoDitado, a)
	return a, nil
}

func (s *MemoryStore) RevogarAceite(_ context.Context, userID string, now time.Time) (Revogacao, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r Revogacao
	u, ok := s.users[userID]
	if !ok {
		return r, nil
	}
	now = now.UTC()
	if k := len(u.aceitesDoDitado); k > 0 && u.aceitesDoDitado[k-1].Aberto() {
		aberto := &u.aceitesDoDitado[k-1]
		aberto.EncerradoEm, aberto.Motivo = now, FimRevogado
		if aberto.EncerradoEm.Before(aberto.AceitoEm) {
			aberto.EncerradoEm = aberto.AceitoEm
		}
		r.Revogou = true
	}
	r.ChavesApagadas = len(u.chavesDeIA)
	u.chavesDeIA = nil
	return r, nil
}

func (s *MemoryStore) ListarAceites(_ context.Context, userID string) ([]Aceite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Aceite{}
	if u, ok := s.users[userID]; ok {
		out = append(out, u.aceitesDoDitado...)
	}
	// Do mais velho ao mais novo pela data do aceite, como o ORDER BY do
	// Postgres; na mesma data, o que terminou vem antes do que está em aberto.
	slices.SortStableFunc(out, func(a, b Aceite) int {
		if c := a.AceitoEm.Compare(b.AceitoEm); c != 0 {
			return c
		}
		switch {
		case a.Aberto() == b.Aberto():
			return 0
		case a.Aberto():
			return 1
		}
		return -1
	})
	return out, nil
}
