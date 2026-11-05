#!/bin/sh
# Artifact download uses Go's standard library to parse and verify the lock.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local
go run ./cmd/fetch-artifacts
