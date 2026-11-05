param([ValidateSet('windows-amd64','linux-amd64')][string]$Target = 'windows-amd64')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$manifest = Get-Content -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Raw | ConvertFrom-Json
$suffix = ''; if ($Target -eq 'windows-amd64') { $suffix = '.exe' }
$destination = Join-Path $projectRoot ('dist\' + $Target)
$tools = Join-Path $destination 'tools'
New-Item -ItemType Directory -Force -Path $tools | Out-Null
$sourceEngine = 'TorrServer-' + $Target + $suffix
Copy-Item -LiteralPath (Join-Path $projectRoot ('artifacts\' + $sourceEngine)) -Destination (Join-Path $tools ('torrserver' + $suffix)) -Force
foreach ($name in @('ffmpeg','ffprobe')) {
  Copy-Item -LiteralPath (Join-Path $projectRoot ('.tools\media\' + $Target + '\' + $name + $suffix)) -Destination $tools -Force
}
foreach ($artifact in $manifest.platforms.$Target) {
  if ((Get-FileHash -LiteralPath (Join-Path $tools $artifact.name) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $artifact.sha256) { throw "Built media differs from lock: $($artifact.name)" }
}
foreach ($directory in @('sources','licenses')) {
  Copy-Item -LiteralPath (Join-Path $projectRoot ('.tools\media\' + $directory)) -Destination $destination -Recurse -Force
}
Copy-Item -LiteralPath (Join-Path $projectRoot 'artifacts\torrserver-source.tar.gz') -Destination (Join-Path $destination 'sources') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\TorrServer-GPL-3.0.txt') -Destination (Join-Path $destination 'licenses') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\Go-BSD-3-Clause.txt') -Destination (Join-Path $destination 'licenses') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\x-sys-LICENSE.txt') -Destination (Join-Path $destination 'licenses') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'build') -Destination (Join-Path $destination 'sources') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\media') -Destination (Join-Path $destination 'sources') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Destination $destination -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'THIRD_PARTY_NOTICES.md') -Destination $destination -Force
Write-Output "Assembled $Target (unsigned development bundle)"
