package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	_ "modernc.org/sqlite"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dictlearn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// O padrão é o dicionário do REPOSITÓRIO. O que o Agent usa é
	// bin/dicionario.json, que o asrstage só copia na primeira montagem (e
	// depois preserva, porque o painel grava nele): para ensinar ao que o
	// Agent usa, -dict bin/dicionario.json.
	caminho := fs.String("dict", "testdata/asr-dataset/dicionario.json",
		"dicionário pessoal (JSON); o que o Agent usa é bin/dicionario.json, e o asrstage só o copia daqui na primeira montagem")
	saiu := fs.String("saiu", "", "o que o ditado produziu — termo ou frase (obrigatório)")
	queria := fs.String("queria", "", "o que deveria ter produzido (obrigatório)")
	restrito := fs.Bool("restrito", false,
		"o alias colide com palavra comum e só deve valer no domínio do termo (ex.: comite/commit)")
	confirmar := fs.Bool("confirmar", false, "confirma sem perguntar (o \"sim\" explícito de quem chama)")
	doUltimo := fs.Bool("do-ultimo", false,
		"preenche -saiu com o texto BRUTO do último ditado registrado, em vez de digitá-lo de memória")
	banco := fs.String("historico", "", "banco do histórico local (vazio: historico.db ao lado do agent)")
	// Sem padrão fixo desde a revisão de 29-set-2026: o padrão era
	// "mauro". Hoje o Agent grava com o dono fixo da instalação, que mora
	// no próprio banco — ver donoDoHistorico.
	usuario := fs.String("usuario", "",
		"dono do histórico lido por -do-ultimo (vazio: o mesmo do Agent — o dono fixo gravado no banco; "+
			"em banco anterior a ele, o usuario do -dict, ou \""+donoSemDicionario+"\" sem dicionário)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *doUltimo {
		// Antes de ler qualquer coisa: a combinação é inválida seja qual
		// for o dicionário ou o banco.
		if strings.TrimSpace(*saiu) != "" {
			fmt.Fprintln(stderr, "dictlearn: -do-ultimo e -saiu são exclusivos — o ponto do primeiro é não digitar o segundo")
			return 2
		}
		db, caminhoBanco, err := abrirHistorico(*banco)
		if err != nil {
			fmt.Fprintf(stderr, "dictlearn: %v\n", err)
			return 1
		}
		defer db.Close()
		dono, origem, aviso, err := donoDoHistorico(context.Background(), *usuario, *caminho, db, caminhoBanco)
		if err != nil {
			fmt.Fprintf(stderr, "dictlearn: decidindo de quem é o histórico: %v\n", err)
			return 1
		}
		if aviso != "" {
			fmt.Fprintf(stderr, "dictlearn: aviso: %s\n", aviso)
		}
		bruto, quando, err := ultimoBruto(db, dono)
		if errors.Is(err, errSemDitado) {
			// O banco tem ditados, mas não deste dono: dizer de onde o
			// dono veio é o que aponta o descompasso com o Agent.
			fmt.Fprintf(stderr, "dictlearn: %v (%s); para ler outro dono: -usuario\n", err, origem)
			return 1
		}
		if err != nil {
			fmt.Fprintf(stderr, "dictlearn: %v\n", err)
			return 1
		}
		*saiu = bruto
		// A data e a hora vão junto porque "o último" é relativo ao dono:
		// o ditado mais recente DESTE dono pode ser de ontem, se o de
		// agora foi gravado com outro. Sem a data, um ditado velho passava
		// por "o que acabei de falar" (revisão de 29-set-2026).
		fmt.Fprintf(stdout, "  do último ditado de %q (%s), em %s:\n  %q\n\n",
			dono, origem, quandoLegivel(quando), bruto)
	}

	if strings.TrimSpace(*saiu) == "" || strings.TrimSpace(*queria) == "" {
		fmt.Fprintln(stderr, "dictlearn: -saiu e -queria são obrigatórios")
		fs.Usage()
		return 2
	}

	// 1. Classificar. É internal/learning que decide se a edição é
	//    pequena o bastante para virar alias — "edição grande nunca vira
	//    alias silenciosamente" (07-flows.md).
	//
	//    Os dois argumentos são o TERMO, não a frase, e isso não é
	//    detalhe de ergonomia: learning.Suggestion usa Before/After
	//    INTEIROS como alias e forma canônica. Passar "esse comite
	//    quebrou" criaria um alias da frase de três palavras, que só
	//    casaria se o ASR produzisse exatamente ela de novo — quase
	//    nunca. Passar "comite" cria o alias que serve para sempre.
	sug, ok := learning.Classify(*saiu, *queria)
	if !ok {
		fmt.Fprintln(stdout, "nada a aprender: os dois textos são equivalentes (só espaço ou pontuação diferem).")
		return 0
	}

	fmt.Fprintf(stdout, "  saiu:   %s\n", sug.Before)
	fmt.Fprintf(stdout, "  queria: %s\n", sug.After)
	fmt.Fprintf(stdout, "\n  classificação: %s\n", sug.Reason)

	if !sug.AliasEligible {
		// Isto não é falha. A edição é grande demais para virar uma
		// regra de substituição — vira Correction (histórico), que este
		// comando ainda não persiste. Dizer isso é melhor que gravar um
		// alias que corrigiria frases inteiras.
		fmt.Fprintln(stdout, "\n  Esta edição NÃO vira alias de dicionário: é grande demais para")
		fmt.Fprintln(stdout, "  virar uma regra de substituição, e uma regra assim corrigiria")
		fmt.Fprintln(stdout, "  frases que não têm nada a ver com esta.")
		return 0
	}

	// 2. Carregar o dicionário ANTES de pedir confirmação: não faz
	//    sentido perguntar "pode gravar?" e só então descobrir que o
	//    arquivo não existe.
	semente, err := carregar(*caminho)
	if err != nil {
		fmt.Fprintln(stderr, "dictlearn:", err)
		return 1
	}

	fmt.Fprintf(stdout, "\n  vai virar alias: %q -> %q\n", sug.AliasFrom, sug.AliasTo)

	// O aviso de distância é a informação que substitui o limiar
	// automático que NÃO existe — ver learning.Suggestion.Similaridade.
	//
	// "certo" -> "errado" tem 0,33 de proximidade; "n oito n" -> "n8n",
	// que a spec EXIGE que seja aprendido (TEST-008), tem 0,25. Não há
	// corte que aceite um e recuse o outro. Quem decide é quem confirma,
	// e sem este aviso ele decidiria sem o dado que importa.
	if sug.Similaridade < learning.SimilaridadeSuspeita {
		fmt.Fprintf(stdout, "\n  ATENÇÃO: os dois termos são distantes (%.2f de proximidade).\n", sug.Similaridade)
		fmt.Fprintln(stdout, "  Isso costuma significar MUDANÇA DE IDEIA, não erro de transcrição —")
		fmt.Fprintf(stdout, "  e um alias faz TODO ditado futuro com %q virar %q.\n", sug.AliasFrom, sug.AliasTo)
		fmt.Fprintln(stdout, "  Confirme só se for mesmo o ASR ouvindo errado a mesma fala.")
	}
	if *restrito {
		fmt.Fprintln(stdout, "  RESTRITO: só vale dentro do domínio do termo (ex.: só num editor de código)")
	}

	// 3. Confirmação explícita. É o AC-022, e é a única porta para
	//    gravar qualquer coisa.
	if !*confirmar {
		if !perguntar(stdin, stdout) {
			fmt.Fprintln(stdout, "cancelado. Nada foi gravado.")
			return 0
		}
	}

	// 4. learning.Confirm é quem grava — e recusa sem a Confirmation.
	//    Usar o pacote real, e não repetir a regra aqui, é o que faz a
	//    garantia do AC-022 valer para este caminho também.
	repoDic := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := semente.Popular(context.Background(), repoDic); err != nil {
		fmt.Fprintln(stderr, "dictlearn:", err)
		return 1
	}
	_, err = learning.Confirm(context.Background(),
		learning.Deps{
			Corrections: learning.NewInMemoryCorrectionRepository(),
			Dictionary:  repoDic,
		},
		learning.ConfirmInput{UserID: semente.Usuario, Suggestion: sug},
		learning.ConfirmedByUser("dictlearn"))
	if err != nil {
		fmt.Fprintln(stderr, "dictlearn: aprendizado recusado:", err)
		return 1
	}

	// 5. Persistir. O passo 4 gravou em MEMÓRIA; sem isto o alias morre
	//    no fim do processo — aprendizado que não sobrevive ao restart é
	//    esquecimento com passos extras.
	//
	//    O arquivo é RELIDO aqui, depois da confirmação, e não se grava a
	//    cópia do passo 2: a pergunta espera uma pessoa, e com
	//    -dict bin/dicionario.json o painel do Agent pode ter gravado uma
	//    variante nesse meio tempo (um clique em Aprender). Gravar a cópia
	//    antiga apagaria o que ele ensinou, e o Agent continuaria usando a
	//    variante em memória até reiniciar. A janela que sobra é a de
	//    ler-e-gravar, do tamanho de uma escrita em disco.
	atual, err := carregar(*caminho)
	if err != nil {
		fmt.Fprintln(stderr, "dictlearn:", err)
		return 1
	}
	if atual.Usuario != semente.Usuario {
		fmt.Fprintln(stderr, "dictlearn: o dono do dicionário mudou enquanto esperava a confirmação; nada foi gravado")
		return 1
	}
	atualizada, err := atual.AcrescentarAlias(sug.AliasTo, sug.AliasFrom, *restrito)
	if err != nil {
		fmt.Fprintln(stderr, "dictlearn:", err)
		return 1
	}
	if err := atualizada.Gravar(*caminho); err != nil {
		fmt.Fprintln(stderr, "dictlearn:", err)
		return 1
	}

	fmt.Fprintf(stdout, "\naprendido. %q agora vira %q.\n", sug.AliasFrom, sug.AliasTo)
	fmt.Fprintf(stdout, "Gravado em %s — dá para conferir o que mudou com `git diff`.\n", *caminho)
	return 0
}

func carregar(caminho string) (dictionary.Semente, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return dictionary.Semente{}, err
	}
	defer f.Close()
	return dictionary.CarregarSemente(f)
}

// perguntar lê o "sim" de quem está rodando. Só "s" ou "sim" confirmam:
// Enter vazio, qualquer outra tecla, ou fim de entrada são "não".
//
// O default é NÃO de propósito. Um prompt em que apertar Enter grava é
// um prompt que não pergunta nada.
func perguntar(stdin io.Reader, stdout io.Writer) bool {
	fmt.Fprint(stdout, "\nConfirma? [s/N] ")
	linha, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(linha) == "" {
		return false
	}
	r := strings.ToLower(strings.TrimSpace(linha))
	return r == "s" || r == "sim"
}

// errSemDitado: o banco existe, mas o dono pedido não tem ditado nele.
var errSemDitado = errors.New("nenhum ditado registrado")

// ultimoBruto lê o texto do ASR do ditado mais recente no histórico
// local.
//
// # Por que isto existe
//
// Até 28-set-2026 o "saiu" só podia vir do que a pessoa digitava de
// memória — e ninguém decora a grafia exata de um erro que viu por meio
// segundo. Mauro ensinou "Wissper" e o ASR produzia "Whisperflow": o
// alias cobria uma forma que provavelmente nunca mais ocorreria.
//
// db vem de abrirHistorico, somente leitura.
//
// Devolve também QUANDO o ditado foi gravado: o mais recente de um dono
// não é necessariamente o que acabou de acontecer, e quem vai confirmar
// um alias precisa ver isso (revisão de 29-set-2026).
func ultimoBruto(db *sql.DB, usuario string) (string, time.Time, error) {
	pagina, err := storage.NewSQLiteTranscriptionRepository(db).List(context.Background(),
		storage.TranscriptionListFilter{UserID: usuario, Limit: 1})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("lendo o histórico: %w", err)
	}
	if len(pagina.Items) == 0 {
		return "", time.Time{}, fmt.Errorf("%w para o usuário %q", errSemDitado, usuario)
	}
	ultimo := pagina.Items[0]
	bruto := strings.TrimSpace(ultimo.RawText)
	if bruto == "" {
		return "", time.Time{}, fmt.Errorf("o último ditado não tem texto bruto registrado")
	}
	return bruto, ultimo.CreatedAt, nil
}

// abrirHistorico abre o histórico local em modo SOMENTE LEITURA, e diz
// qual arquivo abriu. caminho vazio é o historico.db ao lado do
// executável, onde o Agent o grava.
//
// # Por que o banco, e não a Local API do Agent
//
// A API exige token, e o token só existe em memória — nunca em disco
// nem em log (09-security-privacy.md). Publicá-lo para este comando ler
// violaria a decisão. O banco é lido direto, protegido pela permissão
// do sistema de arquivos, sem superfície de rede nova.
//
// Somente leitura porque este comando ENSINA o dicionário, não mexe no
// histórico — nem migra o dono dele, que é trabalho do Agent —, e o Agent
// pode estar escrevendo ali agora.
func abrirHistorico(caminho string) (*sql.DB, string, error) {
	if strings.TrimSpace(caminho) == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, "", fmt.Errorf("localizando o executável: %w", err)
		}
		caminho = filepath.Join(filepath.Dir(exe), "historico.db")
	}
	if _, err := os.Stat(caminho); err != nil {
		return nil, "", fmt.Errorf("histórico não encontrado em %s — o Agent já rodou com esta versão? (%w)", caminho, err)
	}
	db, err := storage.AbrirSomenteLeitura(caminho)
	if err != nil {
		return nil, "", err
	}
	return db, caminho, nil
}

// donoSemDicionario é o dono que o Agent usava, antes do dono fixo, quando
// não havia dicionário (cmd/agent/main.go, donoPelaRegraAntiga).
const donoSemDicionario = "local"

// donoDoHistorico decide de quem é o histórico que -do-ultimo lê, e diz
// de onde tirou a resposta — a origem vai para a tela, porque um dono
// errado só se diagnostica sabendo por que ele foi escolhido.
//
// A regra é a MESMA com que o Agent GRAVA (cmd/agent/main.go,
// escolherDono):
//
//  1. -usuario, quando dado, vence tudo: é a sobrescrita explícita.
//  2. O dono fixo gravado no banco (decisão do Mauro, 29-set-2026). O
//     dicionário não entra: trocar o "usuario" dele não troca mais de
//     quem é o histórico.
//  3. Banco sem dono fixo — anterior a esta versão, sem a tabela, ou um em
//     que a migração ainda não rodou —: a regra antiga, com aviso, que o
//     Agent também usa nesse caso. O usuario do dicionário e, sem
//     dicionário, "local"; dicionário presente mas inválido é erro, como
//     no Agent: alguém tentou configurar um dono, e adivinhar outro seria
//     pior que parar.
//
// Até a revisão de 29-set-2026 o dono era "mauro" fixo e o -dict só era
// lido DEPOIS do histórico. Com o Agent rodando sem dicionário — o jeito
// de ver o ASR cru — ou com um dicionário de outro dono, -do-ultimo
// devolvia o ditado mais recente de "mauro", que podia ser de ontem,
// como se fosse o que acabou de acontecer.
func donoDoHistorico(ctx context.Context, explicito, caminhoDict string, db *sql.DB, caminhoBanco string) (dono, origem, aviso string, err error) {
	if u := strings.TrimSpace(explicito); u != "" {
		return u, "dado por -usuario", "", nil
	}

	fixo, err := storage.LerDonoFixo(ctx, db)
	if err == nil {
		return fixo, "o dono fixo gravado em " + caminhoBanco, "", nil
	}
	if !errors.Is(err, storage.ErrSemDonoFixo) {
		return "", "", "", fmt.Errorf("lendo o dono fixo de %s: %w", caminhoBanco, err)
	}

	aviso = caminhoBanco + " ainda não tem dono fixo — o Agent desta versão ainda não subiu com ele, " +
		"ou a migração não rodou (veja o agent.log); usando a regra antiga"
	semente, err := carregar(caminhoDict)
	switch {
	case err == nil:
		// CarregarSemente recusa semente sem usuario: aqui ele existe.
		return semente.Usuario, "usuario de " + caminhoDict + ", regra antiga", aviso, nil
	case errors.Is(err, os.ErrNotExist):
		return donoSemDicionario, "sem dicionário em " + caminhoDict + ", regra antiga", aviso, nil
	default:
		return "", "", "", err
	}
}

// quandoLegivel formata o instante de um ditado no fuso de quem está na
// frente do terminal, com segundos: dois ditados do mesmo minuto
// precisam ser distinguíveis.
func quandoLegivel(t time.Time) string {
	return t.Local().Format("02/01/2006 15:04:05")
}
