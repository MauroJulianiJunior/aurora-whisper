package main

// Os textos esperados de instalador_golden_test.go: o que foi REVISADO do
// instalador (installer/aurora-whisper.iss) e do workflow de release
// (.github/workflows/release.yml). Sem comentários e sem recuo: o que se
// compara é o código. Ao mudar uma proteção, releia-a inteira e atualize aqui.
//
// O que cada função protege, em uma linha:
//   - PastaDosDados, ContemOuIgual, DiretorioDoProgramaOk: a pasta do programa
//     nunca se mistura com a de dados, que é a que o desinstalador apaga;
//   - EhLinkOuJuncao e PodeApagarOsDados: só se apaga o caminho exato, que
//     existe, que não é atalho e que não tem relação com a pasta do programa;
//   - AgentAberto, FecharOAgent, InitializeUninstall: o Agent fecha como "Sair"
//     (sem /F), e nada é apagado nem copiado com ele aberto;
//   - VcRuntimeOk, AssinadoPelaMicrosoft, InstalarVcRuntime: o redistribuível
//     só roda, com permissão de administrador, se a assinatura for da Microsoft;
//   - PerguntarEApagarOsDados: pergunta antes, o padrão é Não, e no modo
//     silencioso nunca apaga.

// goldenCabecalhoDoCodigo é o const/var que abre o [Code].
const goldenCabecalhoDoCodigo = `
const
EsperaParaFecharMs = 20000;
PassoDaEsperaMs = 250;
CredTypeGeneric = 1;
var
PrecisaDeVc: Boolean;
VcPronto: Boolean;
`

// goldenDoCodigo é o texto de cada function/procedure do [Code], sem comentários nem recuo.
var goldenDoCodigo = map[string]string{
	"PastaDosDados": `
function PastaDosDados(): String;
begin
Result := AddBackslash(ExpandConstant('{localappdata}')) + '{#PastaDeDados}';
end;
`,

	"ContemOuIgual": `
function ContemOuIgual(const Pai, Filho: String): Boolean;
begin
Result := Pos(AnsiLowercase(AddBackslash(RemoveBackslash(Pai))), AnsiLowercase(AddBackslash(RemoveBackslash(Filho)))) = 1;
end;
`,

	"DiretorioDoProgramaOk": `
function DiretorioDoProgramaOk(const Dir: String): Boolean;
begin
Result := not (ContemOuIgual(PastaDosDados(), Dir) or ContemOuIgual(Dir, PastaDosDados()));
end;
`,

	"JaTemOAgentNestaPasta": `
function JaTemOAgentNestaPasta(): Boolean;
begin
Result := FileExists(ExpandConstant('{app}\{#NomeDoExe}'));
end;
`,

	"EhLinkOuJuncao": `
function EhLinkOuJuncao(const Caminho: String): Boolean;
var
Achado: TFindRec;
begin
Result := True;
if FindFirst(Caminho, Achado) then
begin
try
Result := (Achado.Attributes and $400) <> 0;
finally
FindClose(Achado);
end;
end;
end;
`,

	"AgentAberto": `
function AgentAberto(): Boolean;
begin
Result := CheckForMutexes('{#MutexDoAgent}');
end;
`,

	"FecharOAgent": `
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
`,

	"VcRuntimeOk": `
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
`,

	"AspasDoPowerShell": `
function AspasDoPowerShell(const Texto: String): String;
begin
Result := Texto;
StringChangeEx(Result, '''', '''''', True);
end;
`,

	"AssinadoPelaMicrosoft": `
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
`,

	"InstalarVcRuntime": `
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
if not ShellExec('runas', Arquivo, '/install /passive /norestart', '', SW_SHOWNORMAL, ewWaitUntilTerminated, Codigo) then
begin
Log('Permissao negada ou o redistribuivel nao iniciou (codigo ' + IntToStr(Codigo) + ').');
Exit;
end;
Result := ((Codigo = 0) or (Codigo = 1638) or (Codigo = 3010)) and VcRuntimeOk();
end;
`,

	"InitializeSetup": `
function InitializeSetup(): Boolean;
begin
PrecisaDeVc := not VcRuntimeOk();
VcPronto := not PrecisaDeVc;
Result := True;
end;
`,

	"PodeAbrirAgora": `
function PodeAbrirAgora(): Boolean;
begin
Result := VcPronto;
end;
`,

	"NextButtonClick": `
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
`,

	"PrepareToInstall": `
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
Result := '';
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
`,

	"RemoverAspas": `
function RemoverAspas(const Texto: String): String;
begin
Result := Trim(Texto);
if (Length(Result) >= 2) and (Result[1] = '"') and (Result[Length(Result)] = '"') then
Result := Copy(Result, 2, Length(Result) - 2);
end;
`,

	"LimparInicioAutomatico": `
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
`,

	"PodeApagarOsDados": `
function PodeApagarOsDados(const Pasta: String): Boolean;
var
Local: String;
begin
Result := False;
Local := RemoveBackslash(ExpandConstant('{localappdata}'));
if Length(Local) < 4 then Exit;
if CompareText(Pasta, Local + '\{#PastaDeDados}') <> 0 then Exit;
if not DirExists(Pasta) then Exit;
if EhLinkOuJuncao(Pasta) then Exit;
if not DiretorioDoProgramaOk(ExpandConstant('{app}')) then Exit;
Result := True;
end;
`,

	"CredDeleteW": `
function CredDeleteW(TargetName: String; CredType, Flags: DWORD): BOOL;
external 'CredDeleteW@advapi32.dll stdcall';
`,

	"ApagarChaveDeApi": `
procedure ApagarChaveDeApi(const Alvo: String);
begin
if CredDeleteW(Alvo, CredTypeGeneric, 0) then
Log('Credencial ' + Alvo + ' apagada.')
else
Log('Credencial ' + Alvo + ' nao apagada (erro ' + IntToStr(DLLGetLastError()) + '; 1168 = nao existia).');
end;
`,

	"PerguntarEApagarOsDados": `
procedure PerguntarEApagarOsDados();
var
Pasta: String;
begin
if UninstallSilent() then Exit;
Pasta := PastaDosDados();
if MsgBox(FmtMessage(CustomMessage('PerguntaApagarDados'), [Pasta]), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) <> IDYES then Exit;
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
ApagarChaveDeApi('{#AlvoDaChaveNuvem}');
end;
`,

	"InitializeUninstall": `
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
`,

	"CurUninstallStepChanged": `
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
if CurUninstallStep = usUninstall then
LimparInicioAutomatico()
else if CurUninstallStep = usPostUninstall then
PerguntarEApagarOsDados();
end;
`,
}

// goldenDasSecoes são as seções do .iss que decidem o que o Setup instala, cria e executa.
var goldenDasSecoes = map[string]string{
	"Setup": `
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
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\{#NomeDoApp}
DefaultGroupName={#NomeDoApp}
DisableProgramGroupPage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir={#PastaSaida}
OutputBaseFilename=AuroraWhisper-Setup-{#VersaoDoApp}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ShowLanguageDialog=no
SetupIconFile=aurora-whisper.ico
UninstallDisplayIcon={app}\{#NomeDoExe}
SetupMutex=AuroraWhisperSetup
CloseApplications=no
RestartApplications=no
SetupLogging=yes
`,

	"Tasks": `
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
`,

	"InstallDelete": `
Type: filesandordirs; Name: "{app}\painel"; Check: JaTemOAgentNestaPasta
Type: filesandordirs; Name: "{app}\licencas"; Check: JaTemOAgentNestaPasta
`,

	"Icons": `
Name: "{autoprograms}\{#NomeDoApp}"; Filename: "{app}\{#NomeDoExe}"; WorkingDir: "{app}"
Name: "{autodesktop}\{#NomeDoApp}"; Filename: "{app}\{#NomeDoExe}"; WorkingDir: "{app}"; Tasks: desktopicon
`,

	"Run": `
Filename: "{app}\{#NomeDoExe}"; Description: "{cm:AbrirAgora}"; Flags: nowait postinstall skipifsilent; Check: PodeAbrirAgora
`,
}

// goldenDasEtapas são as etapas do workflow que decidem o que entra no Setup e se o job roda.
var goldenDasEtapas = map[string]string{
	"Portão da F2 (ADR-018)": `
- name: Portão da F2 (ADR-018)
shell: powershell
run: |
if ($env:F2_PRONTA -ne 'true') {
throw 'A F2 da ADR-018 (o whisper-server com cmd.Dir e o modelo pelo nome relativo) ainda não entrou. Sem ela o whisper-server.log instalado leva o caminho com o nome do usuário. Veja F2_PRONTA no topo deste arquivo.'
}
`,

	"Checkout": `
- name: Checkout
uses: actions/checkout@v4
with:
persist-credentials: false
`,

	"Versão da tag": `
- name: Versão da tag
shell: powershell
run: |
$tag = $env:GITHUB_REF_NAME
if ($tag -notmatch '^v(\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?)$') {
throw "A tag '$tag' não é uma versão (v0.1.0 ou v0.1.0-rc.1)."
}
"VERSAO=$($Matches[1])" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
Write-Host "Versão: $($Matches[1])"
`,

	"Baixar o zip do whisper.cpp (SHA-256 fixo)": `
- name: Baixar o zip do whisper.cpp (SHA-256 fixo)
shell: powershell
env:
GH_TOKEN: ${{ github.token }}
run: |
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path thirdparty | Out-Null
$zip = 'thirdparty\whisper-bin-x64.zip'
$esperado = $env:WHISPER_ZIP_SHA256
$ok = $false
try {
Invoke-WebRequest -UseBasicParsing -Uri $env:WHISPER_ZIP_URL -OutFile $zip
$ok = ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ieq $esperado)
if (-not $ok) { Write-Host "O zip do upstream tem outro SHA-256: $((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash)" }
} catch {
Write-Host "Não consegui baixar do upstream: $_"
}
if (-not $ok) {
Write-Host "Tentando o espelho no release '$env:MODELO_RELEASE' deste repositório."
gh release download $env:MODELO_RELEASE --repo $env:GITHUB_REPOSITORY --pattern 'whisper-bin-x64.zip' --dir thirdparty --clobber
if ($LASTEXITCODE -eq 0) {
$ok = ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ieq $esperado)
}
}
if (-not $ok) { throw "O zip do whisper.cpp não veio de lugar nenhum com o SHA-256 $esperado." }
Expand-Archive -LiteralPath $zip -DestinationPath 'thirdparty\whisper' -Force
if (-not (Test-Path -LiteralPath 'thirdparty\whisper\Release\whisper-server.exe')) {
throw 'O zip não trouxe Release\whisper-server.exe.'
}
`,

	"Baixar o modelo (release fixo do projeto, SHA-256 fixo)": `
- name: Baixar o modelo (release fixo do projeto, SHA-256 fixo)
shell: powershell
env:
GH_TOKEN: ${{ github.token }}
run: |
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path 'models\quant' | Out-Null
gh release download $env:MODELO_RELEASE --repo $env:GITHUB_REPOSITORY --pattern $env:MODELO_ARQUIVO --dir 'models\quant' --clobber
if ($LASTEXITCODE -ne 0) {
throw "Não consegui baixar $env:MODELO_ARQUIVO do release '$env:MODELO_RELEASE'. Esse release existe? (TODO no topo deste arquivo)"
}
$baixado = Join-Path 'models\quant' $env:MODELO_ARQUIVO
$destino = 'models\quant\small-q4_0.bin'
Move-Item -LiteralPath $baixado -Destination $destino -Force
$tamanho = (Get-Item -LiteralPath $destino).Length
$hash = (Get-FileHash -LiteralPath $destino -Algorithm SHA256).Hash
if ($tamanho -ne [int64]$env:MODELO_BYTES) { throw "O modelo tem $tamanho bytes; esperado $env:MODELO_BYTES." }
if ($hash -ine $env:MODELO_SHA256) { throw "O modelo tem SHA-256 $hash; esperado $env:MODELO_SHA256." }
Write-Host "Modelo conferido: $tamanho bytes, SHA-256 $hash"
`,

	"Montar o pacote e compilar o Setup": `
- name: Montar o pacote e compilar o Setup
shell: powershell
run: .\scripts\montar-instalador.ps1 -Versao $env:VERSAO -ExigirIscc
`,
}
