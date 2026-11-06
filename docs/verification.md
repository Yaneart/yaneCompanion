# Проверки и границы runtime

## LC-005 — локальный приём раздачи (2026-11-06)

Дополнение Internet/UDP, MatriX.148-companion.4 (2026-11-06):

- Connected IPv4 UDP tracker sockets используют тот же destination guard после DNS
  до первой отправки; private/special-purpose/local interface subnets запрещены.
  Весь exchange ограничен 15 s; отмена закрывает socket. DHT/uTP/PEX, IPv6,
  local discovery, proxy и port mapping остаются отключены.
- Windows guard tests и скопированные upstream tracker/socket/metadata tests PASS,
  включая localhost/private/IPv6 denial и cancellation. Guard tests также PASS
  в Debian non-root/read-only/network-none. Bundled raw-auth/late-stream regression PASS.
- Windows/Linux engines из двух свежих source extractions совпали побайтно;
  оба host targets повторно совпали. Lock, source recipe и build proofs обновлены.
  Полный аудит обоих bundle подтвердил каждый artifact/patch hash; в source recipe
  добавлен пропущенный `scripts/assemble.ps1`, необходимый для проверки rebuild lock.
- Настоящий Sintel из Интернета: `.torrent` и cold UDP magnet получили по
  1 048 576 bytes. Тест выбирает самый большой файл: первый в этом multi-file
  torrent — крошечные субтитры. Webseeds/WSS удалены только из test fixture;
  info dictionary/hash сохранён, production validator не менялся.
- Isolated Linux swarm PASS: `.torrent`, cold HTTP magnet, cold UDP magnet —
  по 1 MiB. Synthetic addresses и client /32 остаются только внутри internal network.
- Playwright с настоящими Public localhost:5173/backend:3000 и Windows helper:
  Sintel cold UDP magnet и Frieren SubsPlease S2-09 из реального поиска — список
  файлов, явный выбор видео, по 1 048 576 bytes. Без mocks, CORS/permission overrides.
  Прямой upload Sintel `.torrent` также получил 1 MiB; stop очистил picker,
  controller DELETE и отзыв временного pairing вернули 204. Обычный grant сохранён.
- Исходная Knaben AMB3R остаётся без metadata: 6 UDP trackers ответили и вернули
  peers, проверенные TCP endpoints не завершили handshake (timeout/EOF).
  Успех этой раздачи не заявляется. Ubuntu HTTPS tracker отвечает, но единственный
  пир объявляет кусок из середины ISO; начальный префикс не получен за 120 s.
- Диагностические файлы, snapshots и screenshots удалены по запросу пользователя 2026-11-06; результаты проверок сохранены в этом документе.
  Ни LC-005, ни LC-006 автоматически не закрывались/начинались.

Ниже сохранены результаты предыдущих проверок `.3` и основы LC-004.

Реализация подготовлена; ручная приёмка и новые пользовательские коммиты ещё ожидаются.
Go сохранён по решению пользователя. Полный контракт —
[companion](../../yaneMedia/docs/companion.md#реализованная-граница-lc-005).

- Focused Windows checks: locator/session/API/pairing/engine, startup compilation и
  bundled raw-auth test. Проверены строгий ввод, ownership, replay до CAS, отмена
  revision 1, lease/revoke и отсутствие поздней публикации; heartbeat касается
  только metadata своей idle session и не запускает piece download.
- Debian/Ubuntu/Arch containers: изменённые packages, raw-auth и targeted app lifecycle проходят с network
  none/read-only/non-root. Это Linux userspaces, не native desktop acceptance.
- Настоящий MatriX.148-companion.3 в isolated internal Docker swarm: `.torrent`
  и cold magnet получают ровно 1 MiB из fixture 2 MiB, затем Drop. Peer metadata
  проходит тот же validator до storage. В тесте нет Internet egress; synthetic IPv4
  и client /32 находятся только в test namespace. Production engine/guard/args не
  изменяются; helper API не предоставляет сетевой test mode.
- Публичные Ubuntu swarms не дали bytes за deadline 120 s: старый torrent отклонён
  tracker, актуальный даёт пира без отдачи. Это не PASS Internet swarm delivery.
- Backend: 114 focused tests discovery/catalogs/input/security, включая отдельный
  `TorrentDiscoveryModule` без TorrServer/FFmpeg, guest/account isolation, CSRF,
  rate/concurrency и недопущение provider passkeys в locators.
- Frontend: 44 focused tests — отдельные `test:companion` DOM processes, discovery boundary/sorting/
  mode resolution и Home view lifecycle; focused TypeScript/lint/format.
  Общий процесс для нескольких happy-dom registrations не используется, как и в
  существующем `test:torrent` script.
- Настоящий Public Vite preview localhost:5174 → helper в отдельном test profile:
  pairing, manual metainfo, два файла, stop. Empty/stale discovery — browser
  fixtures; CORS read-only API текущего Home backend подставлен тестом. Browser
  permissions не переопределялись. На 375 px нет горизонтального overflow.
- Engine пересобран дважды из свежих pinned sources; Windows/Linux SHA-256
  совпали и обновлены в lock. Повторные host builds фиксируются в runtime-build.json.

Focused повтор: `go test ./internal/locator ./internal/sessions ./internal/api
./internal/pairing ./internal/engine ./internal/app`. Для raw auth задать
`YANE_TEST_EXECUTABLE`. Для deterministic peer transfer: `scripts/check-swarm.ps1
-Go <portable go.exe> -EngineSource <server path from scripts/engine.ps1>`.
Скрипт удаляет только собственные test containers/network; scratch остаётся в `.tools`.
Ни один полный project suite/build не требуется для этого focused прогона.
Native целевых ОС, HTTPS/LNA, player/media и длительный playback остаются LC-014.

Независимая повторная проверка LC-005 (2026-11-06) выявила два исправленных
дефекта: зависшее pending после `pagehide` во frontend и повторный admission из
запоздавшего `/stream` после Drop. В `.3` `/stream` возвращает 404 для отсутствующей
или оставшейся только в DB раздачи. Bundled regression проверяет активную playlist
(200), затем Drop, запоздавший stream (404) и отсутствие hash в списке.
Новый тест падал на `.2`; на `.3` проходит в Windows и Debian/Ubuntu/Arch containers.
Engine и host повторно собраны побайтно одинаково; `.torrent` и cold magnet снова
передали по 1 MiB в isolated swarm. Browser preview и публичные Internet swarms
в этой повторной проверке не запускались. Ручная приёмка и коммиты ещё ожидаются.

## Предыдущая принятая основа

LC-003 реализует минимальный runtime. Код зафиксирован и опубликован пользователем
в коммите `0c8d2b82b4388f89702858c0a5fdb567efeb4312`; совпадение с `main` на origin
проверено. **LC-003 принят и закрыт 2026-11-05** по явному согласию пользователя:
обязательная native-приёмка целевых ОС перенесена в финальный этап LC-014.
Это перенос срока проверки; native результаты пока не получены и не получают PASS.
LC-004 принят пользователем и закрыт 2026-11-05 после ручной приёмки.
Коммиты yaneMedia/yaneCompanion пользователь создаст отдельно; текущая приёмка
фиксирует проверенную рабочую копию. Следующий LC-005 не начат.
Сопряжение и отзыв работают; website torrent admission, media sessions, tray и
installers остаются будущими этапами.

## LC-004: сопряжение и границы engine

Focused Go проверки `build/engine`, `internal/api`, `internal/pairing`, `internal/app`,
`internal/engine` прошли на Windows development host. API, pairing, egress и настоящий
bundled engine проверены также в трёх указанных ниже Linux containers с UID 65532,
без внешней сети, capabilities и writable root.

- Exact Host/Origin, JSON/preflight, отсутствие и отзыв proof, чужой origin,
  malformed/duplicate JSON, отмена до и после подтверждения, expiry, 5/min,
  verifier-only persistence и восстановление после нового boot — PASS.
- Management proof отделён от CLI bearer, живёт 5 минут; новый proof отзывает
  предыдущий. UI не раскрывает proof в HTML, запрещает framing, имеет CSP и
  требует собственный exact Origin — PASS.
- MatriX.148-companion.1 требует Basic auth до всех raw routes и запрещает browser
  Origin. Тест загружает настоящее `.torrent` с metadata `fixture.mp4`, проверяет
  его наличие, затем получает 401 для GET/HEAD stream/play/m3u — PASS.
- Реальные upstream peer/tracker hooks не достигают локального TCP service;
  HTTP redirect на private адрес блокируется, неподдержанные адреса и UDP trackers
  отклоняются. Враждебная конфигурация не включает DHT/uTP/PEX/IPv6/discovery/proxy
  обратно — PASS на Windows и в Debian/Ubuntu/Arch userspaces.
- Frontend: `bun test ./test/companionConnection.test.tsx` — 14 PASS, 108 assertions;
  focused strict TypeScript и lint/format изменённых файлов — PASS. Проверены отмена
  во время create/save, StrictMode stale reads, отсутствие запросов при загрузке,
  ошибки permission/storage, version/capability и отзыв.
- Chromium: реальная IndexedDB сохраняется после reload и изолирована между origins;
  desktop/mobile 375px UI помещается без горизонтальной прокрутки. Полный app UI →
  настоящий helper → локальное подтверждение совпавшего кода → grant → revoke — PASS;
  настоящий permission denial даёт правильное сообщение без обхода.

Для browser integration HTML/modules приложения подставлялись из локального Vite
под `https://watch.example`; этот exact origin был явно разрешён только в изолированном
test profile helper. LNA permission в тестовом browser context задавалась через CDP,
защитные флаги и proxy для обхода не применялись. Это проверка CORS/consent/UI,
не приёмка TLS/remote IP/permission prompt настоящего production origin. Публичный
стенд не создавался. CLI opener использует штатный Windows browser / Linux xdg-open;
native desktop/tray проверяется в LC-010/014.

Дополнительно подготовлен явный `--dev-origin` для exact HTTP
`localhost`/`127.0.0.1`/`[::1]`: без флага HTTP отвергается, port/host должны совпасть;
внешние/LAN/похожие hosts, wildcard, userinfo и path/query/fragment отвергаются.
Focused API tests — PASS. На настоящем `http://localhost:5173` с обновлённым frontend
Docker container и изолированным профилем helper прошли approve/deny/cancel,
сохранение после reload, явная проверка и revoke. Подстановки HTTPS origin,
переназначения browser permissions и proxy для этого прогона не использовались.
Публичная HTTPS/LNA приёмка остаётся обязательной отдельно. Ручные команды —
в [README](../README.md#ручная-приёмка-lc-004-на-windows).
Проверка local opt-in дополнительно прошла в Debian container; обе host-сборки
после добавления dev-флага повторно воспроизведены побайтно.

Сетевой режим v1: публичный IPv4 TCP; private/special-purpose и local interface
subnets закрыты после DNS непосредственно перед connect. HTTP transport не берёт
proxy из окружения/настроек. UDP trackers, DHT, uTP, PEX, IPv6, local discovery,
port mapping и отдельный public-IP/DNS discovery отключены в patched source.
Это ограничивает совместимость UDP-only раздач. Никакой website torrent admission
или реальный film playback этим этапом не объявляются.

Техническая основа проверки адресов: [Go Dialer.Control](https://pkg.go.dev/net#Dialer)
и [IANA IPv4 special-purpose registry](https://www.iana.org/assignments/iana-ipv4-special-registry/).
Актуальные browser permissions: [Chrome LNA](https://developer.chrome.com/docs/modern-web-guidance/guides/security/local-network-access).

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
на locked source artifacts. LC-004 добавляет pinned source archive torrent dependency,
hashes patch recipe/policy и новые engine binary hashes. Исходники patch и команды
включены в `sources/companion-engine/`. Чистые извлечения upstream и проверенного
module cache дали одинаковые engine binaries для обоих targets;
`release/engine-build.json` фиксирует hashes и build inputs.

## Открытые gates

1. **LC-014:** native Windows 11 и native Debian/Ubuntu/Arch runtime acceptance,
   перенесённая из LC-003 по согласию пользователя 2026-11-05. Обязательны cold start,
   single instance, exit/crash без оставшихся engine/FFmpeg, busy port, user paths/
   permissions и standalone запуск. Контейнерный прогон не объявляется проверкой
   отдельных kernels, desktop/tray или установки.
2. Native GUI/tray, installers, signing, лицензия собственного кода и полный
   corresponding-source/relinking комплект перед внешней публикацией.
3. LC-005 и далее: validated locators/controllers, actual torrent delivery,
   probe/configure/rolling playback, browser negotiation, subtitles, 4K/HDR и
   длительные resource/UX проверки. `pairing.v1` не обещает эти возможности.

Remote настроен пользователем; первый commit и push подтверждены. LC-003 закрыт
по явной приёмке опубликованной реализации и переносу native-проверок в LC-014.
Защита egress и raw media auth выполнена в LC-004 для ограниченного сетевого режима;
её нельзя снимать при подключении последующих session/playback endpoints.

Временные извлечения engine sources, тестовые binaries, изолированный тестовый
профиль и диагностические ACL logs удалены при закрытии LC-004 по команде пользователя.
Готовые runtime bundles, pinned artifacts, инструменты сборки и caches сохранены
для дальнейшей разработки. Персональные настройки и данные Home не затрагивались.
