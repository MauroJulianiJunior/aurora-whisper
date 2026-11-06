package dictionary

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// A cópia do dicionário para a nuvem (ADR-023, "A. A cópia"): o
// dicionario.json de um PC, menos o que diz de programas (o mapa de domínios) e
// de amostras (os ids do dictlearn em alias_origem), com o usuário local trocado
// por um nome fixo. Ela só vai do PC à nuvem, para o ditado no celular montar o
// Smart; nada desce aos PCs, e o arquivo continua sendo o dono.
//
// As mesmas funções servem aos dois lados: o PC monta e confere a cópia antes
// de mandar (BytesDaCopia), e a nuvem a confere ao receber (LerCopia). Os
// mesmos termos dão os mesmos bytes, e o SHA-256 dos bytes é a versão da cópia.
//
// Nenhum erro daqui traz um termo, uma variante ou um domínio: o motivo é
// fechado (MotivoDaCopia), para ir ao painel e ao log sem o texto da pessoa.

// UsuarioDaCopia é o usuário de toda cópia. O nome do usuário local não sai do
// PC; a nuvem troca este pelo dono da conta ao montar o Smart.
const UsuarioDaCopia = "copia"

// Os limites da cópia (ADR-023, A). O dicionário do Mauro, em 05-nov-2026, tinha
// 6 KB, 49 termos, 104 variantes e no máximo 4 por termo.
const (
	MaxBytesDaCopia             = 512 << 10
	MaxTermosDaCopia            = 2000
	MaxVariantesPorTermoDaCopia = 50
	MaxVariantesDaCopia         = 10000
	// maxRunasDaCopia vale para o termo, cada variante, o domínio e o idioma:
	// o mesmo teto de integridade do que o painel grava (maxRunasAprendido).
	maxRunasDaCopia = maxRunasAprendido
)

// MotivoDaCopia é por que uma cópia foi recusada. A lista é fechada: é o code do
// 422 da nuvem e o que o painel do PC traduz numa frase.
type MotivoDaCopia string

const (
	// MotivoCopiaJSON: não é JSON, tem campo desconhecido, ou não tem a forma de
	// uma cópia (outro usuário, o mapa de domínios, um id de amostra).
	MotivoCopiaJSON MotivoDaCopia = "json"
	// MotivoCopiaGrandeDemais: mais que MaxBytesDaCopia.
	MotivoCopiaGrandeDemais MotivoDaCopia = "grande_demais"
	// MotivoCopiaTermosDemais: mais que MaxTermosDaCopia.
	MotivoCopiaTermosDemais MotivoDaCopia = "termos_demais"
	// MotivoCopiaVariantesDemais: mais que MaxVariantesPorTermoDaCopia num termo,
	// ou mais que MaxVariantesDaCopia no total.
	MotivoCopiaVariantesDemais MotivoDaCopia = "variantes_demais"
	// MotivoCopiaTermoInvalido: um termo vazio, ou um termo, uma variante, um
	// domínio ou um idioma com mais de 80 caracteres.
	MotivoCopiaTermoInvalido MotivoDaCopia = "termo_invalido"
)

// ErrCopia é a cópia recusada, com o motivo.
type ErrCopia struct{ Motivo MotivoDaCopia }

func (e *ErrCopia) Error() string { return "dictionary: cópia recusada: " + string(e.Motivo) }

func recusarCopia(m MotivoDaCopia) error { return &ErrCopia{Motivo: m} }

// ParaACopia devolve a cópia da semente para a nuvem, sem tocar na original:
//   - o usuário vira UsuarioDaCopia;
//   - o mapa de domínios não vai (diz quais programas a pessoa usa, e o celular
//     não tem programa em foco);
//   - de alias_origem, só os valores "painel" e "adicionado" (os ids de amostra
//     são da medição e não mudam o Smart);
//   - as entradas vão todas, inclusive as desligadas, com todos os campos e na
//     ordem do arquivo; as variantes vazias (que o Popular pula) saem.
func (s Semente) ParaACopia() Semente {
	c := Semente{Usuario: UsuarioDaCopia, Entradas: make([]EntradaSemente, 0, len(s.Entradas))}
	for v, origem := range s.AliasOrigem {
		if origem == OrigemPainel || origem == OrigemAdicionado {
			if c.AliasOrigem == nil {
				c.AliasOrigem = map[string]string{}
			}
			c.AliasOrigem[v] = origem
		}
	}
	for _, e := range s.Entradas {
		e.Alias = semVazias(e.Alias)
		e.AliasRestrito = semVazias(e.AliasRestrito)
		c.Entradas = append(c.Entradas, e)
	}
	return c
}

// semVazias copia a lista sem as variantes vazias (ou só de espaço). nil fica
// nil, para o omitempty dar os mesmos bytes.
func semVazias(l []string) []string {
	if l == nil {
		return nil
	}
	out := make([]string, 0, len(l))
	for _, v := range l {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BytesDaCopia é o que o PC manda: a cópia da semente (ParaACopia), conferida
// (ValidarCopia), em JSON, com o SHA-256 dos bytes em hexadecimal minúsculo. O
// erro é sempre *ErrCopia.
func (s Semente) BytesDaCopia() ([]byte, string, error) {
	c := s.ParaACopia()
	if err := ValidarCopia(c); err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, "", recusarCopia(MotivoCopiaJSON)
	}
	if len(b) > MaxBytesDaCopia {
		return nil, "", recusarCopia(MotivoCopiaGrandeDemais)
	}
	return b, SHA256DaCopia(b), nil
}

// SHA256DaCopia é a versão da cópia: o SHA-256 dos bytes, em hexadecimal
// minúsculo.
func SHA256DaCopia(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// LerCopia é o que a nuvem faz com os bytes recebidos: o tamanho, o JSON estrito
// (um objeto só, sem campo desconhecido, como o CarregarSemente), a forma de uma
// cópia e os limites (ValidarCopia). Não monta o Smart; quem recebe monta
// (smartconta) e recusa a cópia que não monta. O erro é sempre *ErrCopia.
func LerCopia(b []byte) (Semente, error) {
	if len(b) > MaxBytesDaCopia {
		return Semente{}, recusarCopia(MotivoCopiaGrandeDemais)
	}
	var s Semente
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Semente{}, recusarCopia(MotivoCopiaJSON)
	}
	// Depois do objeto, só espaço: um segundo objeto não passa calado.
	if _, err := dec.Token(); err != io.EOF {
		return Semente{}, recusarCopia(MotivoCopiaJSON)
	}
	if !eCopia(s) {
		return Semente{}, recusarCopia(MotivoCopiaJSON)
	}
	if err := ValidarCopia(s); err != nil {
		return Semente{}, err
	}
	return s, nil
}

// eCopia diz se a semente tem a forma do que ParaACopia produz: o usuário fixo,
// sem o mapa de domínios e sem id de amostra. É a guarda da nuvem contra guardar
// a lista de programas de quem mandou o arquivo inteiro.
func eCopia(s Semente) bool {
	if s.Usuario != UsuarioDaCopia || len(s.Dominios) > 0 {
		return false
	}
	for _, origem := range s.AliasOrigem {
		if origem != OrigemPainel && origem != OrigemAdicionado {
			return false
		}
	}
	return true
}

// ValidarCopia confere os limites da cópia (ADR-023, A). O erro é sempre
// *ErrCopia, e nunca traz o texto.
func ValidarCopia(s Semente) error {
	if len(s.Entradas) > MaxTermosDaCopia {
		return recusarCopia(MotivoCopiaTermosDemais)
	}
	total := 0
	for _, e := range s.Entradas {
		c := strings.TrimSpace(e.Canonica)
		if c == "" || utf8.RuneCountInString(c) > maxRunasDaCopia ||
			utf8.RuneCountInString(e.Dominio) > maxRunasDaCopia ||
			utf8.RuneCountInString(e.Idioma) > maxRunasDaCopia {
			return recusarCopia(MotivoCopiaTermoInvalido)
		}
		n := len(e.Alias) + len(e.AliasRestrito)
		if n > MaxVariantesPorTermoDaCopia {
			return recusarCopia(MotivoCopiaVariantesDemais)
		}
		total += n
		if total > MaxVariantesDaCopia {
			return recusarCopia(MotivoCopiaVariantesDemais)
		}
		for _, l := range [][]string{e.Alias, e.AliasRestrito} {
			for _, v := range l {
				if utf8.RuneCountInString(v) > maxRunasDaCopia {
					return recusarCopia(MotivoCopiaTermoInvalido)
				}
			}
		}
	}
	return nil
}
