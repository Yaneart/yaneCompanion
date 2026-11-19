# Проверки yaneCompanion и открытая приёмка

Состояние на 2026-11-19: development bundle **0.3.0-dev**, engine
**MatriX.148-companion.5**. LC-003–LC-011 приняты пользователем;
native/release-приёмка не заменяется прошедшими fixture/container проверками.
Этот документ объединяет действующие результаты; промежуточные журналы удалены.

## Подтверждённые проверки

| Область               | Проверено                                                                                                                              | Граница                                                                                                    |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Runtime/egress        | Host/Origin/auth, pairing, singleton/порт, process containment, cache ownership, range/peer guards                                     | Windows development host, Debian/Ubuntu/Arch userspaces; не полная native-матрица                          |
| Admission/controllers | Validated locators, limits, CAS/idempotency, cancellation, leases, revoke/Stop и stale publication                                     | Целевые unit/API/browser проверки                                                                          |
| Media/captions        | Реальные FFmpeg, measured clocks, seek/selected duration/audio tails, bounded subtitle windows/index, fonts                            | Неподдерживаемые bitmap/unknown tracks явно недоступны                                                     |
| Раздельное HLS-аудио  | Home/Public MediaVideoRenderer, FFT 440 → 880 Гц, неизменное video/MSE, play/pause, seek, EOF и shorter-track correction               | Registry Player 0.1.24; затем итоговый Public 0.1.25                                                       |
| Muxed EOF tail        | Настоящий FFmpeg, побайтные moof/mdat, continuity/known EOF, <=8 с, прежние byte/cache budgets                                         | TARGETDURATION=8 с первого publish; separate/no-audio=3. Отрицательные сценарии и tester повторно пройдены |
| Probe recovery        | Не более двух попыток в общем бюджете 120 с/32 МиБ, подтверждённый range interruption/internal deadline                                | Invalid input, caller cancellation и resource limits не повторяются                                        |
| Desktop/packages      | Windows 10 fixture с override min version; Debian/Ubuntu/Arch package install, GTK/Xvfb, consent/restart/revoke/Exit                   | Итоговый installer требует Windows 11; Win10/Xvfb не являются Win11/native Linux PASS                      |
| Update/cleanup        | Подписанный ephemeral test manifest, integrity, current/previous rollback, active-session refusal, redacted diagnostics, owned cleanup | Не production feed/signature; symlink-варианты реально проверены на Linux, Windows privilege SKIP          |

В cleanup исправлены совпадающие state/cache roots и traversal через updates symlink:
checkpoint/unknown files сохраняются, чужой/symlink path отклоняется. Регрессии
и независимая перепроверка выполнены. Debian/Arch package guards запрещают reinstall
работающего helper, после Exit допускают; AUR recipe не strip-ит locked tools.

Реальные peer-backed Home → Public запуски фильмов/сериалов, функции и неудачные
альтернативы находятся в [итоговом отчёте](../../yaneMedia/docs/torrent-home-public-review-2026-11-17.md).
Public Chernobyl EOF/auto-next подтверждён после tail fix. Единичные времена запуска
не являются выровненным benchmark или гарантией доступности swarm.

## LC-013 — устойчивость и замеры (2026-11-19)

Реализация проверена в исходниках; пользовательская приёмка не отмечена. Исправлены
гонка cache scan/удаления source directory, runtime disk-pressure shutdown, pending
operations при terminal failure и классификация истёкших engine metadata. Нехватка
места останавливает session с `CACHE_FULL`, сохраняет HTTP/pairing/diagnostics и
запрещает новую admission до восстановления места; повторный запуск просмотра явный.
При старте проверка свободного места по-прежнему выполняется до engine.

Сохранены одна active torrent, восемь controllers, 180 s lease, queues и прежние
byte/cache/font/index limits. Probe: максимум две попытки/120 s/32 МиБ, отменяемый
backoff 250 ms. Сайт: максимум три snapshot/operation/heartbeat запроса при потере
связи, backoff 500/1000 ms и 15 s/request; heartbeat не перекрывается. Auth/permission/
storage/engine errors и admission/seek/audio POST не повторяются автоматически.

Focused проверки PASS: storage pressure во время metadata/select и idle, явное
восстановление, simulated sleep/expired lease, crash containment настоящего FFmpeg,
bounded queues, admission ownership/replay/revoke/cancel, API read grants/late progress,
настоящие probe retries/caller cancellation/audio cancellation и byte-pressure guards.
Гонка cache scan сначала воспроизведена тестом; после исправления три прогона PASS.
Frontend: 6 recovery + 12 hook + 11 playback tests PASS; allowlisted diagnostics PASS.
Подтверждён один controlled Chromium browser прогон с registry Player **0.1.25**,
реальными FFmpeg, frontend localPlayback/localTracks и настоящими API commands.
Изолированный browser transport перенаправлял canonical helper requests на отдельный
loopback port; это не production HTTPS/LNA приёмка. Cleanup: 0 active reads и 0 media dirs.

Устройство: Windows 10 Home x64 **19045**, Intel Xeon E5-2680 v3, 24 logical CPUs,
**15.91 ГиБ RAM**; Chromium **157**. Контролируемый вход: 192 s H.264 с двумя PCM
440/880 Hz дорожками из существующей fixture, чтение локального файла вместо peers.
Fixture generation не включена в media latency. Результаты отдельных прогонов:

| Измерение                            | Результат             | Условия                                                                               |
| ------------------------------------ | --------------------- | ------------------------------------------------------------------------------------- |
| Engine ready                         | 213–214 ms            | Новый isolated engine, без torrent transfer                                           |
| Probe после прерванного range        | 393–643 ms            | Реальный ffprobe, общий bounded retry budget                                          |
| API configure                        | 6.06–6.08 s           | Первые отдельные video/audio producers                                                |
| API seek 72 s                        | 6.60–6.63 s           | Новый источник, file-backed input                                                     |
| API audio switch                     | 2.46–2.50 s           | Видео/source/grant сохранены                                                          |
| Browser prepare                      | 6.56 s                | Frontend adapter + helper probe/configure                                             |
| Browser setSource + play             | 203 ms                | После готовности helper; не time-to-first-frame                                       |
| Browser host seek 120 s              | 6.81 s                | За пределы буфера, source revision изменена                                           |
| Browser audio switch                 | 2.70 s                | Source identity/revision/URL сохранены                                                |
| Повторная буферизация                | 0 событий за 15 s     | Только steady playing после seek/audio, errors=0                                      |
| Pressure cancel                      | 10 ms                 | Заблокированный seek, pending receipt failed, все reads/dirs освобождены              |
| No peers                             | 90.17 s               | Настоящий engine, cold magnet без trackers/DHT/peers; отдельный opt-in тест           |
| Cache после configure/switch         | 148785 / 587618 bytes | Малый fixture, общий hard budget 128 МиБ сохранён                                     |
| Input traffic API-прогона            | 53.5–57.8 MB          | Сумма локальных range reads, не Internet download                                     |
| Input traffic с browser              | 134530603 bytes       | Включает API-прогон и browser, после cleanup active reads=0                           |
| Процессы: sampled peak RSS / private | 62.62 / 90.24 МиБ     | 45 s, 75 samples, test host + обнаруженные children; browser/обычный helper исключены |
| Sampled CPU time                     | 4.70 CPU s            | Те же процессы; короткие workers между samples могут не попасть в сумму               |

Это низкобитрейтный fixture и Windows development host, не reference T480/Arch,
не upper bound для RAM/CPU на тяжёлом видео и не подтверждение Windows 11/native
support. Физическое заполнение диска/исчерпание RAM машины не выполнялось: disk pressure
вводилось на границе monitor→sessions, media budgets проверены отдельно. Реальный
sleep/wake ОС, native install, HTTPS/LNA и 30 min playback остаются LC-014.
Действующий no-cues subtitle limit/429 сохранён. Bundles/packages не пересобирались,
обычный helper, профиль и pairing не менялись.

Повторить focused измерения из корня Companion с env из раздела ниже:

```powershell
& .\.tools\go\bin\go.exe test ./internal/api -run '^TestLC013PlaybackBudgets$' -v -count=1 -timeout 120s
$env:YANE_LC013_LONG = '1'
& .\.tools\go\bin\go.exe test ./internal/api -run '^TestLC013NoPeersDeadline$' -v -count=1 -timeout 150s
```

Опциональный `YANE_LC013_REVIEW` — отдельная абсолютная scratch directory с browser
harness. Test пишет временный stand.json с test-only credential, удаляет его после
закрытия и ждёт stop file; normal port/profile не используется. Browser harness и
сырые логи task-прогона удалены, результаты сохранены только здесь.

## Действующие ограничения

- У раздач без пригодных subtitle cues fallback может достигнуть 128 МиБ и вернуть
  RESOURCE_LIMIT/HTTP429; AV продолжает играть. Home с 1 ГиБ может дать иной результат.
  Полное равенство встроенных captions не достигнуто.
- Холодные metadata/probe/seek зависят от сети, pieces/peers и кеша. Быстрый старт
  на каждой раздаче, 4K/HDR и 30 минут непрерывного просмотра не подтверждены.
- LC-013 реализован и проверен на development host; формальная приёмка не отмечена.
- LC-014: реальные Windows 11, Debian/Ubuntu/Arch desktop, HTTPS/LNA/fullscreen,
  clean install, permissions, sleep/crash и длительный playback. Arch проверяется
  первым после остальных работ; недоступные ОС остаются непроверенными.
- Нужны реальные владелец/ключи/hosting для Authenticode, timestamp, Ed25519 update
  feed и AUR. Development packages unsigned, внешняя публикация не выполнена.
  Corresponding-source/licenses входят в пакеты и сохраняются.

## Сборки и воспроизводимость

Windows/Linux hosts собраны повторно побайтно одинаково после EOF fix.
Актуальные hashes: [runtime-build](../release/runtime-build.json),
[engine-build](../release/engine-build.json), [dependency lock](../release/dependencies.lock.json).
FFmpeg/ffprobe 9.0.2 проверены по исходникам, signature/SHA256 и pinned builder;
[media packages](../release/media/packages.txt) фиксируют build inputs.
Четыре текущих development packages перечислены в dist/packages/packages.json.
Старые 0.2.0 packages, резервный torrserver.exe~ и Go build cache удалены при очистке;
актуальные bundles, исходники, licenses и инструменты сохранены.
После обновления новый host загружается при пользовательском tray Exit/relaunch.
Данные обычного профиля/pairing не используются как test fixture.

## Как повторять проверки

Запускать только проверку затронутого контракта. Например, из корня Companion
для EOF packing (после подготовки bundle):

```powershell
$env:GOTOOLCHAIN = 'local'
$env:GOCACHE = Join-Path (Get-Location) '.tools\go-cache'
$env:GOMODCACHE = Join-Path (Get-Location) '.tools\go-mod'
$env:YANE_TEST_EXECUTABLE = Join-Path (Get-Location) 'dist\windows-amd64\yanecompanion.exe'
& .\.tools\go\bin\go.exe test ./internal/media -run '^TestRealMuxedVideoTailPacking$' -count=1
```

Go cache создаётся автоматически. Полные scripts/check.ps1/check-linux.ps1 требуют
отдельного запроса пользователя. Настройка и упаковка — [installation](./installation.md),
контракты — [README](../README.md). Подготовка изолированного real browser stand
использует свои scratch/profile/credentials; пользовательский helper не останавливается.
