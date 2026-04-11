#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/../.."

VERSION=$(node -p "require('./package.json').version")
echo "Triggering release workflow for v$VERSION..."

gh workflow run release.yml -f version="$VERSION"
echo "Triggered. Watch at: https://github.com/$(gh repo view --json nameWithOwner -q .nameWithOwner)/actions"
