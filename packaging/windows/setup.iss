#ifndef BundleDir
  #error BundleDir is required
#endif
#ifndef OutputDir
  #error OutputDir is required
#endif
#ifndef AppVersion
  #define AppVersion "0.3.1"
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
WizardStyle=modern dynamic windows11 includetitlebar hidebevels
WizardBackColor=#EBEAE5
WizardBackColorDynamicDark=#101112
WizardImageFile=
WizardImageFileDynamicDark=
WizardSmallImageFile=
WizardSmallImageFileDynamicDark=
WizardSizePercent=130,110
ShowLanguageDialog=no
DisableWelcomePage=no
DisableProgramGroupPage=yes
AlwaysShowDirOnReadyPage=yes
#ifdef SignedRelease
SignTool=owner
SignedUninstaller=yes
#endif

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"

[Messages]
en.ButtonNext=&Continue
en.ClickNext=
en.WelcomeLabel1=Install the companion
en.WelcomeLabel2=Watch local streams on yaneMedia with this Windows companion.
en.SelectDirLabel3=
en.SelectDirBrowseLabel=
en.WizardSelectTasks=Shortcuts
en.SelectTasksDesc=Choose where to open Yane.
en.SelectTasksLabel2=A shortcut is always added to the Start menu.
en.ReadyLabel1=Review the destination folder and shortcuts.
en.ReadyLabel2a=
en.ReadyLabel2b=
en.InstallingLabel=Installing [name] files.
en.FinishedHeadingLabel=Ready to watch
en.FinishedLabel=Open yaneCompanion and allow access from yaneMedia when prompted.
en.FinishedLabelNoIcons=Open yaneCompanion and allow access from yaneMedia when prompted.
en.ClickFinish=
ru.ButtonNext=&Продолжить
ru.ClickNext=
ru.WelcomeLabel1=Установка помощника
ru.WelcomeLabel2=Смотрите торренты на yaneMedia через приложение для Windows.
ru.SelectDirLabel3=
ru.SelectDirBrowseLabel=
ru.WizardSelectTasks=Ярлыки
ru.SelectTasksDesc=Выберите, откуда запускать Yane.
ru.SelectTasksLabel2=Ярлык в меню «Пуск» создаётся автоматически.
ru.ReadyLabel1=Проверьте папку установки и выбранные ярлыки.
ru.ReadyLabel2a=
ru.ReadyLabel2b=
ru.InstallingLabel=Копируем файлы [name].
ru.FinishedHeadingLabel=Всё готово к просмотру
ru.FinishedLabel=Откройте yaneCompanion и разрешите подключение с yaneMedia, когда появится запрос.
ru.FinishedLabelNoIcons=Откройте yaneCompanion и разрешите подключение с yaneMedia, когда появится запрос.
ru.ClickFinish=

[CustomMessages]
en.LaunchCompanion=Launch yaneCompanion
ru.LaunchCompanion=Запустить yaneCompanion
en.DesktopShortcut=Create a Yane desktop shortcut
ru.DesktopShortcut=Создать ярлык «Yane» на рабочем столе
en.UninstallShortcut=Uninstall Yane
ru.UninstallShortcut=Удалить Yane

[Tasks]
Name: "desktopicon"; Description: "{cm:DesktopShortcut}"; Flags: unchecked; Check: DesktopShortcutAvailable

[Files]
Source: "{#BundleDir}\Manrope.ttf"; DestName: "wizard-manrope.ttf"; Flags: dontcopy
Source: "..\..\internal\desktop\banner.png"; DestName: "wizard-banner.png"; Flags: dontcopy
Source: "..\..\internal\desktop\banner-light.png"; DestName: "wizard-banner-light.png"; Flags: dontcopy
Source: "{#BundleDir}\*"; DestDir: "{app}"; Excludes: "*.exe~"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\Yane"; Filename: "{app}\yanecompanion.exe"
Name: "{group}\{cm:UninstallShortcut}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Yane"; Filename: "{app}\yanecompanion.exe"; Tasks: desktopicon; Check: DesktopShortcutAvailable

[InstallDelete]
Type: files; Name: "{group}\yaneCompanion.lnk"
Type: files; Name: "{group}\Удалить yaneCompanion.lnk"

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

function DesktopShortcutAvailable(): Boolean;
begin
  Result := not FileExists(ExpandConstant('{autodesktop}\Yane.lnk'));
end;

procedure LayoutActionButtons();
var
  ButtonWidth, ButtonHeight, Gap, Right: Integer;
begin
  ButtonWidth := ScaleX(105);
  ButtonHeight := ScaleY(28);
  Gap := ScaleX(8);
  Right := WizardForm.ClientWidth - ScaleX(20);
  WizardForm.CancelButton.SetBounds(Right - ButtonWidth, WizardForm.ClientHeight - ButtonHeight - ScaleY(12), ButtonWidth, ButtonHeight);
  WizardForm.NextButton.SetBounds(WizardForm.CancelButton.Left - ButtonWidth - Gap, WizardForm.CancelButton.Top, ButtonWidth, ButtonHeight);
  WizardForm.BackButton.SetBounds(WizardForm.NextButton.Left - ButtonWidth - Gap, WizardForm.CancelButton.Top, ButtonWidth, ButtonHeight);
end;

procedure LayoutIntroPage(Heading, Body: TNewStaticText);
var
  Margin, Width: Integer;
begin
  Margin := ScaleX(20);
  Width := Heading.Parent.ClientWidth - 2 * Margin;
  Heading.Font.Size := 16;
  Heading.SetBounds(Margin, ScaleY(16), Width, ScaleY(32));
  Body.SetBounds(Margin, ScaleY(52), Width, Body.Height);
end;

procedure AddWatermark();
var
  Clip: TPanel;
  Watermark: TBitmapImage;
  BannerFile: String;
begin
  if HighContrastActive then Exit;
  Clip := TPanel.Create(WizardForm);
  Clip.Parent := WizardForm;
  Clip.StyleElements := [];
  Clip.ParentBackground := False;
  Clip.BevelOuter := bvNone;
  if IsDarkInstallMode then begin
    Clip.Color := $121110;
    BannerFile := 'wizard-banner.png';
  end else begin
    Clip.Color := $E5EAEB;
    BannerFile := 'wizard-banner-light.png';
  end;
  ExtractTemporaryFile(BannerFile);
  Clip.SetBounds(WizardForm.ClientWidth - ScaleX(167), 0, ScaleX(147), ScaleY(48));
  Watermark := TBitmapImage.Create(Clip);
  Watermark.Parent := Clip;
  Watermark.PngImage.LoadFromFile(ExpandConstant('{tmp}\' + BannerFile));
  Watermark.BackColor := Clip.Color;
  Watermark.Stretch := True;
  { Show only the right half of the shared banner, which holds the watermark. }
  Watermark.SetBounds(-Clip.Width, 0, 2 * Clip.Width, Clip.Height);
end;

procedure InitializeWizard();
var
  Brand: TNewStaticText;
  HeaderHeight: Integer;
begin
  ExtractTemporaryFile('wizard-manrope.ttf');
  WizardFontPath := ExpandConstant('{tmp}\wizard-manrope.ttf');
  if AddFontResourceEx(WizardFontPath, FR_PRIVATE, 0) <> 0 then begin
    WizardForm.Font.Name := 'Manrope';
    WizardForm.WelcomeLabel1.Font.Name := 'Manrope';
    WizardForm.PageNameLabel.Font.Name := 'Manrope';
    WizardForm.FinishedHeadingLabel.Font.Name := 'Manrope';
  end else begin
    WizardFontPath := '';
  end;

  AddWatermark();
  Brand := TNewStaticText.Create(WizardForm);
  Brand.Parent := WizardForm;
  Brand.Caption := 'yaneCompanion';
  Brand.Font.Size := 20;
  Brand.Font.Style := [fsBold];
  Brand.Left := ScaleX(20);
  Brand.Top := ScaleY(12);
  HeaderHeight := ScaleY(50);
  WizardForm.OuterNotebook.Height := WizardForm.OuterNotebook.Height - HeaderHeight;
  WizardForm.OuterNotebook.Top := WizardForm.OuterNotebook.Top + HeaderHeight;

  LayoutIntroPage(WizardForm.WelcomeLabel1, WizardForm.WelcomeLabel2);
  LayoutIntroPage(WizardForm.FinishedHeadingLabel, WizardForm.FinishedLabel);
  WizardForm.RunList.Left := ScaleX(20);
  WizardForm.RunList.Width := WizardForm.FinishedLabel.Width;
  WizardForm.SelectDirBitmapImage.Visible := False;
  WizardForm.SelectDirLabel.Visible := False;
  WizardForm.SelectDirBrowseLabel.Visible := False;
  WizardForm.DirEdit.Top := ScaleY(18);
  WizardForm.DirBrowseButton.Top := WizardForm.DirEdit.Top - ScaleY(2);
  WizardForm.ReadyMemo.WordWrap := True;
  WizardForm.ReadyMemo.ScrollBars := ssVertical;
  LayoutActionButtons();
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if CurPageID = wpSelectDir then begin
    WizardForm.ActiveControl := WizardForm.NextButton;
    WizardForm.DirEdit.SelStart := 0;
    WizardForm.DirEdit.SelLength := 0;
  end;
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
