package learning

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Este arquivo é o motor de sugestão da etapa 3 (aprender com as edições do
// painel): dada uma edição, diz QUE troca de termo vale a pena perguntar à
// pessoa se ela quer que o dicionário aprenda.
//
// # Só sugere, nunca grava
//
// Sugerir é função pura. Não lê arquivo nem banco, não muda o dicionário que
// recebe, não escreve log. Quem grava é a etapa seguinte, e só depois do
// clique da pessoa (AC-022, REQ-020): uma sugestão ignorada não deixa rastro.
//
// # Três textos, não dois
//
// Classify compara o texto de antes com o de depois da edição. Isso acha o
// que a PESSOA mudou, mas não a forma que o reconhecimento de voz escreveu,
// e é essa a forma que um alias precisa ter para valer no próximo ditado
// (decisão de 28-set-2026, "alias aprende a forma que o ASR produz"). O
// texto de antes já passou pelo Smart e pelo Polish, então "SF" pode ter
// virado "ECF" sozinho. Por isso Sugerir recebe também o BRUTO:
//
//  1. compara antes e depois palavra a palavra, ignorando maiúscula, acento e
//     pontuação: o que sobra é a edição da pessoa;
//  2. para cada troca, alinha o texto de antes com o bruto e lê no bruto a
//     forma original (De). É aqui que se enxerga uma troca feita pelo
//     sistema, e que a pessoa desfez ou refez;
//  3. filtra. Cada filtro existe porque o alias vale para TODO ditado futuro
//     e em qualquer aplicativo (nenhum contexto de janela é guardado).
//
// # O que nunca sugere
//
//   - maiúscula, acento, pontuação ou quebra de linha sozinhos: a comparação
//     os ignora, então não há troca. Ensinar "está" faria "esta casa" virar
//     "está casa" em todo texto;
//   - reescrita: troca com mais de 4 palavras ou 30 caracteres de um lado,
//     mais de 2 trocas na mesma edição, ou uma edição que só acrescenta ou só
//     apaga palavras;
//   - números, de qualquer lado ("cinco" para "5", "2026" para "2027"), e
//     também número grudado em letra quando o De tem dígito e os dígitos
//     mudam ("10h" para "11h", "1º" para "2º", "v2" para "v3"), e numeral
//     por extenso que vira outro ("oito" para "seis", "mil" para "milhão",
//     "primeiro" para "terceiro") ou vira uma palavra qualquer. O numeral
//     dentro de um nome soletrado ("k oito s" para "Kubernetes") pode, e o
//     que a spec pede ("n oito n" para "n8n") também;
//   - troca cujo De, ou cujo Para, é só palavra de ligação ("de" para "do"),
//     a não ser que o Para seja um termo ativo do dicionário, escrito como o
//     dicionário o escreve (o "DAS", mas não a preposição "das");
//   - troca com palavra de negação, afirmação ou quantificação em qualquer
//     lado ("não" para "sim", "não posso" para "consigo", "mais" para "mas",
//     "sempre" para "às vezes"): o alias inverteria o sentido;
//   - junção ou separação de palavra de ligação ("se não" para "senão", "de
//     mais" para "demais"): a palavra de ligação é parte do que foi colado, e
//     sem ela o alias ("não" para "senão") mudaria outras frases. A junção
//     com flexão ou assimilação ("a nota" para "anotar", "com firma" para
//     "confirmar") também: a palavra de ligação que saiu da esquerda começa
//     pela mesma letra que o outro lado;
//   - desfazer uma troca que o próprio sistema fez: o Smart trocou "comite"
//     por "commit" e a pessoa voltou para "comitê";
//   - variante que já pertence a um termo do dicionário (a outro, ou ao
//     mesmo: já está aprendida), termo desativado e termo restrito a domínio
//     (o alias valeria só lá, e a etapa promete "qualquer aplicativo"). O termo
//     desativado conta como dono das suas formas, como no dicionário do Agent,
//     que recusa a variante de qualquer termo, ligado ou não: De ou Para que
//     pertencem a um termo desativado também não viram sugestão;
//   - troca de que não se acha a origem no bruto com segurança: a palavra
//     vizinha também foi trocada pelo sistema, e não dá para saber de qual
//     das duas veio o erro.
//
// # Termo do dicionário consertado em parte
//
// A troca mínima de "EFD rain" para "EFD-Reinf" é "rain" para "Reinf", mas o
// que a pessoa consertou foi o termo "EFD-Reinf", que o dicionário já tem.
// Aprender o pedaço criaria o termo "Reinf". Por isso, em volta de cada troca,
// Sugerir olha as palavras que não mudaram e, se elas junto com a troca formam
// a forma canônica de um termo do dicionário, sugere o termo inteiro: De é
// "efd rain" (do bruto) e Para é "EFD-Reinf" (TermoExistente preenchido).
//
// Só a troca mínima, a que a pessoa fez, é medida por número e por negação: o
// "nada" de "nada costa" virando "Nada Consta" já estava lá. E a extensão não
// vale quando o artigo virou a primeira palavra do termo ("o nacional" para
// "Simples Nacional"), nem quando o sistema tirou uma palavra do meio do
// bruto da janela (a gagueira de "efd efd rain"): nos dois casos a troca é
// descartada, e não volta ao fragmento ("rain" para "Reinf") que a extensão
// existe para evitar.
//
// # A maiúscula de começo de frase não é do termo
//
// O Polish capitaliza toda frase, então a primeira palavra de uma frase é
// maiúscula por ser o começo, não por ser um nome. Quando o termo é novo e a
// troca está no começo de uma frase, em que a palavra de antes também era
// maiúscula só por isso, o Para perde a maiúscula da primeira letra
// ("Transcrição ficou pronta" para "Descrição ficou pronta" aprende
// "descrição", não "Descrição", que apareceria errado no meio de outra
// frase). Sigla ("ECF") e nome com mais de uma maiúscula ("Wispr Flow") ficam
// como estão, e também o nome que o texto mostra ser nome: a mesma forma em
// maiúscula no meio de outra frase, ou a palavra de logo depois ter ganhado
// maiúscula na edição ("Cloud code" para "Claude Code"). Termo que já existe
// no dicionário usa a forma do dicionário.
//
// O risco que sobra: um nome de uma palavra só, no começo da frase e sem
// nenhuma dessas provas ("Mário falou" para "Mauro falou"), aprende a forma
// minúscula. Não há como separá-lo de "Descrição" só pelo texto.
//
// # O que sugere mesmo sendo duvidoso
//
// "transcrição" para "descrição" é sugerido: é uma troca curta de palavra
// comum, e nada ortográfico separa um erro de reconhecimento de uma mudança de
// ideia (ver Suggestion.Similaridade, que mediu isso). Quem barra é o clique.

// MaxSugestoes é o teto de sugestões por edição, e também quantas trocas cabem
// numa edição antes de ela virar reescrita: cada troca dá no máximo uma
// sugestão, então mais de MaxSugestoes trocas é mais do que "corrigir um termo
// ou dois".
const MaxSugestoes = 2

// Sugestao é uma troca de termo que a pessoa pode mandar o dicionário
// aprender: onde o bruto do ASR tem De, o texto final passa a ter Para.
type Sugestao struct {
	// De é a forma que o reconhecimento de voz escreveu, lida do texto BRUTO,
	// sem a pontuação das pontas. É o alias a gravar.
	De string
	// Para é a forma final. Se o dicionário já tem esse termo, é a forma
	// canônica dele ("ecf" escrito na edição vira "ECF"); se não, é como a
	// pessoa escreveu. Passe Para como forma canônica a
	// dictionary.Semente.AcrescentarAlias: ela acha o termo existente e
	// cria o que falta.
	Para string
	// TermoExistente é a forma canônica do termo do dicionário que já é Para,
	// ou vazio quando aprender esta troca cria o termo. Só diz qual dos dois
	// casos é: quando não vazio, é igual a Para.
	TermoExistente string
	// OcorrenciasNoHistorico é em quantos ditados do histórico De aparece, se
	// quem chamou passou um contador; 0 caso contrário. É o que deixa o aviso
	// dizer "aparece em 3 ditados", para a pessoa medir o alcance da regra.
	OcorrenciasNoHistorico int
}

// ContadorOcorrencias conta em quantos ditados do histórico a forma de
// aparece. Quem chama injeta, porque Sugerir não lê histórico. Veja
// OcorrenciasEm.
type ContadorOcorrencias func(de string) int

// Motivo diz por que uma edição, ou uma troca dela, não virou sugestão. É
// um conjunto fechado de códigos, NUNCA o texto ditado: pode ir para o
// registro de funcionamento (AC-027) e ser contado.
type Motivo string

const (
	// MotivoEntradaVazia: o bruto, o texto de antes ou o de depois não tem
	// nenhuma palavra.
	MotivoEntradaVazia Motivo = "entrada_vazia"
	// MotivoSemTroca: as palavras são as mesmas. Só mudou maiúscula, acento,
	// pontuação, espaço ou quebra de linha.
	MotivoSemTroca Motivo = "sem_troca"
	// MotivoSoAcrescentaOuApaga: a edição pôs ou tirou palavras, sem trocar
	// uma por outra. Não há termo virando termo.
	MotivoSoAcrescentaOuApaga Motivo = "so_acrescenta_ou_apaga"
	// MotivoReescrita: mais de 2 trocas, troca com mais de 4 palavras ou 30
	// caracteres, ou texto longo demais para alinhar.
	MotivoReescrita Motivo = "reescrita"
	// MotivoSemOrigemNoBruto: não se acha, no bruto, a forma original da
	// troca, ou o bruto de um termo do dicionário consertado em parte tem
	// uma palavra que o sistema tirou no meio.
	MotivoSemOrigemNoBruto Motivo = "sem_origem_no_bruto"
	// MotivoNumero: um dos lados tem número, ou o De tem dígito e os dígitos
	// mudam, ou um numeral por extenso vira outro número ou uma palavra
	// qualquer.
	MotivoNumero Motivo = "numero"
	// MotivoPalavraDeLigacao: um dos lados é só palavra de ligação, ou tem
	// palavra de negação, afirmação ou quantificação, ou a palavra de ligação
	// da ponta da troca é parte de uma palavra que o outro lado juntou ou
	// separou ("se não" para "senão", "a nota" para "anotar"), ou um artigo
	// virou a primeira palavra de um termo do dicionário.
	MotivoPalavraDeLigacao Motivo = "palavra_de_ligacao"
	// MotivoAtravessaFrase: um dos lados cruza fim de frase ou quebra de
	// linha.
	MotivoAtravessaFrase Motivo = "atravessa_frase"
	// MotivoDesfazTrocaDoSistema: a pessoa voltou à forma que o bruto já
	// tinha, isto é, desfez o que o sistema trocou.
	MotivoDesfazTrocaDoSistema Motivo = "desfaz_troca_do_sistema"
	// MotivoJaNoDicionario: De já é variante do mesmo termo de Para.
	MotivoJaNoDicionario Motivo = "ja_no_dicionario"
	// MotivoVarianteDeOutroTermo: De, ou Para, já é variante de outro termo.
	MotivoVarianteDeOutroTermo Motivo = "variante_de_outro_termo"
	// MotivoTermoInativo: o termo de Para existe, desativado.
	MotivoTermoInativo Motivo = "termo_inativo"
	// MotivoTermoRestrito: o termo de Para existe, restrito a um domínio.
	MotivoTermoRestrito Motivo = "termo_restrito"
)

// Resultado é o que Sugerir devolve.
type Resultado struct {
	// Sugestoes tem de 0 a MaxSugestoes trocas, na ordem do texto.
	Sugestoes []Sugestao
	// Descartes tem o motivo de cada troca descartada, na ordem do texto. Se
	// a edição inteira foi barrada (reescrita, texto vazio, nada mudou), tem
	// um motivo só.
	Descartes []Motivo
}

// Sugerir devolve as trocas de termo da edição que valem ser perguntadas à
// pessoa.
//
// bruto é o texto do reconhecimento de voz; antes, o texto final como estava
// antes da edição; depois, o que a pessoa salvou; dic, o dicionário atual.
// contar é opcional (nil): quando passado, preenche
// Sugestao.OcorrenciasNoHistorico.
//
// Pura: não altera dic nem grava nada. Veja a documentação do arquivo para o
// que sugere e o que não.
func Sugerir(bruto, antes, depois string, dic dictionary.Semente, contar ContadorOcorrencias) Resultado {
	c := contexto{
		bruto: bruto, antes: antes, depois: depois,
		tBruto: tokenizar(bruto), tAntes: tokenizar(antes), tDepois: tokenizar(depois),
		ix: novoIndice(dic), contar: contar,
	}
	if len(c.tBruto) == 0 || len(c.tAntes) == 0 || len(c.tDepois) == 0 {
		return barrada(MotivoEntradaVazia)
	}
	c.kBruto, c.kAntes, c.kDepois = chaves(c.tBruto), chaves(c.tAntes), chaves(c.tDepois)

	// 1. O que a pessoa mudou: antes contra depois.
	pares, ok := pareamento(c.kAntes, c.kDepois)
	if !ok {
		return barrada(MotivoReescrita)
	}
	mudancas := trechos(pares, len(c.kAntes), len(c.kDepois))
	if len(mudancas) == 0 {
		return barrada(MotivoSemTroca)
	}

	n := 0
	for _, m := range mudancas {
		if m.troca() {
			n++
		}
	}
	if n > MaxSugestoes {
		return barrada(MotivoReescrita)
	}

	// Palavra de ligação na ponta é da frase, não do termo: "Whisperflow"
	// virando "o Wispr Flow" troca um termo, não um artigo. O tamanho é
	// medido depois de aparar. Quando a palavra de ligação é parte do que o
	// outro lado colou ("se não" virando "senão"), a troca não se aprende: a
	// troca aparada ("não" para "senão") seria um alias que ninguém fez.
	barras := make([]Motivo, len(mudancas))
	for i, m := range mudancas {
		if !m.troca() {
			continue
		}
		m, barras[i] = c.aparar(m)
		if grande(fatia(c.tAntes, antes, m.a1, m.a2)) || grande(fatia(c.tDepois, depois, m.b1, m.b2)) {
			return barrada(MotivoReescrita)
		}
		mudancas[i] = m
	}

	// 2. De onde cada troca veio: bruto contra antes.
	if pb, ok := pareamento(c.kBruto, c.kAntes); ok {
		c.orig = origens(pb, len(c.kBruto), len(c.kAntes))
	}

	var res Resultado
	vistos := map[string]bool{}
	for i, m := range mudancas {
		if !m.troca() {
			res.Descartes = append(res.Descartes, MotivoSoAcrescentaOuApaga)
			continue
		}
		if barras[i] != "" {
			res.Descartes = append(res.Descartes, barras[i])
			continue
		}
		sug, motivo := c.avaliar(m)
		switch {
		case motivo == MotivoReescrita:
			return barrada(MotivoReescrita)
		case motivo != "":
			res.Descartes = append(res.Descartes, motivo)
		case vistos[colapsada(sug.De)]:
			// Duas trocas com o mesmo De pediriam duas vezes o mesmo alias.
		default:
			vistos[colapsada(sug.De)] = true
			res.Sugestoes = append(res.Sugestoes, sug)
		}
	}
	return res
}

func barrada(m Motivo) Resultado { return Resultado{Descartes: []Motivo{m}} }

// contexto reúne o que avaliar precisa de uma edição.
type contexto struct {
	bruto, antes, depois    string
	tBruto, tAntes, tDepois []token
	kBruto, kAntes, kDepois []string
	// orig tem uma origem por palavra de antes. nil quando o bruto e o
	// texto de antes são grandes demais para alinhar.
	orig   []origem
	ix     indice
	contar ContadorOcorrencias
}

// avaliar decide uma troca. Devolve o motivo do descarte, ou vazio com a
// sugestão pronta. O único motivo que barra a edição toda é MotivoReescrita.
func (c *contexto) avaliar(t trecho) (Sugestao, Motivo) {
	// Origem. A troca só tem origem segura se as palavras ao redor dela são
	// as mesmas do bruto. Vizinha trocada pelo sistema quer dizer que a
	// troca pode estar no meio de um termo que o sistema escreveu ("Wispr
	// Flow" virando "Wispr Flux"), e ali "Flow" para "Flux" seria um alias
	// sem sentido.
	if c.orig == nil ||
		(t.a1 > 0 && !c.orig[t.a1-1].casou) ||
		(t.a2 < len(c.orig) && !c.orig[t.a2].casou) {
		return Sugestao{}, MotivoSemOrigemNoBruto
	}
	r1, r2 := c.orig[t.a1].r1, c.orig[t.a2-1].r2
	if r2 <= r1 {
		return Sugestao{}, MotivoSemOrigemNoBruto
	}

	if grande(fatia(c.tBruto, c.bruto, r1, r2)) {
		return Sugestao{}, MotivoReescrita
	}

	// A troca mínima é o que a pessoa mudou. Número e negação se medem nela, e
	// não na janela estendida abaixo: "nada costa" virando "Nada Consta"
	// conserta uma letra, e o "nada" já estava lá.
	kDeMin, kParaMin := c.kBruto[r1:r2], c.kDepois[t.b1:t.b2]

	// A troca mínima pode ser só um pedaço de um termo que o dicionário já
	// tem: com as palavras que não mudaram ao redor, vira o termo inteiro.
	b1, b2 := t.b1, t.b2
	e, ok, motivo := c.estende(t, r1, r2)
	switch {
	case motivo != "":
		return Sugestao{}, motivo
	case ok && soLigacao(kDeMin) && !soLigacao(kParaMin):
		// Um artigo que virou a primeira palavra de um termo ("o nacional" para
		// "Simples Nacional") é da frase. Estendido, o De ("o nacional") não
		// pareceria mais só palavra de ligação, e valeria para todo "o
		// nacional" futuro. Se os dois lados mínimos são de ligação ("de" para
		// "do", dentro de "Banco do Brasil"), a troca é de dentro do termo.
		return Sugestao{}, MotivoPalavraDeLigacao
	case ok:
		b1, b2, r1, r2 = e.b1, e.b2, e.r1, e.r2
	}

	de := fatia(c.tBruto, c.bruto, r1, r2)
	para := fatia(c.tDepois, c.depois, b1, b2)
	kDe, kPara := c.kBruto[r1:r2], c.kDepois[b1:b2]
	paraChave := strings.Join(kPara, "")
	alvo, temAlvo := c.ix.canon[paraChave]

	switch {
	case temNumero(kDeMin) || temNumero(kParaMin) || mudaDigito(kDeMin, kParaMin) || mudaNumeral(kDeMin, kParaMin):
		return Sugestao{}, MotivoNumero
	// Palavra de ligação como Para só barra quando o Para não é um termo do
	// dicionário, escrito como o dicionário escreve: "daz" virando "DAS" é o
	// termo, e a chave de "DAS" ("das") está na lista. "diz" virando a
	// preposição "das" não é o termo, e o alias "diz" para "DAS" trocaria o
	// "diz" de toda frase.
	case soLigacao(kDe) || (soLigacao(kPara) && !(temAlvo && para == strings.TrimSpace(c.ix.entradas[alvo].Canonica))) ||
		temPolaridade(kDeMin) || temPolaridade(kParaMin):
		return Sugestao{}, MotivoPalavraDeLigacao
	case cruzaFrase(de) || cruzaFrase(para):
		return Sugestao{}, MotivoAtravessaFrase
	case strings.Join(kDe, " ") == strings.Join(kPara, " "):
		// O bruto já tinha isto: o sistema trocou, a pessoa voltou.
		return Sugestao{}, MotivoDesfazTrocaDoSistema
	}

	// Dicionário. Dono de De: um termo ativo que já o tem como forma ou
	// variante. Um termo desativado também é dono: o dicionário do Agent
	// recusa a variante que pertence a qualquer termo, ligado ou não, e o
	// botão Aprender não pode aparecer para o que ele sempre recusaria.
	deChave := strings.Join(kDe, "")
	if dono, ok := c.ix.ativo[deChave]; ok {
		if temAlvo && dono == alvo {
			return Sugestao{}, MotivoJaNoDicionario
		}
		return Sugestao{}, MotivoVarianteDeOutroTermo
	}
	if _, ok := c.ix.desativado[deChave]; ok {
		return Sugestao{}, MotivoTermoInativo
	}

	sug := Sugestao{De: compacta(de), Para: compacta(para)}
	if temAlvo {
		e := c.ix.entradas[alvo]
		switch {
		case e.Inativa:
			return Sugestao{}, MotivoTermoInativo
		case e.Restrito:
			return Sugestao{}, MotivoTermoRestrito
		}
		sug.TermoExistente = strings.TrimSpace(e.Canonica)
		sug.Para = sug.TermoExistente
	} else if _, ok := c.ix.ativo[paraChave]; ok {
		// Para é variante de um termo: o Smart o reescreveria, e o alias
		// novo apontaria para algo que não é a forma final.
		return Sugestao{}, MotivoVarianteDeOutroTermo
	} else if _, ok := c.ix.desativado[paraChave]; ok {
		// Variante de um termo desativado: o dicionário recusa criar o termo
		// novo (seria o termo de outro), ligado ou não.
		return Sugestao{}, MotivoTermoInativo
	} else {
		// Termo novo: a maiúscula de começo de frase não é do termo. (Sem
		// termo no dicionário a troca nunca foi estendida, então b1 é o
		// começo da troca em depois, e t.a1 o de antes.)
		sug.Para = c.semCaixaDeInicio(sug.Para, t)
	}
	if c.contar != nil {
		sug.OcorrenciasNoHistorico = c.contar(sug.De)
	}
	return sug, ""
}

// extensao são os limites da troca estendida, em depois (b1, b2) e no bruto
// (r1, r2).
type extensao struct{ b1, b2, r1, r2 int }

// estende procura um termo do dicionário que a troca conserta só em parte. A
// troca mínima de "EFD rain" para "EFD-Reinf" é "rain" para "Reinf", mas o que
// a pessoa consertou foi o termo inteiro. Devolve a troca aumentada das
// palavras vizinhas que não mudaram (as mesmas em antes e em depois, e sem
// troca do sistema no bruto) quando a janela de depois, sem espaço nem
// pontuação, é a forma canônica de um termo do dicionário. Com mais de uma
// janela possível, vale a maior: "whisper flow" para "Wispr Flow" conserta o
// "Wispr Flow", não o "Wispr".
//
// O teto é o de sempre: 4 palavras e 30 caracteres, de cada lado, sem cruzar
// fim de frase.
//
// O bruto da janela tem de ser contíguo. Se o sistema tirou uma palavra do
// meio dela (a gagueira de "efd efd rain", uma hesitação), o De estendido
// levaria a palavra que sumiu, e um alias assim nunca casa. Voltar ao
// pedaço ("rain" para "Reinf") criaria o termo-fragmento que a extensão
// existe para evitar, então a troca inteira é descartada: o motivo volta
// MotivoSemOrigemNoBruto.
func (c *contexto) estende(t trecho, r1, r2 int) (extensao, bool, Motivo) {
	for extra := maxAliasWords - (t.b2 - t.b1); extra >= 1; extra-- {
		for kl := 0; kl <= extra; kl++ {
			switch e, j := c.estendeEm(t, r1, r2, kl, extra-kl); j {
			case janelaOk:
				return e, true, ""
			case janelaSemOrigem:
				return extensao{}, false, MotivoSemOrigemNoBruto
			}
		}
	}
	return extensao{}, false, ""
}

// janela é o que estendeEm achou.
type janela int

const (
	janelaNao       janela = iota // não há termo do dicionário nesta janela
	janelaOk                      // há, e ela vale
	janelaSemOrigem               // há, mas o bruto dela não é contíguo
)

// estendeEm tenta a janela com kl palavras a mais à esquerda e kr à direita.
func (c *contexto) estendeEm(t trecho, r1, r2, kl, kr int) (extensao, janela) {
	b1, b2 := t.b1-kl, t.b2+kr
	if b1 < 0 || b2 > len(c.kDepois) || t.a1-kl < 0 || t.a2+kr > len(c.kAntes) {
		return extensao{}, janelaNao
	}
	for i := 1; i <= kl; i++ {
		if c.kAntes[t.a1-i] != c.kDepois[t.b1-i] || !c.orig[t.a1-i].casou {
			return extensao{}, janelaNao
		}
	}
	for i := 0; i < kr; i++ {
		if c.kAntes[t.a2+i] != c.kDepois[t.b2+i] || !c.orig[t.a2+i].casou {
			return extensao{}, janelaNao
		}
	}
	if _, ok := c.ix.canon[strings.Join(c.kDepois[b1:b2], "")]; !ok {
		return extensao{}, janelaNao
	}
	if !c.brutoContiguo(t, kl, kr) {
		return extensao{}, janelaSemOrigem
	}
	if kl > 0 {
		r1 = c.orig[t.a1-kl].r1
	}
	if kr > 0 {
		r2 = c.orig[t.a2+kr-1].r2
	}
	de, para := fatia(c.tBruto, c.bruto, r1, r2), fatia(c.tDepois, c.depois, b1, b2)
	if grande(de) || grande(para) || cruzaFrase(de) || cruzaFrase(para) {
		return extensao{}, janelaNao
	}
	return extensao{b1, b2, r1, r2}, janelaOk
}

// brutoContiguo é verdadeiro quando, no bruto, as kl palavras da esquerda, a
// troca e as kr da direita são vizinhas: o fim de cada uma é o começo da
// seguinte, sem palavra nenhuma entre elas.
func (c *contexto) brutoContiguo(t trecho, kl, kr int) bool {
	for i := 1; i <= kl; i++ {
		if c.orig[t.a1-i].r2 != c.orig[t.a1-i+1].r1 {
			return false
		}
	}
	for i := 0; i < kr; i++ {
		if c.orig[t.a2+i-1].r2 != c.orig[t.a2+i].r1 {
			return false
		}
	}
	return true
}

// semCaixaDeInicio tira do Para a maiúscula que só existe por a troca estar
// no começo de uma frase. O Polish capitaliza toda frase, então ali a
// maiúscula não diz nada sobre o termo: "Descrição" no começo de uma frase é
// a palavra "descrição". Só age quando a troca começa uma frase nos dois
// textos, a palavra de antes também era maiúscula, e o Para tem uma maiúscula
// só, a da primeira letra. Sigla ("ECF") e nome com outra maiúscula ("Wispr
// Flow") ficam como estão: ali a caixa é prova de que é um nome.
func (c *contexto) semCaixaDeInicio(para string, t trecho) string {
	r, n := utf8.DecodeRuneInString(para)
	if !unicode.IsUpper(r) || strings.IndexFunc(para[n:], unicode.IsUpper) >= 0 {
		return para
	}
	if !inicioDeFrase(c.depois, c.tDepois, t.b1) || !inicioDeFrase(c.antes, c.tAntes, t.a1) {
		return para
	}
	if !maiuscula(c.antes, c.tAntes[t.a1]) || c.caixaEDoTermo(t) {
		return para
	}
	return string(unicode.ToLower(r)) + para[n:]
}

// caixaEDoTermo é verdadeiro quando o texto mostra que a maiúscula do Para é
// do termo, e não do começo da frase. Duas provas:
//
//   - a mesma forma, inteira, aparece em maiúscula no meio de outra frase de
//     depois ("Superbase caiu. Uso Supabase."). Maiúscula em começo de outra
//     frase não prova nada, e minúscula no meio prova o contrário. A própria
//     troca não conta;
//   - a palavra logo depois da troca, que não mudou, ganhou maiúscula nesta
//     edição ("Cloud code" para "Claude Code"): quem pôs o C de Code sabia que
//     era um nome. Se ela já era maiúscula em antes, ou começa outra frase,
//     a maiúscula não foi escolha da pessoa.
func (c *contexto) caixaEDoTermo(t trecho) bool {
	if t.a2 < len(c.tAntes) && t.b2 < len(c.tDepois) && c.kAntes[t.a2] == c.kDepois[t.b2] &&
		!maiuscula(c.antes, c.tAntes[t.a2]) && maiuscula(c.depois, c.tDepois[t.b2]) &&
		!inicioDeFrase(c.depois, c.tDepois, t.b2) {
		return true
	}
	forma := c.kDepois[t.b1:t.b2]
	for p := 0; p+len(forma) <= len(c.kDepois); p++ {
		if p != t.b1 && igual(c.kDepois[p:p+len(forma)], forma) &&
			maiuscula(c.depois, c.tDepois[p]) && !inicioDeFrase(c.depois, c.tDepois, p) {
			return true
		}
	}
	return false
}

// maiuscula é verdadeiro quando a palavra tk de texto começa em maiúscula.
func maiuscula(texto string, tk token) bool {
	r, _ := utf8.DecodeRuneInString(texto[tk.ini:])
	return unicode.IsUpper(r)
}

// igual diz se a e b têm as mesmas palavras, na mesma ordem.
func igual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// aberturas são os sinais que podem ficar entre o fim da frase anterior e a
// primeira palavra da seguinte: aspas (de abertura e de fechamento, que vêm
// depois do ponto: “pronto.” Transcrição), parêntese, travessão e marcador de
// lista.
const aberturas = "\"'“”‘’«»([{-–—•*"

// inicioDeFrase é verdadeiro quando a palavra ts[i] de texto começa uma frase:
// é a primeira do texto, vem depois de quebra de linha, ou depois de ponto,
// exclamação, interrogação ou reticências, sem nada no meio além de espaço e
// das aberturas.
func inicioDeFrase(texto string, ts []token, i int) bool {
	p := texto[:ts[i].ini]
	for len(p) > 0 {
		r, n := utf8.DecodeLastRuneInString(p)
		switch {
		case r == '\n' || r == '\r':
			return true
		case unicode.IsSpace(r) || strings.ContainsRune(aberturas, r):
			p = p[:len(p)-n]
		default:
			return strings.ContainsRune(".!?…", r)
		}
	}
	return true
}

// indice é o dicionário indexado pela forma colapsada de cada termo, a
// mesma comparação do Smart: sem maiúscula, acento, pontuação nem espaço.
type indice struct {
	entradas []dictionary.EntradaSemente
	// canon acha a entrada pela forma canônica, inclusive desativada: para
	// saber que o termo existe e está desligado, em vez de recriá-lo.
	canon map[string]int
	// ativo acha a entrada ATIVA pela forma canônica ou por uma variante,
	// restrita ou não.
	ativo map[string]int
	// desativado acha a entrada DESATIVADA pela forma canônica ou por uma
	// variante. Ela não corrige nada no ditado, mas continua dona das suas
	// formas para o dicionário do Agent, que recusa a variante de qualquer
	// termo, ligado ou não: quem sugere tem de concordar com quem grava.
	desativado map[string]int
}

func novoIndice(dic dictionary.Semente) indice {
	ix := indice{entradas: dic.Entradas, canon: map[string]int{}, ativo: map[string]int{}, desativado: map[string]int{}}
	poe := func(m map[string]int, forma string, i int) {
		// Forma vazia vira a chave "", que nenhuma consulta usa: toda
		// palavra tem ao menos uma letra ou um dígito.
		if k := colapsada(forma); !contemChave(m, k) {
			m[k] = i
		}
	}
	// Canônicas antes das variantes: a forma canônica de uma entrada ganha
	// de uma variante igual em outra.
	for i, e := range dic.Entradas {
		poe(ix.canon, e.Canonica, i)
		if e.Inativa {
			poe(ix.desativado, e.Canonica, i)
		} else {
			poe(ix.ativo, e.Canonica, i)
		}
	}
	for i, e := range dic.Entradas {
		if colapsada(e.Canonica) == "" {
			continue
		}
		formas := ix.ativo
		if e.Inativa {
			formas = ix.desativado
		}
		for _, a := range e.Alias {
			poe(formas, a, i)
		}
		for _, a := range e.AliasRestrito {
			poe(formas, a, i)
		}
	}
	return ix
}

func contemChave(m map[string]int, k string) bool {
	_, ok := m[k]
	return ok
}

func colapsada(s string) string {
	return dictionary.CollapseSpaces(dictionary.Normalize(s))
}

// aparar tira as palavras de ligação das pontas dos dois lados da troca. O
// motivo volta preenchido quando a troca deve ser descartada: a palavra de
// ligação que sairia da ponta é parte de uma palavra que o outro lado juntou
// ou separou. "se não" virando "senão" aparado seria "não" virando "senão", um
// alias que a pessoa não fez e que trocaria o "não" de outras frases.
func (c *contexto) aparar(m trecho) (trecho, Motivo) {
	a1, a2 := apararLado(c.tAntes, c.antes, c.kAntes, m.a1, m.a2)
	b1, b2 := apararLado(c.tDepois, c.depois, c.kDepois, m.b1, m.b2)
	motivo := Motivo("")
	if absorvida(c.kAntes, m.a1, m.a2, a1, a2, c.kDepois[m.b1:m.b2]) ||
		absorvida(c.kDepois, m.b1, m.b2, b1, b2, c.kAntes[m.a1:m.a2]) ||
		encosta(c.kAntes, m.a1, a1, c.kDepois[b1:b2]) ||
		encosta(c.kDepois, m.b1, b1, c.kAntes[a1:a2]) {
		motivo = MotivoPalavraDeLigacao
	}
	return trecho{a1, a2, b1, b2}, motivo
}

// apararLado tira as palavras de ligação das pontas de ks[i:j], sem nunca
// esvaziar o lado. Devolve os novos i e j.
//
// A palavra só sai se estiver separada da vizinha por espaço no texto: "NF-e",
// "e-mail" e "S.A." são um termo só, e o "e" e o "a" deles não são a conjunção
// nem o artigo.
func apararLado(ts []token, texto string, ks []string, i, j int) (int, int) {
	for j-i > 1 && ligacaoNorm[ks[i]] && separadas(ts, texto, i, i+1) {
		i++
	}
	for j-i > 1 && ligacaoNorm[ks[j-1]] && separadas(ts, texto, j-2, j-1) {
		j--
	}
	return i, j
}

// separadas é verdadeiro quando há espaço, ou quebra de linha, entre as
// palavras vizinhas p e q de ts.
func separadas(ts []token, texto string, p, q int) bool {
	return strings.IndexFunc(texto[ts[p].fim:ts[q].ini], unicode.IsSpace) >= 0
}

// absorvida é verdadeiro quando o lado aparado (ks[i0:j0], que ficou ks[i:j])
// perdeu uma palavra de ligação que o outro lado tem colada: o que sobrou,
// sem espaço, é o fim (corte da esquerda) ou o começo (corte da direita) do
// outro lado inteiro. "com tudo" virando "contudo" deixa "tudo", e "contudo"
// termina em "tudo".
func absorvida(ks []string, i0, j0, i, j int, outro []string) bool {
	resto, todo := strings.Join(ks[i:j], ""), strings.Join(outro, "")
	if len(todo) <= len(resto) {
		return false
	}
	return (i > i0 && strings.HasSuffix(todo, resto)) || (j < j0 && strings.HasPrefix(todo, resto))
}

// encosta pega o que absorvida não pega: a junção com flexão ou assimilação.
// O ASR escreve "a nota" para "anotar" (o -r do infinitivo cai na fala) e "com
// firma" para "confirmar" (o m vira n), e o que sobra de cada lado, "nota" e
// "anotar", não é o fim nem o começo do outro. O sinal é a palavra de ligação
// que saiu da esquerda do lado ks (ks[i0:i] saiu, e ks[i-1] é a que encosta):
// se ela começa pela mesma letra que o outro lado, já aparado, é provável que
// tenha sido colada nele. outro é o outro lado depois de aparado.
//
// Só olha a esquerda: a palavra de ligação do português cola na frente ("a",
// "com", "de", "em"), e a da direita já é pega pelo sufixo em absorvida. O
// custo é barrar "otlook" virando "o Outlook": o artigo "o" encosta no "o" de
// Outlook, e não há como separar esse caso de "a nota" virando "anotar" só
// pelas letras. Quem quiser esse alias pode acrescentá-lo no dicionário.
func encosta(ks []string, i0, i int, outro []string) bool {
	if i <= i0 || len(outro) == 0 {
		return false
	}
	a, _ := utf8.DecodeRuneInString(ks[i-1])
	b, _ := utf8.DecodeRuneInString(outro[0])
	return a == b
}

// ligacaoNorm são as palavras de classe fechada que pertencem à frase, não a
// um termo, na forma de chave (sem acento): as de diff.go (artigos,
// preposições e suas contrações) mais as conjunções e as preposições que
// faltavam lá. "a" cobre "à"; "e" cobre "é".
var ligacaoNorm = func() map[string]bool {
	m := map[string]bool{}
	for k := range ligacao {
		m[dictionary.Normalize(k)] = true
	}
	for _, k := range []string{
		"por", "para", "pra", "com", "sem", "sobre", "ate", "entre",
		"e", "ou", "que", "se", "mas", "nem",
	} {
		m[k] = true
	}
	return m
}()

// soLigacao é verdadeiro quando toda palavra de ks é de ligação.
func soLigacao(ks []string) bool {
	for _, k := range ks {
		if !ligacaoNorm[k] {
			return false
		}
	}
	return true
}

// temNumero é verdadeiro quando alguma palavra de ks é um número: só dígitos,
// ou dígitos com o º ou o ª do ordinal ("1º"). "n8n" e "v2" não são: o que a
// spec exige (TEST-008) é "n oito n" virar "n8n".
func temNumero(ks []string) bool {
	for _, k := range ks {
		if ehNumero(k) {
			return true
		}
	}
	return false
}

// ehNumero é verdadeiro quando k é só dígitos, com ou sem o º ou o ª do
// ordinal no fim. As chaves nunca são vazias (toda palavra tem ao menos uma
// letra ou dígito), mas "º" sozinho não é número.
func ehNumero(k string) bool {
	digitos := strings.TrimRight(k, "ºª")
	if digitos == "" {
		return false
	}
	for _, r := range digitos {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// mudaDigito é verdadeiro quando o De tem dígito, mesmo grudado em letra, e o
// Para não tem os mesmos dígitos, na mesma ordem: "10h" para "11h", "v2" para
// "v3", "1º" para "2º". Um alias assim trocaria número em todo texto futuro.
// O De sem dígito não entra: "n oito n" virando "n8n" (TEST-008) continua
// valendo.
func mudaDigito(kDe, kPara []string) bool {
	de := sequenciasDeDigitos(kDe)
	if len(de) == 0 {
		return false
	}
	para := sequenciasDeDigitos(kPara)
	if len(de) != len(para) {
		return true
	}
	for i := range de {
		if de[i] != para[i] {
			return true
		}
	}
	return false
}

// sequenciasDeDigitos devolve as sequências de dígitos de ks, em ordem: "v2"
// e "n8n" dão ["2"] e ["8"], "10h" dá ["10"].
func sequenciasDeDigitos(ks []string) []string {
	var out []string
	for _, k := range ks {
		out = append(out, strings.FieldsFunc(k, func(r rune) bool { return !unicode.IsDigit(r) })...)
	}
	return out
}

// numeraisPorExtenso são os números escritos em palavras, na forma de chave
// (sem acento): a lista do Polish (resolver.numeraisPorExtenso) mais os
// ordinais de primeiro a décimo, com o feminino. "um" e "uma" ficam de fora:
// são artigos antes de serem número, e a lista de palavras de ligação já os
// barra. O teste TestNumeraisAcompanhamOsDoResolver, no pacote resolver, avisa
// se a lista do Polish ganhar palavra que esta não tem.
var numeraisPorExtenso = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(
		"zero um uma dois duas três quatro cinco seis sete oito nove dez onze doze treze " +
			"catorze quatorze quinze dezesseis dezessete dezoito dezenove vinte trinta quarenta " +
			"cinquenta sessenta setenta oitenta noventa cem cento duzentos duzentas trezentos " +
			"trezentas quatrocentos quatrocentas quinhentos quinhentas seiscentos seiscentas " +
			"setecentos setecentas oitocentos oitocentas novecentos novecentas mil milhão milhões " +
			"bilhão bilhões meio meia " +
			"primeiro primeira segundo segunda terceiro terceira quarto quarta quinto quinta " +
			"sexto sexta sétimo sétima oitavo oitava nono nona décimo décima") {
		if k := dictionary.Normalize(w); !ligacaoNorm[k] {
			m[k] = true
		}
	}
	return m
}()

// numeraisDe devolve os numerais por extenso de ks, em ordem.
func numeraisDe(ks []string) []string {
	var out []string
	for _, k := range ks {
		if numeraisPorExtenso[k] {
			out = append(out, k)
		}
	}
	return out
}

// mudaNumeral é verdadeiro quando o alias mudaria o valor de um número escrito
// por extenso. O numeral do De tem de sobreviver no Para, e isso só se sabe
// quando o Para tem dígito, porque "n oito n" virando "n8n" (TEST-008) e "vê
// dois" virando "v2" são o que a spec pede. Então barra:
//
//   - numeral nos dois lados: "oito" para "seis", "mil" para "milhão", "dois"
//     para "duas". Numeral igual nos dois lados não chega aqui, porque o
//     alinhamento já o emparelha e ele fica fora da troca;
//   - De que é só numeral (e palavra de ligação: "mil e quinhentos") e um Para
//     sem dígito: o número virou uma palavra qualquer.
//
// O numeral dentro de um nome soletrado ("k oito s" para "Kubernetes") não
// barra: o alias só vale para aquele nome, e não para o número.
func mudaNumeral(kDe, kPara []string) bool {
	if len(numeraisDe(kDe)) == 0 {
		return false
	}
	if len(numeraisDe(kPara)) > 0 {
		return true
	}
	return !temDigito(kPara) && soNumeralELigacao(kDe)
}

// soNumeralELigacao é verdadeiro quando toda palavra de ks é numeral por
// extenso ou de ligação.
func soNumeralELigacao(ks []string) bool {
	for _, k := range ks {
		if !numeraisPorExtenso[k] && !ligacaoNorm[k] {
			return false
		}
	}
	return true
}

// temDigito é verdadeiro quando alguma palavra de ks tem um dígito.
func temDigito(ks []string) bool {
	for _, k := range ks {
		if strings.IndexFunc(k, unicode.IsDigit) >= 0 {
			return true
		}
	}
	return false
}

// polaridade são as palavras que invertem ou mudam o sentido da frase, na
// forma de chave (sem acento). Um alias com uma delas em qualquer lado nunca é
// a correção de um termo: "não" para "sim", "nunca" para "sempre", "mais" para
// "mas" (que o ouvido confunde). É a lista de palavras protegidas do Polish
// (resolver.protegidas), sem o "sem", que já é palavra de ligação. O teste
// TestPolaridadeAcompanhaProtegidasDoResolver, no pacote resolver, avisa se as
// duas se separarem.
var polaridade = map[string]bool{
	"nao": true, "nunca": true, "nem": true, "nada": true, "ninguem": true, "jamais": true,
	"nenhum": true, "nenhuma": true, "nenhuns": true, "nenhumas": true,
	"sempre": true, "sim": true, "so": true, "somente": true, "apenas": true,
	"mais": true, "menos": true, "todo": true, "toda": true, "todos": true, "todas": true, "tudo": true,
}

// temPolaridade é verdadeiro quando alguma palavra de ks inverte o sentido.
func temPolaridade(ks []string) bool {
	for _, k := range ks {
		if polaridade[k] {
			return true
		}
	}
	return false
}

// grande é verdadeiro quando o trecho passa dos tetos de "pequena e
// mapeável": 4 palavras e 30 caracteres (os mesmos de Classify).
func grande(trecho string) bool {
	s := compacta(trecho)
	return len(strings.Fields(s)) > maxAliasWords || utf8.RuneCountInString(s) > maxAliasChars
}

// cruzaFrase é verdadeiro quando o trecho tem quebra de linha ou um fim de
// frase seguido de espaço. Ponto no meio de "Node.js" ou "S.A." não conta.
func cruzaFrase(trecho string) bool { return fimDeFrase.MatchString(trecho) }

// fimDeFrase acha uma quebra de linha, ou um sinal de fim de frase (os de
// Classify, mais as reticências) seguido de espaço.
var fimDeFrase = regexp.MustCompile(`[\r\n]|[` + regexp.QuoteMeta(sentenceTerminators+"…") + `]\s`)

// OcorrenciasEm conta em quantos dos textos brutos a forma de aparece, como
// palavras inteiras seguidas e ignorando maiúscula, acento e pontuação:
// "sf" está em "mandei o SF." mas não em "sfera". Cada texto conta uma vez.
//
// É o que um ContadorOcorrencias usa sobre o histórico. Quem chama decide o
// que passar (em geral, o bruto dos outros ditados, sem o que acabou de ser
// editado).
func OcorrenciasEm(de string, brutos []string) int {
	alvo := chaves(tokenizar(de))
	if len(alvo) == 0 {
		return 0
	}
	n := 0
	for _, b := range brutos {
		if contem(chaves(tokenizar(b)), alvo) {
			n++
		}
	}
	return n
}

// contem diz se sub aparece em ks, como palavras seguidas.
func contem(ks, sub []string) bool {
	for i := 0; i+len(sub) <= len(ks); i++ {
		achou := true
		for j, s := range sub {
			if ks[i+j] != s {
				achou = false
				break
			}
		}
		if achou {
			return true
		}
	}
	return false
}
