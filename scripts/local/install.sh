#!/bin/bash
set -euo pipefail

VERSION=$(node -p "require('./package.json').version")
go install -ldflags "-X github.com/ekkolyth/dump/internal/version.Version=${VERSION}-dev" ./cmd/dump
echo "Installed dump v${VERSION}-dev to $(go env GOPATH)/bin/dump"
