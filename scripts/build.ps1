param([ValidateSet('windows-amd64','linux-amd64')][string]$Target = 'windows-amd64', [string]$Go = 'go', [string]$SiteOrigin = '', [string]$Version = '0.2.0-dev')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$goVersion = & $Go version
if ($LASTEXITCODE -ne 0 -or $goVersion -notmatch '^go version go1\.27\.3 ') { throw 'Go 1.27.3 is required' }
$env:GOTOOLCHAIN = 'local'; $env:CGO_ENABLED = '0'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
$linkFlags = '-buildid= -s -w'
if ($Version -notmatch '^\d+\.\d+\.\d+(-dev)?$') { throw 'Invalid helper version' }
$linkFlags += ' -X=yanecompanion/release.Version=' + $Version
if ($SiteOrigin) {
  if ($SiteOrigin -notmatch '^https://[a-zA-Z0-9.:-]+$') { throw 'SiteOrigin must be an exact HTTPS origin' }
  $linkFlags += ' -X=yanecompanion/internal/desktop.SiteOrigin=' + $SiteOrigin
}
if ($Target -eq 'windows-amd64') {
  $winres = Join-Path $projectRoot '.tools\bin\go-winres.exe'
  if (-not (Test-Path -LiteralPath $winres)) {
    $env:GOOS = 'windows'; $env:GOBIN = Join-Path $projectRoot '.tools\bin'
    & $Go install github.com/tc-hib/go-winres@v0.3.3
    if ($LASTEXITCODE -ne 0) { throw 'Windows resource tool build failed' }
  }
  & $winres make --in (Join-Path $projectRoot 'packaging\windows\winres.json') --arch amd64 --out (Join-Path $projectRoot 'cmd\yanecompanion\rsrc')
  if ($LASTEXITCODE -ne 0) { throw 'Windows resources failed' }
  $linkFlags += ' -H=windowsgui'
}
$env:GOOS = $Target.Split('-')[0]
$suffix = ''; if ($Target -eq 'windows-amd64') { $suffix = '.exe' }
$outputRoot = Join-Path $projectRoot ('dist\' + $Target)
New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
& $Go -C $projectRoot build -trimpath -buildvcs=false -mod=readonly "-ldflags=$linkFlags" -o (Join-Path $outputRoot ('yanecompanion' + $suffix)) ./cmd/yanecompanion
if ($LASTEXITCODE -ne 0) { throw 'Host build failed' }
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Destination $outputRoot -Force
foreach ($asset in @('yanecompanion.ico','yanecompanion.png','yanecompanion.desktop')) {
  Copy-Item -LiteralPath (Join-Path $projectRoot ('packaging\' + $asset)) -Destination $outputRoot -Force
}
Write-Output "Built $Target"
