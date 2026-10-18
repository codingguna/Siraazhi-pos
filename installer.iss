; Inno Setup Script for Siraazhi Pro POS
; This script generates a standard professional Windows Setup wizard (SiraazhiPOS_Setup.exe)
; identical to how Python, Chrome, and VS Code installers operate.

#define MyAppName "Siraazhi Pro POS"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "Vanur Farmer Producer Company Limited"
#define MyAppURL "https://siraazhi.netlify.app"
#define MyAppExeName "siraazhi-pos.exe"

[Setup]
AppId={{E681C49B-5A38-4F2E-9D4A-3C981452D120}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\SiraazhiPOS
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
OutputDir=.
OutputBaseFilename=SiraazhiPOS_Setup
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "autostart"; Description: "Automatically start Siraazhi POS background service when Windows starts"; GroupDescription: "Windows Integration"

[Files]
Source: "siraazhi-pos.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "start_silent.vbs"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "SiraazhiPOS"; ValueData: """{app}\{#MyAppExeName}"" -headless -port 8080 -db ""{commonappdata}\SiraazhiPOS\data\pos.db"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent
