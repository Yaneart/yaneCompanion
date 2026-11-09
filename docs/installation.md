# Установка yaneCompanion (LC-010)

Реализация для проверки, версия `0.2.0-dev`. Пакеты ещё не опубликованы; этап не
принят и не закрыт. Windows 11 и native Linux desktop остаются отдельными gates.

## Для зрителя

1. Выберите торрент на сайте. В окне подключения выберите Windows 11 x64,
   Debian 12 / Ubuntu 24.04 x64 или Arch Linux x64 и скачайте опубликованный пакет.
2. Windows: откройте `.exe`, установите текущему пользователю и оставьте запуск
   после установки. Debian/Ubuntu: откройте `.deb` в пакетном менеджере; затем
   запустите yaneCompanion из меню приложений. Arch: установите `.pkg.tar.zst`
   через pacman либо `yanecompanion-bin` через yay после публикации владельцем.
3. Уже установленный помощник можно открыть ссылкой на сайте. Ссылка только
   активирует приложение, не меняет адрес сайта и не подтверждает доступ.
4. В release адрес сайта задан сборкой. В development-сборке один раз сохраните
   точный HTTPS origin в окне помощника (без завершающего `/` и пути).
5. Нажмите «Подключить» на сайте, сравните адрес и шестизначный код с окном
   yaneCompanion, нажмите «Разрешить». Выбранный торрент продолжит загрузку на
   сайте. Код не вводится. Browser permission подтверждается отдельно.

Сопряжение сохраняется для origin, профиля браузера и пользователя ОС. Аккаунт
yaneMedia не требуется. Runtime tools поставляются внутри пакета; Node/Bun и
внешний FFmpeg не нужны. Изменение сайта отзывает прежние grants. «Отклонить» и
«Отозвать доступ» находятся в том же окне. Локальный отзыв обнаруживается сайтом
при следующем запросе/heartbeat (до 20 секунд при активном контроллере).

Закрытие окна скрывает его в трей; «Выход» останавливает приложение и просмотр.
Меню трея содержит состояние, управление, сайт и выход. Если desktop не
предоставляет tray host, Linux оставляет окно доступным; его можно свернуть.
Автозапуск выключен по умолчанию и включается только флажком пользователя.
Повторный запуск показывает существующее окно. Linux runtime от root запрещён.
Установщик/пакет не завершает активный просмотр автоматически: требуется «Выход».

## Поставка и сборка владельцем

`scripts/package.ps1` собирает host, проверяет все три tools по embedded lock,
включает notices, licenses, исходники, иконку yaneMedia и desktop launcher.
Иконка PNG — точная копия `yaneMedia/frontend/public/favicon.png`; ICO содержит
варианты 16/32/48/64/256 px этого же рисунка. Новая символика не создаётся.

Примеры из корня репозитория (PowerShell):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\package.ps1 -Go .\.tools\go\bin\go.exe -Target windows-amd64
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\package.ps1 -Go .\.tools\go\bin\go.exe -Target linux-amd64
```

Windows builder: Inno Setup **7.1.1** x64, `-Compiler` указывает на ISCC.exe.
Resource builder — go-winres **0.3.3** (build-only, автоматически устанавливается
в `.tools/bin` при отсутствии). Windows host — **amd64**, Win32 означает название
системного API, не разрядность. `-H=windowsgui` убирает консольное окно.
Linux builder использует Docker с закреплённым Debian image из
`packaging/linux/Dockerfile`; в установке зрителя Docker не участвует.

Результат в `dist/packages/`:

- `yanecompanion-0.2.0-windows-amd64-setup.exe` — per-user Inno installer;
- `yanecompanion_0.2.0_amd64.deb`;
- `yanecompanion-bin-0.2.0-1-x86_64.pkg.tar.zst` — та же identity, что в AUR;
- `yanecompanion-0.2.0-linux-amd64.tar.gz` и `aur/PKGBUILD`, `.install`;
- `packages.json` — размер и SHA-256 фактически собранных файлов.

Без `-ReleaseBaseUrl` AUR directory использует локальный archive для проверки
через makepkg. Для публикуемого recipe задайте окончательный HTTPS base URL;
archive hash будет внесён в PKGBUILD. `.SRCINFO` генерируется владельцем через
`makepkg --printsrcinfo` после окончательного выбора URL. Бинарники на устройстве
зрителя не компилируются. Pacman PreTransaction hook с `AbortOnFail` запрещает
обновление/удаление работающего помощника; одни `.install` callbacks для этого
недостаточны. Debian использует `preinst`/`prerm`. Package managers запускают
приложение только как обычного desktop пользователя через menu/URL handler.

PKGBUILD отключает `strip` и `debug`: изменение bundled executables пакетным
менеджером нарушает embedded SHA-256 lock. Это проверено реальной сборкой makepkg
и побайтовым сравнением всех трёх tools, а не только чтением recipe.

Linux dependencies: GTK 3, GLib, Ayatana AppIndicator, xdg-utils,
desktop-file-utils из репозиториев дистрибутива. GTK используется через purego
**0.10.2**, без C compiler или GTK headers у зрителя. Проверка наличия SNI host
защищает от исчезновения окна в GNOME без расширения индикаторов. Native GNOME,
KDE/Xfce и Wayland необходимо проверять отдельно от Xvfb/Openbox fixtures.

Для сайта после публикации задаются:

```dotenv
VITE_COMPANION_DOWNLOAD_BASE_URL=https://downloads.example/releases/0.2.0
VITE_COMPANION_DOWNLOAD_VERSION=0.2.0
```

Пустая/невалидная конфигурация не создаёт фиктивных download links. Выбор ОС
всегда доступен вручную. Ошибка соединения не объявляется отсутствием установки.
Сайт использует только фиксированную ссылку `yanecompanion://open`. Произвольные
URL arguments, origin, code и credentials через protocol handler не принимаются.

## Подпись и внешняя публикация

Выбран **Authenticode с SHA-256 и RFC 3161 timestamp** для Windows host,
installer и uninstaller. `-Release` требует `-SiteOrigin`, `-ReleaseBaseUrl`,
`-SignTool`, `-SigningThumbprint` и `-TimestampUrl`; приватный ключ остаётся в
certificate store/token владельца, в аргументах используется только thumbprint.
Перед подписью host пересобирается; compiler получает тот же signing command.
Unsigned development packages не выдаются за подписанный release.

Реальный сертификат, timestamp service и release hosting не предоставлены.
Лицензия собственного кода также пока не выбрана владельцем (это существующее
ограничение поставки); полный inventory и corresponding sources включены в
development package. Эти пункты нельзя заменить самоподписанным сертификатом,
фиктивным publisher или автоматической публикацией. Подписанный manifest/update
protocol и расширенная диагностика остаются LC-011.

## Проверка и границы

Focused checks и точные результаты LC-010 заносятся в `docs/verification.md`.
Установка/GUI на Windows 10 — development fixture с отдельным override
`MinimumWindowsVersion=10.0.19041`. Итоговый installer требует Windows 11
(`10.0.22000`) и отклоняет Windows 10. Fixture не является Windows 11 PASS.
Linux containers/Xvfb не являются native Debian/Ubuntu/Arch desktop PASS.
Публичный AUR/yay update проверяется после публикации владельцем; локальная
проверка PKGBUILD сама по себе его не доказывает.

Источники API/форматов: [Microsoft notification area](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shell_notifyiconw),
[GTK 3](https://docs.gtk.org/gtk3/), [purego](https://github.com/ebitengine/purego),
[Inno per-user setup](https://jrsoftware.org/ishelp/topic_setup_privilegesrequired.htm),
[Inno AppMutex](https://jrsoftware.org/ishelp/topic_setup_appmutex.htm),
[SignTool](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool),
[pacman hooks](https://man.archlinux.org/man/alpm-hooks.5.en).
