#!/bin/sh
# Builds the React console and embeds it into the Go binary (internal/ui/dist).
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/web"
[ -d node_modules ] || npm ci --no-audit --no-fund
node node_modules/typescript/bin/tsc -b
node node_modules/vite/bin/vite.js build
rm -rf "$root/internal/ui/dist"
cp -R "$root/web/dist" "$root/internal/ui/dist"
touch "$root/internal/ui/dist/.gitkeep"
echo "UI embedded into internal/ui/dist"
