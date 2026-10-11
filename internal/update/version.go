package update

import (
	"fmt"
	"strconv"
	"strings"
)

// semver é uma versão MAJOR.MINOR.PATCH[-PRERELEASE] já parseada.
// Metadado de build ("+...") é aceito na entrada mas descartado: não
// participa de precedência (regra do semver.org).
type semver struct {
	major, minor, patch int
	pre                 []string // identificadores de pré-release, já separados por "."; nil se release.
}

// parseSemver interpreta s como versão semântica. Aceita um "v" opcional
// na frente (ex.: "v0.10.0"). Rejeita qualquer coisa que não seja
// MAJOR.MINOR.PATCH numérico, com pré-release opcional de identificadores
// não vazios.
func parseSemver(s string) (semver, error) {
	orig := s
	s = strings.TrimPrefix(s, "v")

	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // metadado de build: ignorado para precedência.
	}

	var preRaw string
	hasPre := false
	if i := strings.IndexByte(s, '-'); i >= 0 {
		preRaw = s[i+1:]
		s = s[:i]
		hasPre = true
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("update: versão %q não é MAJOR.MINOR.PATCH", orig)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" {
			return semver{}, fmt.Errorf("update: versão %q tem componente numérico vazio", orig)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("update: versão %q tem componente numérico inválido %q", orig, p)
		}
		nums[i] = n
	}

	var pre []string
	if hasPre {
		if preRaw == "" {
			return semver{}, fmt.Errorf("update: versão %q tem pré-release vazio após \"-\"", orig)
		}
		pre = strings.Split(preRaw, ".")
		for _, id := range pre {
			if id == "" {
				return semver{}, fmt.Errorf("update: versão %q tem identificador de pré-release vazio", orig)
			}
		}
	}

	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, nil
}

// compare devolve -1, 0 ou 1 conforme v é menor, igual ou maior que o,
// seguindo a regra de precedência do semver.org: MAJOR.MINOR.PATCH
// numérico primeiro; a versão sem pré-release é sempre maior que a mesma
// versão com pré-release; entre dois pré-releases, compara identificador a
// identificador (numérico < alfanumérico; numérico comparado por valor;
// alfanumérico comparado lexicograficamente); em empate total, quem tem
// mais identificadores é maior.
func (v semver) compare(o semver) int {
	if c := cmpInt(v.major, o.major); c != 0 {
		return c
	}
	if c := cmpInt(v.minor, o.minor); c != 0 {
		return c
	}
	if c := cmpInt(v.patch, o.patch); c != 0 {
		return c
	}

	switch {
	case len(v.pre) == 0 && len(o.pre) == 0:
		return 0
	case len(v.pre) == 0:
		return 1 // release > pré-release
	case len(o.pre) == 0:
		return -1
	}

	n := len(v.pre)
	if len(o.pre) < n {
		n = len(o.pre)
	}
	for i := 0; i < n; i++ {
		if c := comparePreID(v.pre[i], o.pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(v.pre), len(o.pre))
}

// comparePreID compara um único identificador de pré-release conforme
// semver.org: identificadores puramente numéricos comparam por valor
// inteiro; um identificador numérico sempre precede um alfanumérico;
// dois alfanuméricos comparam lexicograficamente (ASCII).
func comparePreID(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	aNum, bNum := aErr == nil, bErr == nil

	switch {
	case aNum && bNum:
		return cmpInt(an, bn)
	case aNum && !bNum:
		return -1
	case !aNum && bNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// Compare compara duas versões semânticas (MAJOR.MINOR.PATCH[-PRERELEASE]),
// devolvendo -1 se a < b, 0 se a == b, 1 se a > b. Erra se a ou b não forem
// versões semânticas válidas — nunca compara como string (esse é o erro
// clássico: "0.9.0" > "0.10.0" lexicograficamente, o que é errado).
func Compare(a, b string) (int, error) {
	va, err := parseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("update: versão local inválida: %w", err)
	}
	vb, err := parseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("update: versão remota inválida: %w", err)
	}
	return va.compare(vb), nil
}

// IsNewer reporta se candidate é semanticamente maior que current. Nunca
// recomenda downgrade: candidate igual ou menor que current devolve false,
// não erro. Uma versão que não parseia como semver devolve (false, erro) —
// quem chama trata isso como "falha limpa, sem instalar".
func IsNewer(current, candidate string) (bool, error) {
	c, err := Compare(candidate, current)
	if err != nil {
		return false, err
	}
	return c > 0, nil
}
