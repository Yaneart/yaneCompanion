param([ValidateSet('windows-amd64','linux-amd64')][string]$Target = 'windows-amd64', [string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$goVersion = & $Go version
if ($LASTEXITCODE -ne 0 -or $goVersion -notmatch '^go version go1\.27\.3 ') { throw 'Go 1.27.3 is required' }
$env:GOTOOLCHAIN = 'local'; $env:CGO_ENABLED = '0'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'; $env:GOOS = $Target.Split('-')[0]
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
$suffix = ''; if ($Target -eq 'windows-amd64') { $suffix = '.exe' }
$outputRoot = Join-Path $projectRoot ('dist\' + $Target)
New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
& $Go -C $projectRoot build -trimpath -buildvcs=false -mod=readonly '-ldflags=-buildid= -s -w' -o (Join-Path $outputRoot ('yanecompanion' + $suffix)) ./cmd/yanecompanion
if ($LASTEXITCODE -ne 0) { throw 'Host build failed' }
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Destination $outputRoot -Force
Write-Output "Built $Target"
