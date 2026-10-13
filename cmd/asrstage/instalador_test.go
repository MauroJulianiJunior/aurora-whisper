package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/autostart"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/instancia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// Compilar o instalador (installer/aurora-whisper.iss) exige o Inno Setup, que
// não está em toda máquina e não roda na suíte normal. O que dá para travar sem
// ele é o que o script REPETE do Go: o nome do mutex, as pastas, as chaves, o
// modelo, as DLLs, o autostart. Se um lado mudar e o outro não, o instalador
// passa a fechar o Agent errado, apagar a pasta errada ou levar o arquivo
// errado, e nenhum outro teste veria. Estes testes só LEEM os arquivos: nada é
// executado, instalado ou gravado.

const (
	caminhoDoIss      = "../../installer/aurora-whisper.iss"
	caminhoDoScript   = "../../scripts/montar-instalador.ps1"
	caminhoDoWorkflow = "../../.github/workflows/release.yml"
	caminhoDaADR      = "../../docs/adr/ADR-018-instalador.md"
)

// lerBruto devolve os bytes do arquivo do repositório.
func lerBruto(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatalf("lendo %s: %v", rel, err)
	}
	return b
}

// lerTexto devolve o arquivo sem o BOM e com as quebras de linha em \n (o
// .gitattributes põe CRLF no .ps1).
func lerTexto(t *testing.T, rel string) string {
	t.Helper()
	b := bytes.TrimPrefix(lerBruto(t, rel), []byte{0xEF, 0xBB, 0xBF})
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// definicoes lê os "#define Nome valor" do .iss (com ou sem aspas no valor).
func definicoes(iss string) map[string]string {
	re := regexp.MustCompile(`(?m)^\s*#define\s+(\w+)\s+"?([^"\n]*?)"?\s*$`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(iss, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// secao devolve as linhas de [nome] até a próxima seção, sem as de comentário.
func secao(iss, nome string) []string {
	var linhas []string
	dentro := false
	for _, l := range strings.Split(iss, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			dentro = strings.EqualFold(t, "["+nome+"]")
			continue
		}
		if dentro && t != "" && !strings.HasPrefix(t, ";") {
			linhas = append(linhas, t)
		}
	}
	return linhas
}

func TestIss_UTF8ComBOMEAcentosIntactos(t *testing.T) {
	b := lerBruto(t, caminhoDoIss)
	if !bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("o .iss perdeu o BOM UTF-8: o Inno o leria como ANSI e os acentos das mensagens sairiam trocados")
	}
	if !utf8.Valid(b) {
		t.Error("o .iss não é UTF-8 válido")
	}
	if !strings.Contains(string(b), "Não foi possível") {
		t.Error("as mensagens em português perderam os acentos")
	}
}

// O .ps1 é só ASCII: o PowerShell 5.1 lê UTF-8 sem BOM como ANSI, e um acento
// viraria lixo. Com BOM seria seguro, mas um editor que o perdesse quebraria o
// script em silêncio; sem acento, não há como quebrar.
func TestScript_SoASCII(t *testing.T) {
	for i, c := range lerBruto(t, caminhoDoScript) {
		if c > 0x7F {
			t.Fatalf("scripts/montar-instalador.ps1 tem um byte não-ASCII (0x%02X, posição %d)", c, i)
		}
	}
}

func TestIss_AsDLLsSaoAsDoAsrstage(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	def := definicoes(iss)
	re := regexp.MustCompile(`^Source:\s*"\{#PastaStaging\}\\([^\\"*]+)"`)
	var arquivos, dlls []string
	for _, l := range secao(iss, "Files") {
		m := re.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		nome := strings.NewReplacer("{#NomeDoExe}", def["NomeDoExe"], "{#ArquivoDoModelo}", def["ArquivoDoModelo"]).Replace(m[1])
		arquivos = append(arquivos, nome)
		if strings.HasSuffix(nome, ".dll") {
			dlls = append(dlls, nome)
		}
	}
	quer := append([]string(nil), dllsNecessarias...)
	sort.Strings(quer)
	sort.Strings(dlls)
	if strings.Join(dlls, ",") != strings.Join(quer, ",") {
		t.Errorf("as DLLs do .iss:\n  %s\nas do asrstage (dllsNecessarias):\n  %s", strings.Join(dlls, "\n  "), strings.Join(quer, "\n  "))
	}
	// O resto da lista explícita: o Agent, o servidor e o modelo.
	for _, quer := range []string{nomeDoExeDoAgent, "whisper-server.exe", model.SmallQ4_0.Arquivo} {
		achou := false
		for _, a := range arquivos {
			achou = achou || a == quer
		}
		if !achou {
			t.Errorf("o .iss não leva %s", quer)
		}
	}
	if len(arquivos) != len(dllsNecessarias)+3 {
		t.Errorf("o .iss leva %d arquivos soltos (%v); quero as %d DLLs, o Agent, o servidor e o modelo", len(arquivos), arquivos, len(dllsNecessarias))
	}
}

// Sem coringa na raiz do staging: só o painel e as licenças vêm por pasta.
func TestIss_SemCoringaNaRaizDoStaging(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	for _, l := range secao(iss, "Files") {
		if !strings.HasPrefix(l, "Source:") || !strings.Contains(l, "*") {
			continue
		}
		if !strings.Contains(l, `{#PastaStaging}\painel\*`) && !strings.Contains(l, `{#PastaStaging}\licencas\*`) {
			t.Errorf("coringa na raiz do staging: %s", l)
		}
	}
	if !strings.Contains(iss, `Source: "{#PastaStaging}\painel\*"`) || !strings.Contains(iss, `Source: "{#PastaStaging}\licencas\*"`) {
		t.Error("o .iss deixou de levar o painel ou a pasta licencas")
	}
}

// O Setup nunca leva dado de ninguém: nenhuma linha de arquivos ou de
// InstallDelete cita o dicionário, o banco ou o marcador do modo portátil.
func TestIss_NaoLevaDadosNemMarcador(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	linhas := append(secao(iss, "Files"), secao(iss, "InstallDelete")...)
	linhas = append(linhas, secao(iss, "Run")...)
	for _, l := range linhas {
		baixo := strings.ToLower(l)
		for _, proibido := range []string{"dicionario", "historico", pastas.Marcador, ".log"} {
			if strings.Contains(baixo, proibido) {
				t.Errorf("o .iss cita %q onde só vai o programa: %s", proibido, l)
			}
		}
	}
}

// Cada nome que o .iss repete do Go é o do Go.
func TestIss_NomesIguaisAoGo(t *testing.T) {
	def := definicoes(lerTexto(t, caminhoDoIss))
	alvoOpenAI, err := chaves.AlvoDe(ia.OpenAI)
	if err != nil {
		t.Fatal(err)
	}
	alvoAnthropic, err := chaves.AlvoDe(ia.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	for nome, quer := range map[string]string{
		"NomeDoExe":            nomeDoExeDoAgent,
		"MutexDoAgent":         instancia.NomeDoAgent,
		"PastaDeDados":         pastas.NomeDaPastaDeDados,
		"ArquivoDoModelo":      model.SmallQ4_0.Arquivo,
		"ChaveRun":             autostart.ChaveRun,
		"ChaveAprovados":       autostart.ChaveAprovados,
		"ValorDoRun":           autostart.NomeDoValor,
		"AlvoDaChaveOpenAI":    alvoOpenAI,
		"AlvoDaChaveAnthropic": alvoAnthropic,
		// O token do PC na nuvem: o alvo do modo instalado (F6).
		"AlvoDaChaveNuvem": chaves.PrefixoDoAlvo + nuvem.NomeNoCofreInstalado,
	} {
		if def[nome] != quer {
			t.Errorf("#define %s é %q no .iss, e o Go diz %q", nome, def[nome], quer)
		}
	}
	// O nome do executável também é o do Agent (cmd/agent, appName + ".exe").
	if main := lerTexto(t, "../agent/main.go"); !strings.Contains(main, `const appName = "`+strings.TrimSuffix(nomeDoExeDoAgent, ".exe")+`"`) {
		t.Errorf("cmd/agent não chama o executável de %s", nomeDoExeDoAgent)
	}
	// E a mensagem do asrstage manda compilar com o mesmo nome.
	if !strings.Contains(proximosPassos("r", "d"), "/"+nomeDoExeDoAgent+" ") {
		t.Error("a mensagem do asrstage manda compilar o Agent com outro nome")
	}
	if app := def["NomeDoApp"]; app != "Aurora Whisper" {
		t.Errorf("NomeDoApp = %q", app)
	}
}

// Por usuário, sem administrador, o Agent aberto ao terminar, e o que o
// instalador NÃO faz (decisão 3: não liga o início com o Windows).
func TestIss_PorUsuarioENaoLigaOInicioComOWindows(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	setup := strings.Join(secao(iss, "Setup"), "\n")
	for _, quer := range []string{
		"PrivilegesRequired=lowest",
		`DefaultDirName={localappdata}\Programs\{#NomeDoApp}`,
		"CloseApplications=no",
		"ArchitecturesAllowed=x64compatible",
		"AppVersion={#VersaoDoApp}",
		"OutputBaseFilename=AuroraWhisper-Setup-{#VersaoDoApp}",
	} {
		if !strings.Contains(setup, quer) {
			t.Errorf("[Setup] não tem %s", quer)
		}
	}
	if strings.Contains(setup, "PrivilegesRequired=admin") {
		t.Error("o instalador pede administrador")
	}

	// Decisão 3: nenhuma seção [Registry], e nenhuma escrita no registro em [Code].
	// (Um comentário pode citar a seção; o que conta é uma linha que a abre.)
	for _, l := range strings.Split(iss, "\n") {
		if strings.EqualFold(strings.TrimSpace(l), "[registry]") {
			t.Error("o .iss tem uma seção [Registry]: o instalador não liga o Iniciar com o Windows (ADR-018, decisão 3)")
		}
	}
	if strings.Contains(iss, "RegWrite") {
		t.Error("o .iss escreve no registro: o instalador não liga o Iniciar com o Windows")
	}

	// "Abrir agora": marcado por padrão (sem unchecked) e fora do modo silencioso.
	run := strings.Join(secao(iss, "Run"), "\n")
	if !strings.Contains(run, "postinstall") || !strings.Contains(run, "skipifsilent") || strings.Contains(run, "unchecked") {
		t.Errorf("a entrada [Run] não é o 'abrir agora' marcado: %s", run)
	}
}

func TestIss_AtalhoNoMenuIniciarEDesktopSoSeMarcar(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	var menu, desktop string
	for _, l := range secao(iss, "Icons") {
		switch {
		case strings.Contains(l, "{autoprograms}"):
			menu = l
		case strings.Contains(l, "{autodesktop}"):
			desktop = l
		}
	}
	if menu == "" || strings.Contains(menu, "Tasks:") {
		t.Errorf("o atalho do Menu Iniciar sumiu ou depende de uma tarefa: %q", menu)
	}
	if desktop == "" || !strings.Contains(desktop, "Tasks: desktopicon") {
		t.Errorf("o atalho da área de trabalho não depende da tarefa desktopicon: %q", desktop)
	}
	tarefas := strings.Join(secao(iss, "Tasks"), "\n")
	if !strings.Contains(tarefas, `Name: "desktopicon"`) || !strings.Contains(tarefas, "unchecked") {
		t.Errorf("o atalho na área de trabalho não vem desmarcado: %s", tarefas)
	}
}

// O Agent fecha como "Sair" (taskkill SEM /F), e o Setup espera o mutex sumir,
// com teto.
func TestIss_FechaOAgentSemForcarEEsperaOMutex(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	i := strings.Index(iss, `taskkill.exe'),`)
	if i < 0 {
		t.Fatal("o .iss não chama o taskkill")
	}
	fim := strings.Index(iss[i:], "ewWaitUntilTerminated")
	chamada := iss[i : i+fim]
	if !strings.Contains(chamada, "/IM {#NomeDoExe}") {
		t.Errorf("o taskkill não mira o executável do Agent: %s", chamada)
	}
	if regexp.MustCompile(`(?i)/f(\s|"|')`).MatchString(chamada) {
		t.Errorf("o taskkill força o fechamento (/F): o Agent perderia o que está gravando: %s", chamada)
	}
	if !strings.Contains(iss, "CheckForMutexes('{#MutexDoAgent}')") {
		t.Error("o .iss não espera o mutex do Agent")
	}
	m := regexp.MustCompile(`EsperaParaFecharMs\s*=\s*(\d+);`).FindStringSubmatch(iss)
	if m == nil {
		t.Fatal("o .iss não tem o teto da espera (EsperaParaFecharMs)")
	}
	if ms, _ := strconv.Atoi(m[1]); ms < 5000 || ms > 120000 {
		t.Errorf("o teto de espera é %d ms: tem de ser de alguns segundos, e finito", ms)
	}
	if !strings.Contains(iss, "while AgentAberto() and (Esperou < EsperaParaFecharMs)") {
		t.Error("a espera pelo mutex perdeu o teto")
	}
	// O desinstalador também fecha o Agent antes de começar.
	if !regexp.MustCompile(`(?s)function InitializeUninstall\(\): Boolean;.*?FecharOAgent\(\)`).MatchString(iss) {
		t.Error("o desinstalador não fecha o Agent antes de começar")
	}
}

// Desinstalar: pergunta, o padrão é NÃO, e a única pasta que se apaga é a de
// dados, montada no código, com o Agent fechado.
func TestIss_DesinstaladorSoApagaAPastaDeDadosEDepoisDePerguntar(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	if n := strings.Count(iss, "DelTree("); n != 1 {
		t.Fatalf("o .iss tem %d chamadas de DelTree; só pode ter a da pasta de dados", n)
	}
	for _, quer := range []string{
		"DelTree(Pasta, True, True, True)",
		"Pasta := PastaDosDados();",
		"Result := AddBackslash(ExpandConstant('{localappdata}')) + '{#PastaDeDados}';",
		"MB_YESNO or MB_DEFBUTTON2",      // o padrão é Não
		"if UninstallSilent() then Exit", // sem pergunta, não apaga
		"if not PodeApagarOsDados(Pasta) then",
		"if AgentAberto() then", // confere que o Agent está fechado
		"EhLinkOuJuncao(Pasta)",
	} {
		if !strings.Contains(iss, quer) {
			t.Errorf("o .iss não tem %q", quer)
		}
	}
	// A pergunta vem antes de apagar, e a conferência do Agent e do caminho também.
	pergunta := strings.Index(iss, "MB_YESNO or MB_DEFBUTTON2")
	agente := strings.Index(iss, "if AgentAberto() then\n  begin\n    MsgBox(FmtMessage(CustomMessage('AgentAbertoNaoApagar')")
	caminho := strings.Index(iss, "if not PodeApagarOsDados(Pasta) then")
	apagar := strings.Index(iss, "DelTree(")
	if !(pergunta >= 0 && pergunta < agente && agente < caminho && caminho < apagar) {
		t.Errorf("a ordem tem de ser: perguntar (%d), conferir o Agent (%d), conferir o caminho (%d), apagar (%d)", pergunta, agente, caminho, apagar)
	}
	// As credenciais: só as três do programa (as duas chaves de API e, desde a
	// F6, o token do PC na nuvem), pelo nome exato, com a mesma chamada que o
	// Agent usa (CredDeleteW, tipo genérico), e não com o cmdkey. Só na opção
	// "apagar dados" (o golden de PerguntarEApagarOsDados fixa onde): apagar o
	// token sempre desconectaria o PC a cada atualização. E nunca o
	// nuvem-portatil, que é do bin\ de desenvolvimento, não instalado.
	codigo := semComentariosPascal(iss)
	if strings.Count(codigo, "ApagarChaveDeApi('") != 3 {
		t.Error("o desinstalador apaga credenciais além das três do programa")
	}
	for _, alvo := range []string{"AlvoDaChaveOpenAI", "AlvoDaChaveAnthropic", "AlvoDaChaveNuvem"} {
		if strings.Count(codigo, "ApagarChaveDeApi('{#"+alvo+"}');") != 1 {
			t.Errorf("o desinstalador não apaga {#%s} uma vez", alvo)
		}
	}
	if strings.Contains(codigo, "nuvem-portatil") {
		t.Error("o .iss mexe no token do bin\\ de desenvolvimento")
	}
	if !strings.Contains(iss, "AuroraWhisper/openai, AuroraWhisper/anthropic e AuroraWhisper/nuvem") {
		t.Error("a pergunta de apagar os dados não cita as três credenciais")
	}
	if !strings.Contains(codigo, "CredDeleteW(Alvo, CredTypeGeneric, 0)") || strings.Contains(strings.ToLower(codigo), "cmdkey") {
		t.Error("o desinstalador não apaga a chave pelo nome exato com CredDeleteW")
	}
	goChaves := lerTexto(t, "../../internal/agent/chaves/chaves_windows.go")
	if !strings.Contains(goChaves, `advapi32.NewProc("CredDeleteW")`) {
		t.Error("o Agent não apaga a chave com CredDeleteW: o .iss deixou de espelhar chaves.Apagar")
	}
	tipoGo := regexp.MustCompile(`credTypeGeneric\s*=\s*(\d+)`).FindStringSubmatch(goChaves)
	tipoIss := regexp.MustCompile(`CredTypeGeneric\s*=\s*(\d+);`).FindStringSubmatch(iss)
	if tipoGo == nil || tipoIss == nil || tipoGo[1] != tipoIss[1] || tipoGo[1] != "1" {
		t.Errorf("o tipo da credencial: Go %v, .iss %v; os dois têm de ser CRED_TYPE_GENERIC (1)", tipoGo, tipoIss)
	}
	// O início automático só se remove se apontar para o executável desinstalado.
	if !strings.Contains(iss, "if CompareText(Valor, Exe) <> 0 then Exit;") {
		t.Error("o desinstalador remove o início automático sem conferir que é o deste executável")
	}
}

// A pasta do programa nunca se mistura com a de dados.
func TestIss_ProgramaEDadosNaoSeMisturam(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	if !strings.Contains(iss, "Result := not (ContemOuIgual(PastaDosDados(), Dir) or ContemOuIgual(Dir, PastaDosDados()));") {
		t.Error("DiretorioDoProgramaOk perdeu uma das duas direções")
	}
	// Na página da pasta e no modo silencioso (/DIR=).
	if !strings.Contains(iss, "if CurPageID = wpSelectDir then") || !strings.Contains(iss, "DiretorioDoProgramaOk(ExpandConstant('{app}'))") {
		t.Error("a pasta do programa não é conferida na página nem no modo silencioso")
	}
	// A atualização só apaga o painel antigo se já há um Agent ali.
	for _, l := range secao(iss, "InstallDelete") {
		if !strings.Contains(l, "Check: JaTemOAgentNestaPasta") {
			t.Errorf("InstallDelete sem a conferência de que a pasta já é do programa: %s", l)
		}
	}
}

// O runtime do Visual C++: a versão mínima é a do toolset do whisper.cpp, e o
// que se baixa só roda se a assinatura for da Microsoft.
func TestIss_VisualCpp(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	def := definicoes(iss)
	if def["VcMaiorMinimo"] != "14" || def["VcMenorMinimo"] != "51" {
		t.Errorf("versão mínima do VC++ = %s.%s; a medida no whisper-server é 14.51", def["VcMaiorMinimo"], def["VcMenorMinimo"])
	}
	if def["ChaveVc"] != `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64` {
		t.Errorf("ChaveVc = %q", def["ChaveVc"])
	}
	if def["UrlVcRedist"] != "https://aka.ms/vc14/vc_redist.x64.exe" {
		t.Errorf("UrlVcRedist = %q", def["UrlVcRedist"])
	}
	if !strings.HasPrefix(def["UrlVcRedist"], "https://") {
		t.Error("o redistribuível não vem por HTTPS")
	}
	for _, quer := range []string{
		"HKLM64",
		"Get-AuthenticodeSignature",
		"CN=Microsoft Corporation",
		"ShellExec('runas'",
		"if not AssinadoPelaMicrosoft(Arquivo) then",
	} {
		if !strings.Contains(iss, quer) {
			t.Errorf("o .iss não tem %q", quer)
		}
	}
	// A assinatura é conferida ANTES de executar.
	if a, b := strings.Index(iss, "if not AssinadoPelaMicrosoft(Arquivo) then"), strings.Index(iss, "ShellExec('runas'"); a < 0 || a > b {
		t.Error("o redistribuível é executado antes de conferir a assinatura")
	}
	// A única conexão de rede do instalador.
	if n := strings.Count(iss, "DownloadTemporaryFile("); n != 1 {
		t.Errorf("o .iss tem %d downloads; só o do VC++", n)
	}
}

// O pacote leva as licenças.
func TestIss_LevaAsLicencas(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	if !strings.Contains(iss, `DestDir: "{app}\licencas"`) {
		t.Error("o .iss não instala a pasta licencas")
	}
}

// O ícone (ADR-018, F3): o Setup usa o .ico do repositório (SetupIconFile, relativo à
// pasta do .iss), e o programa instalado usa o do próprio executável, que leva o
// mesmo símbolo como recurso (cmd/agent/icone_windows_amd64.syso, ligado pelo
// go build; UninstallDisplayIcon aponta para o executável). Os dois arquivos são
// gerados por cmd/gerarico, que tem os testes do conteúdo; aqui, que o .iss os
// referencia e que existem. Só lê.
func TestIss_IconeDoSetupEDoProgramaExistem(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	def := definicoes(iss)
	var setupIcon, displayIcon string
	for _, l := range secao(iss, "Setup") {
		if k, v, ok := strings.Cut(l, "="); ok {
			switch strings.TrimSpace(k) {
			case "SetupIconFile":
				setupIcon = strings.TrimSpace(v)
			case "UninstallDisplayIcon":
				displayIcon = strings.TrimSpace(v)
			}
		}
	}
	if setupIcon == "" {
		t.Fatal("o [Setup] não tem SetupIconFile: o Setup sairia com o ícone padrão do Inno")
	}
	if filepath.IsAbs(setupIcon) || strings.HasPrefix(setupIcon, `\`) || strings.Contains(setupIcon, ":") || strings.Contains(setupIcon, "{") {
		t.Errorf("SetupIconFile = %q: tem de ser relativo à pasta do .iss, e não um caminho da máquina de quem montou", setupIcon)
	}
	b := lerBruto(t, "../../installer/"+filepath.ToSlash(setupIcon))
	if len(b) < 6+16 || !bytes.Equal(b[:4], []byte{0, 0, 1, 0}) {
		t.Fatalf("%s não é um .ico (ICONDIR com reservado/tipo inválidos)", setupIcon)
	}
	// As camadas que o Windows pede: 16 e 32 (listas e barra de tarefas), 48 e 256 (o Explorador).
	tem := map[int]bool{}
	for i := 0; i < int(b[4])|int(b[5])<<8; i++ {
		w := int(b[6+16*i])
		if w == 0 {
			w = 256
		}
		tem[w] = true
	}
	for _, w := range []int{16, 32, 48, 256} {
		if !tem[w] {
			t.Errorf("o .ico do Setup não tem a camada de %d px", w)
		}
	}

	// O ícone do programa: o do executável, que tem o recurso.
	if want := `{app}\{#NomeDoExe}`; displayIcon != want || def["NomeDoExe"] != nomeDoExeDoAgent {
		t.Errorf("UninstallDisplayIcon = %q, quero %q: o ícone de \"Aplicativos\" é o do executável", displayIcon, want)
	}
	syso := lerBruto(t, "../../cmd/agent/icone_windows_amd64.syso")
	if len(syso) < 20 || syso[0] != 0x64 || syso[1] != 0x86 { // IMAGE_FILE_MACHINE_AMD64, little-endian
		t.Error("cmd/agent/icone_windows_amd64.syso não é um objeto COFF amd64: o executável ficaria sem ícone")
	}
}

// A versão e as pastas vêm de defines que o script passa; o .iss tem padrão
// para cada uma, e o script só passa o que o .iss aceita.
func TestScript_PassaAoIsccSoOQueOIssAceita(t *testing.T) {
	iss := lerTexto(t, caminhoDoIss)
	ps := lerTexto(t, caminhoDoScript)
	passados := regexp.MustCompile(`'/D(\w+)=`).FindAllStringSubmatch(ps, -1)
	if len(passados) != 4 {
		t.Fatalf("o script passa %d defines ao ISCC; quero VersaoDoApp, VersaoNumerica, PastaStaging e PastaSaida", len(passados))
	}
	for _, m := range passados {
		if !strings.Contains(iss, "#ifndef "+m[1]) {
			t.Errorf("o script passa /D%s, e o .iss não o declara com #ifndef", m[1])
		}
	}
}

// As licenças baixadas na montagem apontam para um COMMIT (40 hexadecimais), e
// nunca para "main" nem para uma tag: o texto que entra no Setup não muda debaixo
// do script. O hash de cada uma fica fixado no campo Sha256, e o script falha
// se o arquivo baixado for outro.
func TestScript_LicencasRemotasEmCommitFixo(t *testing.T) {
	ps := lerTexto(t, caminhoDoScript)
	urls := regexp.MustCompile(`Url\s*=\s*'([^']+)'`).FindAllStringSubmatch(ps, -1)
	if len(urls) != 3 {
		t.Fatalf("o script baixa %d licenças; quero 3 (whisper.cpp, openai/whisper e a Apache-2.0)", len(urls))
	}
	commit := regexp.MustCompile(`^https://raw\.githubusercontent\.com/[\w.-]+/[\w.-]+/[0-9a-f]{40}/LICENSE$`)
	raw := 0
	for _, m := range urls {
		if strings.Contains(m[1], "raw.githubusercontent.com") {
			raw++
			if !commit.MatchString(m[1]) {
				t.Errorf("a licença %s não aponta para um commit fixo", m[1])
			}
		} else if !strings.HasPrefix(m[1], "https://") {
			t.Errorf("a licença %s não vem por HTTPS", m[1])
		}
	}
	if raw != 2 {
		t.Errorf("%d licenças vêm do GitHub; quero 2 (whisper.cpp e openai/whisper)", raw)
	}
}

// O script monta como a ADR manda.
func TestScript_MontaComoAADRManda(t *testing.T) {
	ps := lerTexto(t, caminhoDoScript)
	for _, quer := range []string{
		"-trimpath",
		"-ldflags=-H=windowsgui -X main.version=",
		"./cmd/agent",
		"go run ./cmd/asrstage -destino $StagingAbs -sem-dicionario",
		"pnpm -C web build --outDir $PainelDir",
		"pnpm -C web install --frozen-lockfile",
		"/DVersaoDoApp=",
		"$ArquivoDoModelo = '" + model.SmallQ4_0.Arquivo + "'",
		nomeDoExeDoAgent,
		"licencas",
		"THIRD_PARTY_NOTICES.md",
		"internal\\agent\\overlay\\fontes\\OFL.txt",
		"-ExigirIscc",
	} {
		if !strings.Contains(ps, quer) {
			t.Errorf("o script não tem %q", quer)
		}
	}
	// O -o é obrigatório: sem ele o go build grava um agent.exe longe do modelo.
	// E o -trimpath e o -X da versão, na linha do go build de verdade (os
	// comentários também citam as duas coisas).
	goBuild := regexp.MustCompile(`(?m)^Executar 'go build do Agent' \{ & go build .*$`).FindString(ps)
	if goBuild == "" {
		t.Fatal("o script não tem a linha do go build do Agent")
	}
	for _, quer := range []string{" -trimpath ", "-ldflags=-H=windowsgui -X main.version=' + $Versao", " -o $AgentExe ", "./cmd/agent"} {
		if !strings.Contains(goBuild, quer) {
			t.Errorf("o go build do Agent perdeu %q: %s", quer, goBuild)
		}
	}
	// Confere a versão, o -X e o -trimpath no binário, sem executá-lo. Procurar a
	// versão no texto não prova o -X para a 0.1.0 (o valor de desenvolvimento a
	// contém, e o Go o deixa no binário); o que prova é o símbolo
	// main.version.str, que o linker só cria quando aplica um -X a main.version.
	for _, quer := range []string{
		"$Simbolos = & go tool nm $AgentExe",
		`$_ -match '\smain\.version\.str\s*$'`,
		"$Info = (& go version -m $AgentExe) -join",
		"$Info.Contains('-trimpath=true')",
		"$texto.Contains($Versao)",
		"$texto.Contains($caminho)",
	} {
		if !strings.Contains(ps, quer) {
			t.Errorf("o script não confere o binário: falta %q", quer)
		}
	}
	// Cada conferência derruba o script: o throw vem logo na linha seguinte.
	for _, quer := range []string{
		"Count -eq 0) {\n    throw 'O -X main.version nao pegou",
		"if (-not $Info.Contains('-trimpath=true')) {\n    throw ",
		"if (-not $texto.Contains($Versao)) {\n    throw ",
		"if ($texto.Contains($caminho)) {\n        throw ",
	} {
		if !strings.Contains(ps, quer) {
			t.Errorf("a conferência do binário não derruba o script: falta %q", quer)
		}
	}
	// Nunca embute dicionário pessoal, nunca roda o Agent nem o Setup.
	semComentario := regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(ps, "")
	if strings.Contains(semComentario, "testdata") {
		t.Error("o script mexe em testdata: o dicionário do repositório é pessoal")
	}
	if regexp.MustCompile(`(?i)(&|Start-Process|Invoke-Expression)\s+\$?(AgentExe|Setup)\b`).MatchString(semComentario) {
		t.Error("o script executa o Agent ou o Setup gerado")
	}
	// Só apaga dentro de dist\ : a guarda vem antes do único Remove-Item.
	if n := strings.Count(semComentario, "Remove-Item"); n != 1 {
		t.Fatalf("o script tem %d Remove-Item; só pode ter o do staging", n)
	}
	if g, r := strings.Index(semComentario, "Garantir-Dentro-De $StagingAbs $Dist"), strings.Index(semComentario, "Remove-Item"); g < 0 || g > r {
		t.Error("o Remove-Item do staging não é precedido da guarda de que ele fica dentro de dist\\")
	}
	if strings.Contains(semComentario, `'bin`) || strings.Contains(semComentario, `"bin`) {
		t.Error("o script escreve em bin\\")
	}
}

// O workflow de release cria RASCUNHO e nunca publica.
func TestWorkflow_CriaSoRascunhoENuncaPublica(t *testing.T) {
	wf := lerTexto(t, caminhoDoWorkflow)
	semComentario := regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(wf, "")

	for _, quer := range []string{"- 'v*'", "runs-on: windows-latest", "gh release create", "--draft", "--verify-tag", "-ExigirIscc", "montar-instalador.ps1"} {
		if !strings.Contains(semComentario, quer) {
			t.Errorf("o workflow não tem %q", quer)
		}
	}
	for _, proibido := range []string{
		"--draft=false", "--latest", "gh release edit", "gh release upload", "draft: false",
		"git push", "git tag", "workflow_dispatch", "pull_request",
	} {
		if strings.Contains(semComentario, proibido) {
			t.Errorf("o workflow tem %q: ele só cria o rascunho", proibido)
		}
	}
	// O único gh release que escreve é o create.
	if n := strings.Count(semComentario, "gh release create @"); n != 1 {
		t.Errorf("o workflow tem %d chamadas de gh release create", n)
	}
	// Escrita só no job de release, e só contents.
	if n := strings.Count(semComentario, "contents: write"); n != 1 {
		t.Errorf("o workflow tem %d permissões de escrita; só o job do rascunho", n)
	}
	if strings.Contains(semComentario, "write-all") || strings.Contains(semComentario, "id-token") || strings.Contains(semComentario, "packages: write") {
		t.Error("o workflow pede permissão além de contents")
	}
	// Só dispara por tag v*.
	if !regexp.MustCompile(`(?s)on:\s+push:\s+tags:\s+- 'v\*'`).MatchString(semComentario) {
		t.Error("o workflow não dispara só por tag v*")
	}
}

// As notas do release vão a público quando o Mauro publica o rascunho. Nada
// interno entra nelas (o aviso "este release e um rascunho... decisao do
// Mauro" ficou como comentário do workflow), e entra o que quem baixa precisa
// saber: o ditado é local, o Polish é opcional e usa a chave da própria pessoa,
// a nuvem é opcional e exige servidor próprio. ASCII, como o resto do texto: o
// PowerShell 5.1 do runner não garante UTF-8 ao ler o script.
func TestWorkflow_NotasDoReleaseSaoParaOPublico(t *testing.T) {
	wf := lerTexto(t, caminhoDoWorkflow)
	m := regexp.MustCompile(`(?s)\$notas = @"\n(.*?)\n\s*"@`).FindStringSubmatch(wf)
	if m == nil {
		t.Fatal("não achei o texto das notas ($notas = @\"...\"@) no workflow")
	}
	notas := m[1]
	minusculas := strings.ToLower(notas)
	for _, interno := range []string{"rascunho", "nao foi publicado", "decisao do mauro", "antes de publicar", "adr-", "workflow"} {
		if strings.Contains(minusculas, interno) {
			t.Errorf("as notas, que vão a público, trazem %q (texto interno de quem publica)", interno)
		}
	}
	for _, quer := range []string{
		"previa, sem assinatura digital",
		"O ditado funciona no PC, sem conta e sem internet.",
		"O Polish",
		"e opcional e usa a chave da propria pessoa (OpenAI ou Anthropic), cobrada na conta dela.",
		"A nuvem e opcional e exige um servidor proprio",
		"o projeto nao tem servidor central.",
		"SHA-256 do Setup: ``$hash``",
		"SmartScreen",
	} {
		if !strings.Contains(notas, quer) {
			t.Errorf("as notas do release não dizem %q", quer)
		}
	}
	for i, r := range notas {
		if r > 127 {
			t.Errorf("as notas têm um caractere fora do ASCII (%q, byte %d)", r, i)
			break
		}
	}
	// O lembrete de quem publica existe, mas fora das notas: um comentário do workflow.
	if !strings.Contains(wf, "# Lembrete para quem publica") {
		t.Error("o lembrete de quem publica (o roteiro do instalador antes de publicar) sumiu do workflow")
	}
}

// Os valores fixos do workflow são os do código e os da ADR.
func TestWorkflow_HashesFixosSaoOsDoCodigoEDaADR(t *testing.T) {
	wf := lerTexto(t, caminhoDoWorkflow)
	adr := lerTexto(t, caminhoDaADR)
	const zip = "f9ec6c52a2e949b62ab51fa21d0d497958f9e41c3010c157c4e42932d5316f3c"
	for _, quer := range []string{
		"WHISPER_ZIP_SHA256: " + zip,
		"MODELO_SHA256: " + model.SmallQ4_0.SHA256,
		"MODELO_BYTES: '" + strconv.FormatInt(model.SmallQ4_0.Bytes, 10) + "'",
		"MODELO_ARQUIVO: " + model.SmallQ4_0.Arquivo,
	} {
		if !strings.Contains(wf, quer) {
			t.Errorf("o workflow não tem %q", quer)
		}
	}
	if !strings.Contains(adr, zip) {
		t.Error("o SHA-256 do zip do whisper.cpp do workflow não é o da ADR-018")
	}
	// O release do modelo é decisão do Mauro: o TODO tem de estar lá, claro.
	if !strings.Contains(wf, "TODO(Mauro)") || !strings.Contains(wf, "MODELO_RELEASE:") {
		t.Error("o workflow perdeu o TODO do release do modelo")
	}
	// Cada download confere o hash antes de seguir, e a conferência derruba o job.
	if strings.Count(wf, "Get-FileHash") < 3 {
		t.Error("o workflow não confere o hash do zip e do modelo")
	}
	for _, quer := range []string{
		"-ieq $esperado", // o zip: o hash do upstream ou do espelho tem de bater
		"if (-not $ok) { throw ",
		"if ($tamanho -ne [int64]$env:MODELO_BYTES) { throw ",
		"if ($hash -ine $env:MODELO_SHA256) { throw ",
	} {
		if !strings.Contains(wf, quer) {
			t.Errorf("o workflow perdeu a conferência %q", quer)
		}
	}

	// O zip do whisper.cpp leva o whisper-server.exe e as DLLs para dentro do
	// Setup: as DUAS origens (o upstream e o espelho) conferem o hash, e a falta
	// de uma origem que confira derruba o job. Nenhuma atribuição força o resultado.
	const confere = "$ok = ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ieq $esperado)"
	if n := strings.Count(wf, confere); n != 2 {
		t.Errorf("o workflow confere o hash do zip em %d lugares; quero 2 (o upstream e o espelho)", n)
	}
	if regexp.MustCompile(`\$ok\s*=\s*\$true`).MatchString(wf) {
		t.Error("o workflow força $ok = $true: a conferência do hash do zip está desligada")
	}
	if fim := strings.LastIndex(wf, confere); fim < 0 || !strings.Contains(wf[fim:], "if (-not $ok) { throw ") {
		t.Error("depois da última conferência do hash do zip, falta o throw que derruba o job")
	}
	if strings.Index(wf, "Expand-Archive") < strings.LastIndex(wf, confere) {
		t.Error("o zip é expandido antes de conferir o hash")
	}
}

// O portão da F2: a ADR-018 diz que este workflow depende da F2 (o
// whisper-server com cmd.Dir e o modelo pelo nome relativo). Enquanto o portão
// estiver fechado, nenhuma tag gera Setup. O VALOR de F2_PRONTA não é conferido
// aqui: abrir o portão, quando a F2 entrar, é uma decisão e não pode quebrar o teste.
func TestWorkflow_PortaoDaF2EAPrimeiraEtapa(t *testing.T) {
	wf := lerTexto(t, caminhoDoWorkflow)
	etapas := etapasDoWorkflow(wf)
	if len(etapas) == 0 {
		t.Fatal("o workflow não tem etapas")
	}
	if !strings.HasPrefix(etapas[0].nome, "Portão da F2") {
		t.Fatalf("a primeira etapa do job tem de ser o portão da F2; é %q", etapas[0].nome)
	}
	if !strings.Contains(etapas[0].texto, "if ($env:F2_PRONTA -ne 'true') {\nthrow ") {
		t.Errorf("o portão da F2 não derruba o job quando F2_PRONTA não é 'true':\n%s", etapas[0].texto)
	}
	topo := wf[strings.Index(wf, "\nenv:\n"):strings.Index(wf, "\njobs:")]
	if !regexp.MustCompile(`(?m)^  F2_PRONTA: '(true|false)'`).MatchString(topo) {
		t.Error("F2_PRONTA não está declarada, entre aspas, no env do workflow")
	}
	if !strings.Contains(wf, "TODO(Mauro)") {
		t.Error("o workflow perdeu os TODO(Mauro)")
	}
}

// O GH_TOKEN (que no job tem contents: write) só chega aos passos que chamam o
// gh. O pnpm install e o vite build rodam código de terceiros e não podem vê-lo;
// o checkout também não deixa a credencial no .git/config.
func TestWorkflow_GHTokenSoNosPassosQueChamamOGh(t *testing.T) {
	wf := lerTexto(t, caminhoDoWorkflow)
	semComentario := regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(wf, "")
	if topo := wf[strings.Index(wf, "\nenv:\n"):strings.Index(wf, "\njobs:")]; strings.Contains(topo, "GH_TOKEN") {
		t.Error("o GH_TOKEN está no env do workflow inteiro")
	}
	if regexp.MustCompile(`(?m)^    env:\s*$`).MatchString(semComentario) {
		t.Error("o job tem env próprio: o GH_TOKEN iria para todos os passos (pnpm, vite, go)")
	}
	chamaGh := regexp.MustCompile(`(?m)^gh `)
	comToken := 0
	for _, e := range etapasDoWorkflow(wf) {
		tem := strings.Contains(e.texto, "GH_TOKEN: ${{ github.token }}")
		if tem {
			comToken++
		}
		if tem != chamaGh.MatchString(e.texto) {
			t.Errorf("a etapa %q: GH_TOKEN = %v, chama o gh = %v; o token é só de quem chama o gh", e.nome, tem, chamaGh.MatchString(e.texto))
		}
	}
	if comToken != 3 {
		t.Errorf("%d etapas recebem o GH_TOKEN; quero 3 (o espelho do zip, o modelo e o release rascunho)", comToken)
	}
	for _, e := range etapasDoWorkflow(wf) {
		if e.nome == "Checkout" && !strings.Contains(e.texto, "persist-credentials: false") {
			t.Error("o checkout deixa a credencial no .git/config (persist-credentials: false)")
		}
	}
}
