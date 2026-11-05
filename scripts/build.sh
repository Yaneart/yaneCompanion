#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
target=${1:-linux-amd64}
case "$target" in linux-amd64) suffix= ;; windows-amd64) suffix=.exe ;; *) echo 'Unsupported target' >&2; exit 1 ;; esac
case "$(go version)" in 'go version go1.27.3 '*) ;; *) echo 'Go 1.27.3 is required' >&2; exit 1 ;; esac
export GOTOOLCHAIN=local CGO_ENABLED=0 GOARCH=amd64 GOAMD64=v1 GOOS=${target%-*}
export GOCACHE="$PWD/.tools/go-cache"
export GOMODCACHE="$PWD/.tools/go-mod"
mkdir -p "dist/$target"
go build -trimpath -buildvcs=false -mod=readonly '-ldflags=-buildid= -s -w' -o "dist/$target/yanecompanion$suffix" ./cmd/yanecompanion
cp release/dependencies.lock.json "dist/$target/"
