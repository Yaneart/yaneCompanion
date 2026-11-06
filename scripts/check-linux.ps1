param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$env:GOTOOLCHAIN = 'local'; $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
$suitePath = Join-Path $projectRoot 'dist\linux-amd64'
$runnerPath = Join-Path $projectRoot 'test\linux.sh'
$checks = Join-Path $suitePath 'checks'
New-Item -ItemType Directory -Force -Path $checks | Out-Null
foreach ($package in @('internal/config','internal/lifecycle','internal/cache','internal/jobs','internal/media','internal/engine','internal/api','internal/pairing','internal/locator','internal/sessions','build/engine','test/runtime')) {
  & $Go -C $projectRoot test -c -o (Join-Path $checks ($package.Replace('/','-') + '.test')) ('./' + $package)
  if ($LASTEXITCODE -ne 0) { throw "Test compilation failed: $package" }
}
$images = @(
  'debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587',
  'ubuntu@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852',
  'archlinux@sha256:ece6c971fec3c532fd508bce299a446deaf469feecbfe7c353e995bc1a5e7bba'
)
foreach ($image in $images) {
  & docker run --rm --init --network none --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges --read-only --tmpfs /tmp:rw,exec,nosuid,nodev,size=3g --mount "type=bind,source=$suitePath,target=/suite,readonly" --mount "type=bind,source=$runnerPath,target=/run-checks.sh,readonly" $image sh /run-checks.sh
  if ($LASTEXITCODE -ne 0) { throw "Container checks failed: $image" }
}
