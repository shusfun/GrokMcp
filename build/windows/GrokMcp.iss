#define AppName "Grok Supervisor"
#define AppPublisher "GrokMcp"
#define AppExeName "GrokMcp.exe"

#ifndef MyAppVersion
  #define MyAppVersion "0.1.0"
#endif
#ifndef SourceDir
  #define SourceDir "dist\windows-amd64"
#endif
#ifndef OutputDir
  #define OutputDir "dist"
#endif

[Setup]
AppId={{B4A9D8B1-8E9D-4A0D-9F9F-0D4E6B1C7A22}
AppName={#AppName}
AppVersion={#MyAppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={autopf}\Grok Supervisor
DefaultGroupName={#AppName}
UninstallDisplayName={#AppName}
OutputBaseFilename=GrokMcp-{#MyAppVersion}-windows-amd64
OutputDir={#OutputDir}
ArchitecturesInstallIn64BitMode=x64
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
CloseApplications=yes
Uninstallable=yes

[Files]
Source: "{#SourceDir}\{#AppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExeName}"; WorkingDir: "{app}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; WorkingDir: "{app}"

[Run]
Filename: "{app}\{#AppExeName}"; Description: "启动 {#AppName}"; Flags: nowait postinstall skipifsilent

[Code]
function InitializeSetup(): Boolean;
var
  Uninstaller: String;
  ResultCode: Integer;
begin
  Result := True;
  Uninstaller := ExpandConstant('{uninstallexe}');
  if FileExists(Uninstaller) then begin
    if Exec(Uninstaller, '/SILENT', '', SW_SHOW, ewWaitUntilTerminated, ResultCode) then
      Result := False;
  end;
end;
