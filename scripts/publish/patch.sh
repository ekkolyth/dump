#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/../.."

CURRENT=$(node -p "require('./package.json').version")
IFS='.' read -r major minor patch <<< "$CURRENT"
NEW="$major.$minor.$((patch + 1))"

node -e "const p=require('./package.json'); p.version='$NEW'; require('fs').writeFileSync('package.json', JSON.stringify(p, null, 2) + '\n')"

echo "Bumped $CURRENT → $NEW"

git add package.json
git commit -m "release: v$NEW"
git push
