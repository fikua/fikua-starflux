; Inno Setup script for the Starflux Windows installer.
; Built by .github/workflows/release.yml on windows-latest, after
; `fyne package` produces the packaged .exe in SrcDir.

#define MyAppName "Starflux"
#define MyAppPublisher "Fikua"
#define MyAppURL "https://github.com/fikua/fikua-starflux"
#define MyAppExeName "starflux.exe"

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif
#ifndef SrcDir
  #define SrcDir "..\..\fyne-cross\dist\windows-amd64"
#endif
#ifndef OutDir
  #define OutDir "..\..\release"
#endif

[Setup]
AppId={{6C7A6A9E-6C7C-4B9D-9B9F-8C6A1C7A2F10}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
OutputDir={#OutDir}
OutputBaseFilename=starflux-windows-amd64-setup
Compression=lzma
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
DisableProgramGroupPage=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "{#SrcDir}\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional shortcuts:"

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "Launch {#MyAppName}"; Flags: nowait postinstall skipifsilent
