param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$env:GOTOOLCHAIN = 'local'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
$env:YANE_TEST_EXECUTABLE = Join-Path $projectRoot 'dist\windows-amd64\yanecompanion.exe'
if (!(Test-Path -LiteralPath $env:YANE_TEST_EXECUTABLE)) { throw 'Build and assemble the Windows bundle first' }
& $Go -C $projectRoot test -count=1 ./internal/api ./internal/pairing ./internal/locator ./internal/sessions ./build/engine ./internal/config ./internal/cache ./internal/engine ./internal/jobs ./internal/lifecycle ./internal/media ./test/runtime ./release ./cmd/yanecompanion ./cmd/fetch-artifacts ./cmd/patch-engine
if ($LASTEXITCODE -ne 0) { throw 'Focused runtime checks failed' }
& $Go -C $projectRoot mod verify
if ($LASTEXITCODE -ne 0) { throw 'Go module integrity check failed' }
