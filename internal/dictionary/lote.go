package dictionary

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Este arquivo é o PC que recebe o aprendido da nuvem (ADR-019, "Decisões da
// F8", F8a): o par chega sozinho, sem clique aqui (D2), e passa pelas mesmas
// checagens do Aprender. É puro sobre a Semente (AplicarPares) e, sobre o
// arquivo, uma leitura e uma gravação por lote (Arquivo.AplicarPares). Quem
// decide QUAIS pares do pull entram (o eco, o par mudado aqui, o marcador na
// fila) é o sync, com o espelho do SQLite (F8f); quem troca o dicionário e o
// prompt do Agent é o Agent (F8g).
//
// # As regras do PC que recebe, na ordem (a primeira que casa decide)
//
// Onde o par age na origem: sem domínio, em todo lugar; com o domínio X sem
// restrição, no domínio desconhecido e em X; restrito a X, só em X. E o PC que
// recebe NUNCA alarga: a troca nunca passa a valer em mais lugares do que na
// origem.
//
//	o par           o termo neste PC                       grava                 situação
//	X               qualquer, e X fora do mapa dominios    nada                  não aplicado (dominio_ausente)
//	qualquer        existe, restrito sem domínio           nada                  não aplicado (nao_agiria)
//	sem domínio     não existe                             o termo novo (D2)     aplicado
//	sem domínio     existe                                 a variante em alias   aplicado
//	X, sem restr.   com o domínio X (restrito ou não)      a variante em alias   aplicado
//	X, restrito     com o domínio X, restrito              a variante em alias   aplicado
//	X, restrito     com o domínio X, sem restrição         em alias_restrito     aplicado
//	X               não existe                             nada                  não aplicado (palavra_ausente_com_dominio)
//	X               sem domínio, ou com outro domínio      nada                  não aplicado (restricao_nao_cabe)
//
// O mapa dominios vem primeiro: o termo local com o domínio X, sem X no mapa,
// passaria na linha "com o domínio X" e gravaria uma troca presa a um domínio
// que este PC não conhece. Depois da tabela, só nas linhas que gravam: as
// integridades do Aprender (aprenderEm, com o termo desligado), a palavra
// comum (learning.GrafiaArriscada, como defesa) e o "agiria" (Arquivo.
// ValidarPar), conferido sobre a semente ACUMULADA do lote.
//
// # Os casos de borda
//
//   - o mesmo par já vale aqui, de origem "painel": convergente, nada é
//     gravado. É convergente, ANTES da tabela, quando a variante daqui sobe
//     exatamente como o par chega (o mesmo termo, a mesma restrição efetiva e
//     o mesmo domínio): o domínio fora do mapa, que o Aprender do painel não
//     confere, e o alias_restrito de uma entrada restrita, que a tabela poria
//     em alias, não viram troca. E é convergente, pela tabela, quando ela
//     daria o mesmo termo e a mesma lista (a adaptação deste PC: o par sem
//     domínio no termo daqui com domínio, o par sem restrição no termo daqui
//     restrito);
//   - a variante está no termo, posta à mão (sem a origem): "já vale aqui",
//     nada é gravado, nada é adotado, e o desfazer que chega não a tira;
//   - a variante é de outro termo aqui, sem a origem: não aplicado;
//   - o par de origem "painel" que vale aqui e a nuvem mudou (outro termo, ou
//     outra restrição): sai e entra de novo pelas regras (a troca); se o novo
//     não vale aqui, o de antes sai mesmo assim, e o par fica "não aplicado"
//     (a restrição nunca se perde);
//   - a variante com a origem em mais de um lugar: o PC não sabe qual é o
//     par, não toca o arquivo, e o par fica "não aplicado".
//
// # Nada de texto em erro
//
// Os resultados levam só o id, a situação e o motivo, de listas fechadas, e a
// projeção (o que já está no arquivo). Nenhum erro daqui traz a variante ou o
// termo.

// ParDaNuvem é o par como a nuvem o guarda e o manda no pull (o payload do
// learned, e o state do conflict).
type ParDaNuvem struct {
	Variante string
	Termo    string
	Restrito bool
	// Dominio é o nome do domínio; "" é o null do fio.
	Dominio string
	// Desfeito: o par tem undone_at na nuvem.
	Desfeito bool
}

// SituacaoDoPar é o que o lote fez com um par. Lista fechada.
type SituacaoDoPar string

const (
	// SituacaoAplicado: o par passou a valer aqui (criado pela D2,
	// acrescentado a um termo, ou trocado).
	SituacaoAplicado SituacaoDoPar = "aplicado"
	// SituacaoConvergente: o par já valia aqui, de origem "painel", igual ao
	// que chegou (a mesma projeção) ou no lugar que a tabela daria. Nada foi
	// gravado.
	SituacaoConvergente SituacaoDoPar = "convergente"
	// SituacaoJaValeAqui: a variante está no termo, posta à mão. Nada foi
	// gravado, e nada foi adotado.
	SituacaoJaValeAqui SituacaoDoPar = "ja_vale_aqui"
	// SituacaoDesfeito: o par desfeito na nuvem saiu daqui.
	SituacaoDesfeito SituacaoDoPar = "desfeito"
	// SituacaoAusente: o par desfeito na nuvem não tem o que tirar aqui (a
	// variante não está no arquivo com a origem "painel", nem no termo à mão).
	SituacaoAusente SituacaoDoPar = "ausente"
	// SituacaoNaoAplicado: o par não vale aqui, e o Motivo diz por quê.
	SituacaoNaoAplicado SituacaoDoPar = "nao_aplicado"
)

// MotivoDoPar é por que o par não vale neste PC. Lista fechada (ADR-019, "Os
// motivos"); a tela tem uma frase curta para cada um.
type MotivoDoPar string

const (
	// MotivoDominioAusente: o par é preso a um domínio que não está no mapa
	// dominios deste PC.
	MotivoDominioAusente MotivoDoPar = "dominio_ausente"
	// MotivoRestricaoNaoCabe: o termo daqui não tem o domínio do par (não tem
	// domínio, ou tem outro), e a troca valeria em outro lugar, ou em mais
	// lugares, que na origem.
	MotivoRestricaoNaoCabe MotivoDoPar = "restricao_nao_cabe"
	// MotivoPalavraAusenteComDominio: a palavra não existe aqui e a troca é
	// presa a um domínio (a D2 só cria a palavra sem domínio).
	MotivoPalavraAusenteComDominio MotivoDoPar = "palavra_ausente_com_dominio"
	// MotivoVarianteDeOutroTermo: a variante já é de outro termo aqui (ou é
	// outro termo); o termo que seria criado já é variante de outro; ou a
	// variante está com a origem em mais de um lugar.
	MotivoVarianteDeOutroTermo MotivoDoPar = "variante_de_outro_termo"
	// MotivoTermoDesligado: o termo daqui está inativo.
	MotivoTermoDesligado MotivoDoPar = "termo_desligado"
	// MotivoNaoAgiria: o dicionário daqui não aplicaria a variante a um
	// ditado: o termo daqui é restrito sem domínio, o Smart não a escreve
	// (ErrNaoAgiria), ou o par não passaria no Aprender (variante igual ao
	// termo, grande demais, de mais de quatro palavras).
	MotivoNaoAgiria MotivoDoPar = "nao_agiria"
	// MotivoPalavraComum: a variante é comum demais para virar troca
	// (learning.GrafiaArriscada).
	MotivoPalavraComum MotivoDoPar = "palavra_comum"
	// MotivoArquivoIndisponivel: o dicionario.json está ausente, inválido, é
	// de outro usuário ou estava preso na troca de nome (ArquivoIndisponivel).
	// Quem o usa é o sync: o lote não chega a rodar.
	MotivoArquivoIndisponivel MotivoDoPar = "arquivo_indisponivel"
)

// MotivosDoPar é a lista fechada dos motivos, na ordem da ADR.
func MotivosDoPar() []MotivoDoPar {
	return []MotivoDoPar{
		MotivoDominioAusente,
		MotivoRestricaoNaoCabe,
		MotivoPalavraAusenteComDominio,
		MotivoVarianteDeOutroTermo,
		MotivoTermoDesligado,
		MotivoNaoAgiria,
		MotivoPalavraComum,
		MotivoArquivoIndisponivel,
	}
}

// ResultadoDoPar é o que o lote fez com um par.
type ResultadoDoPar struct {
	// ID é IDDoPar da variante que chegou.
	ID       string
	Situacao SituacaoDoPar
	// Motivo, só no SituacaoNaoAplicado.
	Motivo MotivoDoPar
	// Projecao é a linha deste id na projeção do arquivo DEPOIS do lote (a
	// "projeção esperada" do espelho, já com a adaptação deste PC: a grafia do
	// termo daqui, alias ou alias_restrito); nil quando o id não está nela.
	Projecao *ParDoPainel
}

// Lote é o que AplicarPares fez.
type Lote struct {
	// Resultados, um por par pedido, na ordem do pedido.
	Resultados []ResultadoDoPar
	// Mudou: a semente de depois é outra (o arquivo seria escrito com outros
	// bytes). Arquivo.AplicarPares grava uma vez só, e só com Mudou.
	Mudou bool
}

// ConferenciasDoLote são as conferências que o dicionário sozinho não sabe
// fazer. As duas são opcionais: nil não confere.
type ConferenciasDoLote struct {
	// PalavraComum é a de Arquivo.PalavraComum, chamada com grafia verdadeiro
	// para a variante: o motivo, ou "".
	PalavraComum func(texto string, grafia bool) string
	// Agiria é a de Arquivo.ValidarPar: nil se o par age na semente dada,
	// ErrNaoAgiria (errors.Is) se não. Qualquer outro erro interrompe o lote,
	// e AplicarPares o devolve.
	Agiria func(Semente, ParDoPainel) error
}

// AplicarPares aplica o lote à semente, par a par, na ordem, cada um sobre o
// que os anteriores deixaram (a semente acumulada). Não muda s, e não escreve
// em disco.
//
// Um par pode fazer outro parar de agir (o veto: "pedro das" deixa de virar
// troca quando o lote cria o termo DAS). Na ordem do lote, o de depois é
// conferido sobre a semente que já tem o de antes, e não entra. Na ordem
// contrária, uma conferência final, sobre a semente do fim do lote, tira o que
// deixou de agir (o par vetado fica "não aplicado", nao_agiria) e confere de
// novo, até nada mais sair: nenhum par aplicado pelo lote fica sem agir.
func (s Semente) AplicarPares(pares []ParDaNuvem, c ConferenciasDoLote) (Semente, Lote, error) {
	atual := s
	res := make([]ResultadoDoPar, len(pares))
	// aplicados: o índice (em pares) do último par de cada id que este lote
	// aplicou. Um par seguinte do mesmo id decide de novo: o convergente
	// mantém o que o lote aplicou, e qualquer outro o substitui.
	aplicados := map[string]int{}
	var ordem []string
	for i, p := range pares {
		novo, r, err := atual.aplicarUm(p, c)
		if err != nil {
			return s, Lote{}, err
		}
		atual, res[i] = novo, r
		switch r.Situacao {
		case SituacaoAplicado:
			aplicados[r.ID] = i
			ordem = append(ordem, r.ID)
		case SituacaoConvergente:
		default:
			delete(aplicados, r.ID)
		}
	}

	if c.Agiria != nil {
		for {
			vetado, err := atual.primeiroQueNaoAge(ordem, aplicados, c.Agiria)
			if err != nil {
				return s, Lote{}, err
			}
			if vetado == "" {
				break
			}
			i := aplicados[vetado]
			proj := atual.projecaoDe(vetado)
			if proj == nil {
				// Não acontece: primeiroQueNaoAge só devolve id projetado.
				return s, Lote{}, errors.New("dictionary: o par vetado não está na projeção")
			}
			sem, _, err := atual.DesfazerVariante(proj.Variante)
			if err != nil {
				return s, Lote{}, err
			}
			atual = sem
			// O par aplicado e os convergentes do mesmo id depois dele.
			for j := i; j < len(res); j++ {
				if res[j].ID == vetado {
					res[j] = ResultadoDoPar{ID: vetado, Situacao: SituacaoNaoAplicado, Motivo: MotivoNaoAgiria}
				}
			}
			delete(aplicados, vetado)
		}
	}

	projecao := atual.projecaoPorID()
	for i := range res {
		if p, ok := projecao[res[i].ID]; ok {
			res[i].Projecao = &p
		}
	}
	mudou, err := sementesDiferentes(s, atual)
	if err != nil {
		return s, Lote{}, err
	}
	return atual, Lote{Resultados: res, Mudou: mudou}, nil
}

// primeiroQueNaoAge é o primeiro par aplicado pelo lote (na ordem em que foi
// aplicado) que não age na semente s; "" quando todos agem.
func (s Semente) primeiroQueNaoAge(ordem []string, aplicados map[string]int, agiria func(Semente, ParDoPainel) error) (string, error) {
	projecao := s.projecaoPorID()
	vistos := map[string]bool{}
	for _, id := range ordem {
		if _, ok := aplicados[id]; !ok || vistos[id] {
			continue
		}
		vistos[id] = true
		proj, ok := projecao[id]
		if !ok {
			// Um par aplicado sempre está na projeção (aplicarUm confere).
			return "", errors.New("dictionary: o par aplicado saiu da projeção")
		}
		if err := agiria(s, proj); err != nil {
			if errors.Is(err, ErrNaoAgiria) {
				return id, nil
			}
			return "", err
		}
	}
	return "", nil
}

// projecaoDe é a linha do id na projeção de s, ou nil.
func (s Semente) projecaoDe(id string) *ParDoPainel {
	if p, ok := s.projecaoPorID()[id]; ok {
		return &p
	}
	return nil
}

// projecaoPorID é a projeção de s, pelo id.
func (s Semente) projecaoPorID() map[string]ParDoPainel {
	out := map[string]ParDoPainel{}
	for _, p := range s.ParesDoPainel() {
		out[p.ID] = p
	}
	return out
}

// aplicarUm decide um par sobre s. Devolve a semente de depois (s, quando nada
// muda) e o resultado, sem a projeção (AplicarPares a preenche no fim).
func (s Semente) aplicarUm(p ParDaNuvem, c ConferenciasDoLote) (Semente, ResultadoDoPar, error) {
	r := ResultadoDoPar{ID: IDDoPar(p.Variante)}
	chave := ChaveDoPar(p.Variante)
	doPainel, aMao := s.ocorrencias(chave)
	noTermo := false
	for _, o := range aMao {
		if mesmaCoisa(s.Entradas[o.entrada].Canonica, p.Termo) {
			noTermo = true
		}
	}

	if chave == "" {
		// Sem letra nem dígito não há variante: nada aqui é dela.
		return s, naoAplicado(r, MotivoNaoAgiria, p.Desfeito), nil
	}
	if len(doPainel) > 1 {
		// Com a origem em mais de um lugar, o PC não sabe qual é o par.
		return s, naoAplicado(r, MotivoVarianteDeOutroTermo, false), nil
	}

	if p.Desfeito {
		switch {
		case len(doPainel) == 1:
			sem, _, err := s.DesfazerVariante(doPainel[0].texto)
			if err != nil {
				return s, ResultadoDoPar{}, err
			}
			r.Situacao = SituacaoDesfeito
			return sem, r, nil
		case noTermo:
			// O desfazer remoto nunca tira o que a pessoa pôs.
			r.Situacao = SituacaoJaValeAqui
		default:
			r.Situacao = SituacaoAusente
		}
		return s, r, nil
	}

	// O par que a nuvem recusaria não vale aqui, e o arquivo não é tocado (a
	// nuvem confere antes do banco; isto é defesa). Sem isto, o restrito sem
	// domínio passaria pela linha "sem domínio" e valeria em todo lugar.
	if ConferirPar(p.Variante, p.Termo, p.Restrito, dominioDoPar(p.Dominio)) != nil {
		return s, naoAplicado(r, MotivoNaoAgiria, false), nil
	}

	base := s
	if len(doPainel) == 1 {
		o := doPainel[0]
		if s.projetaIgual(o, p) {
			// O par que vale aqui sobe exatamente como chega: convergente
			// antes da tabela. A tabela é para o par novo; aqui ela daria
			// dominio_ausente ao domínio fora do mapa (que o Aprender do
			// painel não confere) e pediria alias para o alias_restrito de
			// uma entrada restrita, e a troca tiraria do arquivo o que este
			// PC aprendeu e a nuvem manda igual.
			r.Situacao = SituacaoConvergente
			return s, r, nil
		}
		alvo, emRestrito, motivo := s.lugarDoPar(p)
		if motivo == "" && alvo == o.entrada && emRestrito == o.emRestrito {
			r.Situacao = SituacaoConvergente
			return s, r, nil
		}
		// A troca: o de antes sai, e o novo entra pelas regras. Se o novo não
		// vale aqui, o de antes não volta.
		sem, _, err := s.DesfazerVariante(o.texto)
		if err != nil {
			return s, ResultadoDoPar{}, err
		}
		base = sem
	} else if noTermo {
		r.Situacao = SituacaoJaValeAqui
		return s, r, nil
	}

	novo, motivo, jaVale, err := base.aprenderDaNuvem(p, c)
	switch {
	case err != nil:
		return s, ResultadoDoPar{}, err
	case jaVale:
		r.Situacao = SituacaoJaValeAqui
		return base, r, nil
	case motivo != "":
		return base, naoAplicado(r, motivo, false), nil
	}
	r.Situacao = SituacaoAplicado
	return novo, r, nil
}

// projetaIgual diz se a ocorrência o, de origem "painel", sobe exatamente como
// o par p: ela está na projeção (age em algum lugar e a nuvem a aceitaria), com
// o mesmo termo (como o Search compara), a mesma restrição efetiva (a entrada
// restrita, ou a variante em alias_restrito) e o mesmo domínio.
func (s Semente) projetaIgual(o ocorrencia, p ParDaNuvem) bool {
	proj, ok := projetar(s.Entradas[o.entrada], o.texto, o.emRestrito)
	return ok && mesmaCoisa(proj.Termo, p.Termo) && proj.Restrito == p.Restrito && proj.Dominio == p.Dominio
}

// naoAplicado é r como "não aplicado", com o motivo. Para o desfazer que não
// tem o que tirar (desfeito verdadeiro), é "ausente".
func naoAplicado(r ResultadoDoPar, m MotivoDoPar, desfeito bool) ResultadoDoPar {
	if desfeito {
		r.Situacao = SituacaoAusente
		return r
	}
	r.Situacao, r.Motivo = SituacaoNaoAplicado, m
	return r
}

// aprenderDaNuvem grava o par ativo em s pela tabela e pelas checagens de
// depois dela. jaVale: a variante já está no termo, à mão. motivo não vazio: o
// par não vale aqui, e s volta como estava.
func (s Semente) aprenderDaNuvem(p ParDaNuvem, c ConferenciasDoLote) (Semente, MotivoDoPar, bool, error) {
	_, emRestrito, motivo := s.lugarDoPar(p)
	if motivo != "" {
		return s, motivo, false, nil
	}
	novo, feito, err := s.aprenderEm(p.Termo, p.Variante, emRestrito)
	switch {
	case errors.Is(err, ErrJaExiste):
		return s, "", true, nil
	case err != nil:
		return s, motivoDoErro(err), false, nil
	}
	if c.PalavraComum != nil && c.PalavraComum(feito.Variante, true) != "" {
		return s, MotivoPalavraComum, false, nil
	}
	proj := novo.projecaoDe(IDDoPar(feito.Variante))
	if proj == nil {
		// Gravado, mas fora da projeção (o termo daqui com mais de 80 runas,
		// por exemplo): nunca subiria, e a varredura o leria como desfeito.
		return s, MotivoNaoAgiria, false, nil
	}
	if c.Agiria != nil {
		if err := c.Agiria(novo, *proj); err != nil {
			if errors.Is(err, ErrNaoAgiria) {
				return s, MotivoNaoAgiria, false, nil
			}
			return s, "", false, err
		}
	}
	return novo, "", false, nil
}

// lugarDoPar é a tabela do PC que recebe: o termo daqui (alvo, -1 se não
// existe) e se a variante entra em alias_restrito, ou o motivo de o par não
// valer aqui. As linhas na ordem; a primeira que casa decide.
func (s Semente) lugarDoPar(p ParDaNuvem) (alvo int, emRestrito bool, motivo MotivoDoPar) {
	alvo = s.indiceDoTermo(p.Termo)
	if p.Dominio != "" {
		if _, ok := s.Dominios[p.Dominio]; !ok {
			return alvo, false, MotivoDominioAusente
		}
	}
	if alvo >= 0 {
		if e := s.Entradas[alvo]; e.Restrito && semDominio(e) {
			// O Search só mostra a entrada restrita no domínio dela, e ela
			// não tem: o termo daqui nunca age, e a variante também não.
			return alvo, false, MotivoNaoAgiria
		}
	}
	if p.Dominio == "" {
		return alvo, false, ""
	}
	if alvo < 0 {
		return alvo, false, MotivoPalavraAusenteComDominio
	}
	e := s.Entradas[alvo]
	if semDominio(e) || e.Dominio != p.Dominio {
		return alvo, false, MotivoRestricaoNaoCabe
	}
	return alvo, p.Restrito && !e.Restrito, ""
}

// motivoDoErro traduz a recusa de aprenderEm no motivo da lista fechada.
func motivoDoErro(err error) MotivoDoPar {
	switch {
	case errors.Is(err, ErrVarianteDeOutroTermo), errors.Is(err, ErrTermoEVarianteDeOutro):
		return MotivoVarianteDeOutroTermo
	case errors.Is(err, ErrTermoInativo):
		return MotivoTermoDesligado
	default:
		// ErrVazio, ErrInvalido, ErrSemNovidade: o Aprender recusaria, e a
		// variante não agiria como troca.
		return MotivoNaoAgiria
	}
}

// ocorrencia é um lugar do arquivo onde está uma variante.
type ocorrencia struct {
	entrada    int
	emRestrito bool
	texto      string
}

// ocorrencias são os lugares onde há uma variante com a chave dada: os de
// origem "painel" e os outros (à mão, dictlearn, "Adicionar termo").
func (s Semente) ocorrencias(chave string) (doPainel, outras []ocorrencia) {
	for i, e := range s.Entradas {
		for _, emRestrito := range []bool{false, true} {
			lista := e.Alias
			if emRestrito {
				lista = e.AliasRestrito
			}
			for _, a := range lista {
				if ChaveDoPar(a) != chave {
					continue
				}
				o := ocorrencia{entrada: i, emRestrito: emRestrito, texto: a}
				if s.AliasOrigem[a] == OrigemPainel {
					doPainel = append(doPainel, o)
				} else {
					outras = append(outras, o)
				}
			}
		}
	}
	return doPainel, outras
}

// sementesDiferentes diz se a e b seriam escritas com outros bytes. A
// serialização é a de Gravar (sem o recuo, que não muda o conteúdo): nil e
// vazio se escrevem igual, e o mapa sai em ordem.
func sementesDiferentes(a, b Semente) (bool, error) {
	ja, err := json.Marshal(a)
	if err != nil {
		return false, err
	}
	jb, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(ja, jb), nil
}

// AplicarPares aplica um lote do pull ao arquivo: UMA leitura, o lote
// (Semente.AplicarPares, com a.PalavraComum e a.ValidarPar), o Validar e UMA
// gravação, só se algo mudou. Devolve a semente que está no disco depois (a
// gravada, ou a lida), para quem chama montar o dicionário em memória.
//
// escolher recebe o arquivo relido e devolve os pares do lote: é onde o sync
// compara a projeção de cada id com a do espelho e pula o par mudado aqui que
// não subiu (o aplicador nunca sobrescreve uma mudança daqui). antesDeGravar,
// se não nil, recebe o lote DEPOIS da conferência e ANTES da gravação, só
// quando há o que gravar: é onde o espelho grava a intenção (write-ahead). Um
// erro dele cancela a gravação e volta como está.
//
// O arquivo que não é nosso (ausente, inválido, de outro usuário) nunca é
// tocado: os erros são os de Aprender (ArquivoIndisponivel diz quais deixam o
// par "arquivo indisponível").
func (a *Arquivo) AplicarPares(escolher func(atual Semente) []ParDaNuvem, antesDeGravar func(Lote) error) (Semente, Lote, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	atual, err := a.ler()
	if err != nil {
		return Semente{}, Lote{}, err
	}
	var pares []ParDaNuvem
	if escolher != nil {
		pares = escolher(atual)
	}
	novo, lote, err := atual.AplicarPares(pares, ConferenciasDoLote{PalavraComum: a.PalavraComum, Agiria: a.ValidarPar})
	if err != nil {
		return Semente{}, Lote{}, &erroNaoCarrega{causa: err}
	}
	if !lote.Mudou {
		return atual, lote, nil
	}
	if a.Validar != nil {
		if err := a.Validar(novo); err != nil {
			return Semente{}, Lote{}, &erroNaoCarrega{causa: err}
		}
	}
	if antesDeGravar != nil {
		if err := antesDeGravar(lote); err != nil {
			return Semente{}, Lote{}, err
		}
	}
	if err := novo.Gravar(a.Caminho); err != nil {
		return Semente{}, Lote{}, err
	}
	return novo, lote, nil
}

// ArquivoIndisponivel diz se err, de uma operação do Arquivo, é o do arquivo
// que o lote não alcança: ausente (ErrSemArquivo), inválido
// (ErrArquivoInvalido), de outro usuário (ErrUsuarioDiferente) ou preso por
// outro processo na troca de nome (destinoPreso). O par fica pendente com
// MotivoArquivoIndisponivel, e nunca vira "tudo desfeito".
func ArquivoIndisponivel(err error) bool {
	return errors.Is(err, ErrSemArquivo) ||
		errors.Is(err, ErrArquivoInvalido) ||
		errors.Is(err, ErrUsuarioDiferente) ||
		destinoPreso(err)
}
