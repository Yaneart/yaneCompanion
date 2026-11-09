#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
target=${1:-linux-amd64}
case "$target" in linux-amd64) suffix= ;; windows-amd64) suffix=.exe ;; *) echo 'Unsupported target' >&2; exit 1 ;; esac
case "$(go version)" in 'go version go1.27.3 '*) ;; *) echo 'Go 1.27.3 is required' >&2; exit 1 ;; esac
export GOTOOLCHAIN=local CGO_ENABLED=0 GOARCH=amd64 GOAMD64=v1
export GOCACHE="$PWD/.tools/go-cache"
export GOMODCACHE="$PWD/.tools/go-mod"
mkdir -p "dist/$target"
flags='-buildid= -s -w'
if [ "$target" = windows-amd64 ]; then
  export GOBIN="$PWD/.tools/bin"
  go install github.com/tc-hib/go-winres@v0.3.3
  "$GOBIN/go-winres" make --in packaging/windows/winres.json --arch amd64 --out cmd/yanecompanion/rsrc
  flags="$flags -H=windowsgui"
fi
export GOOS=${target%-*}
go build -trimpath -buildvcs=false -mod=readonly "-ldflags=$flags" -o "dist/$target/yanecompanion$suffix" ./cmd/yanecompanion
cp release/dependencies.lock.json "dist/$target/"
cp packaging/yanecompanion.ico packaging/yanecompanion.png packaging/yanecompanion.desktop "dist/$target/"
