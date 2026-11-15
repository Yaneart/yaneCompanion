#ifndef BundleDir
  #error BundleDir is required
#endif
#ifndef OutputDir
  #error OutputDir is required
#endif
#ifndef AppVersion
  #define AppVersion "0.3.0"
#endif
#ifndef MinimumWindowsVersion
  #define MinimumWindowsVersion "10.0.22000"
#endif

[Setup]
AppId={{F80F3701-C49E-4AF4-A12B-E51FA56EE2D0}
AppName=yaneCompanion
AppVersion={#AppVersion}
AppPublisher=yaneMedia
DefaultDirName={localappdata}\Programs\yaneCompanion
DefaultGroupName=yaneCompanion
PrivilegesRequired=lowest
SetupArchitecture=x64
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion={#MinimumWindowsVersion}
OutputDir={#OutputDir}
OutputBaseFilename=yanecompanion-{#AppVersion}-windows-amd64-setup
SetupIconFile={#BundleDir}\yanecompanion.ico
UninstallDisplayIcon={app}\yanecompanion.exe
Compression=lzma2
SolidCompression=yes
CloseApplications=no
RestartApplications=no
AppMutex=Local\yaneCompanion.Runtime
WizardStyle=modern
DisableProgramGroupPage=yes
#ifdef SignedRelease
SignTool=owner
SignedUninstaller=yes
#endif

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"

[Files]
Source: "{#BundleDir}\*"; DestDir: "{app}"; Excludes: "*.exe~"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\yaneCompanion"; Filename: "{app}\yanecompanion.exe"
Name: "{group}\Удалить yaneCompanion"; Filename: "{uninstallexe}"

[Registry]
Root: HKCU; Subkey: "Software\Classes\yanecompanion"; ValueType: string; ValueData: "URL:yaneCompanion"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\yanecompanion"; ValueName: "URL Protocol"; ValueType: string; ValueData: ""
Root: HKCU; Subkey: "Software\Classes\yanecompanion\DefaultIcon"; ValueType: string; ValueData: "{app}\yanecompanion.exe,0"
Root: HKCU; Subkey: "Software\Classes\yanecompanion\shell\open\command"; ValueType: string; ValueData: """{app}\yanecompanion.exe"" ""%1"""

[Run]
Filename: "{app}\yanecompanion.exe"; Description: "Запустить yaneCompanion"; Flags: postinstall nowait skipifsilent

[Code]
var
  ClearCache, ClearPairings: Boolean;

function InitializeUninstall(): Boolean;
begin
  Result := not CheckForMutexes('Local\yaneCompanion.Runtime');
  if not Result then MsgBox('Закройте yaneCompanion через «Выход», чтобы остановить просмотр перед удалением.', mbInformation, MB_OK);
  if Result and not UninstallSilent then begin
    ClearCache := MsgBox('Удалить кэш просмотра и загруженные пакеты обновления? При выборе «Нет» они сохранятся.', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
    ClearPairings := MsgBox('Удалить сохранённые подключения сайтов? При выборе «Нет» повторная установка сохранит подключения. Настройки сайта сохраняются.', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Arguments: String;
  ExitCode: Integer;
begin
  if CurUninstallStep = usUninstall then begin
    if ClearCache or ClearPairings then begin
      Arguments := 'clean-data';
      if ClearCache then Arguments := Arguments + ' --cache';
      if ClearPairings then Arguments := Arguments + ' --pairings';
      if not Exec(ExpandConstant('{app}\yanecompanion.exe'), Arguments, '', SW_HIDE, ewWaitUntilTerminated, ExitCode) then
        RaiseException('Не удалось очистить выбранные данные. Удаление остановлено.');
      if ExitCode <> 0 then
        RaiseException('Данные не очищены. Закройте помощник и повторите удаление.');
    end;
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'yaneCompanion');
  end;
end;
