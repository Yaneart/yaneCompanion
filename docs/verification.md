# Проверки и границы LC-003

LC-003 реализует минимальный runtime. Этап ещё не закрыт: обязательная native
приёмка целевых ОС и пользовательский commit/hash остаются открытыми. Pairing,
website admission, media sessions, tray и installers не заявляются работающими.

## Среды проверки, 2026-11-05

| Среда                                                | Что проверяется                                              | Статус приёмки                                                   |
| ---------------------------------------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------- |
| Windows 10 Home x64, 10.0.19045                      | Development bundle, OS locks/ACL, Job Object, реальные tools | Development проверка; Windows 10 вне первой support matrix       |
| Windows 11 x64                                       | Native target LC-002                                         | Среда недоступна, не проверено                                   |
| Debian 12 x64 container                              | Нативные Linux binaries в Debian userspace                   | Контейнерный runtime; native desktop/install не проверены        |
| Ubuntu 24.04.5 x64 container                         | Тот же bundle в Ubuntu userspace                             | Контейнерный runtime; native desktop/install не проверены        |
| Arch Linux x64 container, snapshot 20261101.0.620205 | Тот же bundle в Arch userspace                               | Проверка 2026-11-05; native desktop/tray/pacman/yay не проверены |

Container kernel: `6.18.33.2-microsoft-standard-WSL2`. Образы фиксируются digest:

```text
Debian sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
Ubuntu sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852
Arch   sha256:ece6c971fec3c532fd508bce299a446deaf469feecbfe7c353e995bc1a5e7bba
```

Linux checks запускаются с UID/GID `65532`, без capabilities и сети, с read-only
root и read-only bundle. `/tmp` — отдельный tmpfs с `exec` для теста копирования
bundle, ёмкость 3 GiB. Малый tmpfs закономерно вызывает `CACHE_FULL`: свободное
место должно превышать 1 GiB. `--init` обеспечивает reaping после crash test.
Основные Home-сервисы, их volumes и настройки ОС тесты не меняют.

Итоговый focused прогон: **PASS** на Windows development host и во всех трёх
Linux containers. В каждой Linux userspace прошли также crash containment,
реальный media worker и standalone copy/run с пустым `PATH`. В отдельном
Windows-прогоне `Get-Acl` подтвердил единственную user ACE для state и credential
file, а state DACL защищён от наследования.

## Проверяемые контракты

- HTTP: exact Host/Origin, no-store, отклонение DNS-rebinding host, чужого/null/
  отсутствующего origin, отсутствие admission endpoint/capabilities. Local exit
  требует отдельного local bearer и отсутствия browser Origin.
- Lifecycle: cold start, повторный запуск `ALREADY_RUNNING`, local status/exit,
  `PORT_IN_USE`, принудительное завершение host и следующий cold start.
- Process containment: отдельный тестовый host запускает настоящий bundled
  FFmpeg; после принудительного завершения host FFmpeg не остаётся живым.
- Cache: новый boot удаляет старый отмеченный run; неизвестные пользовательские
  файлы не удаляются. Symlink cleanup прошёл на Linux; на текущей Windows этот тест
  явно skipped: у процесса нет разрешения создать symlink.
- Paths: пробелы/кириллица, абсолютные user/XDG roots, отказ на relative roots,
  права Linux directories `0700` и Windows user-only DACL.
- Jobs/media: одна активная и одна ожидающая работа на lane; saturation/cancel;
  PCM → AAC через новую сборку FFmpeg, настоящий ffprobe JSON, decode в PCM,
  отмена работающего worker, ограничение stdout и освобождение очереди.
- Standalone: host и tools копируются в новый каталог с пробелами/кириллицей;
  runtime стартует и выходит с пустым `PATH`, без dev checkout в текущем каталоге.
- Policies: byte-aware окно ограничено 45 s, absolute position использует
  измеренные anchors; NaN/неизвестная duration не превращаются в точную timeline.

Windows focused command: `scripts/check.ps1 -Go .tools\go\bin\go.exe`.
Linux userspaces из Windows: `scripts/check-linux.ps1 -Go .tools\go\bin\go.exe`.
На native Linux: `sh scripts/check.sh` после подготовки bundle.
Пакеты перечислены в scripts явно; yaneMedia не проходит full build/test/lint.

## Воспроизводимость и integrity

FFmpeg/ffprobe 9.0.2: source SHA-256, detached signature и signing key fingerprint
проверены. Linux и Windows executables совпали побайтно после clean rebuild
в одном pinned builder. Версии build packages находятся в
[`release/media/packages.txt`](../release/media/packages.txt), готовые hashes — в
[`release/dependencies.lock.json`](../release/dependencies.lock.json).
Для host выполняется повторная сборка с фиксированными Go flags; hashes готового
host сохраняются в `release/runtime-build.json` после успешной проверки.
Повторные сборки обоих host targets совпали; `scripts/reproducibility.ps1`
воспроизводит проверку. Go module integrity и оба downloader tools проверены
на всех восьми locked source artifacts.

## Открытые gates

1. Native Windows 11 и native Debian/Ubuntu/Arch runtime acceptance. Контейнерный
   прогон не объявляется проверкой отдельных kernels, desktop/tray или установки.
2. Tracker/peer/DHT egress: upstream peer blocklist не покрывает всю политику.
3. Raw engine media auth: stock MatriX.148 допускает playback существующего hash
   без Basic auth в `streamNoAuth`; startup rejection неизвестного hash не
   доказывает защиты активного потока. До проверенной изоляции/patch admission
   безусловно закрыт; runtime не содержит вызова добавления раздачи.
4. Native GUI/tray, installers, signing, лицензия собственного кода и полный
   corresponding-source/relinking комплект перед внешней публикацией.
5. LC-004 и далее: pairing/tokens, validated locators, actual torrent delivery,
   probe/configure/rolling playback, browser negotiation, subtitles, 4K/HDR и
   длительные resource/UX проверки. Пустые capabilities не обещают эти возможности.

Remote, staging, commits и push не создавались. Полное закрытие LC-003 не следует
из наличия development binaries или чистого Git status.

Автоматическая проверка инструментов отклонила cleanup (`blocked by policy`, без
подробной причины). Пользователь удалил `yaneMedia/storage/lc003` и
`yaneCompanion/.tools/acl-fixture`; отсутствие обоих каталогов подтверждено.
Логи `.tools/acl-check.stdout`/`stderr` не входят в Git или bundles. Живых тестовых
host/engine/FFmpeg и тестовых контейнеров после проверок нет.
