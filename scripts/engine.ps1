param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$goVersion = & $Go version
if ($LASTEXITCODE -ne 0 -or $goVersion -notmatch '^go version go1\.27\.3 ') { throw 'Go 1.27.3 is required' }
$env:GOTOOLCHAIN = 'local'; $env:CGO_ENABLED = '0'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'; $env:GOOS = 'windows'
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
$manifest = Get-Content -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Encoding UTF8 -Raw | ConvertFrom-Json
if (!$manifest.engine.patchHashes) { throw 'Missing engine patch lock' }
foreach ($patch in $manifest.engine.patchHashes.PSObject.Properties) {
  if ((Get-FileHash -LiteralPath (Join-Path $projectRoot $patch.Name) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $patch.Value) { throw "Engine patch hash mismatch: $($patch.Name)" }
}
$sourceArchive = Join-Path $projectRoot 'artifacts\torrserver-source.tar.gz'
$expected = ($manifest.sources | Where-Object name -eq 'torrserver-source.tar.gz').sha256
if ((Get-FileHash -LiteralPath $sourceArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) { throw 'Engine source hash mismatch' }
$work = Join-Path $projectRoot ('.tools\engine-build-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
& tar -xzf $sourceArchive -C $work --strip-components=1
if ($LASTEXITCODE -ne 0) { throw 'Engine extraction failed' }
$engineSource = Join-Path $work 'server'
& $Go -C $engineSource mod download github.com/anacrolix/torrent
if ($LASTEXITCODE -ne 0) { throw 'Pinned torrent module download failed' }
& $Go -C $engineSource mod verify
if ($LASTEXITCODE -ne 0) { throw 'Engine module cache verification failed' }
$torrent = & $Go -C $engineSource list -m -json github.com/anacrolix/torrent | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or $torrent.Replace.Version -ne 'v1.2.34' -or $torrent.Replace.Sum -ne $manifest.engine.torrentModuleSum) { throw 'Unexpected torrent module' }
& $Go -C $projectRoot run ./cmd/patch-engine -source $engineSource -torrent $torrent.Replace.Dir -guard (Join-Path $projectRoot 'build\engine')
if ($LASTEXITCODE -ne 0) { throw 'Engine patch failed' }
foreach ($target in @('windows','linux')) {
  $env:GOOS = $target
  $suffix = ''; if ($target -eq 'windows') { $suffix = '.exe' }
  $output = Join-Path $projectRoot ('.tools\engine\' + $target + '-amd64')
  New-Item -ItemType Directory -Force -Path $output | Out-Null
  & $Go -C $engineSource build -trimpath -buildvcs=false -mod=readonly ('-ldflags=-buildid= -s -w -X server/version.Version=' + $manifest.engine.version) -o (Join-Path $output ('torrserver' + $suffix)) ./cmd
  if ($LASTEXITCODE -ne 0) { throw "Engine build failed: $target" }
}
$env:GOOS = 'windows'
& $Go -C (Join-Path $engineSource 'companion-torrent') test -mod=readonly . ./tracker ./companionguard -run 'TestCompanion|TestSpecialAddresses|TestLocalSocketsNeverConnect|TestHTTPRedirectCannotReachPrivateService' -count=1
if ($LASTEXITCODE -ne 0) { throw 'Engine policy tests failed' }
Write-Output "Patched engine sources: $engineSource"
Write-Output 'Engine binaries built. Verify hashes against release/dependencies.lock.json before assembly.'
