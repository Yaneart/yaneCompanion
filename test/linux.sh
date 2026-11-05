#!/bin/sh
set -eu
cat /etc/os-release
uname -r
export HOME=/tmp/yane-test-home YANE_TEST_EXECUTABLE=/suite/yanecompanion
mkdir -p "$HOME"
for check in /suite/checks/*.test; do
  "$check" -test.v
done
