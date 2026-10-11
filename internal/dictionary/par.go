package dictionary

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

// Este arquivo é o aprendido como a nuvem o vê (ADR-019, "Decisões da F8",
// F8a): o id e a chave de uma variante que o painel aprendeu, as checagens
// que a nuvem faz antes do banco, e a projeção do dicionario.json no que sobe.
//
// # Um lugar só para o PC e a nuvem
//
// O servidor (internal/server/cloudstore) recalcula o id e a chave com estas
// mesmas funções, e confere o par com ConferirPar, e o PC usa a mesma
// ConferirPar na projeção: o que a nuvem recusaria nem sobe. Por isso este
// pacote não importa nada do módulo, só a biblioteca padrão (um teste confere):
// a nuvem o importa sem ciclo.
//
// # Nada novo no JSON
//
// A projeção só LÊ o arquivo. O formato do dicionario.json não muda: um Agent
// antigo recusaria campo novo (CarregarSemente, DisallowUnknownFields).

// O namespace e o nome do UUID v5 do aprendido.
//
// O namespace é o NameSpace_URL da RFC 9562 (6ba7b811-9dad-11d1-80b4-
// 00c04fd430c8), e o nome leva a versão ("v1") antes da chave. Os dois estão
// congelados (par_test.go, com vetores conferidos fora do Go). Mudar Normalize
// ou CollapseSpaces muda a chave e o id, e a nuvem passaria a ter duas linhas
// para a mesma variante, em silêncio. Mudar só o nome ou o namespace muda o id
// e não a chave: o Postgres (pgstore, a conferência da chave antes do INSERT)
// recusa com campo_invalido, para sempre, todo par que já tem linha. Uma
// regra nova de chave entra com outro nome ("v2"), nunca no lugar desta, e
// precisa de uma migração que regrave o id e a chave das linhas da nuvem
// antes de um PC mandar ids v2 (a decisão é do Mauro).
var namespaceDoPar = [16]byte{
	0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1,
	0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
}

const nomeDoParV1 = "aurora-whisper:aprendido:v1:"

// MaxRunasPar é o tamanho máximo, em caracteres (runas), da variante, do termo
// e do nome do domínio de um par: o teto de integridade do dicionario.json
// (maxRunasAprendido) e o CHECK das colunas da learned_pairs (migration 0016,
// char_length, que conta caracteres, e não bytes).
const MaxRunasPar = maxRunasAprendido

// ChaveDoPar é a identidade de uma variante: CollapseSpaces(Normalize(v)), a
// mesma comparação do dicionário ("DCTF Web", "dctf-web" e "DCTFWeb" são a
// mesma variante; "Físico" e "fisico" também). É o normalized_variant da
// learned_pairs, que o servidor calcula e nunca recebe.
//
// A chave nunca tem mais runas que a variante (Normalize troca cada runa por
// uma, ou a junta num espaço, e CollapseSpaces só tira espaços): uma variante
// de até MaxRunasPar runas tem a chave dentro do CHECK da 0016.
func ChaveDoPar(variante string) string {
	return CollapseSpaces(Normalize(variante))
}

// IDDoPar é o id de uma variante aprendida: um UUID v5 (SHA-1) da ChaveDoPar,
// em minúsculas, no formato 8-4-4-4-12. É o mesmo em todo PC e em toda conta,
// sem depender do usuario do arquivo (o idDeterministico de seed.go depende, e
// não é UUID). A identidade é a variante, e não o par: trocar o termo muda a
// mesma entidade.
//
// Uma variante sem letra nem dígito (chave vazia) também tem id, mas
// ConferirPar a recusa: ela nunca sobe.
func IDDoPar(variante string) string {
	return uuidV5(namespaceDoPar, nomeDoParV1+ChaveDoPar(variante))
}

// uuidV5 é o UUID da RFC 9562, seção 5.5: os 16 primeiros bytes do SHA-1 do
// namespace seguido do nome, com a versão (5) e a variante (10) gravadas.
func uuidV5(namespace [16]byte, nome string) string {
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(nome))
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	x := hex.EncodeToString(u[:])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// Os erros de ConferirPar. Nenhum traz texto: a variante é texto ditado, e o
// erro pode ir parar num log. A nuvem responde todos como campo_invalido.
var (
	// ErrParTexto: a variante, o termo ou o domínio têm o caractere nulo, ou
	// não são UTF-8 válido. O Postgres não guarda nenhum dos dois (SQLSTATE
	// 22021, cujo DETAIL citaria o texto no log dele).
	ErrParTexto = errors.New("dictionary: o par tem caractere nulo ou texto que não é UTF-8 válido")
	// ErrParTamanho: a variante ou o termo fora de 1 a MaxRunasPar runas, ou o
	// domínio, quando há, fora disso.
	ErrParTamanho = errors.New("dictionary: variante, termo ou domínio fora de 1 a 80 caracteres")
	// ErrParVarianteVazia: a variante não tem letra nem dígito (Normalize
	// vazio). A chave seria vazia, e o CHECK da 0016 a recusa.
	ErrParVarianteVazia = errors.New("dictionary: a variante não tem letra nem dígito")
	// ErrParRestritoSemDominio: restrito sem domínio nunca age, e o CHECK da
	// 0016 o recusa.
	ErrParRestritoSemDominio = errors.New("dictionary: par restrito sem domínio")
)

// ConferirPar são as checagens do par que a nuvem faz antes do banco, e que o
// PC faz na projeção (ParesDoPainel): o que a nuvem recusaria nem sobe. dominio
// nil é o domínio nulo; um ponteiro para "" é um domínio vazio, e é recusado.
//
// Na ordem: o caractere nulo e o UTF-8 (ErrParTexto); os tamanhos, em runas
// (ErrParTamanho); a variante que não normaliza para nada
// (ErrParVarianteVazia); o restrito sem domínio (ErrParRestritoSemDominio).
// Com isso nenhum push alcança o UNIQUE (user_id, normalized_variant), os
// CHECKs da 0016 nem o erro do byte nulo. O id (IDDoPar) a nuvem confere à
// parte, contra o entity_id que recebeu.
func ConferirPar(variante, termo string, restrito bool, dominio *string) error {
	textos := []string{variante, termo}
	if dominio != nil {
		textos = append(textos, *dominio)
	}
	for _, t := range textos {
		if strings.ContainsRune(t, 0) || !utf8.ValidString(t) {
			return ErrParTexto
		}
	}
	for _, t := range textos {
		if n := utf8.RuneCountInString(t); n < 1 || n > MaxRunasPar {
			return ErrParTamanho
		}
	}
	if Normalize(variante) == "" {
		return ErrParVarianteVazia
	}
	if restrito && dominio == nil {
		return ErrParRestritoSemDominio
	}
	return nil
}

// ParDoPainel é uma variante que o painel aprendeu, como ela está no arquivo
// deste PC e como sobe para a nuvem.
type ParDoPainel struct {
	// ID é IDDoPar(Variante).
	ID string
	// Variante é como está escrita no arquivo.
	Variante string
	// Termo é a forma canônica da entrada, como está escrita aqui.
	Termo string
	// Restrito é a restrição efetiva: a entrada é restrita, ou a variante está
	// em alias_restrito. Só com Dominio.
	Restrito bool
	// Dominio é o NOME do domínio da entrada; "" é sem domínio (o null do
	// fio). Nunca o mapa dominios, que diz que programas a pessoa usa.
	Dominio string
	// EmAliasRestrito diz que a variante está em alias_restrito, e não em
	// alias. É a forma local (a "projeção local guardada" do espelho): não
	// sobe, porque a nuvem só vê a restrição efetiva.
	EmAliasRestrito bool
}

// ParaNuvem é o par como a nuvem o guarda (variant, term, restricted,
// domain), ativo.
func (p ParDoPainel) ParaNuvem() ParDaNuvem {
	return ParDaNuvem{Variante: p.Variante, Termo: p.Termo, Restrito: p.Restrito, Dominio: p.Dominio}
}

// ParesDoPainel é a projeção do arquivo no que sobe: uma linha por variante de
// origem "painel" que age em algum lugar, na ordem do arquivo (as entradas e,
// em cada uma, alias e depois alias_restrito). Só lê: nada muda no JSON. Nunca
// nil.
//
// O que nunca age não sobe:
//
//   - a variante em alias de uma entrada restrita sem domínio, ou em
//     alias_restrito de uma entrada sem domínio: o Search nunca a mostra;
//   - a mesma variante (o mesmo id) com a origem em mais de um lugar: o par é
//     ambíguo, e nenhum dos dois sobe;
//   - o que a nuvem recusaria (ConferirPar): a variante que não normaliza para
//     nada, posta à mão com a origem; variante, termo ou domínio com mais de
//     80 runas, ou com o caractere nulo;
//   - e, pela D1, o que não tem a origem "painel" (o que a pessoa pôs à mão, o
//     dictlearn, o "Adicionar termo").
//
// O domínio só de espaços é "sem domínio", como em opcional (seed.go).
func (s Semente) ParesDoPainel() []ParDoPainel {
	type candidato struct {
		par ParDoPainel
		ok  bool
	}
	var todos []candidato
	vezes := map[string]int{}
	for _, e := range s.Entradas {
		for _, emRestrito := range []bool{false, true} {
			lista := e.Alias
			if emRestrito {
				lista = e.AliasRestrito
			}
			for _, a := range lista {
				if s.AliasOrigem[a] != OrigemPainel {
					continue
				}
				par, ok := projetar(e, a, emRestrito)
				vezes[par.ID]++
				todos = append(todos, candidato{par, ok})
			}
		}
	}
	out := []ParDoPainel{}
	for _, c := range todos {
		if c.ok && vezes[c.par.ID] == 1 {
			out = append(out, c.par)
		}
	}
	return out
}

// VarianteNoArquivo diz onde está, no arquivo, uma variante (pela ChaveDoPar,
// como o dicionário compara), para quem a procura fora da projeção: a
// varredura do sync (internal/sync, F8f3), que não pode ler como desfazer o
// par que continua no arquivo. Só lê.
//
//   - doPainel: a variante está com a origem "painel" em algum lugar. Fora da
//     projeção, ela está no arquivo e não sobe: repetida, numa entrada que
//     nunca age (restrita sem domínio), ou o que a nuvem recusaria;
//   - aMaoNoTermo: a variante está, sem a origem, no termo dado (comparado como
//     o Search compara): a pessoa a adotou, ou a pôs à mão.
//
// A variante sem letra nem dígito (chave vazia) não está em lugar nenhum.
func (s Semente) VarianteNoArquivo(variante, termo string) (doPainel, aMaoNoTermo bool) {
	chave := ChaveDoPar(variante)
	if chave == "" {
		return false, false
	}
	painel, outras := s.ocorrencias(chave)
	for _, o := range outras {
		if mesmaCoisa(s.Entradas[o.entrada].Canonica, termo) {
			aMaoNoTermo = true
		}
	}
	return len(painel) > 0, aMaoNoTermo
}

// projetar é a linha da projeção de uma variante de e. ok falso: ela não sobe
// (nunca age, ou a nuvem a recusaria). O ID vem preenchido mesmo assim, para a
// conta das repetidas.
func projetar(e EntradaSemente, variante string, emRestrito bool) (ParDoPainel, bool) {
	par := ParDoPainel{ID: IDDoPar(variante), Variante: variante, Termo: e.Canonica, EmAliasRestrito: emRestrito}
	if semDominio(e) {
		// Restrita sem domínio, ou alias restrito sem domínio: nunca age.
		if e.Restrito || emRestrito {
			return par, false
		}
	} else {
		par.Dominio = e.Dominio
		par.Restrito = e.Restrito || emRestrito
	}
	if ConferirPar(par.Variante, par.Termo, par.Restrito, dominioDoPar(par.Dominio)) != nil {
		return par, false
	}
	return par, true
}

// semDominio diz se a entrada não tem domínio: vazio, ou só espaços (opcional,
// em seed.go, faz o mesmo).
func semDominio(e EntradaSemente) bool {
	return strings.TrimSpace(e.Dominio) == ""
}

// dominioDoPar é o domínio do par no fio: "" é nulo.
func dominioDoPar(d string) *string {
	if d == "" {
		return nil
	}
	return &d
}
