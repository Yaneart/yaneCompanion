param(
  [Parameter(Mandatory=$true)][string]$Version,
  [Parameter(Mandatory=$true)][UInt64]$Sequence,
  [Parameter(Mandatory=$true)][string]$ReleaseBaseUrl,
  [Parameter(Mandatory=$true)][string]$PublicKey,
  [Parameter(Mandatory=$true)][string]$KeyFile,
  [string]$PreviousManifest = '',
  [string]$Go = 'go',
  [switch]$AllowUnsignedWindows
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$packageRoot = Join-Path $projectRoot 'dist\packages'
if ($Version -notmatch '^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$' -or $Sequence -eq 0 -or $PublicKey -cnotmatch '^[a-f0-9]{64}$') { throw 'Invalid release version, sequence or public key' }
$baseUri = $null
if (-not [Uri]::TryCreate($ReleaseBaseUrl, [UriKind]::Absolute, [ref]$baseUri) -or $baseUri.Scheme -ne 'https' -or -not $baseUri.Host -or $baseUri.UserInfo -or $baseUri.Query -or $baseUri.Fragment) { throw 'ReleaseBaseUrl requires HTTPS without credentials, query or fragment' }
$env:GOTOOLCHAIN = 'local'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
$names = [ordered]@{
  'windows-amd64' = "yanecompanion-$Version-windows-amd64-setup.exe"
  'linux-deb-amd64' = "yanecompanion_${Version}_amd64.deb"
  'linux-arch-amd64' = "yanecompanion-bin-$Version-1-x86_64.pkg.tar.zst"
  'linux-archive-amd64' = "yanecompanion-$Version-linux-amd64.tar.gz"
}
$artifacts = @()
foreach ($platform in $names.Keys) {
  $file = Get-Item -LiteralPath (Join-Path $packageRoot $names[$platform])
  if ($platform -eq 'windows-amd64') {
    $status = (Get-AuthenticodeSignature -LiteralPath $file.FullName).Status
    if ($status -ne 'Valid' -and -not ($AllowUnsignedWindows -and $status -eq 'NotSigned')) { throw 'Windows release requires valid Authenticode or an unsigned installer with explicit AllowUnsignedWindows' }
  }
  $artifacts += [ordered]@{ platform=$platform; name=$file.Name; url=$ReleaseBaseUrl.TrimEnd('/')+'/'+$file.Name; size=$file.Length; sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
}
$now = [DateTimeOffset]::UtcNow
$manifest = [ordered]@{ schema=1; sequence=$Sequence; publishedAt=$now.ToString('yyyy-MM-ddTHH:mm:ssZ'); expiresAt=$now.AddDays(14).ToString('yyyy-MM-ddTHH:mm:ssZ'); current=[ordered]@{ version=$Version; api=[ordered]@{major=1;minor=0}; settingsSchema=1; packages=$artifacts } }
if ($PreviousManifest) {
  & $Go -C $projectRoot run ./cmd/release-manifest -manifest $PreviousManifest -public-key $PublicKey -historical
  if ($LASTEXITCODE -ne 0) { throw 'Previous release manifest verification failed' }
  $envelope = Get-Content -LiteralPath $PreviousManifest -Raw | ConvertFrom-Json
  $prior = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($envelope.payload)) | ConvertFrom-Json
  if ($Sequence -le $prior.sequence) { throw 'Sequence must strictly increase' }
  if ($prior.current.version -eq $Version) {
    if ($prior.current.packages.Count -ne $artifacts.Count) { throw 'Published package set changed; use a new version' }
    foreach ($artifact in $artifacts) {
      $published = $prior.current.packages | Where-Object platform -CEQ $artifact.platform
      if (-not $published -or $published.name -cne $artifact.name -or $published.url -cne $artifact.url -or $published.size -ne $artifact.size -or $published.sha256 -cne $artifact.sha256) { throw 'Published package changed; use a new version' }
    }
    if ($prior.previous) { $manifest.previous = $prior.previous }
  } else {
    $manifest.previous = $prior.current
  }
}
$outputPath = Join-Path $packageRoot 'release.json'
$inputPath = [IO.Path]::GetTempFileName()
try {
  [IO.File]::WriteAllText($inputPath, ($manifest | ConvertTo-Json -Depth 8), [Text.UTF8Encoding]::new($false))
  & $Go -C $projectRoot run ./cmd/release-manifest -manifest $inputPath -packages $packageRoot -key-file $KeyFile -public-key $PublicKey -out $outputPath
  if ($LASTEXITCODE -ne 0) { throw 'Manifest signing failed; nothing has been published' }
} finally {
  Remove-Item -LiteralPath $inputPath
}
Write-Output "Signed manifest ready: $outputPath (not published). Keep previous packages available for rollback."
