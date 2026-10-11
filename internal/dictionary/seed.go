package dictionary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Este arquivo carrega um dicionário pessoal de um JSON para dentro de um
// DictionaryRepository. É o caminho que popula tanto a medição do portão
// (cmd/entitybench) quanto o Agent, antes de existir persistência real
// (SQLite/Postgres — ver doc.go, "implementação real ... entra atrás do
// mesmo contrato").

// Semente é um dicionário pessoal em formato de arquivo.
type Semente struct {
	// Usuario escopa todas as entradas. O dicionário é pessoal por
	// definição (REQ-019), e Search filtra por UserID.
	Usuario string `json:"usuario"`

	// Dominios mapeia domínio -> executáveis que o caracterizam, e é o
	// que permite uma entrada Restricted valer só onde deve:
	//
	//	"dominios": {"dev": ["Code.exe", "devenv.exe"]}
	//
	// Fica no arquivo, e não numa lista dentro do código, de propósito:
	// quais programas são "de desenvolvimento" é escolha de quem usa, e
	// uma heurística escondida seria impossível de auditar ou corrigir.
	//
	// App que não está em nenhum domínio não tem domínio — e entrada
	// restrita não se aplica a ele. Navegador fica de fora de propósito:
	// o mesmo Chrome é onde se lê documentação e onde se escreve e-mail.
	Dominios map[string][]string `json:"dominios,omitempty"`

	// AliasOrigem registra de qual amostra do dataset cada alias veio:
	//
	//	"alias_origem": {"DAAS": "cont-04"}
	//
	// Serve a duas coisas. A primeira é documentação: a regra é que todo
	// alias venha de um erro MEDIDO, nunca de suposição, e isto torna a
	// regra verificável em vez de só declarada.
	//
	// A segunda é medir GENERALIZAÇÃO. Aprender alias das mesmas
	// amostras usadas para medir faz o número validar a si mesmo; com a
	// procedência registrada dá para remover os alias de um lado e medir
	// no outro (SemAliasDe).
	//
	// Alias sem procedência aqui é tratado como sem origem conhecida, e
	// sobrevive a qualquer recorte — porque não há como saber que ele
	// veio do lado de teste.
	AliasOrigem map[string]string `json:"alias_origem,omitempty"`

	Entradas []EntradaSemente `json:"entradas"`
}

// SemAliasDe devolve uma cópia da semente sem os alias cuja origem está
// em amostras, mantendo tudo o mais.
//
// É o que permite medir generalização de verdade: aprender numa metade
// do dataset e medir na outra. Sem isso, o número mede a capacidade do
// dicionário de reproduzir erros que ele já viu — que é uma pergunta
// diferente, e muito mais fácil.
//
// As formas CANÔNICAS não são removidas, de propósito: elas vêm da
// pessoa, não da medição. Um dicionário pessoal legitimamente conhece os
// termos de quem o usa antes de qualquer áudio existir.
func (s Semente) SemAliasDe(amostras map[string]bool) Semente {
	fora := func(alias string) bool {
		origem, temOrigem := s.AliasOrigem[alias]
		return temOrigem && amostras[origem]
	}
	filtrar := func(as []string) []string {
		out := make([]string, 0, len(as))
		for _, a := range as {
			if !fora(a) {
				out = append(out, a)
			}
		}
		return out
	}

	copia := s
	copia.Entradas = make([]EntradaSemente, 0, len(s.Entradas))
	for _, e := range s.Entradas {
		e.Alias = filtrar(e.Alias)
		e.AliasRestrito = filtrar(e.AliasRestrito)
		copia.Entradas = append(copia.Entradas, e)
	}
	return copia
}

// EntradaSemente é um termo do dicionário com suas variantes conhecidas.
type EntradaSemente struct {
	// Canonica é a forma correta — o que deve aparecer no texto final.
	Canonica string `json:"canonica"`
	// Prioridade decide quem entra no vocabulário do ASR quando o
	// orçamento aperta (ver Vocabulario) e desempata candidatos no
	// Search. Maior ganha.
	Prioridade int `json:"prioridade"`
	// Dominio e Idioma restringem quando o termo se aplica. Vazio
	// significa "universal" — sempre considerado.
	Dominio string `json:"dominio,omitempty"`
	Idioma  string `json:"idioma,omitempty"`
	// Alias são as variantes JÁ CONFIRMADAS que devem ser corrigidas
	// automaticamente para a forma canônica.
	//
	// Isto não é enfeite: é o único caminho pelo qual o dicionário
	// corrige alguma coisa em produção hoje. SmartResolver auto-aplica
	// match `exact` e `alias`, mas match FONÉTICO sozinho é tratado como
	// evidência fraca e só é aplicado se o contexto corroborar — e a
	// captura de contexto (TASK-009) não existe. Ver smart.go,
	// MatchReasonPhonetic.
	//
	// Cada alias aqui deve vir de um erro MEDIDO do ASR, nunca de
	// suposição: alias inventado é correção falsa esperando acontecer, e
	// a correção falsa tem portão próprio de ≤1%.
	Alias []string `json:"alias,omitempty"`
	// AliasRestrito são alias que só valem dentro do Dominio da entrada —
	// os que colidem com palavra comum ("comite" -> "commit"). Ficam
	// separados de Alias de propósito: proteger um alias perigoso não
	// pode trancar os seguros da mesma entrada.
	AliasRestrito []string `json:"alias_restrito,omitempty"`
	// Restrito marca a entrada como válida SÓ dentro do seu domínio —
	// ver DictionaryEntry.Restricted. É o que torna seguro um alias que
	// colide com palavra comum ("comite" -> "commit").
	Restrito bool `json:"restrito,omitempty"`
	// Inativa desliga o termo sem apagá-lo — some do vocabulário do ASR
	// e do Search.
	Inativa bool `json:"inativa,omitempty"`
	// Origem marca a entrada que o PAINEL criou ao aprender uma variante de
	// um termo que ainda não existia ("painel"). Vazio é o caso comum: a
	// entrada veio da pessoa ou do dictlearn.
	//
	// Existe para dois fins. O primeiro: Desfazer saber se a entrada nasceu
	// do aprendizado (e então sai inteira, deixando o arquivo como estava) ou
	// se já existia (e então só perde a variante). O segundo: a entrada do
	// painel age só pela variante que a pessoa confirmou, e não entra no
	// vocabulário do ASR (ver DictionaryEntry.AliasOnly). Apagar o campo à
	// mão é o jeito de a pessoa adotar o termo. Não confundir com
	// AliasOrigem, que é por variante.
	//
	// Um Agent anterior a este campo recusa um arquivo que o tenha (o
	// carregamento rejeita campo desconhecido): voltar de versão pede
	// Desfazer antes, ou apagar o campo.
	//
	// O valor "adicionado" (OrigemAdicionado, ADR-020) marca o termo que a
	// pessoa pôs pelo "Adicionar termo" das Configurações. É um VALOR novo, e
	// não um campo: um Agent anterior a ele carrega o arquivo e trata o termo
	// como um termo comum (só "painel" é AliasOnly), o que é o que ele é. A
	// dica para o ASR é a prioridade (PrioridadeDica), e não outro campo.
	Origem string `json:"origem,omitempty"`
}

// CarregarSemente lê o JSON e devolve a semente, sem tocar em
// repositório nenhum. Separado de Popular para que um arquivo malformado
// falhe ANTES de meio dicionário ter sido escrito.
func CarregarSemente(r io.Reader) (Semente, error) {
	var s Semente
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields() // campo com nome errado é erro, não silêncio
	if err := dec.Decode(&s); err != nil {
		return Semente{}, fmt.Errorf("dictionary: lendo semente: %w", err)
	}
	if strings.TrimSpace(s.Usuario) == "" {
		return Semente{}, fmt.Errorf("dictionary: semente sem usuario — o dicionário é pessoal e Search filtra por ele")
	}
	return s, nil
}

// Popular escreve a semente no repositório.
//
// O ID de cada entrada é DERIVADO da forma normalizada, não gerado ao
// acaso: rodar a mesma semente duas vezes produz o mesmo dicionário, o
// que é o que torna a medição do portão reproduzível. Alias seguem a
// mesma regra.
//
// Entradas são escritas na ordem do arquivo. Um erro interrompe e é
// devolvido com o termo que o causou — sem isso, "ErrInvalidEntry" num
// arquivo de 43 termos não diria qual.
func (s Semente) Popular(ctx context.Context, repo DictionaryRepository) error {
	for _, e := range s.Entradas {
		canonica := strings.TrimSpace(e.Canonica)
		if canonica == "" {
			return fmt.Errorf("dictionary: semente tem entrada sem forma canônica")
		}
		id := idDeterministico("e", s.Usuario, canonica)
		entrada := DictionaryEntry{
			ID:            id,
			UserID:        s.Usuario,
			CanonicalForm: canonica,
			Priority:      e.Prioridade,
			Active:        !e.Inativa,
			Restricted:    e.Restrito,
			AliasOnly:     e.Origem == OrigemPainel,
			Adicionado:    e.Origem == OrigemAdicionado,
			Language:      opcional(e.Idioma),
			Domain:        opcional(e.Dominio),
		}
		if err := repo.Upsert(ctx, entrada); err != nil {
			return fmt.Errorf("dictionary: gravando %q: %w", canonica, err)
		}

		for _, a := range append(append([]string{}, e.Alias...), e.AliasRestrito...) {
			restrito := false
			for _, r := range e.AliasRestrito {
				if r == a {
					restrito = true
				}
			}
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			// Alias igual à forma canônica é ruído: o match exato já
			// resolve, e guardá-lo só gastaria espaço e confundiria quem
			// for ler o dicionário depois.
			if Normalize(a) == Normalize(canonica) {
				continue
			}
			if err := repo.UpsertAlias(ctx, DictionaryAlias{
				ID:         idDeterministico("a", id, a),
				EntryID:    id,
				Alias:      a,
				Restricted: restrito,
				Source:     SourceManual,
			}); err != nil {
				return fmt.Errorf("dictionary: gravando alias %q de %q: %w", a, canonica, err)
			}
		}
	}
	return nil
}

// Entradas devolve a semente como DictionaryEntry, sem passar por
// repositório — é o que Vocabulario consome para montar o prompt do ASR.
func (s Semente) ParaEntradas() []DictionaryEntry {
	out := make([]DictionaryEntry, 0, len(s.Entradas))
	for _, e := range s.Entradas {
		canonica := strings.TrimSpace(e.Canonica)
		if canonica == "" {
			continue
		}
		out = append(out, DictionaryEntry{
			ID:            idDeterministico("e", s.Usuario, canonica),
			UserID:        s.Usuario,
			CanonicalForm: canonica,
			Priority:      e.Prioridade,
			Active:        !e.Inativa,
			Restricted:    e.Restrito,
			AliasOnly:     e.Origem == OrigemPainel,
			Adicionado:    e.Origem == OrigemAdicionado,
			Language:      opcional(e.Idioma),
			Domain:        opcional(e.Dominio),
		})
	}
	return out
}

// idDeterministico monta um ID estável a partir das partes. Não é hash:
// é legível de propósito, porque aparece em mensagem de erro e em
// inspeção do dicionário, e um hash ali só esconderia qual termo deu
// problema.
func idDeterministico(prefixo string, partes ...string) string {
	limpas := make([]string, 0, len(partes)+1)
	limpas = append(limpas, prefixo)
	for _, p := range partes {
		limpas = append(limpas, Normalize(p))
	}
	return strings.Join(limpas, ":")
}

// opcional converte string vazia em nil — a convenção de Language/Domain
// em DictionaryEntry, onde nil significa "universal" e um valor
// significa "só neste".
func opcional(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

// DominioDoApp devolve o domínio do executável em foco, ou nil quando ele
// não está em nenhum — e nil é a resposta SEGURA: entrada restrita não se
// aplica a domínio desconhecido.
//
// A comparação ignora maiúsculas porque o Windows não as distingue em
// nome de arquivo, e "Code.exe" e "code.exe" são o mesmo programa.
func (s Semente) DominioDoApp(processName string) *string {
	alvo := strings.ToLower(strings.TrimSpace(processName))
	if alvo == "" {
		return nil
	}
	// Ordem determinística: um app listado em dois domínios daria
	// resultado diferente a cada execução se dependesse do mapa.
	dominios := make([]string, 0, len(s.Dominios))
	for d := range s.Dominios {
		dominios = append(dominios, d)
	}
	sort.Strings(dominios)

	for _, d := range dominios {
		for _, app := range s.Dominios[d] {
			if strings.EqualFold(strings.TrimSpace(app), alvo) {
				dom := d
				return &dom
			}
		}
	}
	return nil
}
