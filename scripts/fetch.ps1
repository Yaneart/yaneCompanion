$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$manifest = Get-Content -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Raw | ConvertFrom-Json
$artifactRoot = Join-Path $projectRoot 'artifacts'
New-Item -ItemType Directory -Force -Path $artifactRoot | Out-Null
foreach ($item in $manifest.sources) {
  if ([IO.Path]::GetFileName($item.name) -ne $item.name) { throw 'Invalid source name' }
  $path = Join-Path $artifactRoot $item.name
  if (!(Test-Path -LiteralPath $path)) {
    $partial = $path + '.part'
    & curl.exe --fail --location --silent --show-error --retry 2 --max-time 180 --proto '=https' --output $partial $item.url
    if ($LASTEXITCODE -ne 0) { throw "Download failed: $($item.name)" }
    if ((Get-FileHash -LiteralPath $partial -Algorithm SHA256).Hash.ToLowerInvariant() -ne $item.sha256) { throw "Hash mismatch: $($item.name)" }
    Move-Item -LiteralPath $partial -Destination $path
  }
  if ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $item.sha256) { throw "Hash mismatch: $($item.name)" }
  Write-Output "Verified $($item.name)"
}
