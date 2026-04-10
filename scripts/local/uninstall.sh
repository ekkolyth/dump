#!/bin/bash
set -euo pipefail

BIN="$(go env GOPATH)/bin/dump"
if [ -f "$BIN" ]; then
  rm "$BIN"
  echo "Removed $BIN"
else
  echo "dump not found at $BIN"
fi
