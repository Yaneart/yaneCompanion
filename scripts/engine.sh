#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
case "$(go version)" in 'go version go1.27.3 '*) ;; *) echo 'Go 1.27.3 is required' >&2; exit 1 ;; esac
export GOTOOLCHAIN=local CGO_ENABLED=0 GOARCH=amd64 GOAMD64=v1
export GOCACHE="$PWD/.tools/go-cache" GOMODCACHE="$PWD/.tools/go-mod"
# Verify the archives against the same lock used by the runtime.
go run ./cmd/fetch-artifacts
work=$(mktemp -d "$PWD/.tools/engine-build-XXXXXXXX")
tar -xzf artifacts/torrserver-source.tar.gz -C "$work" --strip-components=1
source="$work/server"
go -C "$source" mod download github.com/anacrolix/torrent
go -C "$source" mod verify
dependency=$(go -C "$source" list -m -f '{{.Replace.Dir}}' github.com/anacrolix/torrent)
test "$(go -C "$source" list -m -f '{{.Replace.Version}}' github.com/anacrolix/torrent)" = v1.2.34
test "$(go -C "$source" list -m -f '{{.Replace.Sum}}' github.com/anacrolix/torrent)" = 'h1:ptxsr0IA7kaoDV1x/lrVLmz8s7xQ+nYgGJBHQvwM6Ck='
go run ./cmd/patch-engine -source "$source" -torrent "$dependency" -guard "$PWD/build/engine"
for target in windows linux; do
  suffix=; test "$target" != windows || suffix=.exe
  mkdir -p ".tools/engine/$target-amd64"
  GOOS=$target go -C "$source" build -trimpath -buildvcs=false -mod=readonly '-ldflags=-buildid= -s -w -X server/version.Version=MatriX.148-companion.1' -o "$PWD/.tools/engine/$target-amd64/torrserver$suffix" ./cmd
done
go -C "$source/companion-torrent" test -mod=readonly . ./tracker ./companionguard -run 'TestCompanion|TestSpecialAddresses|TestLocalSocketsNeverConnect|TestHTTPRedirectCannotReachPrivateService' -count=1
printf 'Patched engine sources: %s\n' "$source"
