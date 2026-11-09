# Компоненты development bundle

| Компонент                       | Версия                                           | Условия и материалы                                                                    |
| ------------------------------- | ------------------------------------------------ | -------------------------------------------------------------------------------------- |
| Go runtime/toolchain            | 1.27.3                                           | BSD-3-Clause; `licenses/Go-BSD-3-Clause.txt`                                           |
| golang.org/x/sys                | 0.49.0                                           | BSD-3-Clause; `licenses/x-sys-LICENSE.txt`; source commit в lock                       |
| ebitengine/purego (Linux UI)    | 0.10.2                                           | Apache-2.0; `licenses/purego-Apache-2.0.txt`; source ZIP included                     |
| TorrServer                      | MatriX.148-companion.1                           | GPL v3, отдельный процесс; upstream source, изменённые patch sources и recipe включены |
| FFmpeg/ffprobe                  | 9.0.2                                            | Эта build configuration — LGPL v2.1; подписанный source archive и recipe включены      |
| glibc / MinGW-w64 / GCC runtime | версии зафиксированы snapshot и package manifest | Фактические copyright/license notices включены в media build output                    |

Media flags и полный compiler package inventory поставляются в `release/media/`;
artifact SHA-256 и source URLs — в `release/dependencies.lock.json`.
Готовый bundle должен включать `sources/`, `licenses/` и build recipe, а не только exe.
Companion build изменяет обязательность raw HTTP auth и ограничивает исходящие
соединения в TorrServer и его закреплённой зависимости `tsynik/torrent v1.2.34`.
Patch recipe, policy, dependency source archive и build inputs поставляются в
`sources/companion-engine/`; исходные версии и hashes находятся в lock.

Это unsigned development bundle. Перед внешней публикацией требуется завершить
лицензионную проверку фактически статически включённых runtime libraries, обеспечить
corresponding source/relinking materials для применимых LGPL/GPL компонентов и
выбрать лицензию собственного кода. Наличие одного файла notices не закрывает эти
требования. Установщики и путь Authenticode signing реализованы в LC-010;
внешняя публикация выполняется владельцем.

Linux UI использует системные GTK 3, GLib и libayatana-appindicator3, которые
устанавливает пакетный менеджер из подписанных репозиториев дистрибутива. Они не
включены в bundle; их версии и лицензии доступны в package database ОС. На Debian
12 это `libgtk-3-0` и `libayatana-appindicator3-1`, на Ubuntu 24.04 GTK может
разрешаться через пакет `libgtk-3-0t64`, на Arch — `gtk3` и
`libayatana-appindicator`. Отсутствие tray host не прячет единственное окно.

Build-only tools: go-winres 0.3.3 (MIT), Inno Setup 7.1.1 (Inno Setup License),
dpkg-deb и libarchive/zstd из закреплённого Debian builder image. Они не нужны
пользователю. Иконка взята без изменения рисунка из yaneMedia `frontend/public/favicon.png`.
