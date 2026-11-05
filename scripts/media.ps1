$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$output = Join-Path $projectRoot '.tools\media'
& docker build --file (Join-Path $projectRoot 'build\media.Dockerfile') --output "type=local,dest=$output" $projectRoot
if ($LASTEXITCODE -ne 0) { throw 'Media build failed' }
Write-Output "Media tools built at $output"
