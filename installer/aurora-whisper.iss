; ============================================================================
; Aurora Whisper - instalador (Inno Setup 6.7), ADR-018.
;
; Compilar (pelo script, que monta a pasta de staging antes):
;   powershell -File scripts\montar-instalador.ps1 -Versao 0.1.0
; ou, com a pasta de staging ja montada:
;   ISCC.exe /DVersaoDoApp=0.1.0 /DVersaoNumerica=0.1.0.0 installer\aurora-whisper.iss
;
; Este arquivo e salvo em UTF-8 COM BOM: sem o BOM o Inno le o texto como ANSI
; e os acentos das mensagens saem trocados. Um teste (cmd/asrstage,
; instalador_test.go) confere o BOM. Compilar nao executa nada: o ISCC so le
; este arquivo e a pasta de staging e escreve o Setup na pasta de saida.
;
; O QUE FAZ (as decisoes estao na ADR-018):
;   - instala POR USUARIO, sem administrador, em
;     %LOCALAPPDATA%\Programs\Aurora Whisper (so leitura em uso);
;   - os DADOS (historico, dicionario, logs) ficam em %LOCALAPPDATA%\AuroraWhisper,
;     fora desta pasta: o Setup troca a pasta do programa inteira na
;     atualizacao e nunca toca nos dados;
;   - atalho no Menu Iniciar; na area de trabalho so se a pessoa marcar;
;   - ao terminar, oferece abrir o Agent (marcado);
;   - NAO liga o "Iniciar com o Windows" (decisao 3): quem liga e a pessoa,
;     pela bandeja. Por isso nao ha secao [Registry] neste arquivo;
;   - fecha o Agent antes de instalar e de desinstalar (taskkill sem /F, e
;     espera o mutex do Agent sumir, com teto);
;   - confere o runtime do Visual C++ (x64) e, se faltar, baixa o oficial da
;     Microsoft e o executa (o Windows pede permissao uma vez);
;   - o desinstalador pergunta se apaga tambem os dados (padrao: NAO).
;
; O Setup nunca cria dado de quem usa: o pacote nao leva dicionario, nem
; historico.db, nem o marcador "portatil" (o asrstage recusa uma pasta de
; pacote com isso, e esta lista de arquivos e explicita, sem coringa na raiz).
; ============================================================================

; ---- Valores que o script de montagem passa por linha de comando ----------
#ifndef VersaoDoApp
  #define VersaoDoApp "0.0.0-dev"
#endif
; Quatro numeros (x.y.z.w), para as propriedades do arquivo; sem o sufixo de
; prerelease que VersaoDoApp pode ter.
#ifndef VersaoNumerica
  #define VersaoNumerica "0.0.0.0"
#endif
; A pasta que o asrstage -destino monta (relativa a esta pasta, installer\).
#ifndef PastaStaging
  #define PastaStaging "..\dist\staging"
#endif
; Onde o ISCC escreve o Setup. Pasta temporaria do chamador, nunca bin\.
#ifndef PastaSaida
  #define PastaSaida "..\dist"
#endif

; ---- Nomes que o Agent tambem usa (um teste confere cada um contra o Go) ---
#define NomeDoApp "Aurora Whisper"
#define NomeDoExe "aurora-whisper-agent.exe"
; internal/agent/instancia.NomeDoAgent: o mutex que o Agent segura enquanto roda.
#define MutexDoAgent "Local\AuroraWhisperAgent"
; internal/agent/pastas.NomeDaPastaDeDados: a pasta de dados dentro de %LOCALAPPDATA%.
#define PastaDeDados "AuroraWhisper"
; internal/asr/model.SmallQ4_0.Arquivo: o modelo ao lado do executavel.
#define ArquivoDoModelo "ggml-small-q4_0.bin"
; internal/agent/autostart: onde o Agent grava o inicio automatico, se a pessoa ligar.
#define ChaveRun "Software\Microsoft\Windows\CurrentVersion\Run"
#define ChaveAprovados "Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run"
#define ValorDoRun "AuroraWhisper"
; internal/agent/chaves.PrefixoDoAlvo + provedor: as chaves de API no Gerenciador de Credenciais.
#define AlvoDaChaveOpenAI "AuroraWhisper/openai"
#define AlvoDaChaveAnthropic "AuroraWhisper/anthropic"
; internal/agent/nuvem.NomeNoCofreInstalado: o token do PC na nuvem (ADR-019, F6).
; O alvo do modo portatil (o bin\ de desenvolvimento) nao e instalado, e o
; desinstalador nao o toca.
#define AlvoDaChaveNuvem "AuroraWhisper/nuvem"

; ---- O runtime do Visual C++ ------------------------------------------------
; A versao minima e a do linker (toolset) dos binarios do whisper.cpp que vao no
; pacote (14.51, lida no cabecalho PE do whisper-server.exe e das DLLs do zip da
; build b5130): a regra da Microsoft e que o redistribuivel seja igual ou mais
; novo que o toolset que compilou o programa. NAO foi testado rodar o servidor
; com um redistribuivel mais antigo. Se o zip do whisper.cpp mudar, reconferir.
#define ChaveVc "SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64"
#define VcMaiorMinimo 14
#define VcMenorMinimo 51
#define UrlVcRedist "https://aka.ms/vc14/vc_redist.x64.exe"

[Setup]
; O AppId identifica o programa para atualizar por cima e para desinstalar.
; NUNCA mudar depois do primeiro Setup publicado.
AppId={{D8740DB2-3756-465C-96BA-EF589951C223}
AppName={#NomeDoApp}
AppVersion={#VersaoDoApp}
AppVerName={#NomeDoApp} {#VersaoDoApp}
AppPublisher=Mauro Juliani Junior
AppPublisherURL=https://github.com/MauroJulianiJunior/aurora-whisper
AppSupportURL=https://github.com/MauroJulianiJunior/aurora-whisper/issues
AppUpdatesURL=https://github.com/MauroJulianiJunior/aurora-whisper/releases
VersionInfoVersion={#VersaoNumerica}
VersionInfoProductVersion={#VersaoNumerica}
VersionInfoDescription={#NomeDoApp} - instalador
; Por usuario: sem administrador. O Agent ja usa so HKCU e o mutex Local\.
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\{#NomeDoApp}
DefaultGroupName={#NomeDoApp}
DisableProgramGroupPage=yes
; Windows 10/11 de 64 bits (o whisper-server e x64; no Windows on Arm roda emulado).
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir={#PastaSaida}
OutputBaseFilename=AuroraWhisper-Setup-{#VersaoDoApp}
; O modelo (139 MB) comprime pouco, mas comprime.
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ShowLanguageDialog=no
; O icone do Setup e do desinstalador: o Pico estourado, gerado a partir do desenho
; da bandeja por "go run ./cmd/gerarico" (relativo a esta pasta). O do programa
; ("Aplicativos", Menu Iniciar) e o do proprio executavel, que leva o mesmo
; simbolo como recurso (cmd/agent/icone_windows_amd64.syso).
SetupIconFile=aurora-whisper.ico
UninstallDisplayIcon={app}\{#NomeDoExe}
; Dois Setups ao mesmo tempo nao.
SetupMutex=AuroraWhisperSetup
; Fechar o Agent e com o taskkill do [Code], e nao pelo Restart Manager do Inno:
; o Agent nao trata WM_QUERYENDSESSION, e como ele reage a isso nao foi verificado.
CloseApplications=no
RestartApplications=no
; Um log do Setup em %TEMP%, so na maquina de quem instala, para diagnosticar.
SetupLogging=yes

[Languages]
Name: "ptbr"; MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"

[Tasks]
; Sem atalho na area de trabalho por padrao.
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
; Lista EXPLICITA, sem coringa na raiz do staging: assim nada que apareca la por
; engano (dicionario, banco, log, marcador) entra no Setup. Um teste confere que
; as DLLs daqui sao as do asrstage (dllsNecessarias).
Source: "{#PastaStaging}\{#NomeDoExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\whisper-server.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\whisper.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-base.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-alderlake.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-cannonlake.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-cascadelake.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-haswell.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-icelake.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-sandybridge.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-skylakex.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-sse42.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PastaStaging}\ggml-cpu-x64.dll"; DestDir: "{app}"; Flags: ignoreversion
; O modelo de voz, ao lado do executavel (o Agent o procura la e confere o SHA-256).
Source: "{#PastaStaging}\{#ArquivoDoModelo}"; DestDir: "{app}"; Flags: ignoreversion
; O painel e as licencas: so estas duas pastas, com tudo o que tem dentro.
Source: "{#PastaStaging}\painel\*"; DestDir: "{app}\painel"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#PastaStaging}\licencas\*"; DestDir: "{app}\licencas"; Flags: ignoreversion recursesubdirs createallsubdirs

[InstallDelete]
; Atualizar por cima: o painel tem arquivos com nome de hash, e os da versao
; anterior nao podem ficar. So se ja ha um Agent nesta pasta: quem escolheu uma
; pasta que ja tinha outra coisa chamada "painel" nao perde nada.
Type: filesandordirs; Name: "{app}\painel"; Check: JaTemOAgentNestaPasta
Type: filesandordirs; Name: "{app}\licencas"; Check: JaTemOAgentNestaPasta

[Icons]
Name: "{autoprograms}\{#NomeDoApp}"; Filename: "{app}\{#NomeDoExe}"; WorkingDir: "{app}"
Name: "{autodesktop}\{#NomeDoApp}"; Filename: "{app}\{#NomeDoExe}"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
; "Abrir agora": marcado por padrao, e nao no modo silencioso. Sem o Visual C++
; o Agent nao abriria, entao a opcao some.
Filename: "{app}\{#NomeDoExe}"; Description: "{cm:AbrirAgora}"; Flags: nowait postinstall skipifsilent; Check: PodeAbrirAgora

[CustomMessages]
AbrirAgora=Abrir o Aurora Whisper agora
AvisoVc=Visual C++: o Aurora Whisper precisa do "Microsoft Visual C++ Redistributable" (x64, versão 14.51 ou mais nova), e ele não foi encontrado (ou é mais antigo) nesta máquina. O instalador vai baixá-lo da Microsoft (aka.ms/vc14/vc_redist.x64.exe) e executá-lo. O Windows vai pedir permissão de administrador uma vez. É a única conexão de rede do instalador, e só acontece agora.
VcFalhou=Não foi possível instalar o Visual C++ (sem rede, permissão negada ou o arquivo baixado não passou na conferência da assinatura da Microsoft).%n%nO Aurora Whisper vai ser instalado mesmo assim, mas só abre depois que o runtime estiver instalado. Instale o "Microsoft Visual C++ Redistributable x64" em https://aka.ms/vc14/vc_redist.x64.exe e abra o Aurora Whisper de novo.
AgentNaoFechou=O Aurora Whisper está aberto e não fechou a tempo.%n%nFeche-o pelo ícone na bandeja do Windows (menu, Sair) e tente de novo.
PerguntaFecharAgent=O Aurora Whisper está aberto. Para desinstalar, ele precisa ser fechado.%n%nFechar o Aurora Whisper agora e continuar?
DiretorioInvalido=A pasta do programa não pode ser a pasta de dados (%1), nem estar dentro dela, nem conter ela.%n%nEscolha outra pasta, por exemplo a sugerida.
PerguntaApagarDados=Apagar também os dados do Aurora Whisper deste usuário?%n%nIsso apaga o histórico de ditados, o dicionário, as preferências e os logs em %1, e as credenciais do Gerenciador de Credenciais do Windows (AuroraWhisper/openai, AuroraWhisper/anthropic e AuroraWhisper/nuvem). O PC continua em Aparelhos, na nuvem, até você revogá-lo lá.%n%nNão dá para desfazer. Se você só vai atualizar ou reinstalar, escolha Não: os dados ficam.
AgentAbertoNaoApagar=O Aurora Whisper ainda está aberto, então os dados não foram apagados. Feche-o e apague a pasta %1 à mão, se quiser.
PastaRecusada=Por segurança, o desinstalador só apaga a pasta de dados quando ela é exatamente %1, não é um atalho (junção) e não tem relação com a pasta do programa. Isso não se confirmou, e nada foi apagado. Apague a pasta à mão, se quiser.
NaoApagouTudo=Não consegui apagar tudo em %1. Feche o que estiver usando os arquivos e apague a pasta à mão.

[Code]
const
  { Quanto esperar o Agent fechar, em milissegundos, e de quanto em quanto. }
  EsperaParaFecharMs = 20000;
  PassoDaEsperaMs = 250;
  { CRED_TYPE_GENERIC: o tipo da credencial que o Agent grava (chaves_windows.go). }
  CredTypeGeneric = 1;

var
  { Decididos em InitializeSetup e PrepareToInstall. }
  PrecisaDeVc: Boolean;
  VcPronto: Boolean;

{ ---------------------------------------------------------------------------
  Pastas
  --------------------------------------------------------------------------- }

{ A pasta de dados: sempre <localappdata>\AuroraWhisper, montada aqui e em mais
  nenhum lugar. Nunca vem de entrada de quem instala. }
function PastaDosDados(): String;
begin
  Result := AddBackslash(ExpandConstant('{localappdata}')) + '{#PastaDeDados}';
end;

{ Pai contem Filho, ou sao a mesma pasta (sem diferenciar maiusculas). Usa
  AnsiLowercase, e nao Lowercase: este so converte A-Z, e um perfil com acento
  (JOAO e joao, com til) escaparia da conferencia. }
function ContemOuIgual(const Pai, Filho: String): Boolean;
begin
  Result := Pos(AnsiLowercase(AddBackslash(RemoveBackslash(Pai))), AnsiLowercase(AddBackslash(RemoveBackslash(Filho)))) = 1;
end;

{ A pasta do programa nao pode se misturar com a de dados: o Setup troca a do
  programa inteira, e o desinstalador apaga a de dados. }
function DiretorioDoProgramaOk(const Dir: String): Boolean;
begin
  Result := not (ContemOuIgual(PastaDosDados(), Dir) or ContemOuIgual(Dir, PastaDosDados()));
end;

function JaTemOAgentNestaPasta(): Boolean;
begin
  Result := FileExists(ExpandConstant('{app}\{#NomeDoExe}'));
end;

{ A pasta e um atalho (junção ou link simbolico)? Apagar "dentro" dela pode
  apagar outro lugar. Falha para o lado seguro: se nao der para olhar a pasta,
  conta como atalho, e quem chama recusa. }
function EhLinkOuJuncao(const Caminho: String): Boolean;
var
  Achado: TFindRec;
begin
  Result := True;
  if FindFirst(Caminho, Achado) then
  begin
    try
      { FILE_ATTRIBUTE_REPARSE_POINT }
      Result := (Achado.Attributes and $400) <> 0;
    finally
      FindClose(Achado);
    end;
  end;
end;

{ ---------------------------------------------------------------------------
  O Agent aberto
  --------------------------------------------------------------------------- }

function AgentAberto(): Boolean;
begin
  Result := CheckForMutexes('{#MutexDoAgent}');
end;

{ Pede ao Agent que feche e espera ate o teto. taskkill SEM /F: o Agent trata o
  WM_CLOSE como "Sair", e o servidor de voz cai junto (Job Object), liberando
  as DLLs. O filtro de usuario evita fechar o Agent de outra conta quando o
  Setup roda elevado. Se um Agent de desenvolvimento (bin\) estiver aberto,
  ele tambem fecha: o nome do executavel e o mesmo, e os dois nao rodam juntos. }
function FecharOAgent(): Boolean;
var
  Codigo, Esperou: Integer;
begin
  if not AgentAberto() then
  begin
    Result := True;
    Exit;
  end;
  Log('O Agent esta aberto; pedindo para fechar (taskkill sem /F).');
  Exec(ExpandConstant('{sys}\taskkill.exe'),
    '/IM {#NomeDoExe} /FI "USERNAME eq ' + GetEnv('USERDOMAIN') + '\' + GetEnv('USERNAME') + '"',
    '', SW_HIDE, ewWaitUntilTerminated, Codigo);
  Esperou := 0;
  while AgentAberto() and (Esperou < EsperaParaFecharMs) do
  begin
    Sleep(PassoDaEsperaMs);
    Esperou := Esperou + PassoDaEsperaMs;
  end;
  Result := not AgentAberto();
end;

{ ---------------------------------------------------------------------------
  Visual C++
  --------------------------------------------------------------------------- }

function VcRuntimeOk(): Boolean;
var
  Instalado, Maior, Menor: Cardinal;
begin
  Result := False;
  if not RegQueryDWordValue(HKLM64, '{#ChaveVc}', 'Installed', Instalado) then Exit;
  if Instalado <> 1 then Exit;
  if not RegQueryDWordValue(HKLM64, '{#ChaveVc}', 'Major', Maior) then Exit;
  if not RegQueryDWordValue(HKLM64, '{#ChaveVc}', 'Minor', Menor) then Exit;
  Result := (Maior > {#VcMaiorMinimo}) or ((Maior = {#VcMaiorMinimo}) and (Menor >= {#VcMenorMinimo}));
end;

{ Aspas simples dentro de uma string entre aspas simples do PowerShell dobram. }
function AspasDoPowerShell(const Texto: String): String;
begin
  Result := Texto;
  StringChangeEx(Result, '''', '''''', True);
end;

{ O arquivo baixado e executado com permissao de administrador: so roda se a
  assinatura for valida e de "Microsoft Corporation". }
function AssinadoPelaMicrosoft(const Arquivo: String): Boolean;
var
  Codigo: Integer;
  Comando: String;
begin
  Comando := '-NoProfile -NonInteractive -Command "$s = Get-AuthenticodeSignature -LiteralPath ''' + AspasDoPowerShell(Arquivo) +
    '''; if ($s.Status -eq ''Valid'' -and $s.SignerCertificate.Subject -like ''CN=Microsoft Corporation*'') { exit 0 } else { exit 1 }"';
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Comando, '', SW_HIDE, ewWaitUntilTerminated, Codigo)
    and (Codigo = 0);
end;

{ Baixa o redistribuivel oficial, confere a assinatura e o executa. O endereco
  aka.ms aponta sempre para a versao mais nova, entao nao ha hash fixo. Devolve
  True se, depois, o registro mostra o runtime na versao pedida. }
function InstalarVcRuntime(): Boolean;
var
  Arquivo: String;
  Codigo: Integer;
begin
  Result := False;
  Arquivo := ExpandConstant('{tmp}\vc_redist.x64.exe');
  try
    DownloadTemporaryFile('{#UrlVcRedist}', 'vc_redist.x64.exe', '', nil);
  except
    Log('Nao consegui baixar o Visual C++: ' + GetExceptionMessage);
    Exit;
  end;
  if not AssinadoPelaMicrosoft(Arquivo) then
  begin
    Log('O arquivo baixado nao tem assinatura valida da Microsoft; nao foi executado.');
    Exit;
  end;
  { O verbo runas pede a permissao de administrador: o redistribuivel instala
    para a maquina toda. }
  if not ShellExec('runas', Arquivo, '/install /passive /norestart', '', SW_SHOWNORMAL, ewWaitUntilTerminated, Codigo) then
  begin
    Log('Permissao negada ou o redistribuivel nao iniciou (codigo ' + IntToStr(Codigo) + ').');
    Exit;
  end;
  { 0 = instalou; 1638 = ja ha uma versao igual ou mais nova; 3010 = instalou e
    pede reiniciar (as DLLs ja servem para o Agent). }
  Result := ((Codigo = 0) or (Codigo = 1638) or (Codigo = 3010)) and VcRuntimeOk();
end;

{ ---------------------------------------------------------------------------
  Eventos da instalacao
  --------------------------------------------------------------------------- }

function InitializeSetup(): Boolean;
begin
  PrecisaDeVc := not VcRuntimeOk();
  VcPronto := not PrecisaDeVc;
  Result := True;
end;

function PodeAbrirAgora(): Boolean;
begin
  Result := VcPronto;
end;

{ A pagina "Pronto para instalar" avisa do Visual C++ ANTES do clique. }
function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo, MemoTypeInfo, MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: String): String;
begin
  Result := '';
  if MemoDirInfo <> '' then Result := Result + MemoDirInfo + NewLine + NewLine;
  if MemoGroupInfo <> '' then Result := Result + MemoGroupInfo + NewLine + NewLine;
  if MemoTasksInfo <> '' then Result := Result + MemoTasksInfo + NewLine + NewLine;
  if PrecisaDeVc then Result := Result + CustomMessage('AvisoVc') + NewLine;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID = wpSelectDir then
  begin
    if not DiretorioDoProgramaOk(WizardDirValue) then
    begin
      MsgBox(FmtMessage(CustomMessage('DiretorioInvalido'), [PastaDosDados()]), mbError, MB_OK);
      Result := False;
    end;
  end;
end;

{ Roda depois do clique em Instalar e antes de copiar qualquer arquivo. Um texto
  devolvido aqui interrompe a instalacao e o mostra (no modo silencioso, sai com
  erro). }
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  { O /DIR= do modo silencioso nao passa pela pagina da pasta. }
  if not DiretorioDoProgramaOk(ExpandConstant('{app}')) then
  begin
    Result := FmtMessage(CustomMessage('DiretorioInvalido'), [PastaDosDados()]);
    Exit;
  end;
  if not FecharOAgent() then
  begin
    Result := CustomMessage('AgentNaoFechou');
    Exit;
  end;
  if PrecisaDeVc then
  begin
    VcPronto := InstalarVcRuntime();
    if not VcPronto then
      SuppressibleMsgBox(CustomMessage('VcFalhou'), mbError, MB_OK, IDOK);
  end;
end;

{ ---------------------------------------------------------------------------
  Desinstalar
  --------------------------------------------------------------------------- }

function RemoverAspas(const Texto: String): String;
begin
  Result := Trim(Texto);
  if (Length(Result) >= 2) and (Result[1] = '"') and (Result[Length(Result)] = '"') then
    Result := Copy(Result, 2, Length(Result) - 2);
end;

{ Se o inicio automatico (que o Agent grava, se a pessoa ligar) aponta para o
  executavel que esta sendo desinstalado, tira o valor e a marca do Windows.
  Senao o Windows tentaria abrir, a cada logon, um programa que nao existe. Se
  aponta para outro lugar (um bin\ de desenvolvimento), nao mexe. }
procedure LimparInicioAutomatico();
var
  Valor, Exe: String;
begin
  if not RegQueryStringValue(HKCU, '{#ChaveRun}', '{#ValorDoRun}', Valor) then Exit;
  Exe := ExpandConstant('{app}\{#NomeDoExe}');
  Valor := RemoverAspas(Valor);
  StringChangeEx(Valor, '/', '\', True);
  StringChangeEx(Exe, '/', '\', True);
  if CompareText(Valor, Exe) <> 0 then Exit;
  RegDeleteValue(HKCU, '{#ChaveRun}', '{#ValorDoRun}');
  RegDeleteValue(HKCU, '{#ChaveAprovados}', '{#ValorDoRun}');
end;

{ So apaga a pasta de dados se for exatamente <localappdata>\AuroraWhisper, nao
  for um atalho (junção/link), existir, e nao tiver relacao com a pasta do
  programa. O caminho vem de PastaDosDados, nunca de entrada de ninguem. }
function PodeApagarOsDados(const Pasta: String): Boolean;
var
  Local: String;
begin
  Result := False;
  Local := RemoveBackslash(ExpandConstant('{localappdata}'));
  // A pasta local vazia, ou uma raiz de disco: nunca.
  if Length(Local) < 4 then Exit;
  if CompareText(Pasta, Local + '\{#PastaDeDados}') <> 0 then Exit;
  if not DirExists(Pasta) then Exit;
  if EhLinkOuJuncao(Pasta) then Exit;
  if not DiretorioDoProgramaOk(ExpandConstant('{app}')) then Exit;
  Result := True;
end;

{ A mesma chamada que o Agent usa para apagar a chave (internal/agent/chaves,
  CredDeleteW). O cmdkey /delete foi trocado por ela: ele nao diz se a
  credencial GENERICA (o tipo que o Agent grava) e achada pelo nome puro. }
function CredDeleteW(TargetName: String; CredType, Flags: DWORD): BOOL;
external 'CredDeleteW@advapi32.dll stdcall';

procedure ApagarChaveDeApi(const Alvo: String);
begin
  { Apaga so a credencial do proprio programa, pelo nome exato. Ela pode nem
    existir (quem nunca guardou uma chave): o erro 1168 e esperado. }
  if CredDeleteW(Alvo, CredTypeGeneric, 0) then
    Log('Credencial ' + Alvo + ' apagada.')
  else
    Log('Credencial ' + Alvo + ' nao apagada (erro ' + IntToStr(DLLGetLastError()) + '; 1168 = nao existia).');
end;

procedure PerguntarEApagarOsDados();
var
  Pasta: String;
begin
  { Modo silencioso: nao pergunta, e o padrao e NAO apagar. }
  if UninstallSilent() then Exit;
  Pasta := PastaDosDados();
  if MsgBox(FmtMessage(CustomMessage('PerguntaApagarDados'), [Pasta]), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) <> IDYES then Exit;

  { Confere de novo que o Agent esta fechado: ele segura o historico aberto. }
  if AgentAberto() then
  begin
    MsgBox(FmtMessage(CustomMessage('AgentAbertoNaoApagar'), [Pasta]), mbInformation, MB_OK);
    Exit;
  end;
  if not PodeApagarOsDados(Pasta) then
  begin
    MsgBox(FmtMessage(CustomMessage('PastaRecusada'), [Pasta]), mbInformation, MB_OK);
    Exit;
  end;
  if not DelTree(Pasta, True, True, True) then
    MsgBox(FmtMessage(CustomMessage('NaoApagouTudo'), [Pasta]), mbInformation, MB_OK);
  ApagarChaveDeApi('{#AlvoDaChaveOpenAI}');
  ApagarChaveDeApi('{#AlvoDaChaveAnthropic}');
  { O token do PC na nuvem (F6), so nesta opcao: apagar sempre desconectaria o
    PC a cada atualizacao. Apagar o token daqui nao revoga o aparelho na nuvem. }
  ApagarChaveDeApi('{#AlvoDaChaveNuvem}');
end;

{ Fecha o Agent antes de comecar; se nao fechar, nao desinstala (os arquivos
  estariam em uso). Este evento roda ANTES da confirmacao padrao do
  desinstalador: com o Agent aberto, ele pede a sua propria (e, se a pessoa
  disser nao, nada se fecha). No modo silencioso nao pergunta, e a mensagem de
  falha e suprimivel: nenhuma desinstalacao silenciosa fica esperando um clique. }
function InitializeUninstall(): Boolean;
begin
  Result := True;
  if not AgentAberto() then Exit;
  if not UninstallSilent() then
  begin
    if MsgBox(CustomMessage('PerguntaFecharAgent'), mbConfirmation, MB_YESNO) <> IDYES then
    begin
      Result := False;
      Exit;
    end;
  end;
  Result := FecharOAgent();
  if not Result then
    SuppressibleMsgBox(CustomMessage('AgentNaoFechou'), mbError, MB_OK, IDOK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    LimparInicioAutomatico()
  else if CurUninstallStep = usPostUninstall then
    PerguntarEApagarOsDados();
end;
