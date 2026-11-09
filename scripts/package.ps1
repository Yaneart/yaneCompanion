param(
  [ValidateSet('windows-amd64','linux-amd64')][string]$Target = 'windows-amd64',
  [string]$Go = 'go',
  [string]$SiteOrigin = '',
  [string]$Compiler = '',
  [string]$ReleaseBaseUrl = '',
  [string]$SignTool = '',
  [string]$SigningThumbprint = '',
  [string]$TimestampUrl = '',
  [switch]$Release
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$version = '0.2.0'
if ($Release -and (-not $SiteOrigin -or -not $ReleaseBaseUrl)) { throw 'Release requires SiteOrigin and ReleaseBaseUrl' }
if ($ReleaseBaseUrl -and $ReleaseBaseUrl -notmatch '^https://[^\s"''?#]+$') { throw 'ReleaseBaseUrl must be HTTPS without query or fragment' }
$helperVersion = if ($Release) { $version } else { "$version-dev" }
& (Join-Path $PSScriptRoot 'build.ps1') -Target $Target -Go $Go -SiteOrigin $SiteOrigin -Version $helperVersion
& (Join-Path $PSScriptRoot 'assemble.ps1') -Target $Target
$bundlePath = Join-Path $projectRoot ('dist\' + $Target)
$outputPath = Join-Path $projectRoot 'dist\packages'
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
Copy-Item -LiteralPath (Join-Path $projectRoot 'packaging\linux\preinst') -Destination (Join-Path $bundlePath 'package-guard') -Force
$puregoRoot = Join-Path $projectRoot '.tools\go-mod\github.com\ebitengine\purego@v0.10.2'
Copy-Item -LiteralPath (Join-Path $puregoRoot 'LICENSE') -Destination (Join-Path $bundlePath 'licenses\purego-Apache-2.0.txt') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot '.tools\go-mod\cache\download\github.com\ebitengine\purego\@v\v0.10.2.zip') -Destination (Join-Path $bundlePath 'sources\purego-v0.10.2.zip') -Force
$hostSources = Join-Path $bundlePath 'sources\companion-host'
New-Item -ItemType Directory -Force -Path $hostSources | Out-Null
foreach ($name in @('cmd','internal','release','scripts','packaging','build','test','go.mod','go.sum','README.md','THIRD_PARTY_NOTICES.md')) {
  Copy-Item -LiteralPath (Join-Path $projectRoot $name) -Destination $hostSources -Recurse -Force
}
if ($Target -eq 'windows-amd64') {
  if (-not $Compiler) { $Compiler = Join-Path $projectRoot '.tools\inno-7.1.1\ISCC.exe' }
  if (-not (Test-Path -LiteralPath $Compiler)) { throw 'Pass -Compiler with Inno Setup 7.1.1 ISCC.exe' }
  $compilerArgs = @('/Qp', "/DBundleDir=$bundlePath", "/DOutputDir=$outputPath", "/DAppVersion=$version")
  if ($Release) {
    if (-not (Test-Path -LiteralPath $SignTool) -or $SignTool.Contains('"') -or $SigningThumbprint -notmatch '^[a-fA-F0-9]{40}$' -or $TimestampUrl -notmatch '^https://[a-zA-Z0-9./:_-]+$') { throw 'Release requires SignTool, SigningThumbprint and HTTPS TimestampUrl' }
    & $SignTool sign /sha1 $SigningThumbprint /fd SHA256 /tr $TimestampUrl /td SHA256 (Join-Path $bundlePath 'yanecompanion.exe')
    if ($LASTEXITCODE -ne 0) { throw 'Host Authenticode signing failed' }
    if ((Get-AuthenticodeSignature -LiteralPath (Join-Path $bundlePath 'yanecompanion.exe')).Status -ne 'Valid') { throw 'Host Authenticode verification failed' }
    $signCommand = '$q' + $SignTool + '$q sign /sha1 ' + $SigningThumbprint + ' /fd SHA256 /tr ' + $TimestampUrl + ' /td SHA256 $f'
    $compilerArgs += '/DSignedRelease'; $compilerArgs += '/Sowner=' + $signCommand
  }
  & $Compiler @compilerArgs (Join-Path $projectRoot 'packaging\windows\setup.iss')
  if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed' }
  $installer = Join-Path $outputPath "yanecompanion-$version-windows-amd64-setup.exe"
  if ($Release -and (Get-AuthenticodeSignature -LiteralPath $installer).Status -ne 'Valid') { throw 'Installer Authenticode verification failed' }
} else {
  & docker build -t yanecompanion-packager -f (Join-Path $projectRoot 'packaging\linux\Dockerfile') (Join-Path $projectRoot 'packaging\linux')
  if ($LASTEXITCODE -ne 0) { throw 'Linux packaging tools failed' }
  & docker run --rm --mount "type=bind,source=$projectRoot,target=/project" yanecompanion-packager sh /project/scripts/package-linux.sh /project/dist/linux-amd64 /project/dist/packages $version $ReleaseBaseUrl
  if ($LASTEXITCODE -ne 0) { throw 'Linux packaging failed' }
}
$artifacts = Get-ChildItem -LiteralPath $outputPath -File | Where-Object { $_.Name -match '\.(exe|deb|zst|gz)$' }
$manifest = @($artifacts | ForEach-Object { [ordered]@{ name=$_.Name; size=$_.Length; sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() } })
[IO.File]::WriteAllText((Join-Path $outputPath 'packages.json'), (([ordered]@{ version=$version; signedWindowsRelease=[bool]($Release -and $Target -eq 'windows-amd64'); artifacts=$manifest } | ConvertTo-Json -Depth 5) + "`n"), [Text.UTF8Encoding]::new($false))
Write-Output "Packages ready: $outputPath (not published)"
