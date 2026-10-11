package syncengine

import (
	"context"
	"sort"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A varredura do aprendido (ADR-019, "Decisões da F8", F8f3): o
// dicionario.json diante do espelho, no começo de cada rodada, depois do
// Preparar e antes do CompletarEnvio (que precisa do M dela) e do envio. É o
// detector único das mudanças daqui: não há vigia do arquivo, e o dictlearn
// roda em outro processo. O Aprender e o Desfazer do painel acordam o laço e
// sobem a versão local no ato (storage.Fila.SubirVersaoLocal, o gancho da
// F8g).
//
// # Em três passos, sob a trava do dicionário
//
// O arquivo é lido UMA vez, sob a trava do dicionário (a mesma do painel),
// pelo lote do aplicador (DicionarioDoAprendido.AplicarPares): o painel não
// grava no meio da varredura.
//
//  1. A tabela (DecidirVarredura): cada id do espelho e da projeção do
//     arquivo, comparado com o que o espelho guarda, numa transação só (as
//     projeções novas e os marcadores juntos). A ação que não muda nada não
//     é gravada: a varredura ociosa não escreve no SQLite. Com o freio, nada
//     é gravado.
//  2. Os pendentes (o par da nuvem guardado que ainda não vale aqui:
//     storage.EstadoDoAprendido e TemPendente) que a tabela não tocou nesta
//     varredura vão ao lote do aplicador, com o arquivo relido: o que agora
//     vale aqui entra (a intenção antes do arquivo, IntencaoDoPar), e o que
//     não vale continua como estava, sem gravar nada. A F8f3 não guarda uma
//     marca do arquivo para saber se ele "mudou ou voltou": tenta de novo a
//     cada varredura, e só grava o que muda.
//  3. O espelho dos pendentes (DecidirPar), numa transação com a conferência
//     do epoch e do pareamento (storage.Fila.AplicarComAprendido); antes de o
//     arquivo ser tocado, a mesma conferência numa leitura curta
//     (storage.Fila.ConferirPareamento). Mudou: o arquivo não é tocado, e a
//     linha "aplicando" que sobrar a varredura seguinte reconcilia.
//
// # O que nunca acontece
//
//   - O arquivo indisponível (ausente, inválido, de outro usuário, preso na
//     troca de nome) nunca vira delete: é "sem informação", e a varredura não
//     grava nada.
//   - Antes do aceite do "Enviar o histórico" (D3), nenhum upsert nasce, nem o
//     de um par que a nuvem tem: ele é contado (o M da pergunta), e a
//     projeção nova fica guardada com a versão local acima da base, para o
//     clique soltá-lo. Antes do clique sai só o delete de um id que a nuvem
//     tem (escolha menor 5).
//   - O par que saiu do arquivo sem versão da nuvem (nem a da base, nem a
//     pendente) e sem envio em voo não gera envio: a mudança é assentada (a
//     versão coberta passa a ser a local).
//   - A adaptação deste PC (a grafia do termo daqui, o alias_restrito) nunca
//     volta como upsert: quem a grava é o aplicador, que guarda a projeção
//     que escreveu, e o "veio da nuvem" segura o par sem versão.
//   - Uma recusa da nuvem gera uma tentativa por mudança: a linha "recusado"
//     com a projeção igual à recusada não é marcada de novo.

// DicionarioDoAprendido é o dicionario.json deste PC como o sync o usa: o
// lote do aplicador, com o arquivo relido sob a trava do dicionário
// (dictionary.Arquivo.AplicarPares, que o satisfaz), na varredura e no
// Receber (receber.go, F8f4). escolher recebe o
// arquivo relido e devolve os pares do lote; antesDeGravar, a intenção
// (write-ahead), só quando há o que gravar. No Agent (F8g), quem o cumpre
// também troca o dicionário em memória e o prompt depois de gravar. Os erros
// do arquivo indisponível são os de dictionary.ArquivoIndisponivel.
//
// Um erro pode voltar com o arquivo já gravado (a troca em memória que falha
// depois da gravação): o sync o trata como o lote que não terminou. A linha
// "aplicando" que a intenção deixou fica, e a varredura seguinte a confirma
// pelo arquivo; o par da nuvem guardado continua pendente e anda a base na
// tentativa de novo dele, mesmo com o cursor já andado.
type DicionarioDoAprendido interface {
	AplicarPares(escolher func(atual dictionary.Semente) []dictionary.ParDaNuvem,
		antesDeGravar func(dictionary.Lote) error) (dictionary.Semente, dictionary.Lote, error)
}

// LimiteDoFreio é o freio contra a remoção em massa (ADR-019, "Decisões da
// F8", escolha menor 1): uma varredura que acha MAIS do que isto de pares
// saindo do arquivo sem o Desfazer do painel (a versão local não subiu no
// ato) não grava nada, e o painel avisa (ResultadoDaVarredura.Freio). O
// Desfazer do painel não conta: ele sobe a versão local no ato, e pode
// acumular com o PC desconectado. Dez é bem acima do uso normal (o clique
// acorda o laço, e a varredura vê um par por vez) e bem abaixo de um
// dicionario.json antigo restaurado por cima.
const LimiteDoFreio = 10

// PrazoDaEtiqueta é quanto vivem as etiquetas "desfeito em outro aparelho" e
// "trocado em outro aparelho" (escolha menor 2), contados de quando a
// situação começou (storage.EstadoDoAprendido.SituacaoEm). Depois, a etiqueta
// sai, e o texto dela também (fica o id).
const PrazoDaEtiqueta = 30 * 24 * time.Hour

// Os tipos das decisões da varredura, para as contagens do log (lista
// fechada).
const (
	// TipoVarreduraUpsert: o marcador do upsert (o par novo, ou mudado aqui),
	// ou a projeção nova num marcador que espera.
	TipoVarreduraUpsert = "upsert"
	// TipoVarreduraDelete: o marcador do delete (o par saiu daqui, e a nuvem
	// o tem).
	TipoVarreduraDelete = "delete"
	// TipoVarreduraSegurado: o upsert que o clique solta (antes do aceite);
	// é o M.
	TipoVarreduraSegurado = "segurado"
	// TipoVarreduraAssentado: o par saiu daqui, e a nuvem nunca o teve: sem
	// envio.
	TipoVarreduraAssentado = "assentado"
	// TipoVarreduraEspera: o par saiu daqui de volta à mesma conta depois de
	// um esquecer, e a nuvem dela o tinha: o delete espera a versão que o
	// pull traz (storage.EstadoDoAprendido.MesmaContaEsquecida).
	TipoVarreduraEspera = "espera"
	// TipoVarreduraAdotado: a variante perdeu só a origem "painel" e
	// continua no termo: "já vale aqui", nada sobe.
	TipoVarreduraAdotado = "adotado"
	// TipoVarreduraConfirmado: a linha "aplicando" com o arquivo igual à
	// projeção esperada: confirmada, sem envio.
	TipoVarreduraConfirmado = "confirmado"
	// TipoVarreduraDeVolta: a linha "aplicando" com o arquivo que não chegou
	// a ser gravado (ou mudado aqui depois): a intenção sai.
	TipoVarreduraDeVolta = "de_volta"
	// TipoVarreduraEmVoo: a mudança vista com o envio em voo: só a versão
	// local sobe (a projeção é a do envio).
	TipoVarreduraEmVoo = "em_voo"
	// TipoVarreduraEtiqueta: a etiqueta passou do prazo, e saiu com o texto.
	TipoVarreduraEtiqueta = "etiqueta_vencida"
)

// VistaDoArquivo é o dicionario.json diante de um id do aprendido.
type VistaDoArquivo struct {
	// Projecao é a linha do id na projeção do arquivo
	// (dictionary.Semente.ParesDoPainel); nil fora dela.
	Projecao *dictionary.ParDoPainel
	// ForaDaProjecao: fora da projeção, a variante continua no arquivo com a
	// origem "painel" (repetida, numa entrada restrita sem domínio, ou o que a
	// nuvem recusaria). Não é desfazer: nada sobe.
	ForaDaProjecao bool
	// Adotada: fora da projeção, a variante continua, sem a origem, no termo
	// da projeção guardada. Não é desfazer: a linha passa a "já vale aqui".
	Adotada bool
}

// DecisaoDaVarredura é o que a varredura faz com um id.
type DecisaoDaVarredura struct {
	Acao storage.AcaoDoAprendido
	// Tipo é TipoVarredura*, ou "" quando nada muda.
	Tipo string
	// Segura: o id tem um upsert que o clique solta (antes do aceite). É o
	// que o M conta.
	Segura bool
	// Sai: o par saiu do arquivo sem o Desfazer do painel (a versão local
	// não subiu no ato). É o que o freio conta.
	Sai bool
}

// DecidirVarredura é a tabela da varredura (ADR-019, "No PC, a varredura")
// para um id: o estado do espelho, o arquivo diante dele, se o "Enviar o
// histórico" foi aceito e a hora (a das etiquetas). Pura. As linhas, na ordem
// (a primeira que casa decide):
//
//	o arquivo diante do espelho                               o que a varredura faz
//	o envio em voo e o arquivo diferente da guardada            só a versão local sobe (uma vez)
//	"aplicando", o arquivo igual à esperada                     confirma, sem envio
//	"aplicando", o arquivo igual à guardada                     a intenção sai; o par volta a pendente
//	"aplicando", o arquivo diferente das duas                   a intenção sai, e segue como mudado aqui
//	"recusado", e a projeção igual à recusada                   nada
//	igual à guardada, sem mudança daqui, a nuvem tem o par      nada (a etiqueta vencida sai)
//	par novo, ou mudado aqui (outra projeção, ou a versão       upsert; antes do aceite, conta (M)
//	  local acima da base), ou presente sem a nuvem ter o
//	  par e fora do "veio da nuvem"
//	fora da projeção, com a origem "painel" no arquivo          nada (o par está lá, e não sobe)
//	a variante perdeu só a origem e continua no termo           adotada: "já vale aqui", nada sobe
//	o par saiu do arquivo                                       delete, se a nuvem tem o par; de volta
//	                                                              à mesma conta depois de um esquecer,
//	                                                              espera a versão do pull; senão,
//	                                                              assenta, sem envio
//	saiu antes, sem envio, e a nuvem passou a ter o par         delete (o "desfeito aqui" de volta à
//	                                                              mesma conta, quando o pull traz a versão)
//
// O arquivo indisponível nem chega aqui (Varrer não decide nada sem ele). A
// etiqueta some quando a pessoa aprende o par de novo (a linha do upsert) e
// quando passa do PrazoDaEtiqueta; o texto dela sai junto, menos quando ele
// ainda é o par da nuvem pendente (o state de um conflict que não valeu
// aqui), que fica até valer.
func DecidirVarredura(e storage.EstadoDoAprendido, v VistaDoArquivo, aceito bool, agora time.Time) DecisaoDaVarredura {
	var d DecisaoDaVarredura
	a := &d.Acao
	arq := v.Projecao
	saiu := arq == nil && !v.ForaDaProjecao && !v.Adotada

	if m := e.Marcador; m != nil && m.Congelado() {
		// A projeção guardada é a do envio, e não muda no voo, nem no envio a
		// repetir depois de uma reconexão (storage.ErrProjecaoEmVoo): só a
		// versão local sobe, uma vez, para a resposta saber que mudou
		// (DecidirResultadoDoAprendido). A adoção e o par que não sobe
		// esperam a resposta.
		mudou := arq != nil && !mesmaProjecao(arq, e.Projecao) || saiu && e.Projecao != nil
		d.Sai = saiu && e.Projecao != nil && !e.MudouAqui()
		if mudou && e.VersaoLocal == m.VersaoEnviada {
			a.MudouAqui = true
			d.Tipo = TipoVarreduraEmVoo
		}
		return d
	}

	if e.Situacao == storage.EspelhoAplicando {
		// A intenção do aplicador (write-ahead), reconciliada pelo arquivo.
		switch {
		case mesmaProjecao(arq, e.Esperada):
			// O lote gravou, e a página dele (ou a transação dos pendentes)
			// não terminou, ou caiu no meio: confirma, sem envio. A base não
			// é gravada aqui: o par da nuvem guardado, com a versão, fica
			// pendente (TemPendente), e a tentativa de novo dele
			// (PlanejarPendente, na varredura seguinte) converge e anda a
			// base, como o pull da mesma versão faria. O cursor pode já ter
			// andado (a intenção da tentativa de novo de um pendente; o lote
			// que gravou e devolveu erro), e o pull não o traria de novo. A
			// versão guardada é desta conta e deste epoch: o esquecer apaga o
			// par da intenção, e o recomeço a versão dele. Sem a versão (o
			// recomeço), o texto sai. Se a página nova guardou um par mais
			// novo (DecidirPar), é ele que fica pendente.
			if !mesmaProjecao(e.Projecao, e.Esperada) {
				a.Projecao = &storage.NovaProjecao{Par: e.Esperada}
			}
			s := storage.EspelhoNenhuma
			if e.Esperada != nil {
				s = storage.EspelhoAplicado
			}
			a.Situacao = &storage.NovaSituacao{Situacao: s}
			if e.Nuvem != nil && e.Nuvem.Versao == nil {
				a.LimparNuvem = true
				e.Nuvem = nil
			}
			e.Projecao = e.Esperada
			e.Situacao = s
			d.Tipo = TipoVarreduraConfirmado
		default:
			// O arquivo não chegou a ser gravado (igual à guardada): o par
			// volta a pendente (o par da nuvem guardado fica). Diferente das
			// duas: além disso, é mudança daqui (abaixo).
			a.Situacao = &storage.NovaSituacao{}
			e.Situacao = storage.EspelhoNenhuma
			d.Tipo = TipoVarreduraDeVolta
		}
		e.Esperada, e.Motivo, e.SituacaoEm = nil, "", nil
	}

	if e.Situacao == storage.EspelhoRecusado && (mesmaProjecao(arq, e.Projecao) || arq == nil && v.ForaDaProjecao) {
		// A projeção recusada: só uma mudança do par, um epoch novo ou o
		// esquecer liberam a linha.
		return d
	}

	if etiqueta(e.Situacao) && e.SituacaoEm != nil && !agora.Before(e.SituacaoEm.Add(PrazoDaEtiqueta)) {
		a.Situacao = &storage.NovaSituacao{}
		a.LimparNuvem = e.Nuvem != nil && !TemPendente(e)
		e.Situacao = storage.EspelhoNenhuma
		d.Tipo = TipoVarreduraEtiqueta
	}

	g := e.Projecao
	temMarcador := e.Marcador != nil
	switch {
	case arq != nil:
		mudou := !mesmaProjecao(arq, g)
		daqui := mudou || e.MudouAqui()
		semVersao := !nuvemTemOPar(e) && !e.VeioDaNuvem
		if !daqui && !semVersao {
			return d
		}
		if mudou {
			a.Projecao = &storage.NovaProjecao{Par: arq}
			a.MudouAqui = true
		}
		if daqui {
			// A pessoa aprendeu (ou trocou) o par aqui.
			switch {
			case etiqueta(e.Situacao):
				a.Situacao = &storage.NovaSituacao{}
				a.LimparNuvem = e.Nuvem != nil && !TemPendente(e)
			case e.Situacao == storage.EspelhoRecusado:
				a.Situacao = &storage.NovaSituacao{}
			}
			if e.VeioDaNuvem {
				a.VeioDaNuvem = storage.VeioSai
			}
		}
		if !aceito {
			// D3: o upsert espera o clique; a projeção nova fica guardada
			// (com a versão local acima da base), e o clique o solta.
			d.Segura = true
			d.Tipo = TipoVarreduraSegurado
			break
		}
		if !temMarcador {
			a.Marcador = storage.MarcadorCriar
		}
		// (O marcador que já espera leva a projeção nova.)
		d.Tipo = TipoVarreduraUpsert
	case v.ForaDaProjecao:
		// O par continua no arquivo, com a origem, e não sobe.
	case g != nil && v.Adotada:
		a.Projecao = &storage.NovaProjecao{}
		a.Situacao = &storage.NovaSituacao{Situacao: storage.EspelhoJaValeAqui}
		if e.MudouAqui() {
			c := e.VersaoLocal
			a.Cobre = &c
		}
		if temMarcador {
			a.Marcador = storage.MarcadorSai
		}
		d.Tipo = TipoVarreduraAdotado
	case g != nil:
		// O par saiu do arquivo: o Desfazer do painel, ou a mão.
		d.Sai = !e.MudouAqui()
		a.Projecao = &storage.NovaProjecao{}
		if e.Situacao == storage.EspelhoRecusado {
			a.Situacao = &storage.NovaSituacao{}
		}
		if nuvemTemOPar(e) {
			a.MudouAqui = true
			if !temMarcador {
				a.Marcador = storage.MarcadorCriar
			}
			d.Tipo = TipoVarreduraDelete
			break
		}
		if e.MesmaContaEsquecida {
			// De volta à mesma conta depois de um esquecer, com o Desfazer que
			// a varredura não tinha visto (o PC desconectado, ou o envio em
			// voo): a nuvem desta conta tinha o par, e o esquecer só apagou a
			// versão. Não assenta: a mudança fica acima da base, e vira delete
			// quando o pull trouxer a versão (a última linha da tabela).
			// Assentado, o pull do cursor 0 poria o par de volta no arquivo.
			a.MudouAqui = true
			d.Tipo = TipoVarreduraEspera
			break
		}
		// A nuvem nunca o teve: o marcador nem nasce (e o que esperava sai),
		// e a mudança é assentada, senão o pull de uma troca que outro
		// aparelho aprender depois a tiraria daqui.
		if e.MudouAqui() {
			c := e.VersaoLocal
			a.Cobre = &c
		}
		if temMarcador {
			a.Marcador = storage.MarcadorSai
		}
		d.Tipo = TipoVarreduraAssentado
	default:
		// Nem no arquivo nem na projeção guardada. O "desfeito aqui" que não
		// subiu (de volta à mesma conta depois de um esquecer) sai quando a
		// nuvem passa a ter o par (o pull guarda a versão como pendente).
		if e.TemLinha && e.MudouAqui() && !temMarcador && nuvemTemOPar(e) {
			a.Marcador = storage.MarcadorCriar
			d.Tipo = TipoVarreduraDelete
		}
	}
	return d
}

// nuvemTemOPar diz se o PC conhece uma versão do par na nuvem: a da base, ou
// a do par da nuvem guardado (o pendente do pull, ou o state de um conflict).
func nuvemTemOPar(e storage.EstadoDoAprendido) bool {
	return e.ServerVersion != nil || e.Nuvem != nil && e.Nuvem.Versao != nil
}

// soOQueMuda tira de a o que não muda nada no estado e (a base, a projeção, a
// situação, o par da nuvem e o "veio da nuvem" que já são esses; o marcador
// que já existe). A ação que sobra vazia não é gravada: a varredura ociosa,
// e a tentativa de novo de um pendente que continua não valendo aqui, não
// escrevem no SQLite.
func soOQueMuda(e storage.EstadoDoAprendido, a storage.AcaoDoAprendido) storage.AcaoDoAprendido {
	if b := a.Base; b != nil && e.ServerVersion != nil && *e.ServerVersion == b.ServerVersion && e.BaseDesfeita == b.Desfeita {
		a.Base = nil
	}
	if a.ZerarBase && e.ServerVersion == nil {
		a.ZerarBase = false
	}
	if c := a.Cobre; c != nil && !a.MudouAqui && *c == e.VersaoBase {
		a.Cobre = nil
	}
	if p := a.Projecao; p != nil && mesmaProjecao(p.Par, e.Projecao) {
		a.Projecao = nil
	}
	if a.LimparNuvem && e.Nuvem == nil {
		a.LimparNuvem = false
	}
	if g := a.Nuvem; g != nil && mesmoGuardado(e.Nuvem, g) {
		a.Nuvem = nil
	}
	if p := a.Perdido; p != nil && e.Perdido != nil && *p == *e.Perdido {
		a.Perdido = nil
	}
	// (O par perdido novo vai com a etiqueta: o storage o recusa sem ela.)
	if s := a.Situacao; s != nil && s.Situacao == e.Situacao && s.Motivo == e.Motivo && a.Perdido == nil {
		switch {
		case s.Situacao != storage.EspelhoAplicando:
			// Fora de "aplicando", a esperada já é nula.
			a.Situacao = nil
		case a.Esperada != nil && mesmaProjecao(a.Esperada.Par, e.Esperada):
			a.Situacao, a.Esperada = nil, nil
		}
	}
	if a.VeioDaNuvem == storage.VeioEntra && e.VeioDaNuvem || a.VeioDaNuvem == storage.VeioSai && !e.VeioDaNuvem {
		a.VeioDaNuvem = storage.VeioFica
	}
	if a.Marcador == storage.MarcadorCriar && e.Marcador != nil {
		a.Marcador = storage.MarcadorFica
	}
	return a
}

// vistasDoArquivo é o arquivo diante de cada id: os da projeção, e os do
// espelho com uma projeção guardada que não está mais nela (onde está a
// variante: ainda no arquivo com a origem, ou adotada).
func vistasDoArquivo(s dictionary.Semente, es []storage.EstadoDoAprendido) map[string]VistaDoArquivo {
	out := map[string]VistaDoArquivo{}
	for _, p := range s.ParesDoPainel() {
		p := p
		out[p.ID] = VistaDoArquivo{Projecao: &p}
	}
	for _, e := range es {
		if _, ok := out[e.ID]; ok || e.Projecao == nil {
			continue
		}
		painel, aMao := s.VarianteNoArquivo(e.Projecao.Variante, e.Projecao.Termo)
		out[e.ID] = VistaDoArquivo{ForaDaProjecao: painel, Adotada: !painel && aMao}
	}
	return out
}

// ResultadoDaVarredura é o que uma varredura fez, só em contagens.
type ResultadoDaVarredura struct {
	// Trocas é o M: os upserts que o clique em "Enviar o histórico" solta.
	// Só antes do aceite (depois dele, eles já vão à fila).
	Trocas int
	// SemArquivo: o dicionario.json estava indisponível (ausente, inválido,
	// de outro usuário, preso na troca de nome). Nada foi decidido, e o M não
	// é conhecido.
	SemArquivo bool
	// Freio: quantos pares sairiam do arquivo sem o Desfazer do painel,
	// quando passam do LimiteDoFreio; nada foi gravado. 0 sem o freio.
	Freio int
	// Conferiu: a varredura comparou o arquivo com o espelho (a tabela), e o
	// Freio é o desta comparação: 0 é o freio solto. Sem ela (o arquivo
	// indisponível, um erro antes da leitura), o Freio não diz nada.
	Conferiu bool
	// Tipos conta o que a varredura gravou, por TipoVarredura* (o que não
	// mudou nada não conta).
	Tipos map[string]int
	// Reaplicados conta o que a tentativa de novo dos pendentes gravou, pelo
	// tipo de DecidirPar (aplicado, trocado, desfeito, não aplicado...), e
	// NaoAplicados, dos que ela deixou "não aplicado", por motivo.
	Reaplicados  map[string]int
	NaoAplicados map[string]int
	// LoteFalhou: o lote dos pendentes não terminou (o dicionário resultante
	// não carregou, ou o arquivo não pôde ser gravado). Os pendentes ficam
	// como estavam, e a linha "aplicando" que sobrar a varredura seguinte
	// reconcilia.
	LoteFalhou bool
}

// varredura é o estado de uma Varrer, entre os ganchos do lote.
type varredura struct {
	m      *Motor
	ctx    context.Context
	agora  time.Time
	aceito bool
	epoch  string
	res    ResultadoDaVarredura

	leu    bool
	falha  error
	vistas map[string]VistaDoArquivo
	// planos são os pendentes que foram ao lote, na ordem dos pares.
	planos []PlanoDoPar
	// intencaoFalhou: o erro do lote veio da gravação da intenção (o
	// SQLite), e não do arquivo.
	intencaoFalhou bool
}

// Varrer é a varredura do aprendido (o comentário do começo deste arquivo),
// com o epoch deste pareamento (o do Preparar). Sem Dicionario, não faz nada
// (a F7). Devolve storage.ErrPareamentoMudou ou storage.ErrEpochDiferente
// quando a conferência de antes do arquivo, ou a transação dos pendentes,
// viu o pareamento ou o epoch mudados: o que a tabela gravou fica, e o M vale.
func (m *Motor) Varrer(ctx context.Context, epoch string) (ResultadoDaVarredura, error) {
	v := &varredura{m: m, ctx: ctx, agora: m.agora(), epoch: epoch,
		res: ResultadoDaVarredura{Tipos: map[string]int{}, Reaplicados: map[string]int{}, NaoAplicados: map[string]int{}}}
	if m.Dicionario == nil {
		return v.res, nil
	}
	aceito, err := m.Fila.EnvioAceito(ctx)
	if err != nil {
		return v.res, err
	}
	v.aceito = aceito
	_, lote, err := m.Dicionario.AplicarPares(v.escolher, v.antesDeGravar)
	switch {
	case v.falha != nil:
		return v.res, v.falha
	case err != nil && !v.leu:
		if dictionary.ArquivoIndisponivel(err) {
			// Sem informação, nunca "tudo desfeito".
			v.res.SemArquivo = true
			return v.res, nil
		}
		return v.res, err
	case err != nil && v.intencaoFalhou:
		return v.res, err
	case err != nil:
		v.res.LoteFalhou = true
		return v.res, nil
	}
	return v.res, v.terminar(lote)
}

// escolher é o passo 1 e o planejamento do passo 2, sob a trava do
// dicionário, com o arquivo relido: a tabela (numa transação), e os
// pendentes que vão ao lote.
func (v *varredura) escolher(s dictionary.Semente) []dictionary.ParDaNuvem {
	v.leu = true
	es, err := v.m.Fila.Espelho(v.ctx)
	if err != nil {
		v.falha = err
		return nil
	}
	v.vistas = vistasDoArquivo(s, es)
	estados := make(map[string]storage.EstadoDoAprendido, len(es))
	for _, e := range es {
		estados[e.ID] = e
	}
	ids := make([]string, 0, len(v.vistas)+len(es))
	for id := range estados {
		ids = append(ids, id)
	}
	for id, vista := range v.vistas {
		if _, ok := estados[id]; !ok && vista.Projecao != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var mudar []string
	tipos := map[string]int{}
	sai := 0
	for _, id := range ids {
		e, ok := estados[id]
		if !ok {
			e = storage.EstadoDoAprendido{ID: id}
		}
		d := DecidirVarredura(e, v.vistas[id], v.aceito, v.agora)
		if d.Segura {
			v.res.Trocas++
		}
		if d.Sai {
			sai++
		}
		if soOQueMuda(e, d.Acao) != (storage.AcaoDoAprendido{}) {
			mudar = append(mudar, id)
			tipos[d.Tipo]++
		}
	}
	v.res.Conferiu = true
	if sai > LimiteDoFreio {
		// Nada é gravado, nem tentado: o arquivo que perdeu tantos pares de
		// uma vez (um dicionario.json antigo restaurado) é suspeito.
		v.res.Freio = sai
		return nil
	}
	if len(mudar) > 0 {
		if err := v.m.Fila.MudarAprendido(v.ctx, v.agora, mudar, func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return soOQueMuda(e, DecidirVarredura(e, v.vistas[e.ID], v.aceito, v.agora).Acao)
		}); err != nil {
			v.falha = err
			return nil
		}
		for t, n := range tipos {
			v.res.Tipos[t] += n
		}
	}

	// Os pendentes que a tabela não tocou (a primeira linha que casa
	// decide): de novo, sobre o arquivo relido.
	tocados := make(map[string]bool, len(mudar))
	for _, id := range mudar {
		tocados[id] = true
	}
	depois, err := v.m.Fila.Espelho(v.ctx)
	if err != nil {
		v.falha = err
		return nil
	}
	var pares []dictionary.ParDaNuvem
	for _, e := range depois {
		if tocados[e.ID] {
			continue
		}
		plano, ok := PlanejarPendente(e, ArquivoDoPar{Projecao: v.vistas[e.ID].Projecao})
		if !ok || !plano.Lote {
			// O pulado continua pendente como está (DecidirPar não mudaria
			// nada).
			continue
		}
		v.planos = append(v.planos, plano)
		pares = append(pares, plano.Par)
	}
	if len(pares) == 0 {
		return nil
	}
	// A leitura curta de antes do arquivo ("A ordem", passo 1).
	if err := v.m.Fila.ConferirPareamento(v.ctx, v.m.Aparelho, v.epoch); err != nil {
		v.falha, v.planos = err, nil
		return nil
	}
	return pares
}

// antesDeGravar é o passo 2: a intenção de cada id cujo arquivo muda, antes
// da gravação.
func (v *varredura) antesDeGravar(l dictionary.Lote) error {
	acoes := map[string]storage.AcaoDoAprendido{}
	var ids []string
	for i, p := range v.planos {
		if i >= len(l.Resultados) {
			break
		}
		if a, ok := IntencaoDoPar(p, l.Resultados[i]); ok {
			acoes[p.ID] = a
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	err := v.m.Fila.MudarAprendido(v.ctx, v.agora, ids, func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return acoes[e.ID]
	})
	if err != nil {
		v.intencaoFalhou = true
	}
	return err
}

// terminar é o passo 3: o espelho dos pendentes que foram ao lote, numa
// transação com a conferência do epoch e do pareamento. Sem nada que mude,
// nem abre a transação.
func (v *varredura) terminar(l dictionary.Lote) error {
	if len(v.planos) == 0 || len(l.Resultados) != len(v.planos) {
		return nil
	}
	planos := make(map[string]PlanoDoPar, len(v.planos))
	resultados := make(map[string]*dictionary.ResultadoDoPar, len(v.planos))
	ids := make([]string, 0, len(v.planos))
	for i, p := range v.planos {
		r := l.Resultados[i]
		planos[p.ID], resultados[p.ID] = p, &r
		ids = append(ids, p.ID)
	}
	es, err := v.m.Fila.Espelho(v.ctx)
	if err != nil {
		return err
	}
	algum := false
	for _, e := range es {
		if p, ok := planos[e.ID]; ok && soOQueMuda(e, DecidirPar(e, p, resultados[e.ID]).Acao) != (storage.AcaoDoAprendido{}) {
			algum = true
		}
	}
	if !algum {
		return nil
	}
	tipos, motivos := map[string]int{}, map[string]int{}
	err = v.m.Fila.AplicarComAprendido(v.ctx, v.m.Aparelho, v.epoch, nil, nil, nil, storage.RespostaDoAprendido{
		IDs: ids, Agora: v.agora,
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			d := DecidirPar(e, planos[e.ID], resultados[e.ID])
			a := soOQueMuda(e, d.Acao)
			if a != (storage.AcaoDoAprendido{}) {
				tipos[d.Tipo]++
				if m, ok := motivoDoNaoAplicado(a); ok {
					motivos[m]++
				}
			}
			return a
		},
	})
	if err != nil {
		return err
	}
	juntar(v.res.Reaplicados, tipos)
	juntar(v.res.NaoAplicados, motivos)
	return nil
}
