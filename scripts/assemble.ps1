param([ValidateSet('windows-amd64','linux-amd64')][string]$Target = 'windows-amd64')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$manifest = Get-Content -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Raw | ConvertFrom-Json
$suffix = ''; if ($Target -eq 'windows-amd64') { $suffix = '.exe' }
$destination = Join-Path $projectRoot ('dist\' + $Target)
$tools = Join-Path $destination 'tools'
New-Item -ItemType Directory -Force -Path $tools | Out-Null
function Copy-ChangedTool([string]$Source, [string]$Destination) {
  # An unchanged verified tool may be in use by the normal running helper.
  if (-not (Test-Path -LiteralPath $Destination) -or (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash) {
    Copy-Item -LiteralPath $Source -Destination $Destination -Force
  }
}
$sourceEngine = Join-Path $projectRoot ('.tools\engine\' + $Target + '\torrserver' + $suffix)
Copy-ChangedTool $sourceEngine (Join-Path $tools ('torrserver' + $suffix))
foreach ($name in @('ffmpeg','ffprobe')) {
  Copy-ChangedTool (Join-Path $projectRoot ('.tools\media\' + $Target + '\' + $name + $suffix)) (Join-Path $tools ($name + $suffix))
}
foreach ($artifact in $manifest.platforms.$Target) {
  if ((Get-FileHash -LiteralPath (Join-Path $tools $artifact.name) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $artifact.sha256) { throw "Built media differs from lock: $($artifact.name)" }
}
foreach ($directory in @('sources','licenses')) {
  Copy-Item -LiteralPath (Join-Path $projectRoot ('.tools\media\' + $directory)) -Destination $destination -Recurse -Force
}
Copy-Item -LiteralPath (Join-Path $projectRoot 'artifacts\torrserver-source.tar.gz') -Destination (Join-Path $destination 'sources') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'artifacts\torrent-v1.2.34.zip') -Destination (Join-Path $destination 'sources') -Force
$engineRecipe = Join-Path $destination 'sources\companion-engine'
New-Item -ItemType Directory -Force -Path (Join-Path $engineRecipe 'cmd'), (Join-Path $engineRecipe 'scripts') | Out-Null
Copy-Item -LiteralPath (Join-Path $projectRoot 'cmd\patch-engine') -Destination (Join-Path $engineRecipe 'cmd') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'cmd\fetch-artifacts') -Destination (Join-Path $engineRecipe 'cmd') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'scripts\engine.ps1') -Destination (Join-Path $engineRecipe 'scripts') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'scripts\engine.sh') -Destination (Join-Path $engineRecipe 'scripts') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'scripts\assemble.ps1') -Destination (Join-Path $engineRecipe 'scripts') -Force
New-Item -ItemType Directory -Force -Path (Join-Path $engineRecipe 'build'), (Join-Path $engineRecipe 'release'), (Join-Path $engineRecipe 'artifacts') | Out-Null
Copy-Item -LiteralPath (Join-Path $projectRoot 'build\engine') -Destination (Join-Path $engineRecipe 'build') -Recurse -Force
New-Item -ItemType Directory -Force -Path (Join-Path $engineRecipe 'internal') | Out-Null
Copy-Item -LiteralPath (Join-Path $projectRoot 'internal\locator') -Destination (Join-Path $engineRecipe 'internal') -Recurse -Force
foreach ($name in @('go.mod','go.sum')) { Copy-Item -LiteralPath (Join-Path $projectRoot $name) -Destination $engineRecipe -Force }
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Destination (Join-Path $engineRecipe 'release') -Force
foreach ($name in @('torrserver-source.tar.gz','torrent-v1.2.34.zip')) { Copy-Item -LiteralPath (Join-Path $projectRoot ('artifacts\' + $name)) -Destination (Join-Path $engineRecipe 'artifacts') -Force }
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\TorrServer-GPL-3.0.txt') -Destination (Join-Path $destination 'licenses') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\Go-BSD-3-Clause.txt') -Destination (Join-Path $destination 'licenses') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\x-sys-LICENSE.txt') -Destination (Join-Path $destination 'licenses') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'build') -Destination (Join-Path $destination 'sources') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\media') -Destination (Join-Path $destination 'sources') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Destination $destination -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'THIRD_PARTY_NOTICES.md') -Destination $destination -Force
Write-Output "Assembled $Target (unsigned development bundle)"
