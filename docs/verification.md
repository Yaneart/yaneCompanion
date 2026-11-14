# Проверки и границы runtime

## LC-011 — обновление, диагностика и удаление (2026-11-14)

Реализация `0.3.0-dev` подготовлена по отдельной команде. Один независимый tester
дал PASS после исправлений и перепроверки исходных reproductions; этап ожидает
пользовательской приёмки/коммитов и не закрыт. Production signing/publication,
реальные Win11/Debian/Ubuntu/Arch desktop/HTTPS/LNA/30 min остаются внешними gates.

- Focused Go: `go test ./internal/update ./internal/diagnostics ./internal/app
  ./internal/sessions ./internal/api ./internal/desktop ./cmd/release-manifest
  -run 'TestLC011|TestDesktop|TestSettings|TestStopRevisionOneAndRevokePreventLatePublication|TestLeaseAndExpiredOperationTombstone|TestOwnershipReplayAndFileSelection|TestHeartbeatKeepsOnlyOwnedIdleMetadataAlive' -count=1`.
  Проверены подпись/чужой signer, expiry/replay/sequence collision, unsafe metadata,
  corrupt/oversized/interrupted packages и retry, redirects, явный rollback,
  settings schema/API/downgrade guards, redaction, выборы очистки/оба lock и
  отсутствие website/management update routes. Offline owner tool подписывает и
  повторно проверяет реальные fixture bytes; corruption не заменяет good output.
- Те же focused Linux test binaries выполнены non-root в Debian/Ubuntu/Arch
  userspaces без внешней сети. Symlink-root confinement проверяется там; Windows
  symlink test может пропускаться без privilege. GTK host стартует под Xvfb,
  получает navigation на maintenance notebook и штатно выходит; это не native
  desktop/Wayland/full-tray acceptance.
- Windows 10 native API fixture: существующий consent/restart/revoke/Exit test
  дополнен переключением maintenance view, проверкой unconfigured feed без
  остановки runtime, возвратом к подключениям. Startup-error singleton также PASS.
  Реальный Inno fixture с override minimum 19041 устанавливается и переустанавливается
  в отдельный путь, сохраняет site; silent uninstall сохраняет pairings/cache,
  явный `clean-data --cache --pairings` удаляет только выбранное. Protocol registration
  после fixture удалена. Production installer по-прежнему требует Win11 22000;
  эти Windows 10 результаты не являются Win11 PASS.
- yaneMedia: 16 companion connection и 9 local torrent tests PASS; scoped TypeScript
  import graphs трёх изменённых entrypoints — 0 diagnostics, changed-file
  oxlint/Prettier PASS. Playwright в отдельном guest context: старый helper
  вызывает только GET /hello, версия/инструкция обновления и redacted connection
  diagnostics видны; API major 2 просит обновить сайт. 1280×900 и 360×640 без
  горизонтального переполнения, диалог прокручивается; mobile screenshot просмотрен.
  Browser hello в этих version checks — явные fixtures, не production update server.
- Windows/Linux development packages собраны; test signatures не объявляются
  production подписью. Нет полного project test/lint/typecheck/build yaneMedia,
  Git staging/commit/push, публикации или перехода к LC-012.
- Tester подтвердил два P2 в cleanup: потерю checkpoint при совпадающих XDG
  state/cache roots и traversal внутренней symlink `updates` в посторонние данные.
  Main исправил оба: удаляются только owned `current`/`previous`, checkpoint и
  unknown files сохраняются; symlink/non-directory отвергаются, identity открытого
  каталога сверяется до удаления. Permanent regressions покрывают State==Cache,
  Config==State==Cache, повторный cleanup, внутреннюю/внешнюю symlink.
  Focused Windows и Debian/Ubuntu/Arch PASS; tester заново воспроизвёл оба исходных
  CLI случая на свежем Linux host и подтвердил сохранение данных/явный отказ.
  Windows symlink variants SKIP privilege, Linux variants реально выполнены.
  Также исправлен P3 stale AUR `.SRCINFO`: новая генерация PKGBUILD удаляет её,
  owner выпуска генерирует актуальную после выбора URL. Открытых подтверждённых
  дефектов после финальной перепроверки tester нет.

Нужны владелец Ed25519 key/pinned public key, Authenticode certificate/timestamp,
реальный feed/packages hosting и AUR публикация. Ротация ключа — новый trusted
пакет. Подписанный тестовый manifest с ephemeral key доказывает реализацию verifier,
а не доверие к ещё не опубликованным packages. Инструкции — [установка](installation.md).

## LC-010 — desktop и установщики (2026-11-09)

Реализация подготовлена по команде пользователя; этап **не принят и не закрыт**.
Подпись владельцем, публикация downloads/AUR и native-приёмка целевых desktop ОС
остаются открыты. Инструкции: [установка](installation.md).

- Windows development host **10.0.19045**, amd64: bundled host и установленный
  per-user fixture в пути с пробелами/кириллицей прошли native consent, restart с
  сохранённым origin/grant, revoke, window hide, повторный запуск и Exit.
  Фикстура installer отдельно собрана с override min Windows 10; итоговый package
  требует Windows 11 и корректно отвергает Windows 10. Host PE machine `8664`,
  subsystem GUI `2`; installer PE machine `8664`. Это **не Windows 11 PASS**.
- Debian 12, Ubuntu 24.04, Arch snapshot 2026-11-09: настоящая установка пакетов,
  desktop-file validation, non-root GTK startup/Exit под Xvfb/Openbox и focused
  Linux unit checks. Debian GTK consent подтверждён кликом в окне; сохранение
  site/grant после нового процесса проверено. Системных Node/Bun/FFmpeg нет;
  запускаются bundled tools. Containers/Xvfb **не являются native desktop PASS**.
- Debian `preinst`/`prerm` и Arch pacman PreTransaction `AbortOnFail` блокируют
  reinstall работающего helper; status остаётся доступен. После явного Exit
  reinstall проходит. `/proc/comm` используется вместо недоступного в restricted
  root `/proc/exe`. На Arch отдельно проверены direct package и makepkg recipe.
  Default makepkg strip менял все три tools: добавлены `!strip`/`!debug`, после
  чего результаты побайтово совпадают с archive и сохраняют embedded SHA lock.
  Публичный `yay update` не проверен: AUR release не опубликован.
- Оба host targets дважды воспроизведены byte-for-byte; актуальные hashes и lock
  находятся в `release/runtime-build.json`. Engine/media binaries и их security
  lock не менялись. PNG иконки совпадает по SHA-256 с favicon yaneMedia.
- Root Chromium/Playwright на обычной Public `/media/dune-part-two`: первый
  выбор торрента открыл подключение, код сопоставлен с native окном и подтверждён
  там; выбор автоматически продолжился в единственном video. Real API, consent,
  FFmpeg и player, deterministic file-range fixture вместо Internet swarm.
  Наблюдались playback >165s, повторное подключение без кода, native revoke с
  прекращением просмотра при следующем запросе/heartbeat, server-torrent calls0.
  Backend catalog CORS подставлен только в browser stand; helper guards и
  permissions не обходились. Production HTTPS/LNA/fullscreen/30min — LC-014.
- Проверены manual OS download links3, отсутствие фиктивного URL без release
  settings, fixed `yanecompanion://open`, mobile390x700 и 360x640 без horizontal
  overflow. Download base в browser stand — фиктивный test-only HTTPS host;
  реального скачивания или публикации не было.
- Frontend focused tests: connection14, localTorrent9, installation2,
  deferred-selection4 — PASS; каждый React test file отдельным процессом для
  module-mock isolation. Scoped tsc проверил CompanionDialog, installation model,
  LocalTorrentLaunch и их import graph; changed-file prettier/oxlint — PASS.
- Helper focused Settings/Desktop/Origin/Consent/Management и native tests
  `TestInstalledDesktopConsentAndLifecycle`, `TestDesktopStartupFailureRemainsSingle`
  — PASS. Desktop instance tests проверяют private activation proof, Origin/auth
  rejection, GUI lifetime lock и malformed addresses без panic/non-loopback fetch.
- Ровно один независимый tester подтвердил повторные focused checks. Он нашёл
  два P2: второе GUI при `BUNDLE_MISSING` и panic на malformed activation port.
  Исправлены lifetime `desktop.lock`/minimal local activation IPC, canonical port
  validation и обработкой request-construction error. Оба собственных repro tester
  и новые постоянные regressions повторно прошли. Последнее сообщение tester:
  **final focused recheck PASS**; его сводный финальный ответ прерван usage limit.
  Другие агенты не запускались. Открытых подтверждённых дефектов не оставлено.

Полные project suites/builds/lint/typecheck yaneMedia не запускались; Docker Compose
не перестраивался. Bundled host/package builds относятся непосредственно к LC-010.
Обычный профиль helper, аккаунты и основное окружение проекта не использовались
как test fixtures. Подписанный Windows release, native Windows 11 и Linux desktop,
реальные AUR/yay update и production user install→connect→play остаются непроверены.

## LC-007 — озвучки, субтитры и ресурсы (2026-11-07)

Реализован по команде пользователя. Приёмка и пользовательский коммит предстоят;
обычная media page и production host adapter остаются LC-008.
Один независимый tester после исправлений и повторной проверки — **PASS**;
подтверждённых неустранённых дефектов нет.

- Probe выдаёт language/title/default для audio, language/default/forced/SDH и
  availability для текста. Встроенные SRT/WebVTT/mov_text/text превращаются в VTT,
  ASS/SSA сохраняют стили; PGS/VobSub/неизвестные codecs объявляются недоступными.
  Sidecars принимаются только из той же директории с точным stem видео и суффиксами
  языка/forced/SDH/default, ≤2 MiB; другое имя/серия не подбирается.
- GET/HEAD текста и TTF/OTF используют текущий source read grant и прежние exact
  Host/Origin/lease guards, без cookie/bearer/range/query/raw path. Новая probe
  выдаёт новые track refs, replacement сразу закрывает старые resource URLs.
  HEAD не запускает extraction. Font tables/signature/offsets проверяются;
  attachment names не становятся локальными путями или worker args.
- Lazy text lane: одна активная/одна ожидающая работа независимо от continuous
  HLS, deadline 30 s; окно 180 s lookbehind +120 s ahead, cues абсолютные и ≤120 s.
  Окно учитывает player delay±60 s; cache refill при position+90>coverage сохраняет
  запас для negative delay и очередного обновления cues. Exact-name ASS sidecars
  получают те же approved embedded fonts. Недоступные tracks имеют валидный scoped
  descriptor URL, но GET остаётся404 без запуска worker.
  Вход ≤128 MiB/job, sidecar ≤2 MiB, output ≤2 MiB; превышение входного budget
  не публикует частичное окно. До 16 text tracks и четырёх fonts ≤1 MiB каждый,
  resource cache ≤8 MiB включён в общий media cache ≤128 MiB.
  Смена source/stop/revoke/request abort отменяет worker; cleanup ждёт readers.
- Windows: `go test ./internal/media ./internal/sessions ./internal/api` PASS.
  Новый 192 s fixture: H264 copy, две PCM→AAC дорожки, реальные decoded tones
  440/880 Hz, embedded SRT/ASS, exact-name sidecar, TTF; probe/GET/font/seek156 s/
  fresh refs/old grant/cleanup и отмена extraction без partial cache — PASS.
  Регрессии проверяют−60 s в начале, +60 s с cue20..140 на position190,
  ранний refill0..120→160 при position40 и font descriptor внешнего ASS.
- Реальный seek190/192 s выявил конечный AAC-only moof после видео. Перед
  публикацией host удерживает один bounded fragment до следующего header/EOF.
  Один final tail≤100 ms/64 KiB объединяется с ещё не опубликованным video fragment;
  audio bytes сохраняются, измеренное video time не меняется. Mid-file/повторный/
  oversized tail отклоняется. Проверены actual Prepare190, два moof в одном segment,
  unchanged timeline и прежние high-bitrate/budget/initial-chain regressions.
- Debian bookworm: новые media/policy и API resource-grant checks на bundled Linux
  FFmpeg PASS при UID65534, read-only, network none. Это userspace, не native desktop.
- Frontend: `test/localTracks.test.ts` и scoped TypeScript check модуля/стенда PASS.
  Модуль `localTracks.ts` готовит строгие MSE/omit descriptors и обновляет captions
  по confirmed progress. Он не подключает обычную media page.
- Playwright, registry player 0.1.18, настоящий API/pairing/local consent и FFmpeg,
  deterministic file-range backend вместо torrent networking: реальные browser
  peaks 880.37→439.45 Hz, paused seek156.5 s, audio replacement, intent/rate1.5,
  delay/style, embedded/sidecar cues и видимый ASS с настоящим Ink Free font — PASS.
  Быстрые seek90→32: первый AbortError, последний ready32/rate2/paused. Недоступный
  font route даёт caption error, после Off другой SRT работает; ASR остаётся off.
  Continuous32→157.5 s/rate2: source revision неизменна, coverage92→142.8→181.1 s,
  правильные active cues, progress без ошибок; stop→resource404.
- PiP реально включён: warning `video-only`; ASS/DOM captions в PiP не обещаются.
  Fullscreen проверен действием пользователя, браузер отказал в его включении;
  fullscreen native PASS не заявляется. Целевая desktop/HTTPS/LNA/30 min приёмка LC-014
  остаётся обязательной. Этот стенд не проверяет Internet swarm или обычную media page.
- Независимый Chrome recheck: continuous delay−60 до65.27 s/rate2 с правильными cues
  и ранним refill; actual seek190/192 s, endedtrue, +60 longcue; реальный ASS-sidecar
  FontFace/JASSUB и один video — PASS. Disabled PGS не мешает открыть source,
  unknown text codec объясняется русским текстом. Native/HTTPS/30 min не проверены.
- Финальные Linux track/tail/budget/decoder-chain/API tests — PASS при UID65534,
  read-only/network-none; Windows bundled `TestRuntimeLifecycle` — PASS.
  Оба host targets дважды совпали побайтно; proof и input lock сверены. Engine,
  FFmpeg/ffprobe и dependencies lock не менялись. Полной проверки проекта не было.
- Стенды/FFmpeg/helper остановлены;45723/5174 свободны, нормальный профиль не менялся.
  По отдельному запросу пользователя 2026-11-07 удалены scratch
  `.tools/lc007-{browser,linux,tester-browser}`, media `.playwright-mcp` и `lc007-ass.png`.
  Исходники и основные файлы готовых bundles до/после совпали по SHA-256;
  готовые dist, зависимости, toolchains и build caches сохранены.

Focused повторение (portable Go/cache setup — как в README):

```powershell
$env:YANE_TEST_EXECUTABLE = (Resolve-Path .\dist\windows-amd64\yanecompanion.exe).Path
# Необязательно для Go checks; настоящий font нужен для browser FontFace/JASSUB:
$env:YANE_TEST_FONT = 'C:\Windows\Fonts\Inkfree.ttf'
.\.tools\go\bin\go.exe test ./internal/media ./internal/sessions ./internal/api
```

Browser stand opt-in: `YANE_BROWSER_STAND` — абсолютный отдельный scratch directory;
`go test ./internal/api -run '^TestLC007Browser$' -v -timeout 30m`. Порт45723 должен
быть свободен. `manage.url` в scratch открывает настоящее локальное подтверждение.
В yaneMedia/frontend запустить Vite на localhost:5174 и открыть
`/test/localTracks.html`. Файл `stop` в scratch штатно завершает stand. Нормальный
пользовательский профиль не читается. Browser page — test source, в сборку сайта не входит.

## LC-006 — локальный media API (2026-11-06)

Принят и закрыт 2026-11-06; пользовательский коммит `c3fe04a` проверен.
Обычный Public UI сохраняет LC-005 picker. App adapter — LC-008; результаты LC-007 выше.

- Focused Windows: media/session/API/engine contracts и bundled runtime lifecycle.
  Настоящие ffprobe/FFmpeg проверяют непрерывный 96 s H.264 fixture, ready prefix,
  pause/backpressure при целиком buffered входе, продолжение далее 45 s, eviction,
  успешный absolute EOF/ENDLIST, seek к measured GOP и очистку source.
- PCM→AAC stereo проверен настоящим encode/probe/decode при неизменённом H.264.
  Numeric AVProfile values pinned `--enable-small` build учтены; 1440p/4K/HDR guards
  сохраняют исходный video и требуют browser MIME. Фактический 4K/HDR playback на
  целевом железе не проверялся; video SDR encoders/hardware acceleration не объявлены.
- API: probe перед configure, replay до CAS, отмена pending probe без остановки
  session и без late publish, source id/revision, отдельный read grant, точный Origin,
  отказ wrong bearer/Range/query/path, HEAD, old-grant 404 при replacement/revoke.
- Debian bookworm контейнер: те же реальные continuous/audio fixtures и media API/
  cancel checks PASS с network none, read-only, UID 65534, без capabilities.
  Это Linux userspace, не native desktop/install acceptance.
- Настоящий Internet Sintel (исходный info/hash, только test outer WSS/webseeds
  удалены): probe + ready + decode video/audio в 0 s и 63.3 s. Anchor второго
  source 62.25 s, mediaAnchor 0.083333 s; сохранены H.264 1024x436 и AAC 6 channels.
- Playwright: изолированный helper profile, обычное локальное подтверждение pairing,
  установленный registry `@yaneart/yaneplayer 0.1.18`, strict MSE/omit/redirect error.
  Без browser routes, permission/CORS overrides или подмены torrent/media. Sintel
  непрерывно прошёл 264 s с одной revision, pause не изменила позицию; seek 63.3 s
  сохранил paused intent и rate 2, old read URL вернул 404. Stop/revoke закрыли grant.
- MatriX.148-companion.4, его guard, media tool binaries, зависимости и lock не
  изменены. Windows/Linux host bundles повторно собираются; hashes в runtime proof.
- Не проверены native Windows 11/Debian/Ubuntu/Arch, новый HTTPS/LNA прогон,
  полный app player adapter и 30 min просмотр. Native/HTTPS/длинная приёмка — LC-014.
- Один независимый tester: PASS после исправления P1 startup stall. Его unchanged
  воспроизведение с двумя fragments по 25 MiB раньше останавливалось на 4 s без ready;
  теперь ready доступен по реальному префиксу. Permanent regressions также проверяют
  `RESOURCE_LIMIT` до недоступной seek-позиции и сохранение initial decoder chain до
  confirmed progress. Open responses pinning проверен отдельно для Windows eviction.
  Дополнительно tester выполнил три запуска настоящего короткого 1 s фильма и
  финальный `TestRuntimeLifecycle` на готовой Windows-сборке — PASS.
- Финальные Windows/Linux host builds повторены побайтно; runtime proof и lock
  сверены с готовыми executables. Последний browser pass на финальном bundle:
  настоящий кадр Sintel, seek 63.3 s, `video.mode=copy`, AAC 6ch copy, revoke→404.
- По отдельному запросу пользователя 2026-11-06 удалены временные каталоги
  `.tools/lc006-review/`, `.tools/lc006-tester-repro/` и
  `../yaneMedia/.playwright-mcp/`. Исходники и основные файлы Windows/Linux bundles
  до/после совпали по 1021 SHA-256; ready dist, зависимости и build caches сохранены.
  Старые scratch paths/Internet fixture отсутствуют; для `TestTorrentMedia` нужна
  новая проверенная локальная `.torrent` fixture. Первоначальный отказ удаления
  снят отдельным разрешённым запуском вне sandbox; обходов не использовалось.
  Тестовые grants отозваны, helper/private engine и локальный стенд остановлены;
  обычный пользовательский профиль и Docker services проекта не менялись.

Повторение focused checks:

```powershell
$env:YANE_TEST_EXECUTABLE = (Resolve-Path .\dist\windows-amd64\yanecompanion.exe).Path
.\.tools\go\bin\go.exe test ./internal/media ./internal/sessions ./internal/api ./internal/engine
.\.tools\go\bin\go.exe test ./test/runtime -run TestRuntimeLifecycle
# Только при наличии проверенного .torrent и доступных peers:
$env:YANE_TEST_TORRENT = 'C:\path\to\fixture.torrent'
.\.tools\go\bin\go.exe test ./internal/media -run TestTorrentMedia -v
```

Ниже — историческая приёмка LC-005; «LC-006 не начат» описывает тот checkpoint.

## LC-005 — локальный приём раздачи (2026-11-06)

LC-005 принят пользователем и закрыт 2026-11-06 после браузерных проверок и
подтверждения пользовательских коммитов: yaneMedia `9c2d4b9`, yaneCompanion `f2a8772`.
LC-006 ещё не начат. Проверки подтверждают локальную загрузку, без playback;
ограничения конкретных раздач и обязательная native/HTTPS-приёмка остаются ниже.
После приёмки помощник и его приватный engine штатно остановлены; слушающих
процессов на проверенных портах проекта нет. Docker остановлен пользователем.

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
  LC-006 не начат; следующий этап выполняется по отдельной команде пользователя.

Ниже сохранены результаты предыдущих проверок `.3` и основы LC-004.

Эта проверенная рабочая копия вошла в подтверждённые коммиты LC-005.
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
в этой повторной проверке не запускались; Internet/Playwright результаты .4 приведены выше.

## Предыдущая принятая основа

LC-003 реализует минимальный runtime. Код зафиксирован и опубликован пользователем
в коммите `0c8d2b82b4388f89702858c0a5fdb567efeb4312`; совпадение с `main` на origin
проверено. **LC-003 принят и закрыт 2026-11-05** по явному согласию пользователя:
обязательная native-приёмка целевых ОС перенесена в финальный этап LC-014.
Это перенос срока проверки; native результаты пока не получены и не получают PASS.
LC-004 принят пользователем и закрыт 2026-11-05 после ручной приёмки.
Проверенные коммиты LC-004: yaneMedia `1c71769`, yaneCompanion `505cc0d`.
Сопряжение и отзыв работают; website torrent admission впоследствии добавлен
и принят в LC-005. Media sessions, tray и installers остаются будущими этапами.

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
