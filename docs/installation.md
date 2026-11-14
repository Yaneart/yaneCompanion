# Установка и обслуживание yaneCompanion

Текущая версия `0.3.0-dev` (LC-011). LC-010 принят как реализация 2026-11-09.
Пакеты ещё не опубликованы; LC-011 проверен одним tester и ожидает приёмки. Windows 11 и native
Linux desktop остаются отдельными gates LC-014.

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

- `yanecompanion-0.3.0-windows-amd64-setup.exe` — per-user Inno installer;
- `yanecompanion_0.3.0_amd64.deb`;
- `yanecompanion-bin-0.3.0-1-x86_64.pkg.tar.zst` — та же identity, что в AUR;
- `yanecompanion-0.3.0-linux-amd64.tar.gz` и `aur/PKGBUILD`, `.install`;
- `packages.json` — размер и SHA-256 фактически собранных файлов.

Без `-ReleaseBaseUrl` AUR directory использует локальный archive для проверки
через makepkg. Для публикуемого recipe задайте окончательный HTTPS base URL;
archive hash будет внесён в PKGBUILD. Старая `.SRCINFO` при пересборке удаляется;
новая генерируется владельцем через
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
VITE_COMPANION_DOWNLOAD_BASE_URL=https://downloads.example/releases/0.3.0
VITE_COMPANION_DOWNLOAD_VERSION=0.3.0
```

Пустая/невалидная конфигурация не создаёт фиктивных download links. Выбор ОС
всегда доступен вручную. Ошибка соединения не объявляется отсутствием установки.
Сайт использует только фиксированную ссылку `yanecompanion://open`. Произвольные
URL arguments, origin, code и credentials через protocol handler не принимаются.

## Подпись и внешняя публикация

Выбран **Authenticode с SHA-256 и RFC 3161 timestamp** для Windows host,
installer и uninstaller. `-Release` требует `-SiteOrigin`, `-ReleaseBaseUrl`,
`-UpdateFeedUrl`, `-UpdatePublicKey`, а для Windows также `-SignTool`,
`-SigningThumbprint` и `-TimestampUrl`; приватный ключ остаётся в
certificate store/token владельца, в аргументах используется только thumbprint.
Перед подписью host пересобирается; compiler получает тот же signing command.
Unsigned development packages не выдаются за подписанный release.

Реальный сертификат, timestamp service и release hosting не предоставлены.
Лицензия собственного кода также пока не выбрана владельцем (это существующее
ограничение поставки); полный inventory и corresponding sources включены в
development package. Эти пункты нельзя заменить самоподписанным сертификатом,
фиктивным publisher или автоматической публикацией. Подписанный manifest/update
protocol и расширенная диагностика реализованы в LC-011; реальная production-подпись
не заявляется.

## Обновление, откат и удаление (LC-011)

В native-окне выберите «Обслуживание» → «Проверить обновления». Проверок при
запуске/без действия пользователя нет. Полный пакет и предыдущая версия скачиваются
отдельными кнопками. Загрузка блокируется при активной torrent session, включая
подготовку; проверка наличия обновления не меняет просмотр. «Выход» отменяет загрузку.
Сбой или повторный запрос не оставляет готового файла с непроверенными байтами.

После успешной проверки откройте папку пакетов. В `current/` лежит текущий пакет,
в `previous/` — явно запрошенный пакет для отката; рядом сохраняется подписанный
`manifest.json`. Установка только после явного «Выход»: Windows — открыть `.exe`,
Debian/Ubuntu — пакетный менеджер / `sudo apt install ./<package>.deb`, Arch —
`sudo pacman -U ./<package>.pkg.tar.zst`. Пакет для отката установите тем же способом;
на Debian при необходимости подтвердите downgrade, на Arch также допустим `yay`
для обычного обновления после реальной публикации AUR. Если новая версия не
запускается, закройте её и установите заранее сохранённый previous package либо
скачайте предыдущий release из канала владельца. Никаких shell/script команд из
манифеста не исполняется. Автоматическая установка и перезапуск отсутствуют.

Обновление и откат сохраняют `desktop.json` (site), `pairings.json`, профиль и
явный выбор автозапуска. Формат настроек остаётся schema 1; существующие файлы
LC-010 без schema читаются без миграции/перезаписи. Пакеты с другой settings schema
или API major не предлагаются к установке этим updater. Неизвестная явно записанная
schema настроек отклоняется, а не заменяется defaults. При несовместимом API сайт
показывает установленную версию и различает обновление сайта/помощника; минимальная
поддерживаемая helper version на сайте — 0.2.0, development suffix допускается.

«Диагностика» в native-окне показывает version/platform/API/settings schema,
версии engine/media tools и allowlisted startup/playback/update codes. Отчёт
можно выделить в GTK и скопировать Ctrl+C из Windows message box. На сайте есть
раскрываемая «Диагностика подключения», различающая browser permission, origin,
сопряжение и несовместимость. Отчёты не сериализуют snapshots/errors целиком и не
содержат magnet/hash фильма, file names/paths, origins, pairing/controller tokens
или аккаунт. Ошибка установки отличается от ошибки запуска/воспроизведения.

Перед удалением в «Обслуживании» выберите отдельно очистку кэша и сохранённых
подключений, затем «Подготовить удаление и выйти». Активную session нужно остановить
на сайте. Действие выключает autostart, закрывает helper и очищает выбранные данные
под runtime lock; сайт не может его вызвать. Без флажков данные сохраняются.
Затем используйте удаление программы в Windows, `sudo apt remove yanecompanion`
или `sudo pacman -R yanecompanion-bin`. Package hooks не удаляют профили других
пользователей; Linux package managers сохраняют пользовательские данные по умолчанию.
Windows interactive uninstaller предлагает оба выбора; silent uninstall сохраняет
данные. Настройки сайта сохраняются, pairing удаляется только по явному выбору.
CLI-эквивалент после закрытия всех GUI/runtime: `yanecompanion clean-data --cache`
и/или `--pairings`. Нет флагов — отказ. Очистка кэша удаляет только помеченные session
директории и update slots `current`/`previous`; неизвестные файлы и state checkpoint
сохраняются, в том числе при совпадающих XDG state/cache roots. Desktop lock
включает startup-error окно: сначала его нужно закрыть.

### Authenticity и выпуск владельцем

Сборка принимает пару `-UpdateFeedUrl https://.../release.json` и
`-UpdatePublicKey <64 lowercase hex characters>`. Публичный ключ закрепляется
в host, канал/ключ не поступают с сайта, HTTP API или сетевого manifest. Без пары
настроек выводится «Обновления пока не опубликованы владельцем». Production `-Release`
требует эту пару. Приватный Ed25519 seed владелец хранит вне checkout/bundle.

`scripts/release-manifest.ps1 -Version 0.3.0 -Sequence <increasing integer>
-ReleaseBaseUrl https://.../0.3.0 -PublicKey <public hex> -KeyFile <external seed file>
-Go .\.tools\go\bin\go.exe [-PreviousManifest <prior release.json>]` вычисляет
size/SHA-256 всех четырёх фактических пакетов и подписывает `dist/packages/release.json`.
Все current/previous packages должны присутствовать в `dist/packages`; старые файлы
сохраняются при новой сборке. Сначала соберите Windows и Linux последовательно;
одновременная запись в общий output не поддерживается. Windows package должен иметь
действительную Authenticode signature. Release script не публикует файлы и не создаёт
ключи. Seed file — 32 bytes в hex, public key обязан соответствовать ему.
Сначала публикуются packages с versioned URLs, затем manifest по стабильному feed URL;
предыдущие packages остаются доступны. Поддерживайте sequence монотонным, обновляйте
подпись до expiry (14 дней по умолчанию; runtime допускает не более 30 дней).

Envelope: `{payload: base64(canonical JSON), signature: base64(Ed25519 signature)}`;
подпись покрывает `yaneCompanion release manifest v1\n` + exact payload bytes.
Payload: schema 1, sequence, publishedAt/expiresAt, current и optional previous;
release: version, api `{major,minor}`, settingsSchema, packages; package: platform,
name, HTTPS URL, size, sha256. Platforms: windows-amd64/linux-deb-amd64/
linux-arch-amd64/linux-archive-amd64. Runtime отвергает unknown/duplicate JSON fields,
просроченный/будущий feed, replay меньшего sequence и иной payload с тем же sequence.
High-water checkpoint записывается атомарно в state/updates и переживает reinstall.
Redirects, credentials/query/fragment, scripts/произвольные имена, implicit downgrade
и packages >512 MiB запрещены. Два cache slots дают максимум 1 GiB update cache,
плюс bounded manifest metadata, отдельно от media budget; новый transfer в slot заменяет старый. HTTPS/hash не
заменяют подпись. Ротация pinned key требует нового trusted полного пакета.

Низкоуровневый owner tool: `go run ./cmd/release-manifest -manifest <signed.json>
-public-key <hex> [-packages <directory>]` проверяет подпись и локальные packages.
Для signing принимает unsigned JSON input, `-key-file`, `-packages`, `-out`.
`-historical` разрешён только offline verification архивного manifest владельцем;
live updater всегда проверяет expiry. Этим способом старый подписанный release
переносится в свежий manifest как explicit previous без отключения freshness feed.
Документация API: [Go Ed25519](https://pkg.go.dev/crypto/ed25519),
[Go Root](https://pkg.go.dev/os#Root),
[Inno uninstall events](https://jrsoftware.org/ishelp/topic_scriptevents.htm).

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
