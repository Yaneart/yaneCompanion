# Компоненты development bundle

| Компонент                       | Версия                                           | Условия и материалы                                                               |
| ------------------------------- | ------------------------------------------------ | --------------------------------------------------------------------------------- |
| Go runtime/toolchain            | 1.27.3                                           | BSD-3-Clause; `licenses/Go-BSD-3-Clause.txt`                                      |
| golang.org/x/sys                | 0.49.0                                           | BSD-3-Clause; `licenses/x-sys-LICENSE.txt`; source commit в lock                  |
| TorrServer                      | MatriX.148                                       | GPL v3, отдельный процесс; upstream LICENSE и source archive включены             |
| FFmpeg/ffprobe                  | 9.0.2                                            | Эта build configuration — LGPL v2.1; подписанный source archive и recipe включены |
| glibc / MinGW-w64 / GCC runtime | версии зафиксированы snapshot и package manifest | Фактические copyright/license notices включены в media build output               |

Media flags и полный compiler package inventory поставляются в `release/media/`;
artifact SHA-256 и source URLs — в `release/dependencies.lock.json`.
Готовый bundle должен включать `sources/`, `licenses/` и build recipe, а не только exe.

Это unsigned development bundle. Перед внешней публикацией требуется завершить
лицензионную проверку фактически статически включённых runtime libraries, обеспечить
corresponding source/relinking materials для применимых LGPL/GPL компонентов и
выбрать лицензию собственного кода. Наличие одного файла notices не закрывает эти
требования. Signing, installers и каналы выпуска относятся к LC-010/011.
