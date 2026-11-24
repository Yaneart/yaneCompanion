# Выпуск yaneCompanion 0.3.0

Репозиторий распространения — `Yaneart/yaneCompanion`, тег — `v0.3.0`,
название выпуска — **yaneCompanion 0.3.0**.
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
$base = 'https://github.com/Yaneart/yaneCompanion/releases/download/v0.3.0'
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
& .\scripts\release-manifest.ps1 -Go $go -Version 0.3.0 -Sequence 1 `
  -ReleaseBaseUrl $base -PublicKey $publicKey -KeyFile $keyFile -AllowUnsignedWindows
```

Проверьте `dist/packages/packages.json` и подпись `release.json` командой
`go run ./cmd/release-manifest -manifest dist/packages/release.json -public-key <public-key> -packages dist/packages`.
Для будущего выпуска с Authenticode не передавайте `-AllowUnsignedWindows`. При
сборке Windows вместо него задайте `-SignTool`, `-SigningThumbprint` и `-TimestampUrl`.

## GitHub, сайт и AUR

1. Владелец открывает репозиторий и публикует релиз `v0.3.0`. В Assets загружает четыре
   пакета из `packages.json` и сам `packages.json` с контрольными суммами. Проверяет
   анонимное скачивание и SHA-256 каждого файла. Опубликованные пакеты сохраняются
   по своим версионным URL. Исправленный пакет получает новую версию и тег.
2. Владелец публикует `release.json` в ветку `updates` после проверки пакетов.
   Проверяет загрузку манифеста по закреплённому адресу и через «Поддержка» в приложении.
3. Соберите Public frontend с `VITE_COMPANION_DOWNLOAD_BASE_URL`, равным `$base`,
   и `VITE_COMPANION_DOWNLOAD_VERSION=0.3.0`. В Docker Compose передайте их через
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

Первый выпуск yaneCompanion для просмотра торрентов в yaneMedia. В установщики
входят TorrServer и FFmpeg. Приложение поддерживает подтверждение подключения
сайта, светлую и тёмную темы, диагностику и ручную загрузку обновлений.

- Windows x64 — `yanecompanion-0.3.0-windows-amd64-setup.exe`. Установщик без
  Authenticode, Windows может показать предупреждение о неизвестном издателе.
- Debian 12 и Ubuntu 24.04 x64 — `yanecompanion_0.3.0_amd64.deb`.
- Arch Linux x64 — `yanecompanion-bin-0.3.0-1-x86_64.pkg.tar.zst`.
- Linux x64 — `yanecompanion-0.3.0-linux-amd64.tar.gz`.

Контрольные суммы SHA-256 находятся в `packages.json` этого релиза.

## Обслуживание канала обновлений

Срок манифеста — 14 дней. Перевыпускайте его до истечения срока, например раз в
неделю. Передайте последний опубликованный `release.json` в `-PreviousManifest`,
увеличьте `-Sequence` и используйте исходные файлы опубликованных пакетов. При
переподписи той же версии скрипт сверяет состав, URL, размеры и SHA-256 и сохраняет
запись предыдущей версии, если она есть. При новом релизе прежний текущий выпуск
становится доступен для явного отката. В первом выпуске откат отсутствует.

Установку и удаление пакетов описывает [руководство](installation.md).
