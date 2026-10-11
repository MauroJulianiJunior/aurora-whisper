package cloudstore

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"
)

// As chaves da IA de cada pessoa (ADR-021, decisão G e C3a): a chave de API do
// provedor que a pessoa cola no app do celular, guardada SÓ cifrada (a tabela
// ai_provider_keys, a migration 0017). Quem cifra e decifra é o servidor
// (internal/server/cofre), com a chave de cifra que nunca entra no banco; aqui
// mora só a linha: o id da chave de cifra, o nonce, o texto cifrado e o fim da
// chave que a tela mostra.
//
// Nada aqui sincroniza com os PCs, e nada passa pelo ponto único de gravação
// do histórico: não há seq nem evento. O "apagar meus dados" apaga as chaves
// da conta junto com o resto (ApagarDadosDaConta).

// Os provedores da chave da IA: a transcrição no celular (OpenAI e Groq) e o
// Polish no celular (OpenAI e Anthropic). É a lista do CHECK da 0017.
const (
	ProvedorOpenAI    = "openai"
	ProvedorGroq      = "groq"
	ProvedorAnthropic = "anthropic"
)

// ProvedoresDeIA é a lista fechada, na ordem em que as listagens a devolvem.
var ProvedoresDeIA = []string{ProvedorOpenAI, ProvedorGroq, ProvedorAnthropic}

// ProvedorDeIAValido diz se p é um provedor da lista fechada (exatamente,
// em minúsculas).
func ProvedorDeIAValido(p string) bool { return slices.Contains(ProvedoresDeIA, p) }

// Os tamanhos da linha (os CHECKs da 0017; o cofre produz os mesmos).
const (
	TamanhoDoIDDaCifra    = 8
	TamanhoDoNonceDaChave = 12
	// MinCifradoDaChave: um byte de chave e os 16 da etiqueta do GCM.
	MinCifradoDaChave = 17
	// MaxCifradoDaChave: os 2560 bytes da chave (chavedeapi.TamanhoMaximo) e
	// os 16 da etiqueta.
	MaxCifradoDaChave = 2560 + 16
	// MaxFinalDaChave: o fim que a tela mostra (chavedeapi.Final).
	MaxFinalDaChave = 4
)

// ChaveDeIA é uma linha de ai_provider_keys. Nunca carrega a chave em claro.
type ChaveDeIA struct {
	Provedor string
	// IDDaCifra é o id da chave de cifra que cifrou a linha (8 bytes).
	IDDaCifra []byte
	// Nonce tem 12 bytes.
	Nonce []byte
	// Cifrado é o texto cifrado com a etiqueta do GCM no fim.
	Cifrado []byte
	// Final é o fim da chave que a tela mostra (até 4 caracteres).
	Final string
	// CriadaEm é a primeira chave do provedor; AtualizadaEm, a última troca.
	CriadaEm     time.Time
	AtualizadaEm time.Time
}

// ChavesDeIA guarda as chaves da IA, uma por pessoa e por provedor. O
// MemoryStore e o Store do Postgres a implementam, e a mesma suíte
// (internal/server/chavestest) roda contra os dois.
type ChavesDeIA interface {
	// GravarChaveDeIA grava a chave do provedor por cima da que houver, com
	// now como AtualizadaEm (e como CriadaEm, se é a primeira). A linha passa
	// por ConferirChaveDeIA antes. O usuário que não existe é ErrUnknownUser.
	GravarChaveDeIA(ctx context.Context, userID string, k ChaveDeIA, now time.Time) (ChaveDeIA, error)
	// LerChaveDeIA devolve a linha do provedor, ou ErrChaveDeIAAusente.
	LerChaveDeIA(ctx context.Context, userID, provedor string) (ChaveDeIA, error)
	// ListarChavesDeIA devolve as linhas da conta, na ordem de ProvedoresDeIA.
	// Nunca nil.
	ListarChavesDeIA(ctx context.Context, userID string) ([]ChaveDeIA, error)
	// ApagarChaveDeIA apaga a linha do provedor. Não haver linha não é erro:
	// apagou diz se havia.
	ApagarChaveDeIA(ctx context.Context, userID, provedor string) (apagou bool, err error)
}

// Erros das chaves da IA. Nenhum carrega a chave, o cifrado nem o usuário.
var (
	// ErrChaveDeIAAusente: não há chave guardada para o provedor.
	ErrChaveDeIAAusente = errors.New("cloudstore: nenhuma chave da IA guardada para este provedor")
	// ErrProvedorDeIA: o provedor não é da lista fechada.
	ErrProvedorDeIA = errors.New("cloudstore: provedor da IA desconhecido")
	// ErrChaveDeIAInvalida: a linha não tem os tamanhos da 0017 (o id, o
	// nonce, o cifrado ou o fim).
	ErrChaveDeIAInvalida = errors.New("cloudstore: a linha da chave da IA não tem os tamanhos esperados")
)

// ConferirChaveDeIA confere a linha antes do banco: o provedor da lista, os
// tamanhos fixos e o fim em ASCII imprimível. Nenhuma linha que passe aqui
// alcança um CHECK da 0017.
func ConferirChaveDeIA(k ChaveDeIA) error {
	if !ProvedorDeIAValido(k.Provedor) {
		return ErrProvedorDeIA
	}
	if len(k.IDDaCifra) != TamanhoDoIDDaCifra || len(k.Nonce) != TamanhoDoNonceDaChave ||
		len(k.Cifrado) < MinCifradoDaChave || len(k.Cifrado) > MaxCifradoDaChave || len(k.Final) > MaxFinalDaChave {
		return ErrChaveDeIAInvalida
	}
	for i := 0; i < len(k.Final); i++ {
		if k.Final[i] < 0x21 || k.Final[i] > 0x7e {
			return ErrChaveDeIAInvalida
		}
	}
	return nil
}

// copiarChaveDeIA devolve a linha com os bytes copiados: quem a recebe pode
// mudá-los sem mudar o que está guardado.
func copiarChaveDeIA(k ChaveDeIA) ChaveDeIA {
	k.IDDaCifra = slices.Clone(k.IDDaCifra)
	k.Nonce = slices.Clone(k.Nonce)
	k.Cifrado = slices.Clone(k.Cifrado)
	return k
}

var _ ChavesDeIA = (*MemoryStore)(nil)

func (s *MemoryStore) GravarChaveDeIA(_ context.Context, userID string, k ChaveDeIA, now time.Time) (ChaveDeIA, error) {
	if err := ConferirChaveDeIA(k); err != nil {
		return ChaveDeIA{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return ChaveDeIA{}, ErrUnknownUser
	}
	if u.chavesDeIA == nil {
		u.chavesDeIA = make(map[string]ChaveDeIA)
	}
	k = copiarChaveDeIA(k)
	now = now.UTC()
	k.CriadaEm, k.AtualizadaEm = now, now
	if antes, ok := u.chavesDeIA[k.Provedor]; ok {
		// A primeira chave fica como a data de criação, e a troca nunca fica
		// antes dela (o GREATEST e o CHECK da 0017), mesmo com o relógio
		// voltando.
		k.CriadaEm = antes.CriadaEm
		if k.AtualizadaEm.Before(k.CriadaEm) {
			k.AtualizadaEm = k.CriadaEm
		}
	}
	u.chavesDeIA[k.Provedor] = k
	return copiarChaveDeIA(k), nil
}

func (s *MemoryStore) LerChaveDeIA(_ context.Context, userID, provedor string) (ChaveDeIA, error) {
	if !ProvedorDeIAValido(provedor) {
		return ChaveDeIA{}, ErrProvedorDeIA
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return ChaveDeIA{}, ErrChaveDeIAAusente
	}
	k, ok := u.chavesDeIA[provedor]
	if !ok {
		return ChaveDeIA{}, ErrChaveDeIAAusente
	}
	return copiarChaveDeIA(k), nil
}

func (s *MemoryStore) ListarChavesDeIA(_ context.Context, userID string) ([]ChaveDeIA, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ChaveDeIA{}
	u, ok := s.users[userID]
	if !ok {
		return out, nil
	}
	for _, p := range ProvedoresDeIA {
		if k, ok := u.chavesDeIA[p]; ok {
			out = append(out, copiarChaveDeIA(k))
		}
	}
	return out, nil
}

func (s *MemoryStore) ApagarChaveDeIA(_ context.Context, userID, provedor string) (bool, error) {
	if !ProvedorDeIAValido(provedor) {
		return false, ErrProvedorDeIA
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return false, nil
	}
	_, havia := u.chavesDeIA[provedor]
	delete(u.chavesDeIA, provedor)
	return havia, nil
}

// clonarChavesDeIA é a cópia do mapa para a foto da restauração (os bytes de
// cada linha nunca são mudados no lugar: GravarChaveDeIA troca a linha
// inteira).
func clonarChavesDeIA(m map[string]ChaveDeIA) map[string]ChaveDeIA { return maps.Clone(m) }
