package smartconta

import (
	"context"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O "agiria" do lote, com memória.
//
// ValidarPar monta um Smart novo, do zero, a cada par que confere: com 500
// pares aprendidos, o lote do Smart da conta monta 1000 Smarts (um por par, na
// entrada, e outro por par, na conferência final), cada um com centenas de
// termos. É o que fazia montar a conta levar segundos. O acompanhante guarda o
// Smart da última semente que viu e, quando a seguinte é a mesma com termos ou
// variantes a mais (é o que o lote faz: um par de cada vez, sobre a semente
// acumulada), só acrescenta o que veio. Qualquer outra mudança (uma variante
// que saiu, um termo que mudou, outro usuário) refaz o Smart do zero, pelo
// mesmo caminho de SmartDaSemente.
//
// O resultado é o de ValidarPar, par a par: o repositório que o acompanhante
// mantém tem o mesmo conteúdo que SmartDaSemente montaria da mesma semente (as
// entradas e os aliases têm id derivado da forma, e gravar de novo o que já
// está é gravar no mesmo lugar). O teste de equivalência
// (acompanhante_test.go) roda o lote com os dois, em sequências sorteadas, e
// exige a mesma semente e os mesmos resultados.
//
// Não é seguro para uso concorrente: vive dentro de um lote (Montar cria um por
// chamada), que confere um par depois do outro.
type acompanhante struct {
	usuario string
	// entradas é o que o repositório reflete: uma cópia (os aliases são
	// copiados) das entradas da última semente vista, na ordem dela.
	entradas []dictionary.EntradaSemente
	// normais são as formas normalizadas das entradas do repositório. A forma
	// repetida faria o repositório fundir duas entradas, e isso o acompanhante
	// deixa para a montagem do zero.
	normais map[string]bool
	smart   *resolver.SmartResolver

	// refeitos e acrescidos contam quantas vezes o Smart foi montado do zero e
	// quantas só ganhou o que veio. Só o teste os lê: ele exige que os dois
	// caminhos tenham sido percorridos.
	refeitos, acrescidos int
}

// conferenciasDaConta são as Conferencias do PC com o "agiria" acompanhando a
// semente (as mesmas respostas, sem montar um Smart novo a cada par). Devolve
// também o acompanhante, que guarda o Smart da última semente conferida. Cada
// lote usa o seu: o acompanhante não é seguro para uso concorrente.
func conferenciasDaConta() (dictionary.ConferenciasDoLote, *acompanhante) {
	a := &acompanhante{}
	return dictionary.ConferenciasDoLote{PalavraComum: PalavraComum, Agiria: a.agiria}, a
}

// agiria é ValidarPar: o par tem de AGIR na semente, com a restrição efetiva dele.
func (a *acompanhante) agiria(s dictionary.Semente, p dictionary.ParDoPainel) error {
	smart, err := a.smartDe(s)
	if err != nil {
		return err
	}
	age, err := AgeEm(smart, s, dictionary.Aprendida{Termo: p.Termo, Variante: p.Variante}, p.Restrito)
	if err != nil {
		return err
	}
	if !age {
		return dictionary.ErrNaoAgiria
	}
	return nil
}

// smartDe devolve o Smart de s: o que já tem, com o que s acrescentou, ou um
// novo, se s não é a última semente vista com acréscimos.
func (a *acompanhante) smartDe(s dictionary.Semente) (*resolver.SmartResolver, error) {
	if a.smart != nil {
		if mudadas, ok := a.acrescimosDe(s); ok {
			if err := a.acrescentar(s, mudadas); err == nil {
				a.acrescidos++
				return a.smart, nil
			}
			// Não deveria acontecer. O repositório pode estar pela metade:
			// refaz do zero, e o erro, se for de verdade, volta de lá.
		}
	}
	return a.refazer(s)
}

// refazer monta o Smart de s do zero (o caminho de SmartDaSemente) e passa a
// acompanhar s.
func (a *acompanhante) refazer(s dictionary.Semente) (*resolver.SmartResolver, error) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		a.smart = nil
		return nil, err
	}
	a.refeitos++
	a.usuario = s.Usuario
	a.entradas = make([]dictionary.EntradaSemente, 0, len(s.Entradas))
	a.normais = make(map[string]bool, len(s.Entradas))
	for _, e := range s.Entradas {
		a.entradas = append(a.entradas, copiaDaEntrada(e))
		a.normais[dictionary.Normalize(e.Canonica)] = true
	}
	a.smart = resolver.NewSmartResolver(repo)
	return a.smart, nil
}

// acrescimosDe diz se s é a última semente vista com acréscimos (termos novos no
// fim, variantes novas nos termos que já havia) e quais entradas de s o
// repositório ainda não tem por inteiro. ok falso: s é outra coisa, e o Smart
// se refaz.
func (a *acompanhante) acrescimosDe(s dictionary.Semente) (mudadas []int, ok bool) {
	if s.Usuario != a.usuario || len(s.Entradas) < len(a.entradas) {
		return nil, false
	}
	for i, v := range a.entradas {
		e := s.Entradas[i]
		if e.Canonica != v.Canonica || e.Prioridade != v.Prioridade || e.Dominio != v.Dominio ||
			e.Idioma != v.Idioma || e.Restrito != v.Restrito || e.Inativa != v.Inativa || e.Origem != v.Origem {
			return nil, false
		}
		maisAlias, cabe := soAcrescenta(v.Alias, e.Alias)
		if !cabe {
			return nil, false
		}
		maisRestritos, cabe := soAcrescenta(v.AliasRestrito, e.AliasRestrito)
		if !cabe {
			return nil, false
		}
		if maisAlias || maisRestritos {
			mudadas = append(mudadas, i)
		}
	}
	novas := map[string]bool{}
	for i := len(a.entradas); i < len(s.Entradas); i++ {
		n := dictionary.Normalize(s.Entradas[i].Canonica)
		if n == "" || a.normais[n] || novas[n] {
			return nil, false
		}
		novas[n] = true
		mudadas = append(mudadas, i)
	}
	return mudadas, true
}

// acrescentar grava no repositório as entradas mudadas de s (inteiras: gravar
// de novo o que já está é gravar no mesmo lugar) e passa a acompanhar s.
func (a *acompanhante) acrescentar(s dictionary.Semente, mudadas []int) error {
	if len(mudadas) == 0 {
		return nil
	}
	parcial := dictionary.Semente{Usuario: s.Usuario, Entradas: make([]dictionary.EntradaSemente, 0, len(mudadas))}
	for _, i := range mudadas {
		parcial.Entradas = append(parcial.Entradas, s.Entradas[i])
	}
	if err := parcial.Popular(context.Background(), a.smart.Dictionary); err != nil {
		return err
	}
	for _, i := range mudadas {
		e := copiaDaEntrada(s.Entradas[i])
		if i < len(a.entradas) {
			a.entradas[i] = e
			continue
		}
		a.entradas = append(a.entradas, e)
		a.normais[dictionary.Normalize(e.Canonica)] = true
	}
	return nil
}

// soAcrescenta compara a lista de antes com a de depois: cabe é verdadeiro
// quando a de antes é uma subsequência da de depois (as mesmas variantes, na
// mesma ordem, e talvez outras no meio ou no fim), e mais, quando a de depois
// tem variantes a mais.
func soAcrescenta(antes, depois []string) (mais, cabe bool) {
	if len(depois) < len(antes) {
		return false, false
	}
	j := 0
	for _, d := range depois {
		if j < len(antes) && d == antes[j] {
			j++
		}
	}
	if j != len(antes) {
		return false, false
	}
	return len(depois) > len(antes), true
}

func copiaDaEntrada(e dictionary.EntradaSemente) dictionary.EntradaSemente {
	e.Alias = append([]string(nil), e.Alias...)
	e.AliasRestrito = append([]string(nil), e.AliasRestrito...)
	return e
}
