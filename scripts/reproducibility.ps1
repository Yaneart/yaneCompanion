param([string]$Go = 'go', [string]$Version = '0.3.1-dev')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$buildScript = Join-Path $PSScriptRoot 'build.ps1'
$hashes = [ordered]@{}
foreach ($target in @('windows-amd64','linux-amd64')) {
  $suffix = ''; if ($target -eq 'windows-amd64') { $suffix = '.exe' }
  $executable = Join-Path $projectRoot ('dist\' + $target + '\yanecompanion' + $suffix)
  & $buildScript -Go $Go -Target $target -Version $Version
  $first = (Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash.ToLowerInvariant()
  & $buildScript -Go $Go -Target $target -Version $Version
  $second = (Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($first -ne $second) { throw "Host build is not reproducible: $target" }
  $hashes[$target] = $second
}
$proof = [ordered]@{helperVersion=$Version;toolchain='go1.27.3';sourceCommit=$null;developmentBundle=$true;repeatedBuildIdentical=$true;inputLockSHA256=(Get-FileHash -LiteralPath (Join-Path $projectRoot 'release\dependencies.lock.json') -Algorithm SHA256).Hash.ToLowerInvariant();platformSHA256=$hashes}
$json = $proof | ConvertTo-Json -Depth 4
[IO.File]::WriteAllText((Join-Path $projectRoot 'release\runtime-build.json'), $json + "`n", [Text.UTF8Encoding]::new($false))
Write-Output 'Both host targets reproduced byte for byte'
