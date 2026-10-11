package main

import (
	"context"
	"reflect"
	"strings"
	"unicode"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// O "Adicionar termo" das Configurações do lado do binário do Agent
// (ADR-020): grava no mesmo dicionario.json, pela mesma trava do aprendizado
// (aprendiz.Aplicador), e troca o dicionário E o prompt do ASR em memória na
// hora. A dica que a pessoa liga vale no próximo ditado, sem reiniciar. Aqui
// fica a conferência da mudança (conferirTermos), que o Aplicador recebe.

var _ dictionary.Termos = (*aprendizadoDoAgent)(nil)

// conferirTermos é o Arquivo.ConferirMudanca do Agent: cada grafia nova AGE
// (o Smart de depois, diante dela sozinha, escreve o termo, em todo domínio
// onde o termo vive), e nenhuma variante que agia antes deixa de agir
// (conferirMudanca). Não loga nada: variante e termo são texto da pessoa.
func conferirTermos(antes, depois dictionary.Semente, novas []dictionary.Aprendida) error {
	smartDepois, err := smartconta.SmartDaSemente(depois)
	if err != nil {
		return err
	}
	for _, n := range novas {
		age, err := smartconta.AgeEm(smartDepois, depois, n, false)
		if err != nil {
			return err
		}
		if !age {
			return &dictionary.ErroNaoAgiria{Variante: n.Variante}
		}
	}
	return conferirMudanca(antes, depois, smartDepois)
}

// conferirMudanca exige que toda variante que agia no dicionário de antes, e
// que segue no de depois no mesmo termo, continue agindo, em cada domínio da
// conferência (smartconta.DominiosDaConferencia). É o que pega a regressão nova do
// "Adicionar termo": o termo com dica tem prioridade 100 e casamento fonético,
// e a busca põe a prioridade antes do casamento, então ele pode passar na
// frente do alias confirmado de outro termo ("Viti" deixaria de virar "Vitti"
// com "Vite" na dica). Desligar a dica de um termo também pode tirar dele a
// busca das suas próprias variantes. A primeira que deixaria de agir volta
// como *dictionary.ErroQuebraria.
//
// O que saiu do dicionário (Remover) não é conferido: sair é o pedido.
//
// # A forma canônica dos outros termos
//
// Depois dos alias, a forma canônica de cada termo ligado, escrita certa,
// tem de continuar sendo ela mesma (conferirCanonicas). A fonética sozinha
// não é aplicada, mas a corroborada pelo contexto é: com "Vite" na dica (100) e
// "Vitti" (60) no dicionário, "reunião com o Vitti" viraria "reunião com o
// Vite" sempre que o texto perto do cursor citasse "vite". A conferência
// resolve a forma canônica sozinha, com o contexto citando os termos que
// mudaram (o pior caso), e a que deixaria de ser ela mesma volta como
// *dictionary.ErroQuebraria com Variante igual a Termo, e Vira.
//
// # Só o que a mudança alcança
//
// Conferir toda variante custa duas resoluções por variante e domínio, e a
// busca em memória percorre os alias de todas as entradas: no teto do
// "Adicionar termo" (50 termos de 10 grafias, sobre o dicionário do
// repositório) passou de um segundo por clique (BenchmarkConferirTermos_NoTeto,
// 01-out-2026). Por isso a conferência só resolve a variante que a mudança
// ALCANÇA (alcancadas): a de que algum trecho casa, por forma, alias ou
// fonética, com uma entrada que mudou. Para a outra, cada busca que o Smart faz
// sobre ela devolve o mesmo antes e depois (as entradas que casam com ela são
// as mesmas, iguais), e o resultado não muda.
func conferirMudanca(antes, depois dictionary.Semente, smartDepois *resolver.SmartResolver) error {
	smartAntes, err := smartconta.SmartDaSemente(antes)
	if err != nil {
		return err
	}
	alcanca := alcancadas(antes, depois)
	for _, e := range antes.Entradas {
		if e.Inativa {
			continue
		}
		for _, lista := range [][]string{e.Alias, e.AliasRestrito} {
			for _, v := range lista {
				if !temAVariante(depois, e.Canonica, v) || !alcanca(v) {
					continue
				}
				par := dictionary.Aprendida{Termo: e.Canonica, Variante: v}
				for _, dominio := range smartconta.DominiosDaConferencia(antes, e.Canonica, false) {
					agia, err := smartconta.AgeNoDominio(smartAntes, antes.Usuario, par, dominio)
					if err != nil {
						return err
					}
					if !agia {
						continue
					}
					age, err := smartconta.AgeNoDominio(smartDepois, depois.Usuario, par, dominio)
					if err != nil {
						return err
					}
					if !age {
						return &dictionary.ErroQuebraria{Variante: v, Termo: e.Canonica}
					}
				}
			}
		}
	}
	return conferirCanonicas(antes, depois, smartAntes, smartDepois, alcanca)
}

// alcancadas devolve o filtro das variantes que a mudança de antes para depois
// pode alcançar: as que têm um trecho (de até MaxPalavrasVariante palavras,
// como o Smart procura) que casa com uma entrada que mudou, na versão de antes
// ou na de depois.
//
// A busca é feita num dicionário só com as entradas que mudaram, e com tudo
// liberado (sem domínio, restrição, desligamento nem origem do painel): o que
// casa no dicionário inteiro casa aqui também, e o filtro pode deixar passar
// a mais, nunca a menos. Se esse dicionário pequeno não monta, o filtro deixa
// passar tudo.
func alcancadas(antes, depois dictionary.Semente) func(string) bool {
	tudo := func(string) bool { return true }
	deAntes, deDepois := mudadas(antes, depois)
	var repos []*dictionary.InMemoryDictionaryRepository
	for _, es := range [][]dictionary.EntradaSemente{deAntes, deDepois} {
		if len(es) == 0 {
			continue
		}
		livres := make([]dictionary.EntradaSemente, len(es))
		for i, e := range es {
			livres[i] = liberada(e)
		}
		repo := dictionary.NewInMemoryDictionaryRepository(nil)
		if err := (dictionary.Semente{Usuario: "conferencia", Entradas: livres}).Popular(context.Background(), repo); err != nil {
			return tudo
		}
		repos = append(repos, repo)
	}
	return func(v string) bool {
		for _, trecho := range trechosDe(v) {
			for _, repo := range repos {
				cands, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "conferencia", Term: trecho})
				if err != nil || len(cands) > 0 {
					return true
				}
			}
		}
		return false
	}
}

// mudadas são as entradas que a mudança tocou: as de antes que não estão
// iguais em depois, e as de depois que não estavam iguais em antes.
func mudadas(antes, depois dictionary.Semente) (deAntes, deDepois []dictionary.EntradaSemente) {
	for _, e := range antes.Entradas {
		d, ok := entradaDoTermo(depois, e.Canonica)
		if !ok || !reflect.DeepEqual(e, d) {
			deAntes = append(deAntes, e)
		}
	}
	for _, d := range depois.Entradas {
		e, ok := entradaDoTermo(antes, d.Canonica)
		if !ok || !reflect.DeepEqual(e, d) {
			deDepois = append(deDepois, d)
		}
	}
	return deAntes, deDepois
}

// conferirCanonicas exige que a forma canônica de cada termo ligado que segue
// ligado depois, e que a mudança alcança, continue resolvendo para ela mesma
// em cada domínio da conferência, ditada sozinha e com o contexto citando os
// termos mudados (ligados) de depois. Só confere a que já resolvia para ela
// mesma antes, com o mesmo contexto.
func conferirCanonicas(antes, depois dictionary.Semente, smartAntes, smartDepois *resolver.SmartResolver, alcanca func(string) bool) error {
	_, deDepois := mudadas(antes, depois)
	var citadas []string
	for _, d := range deDepois {
		if !d.Inativa {
			citadas = append(citadas, d.Canonica)
		}
	}
	if len(citadas) == 0 {
		return nil
	}
	contexto := strings.Join(citadas, " ")
	for _, e := range antes.Entradas {
		if e.Inativa || !alcanca(e.Canonica) {
			continue
		}
		if d, ok := entradaDoTermo(depois, e.Canonica); !ok || d.Inativa {
			continue
		}
		for _, dominio := range smartconta.DominiosDaConferencia(antes, e.Canonica, false) {
			era, err := resolverSozinha(smartAntes, antes.Usuario, e.Canonica, dominio, contexto)
			if err != nil {
				return err
			}
			if !smartconta.IgualNoDicionario(era, e.Canonica) {
				continue
			}
			fica, err := resolverSozinha(smartDepois, depois.Usuario, e.Canonica, dominio, contexto)
			if err != nil {
				return err
			}
			if !smartconta.IgualNoDicionario(fica, e.Canonica) {
				return &dictionary.ErroQuebraria{Variante: e.Canonica, Termo: e.Canonica, Vira: fica}
			}
		}
	}
	return nil
}

// resolverSozinha é o que o Smart escreve diante de texto sozinho, com o app
// em foco no domínio dado (nil: o desconhecido) e perto como o texto antes
// do cursor. Não loga nada.
func resolverSozinha(smart *resolver.SmartResolver, usuario, texto string, dominio *string, perto string) (string, error) {
	out, err := smart.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:      usuario,
		ASRResult:   asr.ASRResult{Text: texto},
		Context:     uiacontext.ContextSnapshot{PrefixNearCaret: perto},
		CloudPolicy: resolver.CloudDisabled,
		Domain:      dominio,
	})
	if err != nil {
		return "", err
	}
	return out.FinalText, nil
}

// liberada é e sem nada que a esconda numa busca.
func liberada(e dictionary.EntradaSemente) dictionary.EntradaSemente {
	e.Alias = append(append([]string(nil), e.Alias...), e.AliasRestrito...)
	e.AliasRestrito = nil
	e.Restrito, e.Dominio, e.Idioma, e.Inativa, e.Origem = false, "", "", false, ""
	return e
}

// entradaDoTermo acha a entrada do termo em s.
func entradaDoTermo(s dictionary.Semente, termo string) (dictionary.EntradaSemente, bool) {
	for _, e := range s.Entradas {
		if smartconta.IgualNoDicionario(e.Canonica, termo) {
			return e, true
		}
	}
	return dictionary.EntradaSemente{}, false
}

// trechosDe são os trechos de v que o Smart consulta: cada sequência de até
// MaxPalavrasVariante palavras seguidas (letras e dígitos), como está escrita.
func trechosDe(v string) []string {
	runas := []rune(v)
	type palavra struct{ ini, fim int }
	var ps []palavra
	for i := 0; i < len(runas); {
		if !unicode.IsLetter(runas[i]) && !unicode.IsDigit(runas[i]) {
			i++
			continue
		}
		ini := i
		for i < len(runas) && (unicode.IsLetter(runas[i]) || unicode.IsDigit(runas[i])) {
			i++
		}
		ps = append(ps, palavra{ini, i})
	}
	var out []string
	for i := range ps {
		for j := i; j < len(ps) && j-i < dictionary.MaxPalavrasVariante; j++ {
			out = append(out, string(runas[ps[i].ini:ps[j].fim]))
		}
	}
	return out
}

// temAVariante diz se s tem v, exatamente, como variante do termo, ligado.
func temAVariante(s dictionary.Semente, termo, v string) bool {
	for _, e := range s.Entradas {
		if e.Inativa || !smartconta.IgualNoDicionario(e.Canonica, termo) {
			continue
		}
		for _, lista := range [][]string{e.Alias, e.AliasRestrito} {
			for _, a := range lista {
				if a == v {
					return true
				}
			}
		}
	}
	return false
}
