param([string]$Go = 'go', [Parameter(Mandatory=$true)][string]$EngineSource)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$scratch = Join-Path $projectRoot ('.tools\lc005-swarm-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $scratch | Out-Null
$env:GOTOOLCHAIN = 'local'; $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
$env:GOCACHE = Join-Path $projectRoot '.tools\go-cache'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\go-mod'
& $Go -C $EngineSource build -mod=readonly -o (Join-Path $scratch 'seeder') (Join-Path $projectRoot 'build\engine\testdata\swarm\main.go')
if ($LASTEXITCODE -ne 0) { throw 'Seeder fixture compilation failed' }
& $Go -C $projectRoot test -c -o (Join-Path $scratch 'engine.test') ./internal/engine
if ($LASTEXITCODE -ne 0) { throw 'Engine integration test compilation failed' }
$identity = 'lc005-' + [Guid]::NewGuid().ToString('N').Substring(0,12)
$seedName = $identity + '-seed'
$suitePath = Join-Path $projectRoot 'dist\linux-amd64'
$created = $false
try {
  # No Internet egress. Synthetic public addresses exist only in this internal
  # test bridge. A client /32 keeps the fixture outside its local interface
  # prefixes; production guard code and runtime arguments are unchanged.
  & docker network create --internal --subnet 11.247.0.0/24 $identity | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Isolated test network creation failed' }
  $created = $true
  & docker run -d --name $seedName --network $identity --ip 11.247.0.2 --cap-drop ALL --security-opt no-new-privileges --read-only --tmpfs /tmp:rw,nosuid,nodev,size=32m --mount "type=bind,source=$scratch,target=/fixtures" archlinux:base /fixtures/seeder | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Isolated seeder launch failed' }
  $fixture = Join-Path $scratch 'fixture.torrent'
  $deadline = (Get-Date).AddSeconds(20)
  while (!(Test-Path -LiteralPath $fixture)) {
    if ((& docker inspect --format '{{.State.Running}}' $seedName) -ne 'true') { throw 'Isolated seeder stopped before readiness' }
    if ((Get-Date) -gt $deadline) { throw 'Isolated seeder startup deadline' }; Start-Sleep -Milliseconds 100
  }
  & docker run --rm --network $identity --ip 11.247.0.3 --add-host seed.fixture:11.247.0.2 --cap-drop ALL --cap-add NET_ADMIN --security-opt no-new-privileges --read-only --tmpfs /tmp:rw,nosuid,nodev,size=512m --mount "type=bind,source=$scratch,target=/fixtures,readonly" --mount "type=bind,source=$suitePath,target=/suite,readonly" --env YANE_TEST_EXECUTABLE=/suite/yanecompanion --env YANE_TEST_TORRENT=/fixtures/fixture.torrent --env YANE_TEST_MAGNET_TRACKER=http://seed.fixture:18080/announce --env YANE_TEST_UDP_TRACKER=udp://seed.fixture:18081/announce archlinux:base sh -c 'ip address del 11.247.0.3/24 dev eth0 && ip address add 11.247.0.3/32 dev eth0 && ip route add 11.247.0.2/32 dev eth0 && exec /fixtures/engine.test -test.run TestTorrentPrefix -test.v'
  if ($LASTEXITCODE -ne 0) { throw 'Real engine piece delivery failed in the isolated swarm' }
} finally {
  if ($created) {
    & docker rm -f $seedName | Out-Null
    & docker network rm $identity | Out-Null
  }
}
