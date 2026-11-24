# Разработка yaneCompanion

## Сборка из исходников

Для сборки приложения используется Go из [go.mod](../go.mod). Сборка FFmpeg
и Linux-пакетов на Windows использует Docker. Версии и SHA-256 исходников и
инструментов перечислены в [lock-файле](../release/dependencies.lock.json).

Из корня репозитория в PowerShell загрузите исходники и инструменты командой
`.\scripts\fetch.ps1`. Установите Go нужной версии или распакуйте Windows-архив Go
из `artifacts/` в `.tools/`. Затем соберите приложение:

```powershell
$go = (Resolve-Path .\.tools\go\bin\go.exe).Path
.\scripts\engine.ps1 -Go $go
.\scripts\media.ps1
.\scripts\build.ps1 -Go $go -Target windows-amd64
.\scripts\assemble.ps1 -Target windows-amd64
```

Для установленного Go задайте `$go = (Get-Command go).Source`. Для Linux amd64
укажите `-Target linux-amd64` в двух последних командах. Из Linux можно
использовать `scripts/fetch.sh`, `scripts/engine.sh` и `scripts/build.sh`.

TorrServer собирается из закреплённых исходников с патчами Companion.
FFmpeg и ffprobe собираются из подписанного архива в закреплённом окружении
Debian. Версии сборок фиксируются в [release](../release).
Инструкции по упаковке находятся в [руководстве выпуска](release.md).

## Локальная разработка

Помощник слушает `127.0.0.1:45723`. Для проверки yaneMedia на локальном HTTP
запустите из собранного каталога:

```powershell
.\yanecompanion.exe desktop --dev-origin http://localhost:5173
```

На Linux используйте `./yanecompanion` от обычного пользователя. Для отдельного
HTTPS-сайта укажите точный адрес в настройках. Команды `status` и `exit` работают
с запущенным помощником. `run` запускает его без рабочего окна, `manage` открывает
локальное управление для проверки.

## Устройство проекта

- `cmd/yanecompanion` — команды запуска и обслуживания.
- `internal/desktop` и `internal/app` — окно, трей и жизненный цикл программы.
- `internal/api`, `internal/pairing` и `internal/sessions` — локальный API, сопряжение и сессии.
- `internal/engine` и `internal/media` — управление TorrServer и обработка видео.
- `build`, `scripts`, `packaging` и `release` — сборка, упаковка и проверка инструментов.

Профиль и кэш Windows находятся в `%LOCALAPPDATA%\yaneCompanion`. На Linux
используются каталоги `XDG_CONFIG_HOME`, `XDG_STATE_HOME` и `XDG_CACHE_HOME`.
Результаты установки и команды проверки приведены в [verification.md](verification.md).
