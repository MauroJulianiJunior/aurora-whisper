package dictionary

// O índice de busca do repositório em memória.
//
// Search compara a consulta com cada entrada e com os aliases dela. Antes do
// índice, cada comparação refazia o que só depende da entrada: a forma sem
// espaços (CollapseSpaces) e a chave fonética (que normaliza e percorre a
// palavra), e procurava os aliases da entrada varrendo TODOS os aliases do
// repositório. Com 500 termos, cada consulta custava milissegundos, e um
// ditado de 40 palavras faz uma centena de consultas (ADR-021, C2: o Smart da
// conta monta e consulta o dicionário a cada ditado do celular, e o mesmo custo
// já aparecia no PC como "resolucao lenta", limiarResolveLento).
//
// O índice guarda, por entrada, essas duas formas já calculadas, e os aliases
// agrupados pela entrada a que pertencem, com as mesmas duas formas cada um.
// É DERIVADO de entries e aliases: o resultado de Search é o mesmo de antes,
// só que sem refazer a conta. Quem grava (gravarEntrada e gravarAlias) o mantém
// junto dos mapas; Search só o lê.
//
// A chave fonética vem do PhoneticEncoder do repositório, que por contrato é
// determinístico (PhoneticEncoder.Encode): calculá-la uma vez, na gravação, dá
// o que calcular a cada consulta daria.

// entradaIndexada é uma entrada com o que Search compara dela.
type entradaIndexada struct {
	e DictionaryEntry
	// colapsada é CollapseSpaces(e.NormalizedForm).
	colapsada string
	// fonetica é a chave fonética de e.NormalizedForm.
	fonetica string
}

// aliasIndexado é um alias com o que Search compara dele.
type aliasIndexado struct {
	a DictionaryAlias
	// colapsado é CollapseSpaces(a.NormalizedAlias).
	colapsado string
	// fonetico é a chave fonética do alias: a que ele já traz (PhoneticKey), ou a
	// de a.NormalizedAlias quando não traz.
	fonetico string
}

type indiceDeBusca struct {
	// entradas está na ordem em que cada ID apareceu pela primeira vez. A ordem
	// não muda o resultado: Search ordena os candidatos por uma chave total.
	entradas []*entradaIndexada
	porID    map[string]*entradaIndexada
	// aliases agrupa os aliases pela entrada a que pertencem (EntryID), exista
	// ou não a entrada ainda: o alias gravado antes da entrada vale quando ela
	// chega.
	aliases map[string][]aliasIndexado
}

func novoIndiceDeBusca() indiceDeBusca {
	return indiceDeBusca{
		porID:   make(map[string]*entradaIndexada),
		aliases: make(map[string][]aliasIndexado),
	}
}

// poeEntrada põe a entrada no índice, no lugar da de mesmo ID, se houver.
func (ix *indiceDeBusca) poeEntrada(e DictionaryEntry, enc PhoneticEncoder) {
	nova := entradaIndexada{
		e:         e,
		colapsada: CollapseSpaces(e.NormalizedForm),
		fonetica:  enc.Encode(e.NormalizedForm),
	}
	if p, ok := ix.porID[e.ID]; ok {
		*p = nova
		return
	}
	p := &nova
	ix.porID[e.ID] = p
	ix.entradas = append(ix.entradas, p)
}

// poeAlias põe o alias novo no índice, tirando dele o antigo de mesmo ID (tinha
// diz se havia), que pode ser de outra entrada.
func (ix *indiceDeBusca) poeAlias(antigo DictionaryAlias, tinha bool, novo DictionaryAlias, enc PhoneticEncoder) {
	if tinha {
		lista := ix.aliases[antigo.EntryID]
		for i := range lista {
			if lista[i].a.ID == antigo.ID {
				lista = append(lista[:i], lista[i+1:]...)
				break
			}
		}
		if len(lista) == 0 {
			delete(ix.aliases, antigo.EntryID)
		} else {
			ix.aliases[antigo.EntryID] = lista
		}
	}
	fonetico := ""
	if novo.PhoneticKey != nil {
		fonetico = *novo.PhoneticKey
	} else {
		fonetico = enc.Encode(novo.NormalizedAlias)
	}
	ix.aliases[novo.EntryID] = append(ix.aliases[novo.EntryID], aliasIndexado{
		a:         novo,
		colapsado: CollapseSpaces(novo.NormalizedAlias),
		fonetico:  fonetico,
	})
}
