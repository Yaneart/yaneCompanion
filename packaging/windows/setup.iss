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
  #define MinimumWindowsVersion "10.0.19041"
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
WizardStyle=modern dark windows11 includetitlebar hidebevels
WizardBackColor=#101112
WizardImageFile=wizard-panel.png
WizardImageBackColor=#111720
WizardSmallImageFile=wizard-mark.png
WizardSmallImageBackColor=#101112
WizardSizePercent=110,110
DisableWelcomePage=no
DisableProgramGroupPage=yes
#ifdef SignedRelease
SignTool=owner
SignedUninstaller=yes
#endif

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"

[Messages]
en.WelcomeLabel1=Install [name]
en.WelcomeLabel2=The wizard will install [name] on this computer.
en.SelectDirBrowseLabel=Click Browse to choose another folder.
en.ReadyLabel1=The wizard is ready to install [name].
en.InstallingLabel=Installing [name] files.
en.FinishedHeadingLabel=[name] is installed
ru.WelcomeLabel1=Установка [name]
ru.WelcomeLabel2=Мастер установит [name] на этот компьютер.
ru.SelectDirBrowseLabel=Чтобы выбрать другую папку, нажмите «Обзор».
ru.ReadyLabel1=Мастер готов установить [name].
ru.InstallingLabel=Копируем файлы [name].
ru.FinishedHeadingLabel=[name] установлен

[CustomMessages]
en.LaunchCompanion=Launch yaneCompanion
ru.LaunchCompanion=Запустить yaneCompanion

[Files]
Source: "{#BundleDir}\Manrope.ttf"; DestName: "wizard-manrope.ttf"; Flags: dontcopy
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
Filename: "{app}\yanecompanion.exe"; Description: "{cm:LaunchCompanion}"; Flags: postinstall nowait skipifsilent

[Code]
const
  FR_PRIVATE = $10;

var
  ClearCache, ClearPairings: Boolean;
  WizardFontPath: String;

function AddFontResourceEx(FileName: String; Flags, Reserved: LongWord): Integer;
  external 'AddFontResourceExW@gdi32.dll stdcall setuponly';
function RemoveFontResourceEx(FileName: String; Flags, Reserved: LongWord): Boolean;
  external 'RemoveFontResourceExW@gdi32.dll stdcall setuponly';

procedure InitializeWizard();
begin
  ExtractTemporaryFile('wizard-manrope.ttf');
  WizardFontPath := ExpandConstant('{tmp}\wizard-manrope.ttf');
  if AddFontResourceEx(WizardFontPath, FR_PRIVATE, 0) = 0 then begin
    WizardFontPath := '';
    Exit;
  end;
  WizardForm.Font.Name := 'Manrope';
  WizardForm.WelcomeLabel1.Font.Name := 'Manrope';
  WizardForm.PageNameLabel.Font.Name := 'Manrope';
  WizardForm.FinishedHeadingLabel.Font.Name := 'Manrope';
end;

procedure DeinitializeSetup();
begin
  if WizardFontPath <> '' then
    RemoveFontResourceEx(WizardFontPath, FR_PRIVATE, 0);
end;

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
