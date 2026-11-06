#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 CGO_ENABLED=0
export GOCACHE="$PWD/.tools/go-cache" GOMODCACHE="$PWD/.tools/go-mod"
export YANE_TEST_EXECUTABLE="$PWD/dist/linux-amd64/yanecompanion"
test -x "$YANE_TEST_EXECUTABLE"
go test -count=1 ./internal/api ./internal/pairing ./internal/locator ./internal/sessions ./build/engine ./internal/config ./internal/cache ./internal/engine ./internal/jobs ./internal/lifecycle ./internal/media ./test/runtime ./release ./cmd/yanecompanion ./cmd/fetch-artifacts ./cmd/patch-engine
go mod verify
