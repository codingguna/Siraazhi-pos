; =====================================================================
; Siraazhi Pro POS™ — Professional Windows Setup Script (Inno Setup)
; Unified Standard Installer & Maintenance Engine for Windows 10 & 11 (64-bit)
; Modeled after BioVault™ Standard Deployment Pipeline
; =====================================================================

#define MyAppName "Siraazhi Pro POS"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "Vanur Farmer Producer Company Limited"
#define MyAppURL "https://siraazhi.netlify.app"
#define MyAppExeName "siraazhi-pos.exe"
#define MyAppId "{E681C49B-5A38-4F2E-9D4A-3C981452D120}"

[Setup]
AppId={{E681C49B-5A38-4F2E-9D4A-3C981452D120}}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\SiraazhiPOS
DefaultGroupName={#MyAppName}
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SetupIconFile=app.ico
UninstallDisplayIcon={app}\app.ico
OutputDir=output
OutputBaseFilename=SiraazhiPOS_Setup_v1.0.0
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
DisableWelcomePage=no
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "autostart"; Description: "Automatically start Siraazhi POS background service when Windows starts"; GroupDescription: "Windows Integration"

[Dirs]
; Protected data directories for SQLite database and automatic daily backups
Name: "{commonappdata}\SiraazhiPOS"; Flags: uninsneveruninstall
Name: "{commonappdata}\SiraazhiPOS\data"; Flags: uninsneveruninstall
Name: "{commonappdata}\SiraazhiPOS\backups"; Flags: uninsneveruninstall

[Files]
Source: "siraazhi-pos.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "start_silent.vbs"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "app.ico"; DestDir: "{app}"; Flags: ignoreversion

[InstallDelete]
; Clean up old divergent shortcuts from previous versions so only one unified shortcut exists
Type: files; Name: "{commonprograms}\Siraazhi Pro POS.lnk"
Type: files; Name: "{commonprograms}\SiraazhiPOS\Siraazhi Pro POS.lnk"
Type: files; Name: "{userprograms}\Siraazhi Pro POS.lnk"
Type: files; Name: "{userprograms}\SiraazhiPOS\Siraazhi Pro POS.lnk"

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""; IconFilename: "{app}\app.ico"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""; IconFilename: "{app}\app.ico"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "SiraazhiPOS"; ValueData: """{app}\{#MyAppExeName}"" -headless -port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent runascurrentuser

[UninstallRun]
; Terminate processes before file removal
Filename: "taskkill"; Parameters: "/f /im {#MyAppExeName}"; Flags: runhidden; RunOnceId: "KillAppProcs"

[Code]
// -------------------------------------------------------------------
// Helper: Execute a command and wait for completion
// -------------------------------------------------------------------
function ExecCommand(const Filename, Params: String; var ResultCode: Integer): Boolean;
begin
  Result := Exec(Filename, Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// -------------------------------------------------------------------
// Safe Step Handler: Flush SQLite WAL before updating binaries
// -------------------------------------------------------------------
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssInstall then
  begin
    // 1. Ask active server to checkpoint WAL to disk before stopping
    ExecCommand('powershell.exe', '-NoProfile -Command "try { Invoke-RestMethod -Uri http://127.0.0.1:8080/api/system/checkpoint -Method POST -TimeoutSec 1 } catch {}"', ResultCode);
    // 2. Terminate running POS instances before copying new binary
    ExecCommand('taskkill.exe', '/f /im {#MyAppExeName}', ResultCode);
  end;
end;

// -------------------------------------------------------------------
// Post-Uninstall Cleanup Handler: Prompt before deleting database
// -------------------------------------------------------------------
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: String;
  DelChoice: Integer;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    DataDir := ExpandConstant('{commonappdata}\SiraazhiPOS\data');
    if DirExists(DataDir) then
    begin
      DelChoice := MsgBox('Do you also want to DELETE all sales records, customer bills, and product database in ' + DataDir + '?'#13#10#13#10 +
                          '(Click [Yes] to permanently delete all store data, or [No] to safely keep records for future reinstallation)',
                          mbConfirmation, MB_YESNO);
      if DelChoice = IDYES then
      begin
        DelTree(ExpandConstant('{commonappdata}\SiraazhiPOS'), True, True, True);
      end;
    end;
  end;
end;