# Выпуск yaneCompanion 0.3.1

Репозиторий распространения — `Yaneart/yaneCompanion`, тег — `0.3.1`,
название выпуска — **yaneCompanion 0.3.1**.
Для обновлений предусмотрен отдельный `release.json` в публичной ветке `updates`.
Ветка позволяет обновлять подпись манифеста независимо от опубликованных пакетов.
Для выпуска требуется точный HTTPS-адрес yaneMedia. На странице GitHub в блоке About
укажите «Приложение для просмотра торрентов в yaneMedia» и адрес сайта.

## Сборка владельцем

Храните seed Ed25519 вне репозитория и сделайте резервную копию в защищённом
хранилище. Для сборки подготовьте публичный ключ и origin сайта. До сборки
сверьте [лицензии и состав поставки](../THIRD_PARTY_NOTICES.md).
Проверьте наличие архивов TorrServer и FFmpeg, патчей, инструкций сборки,
лицензий и списка версий сборочных пакетов. Отдельно проверьте материалы
для повторной линковки и соответствующие исходники статических runtime-библиотек
из закреплённого Debian snapshot.

```powershell
$go = (Resolve-Path .\.tools\go\bin\go.exe).Path
$version = '0.3.1'
$base = "https://github.com/Yaneart/yaneCompanion/releases/download/$version"
$feed = 'https://raw.githubusercontent.com/Yaneart/yaneCompanion/updates/release.json'
$release = @{
  Go = $go
  SiteOrigin = $siteOrigin
  ReleaseBaseUrl = $base
  UpdateFeedUrl = $feed
  UpdatePublicKey = $publicKey
  Release = $true
}
& .\scripts\package.ps1 @release -Target windows-amd64 -AllowUnsignedWindows
& .\scripts\package.ps1 @release -Target linux-amd64
& .\scripts\release-manifest.ps1 -Go $go -Version $version -Sequence 2 `
  -ReleaseBaseUrl $base -PublicKey $publicKey -KeyFile $keyFile `
  -PreviousManifest $previousManifest -AllowUnsignedWindows
```

`$previousManifest` должен указывать на опубликованный манифест `0.3.0`.
Для проверки отката положите исходные четыре пакета `0.3.0` в `dist/packages`
рядом с новыми пакетами. Проверьте `dist/packages/packages.json` и подпись `release.json` командой
`go run ./cmd/release-manifest -manifest dist/packages/release.json -public-key <public-key> -packages dist/packages`.
Для будущего выпуска с Authenticode не передавайте `-AllowUnsignedWindows`. При
сборке Windows вместо него задайте `-SignTool`, `-SigningThumbprint` и `-TimestampUrl`.

## GitHub, сайт и AUR

Готовый релиз можно опубликовать одной командой от имени владельца. Нужны
Python 3.11+, Go проекта и [GitHub CLI](https://cli.github.com/) с входом через
`gh auth login`. Подготовьте четыре установщика, `packages.json` и подписанный
`release.json`. Убедитесь, что коммит исходников сборки доступен на GitHub и
выбран локально как `HEAD`. При создании тега скрипт использует этот коммит.

```bash
python3 scripts/publish-release.py --publish
```

По умолчанию скрипт берёт файлы из `dist/packages` и публичный ключ из
`~/.local/share/yanecompanion-release/ed25519.pub`. Для отдельного комплекта
задайте `--packages`, `--manifest` и `--public-key-file`. Параметр
`--notes-file` задаёт описание нового выпуска. Запуск без `--publish` выполняет
локальную проверку подписи и файлов.

Скрипт создаёт или продолжает черновик GitHub Release, сверяет SHA-256 файлов и
публикует выпуск, затем обновляет `release.json` непосредственно в `updates`.
Повторный запуск с теми же файлами продолжает прерванную публикацию. Подписанный
манифест с увеличенным `sequence` можно опубликовать той же командой для
продления срока уже выпущенной версии. Публикация выполняется из аккаунта
владельца, подпись создаётся локальным инструментом из предыдущего раздела.

При ручной публикации выполните первые два пункта. После публикации GitHub
выполните шаги для сайта, AUR и приёмки.

1. Владелец открывает репозиторий и публикует релиз `0.3.1`. В Assets загружает четыре
   пакета из `packages.json` и сам `packages.json` с контрольными суммами. Проверяет
   анонимное скачивание и SHA-256 каждого файла. Опубликованные пакеты сохраняются
   по своим версионным URL. Исправленный пакет получает новую версию и тег.
2. Владелец публикует `release.json` в ветку `updates` после проверки пакетов.
   Проверяет загрузку манифеста по закреплённому адресу и через «Поддержка» в приложении.
3. Соберите Public frontend с `VITE_COMPANION_DOWNLOAD_BASE_URL`, равным `$base`,
   и `VITE_COMPANION_DOWNLOAD_VERSION=0.3.1`. В Docker Compose передайте их через
   окружение или корневой `.env`. Проверьте три кнопки загрузки и «Все версии на GitHub».
4. Для AUR возьмите `dist/packages/aur/PKGBUILD` и `yanecompanion-bin.install`.
   Под обычным пользователем Arch выполните `makepkg --printsrcinfo > .SRCINFO`,
   затем проверьте `makepkg --verifysource` и установку собранного пакета. После
   публикации проверьте `yay -S yanecompanion-bin` и обновление через yay.
5. На HTTPS-сайте пройдите чистую установку, запуск из меню и через ссылку сайта,
   разрешение локальной сети, сопряжение, воспроизведение и удаление. Проверьте
   загрузку пакета в «Поддержке», профиль после обновления и откат, когда доступна
   предыдущая версия. Сверьте матрицу проверок с [verification.md](verification.md)
   и выполните пробный запуск с ограниченной группой пользователей.

### Описание выпуска

Исправлены права на ярлык, иконки меню и трея и шрифт в пакетах Linux.
Меню приложений и трей могут читать эти файлы после установки пакета.

- Windows x64 — `yanecompanion-0.3.1-windows-amd64-setup.exe`. Установщик без
  Authenticode, Windows может показать предупреждение о неизвестном издателе.
- Debian 12 и Ubuntu 24.04 x64 — `yanecompanion_0.3.1_amd64.deb`.
- Arch Linux x64 — `yanecompanion-bin-0.3.1-1-x86_64.pkg.tar.zst`.
- Linux x64 — `yanecompanion-0.3.1-linux-amd64.tar.gz`.

Контрольные суммы SHA-256 находятся в `packages.json` этого релиза.

## Обслуживание канала обновлений

Срок манифеста — 14 дней. Перевыпускайте его до истечения срока, например раз в
неделю. Передайте последний опубликованный `release.json` в `-PreviousManifest`,
увеличьте `-Sequence` и используйте исходные файлы опубликованных пакетов. При
переподписи той же версии скрипт сверяет состав, URL, размеры и SHA-256 и сохраняет
запись предыдущей версии, если она есть. При новом релизе прежний текущий выпуск
становится доступен для явного отката. В первом выпуске откат отсутствует.

Установку и удаление пакетов описывает [руководство](installation.md).
