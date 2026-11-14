// Package diagnostics constructs reports from allowlisted values, never from
// arbitrary errors, snapshots, locators, command lines or filesystem paths.
package diagnostics

import (
	"fmt"
	"runtime"
	"strings"

	"yanecompanion/release"
)

func Code(err error) string {
	if err == nil {
		return "OK"
	}
	code := err.Error()
	if code == "OK" {
		return code
	}
	for _, allowed := range strings.Fields("PORT_IN_USE BUNDLE_MISSING BUNDLE_HASH_MISMATCH INVALID_LOCK UNSUPPORTED_PLATFORM STATE_UNAVAILABLE SETTINGS_UNAVAILABLE INVALID_SITE_ORIGIN PAIRING_STORAGE_UNAVAILABLE CACHE_CLEANUP_FAILED CACHE_FULL ENGINE_UNAVAILABLE HTTP_UNAVAILABLE PROCESS_CONTAINMENT_UNAVAILABLE UPDATE_NOT_CONFIGURED UPDATE_MANIFEST_INVALID UPDATE_MANIFEST_EXPIRED UPDATE_SIGNATURE_INVALID UPDATE_REPLAY_DENIED UPDATE_CONNECTION_FAILED UPDATE_DOWNLOAD_INVALID UPDATE_DOWNLOAD_INTERRUPTED UPDATE_PACKAGE_INVALID UPDATE_STORAGE_UNAVAILABLE UPDATE_CHECK_REQUIRED UPDATE_ROLLBACK_UNAVAILABLE UPDATE_DOWNGRADE_DENIED UPDATE_SETTINGS_INCOMPATIBLE UPDATE_PLATFORM_UNSUPPORTED UPDATE_PLAYBACK_ACTIVE UPDATE_URL_INVALID NOT_RUNNING ALREADY_RUNNING RESOURCE_LIMIT PREPARATION_TIMEOUT UNSUPPORTED_MEDIA UNSUPPORTED_CAPABILITY NO_PEERS") {
		if code == allowed {
			return code
		}
	}
	return "OPERATION_FAILED"
}

func Report(startup, runtimeError error, activity string, playback error, update string) string {
	state := "idle"
	for _, v := range strings.Fields("metadata awaiting_file probing awaiting_target preparing ready stopping stopped failed") {
		if activity == v {
			state = v
		}
	}
	return fmt.Sprintf("yaneCompanion %s\nOS: %s/%s\nAPI: 1.0; settings: 1\nEngine: %s; FFmpeg: %s\nStartup: %s\nRuntime: %s\nPlayback: %s (%s)\nUpdate: %s\n", release.Version, runtime.GOOS, runtime.GOARCH, release.EngineVersion, release.MediaVersion, Code(startup), Code(runtimeError), state, Code(playback), CodeString(update))
}

func CodeString(value string) string { return Code(fmt.Errorf("%s", value)) }

func Message(err error) string {
	switch Code(err) {
	case "PORT_IN_USE":
		return "Порт помощника занят. Закройте другой экземпляр и повторите запуск."
	case "BUNDLE_MISSING", "BUNDLE_HASH_MISMATCH", "INVALID_LOCK":
		return "Установка повреждена или неполна. Переустановите проверенный полный пакет."
	case "CACHE_FULL":
		return "Недостаточно места для локального просмотра. Освободите место и повторите запуск."
	case "UPDATE_NOT_CONFIGURED":
		return "Обновления пока не опубликованы владельцем."
	case "UPDATE_PLAYBACK_ACTIVE":
		return "Сначала остановите локальный просмотр на сайте."
	case "UPDATE_SIGNATURE_INVALID", "UPDATE_PACKAGE_INVALID", "UPDATE_MANIFEST_INVALID":
		return "Подлинность или целостность обновления не подтверждена. Пакет не готов к установке."
	case "UPDATE_MANIFEST_EXPIRED", "UPDATE_REPLAY_DENIED":
		return "Сведения об обновлении устарели. Проверьте часы компьютера и повторите позже."
	case "UPDATE_CONNECTION_FAILED", "UPDATE_DOWNLOAD_INTERRUPTED", "UPDATE_DOWNLOAD_INVALID":
		return "Не удалось загрузить обновление. Проверьте сеть и повторите попытку."
	case "UPDATE_SETTINGS_INCOMPATIBLE":
		return "Эта версия несовместима с сохранёнными настройками. Настройки не изменены."
	case "UPDATE_ROLLBACK_UNAVAILABLE":
		return "Предыдущая версия пока недоступна."
	case "UPDATE_PLATFORM_UNSUPPORTED":
		return "Для этой системы пакет не опубликован."
	case "UPDATE_CHECK_REQUIRED":
		return "Сначала проверьте обновления."
	default:
		return "Не удалось выполнить действие. Код: " + Code(err)
	}
}
