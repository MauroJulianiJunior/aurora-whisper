package cloudauth

import (
	"math"
	"sync"
	"time"
)

// Limite é um balde de fichas por chave (o IP no login, o aparelho no push),
// com o relógio injetado para os testes. Fica em memória: um processo só, e
// reiniciar o servidor zera os baldes (ADR-019, "Decisões da F3").
//
// Cada chave começa com o balde cheio (porMinuto fichas) e ganha porMinuto
// fichas por minuto, até encher de novo. Seguro para uso concorrente.
type Limite struct {
	mu        sync.Mutex
	porMinuto float64
	agora     func() time.Time
	baldes    map[string]*balde
}

type balde struct {
	fichas float64
	em     time.Time
}

// maxChaves: acima disso, Permitir apaga os baldes que já encheram de novo
// (iguais a um balde novo). Um balde enche em um minuto, então o que fica são
// as chaves do último minuto, e um ataque com muitos IPs não cresce a memória
// sem fim.
const maxChaves = 10000

// NovoLimite cria um limite de porMinuto por chave. agora nil é time.Now.
func NovoLimite(porMinuto int, agora func() time.Time) *Limite {
	if agora == nil {
		agora = time.Now
	}
	return &Limite{porMinuto: float64(porMinuto), agora: agora, baldes: make(map[string]*balde)}
}

// Permitir gasta n fichas da chave. Sem fichas, devolve false e quanto falta
// esperar (o Retry-After, arredondado para cima em segundos por quem
// responde). n maior que o balde nunca passa.
func (l *Limite) Permitir(chave string, n int) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.agora()
	if float64(n) > l.porMinuto {
		return false, time.Minute
	}
	b, ok := l.baldes[chave]
	if !ok {
		if len(l.baldes) >= maxChaves {
			l.arrumar(now)
		}
		b = &balde{fichas: l.porMinuto, em: now}
		l.baldes[chave] = b
	}
	b.encher(now, l.porMinuto)
	if b.fichas >= float64(n) {
		b.fichas -= float64(n)
		return true, 0
	}
	falta := (float64(n) - b.fichas) / (l.porMinuto / 60)
	return false, time.Duration(math.Ceil(falta * float64(time.Second)))
}

func (b *balde) encher(now time.Time, porMinuto float64) {
	if d := now.Sub(b.em); d > 0 {
		b.fichas = math.Min(porMinuto, b.fichas+d.Minutes()*porMinuto)
	}
	b.em = now
}

func (l *Limite) arrumar(now time.Time) {
	for k, b := range l.baldes {
		b.encher(now, l.porMinuto)
		if b.fichas >= l.porMinuto {
			delete(l.baldes, k)
		}
	}
}

// RetryAfter é o valor do cabeçalho Retry-After: segundos inteiros, para
// cima, e pelo menos 1.
func RetryAfter(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return s
}
